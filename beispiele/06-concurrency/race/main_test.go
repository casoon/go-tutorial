package main

import (
	"sync"
	"testing"
)

// Run with go test -race: the detector watches every memory access while
// the test runs. Without the mutex in Stats this test fails.
func TestStatsConcurrent(t *testing.T) {
	stats := NewStats()
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() { stats.Record(200) })
	}
	wg.Wait()
	if got := stats.Count(200); got != 100 {
		t.Errorf("Count(200) = %d, want 100", got)
	}
}
