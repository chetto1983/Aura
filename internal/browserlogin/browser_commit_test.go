package browserlogin

import "testing"

func TestBrowserLoginResponseDelayedUntilApprovalDoesNotAdvertiseClosedBrowser(t *testing.T) {
	f := newAuthorizerFixture(t)
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
	if err != nil || flow.Status != "approved" || flow.Route != "" {
		t.Fatalf("a caller waking after immediate approval received a closed route or error: %+v %v", flow, err)
	}
	if status, err := s.Status(ownerContext("alice"), "alice"); err != nil || !status.Connected || status.Status != "approved" {
		t.Fatal("early callback was not a verified completed grant")
	}
}
