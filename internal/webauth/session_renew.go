package webauth

import (
	"log/slog"
	"net/http"
	"time"

	authulamodels "github.com/Authula/authula/models"
)

// session_renew.go is the session lifetime: an idle window that use keeps sliding,
// capped by an absolute lifetime counted from sign-in.
//
// Authula slides a session only inside its own AuthMiddleware, which guards /auth/*.
// The cockpit's routes are validated by Validator (the A2 seam) and never reach it, so a
// session used all day still died exactly 12h after sign-in. Measured 2026-10-03 on the
// local stack: three authenticated /api calls left authula.sessions.expires_at unchanged,
// one GET /auth/me moved it. RequireAuth therefore drives the renewal itself.
const (
	// sessionIdleTTL is how long a session survives without being used.
	sessionIdleTTL = 12 * time.Hour
	// sessionRenewInterval bounds renewal writes to one per session per interval.
	sessionRenewInterval = time.Hour
	// sessionAbsoluteTTL ends a session however busy it is (OWASP Session Management
	// Cheat Sheet: an idle timeout alone lets a stolen cookie live forever).
	sessionAbsoluteTTL = 7 * 24 * time.Hour
	// sessionUpdateAge is the same policy in Authula's terms: it renews once the time left
	// drops to UpdateAge, so idle minus interval renews at most once an interval.
	sessionUpdateAge = sessionIdleTTL - sessionRenewInterval
)

// sessionDeadline is when a session stops being valid, whatever its expires_at says:
// Authula's own renewal on /auth/* knows nothing of the absolute cap.
func sessionDeadline(sess *authulamodels.Session) time.Time {
	return sess.CreatedAt.Add(sessionAbsoluteTTL)
}

// renewedExpiry returns the expiry a renewal at now writes, and whether one is due.
func renewedExpiry(sess *authulamodels.Session, now time.Time) (time.Time, bool) {
	if sess.ExpiresAt.Sub(now) > sessionUpdateAge {
		return time.Time{}, false
	}
	next := now.Add(sessionIdleTTL)
	if deadline := sessionDeadline(sess); next.After(deadline) {
		next = deadline
	}
	return next, next.After(sess.ExpiresAt)
}

// sessionCookieSetter re-issues the session cookie; Authula's session plugin implements it.
type sessionCookieSetter interface {
	SetSessionCookie(w http.ResponseWriter, sessionToken string, expiresAt time.Time)
}

// RenewSession slides the request's session forward when a renewal is due and re-issues
// its cookie through Authula's session plugin, so the attributes stay Authula's. It never
// fails the request: a missed renewal leaves the session valid until its current expiry.
func (p *Provider) RenewSession(w http.ResponseWriter, r *http.Request) {
	renewSession(p, p.session, w, r)
}

func renewSession(sessions coreServicesProvider, cookies sessionCookieSetter, w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil || cookie.Value == "" {
		return
	}
	core := sessions.CoreServices()
	if core == nil || core.TokenService == nil || core.SessionService == nil {
		return
	}
	sess, err := core.SessionService.GetByToken(r.Context(), core.TokenService.Hash(cookie.Value))
	if err != nil || sess == nil {
		return
	}
	expires, due := renewedExpiry(sess, time.Now().UTC())
	if !due {
		return
	}
	renewed := *sess
	renewed.ExpiresAt = expires
	if _, err := core.SessionService.Update(r.Context(), &renewed); err != nil {
		slog.Warn("webauth: session renewal failed; it stays valid until its current expiry",
			"err", err)
		return
	}
	cookies.SetSessionCookie(w, cookie.Value, expires)
}
