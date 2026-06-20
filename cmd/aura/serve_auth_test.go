package main

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/identity"
)

type namedIdentityStore struct {
	seen []string
	ids  map[string]identity.Identity
}

func (s *namedIdentityStore) GetIdentityByName(_ context.Context, name string) (identity.Identity, error) {
	s.seen = append(s.seen, name)
	return s.ids[name], nil
}

func TestResolveWebAuthIdentityIDUsesAuthulaOperatorIdentity(t *testing.T) {
	store := &namedIdentityStore{ids: map[string]identity.Identity{
		"local":    {ID: "local-id", Name: "local"},
		"operator": {ID: "operator-id", Name: "operator"},
	}}

	got := resolveWebAuthIdentityID(context.Background(), store, &config.Config{
		WebAuthProvider:         "authula",
		AuthulaOperatorIdentity: "operator",
	})

	if got != "operator-id" {
		t.Fatalf("identity id = %q, want operator-id", got)
	}
	if len(store.seen) != 1 || store.seen[0] != "operator" {
		t.Fatalf("resolved names = %v, want [operator]", store.seen)
	}
}

func TestResolveWebAuthIdentityIDKeepsPassphraseOnLocal(t *testing.T) {
	store := &namedIdentityStore{ids: map[string]identity.Identity{
		"local":    {ID: "local-id", Name: "local"},
		"operator": {ID: "operator-id", Name: "operator"},
	}}

	got := resolveWebAuthIdentityID(context.Background(), store, &config.Config{
		WebAuthProvider:         "passphrase",
		AuthulaOperatorIdentity: "operator",
	})

	if got != "local-id" {
		t.Fatalf("identity id = %q, want local-id", got)
	}
	if len(store.seen) != 1 || store.seen[0] != "local" {
		t.Fatalf("resolved names = %v, want [local]", store.seen)
	}
}

func TestAuthulaProvisioningConfiguredAllowsPassphraseOnboarding(t *testing.T) {
	cfg := &config.Config{
		WebAuthProvider: "passphrase",
		DB:              db.Config{URL: "postgres://aura_app:pw@127.0.0.1:5432/aura"},
		AuthulaSecret:   "0123456789abcdef0123456789abcdef",
	}

	if !authulaProvisioningConfigured(cfg) {
		t.Fatal("passphrase login with Authula DB+secret should still wire onboarding provisioning")
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
