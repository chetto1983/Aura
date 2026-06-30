package documents

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
)

var whitespaceRE = regexp.MustCompile(`\s+`)

// ContentHashes carries the compatibility SHA-1 hash plus the canonical SHA-256 hash.
type ContentHashes struct {
	SHA1   string
	SHA256 string
}

// NormalizeText collapses internal whitespace and trims document text.
func NormalizeText(text string) string {
	return strings.TrimSpace(whitespaceRE.ReplaceAllString(text, " "))
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

// ChunkID derives a stable chunk id from a document id and chunk index.
func ChunkID(documentID string, index int) string {
	return fmt.Sprintf("chunk_%s_%06d", documentID, index)
}

// ChunkHash derives a stable hash from normalized text and source locator.
func ChunkHash(text string, locator Locator) (string, error) {
	loc, err := json.Marshal(locator)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(NormalizeText(text) + "\n" + string(loc)))
	return hex.EncodeToString(sum[:]), nil
}

// BuildExtractedDocument converts extractor output into Aura's indexed document model.
func BuildExtractedDocument(req IngestRequest, contentHash string, resp *ExtractorResponse, createdAt time.Time) (ExtractedDocument, error) {
	if resp == nil {
		return ExtractedDocument{}, fmt.Errorf("extractor response is nil")
	}
	documentID := DocumentID(contentHash, req.SourceID)
	mimeType := req.MIMEType
	if mimeType == "" {
		mimeType = resp.MIMEType
	}
	doc := ExtractedDocument{
		ID:          documentID,
		SourceID:    req.SourceID,
		SourceKind:  req.SourceKind,
		FileName:    req.FileName,
		MIMEType:    mimeType,
		SizeBytes:   req.SizeBytes,
		ContentHash: contentHash,
		Title:       resp.Title,
		Chunks:      make([]Chunk, 0, len(resp.Chunks)),
		CreatedAt:   createdAt,
	}
	for i, chunk := range resp.Chunks {
		text := NormalizeText(chunk.Text)
		if text == "" {
			return ExtractedDocument{}, fmt.Errorf("extractor chunk %d is empty", i)
		}
		hash, err := ChunkHash(text, chunk.Locator)
		if err != nil {
			return ExtractedDocument{}, fmt.Errorf("hash chunk %d: %w", i, err)
		}
		doc.Chunks = append(doc.Chunks, Chunk{
			ID:          ChunkID(documentID, i),
			DocumentID:  documentID,
			SourceID:    req.SourceID,
			ContentHash: contentHash,
			ChunkHash:   hash,
			ChunkIndex:  i,
			ChunkCount:  len(resp.Chunks),
			Kind:        chunk.Kind,
			Text:        text,
			Locator:     chunk.Locator,
			HeadingPath: append([]string(nil), chunk.HeadingPath...),
		})
	}
	return doc, nil
}
