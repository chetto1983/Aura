package remotetunnel

import (
	"github.com/chetto1983/aura/internal/cloudflareapi"
	"testing"
)

func TestOwnershipRequiresPersistedIDAndAuraMarkerAgreement(t *testing.T) {
	const owner = "aura-9f12fb90-379e-4707-8b49-ab798cb4b4f5"
	state := State{TunnelName: owner, Resources: Resources{TunnelID: "tunnel", GatewayPostureID: "gateway", PublicDNSID: "dns"}, Desired: Desired{PublicLabel: "aura", ZoneName: "example.com"}}
	tunnel := cloudflareapi.Tunnel{ID: "tunnel", Name: owner, RemoteConfig: true}
	posture := cloudflareapi.Posture{ID: "gateway", Name: owner + "-gateway", Type: "gateway"}
	dns := cloudflareapi.DNSRecord{ID: "dns", Name: "aura.example.com", Comment: owner, Type: "CNAME"}
	if !ownedTunnel(state, tunnel) || !ownedPosture(&state, posture) || !ownedDNS(&state, routes(&state)[0], dns) {
		t.Fatal("owned resource rejected")
	}
	for _, change := range []func(*cloudflareapi.Tunnel){func(v *cloudflareapi.Tunnel) { v.ID = "foreign" }, func(v *cloudflareapi.Tunnel) { v.Name = "foreign" }, func(v *cloudflareapi.Tunnel) { v.RemoteConfig = false }} {
		changed := tunnel
		change(&changed)
		if ownedTunnel(state, changed) {
			t.Fatal("foreign tunnel accepted")
		}
	}
	for _, change := range []func(*cloudflareapi.Posture){func(v *cloudflareapi.Posture) { v.ID = "foreign" }, func(v *cloudflareapi.Posture) { v.Name = "foreign" }, func(v *cloudflareapi.Posture) { v.Type = "warp" }} {
		changed := posture
		change(&changed)
		if ownedPosture(&state, changed) {
			t.Fatal("foreign posture accepted")
		}
	}
	for _, change := range []func(*cloudflareapi.DNSRecord){func(v *cloudflareapi.DNSRecord) { v.ID = "foreign" }, func(v *cloudflareapi.DNSRecord) { v.Name = "foreign.example.com" }, func(v *cloudflareapi.DNSRecord) { v.Comment = "foreign" }, func(v *cloudflareapi.DNSRecord) { v.Type = "A" }} {
		changed := dns
		change(&changed)
		if ownedDNS(&state, routes(&state)[0], changed) {
			t.Fatal("foreign DNS accepted")
		}
	}
	state.TunnelName = "foreign"
	tunnel.Name = "foreign"
	posture.Name = "foreign-gateway"
	dns.Comment = "foreign"
	if ownedTunnel(state, tunnel) || ownedPosture(&state, posture) || ownedDNS(&state, routes(&state)[0], dns) {
		t.Fatal("matching foreign namespace accepted")
	}
}

func TestOwnershipUsesAuthoritativeConfigSource(t *testing.T) {
	state := State{TunnelName: "aura-9f12fb90-379e-4707-8b49-ab798cb4b4f5", Resources: Resources{TunnelID: "owned"}}
	for _, tc := range []struct {
		source       string
		legacy, want bool
	}{{"cloudflare", false, true}, {"cloudflare", true, true}, {"local", true, false}, {"local", false, false}, {"", true, true}, {"", false, false}, {"unknown", true, false}} {
		tunnel := cloudflareapi.Tunnel{ID: "owned", Name: state.TunnelName, ConfigSource: tc.source, RemoteConfig: tc.legacy}
		if got := ownedTunnel(state, tunnel); got != tc.want {
			t.Fatalf("source=%q legacy=%v got=%v", tc.source, tc.legacy, got)
		}
	}
}

// Cloudflare's device-posture API does not carry a description: the field is accepted on
// create and is absent from every GET/LIST response (measured 2026-09-22 against a live
// gateway rule, whose body was exactly {id, type, name}). Ownership therefore has to rest on
// the name, which embeds the aura-<uuid> owner and which Cloudflare does return.
func TestGatewayPostureOwnedWithoutDescription(t *testing.T) {
	const owner = "aura-9f12fb90-379e-4707-8b49-ab798cb4b4f5"
	state := State{TunnelName: owner, Resources: Resources{GatewayPostureID: "gateway"}}
	if !ownedPosture(&state, cloudflareapi.Posture{ID: "gateway", Name: owner + "-gateway", Type: "gateway"}) {
		t.Fatal("posture as Cloudflare returns it was rejected as foreign")
	}
}
