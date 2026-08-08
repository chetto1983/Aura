package assets

import (
	"context"
	"fmt"
	"io"

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
	assetID := newAssetID()
	key := objectstore.AssetKey(assetID, name)
	asset, err := s.Store.Create(ctx, CreateRequest{
		IdentityID:        in.identityID,
		ThreadID:          in.threadID,
		SourceKind:        in.sourceKind,
		SourceRef:         in.sourceRef,
		Scope:             scope,
		Modality:          modality,
		FileName:          name,
		MIMEType:          mimeType,
		DeclaredSizeBytes: in.sizeBytes,
		ObjectBucket:      bucket,
		ObjectKey:         key,
		Metadata:          in.metadata,
	})
	if err != nil {
		return Asset{}, err
	}
	ref := objectstore.ObjectRef{Bucket: bucket, Key: key}
	attrs, err := objects.Put(ctx, ref, in.reader, objectstore.PutOptions{MIMEType: mimeType, Size: in.sizeBytes})
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
