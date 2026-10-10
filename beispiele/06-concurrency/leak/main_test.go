package main

import (
	"testing"
	"testing/synctest"
	"time"
)

// synctest.Test requires every goroutine started in the bubble to exit.
// A goroutine that stays blocked forever makes the test fail with a
// deadlock instead of hanging, which turns it into a leak detector.
// Replace First with FirstLeaky to see it.
func TestFirstDoesNotLeak(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fetch := func(url string) string {
			if url == "https://fast.example" {
				return "fast"
			}
			time.Sleep(time.Second)
			return "slow"
		}
		got := First([]string{"https://slow.example", "https://fast.example"}, fetch)
		if got != "fast" {
			t.Errorf("got %q, want fast", got)
		}

		// Let the slow fetch finish, then wait until every other goroutine
		// in the bubble has exited or is blocked for good.
		time.Sleep(time.Second)
		synctest.Wait()
	})
}
