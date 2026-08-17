package main

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	assetspkg "github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/config"
	llmpkg "github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/objectstore"
)

func TestBuildObjectStoreBackends(t *testing.T) {
	t.Run("fake", func(t *testing.T) {
		store, err := buildObjectStore(context.Background(), &config.Config{ObjectStoreBackend: "fake"})
		if err != nil {
			t.Fatalf("buildObjectStore(fake) error = %v", err)
		}
		if _, ok := store.(*objectstore.FakeStore); !ok {
			t.Fatalf("buildObjectStore(fake) = %T, want *objectstore.FakeStore", store)
		}
	})

	t.Run("filesystem-dev", func(t *testing.T) {
		endpoint := url.URL{Scheme: "file", Path: filepath.ToSlash(t.TempDir())}
		store, err := buildObjectStore(context.Background(), &config.Config{
			ObjectStoreBackend:  "filesystem-dev",
			ObjectStoreEndpoint: endpoint.String(),
		})
		if err != nil {
			t.Fatalf("buildObjectStore(filesystem-dev) error = %v", err)
		}
		if _, ok := store.(*objectstore.FilesystemStore); !ok {
			t.Fatalf("buildObjectStore(filesystem-dev) = %T, want *objectstore.FilesystemStore", store)
		}
	})

	t.Run("filesystem-dev compose backend override keeps default root", func(t *testing.T) {
		runDir := t.TempDir()
		store, err := buildObjectStore(context.Background(), &config.Config{
			RunDir:              runDir,
			ObjectStoreBackend:  "filesystem-dev",
			ObjectStoreEndpoint: "http://garage:3900",
		})
		if err != nil {
			t.Fatalf("buildObjectStore(filesystem-dev compose default endpoint) error = %v", err)
		}
		fs, ok := store.(*objectstore.FilesystemStore)
		if !ok {
			t.Fatalf("buildObjectStore(filesystem-dev compose default endpoint) = %T, want *objectstore.FilesystemStore", store)
		}
		ref := objectstore.ObjectRef{Bucket: "bucket", Key: objectstore.AssetKey("id", "asset")}
		if _, err := fs.Put(context.Background(), ref, strings.NewReader("asset"), objectstore.PutOptions{MIMEType: "text/plain", Size: 5}); err != nil {
			t.Fatalf("filesystem-dev fallback Put() error = %v", err)
		}
		if _, err := os.Stat(filepath.Join(runDir, "objectstore", "bucket", filepath.FromSlash(ref.Key))); err != nil {
			t.Fatalf("filesystem-dev fallback did not write under run dir: %v", err)
		}
	})

	t.Run("unknown", func(t *testing.T) {
		if _, err := buildObjectStore(context.Background(), &config.Config{ObjectStoreBackend: "mystery"}); err == nil {
			t.Fatal("buildObjectStore(unknown) succeeded, want error")
		}
	})
}

func TestBuildAssetServiceWiresDocumentProcessor(t *testing.T) {
	cfg := &config.Config{
		ObjectStoreBucket:       "aura-assets",
		AssetMaxDocumentBytes:   123,
		AssetMaxImageBytes:      456,
		AssetMaxAudioBytes:      789,
		AssetPresignTTLSec:      42,
		MultimodalTimeoutSec:    7,
		VisionCloud:             true,
		MultimodalBaseURL:       "http://vision-local.test/v1",
		MultimodalModel:         "glm-ocr",
		MultimodalFallbackModel: "minimax/minimax-m3",
		STTBaseURL:              "http://stt.test/v1",
		STTModel:                "large-v3-turbo",
		STTLanguage:             "it",
		LLM: llmpkg.Config{
			Model:   "deepseek/deepseek-v4-flash",
			BaseURL: "http://openrouter.test/api/v1",
			APIKey:  "test-key",
		},
	}
	objects := objectstore.NewFake()

	svc := buildAssetService(cfg, nil, objects)

	if svc.Bucket != "aura-assets" || svc.PresignTTL != 42*time.Second {
		t.Fatalf("asset service bucket/ttl = %q/%s, want aura-assets/42s", svc.Bucket, svc.PresignTTL)
	}
	if svc.Limits.MaxDocumentBytes != 123 || svc.Limits.MaxImageBytes != 456 || svc.Limits.MaxAudioBytes != 789 {
		t.Fatalf("asset service limits = %+v, want configured limits", svc.Limits)
	}
	if _, ok := svc.ProcessingJobs.(*runtimeAssetProcessingQueue); !ok {
		t.Fatalf("asset processing jobs = %T, want *runtimeAssetProcessingQueue", svc.ProcessingJobs)
	}
	// The document leg has no dependencies left to wire: it derives the id the ingest
	// sidecar files the object under and writes nothing, so what is worth pinning is that
	// the image leg shares the SAME instance rather than that either carries a store.
	doc, ok := svc.Processors.Document.(*assetspkg.DocumentProcessor)
	if !ok {
		t.Fatalf("document processor = %T, want *assets.DocumentProcessor", svc.Processors.Document)
	}
	imageDoc, ok := svc.Processors.Image.(*assetspkg.ImageDocumentProcessor)
	if !ok {
		t.Fatalf("image processor = %T, want *assets.ImageDocumentProcessor", svc.Processors.Image)
	}
	if imageDoc.Document != doc {
		t.Fatalf("image document processor document branch = %T, want shared document processor", imageDoc.Document)
	}
	img, ok := imageDoc.Vision.(*assetspkg.ImageProcessor)
	if !ok {
		t.Fatalf("image document processor vision branch = %T, want *assets.ImageProcessor", imageDoc.Vision)
	}
	if img.Objects != objects {
		t.Fatalf("image processor object store = %T, want shared fake store", img.Objects)
	}
	// The vision config is now opaque inside the shared multimodal client, so assert
	// the cfg -> multimodal.VisionConfig projection directly (the wiring under test).
	vcfg := visionConfigFrom(cfg)
	if !vcfg.VisionCloud || vcfg.Model != "deepseek/deepseek-v4-flash" ||
		vcfg.LocalBaseURL != "http://vision-local.test/v1" ||
		vcfg.LocalModel != "glm-ocr" ||
		vcfg.FallbackModel != "minimax/minimax-m3" ||
		vcfg.OpenRouterBaseURL != "http://openrouter.test/api/v1" ||
		vcfg.OpenRouterAPIKey != "test-key" ||
		vcfg.TimeoutSec != 7 {
		t.Fatalf("vision config = %+v, want projected vision config", vcfg)
	}
	audio, ok := svc.Processors.Audio.(*assetspkg.AudioProcessor)
	if !ok {
		t.Fatalf("audio processor = %T, want *assets.AudioProcessor", svc.Processors.Audio)
	}
	if audio.Objects != objects {
		t.Fatalf("audio processor object store = %T, want shared fake store", audio.Objects)
	}
	scfg := sttConfigFrom(cfg)
	if scfg.LocalBaseURL != "http://stt.test/v1" ||
		scfg.LocalModel != "large-v3-turbo" ||
		scfg.Language != "it" ||
		scfg.OpenRouterBaseURL != "http://openrouter.test/api/v1" ||
		scfg.OpenRouterAPIKey != "test-key" ||
		scfg.TimeoutSec != 7 {
		t.Fatalf("stt config = %+v, want projected STT config", scfg)
	}
}
