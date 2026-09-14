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
	svc := &assets.Service{Limits: assets.Limits{MaxImageBytes: 12 << 20}}
	chat := &chatEnv{cfg: &config.Config{}, assets: svc, toolHandles: handles}

	wireMediaTools(chat)

	image := handles.ImageGenerate
	if image.Catalog == nil || image.Client == nil || image.Settings == nil || image.MaxImageBytes != 12<<20 {
		t.Fatalf("image_generate = %+v, want catalog, client, settings and the asset image ceiling", image)
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
			wireMediaTools(&chatEnv{cfg: &config.Config{AuthulaSecret: cfg.secret}, assets: cfg.assets, toolHandles: handles})
			if image := handles.ImageGenerate; *image != (tools.ImageGenerate{}) {
				t.Fatalf("image_generate partially wired: %+v", image)
			}
		})
	}
}
