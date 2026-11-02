package browserlogin

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/identityctx"
)

type browserOpening struct {
	owner, session string
	opts           LaunchOptions
	browser        *browserFixture
}

type browserLauncherFixture struct {
	opened chan browserOpening
	open   func(context.Context, *browserFixture) error
}

func newBrowserLauncher() *browserLauncherFixture {
	return &browserLauncherFixture{opened: make(chan browserOpening, 20)}
}

func (l *browserLauncherFixture) Open(ctx context.Context, session string, opts LaunchOptions) (Session, error) {
	b := &browserFixture{redirect: testRedirect, authorization: make(chan string, 1), callback: make(chan url.Values, 1), failed: make(chan error, 1), closed: make(chan struct{})}
	l.opened <- browserOpening{owner: identityctx.IdentityID(ctx), session: session, opts: opts, browser: b}
	if l.open != nil {
		if err := l.open(ctx, b); err != nil {
			return b, err
		}
	}
	return b, nil
}

type browserFixture struct {
	redirect      string
	authorization chan string
	callback      chan url.Values
	failed        chan error
	closed        chan struct{}
	closeOnce     sync.Once
	closing       chan struct{}
	closeBlock    <-chan struct{}
	callbackReady chan struct{}
	callbackGate  <-chan struct{}
	navigateErr   error
	closeErr      error
}

func (b *browserFixture) RedirectURI() string { return b.redirect }
func (b *browserFixture) Navigate(_ context.Context, auth string) error {
	b.authorization <- auth
	return b.navigateErr
}
func (b *browserFixture) Callback(ctx context.Context) (url.Values, error) {
	if b.callbackReady != nil {
		close(b.callbackReady)
		<-b.callbackGate
	}
	select {
	case query := <-b.callback:
		return query, nil
	case err := <-b.failed:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (b *browserFixture) Close() error {
	b.closeOnce.Do(func() {
		if b.closing != nil {
			close(b.closing)
		}
		if b.closeBlock != nil {
			<-b.closeBlock
		}
		close(b.closed)
	})
	return b.closeErr
}

func testBrowserLogin(t *testing.T, s *authorizerFixture, l Launcher) *fixtureLogin {
	t.Helper()
	b := newFixtureLogin(context.Background(), s, l)
	t.Cleanup(func() { _ = b.Close() })
	return b
}

func receiveBrowser[T any](t *testing.T, c <-chan T) T {
	t.Helper()
	select {
	case value := <-c:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("browser lifecycle did not finish")
		var zero T
		return zero
	}
}

func browserAuthorization(t *testing.T, browser *browserFixture) url.Values {
	t.Helper()
	u, err := url.Parse(receiveBrowser(t, browser.authorization))
	if err != nil {
		t.Fatal(err)
	}
	return u.Query()
}

func assertBrowserError(t *testing.T, s *authorizerFixture, want string) {
	t.Helper()
	status, err := s.Status(ownerContext("alice"), "alice")
	if err != nil || status.Status != "error" || !strings.Contains(status.Error, want) || strings.Contains(status.Error, "SECRET") {
		t.Fatalf("browser failure was not safely actionable: %+v %v", status, err)
	}
}
