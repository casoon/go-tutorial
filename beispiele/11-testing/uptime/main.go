// Command uptime holds a few small functions from the monitoring domain.
// The interesting part is main_test.go:
//
//	go test -v ./beispiele/11-testing/uptime
//	go test -bench=. -benchmem ./beispiele/11-testing/uptime
//	go test -fuzz=FuzzParseInterval -fuzztime=5s ./beispiele/11-testing/uptime
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func main() {
	fmt.Println(Add(2, 3))
	fmt.Println(Classify(200), Classify(503), Classify(42))
	d, err := ParseInterval("90s")
	fmt.Println(d, err)
}

// Add is the smallest possible thing to test.
func Add(a, b int) int { return a + b }

// Classify maps an HTTP status code to "up", "down" or "invalid".
func Classify(code int) string {
	switch {
	case code < 100 || code > 599:
		return "invalid"
	case code < 400:
		return "up"
	default:
		return "down"
	}
}

// ParseInterval parses a check interval like "30s" or "5m" and enforces
// the same bounds as upwatch: at least 10s, at most 24h.
func ParseInterval(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("interval %q: %w", s, err)
	}
	if d < 10*time.Second || d > 24*time.Hour {
		return 0, fmt.Errorf("interval %s: must be between 10s and 24h", d)
	}
	return d, nil
}

// Probe requests url once and returns the status code.
func Probe(ctx context.Context, client *http.Client, url string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}

// WaitUntilUp calls check every interval until it reports true or ctx is
// done. It returns the number of attempts.
func WaitUntilUp(ctx context.Context, every time.Duration, check func(context.Context) bool) (int, error) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for attempt := 1; ; attempt++ {
		if check(ctx) {
			return attempt, nil
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return attempt, ctx.Err()
		}
	}
}

// Pinger is what the readiness handler needs. Defined at the consumer.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Readyz answers 200 if the dependency is reachable and 503 otherwise.
func Readyz(p Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := p.Ping(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "unavailable"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
	}
}
