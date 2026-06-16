// auth.go is the WEB-03 in-binary web-auth boundary (D-01..D-04): a constant-time
// operator-secret login that mints an HMAC-signed session cookie, a RequireAuth
// middleware that gates the whole origin except the public paths (login + assets +
// GET /healthz), a POST /logout that clears the cookie, and a capability gate on the
// only mutating route (POST /agent/run). The cookie crypto lives in auth_cookie.go;
// this file owns the HTTP surface + the consumer-side identity seam.
//
// The boundary is stdlib-only (crypto/hmac + crypto/sha256 + crypto/subtle +
// net/http + time) — no gorilla/* or other 3rd-party session library (RESEARCH
// §Alternatives Considered). It reads ONLY the session cookie, never a client-supplied
// auth/identity header (T-24-13: trusting X-Forwarded-User / Authorization from the
// client is the golang-security client-header anti-pattern; the trust-proxy posture
// means "Aura stays hands-off", not "Aura reads a proxy-injected identity header").
//
// CSRF posture (T-24-19, RESEARCH §Discretion Resolution): SameSite=Strict is the only
// CSRF control this phase. For a same-origin SPA with no cross-origin write path that
// covers the classic vector; a double-submit token is NOT added in Phase 24. Re-evaluate
// if Phase 28/29 introduces a cross-origin write surface.
package agui

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// identityChecker is the narrow consumer-side seam the auth boundary needs from the
// identity store (D-A2-02 "accept interfaces" — internal/identity declares no
// interface, the consumer does, mirroring the Runner seam in server.go). Declaring
// only the two methods used here lets auth_test.go inject a fake while *identity.Store
// satisfies it implicitly at the composition root.
type identityChecker interface {
	GetIdentityByID(ctx context.Context, id string) (Identity, error)
	HasCapability(ctx context.Context, id, capability string) (bool, error)
}

// Identity is the minimal projection the auth boundary observes — it mirrors the
// public field set of identity.Identity so *identity.Store satisfies identityChecker
// structurally without agui importing the concrete return type's full surface.
type Identity struct {
	ID   string
	Name string
	Kind string
}

// AuthDeps carries everything the login handler + RequireAuth + the capability gate
// need, threaded from bootServe. SecretConfigured is the master switch: when false
// (loopback dev, which the Plan-01 boot guard confines to loopback) RequireAuth is a
// no-op pass-through and login is unavailable. SigningKey is sha256(AURA_WEB_AUTH_SECRET).
type AuthDeps struct {
	// Secret is the raw operator passphrase (AURA_WEB_AUTH_SECRET) the login form is
	// compared against constant-time. Empty ⇒ login fail-closed.
	Secret string
	// SigningKey is the derived HMAC-SHA256 key (sha256(Secret)) that signs/verifies
	// the session cookie.
	SigningKey []byte
	// TTL is the absolute session lifetime; a zero value falls back to defaultSessionTTL.
	TTL time.Duration
	// SecretConfigured reports whether in-binary auth is active. When false the whole
	// gate is bypassed (loopback dev) — safe because GuardWebBind only permits an
	// unconfigured secret on a loopback bind.
	SecretConfigured bool
	// LocalIdentityID is the seeded `local` identity a successful login binds the
	// session to (the capability_grants principal, D-02/CORE-03).
	LocalIdentityID string
	// Identities is the capability/identity store seam (a *identity.Store at runtime).
	Identities identityChecker
	// LoginPath is the public login page route an unauthenticated browser GET is
	// redirected to (default "/login").
	LoginPath string
}

// ttl resolves the effective absolute session lifetime, defaulting a zero/negative
// configured TTL to defaultSessionTTL.
func (d AuthDeps) ttl() time.Duration {
	if d.TTL <= 0 {
		return defaultSessionTTL
	}
	return d.TTL
}

// loginPath resolves the public login route, defaulting to "/login".
func (d AuthDeps) loginPath() string {
	if d.LoginPath == "" {
		return "/login"
	}
	return d.LoginPath
}

// LoginHandler returns the POST /login handler: it reads the form passphrase (body
// size-bounded like server.go's handleRun), constant-time compares it against the
// configured secret (validateSecret — fail-closed on an empty/unconfigured secret),
// and on success mints a session cookie bound to the local identity and 303-redirects
// to "/". On any failure it returns a GENERIC 401 with no Set-Cookie and no oracle
// distinguishing "wrong password" from "no secret configured" (T-24-15, golang-security
// V7). The passphrase is never echoed.
func (d AuthDeps) LoginHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxLoginBodyBytes)
		// ParseForm reads the bounded body; an oversized/garbled body is treated as a
		// failed login (generic 401), never a distinct error surface.
		if err := r.ParseForm(); err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		passphrase := r.PostFormValue("passphrase")
		if !validateSecret(passphrase, d.Secret) {
			// Generic body — do NOT reveal whether the secret is unset vs the passphrase
			// is wrong (no login oracle). No Set-Cookie on failure.
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		value := signSession(d.SigningKey, d.LocalIdentityID, time.Now())
		setSessionCookie(w, value, d.ttl())
		// 303 See Other so a browser form POST follows up with a GET of "/".
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

// LogoutHandler returns the POST /logout handler: it clears the session cookie
// (MaxAge < 0) and 303-redirects to the login page. It is safe to call without a
// session (idempotent) and never reads the cookie value.
func (d AuthDeps) LogoutHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clearSessionCookie(w)
		http.Redirect(w, r, d.loginPath(), http.StatusSeeOther)
	}
}

// maxLoginBodyBytes bounds the POST /login form body (Tampering/DoS guard, mirrors
// maxRunBodyBytes in server.go). A login form is a single short passphrase field; 64
// KiB is generous headroom while defanging a hostile body before ParseForm decodes it.
const maxLoginBodyBytes = 64 << 10

// principalKey is the unexported context key the authenticated identity id is stashed
// under by RequireAuth and read by requireCapability. A private type prevents key
// collision with any other package's context values.
type principalKey struct{}

// withPrincipal returns a request whose context carries the authenticated identity id
// (set by RequireAuth after a successful cookie verify).
func withPrincipal(r *http.Request, identityID string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), principalKey{}, identityID))
}

// principalFrom reads the authenticated identity id RequireAuth stashed on the context;
// it returns "" when no principal is set (an unauthenticated path, or a misuse — the
// capability gate treats "" as unauthorized).
func principalFrom(ctx context.Context) string {
	id, _ := ctx.Value(principalKey{}).(string)
	return id
}

// wantsHTML reports whether the request is a browser navigation (an Accept header that
// prefers text/html) rather than an API/XHR call. RequireAuth redirects a browser GET
// to the login page but 401s an API request, so a fetch() gets a clean status instead
// of an HTML login page.
func wantsHTML(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}
