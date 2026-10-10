package monitor

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "https", in: "https://example.com", want: "https://example.com"},
		{name: "trims space", in: "  http://example.com/a  ", want: "http://example.com/a"},
		{name: "drops fragment", in: "https://example.com/#top", want: "https://example.com/"},
		{name: "empty", in: "", wantErr: true},
		{name: "no scheme", in: "example.com", wantErr: true},
		{name: "ftp", in: "ftp://example.com", wantErr: true},
		{name: "no host", in: "https://", wantErr: true},
		{name: "credentials", in: "https://user:pw@example.com", wantErr: true},
		{name: "inner whitespace", in: "https://example.com/? #top", wantErr: true},
		{name: "too long", in: "https://example.com/" + strings.Repeat("a", MaxURLLength), wantErr: true},
		{name: "too long after encoding", in: "https://example.com/" + strings.Repeat("\xa1", 700), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeURL(tt.in)
			if tt.wantErr {
				var ve *ValidationError
				if !errors.As(err, &ve) {
					t.Fatalf("NormalizeURL(%q) error = %v, want *ValidationError", tt.in, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeURL(%q) unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("NormalizeURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNewTargetInterval(t *testing.T) {
	tests := []struct {
		name     string
		interval time.Duration
		want     time.Duration
		wantErr  bool
	}{
		{name: "default", interval: 0, want: DefaultInterval},
		{name: "minimum", interval: MinInterval, want: MinInterval},
		{name: "too short", interval: time.Second, wantErr: true},
		{name: "too long", interval: 48 * time.Hour, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewTarget("https://example.com", tt.interval)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewTarget error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got.Interval != tt.want {
				t.Errorf("Interval = %s, want %s", got.Interval, tt.want)
			}
		})
	}
}

func TestResultUp(t *testing.T) {
	tests := []struct {
		r    Result
		want bool
	}{
		{Result{StatusCode: 200}, true},
		{Result{StatusCode: 301}, true},
		{Result{StatusCode: 404}, false},
		{Result{StatusCode: 503}, false},
		{Result{Err: "timeout"}, false},
	}
	for _, tt := range tests {
		if got := tt.r.Up(); got != tt.want {
			t.Errorf("%+v.Up() = %v, want %v", tt.r, got, tt.want)
		}
	}
}

func FuzzNormalizeURL(f *testing.F) {
	f.Add("https://example.com")
	f.Add("http://[::1]:8080/path?q=1")
	f.Add("javascript:alert(1)")
	f.Fuzz(func(t *testing.T, raw string) {
		got, err := NormalizeURL(raw)
		if err != nil {
			return
		}
		// Whatever we accept must survive a second round unchanged.
		again, err := NormalizeURL(got)
		if err != nil {
			t.Fatalf("normalized URL %q rejected on second pass: %v", got, err)
		}
		if again != got {
			t.Fatalf("not idempotent: %q -> %q", got, again)
		}
	})
}

func BenchmarkNormalizeURL(b *testing.B) {
	for b.Loop() {
		_, _ = NormalizeURL("https://example.com/some/path?query=1#fragment")
	}
}
