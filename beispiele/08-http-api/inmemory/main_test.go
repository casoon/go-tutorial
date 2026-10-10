package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPI(t *testing.T) {
	srv := httptest.NewServer(NewHandler(NewStore()))
	defer srv.Close()

	// Steps build on each other: create, read, delete, read again.
	steps := []struct {
		method, path, body string
		wantStatus         int
		wantBody           string
	}{
		{"GET", "/targets", "", 200, "[]"},
		{"POST", "/targets", `{"url":"https://example.com","interval_seconds":30}`, 201, `"id":1`},
		{"POST", "/targets", `{"url":"https://example.com","color":"red"}`, 400, "unknown field"},
		{"GET", "/targets/1", "", 200, `"interval_seconds":30`},
		{"GET", "/targets/abc", "", 400, "integer"},
		{"PUT", "/targets/1", "", 405, ""},
		{"DELETE", "/targets/1", "", 204, ""},
		{"GET", "/targets/1", "", 404, "not found"},
		{"GET", "/healthz", "", 200, "ok"},
	}
	for _, s := range steps {
		req, err := http.NewRequestWithContext(t.Context(), s.method, srv.URL+s.path, strings.NewReader(s.body))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != s.wantStatus {
			t.Errorf("%s %s: status %d, want %d (body %s)", s.method, s.path, resp.StatusCode, s.wantStatus, body)
		}
		if !strings.Contains(string(body), s.wantBody) {
			t.Errorf("%s %s: body %s, want it to contain %s", s.method, s.path, body, s.wantBody)
		}
	}
}

func TestLocationHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/targets", strings.NewReader(`{"url":"https://example.com"}`))
	NewHandler(NewStore()).ServeHTTP(rec, req)

	if got := rec.Header().Get("Location"); got != "/targets/1" {
		t.Errorf("Location = %q, want /targets/1", got)
	}
}
