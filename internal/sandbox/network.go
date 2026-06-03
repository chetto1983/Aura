package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/web"
)

// Host-side forward-proxy egress allowlist (D-08). Untrusted sidecar code never
// owns its own firewall: in-container iptables needs CAP_NET_ADMIN (contradicts
// cap_drop: ALL) and under gVisor only touches the virtual netstack, so egress is
// contained host-side here. A stdlib net/http CONNECT handler validates the target
// HOSTNAME only (NO MITM, D-10): deny-wins glob allowlist (modeled on Codex
// network_policy.rs) → resolve-then-pin via the internal/web export (no copy of
// the IP-class table, OQ2/A4) → Hijack + bidirectional io.Copy opaque tunnel. The
// bridge-gateway reachability + sidecar HTTP_PROXY wiring is 08-08; the live
// pip→pypi assertion is 08-09. This file ships the proxy LOGIC + unit coverage.

// ErrGlobalDenyWildcard rejects a GLOBAL "*" in the deny list at policy-build
// time: a deny-everything glob makes every allow glob unreachable, so the proxy
// would deny all egress regardless of the allowlist — a silent footgun Codex
// explicitly rejects. A global "*" is only meaningful in the ALLOW list (operator
// opt-in to allow-all); in the deny list it is a misconfiguration.
var ErrGlobalDenyWildcard = errors.New("a global '*' in the network deny list denies all egress (footgun); remove it")

// connectDialTimeout bounds the upstream dial of a pinned IP only; the tunnel
// itself lives for the conversation's use. Mirrors web.dialConnectTimeout.
const connectDialTimeout = 10 * time.Second

// hostGlob is one allow/deny pattern. A leading "*." is a single-label-or-more
// suffix wildcard: "*.pythonhosted.org" matches files.pythonhosted.org and
// a.b.pythonhosted.org but NOT the bare parent pythonhosted.org (the dot is
// mandatory). A bare "*" matches everything. Anything else is an exact (case-fold)
// hostname match. This is a careful stdlib suffix helper — no globset dependency
// (Package Legitimacy Gate; the deny-* footgun is handled at build, not match).
type hostGlob struct {
	pattern string // lower-cased
	global  bool   // bare "*"
	suffix  string // for "*.x" this is ".x" (lower-cased); empty otherwise
}

func parseGlob(pat string) hostGlob {
	p := strings.ToLower(strings.TrimSpace(pat))
	switch {
	case p == "*":
		return hostGlob{pattern: p, global: true}
	case strings.HasPrefix(p, "*."):
		return hostGlob{pattern: p, suffix: p[1:]} // ".pythonhosted.org"
	default:
		return hostGlob{pattern: p}
	}
}

// match reports whether host satisfies this glob. host is lower-cased by the
// caller. The "*.x" form requires a non-empty label before ".x" (HasSuffix on the
// dotted suffix rejects the bare parent and any host that merely ends in the same
// characters without the dot boundary).
func (g hostGlob) match(host string) bool {
	switch {
	case g.global:
		return true
	case g.suffix != "":
		return strings.HasSuffix(host, g.suffix) && len(host) > len(g.suffix)
	default:
		return host == g.pattern
	}
}

// policy is the per-session deny-wins glob allowlist. allow(host) returns true
// ONLY when no deny glob matches AND some allow glob matches (baseline-deny, D-09).
type policy struct {
	allowSet []hostGlob
	denySet  []hostGlob
}

// buildPolicy compiles allow/deny CSV-derived host lists into a policy. It rejects
// a GLOBAL "*" in the deny list (ErrGlobalDenyWildcard) — that would deny all
// egress and silently neuter the allowlist (Codex footgun). A global "*" IS
// permitted in the allow list (explicit operator allow-all).
func buildPolicy(allow, deny []string) (*policy, error) {
	p := &policy{}
	for _, a := range allow {
		if g := parseGlob(a); g.pattern != "" {
			p.allowSet = append(p.allowSet, g)
		}
	}
	for _, d := range deny {
		g := parseGlob(d)
		if g.pattern == "" {
			continue
		}
		if g.global {
			return nil, ErrGlobalDenyWildcard
		}
		p.denySet = append(p.denySet, g)
	}
	return p, nil
}

// allow is the deny-wins gate: a matching deny glob ALWAYS wins; otherwise the
// host must match an allow glob (baseline deny). Modeled on Codex
// network_policy.rs (denied beats allowed; baseline-deny for not-allowed).
func (p *policy) allow(host string) bool {
	h := strings.ToLower(host)
	for _, g := range p.denySet {
		if g.match(h) {
			return false
		}
	}
	for _, g := range p.allowSet {
		if g.match(h) {
			return true
		}
	}
	return false
}

// parseAllowHosts splits the AURA_SANDBOX_NETWORK_ALLOW_HOSTS CSV into trimmed,
// non-empty hostnames at the proxy boundary. An empty/whitespace CSV yields nil —
// the 2a egressless default (no allowlist ⇒ no egress).
func parseAllowHosts(csv string) []string {
	var out []string
	for _, part := range strings.Split(csv, ",") {
		if h := strings.TrimSpace(part); h != "" {
			out = append(out, h)
		}
	}
	return out
}

// resolvePinner is the resolve-then-pin seam the CONNECT handler dials through. In
// production it is a *web.DialGuard (web.NewDialGuard), which delegates to the
// Slice-5 validateAndPin: hostname blocklist → per-(conv,host) DNS pin → resolve +
// classify-every-record + fail-closed-on-any-blocked → pin. The signature mirrors
// web.DialGuard.ResolveAndPin EXACTLY so *web.DialGuard satisfies it with no
// adapter. The unit tier injects a deterministic stub so no live DNS is touched;
// defining the seam here (not taking *web.DialGuard directly) keeps the unit tier
// network-free WITHOUT copying the IP-class table — production wires the web
// export (OQ2/A4).
type resolvePinner interface {
	ResolveAndPin(ctx context.Context, convID, host string) (netip.Addr, string)
}

// newWebGuard builds the production resolve-then-pin guard over the internal/web
// export: a fresh per-process DNS pin cache (dnsPinTTLSec mirrors
// AURA_WEB_DNS_PIN_TTL_SEC) wrapping web.ClassifyIP via web.NewDialGuard. The
// proxy owns one guard; the pin cache keys by (conversation,host). This is the
// ONLY production resolve-then-pin path: web.NewDialGuard / web.ResolveAndPin (no
// copy of web.ClassifyIP / classify).
func newWebGuard(dnsPinTTLSec int) resolvePinner {
	return web.NewDialGuard(dnsPinTTLSec)
}

// Proxy is the host-side CONNECT forward proxy for one session's egress. It binds
// a loopback host port and tunnels only allowlisted hostnames that resolve to
// public IPs. The policy is keyed by conversation_id (convID) so the DNS pin cache
// scopes per conversation (D-25 isolation).
type Proxy struct {
	srv      *http.Server
	ln       net.Listener
	pol      *policy
	guard    resolvePinner
	convID   string
	listenAt string
}

// NewProxy builds a session proxy that will listen at addr (a loopback host
// address such as the bridge-gateway IP:port; the 08-08 wiring picks the address).
// guard is the resolve-then-pin guard. The serve loop binds at Serve time. Tests
// inject a stub guard; production callers use NewSessionProxy.
func NewProxy(addr string, pol *policy, guard resolvePinner, convID string) *Proxy {
	return &Proxy{pol: pol, guard: guard, convID: convID, listenAt: addr}
}

// NewSessionProxy is the production constructor the SessionManager wiring (08-08)
// calls: it parses the AURA_SANDBOX_NETWORK_ALLOW_HOSTS CSV at this boundary,
// compiles the deny-wins policy (rejecting a global-* deny), and builds the
// resolve-then-pin guard over the internal/web export (newWebGuard →
// web.NewDialGuard). An empty allowlist yields a baseline-deny policy — the 2a
// egressless posture. denyHosts is the optional deny CSV (usually empty). The
// returned proxy is bound + run by Serve; addr is the bridge-gateway IP:port.
func NewSessionProxy(addr, allowHosts, denyHosts, convID string, dnsPinTTLSec int) (*Proxy, error) {
	pol, err := buildPolicy(parseAllowHosts(allowHosts), parseAllowHosts(denyHosts))
	if err != nil {
		return nil, err
	}
	return NewProxy(addr, pol, newWebGuard(dnsPinTTLSec), convID), nil
}

// newProxyWithListener wires a Proxy onto an already-open listener (the unit tier
// uses an ephemeral 127.0.0.1:0 listener; production binds via Serve). It shares
// the handler with NewProxy so both paths exercise the same CONNECT logic.
func newProxyWithListener(ln net.Listener, pol *policy, guard resolvePinner, convID string) *Proxy {
	return &Proxy{ln: ln, pol: pol, guard: guard, convID: convID, listenAt: ln.Addr().String()}
}

// Serve binds (if not already bound) and runs the CONNECT proxy until ctx is
// cancelled, then shuts the server down. A nil-after-Close listener is fine —
// Shutdown drains in-flight tunnels. Returns context.Canceled on normal ctx-driven
// teardown (the caller treats that as a clean stop).
func (p *Proxy) Serve(ctx context.Context) error {
	if p.ln == nil {
		ln, err := net.Listen("tcp", p.listenAt)
		if err != nil {
			return fmt.Errorf("proxy listen %s: %w", p.listenAt, err)
		}
		p.ln = ln
	}
	p.srv = &http.Server{
		Handler:     http.HandlerFunc(p.handleConnect),
		ConnContext: func(c context.Context, _ net.Conn) context.Context { return c },
	}
	errCh := make(chan error, 1)
	go func() { errCh <- p.srv.Serve(p.ln) }()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = p.srv.Shutdown(shutCtx)
		return ctx.Err()
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// handleConnect is the CONNECT entrypoint. It validates the target hostname only
// (NO MITM — never reads/forwards a body) and tunnels opaque bytes: (1) reject any
// non-CONNECT method or malformed host:port; (2) deny-wins glob allow check; (3)
// resolve-then-pin via the web export, fail-closed on any blocked record; (4)
// Hijack + dial the pinned IP + io.Copy both ways.
func (p *Proxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodConnect {
		http.Error(w, "only CONNECT is proxied", http.StatusMethodNotAllowed)
		return
	}
	host, port, err := net.SplitHostPort(r.Host)
	if err != nil || host == "" || port == "" {
		http.Error(w, "malformed CONNECT target", http.StatusBadRequest)
		return
	}
	if !p.pol.allow(host) {
		http.Error(w, "host not allowlisted", http.StatusForbidden)
		return
	}
	pinned, reason := p.guard.ResolveAndPin(r.Context(), p.convID, host)
	if reason != "" || !pinned.IsValid() {
		http.Error(w, "host blocked: "+reason, http.StatusForbidden)
		return
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking unsupported", http.StatusInternalServerError)
		return
	}
	clientConn, _, err := hj.Hijack()
	if err != nil {
		http.Error(w, "hijack failed", http.StatusInternalServerError)
		return
	}
	defer clientConn.Close()

	upstream, err := (&net.Dialer{Timeout: connectDialTimeout}).DialContext(
		r.Context(), "tcp", net.JoinHostPort(pinned.String(), port))
	if err != nil {
		_, _ = clientConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return
	}
	defer upstream.Close()

	if _, err := clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		return
	}
	tunnel(r.Context(), clientConn, upstream)
}

// tunnel pumps bytes both ways until either side closes or ctx cancels. A watcher
// goroutine closes both conns on ctx cancellation so a hung tunnel cannot outlive
// the proxy (goleak-clean). The two io.Copy directions race to finish; the first
// EOF/close unblocks the other via the deferred Close above.
func tunnel(ctx context.Context, client, upstream net.Conn) {
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = client.Close()
			_ = upstream.Close()
		case <-done:
		}
	}()
	copyDone := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(upstream, client); copyDone <- struct{}{} }()
	go func() { _, _ = io.Copy(client, upstream); copyDone <- struct{}{} }()
	<-copyDone // first direction to finish closes its conn (deferred) → unblocks the other
}
