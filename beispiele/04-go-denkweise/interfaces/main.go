// Command interfaces shows small interfaces defined by their consumers,
// implicit satisfaction, functions as interface implementations,
// middleware-style wrapping and dependency injection without a framework.
//
//	go run ./beispiele/04-go-denkweise/interfaces
package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
)

// ---- Provider: a concrete store. It knows nothing about its consumers.

type Target struct {
	ID  int
	URL string
}

// MemStore is returned as a concrete type. Callers that need less can
// assign it to their own, smaller interface.
type MemStore struct {
	mu      sync.Mutex
	targets []Target
	results map[int][]int
}

func NewMemStore(urls ...string) *MemStore {
	s := &MemStore{results: make(map[int][]int)}
	for i, u := range urls {
		s.targets = append(s.targets, Target{ID: i + 1, URL: u})
	}
	return s
}

func (s *MemStore) Targets() []Target {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Target(nil), s.targets...)
}

func (s *MemStore) SaveResult(targetID, status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results[targetID] = append(s.results[targetID], status)
}

func (s *MemStore) Results(targetID int) []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.results[targetID]...)
}

// ---- Consumer 1: a runner that checks every target once.
// It declares exactly what it uses.

type runnerStore interface {
	Targets() []Target
	SaveResult(targetID, status int)
}

// Checker returns the status code for a URL.
type Checker interface {
	Check(url string) int
}

// CheckFunc lets an ordinary function serve as a Checker, just like
// http.HandlerFunc does for http.Handler.
type CheckFunc func(url string) int

func (f CheckFunc) Check(url string) int { return f(url) }

// Runner accepts interfaces ...
type Runner struct {
	store   runnerStore
	checker Checker
}

// ... and NewRunner returns a struct.
func NewRunner(store runnerStore, checker Checker) *Runner {
	return &Runner{store: store, checker: checker}
}

func (r *Runner) RunOnce() {
	for _, t := range r.store.Targets() {
		r.store.SaveResult(t.ID, r.checker.Check(t.URL))
	}
}

// ---- Consumer 2: a report that only reads.

type reportStore interface {
	Targets() []Target
	Results(targetID int) []int
}

func WriteReport(w io.Writer, store reportStore) error {
	for _, t := range store.Targets() {
		if _, err := fmt.Fprintf(w, "%-26s %v\n", t.URL, store.Results(t.ID)); err != nil {
			return err
		}
	}
	return nil
}

// Compile-time proof that *MemStore satisfies both consumer interfaces.
// MemStore itself never mentions them.
var (
	_ runnerStore = (*MemStore)(nil)
	_ reportStore = (*MemStore)(nil)
)

// ---- Middleware: a function that wraps a Checker in another Checker.

// WithRetry repeats a check that failed with a 5xx status up to n times.
func WithRetry(next Checker, n int) Checker {
	return CheckFunc(func(url string) int {
		status := next.Check(url)
		for i := 0; i < n && status >= 500; i++ {
			status = next.Check(url)
		}
		return status
	})
}

// fakeCheck answers from a fixed table instead of the network.
func fakeCheck(statuses map[string][]int) CheckFunc {
	var mu sync.Mutex
	return func(url string) int {
		mu.Lock()
		defer mu.Unlock()
		queue := statuses[url]
		if len(queue) == 0 {
			return 0
		}
		statuses[url] = queue[1:]
		return queue[0]
	}
}

// ---- Composition root: all wiring happens here, explicitly.

func run(getenv func(string) string, out io.Writer) error {
	retries := 1
	if v := getenv("RETRIES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("RETRIES: %w", err)
		}
		retries = n
	}

	store := NewMemStore("https://example.com", "https://flaky.example")
	checker := WithRetry(fakeCheck(map[string][]int{
		"https://example.com":   {200},
		"https://flaky.example": {503, 200},
	}), retries)

	NewRunner(store, checker).RunOnce()
	return WriteReport(out, store)
}

func main() {
	if err := run(os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
