package documents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresCatalogStore persists document catalog metadata through sqlc.
//
// pool is the RLS carrier, not a third query handle: migration 0087 put a fail-closed
// floor on aura.documents and its children, so every statement that touches them runs
// inside a transaction that first binds app.current_identity. See scoped.
type PostgresCatalogStore struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
	db   sqlc.DBTX
}

var _ CatalogStore = (*PostgresCatalogStore)(nil)

// NewPostgresCatalogStore builds a Postgres-backed document catalog store.
//
// A *pgxpool.Pool is ALSO kept as the pool, so the store can open the identity-scoped
// transactions aura.documents now requires. Every deployed caller already passes one —
// which is why the signature does not change and no call site moves. Any other DBTX is a
// handle the caller already owns and is responsible for scoping; it is used as-is.
func NewPostgresCatalogStore(database sqlc.DBTX) *PostgresCatalogStore {
	store := &PostgresCatalogStore{q: sqlc.New(database), db: database}
	if pool, ok := database.(*pgxpool.Pool); ok {
		store.pool = pool
	}
	return store
}

// CreateDocument inserts one logical document and mirrors its tags to document_tags.
func (s *PostgresCatalogStore) CreateDocument(ctx context.Context, req CreateDocumentRequest) (Document, error) {
	return scopedValue(ctx, s, req.IdentityID, func(sc catalogTx) (Document, error) {
		return sc.createDocument(ctx, req)
	})
}

// UpdateDocument updates logical document metadata and replaces its tag rows.
func (s *PostgresCatalogStore) UpdateDocument(ctx context.Context, req UpdateDocumentRequest) (Document, error) {
	return scopedValue(ctx, s, req.IdentityID, func(sc catalogTx) (Document, error) {
		return sc.updateDocument(ctx, req)
	})
}

// ListDocuments returns non-deleted document summaries for one identity.
func (s *PostgresCatalogStore) ListDocuments(ctx context.Context, req ListDocumentsRequest) ([]DocumentSummary, error) {
	return scopedValue(ctx, s, req.IdentityID, func(sc catalogTx) ([]DocumentSummary, error) {
		return sc.listDocuments(ctx, req)
	})
}

// GetDocument returns one document and its immutable version history.
func (s *PostgresCatalogStore) GetDocument(ctx context.Context, identityID, documentID string) (DocumentDetail, error) {
	return scopedValue(ctx, s, identityID, func(sc catalogTx) (DocumentDetail, error) {
		return sc.getDocument(ctx, identityID, documentID)
	})
}

// SoftDeleteDocument starts the durable, owner-scoped delete workflow.
func (s *PostgresCatalogStore) SoftDeleteDocument(ctx context.Context, identityID, documentID string) (Document, error) {
	return scopedValue(ctx, s, identityID, func(sc catalogTx) (Document, error) {
		return sc.softDeleteDocument(ctx, identityID, documentID)
	})
}

// ListStorageObjects returns the caller identity's live ledger refs for orphan detection.
func (s *PostgresCatalogStore) ListStorageObjects(ctx context.Context, bucket, prefix string) ([]objectstore.ObjectRef, error) {
	identityID := identityctx.IdentityID(ctx)
	owner, err := pgUUID("identity_id", identityID)
	if err != nil {
		return nil, err
	}
	return scopedValue(ctx, s, identityID, func(sc catalogTx) ([]objectstore.ObjectRef, error) {
		rows, queryErr := sc.q.ListStorageObjects(ctx, sqlc.ListStorageObjectsParams{
			IdentityID: owner, Bucket: bucket, Prefix: prefix,
		})
		if queryErr != nil {
			return nil, queryErr
		}
		out := make([]objectstore.ObjectRef, 0, len(rows))
		for _, row := range rows {
			out = append(out, objectstore.ObjectRef{Bucket: row.Bucket, Key: row.ObjectKey})
		}
		return out, nil
	})
}

func (sc catalogTx) createDocument(ctx context.Context, req CreateDocumentRequest) (Document, error) {
	identityID, err := pgUUID("identity_id", req.IdentityID)
	if err != nil {
		return Document{}, err
	}
	metadata, err := catalogMetadataJSON(req.Metadata)
	if err != nil {
		return Document{}, err
	}
	row, err := sc.q.CreateDocument(ctx, sqlc.CreateDocumentParams{
		IdentityID: identityID,
		Scope:      string(req.Scope), Title: req.Title, Tags: catalogTagsArray(req.Tags),
		Metadata: metadata, Status: string(req.Status), SourceKind: req.SourceKind,
		SourceKey: req.SourceKey, SearchDocumentID: req.SearchDocumentID,
		PipelineGeneration: req.PipelineGeneration,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Document{}, ErrDocumentDeleteInFlight
		}
		return Document{}, err
	}
	doc, err := catalogDocumentFromSQL(row)
	if err != nil {
		return Document{}, err
	}
	if err := sc.replaceDocumentTags(ctx, doc.ID, req.IdentityID, req.Tags); err != nil {
		return Document{}, err
	}
	return doc, nil
}

func (sc catalogTx) updateDocument(ctx context.Context, req UpdateDocumentRequest) (Document, error) {
	documentID, err := pgUUID("document_id", req.DocumentID)
	if err != nil {
		return Document{}, err
	}
	identityID, err := pgUUID("identity_id", req.IdentityID)
	if err != nil {
		return Document{}, err
	}
	activeVersionID, err := pgOptionalUUID("active_version_id", req.ActiveVersionID)
	if err != nil {
		return Document{}, err
	}
	metadata, err := catalogMetadataJSON(req.Metadata)
	if err != nil {
		return Document{}, err
	}
	row, err := sc.q.UpdateDocument(ctx, sqlc.UpdateDocumentParams{
		ID:                 documentID,
		IdentityID:         identityID,
		Scope:              string(req.Scope),
		Title:              req.Title,
		Tags:               catalogTagsArray(req.Tags),
		Metadata:           metadata,
		ActiveVersionID:    activeVersionID,
		Status:             string(req.Status),
		PipelineGeneration: req.PipelineGeneration,
	})
	if err != nil {
		return Document{}, err
	}
	doc, err := catalogDocumentFromSQL(row)
	if err != nil {
		return Document{}, err
	}
	if err := sc.replaceDocumentTags(ctx, doc.ID, req.IdentityID, req.Tags); err != nil {
		return Document{}, err
	}
	return doc, nil
}

func (sc catalogTx) listDocuments(ctx context.Context, req ListDocumentsRequest) ([]DocumentSummary, error) {
	identityID, err := pgUUID("identity_id", req.IdentityID)
	if err != nil {
		return nil, err
	}
	rows, err := sc.q.ListDocuments(ctx, sqlc.ListDocumentsParams{
		IdentityID:  identityID,
		ScopeFilter: string(req.Scope),
		Query:       req.Query,
		TagFilter:   req.Tag,
		RowLimit:    int32(req.Limit),  //nolint:gosec // normalized by CatalogService to <= maxCatalogListLimit.
		RowOffset:   int32(req.Offset), //nolint:gosec // normalized by CatalogService to be non-negative.
	})
	if err != nil {
		return nil, err
	}
	out := make([]DocumentSummary, 0, len(rows))
	for _, row := range rows {
		doc, err := catalogDocumentFromSQL(row)
		if err != nil {
			return nil, err
		}
		out = append(out, doc)
	}
	if err := sc.attachActiveVersionSizes(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (sc catalogTx) getDocument(ctx context.Context, identityID, documentID string) (DocumentDetail, error) {
	pgDocumentID, err := pgUUID("document_id", documentID)
	if err != nil {
		return DocumentDetail{}, err
	}
	pgIdentityID, err := pgUUID("identity_id", identityID)
	if err != nil {
		return DocumentDetail{}, err
	}
	row, err := sc.q.GetDocument(ctx, sqlc.GetDocumentParams{
		ID:         pgDocumentID,
		IdentityID: pgIdentityID,
	})
	if err != nil {
		return DocumentDetail{}, err
	}
	doc, err := catalogDocumentFromSQL(row)
	if err != nil {
		return DocumentDetail{}, err
	}
	versionRows, err := sc.q.ListDocumentVersions(ctx, sqlc.ListDocumentVersionsParams{
		IdentityID: pgIdentityID, DocumentID: pgDocumentID,
	})
	if err != nil {
		return DocumentDetail{}, err
	}
	versions := make([]DocumentVersion, 0, len(versionRows))
	for _, versionRow := range versionRows {
		versions = append(versions, catalogVersionFromSQL(versionRow))
	}
	return DocumentDetail{Document: doc, Versions: versions}, nil
}

func (sc catalogTx) softDeleteDocument(ctx context.Context, identityID, documentID string) (Document, error) {
	pgDocumentID, err := pgUUID("document_id", documentID)
	if err != nil {
		return Document{}, err
	}
	pgIdentityID, err := pgUUID("identity_id", identityID)
	if err != nil {
		return Document{}, err
	}
	row, err := sc.q.SoftDeleteDocument(ctx, sqlc.SoftDeleteDocumentParams{
		ID:         pgDocumentID,
		IdentityID: pgIdentityID,
	})
	if err != nil {
		return Document{}, err
	}
	return catalogDocumentFromSQL(sqlc.AuraDocuments(row))
}

// attachActiveVersionSizes denormalizes each summary's active-version size and
// content type in ONE batched query (never N+1), so the list view renders a size
// and kind per row without fetching each document's detail.
func (sc catalogTx) attachActiveVersionSizes(ctx context.Context, summaries []DocumentSummary) error {
	if sc.raw == nil {
		return nil
	}
	ids := make([]string, 0, len(summaries))
	for i := range summaries {
		if summaries[i].ActiveVersionID != "" {
			ids = append(ids, summaries[i].ActiveVersionID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := sc.raw.Query(ctx, `
SELECT id::text, size_bytes, content_type
FROM aura.document_versions
WHERE id::text = ANY($1)
  AND deleted_at IS NULL`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	type versionFacts struct {
		size        int64
		contentType string
	}
	byID := make(map[string]versionFacts, len(ids))
	for rows.Next() {
		var id, contentType string
		var size int64
		if err := rows.Scan(&id, &size, &contentType); err != nil {
			return err
		}
		byID[id] = versionFacts{size: size, contentType: contentType}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range summaries {
		if facts, ok := byID[summaries[i].ActiveVersionID]; ok {
			summaries[i].ActiveSizeBytes = facts.size
			summaries[i].ActiveContentType = facts.contentType
		}
	}
	return nil
}

// replaceDocumentTags rewrites the searchable tag mirror of one document.
func (sc catalogTx) replaceDocumentTags(ctx context.Context, documentID, actorIdentityID string, tags []string) error {
	pgDocumentID, err := pgUUID("document_id", documentID)
	if err != nil {
		return err
	}
	pgActorID, err := pgOptionalUUID("created_by", actorIdentityID)
	if err != nil {
		return err
	}
	if err := sc.q.DeleteDocumentTags(ctx, pgDocumentID); err != nil {
		return err
	}
	for _, tag := range tags {
		if err := sc.q.UpsertDocumentTag(ctx, sqlc.UpsertDocumentTagParams{
			DocumentID: pgDocumentID,
			Tag:        tag,
			CreatedBy:  pgActorID,
		}); err != nil {
			return err
		}
	}
	return nil
}

// catalogDocumentFromSQL is the single decoder for a document row.
//
// sqlc emits a distinct row type per query, but the delete-path statements return
// aura.documents in table order, so those rows convert to sqlc.AuraDocuments outright.
// Callers use that conversion instead of restating the twenty fields: a column added to
// the table but missing from a query then becomes a compile error here, where a
// field-by-field literal would have silently handed back a zero value.
func catalogDocumentFromSQL(row sqlc.AuraDocuments) (Document, error) {
	metadata, err := catalogMetadataFromJSON(row.Metadata)
	if err != nil {
		return Document{}, err
	}
	return Document{
		ID:         uuidString(row.ID),
		IdentityID: uuidString(row.IdentityID),
		SourceKind: row.SourceKind, SourceKey: row.SourceKey,
		SearchDocumentID:   row.SearchDocumentID,
		PipelineGeneration: row.PipelineGeneration,
		Scope:              DocumentScope(row.Scope),
		Title:              row.Title,
		Tags:               catalogTagsArray(row.Tags),
		Metadata:           metadata,
		ActiveVersionID:    uuidString(row.ActiveVersionID),
		Status:             DocumentStatus(row.Status),
		CreatedAt:          timeValue(row.CreatedAt),
		UpdatedAt:          timeValue(row.UpdatedAt),
		DeletedAt:          timeValue(row.DeletedAt),
	}, nil
}

func catalogVersionFromSQL(row sqlc.AuraDocumentVersions) DocumentVersion {
	return DocumentVersion{
		ID:                 uuidString(row.ID),
		IdentityID:         uuidString(row.IdentityID),
		DocumentID:         uuidString(row.DocumentID),
		SearchDocumentID:   row.SearchDocumentID,
		PipelineGeneration: row.PipelineGeneration,
		AssetID:            uuidString(row.AssetID),
		VersionNumber:      int(row.VersionNumber),
		Status:             DocumentVersionStatus(row.Status),
		SHA1:               row.Sha1,
		SHA256:             row.Sha256,
		ContentType:        row.ContentType,
		SizeBytes:          row.SizeBytes,
		StorageObjectID:    uuidString(row.StorageObjectID),
		ChunkingConfigHash: row.ChunkingConfigHash,
		PipelineConfigHash: row.PipelineConfigHash,
		CreatedAt:          timeValue(row.CreatedAt),
		UpdatedAt:          timeValue(row.UpdatedAt),
	}
}

func catalogMetadataJSON(metadata map[string]any) ([]byte, error) {
	if metadata == nil {
		metadata = map[string]any{}
	}
	out, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("document metadata: %w", err)
	}
	return out, nil
}

func catalogTagsArray(tags []string) []string {
	if tags == nil {
		return []string{}
	}
	return tags
}

func catalogMetadataFromJSON(data []byte) (map[string]any, error) {
	if len(data) == 0 {
		return map[string]any{}, nil
	}
	var metadata map[string]any
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("document metadata: %w", err)
	}
	if metadata == nil {
		return map[string]any{}, nil
	}
	return metadata, nil
}

func pgOptionalUUID(field, value string) (pgtype.UUID, error) {
	if strings.TrimSpace(value) == "" {
		return pgtype.UUID{}, nil
	}
	return pgUUID(field, value)
}
