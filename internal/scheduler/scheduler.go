// Package scheduler periodically checks all due targets with a fixed pool
// of workers and stores the results.
//
// Data flow (fan-out / fan-in):
//
//	ticker ─► ClaimDue ─► jobs ─┬─► worker ─┐
//	                            ├─► worker ─┼─► results ─► writer ─► SaveResult
//	                            └─► worker ─┘
package scheduler

import (
	"context"
	"expvar"
	"log/slog"
	"sync"
	"time"

	"github.com/casoon/go-tutorial/internal/monitor"
)

// Store is what the scheduler needs from persistence. It is defined here,
// at the consumer, and only lists the two methods actually used.
type Store interface {
	ClaimDue(ctx context.Context, now time.Time, limit int) ([]monitor.Target, error)
	SaveResult(ctx context.Context, r monitor.Result) error
}

// Checker performs a single check.
type Checker interface {
	Check(ctx context.Context, t monitor.Target) (monitor.Result, error)
}

// Metrics are published via expvar at /debug/vars.
var (
	checksTotal  = expvar.NewInt("upwatch_checks_total")
	checksDown   = expvar.NewInt("upwatch_checks_down_total")
	saveFailures = expvar.NewInt("upwatch_save_failures_total")
)

// Scheduler wires store and checker together. Create it with New.
type Scheduler struct {
	store   Store
	checker Checker
	logger  *slog.Logger
	workers int
	tick    time.Duration
}

// New returns a Scheduler that looks for due targets every tick and runs
// at most workers checks concurrently.
func New(store Store, checker Checker, logger *slog.Logger, workers int, tick time.Duration) *Scheduler {
	return &Scheduler{
		store:   store,
		checker: checker,
		logger:  logger,
		workers: workers,
		tick:    tick,
	}
}

// Run blocks until ctx is canceled. It then stops dispatching, waits for
// running workers and stores every result that was already produced.
func (s *Scheduler) Run(ctx context.Context) {
	jobs := make(chan monitor.Target)
	results := make(chan monitor.Result)

	var workers sync.WaitGroup
	for range s.workers {
		workers.Go(func() { s.work(ctx, jobs, results) })
	}

	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		s.write(ctx, results)
	}()

	s.dispatch(ctx, jobs)

	// Shutdown order matters: the dispatcher has returned, so nobody sends
	// on jobs any more. Closing it ends the workers' range loops. Once all
	// workers are gone nobody sends on results, so it can be closed too.
	close(jobs)
	workers.Wait()
	close(results)
	<-writerDone
}

// dispatch claims due targets on every tick and hands them to the workers.
func (s *Scheduler) dispatch(ctx context.Context, jobs chan<- monitor.Target) {
	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()

	for {
		due, err := s.store.ClaimDue(ctx, time.Now(), s.workers*4)
		if err != nil && ctx.Err() == nil {
			s.logger.Error("claim due targets", "err", err)
		}
		for _, t := range due {
			select {
			case jobs <- t:
			case <-ctx.Done():
				return
			}
		}

		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}

// work runs checks until jobs is closed.
func (s *Scheduler) work(ctx context.Context, jobs <-chan monitor.Target, results chan<- monitor.Result) {
	for t := range jobs {
		res, err := s.checker.Check(ctx, t)
		if err != nil {
			// Only happens when ctx was canceled: the check was cut short
			// by shutdown and its result would be misleading.
			continue
		}
		results <- res
	}
}

// write persists results one at a time. A single writer keeps SQLite free
// of write contention and is plenty fast for this workload.
func (s *Scheduler) write(ctx context.Context, results <-chan monitor.Result) {
	// Results that arrive during shutdown should still be saved, so the
	// writer uses a context that is not canceled together with ctx.
	saveCtx := context.WithoutCancel(ctx)

	for r := range results {
		checksTotal.Add(1)
		if !r.Up() {
			checksDown.Add(1)
			s.logger.Warn("target down",
				"target_id", r.TargetID, "status", r.StatusCode, "err", r.Err,
				"duration", r.Duration)
		}

		ctx, cancel := context.WithTimeout(saveCtx, 5*time.Second)
		if err := s.store.SaveResult(ctx, r); err != nil {
			saveFailures.Add(1)
			s.logger.Error("save result", "target_id", r.TargetID, "err", err)
		}
		cancel()
	}
}
