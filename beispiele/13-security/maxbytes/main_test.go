package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMaxBytesReader(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
	}{
		{"fits", `{"url":"https://example.com"}`, http.StatusCreated},
		{"broken JSON", `{"url":`, http.StatusBadRequest},
		{"too large", `{"url":"` + strings.Repeat("a", 2*maxBodyBytes) + `"}`, http.StatusRequestEntityTooLarge},
		{"unknown field", `{"url":"https://example.com","admin":true}`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/targets", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			handler().ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d (body: %s)", rec.Code, tt.want, rec.Body)
			}
		})
	}
}
