package main

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/objectstore"
)

func buildObjectStore(ctx context.Context, cfg *config.Config) (objectstore.Store, error) {
	backend := strings.TrimSpace(cfg.ObjectStoreBackend)
	switch backend {
	case "", "garage", "s3":
		return objectstore.NewS3(ctx, objectstore.S3Config{
			Endpoint:       cfg.ObjectStoreEndpoint,
			PublicEndpoint: cfg.ObjectStorePublicEndpoint,
			Region:         cfg.ObjectStoreRegion,
			AccessKey:      cfg.ObjectStoreAccessKey,
			SecretKey:      cfg.ObjectStoreSecretKey,
			PathStyle:      cfg.ObjectStorePathStyle,
		})
	case "filesystem-dev":
		root, err := filesystemObjectRoot(cfg)
		if err != nil {
			return nil, err
		}
		return objectstore.NewFilesystem(root), nil
	case "fake":
		return objectstore.NewFake(), nil
	default:
		return nil, fmt.Errorf("unsupported object store backend %q", backend)
	}
}

func filesystemObjectRoot(cfg *config.Config) (string, error) {
	endpoint := strings.TrimSpace(cfg.ObjectStoreEndpoint)
	if endpoint == "" || isDefaultObjectStoreEndpoint(endpoint) {
		return filepath.Join(cfg.RunDir, "objectstore"), nil
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("filesystem object store endpoint: %w", err)
	}
	if u.Scheme != "file" {
		return "", fmt.Errorf("filesystem object store endpoint must use file://")
	}
	root := filepath.FromSlash(u.Path)
	if runtime.GOOS == "windows" && len(root) >= 3 && root[0] == filepath.Separator && root[2] == ':' {
		root = root[1:]
	}
	if root == "" {
		return "", fmt.Errorf("filesystem object store endpoint path is empty")
	}
	return root, nil
}

func isDefaultObjectStoreEndpoint(endpoint string) bool {
	switch strings.TrimRight(endpoint, "/") {
	case "http://garage:3900", "http://127.0.0.1:3900":
		return true
	default:
		return false
	}
}
