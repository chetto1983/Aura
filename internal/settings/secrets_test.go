package settings

import (
	"errors"
	"strings"
	"testing"
)

const testAuthulaSecret = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

func TestSecretRoundTrip(t *testing.T) {
	aead, err := newSecretAEAD(testAuthulaSecret)
	if err != nil {
		t.Fatalf("newSecretAEAD: %v", err)
	}
	sealed, err := sealSecret(aead, "sk-or-v1-secret")
	if err != nil {
		t.Fatalf("sealSecret: %v", err)
	}
	if !strings.HasPrefix(sealed, secretPrefix) || strings.Contains(sealed, "sk-or-v1-secret") {
		t.Fatalf("sealed = %q, want an enc:v1: value that does not contain the plaintext", sealed)
	}
	if again, _ := sealSecret(aead, "sk-or-v1-secret"); again == sealed {
		t.Fatal("two seals of the same value are identical: the nonce is not random")
	}
	if plain, err := openSecret(aead, sealed); err != nil || plain != "sk-or-v1-secret" {
		t.Fatalf("openSecret = %q, %v; want the plaintext back", plain, err)
	}
}

func TestOpenSecretPassesPlaintextRowsThrough(t *testing.T) {
	aead, _ := newSecretAEAD(testAuthulaSecret)
	if plain, err := openSecret(aead, "legacy-plaintext"); err != nil || plain != "legacy-plaintext" {
		t.Fatalf("openSecret(legacy) = %q, %v; want the row unchanged", plain, err)
	}
}

func TestSecretsNeedTheAuthulaSecret(t *testing.T) {
	if _, err := sealSecret(nil, "value"); !errors.Is(err, ErrSecretsUnavailable) {
		t.Fatalf("sealSecret without a key: err = %v, want ErrSecretsUnavailable", err)
	}
	if _, err := openSecret(nil, secretPrefix+"00:00"); !errors.Is(err, ErrSecretsUnavailable) {
		t.Fatalf("openSecret without a key: err = %v, want ErrSecretsUnavailable", err)
	}
	if sealed, err := sealSecret(nil, ""); err != nil || sealed != "" {
		t.Fatalf("an empty value is not a secret: sealSecret = %q, %v", sealed, err)
	}
}

func TestSettingsKeyIsDomainSeparated(t *testing.T) {
	aead, _ := newSecretAEAD(testAuthulaSecret)
	sealed, _ := sealSecret(aead, "value")
	other, err := aeadWithInfo(testAuthulaSecret, "aura-identity-llm-key-v1")
	if err != nil {
		t.Fatalf("aeadWithInfo: %v", err)
	}
	if _, err := openSecret(other, sealed); err == nil {
		t.Fatal("a key derived under identitykey's info string opened a settings secret")
	}
}

func TestNewSecretAEADInputs(t *testing.T) {
	if _, err := newSecretAEAD("not-hex"); err == nil {
		t.Fatal("a malformed AURA_AUTHULA_SECRET was accepted")
	}
	if aead, err := newSecretAEAD(""); err != nil || aead != nil {
		t.Fatalf("an empty secret = (%v, %v), want (nil, nil): secrets unavailable, not an error", aead, err)
	}
}

func TestOpenSecretRejectsDamagedValues(t *testing.T) {
	aead, _ := newSecretAEAD(testAuthulaSecret)
	sealed, _ := sealSecret(aead, "value")
	flipped := byte('0')
	if sealed[len(sealed)-1] == '0' {
		flipped = '1'
	}
	for _, bad := range []string{
		secretPrefix + "zz:00",
		secretPrefix + "no-separator",
		sealed[:len(sealed)-1] + string(flipped),
	} {
		if _, err := openSecret(aead, bad); err == nil {
			t.Errorf("openSecret(%q) succeeded on a damaged value", bad)
		}
	}
}
