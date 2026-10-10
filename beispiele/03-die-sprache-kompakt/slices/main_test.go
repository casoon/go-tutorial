package main

import (
	"slices"
	"testing"
)

func TestAppendOverwritesSharedArray(t *testing.T) {
	all := []int{200, 200, 503, 200}
	prefix := all[:2]
	prefix = append(prefix, 404)

	if all[2] != 404 {
		t.Fatalf("all[2] = %d, want 404: prefix and all share one array", all[2])
	}
	if &prefix[0] != &all[0] {
		t.Error("prefix should point into all")
	}
}

func TestFullSliceExpressionIsolates(t *testing.T) {
	all := []int{200, 200, 503, 200}
	prefix := all[:2:2]
	prefix = append(prefix, 404)

	if want := []int{200, 200, 503, 200}; !slices.Equal(all, want) {
		t.Errorf("all = %v, want %v", all, want)
	}
	if want := []int{200, 200, 404}; !slices.Equal(prefix, want) {
		t.Errorf("prefix = %v, want %v", prefix, want)
	}
}

func TestArrayAssignmentCopies(t *testing.T) {
	codes := [3]int{200, 200, 503}
	copied := codes
	copied[0] = 500
	if codes[0] != 200 {
		t.Errorf("codes[0] = %d, arrays are values and must not change", codes[0])
	}
}

func TestNilSliceIsUsable(t *testing.T) {
	var s []int
	if s != nil || len(s) != 0 {
		t.Fatal("zero value of a slice is nil with length 0")
	}
	s = append(s, 1)
	if len(s) != 1 {
		t.Errorf("len = %d, want 1", len(s))
	}
}
