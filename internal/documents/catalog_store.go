package documents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/jackc/pgx/v5/pgtype"
)

// PostgresCatalogStore persists document catalog metadata through sqlc.
type PostgresCatalogStore struct {
	q *sqlc.Queries
}

var _ CatalogStore = (*PostgresCatalogStore)(nil)

// NewPostgresCatalogStore builds a Postgres-backed document catalog store.
func NewPostgresCatalogStore(db sqlc.DBTX) *PostgresCatalogStore {
	return &PostgresCatalogStore{q: sqlc.New(db)}
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
		Tags:       req.Tags,
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
		Tags:            req.Tags,
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
	return out, nil
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
	return catalogDocumentFromSQL(row)
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

// RecordAssetVersion creates a logical document, raw storage object, first version, and active-version link.
func (s *PostgresCatalogStore) RecordAssetVersion(ctx context.Context, req RecordAssetVersionRequest) (DocumentVersionRecord, error) {
	doc, err := s.CreateDocument(ctx, CreateDocumentRequest{
		IdentityID: req.IdentityID,
		Scope:      req.Scope,
		Title:      req.Title,
		Metadata:   req.Metadata,
		Status:     req.DocumentStatus,
	})
	if err != nil {
		return DocumentVersionRecord{}, err
	}
	pgIdentityID, err := pgUUID("identity_id", req.IdentityID)
	if err != nil {
		return DocumentVersionRecord{}, err
	}
	pgDocumentID, err := pgUUID("document_id", doc.ID)
	if err != nil {
		return DocumentVersionRecord{}, err
	}
	pgAssetID, err := pgOptionalUUID("asset_id", req.AssetID)
	if err != nil {
		return DocumentVersionRecord{}, err
	}
	storageRow, err := s.q.CreateStorageObject(ctx, sqlc.CreateStorageObjectParams{
		IdentityID:     pgIdentityID,
		DocumentID:     pgDocumentID,
		AssetID:        pgAssetID,
		Bucket:         req.ObjectBucket,
		ObjectKey:      req.ObjectKey,
		Kind:           req.StorageKind,
		Sha1:           req.SHA1,
		Sha256:         req.SHA256,
		Etag:           req.ObjectETag,
		SizeBytes:      req.SizeBytes,
		ContentType:    req.MIMEType,
		RetentionClass: req.RetentionClass,
	})
	if err != nil {
		return DocumentVersionRecord{}, err
	}
	versionRow, err := s.q.CreateDocumentVersion(ctx, sqlc.CreateDocumentVersionParams{
		DocumentID:         pgDocumentID,
		AssetID:            pgAssetID,
		VersionNumber:      int32(req.VersionNumber), //nolint:gosec // normalized positive and starts at 1.
		Status:             req.VersionStatus,
		Sha1:               req.SHA1,
		Sha256:             req.SHA256,
		ContentType:        req.MIMEType,
		SizeBytes:          req.SizeBytes,
		StorageObjectID:    storageRow.ID,
		ChunkingConfigHash: req.ChunkingConfigHash,
		PipelineConfigHash: req.PipelineConfigHash,
	})
	if err != nil {
		return DocumentVersionRecord{}, err
	}
	if _, err := s.q.UpdateStorageObjectVersion(ctx, sqlc.UpdateStorageObjectVersionParams{
		ID:        storageRow.ID,
		VersionID: versionRow.ID,
	}); err != nil {
		return DocumentVersionRecord{}, err
	}
	version := catalogVersionFromSQL(versionRow)
	doc, err = s.UpdateDocument(ctx, UpdateDocumentRequest{
		IdentityID:      req.IdentityID,
		DocumentID:      doc.ID,
		Scope:           doc.Scope,
		Title:           doc.Title,
		Tags:            doc.Tags,
		Metadata:        doc.Metadata,
		ActiveVersionID: version.ID,
		Status:          req.DocumentStatus,
	})
	if err != nil {
		return DocumentVersionRecord{}, err
	}
	return DocumentVersionRecord{Document: doc, Version: version}, nil
}

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
		Tags:            append([]string(nil), row.Tags...),
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
