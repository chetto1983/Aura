package chatgptplan

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// opened is the indexer's view of the store the fixture's daemon wrote: Open, then the
// fixture's endpoints, because a credential records the issuer it was granted by.
func (f *oidcFixture) opened(t *testing.T, dir string) *Service {
	t.Helper()
	s, err := Open(dir, testSecret)
	if err != nil {
		t.Fatal(err)
	}
	s.endpoint = endpoints{issuer: f.server.URL, authorize: f.server.URL + "/authorize", token: f.server.URL + "/token", jwks: f.server.URL + "/jwks", discovery: f.server.URL + "/discovery"}
	s.client = f.server.Client()
	s.client.Timeout = 3 * time.Second
	s.client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return s
}

func TestOpenReadsTheDaemonsStore(t *testing.T) {
	f := newOIDC(t)
	dir := t.TempDir()
	f.connect(t, f.service(t, dir), "alice")
	access, err := f.opened(t, dir).AccessToken(ownerContext("alice"))
	if err != nil || access != "access-oaiapp_alice" {
		t.Fatalf("opened store token = %q, %v", access, err)
	}
}

func TestOpenRefusesAMissingStore(t *testing.T) {
	dir := t.TempDir()
	if _, err := Open(dir, testSecret); !errors.Is(err, ErrStoreMissing) {
		t.Fatalf("Open on an empty directory = %v, want ErrStoreMissing", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "chatgpt-plan")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Open created the store directory: %v", err)
	}
}

func TestOpenRefusesAStoreWithoutHostID(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "chatgpt-plan")
	if err := os.Mkdir(store, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir, testSecret); !errors.Is(err, ErrStoreMissing) {
		t.Fatalf("Open without a host ID = %v, want ErrStoreMissing", err)
	}
	if _, err := os.Stat(filepath.Join(store, "host-id")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Open wrote a host ID: %v", err)
	}
}

func TestOpenedStoreReadsTheCredentialTheDaemonRotated(t *testing.T) {
	f := newOIDC(t)
	dir := t.TempDir()
	daemon := f.service(t, dir)
	f.connect(t, daemon, "alice")
	changeCredential(t, daemon, "alice", func(c *credential) { c.ExpiresAt = time.Now().Add(-time.Second) })
	if access, err := daemon.AccessToken(ownerContext("alice")); err != nil || access != "rotated-access-oaiapp_alice" {
		t.Fatalf("daemon refresh = %q, %v", access, err)
	}
	access, err := f.opened(t, dir).AccessToken(ownerContext("alice"))
	if err != nil || access != "rotated-access-oaiapp_alice" {
		t.Fatalf("opened store token = %q, %v, want the rotated one", access, err)
	}
	if f.refreshes != 1 {
		t.Fatalf("refreshes = %d, want 1: the opened store refreshed with a spent token", f.refreshes)
	}
}
