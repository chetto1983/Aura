package mcp

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"
)

var errRecordingDial = errors.New("recording dial reached")

// recordingDial captures the addr the hardened transport dials so a test can assert
// the transport connected to the PINNED IP literal, never the hostname.
type recordingDial struct{ addr string }

func (r *recordingDial) dial(_ context.Context, _, addr string) (net.Conn, error) {
	r.addr = addr
	return nil, errRecordingDial
}

// TestHardenedDialContextRebindFailClosed proves the rebind shape (a resolver that
// returns a public AND a private record) fails closed before any connect.
func TestHardenedDialContextRebindFailClosed(t *testing.T) {
	rec := &recordingDial{}
	res := fakeResolver{ips: map[string][]netip.Addr{
		"evil.test": mustAddrs(t, "93.184.216.34", "10.0.0.5"),
	}}
	hd := newHardenedDialer(res, rec.dial)
	if _, err := hd.dialContext(context.Background(), "tcp", "evil.test:443"); err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("dialContext = %v, want blocked error", err)
	}
	if rec.addr != "" {
		t.Fatalf("inner dial reached %q, want no dial on a blocked rebind", rec.addr)
	}
}

// TestHardenedDialContextDialsPinnedIP proves a clean public host is dialed by its
// PINNED IP literal (no second lookup can rebind it).
func TestHardenedDialContextDialsPinnedIP(t *testing.T) {
	rec := &recordingDial{}
	res := fakeResolver{ips: map[string][]netip.Addr{
		"good.test": mustAddrs(t, "93.184.216.34"),
	}}
	hd := newHardenedDialer(res, rec.dial)
	_, err := hd.dialContext(context.Background(), "tcp", "good.test:443")
	if !errors.Is(err, errRecordingDial) {
		t.Fatalf("dialContext err = %v, want recording sentinel", err)
	}
	if rec.addr != "93.184.216.34:443" {
		t.Fatalf("dialed %q, want pinned 93.184.216.34:443", rec.addr)
	}
}

// TestHardenedControlRejectsRebind exercises the net.Dialer.Control hook directly:
// it re-classifies the post-resolution IP, rejecting a private/metadata rebind even
// on a dial-by-name path (defense-in-depth), and passing a clean public IP.
func TestHardenedControlRejectsRebind(t *testing.T) {
	hd := newHardenedDialer(fakeResolver{}, nil)
	cases := []struct {
		name    string
		address string
		wantErr bool
	}{
		{"public_ok", "93.184.216.34:443", false},
		{"private_rebind", "10.0.0.5:443", true},
		{"imds_rebind", "169.254.169.254:80", true},
		{"loopback_rebind", "127.0.0.1:8091", true},
		{"unparseable", "not-an-ip", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := hd.control("tcp", tc.address, nil)
			if tc.wantErr != (err != nil) {
				t.Fatalf("control(%q) err = %v, wantErr=%v", tc.address, err, tc.wantErr)
			}
		})
	}
}

func TestHardenedDialerAllowsExactTrustedPrivateAuthority(t *testing.T) {
	rec := &recordingDial{}
	server := ManagedServer{
		Type:   ServerTypeStreamableHTTP,
		URL:    "http://sidecar.test:8080/mcp/",
		Source: "recipe:whatsapp",
		Trust:  ManagedTrust{Class: TrustTrustedRecipe},
	}
	res := fakeResolver{ips: map[string][]netip.Addr{
		"sidecar.test": mustAddrs(t, "172.18.0.5"),
	}}
	hd := newHardenedDialerWithPolicy(res, rec.dial, EgressPolicyForManagedServer(true, server))
	_, err := hd.dialContext(context.Background(), "tcp", "sidecar.test:8080")
	if !errors.Is(err, errRecordingDial) {
		t.Fatalf("exact trusted private dial error = %v, want recording sentinel", err)
	}
	if rec.addr != "172.18.0.5:8080" {
		t.Fatalf("dialed %q, want pinned recipe destination", rec.addr)
	}

	rec.addr = ""
	if _, err := hd.dialContext(context.Background(), "tcp", "sidecar.test:8081"); err == nil {
		t.Fatal("alternate private port reached inner dial")
	}
	if rec.addr != "" {
		t.Fatalf("alternate private port dialed %q", rec.addr)
	}
}

// newHardenedDialer composes the hardened dialer. dial may be nil — a real net.Dialer
// whose Control hook re-checks the post-resolution IP is used then.
func newHardenedDialer(res resolver, dial dialFunc) *hardenedDialer {
	return newHardenedDialerWithPolicy(res, dial, EgressPolicy{enforcePrivate: true})
}
