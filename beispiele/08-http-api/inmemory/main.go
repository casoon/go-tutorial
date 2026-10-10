// Command inmemory is a precursor of upwatch's HTTP API: the same routes
// and JSON handling with net/http, but backed by a map instead of SQLite.
//
//	go run ./beispiele/08-http-api/inmemory
//	curl -i -X POST localhost:8081/targets -d '{"url":"https://example.com"}'
package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"
)

var errNotFound = errors.New("not found")

// Target is the stored model.
type Target struct {
	ID       int64
	URL      string
	Interval time.Duration
}

// Store keeps targets in memory. Many readers may hold the RWMutex at the
// same time, writers get it exclusively.
type Store struct {
	mu      sync.RWMutex
	nextID  int64
	targets map[int64]Target
}

// NewStore returns an empty Store.
func NewStore() *Store {
	return &Store{targets: make(map[int64]Target)}
}

func (s *Store) Create(t Target) Target {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	t.ID = s.nextID
	s.targets[t.ID] = t
	return t
}

func (s *Store) Get(id int64) (Target, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.targets[id]
	if !ok {
		return Target{}, fmt.Errorf("target %d: %w", id, errNotFound)
	}
	return t, nil
}

func (s *Store) List() []Target {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Target, 0, len(s.targets))
	for _, t := range s.targets {
		out = append(out, t)
	}
	slices.SortFunc(out, func(a, b Target) int { return cmp.Compare(a.ID, b.ID) })
	return out
}

func (s *Store) Delete(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.targets[id]; !ok {
		return fmt.Errorf("target %d: %w", id, errNotFound)
	}
	delete(s.targets, id)
	return nil
}

// targetJSON is the wire format, separate from Target.
type targetJSON struct {
	ID              int64  `json:"id"`
	URL             string `json:"url"`
	IntervalSeconds int64  `json:"interval_seconds"`
}

func toJSON(t Target) targetJSON {
	return targetJSON{ID: t.ID, URL: t.URL, IntervalSeconds: int64(t.Interval / time.Second)}
}

type createRequest struct {
	URL             string `json:"url"`
	IntervalSeconds int64  `json:"interval_seconds"`
}

// NewHandler registers all routes on a ServeMux.
func NewHandler(store *Store) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /targets", func(w http.ResponseWriter, r *http.Request) {
		out := []targetJSON{} // encodes as [], never as null
		for _, t := range store.List() {
			out = append(out, toJSON(t))
		}
		writeJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("POST /targets", func(w http.ResponseWriter, r *http.Request) {
		var req createRequest
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		if req.URL == "" {
			writeError(w, http.StatusBadRequest, "url is required")
			return
		}
		if req.IntervalSeconds == 0 {
			req.IntervalSeconds = 60
		}
		t := store.Create(Target{URL: req.URL, Interval: time.Duration(req.IntervalSeconds) * time.Second})
		w.Header().Set("Location", fmt.Sprintf("/targets/%d", t.ID))
		writeJSON(w, http.StatusCreated, toJSON(t))
	})

	mux.HandleFunc("GET /targets/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "id must be an integer")
			return
		}
		t, err := store.Get(id)
		if errors.Is(err, errNotFound) {
			writeError(w, http.StatusNotFound, "target not found")
			return
		}
		writeJSON(w, http.StatusOK, toJSON(t))
	})

	mux.HandleFunc("DELETE /targets/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "id must be an integer")
			return
		}
		if err := store.Delete(id); errors.Is(err, errNotFound) {
			writeError(w, http.StatusNotFound, "target not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func main() {
	srv := &http.Server{
		Addr:              "localhost:8081",
		Handler:           NewHandler(NewStore()),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Println("listening on", srv.Addr)
	log.Fatal(srv.ListenAndServe())
}
