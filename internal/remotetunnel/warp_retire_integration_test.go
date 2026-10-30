//go:build db_integration

package remotetunnel

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/cloudflareapi"
)

// foreignWARPApp answers the one read a retirement of a lone WARP application makes.
type foreignWARPApp struct{ Cloudflare }

func (foreignWARPApp) GetApplication(context.Context, string, string) (cloudflareapi.AccessApplication, error) {
	return cloudflareapi.AccessApplication{ID: "warp-app", Name: "someone-else", Type: "self_hosted"}, nil
}

// SaveCloudflareRemoteAccessDesired reads error + disabled + retained references as deletion
// intent, and warp_app_id is one of those references: a retirement that wrote error on a disabled
// install would turn the operator's next "enable" into a deletion of the public address.
func TestDisabledRetirementFailureNeverBecomesADeletion(t *testing.T) {
	store := NewStore(remoteAccessPool(t))
	state := blankRemoteAccess(t, store)
	state, err := store.SaveDesired(t.Context(), state.Generation, Desired{AccountID: "account", ZoneName: "example.com", PublicLabel: "aura"}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	state.Resources = Resources{AccountID: "account", ZoneID: "zone", TunnelID: "tunnel", PublicDNSID: "pd", OTPProviderID: "otp", PublicAppID: "pa", PublicPolicyID: "pp", WARPAppID: "warp-app"}
	state.TunnelName = "aura-00000000-0000-4000-8000-000000000001"
	state.Phase = PhaseDisabled
	if state, err = store.Advance(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	public := state.Resources
	public.WARPAppID, public.LegacyWARPHostname = "", ""

	engine := New(store, foreignWARPApp{}, nil, &testProjection{t: t}, nil)
	if err = engine.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	held, err := store.Load(t.Context())
	if err != nil || held.Phase != PhaseDisabled || held.Reason != "" || held.Resources.WARPAppID == "" {
		t.Fatalf("a refused retirement changed the row: %+v err=%v", held, err)
	}
	enabled, err := store.SaveDesired(t.Context(), held.Generation, Desired{Enabled: true, AccountID: "account", ZoneName: "example.com", PublicLabel: "aura"}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	enabled.Resources.WARPAppID, enabled.Resources.LegacyWARPHostname = "", ""
	if enabled.Phase != PhaseValidating || !enabled.Desired.Enabled || enabled.Resources != public {
		t.Fatalf("enable became %s (enabled=%v), resources %+v", enabled.Phase, enabled.Desired.Enabled, enabled.Resources)
	}
}
