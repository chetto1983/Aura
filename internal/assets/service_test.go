package assets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/objectstore"
)

const serviceIdentityID = "00000000-0000-0000-0000-000000000001"

func TestServicePresignRejectsUnsupportedExecutable(t *testing.T) {
	svc, _ := newAssetServiceTestRig(t, Limits{
		MaxDocumentBytes: 100,
		MaxImageBytes:    100,
		MaxAudioBytes:    100,
	})

	_, err := svc.Presign(context.Background(), PresignRequest{
		IdentityID:        serviceIdentityID,
		SourceKind:        SourceWeb,
		ThreadID:          "thread-1",
		FileName:          "setup.exe",
		MIMEType:          "application/octet-stream",
		DeclaredSizeBytes: 10,
	})
	if err == nil {
		t.Fatal("Presign(.exe) succeeded, want refusal")
	}
}

func TestServicePresignNeverPutsFilenameInObjectKey(t *testing.T) {
	svc, store := newAssetServiceTestRig(t, Limits{
		MaxDocumentBytes: 100,
		MaxImageBytes:    100,
		MaxAudioBytes:    100,
	})

	resp, err := svc.Presign(context.Background(), PresignRequest{
		IdentityID:        serviceIdentityID,
		SourceKind:        SourceWeb,
		ThreadID:          "thread-1",
		FileName:          `C:\Users\me\Quarterly Secrets.pdf`,
		MIMEType:          "",
		DeclaredSizeBytes: 10,
	})
	if err != nil {
		t.Fatalf("Presign() error = %v", err)
	}
	key := strings.ToLower(resp.Asset.ObjectKey)
	for _, forbidden := range []string{"quarterly", "secrets", ".pdf"} {
		if strings.Contains(key, forbidden) {
			t.Fatalf("object key %q contains filename fragment %q", resp.Asset.ObjectKey, forbidden)
		}
	}
	if len(store.created) != 1 {
		t.Fatalf("store created %d assets, want 1", len(store.created))
	}
	if resp.Asset.FileName != "Quarterly Secrets.pdf" {
		t.Fatalf("FileName = %q, want sanitized basename", resp.Asset.FileName)
	}
}

func TestServiceFinalizeRefusesOversizedActualObject(t *testing.T) {
	svc, _ := newAssetServiceTestRig(t, Limits{
		MaxDocumentBytes: 5,
		MaxImageBytes:    100,
		MaxAudioBytes:    100,
	})

	resp, err := svc.Presign(context.Background(), PresignRequest{
		IdentityID:        serviceIdentityID,
		SourceKind:        SourceWeb,
		ThreadID:          "thread-1",
		FileName:          "manual.pdf",
		MIMEType:          "application/pdf",
		DeclaredSizeBytes: 4,
	})
	if err != nil {
		t.Fatalf("Presign() error = %v", err)
	}
	ref := objectstore.ObjectRef{Bucket: resp.Asset.ObjectBucket, Key: resp.Asset.ObjectKey}
	if _, err := svc.Objects.Put(context.Background(), ref, strings.NewReader("123456"), objectstore.PutOptions{MIMEType: "application/pdf", Size: 6}); err != nil {
		t.Fatalf("Put object: %v", err)
	}

	updated, err := svc.Finalize(context.Background(), serviceIdentityID, resp.Asset.ID)
	if err == nil {
		t.Fatal("Finalize() succeeded, want oversized refusal")
	}
	if updated.Status != StatusRefused || updated.ErrorCode != "asset_refused" {
		t.Fatalf("Finalize() updated asset = %#v, want refused asset", updated)
	}
	if _, err := svc.Objects.Head(context.Background(), ref); err == nil {
		t.Fatal("oversized object still exists after refusal")
	}
}

func TestServiceFinalizeMarksAcceptedAndStartsProcessor(t *testing.T) {
	svc, store := newAssetServiceTestRig(t, Limits{
		MaxDocumentBytes: 100,
		MaxImageBytes:    100,
		MaxAudioBytes:    100,
	})
	processor := &recordingProcessor{
		called: make(chan Asset, 1),
		result: Result{Status: StatusSearchable, DocumentID: "doc-1", Summary: "indexed", Metadata: map[string]any{"k": "v"}},
	}
	svc.Processors.Document = processor

	resp, err := svc.Presign(context.Background(), PresignRequest{
		IdentityID:        serviceIdentityID,
		SourceKind:        SourceWeb,
		ThreadID:          "thread-1",
		FileName:          "manual.pdf",
		MIMEType:          "application/pdf",
		DeclaredSizeBytes: 9,
	})
	if err != nil {
		t.Fatalf("Presign() error = %v", err)
	}
	ref := objectstore.ObjectRef{Bucket: resp.Asset.ObjectBucket, Key: resp.Asset.ObjectKey}
	if _, err := svc.Objects.Put(context.Background(), ref, strings.NewReader("%PDF test"), objectstore.PutOptions{MIMEType: "application/pdf", Size: 9}); err != nil {
		t.Fatalf("Put object: %v", err)
	}

	accepted, err := svc.Finalize(context.Background(), serviceIdentityID, resp.Asset.ID)
	if err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	if accepted.Status != StatusAccepted || accepted.SizeBytes != 9 || accepted.MIMEType != "application/pdf" {
		t.Fatalf("accepted asset = %#v", accepted)
	}
	sum := sha256.Sum256([]byte("%PDF test"))
	if accepted.ContentHash != hex.EncodeToString(sum[:]) {
		t.Fatalf("ContentHash = %q, want sha256", accepted.ContentHash)
	}

	select {
	case processing := <-processor.called:
		if processing.Status != StatusProcessing || processing.ID != accepted.ID {
			t.Fatalf("processor saw asset = %#v, want processing accepted id", processing)
		}
	case <-time.After(time.Second):
		t.Fatal("processor was not called")
	}

	eventually(t, time.Second, func() bool {
		got, err := store.GetForIdentity(context.Background(), accepted.ID, serviceIdentityID)
		return err == nil && got.Status == StatusSearchable && got.DocumentID == "doc-1" && got.Summary == "indexed"
	})
}

type recordingProcessor struct {
	called chan Asset
	result Result
	err    error
}

func (p *recordingProcessor) ProcessAsset(_ context.Context, asset Asset) (Result, error) {
	p.called <- asset
	return p.result, p.err
}

func newAssetServiceTestRig(t *testing.T, limits Limits) (*Service, *fakeAssetStore) {
	t.Helper()
	store := newFakeAssetStore()
	return &Service{
		Store:      store,
		Objects:    objectstore.NewFake(),
		Limits:     limits,
		Bucket:     "asset-test",
		PresignTTL: time.Minute,
	}, store
}

type fakeAssetStore struct {
	mu      sync.Mutex
	next    int
	created []CreateRequest
	assets  map[string]Asset
}

func newFakeAssetStore() *fakeAssetStore {
	return &fakeAssetStore{assets: make(map[string]Asset)}
}

func (s *fakeAssetStore) Create(_ context.Context, req CreateRequest) (Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	id := "asset-test-" + string(rune('0'+s.next))
	asset := Asset{
		ID:                id,
		IdentityID:        req.IdentityID,
		SourceKind:        req.SourceKind,
		SourceRef:         req.SourceRef,
		ThreadID:          req.ThreadID,
		Scope:             req.Scope,
		Modality:          req.Modality,
		Status:            StatusPresigned,
		FileName:          req.FileName,
		MIMEType:          req.MIMEType,
		DeclaredSizeBytes: req.DeclaredSizeBytes,
		ObjectBucket:      req.ObjectBucket,
		ObjectKey:         req.ObjectKey,
		Metadata:          cloneMetadata(req.Metadata),
	}
	s.created = append(s.created, req)
	s.assets[id] = asset
	return asset, nil
}

func (s *fakeAssetStore) GetForIdentity(_ context.Context, id, identityID string) (Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	asset, ok := s.assets[id]
	if !ok || asset.IdentityID != identityID {
		return Asset{}, errors.New("asset not found")
	}
	return asset, nil
}

func (s *fakeAssetStore) ListForThread(_ context.Context, identityID, threadID string) ([]Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Asset
	for _, asset := range s.assets {
		if asset.IdentityID == identityID && asset.ThreadID == threadID {
			out = append(out, asset)
		}
	}
	return out, nil
}

func (s *fakeAssetStore) MarkUploaded(_ context.Context, id, identityID string, size int64, etag string) (Asset, error) {
	return s.update(id, identityID, func(asset *Asset) {
		asset.Status = StatusUploaded
		asset.SizeBytes = size
		asset.ObjectETag = etag
	})
}

func (s *fakeAssetStore) MarkAccepted(_ context.Context, id, identityID string, size int64, hash, mimeType string) (Asset, error) {
	return s.update(id, identityID, func(asset *Asset) {
		asset.Status = StatusAccepted
		asset.SizeBytes = size
		asset.ContentHash = hash
		asset.MIMEType = mimeType
	})
}

func (s *fakeAssetStore) SetStatus(_ context.Context, id, identityID string, status Status, code, message string) (Asset, error) {
	return s.update(id, identityID, func(asset *Asset) {
		asset.Status = status
		asset.ErrorCode = code
		asset.ErrorMessage = message
	})
}

func (s *fakeAssetStore) SetResult(_ context.Context, id, identityID string, result Result) (Asset, error) {
	return s.update(id, identityID, func(asset *Asset) {
		asset.Status = result.Status
		asset.DocumentID = result.DocumentID
		asset.Summary = result.Summary
		asset.Metadata = cloneMetadata(result.Metadata)
		asset.ErrorCode = ""
		asset.ErrorMessage = ""
	})
}

func (s *fakeAssetStore) update(id, identityID string, apply func(*Asset)) (Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	asset, ok := s.assets[id]
	if !ok || asset.IdentityID != identityID {
		return Asset{}, errors.New("asset not found")
	}
	apply(&asset)
	s.assets[id] = asset
	return asset, nil
}

func cloneMetadata(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func eventually(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not satisfied before timeout")
}
