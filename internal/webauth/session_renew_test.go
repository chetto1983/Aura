package webauth

import (
	"errors"
	"testing"
	"time"

	authulamodels "github.com/Authula/authula/models"
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

func TestRenewSession_NoCookieIsANoOp(t *testing.T) {
	var p Provider
	p.RenewSession(nil, reqWithCookie(""))
}
