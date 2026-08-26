package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
	"github.com/chetto1983/aura/internal/mcpoauth"
	"github.com/chetto1983/aura/internal/webauth"
)

const (
	firstUserIdentity  = "b130c94d-a213-463a-a797-ec124104363a"
	secondUserIdentity = "9f1c0f2e-1111-4b2a-9d3e-0f0a0b0c0d0e"
	serviceIdentity    = "00000000-0000-0000-0000-000000000039"
	retiredIdentity    = "22222222-2222-4222-8222-222222222222"
)

type fakeFirstPartyIssuer struct {
	mu     sync.Mutex
	calls  []string
	serial int
	ttl    time.Duration
	now    func() time.Time
	err    error
}

func (f *fakeFirstPartyIssuer) IssueFirstPartyToken(ctx context.Context, resource, identityID string) (webauth.IssuedMCPToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// The production store scopes every row by the identity on the context, so a call
	// that arrives unscoped would store somebody else's credential — or nobody's.
	if got := identityctx.IdentityID(ctx); got != identityID {
		return webauth.IssuedMCPToken{}, fmt.Errorf("context identity %q, want %q", got, identityID)
	}
	if f.err != nil {
		return webauth.IssuedMCPToken{}, f.err
	}
	f.serial++
	f.calls = append(f.calls, identityID+"|"+resource)
	return webauth.IssuedMCPToken{
		AccessToken: fmt.Sprintf("access-%s-%d", identityID, f.serial),
		TokenType:   "Bearer",
		Scope:       mcp.AuraOAuthToolsScope,
		ClientID:    "aura",
		ExpiresAt:   f.now().Add(f.ttl),
	}, nil
}

func (f *fakeFirstPartyIssuer) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]string(nil), f.calls...)
	sort.Strings(out)
	return out
}

type fakeGrantStore struct {
	mu     sync.Mutex
	grants map[string]mcpoauth.Grant
	saves  int
}

func newFakeGrantStore() *fakeGrantStore {
	return &fakeGrantStore{grants: map[string]mcpoauth.Grant{}}
}

func (s *fakeGrantStore) key(ctx context.Context, serverName string) (string, error) {
	owner := identityctx.IdentityID(ctx)
	if owner == "" {
		return "", errors.New("mcpoauth: no identity on context")
	}
	return owner + "\x00" + serverName, nil
}

func (s *fakeGrantStore) Load(ctx context.Context, serverName string) (mcpoauth.Grant, error) {
	key, err := s.key(ctx, serverName)
	if err != nil {
		return mcpoauth.Grant{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	grant, ok := s.grants[key]
	if !ok {
		return mcpoauth.Grant{}, fmt.Errorf("%w: %q", mcpoauth.ErrNoGrant, serverName)
	}
	return grant, nil
}

func (s *fakeGrantStore) Save(ctx context.Context, grant mcpoauth.Grant) error {
	key, err := s.key(ctx, grant.ServerName)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.grants[key] = grant
	s.saves++
	return nil
}

func (s *fakeGrantStore) get(t *testing.T, owner, serverName string) mcpoauth.Grant {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	grant, ok := s.grants[owner+"\x00"+serverName]
	if !ok {
		t.Fatalf("no grant stored for identity %s server %s", owner, serverName)
	}
	return grant
}

func (s *fakeGrantStore) has(owner, serverName string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.grants[owner+"\x00"+serverName]
	return ok
}

type fakeIdentityLister struct {
	rows []identity.Identity
	err  error
}

func (f fakeIdentityLister) ListIdentities(context.Context) ([]identity.Identity, error) {
	return f.rows, f.err
}

// firstPartyTestServers is a fixed two-recipe set so the keeper's behaviour never depends
// on what the machine running the test happens to have installed.
func firstPartyTestServers() map[string]mcp.ManagedServer {
	out := map[string]mcp.ManagedServer{}
	for _, name := range []string{"memory", "calendar"} {
		recipe, ok := mcpmanager.LookupCatalog(name)
		if !ok {
			panic("catalog lost the " + name + " recipe")
		}
		out[name] = recipe.Server
	}
	return out
}

func testGrantKeeper(t *testing.T, clock *time.Time, rows []identity.Identity) (*firstPartyGrantKeeper, *fakeFirstPartyIssuer, *fakeGrantStore) {
	t.Helper()
	now := func() time.Time { return *clock }
	issuer := &fakeFirstPartyIssuer{ttl: 15 * time.Minute, now: now}
	store := newFakeGrantStore()
	keeper := newFirstPartyGrantKeeper(issuer, store, fakeIdentityLister{rows: rows})
	if keeper == nil {
		t.Fatal("newFirstPartyGrantKeeper returned nil for complete dependencies")
	}
	keeper.now = now
	servers := firstPartyTestServers()
	keeper.servers = func() (map[string]mcp.ManagedServer, error) { return servers, nil }
	return keeper, issuer, store
}

func liveIdentities() []identity.Identity {
	return []identity.Identity{
		{ID: firstUserIdentity, Name: "operator@example.test", Kind: "user"},
		{ID: secondUserIdentity, Name: "second@example.test", Kind: "user"},
		{ID: serviceIdentity, Name: "aura-cli", Kind: "service"},
		{ID: retiredIdentity, Name: "gone@example.test", Kind: "user", Deactivated: true},
	}
}

func TestFirstPartyGrantsMintForEveryUserIdentity(t *testing.T) {
	clock := time.Unix(1_788_303_600, 0).UTC()
	keeper, issuer, store := testGrantKeeper(t, &clock, liveIdentities())

	if err := keeper.EnsureNow(t.Context()); err != nil {
		t.Fatalf("EnsureNow: %v", err)
	}

	memory, _ := mcpmanager.LookupCatalog("memory")
	calendar, _ := mcpmanager.LookupCatalog("calendar")
	want := []string{
		firstUserIdentity + "|" + calendar.Server.URL,
		firstUserIdentity + "|" + memory.Server.URL,
		secondUserIdentity + "|" + calendar.Server.URL,
		secondUserIdentity + "|" + memory.Server.URL,
	}
	sort.Strings(want)
	got := issuer.recorded()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("minted %v, want %v", got, want)
	}
	// A service identity has no sidecar data and no cockpit; a deactivated one must not
	// receive a live credential at all.
	for _, id := range []string{serviceIdentity, retiredIdentity} {
		if store.has(id, "memory") {
			t.Errorf("identity %s must not receive an auto-granted memory credential", id)
		}
	}

	grant := store.get(t, firstUserIdentity, "memory")
	if grant.ResourceURL != memory.Server.URL {
		t.Errorf("resource = %q, want %q", grant.ResourceURL, memory.Server.URL)
	}
	if !grant.ExpiresAt.Equal(clock.Add(15 * time.Minute)) {
		t.Errorf("ExpiresAt = %s, want an absolute expiry of %s", grant.ExpiresAt, clock.Add(15*time.Minute))
	}
	// No refresh token, on purpose: Authula cannot store one without a login session, so
	// a stored one could never be redeemed. The keeper re-mints instead.
	if grant.TokenType != "Bearer" || grant.RefreshToken != "" {
		t.Errorf("grant = %+v, want a Bearer token and no refresh token", grant)
	}
	// The stored client blob is what restoreTokenSource reads on a cold start; without a
	// token endpoint it downgrades to "re-authorization required", which for a
	// first-party server means a browser prompt nobody is at.
	var client struct {
		ClientID      string   `json:"client_id"`
		TokenEndpoint string   `json:"token_endpoint"`
		Scopes        []string `json:"scopes"`
	}
	if err := json.Unmarshal(grant.ClientInfo, &client); err != nil {
		t.Fatalf("decode stored client info: %v", err)
	}
	if client.TokenEndpoint != mcp.AuraTokenEndpoint() {
		t.Errorf("token endpoint = %q, want %q", client.TokenEndpoint, mcp.AuraTokenEndpoint())
	}
	if client.ClientID != "aura" {
		t.Errorf("client id = %q, want aura", client.ClientID)
	}
	if len(client.Scopes) != 1 || client.Scopes[0] != mcp.AuraOAuthToolsScope {
		t.Errorf("scopes = %v, want [%s]", client.Scopes, mcp.AuraOAuthToolsScope)
	}
}

// The subject is the tenancy boundary: two identities must never end up sharing a token.
func TestFirstPartyGrantsAreScopedPerIdentity(t *testing.T) {
	clock := time.Unix(1_788_303_600, 0).UTC()
	keeper, _, store := testGrantKeeper(t, &clock, liveIdentities())
	if err := keeper.EnsureNow(t.Context()); err != nil {
		t.Fatalf("EnsureNow: %v", err)
	}

	first := store.get(t, firstUserIdentity, "memory")
	second := store.get(t, secondUserIdentity, "memory")
	if first.AccessToken == second.AccessToken {
		t.Fatal("two identities were handed the same first-party credential")
	}
	// The fake store keys by the identity on the context exactly as RLS does, so a grant
	// that leaked across identities would be visible here.
	if _, err := store.Load(identityctx.WithIdentityID(t.Context(), serviceIdentity), "memory"); !errors.Is(err, mcpoauth.ErrNoGrant) {
		t.Fatalf("service identity sees a memory grant: %v", err)
	}
}

func TestFirstPartyGrantsSecondPassMintsNothing(t *testing.T) {
	clock := time.Unix(1_788_303_600, 0).UTC()
	keeper, issuer, store := testGrantKeeper(t, &clock, liveIdentities())

	if err := keeper.EnsureNow(t.Context()); err != nil {
		t.Fatalf("first EnsureNow: %v", err)
	}
	minted, saved := len(issuer.recorded()), store.saves
	clock = clock.Add(time.Minute)
	if err := keeper.EnsureNow(t.Context()); err != nil {
		t.Fatalf("second EnsureNow: %v", err)
	}
	if got := len(issuer.recorded()); got != minted {
		t.Fatalf("second pass minted %d tokens in total, want the first pass's %d", got, minted)
	}
	if store.saves != saved {
		t.Fatalf("second pass wrote %d grants in total, want the first pass's %d", store.saves, saved)
	}
	// EnsureIdentity is the identity-creation seam and must be just as idempotent.
	if err := keeper.EnsureIdentity(t.Context(), firstUserIdentity); err != nil {
		t.Fatalf("EnsureIdentity: %v", err)
	}
	if store.saves != saved {
		t.Fatalf("EnsureIdentity re-minted an existing grant (%d writes, want %d)", store.saves, saved)
	}
}

func TestFirstPartyGrantsRenewBeforeTheTokenDies(t *testing.T) {
	clock := time.Unix(1_788_303_600, 0).UTC()
	keeper, issuer, store := testGrantKeeper(t, &clock, liveIdentities())
	if err := keeper.EnsureNow(t.Context()); err != nil {
		t.Fatalf("EnsureNow: %v", err)
	}
	before := store.get(t, firstUserIdentity, "memory").AccessToken
	minted := len(issuer.recorded())

	// A 15-minute token with a 7-minute renewal window: at +7 there is still 8 minutes
	// left and nothing may move.
	clock = clock.Add(7 * time.Minute)
	if err := keeper.EnsureNow(t.Context()); err != nil {
		t.Fatalf("EnsureNow at +7m: %v", err)
	}
	if len(issuer.recorded()) != minted {
		t.Fatal("a grant with 8 minutes left was re-minted; the keeper is not idempotent")
	}

	clock = clock.Add(2 * time.Minute)
	if err := keeper.EnsureNow(t.Context()); err != nil {
		t.Fatalf("EnsureNow at +9m: %v", err)
	}
	if len(issuer.recorded()) != 2*minted {
		t.Fatalf("minted %d tokens in total at +9m, want %d — a grant inside the renewal window must be replaced", len(issuer.recorded()), 2*minted)
	}
	after := store.get(t, firstUserIdentity, "memory")
	if after.AccessToken == before {
		t.Fatal("the renewed grant still carries the old access token")
	}
	if !after.ExpiresAt.After(clock) {
		t.Fatalf("renewed expiry %s is not in the future of %s", after.ExpiresAt, clock)
	}
}

// The keeper must re-mint a whole tick before anything expires, or the mount goes dark
// between two ticks.
func TestFirstPartyGrantRenewalWindowOutlivesATick(t *testing.T) {
	if firstPartyGrantRenewBefore <= firstPartyGrantInterval {
		t.Fatalf("renew window %s must exceed the tick interval %s", firstPartyGrantRenewBefore, firstPartyGrantInterval)
	}
}

// A grant's audience is pinned to the resource URL, so a sidecar that moved leaves a
// stored token the server will refuse.
func TestFirstPartyGrantsReplaceAStaleResourceURL(t *testing.T) {
	clock := time.Unix(1_788_303_600, 0).UTC()
	keeper, issuer, store := testGrantKeeper(t, &clock, liveIdentities())
	scoped := identityctx.WithIdentityID(t.Context(), firstUserIdentity)
	if err := store.Save(scoped, mcpoauth.Grant{
		ServerName:  "memory",
		ResourceURL: "http://127.0.0.1:18096/mcp/",
		AccessToken: "stale",
		ExpiresAt:   clock.Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed stale grant: %v", err)
	}

	if err := keeper.EnsureIdentity(t.Context(), firstUserIdentity); err != nil {
		t.Fatalf("EnsureIdentity: %v", err)
	}
	memory, _ := mcpmanager.LookupCatalog("memory")
	got := store.get(t, firstUserIdentity, "memory")
	if got.ResourceURL != memory.Server.URL || got.AccessToken == "stale" {
		t.Fatalf("grant = %+v, want a fresh token for %q", got, memory.Server.URL)
	}
	if len(issuer.recorded()) == 0 {
		t.Fatal("the stale grant was left in place")
	}
}

// SECURITY: a server Aura did not ship must never be handed a token nobody consented to.
func TestFirstPartyMCPServersRefusesEverythingButTheShippedRecipes(t *testing.T) {
	withMemoryMCPRegistry(t)
	enabled := true
	doc := mcp.ManagedConfig{
		Version:    mcp.ManagedConfigVersion,
		MCPServers: map[string]mcp.ManagedServer{},
		Profiles:   map[string]mcp.ManagedProfile{},
	}
	for _, name := range []string{"memory", "calendar", "whatsapp"} {
		recipe, ok := mcpmanager.LookupCatalog(name)
		if !ok {
			t.Fatalf("catalog lost the %s recipe", name)
		}
		server := recipe.Server
		server.Enabled = &enabled
		doc.MCPServers[name] = server
	}
	// What `aura mcp add` writes, plus the two shapes an operator could hand-edit into
	// servers.json to try to look shipped.
	doc.MCPServers["operator-added"] = mcp.ManagedServer{
		Type: mcp.ServerTypeStreamableHTTP, URL: "https://mcp.example.test/mcp",
		Enabled: &enabled, Source: "manual", Trust: mcp.ManagedTrust{Class: mcp.TrustRemoteHTTP},
	}
	doc.MCPServers["borrowed-source"] = mcp.ManagedServer{
		Type: mcp.ServerTypeStreamableHTTP, URL: "https://attacker.example/mcp",
		Enabled: &enabled, Source: "recipe:calendar", Trust: mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
	}
	seedMCPRegistry(t, doc)

	servers, err := firstPartyMCPServers()
	if err != nil {
		t.Fatalf("firstPartyMCPServers: %v", err)
	}
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	if fmt.Sprint(names) != fmt.Sprint([]string{"calendar", "memory", "whatsapp"}) {
		t.Fatalf("self-issuable servers = %v, want exactly the shipped recipes", names)
	}
}

func TestNewFirstPartyGrantKeeperRefusesIncompleteDependencies(t *testing.T) {
	issuer := &fakeFirstPartyIssuer{ttl: time.Minute, now: time.Now}
	store := newFakeGrantStore()
	identities := fakeIdentityLister{}
	if newFirstPartyGrantKeeper(nil, store, identities) != nil {
		t.Error("a keeper with no issuer must be nil")
	}
	if newFirstPartyGrantKeeper(issuer, nil, identities) != nil {
		t.Error("a keeper with no grant store must be nil")
	}
	if newFirstPartyGrantKeeper(issuer, store, nil) != nil {
		t.Error("a keeper with no identity source must be nil")
	}
	if buildFirstPartyGrantKeeper(nil, nil, identities, nil) != nil {
		t.Error("a deployment with no config, pool or Authula provider must not get a keeper")
	}
}

func TestFirstPartyGrantKeeperIsNilSafe(t *testing.T) {
	var keeper *firstPartyGrantKeeper
	if err := keeper.EnsureNow(t.Context()); err != nil {
		t.Fatalf("nil EnsureNow: %v", err)
	}
	if err := keeper.EnsureIdentity(t.Context(), firstUserIdentity); err != nil {
		t.Fatalf("nil EnsureIdentity: %v", err)
	}
	keeper.Start(t.Context())
	keeper.Stop()
}

func TestFirstPartyGrantKeeperTickMintsAndStopsClean(t *testing.T) {
	clock := time.Unix(1_788_303_600, 0).UTC()
	keeper, issuer, _ := testGrantKeeper(t, &clock, liveIdentities())
	keeper.interval = time.Millisecond
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	keeper.Start(ctx)
	deadline := time.After(5 * time.Second)
	for len(issuer.recorded()) == 0 {
		select {
		case <-deadline:
			keeper.Stop()
			t.Fatal("the keeper never minted a grant on its tick")
		case <-time.After(time.Millisecond):
		}
	}
	keeper.Stop()
	keeper.Stop()
}

func TestFirstPartyGrantsReportWhatTheyCouldNotMint(t *testing.T) {
	clock := time.Unix(1_788_303_600, 0).UTC()
	keeper, issuer, store := testGrantKeeper(t, &clock, liveIdentities())
	issuer.err = errors.New("injected: authorization server unavailable")

	err := keeper.EnsureNow(t.Context())
	if err == nil {
		t.Fatal("EnsureNow swallowed a mint failure")
	}
	if store.saves != 0 {
		t.Fatalf("stored %d grants despite a failing issuer", store.saves)
	}

	listErr := newFirstPartyGrantKeeper(issuer, store, fakeIdentityLister{err: errors.New("injected: no database")})
	if listErr == nil {
		t.Fatal("keeper is nil")
	}
	listErr.servers = func() (map[string]mcp.ManagedServer, error) { return firstPartyTestServers(), nil }
	if err := listErr.EnsureNow(t.Context()); err == nil {
		t.Fatal("EnsureNow swallowed an identity-listing failure")
	}
}
