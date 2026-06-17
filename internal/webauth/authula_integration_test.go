//go:build webauth_integration

// Live integration coverage for the embedded Authula provider against the running
// aura-postgres (the construction + lifecycle + DB paths a unit test cannot reach):
// New (builds its own pool on search_path=authula, runs Authula's migrations into the
// isolated schema), Handler/CoreServices, OperatorUserID, the IdentityLinker round-trip
// over aura.identity_auth_links, and the end-to-end Validate seam (mint a real session
// via SessionService.Create, present its raw token cookie, expect the mapped Aura
// identity UUID). Closes goleak-style at the end.
//
// Run via (stack up, password from the container/.env):
//
//	go test -tags webauth_integration ./internal/webauth -run TestAuthulaProvider -count=1
//
// Requires AURA_AUTHULA_DSN (Postgres DSN for the authula schema, e.g. the aura_app DSN)
// and AURA_AUTHULA_SECRET. Under $CI an unset var t.Fatals (no skip-as-green); locally it
// skips with guidance.

package webauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func envOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("integration test requires %s, but it is unset under CI — a skipped "+
				"integration test must not pass as green; wire it in ci.yml", key)
		}
		t.Skipf("integration test requires %s; set it and re-run (stack up)", key)
	}
	return v
}

func TestAuthulaProvider_EndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	dsn := envOrSkip(t, "AURA_AUTHULA_DSN")
	secret := envOrSkip(t, "AURA_AUTHULA_SECRET")
	auraDSN := envOrSkip(t, "AURA_DB_URL") // for the identity-link table (aura.*)

	// Construct the provider: builds its own pool on search_path=authula and runs
	// Authula's core + plugin migrations into the isolated schema.
	provider, err := New(Config{DSN: dsn, Secret: secret, TrustedOrigins: []string{"http://127.0.0.1:9080"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		if cerr := provider.Close(); cerr != nil {
			t.Errorf("Close: %v", cerr)
		}
	})

	if provider.Handler() == nil {
		t.Fatal("Handler() returned nil")
	}
	core := provider.CoreServices()
	if core == nil || core.UserService == nil || core.SessionService == nil || core.TokenService == nil {
		t.Fatal("CoreServices missing required services")
	}

	// Enroll a single operator user directly (sign-up is disabled in prod config).
	// A unique email per run keeps re-runs idempotent-ish against the shared schema.
	email := "operator+" + time.Now().Format("150405.000") + "@aura.local"
	user, err := core.UserService.Create(ctx, "operator", email, true, nil, nil)
	if err != nil {
		t.Fatalf("create operator user: %v", err)
	}
	t.Cleanup(func() { _ = core.UserService.Delete(context.Background(), user.ID) })

	// OperatorUserID resolves the single (or one of the) enrolled user(s). With a shared
	// schema other runs may have left users, so accept either the lone-user id or the
	// ambiguous-multi error — both are correct behaviors; the important assertion is the
	// no-panic + the link round-trip below.
	if uid, uerr := provider.OperatorUserID(ctx); uerr == nil && uid == "" {
		t.Fatal("OperatorUserID returned empty after enrolling a user")
	}

	// Link the operator user to the seeded `local` identity over aura.* and resolve it.
	pool, err := pgxpool.New(ctx, auraDSN)
	if err != nil {
		t.Fatalf("open aura pool: %v", err)
	}
	defer pool.Close()
	const localID = "00000000-0000-0000-0000-000000000001"
	linker := NewIdentityLinker(pool)
	if err := linker.LinkOperator(ctx, localID, user.ID); err != nil {
		t.Fatalf("LinkOperator: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM aura.identity_auth_links WHERE authula_user_id=$1", user.ID)
	})
	gotID, err := linker.ResolveIdentityID(ctx, user.ID)
	if err != nil {
		t.Fatalf("ResolveIdentityID: %v", err)
	}
	if gotID != localID {
		t.Errorf("resolved identity = %q, want %q", gotID, localID)
	}

	// Mint a REAL session via SessionService.Create (store its SHA-256 hash), present the
	// raw token as the hardened cookie, and assert Validate maps it to the Aura identity.
	rawToken, err := core.TokenService.Generate()
	if err != nil {
		t.Fatalf("token generate: %v", err)
	}
	hashed := core.TokenService.Hash(rawToken)
	if _, err := core.SessionService.Create(ctx, user.ID, hashed, nil, nil, time.Hour); err != nil {
		t.Fatalf("session create: %v", err)
	}

	validator := NewValidator(provider, linker)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: rawToken})
	identityID, err := validator.Validate(req)
	if err != nil {
		t.Fatalf("Validate (live session): %v", err)
	}
	if identityID != localID {
		t.Errorf("validated identity = %q, want %q", identityID, localID)
	}

	// A bogus cookie value fails closed.
	bogus := httptest.NewRequest(http.MethodGet, "/", nil)
	bogus.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "not-a-real-token"})
	if _, err := validator.Validate(bogus); err == nil {
		t.Error("Validate accepted a bogus token — must fail closed")
	}
}
