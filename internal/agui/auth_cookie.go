// auth_cookie.go holds the stdlib-only session-cookie crypto for the WEB-03
// in-binary web-auth boundary (D-01/D-02). It is split out of auth.go to keep each
// file well under the 600-LOC ceiling (CLAUDE.md NO GOD CLASS): this file owns the
// HMAC verification + cookie-clear helpers; auth.go owns
// the HTTP middleware + handlers.
//
// The session is a STATELESS, HMAC-SHA256-signed cookie — no session store, no new
// credential storage (D-01). The cookie value is base64url(payload)"."base64url(MAC)
// where payload is "{identityID}|{issuedUnix}"; a single server key derived once from
// sha256(AURA_WEB_AUTH_SECRET) (RESEARCH A2 — one operator secret governs both login
// and signing) keys the MAC. Tamper ⇒ MAC mismatch ⇒ reject; expiry ⇒ issued_at+TTL
// check ⇒ reject. MAC comparison is constant-time so the boundary leaks no timing oracle.
//
// gorilla/securecookie + gorilla/sessions are deliberately NOT used: they are not
// vendored and adding them would violate the no-framework posture (server.go:88) for
// what is ~40 LOC of stdlib HMAC (RESEARCH §Alternatives Considered).
package agui

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// sessionCookieName uses the __Host- prefix, which the browser enforces requires
// Secure:true + no Domain + Path:"/" — a defense-in-depth bind to the exact origin
// (golang-security cookies.md §Cookie Prefix).
const sessionCookieName = "__Host-aura_session"

// defaultSessionTTL is the absolute session lifetime (Open Q2 recommendation: 12h
// absolute, NOT idle — a fixed ceiling regardless of activity). verifySession rejects
// any cookie whose issued_at + this TTL is in the past; the wiring layer may override
// it via AuthDeps.TTL but 12h is the default.
const defaultSessionTTL = 12 * time.Hour

// verifySession is the constant-time, fail-closed cookie verify (T-24-10/T-24-11). It
// returns the bound identityID and ok==true only when (a) the value is well-formed,
// (b) the MAC matches (hmac.Equal — constant-time, never bytes.Equal), and (c) the
// absolute TTL has not elapsed. Any decode error, part-count mismatch, MAC mismatch,
// or expiry returns ("", false) WITHOUT a panic — a forged/edited/expired cookie is
// rejected, never trusted.
func verifySession(key []byte, value string, ttl time.Duration, now time.Time) (identityID string, ok bool) {
	parts := strings.SplitN(value, ".", 2)
	if len(parts) != 2 {
		return "", false
	}
	rawPayload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", false
	}
	gotSig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", false
	}
	// Reject any NON-canonical base64 encoding of either part: RawURLEncoding tolerates
	// non-zero unused trailing bits, so two distinct cookie strings can decode to the
	// same bytes. Without this, flipping the final base64 char of the payload/sig leaves
	// the decoded value (and thus the re-computed MAC) unchanged — a tamper bypass. Re-
	// encoding the decoded bytes and comparing to the presented part forces canonical form.
	if base64.RawURLEncoding.EncodeToString(rawPayload) != parts[0] ||
		base64.RawURLEncoding.EncodeToString(gotSig) != parts[1] {
		return "", false
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(rawPayload)
	if !hmac.Equal(gotSig, mac.Sum(nil)) { // constant-time MAC compare (network.md)
		return "", false
	}
	pp := strings.SplitN(string(rawPayload), "|", 2)
	if len(pp) != 2 {
		return "", false
	}
	issuedUnix, err := strconv.ParseInt(pp[1], 10, 64)
	if err != nil || now.After(time.Unix(issuedUnix, 0).Add(ttl)) {
		return "", false // expired (absolute TTL) — fail closed
	}
	return pp[0], true
}

// clearSessionCookie expires the session on logout (MaxAge < 0 deletes it). It mirrors
// the issuer's Name/Path/flags so the browser matches and drops the exact cookie.
func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1, // delete now
	})
}
