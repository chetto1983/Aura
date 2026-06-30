package assets

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// Store persists multimodal asset lifecycle state in Postgres.
type Store struct {
	q *sqlc.Queries
}

// NewStore builds a Postgres-backed asset store.
func NewStore(db sqlc.DBTX) *Store {
	return &Store{q: sqlc.New(db)}
}

// Create inserts a new presigned asset record.
func (s *Store) Create(ctx context.Context, req CreateRequest) (Asset, error) {
	identityID, err := pgUUID("identity_id", req.IdentityID)
	if err != nil {
		return Asset{}, err
	}
	metadata, err := metadataJSON(req.Metadata)
	if err != nil {
		return Asset{}, err
	}
	row, err := s.q.CreateAsset(ctx, sqlc.CreateAssetParams{
		IdentityID:        identityID,
		SourceKind:        string(req.SourceKind),
		SourceRef:         req.SourceRef,
		ThreadID:          req.ThreadID,
		Scope:             string(req.Scope),
		Modality:          string(req.Modality),
		Status:            string(StatusPresigned),
		FileName:          req.FileName,
		MimeType:          req.MIMEType,
		DeclaredSizeBytes: req.DeclaredSizeBytes,
		ObjectBucket:      req.ObjectBucket,
		ObjectKey:         req.ObjectKey,
		Metadata:          metadata,
	})
	if err != nil {
		return Asset{}, err
	}
	return assetFromSQL(row)
}

// GetForIdentity returns a non-deleted asset by asset and identity id.
func (s *Store) GetForIdentity(ctx context.Context, id, identityID string) (Asset, error) {
	pgID, err := pgUUID("asset id", id)
	if err != nil {
		return Asset{}, err
	}
	pgIdentityID, err := pgUUID("identity_id", identityID)
	if err != nil {
		return Asset{}, err
	}
	row, err := s.q.GetAssetForIdentity(ctx, sqlc.GetAssetForIdentityParams{
		ID:         pgID,
		IdentityID: pgIdentityID,
	})
	if err != nil {
		return Asset{}, err
	}
	return assetFromSQL(row)
}

// ListForThread returns non-deleted assets in thread order.
func (s *Store) ListForThread(ctx context.Context, identityID, threadID string) ([]Asset, error) {
	pgIdentityID, err := pgUUID("identity_id", identityID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListAssetsForThread(ctx, sqlc.ListAssetsForThreadParams{
		IdentityID: pgIdentityID,
		ThreadID:   threadID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]Asset, 0, len(rows))
	for _, row := range rows {
		asset, err := assetFromSQL(row)
		if err != nil {
			return nil, err
		}
		out = append(out, asset)
	}
	return out, nil
}

// ListForLibrary returns recent non-deleted library assets for one identity.
func (s *Store) ListForLibrary(ctx context.Context, identityID string, limit int) ([]Asset, error) {
	pgIdentityID, err := pgUUID("identity_id", identityID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 30
	}
	rows, err := s.q.ListAssetsForLibrary(ctx, sqlc.ListAssetsForLibraryParams{
		IdentityID: pgIdentityID,
		Limit:      int32(limit), //nolint:gosec // caller caps this at a small catalog limit.
	})
	if err != nil {
		return nil, err
	}
	out := make([]Asset, 0, len(rows))
	for _, row := range rows {
		asset, err := assetFromSQL(row)
		if err != nil {
			return nil, err
		}
		out = append(out, asset)
	}
	return out, nil
}

// MarkUploaded records object storage upload completion.
func (s *Store) MarkUploaded(ctx context.Context, id, identityID string, size int64, etag string) (Asset, error) {
	pgID, err := pgUUID("asset id", id)
	if err != nil {
		return Asset{}, err
	}
	pgIdentityID, err := pgUUID("identity_id", identityID)
	if err != nil {
		return Asset{}, err
	}
	row, err := s.q.UpdateAssetUploaded(ctx, sqlc.UpdateAssetUploadedParams{
		ID:         pgID,
		IdentityID: pgIdentityID,
		SizeBytes:  size,
		ObjectEtag: etag,
	})
	if err != nil {
		return Asset{}, err
	}
	return assetFromSQL(row)
}

// MarkAccepted records validation of an uploaded asset.
func (s *Store) MarkAccepted(ctx context.Context, id, identityID string, size int64, hash, mimeType string) (Asset, error) {
	pgID, err := pgUUID("asset id", id)
	if err != nil {
		return Asset{}, err
	}
	pgIdentityID, err := pgUUID("identity_id", identityID)
	if err != nil {
		return Asset{}, err
	}
	row, err := s.q.UpdateAssetAccepted(ctx, sqlc.UpdateAssetAcceptedParams{
		ID:          pgID,
		IdentityID:  pgIdentityID,
		SizeBytes:   size,
		ContentHash: hash,
		MimeType:    mimeType,
	})
	if err != nil {
		return Asset{}, err
	}
	return assetFromSQL(row)
}

// SetStatus records a lifecycle state transition and optional error fields.
func (s *Store) SetStatus(ctx context.Context, id, identityID string, status Status, code, message string) (Asset, error) {
	pgID, err := pgUUID("asset id", id)
	if err != nil {
		return Asset{}, err
	}
	pgIdentityID, err := pgUUID("identity_id", identityID)
	if err != nil {
		return Asset{}, err
	}
	row, err := s.q.UpdateAssetStatus(ctx, sqlc.UpdateAssetStatusParams{
		ID:           pgID,
		IdentityID:   pgIdentityID,
		Status:       string(status),
		ErrorCode:    code,
		ErrorMessage: message,
	})
	if err != nil {
		return Asset{}, err
	}
	return assetFromSQL(row)
}

// SetResult records processing output and clears prior error fields.
func (s *Store) SetResult(ctx context.Context, id, identityID string, result Result) (Asset, error) {
	pgID, err := pgUUID("asset id", id)
	if err != nil {
		return Asset{}, err
	}
	pgIdentityID, err := pgUUID("identity_id", identityID)
	if err != nil {
		return Asset{}, err
	}
	metadata, err := metadataJSON(result.Metadata)
	if err != nil {
		return Asset{}, err
	}
	row, err := s.q.UpdateAssetResult(ctx, sqlc.UpdateAssetResultParams{
		ID:         pgID,
		IdentityID: pgIdentityID,
		Status:     string(result.Status),
		DocumentID: result.DocumentID,
		Summary:    result.Summary,
		Metadata:   metadata,
	})
	if err != nil {
		return Asset{}, err
	}
	return assetFromSQL(row)
}

// Promote moves an asset into the operator library scope.
func (s *Store) Promote(ctx context.Context, id, identityID string) (Asset, error) {
	pgID, err := pgUUID("asset id", id)
	if err != nil {
		return Asset{}, err
	}
	pgIdentityID, err := pgUUID("identity_id", identityID)
	if err != nil {
		return Asset{}, err
	}
	row, err := s.q.PromoteAssetToLibrary(ctx, sqlc.PromoteAssetToLibraryParams{
		ID:         pgID,
		IdentityID: pgIdentityID,
	})
	if err != nil {
		return Asset{}, err
	}
	return assetFromSQL(row)
}

// Delete soft-deletes an asset.
func (s *Store) Delete(ctx context.Context, id, identityID string) (Asset, error) {
	pgID, err := pgUUID("asset id", id)
	if err != nil {
		return Asset{}, err
	}
	pgIdentityID, err := pgUUID("identity_id", identityID)
	if err != nil {
		return Asset{}, err
	}
	row, err := s.q.SoftDeleteAsset(ctx, sqlc.SoftDeleteAssetParams{
		ID:         pgID,
		IdentityID: pgIdentityID,
	})
	if err != nil {
		return Asset{}, err
	}
	return assetFromSQL(row)
}

func assetFromSQL(row sqlc.AuraAssets) (Asset, error) {
	metadata, err := metadataFromJSON(row.Metadata)
	if err != nil {
		return Asset{}, err
	}
	return Asset{
		ID:                uuidString(row.ID),
		IdentityID:        uuidString(row.IdentityID),
		SourceKind:        SourceKind(row.SourceKind),
		SourceRef:         row.SourceRef,
		ThreadID:          row.ThreadID,
		Scope:             Scope(row.Scope),
		Modality:          Modality(row.Modality),
		Status:            Status(row.Status),
		FileName:          row.FileName,
		MIMEType:          row.MimeType,
		DeclaredSizeBytes: row.DeclaredSizeBytes,
		SizeBytes:         row.SizeBytes,
		ContentHash:       row.ContentHash,
		ObjectBucket:      row.ObjectBucket,
		ObjectKey:         row.ObjectKey,
		ObjectETag:        row.ObjectEtag,
		DocumentID:        row.DocumentID,
		Summary:           row.Summary,
		Metadata:          metadata,
		ErrorCode:         row.ErrorCode,
		ErrorMessage:      row.ErrorMessage,
		CreatedAt:         timeValue(row.CreatedAt),
		UploadedAt:        timeValue(row.UploadedAt),
		AcceptedAt:        timeValue(row.AcceptedAt),
		ProcessedAt:       timeValue(row.ProcessedAt),
		SearchableAt:      timeValue(row.SearchableAt),
		CompletedAt:       timeValue(row.CompletedAt),
		DeletedAt:         timeValue(row.DeletedAt),
		UpdatedAt:         timeValue(row.UpdatedAt),
	}, nil
}

func metadataJSON(metadata map[string]any) ([]byte, error) {
	if metadata == nil {
		metadata = map[string]any{}
	}
	out, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("asset metadata: %w", err)
	}
	return out, nil
}

func metadataFromJSON(data []byte) (map[string]any, error) {
	if len(data) == 0 {
		return map[string]any{}, nil
	}
	var metadata map[string]any
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("asset metadata: %w", err)
	}
	if metadata == nil {
		return map[string]any{}, nil
	}
	return metadata, nil
}

func pgUUID(field, value string) (pgtype.UUID, error) {
	u, err := uuid.Parse(value)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("invalid %s %q: %w", field, value, err)
	}
	return pgtype.UUID{Bytes: u, Valid: true}, nil
}

func uuidString(value pgtype.UUID) string {
	if !value.Valid {
		return ""
	}
	return uuid.UUID(value.Bytes).String()
}

func timeValue(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time
}
