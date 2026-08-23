package tracesink

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const testKeyHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestSink(t *testing.T) {
	t.Run("round trip uses a fresh nonce per record", func(t *testing.T) {
		sink, err := New(filepath.Join(t.TempDir(), "trace.enc"), testKeyHex)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		plaintext := []byte(`{"stage":"planning","detail":"private"}`)

		first, err := sink.encrypt(plaintext)
		if err != nil {
			t.Fatalf("first encrypt: %v", err)
		}
		second, err := sink.encrypt(plaintext)
		if err != nil {
			t.Fatalf("second encrypt: %v", err)
		}
		if bytes.Equal(first, second) {
			t.Fatal("two encryptions were identical: nonce was reused")
		}
		for i, ciphertext := range [][]byte{first, second} {
			got, err := sink.decrypt(ciphertext)
			if err != nil {
				t.Fatalf("decrypt %d: %v", i, err)
			}
			if !bytes.Equal(got, plaintext) {
				t.Fatalf("decrypt %d = %q, want %q", i, got, plaintext)
			}
		}
	})

	t.Run("short and tampered ciphertext fail without panic", func(t *testing.T) {
		sink, err := New(filepath.Join(t.TempDir(), "trace.enc"), testKeyHex)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if _, err := sink.decrypt(make([]byte, sink.aead.NonceSize()-1)); err == nil {
			t.Fatal("short ciphertext was accepted")
		}
		ciphertext, err := sink.encrypt([]byte("record"))
		if err != nil {
			t.Fatalf("encrypt: %v", err)
		}
		ciphertext[len(ciphertext)-1] ^= 0xff
		if _, err := sink.decrypt(ciphertext); err == nil {
			t.Fatal("tampered ciphertext was accepted")
		}
	})

	t.Run("constructor fails closed on absent or malformed key", func(t *testing.T) {
		for _, key := range []string{"", "abcd", string(make([]byte, 64))} {
			path := filepath.Join(t.TempDir(), "trace.enc")
			if _, err := New(path, key); err == nil {
				t.Fatalf("New accepted malformed key %q", key)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("malformed-key constructor created %s (err=%v)", path, err)
			}
		}
		if _, err := New("", testKeyHex); err == nil {
			t.Fatal("New accepted an empty destination path")
		}
		if err := (*Sink)(nil).Write([]byte("record")); err == nil {
			t.Fatal("nil Sink accepted a record")
		}
	})

	t.Run("write appends independently framed encrypted records", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "trace.enc")
		sink, err := New(path, testKeyHex)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		want := [][]byte{[]byte("first record"), []byte("second record")}
		for _, record := range want {
			if err := sink.Write(record); err != nil {
				t.Fatalf("Write: %v", err)
			}
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		if bytes.Contains(raw, want[0]) || bytes.Contains(raw, want[1]) {
			t.Fatal("encrypted trace contains plaintext")
		}

		var got [][]byte
		for len(raw) > 0 {
			if len(raw) < frameHeaderBytes {
				t.Fatalf("truncated frame header: %d bytes", len(raw))
			}
			size := int(binary.BigEndian.Uint32(raw[:frameHeaderBytes]))
			raw = raw[frameHeaderBytes:]
			if size > len(raw) {
				t.Fatalf("truncated frame: declared %d, available %d", size, len(raw))
			}
			record, err := sink.decrypt(raw[:size])
			if err != nil {
				t.Fatalf("decrypt frame: %v", err)
			}
			got = append(got, record)
			raw = raw[size:]
		}
		if len(got) != len(want) {
			t.Fatalf("decoded %d records, want %d", len(got), len(want))
		}
		for i := range want {
			if !bytes.Equal(got[i], want[i]) {
				t.Fatalf("record %d = %q, want %q", i, got[i], want[i])
			}
		}
	})

	t.Run("filesystem failures are returned", func(t *testing.T) {
		dir := t.TempDir()
		parentFile := filepath.Join(dir, "not-a-directory")
		if err := os.WriteFile(parentFile, []byte("x"), 0o600); err != nil {
			t.Fatalf("fixture WriteFile: %v", err)
		}
		sink, err := New(filepath.Join(parentFile, "trace.enc"), testKeyHex)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if err := sink.Write([]byte("record")); err == nil {
			t.Fatal("Write through a file parent succeeded")
		}

		directorySink, err := New(dir, testKeyHex)
		if err != nil {
			t.Fatalf("New directory sink: %v", err)
		}
		if err := directorySink.Write([]byte("record")); err == nil {
			t.Fatal("Write to a directory succeeded")
		}
	})
}

func (s *Sink) decrypt(ciphertext []byte) ([]byte, error) {
	nonceSize := s.aead.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("trace sink: ciphertext too short (%d < %d)", len(ciphertext), nonceSize)
	}
	plaintext, err := s.aead.Open(nil, ciphertext[:nonceSize], ciphertext[nonceSize:], nil)
	if err != nil {
		return nil, fmt.Errorf("trace sink: decrypt: %w", err)
	}
	return plaintext, nil
}
