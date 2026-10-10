// Command embedding shows that embedding is composition, not inheritance,
// how to wrap a dependency by embedding an interface, and a struct whose
// zero value is ready to use.
//
//	go run ./beispiele/04-go-denkweise/embedding
package main

import (
	"fmt"
	"sync"
)

// HTTPCheck describes itself via Name.
type HTTPCheck struct {
	URL string
}

func (c HTTPCheck) Name() string { return "http" }

func (c HTTPCheck) Describe() string { return c.Name() + " check of " + c.URL }

// TLSCheck embeds HTTPCheck. Its fields and methods are promoted, so
// tc.URL and tc.Describe() work. TLSCheck also declares its own Name.
type TLSCheck struct {
	HTTPCheck
	MinVersion string
}

func (c TLSCheck) Name() string { return "tls" }

// Store is a dependency with two methods.
type Store interface {
	Save(url string, status int) error
	Count() int
}

// memStore is a minimal Store.
type memStore struct {
	mu    sync.Mutex
	saved []string
}

func (m *memStore) Save(url string, status int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saved = append(m.saved, fmt.Sprintf("%s %d", url, status))
	return nil
}

func (m *memStore) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.saved)
}

// countingStore wraps any Store. Embedding the interface promotes every
// method; countingStore only replaces Save. Count is forwarded unchanged.
type countingStore struct {
	Store
	saves int
}

func (c *countingStore) Save(url string, status int) error {
	c.saves++
	return c.Store.Save(url, status) // explicit call of the wrapped method
}

// Stats needs no constructor: the zero value is an empty, usable
// statistic. sync.Mutex and int are both ready to use at zero.
type Stats struct {
	mu   sync.Mutex
	up   int
	down int
}

func (s *Stats) Record(status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if status >= 200 && status < 400 {
		s.up++
	} else {
		s.down++
	}
}

func (s *Stats) Ratio() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.up+s.down == 0 {
		return 0
	}
	return float64(s.up) / float64(s.up+s.down)
}

func main() {
	tc := TLSCheck{HTTPCheck: HTTPCheck{URL: "https://example.com"}, MinVersion: "1.3"}
	fmt.Println(tc.Name())     // tls: TLSCheck's own method
	fmt.Println(tc.Describe()) // http check of ...: Describe belongs to HTTPCheck

	store := &countingStore{Store: &memStore{}}
	_ = store.Save("https://example.com", 200)
	_ = store.Save("https://example.org", 503)
	fmt.Println("saves:", store.saves, "count:", store.Count())

	var stats Stats // no New, no init
	stats.Record(200)
	stats.Record(503)
	fmt.Printf("up ratio: %.2f\n", stats.Ratio())
}
