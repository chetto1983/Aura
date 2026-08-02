package documents

import (
	"crypto/sha1" // #nosec G505 -- compatibility metadata only; SHA-256 remains canonical.
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

// ContentHashes carries the compatibility SHA-1 hash plus the canonical SHA-256 hash.
type ContentHashes struct {
	SHA1   string
	SHA256 string
}

// ContentHashPath returns the SHA-256 hex digest for a local file path.
func ContentHashPath(path string) (string, error) {
	hashes, err := ContentHashesPath(path)
	if err != nil {
		return "", err
	}
	return hashes.SHA256, nil
}

// ContentHashesPath returns SHA-1 and SHA-256 hex digests for a local file path.
func ContentHashesPath(path string) (ContentHashes, error) {
	f, err := os.Open(path) //nolint:gosec // document ingestion intentionally opens the operator-selected local file.
	if err != nil {
		return ContentHashes{}, err
	}
	defer func() { _ = f.Close() }()
	return ContentHashesReader(f)
}

// ContentHashReader returns the SHA-256 hex digest for bytes read from r.
func ContentHashReader(r io.Reader) (string, error) {
	hashes, err := ContentHashesReader(r)
	if err != nil {
		return "", err
	}
	return hashes.SHA256, nil
}

// ContentHashesReader returns SHA-1 and SHA-256 hex digests for bytes read from r.
func ContentHashesReader(r io.Reader) (ContentHashes, error) {
	sha1Hash := sha1.New() //nolint:gosec // SHA-1 is compatibility metadata; SHA-256 remains canonical.
	sha256Hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(sha1Hash, sha256Hash), r); err != nil {
		return ContentHashes{}, err
	}
	return ContentHashes{
		SHA1:   hex.EncodeToString(sha1Hash.Sum(nil)),
		SHA256: hex.EncodeToString(sha256Hash.Sum(nil)),
	}, nil
}

// DocumentID derives Aura's stable document id from content and source identity.
func DocumentID(contentHash, sourceID string) string {
	sum := sha256.Sum256([]byte(contentHash + ":" + sourceID))
	return "doc_" + hex.EncodeToString(sum[:])[:32]
}
