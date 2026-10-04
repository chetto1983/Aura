package agui

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/objectstore"
)

// The file manager's direct route answers Range requests: iOS plays no <video> from a server
// that does not, and the home-screen app opens a clip from the file manager in the cockpit's
// own preview, which streams it from this route (prd.md §3).

// seekOpener serves one object through a recording store, so a test sees the route's store calls.
type seekOpener struct {
	store *rangeRecordingStore
	ref   objectstore.ObjectRef
	attrs FileAttrs
}

func (o seekOpener) OpenSeekable(ctx context.Context, _, _ string) (*objectstore.SeekableObject, FileAttrs, error) {
	object, err := objectstore.OpenSeekableObject(ctx, o.store, o.ref)
	if err != nil {
		return nil, FileAttrs{}, err
	}
	return object, o.attrs, nil
}

func directClip(t *testing.T) (*Server, *rangeRecordingStore) {
	t.Helper()
	ref := objectstore.ObjectRef{Bucket: "files", Key: "Video/clip.mp4"}
	store := newRangeRecordingStore(t, ref, streamClip())
	attrs := FileAttrs{MIMEType: "video/mp4", SizeBytes: streamClipSize, FileName: "clip.mp4"}
	return fileServer(nil, seekOpener{store: store, ref: ref, attrs: attrs}), store
}

func directRequest(s *Server, target string, header http.Header) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	s.registerFileRoutes(mux)
	req := withPrincipal(httptest.NewRequest(http.MethodGet, target, nil), fileAPIIdentityID)
	maps.Copy(req.Header, header)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestFileDirectServesARangeOfAClip(t *testing.T) {
	s, store := directClip(t)
	rec := directRequest(s, fileManagerBase+"/direct?id=%2FVideo%2Fclip.mp4", rangeHeader("bytes=100-199"))

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", rec.Code)
	}
	if got := rec.Header().Get("Content-Range"); got != "bytes 100-199/2048" {
		t.Fatalf("Content-Range = %q", got)
	}
	if !bytes.Equal(rec.Body.Bytes(), streamClip()[100:200]) {
		t.Fatalf("body is not bytes 100-199 of the clip (%d bytes)", rec.Body.Len())
	}
	if got := rec.Header().Get("Content-Type"); got != "video/mp4" {
		t.Fatalf("Content-Type = %q, want the clip's own type", got)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "inline") {
		t.Fatalf("Content-Disposition = %q, want inline", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
	// One open at the asked offset and no whole-object read: the store serves only the range.
	if store.getCalls != 0 || len(store.offsets) != 1 || store.offsets[0] != 100 {
		t.Fatalf("store calls = %d Get, GetFrom at %v; want one GetFrom at 100", store.getCalls, store.offsets)
	}
}

func TestFileDirectDownloadStaysAWholeAttachment(t *testing.T) {
	s, _ := directClip(t)
	rec := directRequest(s, fileManagerBase+"/direct?id=%2FVideo%2Fclip.mp4&download=true", nil)

	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), streamClip()) {
		t.Fatalf("status = %d with %d bytes, want 200 and the whole clip", rec.Code, rec.Body.Len())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment") {
		t.Fatalf("Content-Disposition = %q, want attachment", got)
	}
	if got := rec.Header().Get("Content-Length"); got != "2048" {
		t.Fatalf("Content-Length = %q, want the object's size", got)
	}
}

// Once the status line is out a failing read can only truncate the body, so it is logged
// under the route's own name rather than the media stream's.
func TestFileDirectLogsAStoreFailureAfterTheStatusLine(t *testing.T) {
	logs := captureWarnings(t)
	s, store := directClip(t)
	store.getFromErr = errors.New("garage connection reset")
	rec := directRequest(s, fileManagerBase+"/direct?id=%2FVideo%2Fclip.mp4", rangeHeader("bytes=100-"))

	if rec.Code != http.StatusPartialContent || rec.Body.Len() != 0 {
		t.Fatalf("status = %d with %d bytes, want the 206 already sent and nothing after it", rec.Code, rec.Body.Len())
	}
	for _, want := range []string{"file read failed", "identity_id=" + fileAPIIdentityID, "garage connection reset"} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("log %q does not contain %q", logs.String(), want)
		}
	}
}
