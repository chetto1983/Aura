package assets

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/objectstore"
)

func TestIngestDocumentPersistsLibraryOriginalBeforeQueueing(t *testing.T) {
	t.Parallel()
	svc, store := newAssetServiceTestRig(t, Limits{
		MaxDocumentBytes: 100,
		MaxImageBytes:    100,
		MaxAudioBytes:    100,
	})
	queue := &recordingProcessingQueue{}
	svc.ProcessingJobs = queue
	processor := &recordingProcessor{called: make(chan Asset, 1)}
	svc.Processors.Document = processor

	const body = "%PDF durable original"
	asset, err := svc.IngestDocument(context.Background(), DocumentIngestRequest{
		IdentityID: serviceIdentityID,
		SourceKind: SourceWorkspace,
		SourceRef:  "sha256:workspace-locator",
		Title:      "  Quarterly report  ",
		FileName:   "report.pdf",
		MIMEType:   "application/pdf",
		SizeBytes:  int64(len(body)),
		Reader:     strings.NewReader(body),
	})
	if err != nil {
		t.Fatalf("IngestDocument() error = %v", err)
	}
	if asset.Status != StatusAccepted || asset.Scope != ScopeLibrary || asset.SourceKind != SourceWorkspace {
		t.Fatalf("accepted asset = %#v, want workspace library asset", asset)
	}
	if queue.asset.ID != asset.ID || queue.asset.Status != StatusAccepted {
		t.Fatalf("queue asset = %#v, want accepted %s", queue.asset, asset.ID)
	}
	select {
	case got := <-processor.called:
		t.Fatalf("processor ran inline with %#v", got)
	default:
	}
	if len(store.created) != 1 || store.created[0].SourceRef != "sha256:workspace-locator" {
		t.Fatalf("created requests = %#v", store.created)
	}
	if got := asset.Metadata["title"]; got != "Quarterly report" {
		t.Fatalf("title metadata = %#v, want trimmed display title", got)
	}
	rc, _, err := svc.Objects.Get(context.Background(), objectstore.ObjectRef{
		Bucket: asset.ObjectBucket,
		Key:    asset.ObjectKey,
	})
	if err != nil {
		t.Fatalf("get original: %v", err)
	}
	defer func() { _ = rc.Close() }()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("stored bytes = %q, want %q", got, body)
	}
}

func TestIngestTelegramDocumentUsesLibraryAndDurableQueue(t *testing.T) {
	t.Parallel()
	svc, _ := newAssetServiceTestRig(t, Limits{
		MaxDocumentBytes: 100,
		MaxImageBytes:    100,
		MaxAudioBytes:    100,
	})
	queue := &recordingProcessingQueue{}
	svc.ProcessingJobs = queue
	processor := &recordingProcessor{called: make(chan Asset, 1)}
	svc.Processors.Document = processor

	asset, err := svc.IngestTelegramFile(context.Background(), TelegramIngestRequest{
		IdentityID: serviceIdentityID,
		ChatID:     42,
		MessageID:  7,
		FileID:     "telegram-pdf",
		FileName:   "manual.pdf",
		MIMEType:   "application/pdf",
		Modality:   ModalityDocument,
		SizeBytes:  9,
		Reader:     strings.NewReader("%PDF test"),
	})
	if err != nil {
		t.Fatalf("IngestTelegramFile() error = %v", err)
	}
	if asset.Scope != ScopeLibrary || asset.Status != StatusAccepted {
		t.Fatalf("Telegram document = %#v, want accepted library asset", asset)
	}
	if queue.asset.ID != asset.ID {
		t.Fatalf("queued asset id = %q, want %q", queue.asset.ID, asset.ID)
	}
	select {
	case got := <-processor.called:
		t.Fatalf("processor ran inline with %#v", got)
	default:
	}
}
