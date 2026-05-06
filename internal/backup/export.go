package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
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
}

type Result struct {
	Bucket string
	Key    string
	Bytes  int64
	Files  int
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
	if uploader == nil {
		return Result{}, fmt.Errorf("uploader is required")
	}
	if strings.TrimSpace(cfg.Bucket) == "" {
		return Result{}, fmt.Errorf("bucket is required")
	}
	key := fmt.Sprintf("backups/%s/aura-backup.tar.gz", now.UTC().Format("2006-01-02-150405"))

	tmp, err := os.CreateTemp("", "aura-backup-*.tar.gz")
	if err != nil {
		return Result{}, fmt.Errorf("create temp archive: %w", err)
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	files, err := writeArchive(tmp, cfg)
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

func writeArchive(w io.Writer, cfg Config) (int, error) {
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
	_, err = io.Copy(tw, file)
	return err
}
