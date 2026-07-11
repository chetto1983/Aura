//go:build db_integration

// Edge subtests for the offline break-glass flow, split from breakglass_integration_test.go
// to keep each file under the 600-LOC cap (CLAUDE.md). Same //go:build db_integration tag,
// same package, same throwaway-DB harness — proves the guard no-writes (R2), the
// missing-account clean failure, and --no-recovery (D-04 opt-out).
package breakglass

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/webauth"
)

// TestRecoverOperatorGuardNoWrite proves R2: 0 and >1 active operators each make
// RecoverOperator error BEFORE any write — authula.accounts and aura.identity_recovery
// stay empty, and no audit row is appended.
func TestRecoverOperatorGuardNoWrite(t *testing.T) {
	t.Run("zero operators", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()
		pool, core := setupBreakglassDB(t, ctx)

		// Only the migration-seeded `local` (kind='system') exists — no kind='user' operator.
		if _, err := RecoverOperator(ctx, Deps{Pool: pool, Core: core}, Secrets{
			Password: bgNewPassword, Question: bgQuestion, Answer: bgAnswer,
		}); err == nil {
			t.Fatal("RecoverOperator with zero operators succeeded; want a guard refusal")
		}
		if got := countRows(t, ctx, pool, `SELECT count(*) FROM authula.accounts`); got != 0 {
			t.Fatalf("authula.accounts = %d after refusal, want 0 (no write)", got)
		}
		if got := countRows(t, ctx, pool, `SELECT count(*) FROM aura.identity_recovery`); got != 0 {
			t.Fatalf("aura.identity_recovery = %d after refusal, want 0 (no write)", got)
		}
		if got := countRows(t, ctx, pool, `SELECT count(*) FROM aura.identity_recovery_audit`); got != 0 {
			t.Fatalf("aura.identity_recovery_audit = %d after refusal, want 0 (no write)", got)
		}
	})

	t.Run("two active operators", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()
		pool, core := setupBreakglassDB(t, ctx)

		// Two ACTIVE kind='user' operators — selectSoleOperator refuses the ambiguity.
		seedUserIdentity(t, ctx, pool, "op-a-"+uuid.NewString()[:8]+"@example.test")
		seedUserIdentity(t, ctx, pool, "op-b-"+uuid.NewString()[:8]+"@example.test")

		if _, err := RecoverOperator(ctx, Deps{Pool: pool, Core: core}, Secrets{
			Password: bgNewPassword, Question: bgQuestion, Answer: bgAnswer,
		}); err == nil {
			t.Fatal("RecoverOperator with two active operators succeeded; want a guard refusal")
		}
		if got := countRows(t, ctx, pool, `SELECT count(*) FROM authula.accounts`); got != 0 {
			t.Fatalf("authula.accounts = %d after refusal, want 0 (no write)", got)
		}
		if got := countRows(t, ctx, pool, `SELECT count(*) FROM aura.identity_recovery`); got != 0 {
			t.Fatalf("aura.identity_recovery = %d after refusal, want 0 (no write)", got)
		}
		if got := countRows(t, ctx, pool, `SELECT count(*) FROM aura.identity_recovery_audit`); got != 0 {
			t.Fatalf("aura.identity_recovery_audit = %d after refusal, want 0 (no write)", got)
		}
	})
}

// TestRecoverOperatorMissingAccount proves the missing-related-row edge: an operator with
// an identity_auth_links row but NO authula.accounts row → a clear error and NO partial
// write (no session delete, no reseed, no audit).
func TestRecoverOperatorMissingAccount(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	pool, core := setupBreakglassDB(t, ctx)

	email := "orphan-" + uuid.NewString()[:8] + "@example.test"
	identityID := seedUserIdentity(t, ctx, pool, email)
	// Link to a bogus Authula user id — no matching authula.users/accounts row exists.
	if err := webauth.NewIdentityLinker(pool).LinkOperator(ctx, identityID, "authula-user-does-not-exist"); err != nil {
		t.Fatalf("seed dangling auth link: %v", err)
	}

	if _, err := RecoverOperator(ctx, Deps{Pool: pool, Core: core}, Secrets{
		Password: bgNewPassword, Question: bgQuestion, Answer: bgAnswer,
	}); err == nil {
		t.Fatal("RecoverOperator with a missing Authula account succeeded; want a clear error")
	}

	if got := countRows(t, ctx, pool, `SELECT count(*) FROM aura.identity_recovery WHERE identity_id=$1`, mustUUID(t, identityID)); got != 0 {
		t.Fatalf("aura.identity_recovery = %d after the missing-account failure, want 0 (no partial write)", got)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM aura.identity_recovery_audit WHERE identity_id=$1`, mustUUID(t, identityID)); got != 0 {
		t.Fatalf("aura.identity_recovery_audit = %d after the missing-account failure, want 0 (no partial write)", got)
	}
}

// TestRecoverOperatorNoRecovery proves --no-recovery (D-04): the password reset + session
// kill + neutral audit still run, but NO identity_recovery row is written.
func TestRecoverOperatorNoRecovery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	pool, core := setupBreakglassDB(t, ctx)

	email := "operator-" + uuid.NewString()[:8] + "@example.test"
	identityID, authulaUserID := seedOperatorLockout(t, ctx, pool, core, email)

	if _, err := RecoverOperator(ctx, Deps{Pool: pool, Core: core, NoRecovery: true}, Secrets{
		Password: bgNewPassword, // Question/Answer intentionally omitted — NoRecovery skips the re-seed.
	}); err != nil {
		t.Fatalf("RecoverOperator --no-recovery: %v", err)
	}

	// Password reset + session kill still happened.
	if !verifyStoredPassword(t, ctx, core, authulaUserID, bgNewPassword) {
		t.Fatal("--no-recovery: the reset password does not verify")
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM authula.sessions WHERE user_id=$1`, authulaUserID); got != 0 {
		t.Fatalf("--no-recovery: sessions = %d, want 0", got)
	}
	// The neutral audit row still lands.
	assertSingleNeutralAudit(t, ctx, pool, identityID)
	// But NO identity_recovery row was written.
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM aura.identity_recovery WHERE identity_id=$1`, mustUUID(t, identityID)); got != 0 {
		t.Fatalf("--no-recovery: identity_recovery = %d, want 0 (re-seed skipped)", got)
	}
}
