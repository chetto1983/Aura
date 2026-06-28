package main

import (
	"time"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/multimodal"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/jackc/pgx/v5/pgxpool"
)

func buildAssetService(cfg *config.Config, pool *pgxpool.Pool, objectStore objectstore.Store) *assets.Service {
	docProcessor := &assets.DocumentProcessor{
		Objects: objectStore,
		Ingest:  newRuntimeDocumentIngestor(cfg, pool),
	}
	return &assets.Service{
		Store:   assets.NewStore(pool),
		Objects: objectStore,
		Processors: assets.ProcessorSet{
			Document: docProcessor,
			// An uploaded image gets BOTH a vision summary (inline chat) AND searchable
			// OCR chunks: the document processor OCRs+indexes it via markitdown (spike 075),
			// the image processor keeps the "describe this image" summary. Fail-soft.
			Image: &assets.ImageDocumentProcessor{
				Vision:   assets.NewImageProcessor(objectStore, visionConfigFrom(cfg)),
				Document: docProcessor,
			},
			Audio: assets.NewAudioProcessor(objectStore, sttConfigFrom(cfg)),
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

func visionConfigFrom(cfg *config.Config) multimodal.VisionConfig {
	return multimodal.VisionConfig{
		VisionCloud:       cfg.VisionCloud,
		Model:             cfg.LLM.Model,
		LocalBaseURL:      cfg.MultimodalBaseURL,
		LocalModel:        cfg.MultimodalModel,
		FallbackModel:     cfg.MultimodalFallbackModel,
		OpenRouterBaseURL: cfg.LLM.BaseURL,
		OpenRouterAPIKey:  cfg.LLM.APIKey,
		TimeoutSec:        cfg.MultimodalTimeoutSec,
	}
}

func sttConfigFrom(cfg *config.Config) multimodal.STTConfig {
	return multimodal.STTConfig{
		LocalBaseURL:      cfg.STTBaseURL,
		LocalModel:        cfg.STTModel,
		Language:          cfg.STTLanguage,
		CloudModel:        cfg.STTCloudModel,
		OpenRouterBaseURL: cfg.LLM.BaseURL,
		OpenRouterAPIKey:  cfg.LLM.APIKey,
		TimeoutSec:        cfg.MultimodalTimeoutSec,
	}
}
