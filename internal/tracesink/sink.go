// Package tracesink provides an append-only encrypted sink for sensitive
// reasoning-trace records.
package tracesink

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/chetto1983/aura/internal/secret"
)

const (
	keyDerivationInfo = "aura-reasoning-trace-sink-v1"
	frameHeaderBytes  = 4
)

// Sink appends independently framed AES-256-GCM records to path. Each record
// uses a fresh nonce and can be authenticated independently.
type Sink struct {
	path   string
	sealer *secret.Sealer
	mu     sync.Mutex
}

// New constructs an encrypted sink. keyHex must contain exactly 32 bytes as
// 64 hexadecimal characters; malformed or absent key material fails closed.
func New(path, keyHex string) (*Sink, error) {
	key, err := secret.DeriveKey(keyHex, keyDerivationInfo)
	if err != nil {
		return nil, fmt.Errorf("trace sink: AURA_TRACE_ENCRYPT_KEY: %w", err)
	}
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("trace sink: path is empty")
	}
	sealer, err := secret.NewSealerFromKey(key)
	if err != nil {
		return nil, fmt.Errorf("trace sink: %w", err)
	}
	return &Sink{path: path, sealer: sealer}, nil
}

// Write encrypts record with a fresh random nonce, then appends a big-endian
// length prefix followed by the nonce-prefixed ciphertext. There is no
// plaintext fallback.
func (s *Sink) Write(record []byte) error {
	if s == nil || s.sealer == nil {
		return errors.New("trace sink: uninitialized")
	}
	sealed, err := s.sealer.Seal(record)
	if err != nil {
		return fmt.Errorf("trace sink: %w", err)
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
