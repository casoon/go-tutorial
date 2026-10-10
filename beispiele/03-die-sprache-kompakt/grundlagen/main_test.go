package main

import "testing"

func TestResultStatus(t *testing.T) {
	tests := []struct {
		code int
		want Status
	}{
		{0, StatusUnknown},
		{200, StatusUp},
		{301, StatusUp},
		{404, StatusDown},
		{503, StatusDown},
	}
	for _, tt := range tests {
		if got := (Result{StatusCode: tt.code}).Status(); got != tt.want {
			t.Errorf("status code %d: got %s, want %s", tt.code, got, tt.want)
		}
	}
}

func TestRecordCheckNeedsPointer(t *testing.T) {
	target := Target{URL: "https://example.com"}
	target.RecordCheck()
	if target.checks != 1 {
		t.Fatalf("checks = %d, want 1", target.checks)
	}

	// A copy is a separate value: changing it leaves the original alone.
	cp := target
	cp.RecordCheck()
	if target.checks != 1 || cp.checks != 2 {
		t.Errorf("original %d, copy %d; want 1 and 2", target.checks, cp.checks)
	}
}

func TestSplitURL(t *testing.T) {
	scheme, rest, ok := splitURL("https://example.com/health")
	if !ok || scheme != "https" || rest != "example.com/health" {
		t.Errorf("got %q %q %v", scheme, rest, ok)
	}
	if _, _, ok := splitURL("example.com"); ok {
		t.Error("want ok=false without scheme")
	}
}

func TestGreeting(t *testing.T) {
	if got := (User{ID: 1, Name: "Ada"}).Greeting(); got != "Hello, Ada" {
		t.Errorf("got %q", got)
	}
}
