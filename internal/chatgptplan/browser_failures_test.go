package chatgptplan

import "testing"

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
