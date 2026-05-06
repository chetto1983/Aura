package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aura/aura/internal/backup"
	"github.com/aura/aura/internal/config"
)

type fakeBackupService struct {
	objects []backup.Object
	export  backup.ArtifactSetResult
}

func (f *fakeBackupService) ListObjects(context.Context) ([]backup.Object, error) {
	return f.objects, nil
}

func (f *fakeBackupService) ExportArtifactSet(context.Context, time.Time) (backup.ArtifactSetResult, error) {
	return f.export, nil
}

func TestBackupList(t *testing.T) {
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	router := NewRouter(Deps{
		RuntimeConfig: testRuntimeConfigWithGarage(),
		Backups: &fakeBackupService{objects: []backup.Object{
			{Key: "backups/2026-05-06-112834/aura-backup.tar.gz", Size: 10, LastModified: now.Add(-time.Hour), Category: "full_restore", Timestamp: "2026-05-06-112834"},
			{Key: "artifacts/2026-05-06-145331/manifest.json", Size: 20, LastModified: now, Category: "manifest", Timestamp: "2026-05-06-145331"},
		}},
	})

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/backups", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var got BackupListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("json: %v", err)
	}
	if got.Bucket != "aura-artifacts" {
		t.Fatalf("bucket = %q", got.Bucket)
	}
	if len(got.Objects) != 2 || got.Objects[0].Key != "artifacts/2026-05-06-145331/manifest.json" {
		t.Fatalf("objects not sorted newest first: %+v", got.Objects)
	}
}

func TestBackupExport(t *testing.T) {
	router := NewRouter(Deps{
		RuntimeConfig: testRuntimeConfigWithGarage(),
		Backups: &fakeBackupService{export: backup.ArtifactSetResult{
			Bucket:    "aura-artifacts",
			Timestamp: "2026-05-06-145331",
			Objects: []backup.ArtifactResult{{
				Category: "manifest",
				Key:      "artifacts/2026-05-06-145331/manifest.json",
				Bytes:    1391,
				Files:    1,
			}},
		}},
	})

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/backups/export", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var got BackupExportResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("json: %v", err)
	}
	if got.Timestamp != "2026-05-06-145331" || len(got.Objects) != 1 || got.Objects[0].SizeBytes != 1391 {
		t.Fatalf("unexpected response: %+v", got)
	}
}

func TestBackupListRequiresConfig(t *testing.T) {
	router := NewRouter(Deps{})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/backups", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
}

func testRuntimeConfigWithGarage() *config.Config {
	return &config.Config{
		GarageS3Bucket: "aura-artifacts",
	}
}
