package mcp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// transport_ssrf.go is the enforce-only Layer-2 SSRF defense (T-31-SSRF-02). It is
// installed by OpenHTTP ONLY when cfg.Enforce is set and the caller supplied no
// http.Client; under dev OpenHTTP keeps http.DefaultClient unchanged (keep-alives
// intact, zero behaviour/perf change). The dialContext resolves+classifies+pins the
// target and dials ONLY the pinned IP (no second lookup can rebind it); the
// net.Dialer.Control hook re-classifies the post-resolution IP right before connect,
// defeating a DNS rebind between resolve and dial. Mirrors internal/web/transport.go.

const mcpDialConnectTimeout = 10 * time.Second

// dialFunc is the injectable inner-dial seam: a test passes a recorder to assert the
// transport dials the pinned IP; production uses a real net.Dialer.
type dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

type hardenedDialer struct {
	res  resolver
	dial dialFunc
}

// newHardenedDialer composes the hardened dialer. dial may be nil — a real net.Dialer
// whose Control hook re-checks the post-resolution IP is used then.
func newHardenedDialer(res resolver, dial dialFunc) *hardenedDialer {
	hd := &hardenedDialer{res: res, dial: dial}
	if hd.dial == nil {
		hd.dial = (&net.Dialer{Timeout: mcpDialConnectTimeout, Control: hd.control}).DialContext
	}
	return hd
}

// newHardenedHTTPClient builds the enforce-only SSRF-hardened http.Client. Keep-
// alives are disabled so a pinned-IP connection is never reused across hosts.
func newHardenedHTTPClient(res resolver) *http.Client {
	hd := newHardenedDialer(res, nil)
	return &http.Client{
		Transport: &http.Transport{
			DialContext:       hd.dialContext,
			DisableKeepAlives: true,
		},
	}
}

// dialContext is the primary hardened gate: split host:port, resolve, classify EVERY
// record (fail closed on ANY blocked one — never cherry-pick a public IP), then dial
// the PINNED first IP literal so no second DNS lookup can rebind the target.
func (h *hardenedDialer) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := h.res.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("ssrf: resolve %q: %w", host, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("ssrf: resolve %q: no addresses", host)
	}
	var pinned netip.Addr
	for _, ip := range ips {
		if reason, blocked := classify(ip); blocked {
			return nil, fmt.Errorf("ssrf: blocked dial %s (%s)", ip, reason)
		}
		if !pinned.IsValid() {
			pinned = ip.Unmap()
		}
	}
	return h.dial(ctx, network, net.JoinHostPort(pinned.String(), port))
}

// control runs AFTER resolution and BEFORE connect (address is the post-resolution
// ip:port), re-classifying the dialed IP so a rebind to a private/metadata target is
// rejected even on a path that dialed by name. Fail-closed on an unparseable host.
func (h *hardenedDialer) control(_ string, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("ssrf: bad dial address %q", address)
	}
	ip, perr := netip.ParseAddr(host)
	if perr != nil {
		return fmt.Errorf("ssrf: unparseable dial ip %q", host)
	}
	if reason, blocked := classify(ip); blocked {
		return fmt.Errorf("ssrf: blocked rebind to %s (%s)", host, reason)
	}
	return nil
}
