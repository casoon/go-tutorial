// Command grundlagen shows the core of the language in the monitor domain:
// constants with iota, structs, methods, multiple return values, maps,
// switch and defer.
//
//	go run ./beispiele/03-die-sprache-kompakt/grundlagen
package main

import (
	"fmt"
	"strings"
	"time"
)

// Status is a small enum. Go has no enum keyword; a named type plus
// constants with iota covers the same ground.
type Status int

const (
	StatusUnknown Status = iota // 0, also the zero value
	StatusUp                    // 1
	StatusDown                  // 2
)

// String makes Status print nicely with fmt (it satisfies fmt.Stringer).
func (s Status) String() string {
	switch s {
	case StatusUp:
		return "up"
	case StatusDown:
		return "down"
	default:
		return "unknown"
	}
}

// User is the example from the text: data plus a method, no class.
type User struct {
	ID   int
	Name string
}

// Greeting has a value receiver: it only reads u.
func (u User) Greeting() string {
	return "Hello, " + u.Name
}

// Target is a URL to check, Result the outcome of one check.
type Target struct {
	URL      string
	Interval time.Duration
	checks   int
}

// RecordCheck has a pointer receiver: it modifies t.
func (t *Target) RecordCheck() { t.checks++ }

type Result struct {
	StatusCode int
	Duration   time.Duration
}

// Status classifies a result. switch without a condition replaces
// if-else chains.
func (r Result) Status() Status {
	switch {
	case r.StatusCode == 0:
		return StatusUnknown
	case r.StatusCode < 400:
		return StatusUp
	default:
		return StatusDown
	}
}

// splitURL returns two values. The caller decides what it needs.
func splitURL(raw string) (scheme, rest string, ok bool) {
	scheme, rest, ok = strings.Cut(raw, "://")
	return scheme, rest, ok
}

func main() {
	defer fmt.Println("done (deferred, runs last)")

	u := User{ID: 1, Name: "Ada"}
	fmt.Println(u.Greeting())

	t := Target{URL: "https://example.com", Interval: time.Minute}
	t.RecordCheck() // Go takes &t automatically
	t.RecordCheck()
	fmt.Printf("%s checked %d times, every %s\n", t.URL, t.checks, t.Interval)

	if scheme, rest, ok := splitURL(t.URL); ok {
		fmt.Println("scheme:", scheme, "rest:", rest)
	}

	results := []Result{
		{StatusCode: 200, Duration: 120 * time.Millisecond},
		{StatusCode: 503, Duration: 80 * time.Millisecond},
		{},
	}
	counts := map[Status]int{}
	for _, r := range results {
		counts[r.Status()]++ // missing keys read as 0
	}
	for i := range 3 {
		s := Status(i)
		fmt.Printf("%-7s %d\n", s, counts[s])
	}
}
