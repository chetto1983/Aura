package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"strings"
	"testing"

	"github.com/aura/aura/internal/source"
)

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

	bad := e.uploadFile("deck.pptx", "application/vnd.openxmlformats-officedocument.presentationml.presentation", []byte("pptx"))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("pptx status = %d, want 400 body=%s", bad.Code, bad.Body.String())
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
