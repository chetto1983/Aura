//go:build !web_integration

package web

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"sync"
	"testing"
)

// recordingDialer captures the address each DialContext call targets so a test
// can assert the transport dialed the PINNED IP, not the hostname. It returns a
// sentinel error (we only assert the dial target, never complete a connection).
type recordingDialer struct {
	mu   sync.Mutex
	dial []string
}

var errStopDial = errors.New("recording dialer: stop")

func (d *recordingDialer) dialContext(_ context.Context, _, addr string) (net.Conn, error) {
	d.mu.Lock()
	d.dial = append(d.dial, addr)
	d.mu.Unlock()
	return nil, errStopDial
}

func (d *recordingDialer) last() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.dial) == 0 {
		return ""
	}
	return d.dial[len(d.dial)-1]
}

// TestTransport_DialsPinnedIP injects a stub resolver that maps the host to a
// PUBLIC IP and a recording dialer; it then drives one request through the
// hardened client and asserts the dialer was handed the pinned IP string
// (net.JoinHostPort(pinnedIP, port)), never the original hostname — proving the
// resolve-once-then-pin path closes the rebinding window (Pattern 1).
func TestTransport_DialsPinnedIP(t *testing.T) {
	t.Parallel()
	res := stubResolver{answers: map[string][]netip.Addr{
		"good.test": {netip.MustParseAddr("1.2.3.4")},
	}}
	rd := &recordingDialer{}
	ht := newHardenedTransport(newGuard(res, newDNSPin(60), nil), rd.dialContext, "Aura/test")

	req, _ := http.NewRequestWithContext(withConvID(context.Background(), "conv-1"),
		http.MethodGet, "http://good.test:8080/x", nil)
	_, err := ht.client.Do(req)
	// The dial is intercepted (errStopDial) — we only care about WHERE it dialed.
	if err == nil {
		t.Fatal("expected the recording dialer to short-circuit the connection")
	}
	if got := rd.last(); got != "1.2.3.4:8080" {
		t.Fatalf("dialed %q, want the pinned IP 1.2.3.4:8080 (not the hostname)", got)
	}
}

// TestTransport_ControlRecheck proves the defense-in-depth Dialer.Control hook
// rejects a post-resolution IP that classifies blocked, independent of the dial
// path. We call the Control hook directly with a blocked resolved address.
func TestTransport_ControlRecheck(t *testing.T) {
	t.Parallel()
	ht := newHardenedTransport(newGuard(stubResolver{}, newDNSPin(60), nil), nil, "Aura/test")

	if err := ht.control("tcp", "169.254.169.254:80", nil); err == nil {
		t.Fatal("Control must reject a blocked post-resolution IP (metadata)")
	}
	if err := ht.control("tcp", "127.0.0.1:80", nil); err == nil {
		t.Fatal("Control must reject loopback")
	}
	// A public IP passes the recheck.
	if err := ht.control("tcp", "1.2.3.4:80", nil); err != nil {
		t.Fatalf("Control rejected a public IP: %v", err)
	}
}

// TestTransport_NoAutoRedirect asserts the hardened client NEVER auto-follows a
// 3xx: CheckRedirect returns http.ErrUseLastResponse so the per-hop revalidation
// loop (fetcher.go, Wave 3) owns redirect handling (D-29).
func TestTransport_NoAutoRedirect(t *testing.T) {
	t.Parallel()
	ht := newHardenedTransport(newGuard(stubResolver{}, newDNSPin(60), nil), nil, "Aura/test")
	if got := ht.client.CheckRedirect(nil, nil); !errors.Is(got, http.ErrUseLastResponse) {
		t.Fatalf("CheckRedirect = %v, want http.ErrUseLastResponse", got)
	}
}

// TestTransport_BlockedHostDialFailsClosed proves the dialContext path itself
// fails closed when validateAndPin blocks the host (no dial attempt reaches the
// recording dialer).
func TestTransport_BlockedHostDialFailsClosed(t *testing.T) {
	t.Parallel()
	res := stubResolver{answers: map[string][]netip.Addr{
		"evil.test": {netip.MustParseAddr("127.0.0.1")},
	}}
	rd := &recordingDialer{}
	ht := newHardenedTransport(newGuard(res, newDNSPin(60), nil), rd.dialContext, "Aura/test")

	req, _ := http.NewRequestWithContext(withConvID(context.Background(), "conv-1"),
		http.MethodGet, "http://evil.test/x", nil)
	if _, err := ht.client.Do(req); err == nil {
		t.Fatal("expected a blocked-host dial to fail closed")
	}
	if rd.last() != "" {
		t.Fatalf("blocked host must not reach the dialer, dialed %q", rd.last())
	}
}
