//go:build db_integration

package remotetunnel

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/cloudflareapi"
)

// pendingZoneCloud lets an enabled pass on a fresh install get past account() and park at the
// nameserver wait, where the account it resolved must already be on the row.
type pendingZoneCloud struct{ Cloudflare }

func (pendingZoneCloud) VerifyToken(context.Context) (cloudflareapi.TokenVerification, error) {
	return cloudflareapi.TokenVerification{}, nil
}

func (pendingZoneCloud) ListAccounts(context.Context) ([]cloudflareapi.Account, error) {
	return []cloudflareapi.Account{{ID: "account"}}, nil
}

func (pendingZoneCloud) ListZones(context.Context, string, string) ([]cloudflareapi.Zone, error) {
	return []cloudflareapi.Zone{{ID: "zone", Name: "example.com", Status: "pending", Account: cloudflareapi.Account{ID: "account"}}}, nil
}

func TestDeleteOnARowWithNoResourcesReachesDisabled(t *testing.T) {
	store := NewStore(remoteAccessPool(t))
	state := blankRemoteAccess(t, store)
	if _, err := store.SaveDesired(t.Context(), state.Generation, Desired{AccountID: "account", ZoneName: "example.com", PublicLabel: "aura"}, "admin"); err != nil {
		t.Fatal(err)
	}
	engine := New(store, pendingZoneCloud{}, nil, &testProjection{t: t}, nil)
	if err := engine.Delete(t.Context(), "admin"); err != nil {
		t.Fatalf("deleting a row that holds nothing failed: %v", err)
	}
	got, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != PhaseDisabled || got.Reason != "" {
		t.Fatalf("phase=%q reason=%q, want disabled with no reason", got.Phase, got.Reason)
	}
}

func TestEnabledReconcilePersistsTheResourceAccount(t *testing.T) {
	store := NewStore(remoteAccessPool(t))
	state := blankRemoteAccess(t, store)
	if _, err := store.SaveDesired(t.Context(), state.Generation, Desired{Enabled: true, AccountID: "account", ZoneName: "example.com", PublicLabel: "aura"}, "admin"); err != nil {
		t.Fatal(err)
	}
	engine := New(store, pendingZoneCloud{}, testMembers{emails: []string{"a@example.com"}, admins: []string{"a@example.com"}}, &testProjection{t: t}, nil, WithCredentials(&testCredentials{}))
	if err := engine.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != PhaseWaitingNameservers || got.Resources.AccountID != "account" {
		t.Fatalf("phase=%q resources account=%q, want waiting_nameservers and account", got.Phase, got.Resources.AccountID)
	}
}
