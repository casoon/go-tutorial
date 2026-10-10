// Command generics shows type parameters and constraints on monitor
// results, and where the standard library already has what you need.
//
//	go run ./beispiele/03-die-sprache-kompakt/generics
package main

import (
	"cmp"
	"fmt"
	"slices"
	"time"
)

type Result struct {
	TargetID   int64
	StatusCode int
	Duration   time.Duration
}

func (r Result) Up() bool { return r.StatusCode >= 200 && r.StatusCode < 400 }

// Number is a constraint: the set of types Average accepts. The tilde
// includes named types whose underlying type is listed, such as
// time.Duration (underlying type int64).
type Number interface {
	~int | ~int64 | ~float64
}

// Average works for every Number. The zero value of T is returned for an
// empty slice, so the caller does not have to handle an error.
func Average[T Number](values []T) T {
	if len(values) == 0 {
		return 0
	}
	var sum T
	for _, v := range values {
		sum += v
	}
	return sum / T(len(values))
}

// Map converts every element. Two type parameters: input and output.
func Map[In, Out any](in []In, f func(In) Out) []Out {
	out := make([]Out, 0, len(in))
	for _, v := range in {
		out = append(out, f(v))
	}
	return out
}

// GroupBy collects values by a key. K must be comparable to serve as a
// map key.
func GroupBy[T any, K comparable](values []T, key func(T) K) map[K][]T {
	groups := make(map[K][]T)
	for _, v := range values {
		k := key(v)
		groups[k] = append(groups[k], v)
	}
	return groups
}

func main() {
	results := []Result{
		{TargetID: 1, StatusCode: 200, Duration: 120 * time.Millisecond},
		{TargetID: 1, StatusCode: 200, Duration: 180 * time.Millisecond},
		{TargetID: 2, StatusCode: 503, Duration: 40 * time.Millisecond},
	}

	durations := Map(results, func(r Result) time.Duration { return r.Duration })
	fmt.Println("average duration:", Average(durations))
	fmt.Println("average status:  ", Average([]int{200, 200, 503}))

	byTarget := GroupBy(results, func(r Result) int64 { return r.TargetID })
	fmt.Println("results for target 1:", len(byTarget[1]))

	// Before writing a generic helper, check slices, maps and cmp.
	slowest := slices.MaxFunc(results, func(a, b Result) int {
		return cmp.Compare(a.Duration, b.Duration)
	})
	fmt.Println("slowest:", slowest.Duration)
	fmt.Println("all up:", !slices.ContainsFunc(results, func(r Result) bool { return !r.Up() }))
}
