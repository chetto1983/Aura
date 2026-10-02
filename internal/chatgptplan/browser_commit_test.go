package chatgptplan

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"
)

type cancelJWKSBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func TestBrowserLoginCancelInvalidatesServiceBeforeHelperFinishes(t *testing.T) {
	f := newOIDC(t)
	s := f.service(t, t.TempDir())
	l := newBrowserLauncher()
	ready, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	defer unblock.Do(func() { close(release) })
	l.open = func(_ context.Context, browser *browserFixture) error {
		browser.callbackReady, browser.callbackGate = ready, release
		return nil
	}
	b := testBrowserLogin(t, s, l)
	flow, err := b.Start(ownerContext("alice"), "alice", "attempt-alice")
	if err != nil {
		t.Fatal(err)
	}
	receiveBrowser(t, ready)
	ctx, cancel := context.WithTimeout(ownerContext("alice"), 30*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- b.Cancel(ctx, "alice", flow.AuthURL) }()
	if err = receiveBrowser(t, result); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("cancel did not wait for the callback helper")
	}
	if status, err := s.Status(ownerContext("alice"), "alice"); err != nil || status.Status != "disconnected" {
		t.Fatal("pending service authorization remained usable until helper cleanup")
	}
	unblock.Do(func() { close(release) })
}

func TestBrowserLoginCancelRespectsDeadlineWhileCredentialStoreIsBusy(t *testing.T) {
	f := newOIDC(t)
	s := f.service(t, t.TempDir())
	l := newBrowserLauncher()
	b := testBrowserLogin(t, s, l)
	flow, err := b.Start(ownerContext("alice"), "alice", "attempt-alice")
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	var unlocked sync.Once
	defer unlocked.Do(s.mu.Unlock)
	ctx, cancel := context.WithTimeout(ownerContext("alice"), 30*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- b.Cancel(ctx, "alice", flow.AuthURL) }()
	if err = receiveBrowser(t, result); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("credential serialization blocked the cancellation response")
	}
	unlocked.Do(s.mu.Unlock)
	if err = b.Cancel(ownerContext("alice"), "alice", flow.AuthURL); err != nil {
		t.Fatal(err)
	}
	if status, err := s.Status(ownerContext("alice"), "alice"); err != nil || status.Status != "disconnected" {
		t.Fatal("deferred invalidation was not joined after credential store became available")
	}
}

func TestBrowserLoginResponseDelayedUntilApprovalDoesNotAdvertiseClosedBrowser(t *testing.T) {
	f := newOIDC(t)
	s := f.service(t, t.TempDir())
	l := newBrowserLauncher()
	b := testBrowserLogin(t, s, l)
	if _, err := b.Start(ownerContext("alice"), "alice", "attempt-alice"); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	pending := b.pending["alice"]
	b.mu.Unlock()
	opening := receiveBrowser(t, l.opened)
	opening.browser.callback <- f.callback(t, browserAuthorization(t, opening.browser), "oaiapp_alice")
	receiveBrowser(t, pending.done)
	flow, err := pending.startResult()
	if err != nil || flow.Status != "approved" || flow.AuthURL != "" {
		t.Fatalf("a caller waking after immediate approval received a closed route or error: %+v %v", flow, err)
	}
	if status, err := s.Status(ownerContext("alice"), "alice"); err != nil || !status.Connected || status.Status != "approved" {
		t.Fatal("early callback was not a verified completed grant")
	}
}

func (b cancelJWKSBody) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}

type cancelAfterJWKSTransport struct {
	base   http.RoundTripper
	cancel context.CancelFunc
}

func (c cancelAfterJWKSTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := c.base.RoundTrip(r)
	if err == nil && r.URL.Path == "/jwks" {
		// jwx v3.3.0 Fetch parses the complete JWKS before its deferred body Close.
		response.Body = cancelJWKSBody{ReadCloser: response.Body, cancel: c.cancel}
	}
	return response, err
}

func TestBrowserLoginCancelledAfterIdentityKeysCannotCommitVerifiedGrant(t *testing.T) {
	for _, existingAccount := range []bool{false, true} {
		name := "initial account"
		if existingAccount {
			name = "retained account"
		}
		t.Run(name, func(t *testing.T) {
			f := newOIDC(t)
			s := f.service(t, t.TempDir())
			if existingAccount {
				f.connect(t, s, "alice")
			}
			_, q := f.authorization(t, s, "alice")
			callback := f.callback(t, q, "oaiapp_alice")
			f.modifyResponse = func(response map[string]any, _ bool) { response["access_token"] = "late-cancelled-grant" }
			ctx, cancel := context.WithCancel(ownerContext("alice"))
			defer cancel()
			s.client.Transport = cancelAfterJWKSTransport{base: s.client.Transport, cancel: cancel}
			if err := s.Callback(ctx, callback); !errors.Is(err, ErrInvalidCallback) || !errors.Is(ctx.Err(), context.Canceled) {
				t.Fatalf("cancelled verified exchange committed: %v, context %v", err, ctx.Err())
			}
			token, err := s.AccessToken(ownerContext("alice"))
			if existingAccount {
				if err != nil || token != "access-oaiapp_alice" {
					t.Fatal("late cancellation replaced the retained account")
				}
			} else if !errors.Is(err, ErrAuthorizationRequired) || token != "" {
				t.Fatal("late cancellation persisted a new grant")
			}
			status, err := s.Status(ownerContext("alice"), "alice")
			if err != nil || status.Connected != existingAccount || status.Status == "starting" {
				t.Fatal("cancelled exchange left a pending flow")
			}
		})
	}
}
