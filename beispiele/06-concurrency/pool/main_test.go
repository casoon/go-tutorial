package main

import (
	"context"
	"fmt"
	"testing"
	"testing/synctest"
	"time"
)

func slowCheck(ctx context.Context, url string) Result {
	select {
	case <-time.After(100 * time.Millisecond):
		return Result{URL: url, Status: 200}
	case <-ctx.Done():
		return Result{URL: url}
	}
}

func urls(n int) []string {
	var out []string
	for i := range n {
		out = append(out, fmt.Sprintf("https://site%d.example", i))
	}
	return out
}

func TestRunUsesAllWorkers(t *testing.T) {
	// Inside the bubble time is virtual: the test takes microseconds and
	// the measured duration is exact.
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		got := Run(t.Context(), urls(10), 3, slowCheck)
		if len(got) != 10 {
			t.Fatalf("got %d results, want 10", len(got))
		}
		// 10 checks on 3 workers: 4 rounds of 100ms.
		if d := time.Since(start); d != 400*time.Millisecond {
			t.Errorf("took %s, want 400ms", d)
		}
	})
}

func TestRunStopsOnCancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
		defer cancel()

		got := Run(ctx, urls(10), 2, slowCheck)
		// Round 1 completes, round 2 is cut short, the rest never starts.
		if len(got) != 4 {
			t.Errorf("got %d results, want 4", len(got))
		}
	})
}
