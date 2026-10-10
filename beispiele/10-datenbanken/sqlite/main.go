// Command sqlite shows database/sql with the pure-Go SQLite driver:
// pool setup, queries with context, ErrNoRows, constraint errors,
// transactions and why placeholders matter.
//
//	go run ./beispiele/10-datenbanken/sqlite
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	"modernc.org/sqlite" // registers the driver "sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrDuplicate = errors.New("already exists")
)

type Target struct {
	ID        int64
	URL       string
	CreatedAt time.Time
}

const schema = `
CREATE TABLE targets (
    id         INTEGER PRIMARY KEY,
    url        TEXT    NOT NULL UNIQUE,
    created_at INTEGER NOT NULL -- unix milliseconds, UTC
);
CREATE TABLE results (
    id          INTEGER PRIMARY KEY,
    target_id   INTEGER NOT NULL REFERENCES targets (id),
    status_code INTEGER NOT NULL CHECK (status_code BETWEEN 100 AND 599)
);`

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	// sql.Open only validates its arguments. No connection exists yet.
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return err
	}
	defer db.Close()

	// Every new connection to ":memory:" gets its own empty database.
	// With a single connection the pool always hands out the same one.
	db.SetMaxOpenConns(1)

	// The first real connection is made here.
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("schema: %w", err)
	}

	for _, u := range []string{"https://example.com", "https://go.dev", "https://example.org"} {
		t, err := createTarget(ctx, db, u)
		if err != nil {
			return err
		}
		fmt.Printf("created  %d %s\n", t.ID, t.URL)
	}

	_, err = createTarget(ctx, db, "https://go.dev")
	fmt.Printf("again    errors.Is(err, ErrDuplicate) = %v (%v)\n", errors.Is(err, ErrDuplicate), err)

	t, err := targetByID(ctx, db, 2)
	if err != nil {
		return err
	}
	fmt.Printf("found    %d %s\n", t.ID, t.URL)

	_, err = targetByID(ctx, db, 99)
	fmt.Printf("missing  errors.Is(err, ErrNotFound) = %v (%v)\n", errors.Is(err, ErrNotFound), err)

	all, err := targets(ctx, db)
	if err != nil {
		return err
	}
	fmt.Printf("all      %d targets\n", len(all))

	// A transaction is all or nothing: the result violates the CHECK
	// constraint, so the target inserted before it is rolled back too.
	err = addWithResult(ctx, db, "https://example.net", 0)
	fmt.Printf("tx fail  %v\n", err)
	n, err := count(ctx, db)
	if err != nil {
		return err
	}
	fmt.Printf("all      %d targets after rollback\n", n)

	// SQL injection: the same input, once glued into the SQL string and
	// once passed as a parameter.
	evil := "x' OR '1'='1"
	unsafeN, err := countByURLUnsafe(ctx, db, evil)
	if err != nil {
		return err
	}
	safeN, err := countByURL(ctx, db, evil)
	if err != nil {
		return err
	}
	fmt.Printf("inject   Sprintf: %d rows, placeholder: %d rows\n", unsafeN, safeN)
	return nil
}

func createTarget(ctx context.Context, db *sql.DB, url string) (Target, error) {
	now := time.Now().UTC()
	res, err := db.ExecContext(ctx,
		`INSERT INTO targets (url, created_at) VALUES (?, ?)`, url, now.UnixMilli())
	if err != nil {
		if isUniqueViolation(err) {
			return Target{}, fmt.Errorf("target %s: %w", url, ErrDuplicate)
		}
		return Target{}, fmt.Errorf("insert target: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Target{}, fmt.Errorf("insert target: %w", err)
	}
	return Target{ID: id, URL: url, CreatedAt: now.Truncate(time.Millisecond)}, nil
}

func targetByID(ctx context.Context, db *sql.DB, id int64) (Target, error) {
	var (
		t         Target
		createdAt int64
	)
	err := db.QueryRowContext(ctx,
		`SELECT id, url, created_at FROM targets WHERE id = ?`, id).
		Scan(&t.ID, &t.URL, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		// Translate the driver-level "no rows" into a domain error.
		return Target{}, fmt.Errorf("target %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return Target{}, fmt.Errorf("select target %d: %w", id, err)
	}
	t.CreatedAt = time.UnixMilli(createdAt).UTC()
	return t, nil
}

func targets(ctx context.Context, db *sql.DB) ([]Target, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, url, created_at FROM targets ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("select targets: %w", err)
	}
	defer rows.Close() // returns the connection to the pool

	var out []Target
	for rows.Next() {
		var (
			t         Target
			createdAt int64
		)
		if err := rows.Scan(&t.ID, &t.URL, &createdAt); err != nil {
			return nil, fmt.Errorf("scan target: %w", err)
		}
		t.CreatedAt = time.UnixMilli(createdAt).UTC()
		out = append(out, t)
	}
	// Next returns false on the end of the result set and on errors.
	// Only Err tells them apart.
	return out, rows.Err()
}

// addWithResult inserts a target and its first result atomically.
func addWithResult(ctx context.Context, db *sql.DB, url string, status int) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() // no-op after a successful Commit

	res, err := tx.ExecContext(ctx,
		`INSERT INTO targets (url, created_at) VALUES (?, ?)`, url, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("insert target: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO results (target_id, status_code) VALUES (?, ?)`, id, status); err != nil {
		return fmt.Errorf("insert result: %w", err)
	}
	return tx.Commit()
}

func count(ctx context.Context, db *sql.DB) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM targets`).Scan(&n)
	return n, err
}

// countByURLUnsafe is the negative example: never build SQL from input.
func countByURLUnsafe(ctx context.Context, db *sql.DB, url string) (int, error) {
	q := fmt.Sprintf(`SELECT COUNT(*) FROM targets WHERE url = '%s'`, url)
	var n int
	err := db.QueryRowContext(ctx, q).Scan(&n)
	return n, err
}

func countByURL(ctx context.Context, db *sql.DB, url string) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM targets WHERE url = ?`, url).Scan(&n)
	return n, err
}

func isUniqueViolation(err error) bool {
	var se *sqlite.Error
	return errors.As(err, &se) && se.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
}
