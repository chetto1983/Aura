package assets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/google/uuid"
)

type StoreBackend interface {
	Create(context.Context, CreateRequest) (Asset, error)
	GetForIdentity(context.Context, string, string) (Asset, error)
	ListForThread(context.Context, string, string) ([]Asset, error)
	MarkUploaded(context.Context, string, string, int64, string) (Asset, error)
	MarkAccepted(context.Context, string, string, int64, string, string) (Asset, error)
	SetStatus(context.Context, string, string, Status, string, string) (Asset, error)
	SetResult(context.Context, string, string, Result) (Asset, error)
}

var _ StoreBackend = (*Store)(nil)

type Service struct {
	Store      StoreBackend
	Objects    objectstore.Store
	Processors ProcessorSet
	Limits     Limits
	Bucket     string
	PresignTTL time.Duration
}

type PresignRequest struct {
	IdentityID        string     `json:"identity_id"`
	SourceKind        SourceKind `json:"source_kind"`
	ThreadID          string     `json:"thread_id"`
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
	assetID := newAssetID()
	key := objectstore.AssetKey(req.IdentityID, assetID)
	asset, err := s.Store.Create(ctx, CreateRequest{
		IdentityID:        req.IdentityID,
		SourceKind:        req.SourceKind,
		ThreadID:          req.ThreadID,
		Scope:             ScopeThread,
		Modality:          modality,
		FileName:          name,
		MIMEType:          mimeType,
		DeclaredSizeBytes: req.DeclaredSizeBytes,
		ObjectBucket:      s.Bucket,
		ObjectKey:         key,
		Metadata:          map[string]any{},
	})
	if err != nil {
		return PresignResponse{}, err
	}
	upload, err := s.Objects.PresignPut(ctx, objectstore.PresignPutRequest{
		Ref:       objectstore.ObjectRef{Bucket: s.Bucket, Key: key},
		MIMEType:  asset.MIMEType,
		Size:      req.DeclaredSizeBytes,
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
	ref := objectstore.ObjectRef{Bucket: asset.ObjectBucket, Key: asset.ObjectKey}
	attrs, err := s.Objects.Head(ctx, ref)
	if err != nil {
		_, _ = s.Store.SetStatus(ctx, asset.ID, identityID, StatusFailed, "object_missing", "uploaded object was not found")
		return Asset{}, err
	}
	if err = s.Limits.Validate(asset.Modality, asset.FileName, attrs.SizeBytes); err != nil {
		updated, _ := s.Store.SetStatus(ctx, asset.ID, identityID, StatusRefused, "asset_refused", err.Error())
		_ = s.Objects.Delete(context.WithoutCancel(ctx), ref)
		return updated, err
	}
	asset, err = s.Store.MarkUploaded(ctx, asset.ID, identityID, attrs.SizeBytes, attrs.ETag)
	if err != nil {
		return Asset{}, err
	}
	hash, sniffed, err := s.hashAndSniff(ctx, ref, asset.FileName)
	if err != nil {
		return Asset{}, err
	}
	asset, err = s.Store.MarkAccepted(ctx, asset.ID, identityID, attrs.SizeBytes, hash, sniffed)
	if err != nil {
		return Asset{}, err
	}
	go s.process(context.WithoutCancel(ctx), asset)
	return asset, nil
}

func (s *Service) process(ctx context.Context, asset Asset) {
	processor := s.Processors.For(asset.Modality)
	if processor == nil {
		_, _ = s.Store.SetStatus(ctx, asset.ID, asset.IdentityID, StatusComplete, "", "")
		return
	}
	processing, err := s.Store.SetStatus(ctx, asset.ID, asset.IdentityID, StatusProcessing, "", "")
	if err != nil {
		return
	}
	result, err := processor.ProcessAsset(ctx, processing)
	if err != nil {
		_, _ = s.Store.SetStatus(ctx, processing.ID, processing.IdentityID, StatusFailed, "processor_failed", err.Error())
		return
	}
	if result.Status == "" {
		result.Status = StatusComplete
	}
	_, _ = s.Store.SetResult(ctx, processing.ID, processing.IdentityID, result)
}

func (s *Service) hashAndSniff(ctx context.Context, ref objectstore.ObjectRef, fileName string) (string, string, error) {
	rc, attrs, err := s.Objects.Get(ctx, ref)
	if err != nil {
		return "", "", err
	}
	defer rc.Close()
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
