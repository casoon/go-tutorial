package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestNaiveCheckIsBypassedByHostname(t *testing.T) {
	if naiveAllowed("http://127.0.0.1:8080/") {
		t.Error("naive check should reject a literal loopback IP")
	}
	// localhost resolves to a loopback address, but the naive check never
	// resolves anything.
	if !naiveAllowed("http://localhost:8080/") {
		t.Error("expected the naive check to let localhost through")
	}
}

func TestGuardBlocksLoopbackByName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("request reached the internal server")
	}))
	defer srv.Close()

	byName := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	for _, target := range []string{srv.URL, byName} {
		_, err := guardedClient().Get(target)
		if !errors.Is(err, errForbidden) {
			t.Errorf("Get(%s) err = %v, want errForbidden", target, err)
		}
	}
}

func TestIsPublic(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{"93.184.215.14", true},
		{"127.0.0.1", false},
		{"10.0.0.1", false},
		{"172.16.0.1", false},
		{"192.168.1.1", false},
		{"169.254.169.254", false}, // cloud metadata endpoint
		{"100.64.0.1", false},      // carrier-grade NAT
		{"0.0.0.0", false},
		{"::1", false},
		{"fd00::1", false},
		{"::ffff:10.0.0.1", false}, // IPv4-mapped IPv6
	}
	for _, tt := range tests {
		if got := isPublic(netip.MustParseAddr(tt.ip)); got != tt.want {
			t.Errorf("isPublic(%s) = %v, want %v", tt.ip, got, tt.want)
		}
	}
}
