package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config describes one backup export. Secrets are used only by command-level
// uploaders; the archive builder itself only needs paths and bucket naming.
type Config struct {
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string

	EnvPath    string
	DBPath     string
	WikiPath   string
	SkillsPath string
	LogDir     string
	AuditPaths []string
}

type Result struct {
	Bucket string
	Key    string
	Bytes  int64
	Files  int
}

type ArtifactResult struct {
	Category string `json:"category"`
	Bucket   string `json:"bucket"`
	Key      string `json:"key"`
	Bytes    int64  `json:"bytes"`
	Files    int    `json:"files"`
}

type ArtifactSetResult struct {
	Bucket    string           `json:"bucket"`
	Timestamp string           `json:"timestamp"`
	Objects   []ArtifactResult `json:"objects"`
}

type Uploader interface {
	PutObject(ctx context.Context, bucket, key string, body io.Reader) error
}

func (c Config) Redacted() string {
	access := ""
	if strings.TrimSpace(c.AccessKey) != "" {
		access = "(configured)"
	}
	secret := ""
	if strings.TrimSpace(c.SecretKey) != "" {
		secret = "(configured)"
	}
	return fmt.Sprintf("endpoint=%s region=%s bucket=%s access_key=%s secret_key=%s", c.Endpoint, c.Region, c.Bucket, access, secret)
}

func Export(ctx context.Context, cfg Config, uploader Uploader, now time.Time) (Result, error) {
	key := fmt.Sprintf("backups/%s/aura-backup.tar.gz", now.UTC().Format("2006-01-02-150405"))
	return exportArchive(ctx, cfg, uploader, key, writeFullRestoreArchive)
}

func ExportArtifactSet(ctx context.Context, cfg Config, uploader Uploader, now time.Time) (ArtifactSetResult, error) {
	if strings.TrimSpace(cfg.Bucket) == "" {
		return ArtifactSetResult{}, fmt.Errorf("bucket is required")
	}
	ts := now.UTC().Format("2006-01-02-150405")
	specs := []struct {
		category string
		key      string
		write    archiveWriter
	}{
		{"full_restore", fmt.Sprintf("backups/%s/aura-backup.tar.gz", ts), writeFullRestoreArchive},
		{"source_originals", fmt.Sprintf("artifacts/%s/source-originals.tar.gz", ts), writeSourceOriginalsArchive},
		{"extractions", fmt.Sprintf("artifacts/%s/extractions.tar.gz", ts), writeExtractionsArchive},
		{"memory_snapshot", fmt.Sprintf("artifacts/%s/memory-snapshot.tar.gz", ts), writeMemorySnapshotArchive},
		{"embedding_index", fmt.Sprintf("artifacts/%s/embedding-index.tar.gz", ts), writeEmbeddingIndexArchive},
		{"audit_bundle", fmt.Sprintf("artifacts/%s/audit-bundle.tar.gz", ts), writeAuditBundleArchive},
	}

	res := ArtifactSetResult{Bucket: cfg.Bucket, Timestamp: ts}
	for _, spec := range specs {
		uploaded, err := exportArchive(ctx, cfg, uploader, spec.key, spec.write)
		if err != nil {
			return res, fmt.Errorf("export %s: %w", spec.category, err)
		}
		if uploaded.Files == 0 && spec.category != "full_restore" {
			continue
		}
		res.Objects = append(res.Objects, ArtifactResult{
			Category: spec.category,
			Bucket:   uploaded.Bucket,
			Key:      uploaded.Key,
			Bytes:    uploaded.Bytes,
			Files:    uploaded.Files,
		})
	}

	manifestKey := fmt.Sprintf("artifacts/%s/manifest.json", ts)
	res.Objects = append(res.Objects, ArtifactResult{
		Category: "manifest",
		Bucket:   cfg.Bucket,
		Key:      manifestKey,
		Files:    1,
	})
	manifest, err := marshalManifestWithStableSize(&res)
	if err != nil {
		return res, fmt.Errorf("marshal manifest: %w", err)
	}
	if err := uploader.PutObject(ctx, cfg.Bucket, manifestKey, strings.NewReader(string(manifest)+"\n")); err != nil {
		return res, fmt.Errorf("upload manifest: %w", err)
	}
	return res, nil
}

type archiveWriter func(io.Writer, Config) (int, error)

func marshalManifestWithStableSize(res *ArtifactSetResult) ([]byte, error) {
	if res == nil || len(res.Objects) == 0 {
		return json.MarshalIndent(res, "", "  ")
	}
	last := len(res.Objects) - 1
	var manifest []byte
	for i := 0; i < 8; i++ {
		var err error
		manifest, err = json.MarshalIndent(res, "", "  ")
		if err != nil {
			return nil, err
		}
		size := int64(len(manifest) + 1)
		if res.Objects[last].Bytes == size {
			return manifest, nil
		}
		res.Objects[last].Bytes = size
	}
	return json.MarshalIndent(res, "", "  ")
}

func exportArchive(ctx context.Context, cfg Config, uploader Uploader, key string, write archiveWriter) (Result, error) {
	if uploader == nil {
		return Result{}, fmt.Errorf("uploader is required")
	}
	if strings.TrimSpace(cfg.Bucket) == "" {
		return Result{}, fmt.Errorf("bucket is required")
	}

	tmp, err := os.CreateTemp("", "aura-backup-*.tar.gz")
	if err != nil {
		return Result{}, fmt.Errorf("create temp archive: %w", err)
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	files, err := write(tmp, cfg)
	if err != nil {
		return Result{}, fmt.Errorf("build backup archive: %w", err)
	}
	size, err := tmp.Seek(0, io.SeekCurrent)
	if err != nil {
		return Result{}, fmt.Errorf("stat temp archive: %w", err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return Result{}, fmt.Errorf("rewind temp archive: %w", err)
	}
	if err := uploader.PutObject(ctx, cfg.Bucket, key, tmp); err != nil {
		return Result{}, fmt.Errorf("upload backup: %w", err)
	}
	return Result{Bucket: cfg.Bucket, Key: key, Bytes: size, Files: files}, nil
}

func writeFullRestoreArchive(w io.Writer, cfg Config) (int, error) {
	gz := gzip.NewWriter(w)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	total := 0
	addFile := func(src, name string) error {
		n, err := addPath(tw, src, name)
		total += n
		return err
	}
	if err := addFile(cfg.EnvPath, "env/.env"); err != nil {
		return total, err
	}
	if err := addFile(cfg.DBPath, "data/"+filepath.Base(cfg.DBPath)); err != nil {
		return total, err
	}
	if err := addFile(cfg.WikiPath, "wiki"); err != nil {
		return total, err
	}
	if err := addFile(cfg.SkillsPath, "skills"); err != nil {
		return total, err
	}
	return total, nil
}

func writeSourceOriginalsArchive(w io.Writer, cfg Config) (int, error) {
	return writeFilteredSourceArchive(w, cfg, func(name string) bool {
		return strings.HasPrefix(name, "original.") || name == "source.json"
	})
}

func writeExtractionsArchive(w io.Writer, cfg Config) (int, error) {
	return writeFilteredSourceArchive(w, cfg, func(name string) bool {
		if name == "ocr.md" || name == "ocr.json" || name == "extract.md" || name == "extract.json" {
			return true
		}
		lower := strings.ToLower(name)
		return strings.HasPrefix(lower, "clean") && strings.HasSuffix(lower, ".md") ||
			strings.HasPrefix(lower, "assets/")
	})
}

func writeMemorySnapshotArchive(w io.Writer, cfg Config) (int, error) {
	gz := gzip.NewWriter(w)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	wiki := strings.TrimSpace(cfg.WikiPath)
	if wiki == "" {
		return 0, nil
	}
	info, err := os.Stat(wiki)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	if !info.IsDir() {
		return addPath(tw, wiki, "memory/wiki")
	}
	return addFilteredDir(tw, wiki, "memory/wiki", func(rel string, d os.DirEntry) bool {
		if d.IsDir() {
			return rel == "." || rel != "raw" && !strings.HasPrefix(rel, "raw/")
		}
		return !strings.HasPrefix(rel, "raw/")
	})
}

func writeEmbeddingIndexArchive(w io.Writer, cfg Config) (int, error) {
	gz := gzip.NewWriter(w)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	total := 0
	add := func(src, name string) error {
		n, err := addPath(tw, src, name)
		total += n
		return err
	}
	if strings.TrimSpace(cfg.DBPath) != "" {
		base := filepath.Base(cfg.DBPath)
		if err := add(cfg.DBPath, "embedding-index/"+base); err != nil {
			return total, err
		}
		for _, suffix := range []string{"-wal", "-shm"} {
			if err := add(cfg.DBPath+suffix, "embedding-index/"+base+suffix); err != nil {
				return total, err
			}
		}
	}
	if strings.TrimSpace(cfg.WikiPath) != "" {
		if err := add(filepath.Join(cfg.WikiPath, "index.md"), "embedding-index/wiki/index.md"); err != nil {
			return total, err
		}
	}
	return total, nil
}

func writeAuditBundleArchive(w io.Writer, cfg Config) (int, error) {
	gz := gzip.NewWriter(w)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	paths := append([]string{}, cfg.AuditPaths...)
	if strings.TrimSpace(cfg.LogDir) != "" {
		paths = append([]string{cfg.LogDir}, paths...)
	}

	total := 0
	seen := map[string]bool{}
	for _, src := range paths {
		src = strings.TrimSpace(src)
		if src == "" || seen[src] {
			continue
		}
		seen[src] = true
		name := "audit/" + filepath.Base(src)
		n, err := addPath(tw, src, name)
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func writeFilteredSourceArchive(w io.Writer, cfg Config, include func(name string) bool) (int, error) {
	gz := gzip.NewWriter(w)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	raw := filepath.Join(strings.TrimSpace(cfg.WikiPath), "raw")
	if strings.TrimSpace(cfg.WikiPath) == "" {
		return 0, nil
	}
	return addFilteredDir(tw, raw, "sources", func(rel string, d os.DirEntry) bool {
		if rel == "." {
			return true
		}
		if d.IsDir() {
			return true
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) < 2 {
			return false
		}
		name := strings.Join(parts[1:], "/")
		return include(name)
	})
}

func addPath(tw *tar.Writer, src, archiveName string) (int, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return 0, nil
	}
	info, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	if !info.IsDir() {
		if err := addOneFile(tw, src, archiveName, info); err != nil {
			return 0, err
		}
		return 1, nil
	}

	count := 0
	err = filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(filepath.Join(archiveName, rel))
		if err := addOneFile(tw, path, name, info); err != nil {
			return err
		}
		count++
		return nil
	})
	return count, err
}

func addFilteredDir(tw *tar.Writer, src, archiveName string, include func(rel string, d os.DirEntry) bool) (int, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return 0, nil
	}
	info, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	if !info.IsDir() {
		return 0, fmt.Errorf("%s is not a directory", src)
	}
	count := 0
	err = filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)
		if include != nil && !include(relSlash, d) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		name := filepath.ToSlash(filepath.Join(archiveName, rel))
		if err := addOneFile(tw, path, name, info); err != nil {
			return err
		}
		count++
		return nil
	})
	return count, err
}

func addOneFile(tw *tar.Writer, src, name string, info os.FileInfo) error {
	file, err := os.Open(src)
	if err != nil {
		return err
	}
	defer file.Close()

	h, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	h.Name = filepath.ToSlash(name)
	if err := tw.WriteHeader(h); err != nil {
		return err
	}
	_, err = io.Copy(tw, io.LimitReader(file, info.Size()))
	return err
}
