package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"strings"
	"testing"

	"github.com/aura/aura/internal/ingest"
	"github.com/aura/aura/internal/markitdown"
	"github.com/aura/aura/internal/source"
)

// fakeMarkitdown is a markitdown.Converter that returns canned markdown per
// MIME type. Production wiring goes through internal/markitdown.Client; the
// fake skips the sidecar to keep upload tests hermetic.
type fakeMarkitdown struct {
	calls int
	errs  []error
}

func (f *fakeMarkitdown) Convert(_ context.Context, in markitdown.ConvertInput) (markitdown.ConvertResult, error) {
	f.calls++
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		if err != nil {
			return markitdown.ConvertResult{}, err
		}
	}
	switch {
	case strings.Contains(in.MimeType, "spreadsheetml"):
		return markitdown.ConvertResult{Markdown: "| item | cost |\n| --- | --- |\n| sandbox | 12 |\n"}, nil
	case strings.Contains(in.MimeType, "wordprocessingml"):
		return markitdown.ConvertResult{Markdown: "# Memo\n\nAura should remember decisions.\n"}, nil
	default:
		// text/csv/json/markdown — return the input bytes as a code fence.
		return markitdown.ConvertResult{Markdown: "Aura should remember CSV and text files.\n"}, nil
	}
}

func TestSourceUploadAcceptsTextAndRejectsUnsupported(t *testing.T) {
	e := newTestEnv(t)
	rr := e.uploadFile("notes.txt", "text/plain", []byte("Aura should remember CSV and text files."))
	if rr.Code != http.StatusOK {
		t.Fatalf("txt upload status = %d body=%s", rr.Code, rr.Body.String())
	}
	var got UploadResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != string(source.StatusExtractComplete) {
		t.Fatalf("status = %s, want %s", got.Status, source.StatusExtractComplete)
	}
	src, err := e.sources.Get(got.ID)
	if err != nil {
		t.Fatalf("source get: %v", err)
	}
	if src.Kind != source.KindText || src.Extract == nil {
		t.Fatalf("source = %+v, want text with extraction metadata", src)
	}
	extract, err := os.ReadFile(e.sources.Path(got.ID, source.ExtractMarkdownFile))
	if err != nil {
		t.Fatalf("read extract.md: %v", err)
	}
	if !strings.Contains(string(extract), "Aura should remember") {
		t.Fatalf("extract.md = %q", extract)
	}

	// Wave 2.9 accepts pptx via markitdown — image uploads stay gated until
	// Wave 2.9.5 wires the OCRBackend.
	bad := e.uploadFile("photo.png", "image/png", []byte("PNG bytes"))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("image status = %d, want 400 body=%s", bad.Code, bad.Body.String())
	}
}

func TestSourceUploadXLSXUsesSandboxExtraction(t *testing.T) {
	e := newTestEnv(t)
	runner := &fakeMarkitdown{}
	e.router = NewRouter(Deps{
		Wiki:      e.wiki,
		Sources:   e.sources,
		Scheduler: e.sched,
		Markitdown: runner,
	})

	rr := e.uploadFile("budget.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", []byte("xlsx bytes"))
	if rr.Code != http.StatusOK {
		t.Fatalf("xlsx upload status = %d body=%s", rr.Code, rr.Body.String())
	}
	var got UploadResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != string(source.StatusExtractComplete) {
		t.Fatalf("status = %s, want %s", got.Status, source.StatusExtractComplete)
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls = %d, want 1", runner.calls)
	}
	src, err := e.sources.Get(got.ID)
	if err != nil {
		t.Fatalf("source get: %v", err)
	}
	if src.Kind != source.KindXLSX || src.Extract == nil || src.Extract.ExtractorName != "markitdown_xlsx" {
		t.Fatalf("source = %+v, want xlsx with sandbox extraction metadata", src)
	}
	extract, err := os.ReadFile(e.sources.Path(got.ID, source.ExtractMarkdownFile))
	if err != nil {
		t.Fatalf("read extract.md: %v", err)
	}
	if !strings.Contains(string(extract), "sandbox") {
		t.Fatalf("extract.md = %q", extract)
	}
}

func TestSourceUploadXLSXDuplicateFailedRetriesExtraction(t *testing.T) {
	e := newTestEnv(t)
	runner := &fakeMarkitdown{errs: []error{errors.New("markitdown unavailable")}}
	e.router = NewRouter(Deps{
		Wiki:      e.wiki,
		Sources:   e.sources,
		Scheduler: e.sched,
		Markitdown: runner,
	})

	body := []byte("same workbook bytes")
	first := e.uploadFile("budget.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", body)
	if first.Code != http.StatusOK {
		t.Fatalf("first xlsx status = %d body=%s", first.Code, first.Body.String())
	}
	var firstResp UploadResponse
	if err := json.Unmarshal(first.Body.Bytes(), &firstResp); err != nil {
		t.Fatalf("decode first: %v", err)
	}
	if firstResp.Status != string(source.StatusFailed) {
		t.Fatalf("first status = %s, want failed", firstResp.Status)
	}

	second := e.uploadFile("budget.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", body)
	if second.Code != http.StatusOK {
		t.Fatalf("second xlsx status = %d body=%s", second.Code, second.Body.String())
	}
	var secondResp UploadResponse
	if err := json.Unmarshal(second.Body.Bytes(), &secondResp); err != nil {
		t.Fatalf("decode second: %v", err)
	}
	if secondResp.ID != firstResp.ID || !secondResp.Duplicate {
		t.Fatalf("second response = %+v, want duplicate retry of %s", secondResp, firstResp.ID)
	}
	if secondResp.Status != string(source.StatusExtractComplete) {
		t.Fatalf("second status = %s, want extract_complete body=%s", secondResp.Status, second.Body.String())
	}
	if runner.calls != 2 {
		t.Fatalf("runner calls = %d, want retry call", runner.calls)
	}
	src, err := e.sources.Get(secondResp.ID)
	if err != nil {
		t.Fatalf("source get: %v", err)
	}
	if src.Status != source.StatusExtractComplete || src.Error != "" || src.Extract == nil || src.Extract.ExtractorName != "markitdown_xlsx" {
		t.Fatalf("source after retry = %+v", src)
	}
	extract, err := os.ReadFile(e.sources.Path(secondResp.ID, source.ExtractMarkdownFile))
	if err != nil {
		t.Fatalf("read extract.md: %v", err)
	}
	if !strings.Contains(string(extract), "sandbox") {
		t.Fatalf("extract.md = %q", extract)
	}
}

func TestSourceUploadDOCXUsesSandboxExtraction(t *testing.T) {
	e := newTestEnv(t)
	runner := &fakeMarkitdown{}
	e.router = NewRouter(Deps{
		Wiki:      e.wiki,
		Sources:   e.sources,
		Scheduler: e.sched,
		Markitdown: runner,
	})

	rr := e.uploadFile("memo.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", []byte("docx bytes"))
	if rr.Code != http.StatusOK {
		t.Fatalf("docx upload status = %d body=%s", rr.Code, rr.Body.String())
	}
	var got UploadResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != string(source.StatusExtractComplete) {
		t.Fatalf("status = %s, want %s", got.Status, source.StatusExtractComplete)
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls = %d, want 1", runner.calls)
	}
	src, err := e.sources.Get(got.ID)
	if err != nil {
		t.Fatalf("source get: %v", err)
	}
	if src.Kind != source.KindDOCX || src.Extract == nil || src.Extract.ExtractorName != "markitdown_docx" {
		t.Fatalf("source = %+v, want docx with sandbox extraction metadata", src)
	}
	extract, err := os.ReadFile(e.sources.Path(got.ID, source.ExtractMarkdownFile))
	if err != nil {
		t.Fatalf("read extract.md: %v", err)
	}
	if !strings.Contains(string(extract), "decisions") {
		t.Fatalf("extract.md = %q", extract)
	}
}

func TestSourceUploadTextCanBeIngested(t *testing.T) {
	e := newTestEnv(t)
	pipeline, err := ingest.New(ingest.Config{Sources: e.sources, Wiki: e.wiki})
	if err != nil {
		t.Fatalf("ingest.New: %v", err)
	}
	e.router = NewRouter(Deps{
		Wiki:       e.wiki,
		Sources:    e.sources,
		Scheduler:  e.sched,
		Ingest:     pipeline,
		Markitdown: &fakeMarkitdown{},
	})

	rr := e.uploadFile("notes.txt", "text/plain", []byte("Aura should compile text sources into the wiki."))
	if rr.Code != http.StatusOK {
		t.Fatalf("txt upload status = %d body=%s", rr.Code, rr.Body.String())
	}
	var upload UploadResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &upload); err != nil {
		t.Fatalf("decode upload: %v", err)
	}
	if upload.Status != string(source.StatusExtractComplete) {
		t.Fatalf("upload status = %s, want %s", upload.Status, source.StatusExtractComplete)
	}

	ingestRR := e.do("POST", "/sources/"+upload.ID+"/ingest")
	if ingestRR.Code != http.StatusOK {
		t.Fatalf("ingest status = %d body=%s", ingestRR.Code, ingestRR.Body.String())
	}
	var ingested IngestResponse
	if err := json.Unmarshal(ingestRR.Body.Bytes(), &ingested); err != nil {
		t.Fatalf("decode ingest: %v", err)
	}
	if ingested.Status != string(source.StatusIngested) || len(ingested.WikiPages) != 1 {
		t.Fatalf("ingest response = %+v, want ingested wiki page", ingested)
	}
	page, err := e.wiki.ReadPage(ingested.WikiPages[0])
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if !strings.Contains(page.Body, "extract.md") {
		t.Fatalf("page body missing extracted markdown pointer:\n%s", page.Body)
	}
	if strings.Contains(page.Body, "Aura should compile text sources") {
		t.Fatalf("source page should not duplicate extracted text:\n%s", page.Body)
	}
}

func TestSourceUploadXLSXCanBeIngested(t *testing.T) {
	e := newTestEnv(t)
	pipeline, err := ingest.New(ingest.Config{Sources: e.sources, Wiki: e.wiki})
	if err != nil {
		t.Fatalf("ingest.New: %v", err)
	}
	runner := &fakeMarkitdown{}
	e.router = NewRouter(Deps{
		Wiki:      e.wiki,
		Sources:   e.sources,
		Scheduler: e.sched,
		Markitdown: runner,
		Ingest:    pipeline,
	})

	rr := e.uploadFile("budget.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", []byte("xlsx bytes"))
	if rr.Code != http.StatusOK {
		t.Fatalf("xlsx upload status = %d body=%s", rr.Code, rr.Body.String())
	}
	var upload UploadResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &upload); err != nil {
		t.Fatalf("decode upload: %v", err)
	}
	if upload.Status != string(source.StatusExtractComplete) {
		t.Fatalf("upload status = %s, want %s", upload.Status, source.StatusExtractComplete)
	}

	ingestRR := e.do("POST", "/sources/"+upload.ID+"/ingest")
	if ingestRR.Code != http.StatusOK {
		t.Fatalf("ingest status = %d body=%s", ingestRR.Code, ingestRR.Body.String())
	}
	var ingested IngestResponse
	if err := json.Unmarshal(ingestRR.Body.Bytes(), &ingested); err != nil {
		t.Fatalf("decode ingest: %v", err)
	}
	if ingested.Status != string(source.StatusIngested) || len(ingested.WikiPages) != 1 {
		t.Fatalf("ingest response = %+v, want ingested wiki page", ingested)
	}
	page, err := e.wiki.ReadPage(ingested.WikiPages[0])
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	for _, want := range []string{
		"Kind: xlsx",
		"wiki/raw/" + upload.ID + "/extract.md",
	} {
		if !strings.Contains(page.Body, want) {
			t.Fatalf("page body missing %q:\n%s", want, page.Body)
		}
	}
	if strings.Contains(page.Body, "sandbox") {
		t.Fatalf("source page should not duplicate extracted xlsx body:\n%s", page.Body)
	}
}

func (e *testEnv) uploadFile(name, contentType string, body []byte) *httptest.ResponseRecorder {
	e.t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="`+name+`"`)
	if contentType != "" {
		header.Set("Content-Type", contentType)
	}
	part, err := writer.CreatePart(header)
	if err != nil {
		e.t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(body); err != nil {
		e.t.Fatalf("write part: %v", err)
	}
	if err := writer.Close(); err != nil {
		e.t.Fatalf("close multipart: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/sources/upload", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	return rr
}
