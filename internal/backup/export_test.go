package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	auradb "github.com/aura/aura/internal/db"
)

type fakeUploader struct {
	bucket string
	key    string
	body   []byte
	puts   []fakePut
}

type fakePut struct {
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
	f.puts = append(f.puts, fakePut{bucket: bucket, key: key, body: data})
	return nil
}

func TestExportNamesAndArchivesState(t *testing.T) {
	dir := t.TempDir()
	mustCreateSQLiteDB(t, filepath.Join(dir, "aura.db"))
	mustWrite(t, filepath.Join(dir, "wiki", "page.md"), []byte("# Page\n"))
	mustWrite(t, filepath.Join(dir, "skills", "demo", "SKILL.md"), []byte("---\nname: demo\n---\n"))
	mustWrite(t, filepath.Join(dir, "wiki", "sources", "source-1", "ocr.md"), []byte("ocr"))

	uploader := &fakeUploader{}
	res, err := Export(context.Background(), Config{
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
	for _, want := range []string{"data/aura.db", "wiki/page.md", "skills/demo/SKILL.md", "wiki/sources/source-1/ocr.md"} {
		if !containsString(names, want) {
			t.Fatalf("archive missing %s in %v", want, names)
		}
	}
}

func TestExportArtifactSetUploadsCategorizedArchives(t *testing.T) {
	dir := t.TempDir()
	mustCreateSQLiteDB(t, filepath.Join(dir, "aura.db"))
	mustWrite(t, filepath.Join(dir, "wiki", "index.md"), []byte("# Index\n"))
	mustWrite(t, filepath.Join(dir, "wiki", "page.md"), []byte("# Page\n"))
	mustWrite(t, filepath.Join(dir, "wiki", "raw", "src_0123456789abcdef", "original.pdf"), []byte("%PDF"))
	mustWrite(t, filepath.Join(dir, "wiki", "raw", "src_0123456789abcdef", "source.json"), []byte(`{"id":"src_0123456789abcdef"}`))
	mustWrite(t, filepath.Join(dir, "wiki", "raw", "src_0123456789abcdef", "ocr.md"), []byte("ocr"))
	mustWrite(t, filepath.Join(dir, "wiki", "raw", "src_0123456789abcdef", "extract.md"), []byte("extract"))
	mustWrite(t, filepath.Join(dir, "wiki", "raw", "src_0123456789abcdef", "cleaned.md"), []byte("clean"))
	mustWrite(t, filepath.Join(dir, "skills", "demo", "SKILL.md"), []byte("---\nname: demo\n---\n"))
	mustWrite(t, filepath.Join(dir, "logs", "aura.log"), []byte("log"))
	mustWrite(t, filepath.Join(dir, "reports", "e2e.json"), []byte(`{"ok":true}`))

	uploader := &fakeUploader{}
	res, err := ExportArtifactSet(context.Background(), Config{
		DBPath:     filepath.Join(dir, "aura.db"),
		WikiPath:   filepath.Join(dir, "wiki"),
		SkillsPath: filepath.Join(dir, "skills"),
		LogDir:     filepath.Join(dir, "logs"),
		AuditPaths: []string{filepath.Join(dir, "reports")},
		Bucket:     "aura-artifacts",
	}, uploader, time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC))
	if err != nil {
		t.Fatalf("ExportArtifactSet() error = %v", err)
	}
	for _, want := range []string{
		"backups/2026-05-06-070809/aura-backup.tar.gz",
		"artifacts/2026-05-06-070809/source-originals.tar.gz",
		"artifacts/2026-05-06-070809/extractions.tar.gz",
		"artifacts/2026-05-06-070809/memory-snapshot.tar.gz",
		"artifacts/2026-05-06-070809/embedding-index.tar.gz",
		"artifacts/2026-05-06-070809/audit-bundle.tar.gz",
		"artifacts/2026-05-06-070809/manifest.json",
	} {
		if !hasObject(res.Objects, want) {
			t.Fatalf("missing uploaded object %s in %+v", want, res.Objects)
		}
	}
	assertArchiveContains(t, uploader, "artifacts/2026-05-06-070809/source-originals.tar.gz", "sources/src_0123456789abcdef/original.pdf")
	assertArchiveContains(t, uploader, "artifacts/2026-05-06-070809/source-originals.tar.gz", "sources/src_0123456789abcdef/source.json")
	assertArchiveContains(t, uploader, "artifacts/2026-05-06-070809/extractions.tar.gz", "sources/src_0123456789abcdef/ocr.md")
	assertArchiveContains(t, uploader, "artifacts/2026-05-06-070809/extractions.tar.gz", "sources/src_0123456789abcdef/extract.md")
	assertArchiveContains(t, uploader, "artifacts/2026-05-06-070809/extractions.tar.gz", "sources/src_0123456789abcdef/cleaned.md")
	assertArchiveContains(t, uploader, "artifacts/2026-05-06-070809/memory-snapshot.tar.gz", "memory/wiki/page.md")
	assertArchiveNotContains(t, uploader, "artifacts/2026-05-06-070809/memory-snapshot.tar.gz", "memory/wiki/raw/src_0123456789abcdef/ocr.md")
	assertArchiveContains(t, uploader, "artifacts/2026-05-06-070809/embedding-index.tar.gz", "embedding-index/aura.db")
	assertArchiveNotContains(t, uploader, "artifacts/2026-05-06-070809/embedding-index.tar.gz", "embedding-index/aura.db-wal")
	assertArchiveContains(t, uploader, "artifacts/2026-05-06-070809/embedding-index.tar.gz", "embedding-index/wiki/index.md")
	assertArchiveContains(t, uploader, "artifacts/2026-05-06-070809/audit-bundle.tar.gz", "audit/logs/aura.log")
	assertArchiveContains(t, uploader, "artifacts/2026-05-06-070809/audit-bundle.tar.gz", "audit/reports/e2e.json")

	var manifest ArtifactSetResult
	manifestBody := bodyForKey(t, uploader, "artifacts/2026-05-06-070809/manifest.json")
	if err := json.Unmarshal(manifestBody, &manifest); err != nil {
		t.Fatalf("manifest json: %v", err)
	}
	if !hasObject(manifest.Objects, "artifacts/2026-05-06-070809/manifest.json") {
		t.Fatalf("manifest should list itself: %+v", manifest.Objects)
	}
	for _, obj := range manifest.Objects {
		if obj.Key == "artifacts/2026-05-06-070809/manifest.json" && obj.Bytes != int64(len(manifestBody)) {
			t.Fatalf("manifest bytes = %d, actual %d", obj.Bytes, len(manifestBody))
		}
	}
}

func TestAddOneFileToleratesGrowingLiveFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aura.log")
	mustWrite(t, path, []byte("before"))
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open append: %v", err)
	}
	if _, err := file.WriteString("-after"); err != nil {
		_ = file.Close()
		t.Fatalf("append: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close append: %v", err)
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := addOneFile(tw, path, "audit/aura.log", info); err != nil {
		t.Fatalf("addOneFile() error = %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	tr := tar.NewReader(bytes.NewReader(buf.Bytes()))
	h, err := tr.Next()
	if err != nil {
		t.Fatalf("tar next: %v", err)
	}
	if h.Size != int64(len("before")) {
		t.Fatalf("header size = %d, want stale stat size", h.Size)
	}
	body, err := io.ReadAll(tr)
	if err != nil {
		t.Fatalf("read tar body: %v", err)
	}
	if string(body) != "before" {
		t.Fatalf("body = %q, want stale-size snapshot", body)
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

func mustCreateSQLiteDB(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	db, err := auradb.Open(path)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE backup_smoke (id INTEGER PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		_ = db.Close()
		t.Fatalf("create backup smoke table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO backup_smoke (value) VALUES ('ok')`); err != nil {
		_ = db.Close()
		t.Fatalf("insert backup smoke row: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close sqlite db: %v", err)
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
	defer func() { _ = gz.Close() }()
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

func hasObject(objects []ArtifactResult, key string) bool {
	for _, obj := range objects {
		if obj.Key == key {
			return true
		}
	}
	return false
}

func assertArchiveContains(t *testing.T, uploader *fakeUploader, key, name string) {
	t.Helper()
	names := tarNames(t, bodyForKey(t, uploader, key))
	if !containsString(names, name) {
		t.Fatalf("%s missing %s in %v", key, name, names)
	}
}

func assertArchiveNotContains(t *testing.T, uploader *fakeUploader, key, name string) {
	t.Helper()
	names := tarNames(t, bodyForKey(t, uploader, key))
	if containsString(names, name) {
		t.Fatalf("%s should not contain %s in %v", key, name, names)
	}
}

func bodyForKey(t *testing.T, uploader *fakeUploader, key string) []byte {
	t.Helper()
	for _, put := range uploader.puts {
		if put.key == key {
			return put.body
		}
	}
	t.Fatalf("upload %s not found in %+v", key, uploader.puts)
	return nil
}

// errWriter is an io.Writer that always returns an error — simulates disk full.
type errWriter struct{ err error }

func (e *errWriter) Write(_ []byte) (int, error) { return 0, e.err }

func TestWriteArchiveCloseErrorPropagated(t *testing.T) {
	w := &errWriter{err: errors.New("disk full")}
	_, err := writeFullRestoreArchive(w, Config{})
	if err == nil {
		t.Fatal("expected error when underlying writer fails (simulates disk-full at gzip/tar close), got nil")
	}
}
