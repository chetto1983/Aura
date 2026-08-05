package main

import (
	"context"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/documents"
)

type recordingIngestionJobCreator struct {
	called bool
	req    documents.CreateIngestionJobRequest
}

func (s *recordingIngestionJobCreator) Create(_ context.Context, req documents.CreateIngestionJobRequest) (documents.IngestionJob, error) {
	s.called = true
	s.req = req
	return documents.IngestionJob{ID: "job-1"}, nil
}

func TestRuntimeAssetProcessingQueueEnqueuesDocumentJob(t *testing.T) {
	now := time.Date(2026, 6, 30, 10, 11, 12, 0, time.UTC)
	store := &recordingIngestionJobCreator{}
	queue := &runtimeAssetProcessingQueue{
		store: store,
		now:   func() time.Time { return now },
	}
	asset := assets.Asset{
		ID:         "asset-1",
		IdentityID: "identity-1",
		Modality:   assets.ModalityDocument,
		Status:     assets.StatusAccepted,
		FileName:   "manual.pdf",
		MIMEType:   "application/pdf",
		SizeBytes:  42,
	}

	if err := queue.EnqueueAssetProcessing(context.Background(), asset); err != nil {
		t.Fatal(err)
	}

	if !store.called {
		t.Fatal("expected durable job store Create to be called")
	}
	req := store.req
	if req.JobType != assetProcessJobType || req.Status != "queued" || req.Stage != "accepted" {
		t.Fatalf("job lifecycle fields = %#v", req)
	}
	if req.IdempotencyKey != "asset_process:asset-1" || req.MaxAttempts != 5 || !req.NextAttemptAt.Equal(now) {
		t.Fatalf("job retry fields = %#v", req)
	}
	if req.Payload["asset_id"] != "asset-1" ||
		req.Payload["identity_id"] != "identity-1" ||
		req.Payload["modality"] != "document" ||
		req.Payload["status"] != "accepted" ||
		req.Payload["file_name"] != "manual.pdf" ||
		req.Payload["mime_type"] != "application/pdf" ||
		req.Payload["size_bytes"] != int64(42) {
		t.Fatalf("job payload = %#v", req.Payload)
	}
}

// TestAssetProcessingIngestionJobRequestSetsIdentityID guards against the
// 8d2701bd1 regression: identity_id was written into the untyped Payload
// map but never onto the request struct's own IdentityID field, so
// createIngestionJobParams (internal/documents/jobs_store.go) rejected
// every asset with "invalid ingestion job identity id \"\"" and finalize
// returned HTTP 400 for all uploads.
func TestAssetProcessingIngestionJobRequestSetsIdentityID(t *testing.T) {
	now := time.Date(2026, 6, 30, 10, 11, 12, 0, time.UTC)
	asset := assets.Asset{
		ID:         "asset-1",
		IdentityID: "identity-1",
		Modality:   assets.ModalityDocument,
		Status:     assets.StatusAccepted,
		FileName:   "manual.pdf",
		MIMEType:   "application/pdf",
		SizeBytes:  42,
	}

	req := assetProcessingIngestionJobRequest(asset, now)

	if req.IdentityID != asset.IdentityID {
		t.Fatalf("IdentityID = %q, want %q", req.IdentityID, asset.IdentityID)
	}
	if req.AssetID != asset.ID {
		t.Fatalf("AssetID = %q, want %q", req.AssetID, asset.ID)
	}
}

func TestRuntimeAssetProcessingQueueRequiresStore(t *testing.T) {
	var queue *runtimeAssetProcessingQueue
	if err := queue.EnqueueAssetProcessing(context.Background(), assets.Asset{}); err == nil {
		t.Fatal("expected missing queue store error")
	}
}
