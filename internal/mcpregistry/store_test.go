package mcpregistry

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/mcp"
)

const registryTestSecret = "1111111111111111111111111111111111111111111111111111111111111111"

func registryFakePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(
		context.Background(),
		"postgres://u:p@127.0.0.1:1/nowhere?sslmode=disable",
	)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func registryStoreForCrypto(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(registryFakePool(t), registryTestSecret)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

func TestNewStoreValidatesInputs(t *testing.T) {
	t.Parallel()

	if _, err := NewStore(nil, registryTestSecret); err == nil {
		t.Fatal("NewStore(nil) returned no error")
	}
	if _, err := NewStore(registryFakePool(t), "short"); err == nil {
		t.Fatal("NewStore accepted an invalid wrapping secret")
	}
	store := registryStoreForCrypto(t)
	queries := sqlc.New(registryFakePool(t))
	txStore := store.Tx(queries)
	if txStore.q != queries || txStore.sealer != store.sealer {
		t.Fatal("Tx did not preserve the query and sealer boundaries")
	}
}

func TestStoreEntryFromRoundTrip(t *testing.T) {
	t.Parallel()

	store := registryStoreForCrypto(t)
	enabled := false
	config, err := json.Marshal(mcp.ManagedServer{
		Command: "node",
		Args:    []string{"server.js"},
		Source:  "stale-config-source",
		Trust:   mcp.ManagedTrust{Class: mcp.TrustTrustedLocal},
		Runtime: mcp.ManagedRuntime{Kind: mcp.RuntimeKindLocal},
	})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	env, err := store.sealer.Seal([]byte(`["TOKEN=secret","MODE=test"]`))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	entry, err := store.entryFrom(sqlc.AuraMcpServer{
		Name:     "notes",
		Source:   "catalog-source",
		Enabled:  pgtype.Bool{Bool: enabled, Valid: true},
		Config:   config,
		EnvEnc:   env,
		Profiles: []string{"default", "work"},
	})
	if err != nil {
		t.Fatalf("entryFrom: %v", err)
	}
	if entry.Name != "notes" || entry.Server.Source != "catalog-source" {
		t.Fatalf("entry identity = %+v", entry)
	}
	if entry.Server.Enabled == nil || *entry.Server.Enabled {
		t.Fatalf("enabled = %v, want explicit false", entry.Server.Enabled)
	}
	if got := strings.Join(entry.Server.Env, ","); got != "TOKEN=secret,MODE=test" {
		t.Errorf("env = %q", got)
	}
	if got := strings.Join(entry.Profiles, ","); got != "default,work" {
		t.Errorf("profiles = %q", got)
	}
}

func TestStoreEntryFromRejectsCorruptRows(t *testing.T) {
	t.Parallel()

	store := registryStoreForCrypto(t)
	invalidJSON, err := store.sealer.Seal([]byte(`{`))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	tests := []struct {
		name string
		row  sqlc.AuraMcpServer
		want string
	}{
		{name: "config", row: sqlc.AuraMcpServer{Name: "bad", Config: []byte(`{`)}, want: "decode config"},
		{name: "ciphertext", row: sqlc.AuraMcpServer{Name: "bad", EnvEnc: []byte("short")}, want: "open env"},
		{name: "env json", row: sqlc.AuraMcpServer{Name: "bad", EnvEnc: invalidJSON}, want: "decode env"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := store.entryFrom(tt.row)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("entryFrom error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestSQLValueConversions(t *testing.T) {
	t.Parallel()

	enabled := true
	if got := boolValue(nil); got.Valid {
		t.Fatalf("boolValue(nil) = %+v", got)
	}
	if got := boolValue(&enabled); !got.Valid || !got.Bool {
		t.Fatalf("boolValue(true) = %+v", got)
	}
	if got := uuidValue("not-a-uuid"); got.Valid {
		t.Fatalf("uuidValue(invalid) = %+v", got)
	}
	if got := uuidValue(" 00000000-0000-0000-0000-000000000001 "); !got.Valid {
		t.Fatalf("uuidValue(valid) = %+v", got)
	}
}
