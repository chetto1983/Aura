package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/config"
	objectstorepkg "github.com/chetto1983/aura/internal/objectstore"
)

func runObjectStore(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: aura objectstore {bootstrap}")
		os.Exit(1)
	}
	cfg := config.LoadDB()
	ctx := context.Background()

	switch args[0] {
	case "bootstrap":
		objectStoreBootstrap(ctx, cfg)
	default:
		fmt.Fprintln(os.Stderr, "usage: aura objectstore {bootstrap}")
		os.Exit(1)
	}
}

func objectStoreBootstrap(ctx context.Context, cfg *config.Config) {
	backend := strings.TrimSpace(cfg.ObjectStoreBackend)
	switch backend {
	case "", "garage", "s3":
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		store, err := objectstorepkg.NewS3(ctx, objectstorepkg.S3Config{
			Endpoint:  cfg.ObjectStoreEndpoint,
			Region:    cfg.ObjectStoreRegion,
			AccessKey: cfg.ObjectStoreAccessKey,
			SecretKey: cfg.ObjectStoreSecretKey,
			PathStyle: cfg.ObjectStorePathStyle,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "objectstore bootstrap:", err)
			os.Exit(1)
		}
		if err := store.ConfigureBrowserUploadCORS(ctx, cfg.ObjectStoreBucket); err != nil {
			fmt.Fprintln(os.Stderr, "objectstore bootstrap:", err)
			os.Exit(1)
		}
		fmt.Println("ok: objectstore browser-upload CORS configured")
	case "filesystem-dev", "fake":
		fmt.Printf("ok: objectstore bootstrap skipped for %s backend\n", backend)
	default:
		fmt.Fprintf(os.Stderr, "objectstore bootstrap: unsupported backend %q\n", backend)
		os.Exit(1)
	}
}
