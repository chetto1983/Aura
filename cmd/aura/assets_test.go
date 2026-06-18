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
		ObjectStoreBucket:     "aura-assets",
		AssetMaxDocumentBytes: 123,
		AssetMaxImageBytes:    456,
		AssetMaxAudioBytes:    789,
		AssetPresignTTLSec:    42,
		MultimodalTimeoutSec:  7,
		DocumentsBaseURL:      "http://documents.test",
	}
	objects := objectstore.NewFake()

	svc := buildAssetService(cfg, nil, objects)

	if svc.Bucket != "aura-assets" || svc.PresignTTL != 42*time.Second {
		t.Fatalf("asset service bucket/ttl = %q/%s, want aura-assets/42s", svc.Bucket, svc.PresignTTL)
	}
	if svc.Limits.MaxDocumentBytes != 123 || svc.Limits.MaxImageBytes != 456 || svc.Limits.MaxAudioBytes != 789 {
		t.Fatalf("asset service limits = %+v, want configured limits", svc.Limits)
	}
	doc, ok := svc.Processors.Document.(*assetspkg.DocumentProcessor)
	if !ok {
		t.Fatalf("document processor = %T, want *assets.DocumentProcessor", svc.Processors.Document)
	}
	if doc.Objects != objects {
		t.Fatalf("document processor object store = %T, want shared fake store", doc.Objects)
	}
	if _, ok := doc.Ingest.(*runtimeDocumentIngestor); !ok {
		t.Fatalf("document processor ingestor = %T, want *runtimeDocumentIngestor", doc.Ingest)
	}
}
