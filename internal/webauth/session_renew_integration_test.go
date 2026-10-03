//go:build webauth_integration

// Live coverage of the session lifetime against the embedded Authula and its Postgres
// schema: RenewSession slides a due session and re-issues the hardened cookie, leaves a
// fresh one alone, and Validate deletes a session past its absolute lifetime.
//
//	go test -tags webauth_integration ./internal/webauth -run TestSessionLifetime -count=1

package webauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	authulamodels "github.com/Authula/authula/models"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSessionLifetime_Live(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	dsn := envOrSkip(t, "AURA_AUTHULA_DSN")
	provider, err := New(Config{
		DSN:            dsn,
		Secret:         envOrSkip(t, "AURA_AUTHULA_SECRET"),
		TrustedOrigins: []string{"http://127.0.0.1:9080"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		if cerr := provider.Close(); cerr != nil {
			t.Errorf("Close: %v", cerr)
		}
	})
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open authula pool: %v", err)
	}
	t.Cleanup(pool.Close)

	core := provider.CoreServices()
	email := "session-lifetime+" + time.Now().Format("150405.000000") + "@aura.local"
	user, err := core.UserService.Create(ctx, "session-lifetime", email, true, nil, nil)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() { _ = core.UserService.Delete(context.Background(), user.ID) })

	newSession := func(t *testing.T) (string, *authulamodels.Session) {
		t.Helper()
		raw, gerr := core.TokenService.Generate()
		if gerr != nil {
			t.Fatalf("token generate: %v", gerr)
		}
		sess, cerr := core.SessionService.Create(ctx, user.ID, core.TokenService.Hash(raw), nil, nil, sessionIdleTTL)
		if cerr != nil {
			t.Fatalf("session create: %v", cerr)
		}
		return raw, sess
	}
	setColumn := func(t *testing.T, column, id string, at time.Time) {
		t.Helper()
		if _, xerr := pool.Exec(ctx, "UPDATE authula.sessions SET "+column+" = $1 WHERE id = $2", at.UTC(), id); xerr != nil {
			t.Fatalf("set %s: %v", column, xerr)
		}
	}
	reload := func(t *testing.T, raw string) *authulamodels.Session {
		t.Helper()
		sess, gerr := core.SessionService.GetByToken(ctx, core.TokenService.Hash(raw))
		if gerr != nil {
			t.Fatalf("reload session: %v", gerr)
		}
		return sess
	}
	renew := func(raw string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: raw})
		rec := httptest.NewRecorder()
		provider.RenewSession(rec, req)
		return rec
	}

	t.Run("a due session slides and its cookie is re-issued", func(t *testing.T) {
		raw, sess := newSession(t)
		setColumn(t, "expires_at", sess.ID, time.Now().Add(30*time.Minute))

		rec := renew(raw)

		got := reload(t, raw)
		if want := time.Now().Add(sessionIdleTTL); got.ExpiresAt.Before(want.Add(-time.Minute)) || got.ExpiresAt.After(want.Add(time.Minute)) {
			t.Fatalf("expires_at = %s, want about %s", got.ExpiresAt, want)
		}
		cookies := rec.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatalf("Set-Cookie count = %d, want 1", len(cookies))
		}
		c := cookies[0]
		if c.Name != SessionCookieName || c.Value != raw || !c.HttpOnly || !c.Secure ||
			c.SameSite != http.SameSiteStrictMode || c.Path != "/" {
			t.Errorf("re-issued cookie lost its hardening: %+v", c)
		}
		if c.MaxAge < int((sessionIdleTTL - time.Minute).Seconds()) {
			t.Errorf("cookie Max-Age = %d, want about %d", c.MaxAge, int(sessionIdleTTL.Seconds()))
		}
	})

	t.Run("a fresh session is not rewritten", func(t *testing.T) {
		raw, sess := newSession(t)
		rec := renew(raw)
		if n := len(rec.Result().Cookies()); n != 0 {
			t.Errorf("Set-Cookie count = %d, want 0 for a fresh session", n)
		}
		if got := reload(t, raw); !got.ExpiresAt.Equal(sess.ExpiresAt) {
			t.Errorf("expires_at moved from %s to %s", sess.ExpiresAt, got.ExpiresAt)
		}
	})

	t.Run("a session past its absolute lifetime is refused and deleted", func(t *testing.T) {
		raw, sess := newSession(t)
		setColumn(t, "created_at", sess.ID, time.Now().Add(-sessionAbsoluteTTL-time.Minute))

		validator := NewValidator(provider, fakeResolver{byUser: map[string]string{user.ID: "aura-identity"}})
		req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: raw})
		if _, verr := validator.Validate(req); !errors.Is(verr, ErrNoSession) {
			t.Fatalf("Validate past the absolute lifetime: want ErrNoSession, got %v", verr)
		}
		if got := reload(t, raw); got != nil {
			t.Errorf("session %s still stored after its absolute lifetime", got.ID)
		}
	})
}
