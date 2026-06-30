package documents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/db/sqlc"
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
