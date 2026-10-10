// Package sqlitestore persists targets and results in SQLite.
//
// It uses the pure-Go driver modernc.org/sqlite, so the binary needs no cgo
// and cross-compiles like any other Go program.
package sqlitestore

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"sort"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/casoon/go-tutorial/internal/monitor"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Store is safe for concurrent use; *sql.DB manages a connection pool.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the database at path and applies migrations.
func Open(ctx context.Context, path string) (*Store, error) {
	q := url.Values{}
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "busy_timeout(5000)")
	// Take the write lock at BEGIN, not at the first write. Otherwise a
	// read-then-write transaction can fail with SQLITE_BUSY mid-way.
	q.Set("_txlock", "immediate")
	dsn := "file:" + path + "?" + q.Encode()

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	// SQLite allows one writer at a time. A small pool is plenty and keeps
	// lock contention low; readers still run in parallel thanks to WAL.
	db.SetMaxOpenConns(4)
	db.SetConnMaxIdleTime(5 * time.Minute)

	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the connection pool.
func (s *Store) Close() error { return s.db.Close() }

// Ping reports whether the database is reachable. Used by /readyz.
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// migrate applies every migration file that has not run yet, each in its
// own transaction, in lexical file order.
func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)

	for _, name := range names {
		if err := s.applyMigration(ctx, name); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
	}
	return nil
}

func (s *Store) applyMigration(ctx context.Context, name string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // no-op after a successful Commit

	var done int
	err = tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schema_migrations WHERE name = ?`, name).Scan(&done)
	if err != nil {
		return err
	}
	if done > 0 {
		return nil
	}

	body, err := migrations.ReadFile(name)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, string(body)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (name) VALUES (?)`, name); err != nil {
		return err
	}
	return tx.Commit()
}

// CreateTarget stores t and returns it with ID and CreatedAt set.
// The first check is due immediately.
func (s *Store) CreateTarget(ctx context.Context, t monitor.Target) (monitor.Target, error) {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO targets (url, interval_seconds, created_at, next_check_at)
		 VALUES (?, ?, ?, ?)`,
		t.URL, int64(t.Interval/time.Second), now.UnixMilli(), now.UnixMilli())
	if err != nil {
		if isUniqueViolation(err) {
			return monitor.Target{}, fmt.Errorf("target %s: %w", t.URL, monitor.ErrDuplicate)
		}
		return monitor.Target{}, fmt.Errorf("insert target: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return monitor.Target{}, fmt.Errorf("insert target: %w", err)
	}
	t.ID = id
	t.CreatedAt = now.Truncate(time.Millisecond)
	return t, nil
}

// Target returns the target with the given id or monitor.ErrNotFound.
func (s *Store) Target(ctx context.Context, id int64) (monitor.Target, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, url, interval_seconds, created_at FROM targets WHERE id = ?`, id)
	t, err := scanTarget(row)
	if errors.Is(err, sql.ErrNoRows) {
		return monitor.Target{}, fmt.Errorf("target %d: %w", id, monitor.ErrNotFound)
	}
	if err != nil {
		return monitor.Target{}, fmt.Errorf("select target %d: %w", id, err)
	}
	return t, nil
}

// Targets returns all targets ordered by id.
func (s *Store) Targets(ctx context.Context) ([]monitor.Target, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, url, interval_seconds, created_at FROM targets ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("select targets: %w", err)
	}
	defer rows.Close()

	targets := []monitor.Target{}
	for rows.Next() {
		t, err := scanTarget(rows)
		if err != nil {
			return nil, fmt.Errorf("scan target: %w", err)
		}
		targets = append(targets, t)
	}
	return targets, rows.Err()
}

// DeleteTarget removes a target and, via ON DELETE CASCADE, its results.
func (s *Store) DeleteTarget(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM targets WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete target %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete target %d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("target %d: %w", id, monitor.ErrNotFound)
	}
	return nil
}

// ClaimDue returns up to limit targets whose next check is due at now and,
// in the same transaction, moves their next check one interval ahead.
// Claiming instead of just selecting means a slow check is never started
// twice by consecutive scheduler ticks.
func (s *Store) ClaimDue(ctx context.Context, now time.Time, limit int) ([]monitor.Target, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx,
		`SELECT id, url, interval_seconds, created_at FROM targets
		 WHERE next_check_at <= ? ORDER BY next_check_at LIMIT ?`,
		now.UnixMilli(), limit)
	if err != nil {
		return nil, fmt.Errorf("select due: %w", err)
	}
	var due []monitor.Target
	for rows.Next() {
		t, err := scanTarget(rows)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan due: %w", err)
		}
		due = append(due, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("select due: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, `UPDATE targets SET next_check_at = ? WHERE id = ?`)
	if err != nil {
		return nil, fmt.Errorf("prepare update: %w", err)
	}
	defer stmt.Close()
	for _, t := range due {
		if _, err := stmt.ExecContext(ctx, now.Add(t.Interval).UnixMilli(), t.ID); err != nil {
			return nil, fmt.Errorf("schedule target %d: %w", t.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return due, nil
}

// SaveResult stores the outcome of a check. A result for a target that was
// deleted in the meantime is silently dropped.
func (s *Store) SaveResult(ctx context.Context, r monitor.Result) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO results (target_id, status_code, duration_ms, error, checked_at)
		 SELECT id, ?, ?, ?, ? FROM targets WHERE id = ?`,
		r.StatusCode, r.Duration.Milliseconds(), r.Err, r.CheckedAt.UnixMilli(), r.TargetID)
	if err != nil {
		return fmt.Errorf("insert result for target %d: %w", r.TargetID, err)
	}
	return nil
}

// Results returns the most recent results of a target, newest first.
func (s *Store) Results(ctx context.Context, targetID int64, limit int) ([]monitor.Result, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, target_id, status_code, duration_ms, error, checked_at
		 FROM results WHERE target_id = ? ORDER BY checked_at DESC, id DESC LIMIT ?`,
		targetID, limit)
	if err != nil {
		return nil, fmt.Errorf("select results: %w", err)
	}
	defer rows.Close()

	results := []monitor.Result{}
	for rows.Next() {
		var (
			r          monitor.Result
			durationMS int64
			checkedAt  int64
		)
		if err := rows.Scan(&r.ID, &r.TargetID, &r.StatusCode, &durationMS, &r.Err, &checkedAt); err != nil {
			return nil, fmt.Errorf("scan result: %w", err)
		}
		r.Duration = time.Duration(durationMS) * time.Millisecond
		r.CheckedAt = time.UnixMilli(checkedAt).UTC()
		results = append(results, r)
	}
	return results, rows.Err()
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanTarget(sc scanner) (monitor.Target, error) {
	var (
		t               monitor.Target
		intervalSeconds int64
		createdAt       int64
	)
	if err := sc.Scan(&t.ID, &t.URL, &intervalSeconds, &createdAt); err != nil {
		return monitor.Target{}, err
	}
	t.Interval = time.Duration(intervalSeconds) * time.Second
	t.CreatedAt = time.UnixMilli(createdAt).UTC()
	return t, nil
}

func isUniqueViolation(err error) bool {
	var se *sqlite.Error
	return errors.As(err, &se) && se.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
}
