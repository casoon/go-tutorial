// Command kette passes a context.Context through several layers, the way
// upwatch does from main through the scheduler and checker down to the
// HTTP request. A local test server plays the monitored website.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"
)

// requestIDKey is unexported, so no other package can collide with it.
type requestIDKey struct{}

// WithRequestID stores request-scoped metadata, not a dependency.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestID returns the ID stored by WithRequestID or "-".
func RequestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}
	return "-"
}

// fetchStatus is the lowest layer: it hands ctx to the HTTP request.
// When ctx ends, the request is aborted and the connection closed.
func fetchStatus(ctx context.Context, client *http.Client, url string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

// Check limits a single check to timeout, like check.Check in upwatch.
// A slow site is a valid outcome ("timeout"); a caller that gave up is an
// error, because then there is no meaningful outcome to record.
func Check(ctx context.Context, client *http.Client, url string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	status, err := fetchStatus(ctx, client, url)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return "", fmt.Errorf("check %s: %w", url, ctx.Err())
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return "timeout", nil
		}
		return err.Error(), nil
	}
	return fmt.Sprintf("status %d", status), nil
}

// ErrShutdown is the reason we cancel with in main.
var ErrShutdown = errors.New("shutting down")

// slowSite answers after delay, or gives up when the client goes away.
func slowSite(delay time.Duration) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(delay):
			w.WriteHeader(http.StatusOK)
		case <-r.Context().Done():
		}
	}))
}

func main() {
	site := slowSite(200 * time.Millisecond)
	defer site.Close()
	client := site.Client()

	ctx := WithRequestID(context.Background(), "req-42")

	// 1. Enough time: the check completes.
	out, err := Check(ctx, client, site.URL, time.Second)
	fmt.Printf("[%s] generous timeout: %q, err=%v\n", RequestID(ctx), out, err)

	// 2. The site is too slow for the check's own timeout.
	out, err = Check(ctx, client, site.URL, 50*time.Millisecond)
	fmt.Printf("[%s] tight timeout:    %q, err=%v\n", RequestID(ctx), out, err)

	// 3. The caller cancels with a cause while the check is running.
	parent, cancel := context.WithCancelCause(ctx)
	stop := context.AfterFunc(parent, func() {
		fmt.Println("AfterFunc: parent canceled, cause:", context.Cause(parent))
	})
	defer stop()
	time.AfterFunc(50*time.Millisecond, func() { cancel(ErrShutdown) })
	out, err = Check(parent, client, site.URL, time.Second)
	fmt.Printf("[%s] caller canceled:  %q, err=%v\n", RequestID(parent), out, err)
	fmt.Println("Err:", parent.Err(), "| Cause:", context.Cause(parent))

	// 4. Work that must finish even though parent is canceled.
	detached := context.WithoutCancel(parent)
	fmt.Printf("WithoutCancel: Err=%v, request ID still %q\n", detached.Err(), RequestID(detached))
}
