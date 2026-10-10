package main

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestStatusFor(t *testing.T) {
	errDB := errors.New("connection refused")
	svc := &service{repo: &repository{targets: map[int64]Target{}}}
	_, errMissing := svc.Describe(42)

	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, http.StatusOK},
		{"not found through two wraps", errMissing, http.StatusNotFound},
		{"validation", &ValidationError{Field: "url", Reason: "empty"}, http.StatusBadRequest},
		{"joined validation", Validate("example.com", time.Minute), http.StatusBadRequest},
		{"unknown", errDB, http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StatusFor(tt.err); got != tt.want {
				t.Errorf("StatusFor(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

func TestWrappedMessage(t *testing.T) {
	svc := &service{repo: &repository{targets: map[int64]Target{}}}
	_, err := svc.Describe(7)
	if got, want := err.Error(), "describe: target 7: not found"; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
}

func TestOpaqueCutsChain(t *testing.T) {
	errDB := errors.New("disk full")
	wrapped := Opaque(errDB)
	if errors.Is(wrapped, errDB) {
		t.Error("errors.Is found the cause, want the chain cut")
	}
	if got, want := wrapped.Error(), "storage unavailable: disk full"; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
}

func TestValidateJoinsAllProblems(t *testing.T) {
	if err := Validate("https://example.com", time.Minute); err != nil {
		t.Fatalf("valid input: %v", err)
	}

	err := Validate("example.com", time.Second)
	// errors.Join keeps every error reachable for errors.As/Is.
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		t.Fatalf("err %T does not unwrap to []error", err)
	}
	if n := len(joined.Unwrap()); n != 2 {
		t.Fatalf("got %d errors, want 2", n)
	}
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Field != "url" {
		t.Errorf("errors.As found %+v, want the url error first", ve)
	}
}
