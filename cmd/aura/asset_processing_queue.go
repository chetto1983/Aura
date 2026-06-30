package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const assetProcessJobType = "asset_process"

type runtimeAssetProcessingQueue struct {
	pool *pgxpool.Pool
}

func newRuntimeAssetProcessingQueue(pool *pgxpool.Pool) *runtimeAssetProcessingQueue {
	return &runtimeAssetProcessingQueue{pool: pool}
}

func (q *runtimeAssetProcessingQueue) EnqueueAssetProcessing(ctx context.Context, asset assets.Asset) error {
	if q == nil || q.pool == nil {
		return fmt.Errorf("asset processing queue is not configured")
	}
	payload, err := json.Marshal(map[string]any{
		"asset_id":    asset.ID,
		"identity_id": asset.IdentityID,
		"modality":    asset.Modality,
		"status":      asset.Status,
		"file_name":   asset.FileName,
		"mime_type":   asset.MIMEType,
		"size_bytes":  asset.SizeBytes,
	})
	if err != nil {
		return fmt.Errorf("asset processing payload: %w", err)
	}
	_, err = sqlc.New(q.pool).CreateIngestionJob(ctx, sqlc.CreateIngestionJobParams{
		JobType:        assetProcessJobType,
		Status:         "queued",
		IdempotencyKey: assetProcessJobType + ":" + asset.ID,
		Stage:          "accepted",
		MaxAttempts:    5,
		NextAttemptAt:  pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		Payload:        payload,
	})
	return err
}
