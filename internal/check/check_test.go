package check

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/casoon/go-tutorial/internal/monitor"
)

func TestCheck(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantStatus int
		wantErr    string
	}{
		{
			name:       "ok",
			handler:    func(w http.ResponseWriter, r *http.Request) {},
			wantStatus: http.StatusOK,
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
			},
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name: "redirect is not followed",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "/elsewhere", http.StatusMovedPermanently)
			},
			wantStatus: http.StatusMovedPermanently,
		},
		{
			name: "slow server times out",
			handler: func(w http.ResponseWriter, r *http.Request) {
				select {
				case <-time.After(time.Second):
				case <-r.Context().Done():
				}
			},
			wantErr: "timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			c := New(NewClient(true), 50*time.Millisecond)
			res, err := c.Check(t.Context(), monitor.Target{ID: 7, URL: srv.URL})
			if err != nil {
				t.Fatalf("Check returned error: %v", err)
			}
			if res.TargetID != 7 {
				t.Errorf("TargetID = %d, want 7", res.TargetID)
			}
			if res.StatusCode != tt.wantStatus {
				t.Errorf("StatusCode = %d, want %d", res.StatusCode, tt.wantStatus)
			}
			if res.Err != tt.wantErr {
				t.Errorf("Err = %q, want %q", res.Err, tt.wantErr)
			}
		})
	}
}

func TestCheckCanceledByCaller(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(20*time.Millisecond, cancel)

	c := New(NewClient(true), time.Second)
	_, err := c.Check(ctx, monitor.Target{URL: srv.URL})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestForbiddenAddresses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("request reached the server")
	}))
	defer srv.Close()

	c := New(NewClient(false), time.Second)
	res, err := c.Check(t.Context(), monitor.Target{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Err, ErrForbiddenAddress.Error()) {
		t.Errorf("Err = %q, want it to mention %q", res.Err, ErrForbiddenAddress)
	}
}

func TestIsPublic(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{"93.184.215.14", true},
		{"2606:4700::1111", true},
		{"127.0.0.1", false},
		{"10.1.2.3", false},
		{"192.168.0.1", false},
		{"169.254.169.254", false}, // cloud metadata endpoint
		{"100.64.0.1", false},
		{"0.0.0.0", false},
		{"::1", false},
		{"fe80::1", false},
		{"fd00::1", false},
		{"::ffff:127.0.0.1", false},
	}
	for _, tt := range tests {
		if got := isPublic(netip.MustParseAddr(tt.ip)); got != tt.want {
			t.Errorf("isPublic(%s) = %v, want %v", tt.ip, got, tt.want)
		}
	}
}
