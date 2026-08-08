package documents

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// DocumentObjectOpener streams one object of one identity's bucket.
//
// It takes a KEY, not an asset id, and that is the whole change: the reconciler records
// where the bytes live, so nothing has to walk a catalog to find out. The implementation
// MUST resolve the owner's own store before reading -- this service relies on that as its
// second gate, not as a courtesy.
//
// An interface rather than a direct dependency because internal/assets imports this
// package, so importing it back would be a cycle.
type DocumentObjectOpener interface {
	OpenObject(ctx context.Context, identityID, key string) (io.ReadCloser, error)
}

// DocumentRecordLookup resolves the id a retrieval hit carries to the record describing
// where its bytes live. An interface for the same reason the opener beside it is one: the
// resolution is the part worth faking, and a concrete index would drag an HTTP server into
// every test of this service.
type DocumentRecordLookup interface {
	DocumentByID(ctx context.Context, identityID, searchDocumentID string) (arcadedb.DocumentCard, error)
}

// OpenedDocument describes the original file behind an indexed document.
type OpenedDocument struct {
	DocumentID string `json:"document_id"`
	FileName   string `json:"file_name"`
	SourceKey  string `json:"source_key"`
	MIMEType   string `json:"mime_type"`
	SizeBytes  int64  `json:"size_bytes"`
	SHA256     string `json:"sha256"`
}

// OpenService resolves an indexed document back to the bytes it was made from.
//
// Retrieval answers "what does this document say" by returning text, which for a
// spreadsheet is the wrong question: measured on a 5889-row customer list, an exact lookup
// scores 100% (BM25) and an aggregate -- "how many customers in Torino" -- scores 0% at
// every k, because the answer is a property of the whole set and lives in no passage.
// Handing over the file removes the ceiling: the agent's container already carries
// LibreOffice, openpyxl and pandas.
//
// ONE HOP. It used to be five -- doc_<hex> to a catalog uuid, to the document, to its
// active version, to an asset id, to an object -- and every one of them was a place the
// chain could break, which is exactly what amendment #114's own audit found when catalog
// rows created by `aura docs ingest` could not satisfy their own document_open contract.
// The record carries source_key now, so there is nothing to walk.
type OpenService struct {
	Index   DocumentRecordLookup
	Objects DocumentObjectOpener
}

// OpenDocument streams the original bytes of documentID for identityID. The caller closes
// the reader.
func (s *OpenService) OpenDocument(
	ctx context.Context,
	identityID, documentID string,
) (io.ReadCloser, OpenedDocument, error) {
	if s == nil || s.Index == nil {
		return nil, OpenedDocument{}, fmt.Errorf("document open service has no document index")
	}
	if s.Objects == nil {
		return nil, OpenedDocument{}, fmt.Errorf("document open service has no object opener")
	}
	documentID = strings.TrimSpace(documentID)
	if documentID == "" {
		return nil, OpenedDocument{}, fmt.Errorf("document_id is required")
	}
	if strings.TrimSpace(identityID) == "" {
		return nil, OpenedDocument{}, fmt.Errorf("identity_id is required")
	}
	record, err := s.Index.DocumentByID(ctx, identityID, documentID)
	if err != nil {
		return nil, OpenedDocument{}, err
	}
	body, err := s.Objects.OpenObject(ctx, identityID, record.SourceKey)
	if err != nil {
		return nil, OpenedDocument{}, fmt.Errorf("open document %s: %w", documentID, err)
	}
	return body, OpenedDocument{
		DocumentID: record.SearchDocumentID,
		FileName:   documentFileName(record),
		SourceKey:  record.SourceKey,
		SizeBytes:  record.SizeBytes,
		SHA256:     record.RawSHA256,
	}, nil
}

// documentFileName recovers a safe base name for the materialized copy.
//
// The name becomes a path component in the caller's box, so a key whose base resolves to
// nothing, to a traversal, or to a dotfile is replaced by the digest rather than trusted.
func documentFileName(record arcadedb.DocumentCard) string {
	name := strings.TrimSpace(record.FileName)
	if name == "" {
		name = record.SourceKey
	}
	name = path.Base(strings.ReplaceAll(strings.TrimSpace(name), `\`, "/"))
	if name == "" || name == "." || name == ".." || strings.HasPrefix(name, ".") {
		return "document-" + shortHash(record.RawSHA256)
	}
	return name
}

func shortHash(sha string) string {
	sha = strings.TrimSpace(sha)
	if len(sha) > 12 {
		return sha[:12]
	}
	if sha == "" {
		return "unknown"
	}
	return sha
}
