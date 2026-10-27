package secret

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

const testSecretHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestSealerRoundTrip(t *testing.T) {
	s, err := NewSealer(testSecretHex, "aura-test-key-v1")
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}
	plaintext := []byte("MCP_OAUTH_CLIENT_SECRET=hunter2")
	sealed, err := s.Seal(plaintext)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if bytes.Contains(sealed, plaintext) {
		t.Fatal("the plaintext survives inside the ciphertext")
	}
	opened, err := s.Open(sealed)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(opened, plaintext) {
		t.Fatalf("round trip = %q, want %q", opened, plaintext)
	}
}

func TestSealIsNonDeterministic(t *testing.T) {
	s, err := NewSealer(testSecretHex, "aura-test-key-v1")
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}
	first, err := s.Seal([]byte("same"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	second, err := s.Seal([]byte("same"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	// A fresh nonce per Seal is what stops equal plaintexts from being recognisable as
	// equal in the table.
	if bytes.Equal(first, second) {
		t.Fatal("two seals of the same plaintext are byte-identical: the nonce is not fresh")
	}
}

// Domain separation asserted in a comment is domain separation nobody checks.
func TestDifferentInfoLabelsCannotOpenEachOther(t *testing.T) {
	a, err := NewSealer(testSecretHex, "aura-store-a-v1")
	if err != nil {
		t.Fatalf("NewSealer a: %v", err)
	}
	b, err := NewSealer(testSecretHex, "aura-store-b-v1")
	if err != nil {
		t.Fatalf("NewSealer b: %v", err)
	}
	sealed, err := a.Seal([]byte("a's secret"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if _, err := b.Open(sealed); err == nil {
		t.Fatal("a sealer opened another domain's ciphertext: the HKDF info is not separating the keys")
	}
}

func TestOptionalHelpersTreatEmptyAsAbsent(t *testing.T) {
	s, err := NewSealer(testSecretHex, "aura-test-key-v1")
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}
	sealed, err := s.SealOptional(nil)
	if err != nil {
		t.Fatalf("SealOptional: %v", err)
	}
	if sealed != nil {
		t.Fatalf("SealOptional(nil) = %v, want nil so the column stays NULL", sealed)
	}
	opened, err := s.OpenOptional(nil)
	if err != nil {
		t.Fatalf("OpenOptional: %v", err)
	}
	if opened != nil {
		t.Fatalf("OpenOptional(nil) = %v, want nil", opened)
	}
}

func TestNewSealerRejectsUnusableInput(t *testing.T) {
	cases := []struct {
		name      string
		secretHex string
		info      string
		wantErr   string
	}{
		{"empty info shares a key with every other store", testSecretHex, "  ", "info label"},
		{"short secret", "abcd", "aura-test-key-v1", "64 hex characters"},
		{"non-hex secret", strings.Repeat("z", 64), "aura-test-key-v1", "valid hex"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewSealer(tc.secretHex, tc.info)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("NewSealer err = %v, want one containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestOpenRejectsTruncatedCiphertext(t *testing.T) {
	s, err := NewSealer(testSecretHex, "aura-test-key-v1")
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}
	if _, err := s.Open([]byte{1, 2, 3}); err == nil {
		t.Fatal("a ciphertext shorter than the nonce was accepted")
	}
}

// TestIdentityKeyIsStableAndSeparated pins what the box relies on: the same identity rebuilds the
// same key on every Resolve, while another identity or another domain gets an unrelated one.
func TestIdentityKeyIsStableAndSeparated(t *testing.T) {
	a1, err := IdentityKey(testSecretHex, "aura-browser-state-v1", "id-a")
	if err != nil {
		t.Fatalf("IdentityKey: %v", err)
	}
	a2, _ := IdentityKey(testSecretHex, "aura-browser-state-v1", "id-a")
	b, _ := IdentityKey(testSecretHex, "aura-browser-state-v1", "id-b")
	otherDomain, _ := IdentityKey(testSecretHex, "aura-other-v1", "id-a")
	if len(a1) != 32 || !bytes.Equal(a1, a2) {
		t.Fatalf("key = %x (len %d), want a stable 32-byte key", a1, len(a1))
	}
	if bytes.Equal(a1, b) || bytes.Equal(a1, otherDomain) {
		t.Fatal("identities or domains that differ must not share a key")
	}
	for _, bad := range [][2]string{{"", "id-a"}, {"aura-browser-state-v1", " "}} {
		if _, err := IdentityKey(testSecretHex, bad[0], bad[1]); err == nil {
			t.Errorf("IdentityKey(%q, %q): want error", bad[0], bad[1])
		}
	}
	if _, err := IdentityKey("short", "aura-browser-state-v1", "id-a"); err == nil {
		t.Error("an unusable master secret must be refused")
	}
}

// The stores that used to carry their own copy of this construction (mcpoauth, objectstore,
// tracesink) wrote rows as nonce||AES-GCM(HKDF-SHA256(secret, info)). Folding them onto Sealer
// must leave every existing row readable, so this seals the old way by hand, per label, and
// opens through the Sealer.
func TestSealerOpensWhatTheRetiredCopiesWrote(t *testing.T) {
	for _, info := range []string{
		"aura-mcp-oauth-identity-key-v1",
		"aura-objectstore-identity-key-v1",
		"aura-reasoning-trace-sink-v1",
	} {
		t.Run(info, func(t *testing.T) {
			raw, err := hex.DecodeString(testSecretHex)
			if err != nil {
				t.Fatal(err)
			}
			key, err := hkdf.Key(sha256.New, raw, nil, info, 32)
			if err != nil {
				t.Fatal(err)
			}
			block, err := aes.NewCipher(key)
			if err != nil {
				t.Fatal(err)
			}
			aead, err := cipher.NewGCM(block)
			if err != nil {
				t.Fatal(err)
			}
			nonce := bytes.Repeat([]byte{7}, aead.NonceSize())
			row := aead.Seal(nonce, nonce, []byte("stored before the fold"), nil)

			derived, err := DeriveKey(testSecretHex, info)
			if err != nil {
				t.Fatalf("DeriveKey: %v", err)
			}
			for name, build := range map[string]func() (*Sealer, error){
				"NewSealer":        func() (*Sealer, error) { return NewSealer(testSecretHex, info) },
				"NewSealerFromKey": func() (*Sealer, error) { return NewSealerFromKey(derived) },
			} {
				s, err := build()
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				got, err := s.Open(row)
				if err != nil || string(got) != "stored before the fold" {
					t.Fatalf("%s opened %q, %v; want the row the old code wrote", name, got, err)
				}
			}
		})
	}
}

func TestDeriveKeyNamesNoVariable(t *testing.T) {
	_, err := DeriveKey("abcd", "aura-test-key-v1")
	if err == nil || strings.Contains(err.Error(), "AURA_") {
		t.Fatalf("DeriveKey err = %v, want one that names no env variable (the caller wraps it)", err)
	}
	_, err = NewSealer("abcd", "aura-test-key-v1")
	if err == nil || !strings.Contains(err.Error(), "AURA_AUTHULA_SECRET") {
		t.Fatalf("NewSealer err = %v, want it to name AURA_AUTHULA_SECRET", err)
	}
}
