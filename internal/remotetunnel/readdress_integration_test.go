//go:build db_integration

package remotetunnel

import (
	"context"
	"slices"
	"testing"

	"github.com/chetto1983/aura/internal/cloudflareapi"
)

// movedAccountCloud sees both accounts, keeps the old tunnel until it is deleted, and parks the
// new account's zone at the nameserver wait so the pass ends after the teardown.
type movedAccountCloud struct {
	Cloudflare
	deleted    bool
	deletedIn  []string
	zonesAsked []string
}

func (*movedAccountCloud) VerifyToken(context.Context) (cloudflareapi.TokenVerification, error) {
	return cloudflareapi.TokenVerification{}, nil
}

func (*movedAccountCloud) ListAccounts(context.Context) ([]cloudflareapi.Account, error) {
	return []cloudflareapi.Account{{ID: "account-a"}, {ID: "account-b"}}, nil
}

func (c *movedAccountCloud) GetTunnel(_ context.Context, _, id string) (cloudflareapi.Tunnel, error) {
	if c.deleted {
		return cloudflareapi.Tunnel{}, &cloudflareapi.APIError{Status: 404}
	}
	return cloudflareapi.Tunnel{ID: id, Name: "aura-00000000-0000-4000-8000-000000000001", ConfigSource: "cloudflare", Status: "inactive"}, nil
}

func (c *movedAccountCloud) DeleteTunnel(_ context.Context, account, _ string) error {
	c.deleted = true
	c.deletedIn = append(c.deletedIn, account)
	return nil
}

func (c *movedAccountCloud) ListZones(_ context.Context, account, name string) ([]cloudflareapi.Zone, error) {
	c.zonesAsked = append(c.zonesAsked, account)
	return []cloudflareapi.Zone{{ID: "zone-b", Name: name, Status: "pending", Account: cloudflareapi.Account{ID: account}}}, nil
}

func TestAccountChangeAgainstTheRealStore(t *testing.T) {
	store := NewStore(remoteAccessPool(t))
	state := blankRemoteAccess(t, store)
	state, err := store.SaveDesired(t.Context(), state.Generation, Desired{Enabled: true, AccountID: "account-a", ZoneName: "example.com", PublicLabel: "aura"}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	state.Resources = Resources{AccountID: "account-a", ZoneID: "zone-a", OTPProviderID: "otp-a", TunnelID: "tunnel"}
	state.OwnedZoneName, state.OwnedHostname = "example.com", "aura.example.com"
	state.TunnelName = "aura-00000000-0000-4000-8000-000000000001"
	state.Phase = PhaseConnecting
	if state, err = store.Advance(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	if _, err = store.SaveDesired(t.Context(), state.Generation, Desired{Enabled: true, AccountID: "account-b", ZoneName: "example.com", PublicLabel: "aura"}, "admin"); err != nil {
		t.Fatal(err)
	}
	cloud := &movedAccountCloud{}
	credentials := &testCredentials{}
	engine := New(store, cloud, testMembers{emails: []string{"a@example.com"}, admins: []string{"a@example.com"}}, &testProjection{t: t}, nil, WithCredentials(credentials))
	if err = engine.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cloud.deletedIn, []string{"account-a"}) {
		t.Fatalf("deleted in %v, want the old account only", cloud.deletedIn)
	}
	if !slices.Equal(cloud.zonesAsked, []string{"account-b"}) || !slices.Equal(credentials.saves, []cloudflareapi.Secret{""}) {
		t.Fatalf("zones asked in %v, credential saves %q", cloud.zonesAsked, credentials.saves)
	}
	got, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got.Resources.AccountID != "account-b" || got.Resources.ZoneID != "zone-b" || got.Resources.OTPProviderID != "" || got.Resources.TunnelID != "" {
		t.Fatalf("resources after the move: %+v", got.Resources)
	}
	if got.OwnedZoneName != "example.com" || got.OwnedHostname != "aura.example.com" || got.Phase != PhaseWaitingNameservers {
		t.Fatalf("owned=%q/%q phase=%q", got.OwnedZoneName, got.OwnedHostname, got.Phase)
	}
}
