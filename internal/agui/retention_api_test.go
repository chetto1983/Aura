package agui

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/google/uuid"
)

func TestOwnerExporterManifestIsVersionedDeterministicAndVerified(t *testing.T) {
	source := &fakeOwnerExportSource{
		snapshot: ExportSnapshot{
			IdentitySnapshotID: "identity-snapshot", ConversationSnapshotID: "conversation-snapshot",
			ConversationJSON: []byte(`{"turns":[{"role":"user","content":"ciao"}]}`),
			Artifacts: []ExportArtifact{
				{ID: "b-id", Filename: "zeta.txt", Size: 4},
				{ID: "a-id", Filename: "caffè.txt", Size: 5},
			},
		},
		bodies: map[string][]byte{"a-id": []byte("hello"), "b-id": []byte("zeta")},
	}
	destination := NewMemoryExportDestination(1 << 20)
	exporter := &OwnerExporter{
		Source: source, Destination: destination, AuraVersion: "v1.2.3", PolicyVersion: "retention-v1",
		Now: func() time.Time { return time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC) },
	}
	result, err := exporter.Export(context.Background(), "owner", "conversation")
	if err != nil {
		t.Fatal(err)
	}
	if result.Manifest.ManifestVersion != ManifestVersion || result.Manifest.AuraVersion != "v1.2.3" || result.Manifest.PolicyVersion != "retention-v1" {
		t.Fatalf("manifest versions = %+v", result.Manifest)
	}
	paths := make([]string, 0, len(result.Manifest.Entries))
	for _, entry := range result.Manifest.Entries {
		if len(entry.SHA256) != 64 || entry.Size < 0 {
			t.Fatalf("invalid manifest entry %+v", entry)
		}
		paths = append(paths, entry.Path)
	}
	if !slices.IsSorted(paths) || !slices.Contains(paths, "assets/a-id/caffè.txt") || !slices.Contains(paths, "conversation.json") {
		t.Fatalf("manifest paths = %v", paths)
	}
	rc, err := destination.Open(context.Background(), result.ExportID)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil || len(reader.File) != len(result.Manifest.Entries)+1 {
		t.Fatalf("archive files = %d, err=%v", len(reader.File), err)
	}
}

func TestOwnerExporterExportDeleteWaitsForVerifiedPublish(t *testing.T) {
	source := &fakeOwnerExportSource{snapshot: ExportSnapshot{
		IdentitySnapshotID: "identity", ConversationSnapshotID: "conversation", ConversationJSON: []byte(`{"turns":[]}`),
	}}
	destination := NewMemoryExportDestination(1 << 20)
	deleter := &fakeOwnerDelete{}
	exporter := &OwnerExporter{Source: source, Destination: destination, Deleter: deleter, PolicyVersion: "v1"}
	if _, err := exporter.ExportDelete(context.Background(), "owner", "conversation"); err != nil {
		t.Fatal(err)
	}
	if deleter.calls != 1 {
		t.Fatalf("delete calls = %d", deleter.calls)
	}

	deleter.calls = 0
	destination.failPut = errors.New("publish failed")
	if _, err := exporter.ExportDelete(context.Background(), "owner", "conversation"); err == nil {
		t.Fatal("publish failure succeeded")
	}
	if deleter.calls != 0 {
		t.Fatalf("publish failure started %d deletes", deleter.calls)
	}
}

func TestOwnerExporterForeignOwnerReturnsNoDataAndNoDelete(t *testing.T) {
	source := &fakeOwnerExportSource{err: ErrOwnerExportNotFound}
	destination := NewMemoryExportDestination(1 << 20)
	deleter := &fakeOwnerDelete{}
	exporter := &OwnerExporter{Source: source, Destination: destination, Deleter: deleter, PolicyVersion: "v1"}
	if _, err := exporter.ExportDelete(context.Background(), "foreign", "conversation"); !errors.Is(err, ErrOwnerExportNotFound) {
		t.Fatalf("ExportDelete error = %v", err)
	}
	if deleter.calls != 0 || destination.Count() != 0 {
		t.Fatalf("foreign export writes/deletes = %d/%d", destination.Count(), deleter.calls)
	}
}

func TestOwnerExporterRejectsTraversalAndReplacedSize(t *testing.T) {
	for _, tc := range []struct {
		name     string
		filename string
		size     int64
		body     string
	}{
		{"traversal", "../escape", 1, "x"},
		{"backslash", `dir\\escape`, 1, "x"},
		{"replaced size", "safe.txt", 99, "short"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := &fakeOwnerExportSource{snapshot: ExportSnapshot{
				IdentitySnapshotID: "identity", ConversationSnapshotID: "conversation",
				ConversationJSON: []byte(`{}`), Artifacts: []ExportArtifact{{ID: "asset", Filename: tc.filename, Size: tc.size}},
			}, bodies: map[string][]byte{"asset": []byte(tc.body)}}
			destination := NewMemoryExportDestination(1 << 20)
			exporter := &OwnerExporter{Source: source, Destination: destination, PolicyVersion: "v1"}
			if _, err := exporter.Export(context.Background(), "owner", "conversation"); err == nil {
				t.Fatal("unsafe export succeeded")
			}
			if destination.Count() != 0 {
				t.Fatal("unsafe export published an archive")
			}
		})
	}
}

func TestOwnerExportAPIScopesExportAndDeleteToPrincipal(t *testing.T) {
	assetID := uuid.NewString()
	newServer := func(owner string) (*Server, *scriptedRunner, *fakeAssetService) {
		store := newOwnerConvStore(goodID, owner)
		run := &scriptedRunner{conv: store}
		assetService := &fakeAssetService{
			listResp: []assets.Asset{{
				ID: assetID, IdentityID: owner, ThreadID: goodID,
				FileName: "caffè.txt", MIMEType: "text/plain", SizeBytes: 5,
			}},
			openResp:  io.NopCloser(bytes.NewReader([]byte("hello"))),
			openAsset: assets.Asset{ID: assetID, IdentityID: owner},
		}
		server := NewServer(run, store, ServerConfig{})
		server.SetAssetService(assetService)
		return server, run, assetService
	}

	t.Run("owner export is a verified archive", func(t *testing.T) {
		server, run, assetService := newServer(localIdentityID)
		req := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/conversations/"+goodID+"/owner-export", nil), localIdentityID)
		rec := httptest.NewRecorder()
		server.Mux().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("owner export status = %d: %s", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Content-Type") != "application/zip" || len(rec.Header().Get("X-Aura-SHA256")) != 64 {
			t.Fatalf("owner export headers = %v", rec.Header())
		}
		archive, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
		if err != nil || len(archive.File) != 3 {
			t.Fatalf("owner export archive files = %d, err=%v", len(archive.File), err)
		}
		if run.deleteLifecycleCalls != 0 || assetService.listIdentityID != localIdentityID {
			t.Fatalf("read export delete/list identity = %d/%q", run.deleteLifecycleCalls, assetService.listIdentityID)
		}
	})

	t.Run("foreign export-delete returns no bytes and does not delete", func(t *testing.T) {
		server, run, assetService := newServer(uuid.NewString())
		req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/conversations/"+goodID+"/export-delete", nil), localIdentityID)
		rec := httptest.NewRecorder()
		server.Mux().ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("foreign export-delete status = %d: %s", rec.Code, rec.Body.String())
		}
		if run.deleteLifecycleCalls != 0 || assetService.listThreadID != "" || assetService.openID != "" {
			t.Fatalf("foreign export-delete touched delete/list/open = %d/%q/%q", run.deleteLifecycleCalls, assetService.listThreadID, assetService.openID)
		}
	})

	t.Run("verified export-delete uses canonical lifecycle", func(t *testing.T) {
		server, run, _ := newServer(localIdentityID)
		req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/conversations/"+goodID+"/export-delete", nil), localIdentityID)
		rec := httptest.NewRecorder()
		server.Mux().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || run.deleteLifecycleCalls != 1 {
			t.Fatalf("owner export-delete status/calls = %d/%d: %s", rec.Code, run.deleteLifecycleCalls, rec.Body.String())
		}
	})

	t.Run("plain delete never creates a hidden export", func(t *testing.T) {
		server, run, assetService := newServer(localIdentityID)
		req := withPrincipal(httptest.NewRequest(http.MethodDelete, "/api/conversations/"+goodID, nil), localIdentityID)
		rec := httptest.NewRecorder()
		server.Mux().ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent || run.deleteLifecycleCalls != 1 {
			t.Fatalf("plain delete status/calls = %d/%d", rec.Code, run.deleteLifecycleCalls)
		}
		if assetService.listThreadID != "" || assetService.openID != "" {
			t.Fatalf("plain delete created hidden export list/open = %q/%q", assetService.listThreadID, assetService.openID)
		}
	})
}

type fakeOwnerExportSource struct {
	snapshot ExportSnapshot
	bodies   map[string][]byte
	err      error
}

func (f *fakeOwnerExportSource) Snapshot(context.Context, string, string) (ExportSnapshot, error) {
	return f.snapshot, f.err
}

func (f *fakeOwnerExportSource) OpenArtifact(_ context.Context, _, id string) (io.ReadCloser, error) {
	body, ok := f.bodies[id]
	if !ok {
		return nil, ErrOwnerExportNotFound
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

type fakeOwnerDelete struct{ calls int }

func (f *fakeOwnerDelete) DeleteConversationLifecycle(context.Context, string, string) (int64, error) {
	f.calls++
	return 1, nil
}
