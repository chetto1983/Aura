package documents

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The candidate-version path the catalog still owns after the pipeline was deleted.
//
// These declarations lived in pipeline_store_types.go and pipeline_store.go, whose only
// surviving caller is catalogTx.recordAssetVersion -- so they moved here rather than
// keeping a pipeline file alive for four symbols. They go with the catalog itself when
// the two-store design lands; nothing new should be built on them.

// ErrPipelineCandidateRejected means the version drifted beneath the write: the
// generation moved on, or the document entered deletion. It is never retried in
// place, because the candidate was built against state that no longer exists.
var ErrPipelineCandidateRejected = errors.New("documents: pipeline candidate is stale or incoherent")

// CandidateVersionRequest reserves an immutable version for stored bytes.
type CandidateVersionRequest struct {
	IdentityID         string
	DocumentID         string
	AssetID            string
	ObjectBucket       string
	ObjectKey          string
	ObjectETag         string
	RetentionClass     string
	SHA1               string
	SHA256             string
	ContentType        string
	SizeBytes          int64
	Status             string
	ChunkingConfigHash string
	PipelineConfigHash string
}

func candidateVersionParams(req CandidateVersionRequest) (sqlc.ReservePipelineCandidateVersionParams, error) {
	identityID, err := pgUUID("candidate identity id", strings.TrimSpace(req.IdentityID))
	if err != nil {
		return sqlc.ReservePipelineCandidateVersionParams{}, err
	}
	documentID, err := pgUUID("candidate document id", strings.TrimSpace(req.DocumentID))
	if err != nil {
		return sqlc.ReservePipelineCandidateVersionParams{}, err
	}
	assetID, err := pgUUID("candidate asset id", strings.TrimSpace(req.AssetID))
	if err != nil {
		return sqlc.ReservePipelineCandidateVersionParams{}, err
	}
	bucket, objectKey := strings.TrimSpace(req.ObjectBucket), strings.TrimSpace(req.ObjectKey)
	if bucket == "" || objectKey == "" {
		return sqlc.ReservePipelineCandidateVersionParams{},
			fmt.Errorf("candidate object bucket and key are required")
	}
	sha256Hex, err := canonicalSHA256(req.SHA256)
	if err != nil {
		return sqlc.ReservePipelineCandidateVersionParams{}, fmt.Errorf("candidate sha256: %w", err)
	}
	versionID, err := DeterministicVersionID(req.DocumentID, sha256Hex)
	if err != nil {
		return sqlc.ReservePipelineCandidateVersionParams{}, err
	}
	pgVersionID, err := pgUUID("candidate version id", versionID)
	if err != nil {
		return sqlc.ReservePipelineCandidateVersionParams{}, err
	}
	if req.SizeBytes < 0 {
		return sqlc.ReservePipelineCandidateVersionParams{}, fmt.Errorf("candidate size must be non-negative")
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = "queued"
	}
	contentType := strings.TrimSpace(req.ContentType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	retentionClass := strings.TrimSpace(req.RetentionClass)
	if retentionClass == "" {
		retentionClass = defaultRetentionClass
	}
	return sqlc.ReservePipelineCandidateVersionParams{
		DocumentID: documentID, IdentityID: identityID, Sha256: sha256Hex,
		AssetID: assetID, ID: pgVersionID, Bucket: bucket, ObjectKey: objectKey,
		Etag: strings.TrimSpace(req.ObjectETag), RetentionClass: retentionClass,
		Status: status, Sha1: strings.TrimSpace(req.SHA1), ContentType: contentType,
		SizeBytes: req.SizeBytes, ChunkingConfigHash: strings.TrimSpace(req.ChunkingConfigHash),
		PipelineConfigHash: strings.TrimSpace(req.PipelineConfigHash),
	}, nil
}

func candidateRejectedError(action string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, ErrPipelineCandidateRejected) {
		return fmt.Errorf("%s: %w", action, ErrPipelineCandidateRejected)
	}
	return fmt.Errorf("%s: %w", action, err)
}

// DeterministicVersionID derives an RFC-4122 variant, version-8 UUID from the
// logical document UUID and canonical raw SHA-256. The same bytes replay to the
// same version across workers and processes.
func DeterministicVersionID(documentID, rawSHA256 string) (string, error) {
	if _, err := uuid.Parse(strings.TrimSpace(documentID)); err != nil {
		return "", fmt.Errorf("document id: %w", err)
	}
	rawSHA256, err := canonicalSHA256(rawSHA256)
	if err != nil {
		return "", fmt.Errorf("raw sha256: %w", err)
	}
	return deterministicUUID("aura.document.version.v1", documentID, rawSHA256), nil
}

func deterministicUUID(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(part))
	}
	var id uuid.UUID
	copy(id[:], h.Sum(nil)[:16])
	id[6] = (id[6] & 0x0f) | 0x80
	id[8] = (id[8] & 0x3f) | 0x80
	return id.String()
}

func canonicalSHA256(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return "", fmt.Errorf("must be 64 hexadecimal characters")
	}
	return value, nil
}
