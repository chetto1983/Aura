package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/webauth"
)

type namedIdentityStore struct {
	err     error
	listErr error
	seen    []string
	ids     map[string]identity.Identity
	list    []identity.Identity
}

func (s *namedIdentityStore) GetIdentityByName(_ context.Context, name string) (identity.Identity, error) {
	s.seen = append(s.seen, name)
	if s.err != nil {
		return identity.Identity{}, s.err
	}
	return s.ids[name], nil
}

func (s *namedIdentityStore) ListIdentities(context.Context) ([]identity.Identity, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return append([]identity.Identity(nil), s.list...), nil
}

func TestResolveWebAuthIdentityIDUsesAuthulaOperatorIdentity(t *testing.T) {
	store := &namedIdentityStore{ids: map[string]identity.Identity{
		"local":    {ID: "local-id", Name: "local"},
		"operator": {ID: "operator-id", Name: "operator"},
	}}

	got, err := resolveWebAuthIdentityID(context.Background(), store, &config.Config{
		WebAuthProvider:         "authula",
		AuthulaOperatorIdentity: "operator",
	})
	if err != nil {
		t.Fatalf("resolveWebAuthIdentityID: %v", err)
	}

	if got != "operator-id" {
		t.Fatalf("identity id = %q, want operator-id", got)
	}
	if len(store.seen) != 1 || store.seen[0] != "operator" {
		t.Fatalf("resolved names = %v, want [operator]", store.seen)
	}
}

func TestResolveWebAuthIdentityIDUsesOperatorIdentityIndependentOfProvider(t *testing.T) {
	store := &namedIdentityStore{ids: map[string]identity.Identity{
		"local":    {ID: "local-id", Name: "local"},
		"operator": {ID: "operator-id", Name: "operator"},
	}}

	got, err := resolveWebAuthIdentityID(context.Background(), store, &config.Config{
		WebAuthProvider:         "passphrase",
		AuthulaOperatorIdentity: "operator",
	})
	if err != nil {
		t.Fatalf("resolveWebAuthIdentityID: %v", err)
	}

	if got != "operator-id" {
		t.Fatalf("identity id = %q, want operator-id", got)
	}
	if len(store.seen) != 1 || store.seen[0] != "operator" {
		t.Fatalf("resolved names = %v, want [operator]", store.seen)
	}
}

func TestResolveWebAuthIdentityIDDefaultIsOptional(t *testing.T) {
	store := &namedIdentityStore{ids: map[string]identity.Identity{
		"local": {ID: "local-id", Name: "local"},
	}}

	got, err := resolveWebAuthIdentityID(context.Background(), store, &config.Config{})
	if err != nil {
		t.Fatalf("resolveWebAuthIdentityID: %v", err)
	}
	if got != "" {
		t.Fatalf("identity id = %q, want empty optional fallback", got)
	}
	if len(store.seen) != 0 {
		t.Fatalf("resolved names = %v, want no lookup for empty default", store.seen)
	}
}

func TestResolveWebAuthIdentityIDToleratesMissingLegacyLocal(t *testing.T) {
	store := &namedIdentityStore{err: identity.ErrIdentityNotFound}

	got, err := resolveWebAuthIdentityID(context.Background(), store, &config.Config{
		AuthulaOperatorIdentity: "local",
	})
	if err != nil {
		t.Fatalf("resolveWebAuthIdentityID: %v", err)
	}
	if got != "" {
		t.Fatalf("identity id = %q, want empty when legacy local is gone", got)
	}
	if len(store.seen) != 1 || store.seen[0] != "local" {
		t.Fatalf("resolved names = %v, want [local]", store.seen)
	}
}

func TestResolveWebAuthIdentityIDFailsWhenConfiguredIdentityMissing(t *testing.T) {
	store := &namedIdentityStore{err: errors.New("identity not found")}

	got, err := resolveWebAuthIdentityID(context.Background(), store, &config.Config{
		AuthulaOperatorIdentity: "missing-operator",
	})

	if err == nil {
		t.Fatalf("resolveWebAuthIdentityID returned nil error with identity id %q", got)
	}
	if got != "" {
		t.Fatalf("identity id = %q, want empty on lookup failure", got)
	}
	if len(store.seen) != 1 || store.seen[0] != "missing-operator" {
		t.Fatalf("resolved names = %v, want [missing-operator]", store.seen)
	}
}

func TestResolveSetupIdentityIDPrefersUserIdentity(t *testing.T) {
	store := &namedIdentityStore{
		ids: map[string]identity.Identity{
			"local": {ID: "local-id", Name: "local", Kind: "system"},
		},
		list: []identity.Identity{
			{ID: "local-id", Name: "local", Kind: "system"},
			{ID: "user-id", Name: "operator", Kind: "user"},
		},
	}

	if got := resolveSetupIdentityID(context.Background(), store); got != "user-id" {
		t.Fatalf("setup identity id = %q, want user-id", got)
	}
}

func TestResolveSetupIdentityIDFallsBackToLegacyLocal(t *testing.T) {
	store := &namedIdentityStore{
		ids: map[string]identity.Identity{
			"local": {ID: "local-id", Name: "local", Kind: "system"},
		},
		list: []identity.Identity{{ID: "local-id", Name: "local", Kind: "system"}},
	}

	if got := resolveSetupIdentityID(context.Background(), store); got != "local-id" {
		t.Fatalf("setup identity id = %q, want local-id", got)
	}
}

func TestResolveSetupIdentityIDToleratesMissingLegacyLocal(t *testing.T) {
	store := &namedIdentityStore{err: identity.ErrIdentityNotFound}

	if got := resolveSetupIdentityID(context.Background(), store); got != "" {
		t.Fatalf("setup identity id = %q, want empty", got)
	}
}

func TestAuthulaProvisioningConfiguredIgnoresLegacyProviderFlag(t *testing.T) {
	cfg := &config.Config{
		WebAuthProvider: "passphrase",
		DB:              db.Config{URL: "postgres://aura_app:pw@127.0.0.1:5432/aura"},
		AuthulaSecret:   "0123456789abcdef0123456789abcdef",
	}

	if !authulaProvisioningConfigured(cfg) {
		t.Fatal("Authula DB+secret should be configured regardless of the legacy provider flag")
	}
}

func TestBuildAuthDepsRequiresAuthulaConfigEvenWithLegacyProviderFlag(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("buildAuthDeps panicked before returning an Authula configuration error: %v", r)
		}
	}()
	_, provider, err := buildAuthDeps(context.Background(), &chatEnv{
		cfg: &config.Config{
			WebAuthProvider: "passphrase",
			WebAuthSecret:   "legacy-passphrase",
		},
	})

	if err == nil {
		t.Fatal("buildAuthDeps succeeded without Authula DSN+secret, want boot failure")
	}
	if provider != nil {
		t.Fatal("buildAuthDeps returned an Authula provider after a configuration failure")
	}
	if !strings.Contains(err.Error(), "authula") {
		t.Fatalf("error = %q, want authula configuration context", err)
	}
}

func TestAuthulaProvisioningConfiguredRequiresSecretAndDSN(t *testing.T) {
	cases := []struct {
		name string
		cfg  *config.Config
	}{
		{name: "nil", cfg: nil},
		{name: "missing secret", cfg: &config.Config{DB: db.Config{URL: "postgres://x"}}},
		{name: "missing dsn", cfg: &config.Config{AuthulaSecret: "secret"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if authulaProvisioningConfigured(tc.cfg) {
				t.Fatal("incomplete Authula provisioning config must not be treated as configured")
			}
		})
	}
}

type fakeStartupLinker struct {
	resolved string
	err      error
	links    []struct {
		identityID    string
		authulaUserID string
	}
}

func (f *fakeStartupLinker) ResolveIdentityID(context.Context, string) (string, error) {
	return f.resolved, f.err
}

func (f *fakeStartupLinker) LinkOperator(_ context.Context, identityID, authulaUserID string) error {
	f.links = append(f.links, struct {
		identityID    string
		authulaUserID string
	}{identityID: identityID, authulaUserID: authulaUserID})
	return nil
}

func TestEnsureAuthulaUserLinkedKeepsExistingUserLink(t *testing.T) {
	linker := &fakeStartupLinker{resolved: "user-id"}
	err := ensureAuthulaUserLinked(context.Background(), linker, nil, "local-id", "auth-user", func(context.Context, *pgxpool.Pool, string) (string, error) {
		return "user-id", nil
	})
	if err != nil {
		t.Fatalf("ensureAuthulaUserLinked: %v", err)
	}
	if len(linker.links) != 0 {
		t.Fatalf("existing user link was overwritten: %+v", linker.links)
	}
}

func TestEnsureAuthulaUserLinkedRepairsLegacyLocalLink(t *testing.T) {
	linker := &fakeStartupLinker{resolved: "local-id"}
	err := ensureAuthulaUserLinked(context.Background(), linker, nil, "local-id", "auth-user", func(context.Context, *pgxpool.Pool, string) (string, error) {
		return "user-id", nil
	})
	if err != nil {
		t.Fatalf("ensureAuthulaUserLinked: %v", err)
	}
	if len(linker.links) != 1 || linker.links[0].identityID != "user-id" || linker.links[0].authulaUserID != "auth-user" {
		t.Fatalf("links = %+v, want repair to user-id/auth-user", linker.links)
	}
}

func TestEnsureAuthulaUserLinkedUsesMatchingUserIdentityBeforeFallback(t *testing.T) {
	linker := &fakeStartupLinker{err: webauth.ErrLinkNotFound}
	err := ensureAuthulaUserLinked(context.Background(), linker, nil, "local-id", "auth-user", func(context.Context, *pgxpool.Pool, string) (string, error) {
		return "user-id", nil
	})
	if err != nil {
		t.Fatalf("ensureAuthulaUserLinked: %v", err)
	}
	if len(linker.links) != 1 || linker.links[0].identityID != "user-id" {
		t.Fatalf("links = %+v, want user-id", linker.links)
	}
}

func TestEnsureAuthulaUserLinkedFallsBackWhenNoUserIdentityExists(t *testing.T) {
	linker := &fakeStartupLinker{err: webauth.ErrLinkNotFound}
	err := ensureAuthulaUserLinked(context.Background(), linker, nil, "local-id", "auth-user", func(context.Context, *pgxpool.Pool, string) (string, error) {
		return "", nil
	})
	if err != nil {
		t.Fatalf("ensureAuthulaUserLinked: %v", err)
	}
	if len(linker.links) != 1 || linker.links[0].identityID != "local-id" {
		t.Fatalf("links = %+v, want local fallback", linker.links)
	}
}
