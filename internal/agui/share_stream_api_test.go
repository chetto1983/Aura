package agui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/chetto1983/aura/internal/share"
	"github.com/google/uuid"
)

const (
	streamShareToken = "public-token"
	streamShareID    = "0199a000-0000-7000-8000-00000000000a"
	streamVideoID    = "0199a000-0000-7000-8000-0000000000b1"
	streamDocID      = "0199a000-0000-7000-8000-0000000000b2"
)

var errShareMiss = errors.New("share not found")

// streamShareService resolves one public token and one internal share id to a snapshot holding
// a clip and a text file, and serves their bytes from a recording store under the share keys.
// Methods the artifact routes never call are left to the embedded nil interface.
type streamShareService struct {
	ShareService
	snap    share.Snapshot
	link    share.Link
	objects *rangeRecordingStore
}

func (f *streamShareService) ResolveByToken(_ context.Context, token string) (share.Snapshot, share.Link, error) {
	if token != streamShareToken {
		return share.Snapshot{}, share.Link{}, errShareMiss
	}
	return f.snap, f.link, nil
}

func (f *streamShareService) ResolveInternal(_ context.Context, shareID, _ string) (share.Snapshot, share.Link, error) {
	if shareID != streamShareID {
		return share.Snapshot{}, share.Link{}, errShareMiss
	}
	return f.snap, f.link, nil
}

func (f *streamShareService) OpenArtifact(ctx context.Context, shareID, snapshotID uuid.UUID, assetID string) (io.ReadCloser, error) {
	body, _, err := f.objects.Get(ctx, streamShareRef(shareID, snapshotID, assetID))
	return body, err
}

func (f *streamShareService) OpenArtifactSeekable(ctx context.Context, shareID, snapshotID uuid.UUID, assetID string, size int64) (io.ReadSeekCloser, error) {
	return objectstore.NewSeekableObject(ctx, f.objects, streamShareRef(shareID, snapshotID, assetID), size), nil
}

func streamShareRef(shareID, snapshotID uuid.UUID, assetID string) objectstore.ObjectRef {
	return objectstore.ObjectRef{Bucket: "share-bucket", Key: objectstore.ShareArtifactKey(shareID, snapshotID, uuid.MustParse(assetID))}
}

func newShareStreamRig(t *testing.T) (*Server, *rangeRecordingStore) {
	t.Helper()
	link := share.Link{ID: uuid.MustParse(streamShareID), SnapshotID: uuid.MustParse("0199a000-0000-7000-8000-0000000000c1")}
	store := newRangeRecordingStore(t, streamShareRef(link.ID, link.SnapshotID, streamVideoID), streamClip())
	doc := []byte("shared notes")
	if _, err := store.Put(context.Background(), streamShareRef(link.ID, link.SnapshotID, streamDocID), bytes.NewReader(doc), objectstore.PutOptions{}); err != nil {
		t.Fatalf("seed doc: %v", err)
	}
	svc := &streamShareService{
		snap: share.Snapshot{Artifacts: []share.SnapshotArtifact{
			{AssetID: streamVideoID, FileName: "clip.mp4", MIMEType: "video/mp4", SizeBytes: streamClipSize},
			{AssetID: streamDocID, FileName: "notes.txt", MIMEType: "text/plain", SizeBytes: int64(len(doc))},
		}},
		link:    link,
		objects: store,
	}
	s := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
	s.SetShareService(svc)
	return s, store
}

type shareTier struct {
	name      string
	principal string
	asset     func(assetID string) string
}

func shareTiers() []shareTier {
	return []shareTier{
		{"public", "", func(assetID string) string { return "/s/" + streamShareToken + "/asset/" + assetID }},
		{"internal", assetAPIIdentityID, func(assetID string) string { return "/api/shares/" + streamShareID + "/asset/" + assetID }},
	}
}

func TestShareStreamServesRangesInlineOnBothTiers(t *testing.T) {
	for _, tier := range shareTiers() {
		for _, tc := range rangeCases() {
			t.Run(tier.name+"/"+tc.name, func(t *testing.T) {
				s, store := newShareStreamRig(t)
				rec := streamRequest(s, http.MethodGet, tier.asset(streamVideoID)+"/stream", tier.principal, tc.header)
				assertRangeResponse(t, tc, rec, store, "video/mp4")
			})
		}
	}
}

func TestShareStreamRefusesWithoutOpeningTheStore(t *testing.T) {
	for _, tier := range shareTiers() {
		for _, tc := range []struct {
			name   string
			path   string
			status int
		}{
			{"asset outside the snapshot", tier.asset("0199a000-0000-7000-8000-0000000000ff") + "/stream", http.StatusNotFound},
			{"artifact that is not a video", tier.asset(streamDocID) + "/stream", http.StatusUnsupportedMediaType},
		} {
			t.Run(tier.name+"/"+tc.name, func(t *testing.T) {
				s, store := newShareStreamRig(t)
				rec := streamRequest(s, http.MethodGet, tc.path, tier.principal, rangeHeader("bytes=0-1"))
				if rec.Code != tc.status {
					t.Fatalf("status = %d, want %d", rec.Code, tc.status)
				}
				if offsets, gets := store.opened(); len(offsets) != 0 || gets != 0 {
					t.Fatalf("refused request opened the store at %v with %d Gets", offsets, gets)
				}
			})
		}
	}
}

// An unknown token and an unknown internal id answer the same 404 the download routes do.
func TestShareStreamRefusesAnUnresolvedShareLikeDownload(t *testing.T) {
	s, store := newShareStreamRig(t)
	for _, paths := range [][2]string{
		{"/s/wrong-token/asset/" + streamVideoID, ""},
		{"/api/shares/0199a000-0000-7000-8000-0000000000ee/asset/" + streamVideoID, assetAPIIdentityID},
	} {
		stream := streamRequest(s, http.MethodGet, paths[0]+"/stream", paths[1], rangeHeader("bytes=0-1"))
		download := streamRequest(s, http.MethodGet, paths[0], paths[1], nil)
		if stream.Code != http.StatusNotFound || stream.Code != download.Code || stream.Body.String() != download.Body.String() {
			t.Fatalf("%s: stream = %d %q, download = %d %q; want the same 404", paths[0], stream.Code, stream.Body.String(), download.Code, download.Body.String())
		}
	}
	if offsets, gets := store.opened(); len(offsets) != 0 || gets != 0 {
		t.Fatalf("an unresolved share opened the store at %v with %d Gets", offsets, gets)
	}
}

func TestShareStreamRequiresAServiceAndTheInternalTierAPrincipal(t *testing.T) {
	bare := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
	for _, tier := range shareTiers() {
		if rec := streamRequest(bare, http.MethodGet, tier.asset(streamVideoID)+"/stream", tier.principal, nil); rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s without a share service = %d, want 503", tier.name, rec.Code)
		}
	}
	s, _ := newShareStreamRig(t)
	if rec := streamRequest(s, http.MethodGet, "/api/shares/"+streamShareID+"/asset/"+streamVideoID+"/stream", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("internal stream without a principal = %d, want 401", rec.Code)
	}
}

// The download routes keep their inert attachment for every artifact, video included: streaming
// lives on its own sibling route and changes nothing here.
func TestShareDownloadStaysAnInertAttachmentOnBothTiers(t *testing.T) {
	for _, tier := range shareTiers() {
		t.Run(tier.name, func(t *testing.T) {
			s, _ := newShareStreamRig(t)
			rec := streamRequest(s, http.MethodGet, tier.asset(streamVideoID), tier.principal, rangeHeader("bytes=0-99"))
			if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), streamClip()) {
				t.Fatalf("download = %d with %d bytes, want 200 and the whole clip", rec.Code, rec.Body.Len())
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/octet-stream" {
				t.Fatalf("Content-Type = %q, want application/octet-stream", ct)
			}
			if cd := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") {
				t.Fatalf("Content-Disposition = %q, want attachment", cd)
			}
			if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("Content-Length") != "2048" {
				t.Fatalf("nosniff = %q, Content-Length = %q", rec.Header().Get("X-Content-Type-Options"), rec.Header().Get("Content-Length"))
			}
			if rec.Header().Get("Accept-Ranges") != "" {
				t.Fatalf("download advertised Accept-Ranges %q", rec.Header().Get("Accept-Ranges"))
			}
		})
	}
}
