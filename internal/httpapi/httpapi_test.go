package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/casoon/go-tutorial/internal/monitor"
	"github.com/casoon/go-tutorial/internal/sqlitestore"
)

// newServer runs the real API against a real (temporary) SQLite database.
// No mocks: httptest gives us a real HTTP server in-process.
func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	store, err := sqlitestore.Open(t.Context(), filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(New(store, logger))
	t.Cleanup(srv.Close)
	return srv
}

func do(t *testing.T, method, url, body string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(b)
}

func TestTargetsAPI(t *testing.T) {
	srv := newServer(t)

	resp, body := do(t, "POST", srv.URL+"/targets", `{"url":"https://example.com","interval_seconds":30}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST status = %d, body = %s", resp.StatusCode, body)
	}
	if loc := resp.Header.Get("Location"); loc != "/targets/1" {
		t.Errorf("Location = %q, want /targets/1", loc)
	}
	var created targetJSON
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatal(err)
	}
	if created.URL != "https://example.com" || created.IntervalSeconds != 30 {
		t.Errorf("created = %+v", created)
	}

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		wantBody   string
	}{
		{"list", "GET", "/targets", "", 200, `"url":"https://example.com"`},
		{"get", "GET", "/targets/1", "", 200, `"interval_seconds":30`},
		{"get unknown", "GET", "/targets/99", "", 404, "not found"},
		{"get bad id", "GET", "/targets/abc", "", 400, "positive integer"},
		{"duplicate", "POST", "/targets", `{"url":"https://example.com"}`, 409, "already exists"},
		{"interval overflow", "POST", "/targets", `{"url":"https://b.de","interval_seconds":18446744084}`, 400, "interval"},
		{"invalid url", "POST", "/targets", `{"url":"ftp://x"}`, 400, "scheme"},
		{"unknown field", "POST", "/targets", `{"url":"https://a.de","admin":true}`, 400, "unknown field"},
		{"broken json", "POST", "/targets", `{"url":`, 400, "invalid JSON"},
		{"too large", "POST", "/targets", `{"url":"https://a.de/` + strings.Repeat("a", 20000) + `"}`, 400, "must not exceed"},
		{"results empty", "GET", "/targets/1/results", "", 200, "[]"},
		{"results bad limit", "GET", "/targets/1/results?limit=0", "", 400, "limit"},
		{"results unknown", "GET", "/targets/99/results", "", 404, "not found"},
		{"wrong method", "PUT", "/targets/1", "", 405, ""},
		{"healthz", "GET", "/healthz", "", 200, "ok"},
		{"readyz", "GET", "/readyz", "", 200, "ready"},
		{"delete", "DELETE", "/targets/1", "", 204, ""},
		{"delete again", "DELETE", "/targets/1", "", 404, "not found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, body := do(t, tt.method, srv.URL+tt.path, tt.body)
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", resp.StatusCode, tt.wantStatus, body)
			}
			if !strings.Contains(body, tt.wantBody) {
				t.Errorf("body = %s, want it to contain %q", body, tt.wantBody)
			}
		})
	}
}

// failingStore makes Targets fail, to check that internal errors are
// not leaked to the client.
type failingStore struct{ Store }

func (failingStore) Targets(context.Context) ([]monitor.Target, error) {
	return nil, errors.New("secret: open /var/lib/upwatch/upwatch.db: permission denied")
}

func TestInternalErrorsAreNotLeaked(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(New(failingStore{}, logger))
	defer srv.Close()

	resp, body := do(t, "GET", srv.URL+"/targets", "")
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	if strings.Contains(body, "secret") {
		t.Errorf("body leaks internal error: %s", body)
	}
}

func TestPanicBecomes500(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// The embedded nil Store panics on any call that is not overridden.
	srv := httptest.NewServer(New(failingStore{}, logger))
	defer srv.Close()

	resp, _ := do(t, "GET", srv.URL+"/targets/1", "")
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
}
