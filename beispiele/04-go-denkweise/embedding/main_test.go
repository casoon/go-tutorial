package main

import "testing"

// In C# or Java, an overridden Name would be called from the base class
// method (virtual dispatch). In Go, Describe has an HTTPCheck receiver
// and calls HTTPCheck.Name. The outer type is invisible to it.
func TestEmbeddingIsNotInheritance(t *testing.T) {
	tc := TLSCheck{HTTPCheck: HTTPCheck{URL: "https://example.com"}}
	if got := tc.Name(); got != "tls" {
		t.Errorf("Name() = %q, want tls", got)
	}
	if got, want := tc.Describe(), "http check of https://example.com"; got != want {
		t.Errorf("Describe() = %q, want %q", got, want)
	}
}

func TestEmbeddedInterfaceForwards(t *testing.T) {
	inner := &memStore{}
	store := &countingStore{Store: inner}
	if err := store.Save("https://example.com", 200); err != nil {
		t.Fatal(err)
	}
	if store.saves != 1 || store.Count() != 1 || inner.Count() != 1 {
		t.Errorf("saves=%d count=%d inner=%d, want 1 each", store.saves, store.Count(), inner.Count())
	}
}

func TestStatsZeroValue(t *testing.T) {
	var s Stats
	if s.Ratio() != 0 {
		t.Fatalf("empty ratio = %v, want 0", s.Ratio())
	}
	s.Record(200)
	s.Record(200)
	s.Record(500)
	s.Record(204)
	if got := s.Ratio(); got != 0.75 {
		t.Errorf("ratio = %v, want 0.75", got)
	}
}
