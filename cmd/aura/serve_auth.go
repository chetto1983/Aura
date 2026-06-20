// serve_auth.go builds the WEB-03 web-auth dependency bundle (agui.AuthDeps) the
// `aura serve` daemon threads into newServeHandler. It is split out of serve.go
// (refactor-on-touch, CLAUDE.md ≤600 LOC) and owns three things:
//
//   - identityCheckerAdapter: a thin bridge so *identity.Store satisfies the agui
//     consumer-side identityChecker seam. identity.Store returns identity.Identity;
//     agui declares its own narrow Identity projection so the agui package does not
//     import internal/identity. The adapter does the trivial field copy.
//   - buildAuthDeps: derives the HMAC signing key from AURA_WEB_AUTH_SECRET (one
//     operator secret governs both login and signing — RESEARCH A2), binds the
//     session to the seeded `local` identity, and sets SecretConfigured from the
//     non-empty secret so RequireAuth no-ops on loopback dev (where the Plan-01 boot
//     guard permits an unconfigured secret).
//   - the Authula provider seam (docs/cockpit-overhaul/05-authula-auth-SPEC.md,
//     Option A2): when AURA_WEB_AUTH_PROVIDER=authula, it constructs the embedded
//     Authula framework on the isolated authula schema, binds the operator user ⇄
//     `local` identity, and injects a SessionValidator into AuthDeps so RequireAuth's
//     cookie core validates the Authula session instead of the HMAC cookie. Default
//     (passphrase) leaves everything byte-identical to before.
package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/webauth"
)

// identityCheckerAdapter bridges *identity.Store onto the agui.identityChecker seam:
// agui declares its own Identity projection (so it need not import internal/identity),
// and this adapter converts identity.Identity → agui.Identity on GetIdentityByID while
// passing HasCapability straight through. HasCapability/GetIdentityByID names + the
// `*` wildcard semantics are owned by internal/identity (migration 0004 seeds the
// `local` wildcard).
type identityCheckerAdapter struct {
	store *identity.Store
}

func (a identityCheckerAdapter) GetIdentityByID(ctx context.Context, id string) (agui.Identity, error) {
	idn, err := a.store.GetIdentityByID(ctx, id)
	if err != nil {
		return agui.Identity{}, err
	}
	return agui.Identity{ID: idn.ID, Name: idn.Name, Kind: idn.Kind}, nil
}

func (a identityCheckerAdapter) HasCapability(ctx context.Context, id, capability string) (bool, error) {
	return a.store.HasCapability(ctx, id, capability)
}

// buildAuthDeps assembles the WEB-03 auth bundle from the booted composition root. The
// secret comes from AURA_WEB_AUTH_SECRET (chat.cfg.WebAuthSecret); SecretConfigured is
// true only for a non-empty trimmed secret, which gates whether RequireAuth is active.
// The signing key is sha256(secret) so a single operator secret governs both the login
// compare and the cookie signature. The session binds to the seeded `local` identity
// (resolved by name, the existing fail-soft helper) — its `*` wildcard passes the
// capability gate.
//
// When AURA_WEB_AUTH_PROVIDER=authula it ALSO constructs the embedded Authula provider
// (returned so the daemon can Close its cleanup workers) and injects a SessionValidator
// so RequireAuth validates the Authula session cookie. A provider construction failure
// is returned so bootServe fails the daemon boot cleanly rather than silently falling
// back to passphrase on a misconfigured cutover. The default passphrase path returns a
// nil provider and a nil SessionValidator (byte-identical legacy behavior).
func buildAuthDeps(ctx context.Context, chat *chatEnv) (agui.AuthDeps, *webauth.Provider, error) {
	secret := strings.TrimSpace(chat.cfg.WebAuthSecret)
	key := sha256.Sum256([]byte(secret))
	deps := agui.AuthDeps{
		Secret:           secret,
		SigningKey:       key[:],
		SecretConfigured: secret != "",
		LocalIdentityID:  resolveWebAuthIdentityID(ctx, chat.identity, chat.cfg),
		Identities:       identityCheckerAdapter{store: chat.identity},
		LoginPath:        "/login",
	}

	if !strings.EqualFold(strings.TrimSpace(chat.cfg.WebAuthProvider), "authula") {
		return deps, nil, nil // passphrase (default) — no Authula, no validator
	}

	provider, validator, err := buildAuthulaProvider(ctx, chat, deps.LocalIdentityID)
	if err != nil {
		return agui.AuthDeps{}, nil, err
	}
	// The Authula provider is now the cookie issuer/validator. SecretConfigured stays
	// true so the whole-origin gate is active even when no passphrase secret is set —
	// the Authula cutover should not require a passphrase to keep the gate on.
	deps.SecretConfigured = true
	deps.SessionValidator = func(r *http.Request) (string, bool) {
		id, verr := validator.Validate(r)
		if verr != nil {
			return "", false
		}
		return id, true
	}
	return deps, provider, nil
}

func authulaProvisioningConfigured(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	if strings.TrimSpace(cfg.AuthulaSecret) == "" {
		return false
	}
	return strings.TrimSpace(cfg.AuthulaDatabaseURL) != "" || strings.TrimSpace(cfg.DB.URL) != ""
}

// buildAuthulaProvider constructs the embedded Authula framework on the isolated authula
// schema (deriving the DSN from AURA_DB_URL with ?search_path=authula when
// AURA_AUTHULA_DATABASE_URL is unset), builds the identity-link resolver over Aura's
// pool, and pins the Authula operator user to the configured Aura identity (default
// `local`). The trusted-origin list seeds Authula's CSRF Fetch-Metadata check; for a
// loopback/same-origin cockpit the bound host is the only trusted origin.
func buildAuthulaProvider(ctx context.Context, chat *chatEnv, localIdentityID string) (*webauth.Provider, *webauth.Validator, error) {
	dsn := strings.TrimSpace(chat.cfg.AuthulaDatabaseURL)
	if dsn == "" {
		dsn = chat.cfg.DB.URL // webauth.New appends ?search_path=authula
	}
	provider, err := webauth.New(webauth.Config{
		DSN:            dsn,
		Secret:         chat.cfg.AuthulaSecret,
		TrustedOrigins: authulaTrustedOrigins(chat.cfg.AGUIBind),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("authula provider: %w", err)
	}
	linker := webauth.NewIdentityLinker(chat.pool)
	// FIRST-operator enrollment pin (spec §5; relaxed by prd.md amendment #64 / D-07):
	// when exactly one Authula user exists, bind it to the configured Aura identity
	// (default `local`) so the validate path resolves authula-user-id → identity UUID
	// with no extra CLI. First-run enrollment (creating the Authula user + TOTP) happens
	// out-of-band (spec OQ-4); until then OperatorUserID returns "" and the link is
	// deferred — the daemon boots and simply 401s the cockpit until the operator is
	// enrolled.
	//
	// Multi-user (Phase 28): once a 2nd web-loginable identity is provisioned via the
	// onboarding wizard, OperatorUserID returns ErrOperatorAmbiguous — the auto-pin is
	// SKIPPED (not fatal) and the already-linked `local` identity is left untouched. Each
	// identity (the first operator + every provisioned one) carries its OWN
	// aura.identity_auth_links row, and the live session-validate path resolves identity
	// 1:N via ResolveIdentityID over that table — it never depends on OperatorUserID.
	switch uid, uerr := provider.OperatorUserID(ctx); {
	case uerr == nil && uid != "":
		if lerr := linker.LinkOperator(ctx, localIdentityID, uid); lerr != nil {
			fmt.Println("aura serve: authula operator link failed:", lerr)
		}
	case errors.Is(uerr, webauth.ErrOperatorAmbiguous):
		fmt.Println("aura serve: multiple operators enrolled — first-operator auto-pin skipped; multi-user resolves via identity_auth_links")
	case uerr != nil:
		fmt.Println("aura serve: authula operator lookup deferred (operator not yet enrolled):", uerr)
	}
	return provider, webauth.NewValidator(provider, linker), nil
}

type identityNameResolver interface {
	GetIdentityByName(ctx context.Context, name string) (identity.Identity, error)
}

// resolveWebAuthIdentityID returns the Aura identity id used by the active web-auth
// provider. The passphrase provider intentionally stays pinned to the seeded `local`
// identity; the Authula provider honors AURA_AUTHULA_OPERATOR_IDENTITY so operators can
// bind the Authula user to a non-default Aura identity without touching code.
func resolveWebAuthIdentityID(ctx context.Context, idStore identityNameResolver, cfg *config.Config) string {
	name := localIdentityName
	if cfg != nil && strings.EqualFold(strings.TrimSpace(cfg.WebAuthProvider), "authula") {
		if configured := strings.TrimSpace(cfg.AuthulaOperatorIdentity); configured != "" {
			name = configured
		}
	}
	id, err := idStore.GetIdentityByName(ctx, name)
	if err != nil {
		slog.Warn("aura serve: resolve web-auth identity", "name", name, "err", err)
		return ""
	}
	return id.ID
}

// authulaTrustedOrigins derives the CSRF Fetch-Metadata trusted-origin list from the
// cockpit bind. A loopback/same-origin SPA only ever issues same-origin requests, so the
// bound host (http + https) is the trusted origin; an empty/odd bind yields no entries
// (the double-submit token layer still applies — Fetch-Metadata is the second layer).
func authulaTrustedOrigins(bind string) []string {
	host, port, err := net.SplitHostPort(bind)
	if err != nil {
		return nil
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	hostport := net.JoinHostPort(host, port)
	return []string{"http://" + hostport, "https://" + hostport}
}
