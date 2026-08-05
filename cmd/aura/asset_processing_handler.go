package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/documents"
)

type acceptedAssetProcessor interface {
	ProcessAccepted(context.Context, string, string) (assets.Asset, error)
}

type runtimeAssetProcessHandler struct {
	assets acceptedAssetProcessor
}

func (h runtimeAssetProcessHandler) HandleIngestionJob(ctx context.Context, job documents.IngestionJob) error {
	if h.assets == nil {
		return fmt.Errorf("asset process handler is not configured")
	}
	assetID, err := ingestionPayloadString(job.Payload, "asset_id")
	if err != nil {
		return err
	}
	identityID, err := ingestionPayloadString(job.Payload, "identity_id")
	if err != nil {
		return err
	}
	asset, err := h.assets.ProcessAccepted(documents.WithClaimedIngestionJob(ctx, job), identityID, assetID)
	if err != nil {
		return err
	}
	if committedJob, ok := asset.Metadata["pipeline_activation_job_id"].(string); ok && committedJob == job.ID {
		return documents.ErrIngestionJobCompletionCommitted
	}
	return nil
}

func ingestionPayloadString(payload map[string]any, key string) (string, error) {
	value, ok := payload[key].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("ingestion job payload missing %q", key)
	}
	return value, nil
}
