package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestAdd(t *testing.T) {
	got := Add(2, 3)
	if got != 5 {
		t.Fatalf("got %d, want 5", got)
	}
}

// Table-driven: one loop, many cases, one subtest per case.
func TestClassify(t *testing.T) {
	tests := []struct {
		name string
		code int
		want string
	}{
		{"ok", 200, "up"},
		{"redirect", 301, "up"},
		{"not found", 404, "down"},
		{"server error", 503, "down"},
		{"too small", 99, "invalid"},
		{"too large", 600, "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.code); got != tt.want {
				t.Errorf("Classify(%d) = %q, want %q", tt.code, got, tt.want)
			}
		})
	}
}

func TestParseInterval(t *testing.T) {
	tests := []struct {
		in      string
		want    time.Duration
		wantErr string
	}{
		{in: "30s", want: 30 * time.Second},
		{in: "1h30m", want: 90 * time.Minute},
		{in: "5s", wantErr: "between"},
		{in: "48h", wantErr: "between"},
		{in: "soon", wantErr: "invalid duration"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseInterval(tt.in)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ParseInterval(%q) error = %v, want it to contain %q", tt.in, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseInterval(%q) unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ParseInterval(%q) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}

// httptest.NewServer: a real HTTP server on a loopback port.
func TestProbe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/down" {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	t.Cleanup(srv.Close)

	for path, want := range map[string]int{"/": 200, "/down": 503} {
		got, err := Probe(t.Context(), srv.Client(), srv.URL+path)
		if err != nil {
			t.Fatalf("Probe(%s): %v", path, err)
		}
		if got != want {
			t.Errorf("Probe(%s) = %d, want %d", path, got, want)
		}
	}
}

// pingFunc turns a function into a Pinger: the smallest possible fake.
type pingFunc func(context.Context) error

func (f pingFunc) Ping(ctx context.Context) error { return f(ctx) }

// serve calls h directly, without a network, and returns the recording.
func serve(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// httptest.NewRecorder: test a handler like a plain function.
func TestReadyz(t *testing.T) {
	up := pingFunc(func(context.Context) error { return nil })
	down := pingFunc(func(context.Context) error { return errors.New("database is locked") })

	if rec := serve(t, Readyz(up), "/readyz"); rec.Code != http.StatusOK {
		t.Errorf("up: status = %d, want 200", rec.Code)
	}
	rec := serve(t, Readyz(down), "/readyz")
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("down: status = %d, want 503", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "locked") {
		t.Errorf("down: body leaks the internal error: %s", rec.Body)
	}
}

// synctest: a fake clock. The test waits 2 minutes of "time" in no time.
func TestWaitUntilUp(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		check := func(context.Context) bool {
			calls++
			return calls == 3 // up on the third attempt
		}

		start := time.Now()
		attempts, err := WaitUntilUp(t.Context(), time.Minute, check)
		if err != nil {
			t.Fatal(err)
		}
		if attempts != 3 {
			t.Errorf("attempts = %d, want 3", attempts)
		}
		if elapsed := time.Since(start); elapsed != 2*time.Minute {
			t.Errorf("elapsed = %s, want exactly 2m0s", elapsed)
		}
	})
}

func TestWaitUntilUpGivesUp(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		defer cancel()

		never := func(context.Context) bool { return false }
		attempts, err := WaitUntilUp(ctx, time.Minute, never)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want DeadlineExceeded", err)
		}
		// Ticks at 1..4m run checks 2..5. At 5m the timer and the deadline
		// fire together; select picks either, so 5 or 6 attempts are valid.
		if attempts < 5 || attempts > 6 {
			t.Errorf("attempts = %d, want 5 or 6", attempts)
		}
	})
}

func BenchmarkParseInterval(b *testing.B) {
	for b.Loop() {
		_, _ = ParseInterval("1h30m")
	}
}

// Property: every accepted interval survives a round trip through
// Duration.String.
func FuzzParseInterval(f *testing.F) {
	f.Add("30s")
	f.Add("1h30m")
	f.Add("-5m")
	f.Fuzz(func(t *testing.T, s string) {
		d, err := ParseInterval(s)
		if err != nil {
			return
		}
		again, err := ParseInterval(d.String())
		if err != nil {
			t.Fatalf("ParseInterval(%q) = %s, but %q is rejected: %v", s, d, d.String(), err)
		}
		if again != d {
			t.Fatalf("round trip %q: %s != %s", s, d, again)
		}
	})
}
