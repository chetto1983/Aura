package agui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identitykey"
)

var routeRows = []sqlc.AuraSettings{
	{Key: "AURA_LLM_PROVIDER", Value: "openrouter"},
	{Key: "AURA_LLM_MODEL", Value: "z-ai/glm-5.3-flash"},
	{Key: "AURA_OPENROUTER_SERVICES_CAP_USD", Value: "20"},
}

func reconcileServer(rows []sqlc.AuraSettings, ids []identity.Identity, admins ...string) (*Server, *fakeMinting, *fakeIdentityKeys, *fakeSettingsStore, *fakeLLMRouteReloader) {
	minting, keys := &fakeMinting{keySet: true}, newFakeIdentityKeys()
	store, reloader := &fakeSettingsStore{rows: rows}, &fakeLLMRouteReloader{}
	caps := adminCaps(admins...)
	caps.identities = ids
	s := &Server{settings: store, llmRouteReloader: reloader, idAdmin: caps}
	s.SetOpenRouterKeys(NewIdentityKeyMinter(minting, keys, caps, billing))
	return s, minting, keys, store, reloader
}

func withServicesKey(rows []sqlc.AuraSettings) []sqlc.AuraSettings {
	return append(slices.Clone(rows), sqlc.AuraSettings{Key: "OPENROUTER_API_KEY", Value: "sk-or-v1-existing", IsSecret: true})
}

func TestReconcileMintsTheServicesKeyAndKeepsTheRoute(t *testing.T) {
	s, minting, _, store, reloader := reconcileServer(routeRows, nil)
	res, err := s.EnsureOpenRouterKeys(context.Background())
	if err != nil {
		t.Fatalf("EnsureOpenRouterKeys: %v", err)
	}
	if res.ServicesLabel != "sk-or-v1-...hash-1" {
		t.Fatalf("services label = %q, want the minted key's label", res.ServicesLabel)
	}
	if req := minting.minted[0]; req.Name != "aura-services" || req.Limit == nil || *req.Limit != 2000 {
		t.Fatalf("services mint = %+v, want aura-services with a 20.00 cap", req)
	}
	prepared := reloader.validated[0]
	if prepared["AURA_LLM_MODEL"] != "z-ai/glm-5.3-flash" || prepared["OPENROUTER_API_KEY"] != "sk-or-v1-hash-1" {
		t.Fatalf("prepared profile = %v, want the persisted route plus the new key", prepared)
	}
	if store.upserted["OPENROUTER_API_KEY"] != "sk-or-v1-hash-1" || len(reloader.applied) != 1 {
		t.Fatalf("stored = %v applied = %d, want the key stored and the profile published once", store.upserted, len(reloader.applied))
	}
}

func TestReconcileWaitsForTheServicesCap(t *testing.T) {
	s, minting, _, _, _ := reconcileServer(routeRows[:2], nil)
	res, err := s.EnsureOpenRouterKeys(context.Background())
	if !errors.Is(err, ErrServicesCapUnset) || len(res.Errors) != 1 || len(minting.minted) != 0 {
		t.Fatalf("result = %+v err = %v mints = %d; want ErrServicesCapUnset and no mint", res, err, len(minting.minted))
	}
}

func TestReconcileRevokesTheServicesKeyWhenTheProfileIsRejected(t *testing.T) {
	s, minting, _, store, reloader := reconcileServer(routeRows, nil)
	reloader.err = errors.New("model not found")
	if _, err := s.EnsureOpenRouterKeys(context.Background()); err == nil {
		t.Fatal("a rejected profile passed silently")
	}
	if len(minting.revoked) != 1 || minting.revoked[0] != "hash-1" {
		t.Fatalf("revoked = %v, want the unrecorded services key", minting.revoked)
	}
	if _, stored := store.upserted["OPENROUTER_API_KEY"]; stored {
		t.Fatal("the services key was stored although the profile was rejected")
	}
}

func TestReconcileLeavesAnExistingServicesKey(t *testing.T) {
	s, minting, _, _, _ := reconcileServer(withServicesKey(routeRows), nil)
	if _, err := s.EnsureOpenRouterKeys(context.Background()); err != nil {
		t.Fatalf("EnsureOpenRouterKeys: %v", err)
	}
	if len(minting.minted) != 0 {
		t.Fatalf("mints = %+v, want none", minting.minted)
	}
}

func TestReconcileMintsEveryActiveUserIdentity(t *testing.T) {
	ids := []identity.Identity{
		{ID: "admin-1", Kind: "user"}, {ID: "member-1", Kind: "user"},
		{ID: "aura-cli", Kind: "service"}, {ID: "gone-1", Kind: "user", Deactivated: true},
		// The seeded `local` operator is kind system and is deleted at first login, its key
		// row with it: a key minted for it outlives Aura's record of it (measured 2026-09-11).
		{ID: "local", Kind: "system"}, {ID: "tg-1", Kind: "channel"},
	}
	s, _, keys, _, _ := reconcileServer(withServicesKey(routeRows), ids, "admin-1")
	res, err := s.EnsureOpenRouterKeys(context.Background())
	if err != nil {
		t.Fatalf("EnsureOpenRouterKeys: %v", err)
	}
	if !slices.Equal(res.IdentitiesMinted, []string{"admin-1", "member-1"}) {
		t.Fatalf("identities minted = %v, want admin-1 and member-1 only", res.IdentitiesMinted)
	}
	if keys.records["admin-1"].LimitUSD != nil {
		t.Fatal("the admin's key has a cap, want none")
	}
	if got := keys.records["member-1"].LimitUSD; got == nil || *got != 0 {
		t.Fatal("the member's key is not at a zero cap")
	}
}

func TestReconcileAlignsLimitsWithRoles(t *testing.T) {
	ids := []identity.Identity{{ID: "admin-1", Kind: "user"}, {ID: "demoted-1", Kind: "user"}}
	s, minting, keys, _, _ := reconcileServer(withServicesKey(routeRows), ids, "admin-1")
	keys.records["admin-1"] = identitykey.Record{Key: "k1", Hash: "hash-admin", LimitUSD: capUSD(0)}
	keys.records["demoted-1"] = identitykey.Record{Key: "k2", Hash: "hash-demoted"}

	res, err := s.EnsureOpenRouterKeys(context.Background())
	if err != nil {
		t.Fatalf("EnsureOpenRouterKeys: %v", err)
	}
	if !minting.patched["hash-admin"].ClearLimit || keys.records["admin-1"].LimitUSD != nil {
		t.Fatal("the admin's zero cap was not cleared")
	}
	if patch := minting.patched["hash-demoted"]; patch.Limit == nil || *patch.Limit != 0 {
		t.Fatal("the demoted identity's key was not put back to a zero cap")
	}
	if !slices.Equal(res.LimitsAligned, []string{"admin-1", "demoted-1"}) {
		t.Fatalf("limits aligned = %v, want both", res.LimitsAligned)
	}
}

func TestReconcileWaits(t *testing.T) {
	for name, tc := range map[string]struct {
		keySet, routeBills bool
		want               string
	}{
		"local route":           {keySet: true, routeBills: false, want: skipLocalRoute},
		"no management key yet": {keySet: false, routeBills: true, want: skipManagementKeyUnset},
	} {
		t.Run(name, func(t *testing.T) {
			s, minting, _, _, _ := reconcileServer(routeRows, []identity.Identity{{ID: "admin-1", Kind: "user"}}, "admin-1")
			minting.keySet = tc.keySet
			s.keyMinter.routeBills = func() bool { return tc.routeBills }
			res, err := s.EnsureOpenRouterKeys(context.Background())
			if err != nil || res.Skipped != tc.want || len(minting.minted) != 0 {
				t.Fatalf("result = %+v err = %v mints = %d; want skipped %q and nothing minted", res, err, len(minting.minted), tc.want)
			}
		})
	}
}

func TestReconcileKeepsGoingAfterOneIdentityFails(t *testing.T) {
	ids := []identity.Identity{{ID: "broken-1", Kind: "user"}, {ID: "member-1", Kind: "user"}}
	s, minting, _, _, _ := reconcileServer(withServicesKey(routeRows), ids)
	minting.failFor = map[string]error{"broken-1": errors.New("provider 500")}
	res, err := s.EnsureOpenRouterKeys(context.Background())
	if err == nil || len(res.Errors) != 1 {
		t.Fatalf("result = %+v err = %v, want one reported error", res, err)
	}
	if !slices.Equal(res.IdentitiesMinted, []string{"member-1"}) {
		t.Fatalf("identities minted = %v, want member-1 despite broken-1", res.IdentitiesMinted)
	}
}

func TestSavingTheManagementKeyRunsTheReconciler(t *testing.T) {
	s, _, keys, _, _ := reconcileServer(withServicesKey(routeRows), []identity.Identity{{ID: "admin-1", Kind: "user"}}, "admin-1")
	rr, r := putReq(t, "AURA_OPENROUTER_MANAGEMENT_KEY", "sk-or-v1-mgmt", "admin-1")
	s.handlePutSetting(rr, r)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"openrouter_keys"`) {
		t.Fatalf("status = %d body = %s, want 200 with the reconcile result", rr.Code, rr.Body.String())
	}
	if _, minted := keys.records["admin-1"]; !minted {
		t.Fatal("saving the management key did not mint the admin's key")
	}
}

func TestReconcileEndpointReportsTheResult(t *testing.T) {
	s, _, _, _, _ := reconcileServer(routeRows, nil)
	rr := httptest.NewRecorder()
	s.handleReconcileOpenRouterKeys(rr, httptest.NewRequest(http.MethodPost, "/api/admin/openrouter/reconcile", nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"services_label":"sk-or-v1-...hash-1"`) {
		t.Fatalf("status = %d body = %s, want 200 with the services label", rr.Code, rr.Body.String())
	}
}

// TestReconcileReportsTheLabelsItMinted: the first-run setup shows the admin their new key by its
// masked label, so a run names the labels of the keys it minted, and only those.
func TestReconcileReportsTheLabelsItMinted(t *testing.T) {
	s, _, _, _, _ := reconcileServer(withServicesKey(routeRows), []identity.Identity{{ID: "admin-1", Kind: "user"}}, "admin-1")
	res, err := s.EnsureOpenRouterKeys(context.Background())
	if err != nil {
		t.Fatalf("EnsureOpenRouterKeys: %v", err)
	}
	if got := res.MintedLabels["admin-1"]; got != "sk-or-v1-...hash-1" {
		t.Fatalf("minted label = %q, want the admin key's masked label", got)
	}
	again, err := s.EnsureOpenRouterKeys(context.Background())
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if len(again.MintedLabels) != 0 {
		t.Fatalf("second run labels = %v, want none: it minted nothing", again.MintedLabels)
	}
}
