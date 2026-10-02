package chatgptplan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestIdentityAndRedirectValidation(t *testing.T) {
	s, err := New(t.TempDir(), testSecret)
	if err != nil {
		t.Fatal(err)
	}
	if s.endpoint.issuer != issuer || s.endpoint.token != issuer+"/api/accounts/oauth/token" {
		t.Fatal("production endpoint drifted")
	}
	if _, err = s.AccessToken(context.Background()); !errors.Is(err, ErrIdentityRequired) {
		t.Fatal("unscoped access accepted")
	}
	if _, err = s.Start(ownerContext("bob"), "alice", testRedirect); !errors.Is(err, ErrIdentityRequired) {
		t.Fatal("cross-owner authorization accepted")
	}
	if _, err = s.Status(ownerContext("bob"), "alice"); !errors.Is(err, ErrIdentityRequired) {
		t.Fatal("cross-owner status accepted")
	}
	if err = s.Disconnect(ownerContext("bob"), "alice"); !errors.Is(err, ErrIdentityRequired) {
		t.Fatal("cross-owner disconnect accepted")
	}
	for _, owner := range []string{"", " alice", "alice "} {
		if _, err = s.Start(context.Background(), owner, testRedirect); !errors.Is(err, ErrIdentityRequired) {
			t.Fatal("invalid owner accepted")
		}
	}
	for _, redirect := range []string{"http://localhost:8089/auth/callback", "https://127.0.0.1:8089/auth/callback", "http://127.0.0.1/auth/callback", "http://127.0.0.1:0/auth/callback", "http://127.0.0.1:65536/auth/callback", "http://127.0.0.1:8089/callback", "http://user@127.0.0.1:8089/auth/callback", "http://127.0.0.1:8089/auth/callback?extra=1", "http://127.0.0.1:8089/auth/callback#fragment", "%"} {
		if _, err = s.Start(ownerContext("alice"), "alice", redirect); err == nil {
			t.Fatalf("accepted redirect %q", redirect)
		}
	}
	ctx, cancel := context.WithCancel(ownerContext("alice"))
	cancel()
	if _, err = s.Start(ctx, "alice", testRedirect); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled flow started")
	}
	if err = s.Disconnect(ownerContext("alice"), "alice"); err != nil {
		t.Fatal(err)
	}
}

func TestCredentialBindingTamperingAndPrivateModes(t *testing.T) {
	f := newOIDC(t)
	s := f.service(t, t.TempDir())
	f.connect(t, s, "alice")
	data, err := os.ReadFile(s.path("alice"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(s.path("bob"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Status(ownerContext("bob"), "bob"); err == nil {
		t.Fatal("cross-owner ciphertext decrypted")
	}
	data[len(data)-1] ^= 1
	if err = atomicWrite(s.path("alice"), data); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AccessToken(ownerContext("alice")); err == nil {
		t.Fatal("tampered credentials accepted")
	}
	if runtime.GOOS != "windows" {
		for _, path := range []string{s.dir, s.path("alice"), filepath.Join(s.dir, "host-id"), filepath.Join(s.dir, ".lock")} {
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			want := os.FileMode(0o600)
			if info.IsDir() {
				want = 0o700
			}
			if info.Mode().Perm() != want {
				t.Fatalf("%s mode=%v, want %v", path, info.Mode().Perm(), want)
			}
		}
	}
	if !strings.HasPrefix(s.path("../../outside"), s.dir+string(filepath.Separator)) {
		t.Fatal("credential path escaped storage")
	}
}

func TestConstructorAndStorageFailures(t *testing.T) {
	if _, err := New("", testSecret); err == nil {
		t.Fatal("empty storage accepted")
	}
	if _, err := New(t.TempDir(), "bad"); err == nil {
		t.Fatal("invalid wrapping key accepted")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "chatgpt-plan"), []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(dir, testSecret); err == nil {
		t.Fatal("storage file used as directory")
	}
	s, err := New(t.TempDir(), testSecret)
	if err != nil {
		t.Fatal(err)
	}
	if err = atomicWrite(filepath.Join(s.dir, "host-id"), []byte("bad-host")); err != nil {
		t.Fatal(err)
	}
	if _, err = New(filepath.Dir(s.dir), testSecret); err == nil {
		t.Fatal("corrupt stable host silently recreated")
	}
	if err = os.Mkdir(s.path("alice"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Status(ownerContext("alice"), "alice"); err == nil {
		t.Fatal("unreadable credential accepted")
	}
	if err = atomicWrite(filepath.Join(s.dir, "missing", "credential"), []byte("secret")); err == nil {
		t.Fatal("atomic write into missing directory accepted")
	}
}

func TestCrossProcessLockRespectsContext(t *testing.T) {
	s, err := New(t.TempDir(), testSecret)
	if err != nil {
		t.Fatal(err)
	}
	release, err := s.lockStore(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if next, err := s.lockStore(ctx); !errors.Is(err, context.DeadlineExceeded) {
		if next != nil {
			next()
		}
		t.Fatalf("contended lock=%v", err)
	}
}
