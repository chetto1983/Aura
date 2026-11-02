package browserlogin

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/identityctx"
)

type browserLogWriter chan string

func (w browserLogWriter) Write(data []byte) (int, error) {
	w <- string(data)
	return len(data), nil
}

func TestBrowserLoginSuccessfulCallbackReportsCleanupFailureImmediately(t *testing.T) {
	f := newAuthorizerFixture(t)
	s := f.service(t, t.TempDir())
	l := newBrowserLauncher()
	l.open = func(_ context.Context, browser *browserFixture) error {
		browser.closeErr = errors.New("SECRET-helper-output https://auth.openai.com?code=PRIVATE")
		return nil
	}
	b := testBrowserLogin(t, s, l)
	warnings := make(browserLogWriter, 1)
	b.logger = slog.New(slog.NewJSONHandler(warnings, nil))
	if _, err := b.Start(ownerContext("alice"), "alice", "attempt-alice"); err != nil {
		t.Fatal(err)
	}
	opening := receiveBrowser(t, l.opened)
	opening.browser.callback <- f.callback(t, browserAuthorization(t, opening.browser), "oaiapp_alice")
	warning := receiveBrowser(t, warnings)
	for _, value := range []string{"WARN", "cleanup failed", "identity_id", "alice", "could not close the login browser"} {
		if !strings.Contains(warning, value) {
			t.Fatalf("cleanup warning omitted %q: %s", value, warning)
		}
	}
	for _, value := range []string{"SECRET", "PRIVATE", "auth.openai.com", "access-", "refresh-"} {
		if strings.Contains(warning, value) {
			t.Fatal("cleanup warning exposed credential or provider output")
		}
	}
	if status, err := s.Status(ownerContext("alice"), "alice"); err != nil || !status.Connected || status.Status != "approved" {
		t.Fatalf("cleanup failure invalidated a successful grant: %+v %v", status, err)
	}
}

func TestBrowserLoginReplacementWaitsForOwnCleanupWithoutBlockingOthers(t *testing.T) {
	f := newAuthorizerFixture(t)
	s := f.service(t, t.TempDir())
	l := newBrowserLauncher()
	closing, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	defer unblock.Do(func() { close(release) })
	first := true
	l.open = func(ctx context.Context, browser *browserFixture) error {
		if identityctx.IdentityID(ctx) == "alice" && first {
			first = false
			browser.closing, browser.closeBlock = closing, release
		}
		return nil
	}
	b := testBrowserLogin(t, s, l)
	old, err := b.Start(ownerContext("alice"), "alice", "attempt-alice")
	if err != nil {
		t.Fatal(err)
	}
	receiveBrowser(t, l.opened)
	ctx, cancel := context.WithTimeout(ownerContext("alice"), 30*time.Millisecond)
	defer cancel()
	cancelResult := make(chan error, 1)
	go func() { cancelResult <- b.Cancel(ctx, "alice", old.Route) }()
	receiveBrowser(t, closing)
	if err = receiveBrowser(t, cancelResult); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("cancel failed to respect its caller while browser cleanup was blocked")
	}
	newResult := make(chan Flow, 1)
	go func() {
		flow, err := b.Start(ownerContext("alice"), "alice", "attempt-alice")
		if err != nil {
			t.Error(err)
		}
		newResult <- flow
	}()
	if _, err = b.Start(ownerContext("bob"), "bob", "attempt-bob"); err != nil {
		t.Fatal("one owner's cleanup blocked another's startup")
	}
	if opening := receiveBrowser(t, l.opened); opening.owner != "bob" {
		t.Fatal("replacement browser started before old resources were joined")
	}
	unblock.Do(func() { close(release) })
	if flow := receiveBrowser(t, newResult); flow.Route == old.Route || flow.Route == "" {
		t.Fatal("replacement retained the cancelled browser route")
	}
	if opening := receiveBrowser(t, l.opened); opening.owner != "alice" {
		t.Fatal("owner's replacement was not launched after cleanup")
	}
}
