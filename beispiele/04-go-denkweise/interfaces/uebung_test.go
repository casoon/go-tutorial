//go:build uebung

// This file belongs to an exercise in chapter 04 and is excluded from the
// normal build. It compiles only after you have added WithFallback to
// main.go. Run it with:
//
//	go test -tags uebung ./beispiele/04-go-denkweise/interfaces
package main

import "testing"

func TestWithFallback(t *testing.T) {
	tests := []struct {
		name          string
		primary       int // status returned by the primary checker
		want          int
		fallbackCalls int
	}{
		{"primary ok", 204, 204, 0},
		{"primary redirect", 301, 301, 0},
		{"primary server error", 503, 200, 1},
		{"primary unreachable", 0, 200, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			fallback := CheckFunc(func(string) int {
				calls++
				return 200
			})
			primary := CheckFunc(func(string) int { return tt.primary })

			got := WithFallback(primary, fallback).Check("https://example.com")
			if got != tt.want {
				t.Errorf("status = %d, want %d", got, tt.want)
			}
			if calls != tt.fallbackCalls {
				t.Errorf("fallback called %d times, want %d", calls, tt.fallbackCalls)
			}
		})
	}
}
