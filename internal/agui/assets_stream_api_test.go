package agui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/objectstore"
)

// rangeRecordingStore holds real bytes and records every Head, whole-object Get and ranged open,
// so a test proves exactly which store calls a request made.
type rangeRecordingStore struct {
	objectstore.Store
	heads    int
	getCalls int
	offsets  []int64
	// getFromErr fails every ranged open; cancel, when set, first cancels the request the way a
	// client disconnect does.
	getFromErr error
	cancel     context.CancelFunc
}

func (s *rangeRecordingStore) Head(ctx context.Context, ref objectstore.ObjectRef) (objectstore.Attrs, error) {
	s.heads++
	return s.Store.Head(ctx, ref)
}

func (s *rangeRecordingStore) Get(ctx context.Context, ref objectstore.ObjectRef) (io.ReadCloser, objectstore.Attrs, error) {
	s.getCalls++
	return s.Store.Get(ctx, ref)
}

func (s *rangeRecordingStore) GetFrom(ctx context.Context, ref objectstore.ObjectRef, offset int64) (io.ReadCloser, error) {
	s.offsets = append(s.offsets, offset)
	if s.cancel != nil {
		s.cancel()
	}
	if s.getFromErr != nil {
		return nil, s.getFromErr
	}
	return s.Store.GetFrom(ctx, ref, offset)
}

func (s *rangeRecordingStore) assertUntouched(t *testing.T) {
	t.Helper()
	if s.heads != 0 || s.getCalls != 0 || len(s.offsets) != 0 {
		t.Fatalf("store calls = %d Head, %d Get, GetFrom at %v; want none", s.heads, s.getCalls, s.offsets)
	}
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

// captureWarnings routes slog to a buffer for one test; the handlers log through the default.
func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
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
		{"several ranges are served whole", rangeHeader("bytes=0-0,0-0,0-0,1000-1009"), http.StatusOK, clip, "", []int64{0}},
	}
}

// assertRangeResponse checks one Range answer end to end: status, exact bytes, the headers a
// <video> element depends on, and the store calls — one Head, then opens only at the offsets
// the range needs, never a whole-object Get.
func assertRangeResponse(t *testing.T, tc rangeCase, rec *httptest.ResponseRecorder, store *rangeRecordingStore, contentType string) {
	t.Helper()
	if rec.Code != tc.status {
		t.Fatalf("status = %d, want %d: %s", rec.Code, tc.status, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Range"); got != tc.contentRange {
		t.Fatalf("Content-Range = %q, want %q", got, tc.contentRange)
	}
	if store.heads != 1 || store.getCalls != 0 || !slices.Equal(store.offsets, tc.offsets) {
		t.Fatalf("store calls = %d Head, %d Get, GetFrom at %v; want 1 Head, no Get, GetFrom at %v",
			store.heads, store.getCalls, store.offsets, tc.offsets)
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

func TestAssetStreamHeadReadsNoBytes(t *testing.T) {
	s, _, store := newAssetStreamRig(t, "video/webm")
	rec := streamRequest(s, http.MethodHead, "/api/assets/asset-1/stream", assetAPIIdentityID, nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Length") != "2048" || rec.Body.Len() != 0 {
		t.Fatalf("HEAD = %d, Content-Length %q, %d body bytes; want 200, 2048, none", rec.Code, rec.Header().Get("Content-Length"), rec.Body.Len())
	}
	if rec.Header().Get("Content-Type") != "video/webm" {
		t.Fatalf("Content-Type = %q, want video/webm", rec.Header().Get("Content-Type"))
	}
	if store.heads != 1 || store.getCalls != 0 || len(store.offsets) != 0 {
		t.Fatalf("HEAD store calls = %d Head, %d Get, GetFrom at %v; want only the Head", store.heads, store.getCalls, store.offsets)
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
			if store.getCalls != 0 || len(store.offsets) != 0 {
				t.Fatalf("a refused type read the store: %d Get, GetFrom at %v", store.getCalls, store.offsets)
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

// An owned row whose object is gone answers 404 on GET and HEAD alike, before a status line
// could promise bytes that do not exist.
func TestAssetStreamRefusesAMissingObject(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			s, fake, store := newAssetStreamRig(t, "video/mp4")
			fake.openAsset.ObjectKey = "identity/gone.mp4"
			rec := streamRequest(s, method, "/api/assets/asset-1/stream", assetAPIIdentityID, rangeHeader("bytes=0-99"))
			if rec.Code != http.StatusNotFound || rec.Header().Get("Content-Range") != "" {
				t.Fatalf("%s missing object = %d (Content-Range %q), want a plain 404", method, rec.Code, rec.Header().Get("Content-Range"))
			}
			if store.heads != 1 || len(store.offsets) != 0 {
				t.Fatalf("store calls = %d Head, GetFrom at %v; want the Head only", store.heads, store.offsets)
			}
		})
	}
}

// Once the status line is out a failing read can only truncate the body, so the failure is
// logged; a request the client cancelled is not a store fault and stays quiet.
func TestAssetStreamLogsAStoreFailureAfterTheStatusLine(t *testing.T) {
	logs := captureWarnings(t)
	s, _, store := newAssetStreamRig(t, "video/mp4")
	store.getFromErr = errors.New("garage connection reset")
	rec := streamRequest(s, http.MethodGet, "/api/assets/asset-1/stream", assetAPIIdentityID, rangeHeader("bytes=100-"))
	if rec.Code != http.StatusPartialContent || rec.Body.Len() != 0 {
		t.Fatalf("status = %d with %d bytes, want the 206 already sent and nothing after it", rec.Code, rec.Body.Len())
	}
	for _, want := range []string{"video stream read failed", "asset_id=asset-1", "garage connection reset"} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("log %q does not contain %q", logs.String(), want)
		}
	}

	logs.Reset()
	cancelled, _, cancelStore := newAssetStreamRig(t, "video/mp4")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancelStore.cancel, cancelStore.getFromErr = cancel, context.Canceled
	req := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/assets/asset-1/stream", nil).WithContext(ctx), assetAPIIdentityID)
	cancelled.Mux().ServeHTTP(httptest.NewRecorder(), req)
	if len(cancelStore.offsets) != 1 || logs.Len() != 0 {
		t.Fatalf("GetFrom at %v, log %q; want one open and no warning for a client that left", cancelStore.offsets, logs.String())
	}
}

func TestAssetStreamRequiresAServiceAndAPrincipal(t *testing.T) {
	bare := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
	if rec := streamRequest(bare, http.MethodGet, "/api/assets/asset-1/stream", assetAPIIdentityID, nil); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no asset service status = %d, want 503", rec.Code)
	}
	s, fake, store := newAssetStreamRig(t, "video/mp4")
	if rec := streamRequest(s, http.MethodGet, "/api/assets/asset-1/stream", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no principal status = %d, want 401", rec.Code)
	}
	gated := RequireAuth(s.Mux(), testDeps("operator-secret"))
	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/assets/asset-1/stream", nil))
	if rec.Code != http.StatusUnauthorized || fake.openID != "" {
		t.Fatalf("no session = %d with open id %q, want 401 before the asset service", rec.Code, fake.openID)
	}
	store.assertUntouched(t)
}
