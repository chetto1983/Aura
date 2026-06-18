package main

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/config"
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
