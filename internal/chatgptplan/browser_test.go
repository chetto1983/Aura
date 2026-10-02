package chatgptplan

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/identityctx"
)

type browserOpening struct {
	owner, session string
	browser        *browserFixture
}

type browserLauncherFixture struct {
	opened chan browserOpening
	open   func(context.Context, *browserFixture) error
}

func newBrowserLauncher() *browserLauncherFixture {
	return &browserLauncherFixture{opened: make(chan browserOpening, 20)}
}

func (l *browserLauncherFixture) Open(ctx context.Context, session string) (BrowserSession, error) {
	b := &browserFixture{redirect: testRedirect, authorization: make(chan string, 1), callback: make(chan url.Values, 1), failed: make(chan error, 1), closed: make(chan struct{})}
	l.opened <- browserOpening{owner: identityctx.IdentityID(ctx), session: session, browser: b}
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

func testBrowserLogin(t *testing.T, s *Service, l BrowserLauncher) *BrowserLogin {
	t.Helper()
	b := NewBrowserLogin(context.Background(), s, l)
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

func TestBrowserLoginOwnerScopedRegistrationAndCredentialCompletion(t *testing.T) {
	f := newOIDC(t)
	s := f.service(t, t.TempDir())
	l := newBrowserLauncher()
	b := testBrowserLogin(t, s, l)
	flow, err := b.Start(ownerContext("alice"), "alice", "attempt-alice")
	if err != nil || flow.Status != "authorization_required" || len(flow.AuthURL) != len("/browser/chatgpt-")+24 {
		t.Fatalf("browser flow: %+v %v", flow, err)
	}
	opened := receiveBrowser(t, l.opened)
	if opened.owner != "alice" || flow.AuthURL != "/browser/"+opened.session || !b.Owns(ownerContext("alice"), "alice", opened.session) {
		t.Fatal("browser did not retain its owner and private route")
	}
	if b.Owns(ownerContext("bob"), "alice", opened.session) || b.Owns(ownerContext("bob"), "bob", opened.session) || b.Owns(ownerContext("alice"), "alice", "chatgpt-missing") {
		t.Fatal("live login browser exposed across identities")
	}
	if _, err = b.Start(ownerContext("bob"), "alice", "attempt-alice"); !errors.Is(err, ErrIdentityRequired) {
		t.Fatal("cross-owner login accepted")
	}
	q := browserAuthorization(t, opened.browser)
	if q.Get("redirect_uri") != opened.browser.redirect || q.Get("code_challenge_method") != "S256" || q.Get("resource") != resource || q.Get("agent_name_hint") != "Aura" {
		t.Fatal("remote browser authorization lost the documented OAuth contract")
	}
	opened.browser.callback <- f.callback(t, q, "oaiapp_alice")
	receiveBrowser(t, opened.browser.closed)
	if token, err := s.AccessToken(ownerContext("alice")); err != nil || token != "access-oaiapp_alice" {
		t.Fatalf("remote sign-in did not persist its verified credential: %q %v", token, err)
	}
	if b.Owns(ownerContext("alice"), "alice", opened.session) {
		t.Fatal("completed browser remained accessible")
	}
}

func TestBrowserLoginConcurrentReuseDoesNotBlockAnotherOwner(t *testing.T) {
	f := newOIDC(t)
	s := f.service(t, t.TempDir())
	l := newBrowserLauncher()
	release := make(chan struct{})
	l.open = func(ctx context.Context, _ *browserFixture) error {
		if identityctx.IdentityID(ctx) == "alice" {
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}
	b := testBrowserLogin(t, s, l)
	type result struct {
		flow Flow
		err  error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			flow, err := b.Start(ownerContext("alice"), "alice", "attempt-alice")
			results <- result{flow, err}
		}()
	}
	receiveBrowser(t, l.opened)
	if flow, err := b.Start(ownerContext("bob"), "bob", "attempt-bob"); err != nil || flow.AuthURL == "" {
		t.Fatalf("one owner's startup blocked another: %+v %v", flow, err)
	}
	if opened := receiveBrowser(t, l.opened); opened.owner != "bob" {
		t.Fatal("repeated login launched another browser")
	}
	close(release)
	first, second := receiveBrowser(t, results), receiveBrowser(t, results)
	if first.err != nil || second.err != nil || first.flow != second.flow {
		t.Fatal("concurrent login did not reuse the ready session")
	}
	if err := b.Cancel(ownerContext("bob"), "bob", ""); err != nil {
		t.Fatal(err)
	}
	if err := b.Cancel(ownerContext("alice"), "alice", ""); err != nil {
		t.Fatal(err)
	}
}

func TestBrowserLoginCancellationPreservesAccountAndCannotCancelReplacement(t *testing.T) {
	f := newOIDC(t)
	s := f.service(t, t.TempDir())
	f.connect(t, s, "alice")
	l := newBrowserLauncher()
	b := testBrowserLogin(t, s, l)
	old, err := b.Start(ownerContext("alice"), "alice", "attempt-alice")
	if err != nil {
		t.Fatal(err)
	}
	oldBrowser := receiveBrowser(t, l.opened)
	if err = b.Cancel(ownerContext("bob"), "alice", old.AuthURL); !errors.Is(err, ErrIdentityRequired) {
		t.Fatal("another owner cancelled the login")
	}
	if err = b.Cancel(ownerContext("alice"), "alice", old.AuthURL); err != nil {
		t.Fatal(err)
	}
	receiveBrowser(t, oldBrowser.browser.closed)
	status, err := s.Status(ownerContext("alice"), "alice")
	if err != nil || !status.Connected || status.Status != "approved" || b.Owns(ownerContext("alice"), "alice", oldBrowser.session) {
		t.Fatalf("cancel damaged the active account: %+v %v", status, err)
	}
	fresh, err := b.Start(ownerContext("alice"), "alice", "attempt-alice")
	if err != nil || fresh.AuthURL == old.AuthURL {
		t.Fatal("cancelled browser route reused")
	}
	freshBrowser := receiveBrowser(t, l.opened)
	if err = b.Cancel(ownerContext("alice"), "alice", old.AuthURL); err != nil || !b.Owns(ownerContext("alice"), "alice", freshBrowser.session) {
		t.Fatal("late popup cancellation killed a replacement")
	}
	if err = b.Cancel(ownerContext("alice"), "alice", "/browser/unknown"); err != nil {
		t.Fatal(err)
	}
}

type pausedTokenTransport struct {
	base    http.RoundTripper
	entered chan struct{}
	release chan struct{}
}

func (p pausedTokenTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Path == "/token" {
		close(p.entered)
		select {
		case <-p.release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	}
	return p.base.RoundTrip(r)
}

func TestScopedCallbackDoesNotConsumeAnotherOwnersState(t *testing.T) {
	f := newOIDC(t)
	s := f.service(t, t.TempDir())
	_, q := f.authorization(t, s, "alice")
	callback := f.callback(t, q, "oaiapp_alice")
	if err := s.Callback(ownerContext("bob"), callback); !errors.Is(err, ErrInvalidCallback) {
		t.Fatal("scoped browser consumed another owner's OAuth state")
	}
	if err := s.Callback(ownerContext("alice"), callback); err != nil {
		t.Fatal("rejected cross-owner attempt consumed the legitimate state")
	}
}

func TestDisconnectDuringExchangeCannotResurrectCredentials(t *testing.T) {
	f := newOIDC(t)
	s := f.service(t, t.TempDir())
	_, q := f.authorization(t, s, "alice")
	callback := f.callback(t, q, "oaiapp_alice")
	paused := pausedTokenTransport{base: s.client.Transport, entered: make(chan struct{}), release: make(chan struct{})}
	s.client.Transport = paused
	result := make(chan error, 1)
	go func() { result <- s.Callback(ownerContext("alice"), callback) }()
	receiveBrowser(t, paused.entered)
	if _, err := s.Start(ownerContext("bob"), "bob", testRedirect); err != nil {
		t.Fatal("token exchange blocked an unrelated owner's login")
	}
	if err := s.Disconnect(ownerContext("alice"), "alice"); err != nil {
		t.Fatal(err)
	}
	close(paused.release)
	if err := receiveBrowser(t, result); !errors.Is(err, ErrInvalidCallback) {
		t.Fatalf("cancelled exchange committed its credential: %v", err)
	}
	if _, err := s.AccessToken(ownerContext("alice")); !errors.Is(err, ErrAuthorizationRequired) {
		t.Fatal("disconnect was undone by the delayed exchange")
	}
}

func assertBrowserError(t *testing.T, s *Service, want string) {
	t.Helper()
	status, err := s.Status(ownerContext("alice"), "alice")
	if err != nil || status.Status != "error" || !strings.Contains(status.Error, want) || strings.Contains(status.Error, "SECRET") {
		t.Fatalf("browser failure was not safely actionable: %+v %v", status, err)
	}
}
