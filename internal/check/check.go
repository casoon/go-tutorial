// Package check performs a single HTTP check against a URL.
package check

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"

	"github.com/casoon/go-tutorial/internal/monitor"
)

// Checker issues HTTP requests and turns them into monitor.Results.
// The zero value is not usable; create one with New.
type Checker struct {
	client  *http.Client
	timeout time.Duration
	now     func() time.Time
}

// ErrForbiddenAddress is returned when a target resolves to an address
// upwatch must not connect to.
var ErrForbiddenAddress = errors.New("forbidden address")

// New returns a Checker whose requests time out after timeout.
// A nil client selects NewClient(false).
func New(client *http.Client, timeout time.Duration) *Checker {
	if client == nil {
		client = NewClient(false)
	}
	return &Checker{client: client, timeout: timeout, now: time.Now}
}

// NewClient returns the HTTP client used for checks.
//
// Unless allowPrivate is set, it refuses to connect to loopback, private,
// link-local and similar addresses. Users choose the URLs, so without this
// guard anyone could make upwatch probe the internal network or a cloud
// metadata endpoint (server-side request forgery, SSRF).
func NewClient(allowPrivate bool) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	if !allowPrivate {
		// Control runs after DNS resolution, right before connect, so it
		// sees the real IP. Checking the hostname in the URL would not
		// be enough: evil.example could resolve to 127.0.0.1.
		dialer.Control = func(_, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil {
				return err
			}
			if !isPublic(ap.Addr()) {
				return fmt.Errorf("%s: %w", ap.Addr(), ErrForbiddenAddress)
			}
			return nil
		}
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = dialer.DialContext
	transport.Proxy = nil // a proxy would bypass the address check
	transport.MaxIdleConnsPerHost = 2

	return &http.Client{
		Transport: transport,
		// Do not follow redirects: a 301 is an answer, not an error.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func isPublic(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() &&
		!ip.IsLinkLocalUnicast() &&
		!netip.MustParsePrefix("100.64.0.0/10").Contains(ip) // carrier-grade NAT
}

// Check requests target.URL once. Network errors are recorded in the result,
// not returned: an unreachable site is a valid outcome of a check.
// The returned error is non-nil only if ctx was canceled by the caller.
func (c *Checker) Check(ctx context.Context, target monitor.Target) (monitor.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	res := monitor.Result{TargetID: target.ID, CheckedAt: c.now().UTC()}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.URL, nil)
	if err != nil {
		res.Err = fmt.Sprintf("build request: %v", err)
		return res, nil
	}
	req.Header.Set("User-Agent", "upwatch/1.0 (+https://github.com/casoon/go-tutorial)")

	start := time.Now()
	resp, err := c.client.Do(req)
	res.Duration = time.Since(start)
	if err != nil {
		// Distinguish "the caller gave up" from "the site is slow".
		if errors.Is(ctx.Err(), context.Canceled) {
			return res, fmt.Errorf("check %s: %w", target.URL, ctx.Err())
		}
		res.Err = describe(err)
		return res, nil
	}
	defer resp.Body.Close()

	// Drain a bounded part of the body so the connection can be reused.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))

	res.StatusCode = resp.StatusCode
	return res, nil
}

func describe(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return err.Error()
}
