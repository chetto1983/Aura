package webauth

import (
	"strings"
	"testing"

	authulasecurity "github.com/Authula/authula/core/security"
)

// Authula v1.51 panics at boot unless the secret is padded Base64 of 32 bytes; the
// operator's 64-hex secret must be accepted unchanged and map to a valid key.
func TestAuthulaKeyIsAcceptedByAuthula(t *testing.T) {
	secret := strings.Repeat("ab", 32)
	key, err := authulasecurity.DecodeSecret(authulaKey(secret))
	if err != nil {
		t.Fatalf("DecodeSecret(authulaKey(%q)): %v", secret, err)
	}
	if len(key) != authulasecurity.SecretKeySize {
		t.Fatalf("key length = %d, want %d", len(key), authulasecurity.SecretKeySize)
	}
	if authulaKey(secret+"\n") != authulaKey(secret) {
		t.Fatal("authulaKey must ignore surrounding whitespace like validateAuthulaSecret")
	}
	if authulaKey(secret) == authulaKey(strings.Repeat("cd", 32)) {
		t.Fatal("distinct secrets must give distinct keys")
	}
}
