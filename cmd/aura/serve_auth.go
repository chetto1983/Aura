// serve_auth.go builds the WEB-03 web-auth dependency bundle (agui.AuthDeps) the
// `aura serve` daemon threads into newServeHandler. It is split out of serve.go
// (refactor-on-touch, CLAUDE.md ≤600 LOC) and owns three things:
//
//   - identityCheckerAdapter: a thin bridge so *identity.Store satisfies the agui
//     consumer-side identityChecker seam. identity.Store returns identity.Identity;
//     agui declares its own narrow Identity projection so the agui package does not
//     import internal/identity. The adapter does the trivial field copy.
//   - buildAuthDeps: constructs the embedded Authula framework on the isolated authula
//     schema, binds the operator user ⇄ Aura identity, and injects a SessionValidator
//     into AuthDeps so RequireAuth's cookie core validates the Authula session.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

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
	return agui.Identity{ID: idn.ID, Name: idn.Name, Kind: idn.Kind, Deactivated: idn.Deactivated}, nil
}

func (a identityCheckerAdapter) HasCapability(ctx context.Context, id, capability string) (bool, error) {
	return a.store.HasCapability(ctx, id, capability)
}

// buildAuthDeps assembles the WEB-03 auth bundle from the booted composition root.
// Authula is the only web-auth provider: boot fails when its DB/secret configuration is
// incomplete, the legacy passphrase secret/signing key stay neutral, and RequireAuth is
// always active through the Authula session validator.
func buildAuthDeps(ctx context.Context, chat *chatEnv) (agui.AuthDeps, *webauth.Provider, error) {
	if !authulaProvisioningConfigured(chat.cfg) {
		return agui.AuthDeps{}, nil, fmt.Errorf("authula web auth misconfigured: set AURA_AUTHULA_SECRET and AURA_AUTHULA_DATABASE_URL or AURA_DB_URL")
	}
	localIdentityID, err := resolveWebAuthIdentityID(ctx, chat.identity, chat.cfg)
	if err != nil {
		return agui.AuthDeps{}, nil, err
	}
	provider, validator, err := buildAuthulaProvider(ctx, chat, localIdentityID)
	if err != nil {
		return agui.AuthDeps{}, nil, err
	}
	deps := agui.AuthDeps{
		Secret:           "",
		SigningKey:       nil,
		SecretConfigured: true,
		LocalIdentityID:  localIdentityID,
		Identities:       identityCheckerAdapter{store: chat.identity},
		LoginPath:        "/login",
		SessionValidator: func(r *http.Request) (string, bool) {
			id, verr := validator.Validate(r)
			if verr != nil {
				return "", false
			}
			return id, true
		},
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
// pool, and makes sure the first Authula user resolves to its real Aura user identity.
// The trusted-origin list seeds Authula's CSRF Fetch-Metadata check; for a
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
		RateLimitMax:   chat.cfg.AuthulaRateLimitMax,
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
		if lerr := ensureAuthulaUserLinked(ctx, linker, chat.pool, localIdentityID, uid, preferredUserIdentityForAuthulaUser); lerr != nil {
			fmt.Println("aura serve: authula operator link failed:", lerr)
		} else if rerr := retireLegacyLocalIdentityForAuthulaUser(ctx, chat.pool, uid); rerr != nil {
			fmt.Println("aura serve: legacy local identity retirement skipped:", rerr)
		}
	case errors.Is(uerr, webauth.ErrOperatorAmbiguous):
		fmt.Println("aura serve: multiple operators enrolled — first-operator auto-pin skipped; multi-user resolves via identity_auth_links")
	case uerr != nil:
		fmt.Println("aura serve: authula operator lookup deferred (operator not yet enrolled):", uerr)
	}
	validator := webauth.NewValidator(provider, linker)
	if err := provider.ConfigureMCPOAuth(validator, chat.cfg.WebPublicURL); err != nil {
		_ = provider.Close()
		return nil, nil, fmt.Errorf("configure MCP OAuth: %w", err)
	}
	return provider, validator, nil
}

type startupIdentityLinker interface {
	ResolveIdentityID(ctx context.Context, authulaUserID string) (string, error)
	LinkOperator(ctx context.Context, identityID, authulaUserID string) error
}

type preferredAuthulaIdentityFunc func(context.Context, *pgxpool.Pool, string) (string, error)

func ensureAuthulaUserLinked(
	ctx context.Context,
	linker startupIdentityLinker,
	pool *pgxpool.Pool,
	fallbackIdentityID string,
	authulaUserID string,
	preferred preferredAuthulaIdentityFunc,
) error {
	if linker == nil || strings.TrimSpace(authulaUserID) == "" {
		return nil
	}
	preferredID, preferredErr := preferred(ctx, pool, authulaUserID)
	existingID, err := linker.ResolveIdentityID(ctx, authulaUserID)
	if err == nil {
		if preferredErr == nil &&
			strings.TrimSpace(preferredID) != "" &&
			existingID == fallbackIdentityID &&
			preferredID != existingID {
			return linker.LinkOperator(ctx, preferredID, authulaUserID)
		}
		return nil
	}
	if !errors.Is(err, webauth.ErrLinkNotFound) {
		return err
	}
	targetID := fallbackIdentityID
	if preferredErr == nil && strings.TrimSpace(preferredID) != "" {
		targetID = preferredID
	}
	if strings.TrimSpace(targetID) == "" {
		return fmt.Errorf("authula user %q has no Aura identity target", authulaUserID)
	}
	return linker.LinkOperator(ctx, targetID, authulaUserID)
}

func retireLegacyLocalIdentityForAuthulaUser(ctx context.Context, pool *pgxpool.Pool, authulaUserID string) error {
	if pool == nil || strings.TrimSpace(authulaUserID) == "" {
		return nil
	}
	targetID, err := preferredUserIdentityForAuthulaUser(ctx, pool, authulaUserID)
	if err != nil || strings.TrimSpace(targetID) == "" {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("retire legacy local identity: begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	var localID string
	if err := tx.QueryRow(ctx,
		`SELECT id::text FROM aura.identities WHERE name=$1 AND kind='system'`,
		localIdentityName,
	).Scan(&localID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("retire legacy local identity: lookup local: %w", err)
	}
	if localID == targetID {
		return nil
	}
	for _, stmt := range []string{
		`UPDATE aura.conversations SET identity_id=$1::uuid WHERE identity_id=$2::uuid`,
		`UPDATE aura.assets SET identity_id=$1::uuid WHERE identity_id=$2::uuid`,
		`UPDATE aura.telegram_accounts SET identity_id=$1::uuid WHERE identity_id=$2::uuid`,
		`UPDATE aura.identity_auth_links SET identity_id=$1::uuid WHERE identity_id=$2::uuid`,
	} {
		if _, err := tx.Exec(ctx, stmt, targetID, localID); err != nil {
			return fmt.Errorf("retire legacy local identity: migrate refs: %w", err)
		}
	}
	if _, err := tx.Exec(ctx,
		`UPDATE aura.assets
		 SET object_key = replace(object_key, $2, $1)
		 WHERE object_key LIKE '%' || $2 || '%'
		   AND status IN ('deleted', 'canceled', 'failed', 'refused')`,
		targetID, localID,
	); err != nil {
		return fmt.Errorf("retire legacy local identity: scrub deleted asset keys: %w", err)
	}
	for _, stmt := range []string{
		`UPDATE aura.scheduler_tasks SET identity_id=$1 WHERE identity_id=$2`,
		`UPDATE aura.pending_notifications SET identity_id=$1 WHERE identity_id=$2`,
	} {
		if _, err := tx.Exec(ctx, stmt, targetID, localIdentityName); err != nil {
			return fmt.Errorf("retire legacy local identity: migrate text refs: %w", err)
		}
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM aura.telegram_setup_pending WHERE identity_id=$1::uuid`,
		localID,
	); err != nil {
		return fmt.Errorf("retire legacy local identity: delete pending setup tokens: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM aura.identities WHERE id=$1::uuid AND name=$2 AND kind='system'`,
		localID, localIdentityName,
	); err != nil {
		return fmt.Errorf("retire legacy local identity: delete local: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("retire legacy local identity: commit: %w", err)
	}
	committed = true
	return nil
}

func preferredUserIdentityForAuthulaUser(ctx context.Context, pool *pgxpool.Pool, authulaUserID string) (string, error) {
	if pool == nil || strings.TrimSpace(authulaUserID) == "" {
		return "", nil
	}
	const q = `
		SELECT i.id::text
		FROM authula.users u
		JOIN aura.identities i ON i.kind = 'user' AND lower(i.name) = lower(u.email)
		WHERE u.id::text = $1
		LIMIT 1`
	var identityID string
	if err := pool.QueryRow(ctx, q, authulaUserID).Scan(&identityID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("resolve authula user identity: %w", err)
	}
	return identityID, nil
}

type identityNameResolver interface {
	GetIdentityByName(ctx context.Context, name string) (identity.Identity, error)
}

// resolveWebAuthIdentityID returns the optional legacy Aura identity fallback used by
// Authula web auth. Empty is valid: modern installs link Authula users to real
// aura.identities rows during bootstrap/provisioning and do not need `local`.
func resolveWebAuthIdentityID(ctx context.Context, idStore identityNameResolver, cfg *config.Config) (string, error) {
	name := ""
	if cfg != nil {
		if configured := strings.TrimSpace(cfg.AuthulaOperatorIdentity); configured != "" {
			name = configured
		}
	}
	if name == "" {
		return "", nil
	}
	id, err := idStore.GetIdentityByName(ctx, name)
	if err != nil {
		if name == localIdentityName && errors.Is(err, identity.ErrIdentityNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("resolve web-auth identity %q: %w", name, err)
	}
	if strings.TrimSpace(id.ID) == "" {
		return "", fmt.Errorf("resolve web-auth identity %q: empty identity id", name)
	}
	return id.ID, nil
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
