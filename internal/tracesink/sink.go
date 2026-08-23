// Package tracesink provides an append-only encrypted sink for sensitive
// reasoning-trace records.
package tracesink

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	keyDerivationInfo = "aura-reasoning-trace-sink-v1"
	frameHeaderBytes  = 4
)

// Sink appends independently framed AES-256-GCM records to path. Each record
// uses a fresh nonce and can be authenticated independently.
type Sink struct {
	path string
	aead cipher.AEAD
	mu   sync.Mutex
}

// New constructs an encrypted sink. keyHex must contain exactly 32 bytes as
// 64 hexadecimal characters; malformed or absent key material fails closed.
func New(path, keyHex string) (*Sink, error) {
	key, err := deriveKey(keyHex)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("trace sink: path is empty")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("trace sink: AES-256: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("trace sink: GCM: %w", err)
	}
	return &Sink{path: path, aead: aead}, nil
}

// Write encrypts record with a fresh random nonce, then appends a big-endian
// length prefix followed by the nonce-prefixed ciphertext. There is no
// plaintext fallback.
func (s *Sink) Write(record []byte) error {
	if s == nil || s.aead == nil {
		return errors.New("trace sink: uninitialized")
	}
	sealed, err := s.encrypt(record)
	if err != nil {
		return err
	}
	if len(sealed) > math.MaxUint32 {
		return fmt.Errorf("trace sink: encrypted record too large: %d bytes", len(sealed))
	}
	frame := make([]byte, frameHeaderBytes+len(sealed))
	binary.BigEndian.PutUint32(frame[:frameHeaderBytes], uint32(len(sealed))) // #nosec G115 -- upper-bounded above.
	copy(frame[frameHeaderBytes:], sealed)

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return fmt.Errorf("trace sink: mkdir: %w", err)
	}
	// #nosec G304 -- path is an explicit operator-controlled trace destination.
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("trace sink: open: %w", err)
	}
	defer func() { _ = f.Close() }()
	if err := writeAll(f, frame); err != nil {
		return fmt.Errorf("trace sink: append: %w", err)
	}
	return nil
}

func (s *Sink) encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("trace sink: nonce: %w", err)
	}
	return s.aead.Seal(nonce, nonce, plaintext, nil), nil
}

func deriveKey(keyHex string) ([]byte, error) {
	encoded := strings.TrimSpace(keyHex)
	if len(encoded) != 64 {
		return nil, errors.New("trace sink: AURA_TRACE_ENCRYPT_KEY must be 64 hex characters (32 bytes)")
	}
	raw, err := hex.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("trace sink: AURA_TRACE_ENCRYPT_KEY must be valid hex: %w", err)
	}
	key, err := hkdf.Key(sha256.New, raw, nil, keyDerivationInfo, 32)
	if err != nil {
		return nil, fmt.Errorf("trace sink: derive key: %w", err)
	}
	return key, nil
}

func writeAll(f *os.File, data []byte) error {
	for len(data) > 0 {
		n, err := f.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return errors.New("zero-byte write")
		}
		data = data[n:]
	}
	return nil
}
