// Command pool checks a list of URLs with a fixed number of workers:
// fan-out to the workers, fan-in of their results. The structure is the
// same as in upwatch's scheduler, without database and real HTTP.
package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"
)

// Result is the outcome of one simulated check.
type Result struct {
	URL    string
	Status int
	Took   time.Duration
}

// CheckFunc performs a single check.
type CheckFunc func(ctx context.Context, url string) Result

// Run checks every URL with the given number of workers and returns the
// results in completion order. If ctx is canceled, Run stops handing out
// new URLs, lets running checks finish and returns what it has.
func Run(ctx context.Context, urls []string, workers int, check CheckFunc) []Result {
	jobs := make(chan string)
	results := make(chan Result)

	// Fan-out: several workers receive from the same channel.
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for u := range jobs {
				results <- check(ctx, u)
			}
		})
	}

	// Fan-in: one goroutine collects from the shared results channel.
	var out []Result
	collected := make(chan struct{})
	go func() {
		defer close(collected)
		for r := range results {
			out = append(out, r)
		}
	}()

	dispatch(ctx, urls, jobs)

	close(jobs)    // 1. no more jobs: workers leave their range loops
	wg.Wait()      // 2. all workers gone: nobody sends on results
	close(results) // 3. collector leaves its range loop
	<-collected    // 4. out is complete and no longer written to
	return out
}

func dispatch(ctx context.Context, urls []string, jobs chan<- string) {
	for _, u := range urls {
		select {
		case jobs <- u:
		case <-ctx.Done():
			return
		}
	}
}

// fakeCheck pretends to request url and takes 50–150ms.
func fakeCheck(ctx context.Context, url string) Result {
	d := time.Duration(50+rand.IntN(100)) * time.Millisecond
	select {
	case <-time.After(d):
		return Result{URL: url, Status: 200, Took: d}
	case <-ctx.Done():
		return Result{URL: url, Took: d}
	}
}

func main() {
	var urls []string
	for i := range 12 {
		urls = append(urls, fmt.Sprintf("https://site%02d.example", i))
	}

	start := time.Now()
	results := Run(context.Background(), urls, 4, fakeCheck)
	for _, r := range results {
		fmt.Printf("%s %d %s\n", r.URL, r.Status, r.Took)
	}
	fmt.Printf("%d checks with 4 workers in %s\n", len(results), time.Since(start).Round(10*time.Millisecond))
}
