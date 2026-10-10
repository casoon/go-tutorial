// Package httpapi exposes upwatch as a JSON API using only net/http.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/casoon/go-tutorial/internal/monitor"
)

// Store is the subset of persistence the API needs.
type Store interface {
	CreateTarget(ctx context.Context, t monitor.Target) (monitor.Target, error)
	Target(ctx context.Context, id int64) (monitor.Target, error)
	Targets(ctx context.Context) ([]monitor.Target, error)
	DeleteTarget(ctx context.Context, id int64) error
	Results(ctx context.Context, targetID int64, limit int) ([]monitor.Result, error)
	Ping(ctx context.Context) error
}

// maxBodyBytes caps request bodies. A target is a URL and a number;
// anything larger is a mistake or an attack.
const maxBodyBytes = 16 << 10

// API holds the dependencies of all handlers.
type API struct {
	store  Store
	logger *slog.Logger
}

// New returns the complete HTTP handler including middleware.
func New(store Store, logger *slog.Logger) http.Handler {
	a := &API{store: store, logger: logger}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /targets", a.listTargets)
	mux.HandleFunc("POST /targets", a.createTarget)
	mux.HandleFunc("GET /targets/{id}", a.getTarget)
	mux.HandleFunc("DELETE /targets/{id}", a.deleteTarget)
	mux.HandleFunc("GET /targets/{id}/results", a.listResults)
	mux.HandleFunc("GET /healthz", a.healthz)
	mux.HandleFunc("GET /readyz", a.readyz)

	// Outermost first: logging sees every request, including those that
	// panicked, because recoverPanics turns the panic into a 500 inside it.
	return a.logRequests(a.recoverPanics(securityHeaders(mux)))
}

// Wire format. Keeping it separate from monitor.Target means the database
// schema, the domain model and the public API can change independently.

type targetJSON struct {
	ID              int64     `json:"id"`
	URL             string    `json:"url"`
	IntervalSeconds int64     `json:"interval_seconds"`
	CreatedAt       time.Time `json:"created_at"`
}

func toTargetJSON(t monitor.Target) targetJSON {
	return targetJSON{
		ID:              t.ID,
		URL:             t.URL,
		IntervalSeconds: int64(t.Interval / time.Second),
		CreatedAt:       t.CreatedAt,
	}
}

type resultJSON struct {
	ID         int64     `json:"id"`
	StatusCode int       `json:"status_code"`
	DurationMS int64     `json:"duration_ms"`
	Error      string    `json:"error,omitempty"`
	Up         bool      `json:"up"`
	CheckedAt  time.Time `json:"checked_at"`
}

type createTargetRequest struct {
	URL             string `json:"url"`
	IntervalSeconds int64  `json:"interval_seconds"`
}

func (a *API) listTargets(w http.ResponseWriter, r *http.Request) {
	targets, err := a.store.Targets(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	out := make([]targetJSON, 0, len(targets))
	for _, t := range targets {
		out = append(out, toTargetJSON(t))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) createTarget(w http.ResponseWriter, r *http.Request) {
	var req createTargetRequest
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	target, err := monitor.NewTarget(req.URL, time.Duration(req.IntervalSeconds)*time.Second)
	if err != nil {
		a.handleError(w, r, err)
		return
	}
	created, err := a.store.CreateTarget(r.Context(), target)
	if err != nil {
		a.handleError(w, r, err)
		return
	}

	w.Header().Set("Location", fmt.Sprintf("/targets/%d", created.ID))
	writeJSON(w, http.StatusCreated, toTargetJSON(created))
}

func (a *API) getTarget(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	t, err := a.store.Target(r.Context(), id)
	if err != nil {
		a.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toTargetJSON(t))
}

func (a *API) deleteTarget(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := a.store.DeleteTarget(r.Context(), id); err != nil {
		a.handleError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listResults(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	limit := 50
	if s := r.URL.Query().Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 1000 {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 1000")
			return
		}
		limit = n
	}

	// Distinguish "unknown target" (404) from "no results yet" (empty list).
	if _, err := a.store.Target(r.Context(), id); err != nil {
		a.handleError(w, r, err)
		return
	}
	results, err := a.store.Results(r.Context(), id, limit)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	out := make([]resultJSON, 0, len(results))
	for _, res := range results {
		out = append(out, resultJSON{
			ID:         res.ID,
			StatusCode: res.StatusCode,
			DurationMS: res.Duration.Milliseconds(),
			Error:      res.Err,
			Up:         res.Up(),
			CheckedAt:  res.CheckedAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// healthz answers "is the process alive?" and never touches dependencies.
func (a *API) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// readyz answers "can this instance serve traffic?" and checks the database.
func (a *API) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	if err := a.store.Ping(ctx); err != nil {
		a.logger.Warn("readiness check failed", "err", err)
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// handleError maps domain errors to HTTP status codes. This is the only
// place where the API needs to know which errors exist.
func (a *API) handleError(w http.ResponseWriter, r *http.Request, err error) {
	var ve *monitor.ValidationError
	switch {
	case errors.As(err, &ve):
		writeError(w, http.StatusBadRequest, ve.Error())
	case errors.Is(err, monitor.ErrNotFound):
		writeError(w, http.StatusNotFound, "target not found")
	case errors.Is(err, monitor.ErrDuplicate):
		writeError(w, http.StatusConflict, "target already exists")
	default:
		a.serverError(w, r, err)
	}
}

// serverError logs the details and returns a generic message: internal
// error texts can leak SQL, file paths or hostnames.
func (a *API) serverError(w http.ResponseWriter, r *http.Request, err error) {
	a.logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, "internal server error")
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "id must be a positive integer")
		return 0, false
	}
	return id, true
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return fmt.Errorf("body must not exceed %d bytes", maxErr.Limit)
		}
		return fmt.Errorf("invalid JSON: %v", err)
	}
	if dec.More() {
		return errors.New("body must contain a single JSON object")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
