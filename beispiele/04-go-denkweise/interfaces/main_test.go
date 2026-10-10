package main

import (
	"slices"
	"strings"
	"testing"
)

func TestRunnerWithCheckFunc(t *testing.T) {
	store := NewMemStore("https://a.example", "https://b.example")
	always := CheckFunc(func(string) int { return 204 })

	NewRunner(store, always).RunOnce()

	for _, id := range []int{1, 2} {
		if got := store.Results(id); !slices.Equal(got, []int{204}) {
			t.Errorf("target %d: results %v, want [204]", id, got)
		}
	}
}

func TestWithRetry(t *testing.T) {
	calls := 0
	flaky := CheckFunc(func(string) int {
		calls++
		if calls < 3 {
			return 503
		}
		return 200
	})

	if got := WithRetry(flaky, 1).Check("x"); got != 503 {
		t.Errorf("one retry: got %d, want 503", got)
	}
	calls = 0
	if got := WithRetry(flaky, 2).Check("x"); got != 200 {
		t.Errorf("two retries: got %d, want 200", got)
	}
}

func TestRun(t *testing.T) {
	var out strings.Builder
	env := func(key string) string {
		if key == "RETRIES" {
			return "0"
		}
		return ""
	}
	if err := run(env, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "[503]") {
		t.Errorf("without retries the flaky target should report 503:\n%s", out.String())
	}

	if err := run(func(string) string { return "many" }, &out); err == nil {
		t.Error("want error for invalid RETRIES")
	}
}
