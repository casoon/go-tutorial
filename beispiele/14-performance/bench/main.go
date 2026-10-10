// Command bench holds small functions whose cost is compared in
// bench_test.go. Run the benchmarks with:
//
//	go test -bench=. -benchmem ./beispiele/14-performance/bench
package main

import (
	"fmt"
	"strconv"
	"strings"
)

// joinPlus builds a line with +=. Every step allocates a new string and
// copies everything written so far.
func joinPlus(codes []int) string {
	s := ""
	for i, c := range codes {
		if i > 0 {
			s += ","
		}
		s += strconv.Itoa(c)
	}
	return s
}

// joinBuilder writes into one growing buffer.
func joinBuilder(codes []int) string {
	var b strings.Builder
	for i, c := range codes {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Itoa(c))
	}
	return b.String()
}

type target struct {
	ID  int64
	URL string
}

// collectGrow starts with an empty slice and lets append grow it.
func collectGrow(n int) []target {
	var out []target
	for i := range n {
		out = append(out, target{ID: int64(i), URL: "https://example.com"})
	}
	return out
}

// collectPrealloc knows the final length and allocates once,
// like make([]targetJSON, 0, len(targets)) in httpapi.
func collectPrealloc(n int) []target {
	out := make([]target, 0, n)
	for i := range n {
		out = append(out, target{ID: int64(i), URL: "https://example.com"})
	}
	return out
}

func main() {
	codes := []int{200, 301, 404, 503}
	fmt.Println(joinPlus(codes), joinBuilder(codes))
	fmt.Println(len(collectGrow(100)), len(collectPrealloc(100)))
}
