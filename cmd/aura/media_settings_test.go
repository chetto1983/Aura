package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/mediagen"
)

// fakeMediaLister is a minimal settings.Lister stub: mutate rows between calls
// to prove a port instance reads the store fresh, with no restart and no
// separate invalidation.
type fakeMediaLister struct {
	rows []sqlc.AuraSettings
	err  error
}

func (f *fakeMediaLister) List(context.Context) ([]sqlc.AuraSettings, error) {
	return f.rows, f.err
}

func TestMediaSettingsModelSavedValueLiveThenResetToDefault(t *testing.T) {
	lister := &fakeMediaLister{}
	port := newMediaSettings(lister)

	model, err := port.Model(context.Background(), mediagen.KindImage)
	if err != nil || model != mediagen.DefaultImageModel {
		t.Fatalf("Model() = %q, %v, want default %q", model, err, mediagen.DefaultImageModel)
	}

	lister.rows = []sqlc.AuraSettings{{Key: imageModelSettingKey, Value: "vendor/other-image-model"}}
	model, err = port.Model(context.Background(), mediagen.KindImage)
	if err != nil || model != "vendor/other-image-model" {
		t.Fatalf("Model() after save = %q, %v, want the saved value visible on the same port instance", model, err)
	}

	lister.rows = nil
	model, err = port.Model(context.Background(), mediagen.KindImage)
	if err != nil || model != mediagen.DefaultImageModel {
		t.Fatalf("Model() after reset = %q, %v, want default again with no restart", model, err)
	}
}

func TestMediaSettingsModelVideoDefault(t *testing.T) {
	port := newMediaSettings(&fakeMediaLister{})
	model, err := port.Model(context.Background(), mediagen.KindVideo)
	if err != nil || model != mediagen.DefaultVideoModel {
		t.Fatalf("Model(video) = %q, %v, want default %q", model, err, mediagen.DefaultVideoModel)
	}
}

func TestMediaSettingsModelRejectsUnknownKind(t *testing.T) {
	port := newMediaSettings(&fakeMediaLister{})
	if _, err := port.Model(context.Background(), mediagen.Kind("audio")); err == nil {
		t.Fatal("Model() must reject an unknown Kind")
	}
}

// TestMediaSettingsModelSurfacesStoreError proves a store error is returned,
// never silently swapped for the default model.
func TestMediaSettingsModelSurfacesStoreError(t *testing.T) {
	lister := &fakeMediaLister{err: errors.New("aura.settings unavailable")}
	port := newMediaSettings(lister)
	if _, err := port.Model(context.Background(), mediagen.KindImage); err == nil {
		t.Fatal("Model() must surface a store error, not silently switch to the default model")
	}
}

func TestMediaSettingsVideoInlineWaitDefaultAndZero(t *testing.T) {
	lister := &fakeMediaLister{}
	port := newMediaSettings(lister)

	wait, err := port.VideoInlineWait(context.Background())
	if err != nil || wait != time.Duration(mediagen.DefaultVideoInlineWaitSec)*time.Second {
		t.Fatalf("VideoInlineWait() = %v, %v, want the default %ds", wait, err, mediagen.DefaultVideoInlineWaitSec)
	}

	lister.rows = []sqlc.AuraSettings{{Key: videoInlineWaitSettingKey, Value: "0"}}
	wait, err = port.VideoInlineWait(context.Background())
	if err != nil || wait != 0 {
		t.Fatalf("VideoInlineWait() with stored 0 = %v, %v, want 0 (immediate detachment is allowed)", wait, err)
	}
}

func TestMediaSettingsVideoInlineWaitRejectsNegative(t *testing.T) {
	lister := &fakeMediaLister{rows: []sqlc.AuraSettings{{Key: videoInlineWaitSettingKey, Value: "-5"}}}
	port := newMediaSettings(lister)
	if _, err := port.VideoInlineWait(context.Background()); err == nil {
		t.Fatal("VideoInlineWait() must reject a negative stored wait")
	}
}

func TestMediaSettingsVideoInlineWaitRejectsGarbage(t *testing.T) {
	lister := &fakeMediaLister{rows: []sqlc.AuraSettings{{Key: videoInlineWaitSettingKey, Value: "soon"}}}
	port := newMediaSettings(lister)
	if _, err := port.VideoInlineWait(context.Background()); err == nil {
		t.Fatal("VideoInlineWait() must reject a non-integer stored wait")
	}
}

func TestBootAssetMaxVideoBytesDefaultAndOverride(t *testing.T) {
	maxBytes, err := bootAssetMaxVideoBytes(context.Background(), &fakeMediaLister{})
	if err != nil || maxBytes != mediagen.DefaultAssetMaxVideoBytes {
		t.Fatalf("bootAssetMaxVideoBytes() = %d, %v, want default %d", maxBytes, err, mediagen.DefaultAssetMaxVideoBytes)
	}

	lister := &fakeMediaLister{rows: []sqlc.AuraSettings{{Key: assetMaxVideoBytesSettingKey, Value: "1048576"}}}
	maxBytes, err = bootAssetMaxVideoBytes(context.Background(), lister)
	if err != nil || maxBytes != 1048576 {
		t.Fatalf("bootAssetMaxVideoBytes() override = %d, %v, want 1048576", maxBytes, err)
	}
}

func TestBootAssetMaxVideoBytesRejectsNonpositive(t *testing.T) {
	for _, value := range []string{"0", "-100"} {
		lister := &fakeMediaLister{rows: []sqlc.AuraSettings{{Key: assetMaxVideoBytesSettingKey, Value: value}}}
		if _, err := bootAssetMaxVideoBytes(context.Background(), lister); err == nil {
			t.Fatalf("bootAssetMaxVideoBytes() with stored %q must be rejected", value)
		}
	}
}

func TestMediaSettingsNilListerFallsBackToDefaults(t *testing.T) {
	port := newMediaSettings(nil)
	if model, err := port.Model(context.Background(), mediagen.KindImage); err != nil || model != mediagen.DefaultImageModel {
		t.Fatalf("Model() with a nil lister = %q, %v, want default", model, err)
	}
	if maxBytes, err := bootAssetMaxVideoBytes(context.Background(), nil); err != nil || maxBytes != mediagen.DefaultAssetMaxVideoBytes {
		t.Fatalf("bootAssetMaxVideoBytes() with a nil lister = %d, %v, want default", maxBytes, err)
	}
}
