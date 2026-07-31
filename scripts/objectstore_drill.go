//go:build ignore

// objectstore_drill exercises an authenticated Garage/S3 export-delete-restore lifecycle.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/objectstore"
)

const (
	maxDrillObjectBytes = 1 << 20
	maxDrillTotalBytes  = 4 << 20
)

type manifestObject struct {
	Key      string `json:"key"`
	File     string `json:"file"`
	MIMEType string `json:"mime_type"`
	Bytes    int64  `json:"bytes"`
	SHA256   string `json:"sha256"`
}

type backupManifest struct {
	SchemaVersion int              `json:"schema_version"`
	Bucket        string           `json:"bucket"`
	Prefix        string           `json:"prefix"`
	CreatedAt     string           `json:"created_at"`
	Objects       []manifestObject `json:"objects"`
}

type drillReport struct {
	Plane         string `json:"plane"`
	RPOSeconds    int64  `json:"rpo_seconds"`
	RTOMillis     int64  `json:"rto_ms"`
	RestoredItems int    `json:"restored_items"`
	RestoredBytes int64  `json:"restored_bytes"`
	ChecksumOK    bool   `json:"checksum_ok"`
	CleanupOK     bool   `json:"cleanup_ok"`
}

func requiredEnv(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		fmt.Fprintf(os.Stderr, "objectstore drill: %s is required\n", name)
		os.Exit(2)
	}
	return value
}

func main() {
	var prefix, backupDir, output string
	flag.StringVar(&prefix, "prefix", "", "owned fixture prefix (must begin dr/)")
	flag.StringVar(&backupDir, "backup-dir", "", "empty directory for exported fixture bytes")
	flag.StringVar(&output, "output", "", "report JSON path")
	flag.Parse()
	if !strings.HasPrefix(prefix, "dr/") || strings.Contains(prefix, "..") || !strings.HasSuffix(prefix, "/") {
		fmt.Fprintln(os.Stderr, "objectstore drill: prefix must have shape dr/<owned-id>/")
		os.Exit(2)
	}
	if backupDir == "" || output == "" {
		fmt.Fprintln(os.Stderr, "objectstore drill: --backup-dir and --output are required")
		os.Exit(2)
	}
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	bucket := requiredEnv("AURA_OBJECTSTORE_BUCKET")
	store, err := objectstore.NewS3(ctx, objectstore.S3Config{
		Endpoint:       requiredEnv("AURA_OBJECTSTORE_ENDPOINT"),
		Region:         requiredEnv("AURA_OBJECTSTORE_REGION"),
		AccessKey:      requiredEnv("AURA_OBJECTSTORE_ACCESS_KEY"),
		SecretKey:      requiredEnv("AURA_OBJECTSTORE_SECRET_KEY"),
		PathStyle:      true,
		PublicEndpoint: os.Getenv("AURA_OBJECTSTORE_PUBLIC_ENDPOINT"),
	})
	if err != nil {
		fatal(err)
	}

	fixtures := map[string]struct {
		body string
		mime string
	}{
		prefix + "sentinel.txt":     {body: "aura-objectstore-dr-sentinel\n", mime: "text/plain"},
		prefix + "nested/state.bin": {body: "\x00aura-dr-state\xff", mime: "application/octet-stream"},
	}
	defer cleanupPrefix(context.WithoutCancel(ctx), store, bucket, prefix)
	for key, fixture := range fixtures {
		if _, err := store.Put(ctx, objectstore.ObjectRef{Bucket: bucket, Key: key},
			strings.NewReader(fixture.body), objectstore.PutOptions{
				MIMEType: fixture.mime, Size: int64(len(fixture.body)),
			}); err != nil {
			fatal(fmt.Errorf("seed %s: %w", key, err))
		}
	}

	manifest, err := exportPrefix(ctx, store, bucket, prefix, backupDir)
	if err != nil {
		fatal(err)
	}
	if len(manifest.Objects) != len(fixtures) {
		fatal(fmt.Errorf("exported %d fixture objects, want %d", len(manifest.Objects), len(fixtures)))
	}
	if err := writeJSON(filepath.Join(backupDir, "manifest.json"), manifest); err != nil {
		fatal(err)
	}
	if err := deletePrefix(ctx, store, bucket, prefix); err != nil {
		fatal(err)
	}
	if err := assertPrefixCount(ctx, store, bucket, prefix, 0); err != nil {
		fatal(err)
	}

	restoreStart := time.Now()
	restoredBytes, err := restoreManifest(ctx, store, manifest, backupDir)
	if err != nil {
		fatal(err)
	}
	if err := verifyManifest(ctx, store, manifest); err != nil {
		fatal(err)
	}
	rto := time.Since(restoreStart)
	if err := deletePrefix(ctx, store, bucket, prefix); err != nil {
		fatal(err)
	}
	if err := assertPrefixCount(ctx, store, bucket, prefix, 0); err != nil {
		fatal(err)
	}

	report := drillReport{
		Plane:         "garage",
		RPOSeconds:    0,
		RTOMillis:     rto.Milliseconds(),
		RestoredItems: len(manifest.Objects),
		RestoredBytes: restoredBytes,
		ChecksumOK:    true,
		CleanupOK:     true,
	}
	if err := writeJSON(output, report); err != nil {
		fatal(err)
	}
}

func exportPrefix(
	ctx context.Context,
	store objectstore.Store,
	bucket, prefix, backupDir string,
) (backupManifest, error) {
	objects, err := store.List(ctx, objectstore.ListRequest{Bucket: bucket, Prefix: prefix, Limit: 100})
	if err != nil {
		return backupManifest{}, fmt.Errorf("list fixture prefix: %w", err)
	}
	manifest := backupManifest{
		SchemaVersion: 1, Bucket: bucket, Prefix: prefix,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Objects:   make([]manifestObject, 0, len(objects)),
	}
	var total int64
	for index, info := range objects {
		reader, attrs, err := store.Get(ctx, info.Ref)
		if err != nil {
			return backupManifest{}, fmt.Errorf("get %s: %w", info.Ref.Key, err)
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, maxDrillObjectBytes+1))
		closeErr := reader.Close()
		if readErr != nil {
			return backupManifest{}, fmt.Errorf("read %s: %w", info.Ref.Key, readErr)
		}
		if closeErr != nil {
			return backupManifest{}, fmt.Errorf("close %s: %w", info.Ref.Key, closeErr)
		}
		if len(data) > maxDrillObjectBytes {
			return backupManifest{}, fmt.Errorf("fixture object %s exceeds %d bytes", info.Ref.Key, maxDrillObjectBytes)
		}
		total += int64(len(data))
		if total > maxDrillTotalBytes {
			return backupManifest{}, fmt.Errorf("fixture prefix exceeds %d bytes", maxDrillTotalBytes)
		}
		name := fmt.Sprintf("%06d.bin", index)
		if err := os.WriteFile(filepath.Join(backupDir, name), data, 0o600); err != nil {
			return backupManifest{}, err
		}
		sum := sha256.Sum256(data)
		manifest.Objects = append(manifest.Objects, manifestObject{
			Key: info.Ref.Key, File: name, MIMEType: attrs.MIMEType,
			Bytes: int64(len(data)), SHA256: hex.EncodeToString(sum[:]),
		})
	}
	return manifest, nil
}

func restoreManifest(
	ctx context.Context,
	store objectstore.Store,
	manifest backupManifest,
	backupDir string,
) (int64, error) {
	var total int64
	for _, item := range manifest.Objects {
		data, err := os.ReadFile(filepath.Join(backupDir, item.File))
		if err != nil {
			return 0, err
		}
		if int64(len(data)) != item.Bytes {
			return 0, fmt.Errorf("backup byte count for %s changed", item.Key)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != item.SHA256 {
			return 0, fmt.Errorf("backup checksum for %s changed", item.Key)
		}
		if _, err := store.Put(ctx, objectstore.ObjectRef{Bucket: manifest.Bucket, Key: item.Key},
			bytes.NewReader(data), objectstore.PutOptions{
				MIMEType: item.MIMEType, Size: int64(len(data)),
			}); err != nil {
			return 0, fmt.Errorf("restore %s: %w", item.Key, err)
		}
		total += int64(len(data))
	}
	return total, nil
}

func verifyManifest(ctx context.Context, store objectstore.Store, manifest backupManifest) error {
	for _, item := range manifest.Objects {
		reader, _, err := store.Get(ctx, objectstore.ObjectRef{Bucket: manifest.Bucket, Key: item.Key})
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, item.Bytes+1))
		closeErr := reader.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		sum := sha256.Sum256(data)
		if int64(len(data)) != item.Bytes || hex.EncodeToString(sum[:]) != item.SHA256 {
			return fmt.Errorf("restored checksum mismatch for %s", item.Key)
		}
	}
	return nil
}

func cleanupPrefix(ctx context.Context, store objectstore.Store, bucket, prefix string) {
	cleanupCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_ = deletePrefix(cleanupCtx, store, bucket, prefix)
}

func deletePrefix(ctx context.Context, store objectstore.Store, bucket, prefix string) error {
	if !strings.HasPrefix(prefix, "dr/") || strings.Contains(prefix, "..") {
		return fmt.Errorf("refusing cleanup outside owned dr prefix: %q", prefix)
	}
	objects, err := store.List(ctx, objectstore.ListRequest{Bucket: bucket, Prefix: prefix, Limit: 100})
	if err != nil {
		return err
	}
	for _, info := range objects {
		if !strings.HasPrefix(info.Ref.Key, prefix) {
			return fmt.Errorf("list returned key outside fixture prefix: %s", info.Ref.Key)
		}
		if err := store.Delete(ctx, info.Ref); err != nil {
			return err
		}
	}
	return nil
}

func assertPrefixCount(ctx context.Context, store objectstore.Store, bucket, prefix string, want int) error {
	objects, err := store.List(ctx, objectstore.ListRequest{Bucket: bucket, Prefix: prefix, Limit: 100})
	if err != nil {
		return err
	}
	if len(objects) != want {
		return fmt.Errorf("fixture prefix contains %d objects, want %d", len(objects), want)
	}
	return nil
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "objectstore drill:", err)
	os.Exit(1)
}
