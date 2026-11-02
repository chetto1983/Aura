package browserlogin

import (
	"context"
	"crypto/rand"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"

	"go.uber.org/goleak"

	"github.com/chetto1983/aura/internal/identityctx"
)

var (
	ErrIdentityRequired      = errors.New("identity required")
	ErrAuthorizationRequired = errors.New("authorization required")
	ErrInvalidCallback       = errors.New("invalid callback")
)

const testRedirect = "http://127.0.0.1:8089/auth/callback"

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

type authorizerPending struct{ authURL, state, status, failure string }
type authorizerFixture struct {
	mu     sync.Mutex
	flows  map[string]*authorizerPending
	tokens map[string]string
}

func newAuthorizerFixture(t *testing.T) *authorizerFixture {
	t.Helper()
	return &authorizerFixture{flows: map[string]*authorizerPending{}, tokens: map[string]string{}}
}

func (a *authorizerFixture) service(t *testing.T, _ string) *authorizerFixture { t.Helper(); return a }

func (a *authorizerFixture) Start(ctx context.Context, owner, redirect string) (string, error) {
	if err := fixtureOwner(ctx, owner); err != nil {
		return "", err
	}
	u, err := url.Parse(redirect)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Path != "/auth/callback" {
		return "", ErrInvalidCallback
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	state := rand.Text()
	authURL := "https://provider.example/authorize?state=" + state
	a.flows[owner] = &authorizerPending{authURL: authURL, state: state, status: "authorization_required"}
	return authURL, nil
}

func (a *authorizerFixture) Callback(ctx context.Context, query url.Values) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	owner := identityctx.IdentityID(ctx)
	p := a.flows[owner]
	if ctx.Err() != nil || p == nil || p.state != query.Get("state") || p.status != "authorization_required" {
		return ErrInvalidCallback
	}
	if query.Get("error") != "" {
		p.status, p.failure = "error", "authorization was declined"
		return ErrInvalidCallback
	}
	if query.Get("code") == "" {
		return ErrInvalidCallback
	}
	a.tokens[owner] = "access-" + query.Get("code")
	p.status = "approved"
	return nil
}

func (a *authorizerFixture) Cancel(owner, authURL string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if p := a.flows[owner]; p != nil && authURL != "" && p.authURL == authURL && p.status != "approved" {
		delete(a.flows, owner)
	}
}

func (a *authorizerFixture) Fail(owner, authURL, message string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p := a.flows[owner]
	if authURL == "" {
		if p != nil && (p.status == "authorization_required" || p.status == "starting") {
			return
		}
		p = &authorizerPending{}
		a.flows[owner] = p
	} else if p == nil || p.authURL != authURL || p.status != "authorization_required" {
		return
	}
	p.status, p.failure = "error", message
}

func (a *authorizerFixture) Status(_ context.Context, owner string) (struct {
	Connected     bool
	Status, Error string
}, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := struct {
		Connected     bool
		Status, Error string
	}{Connected: a.tokens[owner] != "", Status: "disconnected"}
	if out.Connected {
		out.Status = "approved"
	}
	if p := a.flows[owner]; p != nil {
		out.Status, out.Error = p.status, p.failure
	}
	return out, nil
}

func (a *authorizerFixture) AccessToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if token := a.tokens[identityctx.IdentityID(ctx)]; token != "" {
		return token, nil
	}
	return "", ErrAuthorizationRequired
}

func (*authorizerFixture) callback(t *testing.T, query url.Values, subject string) url.Values {
	t.Helper()
	return url.Values{"state": {query.Get("state")}, "code": {subject}}
}

func ownerContext(owner string) context.Context {
	return identityctx.WithIdentityID(context.Background(), owner)
}
func fixtureOwner(ctx context.Context, owner string) error {
	if strings.TrimSpace(owner) == "" || strings.TrimSpace(owner) != owner {
		return ErrIdentityRequired
	}
	if scoped := identityctx.IdentityID(ctx); scoped != "" && scoped != owner {
		return ErrIdentityRequired
	}
	return ctx.Err()
}

type fixtureLogin struct {
	*Login
	authorizer Authorizer
}

func (l *fixtureLogin) Start(ctx context.Context, owner, attempt string) (Flow, error) {
	return l.Login.Start(ctx, owner, attempt, LaunchOptions{}, l.authorizer)
}

func newFixtureLogin(root context.Context, a Authorizer, launcher Launcher) *fixtureLogin {
	return &fixtureLogin{Login: New(root, launcher, Options{
		Prefix: "chatgpt-", Route: "/browser/", CheckOwner: fixtureOwner,
		Messages: Messages{
			AttemptRequired:  "chatgpt plan: login attempt id is required",
			ShuttingDown:     "chatgpt plan: browser login is shutting down",
			StartupTimeout:   "chatgpt plan: browser startup timed out; try signing in again",
			CleanupFailed:    "chatgpt plan: could not close the login browser; try again",
			CleanupLog:       "ChatGPT login browser cleanup failed",
			OpenFailed:       "chatgpt plan: could not open the login browser; try signing in again",
			BrowserStopped:   "chatgpt plan: the login browser stopped; try signing in again",
			CallbackRejected: "chatgpt plan: the sign-in callback was rejected; try signing in again",
			Expired:          "chatgpt plan: sign-in expired; try signing in again",
		},
	}), authorizer: a}
}
