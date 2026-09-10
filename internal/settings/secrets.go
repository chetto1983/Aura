package settings

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// secretDerivationInfo domain-separates the settings wrapping key from every other key
// derived from AURA_AUTHULA_SECRET; identitykey uses "aura-identity-llm-key-v1".
const secretDerivationInfo = "aura-settings-secret-v1"

// secretPrefix marks a value this package encrypted. A secret row without it is a plaintext
// row from before encryption, which EncryptPlaintextSecrets converts at boot.
const secretPrefix = "enc:v1:"

// ErrSecretsUnavailable reports a secret row written or read through a store built without
// AURA_AUTHULA_SECRET. Nothing is stored in the clear in its place.
var ErrSecretsUnavailable = errors.New("settings: secret rows need AURA_AUTHULA_SECRET")

var errMalformedSecret = errors.New("settings: malformed secret value")

// newSecretAEAD derives the settings wrapping key. An empty secret yields no cipher (secret
// rows unavailable); a malformed one is an error, so a mis-provisioned deployment never
// stores a credential in the clear.
func newSecretAEAD(authulaSecretHex string) (cipher.AEAD, error) {
	if strings.TrimSpace(authulaSecretHex) == "" {
		return nil, nil
	}
	return aeadWithInfo(authulaSecretHex, secretDerivationInfo)
}

// aeadWithInfo takes the info string as a parameter so a test can derive a key under another
// store's info and assert the two cannot open each other's values.
func aeadWithInfo(authulaSecretHex, info string) (cipher.AEAD, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(authulaSecretHex))
	if err != nil || len(raw) != 32 {
		return nil, errors.New("settings: AURA_AUTHULA_SECRET must be 64 hex characters (32 bytes)")
	}
	key, err := hkdf.Key(sha256.New, raw, nil, info, 32)
	if err != nil {
		return nil, fmt.Errorf("settings: derive key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("settings: cipher: %w", err)
	}
	return cipher.NewGCM(block)
}

// sealSecret encrypts one value as enc:v1:<nonce hex>:<ciphertext hex>. An empty value is
// stored empty: clearing a setting is not a secret.
func sealSecret(aead cipher.AEAD, plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	if aead == nil {
		return "", ErrSecretsUnavailable
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("settings: nonce: %w", err)
	}
	sealed := aead.Seal(nil, nonce, []byte(plaintext), nil)
	return secretPrefix + hex.EncodeToString(nonce) + ":" + hex.EncodeToString(sealed), nil
}

// openSecret decrypts a value sealSecret wrote and passes a legacy plaintext value through.
func openSecret(aead cipher.AEAD, stored string) (string, error) {
	rest, encrypted := strings.CutPrefix(stored, secretPrefix)
	if !encrypted {
		return stored, nil
	}
	if aead == nil {
		return "", ErrSecretsUnavailable
	}
	nonceHex, sealedHex, ok := strings.Cut(rest, ":")
	if !ok {
		return "", errMalformedSecret
	}
	nonce, err := hex.DecodeString(nonceHex)
	if err != nil || len(nonce) != aead.NonceSize() {
		return "", errMalformedSecret
	}
	sealed, err := hex.DecodeString(sealedHex)
	if err != nil {
		return "", errMalformedSecret
	}
	plain, err := aead.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", fmt.Errorf("settings: decrypt: %w", err)
	}
	return string(plain), nil
}
