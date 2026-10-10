package main

import "testing"

func TestGreeting(t *testing.T) {
	tests := []struct {
		name string
		urls []string
		want string
	}{
		{"none", nil, "upwatch: no URLs to watch yet"},
		{"one", []string{"https://example.com"}, "upwatch: watching https://example.com"},
		{"two", []string{"https://a.example", "https://b.example"}, "upwatch: watching https://a.example, https://b.example"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := greeting(tt.urls); got != tt.want {
				t.Errorf("greeting(%q) = %q, want %q", tt.urls, got, tt.want)
			}
		})
	}
}
