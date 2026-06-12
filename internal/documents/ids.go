package documents

import (
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

func NormalizeText(text string) string {
	return strings.TrimSpace(whitespaceRE.ReplaceAllString(text, " "))
}

func ContentHashPath(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	return ContentHashReader(f)
}

func ContentHashReader(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func DocumentID(contentHash, sourceID string) string {
	sum := sha256.Sum256([]byte(contentHash + ":" + sourceID))
	return "doc_" + hex.EncodeToString(sum[:])[:32]
}

func ChunkID(documentID string, index int) string {
	return fmt.Sprintf("chunk_%s_%06d", documentID, index)
}

func ChunkHash(text string, locator Locator) (string, error) {
	loc, err := json.Marshal(locator)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(NormalizeText(text) + "\n" + string(loc)))
	return hex.EncodeToString(sum[:]), nil
}

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
