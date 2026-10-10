// Command ssrfguard compares a naive SSRF check (looking at the URL) with
// the approach upwatch uses (checking the resolved IP right before connect).
//
// Run it with: go run ./beispiele/13-security/ssrfguard
package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

var errForbidden = errors.New("forbidden address")

// naiveAllowed inspects the URL only. It rejects literal private IPs but
// has no idea where a hostname will resolve to.
func naiveAllowed(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	ip, err := netip.ParseAddr(u.Hostname())
	if err != nil {
		return true // a hostname: looks harmless, is not checked
	}
	return isPublic(ip)
}

// guardedClient checks the address the dialer is about to connect to,
// after DNS resolution. This is the same idea as check.NewClient in upwatch.
func guardedClient() *http.Client {
	dialer := &net.Dialer{
		Timeout: 2 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil {
				return err
			}
			if !isPublic(ap.Addr()) {
				return fmt.Errorf("%s: %w", ap.Addr(), errForbidden)
			}
			return nil
		},
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = dialer.DialContext
	transport.Proxy = nil
	return &http.Client{
		Transport: transport,
		Timeout:   5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func isPublic(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() &&
		!ip.IsLinkLocalUnicast() &&
		!netip.MustParsePrefix("100.64.0.0/10").Contains(ip)
}

func main() {
	// Stands in for an internal service, e.g. an admin API on localhost.
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "internal secrets")
	}))
	defer internal.Close()

	// Same server, addressed by name instead of by IP.
	byName := strings.Replace(internal.URL, "127.0.0.1", "localhost", 1)

	for _, target := range []string{internal.URL, byName} {
		fmt.Printf("%s\n  naive check allows: %v\n", target, naiveAllowed(target))

		resp, err := guardedClient().Get(target)
		if err != nil {
			fmt.Printf("  guarded client:     %v\n", err)
			continue
		}
		resp.Body.Close()
		fmt.Printf("  guarded client:     %s\n", resp.Status)
	}
}
