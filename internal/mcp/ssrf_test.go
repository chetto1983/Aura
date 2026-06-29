package mcp

import (
	"context"
	"errors"
	"net/netip"
	"testing"
)

// fakeResolver is the injected DNS seam for the guard tests: no real network, so
// the table rows always run (no env, no skip-as-green). A non-nil err makes every
// lookup fail; otherwise host→addrs comes from ips.
type fakeResolver struct {
	ips map[string][]netip.Addr
	err error
}

func (f fakeResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.ips[host], nil
}

func mustAddrs(t *testing.T, ss ...string) []netip.Addr {
	t.Helper()
	out := make([]netip.Addr, 0, len(ss))
	for _, s := range ss {
		a, err := netip.ParseAddr(s)
		if err != nil {
			t.Fatalf("parse addr %q: %v", s, err)
		}
		out = append(out, a)
	}
	return out
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name    string
		addr    string // empty → zero (invalid) Addr
		reason  string
		blocked bool
	}{
		{"public_v4", "93.184.216.34", "", false},
		{"public_v6", "2606:2800:220:1:248:1893:25c8:1946", "", false},
		{"invalid_zero", "", "invalid_target", true},
		{"loopback_v4", "127.0.0.1", "loopback", true},
		{"loopback_v6", "::1", "loopback", true},
		{"link_local_imds", "169.254.169.254", "link_local", true},
		{"link_local_unicast_v6", "fe80::1", "link_local", true},
		{"mapped_imds_unmap_first", "::ffff:169.254.169.254", "link_local", true},
		{"private_10", "10.0.0.5", "private", true},
		{"private_172", "172.16.3.4", "private", true},
		{"private_192", "192.168.1.10", "private", true},
		// fd00:ec2::/32 is the AWS IPv6 metadata range but is shadowed by the
		// RFC-4193 IsPrivate() (fc00::/7) predicate that runs earlier — mirrored
		// faithfully from internal/web; it is still blocked, just as "private".
		{"ipv6_metadata_range_as_private", "fd00:ec2::254", "private", true},
		// 224.0.0.0/24 is link-local multicast (hits the earlier branch); use an
		// administratively-scoped multicast address to reach the plain multicast class.
		{"link_local_multicast", "224.0.0.1", "link_local", true},
		{"multicast", "239.0.0.1", "multicast", true},
		{"unspecified", "0.0.0.0", "unspecified", true},
		{"cgnat", "100.64.0.1", "cgnat", true},
		{"this_network", "0.1.2.3", "this_network", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ip netip.Addr
			if tc.addr != "" {
				ip = mustAddrs(t, tc.addr)[0]
			}
			reason, blocked := classify(ip)
			if blocked != tc.blocked || reason != tc.reason {
				t.Fatalf("classify(%s) = (%q,%v), want (%q,%v)", tc.addr, reason, blocked, tc.reason, tc.blocked)
			}
		})
	}
}

func TestGuardEndpoint(t *testing.T) {
	imds := "169.254.169.254"
	cases := []struct {
		name       string
		raw        string
		enforce    bool
		allowHosts map[string]struct{}
		resolve    map[string][]netip.Addr
		resolveErr bool
		wantErr    bool
		wantHost   string
	}{
		// Unconditional barriers (break the CodeQL taint flow on EVERY policy branch).
		{name: "parse_error", raw: "http://[::1", wantErr: true},
		{name: "empty_host", raw: "http:///mcp", wantErr: true},
		{name: "non_http_scheme_dev", raw: "ftp://example.test/x", enforce: false, wantErr: true},
		{name: "non_http_scheme_enforce", raw: "gopher://example.test/x", enforce: true, wantErr: true},
		{name: "metadata_host_dev", raw: "http://metadata.google.internal/x", enforce: false, wantErr: true},
		{name: "metadata_host_enforce", raw: "http://metadata.google.internal/x", enforce: true, wantErr: true},
		{name: "metadata_host_uppercase", raw: "http://Metadata.Google.Internal/x", enforce: false, wantErr: true},
		{
			name: "imds_ip_dev", raw: "http://imds.test/x", enforce: false,
			resolve: map[string][]netip.Addr{"imds.test": mustAddrs(t, imds)}, wantErr: true,
		},
		{
			name: "imds_ip_enforce", raw: "http://imds.test/x", enforce: true,
			resolve: map[string][]netip.Addr{"imds.test": mustAddrs(t, imds)}, wantErr: true,
		},
		{
			name: "mapped_imds_dev_unmap", raw: "http://mapped.test/x", enforce: false,
			resolve: map[string][]netip.Addr{"mapped.test": mustAddrs(t, "::ffff:169.254.169.254")}, wantErr: true,
		},
		// Allowed public host — passes under BOTH policies.
		{
			name: "public_dev", raw: "http://example.test/mcp", enforce: false,
			resolve:  map[string][]netip.Addr{"example.test": mustAddrs(t, "93.184.216.34")},
			wantHost: "example.test",
		},
		{
			name: "public_enforce", raw: "https://example.test/mcp", enforce: true,
			resolve:  map[string][]netip.Addr{"example.test": mustAddrs(t, "93.184.216.34")},
			wantHost: "example.test",
		},
		// Loopback: ALLOWED under dev (httptest + off-container sidecars), BLOCKED under enforce.
		{
			name: "loopback_dev_allowed", raw: "http://127.0.0.1:8091/mcp", enforce: false,
			resolve:  map[string][]netip.Addr{"127.0.0.1": mustAddrs(t, "127.0.0.1")},
			wantHost: "127.0.0.1",
		},
		{
			name: "loopback_enforce_blocked", raw: "http://127.0.0.1:8091/mcp", enforce: true,
			resolve: map[string][]netip.Addr{"127.0.0.1": mustAddrs(t, "127.0.0.1")}, wantErr: true,
		},
		{
			name: "loopback_enforce_allowlisted", raw: "http://127.0.0.1:8091/mcp", enforce: true,
			allowHosts: map[string]struct{}{"127.0.0.1": {}},
			resolve:    map[string][]netip.Addr{"127.0.0.1": mustAddrs(t, "127.0.0.1")},
			wantHost:   "127.0.0.1",
		},
		// Private (docker compose-DNS sidecar): ALLOWED under dev, BLOCKED under enforce.
		{
			name: "private_dev_allowed", raw: "http://aura-agent-memory-mcp:8080/mcp", enforce: false,
			resolve:  map[string][]netip.Addr{"aura-agent-memory-mcp": mustAddrs(t, "172.18.0.5")},
			wantHost: "aura-agent-memory-mcp",
		},
		{
			name: "private_enforce_blocked", raw: "http://aura-agent-memory-mcp:8080/mcp", enforce: true,
			resolve: map[string][]netip.Addr{"aura-agent-memory-mcp": mustAddrs(t, "172.18.0.5")}, wantErr: true,
		},
		{
			name: "private_enforce_allowlisted", raw: "http://aura-agent-memory-mcp:8080/mcp", enforce: true,
			allowHosts: map[string]struct{}{"aura-agent-memory-mcp": {}},
			resolve:    map[string][]netip.Addr{"aura-agent-memory-mcp": mustAddrs(t, "172.18.0.5")},
			wantHost:   "aura-agent-memory-mcp",
		},
		// Mixed record sets.
		{
			name: "mixed_public_private_enforce_failclosed", raw: "http://mixed.test/x", enforce: true,
			resolve: map[string][]netip.Addr{"mixed.test": mustAddrs(t, "93.184.216.34", "10.0.0.5")}, wantErr: true,
		},
		{
			name: "mixed_public_loopback_dev_allowed", raw: "http://mix2.test/x", enforce: false,
			resolve:  map[string][]netip.Addr{"mix2.test": mustAddrs(t, "93.184.216.34", "127.0.0.1")},
			wantHost: "mix2.test",
		},
		{
			name: "mixed_public_imds_dev_blocked", raw: "http://mix3.test/x", enforce: false,
			resolve: map[string][]netip.Addr{"mix3.test": mustAddrs(t, "93.184.216.34", "169.254.169.254")}, wantErr: true,
		},
		// Resolution failure fails closed.
		{
			name: "resolve_failure", raw: "http://broken.test/x", enforce: false,
			resolveErr: true, wantErr: true,
		},
		{
			name: "resolve_empty", raw: "http://nohost.test/x", enforce: false,
			resolve: map[string][]netip.Addr{}, wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := fakeResolver{ips: tc.resolve}
			if tc.resolveErr {
				res.err = errors.New("dns boom")
			}
			u, err := guardEndpoint(context.Background(), tc.raw, tc.enforce, tc.allowHosts, res)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("guardEndpoint(%q, enforce=%v) = nil err, want error", tc.raw, tc.enforce)
				}
				return
			}
			if err != nil {
				t.Fatalf("guardEndpoint(%q, enforce=%v) unexpected error: %v", tc.raw, tc.enforce, err)
			}
			if u == nil {
				t.Fatalf("guardEndpoint(%q) returned nil url with nil error", tc.raw)
			}
			if u.Hostname() != tc.wantHost {
				t.Fatalf("guardEndpoint(%q) host = %q, want %q", tc.raw, u.Hostname(), tc.wantHost)
			}
		})
	}
}
