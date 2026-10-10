package main

import (
	"slices"
	"testing"
)

func statusCodes(n int) []int {
	codes := make([]int, n)
	for i := range codes {
		codes[i] = 200 + i%5*100
	}
	return codes
}

// A benchmark is only worth something if both variants compute the same.
func TestVariantsAgree(t *testing.T) {
	codes := statusCodes(50)
	if a, b := joinPlus(codes), joinBuilder(codes); a != b {
		t.Fatalf("joinPlus = %q, joinBuilder = %q", a, b)
	}
	if !slices.Equal(collectGrow(100), collectPrealloc(100)) {
		t.Fatal("collect variants differ")
	}
}

func BenchmarkJoinPlus(b *testing.B) {
	codes := statusCodes(100)
	for b.Loop() {
		joinPlus(codes)
	}
}

func BenchmarkJoinBuilder(b *testing.B) {
	codes := statusCodes(100)
	for b.Loop() {
		joinBuilder(codes)
	}
}

// sink makes the result escape, as it does in a real handler that encodes
// the slice to JSON. Without it the compiler may inline collectPrealloc
// and keep the fixed-size slice on the stack, which measures something else.
var sink []target

func BenchmarkCollectGrow(b *testing.B) {
	for b.Loop() {
		sink = collectGrow(1000)
	}
}

func BenchmarkCollectPrealloc(b *testing.B) {
	for b.Loop() {
		sink = collectPrealloc(1000)
	}
}
