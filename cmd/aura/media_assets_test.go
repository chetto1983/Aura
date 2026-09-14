package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/mediagen"
	"github.com/chetto1983/aura/internal/objectstore"
)

// ownedAssetStore answers GetForIdentity only for its owner, the way the RLS-scoped
// store does, so the adapter's identity/asset argument order is observable.
type ownedAssetStore struct {
	*recordingAssetStore
	owned assets.Asset
}

func (s ownedAssetStore) GetForIdentity(_ context.Context, id, identityID string) (assets.Asset, error) {
	if id != s.owned.ID || identityID != s.owned.IdentityID {
		return assets.Asset{}, errors.New("asset not found")
	}
	return s.owned, nil
}

func newOwnedAssetService(t *testing.T, owned assets.Asset, body []byte) *assets.Service {
	t.Helper()
	objects := objectstore.NewFake()
	ref := objectstore.ObjectRef{Bucket: owned.ObjectBucket, Key: owned.ObjectKey}
	if _, err := objects.Put(context.Background(), ref, bytes.NewReader(body), objectstore.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	return &assets.Service{Store: ownedAssetStore{recordingAssetStore: &recordingAssetStore{}, owned: owned}, Objects: objects}
}

func ownedImage() assets.Asset {
	return assets.Asset{
		ID: "asset-photo", IdentityID: "owner-1", Modality: assets.ModalityImage, MIMEType: "image/png",
		SizeBytes: 9, DeclaredSizeBytes: 1 << 20, ObjectBucket: "aura-assets", ObjectKey: "owner-1/asset-photo",
	}
}

func TestMediaAssetAdapterOpensTheOwnersImage(t *testing.T) {
	body := []byte("png-bytes")
	adapter := mediaAssetAdapter{svc: newOwnedAssetService(t, ownedImage(), body)}

	rc, meta, err := adapter.Open(context.Background(), "owner-1", "asset-photo")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = rc.Close() }()
	got, err := io.ReadAll(rc)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("read %q, %v; want the stored object", got, err)
	}
	if meta != (mediagen.ReferenceMeta{MIMEType: "image/png", Modality: "image", SizeBytes: 9}) {
		t.Fatalf("meta = %+v, want the accepted size, not the declared one", meta)
	}
}

func TestMediaAssetAdapterFeedsReferenceLoading(t *testing.T) {
	body := []byte("png-bytes")
	adapter := mediaAssetAdapter{svc: newOwnedAssetService(t, ownedImage(), body)}

	refs, err := mediagen.LoadReferences(context.Background(), adapter, "owner-1", []string{"asset-photo"}, 1<<20)
	if err != nil {
		t.Fatalf("LoadReferences: %v", err)
	}
	want := "data:image/png;base64," + base64.StdEncoding.EncodeToString(body)
	if len(refs) != 1 || refs[0].ImageURL.URL != want {
		t.Fatalf("references = %+v, want one data URL of the owned image", refs)
	}
	if _, err := mediagen.LoadReferences(context.Background(), adapter, "intruder", []string{"asset-photo"}, 1<<20); mediagen.ErrorCode(err) != "asset_not_found" {
		t.Fatalf("a foreign identity's reference -> %q, want asset_not_found", mediagen.ErrorCode(err))
	}
}

func TestMediaAssetAdapterRefusesWithoutAReadableAsset(t *testing.T) {
	missingObject := ownedImage()
	svc := newOwnedAssetService(t, ownedImage(), []byte("png"))
	svc.Store = ownedAssetStore{recordingAssetStore: &recordingAssetStore{}, owned: func() assets.Asset {
		missingObject.ObjectKey = "owner-1/never-uploaded"
		return missingObject
	}()}

	for name, adapter := range map[string]mediaAssetAdapter{
		"no asset service": {},
		"no stored object": {svc: svc},
	} {
		t.Run(name, func(t *testing.T) {
			rc, meta, err := adapter.Open(context.Background(), "owner-1", "asset-photo")
			if err == nil || rc != nil || meta != (mediagen.ReferenceMeta{}) {
				t.Fatalf("Open = %v, %+v, %v; want an error and nothing to read", rc, meta, err)
			}
		})
	}
}

// agentVideoStore keeps agent assets as aura.assets does on this path: CreateAsset's ON
// CONFLICT answers a repeated (identity, source kind, source reference) with the row already
// stored, and the upload and acceptance steps move that row to accepted.
type agentVideoStore struct {
	*recordingAssetStore
	rows    map[string]assets.Asset
	creates int
	uploads int
}

func newAgentVideoService() (*assets.Service, *agentVideoStore, *objectstore.FakeStore) {
	store := &agentVideoStore{recordingAssetStore: &recordingAssetStore{}, rows: map[string]assets.Asset{}}
	objects := objectstore.NewFake()
	return &assets.Service{Store: store, Objects: objects}, store, objects
}

func (s *agentVideoStore) Create(_ context.Context, req assets.CreateRequest) (assets.Asset, error) {
	s.creates++
	key := req.IdentityID + "|" + string(req.SourceKind) + "|" + req.SourceRef
	if row, ok := s.rows[key]; ok {
		return row, nil
	}
	row := assets.Asset{
		ID: "asset-video-1", IdentityID: req.IdentityID, SourceKind: req.SourceKind, SourceRef: req.SourceRef,
		ToolCallID: req.ToolCallID, ThreadID: req.ThreadID, Scope: req.Scope, Modality: req.Modality,
		Status: assets.StatusCreated, FileName: req.FileName, MIMEType: req.MIMEType,
		DeclaredSizeBytes: req.DeclaredSizeBytes, ObjectBucket: req.ObjectBucket, ObjectKey: req.ObjectKey,
	}
	s.rows[key] = row
	return row, nil
}

func (s *agentVideoStore) MarkUploaded(_ context.Context, id, _ string, size int64, _ string) (assets.Asset, error) {
	s.uploads++
	return s.move(id, func(row *assets.Asset) { row.Status, row.SizeBytes = assets.StatusUploaded, size })
}

func (s *agentVideoStore) MarkAccepted(_ context.Context, id, _ string, size int64, hash, mimeType string) (assets.Asset, error) {
	return s.move(id, func(row *assets.Asset) {
		row.Status, row.SizeBytes, row.ContentHash, row.MIMEType = assets.StatusAccepted, size, hash, mimeType
	})
}

func (s *agentVideoStore) move(id string, change func(*assets.Asset)) (assets.Asset, error) {
	for key, row := range s.rows {
		if row.ID == id {
			change(&row)
			s.rows[key] = row
			return row, nil
		}
	}
	return assets.Asset{}, errors.New("asset not found")
}

var (
	mp4Clip  = []byte("\x00\x00\x00\x18ftypisom\x00\x00\x02\x00isommp41\x00\x00\x00\x08free")
	webmClip = []byte("\x1a\x45\xdf\xa3\x9f\x42\x86\x81\x01\x42\xf7\x81\x01webm")
	videoJob = mediagen.Job{ID: "job-7", IdentityID: "owner-1", ConversationID: "thread-a", ToolCallID: "call-submit"}
)

// TestMediaAssetAdapterIngestsTheVideoCompleteAccepts checks the row against what
// mediagen.Store.Complete requires: the owner's accepted agent video in the job's conversation.
func TestMediaAssetAdapterIngestsTheVideoCompleteAccepts(t *testing.T) {
	for name, tc := range map[string]struct {
		clip           []byte
		mime, fileName string
	}{
		"mp4":  {mp4Clip, "video/mp4", "generated.mp4"},
		"webm": {webmClip, "video/webm", "generated.webm"},
	} {
		t.Run(name, func(t *testing.T) {
			svc, store, objects := newAgentVideoService()
			id, err := mediaAssetAdapter{svc: svc}.IngestVideo(context.Background(), videoJob, tc.clip)
			if err != nil {
				t.Fatalf("IngestVideo: %v", err)
			}
			row := store.rows["owner-1|agent|media-job:job-7"]
			if row.ID != id || row.IdentityID != "owner-1" || row.ThreadID != "thread-a" || row.Scope != assets.ScopeThread ||
				row.SourceKind != assets.SourceAgent || row.Modality != assets.ModalityVideo || row.Status != assets.StatusAccepted {
				t.Fatalf("row = %+v, want the owner's accepted agent video in the job's conversation", row)
			}
			if row.MIMEType != tc.mime || row.FileName != tc.fileName || row.SizeBytes != int64(len(tc.clip)) || row.ToolCallID != "" {
				t.Fatalf("row = %+v, want %s named %s, its size, and no tool call before a delivery claim", row, tc.mime, tc.fileName)
			}
			rc, _, err := objects.Get(context.Background(), objectstore.ObjectRef{Bucket: row.ObjectBucket, Key: row.ObjectKey})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = rc.Close() }()
			if stored, err := io.ReadAll(rc); err != nil || !bytes.Equal(stored, tc.clip) {
				t.Fatalf("stored object = %q, %v; want the clip", stored, err)
			}
		})
	}
}

func TestMediaAssetAdapterReingestReturnsTheStoredVideo(t *testing.T) {
	svc, store, _ := newAgentVideoService()
	adapter := mediaAssetAdapter{svc: svc}
	first, err := adapter.IngestVideo(context.Background(), videoJob, mp4Clip)
	if err != nil {
		t.Fatal(err)
	}
	second, err := adapter.IngestVideo(context.Background(), videoJob, mp4Clip)
	if err != nil || second != first {
		t.Fatalf("second ingest = %q, %v; want the asset %q already stored", second, err, first)
	}
	if store.creates != 2 || store.uploads != 1 || len(store.rows) != 1 {
		t.Fatalf("creates=%d uploads=%d rows=%d; a repeated job must reuse its one accepted asset",
			store.creates, store.uploads, len(store.rows))
	}
}

func TestMediaAssetAdapterRefusesAClipItCannotStoreAsVideo(t *testing.T) {
	for name, clip := range map[string][]byte{
		"quicktime":       []byte("\x00\x00\x00\x14ftypqt  \x00\x00\x02\x00qt  "),
		"html error page": []byte("<html><body>Bad gateway</body></html>"),
		"empty":           nil,
	} {
		t.Run(name, func(t *testing.T) {
			svc, store, _ := newAgentVideoService()
			_, err := mediaAssetAdapter{svc: svc}.IngestVideo(context.Background(), videoJob, clip)
			if mediagen.ErrorCode(err) != "unsupported" || store.creates != 0 {
				t.Fatalf("IngestVideo = %v after %d creates; want unsupported before any asset row", err, store.creates)
			}
		})
	}
	if _, err := (mediaAssetAdapter{}).IngestVideo(context.Background(), videoJob, mp4Clip); !errors.Is(err, errNoAssetService) {
		t.Fatalf("IngestVideo without an asset service = %v", err)
	}
}

func TestImageGenerateHandleRetainedWithoutDependencies(t *testing.T) {
	reg, handles := buildBaseRegistryWithHandles(config.LoadDB(), nil, nil)
	image := handles.ImageGenerate
	if image == nil {
		t.Fatal("the registry must retain the image_generate handle for serve-boot wiring")
	}
	if image.Credentials != nil || image.Settings != nil || image.Catalog != nil || image.Client != nil ||
		image.References != nil || image.Assets != nil || image.MaxImageBytes != 0 {
		t.Fatalf("image_generate carries dependencies at registry build: %+v", image)
	}
	registered, ok := reg.Get("image_generate")
	if !ok || registered != image || !registered.Spec().Deferred {
		t.Fatal("the registry must list the retained, deferred image_generate tool")
	}
}

func TestWireMediaToolsInjectsLiveDependencies(t *testing.T) {
	_, handles := buildBaseRegistryWithHandles(config.LoadDB(), nil, nil)
	svc := &assets.Service{Limits: assets.Limits{MaxImageBytes: 12 << 20, MaxVideoBytes: 30 << 20}}
	chat := &chatEnv{cfg: &config.Config{}, assets: svc, toolHandles: handles}

	media := newMediaDeps(chat)
	wireMediaTools(chat, media)

	image := handles.ImageGenerate
	if image.Catalog == nil || image.Client == nil || image.Settings == nil || image.MaxImageBytes != 12<<20 {
		t.Fatalf("image_generate = %+v, want catalog, client, settings and the asset image ceiling", image)
	}
	if image.Client != media.client || image.Catalog != media.catalog || image.Credentials != media.credentials {
		t.Fatal("image_generate must use the one client, catalog and credentials the video watcher shares")
	}
	if media.maxVideoBytes != 30<<20 || media.jobs != nil {
		t.Fatalf("media video ceiling = %d, jobs = %v; want the asset service's boot ceiling and no job store without a pool",
			media.maxVideoBytes, media.jobs)
	}
	if refs, ok := image.References.(mediaAssetAdapter); !ok || refs.svc != svc {
		t.Fatalf("References = %#v, want the asset-service reference adapter", image.References)
	}
	if deliverer, ok := image.Assets.(sendFileAssetAdapter); !ok || deliverer.svc != svc {
		t.Fatalf("Assets = %#v, want send_file's asset adapter over the same service", image.Assets)
	}
	// No identity resolver exists without a pool; the port must still answer no_key rather
	// than calling through a typed-nil resolver.
	if _, _, err := image.Credentials.For(context.Background(), "owner-1"); mediagen.ErrorCode(err) != "no_key" {
		t.Fatalf("credentials without a resolver -> %q (%v), want no_key", mediagen.ErrorCode(err), err)
	}
}

func TestWireMediaToolsLeavesTheToolRefusingWhenItCannotBeServed(t *testing.T) {
	svc := &assets.Service{Limits: assets.Limits{MaxImageBytes: 12 << 20}}
	for name, cfg := range map[string]struct {
		secret string
		assets *assets.Service
	}{
		"no asset service":        {assets: nil},
		"unreadable settings key": {secret: "not-hex", assets: svc},
	} {
		t.Run(name, func(t *testing.T) {
			_, handles := buildBaseRegistryWithHandles(config.LoadDB(), nil, nil)
			chat := &chatEnv{cfg: &config.Config{AuthulaSecret: cfg.secret}, assets: cfg.assets, toolHandles: handles}
			wireMediaTools(chat, newMediaDeps(chat))
			if image := handles.ImageGenerate; *image != (tools.ImageGenerate{}) {
				t.Fatalf("image_generate partially wired: %+v", image)
			}
		})
	}
}
