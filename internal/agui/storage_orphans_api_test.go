package agui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/objectstore"
)

func TestStorageOrphansAPIDryRun(t *testing.T) {
	storage := &fakeStorageOrphans{
		dryRunResp: documents.StorageOrphanReport{
			Bucket:      "bucket",
			Prefix:      "identity/a/",
			DryRunToken: "token-1",
			Orphans: []objectstore.ObjectInfo{{
				Ref: objectstore.ObjectRef{Bucket: "bucket", Key: "identity/a/orphan"},
			}},
		},
	}
	s := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
	s.SetStorageOrphans(storage)

	req := httptest.NewRequest(http.MethodGet, "/api/storage/orphans?bucket=bucket&prefix=identity/a/", nil)
	req = withPrincipal(req, documentAPIIdentityID)
	rec := httptest.NewRecorder()

	s.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if storage.dryRunReq.Bucket != "bucket" || storage.dryRunReq.Prefix != "identity/a/" {
		t.Fatalf("dry-run request = %#v", storage.dryRunReq)
	}
	var got documents.StorageOrphanReport
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.DryRunToken != "token-1" || len(got.Orphans) != 1 {
		t.Fatalf("response = %#v", got)
	}
}

func TestStorageOrphansAPICleanup(t *testing.T) {
	storage := &fakeStorageOrphans{
		cleanupResp: documents.StorageOrphanReport{Bucket: "bucket", Prefix: "identity/a/", DryRunToken: "token-1", Deleted: 2},
	}
	s := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
	s.SetStorageOrphans(storage)

	req := httptest.NewRequest(http.MethodPost, "/api/storage/orphans/cleanup", strings.NewReader(`{"bucket":"bucket","prefix":"identity/a/","dry_run_token":"token-1","confirm":"DELETE ORPHAN OBJECTS"}`))
	req = withPrincipal(req, documentAPIIdentityID)
	rec := httptest.NewRecorder()

	s.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if storage.cleanupReq.DryRunToken != "token-1" || storage.cleanupReq.Confirm != documents.StorageOrphanCleanupConfirmation {
		t.Fatalf("cleanup request = %#v", storage.cleanupReq)
	}
	var got documents.StorageOrphanReport
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Deleted != 2 {
		t.Fatalf("deleted = %d", got.Deleted)
	}
}

func TestStorageOrphansAPIRequiresServiceAndPrincipal(t *testing.T) {
	s := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
	req := httptest.NewRequest(http.MethodGet, "/api/storage/orphans?bucket=bucket", nil)
	rec := httptest.NewRecorder()

	s.Mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unwired status = %d, want 503", rec.Code)
	}

	s.SetStorageOrphans(&fakeStorageOrphans{})
	rec = httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want 401", rec.Code)
	}
}

type fakeStorageOrphans struct {
	dryRunReq   documents.StorageOrphanRequest
	cleanupReq  documents.StorageOrphanCleanupRequest
	dryRunResp  documents.StorageOrphanReport
	cleanupResp documents.StorageOrphanReport
}

func (f *fakeStorageOrphans) DryRun(_ context.Context, req documents.StorageOrphanRequest) (documents.StorageOrphanReport, error) {
	f.dryRunReq = req
	return f.dryRunResp, nil
}

func (f *fakeStorageOrphans) Cleanup(_ context.Context, req documents.StorageOrphanCleanupRequest) (documents.StorageOrphanReport, error) {
	f.cleanupReq = req
	return f.cleanupResp, nil
}
