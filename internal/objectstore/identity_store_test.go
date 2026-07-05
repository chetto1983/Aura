package objectstore

import (
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/identityctx"
)

// validSecret is a 64-hex-char (32-byte) stand-in for AURA_AUTHULA_SECRET.
var validSecret = strings.Repeat("ab", 32)

func testStore(t *testing.T, localID string) *IdentityStore {
	t.Helper()
	s, err := NewIdentityStore(nil, validSecret,
		Credentials{Bucket: "aura-assets", AccessKey: "GKshared", SecretKey: "sharedsecret"}, localID)
	if err != nil {
		t.Fatalf("NewIdentityStore: %v", err)
	}
	return s
}

func TestNewIdentityStoreValidatesSecret(t *testing.T) {
	for _, bad := range []string{"", "tooshort", strings.Repeat("ab", 16), strings.Repeat("zz", 32)} {
		if _, err := NewIdentityStore(nil, bad, Credentials{}, "local"); err == nil {
			t.Errorf("NewIdentityStore(%q) = nil error, want rejection", bad)
		}
	}
	if _, err := NewIdentityStore(nil, validSecret, Credentials{}, "local"); err != nil {
		t.Errorf("NewIdentityStore(valid) = %v, want nil", err)
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	s := testStore(t, "local")
	for _, plaintext := range []string{"", "s3cr3t!", strings.Repeat("x", 200)} {
		enc, err := s.encrypt(plaintext)
		if err != nil {
			t.Fatalf("encrypt(%q): %v", plaintext, err)
		}
		if string(enc) == plaintext && plaintext != "" {
			t.Errorf("ciphertext equals plaintext for %q (not encrypted)", plaintext)
		}
		if len(enc) <= len(plaintext) {
			t.Errorf("ciphertext len %d <= plaintext len %d (missing nonce/tag)", len(enc), len(plaintext))
		}
		got, err := s.decrypt(enc)
		if err != nil {
			t.Fatalf("decrypt: %v", err)
		}
		if got != plaintext {
			t.Errorf("round-trip = %q, want %q", got, plaintext)
		}
	}
}

func TestEncryptUsesDistinctNonces(t *testing.T) {
	s := testStore(t, "local")
	a, _ := s.encrypt("same")
	b, _ := s.encrypt("same")
	if string(a) == string(b) {
		t.Error("two encryptions of the same plaintext produced identical ciphertext (nonce reuse)")
	}
}

func TestDecryptTamperFails(t *testing.T) {
	s := testStore(t, "local")
	enc, err := s.encrypt("s3cr3t!")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	enc[len(enc)-1] ^= 0xff
	if _, err := s.decrypt(enc); err == nil {
		t.Error("decrypt of tampered ciphertext = nil error, want failure")
	}
	if _, err := s.decrypt([]byte{0x00}); err == nil {
		t.Error("decrypt of too-short input = nil error, want failure")
	}
}

// TestResolveSharedForLocalAndEmpty: the local principal (empty/CLI, the "local"
// name, or the configured local UUID) resolves to the shared bucket without a DB hit.
func TestResolveSharedForLocalAndEmpty(t *testing.T) {
	const localUUID = "00000000-0000-0000-0000-000000000001"
	s := testStore(t, localUUID)
	want := Credentials{Bucket: "aura-assets", AccessKey: "GKshared", SecretKey: "sharedsecret"}

	for _, ctx := range []context.Context{
		context.Background(),
		identityctx.WithIdentityID(context.Background(), "local"),
		identityctx.WithIdentityID(context.Background(), localUUID),
	} {
		got, err := s.Resolve(ctx)
		if err != nil {
			t.Fatalf("Resolve(shared): %v", err)
		}
		if got != want {
			t.Errorf("Resolve(shared) = %+v, want %+v", got, want)
		}
	}
}

// TestResolveRejectsInvalidIdentity: a non-shared identity that fails the traversal
// guard or is not a UUID is rejected BEFORE any DB access (nil pool never dereferenced).
func TestResolveRejectsInvalidIdentity(t *testing.T) {
	s := testStore(t, "00000000-0000-0000-0000-000000000001")
	for _, bad := range []string{"../evil", "a/b", "not-a-uuid", "has space"} {
		ctx := identityctx.WithIdentityID(context.Background(), bad)
		if _, err := s.Resolve(ctx); err == nil {
			t.Errorf("Resolve(identity=%q) = nil error, want rejection", bad)
		}
	}
}
