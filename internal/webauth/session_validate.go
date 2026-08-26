// session_validate.go is the Option-A2 seam (spec §3.2/§4.4/OQ-3): it replaces ONLY
// the cookie-validation core of agui.RequireAuth when AURA_WEB_AUTH_PROVIDER=authula.
// The validate path is read verbatim from Authula's own session hook
// (plugins/session/plugin.go:125-144):
//
//	hashed := TokenService.Hash(rawCookieValue)   // SHA-256 hex
//	sess, err := SessionService.GetByToken(ctx, hashed)
//	if err==nil && sess!=nil && sess.ExpiresAt.After(now.UTC()) { ok }
//
// On a hit it maps the Authula user-id -> the bound Aura identity UUID (identity_link.go)
// so the existing principalKey{} contract + RequireCapability + capability_grants gate
// keep working byte-for-byte. There is NO "validate raw cookie" convenience on the
// SessionService interface, so the seam hashes the cookie itself (services/core.go:30-57).
package webauth

import (
	"context"
	"errors"
	"net/http"
	"time"

	authulaservices "github.com/Authula/authula/services"
)

// ErrNoSession is returned by Validate when the request carries no usable Authula
// session (missing cookie, unknown token, or expired). The caller (the agui seam)
// maps it to a 302 login / 401 exactly as the passphrase path does on a bad cookie.
var ErrNoSession = errors.New("webauth: no valid authula session")

// userResolver maps an Authula user-id to the bound Aura identity UUID. identity_link.go
// implements it over the identity_auth_links join table; tests inject a fake.
type userResolver interface {
	ResolveIdentityID(ctx context.Context, authulaUserID string) (string, error)
}

// coreServicesProvider yields Authula's CoreServices (SessionService + TokenService).
// *Provider satisfies it via CoreServices(); a fake satisfies it in unit tests so the
// validate logic is exercised without a live Authula/Postgres stack.
type coreServicesProvider interface {
	CoreServices() *authulaservices.CoreServices
}

// Validator is the cookie-validation core the agui boundary injects in place of the
// HMAC verifySession when the provider is authula. It owns no HTTP semantics — it
// returns the Aura identity UUID or ErrNoSession; the agui layer keeps the redirect /
// 401 / existence-recheck behaviour.
type Validator struct {
	provider coreServicesProvider
	resolver userResolver
}

type SessionIdentity struct {
	IdentityID string
	UserID     string
	SessionID  string
}

// NewValidator binds the validate core to the constructed provider + the identity link
// resolver. Both must be non-nil at the authula composition root.
func NewValidator(p coreServicesProvider, r userResolver) *Validator {
	return &Validator{provider: p, resolver: r}
}

// Validate reads the hardened session cookie, hashes it, looks the session up via
// Authula's SessionService, checks the absolute expiry itself (Authula does the expiry
// comparison in its hook, not inside GetByToken — OQ-3), then maps the Authula user-id
// to the Aura identity UUID. Any miss/expiry/decode failure returns ErrNoSession (never
// a panic) so the seam fails closed.
func (v *Validator) Validate(r *http.Request) (identityID string, err error) {
	resolved, err := v.SessionIdentity(r)
	if err != nil {
		return "", err
	}
	return resolved.IdentityID, nil
}

// SessionIdentity resolves the authenticated Authula session and its bound Aura
// identity in one lookup. OAuth token minting needs the session id as well as the
// tenant id so refresh revocation remains tied to the login session.
func (v *Validator) SessionIdentity(r *http.Request) (SessionIdentity, error) {
	if v == nil || v.provider == nil || v.resolver == nil {
		return SessionIdentity{}, ErrNoSession
	}
	cookie, cerr := r.Cookie(SessionCookieName)
	if cerr != nil || cookie.Value == "" {
		return SessionIdentity{}, ErrNoSession
	}
	core := v.provider.CoreServices()
	if core == nil || core.TokenService == nil || core.SessionService == nil {
		return SessionIdentity{}, ErrNoSession
	}
	hashed := core.TokenService.Hash(cookie.Value)
	sess, gerr := core.SessionService.GetByToken(r.Context(), hashed)
	if gerr != nil || sess == nil {
		return SessionIdentity{}, ErrNoSession
	}
	if !sess.ExpiresAt.After(time.Now().UTC()) {
		return SessionIdentity{}, ErrNoSession // expired — fail closed (mirrors session/plugin.go:136)
	}
	auraID, rerr := v.resolver.ResolveIdentityID(r.Context(), sess.UserID)
	if rerr != nil || auraID == "" {
		return SessionIdentity{}, ErrNoSession
	}
	return SessionIdentity{IdentityID: auraID, UserID: sess.UserID, SessionID: sess.ID}, nil
}
