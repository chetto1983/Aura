package documents

import (
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- ids.go error/edge paths ---

func TestContentHashPathOpensFileAndHashesContent(t *testing.T) {
	path := writeTempFile(t, "payload")
	got, err := ContentHashPath(path)
	if err != nil {
		t.Fatal(err)
	}
	// sha256("payload") is stable and deterministic.
	want := "239f59ed55e737c77147cf55ad0c1b030b6d7ee748a7426952f9b852d5a935e5"
	if got != want {
		t.Fatalf("hash = %q, want %q", got, want)
	}
}

func TestContentHashPathReturnsErrorForMissingFile(t *testing.T) {
	_, err := ContentHashPath(t.TempDir() + "/does-not-exist.pdf")
	if err == nil {
		t.Fatal("want error for missing file")
	}
}

func TestContentHashReaderPropagatesReadError(t *testing.T) {
	_, err := ContentHashReader(failingReader{err: errors.New("disk read failed")})
	if err == nil || !strings.Contains(err.Error(), "disk read failed") {
		t.Fatalf("want read error, got %v", err)
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

func TestBuildExtractedDocumentRejectsNilResponse(t *testing.T) {
	_, err := BuildExtractedDocument(IngestRequest{SourceID: "cli"}, "hash", nil, time.Unix(10, 0))
	if err == nil || !strings.Contains(err.Error(), "nil") {
		t.Fatalf("want nil response error, got %v", err)
	}
}

func TestBuildExtractedDocumentRejectsChunkEmptyAfterNormalize(t *testing.T) {
	resp := &ExtractorResponse{
		Chunks: []ExtractedChunk{
			{Kind: "page", Text: "real", Locator: Locator{Page: 1}},
			{Kind: "page", Text: " \t\n  ", Locator: Locator{Page: 2}},
		},
	}
	_, err := BuildExtractedDocument(IngestRequest{SourceID: "cli"}, "hash", resp, time.Unix(10, 0))
	if err == nil || !strings.Contains(err.Error(), "chunk 1 is empty") {
		t.Fatalf("want empty-chunk error for index 1, got %v", err)
	}
}

func TestBuildExtractedDocumentFallsBackToResponseMIMEAndCopiesHeadingPath(t *testing.T) {
	resp := &ExtractorResponse{
		Title:    "Manual",
		MIMEType: "application/pdf",
		Chunks: []ExtractedChunk{
			{Kind: "page", Text: "hello", Locator: Locator{Page: 1}, HeadingPath: []string{"Safety", "Reset"}},
		},
	}
	// req.MIMEType empty -> doc.MIMEType inherits resp.MIMEType.
	doc, err := BuildExtractedDocument(IngestRequest{SourceID: "cli"}, "hash", resp, time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	if doc.MIMEType != "application/pdf" {
		t.Fatalf("mime type = %q, want fallback from response", doc.MIMEType)
	}
	// HeadingPath must be a defensive copy, not an alias of the extractor chunk's slice.
	resp.Chunks[0].HeadingPath[0] = "MUTATED"
	if doc.Chunks[0].HeadingPath[0] != "Safety" {
		t.Fatalf("heading path was aliased, got %v", doc.Chunks[0].HeadingPath)
	}
}

// --- extract_client.go error paths ---

func TestExtractClientRequiresBaseURL(t *testing.T) {
	_, err := (&ExtractClient{}).ExtractFile(t.Context(), "irrelevant", IngestRequest{})
	if err == nil || !strings.Contains(err.Error(), "base URL") {
		t.Fatalf("want base URL error, got %v", err)
	}
}

func TestExtractClientFailsWhenFileMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The body pipe closes with the os.Open error before the server reads it.
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	client := &ExtractClient{BaseURL: srv.URL, Client: srv.Client()}
	_, err := client.ExtractFile(t.Context(), t.TempDir()+"/missing.pdf", IngestRequest{FileName: "missing.pdf"})
	if err == nil {
		t.Fatal("want error when file cannot be opened")
	}
}

func TestExtractClientRejectsEmptyChunkList(t *testing.T) {
	path := writeTempFile(t, "payload")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		drainMultipart(t, r)
		_, _ = w.Write([]byte(`{"title":"x","chunks":[]}`))
	}))
	defer srv.Close()

	client := &ExtractClient{BaseURL: srv.URL, Client: srv.Client()}
	_, err := client.ExtractFile(t.Context(), path, IngestRequest{FileName: "doc.txt"})
	if err == nil || !strings.Contains(err.Error(), "no chunks") {
		t.Fatalf("want no-chunks error, got %v", err)
	}
}

func TestExtractClientRejectsMalformedJSON(t *testing.T) {
	path := writeTempFile(t, "payload")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		drainMultipart(t, r)
		_, _ = w.Write([]byte(`{not json`))
	}))
	defer srv.Close()

	client := &ExtractClient{BaseURL: srv.URL, Client: srv.Client()}
	_, err := client.ExtractFile(t.Context(), path, IngestRequest{FileName: "doc.txt"})
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("want decode error, got %v", err)
	}
}

func TestExtractClientFallsBackToFileNameFromPath(t *testing.T) {
	path := writeNamedTempFile(t, "report.docx", "payload")
	var sawName string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mr, err := r.MultipartReader()
		if err != nil {
			t.Fatalf("multipart reader: %v", err)
		}
		for {
			part, err := mr.NextPart()
			if errors.Is(err, http.ErrBodyReadAfterClose) || err != nil {
				break
			}
			if part.FormName() == "file" {
				sawName = part.FileName()
			}
		}
		_, _ = w.Write([]byte(`{"title":"x","chunks":[{"kind":"text","text":"hi"}]}`))
	}))
	defer srv.Close()

	client := &ExtractClient{BaseURL: srv.URL, Client: srv.Client()}
	// req.FileName empty -> derives base name from path.
	if _, err := client.ExtractFile(t.Context(), path, IngestRequest{}); err != nil {
		t.Fatal(err)
	}
	if sawName != "report.docx" {
		t.Fatalf("multipart file name = %q, want report.docx", sawName)
	}
}

func TestValidateExtractorResponseRejectsNil(t *testing.T) {
	if err := validateExtractorResponse(nil); err == nil || !strings.Contains(err.Error(), "nil") {
		t.Fatalf("want nil error, got %v", err)
	}
}

// --- search.go value-helper default branches ---

func TestStringValueReturnsEmptyForNonString(t *testing.T) {
	if got := stringValue(123); got != "" {
		t.Fatalf("stringValue(int) = %q, want empty", got)
	}
	if got := stringValue(nil); got != "" {
		t.Fatalf("stringValue(nil) = %q, want empty", got)
	}
}

func TestFloatValueReturnsNaNForUnknownType(t *testing.T) {
	if got := floatValue("not-a-number"); !math.IsNaN(got) {
		t.Fatalf("floatValue(string) = %f, want NaN", got)
	}
	if got := floatValue(nil); !math.IsNaN(got) {
		t.Fatalf("floatValue(nil) = %f, want NaN", got)
	}
}

func TestSearchHitFromRowRejectsInvalidLocatorJSON(t *testing.T) {
	fake := &fakeKnowledgeClient{
		readRows: []map[string]any{
			{"document_id": "doc_1", "chunk_id": "c1", "locator_json": "{not json"},
		},
	}
	searcher := &Searcher{Client: fake}
	_, err := searcher.Search(t.Context(), SearchRequest{Query: "reset"})
	if err == nil || !strings.Contains(err.Error(), "decode locator") {
		t.Fatalf("want decode locator error, got %v", err)
	}
}

// --- service.go normalizeIngestRequest MIME fallback ---

func TestServiceDefaultsMIMEToOctetStreamForUnknownExtension(t *testing.T) {
	// A .docx file with an extension mime.TypeByExtension may not know on a bare runner;
	// drive the octet-stream fallback through an extension with no MIME registration while
	// keeping it in the supported set is not possible, so assert via normalizeIngestRequest
	// directly for an unsupported-but-named path the helper still normalizes.
	got := normalizeIngestRequest(IngestRequest{}, "/tmp/data.unknownext", 7)
	if got.MIMEType != "application/octet-stream" {
		t.Fatalf("mime fallback = %q, want application/octet-stream", got.MIMEType)
	}
	if got.SourceID != "cli" || got.SourceKind != "local" || got.FileName != "data.unknownext" {
		t.Fatalf("normalized request = %#v", got)
	}
	if got.OriginalPath != "/tmp/data.unknownext" || got.SizeBytes != 7 {
		t.Fatalf("normalized request = %#v", got)
	}
}

func TestServicePreservesProvidedIngestRequestFields(t *testing.T) {
	got := normalizeIngestRequest(IngestRequest{
		SourceID:     "telegram",
		SourceKind:   "chat",
		OriginalPath: "/orig/path.pdf",
		FileName:     "custom.pdf",
		MIMEType:     "application/pdf",
	}, "/tmp/path.pdf", 99)
	if got.SourceID != "telegram" || got.SourceKind != "chat" || got.OriginalPath != "/orig/path.pdf" {
		t.Fatalf("normalized request mutated provided fields: %#v", got)
	}
	if got.FileName != "custom.pdf" || got.MIMEType != "application/pdf" || got.SizeBytes != 99 {
		t.Fatalf("normalized request = %#v", got)
	}
}
