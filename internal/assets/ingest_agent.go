package assets

import (
	"context"
	"fmt"
	"io"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/objectstore"
)

// objectIngest is the per-source input to the shared ingest orchestration behind
// IngestTelegramFile and IngestAgentFile. The source-specific deltas ride as fields so neither
// caller duplicates the Create -> Put -> MarkAccepted sequence:
//
//   - enforceLimits gates s.Limits.Validate (pre- and post-Put). Telegram enforces the
//     per-modality cap; agent deliveries (D-04) do NOT — send_file's 50 MiB gate is the single
//     authoritative delivery ceiling, and a second per-modality rejection here would half-deliver.
//   - process gates s.processAsset (embeddings / doc extraction / knowledge-graph indexing).
//     Telegram processes; agent deliveries (D-03) do NOT — a produced deliverable is display +
//     ephemeral and must never silently become searchable memory.
type objectIngest struct {
	identityID    string
	threadID      string
	sourceKind    SourceKind
	sourceRef     string
	toolCallID    string
	fileName      string
	mimeType      string
	modality      Modality
	sizeBytes     int64
	reader        io.Reader
	scope         Scope
	metadata      map[string]any
	enforceLimits bool
	process       bool
}

// ingestObject stores one media stream under the identity's per-identity object key and drives the
// asset lifecycle to Accepted (then, when process is set, through the processor). It is the shared
// body of IngestTelegramFile and IngestAgentFile; the per-source deltas ride on objectIngest.
func (s *Service) ingestObject(ctx context.Context, in objectIngest) (Asset, error) {
	if s.Store == nil || s.Objects == nil {
		return Asset{}, fmt.Errorf("asset service is not configured")
	}
	if in.reader == nil {
		return Asset{}, fmt.Errorf("asset reader is nil")
	}
	name := cleanFileName(in.fileName)
	mimeType := normalizeMIME(in.mimeType, name)
	modality := in.modality
	if modality == "" || modality == ModalityUnknown {
		modality = InferModality(name, mimeType)
	}
	if in.enforceLimits {
		if err := s.Limits.Validate(modality, name, in.sizeBytes); err != nil {
			return Asset{}, err
		}
	}
	scope := in.scope
	if scope == "" {
		scope = ScopeThread
	}
	if scope != ScopeThread && scope != ScopeLibrary {
		return Asset{}, fmt.Errorf("unsupported asset scope %q", scope)
	}
	objects, bucket, err := s.objectsFor(identityctx.WithIdentityID(ctx, in.identityID))
	if err != nil {
		return Asset{}, err
	}
	place := objectstore.PlaceAsset(objectAssetID(scope, in.identityID, in.sourceRef, name), name)
	asset, err := s.Store.Create(ctx, CreateRequest{
		IdentityID:        in.identityID,
		ThreadID:          in.threadID,
		SourceKind:        in.sourceKind,
		SourceRef:         in.sourceRef,
		ToolCallID:        in.toolCallID,
		Scope:             scope,
		Modality:          modality,
		FileName:          name,
		MIMEType:          mimeType,
		DeclaredSizeBytes: in.sizeBytes,
		ObjectBucket:      bucket,
		ObjectKey:         place.Key,
		Metadata:          in.metadata,
	})
	if err != nil {
		asset, err = s.reingestTarget(ctx, in.identityID, place.Key, err)
		if err != nil {
			return Asset{}, err
		}
	}
	// A stable source_ref can return an existing row after a retry. Accepted is
	// already complete; earlier states resume against the row's persisted object
	// placement, never the throwaway asset id generated for this Create attempt.
	if in.sourceRef != "" && asset.Status == StatusAccepted {
		return asset, nil
	}
	place = objectstore.PlaceAsset(asset.ID, asset.FileName)
	ref := objectstore.ObjectRef{Bucket: asset.ObjectBucket, Key: asset.ObjectKey}
	attrs, err := objects.Put(ctx, ref, in.reader,
		objectstore.PutOptions{MIMEType: mimeType, Size: in.sizeBytes, Metadata: place.Metadata})
	if err != nil {
		_, _ = s.Store.SetStatus(ctx, asset.ID, in.identityID, StatusFailed, "object_put_failed", err.Error())
		return Asset{}, err
	}
	if in.enforceLimits {
		if err = s.Limits.Validate(modality, name, attrs.SizeBytes); err != nil {
			updated, _ := s.Store.SetStatus(ctx, asset.ID, in.identityID, StatusRefused, "asset_refused", err.Error())
			_ = objects.Delete(context.WithoutCancel(ctx), ref)
			return updated, err
		}
	}
	asset, err = s.Store.MarkUploaded(ctx, asset.ID, in.identityID, attrs.SizeBytes, attrs.ETag)
	if err != nil {
		return Asset{}, err
	}
	hash, sniffed, err := s.hashAndSniff(ctx, objects, ref, asset.FileName)
	if err != nil {
		return Asset{}, err
	}
	asset, err = s.Store.MarkAccepted(ctx, asset.ID, in.identityID, attrs.SizeBytes, hash, sniffed)
	if err != nil {
		return Asset{}, err
	}
	if !in.process {
		return asset, nil
	}
	// Documents always cross the durable queue boundary. The source staging file
	// may disappear as soon as this returns because Garage already owns the bytes;
	// conversion never depends on a caller's host or sandbox path.
	if modality == ModalityDocument {
		if err := s.enqueueProcessing(ctx, asset); err != nil {
			updated, _ := s.Store.SetStatus(ctx, asset.ID, in.identityID, StatusFailed, "processing_enqueue_failed", err.Error())
			return updated, err
		}
		return asset, nil
	}
	return s.processAsset(ctx, asset)
}

// IngestAgentFile ingests one agent-produced deliverable (a send_file host file) as an owned,
// thread-scoped asset (WEBART-01). It is the delivery-only mirror of IngestTelegramFile: it does
// NOT enforce the per-modality Limits cap (D-04) and does NOT route the asset through processAsset
// (D-03), so the returned asset is Accepted rather than processed. The caller closes req.Reader.
func (s *Service) IngestAgentFile(ctx context.Context, req AgentIngestRequest) (Asset, error) {
	return s.ingestObject(ctx, objectIngest{
		identityID:    req.IdentityID,
		threadID:      req.ThreadID,
		sourceKind:    SourceAgent,
		sourceRef:     req.SourceRef,
		toolCallID:    req.ToolCallID,
		fileName:      req.FileName,
		mimeType:      req.MIMEType,
		modality:      req.Modality,
		sizeBytes:     req.SizeBytes,
		reader:        req.Reader,
		scope:         ScopeThread,
		enforceLimits: false,
		process:       false,
	})
}

// reingestTarget turns a duplicate-object-key insert into the row that already holds the
// key, so re-ingesting the same object REPLACES what is there instead of failing.
//
// CreateAsset's ON CONFLICT covers (identity_id, source_kind, source_ref) and only for
// source_kind 'agent', but aura.assets also holds (identity_id, object_key) UNIQUE and
// that one binds every route. Measured 2026-09-09 through the documents MCP, whose ingest
// is source_kind 'cli': re-ingesting the same path returned
// "duplicate key value violates unique constraint assets_identity_object_key_idx"
// (SQLSTATE 23505), while document_ingest's own description promises the opposite --
// "Re-ingesting the same path replaces what is there rather than adding a copy."
//
// The conflict target is not simply widened to the object key because the two indexes do
// not cover the same ground: a thread-scoped asset takes a random id, so its object key
// never collides, and moving the target there would silently drop the source_ref dedup
// that route relies on.
//
// Any other failure is returned as it arrived: only the row that already occupies THIS
// key is a re-ingest, and a lookup that cannot find it means the violation was not the
// one assumed.
func (s *Service) reingestTarget(
	ctx context.Context, identityID, objectKey string, createErr error,
) (Asset, error) {
	if !db.IsUniqueViolation(createErr) {
		return Asset{}, createErr
	}
	existing, err := s.Store.ByObjectKey(ctx, identityID, objectKey)
	if err != nil {
		return Asset{}, createErr
	}
	return existing, nil
}
