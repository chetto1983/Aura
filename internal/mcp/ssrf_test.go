package mcp

import (
	"context"
	"errors"
	"net/netip"
	"net/url"
	"strings"
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
		// An IPv4 address carried in IPv6 is judged as itself (netguard): the NAT64 and
		// 6to4 forms of the IMDS address stay out even under dev, and a DNS64 answer for a
		// public host stays reachable under enforce.
		{
			name: "metadata_host_root_dot_dev", raw: "http://metadata.google.internal./x", enforce: false,
			wantErr: true,
		},
		{
			name: "nat64_imds_dev_blocked", raw: "http://nat64.test/x", enforce: false,
			resolve: map[string][]netip.Addr{"nat64.test": mustAddrs(t, "64:ff9b::a9fe:a9fe")}, wantErr: true,
		},
		{
			name: "sixtofour_imds_dev_blocked", raw: "http://sixtofour.test/x", enforce: false,
			resolve: map[string][]netip.Addr{"sixtofour.test": mustAddrs(t, "2002:a9fe:a9fe::1")}, wantErr: true,
		},
		{
			name: "dns64_public_enforce", raw: "https://dns64.test/mcp", enforce: true,
			resolve:  map[string][]netip.Addr{"dns64.test": mustAddrs(t, "64:ff9b::5db8:d822")},
			wantHost: "dns64.test",
		},
		{
			name: "reserved_enforce_blocked", raw: "http://testnet.test/x", enforce: true,
			resolve: map[string][]netip.Addr{"testnet.test": mustAddrs(t, "198.51.100.7")}, wantErr: true,
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
			name: "private_dev_allowed", raw: "http://aura-arcadedb-mcp:8096/mcp", enforce: false,
			resolve:  map[string][]netip.Addr{"aura-arcadedb-mcp": mustAddrs(t, "172.18.0.5")},
			wantHost: "aura-arcadedb-mcp",
		},
		{
			name: "private_enforce_blocked", raw: "http://aura-arcadedb-mcp:8096/mcp", enforce: true,
			resolve: map[string][]netip.Addr{"aura-arcadedb-mcp": mustAddrs(t, "172.18.0.5")}, wantErr: true,
		},
		{
			name: "private_enforce_allowlisted", raw: "http://aura-arcadedb-mcp:8096/mcp", enforce: true,
			allowHosts: map[string]struct{}{"aura-arcadedb-mcp": {}},
			resolve:    map[string][]netip.Addr{"aura-arcadedb-mcp": mustAddrs(t, "172.18.0.5")},
			wantHost:   "aura-arcadedb-mcp",
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

func TestStrictManagedEgressAllowsOnlyExactTrustedRecipeAuthority(t *testing.T) {
	server := ManagedServer{
		Type:   ServerTypeStreamableHTTP,
		URL:    "http://sidecar.test:8080/mcp/",
		Source: "recipe:calendar",
		Trust:  ManagedTrust{Class: TrustTrustedRecipe},
	}
	policy := EgressPolicyForManagedServer(true, server)
	res := fakeResolver{ips: map[string][]netip.Addr{
		"sidecar.test": mustAddrs(t, "172.18.0.5"),
	}}

	if _, err := guardEndpointWithPolicy(context.Background(), server.URL, policy, res); err != nil {
		t.Fatalf("exact trusted recipe authority rejected: %v", err)
	}
	if _, err := guardEndpointWithPolicy(context.Background(), "http://sidecar.test:8081/mcp/", policy, res); err == nil {
		t.Fatal("alternate private port inherited recipe authorization")
	}

	remote := server
	remote.Source = "operator:remote"
	remote.Trust = ManagedTrust{Class: TrustRemoteHTTP}
	if _, err := guardEndpointWithPolicy(
		context.Background(),
		remote.URL,
		EgressPolicyForManagedServer(true, remote),
		res,
	); err == nil {
		t.Fatal("remote_http received an implicit private destination allowance")
	}
}

func TestStrictManagedEgressRejectsMixedDNSAndMetadataEvenForRecipe(t *testing.T) {
	server := ManagedServer{
		Type:   ServerTypeStreamableHTTP,
		URL:    "http://sidecar.test:8080/mcp/",
		Source: SourceRecipeMemory,
		Trust:  ManagedTrust{Class: TrustTrustedRecipe},
	}
	policy := EgressPolicyForManagedServer(true, server)
	cases := map[string][]netip.Addr{
		"mixed public private": mustAddrs(t, "93.184.216.34", "172.18.0.5"),
		"ipv4 metadata":        mustAddrs(t, "169.254.169.254"),
		"ipv6 metadata":        mustAddrs(t, "fd00:ec2::254"),
	}
	for name, ips := range cases {
		t.Run(name, func(t *testing.T) {
			res := fakeResolver{ips: map[string][]netip.Addr{"sidecar.test": ips}}
			if _, err := guardEndpointWithPolicy(context.Background(), server.URL, policy, res); err == nil {
				t.Fatal("strict trusted recipe accepted an adversarial DNS result")
			}
		})
	}
}

// guardEndpoint validates a raw MCP endpoint URL and returns the parsed *url.URL or
// an error. Five steps, the first three UNCONDITIONAL (they break the CodeQL taint
// flow on every policy branch):
//
//  1. parse + non-empty host;
//  2. scheme ∈ allowedSchemes;
//  3. host is not a netguard metadata hostname;
//  4. resolve and classify EVERY record; strict mode rejects private targets except
//     the exact authority of a trusted built-in recipe, and rejects mixed DNS even
//     for that recipe; dev rejects metadata/link-local while allowing local sidecars;
//  5. return the validated URL.
func guardEndpoint(ctx context.Context, raw string, enforce bool, allowHosts map[string]struct{}, res resolver) (*url.URL, error) {
	policy := EgressPolicy{enforcePrivate: enforce}
	if enforce && len(allowHosts) > 0 {
		if u, err := url.Parse(strings.TrimSpace(raw)); err == nil {
			if _, ok := allowHosts[normalizeEgressHost(u.Hostname())]; ok {
				if authority, authorityErr := canonicalURLAuthority(u); authorityErr == nil {
					policy.allowedPrivateAuthorities = map[string]struct{}{authority: {}}
				}
			}
		}
	}
	return guardEndpointWithPolicy(ctx, raw, policy, res)
}
