package main

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/multimodal"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/jackc/pgx/v5/pgxpool"
)

func buildAssetService(cfg *config.Config, pool *pgxpool.Pool, objectStore objectstore.Store) *assets.Service {
	pipeline, pipelineErr := buildRuntimeDocumentPipeline(cfg, pool)
	if pipelineErr != nil && pool != nil {
		slog.Error("aura assets: document pipeline unavailable", "err", pipelineErr)
	}
	docProcessor := &assets.DocumentProcessor{
		Objects:         objectStore,
		Ingest:          newRuntimeDocumentIngestor(cfg, pool),
		VersionRecorder: newRuntimeDocumentVersionRecorder(pool),
		Pipeline:        pipeline,
	}
	imageProc := assets.NewImageProcessor(objectStore, visionConfigFrom(cfg))
	audioProc := assets.NewAudioProcessor(objectStore, sttConfigFrom(cfg))
	svc := &assets.Service{
		Store:          assets.NewStore(pool),
		Objects:        objectStore,
		ProcessingJobs: newRuntimeAssetProcessingQueue(pool),
		Processors: assets.ProcessorSet{
			Document: docProcessor,
			// An uploaded image gets a vision summary (inline chat) AND a catalog row so
			// document_search can name it and document_open can hand the agent the file.
			// The document leg REGISTERS only — no OCR, no chunking runs here (see
			// ImageDocumentProcessor). Fail-soft: either leg can be down.
			Image: &assets.ImageDocumentProcessor{
				Vision:   imageProc,
				Document: docProcessor,
			},
			Audio: audioProc,
		},
		Limits: assets.Limits{
			MaxDocumentBytes: int64(cfg.AssetMaxDocumentBytes),
			MaxImageBytes:    int64(cfg.AssetMaxImageBytes),
			MaxAudioBytes:    int64(cfg.AssetMaxAudioBytes),
		},
		Bucket:     cfg.ObjectStoreBucket,
		PresignTTL: time.Duration(cfg.AssetPresignTTLSec) * time.Second,
	}
	// Per-identity object isolation (D-08 / VERIF-4): when the pool + Authula KEK secret are
	// present, route every asset object op through the IdentityStore resolver so an identity's
	// bytes land in ITS OWN Garage bucket under ITS OWN key. Absent → the shared Objects store
	// handles every op (pre-cutover / interview-only deploy; assets_test.go's nil-pool call stays
	// valid — no signature change).
	if bundle := buildObjectResolverBundle(cfg, pool); bundle != nil {
		svc.IdentityObjects = bundle.Resolver
		svc.PerIdentityStore = bundle.PerIdentityStore
		docProcessor.PerIdentityObjects = bundle
		imageProc.PerIdentityObjects = bundle
		audioProc.PerIdentityObjects = bundle
	}
	return svc
}

// buildObjectResolverBundle constructs the per-identity object-store resolver + a caching
// per-identity S3Store factory, or nil when the daemon is not yet multi-user provisioned
// (no pool, or no AURA_AUTHULA_SECRET to derive the credential-wrapping KEK). A NewIdentityStore
// failure logs + returns nil so boot never aborts — the shared path still serves every op.
func buildObjectResolverBundle(cfg *config.Config, pool *pgxpool.Pool) *assets.ObjectResolverBundle {
	if pool == nil || strings.TrimSpace(cfg.AuthulaSecret) == "" {
		return nil
	}
	shared := objectstore.Credentials{
		Bucket:    cfg.ObjectStoreBucket,
		AccessKey: cfg.ObjectStoreAccessKey,
		SecretKey: cfg.ObjectStoreSecretKey,
	}
	resolver, err := objectstore.NewIdentityStore(pool, cfg.AuthulaSecret, shared, localSeededIdentityID)
	if err != nil {
		slog.Warn("aura assets: per-identity object resolver disabled", "err", err)
		return nil
	}
	return &assets.ObjectResolverBundle{
		Resolver:         resolver,
		PerIdentityStore: newCachingPerIdentityStoreFactory(cfg),
		SharedBucket:     cfg.ObjectStoreBucket,
	}
}

// newCachingPerIdentityStoreFactory builds a StoreFactory that layers resolved per-identity
// creds on the SHARED S3 endpoint/region/path-style, caching one S3Store per access key
// (sync.Map) so repeated requests reuse a client. NewS3 is a purely local construction (static
// creds, no network round-trip), so a background context is correct for the lazy build.
func newCachingPerIdentityStoreFactory(cfg *config.Config) assets.StoreFactory {
	var cache sync.Map // access key -> objectstore.Store
	return func(creds objectstore.Credentials) (objectstore.Store, error) {
		if v, ok := cache.Load(creds.AccessKey); ok {
			return v.(objectstore.Store), nil
		}
		st, err := objectstore.NewS3(context.Background(), objectstore.S3Config{
			Endpoint:       cfg.ObjectStoreEndpoint,
			PublicEndpoint: cfg.ObjectStorePublicEndpoint,
			Region:         cfg.ObjectStoreRegion,
			AccessKey:      creds.AccessKey,
			SecretKey:      creds.SecretKey,
			PathStyle:      cfg.ObjectStorePathStyle,
		})
		if err != nil {
			return nil, err
		}
		actual, _ := cache.LoadOrStore(creds.AccessKey, objectstore.Store(st))
		return actual.(objectstore.Store), nil
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
