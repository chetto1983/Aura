package secret

import (
	"bytes"
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
