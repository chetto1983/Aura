package documents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// PostgresCatalogStore persists document catalog metadata through sqlc.
type PostgresCatalogStore struct {
	q  *sqlc.Queries
	db sqlc.DBTX
}

var _ CatalogStore = (*PostgresCatalogStore)(nil)

// NewPostgresCatalogStore builds a Postgres-backed document catalog store.
func NewPostgresCatalogStore(db sqlc.DBTX) *PostgresCatalogStore {
	return &PostgresCatalogStore{q: sqlc.New(db), db: db}
}

// CreateDocument inserts one logical document and mirrors its tags to document_tags.
func (s *PostgresCatalogStore) CreateDocument(ctx context.Context, req CreateDocumentRequest) (Document, error) {
	identityID, err := pgUUID("identity_id", req.IdentityID)
	if err != nil {
		return Document{}, err
	}
	metadata, err := catalogMetadataJSON(req.Metadata)
	if err != nil {
		return Document{}, err
	}
	row, err := s.q.CreateDocument(ctx, sqlc.CreateDocumentParams{
		IdentityID: identityID,
		Scope:      string(req.Scope),
		Title:      req.Title,
		Tags:       catalogTagsArray(req.Tags),
		Metadata:   metadata,
		Status:     string(req.Status),
	})
	if err != nil {
		return Document{}, err
	}
	doc, err := catalogDocumentFromSQL(row)
	if err != nil {
		return Document{}, err
	}
	if err := s.replaceDocumentTags(ctx, doc.ID, req.IdentityID, req.Tags); err != nil {
		return Document{}, err
	}
	return doc, nil
}

// UpdateDocument updates logical document metadata and replaces its tag rows.
func (s *PostgresCatalogStore) UpdateDocument(ctx context.Context, req UpdateDocumentRequest) (Document, error) {
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
	row, err := s.q.UpdateDocument(ctx, sqlc.UpdateDocumentParams{
		ID:              documentID,
		IdentityID:      identityID,
		Scope:           string(req.Scope),
		Title:           req.Title,
		Tags:            catalogTagsArray(req.Tags),
		Metadata:        metadata,
		ActiveVersionID: activeVersionID,
		Status:          string(req.Status),
	})
	if err != nil {
		return Document{}, err
	}
	doc, err := catalogDocumentFromSQL(row)
	if err != nil {
		return Document{}, err
	}
	if err := s.replaceDocumentTags(ctx, doc.ID, req.IdentityID, req.Tags); err != nil {
		return Document{}, err
	}
	return doc, nil
}

// ListDocuments returns non-deleted document summaries for one identity.
func (s *PostgresCatalogStore) ListDocuments(ctx context.Context, req ListDocumentsRequest) ([]DocumentSummary, error) {
	identityID, err := pgUUID("identity_id", req.IdentityID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListDocuments(ctx, sqlc.ListDocumentsParams{
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
	if err := s.attachActiveVersionSizes(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

// attachActiveVersionSizes denormalizes each summary's active-version size and
// content type in ONE batched query (never N+1), so the list view renders a size
// and kind per row without fetching each document's detail.
func (s *PostgresCatalogStore) attachActiveVersionSizes(ctx context.Context, summaries []DocumentSummary) error {
	if s.db == nil {
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
	rows, err := s.db.Query(ctx, `
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

// GetDocument returns one document and its immutable version history.
func (s *PostgresCatalogStore) GetDocument(ctx context.Context, identityID, documentID string) (DocumentDetail, error) {
	pgDocumentID, err := pgUUID("document_id", documentID)
	if err != nil {
		return DocumentDetail{}, err
	}
	pgIdentityID, err := pgUUID("identity_id", identityID)
	if err != nil {
		return DocumentDetail{}, err
	}
	row, err := s.q.GetDocument(ctx, sqlc.GetDocumentParams{
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
	versionRows, err := s.q.ListDocumentVersions(ctx, pgDocumentID)
	if err != nil {
		return DocumentDetail{}, err
	}
	versions := make([]DocumentVersion, 0, len(versionRows))
	for _, versionRow := range versionRows {
		versions = append(versions, catalogVersionFromSQL(versionRow))
	}
	return DocumentDetail{Document: doc, Versions: versions}, nil
}

// ErrDocumentNotCatalogued reports that a search document has no row in aura.documents.
// It is the normal state of a CLI-ingested document: the catalog is written only by the
// asset upload chain, so `aura docs ingest` produces graph chunks and a job ledger row and
// nothing the cockpit's document list can see.
var ErrDocumentNotCatalogued = errors.New("documents: search document is not in the catalog")

// SetSearchDocumentStatus corrects one catalogued document's status without touching
// anything else, and without an identity: a background worker knows which document it
// failed on, not who owns it. UpdateDocument cannot serve this — it needs identity_id and
// rewrites every column, so a narrow correction would have to invent scope, title and tags
// to make it.
//
// The argument is the SEARCH document id (doc_<hex>, derived from file content), which is
// the only id an embedding worker holds. It is a different namespace from the catalog's
// uuid primary key, and passing one where the other was expected is how the "a dead
// embedding never stays ready" guarantee silently never fired: uuid.Parse rejected the
// string, the worker swallowed the error into a WARN, and the catalog kept saying ready.
// The mapping lives in metadata->>'search_document_id', written by
// CatalogService.RecordAssetVersion and read the same way by the ownership backfill.
func (s *PostgresCatalogStore) SetSearchDocumentStatus(ctx context.Context, searchDocumentID string, status DocumentStatus, reason string) error {
	pgDocumentID, err := s.catalogIDForSearchDocument(ctx, searchDocumentID)
	if err != nil {
		return err
	}
	if _, err := s.q.SetDocumentStatus(ctx, sqlc.SetDocumentStatusParams{
		ID:     pgDocumentID,
		Status: string(status),
		Reason: reason,
	}); err != nil {
		return fmt.Errorf("set document status: %w", err)
	}
	return nil
}

// CatalogIDForSearchDocument resolves the catalog uuid behind a `doc_<hex>` search id.
// Exported for OpenService, which must reach the original asset starting from the only id
// a retrieval hit carries; it is the same lookup SetSearchDocumentStatus does, and holds no
// identity gate of its own — the caller applies one (GetDocument) before using the result.
func (s *PostgresCatalogStore) CatalogIDForSearchDocument(ctx context.Context, searchDocumentID string) (string, error) {
	id, err := s.catalogIDForSearchDocument(ctx, searchDocumentID)
	if err != nil {
		return "", err
	}
	return uuidString(id), nil
}

// catalogIDForSearchDocument resolves the catalog uuid a search document was recorded
// under. Newest first: a re-uploaded file keeps its content-derived search id, so the same
// string can name several catalog rows over time and only the live one may be corrected.
func (s *PostgresCatalogStore) catalogIDForSearchDocument(ctx context.Context, searchDocumentID string) (pgtype.UUID, error) {
	if strings.TrimSpace(searchDocumentID) == "" {
		return pgtype.UUID{}, fmt.Errorf("search_document_id is empty")
	}
	if s.db == nil {
		return pgtype.UUID{}, fmt.Errorf("catalog store has no database handle")
	}
	var id string
	err := s.db.QueryRow(ctx, `
SELECT id::text
FROM aura.documents
WHERE metadata->>'search_document_id' = $1
  AND deleted_at IS NULL
ORDER BY created_at DESC
LIMIT 1`, searchDocumentID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, fmt.Errorf("%w: %s", ErrDocumentNotCatalogued, searchDocumentID)
	}
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("resolve catalog document for %s: %w", searchDocumentID, err)
	}
	return pgUUID("document_id", id)
}

// SoftDeleteDocument marks one logical document deleted for an identity.
func (s *PostgresCatalogStore) SoftDeleteDocument(ctx context.Context, identityID, documentID string) (Document, error) {
	pgDocumentID, err := pgUUID("document_id", documentID)
	if err != nil {
		return Document{}, err
	}
	pgIdentityID, err := pgUUID("identity_id", identityID)
	if err != nil {
		return Document{}, err
	}
	row, err := s.q.SoftDeleteDocument(ctx, sqlc.SoftDeleteDocumentParams{
		ID:         pgDocumentID,
		IdentityID: pgIdentityID,
	})
	if err != nil {
		return Document{}, err
	}
	doc, err := catalogDocumentFromSQL(row)
	if err != nil {
		return Document{}, err
	}
	if err := s.softDeleteDocumentAssets(ctx, pgIdentityID, pgDocumentID); err != nil {
		return doc, err
	}
	return doc, nil
}

// ListStorageObjects returns ledgered object refs for orphan detection.
func (s *PostgresCatalogStore) ListStorageObjects(ctx context.Context, bucket, prefix string) ([]objectstore.ObjectRef, error) {
	rows, err := s.q.ListStorageObjects(ctx, sqlc.ListStorageObjectsParams{
		Bucket: bucket,
		Prefix: prefix,
	})
	if err != nil {
		return nil, err
	}
	out := make([]objectstore.ObjectRef, 0, len(rows))
	for _, row := range rows {
		out = append(out, objectstore.ObjectRef{Bucket: row.Bucket, Key: row.ObjectKey})
	}
	return out, nil
}

// RecordAssetVersion records a logical document, raw storage object, first version, and active-version link.
func (s *PostgresCatalogStore) replaceDocumentTags(ctx context.Context, documentID, actorIdentityID string, tags []string) error {
	pgDocumentID, err := pgUUID("document_id", documentID)
	if err != nil {
		return err
	}
	pgActorID, err := pgOptionalUUID("created_by", actorIdentityID)
	if err != nil {
		return err
	}
	if err := s.q.DeleteDocumentTags(ctx, pgDocumentID); err != nil {
		return err
	}
	for _, tag := range tags {
		if err := s.q.UpsertDocumentTag(ctx, sqlc.UpsertDocumentTagParams{
			DocumentID: pgDocumentID,
			Tag:        tag,
			CreatedBy:  pgActorID,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *PostgresCatalogStore) softDeleteDocumentAssets(ctx context.Context, identityID, documentID pgtype.UUID) error {
	if s.db == nil {
		return nil
	}
	_, err := s.db.Exec(ctx, `
UPDATE aura.assets
SET status = 'deleted',
    deleted_at = COALESCE(deleted_at, now()),
    updated_at = now()
WHERE identity_id = $1
  AND deleted_at IS NULL
  AND id IN (
    SELECT asset_id
    FROM aura.document_versions
    WHERE document_id = $2
      AND asset_id IS NOT NULL
  )`, identityID, documentID)
	return err
}

func catalogDocumentFromSQL(row sqlc.AuraDocuments) (Document, error) {
	metadata, err := catalogMetadataFromJSON(row.Metadata)
	if err != nil {
		return Document{}, err
	}
	return Document{
		ID:              uuidString(row.ID),
		IdentityID:      uuidString(row.IdentityID),
		Scope:           DocumentScope(row.Scope),
		Title:           row.Title,
		Tags:            catalogTagsArray(row.Tags),
		Metadata:        metadata,
		ActiveVersionID: uuidString(row.ActiveVersionID),
		Status:          DocumentStatus(row.Status),
		CreatedAt:       timeValue(row.CreatedAt),
		UpdatedAt:       timeValue(row.UpdatedAt),
		DeletedAt:       timeValue(row.DeletedAt),
	}, nil
}

func catalogVersionFromSQL(row sqlc.AuraDocumentVersions) DocumentVersion {
	return DocumentVersion{
		ID:                 uuidString(row.ID),
		DocumentID:         uuidString(row.DocumentID),
		AssetID:            uuidString(row.AssetID),
		VersionNumber:      int(row.VersionNumber),
		Status:             row.Status,
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
