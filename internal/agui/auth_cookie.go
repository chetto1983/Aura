// auth_cookie.go holds the stdlib-only session-cookie crypto for the WEB-03
// in-binary web-auth boundary (D-01/D-02). It is split out of auth.go to keep each
// file well under the 600-LOC ceiling (CLAUDE.md NO GOD CLASS): this file owns the
// secret compare + the HMAC sign/verify + the cookie (un)set helpers; auth.go owns
// the HTTP middleware + handlers.
//
// The session is a STATELESS, HMAC-SHA256-signed cookie — no session store, no new
// credential storage (D-01). The cookie value is base64url(payload)"."base64url(MAC)
// where payload is "{identityID}|{issuedUnix}"; a single server key derived once from
// sha256(AURA_WEB_AUTH_SECRET) (RESEARCH A2 — one operator secret governs both login
// and signing) keys the MAC. Tamper ⇒ MAC mismatch ⇒ reject; expiry ⇒ issued_at+TTL
// check ⇒ reject. Every compare is constant-time (subtle.ConstantTimeCompare for the
// secret, hmac.Equal for the MAC) so the boundary leaks no timing oracle.
//
// gorilla/securecookie + gorilla/sessions are deliberately NOT used: they are not
// vendored and adding them would violate the no-framework posture (server.go:88) for
// what is ~40 LOC of stdlib HMAC (RESEARCH §Alternatives Considered).
package agui

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// sessionCookieName uses the __Host- prefix, which the browser enforces requires
// Secure:true + no Domain + Path:"/" — a defense-in-depth bind to the exact origin
// (golang-security cookies.md §Cookie Prefix). setSessionCookie writes exactly those
// attributes so the prefix contract holds.
const sessionCookieName = "__Host-aura_session"

// defaultSessionTTL is the absolute session lifetime (Open Q2 recommendation: 12h
// absolute, NOT idle — a fixed ceiling regardless of activity). verifySession rejects
// any cookie whose issued_at + this TTL is in the past; the wiring layer may override
// it via AuthDeps.TTL but 12h is the default.
const defaultSessionTTL = 12 * time.Hour

// deriveSigningKey turns the operator secret into the 32-byte HMAC key. A single
// AURA_WEB_AUTH_SECRET governs both the login compare and the cookie signature
// (RESEARCH A2) — sha256 stretches the arbitrary-length passphrase to a fixed key.
// An empty secret yields a deterministic-but-useless key; validateSecret already
// fail-closes login when the secret is empty, so no valid cookie is ever minted.
func deriveSigningKey(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

// validateSecret is the fail-closed login check (D-01 / T-24-09). An empty configured
// secret means in-binary auth is NOT enabled — reject ALL logins (never accept an empty
// passphrase, never treat ""=="" as a match). The compare is constant-time so a login
// probe cannot time the secret byte-by-byte (subtle handles unequal lengths safely).
func validateSecret(provided, configured string) bool {
	if configured == "" {
		return false // fail-closed: no secret configured ⇒ no login possible via this path
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(configured)) == 1
}

// signSession builds the cookie value: base64url(identityID "|" issuedUnix) "."
// base64url(HMAC-SHA256(payload, key)). RawURLEncoding keeps the value cookie-safe
// (ASCII, no padding "=").
func signSession(key []byte, identityID string, issued time.Time) string {
	payload := identityID + "|" + strconv.FormatInt(issued.Unix(), 10)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payload))
	sig := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." +
		base64.RawURLEncoding.EncodeToString(sig)
}

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

// setSessionCookie writes the locked D-02/SC3 cookie attributes via the stdlib
// serializer (never a hand-rolled Set-Cookie string). The __Host- prefix REQUIRES
// Secure:true + Domain:"" + Path:"/", which is exactly what is written here.
func setSessionCookie(w http.ResponseWriter, value string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,                    // no JS access (CWE-1004)
		Secure:   true,                    // HTTPS only (CWE-614, __Host- requires it)
		SameSite: http.SameSiteStrictMode, // CSRF baseline (CWE-352)
		MaxAge:   int(ttl.Seconds()),
	})
}

// clearSessionCookie expires the session on logout (MaxAge < 0 deletes it). It mirrors
// the Name/Path/flags of setSessionCookie so the browser matches and drops the exact
// __Host- cookie.
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
