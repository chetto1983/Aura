// bundle_test.go proves the D-09 (amended) agent-artifacts-only filter and the
// copy-on-share blob writer with NO Garage and NO build tag: every test here runs on
// objectstore.NewFake() so the whole security-critical filter is coverage-gate-safe under
// every tag combination, including none at all.
package share

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/google/uuid"
)

// fakeArtifactOpener is a minimal ArtifactOpener test double keyed by asset id.
type fakeArtifactOpener struct {
	bodies map[string][]byte
}

func (f *fakeArtifactOpener) OpenForIdentity(_ context.Context, assetID, _ string) (io.ReadCloser, assets.Asset, error) {
	b, ok := f.bodies[assetID]
	if !ok {
		return nil, assets.Asset{}, fmt.Errorf("fakeArtifactOpener: unknown asset %s", assetID)
	}
	return io.NopCloser(bytes.NewReader(b)), assets.Asset{ID: assetID}, nil
}

// TestBundleFiltersAgentArtifacts covers every <behavior> row from the plan: only a live
// (non-deleted, non-canceled) agent-produced artifact survives BundleFilter — every other
// SourceKind, including an unrecognized/empty one, is dropped (fail-closed allowlist of one).
func TestBundleFiltersAgentArtifacts(t *testing.T) {
	cases := []struct {
		name       string
		sourceKind assets.SourceKind
		status     assets.Status
		wantKept   bool
	}{
		{"agent live artifact is bundled", assets.SourceAgent, assets.StatusComplete, true},
		{"web upload is never bundled", assets.SourceWeb, assets.StatusComplete, false},
		{"telegram upload is never bundled", assets.SourceTelegram, assets.StatusComplete, false},
		{"cli upload is never bundled", assets.SourceCLI, assets.StatusComplete, false},
		{"deleted agent artifact is dropped", assets.SourceAgent, assets.StatusDeleted, false},
		{"canceled agent artifact is dropped", assets.SourceAgent, assets.StatusCanceled, false},
		{"unknown/empty source_kind fails closed", assets.SourceKind(""), assets.StatusComplete, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := assets.Asset{ID: "asset-1", SourceKind: tc.sourceKind, Status: tc.status}
			got := BundleFilter([]assets.Asset{candidate})
			kept := len(got) == 1
			if kept != tc.wantKept {
				t.Fatalf("BundleFilter(%+v) kept=%v, want %v", candidate, kept, tc.wantKept)
			}
		})
	}
}

// TestBundleArtifactsCopiesUnderPrefix proves bundleArtifacts copies each artifact's bytes
// into objectstore.ShareArtifactKey(shareID, snapshotID, assetID) — a key ALWAYS under
// objectstore.ShareKeyPrefix(shareID), the invariant Revoke's List+Delete depends on to
// reclaim every byte — and returns the matching 4-key SnapshotArtifact descriptors.
func TestBundleArtifactsCopiesUnderPrefix(t *testing.T) {
	ctx := context.Background()
	store := objectstore.NewFake()
	const bucket = "share-test-bucket"
	shareID := uuid.Must(uuid.NewV7())
	snapshotID := uuid.Must(uuid.NewV7())
	assetID := uuid.Must(uuid.NewV7())

	body := []byte("hello artifact bytes")
	opener := &fakeArtifactOpener{bodies: map[string][]byte{assetID.String(): body}}
	candidate := assets.Asset{
		ID: assetID.String(), FileName: "report.pdf", MIMEType: "application/pdf", SizeBytes: int64(len(body)),
		SourceKind: assets.SourceAgent, Status: assets.StatusComplete,
	}

	got, err := bundleArtifacts(ctx, store, bucket, shareID, snapshotID, "owner-1", opener, []assets.Asset{candidate})
	if err != nil {
		t.Fatalf("bundleArtifacts: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("bundleArtifacts returned %d artifacts, want 1", len(got))
	}
	if got[0].AssetID != assetID.String() || got[0].FileName != "report.pdf" {
		t.Fatalf("bundleArtifacts descriptor = %+v, want asset_id=%s filename=report.pdf", got[0], assetID)
	}

	wantKey := objectstore.ShareArtifactKey(shareID, snapshotID, assetID)
	if !bytesUnderPrefix(wantKey, objectstore.ShareKeyPrefix(shareID)) {
		t.Fatalf("artifact key %q is not under share prefix %q", wantKey, objectstore.ShareKeyPrefix(shareID))
	}

	rc, _, err := store.Get(ctx, objectstore.ObjectRef{Bucket: bucket, Key: wantKey})
	if err != nil {
		t.Fatalf("Get(bundled artifact): %v", err)
	}
	defer rc.Close()
	gotBody, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read bundled artifact: %v", err)
	}
	if !bytes.Equal(gotBody, body) {
		t.Fatalf("bundled artifact bytes = %q, want %q", gotBody, body)
	}
}

// TestBundleArtifactsOpenFailure proves bundleArtifacts surfaces (wraps, does not swallow) an
// ArtifactOpener failure — a candidate whose bytes the opener cannot produce fails the whole
// bundle rather than silently skipping it.
func TestBundleArtifactsOpenFailure(t *testing.T) {
	ctx := context.Background()
	store := objectstore.NewFake()
	opener := &fakeArtifactOpener{bodies: map[string][]byte{}} // empty: every open fails
	candidate := assets.Asset{
		ID: uuid.Must(uuid.NewV7()).String(), SourceKind: assets.SourceAgent, Status: assets.StatusComplete,
	}

	if _, err := bundleArtifacts(ctx, store, "share-test-bucket", uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()),
		"owner-1", opener, []assets.Asset{candidate}); err == nil {
		t.Fatal("bundleArtifacts with a failing opener succeeded, want an error")
	}
}

// TestBundleArtifactsInvalidAssetID proves a non-UUID asset id fails BEFORE any open/Put is
// attempted — the ShareArtifactKey derivation cannot proceed without a real uuid.UUID.
func TestBundleArtifactsInvalidAssetID(t *testing.T) {
	ctx := context.Background()
	store := objectstore.NewFake()
	opener := &fakeArtifactOpener{bodies: map[string][]byte{}}
	candidate := assets.Asset{ID: "not-a-uuid", SourceKind: assets.SourceAgent, Status: assets.StatusComplete}

	if _, err := bundleArtifacts(ctx, store, "share-test-bucket", uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()),
		"owner-1", opener, []assets.Asset{candidate}); err == nil {
		t.Fatal("bundleArtifacts with a non-UUID asset id succeeded, want an error")
	}
}

// TestBundleDropBlobsListError proves dropBlobs surfaces a List failure rather than
// silently treating it as "nothing to delete".
func TestBundleDropBlobsListError(t *testing.T) {
	store := objectstore.NewFake()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := dropBlobs(ctx, store, "share-test-bucket", uuid.Must(uuid.NewV7())); err == nil {
		t.Fatal("dropBlobs with a canceled context succeeded, want an error")
	}
}

// TestBundleDropBlobsIdempotent proves dropBlobs reclaims every byte under a share's prefix
// and that a SECOND call on the now-empty prefix is a no-op, not an error.
func TestBundleDropBlobsIdempotent(t *testing.T) {
	ctx := context.Background()
	store := objectstore.NewFake()
	const bucket = "share-test-bucket"
	shareID := uuid.Must(uuid.NewV7())
	snapshotID := uuid.Must(uuid.NewV7())
	assetID := uuid.Must(uuid.NewV7())

	if _, err := store.Put(ctx, objectstore.ObjectRef{Bucket: bucket, Key: objectstore.ShareSnapshotKey(shareID, snapshotID)},
		bytes.NewReader([]byte(`{"schema_version":1}`)), objectstore.PutOptions{MIMEType: "application/json"}); err != nil {
		t.Fatalf("seed snapshot blob: %v", err)
	}
	if _, err := store.Put(ctx, objectstore.ObjectRef{Bucket: bucket, Key: objectstore.ShareArtifactKey(shareID, snapshotID, assetID)},
		bytes.NewReader([]byte("bytes")), objectstore.PutOptions{}); err != nil {
		t.Fatalf("seed artifact blob: %v", err)
	}

	if err := dropBlobs(ctx, store, bucket, shareID); err != nil {
		t.Fatalf("dropBlobs (first call): %v", err)
	}
	objs, err := store.List(ctx, objectstore.ListRequest{Bucket: bucket, Prefix: objectstore.ShareKeyPrefix(shareID)})
	if err != nil {
		t.Fatalf("List after dropBlobs: %v", err)
	}
	if len(objs) != 0 {
		t.Fatalf("List after dropBlobs = %d objects, want 0", len(objs))
	}

	// Second call on an already-empty prefix must be a no-op, not an error.
	if err := dropBlobs(ctx, store, bucket, shareID); err != nil {
		t.Fatalf("dropBlobs (second, idempotent call): %v", err)
	}
}

// bytesUnderPrefix reports whether key starts with prefix — a tiny local helper so the test
// above reads as an explicit assertion rather than a raw strings.HasPrefix call buried inline.
func bytesUnderPrefix(key, prefix string) bool {
	return len(key) >= len(prefix) && key[:len(prefix)] == prefix
}
