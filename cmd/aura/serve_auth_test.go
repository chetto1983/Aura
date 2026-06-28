package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/identity"
)

type namedIdentityStore struct {
	err  error
	seen []string
	ids  map[string]identity.Identity
}

func (s *namedIdentityStore) GetIdentityByName(_ context.Context, name string) (identity.Identity, error) {
	s.seen = append(s.seen, name)
	if s.err != nil {
		return identity.Identity{}, s.err
	}
	return s.ids[name], nil
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
