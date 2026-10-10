package sqlitestore

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/casoon/go-tutorial/internal/monitor"
)

// newStore opens a fresh database in a per-test temp directory.
// This is an integration test: it talks to a real SQLite database.
func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func mustCreate(t *testing.T, s *Store, url string, interval time.Duration) monitor.Target {
	t.Helper()
	target, err := monitor.NewTarget(url, interval)
	if err != nil {
		t.Fatalf("NewTarget: %v", err)
	}
	created, err := s.CreateTarget(t.Context(), target)
	if err != nil {
		t.Fatalf("CreateTarget: %v", err)
	}
	return created
}

func TestTargetLifecycle(t *testing.T) {
	s := newStore(t)
	ctx := t.Context()

	created := mustCreate(t, s, "https://example.com", time.Minute)
	if created.ID == 0 {
		t.Fatal("expected ID to be set")
	}

	got, err := s.Target(ctx, created.ID)
	if err != nil {
		t.Fatalf("Target: %v", err)
	}
	if got != created {
		t.Errorf("Target = %+v, want %+v", got, created)
	}

	if _, err := s.CreateTarget(ctx, created); !errors.Is(err, monitor.ErrDuplicate) {
		t.Errorf("duplicate CreateTarget err = %v, want ErrDuplicate", err)
	}

	all, err := s.Targets(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("Targets = %v, %v; want 1 target", all, err)
	}

	if err := s.DeleteTarget(ctx, created.ID); err != nil {
		t.Fatalf("DeleteTarget: %v", err)
	}
	if _, err := s.Target(ctx, created.ID); !errors.Is(err, monitor.ErrNotFound) {
		t.Errorf("Target after delete err = %v, want ErrNotFound", err)
	}
	if err := s.DeleteTarget(ctx, created.ID); !errors.Is(err, monitor.ErrNotFound) {
		t.Errorf("second DeleteTarget err = %v, want ErrNotFound", err)
	}
}

func TestClaimDue(t *testing.T) {
	s := newStore(t)
	ctx := t.Context()
	target := mustCreate(t, s, "https://example.com", time.Minute)

	now := time.Now().Add(time.Second)
	due, err := s.ClaimDue(ctx, now, 10)
	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	if len(due) != 1 || due[0].ID != target.ID {
		t.Fatalf("ClaimDue = %+v, want target %d", due, target.ID)
	}

	// Claimed targets are not due again until one interval has passed.
	due, err = s.ClaimDue(ctx, now.Add(30*time.Second), 10)
	if err != nil || len(due) != 0 {
		t.Fatalf("second ClaimDue = %+v, %v; want none", due, err)
	}
	due, err = s.ClaimDue(ctx, now.Add(time.Minute), 10)
	if err != nil || len(due) != 1 {
		t.Fatalf("ClaimDue after interval = %+v, %v; want 1", due, err)
	}
}

func TestResults(t *testing.T) {
	s := newStore(t)
	ctx := t.Context()
	target := mustCreate(t, s, "https://example.com", time.Minute)

	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for i := range 3 {
		err := s.SaveResult(ctx, monitor.Result{
			TargetID:   target.ID,
			StatusCode: 200,
			Duration:   time.Duration(i+1) * 10 * time.Millisecond,
			CheckedAt:  base.Add(time.Duration(i) * time.Minute),
		})
		if err != nil {
			t.Fatalf("SaveResult: %v", err)
		}
	}

	results, err := s.Results(ctx, target.ID, 2)
	if err != nil {
		t.Fatalf("Results: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if want := base.Add(2 * time.Minute); !results[0].CheckedAt.Equal(want) {
		t.Errorf("newest CheckedAt = %s, want %s", results[0].CheckedAt, want)
	}
	if results[0].Duration != 30*time.Millisecond {
		t.Errorf("Duration = %s, want 30ms", results[0].Duration)
	}

	// Deleting the target removes its results (ON DELETE CASCADE) and
	// late results for it are dropped.
	if err := s.DeleteTarget(ctx, target.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveResult(ctx, monitor.Result{TargetID: target.ID, CheckedAt: base}); err != nil {
		t.Fatalf("SaveResult for deleted target: %v", err)
	}
	results, err = s.Results(ctx, target.ID, 10)
	if err != nil || len(results) != 0 {
		t.Fatalf("Results after delete = %v, %v; want none", results, err)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	for range 2 {
		s, err := Open(t.Context(), path)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		s.Close()
	}
}
