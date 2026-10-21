package agui

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"testing"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/docfields"
	"github.com/chetto1983/aura/internal/documents"
)

type previewDocumentsStore struct{ fakeDocumentsStore }

func (f *previewDocumentsStore) Document(_ context.Context, identity, id string) (docfields.DocumentDetail, error) {
	if id != identity {
		return docfields.DocumentDetail{}, docfields.ErrNotFound
	}
	return docfields.DocumentDetail{ID: id, SourceKey: "same.png", FileName: "same.png", SizeBytes: 99999999, RawSHA256: "same-version"}, nil
}

type apiPreviewer struct {
	err            error
	identity, name string
	size           int64
}

func (p *apiPreviewer) Preview(ctx context.Context, owner, name string, open documents.PreviewSource) ([]byte, error) {
	p.identity, p.name = owner, name
	if p.err != nil {
		return nil, p.err
	}
	body, size, err := open(ctx)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	p.size = size
	return io.ReadAll(body)
}
func TestDocumentThumbnailScopesLookupAndSource(t *testing.T) {
	opener := &fakeFileOpener{body: "tenant one", mime: "text/html"}
	p := &apiPreviewer{}
	s := documentsServer(&previewDocumentsStore{})
	s.SetFileOpener(opener)
	s.SetDocumentPreviewer(p)
	for _, id := range []string{"missing", policyOther} {
		rec := policyRequest(t, s, "GET", "/api/documents/"+id+"/thumbnail", "", documentsOwner)
		if rec.Code != 404 || opener.key != "" {
			t.Fatalf("foreign/missing=%d opened=%q", rec.Code, opener.key)
		}
	}
	for _, owner := range []string{documentsOwner, policyOther} {
		opener.body = owner
		rec := policyRequest(t, s, "GET", "/api/documents/"+owner+"/thumbnail?v=same-version&url=https://invalid.example", "", owner)
		if rec.Code != 200 || rec.Body.String() != owner || opener.identity != owner || opener.key != "same.png" || p.identity != owner || p.name != "same.png" || p.size != 36 {
			t.Fatalf("code=%d body=%s opener=%+v provider=%+v", rec.Code, rec.Body.String(), opener, p)
		}
		if rec.Header().Get("Content-Type") != "image/png" || rec.Header().Get("Cache-Control") != "private, no-store" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("headers=%v", rec.Header())
		}
	}
}
func TestDocumentThumbnailExplicitFailures(t *testing.T) {
	for _, c := range []struct {
		err    error
		status int
		code   string
	}{
		{documents.ErrPreviewUnsupported, 415, "thumbnail_unsupported"},
		{documents.ErrPreviewTooLarge, 413, "thumbnail_too_large"},
		{documents.ErrPreviewInvalid, 422, "thumbnail_invalid"},
		{documents.ErrPreviewUnavailable, 503, "thumbnail_unavailable"},
		{errors.Join(documents.ErrPreviewUnavailable, context.Canceled), 503, "thumbnail_unavailable"},
		{context.Canceled, 408, "thumbnail_cancelled"},
		{context.DeadlineExceeded, 504, "thumbnail_timeout"},
		{errors.New("private secret"), 500, "thumbnail_failed"},
	} {
		s := documentsServer(&previewDocumentsStore{})
		s.SetFileOpener(&fakeFileOpener{})
		s.SetDocumentPreviewer(&apiPreviewer{err: c.err})
		status, body := documentsRequest(t, s, "GET", "/api/documents/"+documentsOwner+"/thumbnail", "")
		if status != c.status || body["error"] != c.code || body["message"] != nil {
			t.Fatalf("%v=%d %v", c.err, status, body)
		}
	}
	s := documentsServer(&previewDocumentsStore{})
	s.SetFileOpener(&fakeFileOpener{err: fs.ErrNotExist})
	s.SetDocumentPreviewer(&apiPreviewer{})
	if code, _ := documentsRequest(t, s, "GET", "/api/documents/"+documentsOwner+"/thumbnail", ""); code != 404 {
		t.Fatal(code)
	}
	for _, s := range []*Server{documentsServer(nil), documentsServer(&previewDocumentsStore{})} {
		if code, _ := documentsRequest(t, s, "GET", "/api/documents/"+documentsOwner+"/thumbnail", ""); code != 503 {
			t.Fatal(code)
		}
	}
}

type apiIngestionReader struct {
	identity string
	state    *arcadedb.IngestState
	err      error
}

func (f *apiIngestionReader) IngestState(_ context.Context, identity string) (*arcadedb.IngestState, error) {
	f.identity = identity
	return f.state, f.err
}
func TestDocumentIngestionPreservesNativeState(t *testing.T) {
	reader := &apiIngestionReader{}
	s := documentsServer(nil)
	s.SetDocumentIngestionReader(reader)
	for _, state := range []*arcadedb.IngestState{nil, {Status: "running", ObservedAt: "now", InProgress: 5, Finished: 20, Errors: 3}, {Status: "ready", ObservedAt: "then", Errors: 2}} {
		reader.state = state
		code, body := documentsRequest(t, s, "GET", "/api/documents/ingestion", "")
		if code != 200 || reader.identity != documentsOwner {
			t.Fatalf("status=%d identity=%s", code, reader.identity)
		}
		if state == nil {
			if value, ok := body["indexing"]; !ok || value != nil {
				t.Fatalf("missing=%v", body)
			}
			continue
		}
		v := body["indexing"].(map[string]any)
		if v["status"] != state.Status || v["observed_at"] != state.ObservedAt || v["errors"] != float64(state.Errors) || v["finished"] != float64(state.Finished) || v["in_progress"] != float64(state.InProgress) {
			t.Fatalf("state altered: %v", v)
		}
	}
	reader.err = errors.New("read failed")
	if code, _ := documentsRequest(t, s, "GET", "/api/documents/ingestion", ""); code != 500 {
		t.Fatal(code)
	}
	if code, _ := documentsRequest(t, documentsServer(nil), "GET", "/api/documents/ingestion", ""); code != 503 {
		t.Fatal(code)
	}
}
