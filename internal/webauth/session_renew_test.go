package webauth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	authulamodels "github.com/Authula/authula/models"
	authulaservices "github.com/Authula/authula/services"
)

func TestRenewedExpiry(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		created   time.Time
		expires   time.Time
		wantDue   bool
		wantUntil time.Time
	}{
		{"just signed in", now, now.Add(sessionIdleTTL), false, time.Time{}},
		{"renewed less than an interval ago", now.Add(-3 * time.Hour), now.Add(sessionIdleTTL - 30*time.Minute), false, time.Time{}},
		{"renewed exactly an interval ago", now.Add(-3 * time.Hour), now.Add(sessionUpdateAge), true, now.Add(sessionIdleTTL)},
		{"idle for most of the window", now.Add(-20 * time.Hour), now.Add(10 * time.Minute), true, now.Add(sessionIdleTTL)},
		{"near the absolute cap", now.Add(-sessionAbsoluteTTL + 2*time.Hour), now.Add(time.Hour), true, now.Add(2 * time.Hour)},
		{"already slid to the cap", now.Add(-sessionAbsoluteTTL + time.Hour), now.Add(time.Hour), false, time.Time{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess := &authulamodels.Session{CreatedAt: tt.created, ExpiresAt: tt.expires}
			until, due := renewedExpiry(sess, now)
			if due != tt.wantDue {
				t.Fatalf("due = %v, want %v", due, tt.wantDue)
			}
			if due && !until.Equal(tt.wantUntil) {
				t.Errorf("renewed until %s, want %s", until, tt.wantUntil)
			}
		})
	}
}

func TestSessionPolicyRenewsBeforeItExpires(t *testing.T) {
	if sessionUpdateAge <= 0 || sessionUpdateAge >= sessionIdleTTL {
		t.Fatalf("update age %s must sit inside the idle window %s", sessionUpdateAge, sessionIdleTTL)
	}
	if sessionAbsoluteTTL <= sessionIdleTTL {
		t.Fatalf("absolute lifetime %s must exceed the idle window %s", sessionAbsoluteTTL, sessionIdleTTL)
	}
}

// A session Authula itself slid past the absolute cap (its /auth/* renewal knows no cap)
// is refused by the cockpit and deleted, so /auth/* refuses it from then on as well.
func TestValidate_PastAbsoluteLifetimeIsRefusedAndDeleted(t *testing.T) {
	const cookie = "rawtoken"
	const uid = "authula-user-1"
	var deleted []string
	sess := fakeSession{
		byToken: map[string]*authulamodels.Session{"h:" + cookie: {
			ID:        "sess-1",
			UserID:    uid,
			CreatedAt: time.Now().Add(-sessionAbsoluteTTL - time.Minute),
			ExpiresAt: time.Now().Add(time.Hour),
		}},
		deleted: &deleted,
	}
	v := validatorWith(sess, fakeResolver{byUser: map[string]string{uid: "aura-1"}})

	if id, err := v.Validate(reqWithCookie(cookie)); !errors.Is(err, ErrNoSession) {
		t.Fatalf("want ErrNoSession past the absolute lifetime, got id=%q err=%v", id, err)
	}
	if len(deleted) != 1 || deleted[0] != "sess-1" {
		t.Errorf("deleted sessions = %v, want [sess-1]", deleted)
	}
}

type issuedCookie struct {
	token   string
	expires time.Time
}

type fakeCookies struct{ issued []issuedCookie }

func (f *fakeCookies) SetSessionCookie(_ http.ResponseWriter, token string, expires time.Time) {
	f.issued = append(f.issued, issuedCookie{token, expires})
}

func TestRenewSession(t *testing.T) {
	const cookie = "rawtoken"
	now := time.Now().UTC()
	due := &authulamodels.Session{ID: "due", CreatedAt: now.Add(-3 * time.Hour), ExpiresAt: now.Add(30 * time.Minute)}
	fresh := &authulamodels.Session{ID: "fresh", CreatedAt: now, ExpiresAt: now.Add(sessionIdleTTL)}
	tests := []struct {
		name       string
		sessions   fakeSession
		core       bool
		req        *http.Request
		wantRenew  bool
		wantCookie bool
	}{
		{"no cookie", fakeSession{}, true, reqWithCookie(""), false, false},
		{"no core services", fakeSession{}, false, reqWithCookie(cookie), false, false},
		{"unknown token", fakeSession{byToken: map[string]*authulamodels.Session{}}, true, reqWithCookie(cookie), false, false},
		{"session service error", fakeSession{err: errors.New("db down")}, true, reqWithCookie(cookie), false, false},
		{"fresh session", fakeSession{byToken: map[string]*authulamodels.Session{"h:" + cookie: fresh}}, true, reqWithCookie(cookie), false, false},
		{"due session", fakeSession{byToken: map[string]*authulamodels.Session{"h:" + cookie: due}}, true, reqWithCookie(cookie), true, true},
		{"update fails", fakeSession{byToken: map[string]*authulamodels.Session{"h:" + cookie: due}, updateErr: errors.New("db down")}, true, reqWithCookie(cookie), false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var updated []authulamodels.Session
			tt.sessions.updated = &updated
			var core *authulaservices.CoreServices
			if tt.core {
				core = &authulaservices.CoreServices{TokenService: fakeToken{}, SessionService: tt.sessions}
			}
			cookies := &fakeCookies{}

			renewSession(fakeCore{cs: core}, cookies, httptest.NewRecorder(), tt.req)

			if renewed := len(updated) == 1; renewed != tt.wantRenew {
				t.Fatalf("renewed = %v, want %v", renewed, tt.wantRenew)
			}
			if issued := len(cookies.issued) == 1; issued != tt.wantCookie {
				t.Fatalf("cookie issued = %v, want %v", issued, tt.wantCookie)
			}
			if !tt.wantRenew {
				return
			}
			want := now.Add(sessionIdleTTL)
			if got := updated[0].ExpiresAt; got.Before(want.Add(-time.Minute)) || got.After(want.Add(time.Minute)) {
				t.Errorf("renewed expiry %s, want about %s", got, want)
			}
			if c := cookies.issued[0]; c.token != cookie || !c.expires.Equal(updated[0].ExpiresAt) {
				t.Errorf("cookie %+v does not carry the renewed session", c)
			}
		})
	}
}
