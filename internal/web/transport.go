package web

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// dialConnectTimeout bounds the dial only; the total request deadline rides the
// request ctx (the fetcher sets it), mirroring the sandbox docker.go idiom.
const dialConnectTimeout = 10 * time.Second

// dialFunc is the injectable dial seam. Tests pass a recording dialer to assert
// the transport dials the pinned IP; production uses a real net.Dialer.
type dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// convCtxKey carries the conversation ID through the request ctx so dialContext
// (which only receives network+addr) can scope the DNS pin to the conversation.
// An unexported key type prevents collisions with other packages' ctx values.
type convCtxKey struct{}

// withConvID stamps the conversation ID onto a ctx for the hardened client. The
// fetcher wraps the request ctx with this before client.Do so the pin cache keys
// by the right conversation (D-25 per-conversation isolation).
func withConvID(ctx context.Context, convID string) context.Context {
	return context.WithValue(ctx, convCtxKey{}, convID)
}

func convIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(convCtxKey{}).(string); ok {
		return v
	}
	return ""
}

// hardenedTransport is the SSRF-hardened http.Client and its building blocks. The
// dialContext resolves+classifies+pins then dials ONLY the pinned IP (primary
// defense); the Control hook re-checks the post-resolution IP (defense-in-depth);
// CheckRedirect refuses to auto-follow so the fetcher revalidates each hop;
// DisableKeepAlives keeps goleak order-independent (copied from docker.go).
type hardenedTransport struct {
	client *http.Client
	guard  *guard
	dial   dialFunc
	ua     string
}

// newHardenedTransport composes the hardened client. dial may be nil — a real
// net.Dialer with a connect timeout is used then. ua is the Aura User-Agent the
// fetcher stamps on every request (D-34/D-35); it is held here so the single
// client owns the egress identity.
func newHardenedTransport(g *guard, dial dialFunc, ua string) *hardenedTransport {
	ht := &hardenedTransport{guard: g, dial: dial, ua: ua}
	if ht.dial == nil {
		// Production path: a real dialer whose Control hook re-checks the
		// post-resolution IP (defense-in-depth). When a test injects a dialFunc,
		// the Control recheck is asserted directly via ht.control.
		ht.dial = (&net.Dialer{Timeout: dialConnectTimeout, Control: ht.control}).DialContext
	}
	ht.client = &http.Client{
		Transport: &http.Transport{
			DialContext:       ht.dialContext,
			DisableKeepAlives: true,
			// Setting DialContext silently disables Go's automatic HTTP/2 upgrade,
			// so without this every web_fetch spoke HTTP/1.1. That is not merely
			// slower — it changes the FAILURE mode. A WAF refusing the client
			// answers h2 with RST_STREAM in ~0.2s (a real, reportable error), while
			// over HTTP/1.1 it simply never answers and the fetch burns the whole
			// deadline before reporting `timeout` with the cause discarded. That is
			// exactly how six br-automation.com fetches looked like network trouble.
			// SSRF containment is unaffected: dialContext still pins the resolved IP
			// and the Control hook still re-checks it, whatever the ALPN protocol.
			ForceAttemptHTTP2: true,
		},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse // never auto-follow (D-29)
		},
	}
	return ht
}

// dialContext is the primary SSRF gate on the dial path. It splits the requested
// host:port, runs validateAndPin (hostname blocklist → pin reuse → resolve +
// classify-every + pin), fails closed on any block reason, and dials the PINNED
// IP string — never the hostname — so no second DNS lookup can rebind the target
// (Pitfall 1 TOCTOU). The Control hook below re-checks even this dialed IP.
func (h *hardenedTransport) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	pinned, reason := h.guard.validateAndPin(ctx, convIDFrom(ctx), host)
	if reason != "" {
		return nil, &internalError{code: CodeBlockedURL, reason: reason, message: "blocked", host: host}
	}
	return h.dial(ctx, network, net.JoinHostPort(pinned.String(), port))
}

// control is the net.Dialer.Control hook: it runs AFTER resolution and BEFORE
// connect, so address is the post-resolution ip:port. It re-parses that IP and
// rejects a blocked one even if some code path dialed by name (defense-in-depth,
// T-07-12). An unparseable host is rejected fail-closed.
func (h *hardenedTransport) control(_ string, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return &internalError{code: CodeBlockedURL, reason: ReasonInvalidTarget, message: "blocked"}
	}
	ip, perr := netip.ParseAddr(host)
	if perr != nil {
		return &internalError{code: CodeBlockedURL, reason: ReasonInvalidTarget, message: "blocked"}
	}
	if _, blocked := classify(ip); blocked {
		return &internalError{code: CodeBlockedURL, reason: ReasonPrivateOrMetadata, message: "blocked", resolvedIP: host}
	}
	return nil
}
