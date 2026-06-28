package agui

import (
	"strings"
	"testing"
)

func TestNormalizeRecoveryAnswer(t *testing.T) {
	got := NormalizeRecoveryAnswer("  My   First   School  ")
	if got != "my first school" {
		t.Fatalf("normalized = %q, want %q", got, "my first school")
	}
}

func TestRecoveryHasherHashVerify(t *testing.T) {
	h := RecoveryHasher{}
	hash, version, err := h.HashAnswer("Blue Guitar")
	if err != nil {
		t.Fatalf("HashAnswer: %v", err)
	}
	if version != recoveryAnswerHashVersion {
		t.Fatalf("version = %q, want %q", version, recoveryAnswerHashVersion)
	}
	if strings.Contains(hash, "Blue Guitar") {
		t.Fatal("hash leaked the raw answer")
	}
	if !h.VerifyAnswer(" blue   guitar ", hash) {
		t.Fatal("normalized answer should verify")
	}
	if h.VerifyAnswer("red guitar", hash) {
		t.Fatal("wrong answer verified")
	}
}

func TestHashOpaqueSecretVerify(t *testing.T) {
	raw := "123456"
	hash, err := HashOpaqueSecret(raw)
	if err != nil {
		t.Fatalf("HashOpaqueSecret: %v", err)
	}
	if strings.Contains(hash, raw) {
		t.Fatal("opaque hash leaked raw secret")
	}
	if !VerifyOpaqueSecret(raw, hash) {
		t.Fatal("opaque secret should verify")
	}
	if VerifyOpaqueSecret("654321", hash) {
		t.Fatal("wrong opaque secret verified")
	}
}

func TestHashLookupTokenIsDeterministicAndNonReversible(t *testing.T) {
	token := "reset-token-123"
	a := HashLookupToken(token)
	b := HashLookupToken(token)
	if a != b {
		t.Fatal("lookup token hash must be deterministic")
	}
	if strings.Contains(a, token) {
		t.Fatal("lookup token hash leaked raw token")
	}
}
