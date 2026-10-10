// Package monitor holds the core types of upwatch: the targets we watch and
// the results of checking them. It has no dependencies on HTTP, SQL or any
// other infrastructure, so every other package can import it.
package monitor

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"
)

// Sentinel errors shared by all storage implementations.
var (
	ErrNotFound  = errors.New("not found")
	ErrDuplicate = errors.New("already exists")
)

// Interval bounds keep users from hammering a site or never checking it.
const (
	MinInterval     = 10 * time.Second
	MaxInterval     = 24 * time.Hour
	DefaultInterval = time.Minute
)

// Target is a URL that upwatch checks periodically.
type Target struct {
	ID        int64
	URL       string
	Interval  time.Duration
	CreatedAt time.Time
}

// Result is the outcome of a single check.
type Result struct {
	ID         int64
	TargetID   int64
	StatusCode int
	Duration   time.Duration
	Err        string
	CheckedAt  time.Time
}

// Up reports whether the check counts as successful.
func (r Result) Up() bool {
	return r.Err == "" && r.StatusCode >= 200 && r.StatusCode < 400
}

// ValidationError describes invalid user input. Handlers map it to 400.
type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("invalid %s: %s", e.Field, e.Reason)
}

// NewTarget validates raw input and returns a Target ready to be stored.
// An interval of zero selects DefaultInterval.
func NewTarget(rawURL string, interval time.Duration) (Target, error) {
	u, err := NormalizeURL(rawURL)
	if err != nil {
		return Target{}, err
	}
	if interval == 0 {
		interval = DefaultInterval
	}
	if interval < MinInterval || interval > MaxInterval {
		return Target{}, &ValidationError{
			Field:  "interval",
			Reason: fmt.Sprintf("must be between %s and %s", MinInterval, MaxInterval),
		}
	}
	return Target{URL: u, Interval: interval}, nil
}

// MaxURLLength bounds the normalized URL, which is what gets stored.
const MaxURLLength = 2048

// NormalizeURL accepts only absolute http(s) URLs with a host.
func NormalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", &ValidationError{Field: "url", Reason: "must not be empty"}
	}
	// Cheap early exit before parsing; the binding check is below.
	if len(raw) > MaxURLLength {
		return "", &ValidationError{Field: "url", Reason: "too long"}
	}
	// Valid URLs never contain raw whitespace, and url.Parse lets it through
	// in the query. Also found by the fuzzer: "http://x? #f" lost its
	// fragment and then ended in a space that the next pass trimmed.
	if strings.ContainsFunc(raw, unicode.IsSpace) {
		return "", &ValidationError{Field: "url", Reason: "must not contain whitespace"}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", &ValidationError{Field: "url", Reason: "cannot be parsed"}
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", &ValidationError{Field: "url", Reason: "scheme must be http or https"}
	}
	if u.Host == "" {
		return "", &ValidationError{Field: "url", Reason: "host is missing"}
	}
	if u.User != nil {
		return "", &ValidationError{Field: "url", Reason: "credentials in URL are not allowed"}
	}
	u.Fragment = ""

	// Normalizing can make a URL longer: String() percent-encodes bytes
	// that may not appear raw, turning one byte into three. Found by
	// FuzzNormalizeURL, see testdata/fuzz.
	normalized := u.String()
	if len(normalized) > MaxURLLength {
		return "", &ValidationError{Field: "url", Reason: "too long"}
	}
	return normalized, nil
}
