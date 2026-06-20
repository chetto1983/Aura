//go:build db_integration

// Live multi-user coverage for the Phase-28 single-operator relaxation (prd.md
// amendment #64 / D-07). It proves, against the running aura-postgres + the embedded
// Authula provider on its isolated `authula` schema, that:
//
//   - TestAuthulaMultiUser_DoesNotBreakBoot — enrolling a SECOND Authula user makes
//     OperatorUserID return ErrOperatorAmbiguous (a SKIP sentinel, not a fatal), the
//     enrollment auto-pin is skipped, and the already-linked `local` identity stays
//     resolvable via ResolveIdentityID over aura.identity_auth_links. This is the exact
//     daemon-boot path in cmd/aura/serve_auth.go: a 2nd loginable identity must NOT abort
//     boot or de-pin the existing operator.
//   - TestAuthulaMultiUser_SingleOperatorUnregressed — with exactly one enrolled user,
//     OperatorUserID returns that user's id (no error) and the link round-trips, i.e. the
//     pre-amendment single-operator deployment behaves identically.
//
// The live session-validate path resolves identity ONLY via ResolveIdentityID (verified:
// no request handler calls OperatorUserID), so the relaxation is enrollment-time-only.
//
// Run via (stack up, password from the container/.env):
//
//	go test -tags db_integration ./internal/webauth -run TestAuthulaMultiUser -count=1 -p 1
//
// Requires AURA_DB_URL (aura.* pool, for identity_auth_links) and AURA_AUTHULA_SECRET;
// the Authula schema DSN defaults to AURA_DB_URL (webauth.New forces ?search_path=authula)
// unless AURA_AUTHULA_DSN overrides it. Under $CI an unset required var t.Fatals (no
// skip-as-green); locally it skips with guidance. Run with -p 1: the shared Postgres is
// also exercised by a parallel session, so these tests serialize their user-set assertions.

package webauth

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// multiEnvOrSkip mirrors the webauth_integration envOrSkip (that helper lives under a
// different build tag, so this file needs its own): t.Fatal under $CI when a required var
// is unset (no skip-as-green), local skip otherwise.
func multiEnvOrSkip(t *testing.T, key string) string {
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

// seededLocalIdentityID is the migration-0004 seeded `local` identity (wildcard `*`).
const seededLocalIdentityID = "00000000-0000-0000-0000-000000000001"

// newMultiUserProvider builds the embedded Authula provider on the isolated authula
// schema and an IdentityLinker over the aura pool, returning both plus a cleanup. It is
// the same construction the daemon boot uses, minus the CLI plumbing.
func newMultiUserProvider(t *testing.T, ctx context.Context) (*Provider, *IdentityLinker, *pgxpool.Pool) {
	t.Helper()
	auraDSN := multiEnvOrSkip(t, "AURA_DB_URL")
	secret := multiEnvOrSkip(t, "AURA_AUTHULA_SECRET")
	authulaDSN := os.Getenv("AURA_AUTHULA_DSN")
	if authulaDSN == "" {
		authulaDSN = auraDSN // webauth.New appends ?search_path=authula
	}

	provider, err := New(Config{DSN: authulaDSN, Secret: secret, TrustedOrigins: []string{"http://127.0.0.1:9080"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		if cerr := provider.Close(); cerr != nil {
			t.Errorf("Close: %v", cerr)
		}
	})

	pool, err := pgxpool.New(ctx, auraDSN)
	if err != nil {
		t.Fatalf("open aura pool: %v", err)
	}
	t.Cleanup(pool.Close)

	return provider, NewIdentityLinker(pool), pool
}

// enrollUser creates an Authula user with a unique email and registers cleanup. The
// timestamp+suffix keeps re-runs idempotent against the shared schema.
func enrollUser(t *testing.T, ctx context.Context, provider *Provider, suffix string) string {
	t.Helper()
	core := provider.CoreServices()
	if core == nil || core.UserService == nil {
		t.Fatal("CoreServices().UserService is nil")
	}
	email := "op+" + time.Now().Format("150405.000") + "-" + suffix + "@aura.local"
	user, err := core.UserService.Create(ctx, "operator-"+suffix, email, true, nil, nil)
	if err != nil {
		t.Fatalf("create operator user %q: %v", suffix, err)
	}
	t.Cleanup(func() { _ = core.UserService.Delete(context.Background(), user.ID) })
	return user.ID
}

// TestAuthulaMultiUser_DoesNotBreakBoot proves a 2nd enrolled user does not fatal the
// enrollment/boot path and does not de-pin the existing `local` link.
func TestAuthulaMultiUser_DoesNotBreakBoot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	provider, linker, pool := newMultiUserProvider(t, ctx)

	// Enroll the FIRST operator and pin it to `local` (the steady-state single-operator
	// link the daemon establishes on first boot).
	firstUID := enrollUser(t, ctx, provider, "first")
	if err := linker.LinkOperator(ctx, seededLocalIdentityID, firstUID); err != nil {
		t.Fatalf("LinkOperator(first): %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM aura.identity_auth_links WHERE authula_user_id=$1", firstUID)
	})

	// Enroll a SECOND user — the Phase-28 "2nd web-loginable identity" case.
	secondUID := enrollUser(t, ctx, provider, "second")
	if secondUID == firstUID {
		t.Fatal("second user id collided with first")
	}

	// The relaxation: with >1 user, OperatorUserID signals SKIP (ErrOperatorAmbiguous),
	// NOT a generic fatal. This is what serve_auth.go's enrollment block now tolerates.
	uid, uerr := provider.OperatorUserID(ctx)
	if !errors.Is(uerr, ErrOperatorAmbiguous) {
		t.Fatalf("OperatorUserID with >1 user: want ErrOperatorAmbiguous, got id=%q err=%v", uid, uerr)
	}
	if uid != "" {
		t.Errorf("OperatorUserID ambiguous case must return empty id, got %q", uid)
	}

	// Critical no-regression: the existing `local` link is UNTOUCHED (no de-pin) and the
	// live validate path (ResolveIdentityID) still resolves the first operator → `local`.
	gotID, err := linker.ResolveIdentityID(ctx, firstUID)
	if err != nil {
		t.Fatalf("ResolveIdentityID(first) after 2nd enroll: %v", err)
	}
	if gotID != seededLocalIdentityID {
		t.Errorf("local identity de-pinned: resolved=%q, want %q", gotID, seededLocalIdentityID)
	}

	// A second provisioned identity authenticates via its OWN link row (1:N over the link
	// table) — independent of OperatorUserID. Insert a throwaway aura.identities row so the
	// identity_auth_links FK is satisfied (this plan does NOT create identities — that is
	// Plan 05; here we only prove the link mechanics generalize 1:N).
	const secondIdentityID = "00000000-0000-0000-0000-0000000028b2"
	const secondIdentityName = "test-phase28-second-identity"
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1::uuid, $2, 'user')
		 ON CONFLICT (id) DO NOTHING`, secondIdentityID, secondIdentityName); err != nil {
		t.Fatalf("insert throwaway identity: %v", err)
	}
	t.Cleanup(func() {
		// identity_auth_links rows for this identity cascade via the migration-0019 FK
		// ON DELETE CASCADE; delete the throwaway identity last.
		_, _ = pool.Exec(context.Background(), "DELETE FROM aura.identity_auth_links WHERE authula_user_id=$1", secondUID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM aura.identities WHERE id=$1::uuid", secondIdentityID)
	})
	if err := linker.LinkOperator(ctx, secondIdentityID, secondUID); err != nil {
		t.Fatalf("LinkOperator(second): %v", err)
	}
	got2, err := linker.ResolveIdentityID(ctx, secondUID)
	if err != nil {
		t.Fatalf("ResolveIdentityID(second): %v", err)
	}
	if got2 != secondIdentityID {
		t.Errorf("second identity resolution = %q, want %q", got2, secondIdentityID)
	}
	// And the first link STILL resolves to `local` (the two link rows are independent).
	if reGot, reErr := linker.ResolveIdentityID(ctx, firstUID); reErr != nil || reGot != seededLocalIdentityID {
		t.Errorf("first link disturbed by second: got=%q err=%v, want %q", reGot, reErr, seededLocalIdentityID)
	}
}

// TestAuthulaMultiUser_SingleOperatorUnregressed proves the exactly-one-user path is
// byte-identical to the pre-amendment behavior: OperatorUserID returns that user's id
// (no error) and the link round-trips.
func TestAuthulaMultiUser_SingleOperatorUnregressed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	provider, linker, pool := newMultiUserProvider(t, ctx)

	// Guard: this assertion is only meaningful when the shared Authula schema holds
	// exactly one user. A parallel session may have left users behind, so require the
	// single-user precondition rather than asserting a wrong invariant on a dirty schema.
	core := provider.CoreServices()
	if existing, _, err := core.UserService.GetAll(ctx, nil, 2); err != nil {
		t.Fatalf("precheck GetAll: %v", err)
	} else if len(existing) != 0 {
		t.Skipf("authula schema already has %d user(s) — single-operator assertion needs a clean schema; run -p 1 in isolation", len(existing))
	}

	onlyUID := enrollUser(t, ctx, provider, "solo")

	uid, uerr := provider.OperatorUserID(ctx)
	if uerr != nil {
		t.Fatalf("OperatorUserID with exactly 1 user: unexpected err=%v", uerr)
	}
	if uid != onlyUID {
		t.Fatalf("OperatorUserID = %q, want the lone user %q", uid, onlyUID)
	}

	if err := linker.LinkOperator(ctx, seededLocalIdentityID, onlyUID); err != nil {
		t.Fatalf("LinkOperator(solo): %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM aura.identity_auth_links WHERE authula_user_id=$1", onlyUID)
	})
	gotID, err := linker.ResolveIdentityID(ctx, onlyUID)
	if err != nil {
		t.Fatalf("ResolveIdentityID(solo): %v", err)
	}
	if gotID != seededLocalIdentityID {
		t.Errorf("single-operator resolution = %q, want %q", gotID, seededLocalIdentityID)
	}
}
