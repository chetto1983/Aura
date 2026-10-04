package agui

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

// Why a request was sent to the login page instead of the cockpit. Until 2026-10-04 the gate
// said nothing: a member reported being asked to sign in again after opening a PDF on an
// iPad, and their appliance's log could not tell a cookie the browser never sent from one
// the server refused (prd.md §3).
const (
	refusedNoCookie     = "no_cookie"
	refusedSession      = "session_refused"
	refusedIdentityGone = "identity_gone"
)

// sessionChallenge is the WWW-Authenticate value on the 401 that means the request has
// no live session. The SPA sends the person to the login page on it and on nothing else:
// a handler's own 401 (the calendar not yet authorized, a relayed upstream refusal)
// arrives while the session is still valid (web/src/api/sessionExpiry.ts).
const sessionChallenge = "Session"

// userAgentLogCap bounds the user agent a refusal logs; a header is attacker-sized.
const userAgentLogCap = 160

func (d AuthDeps) missingOrRefused(r *http.Request) string {
	name := d.SessionCookieName
	if name == "" {
		name = sessionCookieName
	}
	if _, err := r.Cookie(name); err != nil {
		return refusedNoCookie
	}
	return refusedSession
}

// refuseSession logs why the request has no session, then sends it to the login page.
//
// A cookie-less API call is logged at debug: it is what a scanner, or a tab left open after
// signing out, sends all day, and it says nothing about a person losing a session. Every
// other refusal is info, a navigation without a cookie included -- the case the iPad report
// needs to see.
func (d AuthDeps) refuseSession(w http.ResponseWriter, r *http.Request, reason string) {
	navigation := wantsHTML(r)
	level := slog.LevelInfo
	if reason == refusedNoCookie && !navigation {
		level = slog.LevelDebug
	}
	userAgent := r.UserAgent()
	if len(userAgent) > userAgentLogCap {
		userAgent = userAgent[:userAgentLogCap]
	}
	slog.Log(r.Context(), level, "web session refused",
		"reason", reason, "method", r.Method, "path", r.URL.Path,
		"navigation", navigation, "user_agent", userAgent)
	d.redirectToLogin(w, r)
}

// redirectToLogin sends a browser navigation (Accept: text/html GET) to the login page
// with 302, but answers an API/XHR request with a plain 401 — so a fetch() gets a clean
// status code instead of an HTML login page it cannot use.
func (d AuthDeps) redirectToLogin(w http.ResponseWriter, r *http.Request) {
	if wantsHTML(r) {
		http.Redirect(w, r, d.loginTarget(r), http.StatusFound) //nolint:gosec // always the same-origin login path; the request URI only rides in next, query-escaped, and the SPA re-checks it with safeReturnPath.
		return
	}
	w.Header().Set("WWW-Authenticate", sessionChallenge)
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}

// loginTarget is the login page, carrying in `next` the page a refused navigation asked
// for, so signing in lands there again -- what the SPA's own expiry redirect already does
// (web/src/api/sessionExpiry.ts). The root needs no `next`, and an /api/ path is left out:
// the SPA routes `next` client-side, and an /api/ URL is a file or a response, not a page
// it can show.
func (d AuthDeps) loginTarget(r *http.Request) string {
	if r.URL.Path == "/" || strings.HasPrefix(r.URL.Path, "/api/") {
		return d.loginPath()
	}
	return d.loginPath() + "?next=" + url.QueryEscape(r.URL.RequestURI())
}
