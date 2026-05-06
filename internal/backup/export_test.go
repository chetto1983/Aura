package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeUploader struct {
	bucket string
	key    string
	body   []byte
}

func (f *fakeUploader) PutObject(ctx context.Context, bucket, key string, body io.Reader) error {
	f.bucket = bucket
	f.key = key
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	f.body = data
	return nil
}

func TestExportNamesAndArchivesState(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".env"), []byte("LLM_API_KEY=secret\n"))
	mustWrite(t, filepath.Join(dir, "aura.db"), []byte("sqlite"))
	mustWrite(t, filepath.Join(dir, "wiki", "page.md"), []byte("# Page\n"))
	mustWrite(t, filepath.Join(dir, "skills", "demo", "SKILL.md"), []byte("---\nname: demo\n---\n"))
	mustWrite(t, filepath.Join(dir, "wiki", "sources", "source-1", "ocr.md"), []byte("ocr"))

	uploader := &fakeUploader{}
	res, err := Export(context.Background(), Config{
		EnvPath:    filepath.Join(dir, ".env"),
		DBPath:     filepath.Join(dir, "aura.db"),
		WikiPath:   filepath.Join(dir, "wiki"),
		SkillsPath: filepath.Join(dir, "skills"),
		Bucket:     "aura-artifacts",
	}, uploader, time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC))
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if res.Key != "backups/2026-05-06-070809/aura-backup.tar.gz" || uploader.key != res.Key {
		t.Fatalf("key = %q uploader=%q", res.Key, uploader.key)
	}
	if uploader.bucket != "aura-artifacts" {
		t.Fatalf("bucket = %q", uploader.bucket)
	}
	names := tarNames(t, uploader.body)
	for _, want := range []string{"env/.env", "data/aura.db", "wiki/page.md", "skills/demo/SKILL.md", "wiki/sources/source-1/ocr.md"} {
		if !containsString(names, want) {
			t.Fatalf("archive missing %s in %v", want, names)
		}
	}
}

func TestConfigRedaction(t *testing.T) {
	cfg := Config{
		Endpoint:  "http://garage:3900",
		Bucket:    "aura-artifacts",
		AccessKey: "GKsecret",
		SecretKey: "very-secret",
	}
	got := cfg.Redacted()
	if strings.Contains(got, cfg.AccessKey) || strings.Contains(got, cfg.SecretKey) {
		t.Fatalf("redacted config leaked secrets: %s", got)
	}
	if !strings.Contains(got, "aura-artifacts") || !strings.Contains(got, "(configured)") {
		t.Fatalf("redacted config missing useful detail: %s", got)
	}
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func tarNames(t *testing.T, data []byte) []string {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var out []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		out = append(out, h.Name)
	}
	return out
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
