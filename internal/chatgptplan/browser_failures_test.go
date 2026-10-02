package chatgptplan

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestBrowserLoginStartupFailuresAreSanitizedAndCloseResources(t *testing.T) {
	for _, test := range []struct {
		name string
		open func(context.Context, *browserFixture) error
	}{
		{"launch", func(context.Context, *browserFixture) error { return errors.New("SECRET-browser-output") }},
		{"navigation", func(_ context.Context, b *browserFixture) error {
			b.navigateErr = errors.New("SECRET-browser-output")
			return nil
		}},
		{"unsafe redirect", func(_ context.Context, b *browserFixture) error {
			b.redirect = "https://attacker.example/auth/callback"
			return nil
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newOIDC(t)
			s := f.service(t, t.TempDir())
			l := newBrowserLauncher()
			l.open = test.open
			b := testBrowserLogin(t, s, l)
			if flow, err := b.Start(ownerContext("alice"), "alice", "attempt-alice"); err == nil || flow.AuthURL != "" || strings.Contains(err.Error(), "SECRET") {
				t.Fatalf("failed startup returned a live browser: %+v %v", flow, err)
			}
			receiveBrowser(t, receiveBrowser(t, l.opened).browser.closed)
			assertBrowserError(t, s, "could not open")
		})
	}
}

func TestBrowserLoginStartupTimeoutAndRequestCancellation(t *testing.T) {
	for _, timeout := range []bool{true, false} {
		name := "request cancellation"
		if timeout {
			name = "startup timeout"
		}
		t.Run(name, func(t *testing.T) {
			f := newOIDC(t)
			s := f.service(t, t.TempDir())
			l := newBrowserLauncher()
			l.open = func(ctx context.Context, _ *browserFixture) error { <-ctx.Done(); return ctx.Err() }
			b := testBrowserLogin(t, s, l)
			if timeout {
				b.startupTimeout = 20 * time.Millisecond
			}
			ctx, cancel := context.WithCancel(ownerContext("alice"))
			defer cancel()
			result := make(chan error, 1)
			go func() { _, err := b.Start(ctx, "alice", "attempt-alice"); result <- err }()
			opened := receiveBrowser(t, l.opened)
			if !timeout {
				cancel()
			}
			if err := receiveBrowser(t, result); err == nil {
				t.Fatal("interrupted startup succeeded")
			}
			receiveBrowser(t, opened.browser.closed)
			if timeout {
				assertBrowserError(t, s, "startup timed out")
			} else if status, err := s.Status(ownerContext("alice"), "alice"); err != nil || status.Status != "disconnected" {
				t.Fatalf("request cancellation left a pending flow: %+v %v", status, err)
			}
		})
	}
}

func TestBrowserLoginRequestCancellationAfterReadinessKeepsLoginAlive(t *testing.T) {
	f := newOIDC(t)
	s := f.service(t, t.TempDir())
	l := newBrowserLauncher()
	b := testBrowserLogin(t, s, l)
	ctx, cancel := context.WithCancel(ownerContext("alice"))
	defer cancel()
	if _, err := b.Start(ctx, "alice", "attempt-alice"); err != nil {
		t.Fatal(err)
	}
	opened := receiveBrowser(t, l.opened)
	cancel()
	if !b.Owns(ownerContext("alice"), "alice", opened.session) || b.Owns(context.Background(), "alice", opened.session) {
		t.Fatal("HTTP cancellation or missing identity changed browser ownership")
	}
	opened.browser.callback <- f.callback(t, browserAuthorization(t, opened.browser), "oaiapp_alice")
	receiveBrowser(t, opened.browser.closed)
	if status, err := s.Status(ownerContext("alice"), "alice"); err != nil || !status.Connected {
		t.Fatal("closing the successful login request stopped its callback")
	}
}

func TestBrowserLoginCallbackFailuresAndExpiryCloseResources(t *testing.T) {
	for _, failure := range []string{"browser closed", "expired", "invalid state", "declined"} {
		t.Run(failure, func(t *testing.T) {
			f := newOIDC(t)
			s := f.service(t, t.TempDir())
			l := newBrowserLauncher()
			b := testBrowserLogin(t, s, l)
			if failure == "expired" {
				b.timeToLive = 50 * time.Millisecond
			}
			if _, err := b.Start(ownerContext("alice"), "alice", "attempt-alice"); err != nil {
				t.Fatal(err)
			}
			opened := receiveBrowser(t, l.opened)
			q := browserAuthorization(t, opened.browser)
			want := "browser stopped"
			switch failure {
			case "browser closed":
				opened.browser.failed <- errors.New("SECRET-helper-output")
			case "expired":
				want = "sign-in expired"
			case "invalid state":
				opened.browser.callback <- url.Values{"state": {"other-owner-state"}, "code": {"secret"}}
				want = "callback was rejected"
			case "declined":
				opened.browser.callback <- url.Values{"state": {q.Get("state")}, "error": {"SECRET-denial-description"}}
				want = "authorization was declined"
			}
			receiveBrowser(t, opened.browser.closed)
			assertBrowserError(t, s, want)
			if _, err := s.AccessToken(ownerContext("alice")); !errors.Is(err, ErrAuthorizationRequired) {
				t.Fatal("failed browser stored a credential")
			}
		})
	}
}

func TestBrowserLoginShutdownCancelsAndJoinsPendingSessions(t *testing.T) {
	f := newOIDC(t)
	s := f.service(t, t.TempDir())
	l := newBrowserLauncher()
	root, cancel := context.WithCancel(context.Background())
	b := NewBrowserLogin(root, s, l)
	defer func() { cancel(); _ = b.Close() }()
	for _, owner := range []string{"alice", "bob"} {
		if _, err := b.Start(ownerContext(owner), owner, "attempt-"+owner); err != nil {
			t.Fatal(err)
		}
	}
	first, second := receiveBrowser(t, l.opened), receiveBrowser(t, l.opened)
	cancel()
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	receiveBrowser(t, first.browser.closed)
	receiveBrowser(t, second.browser.closed)
	if _, err := b.Start(ownerContext("alice"), "alice", "attempt-alice"); err == nil {
		t.Fatal("shutdown manager reopened a browser")
	}
	if status, err := s.Status(ownerContext("alice"), "alice"); err != nil || status.Status != "disconnected" {
		t.Fatal("shutdown left the authorization state live")
	}
}

func TestBrowserLoginCleanupErrorsAndCancelledRequests(t *testing.T) {
	f := newOIDC(t)
	s := f.service(t, t.TempDir())
	l := newBrowserLauncher()
	l.open = func(_ context.Context, b *browserFixture) error {
		b.closeErr = errors.New("SECRET-process-output")
		return nil
	}
	b := testBrowserLogin(t, s, l)
	flow, err := b.Start(ownerContext("alice"), "alice", "attempt-alice")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(ownerContext("alice"))
	cancel()
	if err = b.Cancel(ctx, "alice", flow.AuthURL); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled request mutated a live browser")
	}
	if err = b.Cancel(ownerContext("alice"), "alice", flow.AuthURL); err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatal("cleanup failure was lost or leaked browser output")
	}
	if err = b.Close(); err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatal("shutdown did not report the cleanup failure")
	}
	if _, err = b.Start(ctx, "alice", "attempt-alice"); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled login request accepted")
	}
}

func TestBrowserServiceStaleFailureCannotMutateReplacement(t *testing.T) {
	f := newOIDC(t)
	s := f.service(t, t.TempDir())
	old, _ := f.authorization(t, s, "alice")
	fresh, err := s.Start(ownerContext("alice"), "alice", "http://127.0.0.1:8088/auth/callback")
	if err != nil {
		t.Fatal(err)
	}
	s.cancelBrowser("alice", old.AuthURL)
	s.failBrowser("alice", old.AuthURL, "stale")
	s.failBrowser("alice", "", "startup failed")
	if status, err := s.Status(ownerContext("alice"), "alice"); err != nil || status.Status != "authorization_required" || s.flows["alice"].flow != fresh {
		t.Fatal("stale browser cleanup replaced the current flow")
	}
}
