package main

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identity"
)

// backfillEnv is the smallest chatEnv the builder reads: a config and an identity store.
// The store is never called here — every case under test decides before any I/O.
func backfillEnv(arcadeURL, embedURL string) *chatEnv {
	return &chatEnv{
		cfg: &config.Config{
			ArcadeDB: config.ArcadeDBConfig{BaseURL: arcadeURL},
			Embed:    config.EmbedConfig{BaseURL: embedURL},
		},
		identity: identity.New(nil),
	}
}

// Every unconfigured path must return a BARE nil. A nil *arcadedb.TenantBackfill returned
// as the interface would be non-nil to the handler, the "disabled" branch would never fire,
// and the sweep would fail every five minutes instead of being off.
func TestBuildMemoryEmbedBackfillReturnsBareNilWhenUnconfigured(t *testing.T) {
	t.Setenv("AURA_ARCADEDB_TENANT_SECRET", strings.Repeat("s", 32))
	cases := map[string]*chatEnv{
		"no env":               nil,
		"no config":            {},
		"no memory server":     backfillEnv("", "http://127.0.0.1:8081"),
		"no embedding sidecar": backfillEnv("http://127.0.0.1:2480", ""),
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			if got := buildMemoryEmbedBackfill(env); got != nil {
				t.Fatalf("got %#v, want a bare nil so the sweep registers as disabled", got)
			}
		})
	}
}

// Without the derivation secret the sweep cannot mint a tenant credential, so it must be
// off rather than knocking on every database with a credential it cannot produce.
func TestBuildMemoryEmbedBackfillNeedsTheTenantSecret(t *testing.T) {
	t.Setenv("AURA_ARCADEDB_TENANT_SECRET", "")
	if got := buildMemoryEmbedBackfill(backfillEnv("http://127.0.0.1:2480", "http://127.0.0.1:8081")); got != nil {
		t.Fatalf("got %#v, want disabled without the tenant derivation secret", got)
	}
}

func TestBuildMemoryEmbedBackfillWiresTheSweep(t *testing.T) {
	t.Setenv("AURA_ARCADEDB_TENANT_SECRET", strings.Repeat("s", 32))
	got := buildMemoryEmbedBackfill(backfillEnv("http://127.0.0.1:2480", "http://127.0.0.1:8081"))
	if _, ok := got.(*arcadedb.TenantBackfill); !ok {
		t.Fatalf("got %#v, want the live per-tenant backfill", got)
	}
}
