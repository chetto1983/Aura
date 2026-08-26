//go:build db_integration

package mcpregistry

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/mcp"
)

func registryEnvOrSkip(t *testing.T, key string) string {
	t.Helper()
	value := os.Getenv(key)
	if value == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("mcpregistry integration requires %s under CI", key)
		}
		t.Skipf("mcpregistry integration requires %s", key)
	}
	return value
}

func registryPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := db.Open(ctx, &db.Config{URL: registryEnvOrSkip(t, "AURA_DB_URL")})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func liveRegistryStore(t *testing.T, pool *pgxpool.Pool) *Store {
	t.Helper()
	store, err := NewStore(pool, registryEnvOrSkip(t, "AURA_AUTHULA_SECRET"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

func TestStoreRoundTripSealsEnvironmentAndRemoves(t *testing.T) {
	pool := registryPool(t)
	store := liveRegistryStore(t, pool)
	ctx := context.Background()
	name := "mcpregistry-test-" + uuid.NewString()
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM aura.mcp_server WHERE name = $1`, name) })

	disabled := false
	entry := Entry{
		Name: "  " + name + "  ",
		Server: mcp.ManagedServer{
			Command: "node",
			Args:    []string{"server.js"},
			Env:     []string{"TOKEN=plaintext-secret", "MODE=test"},
			Enabled: &disabled,
			Source:  "  custom-test  ",
			Trust:   mcp.ManagedTrust{Class: mcp.TrustTrustedLocal},
			Runtime: mcp.ManagedRuntime{Kind: mcp.RuntimeKindLocal},
		},
		Profiles: []string{"work", "default"},
	}
	if err := store.Upsert(ctx, entry, "service-identity"); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	var ciphertext []byte
	if err := pool.QueryRow(ctx, `SELECT env_enc FROM aura.mcp_server WHERE name = $1`, name).Scan(&ciphertext); err != nil {
		t.Fatalf("read ciphertext: %v", err)
	}
	if len(ciphertext) == 0 || bytes.Contains(ciphertext, []byte("plaintext-secret")) {
		t.Fatal("env_enc is empty or contains the plaintext credential")
	}

	doc, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got, ok := doc.MCPServers[name]
	if !ok {
		t.Fatalf("List omitted %q", name)
	}
	if got.Source != "custom-test" || got.Enabled == nil || *got.Enabled {
		t.Fatalf("server metadata = %+v", got)
	}
	if strings.Join(got.Env, ",") != "TOKEN=plaintext-secret,MODE=test" {
		t.Errorf("server env = %v", got.Env)
	}
	for _, profile := range []string{"default", "work"} {
		if names := doc.Profiles[profile].Servers; len(names) != 1 || names[0] != name {
			t.Errorf("profile %s servers = %v", profile, names)
		}
	}

	entry.Name = name
	entry.Server.Env = nil
	entry.Server.Enabled = nil
	entry.Profiles = nil
	if err := store.Upsert(ctx, entry, ""); err != nil {
		t.Fatalf("replace: %v", err)
	}
	doc, err = store.List(ctx)
	if err != nil {
		t.Fatalf("List after replace: %v", err)
	}
	got = doc.MCPServers[name]
	if got.Env != nil || got.Enabled != nil {
		t.Fatalf("replace retained optional values: %+v", got)
	}

	if err := store.Remove(ctx, "  "+name+"  "); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := store.Remove(ctx, name); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Remove = %v, want ErrNotFound", err)
	}
	if err := store.Upsert(ctx, Entry{Name: "   "}, ""); err == nil {
		t.Fatal("Upsert accepted a blank name")
	}
}

func TestStoreWrapsDatabaseErrors(t *testing.T) {
	pool := registryPool(t)
	store := liveRegistryStore(t, pool)
	pool.Close()
	ctx := context.Background()

	if _, err := store.List(ctx); err == nil || !strings.Contains(err.Error(), "mcpregistry: list") {
		t.Fatalf("List error = %v", err)
	}
	if err := store.Upsert(ctx, Entry{Name: "closed", Server: mcp.ManagedServer{}}, ""); err == nil || !strings.Contains(err.Error(), "mcpregistry: upsert") {
		t.Fatalf("Upsert error = %v", err)
	}
	if err := store.Remove(ctx, "closed"); err == nil || !strings.Contains(err.Error(), "mcpregistry: remove") {
		t.Fatalf("Remove error = %v", err)
	}
}
