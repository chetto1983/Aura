package objectstore

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestShareSnapshotKeyShape mirrors TestAssetKeyContainsNoFilename's shape:
// exact-equality against the expected string for fixed UUIDs, then a
// forbidden-substring loop.
func TestShareSnapshotKeyShape(t *testing.T) {
	shareID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	snapshotID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	key := ShareSnapshotKey(shareID, snapshotID)
	want := "share/11111111-1111-1111-1111-111111111111/snapshot/22222222-2222-2222-2222-222222222222/canonical.json"
	if key != want {
		t.Fatalf("ShareSnapshotKey() = %q, want %q", key, want)
	}
	for _, forbidden := range []string{"identity/", "..", "\\"} {
		if strings.Contains(key, forbidden) {
			t.Fatalf("ShareSnapshotKey() contains forbidden fragment %q in %q", forbidden, key)
		}
	}
}

// TestShareArtifactKeyShape mirrors TestAssetKeyContainsNoFilename's shape
// for the artifact key.
func TestShareArtifactKeyShape(t *testing.T) {
	shareID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	snapshotID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	assetID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	key := ShareArtifactKey(shareID, snapshotID, assetID)
	want := "share/11111111-1111-1111-1111-111111111111/snapshot/22222222-2222-2222-2222-222222222222/asset/33333333-3333-3333-3333-333333333333"
	if key != want {
		t.Fatalf("ShareArtifactKey() = %q, want %q", key, want)
	}
	for _, forbidden := range []string{"identity/", "..", "\\"} {
		if strings.Contains(key, forbidden) {
			t.Fatalf("ShareArtifactKey() contains forbidden fragment %q in %q", forbidden, key)
		}
	}
}

// TestShareKeyNamespaceDisjoint is the D-12 property: for many generated
// uuid triples, a share key is never identity/-prefixed, an asset key is
// never share/-prefixed, and distinct share ids yield distinct prefixes
// (T-37F-05).
func TestShareKeyNamespaceDisjoint(t *testing.T) {
	for i := 0; i < 200; i++ {
		shareA := uuid.New()
		shareB := uuid.New()
		snapshotID := uuid.New()
		assetID := uuid.New()

		if strings.HasPrefix(ShareSnapshotKey(shareA, snapshotID), "identity/") {
			t.Fatalf("ShareSnapshotKey(%s, %s) is identity/-prefixed", shareA, snapshotID)
		}
		if strings.HasPrefix(ShareArtifactKey(shareA, snapshotID, assetID), "identity/") {
			t.Fatalf("ShareArtifactKey(%s, %s, %s) is identity/-prefixed", shareA, snapshotID, assetID)
		}
		if strings.HasPrefix(AssetKey(shareA.String(), assetID.String()), "share/") {
			t.Fatalf("AssetKey(%s, %s) is share/-prefixed", shareA, assetID)
		}
		if shareA != shareB && ShareKeyPrefix(shareA) == ShareKeyPrefix(shareB) {
			t.Fatalf("ShareKeyPrefix(%s) == ShareKeyPrefix(%s), want distinct prefixes for distinct share ids", shareA, shareB)
		}
	}
}

// TestShareArtifactKeyUnderPrefix proves the revoke-completeness invariant:
// every artifact key sits under its share's ShareKeyPrefix, so revoke's
// List(prefix)+Delete reclaims every byte (T-37F-07).
func TestShareArtifactKeyUnderPrefix(t *testing.T) {
	for i := 0; i < 200; i++ {
		shareID := uuid.New()
		snapshotID := uuid.New()
		assetID := uuid.New()
		artifactKey := ShareArtifactKey(shareID, snapshotID, assetID)
		prefix := ShareKeyPrefix(shareID)
		if !strings.HasPrefix(artifactKey, prefix) {
			t.Fatalf("ShareArtifactKey(%s, %s, %s) = %q does not sit under ShareKeyPrefix(%s) = %q",
				shareID, snapshotID, assetID, artifactKey, shareID, prefix)
		}
	}
}
