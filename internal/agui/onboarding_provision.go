package agui

import (
	"context"
	"errors"
	"time"
)

// onboarding_provision.go is the ordered cross-store provisioning saga (ONBD-01a/01b /
// RESEARCH §Hard Problem 1). Provisioning a loginable identity spans THREE independent
// stores that cannot share a transaction — the aura.* pgx pool, the Authula `authula`
// schema on its OWN database/sql pool, and the Telegram mint token — so atomicity is a
// saga with per-leg compensation, NOT a single tx.
//
// The saga consumes narrow consumer-side ports (declared here) so this package stays free
// of the internal/channels/telegram import (which would cycle: telegram imports agui). The
// composition root (cmd/aura/serve.go) wires the concrete adapters; tests inject fakes OR
// real adapters over the live stores.
//
// Order (RESEARCH §Hard Problem 1):
//
//	0. pre-validate (no writes): creator HasCapability(identity.create); requested caps ⊆
//	   creator-grants AND no '*'; email non-empty + Authula GetByEmail==none.
//	1. Leg B (Authula, fails cheapest on dup email): Hash → CreateUser → CreateAccount;
//	   COMP_B = DeleteUser.
//	2. Leg A (aura, one db.WithTx): INSERT identity + GrantCapability per cap + LinkOperator;
//	   on failure → COMP_B.
//	3. Leg C (Telegram mint): InsertPending(new identity, +1h); on failure → DeleteIdentity
//	   + COMP_B.
//	4. one immutable identity_audit row (a tiny final db.WithTx AFTER Leg C — RESEARCH L8:
//	   exactly one row, ONLY on full success; a rolled-back flow has none).
//
// Then (ONBD-02) the confirmed interview Agent.md is written for the NEW identity id; a
// skipped interview writes nothing. The Telegram CONSUME is async (the user scans later);
// an unscanned token simply expires (1h TTL) — "identity created" = legs A+B+C committed.

// onboardingTokenTTL is the Telegram onboarding-token lifetime (matches the setup wizard's
// 1h TTL). An unscanned token expires and is GC'd; it never leaves a half-linked identity.
const onboardingTokenTTL = time.Hour

// authulaUser is the minimal projection of a created Authula user the saga needs (the id
// for the link + compensation, the email echoed into the account). Declared consumer-side
// so the port returns plain fields, not the Authula model type.
type authulaUser struct {
	ID    string
	Email string
}

// authulaCore is the narrow port over Authula's CoreServices (Leg B + compensation). The
// composition-root adapter wraps *authulaservices.CoreServices (PasswordService.Hash +
// UserService.GetByEmail/Create/Delete + AccountService.Create). The saga calls these
// directly, bypassing the DisableSignUp use-case (verified reachable — RESEARCH §Authula).
type authulaCore interface {
	// UserByEmail returns (found, error). A found user is a duplicate → the saga fails the
	// pre-validate with a clean 409 before any write.
	UserByEmail(ctx context.Context, email string) (bool, error)
	// HashPassword hashes the write-only initial password (never persisted/echoed raw).
	HashPassword(password string) (string, error)
	// CreateUser creates the Authula user (Leg B step 1). name=email, emailVerified=true.
	CreateUser(ctx context.Context, email string) (authulaUser, error)
	// CreateAccount attaches the hashed password as the email-provider account (Leg B
	// step 2).
	CreateAccount(ctx context.Context, userID, email, passwordHash string) error
	// DeleteUser is COMP_B: it removes the Authula user (the account cascades). Used to
	// compensate a failed Leg A/C.
	DeleteUser(ctx context.Context, userID string) error
}

// auraLegParams carries the aura-leg write inputs.
type auraLegParams struct {
	IdentityName    string
	Capabilities    []string
	AuthulaUserID   string
	ActorIdentityID string
}

// auraLegWriter is the narrow port over the aura.* leg (Leg A + the final audit row +
// compensation). The composition-root adapter implements these with db.WithTx over the
// shared pool. CreateIdentityWithGrants does the INSERT identity + GrantCapability per cap
// + LinkOperator in ONE tx (internally atomic), returning the new identity UUID. A
// duplicate name surfaces errOnboardingDuplicate (the aura.identities NOT NULL UNIQUE name
// → 23505). WriteAuditRow writes exactly one immutable identity_audit row in its own tiny
// tx (RESEARCH L8). DeleteIdentity compensates Leg A (grants + link cascade via FK).
type auraLegWriter interface {
	CreateIdentityWithGrants(ctx context.Context, p auraLegParams) (identityID string, err error)
	WriteAuditRow(ctx context.Context, p auraLegParams, newIdentityID string) error
	DeleteIdentity(ctx context.Context, identityName string) error
}

// telegramMint is the narrow port over the Telegram mint/poll (Leg C + the status poll).
// The composition-root adapter wraps *telegram.Store (InsertPending + PendingConsumed),
// avoiding the telegram→agui import cycle. The consume is async (channel-side), never here.
type telegramMint interface {
	InsertPending(ctx context.Context, onboardingToken, identityID string, expiresAt time.Time) error
	PendingConsumed(ctx context.Context, onboardingToken string) (bool, error)
}

// errProvisioningUnavailable is returned when a provisioning port is unwired (an
// interview-only deployment). The handler renders it as a sanitized 502.
var errProvisioningUnavailable = errors.New("onboarding: provisioning backend not configured")

// Provision is implemented in Task 2 (the ordered saga + compensation). This Task-1
// placeholder makes the package build with the interview side wired; until the saga lands,
// a provision attempt is a sanitized backend-unavailable error.
func (s *onboardingService) Provision(_ context.Context, _ string, _ OnboardingProvisionRequest) (OnboardingProvisionResponse, error) {
	return OnboardingProvisionResponse{}, errProvisioningUnavailable
}

// TelegramStatus is implemented in Task 2 (the PendingConsumed poll). This Task-1
// placeholder makes the package build with the interview side wired.
func (s *onboardingService) TelegramStatus(_ context.Context, _ string) (OnboardingTelegramStatus, error) {
	return OnboardingTelegramStatus{}, errProvisioningUnavailable
}
