package documents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/chetto1983/aura/internal/objectstore"
)

func loadPipelineArtifact(
	ctx context.Context,
	objects objectstore.Store,
	bucket string,
	record PipelineStageRecord,
	out any,
) error {
	if objects == nil || strings.TrimSpace(bucket) == "" || out == nil {
		return fmt.Errorf("document pipeline: cached artifact reader is not configured")
	}
	if strings.TrimSpace(record.ArtifactStorageObjectID) == "" ||
		strings.TrimSpace(record.ArtifactObjectKey) == "" ||
		record.ArtifactSizeBytes <= 0 || !isSHA256(record.ArtifactSHA256) {
		return fmt.Errorf("document pipeline: cached artifact metadata is incomplete")
	}
	body, attrs, err := objects.Get(ctx, objectstore.ObjectRef{
		Bucket: bucket,
		Key:    record.ArtifactObjectKey,
	})
	if err != nil {
		return fmt.Errorf("document pipeline: read cached artifact: %w", err)
	}
	defer func() { _ = body.Close() }()
	if attrs.SizeBytes > 0 && attrs.SizeBytes != record.ArtifactSizeBytes {
		return fmt.Errorf("document pipeline: cached artifact size does not match metadata")
	}
	raw, err := io.ReadAll(io.LimitReader(body, record.ArtifactSizeBytes+1))
	if err != nil {
		return fmt.Errorf("document pipeline: read cached artifact body: %w", err)
	}
	if int64(len(raw)) != record.ArtifactSizeBytes {
		return fmt.Errorf("document pipeline: cached artifact body has an invalid size")
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != record.ArtifactSHA256 {
		return fmt.Errorf("document pipeline: cached artifact digest does not match metadata")
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("document pipeline: decode cached artifact: %w", err)
	}
	return nil
}

func isSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// discardUnledgeredArtifact removes an uploaded artifact only when PostgreSQL proves
// no live ledger row in this identity references it. A returned commit error does not
// prove the ledger insert failed, and derived keys are content addressed and therefore
// shareable, so an unconditional delete can strip an artifact another version still
// cites. When ownership cannot be established the object is deliberately left behind
// for reconciliation: an orphan object is recoverable, a deleted live one is not.
func discardUnledgeredArtifact(
	ctx context.Context,
	persistence PipelinePersistence,
	objects objectstore.Store,
	query ArtifactLedgerQuery,
) error {
	owners, err := persistence.CountArtifactLedgerOwners(ctx, ArtifactLedgerQuery{
		IdentityID: query.IdentityID, Bucket: query.Bucket, ObjectKey: query.ObjectKey,
	})
	if err != nil {
		return fmt.Errorf("%w: %w", ErrPipelineArtifactOwnershipUnproven, err)
	}
	if owners > 0 {
		return fmt.Errorf("%w: %d live ledger row(s) reference %s",
			ErrPipelineArtifactOwnershipUnproven, owners, query.ObjectKey)
	}
	return deletePipelineArtifact(ctx, objects,
		objectstore.ObjectRef{Bucket: query.Bucket, Key: query.ObjectKey})
}

func deletePipelineArtifact(
	ctx context.Context,
	objects objectstore.Store,
	ref objectstore.ObjectRef,
) error {
	if err := objects.Delete(ctx, ref); err != nil {
		return fmt.Errorf("document pipeline: remove untracked artifact: %w", err)
	}
	if _, err := objects.Head(ctx, ref); err == nil {
		return fmt.Errorf("document pipeline: untracked artifact remains readable")
	} else if !objectstore.IsNotFound(err) {
		return fmt.Errorf("document pipeline: verify untracked artifact absence: %w", err)
	}
	return nil
}
