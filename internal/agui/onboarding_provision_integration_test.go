//go:build db_integration

// Live cross-store provisioning saga coverage (ONBD-01a/01b — the high-risk track). It
// drives the REAL onboardingService.Provision against a disposable migrated Postgres DB:
// the aura.* pool, the immutable aura.identity_audit table, and telegram_setup_pending.
// It asserts the saga's all-or-nothing guarantee against the LIVE orphan-critical stores:
// every failure-injection point (B1/B2/A/recovery/C/audit) leaves ZERO orphans, a
// double-submit yields exactly one identity, exactly one IMMUTABLE audit row per success,
// and no secret reaches a log line over a full run.
//
// Authula leg note: the agui package runs under goleak.VerifyTestMain (main_test.go), and
// the embedded Authula provider spawns long-lived database/sql + rate-limit cleanup
// goroutines that goleak (correctly) flags. Constructing the real provider here would fail
// the package goleak gate. So Leg B uses a STATEFUL Authula fake that records created/
// deleted users in-memory (so COMP_B's zero-orphan-user property is provable) with the
// SAME ordered semantics as the real adapter. The REAL Authula CoreServices Leg B
// (PasswordService.Hash + UserService.Create/Delete + AccountService.Create) is live-proven
// in internal/webauth/authula_integration_test.go + authula_multiuser_test.go; the
// composition-root adapter that wires it is cmd/aura/serve_onboarding.go.
//
// Run via (stack up, password from the container/.env):
//
//	go test -tags db_integration ./internal/agui -run 'TestProvisionSagaLive|TestProvisionIdempotent|TestIdentityAuditImmutable|TestProvisionNoSecretInLogsLive' -count=1 -p 1
//
// Requires POSTGRES_PASSWORD plus optional PGHOST/PGPORT. The test creates and drops a
// throwaway DB, so append-only audit rows never pollute the shared aura DB. No-skip-as-
// green: envOrSkip t.Fatals under $CI when required env is unset.

package agui

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestProvisionSagaLive(t *testing.T) {
	env := newLiveSagaEnv(t)

	t.Run("happy path commits all legs + one audit row", func(t *testing.T) {
		au := newStatefulAuthula()
		svc, tok, email := env.service(t, au, env.auraLeg, env.telegram)
		t.Cleanup(func() { env.cleanupProvisioned(email) })
		resp, err := svc.Provision(ownerCtx(), env.creator, tok, liveProvReq(email, "temp-pw-123"))
		if err != nil {
			t.Fatalf("Provision: %v", err)
		}
		ids, grants, links, tokens, recovery, audit := env.auraOrphans(t, email)
		if ids != 1 || grants != 1 || links != 1 || tokens != 1 || recovery != 1 || audit != 1 || au.liveUsers() != 1 {
			t.Fatalf("happy path stores: identities=%d grants=%d links=%d tokens=%d recovery=%d audit=%d authula=%d",
				ids, grants, links, tokens, recovery, audit, au.liveUsers())
		}
		if resp.DeepLink == "" || resp.QRSVG == "" {
			t.Error("happy path must return deep-link + QR")
		}
		if strings.Contains(resp.QRSVG, "temp-pw-123") {
			t.Error("QR SVG leaked the password")
		}
	})

	inject := []struct {
		name  string
		auFn  func() *statefulAuthula
		legFn func() AuraLegWriter
		tgFn  func() TelegramMint
		recFn func() RecoverySetupWriter
	}{
		{"B1 create-user fails", func() *statefulAuthula { a := newStatefulAuthula(); a.failCreateUser = true; return a },
			func() AuraLegWriter { return env.auraLeg }, func() TelegramMint { return env.telegram }, func() RecoverySetupWriter { return env.recovery }},
		{"B2 create-account fails", func() *statefulAuthula { a := newStatefulAuthula(); a.failCreateAcct = true; return a },
			func() AuraLegWriter { return env.auraLeg }, func() TelegramMint { return env.telegram }, func() RecoverySetupWriter { return env.recovery }},
		{"A aura-leg fails", newStatefulAuthula,
			func() AuraLegWriter { return faultAuraLeg{AuraLegWriter: env.auraLeg, failCreate: true} }, func() TelegramMint { return env.telegram }, func() RecoverySetupWriter { return env.recovery }},
		{"recovery write fails", newStatefulAuthula,
			func() AuraLegWriter { return env.auraLeg }, func() TelegramMint { return env.telegram },
			func() RecoverySetupWriter { return faultRecovery{RecoverySetupWriter: env.recovery, fail: true} }},
		{"C telegram mint fails", newStatefulAuthula,
			func() AuraLegWriter { return env.auraLeg }, func() TelegramMint { return faultTelegram{TelegramMint: env.telegram, fail: true} }, func() RecoverySetupWriter { return env.recovery }},
		{"audit write fails", newStatefulAuthula,
			func() AuraLegWriter { return faultAuraLeg{AuraLegWriter: env.auraLeg, failAudit: true} }, func() TelegramMint { return env.telegram }, func() RecoverySetupWriter { return env.recovery }},
	}
	for _, tc := range inject {
		t.Run(tc.name+" -> zero orphans", func(t *testing.T) {
			au := tc.auFn()
			svc, tok, email := env.service(t, au, tc.legFn(), tc.tgFn())
			svc.recovery = tc.recFn()
			t.Cleanup(func() { env.cleanupProvisioned(email) })
			if _, err := svc.Provision(ownerCtx(), env.creator, tok, liveProvReq(email, "temp-pw-123")); err == nil {
				t.Fatalf("%s: provision must error", tc.name)
			}
			ids, grants, links, tokens, recovery, audit := env.auraOrphans(t, email)
			if ids != 0 || grants != 0 || links != 0 || tokens != 0 || recovery != 0 || audit != 0 || au.liveUsers() != 0 {
				t.Fatalf("%s LEFT ORPHANS: identities=%d grants=%d links=%d tokens=%d recovery=%d audit=%d authula=%d",
					tc.name, ids, grants, links, tokens, recovery, audit, au.liveUsers())
			}
		})
	}
}

// TestProvisionIdempotent proves a double-submit (same email) yields exactly one identity +
// a clean ErrOnboardingDuplicate (the aura.identities NOT NULL UNIQUE name 23505), never a
// 2nd identity, and the 2nd attempt leaves no orphan Authula user.
func TestProvisionIdempotent(t *testing.T) {
	env := newLiveSagaEnv(t)
	au := newStatefulAuthula()
	svc, tok, email := env.service(t, au, env.auraLeg, env.telegram)
	t.Cleanup(func() { env.cleanupProvisioned(email) })

	if _, err := svc.Provision(ownerCtx(), env.creator, tok, liveProvReq(email, "temp-pw-123")); err != nil {
		t.Fatalf("first provision: %v", err)
	}

	// Second provision with the SAME email on a fresh session: the Authula pre-check sees
	// the existing user → ErrOnboardingDuplicate (no write); one identity remains.
	svc2, tok2, _ := env.service(t, au, env.auraLeg, env.telegram)
	_, err := svc2.Provision(ownerCtx(), env.creator, tok2, liveProvReq(email, "temp-pw-123"))
	if !errors.Is(err, ErrOnboardingDuplicate) {
		t.Fatalf("double-submit err = %v, want ErrOnboardingDuplicate", err)
	}
	var count int
	if err := env.pool.QueryRow(ownerCtx(),
		`SELECT count(*) FROM aura.identities WHERE name=$1`, email).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("double-submit produced %d identities, want exactly 1", count)
	}
	if au.liveUsers() != 1 {
		t.Fatalf("double-submit left %d Authula users, want 1 (no orphan from the 2nd attempt)", au.liveUsers())
	}
}

// TestIdentityAuditImmutable proves exactly one immutable audit row on success and that it
// cannot be mutated (UPDATE/DELETE rejected by the append-only trigger, T-28-05-06). A
// rolled-back flow (B1) writes none.
func TestIdentityAuditImmutable(t *testing.T) {
	env := newLiveSagaEnv(t)

	au := newStatefulAuthula()
	svc, tok, email := env.service(t, au, env.auraLeg, env.telegram)
	t.Cleanup(func() { env.cleanupProvisioned(email) })
	if _, err := svc.Provision(ownerCtx(), env.creator, tok, liveProvReq(email, "temp-pw-123")); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	ctx := ownerCtx()
	var auditID string
	if err := env.pool.QueryRow(ctx,
		`SELECT id::text FROM aura.identity_audit WHERE new_identity_name=$1`, email).Scan(&auditID); err != nil {
		t.Fatalf("read audit row: %v", err)
	}
	if _, err := env.pool.Exec(ctx, `UPDATE aura.identity_audit SET new_identity_name='x' WHERE id=$1`, auditID); err == nil {
		t.Error("UPDATE on identity_audit succeeded — must be append-only")
	}
	if _, err := env.pool.Exec(ctx, `DELETE FROM aura.identity_audit WHERE id=$1`, auditID); err == nil {
		t.Error("DELETE on identity_audit succeeded — must be append-only")
	}

	// A rolled-back flow (B1) writes no audit row.
	au2 := newStatefulAuthula()
	au2.failCreateUser = true
	svc2, tok2, email2 := env.service(t, au2, env.auraLeg, env.telegram)
	t.Cleanup(func() { env.cleanupProvisioned(email2) })
	_, _ = svc2.Provision(ctx, env.creator, tok2, liveProvReq(email2, "temp-pw-123"))
	var rolledBackAudit int
	_ = env.pool.QueryRow(ctx, `SELECT count(*) FROM aura.identity_audit WHERE new_identity_name=$1`, email2).Scan(&rolledBackAudit)
	if rolledBackAudit != 0 {
		t.Fatalf("rolled-back flow wrote %d audit rows, want 0", rolledBackAudit)
	}
}

// TestProvisionNoSecretInLogsLive captures slog over a full LIVE provision run and asserts
// the Authula password never appears in any log line — the cross-cutting no-leak guarantee
// exercised against the real aura stores.
func TestProvisionNoSecretInLogsLive(t *testing.T) {
	env := newLiveSagaEnv(t)
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	const secret = "L1ve-Sup3r-Secret-PW!"
	au := newStatefulAuthula()
	svc, tok, email := env.service(t, au, env.auraLeg, env.telegram)
	t.Cleanup(func() { env.cleanupProvisioned(email) })
	if _, err := svc.Provision(ownerCtx(), env.creator, tok, liveProvReq(email, secret)); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if strings.Contains(buf.String(), secret) {
		t.Fatalf("Authula password leaked into a log line over a live run:\n%s", buf.String())
	}
}
