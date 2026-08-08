package assets

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/objectstore"
)

// TestIngestAgentFileStoresAcceptedAssetSkippingProcess proves the happy path plus the D-03
// skip-processAsset invariant: the delivered bytes land under the identity's per-identity object
// key, the row is an owned thread-scoped SourceAgent asset, and it stops at Accepted (never routed
// into the embedding / knowledge pipeline — a deliverable must not become searchable memory).
func TestIngestAgentFileStoresAcceptedAssetSkippingProcess(t *testing.T) {
	svc, store := newAssetServiceTestRig(t, Limits{
		MaxDocumentBytes: 100,
		MaxImageBytes:    100,
		MaxAudioBytes:    100,
	})
	// A wired processor proves D-03: IngestAgentFile must NOT route the asset through processAsset,
	// so this processor is never invoked (its channel stays empty).
	processor := &recordingProcessor{
		called: make(chan Asset, 1),
		result: Result{Status: StatusComplete},
	}
	svc.Processors.Document = processor

	const body = "agent report bytes"
	asset, err := svc.IngestAgentFile(context.Background(), AgentIngestRequest{
		IdentityID: serviceIdentityID,
		ThreadID:   "thread-agent-1",
		FileName:   `sub\report.pdf`,
		MIMEType:   "application/pdf",
		Modality:   ModalityDocument,
		SizeBytes:  int64(len(body)),
		Reader:     strings.NewReader(body),
	})
	if err != nil {
		t.Fatalf("IngestAgentFile() error = %v", err)
	}
	if asset.Status != StatusAccepted {
		t.Fatalf("asset.Status = %q, want %q (processAsset must be skipped, D-03)", asset.Status, StatusAccepted)
	}
	if asset.SourceKind != SourceAgent {
		t.Fatalf("asset.SourceKind = %q, want %q", asset.SourceKind, SourceAgent)
	}
	if asset.ThreadID != "thread-agent-1" || asset.Scope != ScopeThread {
		t.Fatalf("asset scope/thread = %q/%q, want thread-scoped delivery", asset.Scope, asset.ThreadID)
	}
	if asset.FileName != "report.pdf" {
		t.Fatalf("asset.FileName = %q, want sanitized basename", asset.FileName)
	}
	if asset.SourceRef != "" {
		t.Fatalf("asset.SourceRef = %q, want empty (an agent delivery has no source ref)", asset.SourceRef)
	}
	select {
	case got := <-processor.called:
		t.Fatalf("processAsset ran for an agent delivery = %#v (D-03 violated)", got)
	default:
	}
	// The identity segment is gone from the key because the BUCKET is per-identity now, so
	// repeating the id inside a store that belongs to that id separated nothing. What the key
	// must do is land where the owner can see it and the reconciler can read it.
	if !strings.HasPrefix(asset.ObjectKey, "chat/") {
		t.Fatalf("asset.ObjectKey = %q, want it under the browsable chat/ prefix", asset.ObjectKey)
	}
	if strings.Contains(asset.ObjectKey, serviceIdentityID) {
		t.Fatalf("asset.ObjectKey = %q still repeats the identity the bucket already scopes", asset.ObjectKey)
	}
	rc, _, err := svc.Objects.Get(context.Background(), objectstore.ObjectRef{Bucket: asset.ObjectBucket, Key: asset.ObjectKey})
	if err != nil {
		t.Fatalf("stored object Get: %v", err)
	}
	defer func() { _ = rc.Close() }()
	stored, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if string(stored) != body {
		t.Fatalf("stored bytes = %q, want %q", stored, body)
	}
	if len(store.created) != 1 || store.created[0].SourceKind != SourceAgent {
		t.Fatalf("created requests = %#v, want one agent create", store.created)
	}
}

// TestIngestAgentFileBypassesLimits proves D-04: a file that exceeds the per-modality Limits cap
// (which IngestTelegramFile would refuse) still ingests to Accepted — send_file's 50 MiB gate is
// the single authoritative delivery ceiling, so Limits.Validate is skipped for agent deliveries.
func TestIngestAgentFileBypassesLimits(t *testing.T) {
	svc, _ := newAssetServiceTestRig(t, Limits{
		MaxDocumentBytes: 5,
		MaxImageBytes:    5,
		MaxAudioBytes:    5,
	})
	const body = "this document is far larger than the 5-byte per-modality cap"
	asset, err := svc.IngestAgentFile(context.Background(), AgentIngestRequest{
		IdentityID: serviceIdentityID,
		ThreadID:   "thread-agent-2",
		FileName:   "big.pdf",
		MIMEType:   "application/pdf",
		Modality:   ModalityDocument,
		SizeBytes:  int64(len(body)),
		Reader:     strings.NewReader(body),
	})
	if err != nil {
		t.Fatalf("IngestAgentFile() oversized-per-modality error = %v, want ingest (D-04)", err)
	}
	if asset.Status != StatusAccepted {
		t.Fatalf("asset.Status = %q, want %q (Limits.Validate must be skipped, D-04)", asset.Status, StatusAccepted)
	}
}

// TestIngestAgentFilePutErrorMovesAssetToFailed proves the object-Put failure path: the method
// surfaces the error and moves the created asset to StatusFailed (object_put_failed) so the caller
// (37A-02) can degrade to path-only delivery.
func TestIngestAgentFilePutErrorMovesAssetToFailed(t *testing.T) {
	svc, store := newAssetServiceTestRig(t, Limits{
		MaxDocumentBytes: 100,
		MaxImageBytes:    100,
		MaxAudioBytes:    100,
	})
	putErr := errors.New("garage put boom")
	svc.Objects = &failingPutStore{Store: objectstore.NewFake(), err: putErr}

	_, err := svc.IngestAgentFile(context.Background(), AgentIngestRequest{
		IdentityID: serviceIdentityID,
		ThreadID:   "thread-agent-3",
		FileName:   "report.pdf",
		MIMEType:   "application/pdf",
		Modality:   ModalityDocument,
		SizeBytes:  9,
		Reader:     strings.NewReader("%PDF test"),
	})
	if !errors.Is(err, putErr) {
		t.Fatalf("IngestAgentFile() error = %v, want %v", err, putErr)
	}
	if len(store.assets) != 1 {
		t.Fatalf("store has %d assets, want 1 failed create", len(store.assets))
	}
	var got Asset
	for _, a := range store.assets {
		got = a
	}
	if got.Status != StatusFailed || got.ErrorCode != "object_put_failed" {
		t.Fatalf("failed asset = %#v, want StatusFailed/object_put_failed", got)
	}
}

// failingPutStore is an objectstore.Store whose Put always fails; every other method delegates to
// the embedded fake so the ingest reaches Put with a real created asset.
type failingPutStore struct {
	objectstore.Store
	err error
}

func (f *failingPutStore) Put(context.Context, objectstore.ObjectRef, io.Reader, objectstore.PutOptions) (objectstore.Attrs, error) {
	return objectstore.Attrs{}, f.err
}
