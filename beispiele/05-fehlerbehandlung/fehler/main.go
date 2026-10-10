// Command fehler shows the error handling patterns used throughout upwatch:
// sentinel errors, custom error types, wrapping with %w, errors.Is/As,
// errors.Join and mapping errors to status codes at the boundary.
package main

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ErrNotFound is a sentinel error: a fixed value callers compare against.
var ErrNotFound = errors.New("not found")

// ValidationError carries details about invalid input.
type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("invalid %s: %s", e.Field, e.Reason)
}

// Target is a URL we want to watch.
type Target struct {
	ID       int64
	URL      string
	Interval time.Duration
}

// repository simulates a storage layer.
type repository struct {
	targets map[int64]Target
	broken  error // if set, every call fails with this error
}

func (r *repository) Find(id int64) (Target, error) {
	if r.broken != nil {
		return Target{}, fmt.Errorf("query target %d: %w", id, r.broken)
	}
	t, ok := r.targets[id]
	if !ok {
		return Target{}, fmt.Errorf("target %d: %w", id, ErrNotFound)
	}
	return t, nil
}

// service adds context to every error before passing it on.
type service struct {
	repo *repository
}

func (s *service) Describe(id int64) (string, error) {
	t, err := s.repo.Find(id)
	if err != nil {
		return "", fmt.Errorf("describe: %w", err)
	}
	return fmt.Sprintf("#%d %s every %s", t.ID, t.URL, t.Interval), nil
}

// Validate reports every problem at once, like config.Load in upwatch.
func Validate(rawURL string, interval time.Duration) error {
	var errs []error
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		errs = append(errs, &ValidationError{Field: "url", Reason: "scheme must be http or https"})
	}
	if interval < 10*time.Second {
		errs = append(errs, &ValidationError{Field: "interval", Reason: "must be at least 10s"})
	}
	return errors.Join(errs...) // nil if errs is empty
}

// StatusFor maps errors to HTTP status codes. It is the only place that
// needs to know which errors exist.
func StatusFor(err error) int {
	var ve *ValidationError
	switch {
	case err == nil:
		return http.StatusOK
	case errors.As(err, &ve):
		return http.StatusBadRequest
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}

// Opaque wraps err with %v: the message stays, the chain is cut.
// Callers can no longer detect the underlying error.
func Opaque(err error) error {
	return fmt.Errorf("storage unavailable: %v", err)
}

func main() {
	errDiskFull := errors.New("disk full")
	ok := &service{repo: &repository{targets: map[int64]Target{
		1: {ID: 1, URL: "https://example.com", Interval: time.Minute},
	}}}
	broken := &service{repo: &repository{broken: errDiskFull}}

	for _, c := range []struct {
		name string
		svc  *service
		id   int64
	}{
		{"found", ok, 1},
		{"missing", ok, 2},
		{"broken", broken, 1},
	} {
		desc, err := c.svc.Describe(c.id)
		fmt.Printf("%-8s status=%d desc=%q err=%v\n", c.name, StatusFor(err), desc, err)
	}

	err := Validate("ftp://example.com", time.Second)
	fmt.Printf("validate status=%d\n%v\n", StatusFor(err), err)

	_, err = broken.Describe(1)
	fmt.Println("wrapped with w, Is finds cause:", errors.Is(err, errDiskFull))
	fmt.Println("wrapped with v, Is finds cause:", errors.Is(Opaque(err), errDiskFull))
}
