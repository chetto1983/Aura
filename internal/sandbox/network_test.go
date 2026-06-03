//go:build !sandbox_integration

package sandbox

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"
)

// stubResolvePinner is the deterministic resolve-then-pin seam injected into the
// proxy so the SSRF gate is exercised without live DNS. In production the proxy
// holds a *web.DialGuard (which delegates to the Slice-5 validateAndPin); here a
// stub maps host→(pinned IP, block reason) so the unit tier never touches the
// network. A non-empty reason means "blocked" exactly as the export contracts.
type stubResolvePinner struct {
	pins    map[string]netip.Addr
	reasons map[string]string
}

func (s stubResolvePinner) ResolveAndPin(_ context.Context, _, host string) (netip.Addr, string) {
	if r, ok := s.reasons[host]; ok && r != "" {
		return netip.Addr{}, r
	}
	if ip, ok := s.pins[host]; ok {
		return ip, ""
	}
	return netip.Addr{}, "no_record"
}

// TestProxy_AllowlistGlobAndSSRF is the CAP-02 unit gate: deny-wins glob
// precedence, the "*.x" does-NOT-match-parent rule, global-* rejection at build,
// resolve-then-pin SSRF reject, and malformed-CONNECT reject — all without live
// network. It drives the whole proxy through an in-process listener.
func TestProxy_AllowlistGlobAndSSRF(t *testing.T) {
	t.Parallel()

	t.Run("policy glob precedence (deny wins, *.x not parent)", func(t *testing.T) {
		t.Parallel()
		pol, err := buildPolicy(
			[]string{"pypi.org", "*.pythonhosted.org", "files.pythonhosted.org"},
			[]string{"files.pythonhosted.org"},
		)
		if err != nil {
			t.Fatalf("buildPolicy: %v", err)
		}
		cases := []struct {
			host string
			want bool
		}{
			{"pypi.org", true},                           // exact allow
			{"a.pythonhosted.org", true},                 // *.pythonhosted.org allow
			{"files.pythonhosted.org", false},            // matched by allow AND deny → deny wins
			{"pythonhosted.org", false},                  // *.x must NOT match the bare parent
			{"example.com", false},                       // not allowed → baseline deny
			{"1.1.1.1", false},                           // not allowed → baseline deny
			{"evil.pythonhosted.org.attacker.io", false}, // suffix trick must not pass
		}
		for _, c := range cases {
			if got := pol.allow(c.host); got != c.want {
				t.Errorf("allow(%q) = %v, want %v", c.host, got, c.want)
			}
		}
	})

	t.Run("global * in deny list rejected at build", func(t *testing.T) {
		t.Parallel()
		if _, err := buildPolicy([]string{"pypi.org"}, []string{"*"}); err == nil {
			t.Fatal("buildPolicy accepted a global * in the deny list (Codex footgun)")
		}
	})

	t.Run("global * allowed in allow list (allow-all is a legitimate operator choice)", func(t *testing.T) {
		t.Parallel()
		pol, err := buildPolicy([]string{"*"}, nil)
		if err != nil {
			t.Fatalf("buildPolicy allow-* rejected: %v", err)
		}
		if !pol.allow("anything.example.com") {
			t.Fatal("allow-* policy did not allow an arbitrary host")
		}
	})

	t.Run("CONNECT to allowlisted public host tunnels", func(t *testing.T) {
		t.Parallel()
		// Upstream echo server the proxy will tunnel to.
		upstream, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("upstream listen: %v", err)
		}
		defer upstream.Close()
		go func() {
			c, aerr := upstream.Accept()
			if aerr != nil {
				return
			}
			defer c.Close()
			_, _ = io.Copy(c, c) // echo
		}()
		upIP := upstream.Addr().(*net.TCPAddr)

		pol, err := buildPolicy([]string{"good.test"}, nil)
		if err != nil {
			t.Fatalf("buildPolicy: %v", err)
		}
		guard := stubResolvePinner{
			pins: map[string]netip.Addr{"good.test": upIP.AddrPort().Addr()},
		}
		px := newTestProxy(t, pol, guard, "conv-1")

		target := net.JoinHostPort("good.test", strconv.Itoa(upIP.Port))
		clientConn := dialProxyCONNECT(t, px.addr(), target)
		defer clientConn.Close()

		br := bufio.NewReader(clientConn)
		status, rerr := br.ReadString('\n')
		if rerr != nil {
			t.Fatalf("read CONNECT status: %v", rerr)
		}
		if !strings.Contains(status, "200") {
			t.Fatalf("CONNECT to allowlisted host failed: %q", status)
		}
		// Drain the rest of the response header block.
		for {
			line, lerr := br.ReadString('\n')
			if lerr != nil || strings.TrimSpace(line) == "" {
				break
			}
		}
		// Tunnel is opaque: write through, read echo.
		if _, werr := clientConn.Write([]byte("ping\n")); werr != nil {
			t.Fatalf("tunnel write: %v", werr)
		}
		_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
		got, gerr := br.ReadString('\n')
		if gerr != nil {
			t.Fatalf("tunnel read: %v", gerr)
		}
		if strings.TrimSpace(got) != "ping" {
			t.Fatalf("echo mismatch: got %q", got)
		}
	})

	t.Run("CONNECT to non-allowlisted host refused", func(t *testing.T) {
		t.Parallel()
		pol, err := buildPolicy([]string{"good.test"}, nil)
		if err != nil {
			t.Fatalf("buildPolicy: %v", err)
		}
		px := newTestProxy(t, pol, stubResolvePinner{}, "conv-1")
		status := connectStatus(t, px.addr(), "example.com:443")
		if strings.Contains(status, "200") {
			t.Fatalf("non-allowlisted host was tunneled: %q", status)
		}
		if !strings.Contains(status, "403") {
			t.Fatalf("expected 403 for non-allowlisted host, got %q", status)
		}
	})

	t.Run("CONNECT to allowlisted host resolving to private IP refused (SSRF)", func(t *testing.T) {
		t.Parallel()
		pol, err := buildPolicy([]string{"sneaky.test"}, nil)
		if err != nil {
			t.Fatalf("buildPolicy: %v", err)
		}
		guard := stubResolvePinner{reasons: map[string]string{"sneaky.test": "private"}}
		px := newTestProxy(t, pol, guard, "conv-1")
		status := connectStatus(t, px.addr(), "sneaky.test:443")
		if strings.Contains(status, "200") {
			t.Fatalf("host resolving to a private IP was tunneled: %q", status)
		}
		if !strings.Contains(status, "403") {
			t.Fatalf("expected 403 for SSRF-blocked host, got %q", status)
		}
	})

	t.Run("malformed CONNECT target refused", func(t *testing.T) {
		t.Parallel()
		pol, err := buildPolicy([]string{"good.test"}, nil)
		if err != nil {
			t.Fatalf("buildPolicy: %v", err)
		}
		px := newTestProxy(t, pol, stubResolvePinner{}, "conv-1")
		// No port → not a clean host:port.
		status := connectStatus(t, px.addr(), "good.test")
		if strings.Contains(status, "200") {
			t.Fatalf("malformed CONNECT (no port) was tunneled: %q", status)
		}
	})

	t.Run("allowlisted host with unreachable upstream returns 502", func(t *testing.T) {
		t.Parallel()
		// Allocate then immediately free a port so the dial is refused.
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		deadPort := l.Addr().(*net.TCPAddr).Port
		_ = l.Close()

		pol, err := buildPolicy([]string{"dead.test"}, nil)
		if err != nil {
			t.Fatalf("buildPolicy: %v", err)
		}
		guard := stubResolvePinner{pins: map[string]netip.Addr{"dead.test": netip.MustParseAddr("127.0.0.1")}}
		px := newTestProxy(t, pol, guard, "conv-1")
		status := connectStatus(t, px.addr(), net.JoinHostPort("dead.test", strconv.Itoa(deadPort)))
		// The CONNECT passes the allow + SSRF gates (200 would only come AFTER a
		// successful upstream dial); the refused dial yields 502 over the hijacked conn.
		if !strings.Contains(status, "502") {
			t.Fatalf("expected 502 for unreachable upstream, got %q", status)
		}
	})
}

// TestParseAllowHosts covers the CSV boundary parser: trims, drops empties, and an
// all-empty CSV yields no hosts (the 2a egressless default).
func TestParseAllowHosts(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"pypi.org", []string{"pypi.org"}},
		{" pypi.org , *.pythonhosted.org ,", []string{"pypi.org", "*.pythonhosted.org"}},
	}
	for _, c := range cases {
		got := parseAllowHosts(c.in)
		if len(got) != len(c.want) {
			t.Fatalf("parseAllowHosts(%q) = %v, want %v", c.in, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("parseAllowHosts(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

// TestNewSessionProxy covers the production constructor's CSV→policy→guard wiring
// at the boundary: a valid allowlist yields a usable proxy with the compiled
// deny-wins policy, and a global-* deny CSV propagates ErrGlobalDenyWildcard
// (fail-fast at construction, not at first CONNECT). It does NOT serve — the live
// resolve-then-pin path is 08-09.
func TestNewSessionProxy(t *testing.T) {
	t.Parallel()

	t.Run("valid CSV builds a deny-wins proxy", func(t *testing.T) {
		t.Parallel()
		px, err := NewSessionProxy("127.0.0.1:0", "pypi.org, *.pythonhosted.org",
			"files.pythonhosted.org", "conv-1", 60)
		if err != nil {
			t.Fatalf("NewSessionProxy: %v", err)
		}
		if px.guard == nil {
			t.Fatal("NewSessionProxy did not wire a resolve-then-pin guard")
		}
		if !px.pol.allow("pypi.org") || px.pol.allow("files.pythonhosted.org") {
			t.Fatal("compiled policy did not honor deny-wins precedence")
		}
	})

	t.Run("global-* deny CSV fails fast", func(t *testing.T) {
		t.Parallel()
		_, err := NewSessionProxy("127.0.0.1:0", "pypi.org", "*", "conv-1", 60)
		if !errors.Is(err, ErrGlobalDenyWildcard) {
			t.Fatalf("NewSessionProxy global-* deny: got %v, want ErrGlobalDenyWildcard", err)
		}
	})
}

// --- test helpers (no package-level TestMain here — docker_test.go owns it) ---

// testProxy wraps a Proxy serving on an ephemeral port for the unit tier, with a
// ctx-bound Serve goroutine torn down via t.Cleanup (goleak-clean).
type testProxy struct {
	p  *Proxy
	ln net.Listener
}

func (tp *testProxy) addr() string { return tp.ln.Addr().String() }

func newTestProxy(t *testing.T, pol *policy, guard resolvePinner, convID string) *testProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("proxy listen: %v", err)
	}
	p := newProxyWithListener(ln, pol, guard, convID)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if serr := p.Serve(ctx); serr != nil && !errors.Is(serr, context.Canceled) {
			t.Logf("Serve returned: %v", serr)
		}
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return &testProxy{p: p, ln: ln}
}

// dialProxyCONNECT opens a raw conn to the proxy and writes a CONNECT request for
// target, returning the still-open conn for tunnel I/O.
func dialProxyCONNECT(t *testing.T, proxyAddr, target string) net.Conn {
	t.Helper()
	c, err := net.DialTimeout("tcp", proxyAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	req := "CONNECT " + target + " HTTP/1.1\r\nHost: " + target + "\r\n\r\n"
	if _, err := c.Write([]byte(req)); err != nil {
		t.Fatalf("write CONNECT: %v", err)
	}
	return c
}

// connectStatus issues a CONNECT and returns the status line, closing the conn.
func connectStatus(t *testing.T, proxyAddr, target string) string {
	t.Helper()
	c := dialProxyCONNECT(t, proxyAddr, target)
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	line, err := bufio.NewReader(c).ReadString('\n')
	if err != nil && line == "" {
		t.Fatalf("read CONNECT status: %v", err)
	}
	return line
}
