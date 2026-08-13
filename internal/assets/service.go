//nolint:revive // Internal asset service API is exported for AG-UI and CLI wiring.
package assets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/google/uuid"
)

type StoreBackend interface {
	Create(context.Context, CreateRequest) (Asset, error)
	GetForIdentity(context.Context, string, string) (Asset, error)
	ListForThread(context.Context, string, string) ([]Asset, error)
	ListForLibrary(context.Context, string, int) ([]Asset, error)
	MarkUploaded(context.Context, string, string, int64, string) (Asset, error)
	MarkAccepted(context.Context, string, string, int64, string, string) (Asset, error)
	SetStatus(context.Context, string, string, Status, string, string) (Asset, error)
	SetResult(context.Context, string, string, Result) (Asset, error)
	Promote(context.Context, string, string) (Asset, error)
	Delete(context.Context, string, string) (Asset, error)
}

type ProcessingJobQueue interface {
	EnqueueAssetProcessing(context.Context, Asset) error
}

var _ StoreBackend = (*Store)(nil)

type Service struct {
	Store   StoreBackend
	Objects objectstore.Store
	// IdentityObjects + PerIdentityStore route each object op through the per-identity
	// credential resolver (D-08): an identity's bytes land in ITS OWN bucket under ITS OWN
	// key. Both nil → the shared Objects store handles every op (backward compat).
	IdentityObjects  ObjectResolver
	PerIdentityStore StoreFactory
	Processors       ProcessorSet
	ProcessingJobs   ProcessingJobQueue
	Limits           Limits
	Bucket           string
	PresignTTL       time.Duration
}

// objectsFor resolves the object Store + bucket for the identity carried on ctx (the caller
// stamps the asset owner). A nil resolver/factory → the shared store+bucket.
func (s *Service) objectsFor(ctx context.Context) (objectstore.Store, string, error) {
	return resolveObjects(ctx, s.IdentityObjects, s.PerIdentityStore, s.Objects, s.Bucket)
}

type PresignRequest struct {
	IdentityID        string     `json:"identity_id"`
	SourceKind        SourceKind `json:"source_kind"`
	ThreadID          string     `json:"thread_id"`
	Scope             Scope      `json:"scope"`
	FileName          string     `json:"file_name"`
	MIMEType          string     `json:"mime_type"`
	DeclaredSizeBytes int64      `json:"size_bytes"`
	ModalityHint      Modality   `json:"modality_hint"`
}

type PresignResponse struct {
	Asset  Asset                    `json:"asset"`
	Upload objectstore.PresignedPut `json:"upload"`
}

func (s *Service) Presign(ctx context.Context, req PresignRequest) (PresignResponse, error) {
	if s.Store == nil || s.Objects == nil {
		return PresignResponse{}, fmt.Errorf("asset service is not configured")
	}
	name := cleanFileName(req.FileName)
	mimeType := normalizeMIME(req.MIMEType, name)
	modality := req.ModalityHint
	if modality == "" || modality == ModalityUnknown {
		modality = InferModality(name, mimeType)
	}
	if err := s.Limits.Validate(modality, name, req.DeclaredSizeBytes); err != nil {
		return PresignResponse{}, err
	}
	scope := req.Scope
	if scope == "" {
		scope = ScopeThread
	}
	if scope != ScopeThread && scope != ScopeLibrary {
		return PresignResponse{}, fmt.Errorf("unsupported asset scope %q", scope)
	}
	objects, bucket, err := s.objectsFor(identityctx.WithIdentityID(ctx, req.IdentityID))
	if err != nil {
		return PresignResponse{}, err
	}
	assetID := newAssetID()
	place := objectstore.PlaceAsset(assetID, name)
	key := place.Key
	asset, err := s.Store.Create(ctx, CreateRequest{
		IdentityID:        req.IdentityID,
		SourceKind:        req.SourceKind,
		ThreadID:          req.ThreadID,
		Scope:             scope,
		Modality:          modality,
		FileName:          name,
		MIMEType:          mimeType,
		DeclaredSizeBytes: req.DeclaredSizeBytes,
		ObjectBucket:      bucket,
		ObjectKey:         key,
		Metadata:          map[string]any{},
	})
	if err != nil {
		return PresignResponse{}, err
	}
	upload, err := objects.PresignPut(ctx, objectstore.PresignPutRequest{
		Ref:       objectstore.ObjectRef{Bucket: bucket, Key: key},
		MIMEType:  asset.MIMEType,
		Size:      req.DeclaredSizeBytes,
		Metadata:  place.Metadata,
		ExpiresIn: s.ttl(),
	})
	if err != nil {
		return PresignResponse{}, err
	}
	return PresignResponse{Asset: asset, Upload: upload}, nil
}

func (s *Service) Finalize(ctx context.Context, identityID, assetID string) (Asset, error) {
	if s.Store == nil || s.Objects == nil {
		return Asset{}, fmt.Errorf("asset service is not configured")
	}
	asset, err := s.Store.GetForIdentity(ctx, assetID, identityID)
	if err != nil {
		return Asset{}, err
	}
	objects, _, err := s.objectsFor(identityctx.WithIdentityID(ctx, identityID))
	if err != nil {
		return Asset{}, err
	}
	ref := objectstore.ObjectRef{Bucket: asset.ObjectBucket, Key: asset.ObjectKey}
	attrs, err := objects.Head(ctx, ref)
	if err != nil {
		_, _ = s.Store.SetStatus(ctx, asset.ID, identityID, StatusFailed, "object_missing", "uploaded object was not found")
		return Asset{}, err
	}
	if err = s.Limits.Validate(asset.Modality, asset.FileName, attrs.SizeBytes); err != nil {
		updated, _ := s.Store.SetStatus(ctx, asset.ID, identityID, StatusRefused, "asset_refused", err.Error())
		_ = objects.Delete(context.WithoutCancel(ctx), ref)
		return updated, err
	}
	asset, err = s.Store.MarkUploaded(ctx, asset.ID, identityID, attrs.SizeBytes, attrs.ETag)
	if err != nil {
		return Asset{}, err
	}
	hash, sniffed, err := s.hashAndSniff(ctx, objects, ref, asset.FileName)
	if err != nil {
		return Asset{}, err
	}
	asset, err = s.Store.MarkAccepted(ctx, asset.ID, identityID, attrs.SizeBytes, hash, sniffed)
	if err != nil {
		return Asset{}, err
	}
	if err := s.enqueueProcessing(ctx, asset); err != nil {
		updated, _ := s.Store.SetStatus(ctx, asset.ID, identityID, StatusFailed, "processing_enqueue_failed", err.Error())
		return updated, err
	}
	return asset, nil
}

func (s *Service) GetForIdentity(ctx context.Context, id, identityID string) (Asset, error) {
	if s.Store == nil {
		return Asset{}, fmt.Errorf("asset service is not configured")
	}
	return s.Store.GetForIdentity(ctx, id, identityID)
}

// OpenForIdentity resolves the owner-scoped asset (an error → 404 on miss / not-owned upstream,
// D-12) and opens its per-identity object body for streaming. The GetForIdentity ownership gate
// runs BEFORE any object-store read, so a non-owner never reaches the store (T-IDOR). It returns a
// stream-through ReadCloser, never a presigned/direct store URL (D-09); the caller closes it.
func (s *Service) OpenForIdentity(ctx context.Context, id, identityID string) (io.ReadCloser, Asset, error) {
	asset, err := s.GetForIdentity(ctx, id, identityID)
	if err != nil {
		return nil, Asset{}, err
	}
	objects, _, err := s.objectsFor(identityctx.WithIdentityID(ctx, identityID))
	if err != nil {
		return nil, Asset{}, err
	}
	rc, _, err := objects.Get(ctx, objectstore.ObjectRef{Bucket: asset.ObjectBucket, Key: asset.ObjectKey})
	return rc, asset, err
}

func (s *Service) ListForThread(ctx context.Context, identityID, threadID string) ([]Asset, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("asset service is not configured")
	}
	return s.Store.ListForThread(ctx, identityID, threadID)
}

func (s *Service) ListForLibrary(ctx context.Context, identityID string, limit int) ([]Asset, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("asset service is not configured")
	}
	if limit <= 0 || limit > maxCatalogDocs {
		limit = maxCatalogDocs
	}
	return s.Store.ListForLibrary(ctx, identityID, limit)
}

func (s *Service) Promote(ctx context.Context, identityID, assetID string) (Asset, error) {
	if s.Store == nil {
		return Asset{}, fmt.Errorf("asset service is not configured")
	}
	return s.Store.Promote(ctx, assetID, identityID)
}

func (s *Service) Delete(ctx context.Context, identityID, assetID string) (Asset, error) {
	if s.Store == nil {
		return Asset{}, fmt.Errorf("asset service is not configured")
	}
	asset, err := s.Store.Delete(ctx, assetID, identityID)
	if err != nil {
		return Asset{}, err
	}
	if s.Objects != nil && asset.ObjectBucket != "" && asset.ObjectKey != "" {
		// Best-effort object cleanup on the OWNER's resolved store (a resolution fault must
		// not fail the record delete — the row is already gone and the object is orphaned-safe).
		if objects, _, rErr := s.objectsFor(identityctx.WithIdentityID(ctx, identityID)); rErr == nil {
			_ = objects.Delete(context.WithoutCancel(ctx), objectstore.ObjectRef{Bucket: asset.ObjectBucket, Key: asset.ObjectKey})
		}
	}
	return asset, nil
}

func (s *Service) Retry(ctx context.Context, identityID, assetID string) (Asset, error) {
	if s.Store == nil {
		return Asset{}, fmt.Errorf("asset service is not configured")
	}
	asset, err := s.Store.SetStatus(ctx, assetID, identityID, StatusAccepted, "", "")
	if err != nil {
		return Asset{}, err
	}
	if err := s.enqueueProcessing(ctx, asset); err != nil {
		updated, _ := s.Store.SetStatus(ctx, asset.ID, identityID, StatusFailed, "processing_enqueue_failed", err.Error())
		return updated, err
	}
	return asset, nil
}

// ProcessAccepted runs the processor for an already accepted asset.
func (s *Service) ProcessAccepted(ctx context.Context, identityID, assetID string) (Asset, error) {
	if s.Store == nil {
		return Asset{}, fmt.Errorf("asset service is not configured")
	}
	asset, err := s.Store.GetForIdentity(ctx, assetID, identityID)
	if err != nil {
		return Asset{}, err
	}
	switch asset.Status {
	case StatusAccepted, StatusProcessing:
		return s.processAsset(ctx, asset)
	case StatusFailed:
		// A failed handler leaves both the job queued for retry and the asset in
		// failed state. Re-arm the asset only after the durable worker has claimed
		// that retry; otherwise the next attempt would reject itself before the
		// processor can recover.
		asset, err = s.Store.SetStatus(ctx, asset.ID, identityID, StatusAccepted, "", "")
		if err != nil {
			return Asset{}, err
		}
		return s.processAsset(ctx, asset)
	case StatusSearchable, StatusComplete:
		return asset, nil
	default:
		return Asset{}, fmt.Errorf("asset %s is %s, not accepted for processing", asset.ID, asset.Status)
	}
}

// IngestTelegramFile ingests one Telegram media stream as an owned, thread-scoped asset. It
// enforces the per-modality Limits cap and routes the accepted asset through the processing
// pipeline (embeddings / knowledge indexing) — see ingestObject (ingest_agent.go) for the shared
// orchestration and IngestAgentFile for the delivery-only variant that skips both.
func (s *Service) IngestTelegramFile(ctx context.Context, req TelegramIngestRequest) (Asset, error) {
	if s.Store == nil || s.Objects == nil {
		return Asset{}, fmt.Errorf("asset service is not configured")
	}
	if req.Reader == nil {
		return Asset{}, fmt.Errorf("telegram asset reader is nil")
	}
	sourceRef, err := telegramSourceRef(req)
	if err != nil {
		return Asset{}, err
	}
	scope := ScopeThread
	if req.Modality == ModalityDocument || (req.Modality == "" && InferModality(req.FileName, req.MIMEType) == ModalityDocument) {
		scope = ScopeLibrary
	}
	return s.ingestObject(ctx, objectIngest{
		identityID:    req.IdentityID,
		threadID:      req.ThreadID,
		sourceKind:    SourceTelegram,
		sourceRef:     sourceRef,
		fileName:      req.FileName,
		mimeType:      req.MIMEType,
		modality:      req.Modality,
		sizeBytes:     req.SizeBytes,
		reader:        req.Reader,
		scope:         scope,
		enforceLimits: true,
		process:       true,
	})
}

// IngestDocument stores a non-presigned document under its owner's Garage
// credentials and enqueues the accepted asset for durable processing. It is the
// canonical ingress used by CLI and workspace indexing; callers may delete their
// local staging copy after this method returns.
func (s *Service) IngestDocument(ctx context.Context, req DocumentIngestRequest) (Asset, error) {
	if req.Reader == nil {
		return Asset{}, fmt.Errorf("document asset reader is nil")
	}
	if req.SourceKind == "" {
		return Asset{}, fmt.Errorf("document source kind is required")
	}
	metadata := map[string]any{}
	if title := strings.TrimSpace(req.Title); title != "" {
		metadata["title"] = title
	}
	return s.ingestObject(ctx, objectIngest{
		identityID:    req.IdentityID,
		threadID:      req.ThreadID,
		sourceKind:    req.SourceKind,
		sourceRef:     req.SourceRef,
		fileName:      req.FileName,
		mimeType:      req.MIMEType,
		modality:      ModalityDocument,
		sizeBytes:     req.SizeBytes,
		reader:        req.Reader,
		scope:         ScopeLibrary,
		metadata:      metadata,
		enforceLimits: true,
		process:       true,
	})
}

func (s *Service) enqueueProcessing(ctx context.Context, asset Asset) error {
	if s.ProcessingJobs == nil {
		return nil
	}
	return s.ProcessingJobs.EnqueueAssetProcessing(ctx, asset)
}

func (s *Service) processAsset(ctx context.Context, asset Asset) (Asset, error) {
	processor := s.Processors.For(asset.Modality)
	if processor == nil {
		return s.Store.SetStatus(ctx, asset.ID, asset.IdentityID, StatusComplete, "", "")
	}
	processing, err := s.Store.SetStatus(ctx, asset.ID, asset.IdentityID, StatusProcessing, "", "")
	if err != nil {
		return Asset{}, err
	}
	result, err := processor.ProcessAsset(ctx, processing)
	if err != nil {
		failed, setErr := s.Store.SetStatus(ctx, processing.ID, processing.IdentityID, StatusFailed, "processor_failed", err.Error())
		if setErr != nil {
			return Asset{}, setErr
		}
		return failed, err
	}
	if result.Status == "" {
		result.Status = StatusComplete
	}
	return s.Store.SetResult(ctx, processing.ID, processing.IdentityID, result)
}

func telegramSourceRef(req TelegramIngestRequest) (string, error) {
	payload := struct {
		ChatID    int64  `json:"chat_id"`
		MessageID int    `json:"message_id"`
		FileID    string `json:"file_id"`
	}{
		ChatID:    req.ChatID,
		MessageID: req.MessageID,
		FileID:    req.FileID,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (s *Service) hashAndSniff(ctx context.Context, objects objectstore.Store, ref objectstore.ObjectRef, fileName string) (string, string, error) {
	rc, attrs, err := objects.Get(ctx, ref)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = rc.Close() }()
	h := sha256.New()
	if _, err = io.Copy(h, rc); err != nil {
		return "", "", err
	}
	mimeType := attrs.MIMEType
	if mimeType == "" {
		mimeType = normalizeMIME("", fileName)
	}
	return hex.EncodeToString(h.Sum(nil)), mimeType, nil
}

func (s *Service) ttl() time.Duration {
	if s.PresignTTL > 0 {
		return s.PresignTTL
	}
	return 10 * time.Minute
}

func cleanFileName(raw string) string {
	name := strings.TrimSpace(raw)
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(name)
	if name == "." || name == "/" || name == "" {
		return "upload.bin"
	}
	return name
}

func normalizeMIME(raw, fileName string) string {
	if raw = strings.TrimSpace(raw); raw != "" {
		return raw
	}
	if mt := mime.TypeByExtension(strings.ToLower(filepath.Ext(fileName))); mt != "" {
		return mt
	}
	return "application/octet-stream"
}

func newAssetID() string {
	return uuid.NewString()
}
