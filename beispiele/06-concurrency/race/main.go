// Command race counts check results from many goroutines, correctly
// synchronized with a mutex. racy.go is the broken variant; run it with
//
//	go run -race racy.go
package main

import (
	"fmt"
	"sync"
)

// Stats counts results per status code. It is safe for concurrent use.
type Stats struct {
	mu     sync.Mutex
	counts map[int]int
}

// NewStats returns empty Stats.
func NewStats() *Stats {
	return &Stats{counts: make(map[int]int)}
}

// Record adds one result.
func (s *Stats) Record(status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counts[status]++
}

// Count returns how many results had the given status.
func (s *Stats) Count(status int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[status]
}

func main() {
	stats := NewStats()
	var wg sync.WaitGroup
	for i := range 1000 {
		wg.Go(func() {
			if i%10 == 0 {
				stats.Record(503)
				return
			}
			stats.Record(200)
		})
	}
	wg.Wait()
	fmt.Println("200:", stats.Count(200), "503:", stats.Count(503))
}
