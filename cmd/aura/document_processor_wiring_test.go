package main

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/mediagen"
)

// TestResolveAssetMaxVideoBytesFailsClosedOnAReadError pins the boot-error decision without
// a daemon: assetMaxVideoBytesFor cannot be exercised on its fail-closed branch without a
// live pool (settings.NewStore needs one), but the decision it delegates to --
// resolveAssetMaxVideoBytes -- takes a settings.Lister, so a fake one proves the contract:
// an unreadable or invalid stored AURA_ASSET_MAX_VIDEO_BYTES value returns 0 (every video
// refused), never a silent fallback to the compiled default.
func TestResolveAssetMaxVideoBytesFailsClosedOnAReadError(t *testing.T) {
	lister := &fakeMediaLister{err: errors.New("aura.settings unavailable")}
	if got := resolveAssetMaxVideoBytes(context.Background(), lister); got != 0 {
		t.Fatalf("resolveAssetMaxVideoBytes() with a store read error = %d, want 0 (fail closed)", got)
	}
}

// TestResolveAssetMaxVideoBytesFailsClosedOnAnInvalidStoredValue is the other fail-closed
// path: bootAssetMaxVideoBytes itself rejects a nonpositive stored row, and that error must
// reach the same fail-closed 0, never the compiled default that would hide the bad value.
func TestResolveAssetMaxVideoBytesFailsClosedOnAnInvalidStoredValue(t *testing.T) {
	lister := &fakeMediaLister{rows: []sqlc.AuraSettings{{Key: assetMaxVideoBytesSettingKey, Value: "0"}}}
	if got := resolveAssetMaxVideoBytes(context.Background(), lister); got != 0 {
		t.Fatalf("resolveAssetMaxVideoBytes() with an invalid stored value = %d, want 0 (fail closed)", got)
	}
}

// TestResolveAssetMaxVideoBytesDefaultsWithNoOverride is the non-error path: an absent row
// (or, per bootAssetMaxVideoBytes' own contract, a nil lister) is not a failure, so it takes
// the compiled default.
func TestResolveAssetMaxVideoBytesDefaultsWithNoOverride(t *testing.T) {
	if got := resolveAssetMaxVideoBytes(context.Background(), &fakeMediaLister{}); got != mediagen.DefaultAssetMaxVideoBytes {
		t.Fatalf("resolveAssetMaxVideoBytes() with no stored row = %d, want default %d", got, mediagen.DefaultAssetMaxVideoBytes)
	}
	if got := resolveAssetMaxVideoBytes(context.Background(), nil); got != mediagen.DefaultAssetMaxVideoBytes {
		t.Fatalf("resolveAssetMaxVideoBytes() with a nil lister = %d, want default %d", got, mediagen.DefaultAssetMaxVideoBytes)
	}
}

// TestAssetMaxVideoBytesForWithNoPoolTakesTheDefault proves assetMaxVideoBytesFor's nil-pool
// branch without a daemon: no pool means no aura.settings to read, which is the one case
// bootAssetMaxVideoBytes itself treats as "no override" rather than a failure.
func TestAssetMaxVideoBytesForWithNoPoolTakesTheDefault(t *testing.T) {
	if got := assetMaxVideoBytesFor(nil, nil); got != mediagen.DefaultAssetMaxVideoBytes {
		t.Fatalf("assetMaxVideoBytesFor(nil pool) = %d, want default %d", got, mediagen.DefaultAssetMaxVideoBytes)
	}
}
