// Package webauth embeds the Authula (Apache-2.0, v1.11.0) Go auth framework as
// the cockpit's "industrial" web-auth provider, behind the AURA_WEB_AUTH_PROVIDER
// feature flag (see docs/cockpit-overhaul/05-authula-auth-SPEC.md, Option A2). It
// constructs authula.New with the three mandatory hardenings:
//
//   - H1 schema isolation: Authula has NO table-prefix config, so it gets its own
//     dedicated `authula` Postgres schema via a DSN `search_path=authula` and its
//     OWN database/sql pool (lib/pq honours search_path). Aura's aura.* schema and
//     pgxpool are never touched. The `authula` schema is CREATEd by Aura migration
//     0019; Authula's own bun migrator fills its CONTENTS.
//   - H2 cookie: __Host-authula_session + Secure + SameSite=Strict + 12h absolute
//     lifetime (Authula defaults Secure=false / SameSite=lax — flipped here).
//   - H3 CSRF: the csrf plugin (double-submit cookie + Fetch-Metadata header
//     protection) plus a per-cookie-name __Host- CSRF token.
//
// The validate-only seam (RequireAuth's cookie core) lives in session_validate.go;
// the operator user-id <-> `local` identity binding lives in identity_link.go. This
// file owns ONLY construction + lifecycle (Close).
package webauth

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	authula "github.com/Authula/authula"
	authulaconfig "github.com/Authula/authula/config"
	authulamodels "github.com/Authula/authula/models"
	csrfplugin "github.com/Authula/authula/plugins/csrf"
	emailpasswordplugin "github.com/Authula/authula/plugins/email-password"
	emailpasswordtypes "github.com/Authula/authula/plugins/email-password/types"
	ratelimitplugin "github.com/Authula/authula/plugins/rate-limit"
	ratelimittypes "github.com/Authula/authula/plugins/rate-limit/types"
	sessionplugin "github.com/Authula/authula/plugins/session"
	totpplugin "github.com/Authula/authula/plugins/totp"
	totptypes "github.com/Authula/authula/plugins/totp/types"
	authulaservices "github.com/Authula/authula/services"
)

// SessionCookieName is the hardened __Host--prefixed session cookie Authula sets.
// The __Host- prefix is honoured because the session plugin always sets Path:/ and
// no Domain (verified in plugins/session/plugin.go:165-177); Secure must be true,
// which Config below flips on.
const SessionCookieName = "__Host-authula_session"

// CSRFCookieName / CSRFHeaderName are the double-submit pair the SPA must echo on
// every state-changing request (spec §4.7). The header name is Authula's canonical
// default; the cookie is __Host--prefixed to bind it to the exact origin.
const (
	CSRFCookieName = "__Host-authula_csrf_token"
	CSRFHeaderName = "X-AUTHULA-CSRF-TOKEN"
)

// sessionAbsoluteTTL matches Aura's existing passphrase cookie absolute lifetime
// (agui.defaultSessionTTL = 12h) so the cutover does not change how long a session
// survives. It maps to Authula's CookieMaxAge + ExpiresIn.
const sessionAbsoluteTTL = 12 * time.Hour

// authPoolMaxConns keeps Authula's own pool modest — auth is low-QPS (spec §6.2).
const authPoolMaxConns = 5

// Config is the wiring input for New. DSN is the Postgres connection string for the
// authula schema; if it lacks a search_path it is forced to search_path=authula so
// Authula's bare users/sessions/accounts tables can never collide with aura.* or
// public (H1). Secret is the 32-byte hex Authula uses to derive its HMAC/token keys
// (AURA_AUTHULA_SECRET). TrustedOrigins seeds the CSRF Fetch-Metadata origin check.
type Config struct {
	DSN            string
	Secret         string
	TrustedOrigins []string
}

// Provider owns the constructed *authula.Auth: its mounted /auth/* HTTP handler, the
// session-validate seam, and lifecycle Close. It is nil-safe so the passphrase path
// can leave it unset.
type Provider struct {
	auth *authula.Auth
}

// New constructs the embedded Authula provider with the hardened config. It does NOT
// pass a caller-supplied DB — Authula builds its OWN database/sql pool over the
// search_path=authula DSN (separate pool, H1 §6.2), running its core + plugin
// migrations into the authula schema at construction. authula.New panics on any init
// failure (documented in auth.go), so New recovers and returns a plain error to keep
// the daemon boot fail-soft rather than crashing the whole process.
func New(cfg Config) (_ *Provider, err error) {
	dsn, derr := ensureAuthulaSearchPath(cfg.DSN)
	if derr != nil {
		return nil, fmt.Errorf("webauth: authula dsn: %w", derr)
	}
	if err := validateAuthulaSecret(cfg.Secret); err != nil {
		return nil, err
	}

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("webauth: authula init: %v", r)
		}
	}()

	authCfg := authulaconfig.NewConfig(
		authulaconfig.WithAppName("Aura"),
		authulaconfig.WithBasePath(basePath),
		authulaconfig.WithSecret(cfg.Secret),
		authulaconfig.WithDatabase(authulamodels.DatabaseConfig{
			Provider:     "postgres",
			URL:          dsn,
			MaxOpenConns: authPoolMaxConns,
			MaxIdleConns: 2,
		}),
		// Gochannel (in-memory) event bus — never the PG/SQLite watermill transports,
		// which would auto-create their own schema (H1 §6.3).
		authulaconfig.WithEventBus(authulamodels.EventBusConfig{
			Provider:  "gochannel",
			GoChannel: &authulamodels.GoChannelConfig{BufferSize: 100},
		}),
		// H2: __Host- + Secure + SameSite=Strict + 12h absolute lifetime.
		authulaconfig.WithSession(authulamodels.SessionConfig{
			CookieName:         SessionCookieName,
			ExpiresIn:          sessionAbsoluteTTL,
			UpdateAge:          sessionAbsoluteTTL,
			CookieMaxAge:       sessionAbsoluteTTL,
			Secure:             true,
			HttpOnly:           true,
			SameSite:           "strict",
			MaxSessionsPerUser: 5,
		}),
		// H3 origin defense: the CSRF plugin's Fetch-Metadata layer compares the
		// request Origin / Sec-Fetch-Site against these trusted origins.
		authulaconfig.WithSecurity(authulamodels.SecurityConfig{
			TrustedOrigins: cfg.TrustedOrigins,
		}),
		authulaconfig.WithPlugins(authulamodels.PluginsConfig{
			authulamodels.PluginSession.String():       map[string]any{"enabled": true},
			authulamodels.PluginEmailPassword.String(): map[string]any{"enabled": true},
			authulamodels.PluginTOTP.String():          map[string]any{"enabled": true},
			authulamodels.PluginCSRF.String():          map[string]any{"enabled": true},
			authulamodels.PluginRateLimit.String():     map[string]any{"enabled": true},
		}),
	)

	auth := authula.New(&authula.AuthConfig{
		Config:  authCfg,
		Plugins: buildPlugins(),
	})
	// Force handler construction now (registers routes/hooks once via sync.Once) so a
	// late registration error surfaces at boot, not on the first request.
	_ = auth.Handler()
	return &Provider{auth: auth}, nil
}

func validateAuthulaSecret(secret string) error {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return fmt.Errorf("webauth: AURA_AUTHULA_SECRET must be set when AURA_WEB_AUTH_PROVIDER=authula")
	}
	if len(secret) != 64 {
		return fmt.Errorf("webauth: AURA_AUTHULA_SECRET must be 64 hex characters (32 bytes)")
	}
	if _, err := hex.DecodeString(secret); err != nil {
		return fmt.Errorf("webauth: AURA_AUTHULA_SECRET must be 64 hex characters (32 bytes)")
	}
	return nil
}

// buildPlugins instantiates the enabled-plugin set with their Enabled flag set, so
// authula.New caches each Config() into PreParsedConfigs and IsPluginEnabled reports
// true (the manual-instantiation path, verified in auth.go:102-112 + util.IsPluginEnabled).
// access-control / oauth2 / jwt / bearer are deliberately OMITTED: Aura keeps authz in
// capability_grants (spec §5) and v1 is single-operator password+TOTP (spec §4.3).
func buildPlugins() []authulamodels.Plugin {
	return []authulamodels.Plugin{
		sessionplugin.New(sessionplugin.SessionPluginConfig{Enabled: true}),
		emailpasswordplugin.New(emailpasswordtypes.EmailPasswordPluginConfig{
			Enabled: true,
			// Single operator: no public sign-up, no email verification flow (no mailer
			// plugin is wired). The operator is provisioned out-of-band (spec OQ-4).
			DisableSignUp:            true,
			RequireEmailVerification: false,
		}),
		totpplugin.New(totptypes.TOTPPluginConfig{
			Enabled:         true,
			BackupCodeCount: 10,
			// The TOTP-pending cookie must also be Secure + Strict in production.
			SecureCookie: true,
			SameSite:     "strict",
		}),
		csrfplugin.New(csrfplugin.CSRFPluginConfig{
			Enabled:                true,
			CookieName:             CSRFCookieName,
			HeaderName:             CSRFHeaderName,
			Secure:                 true,
			SameSite:               "strict",
			EnableHeaderProtection: true,
		}),
		ratelimitplugin.New(ratelimittypes.RateLimitPluginConfig{
			Enabled: true,
			// In-memory backend: no external dep that can fail-open (spec §8.4 DoS row).
			Provider: ratelimittypes.RateLimitProviderInMemory,
			Window:   time.Minute,
			Max:      30,
		}),
	}
}

// Handler returns Authula's mounted HTTP handler (all /auth/* credential routes).
func (p *Provider) Handler() http.Handler {
	return p.auth.Handler()
}

// CoreServices exposes Authula's SessionService + TokenService for the validate seam.
func (p *Provider) CoreServices() *authulaservices.CoreServices {
	return p.auth.CoreServices()
}

// ErrOperatorAmbiguous is returned by OperatorUserID when more than one Authula user
// exists, so the FIRST-operator auto-pin can no longer single out "the" operator. It is
// a SKIP signal, not a fatal: Phase 28 (prd.md amendment #64, D-07) intentionally enrolls
// a 2nd web-loginable identity, and the live session-validate path resolves identity 1:N
// via ResolveIdentityID over aura.identity_auth_links — never via OperatorUserID. The
// caller treats this sentinel as "leave the existing local link as-is" (no de-pin).
var ErrOperatorAmbiguous = errors.New("webauth: multiple operators enrolled — first-operator auto-pin skipped (multi-user resolves via identity_auth_links)")

// OperatorUserID is an ENROLLMENT-TIME helper that auto-pins the FIRST operator to the
// `local` identity (spec §5; relaxed by prd.md amendment #64 / D-07). It returns "" (no
// error) when no operator is yet enrolled, the lone user's id when exactly one exists,
// and ErrOperatorAmbiguous when more than one user is present.
//
// The >1-user case is non-fatal by design: it is NOT a request-path call (the live
// validate path uses ResolveIdentityID over aura.identity_auth_links, UNIQUE on
// authula_user_id, 1:N-ready), so a 2nd enrolled identity logs in and resolves with no
// resolution-path change. The caller skips the auto-pin on ErrOperatorAmbiguous and
// leaves the already-linked `local` identity untouched (no de-pin, no regression).
func (p *Provider) OperatorUserID(ctx context.Context) (string, error) {
	if p == nil || p.auth == nil {
		return "", nil
	}
	core := p.auth.CoreServices()
	if core == nil || core.UserService == nil {
		return "", nil
	}
	users, _, err := core.UserService.GetAll(ctx, nil, 2)
	if err != nil {
		return "", fmt.Errorf("webauth: list operator users: %w", err)
	}
	switch len(users) {
	case 0:
		return "", nil // not yet enrolled
	case 1:
		return users[0].ID, nil
	default:
		// >1 user: the first-operator auto-pin is ambiguous. Signal SKIP (not fatal) —
		// multi-user resolution rides on identity_auth_links via ResolveIdentityID.
		return "", ErrOperatorAmbiguous
	}
}

// Close releases Authula's plugins + core systems (the expiry cleanup workers) so the
// daemon shuts down goleak-clean. It is nil-safe (the passphrase path leaves it unset).
func (p *Provider) Close() error {
	if p == nil || p.auth == nil {
		return nil
	}
	var firstErr error
	if err := p.auth.ClosePlugins(); err != nil {
		firstErr = err
	}
	if err := p.auth.CloseSystems(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// ensureAuthulaSearchPath guarantees the DSN carries search_path=authula so every
// bare Authula table lands in the isolated schema (H1). If the operator already set a
// search_path (via AURA_AUTHULA_DATABASE_URL) it is respected as-is.
func ensureAuthulaSearchPath(dsn string) (string, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return "", fmt.Errorf("empty DSN")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	q := u.Query()
	if q.Get("search_path") == "" {
		q.Set("search_path", authulaSchema)
		u.RawQuery = q.Encode()
	}
	return u.String(), nil
}

// authulaSchema / basePath are the wiring constants the migration (authulaSchema) and
// the route mount (basePath) share.
const (
	authulaSchema = "authula"
	basePath      = "/auth"
)
