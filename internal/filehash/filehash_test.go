package filehash

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestSHA256HashesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "model.gguf")
	if err := os.WriteFile(path, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := SHA256(path)
	if err != nil {
		t.Fatalf("SHA256: %v", err)
	}
	// FIPS 180-2, appendix B.1: SHA-256("abc").
	if want := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"; got != want {
		t.Errorf("SHA256(abc) = %s, want %s", got, want)
	}
}

func TestSHA256ReportsAMissingFile(t *testing.T) {
	if _, err := SHA256(filepath.Join(t.TempDir(), "absent")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("SHA256(absent) error = %v, want fs.ErrNotExist", err)
	}
}

func TestSHA256ReportsAnUnreadablePath(t *testing.T) {
	if _, err := SHA256(t.TempDir()); err == nil {
		t.Fatal("SHA256(a directory) succeeded, want the read error")
	}
}
