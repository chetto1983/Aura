package secret

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

// Sealer is at-rest encryption for a secret Aura stores in Postgres: HKDF-SHA256 from
// AURA_AUTHULA_SECRET into a domain-separated AES-256 key, then AES-GCM with the nonce
// prepended to the ciphertext.
//
// It exists because that exact construction had been written three times —
// internal/mcpoauth (OAuth grants), internal/objectstore (identity object store) and, for
// the derivation half, internal/agui/recovery_hash — and a fourth copy was about to be
// written for the MCP server registry. Three copies of a cryptographic routine is three
// places a fix has to land, and the wire format is already shared by construction: the two
// stores prepend the nonce so their columns are byte-compatible.
//
// The `info` parameter is not decoration. Two stores that derive from the same master
// secret with the same info share a key, so one leaked key opens both; that is the whole
// reason HKDF takes an info string, and every caller must pass its own.
type Sealer struct {
	aead cipher.AEAD
}

// NewSealer derives the wrapping key for one domain. secretHex is the 64-hex-character
// AURA_AUTHULA_SECRET; info is that domain's own HKDF label (for example
// "aura-mcp-registry-key-v1"), which MUST differ from every other store's.
func NewSealer(secretHex, info string) (*Sealer, error) {
	if strings.TrimSpace(info) == "" {
		return nil, errors.New("secret: a sealer needs a non-empty HKDF info label to keep its key domain-separated")
	}
	key, err := deriveKey(secretHex, info)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secret: aes cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secret: gcm: %w", err)
	}
	return &Sealer{aead: aead}, nil
}

// Seal prepends a fresh random nonce to the ciphertext.
func (s *Sealer) Seal(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("secret: nonce: %w", err)
	}
	return s.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// SealOptional returns nil for an absent value rather than the ciphertext of an empty
// string, so a NULL column keeps meaning "there is none" instead of "there is an empty one".
func (s *Sealer) SealOptional(plaintext []byte) ([]byte, error) {
	if len(plaintext) == 0 {
		return nil, nil
	}
	return s.Seal(plaintext)
}

// Open reverses Seal. The error deliberately does not echo the ciphertext: a decrypt
// failure means the wrapping key changed or the row was tampered with, and neither is
// diagnosed by dumping bytes into a log.
func (s *Sealer) Open(ciphertext []byte) ([]byte, error) {
	ns := s.aead.NonceSize()
	if len(ciphertext) < ns {
		return nil, fmt.Errorf("secret: ciphertext too short (%d < %d)", len(ciphertext), ns)
	}
	plaintext, err := s.aead.Open(nil, ciphertext[:ns], ciphertext[ns:], nil)
	if err != nil {
		return nil, fmt.Errorf("secret: decrypt: %w", err)
	}
	return plaintext, nil
}

// OpenOptional mirrors SealOptional: no ciphertext means no value, not an error.
func (s *Sealer) OpenOptional(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) == 0 {
		return nil, nil
	}
	return s.Open(ciphertext)
}

func deriveKey(secretHex, info string) ([]byte, error) {
	trimmed := strings.TrimSpace(secretHex)
	if len(trimmed) != 64 {
		return nil, errors.New("secret: AURA_AUTHULA_SECRET must be 64 hex characters (32 bytes)")
	}
	raw, err := hex.DecodeString(trimmed)
	if err != nil {
		return nil, fmt.Errorf("secret: AURA_AUTHULA_SECRET must be valid hex: %w", err)
	}
	key, err := hkdf.Key(sha256.New, raw, nil, info, 32)
	if err != nil {
		return nil, fmt.Errorf("secret: derive key: %w", err)
	}
	return key, nil
}
