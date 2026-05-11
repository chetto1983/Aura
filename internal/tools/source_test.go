package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/ocr"
	"github.com/aura/aura/internal/source"
)

type fakeSourceRepository struct {
	dir     string
	nextID  string
	records map[string]*source.Source
}

func newFakeSourceRepository(t *testing.T) *fakeSourceRepository {
	t.Helper()
	return &fakeSourceRepository{
		dir:     t.TempDir(),
		nextID:  "src_0000000000000001",
		records: make(map[string]*source.Source),
	}
}

func (f *fakeSourceRepository) Put(_ context.Context, in source.PutInput) (*source.Source, bool, error) {
	id := f.nextID
	if id == "" {
		id = "src_0000000000000001"
	}
	rec := &source.Source{
		ID:        id,
		Kind:      in.Kind,
		Filename:  in.Filename,
		MimeType:  in.MimeType,
		SHA256:    strings.Repeat("a", 64),
		SizeBytes: int64(len(in.Bytes)),
		CreatedAt: time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC),
		Status:    source.StatusStored,
	}
	f.records[id] = rec
	return cloneFakeSource(rec), false, nil
}

func (f *fakeSourceRepository) Get(id string) (*source.Source, error) {
	rec, ok := f.records[id]
	if !ok {
		return nil, os.ErrNotExist
	}
	return cloneFakeSource(rec), nil
}

func (f *fakeSourceRepository) List(filter source.ListFilter) ([]*source.Source, error) {
	var out []*source.Source
	for _, rec := range f.records {
		if filter.Kind != "" && rec.Kind != filter.Kind {
			continue
		}
		if filter.Status != "" && rec.Status != filter.Status {
			continue
		}
		out = append(out, cloneFakeSource(rec))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (f *fakeSourceRepository) Update(id string, mutator func(*source.Source) error) (*source.Source, error) {
	rec, ok := f.records[id]
	if !ok {
		return nil, os.ErrNotExist
	}
	cp := cloneFakeSource(rec)
	if err := mutator(cp); err != nil {
		return nil, err
	}
	f.records[id] = cp
	return cloneFakeSource(cp), nil
}

func (f *fakeSourceRepository) Delete(_ context.Context, id string) error {
	if _, ok := f.records[id]; !ok {
		return os.ErrNotExist
	}
	delete(f.records, id)
	_ = os.RemoveAll(filepath.Join(f.dir, id))
	return nil
}

func (f *fakeSourceRepository) Path(id, name string) string {
	if _, ok := f.records[id]; !ok || name == "" || strings.ContainsAny(name, `/\`) {
		return ""
	}
	path := filepath.Join(f.dir, id, name)
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	return path
}

func cloneFakeSource(src *source.Source) *source.Source {
	if src == nil {
		return nil
	}
	cp := *src
	cp.WikiPages = append([]string(nil), src.WikiPages...)
	return &cp
}

// newTestSourceStore creates an isolated source.Store rooted at a temp wiki dir.
func newTestSourceStore(t *testing.T) *source.Store {
	t.Helper()
	store, err := source.NewStore(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	return store
}

func TestSourceToolsAcceptRepositoryInterface(t *testing.T) {
	repo := newFakeSourceRepository(t)
	storeTool := NewStoreSourceTool(repo)
	out, err := storeTool.Execute(t.Context(), map[string]any{
		"kind":     "text",
		"filename": "note.txt",
		"content":  "hello from fake repository",
	})
	if err != nil {
		t.Fatalf("store Execute: %v", err)
	}
	if !strings.Contains(out, "src_0000000000000001") {
		t.Fatalf("store output = %q", out)
	}

	readTool := NewReadSourceTool(repo)
	out, err = readTool.Execute(t.Context(), map[string]any{
		"source_id": "src_0000000000000001",
		"mode":      "metadata",
	})
	if err != nil {
		t.Fatalf("read Execute: %v", err)
	}
	if !strings.Contains(out, "Filename: note.txt") {
		t.Fatalf("read output = %q", out)
	}

	listTool := NewListSourcesTool(repo)
	out, err = listTool.Execute(t.Context(), map[string]any{"kind": "text"})
	if err != nil {
		t.Fatalf("list Execute: %v", err)
	}
	if !strings.Contains(out, "note.txt") {
		t.Fatalf("list output = %q", out)
	}

	lintTool := NewLintSourcesTool(repo)
	out, err = lintTool.Execute(t.Context(), nil)
	if err != nil {
		t.Fatalf("lint Execute: %v", err)
	}
	if !strings.Contains(out, "Stored, awaiting OCR") {
		t.Fatalf("lint output = %q", out)
	}
}

func TestStoreSourceTool_TextAndDedup(t *testing.T) {
	store := newTestSourceStore(t)
	tool := NewStoreSourceTool(store)

	if tool.Name() != "store_source" {
		t.Fatalf("Name = %q", tool.Name())
	}
	if tool.Description() == "" {
		t.Fatal("Description is empty")
	}
	if tool.Parameters()["type"] != "object" {
		t.Fatal("Parameters not an object schema")
	}

	out, err := tool.Execute(context.Background(), map[string]any{
		"kind":     "text",
		"filename": "note.txt",
		"content":  "hello aura",
	})
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	if !strings.HasPrefix(out, "Stored source src_") {
		t.Errorf("first call output = %q, want Stored prefix", out)
	}
	if !strings.Contains(out, "kind=text") || !strings.Contains(out, "status=stored") {
		t.Errorf("missing summary fields in %q", out)
	}

	// Same content → dedup, returns existing id.
	out2, err := tool.Execute(context.Background(), map[string]any{
		"kind":     "text",
		"filename": "note-renamed.txt",
		"content":  "hello aura",
	})
	if err != nil {
		t.Fatalf("dedup Execute: %v", err)
	}
	if !strings.HasPrefix(out2, "Already stored source src_") {
		t.Errorf("dedup output = %q, want Already stored prefix", out2)
	}
}

func TestStoreSourceTool_URLAndValidation(t *testing.T) {
	store := newTestSourceStore(t)
	tool := NewStoreSourceTool(store)

	out, err := tool.Execute(context.Background(), map[string]any{
		"kind":     "url",
		"filename": "arxiv-paper",
		"content":  "https://arxiv.org/abs/2401.0001",
	})
	if err != nil {
		t.Fatalf("Execute url: %v", err)
	}
	if !strings.Contains(out, "kind=url") {
		t.Errorf("kind not echoed: %q", out)
	}

	cases := []struct {
		name string
		args map[string]any
	}{
		{"missing kind", map[string]any{"filename": "x", "content": "y"}},
		{"missing filename", map[string]any{"kind": "text", "content": "y"}},
		{"missing content", map[string]any{"kind": "text", "filename": "x"}},
		{"bad kind", map[string]any{"kind": "pdf", "filename": "x", "content": "y"}},
		{"empty content", map[string]any{"kind": "text", "filename": "x", "content": "   "}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tool.Execute(context.Background(), tc.args); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}

func TestStoreSourceTool_NilStore(t *testing.T) {
	tool := NewStoreSourceTool(nil)
	if _, err := tool.Execute(context.Background(), map[string]any{"kind": "text", "filename": "x", "content": "y"}); err == nil {
		t.Fatal("expected nil-store error")
	}
}

func TestReadSourceTool_Modes(t *testing.T) {
	store := newTestSourceStore(t)

	src, _, err := store.Put(context.Background(), source.PutInput{
		Kind:     source.KindText,
		Filename: "note.txt",
		MimeType: "text/plain",
		Bytes:    []byte("first line\nsecond line"),
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	read := NewReadSourceTool(store)
	if read.Name() != "read_source" {
		t.Fatalf("name = %q", read.Name())
	}

	// metadata mode
	meta, err := read.Execute(context.Background(), map[string]any{
		"source_id": src.ID,
		"mode":      "metadata",
	})
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	for _, want := range []string{src.ID, "Filename: note.txt", "Kind: text", "Status: stored", "SHA256: " + src.SHA256} {
		if !strings.Contains(meta, want) {
			t.Errorf("metadata missing %q in %q", want, meta)
		}
	}

	// excerpt mode falls back to original.txt for text kind when no ocr.md.
	excerpt, err := read.Execute(context.Background(), map[string]any{"source_id": src.ID})
	if err != nil {
		t.Fatalf("excerpt: %v", err)
	}
	if !strings.Contains(excerpt, "first line") {
		t.Errorf("excerpt missing original content: %q", excerpt)
	}

	// pdf without ocr.md → error directing user to run ocr_source.
	pdf, _, err := store.Put(context.Background(), source.PutInput{
		Kind:     source.KindPDF,
		Filename: "doc.pdf",
		MimeType: "application/pdf",
		Bytes:    []byte("%PDF-1.4 dummy"),
	})
	if err != nil {
		t.Fatalf("put pdf: %v", err)
	}
	if _, err := read.Execute(context.Background(), map[string]any{"source_id": pdf.ID, "mode": "ocr"}); err == nil {
		t.Fatal("expected ocr.md-not-found error")
	} else if !strings.Contains(err.Error(), "run ocr_source") {
		t.Errorf("error should hint ocr_source: %v", err)
	}

	// unknown id
	if _, err := read.Execute(context.Background(), map[string]any{"source_id": "src_0000000000000000"}); err == nil {
		t.Fatal("expected not-found error")
	}

	// invalid id (not src_<16hex>)
	if _, err := read.Execute(context.Background(), map[string]any{"source_id": "../../etc/passwd"}); err == nil {
		t.Fatal("expected invalid-id error")
	}

	// bad mode
	if _, err := read.Execute(context.Background(), map[string]any{"source_id": src.ID, "mode": "bogus"}); err == nil {
		t.Fatal("expected bad-mode error")
	}
}

func TestReadSourceTool_OCRMarkdownPath(t *testing.T) {
	store := newTestSourceStore(t)
	pdf, _, err := store.Put(context.Background(), source.PutInput{
		Kind:     source.KindPDF,
		Filename: "doc.pdf",
		MimeType: "application/pdf",
		Bytes:    []byte("%PDF-1.4 dummy"),
	})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := os.WriteFile(store.Path(pdf.ID, "ocr.md"), []byte("# Source OCR: doc.pdf\n\n## Page 1\n\nbody"), 0o644); err != nil {
		t.Fatalf("write ocr.md: %v", err)
	}

	read := NewReadSourceTool(store)
	out, err := read.Execute(context.Background(), map[string]any{"source_id": pdf.ID, "mode": "ocr"})
	if err != nil {
		t.Fatalf("read ocr: %v", err)
	}
	if !strings.Contains(out, "# Source OCR: doc.pdf") {
		t.Errorf("ocr.md not returned: %q", out)
	}
}

func TestListSourcesTool_FilterAndLimit(t *testing.T) {
	store := newTestSourceStore(t)

	for _, body := range []string{"text-1", "text-2", "text-3"} {
		if _, _, err := store.Put(context.Background(), source.PutInput{
			Kind:     source.KindText,
			Filename: body + ".txt",
			MimeType: "text/plain",
			Bytes:    []byte(body),
		}); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	if _, _, err := store.Put(context.Background(), source.PutInput{
		Kind:     source.KindURL,
		Filename: "site",
		MimeType: "text/x-uri",
		Bytes:    []byte("https://example.com"),
	}); err != nil {
		t.Fatalf("put url: %v", err)
	}

	list := NewListSourcesTool(store)
	if list.Parameters()["required"] != nil {
		t.Fatal("list_sources should have no required params")
	}

	all, err := list.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if !strings.Contains(all, "Sources (4") {
		t.Errorf("expected 4 sources in %q", all)
	}

	textOnly, err := list.Execute(context.Background(), map[string]any{"kind": "text"})
	if err != nil {
		t.Fatalf("list text: %v", err)
	}
	if !strings.Contains(textOnly, "Sources (3") || !strings.Contains(textOnly, "kind=text") {
		t.Errorf("text filter wrong: %q", textOnly)
	}
	if strings.Contains(textOnly, "https://example.com") {
		t.Errorf("text filter should not include url source: %q", textOnly)
	}

	limited, err := list.Execute(context.Background(), map[string]any{"limit": 2})
	if err != nil {
		t.Fatalf("limit: %v", err)
	}
	if !strings.Contains(limited, "Sources (2") || !strings.Contains(limited, "truncated") {
		t.Errorf("limit truncation missing: %q", limited)
	}
}

func TestListSourcesTool_Empty(t *testing.T) {
	store := newTestSourceStore(t)
	list := NewListSourcesTool(store)
	out, err := list.Execute(context.Background(), map[string]any{"status": "ingested"})
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if !strings.Contains(out, "No sources match") {
		t.Errorf("empty message wrong: %q", out)
	}
}

func TestLintSourcesTool_Buckets(t *testing.T) {
	store := newTestSourceStore(t)

	stored, _, err := store.Put(context.Background(), source.PutInput{
		Kind:     source.KindPDF,
		Filename: "stored.pdf",
		MimeType: "application/pdf",
		Bytes:    []byte("%PDF stored"),
	})
	if err != nil {
		t.Fatalf("put stored: %v", err)
	}

	complete, _, err := store.Put(context.Background(), source.PutInput{
		Kind:     source.KindPDF,
		Filename: "complete.pdf",
		MimeType: "application/pdf",
		Bytes:    []byte("%PDF complete"),
	})
	if err != nil {
		t.Fatalf("put complete: %v", err)
	}
	if _, err := store.Update(complete.ID, func(s *source.Source) error {
		s.Status = source.StatusOCRComplete
		s.OCRModel = "mistral-ocr-latest"
		s.PageCount = 3
		return nil
	}); err != nil {
		t.Fatalf("update complete: %v", err)
	}

	failed, _, err := store.Put(context.Background(), source.PutInput{
		Kind:     source.KindPDF,
		Filename: "failed.pdf",
		MimeType: "application/pdf",
		Bytes:    []byte("%PDF failed"),
	})
	if err != nil {
		t.Fatalf("put failed: %v", err)
	}
	if _, err := store.Update(failed.ID, func(s *source.Source) error {
		s.Status = source.StatusFailed
		s.Error = "boom"
		return nil
	}); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	ingested, _, err := store.Put(context.Background(), source.PutInput{
		Kind:     source.KindPDF,
		Filename: "ingested.pdf",
		MimeType: "application/pdf",
		Bytes:    []byte("%PDF ingested"),
	})
	if err != nil {
		t.Fatalf("put ingested: %v", err)
	}
	if _, err := store.Update(ingested.ID, func(s *source.Source) error {
		s.Status = source.StatusIngested
		return nil
	}); err != nil {
		t.Fatalf("update ingested: %v", err)
	}

	lint := NewLintSourcesTool(store)
	out, err := lint.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("lint: %v", err)
	}
	for _, want := range []string{
		"Stored, awaiting OCR (1)",
		stored.ID,
		"OCR complete, awaiting ingest (1)",
		complete.ID,
		"Failed (1)",
		failed.ID,
		"error: boom",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lint output missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, ingested.ID) {
		t.Errorf("lint should not include ingested source: %s", out)
	}
}

func TestLintSourcesTool_AllClean(t *testing.T) {
	store := newTestSourceStore(t)
	lint := NewLintSourcesTool(store)
	out, err := lint.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("lint: %v", err)
	}
	if !strings.Contains(out, "No sources need attention") {
		t.Errorf("clean output wrong: %q", out)
	}
}

func TestOCRSourceTool_NoClient(t *testing.T) {
	store := newTestSourceStore(t)
	tool := NewOCRSourceTool(store, nil)
	if _, err := tool.Execute(context.Background(), map[string]any{"source_id": "src_0000000000000000"}); err == nil {
		t.Fatal("expected disabled-OCR error")
	}
}

func TestOCRSourceTool_RejectsNonPDF(t *testing.T) {
	store := newTestSourceStore(t)
	src, _, err := store.Put(context.Background(), source.PutInput{
		Kind:     source.KindText,
		Filename: "note.txt",
		MimeType: "text/plain",
		Bytes:    []byte("hello"),
	})
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	// Even a non-nil client should still get rejected before any HTTP call.
	client := ocr.New(ocr.Config{APIKey: "k", BaseURL: "http://invalid", Model: "mistral-ocr-latest"})
	tool := NewOCRSourceTool(store, client)
	_, err = tool.Execute(context.Background(), map[string]any{"source_id": src.ID})
	if err == nil || !strings.Contains(err.Error(), "only PDF sources") {
		t.Fatalf("expected PDF-only error, got %v", err)
	}
}

func TestOCRSourceTool_HappyPath(t *testing.T) {
	store := newTestSourceStore(t)
	pdf := []byte("%PDF-1.4 fake")
	src, _, err := store.Put(context.Background(), source.PutInput{
		Kind:     source.KindPDF,
		Filename: "test.pdf",
		MimeType: "application/pdf",
		Bytes:    pdf,
	})
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sanity-check we hit the OCR endpoint with a base64 PDF body.
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if doc, _ := body["document"].(map[string]any); doc == nil || !strings.HasPrefix(doc["document_url"].(string), "data:application/pdf;base64,") {
			t.Errorf("missing base64 document_url: %v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"model": "mistral-ocr-latest",
			"pages": [
				{"index": 0, "markdown": "# Hello\n\nbody"},
				{"index": 1, "markdown": "page 2"}
			],
			"usage_info": {"pages_processed": 2, "doc_size_bytes": 42}
		}`)
	}))
	defer srv.Close()

	client := ocr.New(ocr.Config{APIKey: "k", BaseURL: srv.URL + "/v1", Model: "mistral-ocr-latest"})
	tool := NewOCRSourceTool(store, client)

	out, err := tool.Execute(context.Background(), map[string]any{"source_id": src.ID})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{"OCR complete", src.ID, "2 page(s)", "model=mistral-ocr-latest"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in result %q", want, out)
		}
	}

	// Status flipped + ocr.md/ocr.json written.
	updated, err := store.Get(src.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Status != source.StatusOCRComplete {
		t.Errorf("status = %s, want ocr_complete", updated.Status)
	}
	if updated.PageCount != 2 {
		t.Errorf("page_count = %d, want 2", updated.PageCount)
	}
	if updated.OCRModel == "" {
		t.Errorf("ocr_model not set")
	}

	mdBytes, err := os.ReadFile(filepath.Join(store.RawDir(), src.ID, "ocr.md"))
	if err != nil {
		t.Fatalf("read ocr.md: %v", err)
	}
	if !strings.Contains(string(mdBytes), "# Source OCR: test.pdf") {
		t.Errorf("ocr.md missing header: %s", mdBytes)
	}
	jsonBytes, err := os.ReadFile(filepath.Join(store.RawDir(), src.ID, "ocr.json"))
	if err != nil {
		t.Fatalf("read ocr.json: %v", err)
	}
	if !strings.Contains(string(jsonBytes), "pages_processed") {
		t.Errorf("ocr.json incomplete: %s", jsonBytes)
	}
}

func TestOCRSourceTool_FailureMarksSourceFailed(t *testing.T) {
	store := newTestSourceStore(t)
	src, _, err := store.Put(context.Background(), source.PutInput{
		Kind:     source.KindPDF,
		Filename: "boom.pdf",
		MimeType: "application/pdf",
		Bytes:    []byte("%PDF boom"),
	})
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream broke", http.StatusBadGateway)
	}))
	defer srv.Close()

	client := ocr.New(ocr.Config{APIKey: "k", BaseURL: srv.URL + "/v1", Model: "mistral-ocr-latest"})
	tool := NewOCRSourceTool(store, client)

	if _, err := tool.Execute(context.Background(), map[string]any{"source_id": src.ID}); err == nil {
		t.Fatal("expected upstream error")
	}

	updated, err := store.Get(src.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Status != source.StatusFailed {
		t.Errorf("status = %s, want failed", updated.Status)
	}
	if updated.Error == "" {
		t.Errorf("expected error message recorded")
	}
}

type fakeSourcePurger struct {
	purged []string
	fail   error
}

func (f *fakeSourcePurger) PurgeSource(_ context.Context, id string) error {
	if f.fail != nil {
		return f.fail
	}
	f.purged = append(f.purged, id)
	return nil
}

func TestDeleteSourceTool_RemovesFilesAndPurgesIndex(t *testing.T) {
	ctx := context.Background()
	store := newTestSourceStore(t)
	src, _, err := store.Put(ctx, source.PutInput{
		Kind:     source.KindText,
		Filename: "to-delete.txt",
		MimeType: "text/plain",
		Bytes:    []byte("ephemeral note"),
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	purger := &fakeSourcePurger{}
	tool := NewDeleteSourceTool(store, purger)
	if tool == nil {
		t.Fatal("expected delete_source tool")
	}
	out, err := tool.Execute(ctx, map[string]any{"source_id": src.ID})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "Deleted source "+src.ID) {
		t.Fatalf("output = %q, want delete confirmation", out)
	}
	if _, err := store.Get(src.ID); err == nil {
		t.Fatal("source still readable after delete")
	}
	if len(purger.purged) != 1 || purger.purged[0] != src.ID {
		t.Fatalf("purger calls = %#v, want [%s]", purger.purged, src.ID)
	}
}

func TestDeleteSourceTool_UnknownIDReturnsError(t *testing.T) {
	store := newTestSourceStore(t)
	tool := NewDeleteSourceTool(store, &fakeSourcePurger{})
	_, err := tool.Execute(context.Background(), map[string]any{"source_id": "src_ffffffffffffffff"})
	if err == nil {
		t.Fatal("expected not-found error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error = %v, want 'not found'", err)
	}
}

func TestDeleteSourceTool_PurgeErrorSurfacesAfterFileDelete(t *testing.T) {
	ctx := context.Background()
	store := newTestSourceStore(t)
	src, _, err := store.Put(ctx, source.PutInput{
		Kind:     source.KindText,
		Filename: "purge-fail.txt",
		MimeType: "text/plain",
		Bytes:    []byte("body"),
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	purger := &fakeSourcePurger{fail: errors.New("qdrant down")}
	tool := NewDeleteSourceTool(store, purger)
	if _, err := tool.Execute(ctx, map[string]any{"source_id": src.ID}); err == nil {
		t.Fatal("expected purge error to surface")
	}
	if _, err := store.Get(src.ID); err == nil {
		// File deletion must still have happened — we want this irreversible
		// half completed so the operator can rebuild the index next.
		t.Fatal("source files survived a partial delete; expected files-gone + warning")
	}
}
