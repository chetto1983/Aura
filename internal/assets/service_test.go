package assets

import (
	"context"
	"errors"
	"maps"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/documents"
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

func TestServicePresignAcceptsLibraryScope(t *testing.T) {
	svc, store := newAssetServiceTestRig(t, Limits{
		MaxDocumentBytes: 256,
		MaxImageBytes:    100,
		MaxAudioBytes:    100,
	})

	_, err := svc.Presign(context.Background(), PresignRequest{
		IdentityID:        serviceIdentityID,
		SourceKind:        SourceWeb,
		Scope:             ScopeLibrary,
		FileName:          "manual.pdf",
		MIMEType:          "application/pdf",
		DeclaredSizeBytes: 128,
		ModalityHint:      ModalityDocument,
	})
	if err != nil {
		t.Fatalf("Presign returned error: %v", err)
	}
	if len(store.created) != 1 {
		t.Fatalf("created %d assets, want 1", len(store.created))
	}
	if store.created[0].Scope != ScopeLibrary {
		t.Fatalf("Create scope = %q, want %q", store.created[0].Scope, ScopeLibrary)
	}
}

func TestServicePresignDefaultsToThreadScope(t *testing.T) {
	svc, store := newAssetServiceTestRig(t, Limits{
		MaxDocumentBytes: 100,
		MaxImageBytes:    100,
		MaxAudioBytes:    100,
	})

	_, err := svc.Presign(context.Background(), PresignRequest{
		IdentityID:        serviceIdentityID,
		SourceKind:        SourceWeb,
		ThreadID:          "thread-1",
		FileName:          "note.txt",
		MIMEType:          "text/plain",
		DeclaredSizeBytes: 32,
		ModalityHint:      ModalityDocument,
	})
	if err != nil {
		t.Fatalf("Presign returned error: %v", err)
	}
	if len(store.created) != 1 {
		t.Fatalf("created %d assets, want 1", len(store.created))
	}
	if store.created[0].Scope != ScopeThread {
		t.Fatalf("Create scope = %q, want %q", store.created[0].Scope, ScopeThread)
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

func TestServiceFinalizeMarksAcceptedAndEnqueuesProcessing(t *testing.T) {
	svc, store := newAssetServiceTestRig(t, Limits{
		MaxDocumentBytes: 100,
		MaxImageBytes:    100,
		MaxAudioBytes:    100,
	})
	queue := &recordingProcessingQueue{}
	svc.ProcessingJobs = queue
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
	hashes, err := documents.ContentHashesReader(strings.NewReader("%PDF test"))
	if err != nil {
		t.Fatal(err)
	}
	if accepted.ContentHash != hashes.SHA256 {
		t.Fatalf("ContentHash = %q, want sha256", accepted.ContentHash)
	}
	if queue.asset.ID != accepted.ID || queue.asset.Status != StatusAccepted {
		t.Fatalf("processing queue saw asset = %#v, want accepted asset %s", queue.asset, accepted.ID)
	}

	select {
	case processing := <-processor.called:
		t.Fatalf("processor ran inline for queued asset = %#v", processing)
	case <-time.After(50 * time.Millisecond):
	}

	got, err := store.GetForIdentity(context.Background(), accepted.ID, serviceIdentityID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusAccepted {
		t.Fatalf("stored asset status = %s, want accepted until durable worker claims it", got.Status)
	}
}

func TestServiceRetryEnqueuesProcessingWithoutStartingProcessor(t *testing.T) {
	svc, store := newAssetServiceTestRig(t, Limits{
		MaxDocumentBytes: 100,
		MaxImageBytes:    100,
		MaxAudioBytes:    100,
	})
	queue := &recordingProcessingQueue{}
	svc.ProcessingJobs = queue
	processor := &recordingProcessor{
		called: make(chan Asset, 1),
		result: Result{Status: StatusSearchable, DocumentID: "doc-1", Summary: "indexed"},
	}
	svc.Processors.Document = processor
	asset, err := store.Create(context.Background(), CreateRequest{
		IdentityID:        serviceIdentityID,
		SourceKind:        SourceWeb,
		Scope:             ScopeThread,
		Modality:          ModalityDocument,
		FileName:          "manual.pdf",
		MIMEType:          "application/pdf",
		DeclaredSizeBytes: 9,
		ObjectBucket:      "asset-test",
		ObjectKey:         "asset-key",
		Metadata:          map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	asset, err = store.SetStatus(context.Background(), asset.ID, serviceIdentityID, StatusFailed, "processor_failed", "boom")
	if err != nil {
		t.Fatal(err)
	}

	retry, err := svc.Retry(context.Background(), serviceIdentityID, asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retry.Status != StatusAccepted || retry.ErrorCode != "" || retry.ErrorMessage != "" {
		t.Fatalf("retry asset = %#v, want accepted with cleared error", retry)
	}
	if queue.asset.ID != retry.ID || queue.asset.Status != StatusAccepted {
		t.Fatalf("processing queue saw asset = %#v, want accepted retry %s", queue.asset, retry.ID)
	}
	select {
	case processing := <-processor.called:
		t.Fatalf("processor ran inline for retried asset = %#v", processing)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestServiceProcessAcceptedRunsProcessor(t *testing.T) {
	svc, store := newAssetServiceTestRig(t, Limits{
		MaxDocumentBytes: 100,
		MaxImageBytes:    100,
		MaxAudioBytes:    100,
	})
	processor := &recordingProcessor{
		called: make(chan Asset, 1),
		result: Result{Status: StatusSearchable, DocumentID: "doc-1", Summary: "indexed"},
	}
	svc.Processors.Document = processor
	asset, err := store.Create(context.Background(), CreateRequest{
		IdentityID:        serviceIdentityID,
		SourceKind:        SourceWeb,
		Scope:             ScopeThread,
		Modality:          ModalityDocument,
		FileName:          "manual.pdf",
		MIMEType:          "application/pdf",
		DeclaredSizeBytes: 9,
		ObjectBucket:      "asset-test",
		ObjectKey:         "asset-key",
		Metadata:          map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	asset, err = store.MarkAccepted(context.Background(), asset.ID, serviceIdentityID, 9, "hash", "application/pdf")
	if err != nil {
		t.Fatal(err)
	}

	processed, err := svc.ProcessAccepted(context.Background(), serviceIdentityID, asset.ID)
	if err != nil {
		t.Fatal(err)
	}

	if processed.Status != StatusSearchable || processed.DocumentID != "doc-1" || processed.Summary != "indexed" {
		t.Fatalf("processed asset = %#v", processed)
	}
	select {
	case processing := <-processor.called:
		if processing.Status != StatusProcessing || processing.ID != asset.ID {
			t.Fatalf("processor saw asset = %#v", processing)
		}
	default:
		t.Fatal("processor was not called")
	}
}

func TestServiceProcessAcceptedRearmsFailedAssetForDurableRetry(t *testing.T) {
	svc, store := newAssetServiceTestRig(t, Limits{
		MaxDocumentBytes: 100,
		MaxImageBytes:    100,
		MaxAudioBytes:    100,
	})
	processor := &recordingProcessor{
		called: make(chan Asset, 1),
		result: Result{Status: StatusSearchable, DocumentID: "doc-1", Summary: "recovered"},
	}
	svc.Processors.Document = processor
	asset, err := store.Create(context.Background(), CreateRequest{
		IdentityID: serviceIdentityID, SourceKind: SourceWeb, Scope: ScopeLibrary,
		Modality: ModalityDocument, FileName: "manual.pdf", MIMEType: "application/pdf",
		DeclaredSizeBytes: 9, ObjectBucket: "asset-test", ObjectKey: "asset-key",
		Metadata: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	asset, err = store.SetStatus(context.Background(), asset.ID, serviceIdentityID, StatusFailed, "processor_failed", "temporary")
	if err != nil {
		t.Fatal(err)
	}

	processed, err := svc.ProcessAccepted(context.Background(), serviceIdentityID, asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	if processed.Status != StatusSearchable || processed.Summary != "recovered" {
		t.Fatalf("processed retry = %#v", processed)
	}
	select {
	case processing := <-processor.called:
		if processing.Status != StatusProcessing || processing.ErrorCode != "" || processing.ErrorMessage != "" {
			t.Fatalf("processor saw retry asset = %#v, want clean processing state", processing)
		}
	default:
		t.Fatal("processor was not called for the durable retry")
	}
}

func TestServiceIngestTelegramFileStoresObjectAndReturnsProcessedAsset(t *testing.T) {
	svc, store := newAssetServiceTestRig(t, Limits{
		MaxDocumentBytes: 100,
		MaxImageBytes:    100,
		MaxAudioBytes:    100,
	})
	processor := &recordingProcessor{
		called: make(chan Asset, 1),
		result: Result{Status: StatusComplete, Summary: "ciao Aura", Metadata: map[string]any{"transcript": "ciao Aura"}},
	}
	svc.Processors.Audio = processor

	asset, err := svc.IngestTelegramFile(context.Background(), TelegramIngestRequest{
		IdentityID: serviceIdentityID,
		ChatID:     42,
		MessageID:  7,
		FileID:     "voice-file",
		FileName:   `voice\unsafe.ogg`,
		MIMEType:   "audio/ogg",
		Modality:   ModalityAudio,
		SizeBytes:  13,
		Reader:     strings.NewReader("OggS test"),
	})
	if err != nil {
		t.Fatalf("IngestTelegramFile() error = %v", err)
	}
	if asset.Status != StatusComplete || asset.Summary != "ciao Aura" {
		t.Fatalf("processed asset = %+v, want complete transcript summary", asset)
	}
	if asset.SourceKind != SourceTelegram || asset.FileName != "unsafe.ogg" || asset.Modality != ModalityAudio {
		t.Fatalf("asset metadata = %+v, want Telegram audio asset with sanitized filename", asset)
	}
	if !strings.Contains(asset.SourceRef, `"chat_id":42`) || !strings.Contains(asset.SourceRef, `"message_id":7`) || !strings.Contains(asset.SourceRef, `"file_id":"voice-file"`) {
		t.Fatalf("SourceRef = %q, want Telegram source JSON", asset.SourceRef)
	}

	select {
	case processing := <-processor.called:
		if processing.Status != StatusProcessing || processing.ID != asset.ID {
			t.Fatalf("processor saw asset = %#v, want processing asset id %s", processing, asset.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("processor was not called")
	}

	attrs, err := svc.Objects.Head(context.Background(), objectstore.ObjectRef{Bucket: asset.ObjectBucket, Key: asset.ObjectKey})
	if err != nil {
		t.Fatalf("stored object Head: %v", err)
	}
	if attrs.SizeBytes != int64(len("OggS test")) || attrs.MIMEType != "audio/ogg" {
		t.Fatalf("stored attrs = %+v, want audio object bytes/mime", attrs)
	}
	if len(store.created) != 1 || store.created[0].SourceKind != SourceTelegram {
		t.Fatalf("created requests = %#v, want one Telegram create", store.created)
	}
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

type recordingProcessingQueue struct {
	asset Asset
	err   error
}

func (q *recordingProcessingQueue) EnqueueAssetProcessing(_ context.Context, asset Asset) error {
	q.asset = asset
	return q.err
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

func (s *fakeAssetStore) ListForLibrary(_ context.Context, identityID string, limit int) ([]Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Asset
	for _, asset := range s.assets {
		if asset.IdentityID == identityID && asset.Scope == ScopeLibrary {
			out = append(out, asset)
			if limit > 0 && len(out) >= limit {
				break
			}
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

func (s *fakeAssetStore) Promote(_ context.Context, id, identityID string) (Asset, error) {
	return s.update(id, identityID, func(asset *Asset) {
		asset.Scope = ScopeLibrary
	})
}

func (s *fakeAssetStore) Delete(_ context.Context, id, identityID string) (Asset, error) {
	return s.update(id, identityID, func(asset *Asset) {
		asset.Status = StatusDeleted
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
	maps.Copy(out, in)
	return out
}
