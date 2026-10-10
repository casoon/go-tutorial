package scheduler

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/casoon/go-tutorial/internal/monitor"
)

// fakeStore hands out every target exactly once and records saved results.
// A fake is simpler and more honest than a mock: it has real behavior.
type fakeStore struct {
	mu      sync.Mutex
	pending []monitor.Target
	saved   []monitor.Result
}

func (f *fakeStore) ClaimDue(_ context.Context, _ time.Time, limit int) ([]monitor.Target, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := min(limit, len(f.pending))
	due := f.pending[:n]
	f.pending = f.pending[n:]
	return due, nil
}

func (f *fakeStore) SaveResult(_ context.Context, r monitor.Result) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = append(f.saved, r)
	return nil
}

func (f *fakeStore) savedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.saved)
}

// fakeChecker pretends every check takes 100ms and returns 200 OK.
type fakeChecker struct{}

func (fakeChecker) Check(ctx context.Context, t monitor.Target) (monitor.Result, error) {
	select {
	case <-time.After(100 * time.Millisecond):
		return monitor.Result{TargetID: t.ID, StatusCode: 200}, nil
	case <-ctx.Done():
		return monitor.Result{}, ctx.Err()
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRunChecksAllTargets(t *testing.T) {
	// synctest runs the test in a bubble with a fake clock: time only
	// advances when every goroutine in the bubble is blocked. The test is
	// fast and deterministic even though the code uses real timers.
	synctest.Test(t, func(t *testing.T) {
		store := &fakeStore{}
		for i := range 10 {
			store.pending = append(store.pending, monitor.Target{ID: int64(i + 1)})
		}

		ctx, cancel := context.WithCancel(t.Context())
		s := New(store, fakeChecker{}, discardLogger(), 3, time.Second)

		done := make(chan struct{})
		go func() {
			s.Run(ctx)
			close(done)
		}()

		// 10 checks of 100ms on 3 workers need 4 rounds = 400ms.
		time.Sleep(399 * time.Millisecond)
		synctest.Wait()
		if got := store.savedCount(); got != 9 {
			t.Fatalf("after 399ms saved = %d, want 9", got)
		}

		time.Sleep(time.Millisecond)
		synctest.Wait()
		if got := store.savedCount(); got != 10 {
			t.Fatalf("after 400ms saved = %d, want 10", got)
		}

		cancel()
		<-done
	})
}

func TestRunStopsOnCancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := &fakeStore{pending: []monitor.Target{{ID: 1}, {ID: 2}}}
		ctx, cancel := context.WithCancel(t.Context())
		s := New(store, fakeChecker{}, discardLogger(), 2, time.Second)

		done := make(chan struct{})
		go func() {
			s.Run(ctx)
			close(done)
		}()

		// Cancel while both checks are in flight: Run must return promptly
		// and must not save the interrupted checks.
		time.Sleep(50 * time.Millisecond)
		cancel()
		<-done

		if got := store.savedCount(); got != 0 {
			t.Fatalf("saved = %d, want 0", got)
		}
	})
}
