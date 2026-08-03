package documents

import (
	"context"
	"fmt"
	"log/slog"
)

// DocumentAssetDeleter removes source assets that advertise a deleted document.
type DocumentAssetDeleter interface {
	DeleteDocumentAsset(ctx context.Context, identityID, assetID string) error
}

// StagedDocumentPurger removes the copy document_open wrote into the identity's
// sandbox. It is an interface so this package keeps knowing nothing about
// containers; the implementation lives at the composition root, next to the router
// document_open already holds.
type StagedDocumentPurger interface {
	PurgeStagedDocument(ctx context.Context, fileName string) error
}

// DeleteService coordinates logical document delete with source-asset cleanup and
// the removal of the staged copy inside the sandbox.
type DeleteService struct {
	Catalog *CatalogService
	Assets  DocumentAssetDeleter
	Staged  StagedDocumentPurger
}

// SoftDeleteDocument soft-deletes catalog metadata and removes the source assets.
func (s *DeleteService) SoftDeleteDocument(ctx context.Context, identityID, documentID string) (Document, error) {
	if s == nil || s.Catalog == nil {
		return Document{}, fmt.Errorf("document delete service has no catalog")
	}
	detail, err := s.Catalog.GetDocument(ctx, identityID, documentID)
	if err != nil {
		return Document{}, err
	}
	deleted, err := s.Catalog.DeleteDocument(ctx, identityID, documentID)
	if err != nil {
		return Document{}, err
	}
	// The catalog soft-delete above is the authoritative delete the operator sees.
	// Asset object cleanup is best-effort reconciliation: it must NOT fail the
	// operator's delete, because (a) the catalog step already soft-deletes the
	// document's asset rows, so re-deleting them here returns "already deleted",
	// and (b) any residue is swept by the storage-orphan tooling. A cleanup error
	// is logged and swallowed so the delete still succeeds.
	if s.Assets != nil {
		for _, version := range detail.Versions {
			if version.AssetID == "" {
				continue
			}
			if err := s.Assets.DeleteDocumentAsset(ctx, identityID, version.AssetID); err != nil {
				slog.Default().Warn("document delete: asset cleanup failed (reconciled by orphan cleanup)",
					"document_id", documentID, "asset_id", version.AssetID, "error", err)
			}
		}
	}
	s.purgeStagedCopy(ctx, documentID, detail)
	return deleted, nil
}

// purgeStagedCopy removes the file document_open left in the sandbox.
//
// Without it the delete was only half a delete. Measured live on 2026-08-03: the
// operator deleted Clienti.xlsx, the catalog closed the document AND its asset, and
// document_search stopped returning it — then the same question was asked again and
// she answered it, because the staged copy was still on the box filesystem and
// fs_glob found it by name. Deleting data the agent can still read is not deleting
// data.
//
// It runs AFTER the catalog delete, deliberately: the catalog row is what the
// operator sees, and a container that cannot be reached must not hold their delete
// hostage. A failure here is logged loudly rather than swallowed — it means bytes
// the operator asked to be gone are still readable, which the orphan-cleanup
// endpoint does not cover: that one reconciles the object store, not the box volume.
//
// The name comes from documentFileName — the SAME function OpenDocument stages
// under (open.go) — and not from the raw catalog title. That distinction is the
// whole correctness of this function: the title is an uploaded file name and can
// carry separators or resolve to a dotted name, in which case the staging side
// replaces it with "document-<sha12>". A purge built on the raw title would then
// name a path nothing was ever written to and remove NOTHING, silently, which is
// worse than not trying — the operator would be told the delete succeeded.
//
// One name per version, because a document with several versions can have any of
// them staged, and rm -f on an absent path is free.
//
// A copy the model chose to stage under a caller-supplied file_name still survives
// this: closing that needs a reconcile over the whole documents directory rather
// than a targeted remove.
func (s *DeleteService) purgeStagedCopy(ctx context.Context, documentID string, detail DocumentDetail) {
	if s.Staged == nil {
		return
	}
	for _, name := range stagedNames(detail) {
		if err := s.Staged.PurgeStagedDocument(ctx, name); err != nil {
			slog.Default().Error("document delete: the staged copy is STILL READABLE in the sandbox",
				"document_id", documentID, "file_name", name, "error", err)
		}
	}
}

// stagedNames returns every name OpenDocument could have staged this document
// under, deduplicated and in version order.
func stagedNames(detail DocumentDetail) []string {
	seen := map[string]struct{}{}
	names := make([]string, 0, len(detail.Versions)+1)
	add := func(name string) {
		if name == "" {
			return
		}
		if _, dup := seen[name]; dup {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	for _, version := range detail.Versions {
		add(documentFileName(detail.Document, version))
	}
	// A document with no version row still has a title, and a copy could have been
	// staged from an earlier state. The zero version only affects the fallback name.
	if len(detail.Versions) == 0 {
		add(documentFileName(detail.Document, DocumentVersion{}))
	}
	return names
}

// DeletingCatalog exposes normal catalog operations and routes delete through asset
// cleanup and the sandbox purge.
type DeletingCatalog struct {
	Catalog *CatalogService
	Assets  DocumentAssetDeleter
	Staged  StagedDocumentPurger
}

// CreateDocument delegates to the wrapped catalog.
func (c *DeletingCatalog) CreateDocument(ctx context.Context, req CreateDocumentRequest) (Document, error) {
	catalog, err := c.catalog()
	if err != nil {
		return Document{}, err
	}
	return catalog.CreateDocument(ctx, req)
}

// UpdateDocument delegates to the wrapped catalog.
func (c *DeletingCatalog) UpdateDocument(ctx context.Context, req UpdateDocumentRequest) (Document, error) {
	catalog, err := c.catalog()
	if err != nil {
		return Document{}, err
	}
	return catalog.UpdateDocument(ctx, req)
}

// ListDocuments delegates to the wrapped catalog.
func (c *DeletingCatalog) ListDocuments(ctx context.Context, req ListDocumentsRequest) ([]DocumentSummary, error) {
	catalog, err := c.catalog()
	if err != nil {
		return nil, err
	}
	return catalog.ListDocuments(ctx, req)
}

// GetDocument delegates to the wrapped catalog.
func (c *DeletingCatalog) GetDocument(ctx context.Context, identityID, documentID string) (DocumentDetail, error) {
	catalog, err := c.catalog()
	if err != nil {
		return DocumentDetail{}, err
	}
	return catalog.GetDocument(ctx, identityID, documentID)
}

// DeleteDocument soft-deletes the document and removes its source assets.
func (c *DeletingCatalog) DeleteDocument(ctx context.Context, identityID, documentID string) (Document, error) {
	catalog, err := c.catalog()
	if err != nil {
		return Document{}, err
	}
	return (&DeleteService{Catalog: catalog, Assets: c.Assets, Staged: c.Staged}).
		SoftDeleteDocument(ctx, identityID, documentID)
}

func (c *DeletingCatalog) catalog() (*CatalogService, error) {
	if c == nil || c.Catalog == nil {
		return nil, fmt.Errorf("deleting catalog has no catalog")
	}
	return c.Catalog, nil
}
