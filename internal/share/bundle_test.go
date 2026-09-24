// bundle_test.go proves the D-09 (amended) agent-artifacts-only filter and the
// copy-on-share blob writer with NO Garage and NO build tag: every test here runs on
// objectstore.NewFake() so the whole security-critical filter is coverage-gate-safe under
// every tag combination, including none at all.
package share

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/google/uuid"
)

// fakeArtifactOpener is a minimal ArtifactOpener test double keyed by asset id; closeErr is
// what every opened body returns from Close.
type fakeArtifactOpener struct {
	bodies   map[string][]byte
	closeErr error
}

func (f *fakeArtifactOpener) OpenForIdentity(_ context.Context, assetID, _ string) (io.ReadCloser, assets.Asset, error) {
	b, ok := f.bodies[assetID]
	if !ok {
		return nil, assets.Asset{}, fmt.Errorf("fakeArtifactOpener: unknown asset %s", assetID)
	}
	return closeErrReader{Reader: bytes.NewReader(b), err: f.closeErr}, assets.Asset{ID: assetID}, nil
}

type closeErrReader struct {
	io.Reader
	err error
}

func (c closeErrReader) Close() error { return c.err }

// faultyStore delegates to a real fake and fails only the operation a test names.
type faultyStore struct {
	objectstore.Store
	putErr, deleteErr error
}

func (s faultyStore) Put(ctx context.Context, ref objectstore.ObjectRef, body io.Reader, opts objectstore.PutOptions) (objectstore.Attrs, error) {
	if s.putErr != nil {
		return objectstore.Attrs{}, s.putErr
	}
	return s.Store.Put(ctx, ref, body, opts)
}

func (s faultyStore) Delete(ctx context.Context, ref objectstore.ObjectRef) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	return s.Store.Delete(ctx, ref)
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

// TestDefaultTierIsInternal proves D-01's fail-closed tier default at the unit level, with no
// database: resolveTier(service.go) resolves an absent Tier and any unrecognized Tier value to
// TierInternal, and ONLY an explicit TierPublic request ever yields TierPublic. VALIDATION.md
// lists this as a unit test; it lives here (bundle_test.go, untagged) rather than in
// service_integration_test.go (db_integration-tagged) because resolveTier is a pure function —
// an untagged home runs everywhere, which is strictly cheaper than a tagged one for the same
// property. See the plan's own "prefer the untagged home" guidance for this exact case.
func TestDefaultTierIsInternal(t *testing.T) {
	cases := []struct {
		name      string
		requested Tier
		want      Tier
	}{
		{"absent tier resolves to internal", Tier(""), TierInternal},
		{"garbage tier resolves to internal", Tier("garbage"), TierInternal},
		{"explicit internal stays internal", TierInternal, TierInternal},
		{"explicit public is the ONLY way to reach public", TierPublic, TierPublic},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveTier(tc.requested); got != tc.want {
				t.Fatalf("resolveTier(%q) = %q, want %q", tc.requested, got, tc.want)
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

// TestBundleArtifactsSurfacesCopyFailures: a failed Put or a failed Close of the source body
// fails the whole bundle — a share must never publish a snapshot missing an artifact it lists.
func TestBundleArtifactsSurfacesCopyFailures(t *testing.T) {
	assetID := uuid.Must(uuid.NewV7()).String()
	candidate := assets.Asset{ID: assetID, SourceKind: assets.SourceAgent, Status: assets.StatusComplete}
	body := map[string][]byte{assetID: []byte("bytes")}
	cases := []struct {
		name   string
		store  objectstore.Store
		opener *fakeArtifactOpener
		want   string
	}{
		{"put", faultyStore{Store: objectstore.NewFake(), putErr: errors.New("disk full")},
			&fakeArtifactOpener{bodies: body}, "put: disk full"},
		{"close", objectstore.NewFake(),
			&fakeArtifactOpener{bodies: body, closeErr: errors.New("stream reset")}, "close: stream reset"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := bundleArtifacts(context.Background(), tc.store, "share-test-bucket",
				uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), "owner-1", tc.opener, []assets.Asset{candidate})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("bundleArtifacts error = %v, want it to name %q", err, tc.want)
			}
		})
	}
}

// TestDropSnapshotBlobsKeepsTheNewSnapshot pins why Update reclaims one snapshot and not the
// share prefix: the replacement's blobs already sit under the same share when the old ones go.
func TestDropSnapshotBlobsKeepsTheNewSnapshot(t *testing.T) {
	ctx := context.Background()
	store := objectstore.NewFake()
	const bucket = "share-test-bucket"
	shareID, oldSnap, newSnap := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	for _, snap := range []uuid.UUID{oldSnap, newSnap} {
		for _, key := range []string{objectstore.ShareSnapshotKey(shareID, snap), objectstore.ShareArtifactKey(shareID, snap, uuid.Must(uuid.NewV7()))} {
			if _, err := store.Put(ctx, objectstore.ObjectRef{Bucket: bucket, Key: key}, bytes.NewReader([]byte("x")), objectstore.PutOptions{}); err != nil {
				t.Fatalf("seed %s: %v", key, err)
			}
		}
	}

	if err := dropSnapshotBlobs(ctx, store, bucket, shareID, oldSnap); err != nil {
		t.Fatalf("dropSnapshotBlobs: %v", err)
	}
	left, err := store.List(ctx, objectstore.ListRequest{Bucket: bucket, Prefix: objectstore.ShareKeyPrefix(shareID)})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(left) != 2 {
		t.Fatalf("%d blobs left under the share, want the new snapshot's 2", len(left))
	}
	for _, o := range left {
		if !strings.Contains(o.Ref.Key, newSnap.String()) {
			t.Errorf("blob %s survived but does not belong to the new snapshot", o.Ref.Key)
		}
	}
}

// TestBlobReclaimSurfacesDeleteFailures: a Delete that fails must fail the reclaim, or Revoke
// and Update would stamp a share clean while its bytes stay in the bucket.
func TestBlobReclaimSurfacesDeleteFailures(t *testing.T) {
	ctx := context.Background()
	fake := objectstore.NewFake()
	const bucket = "share-test-bucket"
	shareID, snap := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	if _, err := fake.Put(ctx, objectstore.ObjectRef{Bucket: bucket, Key: objectstore.ShareSnapshotKey(shareID, snap)},
		bytes.NewReader([]byte("x")), objectstore.PutOptions{}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	store := faultyStore{Store: fake, deleteErr: errors.New("access denied")}

	if err := dropBlobs(ctx, store, bucket, shareID); err == nil || !strings.Contains(err.Error(), "access denied") {
		t.Errorf("dropBlobs error = %v, want the Delete failure", err)
	}
	if err := dropSnapshotBlobs(ctx, store, bucket, shareID, snap); err == nil || !strings.Contains(err.Error(), "access denied") {
		t.Errorf("dropSnapshotBlobs error = %v, want the Delete failure", err)
	}
}

// TestBundleDropBlobsListError and TestDropSnapshotBlobsListError prove both blob-reclaim
// helpers surface a List failure rather than silently treating it as "nothing to delete".
func TestBundleDropBlobsListError(t *testing.T) {
	store := objectstore.NewFake()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := dropBlobs(ctx, store, "share-test-bucket", uuid.Must(uuid.NewV7())); err == nil {
		t.Fatal("dropBlobs with a canceled context succeeded, want an error")
	}
}

func TestDropSnapshotBlobsListError(t *testing.T) {
	store := objectstore.NewFake()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := dropSnapshotBlobs(ctx, store, "share-test-bucket", uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())); err == nil {
		t.Fatal("dropSnapshotBlobs with a canceled context succeeded, want an error")
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
