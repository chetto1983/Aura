package documents

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"
)

// SearchDocumentResolver maps a content-derived search document id (`doc_<hex>`,
// what a retrieval hit carries) to the catalog's uuid primary key. The two are
// different namespaces; PostgresCatalogStore holds the mapping in
// metadata->>'search_document_id'.
//
// It is a separate interface rather than a CatalogStore method so the existing
// CatalogStore fakes keep compiling: resolution is needed by this service alone.
type SearchDocumentResolver interface {
	CatalogIDForSearchDocument(ctx context.Context, searchDocumentID string) (string, error)
}

// DocumentAssetOpener opens the stored body of one identity-scoped asset. The
// implementation MUST re-check ownership before touching the object store — this
// service relies on that as its second gate, not as a courtesy.
type DocumentAssetOpener interface {
	OpenDocumentAsset(ctx context.Context, identityID, assetID string) (io.ReadCloser, error)
}

// OpenedDocument describes the original file behind an indexed document.
type OpenedDocument struct {
	DocumentID string `json:"document_id"`
	CatalogID  string `json:"catalog_id"`
	FileName   string `json:"file_name"`
	MIMEType   string `json:"mime_type"`
	SizeBytes  int64  `json:"size_bytes"`
	SHA256     string `json:"sha256"`
}

// OpenService resolves an indexed document back to the bytes it was made from.
//
// Retrieval answers "what does this document say" by returning text, which for a
// spreadsheet is the wrong question: measured on a 5889-row customer list, an
// exact lookup scores 100% (BM25) and an aggregate — "how many customers in
// Torino" — scores 0% at every k, because the answer is a property of the whole
// set and lives in no passage. Handing over the file removes the ceiling: the
// agent's container already carries LibreOffice, openpyxl and pandas.
//
// Two identity gates, deliberately. CatalogService.GetDocument is scoped, and
// the asset opener re-checks ownership before any object-store read (T-IDOR,
// D-12). A non-owner is refused twice and never reaches storage.
type OpenService struct {
	Catalog  *CatalogService
	Resolver SearchDocumentResolver
	Assets   DocumentAssetOpener
}

// OpenDocument streams the original bytes of documentID for identityID. The
// caller closes the reader. documentID may be either id namespace — a retrieval
// hit carries the search id, the web catalog carries the uuid, and the agent
// holds whichever its caller gave it.
func (s *OpenService) OpenDocument(
	ctx context.Context,
	identityID, documentID string,
) (io.ReadCloser, OpenedDocument, error) {
	if s == nil || s.Catalog == nil {
		return nil, OpenedDocument{}, fmt.Errorf("document open service has no catalog")
	}
	if s.Assets == nil {
		return nil, OpenedDocument{}, fmt.Errorf("document open service has no asset opener")
	}
	documentID = strings.TrimSpace(documentID)
	if documentID == "" {
		return nil, OpenedDocument{}, fmt.Errorf("document_id is required")
	}
	if strings.TrimSpace(identityID) == "" {
		return nil, OpenedDocument{}, fmt.Errorf("identity_id is required")
	}

	catalogID, err := s.catalogID(ctx, documentID)
	if err != nil {
		return nil, OpenedDocument{}, err
	}
	detail, err := s.Catalog.GetDocument(ctx, identityID, catalogID)
	if err != nil {
		return nil, OpenedDocument{}, err
	}
	version, err := activeVersion(detail)
	if err != nil {
		return nil, OpenedDocument{}, fmt.Errorf("document %s: %w", documentID, err)
	}
	body, err := s.Assets.OpenDocumentAsset(ctx, identityID, version.AssetID)
	if err != nil {
		return nil, OpenedDocument{}, fmt.Errorf("open document %s: %w", documentID, err)
	}
	return body, OpenedDocument{
		DocumentID: documentID,
		CatalogID:  catalogID,
		FileName:   documentFileName(detail.Document, version),
		MIMEType:   version.ContentType,
		SizeBytes:  version.SizeBytes,
		SHA256:     version.SHA256,
	}, nil
}

// catalogID normalizes either id namespace to the catalog uuid. A uuid is used
// as-is; anything else (`doc_<hex>`) goes through the resolver.
func (s *OpenService) catalogID(ctx context.Context, documentID string) (string, error) {
	if _, err := uuid.Parse(documentID); err == nil {
		return documentID, nil
	}
	if s.Resolver == nil {
		return "", fmt.Errorf(
			"document %s is a search id and no catalog resolver is configured", documentID)
	}
	catalogID, err := s.Resolver.CatalogIDForSearchDocument(ctx, documentID)
	if err != nil {
		return "", fmt.Errorf("resolve document %s: %w", documentID, err)
	}
	return catalogID, nil
}

// activeVersion picks the version whose bytes the document currently stands for:
// the one the catalog marks active, else the highest version number. A version
// without an asset id predates the asset chain and has no original to open.
func activeVersion(detail DocumentDetail) (DocumentVersion, error) {
	if len(detail.Versions) == 0 {
		return DocumentVersion{}, fmt.Errorf("has no stored versions")
	}
	versions := append([]DocumentVersion(nil), detail.Versions...)
	sort.SliceStable(versions, func(i, j int) bool {
		return versions[i].VersionNumber > versions[j].VersionNumber
	})
	chosen := versions[0]
	if active := strings.TrimSpace(detail.Document.ActiveVersionID); active != "" {
		for _, version := range versions {
			if version.ID == active {
				chosen = version
				break
			}
		}
	}
	if strings.TrimSpace(chosen.AssetID) == "" {
		return DocumentVersion{}, fmt.Errorf(
			"version %d has no source asset; its original was never stored", chosen.VersionNumber)
	}
	return chosen, nil
}

// documentFileName recovers a safe base name for the materialized copy. The
// catalog stores the uploaded name as the document title (RecordAssetVersion
// defaults Title to FileName), so that is the best name available; a title that
// carries separators or resolves to nothing is replaced rather than trusted,
// since it becomes a path component.
func documentFileName(doc Document, version DocumentVersion) string {
	name := filepath.Base(strings.TrimSpace(doc.Title))
	name = strings.TrimSpace(strings.ReplaceAll(name, `\`, "/"))
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	if name == "" || name == "." || name == ".." || strings.HasPrefix(name, ".") {
		return "document-" + shortHash(version.SHA256)
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
