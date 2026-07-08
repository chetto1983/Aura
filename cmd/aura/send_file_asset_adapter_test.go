package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/objectstore"
)

// recordingAssetStore is a daemon-free assets.StoreBackend that records the Create request and
// echoes a fixed accepted asset through the lifecycle, so the adapter's os.Open -> IngestAgentFile
// field mapping can be asserted without a live Postgres/Garage stack.
type recordingAssetStore struct {
	created assets.CreateRequest
	asset   assets.Asset
}

func (r *recordingAssetStore) Create(_ context.Context, req assets.CreateRequest) (assets.Asset, error) {
	r.created = req
	r.asset = assets.Asset{
		ID:           "asset-rec-1",
		IdentityID:   req.IdentityID,
		ThreadID:     req.ThreadID,
		FileName:     req.FileName,
		MIMEType:     req.MIMEType,
		SourceKind:   req.SourceKind,
		ObjectBucket: req.ObjectBucket,
		ObjectKey:    req.ObjectKey,
	}
	return r.asset, nil
}

func (r *recordingAssetStore) MarkUploaded(context.Context, string, string, int64, string) (assets.Asset, error) {
	return r.asset, nil
}
func (r *recordingAssetStore) MarkAccepted(context.Context, string, string, int64, string, string) (assets.Asset, error) {
	return r.asset, nil
}
func (r *recordingAssetStore) GetForIdentity(context.Context, string, string) (assets.Asset, error) {
	return assets.Asset{}, nil
}
func (r *recordingAssetStore) ListForThread(context.Context, string, string) ([]assets.Asset, error) {
	return nil, nil
}
func (r *recordingAssetStore) ListForLibrary(context.Context, string, int) ([]assets.Asset, error) {
	return nil, nil
}
func (r *recordingAssetStore) SetStatus(context.Context, string, string, assets.Status, string, string) (assets.Asset, error) {
	return r.asset, nil
}
func (r *recordingAssetStore) SetResult(context.Context, string, string, assets.Result) (assets.Asset, error) {
	return r.asset, nil
}
func (r *recordingAssetStore) Promote(context.Context, string, string) (assets.Asset, error) {
	return assets.Asset{}, nil
}
func (r *recordingAssetStore) Delete(context.Context, string, string) (assets.Asset, error) {
	return assets.Asset{}, nil
}

// TestSendFileAssetAdapterForwards proves the adapter opens the host file and forwards the
// delivery to IngestAgentFile with the correct identity/thread/filename/mime/size mapping, and
// returns the created asset id.
func TestSendFileAssetAdapterForwards(t *testing.T) {
	var _ tools.AssetDeliverer = sendFileAssetAdapter{} // interface satisfaction

	root := t.TempDir()
	path := filepath.Join(root, "report.pdf")
	const body = "%PDF-1.4 body bytes"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	store := &recordingAssetStore{}
	svc := &assets.Service{Store: store, Objects: objectstore.NewFake()}
	adapter := sendFileAssetAdapter{svc: svc}

	id, err := adapter.IngestAgentDelivery(context.Background(), "id-9", "thread-9", path, "report.pdf", "application/pdf", int64(len(body)))
	if err != nil {
		t.Fatalf("IngestAgentDelivery: %v", err)
	}
	if id != "asset-rec-1" {
		t.Fatalf("returned asset id = %q, want asset-rec-1", id)
	}
	if store.created.IdentityID != "id-9" || store.created.ThreadID != "thread-9" {
		t.Fatalf("create identity/thread = %q/%q, want id-9/thread-9", store.created.IdentityID, store.created.ThreadID)
	}
	if store.created.FileName != "report.pdf" || store.created.MIMEType != "application/pdf" {
		t.Fatalf("create filename/mime = %q/%q, want report.pdf/application/pdf", store.created.FileName, store.created.MIMEType)
	}
	if store.created.SourceKind != assets.SourceAgent {
		t.Fatalf("create source kind = %q, want %q", store.created.SourceKind, assets.SourceAgent)
	}
	if store.created.DeclaredSizeBytes != int64(len(body)) {
		t.Fatalf("create declared size = %d, want %d", store.created.DeclaredSizeBytes, len(body))
	}
}

// TestSendFileAssetAdapterMissingFileErrors proves the os.Open-first ordering: a missing host file
// errors before the service is touched (svc is nil here), so the tool degrades to path-only.
func TestSendFileAssetAdapterMissingFileErrors(t *testing.T) {
	adapter := sendFileAssetAdapter{svc: nil}
	_, err := adapter.IngestAgentDelivery(context.Background(), "id", "thread", filepath.Join(t.TempDir(), "nope.bin"), "nope.bin", "", 0)
	if err == nil {
		t.Fatal("a missing host file must error (os.Open) before the ingest service is touched")
	}
}

// TestSendFileHandleRetainedNilAssets proves the composition-root wiring guard: the built registry
// retains a non-nil SendFile handle (so serve boot can set .Assets) whose Assets is nil at build
// time (static/manifest/CLI paths stay path-only, D-02).
func TestSendFileHandleRetainedNilAssets(t *testing.T) {
	_, handles := buildBaseRegistryWithHandles(config.LoadDB(), nil, nil)
	if handles.SendFile == nil {
		t.Fatal("buildBaseRegistryWithHandles must retain a non-nil SendFile handle for serve-boot .Assets wiring")
	}
	if handles.SendFile.Assets != nil {
		t.Fatal("SendFile.Assets must be nil at registry build — path-only degrade on static/CLI paths (D-02)")
	}
}
