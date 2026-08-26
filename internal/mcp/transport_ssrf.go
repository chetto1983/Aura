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

// transport_ssrf.go is the strict-policy Layer-2 SSRF defense (T-31-SSRF-02).
// OpenHTTP installs it when the resolved EgressPolicy is enforced and no test
// client was injected. The dialContext resolves+classifies+pins the target and
// dials ONLY the pinned IP; the Control hook re-classifies the post-resolution
// IP immediately before connect. Mirrors internal/web/transport.go.

const mcpDialConnectTimeout = 10 * time.Second

// withMCPRedirectGuard re-validates EVERY hop of a redirect chain against the same
// policy that cleared the initial endpoint. Without it the endpoint check is a
// formality: a cleared host can 302 straight into private or metadata space.
//
// It lives here rather than beside the HTTP client because it must outlive
// http_client.go, deleted in plan 45.1-03 — and because Layer-2 egress defense is
// exactly this file's concern.
func withMCPRedirectGuard(base *http.Client, policy EgressPolicy, res resolver) *http.Client {
	cloned := *base
	previous := base.CheckRedirect
	cloned.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if _, err := guardEndpointWithPolicy(req.Context(), req.URL.String(), policy, res); err != nil {
			return fmt.Errorf("mcp redirect blocked: %w", err)
		}
		if previous != nil {
			return previous(req, via)
		}
		if len(via) >= 10 {
			return fmt.Errorf("mcp redirect stopped after %d hops", len(via))
		}
		return nil
	}
	return &cloned
}

// dialFunc is the injectable inner-dial seam: a test passes a recorder to assert the
// transport dials the pinned IP; production uses a real net.Dialer.
type dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

type hardenedDialer struct {
	res    resolver
	dial   dialFunc
	policy EgressPolicy
}

func newHardenedDialerWithPolicy(res resolver, dial dialFunc, policy EgressPolicy) *hardenedDialer {
	return &hardenedDialer{res: res, dial: dial, policy: policy}
}

// newHardenedHTTPClient builds the enforce-only SSRF-hardened http.Client. Keep-
// alives are disabled so a pinned-IP connection is never reused across hosts.
func newHardenedHTTPClient(res resolver, policy EgressPolicy) *http.Client {
	hd := newHardenedDialerWithPolicy(res, nil, policy)
	return &http.Client{
		Transport: &http.Transport{
			DialContext:       hd.dialContext,
			DisableKeepAlives: true,
		},
	}
}

func oauthHTTPClient(policy EgressPolicy) *http.Client {
	client := http.DefaultClient
	if policy.Enforced() {
		client = newHardenedHTTPClient(net.DefaultResolver, policy)
	}
	return withMCPRedirectGuard(client, policy, net.DefaultResolver)
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
	authority := canonicalDialAuthority(host, port)
	pinned, err := validateResolvedTarget(h.policy, authority, ips)
	if err != nil {
		return nil, err
	}
	pinnedAddress := net.JoinHostPort(pinned.String(), port)
	if h.dial != nil {
		return h.dial(ctx, network, pinnedAddress)
	}
	allowPrivate := h.policy.allowsPrivateAuthority(authority)
	dialer := &net.Dialer{
		Timeout: mcpDialConnectTimeout,
		Control: func(network, address string, conn syscall.RawConn) error {
			return h.controlAddress(network, address, conn, allowPrivate)
		},
	}
	return dialer.DialContext(ctx, network, pinnedAddress)
}

func (h *hardenedDialer) controlAddress(_ string, address string, _ syscall.RawConn, allowPrivate bool) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("ssrf: bad dial address %q", address)
	}
	ip, perr := netip.ParseAddr(host)
	if perr != nil {
		return fmt.Errorf("ssrf: unparseable dial ip %q", host)
	}
	reason, blocked := classify(ip)
	if metadataAddress(ip) {
		return fmt.Errorf("ssrf: blocked rebind to %s (%s)", host, reason)
	}
	if blocked && (!allowPrivate || !recipePrivateReason(reason)) {
		return fmt.Errorf("ssrf: blocked rebind to %s (%s)", host, reason)
	}
	return nil
}
