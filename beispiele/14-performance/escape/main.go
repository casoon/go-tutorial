// Command escape is a small playground for the compiler's escape analysis.
//
// Show the compiler's decisions with:
//
//	go build -gcflags=-m -o /dev/null ./beispiele/14-performance/escape
package main

import "fmt"

type result struct {
	statusCode int
	durationMS int64
}

// up takes a copy and returns a bool: nothing leaves the function.
func up(r result) bool {
	return r.statusCode >= 200 && r.statusCode < 400
}

// newResult returns a pointer to a local variable. The variable must
// outlive the call, so the compiler moves it to the heap.
func newResult(code int) *result {
	r := result{statusCode: code}
	return &r
}

// sum uses a slice whose size the compiler knows and which does not
// leave the function: it can live on the stack.
func sum() int64 {
	durations := make([]int64, 8)
	for i := range durations {
		durations[i] = int64(i * 10)
	}
	var total int64
	for _, d := range durations {
		total += d
	}
	return total
}

// collect returns its slice, and its size is only known at run time.
func collect(n int) []int64 {
	durations := make([]int64, 0, n)
	for i := range n {
		durations = append(durations, int64(i))
	}
	return durations
}

func main() {
	r := result{statusCode: 200, durationMS: 42}
	p := newResult(503)
	fmt.Println(up(r), up(*p), sum(), len(collect(4)))
}
