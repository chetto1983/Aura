//go:build db_integration

// Integration tests for the Postgres operator profile (migration 0097). Requires a running
// Postgres with the migrations applied:
//
//	make db-migrate
//	AURA_DB_URL + AURA_DB_MIGRATE_URL + POSTGRES_PASSWORD set in env
//
// Run via:
//
//	go test -tags db_integration -race ./internal/onboarding -count=1
//
// No-skip-as-green: envOrSkip t.Fatals under $CI when the DSN is unset.
package onboarding

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/dbtest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func envOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("integration test requires %s, but it is unset under CI — "+
				"a skipped integration test must not pass as green; wire it in ci.yml", key)
		}
		t.Skipf("integration test requires %s; set it and re-run (e.g. via .env + make db-up)", key)
	}
	return v
}

func migratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	migrateURL := dbtest.MigrateURL(t, envOrSkip(t, "AURA_DB_MIGRATE_URL"))
	appURL := envOrSkip(t, "AURA_DB_URL")

	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	if err := db.EnsureRoles(ctx, fmt.Sprintf("postgres://aura:%s@%s:%s/aura?sslmode=disable", pwd, host, port), pwd); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	if _, err := db.Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	pool, err := db.Open(ctx, &db.Config{URL: appURL})
	if err != nil {
		t.Fatalf("Open (aura_app): %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// seedIdentity inserts a throwaway identity (identity_profiles is FK'd to it) and removes
// it — and its profile, by ON DELETE CASCADE — afterwards, so re-runs start clean.
func seedIdentity(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx := context.Background()
	id := uuid.NewString()
	name := "profile-test-" + id[:8]
	if _, err := pool.Exec(ctx,
		"INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'service')", id, name,
	); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "DELETE FROM aura.identities WHERE id = $1", id); err != nil {
			t.Logf("cleanup identity %s (best-effort): %v", id, err)
		}
	})
	return id
}

// The round trip the turn path depends on: what the operator typed comes back typed, and
// the timezone is readable by SQL rather than by a similarity search.
func TestProfileRoundTrip(t *testing.T) {
	pool := migratedPool(t)
	store := NewProfileStore(pool)
	id := seedIdentity(t, pool)
	ctx := context.Background()
	voice := true

	want := Answers{
		Name: "Davide", Role: "programmatore", Company: "Aura", Location: "Piacenza",
		Timezone: "Europe/Rome", Lang: "it", TonePreference: "diretto",
		ResponseLength: "breve", CustomInstructions: "cita le fonti",
		VoiceMode: &voice,
		Expertise: []string{"Go", "Postgres"},
		Stack:     []string{"ArcadeDB"},
		Projects:  []string{"Aura"},
		Goals:     []string{"produzione"},
		Interests: []string{"vela"},
		People:    []string{"Chiara"},
		Vetoes:    []string{"non scrivere email al mio posto"},
	}
	if err := store.StoreConfirmed(ctx, id, want); err != nil {
		t.Fatalf("StoreConfirmed: %v", err)
	}

	got, ok, err := store.Load(ctx, id)
	if err != nil || !ok {
		t.Fatalf("Load = %v, %v", ok, err)
	}
	if got.Name != want.Name || got.Role != want.Role || got.Timezone != want.Timezone {
		t.Errorf("scalars round-tripped as %+v, want %+v", got, want)
	}
	if got.VoiceMode == nil || !*got.VoiceMode {
		t.Errorf("VoiceMode = %v, want true", got.VoiceMode)
	}
	if got.CanProactiveMessage != nil {
		t.Errorf("CanProactiveMessage = %v, want nil — unanswered is not 'no'", *got.CanProactiveMessage)
	}
	if len(got.Vetoes) != 1 || got.Vetoes[0] != want.Vetoes[0] {
		t.Errorf("Vetoes = %v, want %v", got.Vetoes, want.Vetoes)
	}
	if zone := store.Timezone(ctx, id); zone != "Europe/Rome" {
		t.Errorf("Timezone = %q, want Europe/Rome — the clock reads this on the turn path", zone)
	}
	if block := store.ProfileBlock(ctx, id); block == "" {
		t.Error("ProfileBlock is empty for a filled profile")
	}
}

// A second submission that leaves a field blank must not erase what the first one wrote:
// the profile editor saves the form it rendered, and a partially-loaded form would
// otherwise delete the rest.
func TestProfilePartialUpdateNeverErases(t *testing.T) {
	pool := migratedPool(t)
	store := NewProfileStore(pool)
	id := seedIdentity(t, pool)
	ctx := context.Background()

	if err := store.Save(ctx, id, Answers{Name: "Davide", Company: "Aura", Expertise: []string{"Go"}}); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if err := store.Save(ctx, id, Answers{Role: "programmatore"}); err != nil {
		t.Fatalf("second save: %v", err)
	}

	got, _, err := store.Load(ctx, id)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Name != "Davide" || got.Company != "Aura" {
		t.Errorf("a partial update erased earlier answers: %+v", got)
	}
	if got.Role != "programmatore" {
		t.Errorf("Role = %q, want the updated value", got.Role)
	}
	if len(got.Expertise) != 1 || got.Expertise[0] != "Go" {
		t.Errorf("Expertise = %v, want the earlier list preserved", got.Expertise)
	}
}

// The gate: never asked → asked → answered, plus the nudge record a channel keeps here so
// it does not keep one of its own.
func TestOnboardingGateTransitions(t *testing.T) {
	pool := migratedPool(t)
	store := NewProfileStore(pool)
	ctx := context.Background()

	t.Run("never asked", func(t *testing.T) {
		id := seedIdentity(t, pool)
		st, err := store.Status(ctx, id)
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if st.Completed || st.Skipped || st.Nudged {
			t.Errorf("a fresh identity reads as %+v, want the zero gate", st)
		}
	})

	t.Run("completed", func(t *testing.T) {
		id := seedIdentity(t, pool)
		if err := store.StoreConfirmed(ctx, id, Answers{Name: "Davide"}); err != nil {
			t.Fatalf("StoreConfirmed: %v", err)
		}
		st, err := store.Status(ctx, id)
		if err != nil || !st.Completed || st.Skipped {
			t.Fatalf("Status after confirm = %+v, %v", st, err)
		}
	})

	t.Run("skipped writes no answers", func(t *testing.T) {
		id := seedIdentity(t, pool)
		if err := store.StoreSkipped(ctx, id); err != nil {
			t.Fatalf("StoreSkipped: %v", err)
		}
		st, err := store.Status(ctx, id)
		if err != nil || !st.Skipped || st.Completed {
			t.Fatalf("Status after skip = %+v, %v", st, err)
		}
		if got, _, err := store.Load(ctx, id); err != nil || got.Name != "" {
			t.Errorf("skip wrote answers: %+v (%v)", got, err)
		}
	})

	t.Run("nudge is remembered", func(t *testing.T) {
		id := seedIdentity(t, pool)
		if err := store.MarkNudged(ctx, id); err != nil {
			t.Fatalf("MarkNudged: %v", err)
		}
		st, err := store.Status(ctx, id)
		if err != nil || !st.Nudged {
			t.Fatalf("Status after nudge = %+v, %v", st, err)
		}
		// A later real answer must not lose the nudge record, and must open the gate.
		if err := store.StoreConfirmed(ctx, id, Answers{Name: "Davide"}); err != nil {
			t.Fatalf("StoreConfirmed after nudge: %v", err)
		}
		if st, err := store.Status(ctx, id); err != nil || !st.Completed || !st.Nudged {
			t.Fatalf("Status after confirm-following-nudge = %+v, %v", st, err)
		}
	})
}

// The fail-closed floor (migration 0097): without app.current_identity the row is invisible
// even to aura_app, so a forgotten WithIdentityTx cannot leak one operator's profile into
// another's context.
func TestProfileIsInvisibleWithoutIdentity(t *testing.T) {
	pool := migratedPool(t)
	store := NewProfileStore(pool)
	id := seedIdentity(t, pool)
	ctx := context.Background()

	if err := store.Save(ctx, id, Answers{Name: "Davide"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	var n int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM aura.identity_profiles WHERE identity_id = $1", id,
	).Scan(&n); err != nil {
		t.Fatalf("bare-pool count: %v", err)
	}
	if n != 0 {
		t.Errorf("profile row visible without app.current_identity (count = %d)", n)
	}

	// And another identity's transaction must not see it either.
	other := seedIdentity(t, pool)
	if err := db.WithIdentityTxRaw(ctx, pool, other, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM aura.identity_profiles WHERE identity_id = $1", id).Scan(&n)
	}); err != nil {
		t.Fatalf("cross-identity read: %v", err)
	}
	if n != 0 {
		t.Errorf("identity %s can see %s's profile (count = %d)", other, id, n)
	}
}

// An unparsable identity is a programming error, not a degraded read: it must surface
// rather than silently write nothing.
func TestProfileRejectsMalformedIdentity(t *testing.T) {
	pool := migratedPool(t)
	store := NewProfileStore(pool)
	ctx := context.Background()

	if err := store.Save(ctx, "not-a-uuid", Answers{Name: "x"}); err == nil {
		t.Error("Save accepted a malformed identity")
	}
	if _, err := store.Status(ctx, "not-a-uuid"); err == nil {
		t.Error("Status accepted a malformed identity")
	}
	if err := store.StoreConfirmed(ctx, "", Answers{Name: "x"}); err == nil {
		t.Error("StoreConfirmed accepted an empty identity")
	}
	if err := store.StoreSkipped(ctx, ""); err == nil {
		t.Error("StoreSkipped accepted an empty identity")
	}
	// The clock never fails a turn, so a bad id there is silence, not an error.
	if zone := store.Timezone(ctx, "not-a-uuid"); zone != "" {
		t.Errorf("Timezone on a malformed identity = %q, want empty", zone)
	}
}
