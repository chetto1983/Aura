package chatgptplan

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestBrowserLoginDifferentTabAttemptReplacesAndRejectsOldCancellation(t *testing.T) {
	f := newOIDC(t)
	s := f.service(t, t.TempDir())
	l := newBrowserLauncher()
	b := testBrowserLogin(t, s, l)
	oldRequest, cancelOldRequest := context.WithCancel(ownerContext("alice"))
	defer cancelOldRequest()
	old, err := b.Start(oldRequest, "alice", "tab-a-attempt")
	if err != nil {
		t.Fatal(err)
	}
	oldBrowser := receiveBrowser(t, l.opened)
	oldAuthorization := browserAuthorization(t, oldBrowser.browser)
	b.mu.Lock()
	oldPending := b.pending["alice"]
	b.mu.Unlock()
	fresh, err := b.Start(ownerContext("alice"), "alice", "tab-b-attempt")
	if err != nil || fresh.AuthURL == old.AuthURL {
		t.Fatal("a different tab reused the old cancellable browser route")
	}
	receiveBrowser(t, oldBrowser.browser.closed)
	if flow, err := oldPending.startResult(); !errors.Is(err, context.Canceled) || flow.AuthURL != "" {
		t.Fatal("a delayed old response advertised the replaced browser as live")
	}
	freshBrowser := receiveBrowser(t, l.opened)
	if b.Owns(ownerContext("alice"), "alice", oldBrowser.session) || !b.Owns(ownerContext("alice"), "alice", freshBrowser.session) {
		t.Fatal("replacement did not transfer live-browser ownership")
	}
	if err = s.Callback(ownerContext("alice"), f.callback(t, oldAuthorization, "oaiapp_alice")); !errors.Is(err, ErrInvalidCallback) {
		t.Fatal("replaced tab's callback was still valid")
	}
	cancelOldRequest()
	if err = b.Cancel(ownerContext("alice"), "alice", old.AuthURL); err != nil || !b.Owns(ownerContext("alice"), "alice", freshBrowser.session) {
		t.Fatal("closing the old tab cancelled the replacement")
	}
	retry, err := b.Start(ownerContext("alice"), "alice", "tab-b-attempt")
	if err != nil || retry != fresh {
		t.Fatal("retry of the same attempt replaced its browser")
	}
	select {
	case <-l.opened:
		t.Fatal("same-attempt retry launched another browser")
	default:
	}
	freshBrowser.browser.callback <- f.callback(t, browserAuthorization(t, freshBrowser.browser), "oaiapp_alice")
	receiveBrowser(t, freshBrowser.browser.closed)
	if token, err := s.AccessToken(ownerContext("alice")); err != nil || token != "access-oaiapp_alice" {
		t.Fatal("replacement tab could not finish sign-in")
	}
}

func TestBrowserLoginReplacementRequestCancellationIsBounded(t *testing.T) {
	f := newOIDC(t)
	s := f.service(t, t.TempDir())
	l := newBrowserLauncher()
	closing, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	defer unblock.Do(func() { close(release) })
	l.open = func(_ context.Context, browser *browserFixture) error {
		browser.closing, browser.closeBlock = closing, release
		return nil
	}
	b := testBrowserLogin(t, s, l)
	if _, err := b.Start(ownerContext("alice"), "alice", "tab-a-attempt"); err != nil {
		t.Fatal(err)
	}
	receiveBrowser(t, l.opened)
	ctx, cancel := context.WithTimeout(ownerContext("alice"), 30*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := b.Start(ctx, "alice", "tab-b-attempt"); result <- err }()
	receiveBrowser(t, closing)
	if err := receiveBrowser(t, result); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("replacement ignored the request lifetime while joining the old browser")
	}
	unblock.Do(func() { close(release) })
}

func TestBrowserLoginRequiresAnAttemptID(t *testing.T) {
	f := newOIDC(t)
	l := newBrowserLauncher()
	b := testBrowserLogin(t, f.service(t, t.TempDir()), l)
	if flow, err := b.Start(ownerContext("alice"), "alice", ""); err == nil || flow.AuthURL != "" {
		t.Fatal("a login without a unique attempt was accepted")
	}
	select {
	case <-l.opened:
		t.Fatal("invalid attempt started a browser")
	default:
	}
}
