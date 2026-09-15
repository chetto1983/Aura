package agui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"maps"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/objectstore"
)

// rangeRecordingStore holds real bytes and records every ranged open, so a test proves a
// Range request opened the store at the range start and never read the object whole.
type rangeRecordingStore struct {
	objectstore.Store
	mu       sync.Mutex
	offsets  []int64
	getCalls int
}

func (s *rangeRecordingStore) Get(ctx context.Context, ref objectstore.ObjectRef) (io.ReadCloser, objectstore.Attrs, error) {
	s.mu.Lock()
	s.getCalls++
	s.mu.Unlock()
	return s.Store.Get(ctx, ref)
}

func (s *rangeRecordingStore) GetFrom(ctx context.Context, ref objectstore.ObjectRef, offset int64) (io.ReadCloser, error) {
	s.mu.Lock()
	s.offsets = append(s.offsets, offset)
	s.mu.Unlock()
	return s.Store.GetFrom(ctx, ref, offset)
}

func (s *rangeRecordingStore) opened() ([]int64, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.offsets), s.getCalls
}

const streamClipSize = 2048

func streamClip() []byte {
	data := make([]byte, streamClipSize)
	for i := range data {
		data[i] = byte(i % 251)
	}
	return data
}

func newRangeRecordingStore(t *testing.T, ref objectstore.ObjectRef, data []byte) *rangeRecordingStore {
	t.Helper()
	store := &rangeRecordingStore{Store: objectstore.NewFake()}
	if _, err := store.Put(context.Background(), ref, bytes.NewReader(data), objectstore.PutOptions{Size: int64(len(data))}); err != nil {
		t.Fatalf("seed Put: %v", err)
	}
	return store
}

func newAssetStreamRig(t *testing.T, mimeType string) (*Server, *fakeAssetService, *rangeRecordingStore) {
	t.Helper()
	asset := assets.Asset{
		ID: "asset-1", IdentityID: assetAPIIdentityID, FileName: "clip.mp4", MIMEType: mimeType,
		SizeBytes: streamClipSize, ObjectBucket: "bucket", ObjectKey: "identity/clip.mp4",
	}
	store := newRangeRecordingStore(t, objectstore.ObjectRef{Bucket: asset.ObjectBucket, Key: asset.ObjectKey}, streamClip())
	fake := &fakeAssetService{openAsset: asset, seekStore: store}
	s := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
	s.SetAssetService(fake)
	return s, fake, store
}

func streamRequest(s *Server, method, path, identityID string, header http.Header) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	maps.Copy(req.Header, header)
	if identityID != "" {
		req = withPrincipal(req, identityID)
	}
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	return rec
}

func rangeHeader(spec string) http.Header {
	return http.Header{"Range": {spec}}
}

type rangeCase struct {
	name         string
	header       http.Header
	status       int
	body         []byte
	contentRange string
	offsets      []int64
}

func rangeCases() []rangeCase {
	clip := streamClip()
	return []rangeCase{
		{"no range serves the whole clip", nil, http.StatusOK, clip, "", []int64{0}},
		{"single range", rangeHeader("bytes=0-99"), http.StatusPartialContent, clip[:100], "bytes 0-99/2048", []int64{0}},
		{"open-ended range", rangeHeader("bytes=100-"), http.StatusPartialContent, clip[100:], "bytes 100-2047/2048", []int64{100}},
		{"suffix range", rangeHeader("bytes=-100"), http.StatusPartialContent, clip[1948:], "bytes 1948-2047/2048", []int64{1948}},
		{"unsatisfiable range", rangeHeader("bytes=999999-"), http.StatusRequestedRangeNotSatisfiable, nil, "bytes */2048", nil},
		{"stale If-Range falls back to the whole clip", http.Header{"Range": {"bytes=0-99"}, "If-Range": {`"stale"`}}, http.StatusOK, clip, "", []int64{0}},
	}
}

// assertRangeResponse checks one Range answer end to end: status, exact bytes, the headers a
// <video> element depends on, and the offsets the store was opened at.
func assertRangeResponse(t *testing.T, tc rangeCase, rec *httptest.ResponseRecorder, store *rangeRecordingStore, contentType string) {
	t.Helper()
	if rec.Code != tc.status {
		t.Fatalf("status = %d, want %d: %s", rec.Code, tc.status, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Range"); got != tc.contentRange {
		t.Fatalf("Content-Range = %q, want %q", got, tc.contentRange)
	}
	offsets, gets := store.opened()
	if !slices.Equal(offsets, tc.offsets) || gets != 0 {
		t.Fatalf("store opened at %v with %d whole-object Gets, want %v and none", offsets, gets, tc.offsets)
	}
	if tc.status == http.StatusRequestedRangeNotSatisfiable {
		return
	}
	if !bytes.Equal(rec.Body.Bytes(), tc.body) {
		t.Fatalf("body = %d bytes, want %d exact bytes", rec.Body.Len(), len(tc.body))
	}
	headers := map[string]string{
		"Accept-Ranges":          "bytes",
		"Content-Type":           contentType,
		"X-Content-Type-Options": "nosniff",
	}
	for key, want := range headers {
		if got := rec.Header().Get(key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "inline;") || !strings.Contains(cd, "filename*=UTF-8''clip.mp4") {
		t.Fatalf("Content-Disposition = %q, want inline with the file name", cd)
	}
}

func TestAssetStreamServesRangesInline(t *testing.T) {
	for _, tc := range rangeCases() {
		t.Run(tc.name, func(t *testing.T) {
			s, fake, store := newAssetStreamRig(t, "video/mp4")
			rec := streamRequest(s, http.MethodGet, "/api/assets/asset-1/stream", assetAPIIdentityID, tc.header)
			assertRangeResponse(t, tc, rec, store, "video/mp4")
			if fake.openID != "asset-1" || fake.openIdentityID != assetAPIIdentityID {
				t.Fatalf("OpenSeekableForIdentity(id=%q, identity=%q), want asset-1 + bound identity", fake.openID, fake.openIdentityID)
			}
		})
	}
}

// A multi-range request makes ServeContent seek between parts, so each part must come from a
// store reopened at its own start.
func TestAssetStreamReopensTheStoreAtEachRangeOfAMultiRangeRequest(t *testing.T) {
	s, _, store := newAssetStreamRig(t, "video/mp4")
	rec := streamRequest(s, http.MethodGet, "/api/assets/asset-1/stream", assetAPIIdentityID, rangeHeader("bytes=0-9,1000-1009"))
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", rec.Code)
	}
	mediaType, params, err := mime.ParseMediaType(rec.Header().Get("Content-Type"))
	if err != nil || mediaType != "multipart/byteranges" {
		t.Fatalf("Content-Type = %q (%v), want multipart/byteranges", rec.Header().Get("Content-Type"), err)
	}
	clip := streamClip()
	reader := multipart.NewReader(rec.Body, params["boundary"])
	for _, want := range [][]byte{clip[0:10], clip[1000:1010]} {
		part, err := reader.NextPart()
		if err != nil {
			t.Fatalf("next part: %v", err)
		}
		if part.Header.Get("Content-Type") != "video/mp4" {
			t.Fatalf("part Content-Type = %q, want video/mp4", part.Header.Get("Content-Type"))
		}
		got, err := io.ReadAll(part)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("part = %v (%v), want %v", got, err, want)
		}
	}
	if offsets, gets := store.opened(); !slices.Equal(offsets, []int64{0, 1000}) || gets != 0 {
		t.Fatalf("store opened at %v with %d Gets, want [0 1000] and none", offsets, gets)
	}
}

func TestAssetStreamHeadDoesNotOpenTheStore(t *testing.T) {
	s, _, store := newAssetStreamRig(t, "video/webm")
	rec := streamRequest(s, http.MethodHead, "/api/assets/asset-1/stream", assetAPIIdentityID, nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Length") != "2048" || rec.Body.Len() != 0 {
		t.Fatalf("HEAD = %d, Content-Length %q, %d body bytes; want 200, 2048, none", rec.Code, rec.Header().Get("Content-Length"), rec.Body.Len())
	}
	if rec.Header().Get("Content-Type") != "video/webm" {
		t.Fatalf("Content-Type = %q, want video/webm", rec.Header().Get("Content-Type"))
	}
	if offsets, gets := store.opened(); len(offsets) != 0 || gets != 0 {
		t.Fatalf("HEAD opened the store at %v with %d Gets", offsets, gets)
	}
}

func TestAssetStreamServesOnlyTheAcceptedVideoTypes(t *testing.T) {
	for _, tc := range []struct {
		mimeType    string
		status      int
		contentType string
	}{
		{"video/mp4", http.StatusPartialContent, "video/mp4"},
		{"video/webm", http.StatusPartialContent, "video/webm"},
		{"Video/MP4; codecs=avc1", http.StatusPartialContent, "video/mp4"},
		{"video/quicktime", http.StatusUnsupportedMediaType, ""},
		{"application/pdf", http.StatusUnsupportedMediaType, ""},
		{"text/html", http.StatusUnsupportedMediaType, ""},
		{"not a type", http.StatusUnsupportedMediaType, ""},
	} {
		t.Run(tc.mimeType, func(t *testing.T) {
			s, _, store := newAssetStreamRig(t, tc.mimeType)
			rec := streamRequest(s, http.MethodGet, "/api/assets/asset-1/stream", assetAPIIdentityID, rangeHeader("bytes=0-1"))
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}
			if tc.status != http.StatusUnsupportedMediaType {
				if got := rec.Header().Get("Content-Type"); got != tc.contentType {
					t.Fatalf("Content-Type = %q, want %q", got, tc.contentType)
				}
				return
			}
			if offsets, gets := store.opened(); len(offsets) != 0 || gets != 0 {
				t.Fatalf("a refused type opened the store at %v with %d Gets", offsets, gets)
			}
		})
	}
}

// A foreign or absent asset is refused exactly as download refuses it: the same status and
// the same body, so the stream route adds no existence oracle.
func TestAssetStreamRefusesAForeignAssetLikeDownload(t *testing.T) {
	notFound := errors.New("asset not found")
	serve := func(route string) *httptest.ResponseRecorder {
		s := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
		s.SetAssetService(&fakeAssetService{openErr: notFound})
		return streamRequest(s, http.MethodGet, "/api/assets/someone-elses-asset/"+route, assetAPIIdentityID, rangeHeader("bytes=0-99"))
	}
	stream, download := serve("stream"), serve("download")
	if stream.Code != http.StatusNotFound || stream.Code != download.Code || stream.Body.String() != download.Body.String() {
		t.Fatalf("stream = %d %q, download = %d %q; want the same 404", stream.Code, stream.Body.String(), download.Code, download.Body.String())
	}
}

func TestAssetStreamRequiresAServiceAndAPrincipal(t *testing.T) {
	bare := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
	if rec := streamRequest(bare, http.MethodGet, "/api/assets/asset-1/stream", assetAPIIdentityID, nil); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no asset service status = %d, want 503", rec.Code)
	}
	s, fake, _ := newAssetStreamRig(t, "video/mp4")
	if rec := streamRequest(s, http.MethodGet, "/api/assets/asset-1/stream", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no principal status = %d, want 401", rec.Code)
	}
	gated := RequireAuth(s.Mux(), testDeps("operator-secret"))
	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/assets/asset-1/stream", nil))
	if rec.Code != http.StatusUnauthorized || fake.openID != "" {
		t.Fatalf("no session = %d with open id %q, want 401 before the asset service", rec.Code, fake.openID)
	}
}
