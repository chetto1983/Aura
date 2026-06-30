package main

import (
	"context"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/jackc/pgx/v5/pgxpool"
)

const assetProcessJobType = "asset_process"

type ingestionJobCreator interface {
	Create(context.Context, documents.CreateIngestionJobRequest) (documents.IngestionJob, error)
}

type runtimeAssetProcessingQueue struct {
	store ingestionJobCreator
	now   func() time.Time
}

func newRuntimeAssetProcessingQueue(pool *pgxpool.Pool) *runtimeAssetProcessingQueue {
	queue := &runtimeAssetProcessingQueue{now: time.Now}
	if pool != nil {
		queue.store = documents.NewPostgresIngestionJobStore(pool)
	}
	return queue
}

func (q *runtimeAssetProcessingQueue) EnqueueAssetProcessing(ctx context.Context, asset assets.Asset) error {
	if q == nil || q.store == nil {
		return fmt.Errorf("asset processing queue is not configured")
	}
	_, err := q.store.Create(ctx, assetProcessingIngestionJobRequest(asset, q.clock()))
	return err
}

func (q *runtimeAssetProcessingQueue) clock() time.Time {
	if q == nil || q.now == nil {
		return time.Now().UTC()
	}
	return q.now().UTC()
}

func assetProcessingIngestionJobRequest(asset assets.Asset, nextAttemptAt time.Time) documents.CreateIngestionJobRequest {
	return documents.CreateIngestionJobRequest{
		JobType:        assetProcessJobType,
		Status:         "queued",
		IdempotencyKey: assetProcessJobType + ":" + asset.ID,
		Stage:          "accepted",
		MaxAttempts:    5,
		NextAttemptAt:  nextAttemptAt,
		Payload: map[string]any{
			"asset_id":    asset.ID,
			"identity_id": asset.IdentityID,
			"modality":    string(asset.Modality),
			"status":      string(asset.Status),
			"file_name":   asset.FileName,
			"mime_type":   asset.MIMEType,
			"size_bytes":  asset.SizeBytes,
		},
	}
}
