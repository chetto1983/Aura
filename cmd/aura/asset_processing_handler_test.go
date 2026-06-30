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
}

func (p *recordingAssetProcessor) ProcessAccepted(_ context.Context, identityID, assetID string) (assets.Asset, error) {
	p.identityID = identityID
	p.assetID = assetID
	return assets.Asset{ID: assetID, IdentityID: identityID, Status: assets.StatusSearchable}, p.err
}

func TestRuntimeAssetProcessHandlerProcessesPayload(t *testing.T) {
	processor := &recordingAssetProcessor{}
	handler := runtimeAssetProcessHandler{assets: processor}
	job := documents.IngestionJob{
		JobType: assetProcessJobType,
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
