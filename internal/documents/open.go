package documents

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"mime"
	"path"
	"strings"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// DocumentObjectOpener streams one object of one identity's bucket, together with the
// content type the store recorded for it.
//
// It takes a KEY, not an asset id, and that is the whole change: the reconciler records
// where the bytes live, so nothing has to walk a catalog to find out. The implementation
// MUST resolve the owner's own store before reading -- this service relies on that as its
// second gate, not as a courtesy.
//
// The content type comes back because the STORE is the only place that holds it: the
// document card carries a name, a size and a digest, but no media type, so a caller that
// wanted one had nothing to read but the file extension. Empty means the store has none,
// which is an answer; a field nobody ever filled was not.
//
// An interface rather than a direct dependency because internal/assets imports this
// package, so importing it back would be a cycle.
type DocumentObjectOpener interface {
	OpenObject(ctx context.Context, identityID, key string) (io.ReadCloser, string, error)
}

// DocumentRecordLookup resolves the id a retrieval hit carries to the record describing
// where its bytes live. An interface for the same reason the opener beside it is one: the
// resolution is the part worth faking, and a concrete index would drag an HTTP server into
// every test of this service.
type DocumentRecordLookup interface {
	DocumentByID(ctx context.Context, identityID, searchDocumentID string) (arcadedb.DocumentCard, error)
}

// UploadLookup resolves an id the index does not hold to the caller's own upload of that id:
// an attachment is in the store as soon as its upload is accepted, before the ingest writes its
// card, and a photo or a clip never gets one. An interface for the same cycle reason as the
// opener: internal/assets imports this package.
type UploadLookup interface {
	UploadByID(ctx context.Context, identityID, assetID string) (OpenedDocument, error)
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
//
// The one fallback is an attachment: an id the index does not hold opens the caller's own upload
// of that id, when Uploads is set (measured 2026-10-19: the agent called document_open with the
// asset id of a spreadsheet attached two seconds earlier, and the index answered "not found").
type OpenService struct {
	Index   DocumentRecordLookup
	Objects DocumentObjectOpener
	Uploads UploadLookup
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
		return s.openUpload(ctx, identityID, documentID, err)
	}
	body, mimeType, err := s.Objects.OpenObject(ctx, identityID, record.SourceKey)
	if err != nil {
		return nil, OpenedDocument{}, fmt.Errorf("open document %s: %w", documentID, err)
	}
	fileName := documentFileName(record)
	return body, OpenedDocument{
		DocumentID: record.SearchDocumentID,
		FileName:   fileName,
		SourceKey:  record.SourceKey,
		MIMEType:   openedMIMEType(mimeType, fileName),
		SizeBytes:  record.SizeBytes,
		SHA256:     record.RawSHA256,
	}, nil
}

const genericMIMEType = "application/octet-stream"

// openedMIMEType is the media type the store recorded, or the file name's when it recorded
// none or only application/octet-stream, which says the uploader did not know the type.
func openedMIMEType(stored, fileName string) string {
	if stored != "" && stored != genericMIMEType {
		return stored
	}
	if byName := mime.TypeByExtension(strings.ToLower(path.Ext(fileName))); byName != "" {
		return byName
	}
	return genericMIMEType
}

// openUpload opens the caller's own upload of id, or returns the index's error when there is no
// such upload: a wrong id must still read as the index's "not found".
func (s *OpenService) openUpload(ctx context.Context, identityID, id string, indexErr error) (io.ReadCloser, OpenedDocument, error) {
	if s.Uploads == nil {
		return nil, OpenedDocument{}, indexErr
	}
	upload, err := s.Uploads.UploadByID(ctx, identityID, id)
	if err != nil {
		return nil, OpenedDocument{}, indexErr
	}
	body, mimeType, err := s.Objects.OpenObject(ctx, identityID, upload.SourceKey)
	if err != nil {
		return nil, OpenedDocument{}, fmt.Errorf("open upload %s: %w", id, err)
	}
	upload.MIMEType = openedMIMEType(cmp.Or(mimeType, upload.MIMEType), upload.FileName)
	return body, upload, nil
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
