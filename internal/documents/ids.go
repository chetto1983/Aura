package documents

import (
	"crypto/sha1" // #nosec G505 -- compatibility metadata only; SHA-256 remains canonical.
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
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

// DocumentID derives the legacy content-sensitive search identifier. New
// ingestion uses SearchDocumentID so a byte change creates a version rather
// than a second logical document.
func DocumentID(contentHash, sourceID string) string {
	sum := sha256.Sum256([]byte(contentHash + ":" + sourceID))
	return "doc_" + hex.EncodeToString(sum[:])[:32]
}

// SourceKey fingerprints a connector-native locator before it enters the
// document control plane. The fallback must be an ingress-stable idempotency
// key, not a temporary path.
func SourceKey(sourceRef, fallback string) (string, error) {
	locator := strings.TrimSpace(sourceRef)
	if locator == "" {
		locator = strings.TrimSpace(fallback)
	}
	if locator == "" {
		return "", fmt.Errorf("document source locator is required")
	}
	sum := sha256.Sum256([]byte(locator))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// SearchDocumentID is stable across content versions and owner-scoped by
// construction. It deliberately contains no source locator or identity text.
func SearchDocumentID(identityID, sourceKind, sourceKey string) (string, error) {
	identityID = strings.TrimSpace(identityID)
	sourceKind = strings.ToLower(strings.TrimSpace(sourceKind))
	sourceKey = strings.TrimSpace(sourceKey)
	if identityID == "" || sourceKind == "" || sourceKey == "" {
		return "", fmt.Errorf("document identity, source kind, and source key are required")
	}
	h := sha256.New()
	for _, part := range []string{"aura.document.search.v1", identityID, sourceKind, sourceKey} {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(part))
	}
	return "doc_" + hex.EncodeToString(h.Sum(nil))[:32], nil
}
