package main

import (
	"time"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/jackc/pgx/v5/pgxpool"
)

func buildAssetService(cfg *config.Config, pool *pgxpool.Pool, objectStore objectstore.Store) *assets.Service {
	return &assets.Service{
		Store:   assets.NewStore(pool),
		Objects: objectStore,
		Processors: assets.ProcessorSet{
			Document: &assets.DocumentProcessor{
				Objects: objectStore,
				Ingest:  newRuntimeDocumentIngestor(cfg, pool),
			},
		},
		Limits: assets.Limits{
			MaxDocumentBytes: int64(cfg.AssetMaxDocumentBytes),
			MaxImageBytes:    int64(cfg.AssetMaxImageBytes),
			MaxAudioBytes:    int64(cfg.AssetMaxAudioBytes),
		},
		Bucket:     cfg.ObjectStoreBucket,
		PresignTTL: time.Duration(cfg.AssetPresignTTLSec) * time.Second,
	}
}
