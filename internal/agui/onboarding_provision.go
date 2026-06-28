package agui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/profile"
)

// onboarding_provision.go is the ordered cross-store provisioning saga (ONBD-01a/01b /
// RESEARCH §Hard Problem 1). Provisioning a loginable identity spans FOUR independent
// stores that cannot share a transaction — the aura.* pgx pool, the Authula `authula`
// schema on its OWN database/sql pool, the recovery challenge row, and the Telegram mint
// token — so atomicity is a saga with per-leg compensation, NOT a single tx.
//
// The saga consumes narrow consumer-side ports (declared here) so this package stays free
// of the internal/channels/telegram import (which would cycle: telegram imports agui). The
// composition root (cmd/aura/serve.go) wires the concrete adapters; tests inject fakes OR
// real adapters over the live stores.
//
// Order (RESEARCH §Hard Problem 1):
//
//	0. pre-validate (no writes): creator HasCapability(identity.create); requested caps ⊆
//	   creator-grants AND no '*'; valid request shape + Authula GetByEmail==none.
//	1. Leg B (Authula, fails cheapest on dup email): Hash → CreateUser → CreateAccount;
//	   COMP_B = DeleteUser.
//	2. Leg A (aura, one db.WithTx): INSERT identity + GrantCapability per cap + LinkOperator;
//	   on failure → COMP_B.
//	3. Recovery setup: hash answer + upsert challenge; on failure → DeleteIdentity + COMP_B.
//	4. Leg C (Telegram mint): InsertPending(new identity, +1h); on failure → DeleteIdentity
//	   + COMP_B.
//	5. one immutable identity_audit row (a tiny final db.WithTx AFTER Leg C); on failure
//	   DeletePending + DeleteIdentity + COMP_B so a rolled-back flow has none.
//
// Then (ONBD-02) the confirmed interview Agent.md is written for the NEW identity id; a
// skipped interview writes nothing. The Telegram CONSUME is async (the user scans later);
// an unscanned token simply expires (1h TTL) — "identity created" = B+A+recovery+C+audit committed.

// onboardingTokenTTL is the Telegram onboarding-token lifetime (matches the setup wizard's
// 1h TTL). An unscanned token expires and is GC'd; it never leaves a half-linked identity.
const onboardingTokenTTL = time.Hour

// identityCreateCapability is the capability_grants name the create mutation is gated on
// (ONBD-01a / D-04, parity with agent.run). The route mount enforces it via
// RequireCapability; the saga re-checks it (belt-and-suspenders) so the creator must hold
// identity.create (or the '*' wildcard) to provision. The seeded `local` identity holds
// '*' and so passes.
const identityCreateCapability = "identity.create"

// AuthulaUser is the minimal projection of a created Authula user the saga needs (the id
// for the link + compensation, the email echoed into the account). Declared consumer-side
// so the port returns plain fields, not the Authula model type.
type AuthulaUser struct {
	ID    string
	Email string
}

// AuthulaCore is the narrow port over Authula's CoreServices (Leg B + compensation). The
// composition-root adapter wraps *authulaservices.CoreServices (PasswordService.Hash +
// UserService.GetByEmail/Create/Delete + AccountService.Create). The saga calls these
// directly, bypassing the DisableSignUp use-case (verified reachable — RESEARCH §Authula).
type AuthulaCore interface {
	// UserByEmail returns (found, error). A found user is a duplicate → the saga fails the
	// pre-validate with a clean 409 before any write.
	UserByEmail(ctx context.Context, email string) (bool, error)
	// HashPassword hashes the write-only initial password (never persisted/echoed raw).
	HashPassword(password string) (string, error)
	// CreateUser creates the Authula user (Leg B step 1). name=email, emailVerified=true.
	CreateUser(ctx context.Context, email string) (AuthulaUser, error)
	// CreateAccount attaches the hashed password as the email-provider account (Leg B
	// step 2).
	CreateAccount(ctx context.Context, userID, email, passwordHash string) error
	// DeleteUser is COMP_B: it removes the Authula user (the account cascades). Used to
	// compensate a failed Leg A/C.
	DeleteUser(ctx context.Context, userID string) error
}

// AuraLegParams carries the aura-leg write inputs.
type AuraLegParams struct {
	IdentityName    string
	Capabilities    []string
	AuthulaUserID   string
	ActorIdentityID string
}

// AuraLegWriter is the narrow port over the aura.* leg (Leg A + the final audit row +
// compensation). The composition-root adapter implements these with db.WithTx over the
// shared pool. CreateIdentityWithGrants does the INSERT identity + GrantCapability per cap
// + LinkOperator in ONE tx (internally atomic), returning the new identity UUID. A
// duplicate name surfaces ErrOnboardingDuplicate (the aura.identities NOT NULL UNIQUE name
// → 23505). WriteAuditRow writes exactly one immutable identity_audit row in its own tiny
// tx (RESEARCH L8). DeleteIdentity compensates Leg A (grants + link cascade via FK).
type AuraLegWriter interface {
	CreateIdentityWithGrants(ctx context.Context, p AuraLegParams) (identityID string, err error)
	WriteAuditRow(ctx context.Context, p AuraLegParams, newIdentityID string) error
	DeleteIdentity(ctx context.Context, identityName string) error
}

// TelegramMint is the narrow port over the Telegram mint/poll (Leg C + compensation + the
// status poll). The composition-root adapter wraps *telegram.Store, avoiding the
// telegram→agui import cycle. The consume is async (channel-side), never here.
type TelegramMint interface {
	InsertPending(ctx context.Context, onboardingToken, identityID string, expiresAt time.Time) error
	DeletePending(ctx context.Context, onboardingToken string) error
	PendingConsumed(ctx context.Context, onboardingToken string) (bool, error)
}

// errProvisioningUnavailable is returned when a provisioning port is unwired (an
// interview-only deployment). The handler renders it as a sanitized 502.
var errProvisioningUnavailable = errors.New("onboarding: provisioning backend not configured")

// Provision runs the ordered cross-store saga at the wizard's final "Create" confirm
// (ONBD-01a/01b / RESEARCH §Hard Problem 1). It is the ONLY place the cross-store writes
// happen, so an abandoned wizard (session expired before this call) leaves ZERO rows. On
// success it persists the confirmed interview Agent.md for the new identity (ONBD-02) and
// returns the Telegram deep-link + a server-rendered QR (the bot token never leaks). The
// password is hashed immediately and never echoed/logged.
func (s *onboardingService) Provision(ctx context.Context, requesterIdentityID, token string, in OnboardingProvisionRequest) (OnboardingProvisionResponse, error) {
	entry, err := s.sessionForRequester(token, requesterIdentityID)
	if err != nil {
		return OnboardingProvisionResponse{}, err
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.provisioned {
		return OnboardingProvisionResponse{}, errOnboardingSessionNotFound
	}
	creator := entry.creatorIdentityID
	entry.linkTelegram = in.LinkTelegram

	// ---- 0. PRE-VALIDATE (no writes; fail fast) ----
	if err := validateOnboardingProvision(in); err != nil {
		return OnboardingProvisionResponse{}, err
	}
	if s.authula == nil || s.auraLeg == nil || s.telegram == nil || strings.TrimSpace(s.botName) == "" || s.recovery == nil {
		slog.Warn("onboarding: provisioning backend not configured",
			"authula", s.authula != nil,
			"aura_leg", s.auraLeg != nil,
			"telegram", s.telegram != nil,
			"bot_username", strings.TrimSpace(s.botName) != "",
			"recovery", s.recovery != nil,
		)
		return OnboardingProvisionResponse{}, errProvisioningUnavailable
	}
	if err := s.validateNoEscalation(ctx, creator, in.Capabilities); err != nil {
		return OnboardingProvisionResponse{}, err
	}
	identityName := strings.TrimSpace(in.Email)
	// Duplicate pre-check (fail before any write): an existing Authula user with this email
	// is a clean 409. A racing create is still caught by the aura.identities UNIQUE name
	// (23505) at Leg A → ErrOnboardingDuplicate (idempotent double-submit → one identity).
	exists, err := s.authula.UserByEmail(ctx, identityName)
	if err != nil {
		return OnboardingProvisionResponse{}, provisionFail("authula lookup", err)
	}
	if exists {
		return OnboardingProvisionResponse{}, ErrOnboardingDuplicate
	}

	// ---- 1. LEG B (Authula user) — placed FIRST so a dup email fails with zero aura writes ----
	hash, err := s.authula.HashPassword(in.Password)
	if err != nil {
		return OnboardingProvisionResponse{}, provisionFail("password hash", err)
	}
	user, err := s.authula.CreateUser(ctx, identityName) // B1
	if err != nil {
		return OnboardingProvisionResponse{}, provisionFail("authula create user", err)
	}
	compB := func() {
		if derr := s.authula.DeleteUser(context.WithoutCancel(ctx), user.ID); derr != nil {
			slog.Error("onboarding: COMP_B (delete authula user) failed", "step", "compensate")
		}
	}
	if err := s.authula.CreateAccount(ctx, user.ID, user.Email, hash); err != nil { // B2
		compB()
		return OnboardingProvisionResponse{}, provisionFail("authula create account", err)
	}

	// ---- 2. LEG A (aura.* — ONE internally-atomic db.WithTx) ----
	identityID, err := s.auraLeg.CreateIdentityWithGrants(ctx, AuraLegParams{
		IdentityName:    identityName,
		Capabilities:    in.Capabilities,
		AuthulaUserID:   user.ID,
		ActorIdentityID: creator,
	})
	if err != nil {
		compB() // A failed → undo the Authula user
		if errors.Is(err, ErrOnboardingDuplicate) {
			return OnboardingProvisionResponse{}, ErrOnboardingDuplicate
		}
		return OnboardingProvisionResponse{}, provisionFail("aura identity write", err)
	}

	// ---- 3. RECOVERY SETUP (required before Telegram mint) ----
	answerHash, answerVersion, err := (RecoveryHasher{}).HashAnswer(in.SecurityAnswer)
	if err != nil {
		if derr := s.auraLeg.DeleteIdentity(context.WithoutCancel(ctx), identityName); derr != nil {
			slog.Error("onboarding: COMP_A (delete identity) after recovery hash failed", "step", "compensate")
		}
		compB()
		return OnboardingProvisionResponse{}, provisionFail("recovery hash", err)
	}
	if err := s.recovery.UpsertRecovery(ctx, identityID, strings.TrimSpace(in.SecurityQuestion), answerHash, answerVersion); err != nil {
		if derr := s.auraLeg.DeleteIdentity(context.WithoutCancel(ctx), identityName); derr != nil {
			slog.Error("onboarding: COMP_A (delete identity) after recovery write failed", "step", "compensate")
		}
		compB()
		return OnboardingProvisionResponse{}, provisionFail("recovery write", err)
	}

	// ---- 4. LEG C (Telegram token mint) ----
	onboardingToken := ""
	if in.LinkTelegram {
		onboardingToken = uuid.NewString()
		if err := s.telegram.InsertPending(ctx, onboardingToken, identityID, time.Now().UTC().Add(onboardingTokenTTL)); err != nil {
			// C failed → undo A (identity + grants + link cascade) then B.
			if derr := s.telegram.DeletePending(context.WithoutCancel(ctx), onboardingToken); derr != nil {
				slog.Error("onboarding: COMP_C (delete telegram pending) after mint failure failed", "step", "compensate")
			}
			if derr := s.auraLeg.DeleteIdentity(context.WithoutCancel(ctx), identityName); derr != nil {
				slog.Error("onboarding: COMP_A (delete identity) failed", "step", "compensate")
			}
			compB()
			return OnboardingProvisionResponse{}, provisionFail("telegram mint", err)
		}
	}

	// ---- 5. AUDIT (a tiny final tx AFTER Leg C — RESEARCH L8: exactly one row, only on success) ----
	if err := s.auraLeg.WriteAuditRow(ctx, AuraLegParams{
		IdentityName:    identityName,
		Capabilities:    in.Capabilities,
		AuthulaUserID:   user.ID,
		ActorIdentityID: creator,
	}, identityID); err != nil {
		// The audit row is a MUST (no loginable identity without it). If it fails, fully
		// compensate the whole saga so we never leave an unaudited identity.
		if derr := s.telegram.DeletePending(context.WithoutCancel(ctx), onboardingToken); derr != nil {
			slog.Error("onboarding: COMP_C (delete telegram pending) after audit failure failed", "step", "compensate")
		}
		if derr := s.auraLeg.DeleteIdentity(context.WithoutCancel(ctx), identityName); derr != nil {
			slog.Error("onboarding: COMP_A (delete identity) after audit failure failed", "step", "compensate")
		}
		compB()
		return OnboardingProvisionResponse{}, provisionFail("identity audit write", err)
	}

	// ---- 6. SUCCESS — record the mint token + persist the confirmed interview Agent.md ----
	// Record the minted onboarding token on the live session entry so the telegram-status
	// poll can read it back via PendingConsumed. Mark the session provisioned so retry /
	// double-submit attempts cannot run the saga a second time.
	entry.onboardingToken = onboardingToken
	entry.provisioned = true
	// A confirmed interview wrote DraftAgentMD + StatusCompleted into the session; a skipped
	// interview leaves no draft → no profile (the AC: skip writes no profile). This is
	// idempotent + best-effort: a profile write failure does NOT roll back the (committed)
	// loginable identity — the profile is re-seedable on first interaction.
	s.persistProfile(entry, identityID)

	resp := OnboardingProvisionResponse{IdentityID: identityID}
	if in.LinkTelegram && s.botName != "" {
		deepLink := fmt.Sprintf("https://t.me/%s?start=%s", s.botName, onboardingToken)
		resp.DeepLink = deepLink
		if svg, qerr := renderQRSVG(deepLink); qerr == nil {
			resp.QRSVG = svg
		} else {
			slog.Warn("onboarding: qr render failed", "step", "qr")
		}
	}
	return resp, nil
}

// TelegramStatus reports whether the minted onboarding token has been consumed (the user
// scanned the deep-link and the channel linked the identity) via a REST poll over
// PendingConsumed (ONBD-01b / R6 — NOT SSE). The session must still be live (the wizard is
// polling); an unknown token to PendingConsumed surfaces as not-linked, never a 500.
func (s *onboardingService) TelegramStatus(ctx context.Context, requesterIdentityID, token string) (OnboardingTelegramStatus, error) {
	entry, err := s.sessionForRequester(token, requesterIdentityID)
	if err != nil {
		return OnboardingTelegramStatus{}, err
	}
	entry.mu.Lock()
	tok := entry.onboardingToken
	entry.mu.Unlock()
	if tok == "" {
		// No token minted yet (provision not yet called / no Telegram link requested).
		return OnboardingTelegramStatus{Linked: false}, nil
	}
	if s.telegram == nil {
		return OnboardingTelegramStatus{}, errProvisioningUnavailable
	}
	linked, err := s.telegram.PendingConsumed(ctx, tok)
	if err != nil {
		// A not-found / not-yet-consumed token is simply "not linked" — never a 500.
		return OnboardingTelegramStatus{Linked: false}, nil
	}
	return OnboardingTelegramStatus{Linked: linked}, nil
}

// validateNoEscalation is the server-side no-escalation re-validation (ONBD-01a / D-06,
// belt-and-suspenders behind RequireCapability + the store's '*' rejection): the creator
// must hold identity.create (or '*'), the requested set must contain NO '*', and every
// requested cap must be one the creator holds (subset ⊆ creator-grants). A creator with
// '*' may grant any NAMED cap (but never '*' itself).
func (s *onboardingService) validateNoEscalation(ctx context.Context, creator string, requested []string) error {
	if creator == "" {
		return errOnboardingForbidden
	}
	grants, err := s.caps.ListCapabilities(ctx, creator)
	if err != nil {
		return provisionFail("list creator capabilities", err)
	}
	creatorSet := make(map[string]bool, len(grants))
	hasWildcard := false
	for _, g := range grants {
		creatorSet[g] = true
		if g == wildcardCapability {
			hasWildcard = true
		}
	}
	// The creator must be authorized to create identities (route gate backstop).
	if !hasWildcard && !creatorSet[identityCreateCapability] {
		return errOnboardingForbidden
	}
	for _, c := range requested {
		if err := identity.ValidateCapabilityName(c); err != nil {
			return fmt.Errorf("%w: %v", ErrOnboardingEscalation, err)
		}
		if !hasWildcard && !creatorSet[c] {
			return fmt.Errorf("%w: %q is not held by the creator", ErrOnboardingEscalation, c)
		}
	}
	return nil
}

// persistProfile writes the confirmed interview Agent.md for the new identity (ONBD-02). A
// skipped/incomplete interview (no draft) writes nothing. Best-effort: a write failure is
// logged but does NOT fail the (already-committed) provision — the profile is re-seedable.
func (s *onboardingService) persistProfile(entry *sessionEntry, identityID string) {
	if s.profiles == nil || entry == nil || entry.session == nil {
		return
	}
	draft := strings.TrimSpace(entry.session.DraftAgentMD)
	if draft == "" {
		return // skipped / unconfirmed interview → no profile
	}
	err := s.profiles.WriteProfile(identityID, profile.Profile{
		AgentMD:     entry.session.DraftAgentMD,
		Preferences: entry.session.Preferences,
		Metadata:    profile.Metadata{OnboardingCompleted: true},
		Change:      "onboarding wizard",
	})
	if err != nil {
		slog.Warn("onboarding: persist profile failed", "step", "profile")
	}
}

// provisionFail wraps an internal saga error with a FIXED stage label and logs a fixed
// message (never err.Error() verbatim — the setup handleToken precedent, T-13-07: a
// transport error could echo the password or recovery answer). The returned error carries
// only the fixed stage + sentinel, never the arbitrary backend message the handler may
// surface.
func provisionFail(stage string, _ error) error {
	slog.Error("onboarding: provisioning step failed", "stage", stage)
	return fmt.Errorf("onboarding: %s failed: %w", stage, errProvisioningUnavailable)
}
