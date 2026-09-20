//go:build db_integration

package remotetunnel

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/dbtest"
	"github.com/chetto1983/aura/internal/settings"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStorePersistsGenerationAndEncryptedCredentials(t *testing.T) {
	dsn := os.Getenv("AURA_DB_MIGRATE_URL")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("AURA_DB_MIGRATE_URL required in CI")
		}
		t.Skip("requires disposable PostgreSQL")
	}
	dsn = dbtest.MigrateURL(t, dsn)
	if _, err := db.Migrate(t.Context(), dsn); err != nil {
		t.Fatal(err)
	}
	appDSN := os.Getenv("AURA_DB_URL")
	if appDSN == "" {
		t.Fatal("AURA_DB_URL required for runtime-role verification")
	}
	pool, err := pgxpool.New(t.Context(), dbtest.MigrateURL(t, appDSN))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	s := NewStore(pool)
	initial, err := s.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	d := Desired{Enabled: true, AccountID: "account", ZoneName: "example.com", PublicLabel: "aura", WARPLabel: "aura-warp"}
	state, err := s.SaveDesired(t.Context(), initial.Generation, d, "admin")
	if err != nil || state.Generation != initial.Generation+1 {
		t.Fatalf("state=%+v err=%v", state, err)
	}
	if _, err := s.SaveDesired(t.Context(), initial.Generation, d, "stale"); !errors.Is(err, ErrStaleGeneration) {
		t.Fatalf("stale save: %v", err)
	}
	state.Resources.TunnelID = "tunnel"
	state.Phase = PhaseProvisioning
	if _, err := s.Advance(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewStore(pool).Load(t.Context())
	if err != nil || reloaded.Resources.TunnelID != "tunnel" {
		t.Fatalf("resume=%+v err=%v", reloaded, err)
	}
	state.Generation--
	if _, err := s.Advance(t.Context(), state); !errors.Is(err, ErrStaleGeneration) {
		t.Fatalf("stale advance: %v", err)
	}
	secrets, err := settings.NewStore(pool, strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"CLOUDFLARE_API_TOKEN", "CLOUDFLARE_TUNNEL_TOKEN"} {
		if _, err := secrets.Upsert(t.Context(), key, "fixture-credential", ""); err != nil {
			t.Fatal(err)
		}
		var raw string
		if err := pool.QueryRow(t.Context(), "SELECT value FROM aura.settings WHERE key=$1", key).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(raw, "enc:v1:") || strings.Contains(raw, "fixture-credential") {
			t.Fatal("plaintext credential in database")
		}
		if got, err := secrets.Secret(t.Context(), key); err != nil || got != "fixture-credential" {
			t.Fatal("decrypt failed")
		}
		if err := secrets.Delete(t.Context(), key); err != nil {
			t.Fatal(err)
		}
	}
}
