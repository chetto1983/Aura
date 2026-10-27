// Package filehash hashes a file without holding it in memory.
package filehash

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

// SHA256 returns the lower-case hex SHA-256 of the file at path, streamed.
func SHA256(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // callers pass an operator-configured path whose hash is the check.
	if err != nil {
		return "", err
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
