//go:build db_integration

// Live-Postgres proof that plan 02-06's two composition-root adapters actually round
// trip: a mint against a fake OpenRouter server persists through identitykey.Store.Save
// on a REAL pool, a Load scoped to that identity decrypts back the same key, and the
// reverse-saga adapter revokes by identity id (looking the hash up itself) against the
// same fake server's DELETE-then-verifying-GET pair. openrouterprovision's own client
// behavior (the omitempty trap, the 403 classifier, ...) is already proven against
// httptest in plan 02-03; this test proves the WIRING — the part only a live Postgres
// round trip can prove — never the real OpenRouter API (CLAUDE.md: never call it).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/dbtest"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/identitykey"
)

func openRouterKeyEnvOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("integration test requires %s, but it is unset under CI", key)
		}
		t.Skipf("integration test requires %s; set it and re-run (e.g. via .env + make db-up)", key)
	}
	return v
}

// fakeOpenRouterProvisioningServer stands in for OpenRouter's Provisioning API: mint
// returns a fixed key/hash/label, GET returns 200 until revoked then 404, DELETE marks
// it revoked. This is a test double for the PROVIDER, not for anything this plan wrote —
// openrouterprovision's own client is exercised for real against it.
func fakeOpenRouterProvisioningServer(t *testing.T) *httptest.Server {
	t.Helper()
	const rawKey = "sk-or-v1-live-roundtrip-test"
	const hash = "hash-live-roundtrip"
	revoked := false
	mux := http.NewServeMux()
	mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "want POST", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"key": rawKey,
			"data": map[string]any{
				"hash":  hash,
				"label": "sk-or-v1-liv...est1",
				"name":  "roundtrip-test",
			},
		})
	})
	mux.HandleFunc("/keys/"+hash, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodDelete:
			revoked = true
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"deleted": true})
		case http.MethodGet:
			if revoked {
				http.Error(w, `{"error":{"message":"key not found"}}`, http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"hash": hash, "label": "sk-or-v1-liv...est1"},
			})
		default:
			http.Error(w, "want GET or DELETE", http.StatusMethodNotAllowed)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestOpenRouterKeyAdaptersRoundTripLive proves the composition this plan wrote — not
// openrouterprovision's own client, and not the real OpenRouter API — actually works
// end to end against a real Postgres: mint persists a DECRYPTABLE key, and revoke
// (identity-keyed) removes it at the fake provider.
func TestOpenRouterKeyAdaptersRoundTripLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pwd := openRouterKeyEnvOrSkip(t, "POSTGRES_PASSWORD")
	migrateURL := dbtest.MigrateURL(t, openRouterKeyEnvOrSkip(t, "AURA_DB_MIGRATE_URL"))
	appURL := openRouterKeyEnvOrSkip(t, "AURA_DB_URL")
	authulaSecret := openRouterKeyEnvOrSkip(t, "AURA_AUTHULA_SECRET")

	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	bootstrap := fmt.Sprintf("postgres://aura:%s@%s:%s/aura?sslmode=disable", pwd, host, port)
	if err := db.EnsureRoles(ctx, bootstrap, pwd); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	if _, err := db.Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	pool, err := db.Open(ctx, &db.Config{URL: appURL})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(pool.Close)

	newID := uuid.New()
	identityID := newID.String()
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')`,
		pgtype.UUID{Bytes: newID, Valid: true}, "openrouter-roundtrip-"+identityID[:8],
	); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM aura.identities WHERE id = $1`, pgtype.UUID{Bytes: newID, Valid: true})
	})

	store, err := identitykey.NewStore(pool, authulaSecret)
	if err != nil {
		t.Fatalf("identitykey.NewStore: %v", err)
	}
	srv := fakeOpenRouterProvisioningServer(t)
	cfg := openRouterKeyConfig{
		client: srv.Client(), baseURL: srv.URL, store: store,
		managementKey: func(context.Context) (string, error) { return "test-management-key", nil },
	}
	mint := agui.NewIdentityKeyMinter(openRouterMintingAdapter{cfg}, store, memberCapabilities{}, func() bool { return true })
	revoke := openRouterKeyRevokeAdapter{cfg}

	minted, err := mint.MintKey(ctx, identityID, identityID)
	if err != nil {
		t.Fatalf("MintKey: %v", err)
	}
	if minted.Hash == "" || minted.Label == "" {
		t.Fatalf("MintKey returned an empty hash/label: %+v", minted)
	}

	// The real proof: Load, scoped to the SAME identity, decrypts back the key the fake
	// server minted — the composition (HTTP response -> Store.Save -> Store.Load)
	// actually round trips on a live Postgres, not just in a fake.
	scoped := identityctx.WithIdentityID(ctx, identityID)
	rec, err := store.Load(scoped)
	if err != nil {
		t.Fatalf("Load after mint: %v", err)
	}
	if rec.Key != "sk-or-v1-live-roundtrip-test" {
		t.Fatalf("decrypted key = %q, want the raw key the fake server minted", rec.Key)
	}
	if rec.Hash != minted.Hash {
		t.Fatalf("stored hash = %q, want %q (MintKey's own return value)", rec.Hash, minted.Hash)
	}
	if rec.LimitUSD == nil || *rec.LimitUSD != 0 {
		t.Fatalf("stored cap = %v, want a member's zero cap", rec.LimitUSD)
	}

	// The reverse-saga adapter is identity-keyed: it must look the hash up itself and
	// revoke it — proving deprovision.go's real contract, not just the forward leg's.
	if err := revoke.RevokeKey(ctx, identityID); err != nil {
		t.Fatalf("RevokeKey(identityID): %v", err)
	}
	// Idempotent: revoking an already-revoked key must still converge (CRED-08).
	if err := revoke.RevokeKey(ctx, identityID); err != nil {
		t.Fatalf("second RevokeKey(identityID) (already revoked): %v", err)
	}
}

// memberCapabilities answers "not an admin" for every identity.
type memberCapabilities struct{}

func (memberCapabilities) HasCapability(context.Context, string, string) (bool, error) {
	return false, nil
}
