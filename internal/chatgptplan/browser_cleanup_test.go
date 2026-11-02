package chatgptplan

import (
	"errors"
	"testing"
)

func TestBrowserLoginCancelDuringTokenExchangeJoinsWithoutResurrection(t *testing.T) {
	f := newOIDC(t)
	s := f.service(t, t.TempDir())
	f.connect(t, s, "alice")
	l := newBrowserLauncher()
	b := testBrowserLogin(t, s, l)
	flow, err := b.Start(ownerContext("alice"), "alice", "attempt-alice")
	if err != nil {
		t.Fatal(err)
	}
	opening := receiveBrowser(t, l.opened)
	callback := f.callback(t, browserAuthorization(t, opening.browser), "oaiapp_alice")
	paused := pausedTokenTransport{base: s.client.Transport, entered: make(chan struct{}), release: make(chan struct{})}
	s.client.Transport = paused
	opening.browser.callback <- callback
	receiveBrowser(t, paused.entered)
	if err = b.Cancel(ownerContext("alice"), "alice", flow.AuthURL); err != nil {
		t.Fatal(err)
	}
	receiveBrowser(t, opening.browser.closed)
	if status, err := s.Status(ownerContext("alice"), "alice"); err != nil || !status.Connected || status.Status != "approved" {
		t.Fatalf("cancelled exchange changed the retained account: %+v %v", status, err)
	}
	if err = s.Disconnect(ownerContext("alice"), "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AccessToken(ownerContext("alice")); !errors.Is(err, ErrAuthorizationRequired) {
		t.Fatal("cancelled exchange resurrected the disconnected credential")
	}
}
