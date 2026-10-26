package assets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/objectstore"
)

const serviceIdentityID = "00000000-0000-0000-0000-000000000001"

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
		result: Result{Status: StatusComplete, DocumentID: "doc-1", Summary: "indexed", Metadata: map[string]any{"k": "v"}},
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
		result: Result{Status: StatusComplete, DocumentID: "doc-1", Summary: "indexed"},
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
		result: Result{Status: StatusComplete, DocumentID: "doc-1", Summary: "indexed"},
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

	if processed.Status != StatusComplete || processed.DocumentID != "doc-1" || processed.Summary != "indexed" {
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

// The cockpit retry route reaches Service.Retry with any asset id. An asset that succeeded,
// or is still being processed, must not go back into the queue (2026-10-26).
func TestServiceRetryRefusesAnAssetThatDidNotFail(t *testing.T) {
	for _, status := range []Status{StatusComplete, StatusProcessing, StatusAccepted} {
		t.Run(string(status), func(t *testing.T) {
			svc, store := newAssetServiceTestRig(t, Limits{MaxDocumentBytes: 100})
			queue := &recordingProcessingQueue{}
			svc.ProcessingJobs = queue
			asset, err := store.Create(context.Background(), CreateRequest{
				IdentityID: serviceIdentityID, SourceKind: SourceWeb, Scope: ScopeThread,
				Modality: ModalityDocument, FileName: "manual.pdf", MIMEType: "application/pdf",
				DeclaredSizeBytes: 9, ObjectBucket: "asset-test", ObjectKey: "asset-key-" + string(status),
				Metadata: map[string]any{},
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.SetStatus(context.Background(), asset.ID, serviceIdentityID, status, "", ""); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.Retry(context.Background(), serviceIdentityID, asset.ID); !errors.Is(err, ErrNotRetryable) {
				t.Fatalf("Retry of a %s asset = %v, want ErrNotRetryable", status, err)
			}
			if queue.asset.ID != "" {
				t.Fatalf("a %s asset was queued for processing again: %#v", status, queue.asset)
			}
		})
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
		result: Result{Status: StatusComplete, DocumentID: "doc-1", Summary: "recovered"},
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
	if processed.Status != StatusComplete || processed.Summary != "recovered" {
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
	calls int
	err   error
}

func (q *recordingProcessingQueue) EnqueueAssetProcessing(_ context.Context, asset Asset) error {
	q.asset = asset
	q.calls++
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
