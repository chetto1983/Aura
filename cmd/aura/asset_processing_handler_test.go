package main

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/documents"
)

type recordingAssetProcessor struct {
	identityID string
	assetID    string
	err        error
	result     assets.Asset
	claim      documents.IngestionJob
	hasClaim   bool
}

func (p *recordingAssetProcessor) ProcessAccepted(ctx context.Context, identityID, assetID string) (assets.Asset, error) {
	p.identityID = identityID
	p.assetID = assetID
	p.claim, p.hasClaim = documents.ClaimedIngestionJob(ctx)
	if p.result.ID != "" {
		return p.result, p.err
	}
	return assets.Asset{ID: assetID, IdentityID: identityID, Status: assets.StatusSearchable}, p.err
}

func TestRuntimeAssetProcessHandlerProcessesPayload(t *testing.T) {
	processor := &recordingAssetProcessor{}
	handler := runtimeAssetProcessHandler{assets: processor}
	job := documents.IngestionJob{
		ID: "queue-job-1", JobType: assetProcessJobType,
		Payload: map[string]any{
			"asset_id":    "asset-1",
			"identity_id": "identity-1",
		},
	}

	if err := handler.HandleIngestionJob(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if processor.assetID != "asset-1" || processor.identityID != "identity-1" {
		t.Fatalf("processor got identity=%q asset=%q", processor.identityID, processor.assetID)
	}
	if !processor.hasClaim || processor.claim.ID != "queue-job-1" {
		t.Fatalf("claimed job was not propagated: %#v", processor.claim)
	}
}

func TestRuntimeAssetProcessHandlerRecognizesAtomicPipelineCompletion(t *testing.T) {
	processor := &recordingAssetProcessor{result: assets.Asset{
		ID: "asset-1", IdentityID: "identity-1", Status: assets.StatusSearchable,
		Metadata: map[string]any{"pipeline_activation_job_id": "queue-job-1"},
	}}
	err := (runtimeAssetProcessHandler{assets: processor}).HandleIngestionJob(
		context.Background(), documents.IngestionJob{
			ID: "queue-job-1", JobType: assetProcessJobType,
			Payload: map[string]any{"asset_id": "asset-1", "identity_id": "identity-1"},
		},
	)
	if !errors.Is(err, documents.ErrIngestionJobCompletionCommitted) {
		t.Fatalf("error = %v, want committed completion sentinel", err)
	}
}

func TestRuntimeAssetProcessHandlerRejectsMissingPayload(t *testing.T) {
	handler := runtimeAssetProcessHandler{assets: &recordingAssetProcessor{}}
	err := handler.HandleIngestionJob(context.Background(), documents.IngestionJob{
		JobType: assetProcessJobType,
		Payload: map[string]any{"asset_id": "asset-1"},
	})
	if err == nil {
		t.Fatal("expected missing identity_id error")
	}
}

func TestRuntimeAssetProcessHandlerPropagatesProcessorError(t *testing.T) {
	processor := &recordingAssetProcessor{err: errors.New("processor failed")}
	handler := runtimeAssetProcessHandler{assets: processor}
	err := handler.HandleIngestionJob(context.Background(), documents.IngestionJob{
		JobType: assetProcessJobType,
		Payload: map[string]any{
			"asset_id":    "asset-1",
			"identity_id": "identity-1",
		},
	})
	if !errors.Is(err, processor.err) {
		t.Fatalf("error = %v, want processor error", err)
	}
}
