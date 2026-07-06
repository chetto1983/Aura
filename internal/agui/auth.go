// auth.go is the WEB-03 in-binary web-auth boundary (D-01..D-04): a constant-time
// Auth middleware gates the whole origin except the public paths (login + assets +
// GET /healthz), clears sessions on logout, and applies capability gates to mutating
// routes. The legacy HMAC passphrase helpers remain covered by unit tests, but
// `aura serve` mounts Authula as the only credential issuer.
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
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/identityctx"
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
	// Deactivated is the D-27 soft-delete marker (deactivated_at IS NOT NULL). A
	// deactivated principal is denied at RequireAuth (HI-02) even with a valid session.
	Deactivated bool
}

// AuthDeps carries everything RequireAuth + the capability gate need, threaded from
// bootServe. SecretConfigured is the master switch: when false (loopback dev, which
// the boot guard confines to loopback) RequireAuth is a no-op pass-through.
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
	// AuthBasePath is the credential-flow route prefix served by the
	// embedded Authula provider (its BasePath, "/auth"). Every path under it is public
	// (reachable WITHOUT a session) because login / TOTP-verify happen before a session
	// exists; Authula's own per-route metadata + CSRF middleware gate those routes.
	AuthBasePath string
	// PublicAsset reports whether a path is a real embedded static asset the login page
	// needs before a session exists (the shared SPA bundle/styles, the PWA service
	// worker/manifest, icons). It is wired from webui.IsPublicAsset at the composition
	// root (cmd/aura/serve_webui.go); a nil predicate means no static asset is public
	// (the gate still serves the login route + GET /healthz).
	PublicAsset func(path string) bool
	// PublicRoute reports whether a non-asset route is intentionally reachable before
	// a session. The composition root uses this for tiny bootstrap contracts such as
	// the auth-provider config; credential subtrees should still use AuthBasePath.
	PublicRoute func(r *http.Request) bool
	// SessionValidator, when non-nil, REPLACES the HMAC cookie-validation core inside
	// RequireAuth (the Option-A2 seam, AURA_WEB_AUTH_PROVIDER=authula): given a request
	// it returns the bound Aura identity UUID + ok. When nil, RequireAuth falls back to
	// the legacy stdlib HMAC verifySession path used by unit tests. Either way the result feeds the SAME existence re-check +
	// withPrincipal + RequireCapability contract — only the issuer/validator of the
	// cookie changes, never the principalKey{} downstream.
	SessionValidator func(r *http.Request) (identityID string, ok bool)
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

// isPublicPath reports whether a request path is reachable WITHOUT a session (D-03):
// the login page route and its static assets (the shared SPA bundle/styles + PWA +
// icons, via the PublicAsset predicate wired from webui). GET /healthz is handled
// separately in RequireAuth (only GET is public). GET /readyz is also handled there,
// but only for same-process loopback health probes; unauthenticated host/browser
// requests stay gated. The SPA shell at "/", /metrics and /debug/vars stay gated.
// The static bundle is the SAME code for everyone, so gating it would only break the
// login render without protecting anything — the session protects the API + rendering
// an authenticated shell, not the static assets.
func (d AuthDeps) isPublicPath(p string) bool {
	if p == d.loginPath() {
		return true
	}
	// The Authula credential-flow subtree (login / totp / logout) must be reachable
	// before a session exists; Authula's own metadata + CSRF middleware gate it.
	if d.AuthBasePath != "" && (p == d.AuthBasePath || strings.HasPrefix(p, d.AuthBasePath+"/")) {
		return true
	}
	return d.PublicAsset != nil && d.PublicAsset(p)
}

// RequireAuth gates the WHOLE origin (D-03) except the public paths. When auth is not
// configured (SecretConfigured==false, loopback dev) it returns `next` unchanged — a
// no-op pass-through (mirrors server.go withCORS), safe because the Plan-01 boot guard
// only permits an unconfigured secret on a loopback bind. When active it:
//  1. lets isPublicPath requests + GET /healthz through (login + assets + liveness);
//     GET /readyz is allowed only for same-process loopback orchestrator probes;
//  2. reads ONLY r.Cookie(sessionCookieName) — never a client auth/identity header
//     (T-24-13, golang-security client-header anti-pattern);
//  3. redirects a browser GET to the login page (302) / 401s an API request on a
//     missing or invalid cookie;
//  4. verifySession (HMAC + absolute TTL);
//  5. confirms the bound identity still exists (a deleted identity invalidates the
//     session);
//  6. stashes the principal on the request context and calls next.
func RequireAuth(next http.Handler, deps AuthDeps) http.Handler {
	if !deps.SecretConfigured {
		return next // loopback dev — auth disabled, pass-through (boot guard confines this to loopback)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// GET /healthz stays public for proxy/orchestrator liveness (D-03, Pitfall 4);
		// GET /readyz is public only to same-process loopback probes so Docker can query
		// dependency readiness without minting a user session; host/proxy requests stay gated.
		if r.URL.Path == "/healthz" && r.Method == http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/readyz" && r.Method == http.MethodGet && isLoopbackRemoteAddr(r.RemoteAddr) {
			next.ServeHTTP(w, r)
			return
		}
		if deps.isPublicPath(r.URL.Path) || (deps.PublicRoute != nil && deps.PublicRoute(r)) {
			next.ServeHTTP(w, r)
			return
		}
		identityID, ok := deps.validateSession(r)
		if !ok {
			deps.redirectToLogin(w, r)
			return
		}
		// The bound identity must still exist — a deleted identity invalidates the
		// session even with a valid MAC / Authula token (D-02 principal binding). This
		// re-check is provider-agnostic: it runs for BOTH the passphrase and Authula
		// paths so a deleted operator identity always 401s the next request (AC-11).
		if _, err := deps.Identities.GetIdentityByID(r.Context(), identityID); err != nil {
			deps.redirectToLogin(w, r)
			return
		}
		next.ServeHTTP(w, withPrincipal(r, identityID))
	})
}

func isLoopbackRemoteAddr(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// validateSession is the provider dispatch for the cookie-validation core (D-03,
// Option-A2 seam). When SessionValidator is wired (AURA_WEB_AUTH_PROVIDER=authula) it
// delegates to it (Authula session lookup → mapped Aura identity UUID); otherwise it
// reads the __Host-aura_session cookie and runs the stdlib HMAC verifySession
// (passphrase). Both return the bound Aura identity UUID + ok; RequireAuth applies the
// identical existence re-check + withPrincipal afterwards.
func (d AuthDeps) validateSession(r *http.Request) (identityID string, ok bool) {
	if d.SessionValidator != nil {
		return d.SessionValidator(r)
	}
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return "", false
	}
	return verifySession(d.SigningKey, c.Value, d.ttl(), time.Now())
}

// redirectToLogin sends a browser navigation (Accept: text/html GET) to the login page
// with 302, but answers an API/XHR request with a plain 401 — so a fetch() gets a clean
// status code instead of an HTML login page it cannot use.
func (d AuthDeps) redirectToLogin(w http.ResponseWriter, r *http.Request) {
	if wantsHTML(r) {
		http.Redirect(w, r, d.loginPath(), http.StatusFound)
		return
	}
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}

// RequireCapability wraps a mutating-route handler with the capability_grants check
// (D-04): it reads the principal RequireAuth stashed, asks the identity store
// HasCapability(principal, capability), and 403s on a missing principal, a store error,
// or a denied capability. This is the seam that exercises the dormant capability_grants
// scaffolding on the ONLY mutating route (POST /agent/run); the seeded `local` identity
// passes via its `*` wildcard. It invents NO governance write routes — those land in
// Phase 28. Exported so the composition root (cmd/aura) interposes it on the parent mux.
func RequireCapability(next http.Handler, deps AuthDeps, capability string) http.Handler {
	if !deps.SecretConfigured {
		return next // loopback dev - auth disabled, pass-through with RequireAuth
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identityID := principalFrom(r.Context())
		if identityID == "" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		ok, err := deps.Identities.HasCapability(r.Context(), identityID, capability)
		if err != nil || !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// principalKey is the unexported context key the authenticated identity id is stashed
// under by RequireAuth and read by requireCapability. A private type prevents key
// collision with any other package's context values.
type principalKey struct{}

// withPrincipal returns a request whose context carries the authenticated identity id
// (set by RequireAuth after a successful cookie verify).
func withPrincipal(r *http.Request, identityID string) *http.Request {
	ctx := context.WithValue(r.Context(), principalKey{}, identityID)
	ctx = identityctx.WithIdentityID(ctx, identityID)
	return r.WithContext(ctx)
}

// principalFrom reads the authenticated identity id RequireAuth stashed on the context;
// it returns "" when no principal is set (an unauthenticated path, or a misuse — the
// capability gate treats "" as unauthorized).
func principalFrom(ctx context.Context) string {
	id, _ := ctx.Value(principalKey{}).(string)
	if id != "" {
		return id
	}
	id = identityctx.IdentityID(ctx)
	return id
}

// localIdentityID is the seeded `local` identity (migration 0004) the owner-scoped web
// surfaces (Phase 36) fall back to when no authenticated principal is on the context: the
// loopback-dev pass-through (SecretConfigured=false, RequireAuth is a no-op) and any other
// no-principal path (D-25). In production (Authula) RequireAuth always stamps the principal
// via withPrincipal, so this fallback is dev-only and never silently scopes a real
// authenticated request to `local`.
const localIdentityID = "00000000-0000-0000-0000-000000000001"

// scopedIdentityID resolves the owner key for an owner-scoped store call (Phase 36 MUSR-01):
// the authenticated principal when present, else the `local` fallback so the operator is
// never self-locked-out of their own conversations/approvals by the RLS owner policy.
func scopedIdentityID(ctx context.Context) string {
	if id := principalFrom(ctx); id != "" {
		return id
	}
	return localIdentityID
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
