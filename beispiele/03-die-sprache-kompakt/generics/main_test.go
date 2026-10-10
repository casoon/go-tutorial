package main

import (
	"slices"
	"testing"
	"time"
)

func TestAverage(t *testing.T) {
	if got := Average([]int{200, 200, 503}); got != 301 {
		t.Errorf("int average = %d, want 301 (integer division)", got)
	}
	if got := Average([]float64{1, 2}); got != 1.5 {
		t.Errorf("float average = %v, want 1.5", got)
	}
	if got := Average([]time.Duration{time.Second, 3 * time.Second}); got != 2*time.Second {
		t.Errorf("duration average = %v, want 2s", got)
	}
	if got := Average[int](nil); got != 0 {
		t.Errorf("empty average = %d, want 0", got)
	}
}

func TestMap(t *testing.T) {
	results := []Result{{StatusCode: 200}, {StatusCode: 503}}
	got := Map(results, Result.Up) // a method expression is a func(Result) bool
	if want := []bool{true, false}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestGroupBy(t *testing.T) {
	results := []Result{{TargetID: 1}, {TargetID: 2}, {TargetID: 1}}
	groups := GroupBy(results, func(r Result) int64 { return r.TargetID })
	if len(groups) != 2 || len(groups[1]) != 2 || len(groups[2]) != 1 {
		t.Errorf("unexpected groups: %v", groups)
	}
}
