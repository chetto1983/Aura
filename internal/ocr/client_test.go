package ocr

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProcessSuccess(t *testing.T) {
	pdf := []byte("%PDF-1.4 dummy")
	wantB64 := base64.StdEncoding.EncodeToString(pdf)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/ocr" {
			t.Errorf("path = %q, want /v1/ocr", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want Bearer test-key", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}

		var body OCRRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Model != "mistral-ocr-latest" {
			t.Errorf("model = %q", body.Model)
		}
		if body.Document.Type != "document_url" {
			t.Errorf("document.type = %q", body.Document.Type)
		}
		if !strings.HasPrefix(body.Document.DocumentURL, "data:application/pdf;base64,") {
			t.Errorf("documentURL prefix wrong: %q", body.Document.DocumentURL[:40])
		}
		if !strings.HasSuffix(body.Document.DocumentURL, wantB64) {
			t.Errorf("documentURL b64 mismatch")
		}
		if body.IncludeImageBase64 {
			t.Errorf("IncludeImageBase64 = true, want false by default")
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"model": "mistral-ocr-2512+1",
			"pages": [
				{"index": 0, "markdown": "# Title\n\nPara"},
				{"index": 1, "markdown": "Page 2 body"}
			],
			"usage_info": {"pages_processed": 2, "doc_size_bytes": 14}
		}`)
	}))
	defer srv.Close()

	c := New(Config{APIKey: "test-key", BaseURL: srv.URL + "/v1", Model: "mistral-ocr-latest"})
	res, err := c.Process(context.Background(), ProcessInput{PDFBytes: pdf})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(res.Response.Pages) != 2 {
		t.Fatalf("pages: %d", len(res.Response.Pages))
	}
	if res.Response.Pages[0].Markdown != "# Title\n\nPara" {
		t.Errorf("page[0].markdown = %q", res.Response.Pages[0].Markdown)
	}
	if res.Response.UsageInfo == nil || res.Response.UsageInfo.PagesProcessed != 2 {
		t.Errorf("usage missing or wrong: %+v", res.Response.UsageInfo)
	}
	if !strings.Contains(string(res.RawJSON), `"pages_processed": 2`) {
		t.Errorf("raw json not preserved: %s", string(res.RawJSON))
	}
}

func TestProcessIncludeImagesFlag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body OCRRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		if !body.IncludeImageBase64 {
			t.Errorf("IncludeImageBase64 = false, want true")
		}
		_, _ = io.WriteString(w, `{"pages":[]}`)
	}))
	defer srv.Close()

	c := New(Config{APIKey: "k", BaseURL: srv.URL + "/v1"})
	if _, err := c.Process(context.Background(), ProcessInput{PDFBytes: []byte("x"), IncludeImages: true}); err != nil {
		t.Fatalf("Process: %v", err)
	}
}

func TestProcessSendsExtractionFlags(t *testing.T) {
	var got OCRRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = io.WriteString(w, `{"pages":[]}`)
	}))
	defer srv.Close()

	c := New(Config{
		APIKey:        "k",
		BaseURL:       srv.URL + "/v1",
		TableFormat:   "markdown",
		ExtractHeader: true,
		ExtractFooter: true,
	})
	if _, err := c.Process(context.Background(), ProcessInput{PDFBytes: []byte("x")}); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got.TableFormat != "markdown" {
		t.Errorf("TableFormat = %q, want markdown", got.TableFormat)
	}
	if !got.ExtractHeader {
		t.Errorf("ExtractHeader = false, want true")
	}
	if !got.ExtractFooter {
		t.Errorf("ExtractFooter = false, want true")
	}
}

func TestProcessOmitsEmptyExtractionFlags(t *testing.T) {
	// Verify the JSON body doesn't carry the keys when zero-valued. This
	// matters because Mistral may treat "" table_format differently than the
	// key being absent.
	rawBody := []byte{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawBody, _ = io.ReadAll(r.Body)
		_, _ = io.WriteString(w, `{"pages":[]}`)
	}))
	defer srv.Close()

	c := New(Config{APIKey: "k", BaseURL: srv.URL + "/v1"})
	if _, err := c.Process(context.Background(), ProcessInput{PDFBytes: []byte("x")}); err != nil {
		t.Fatalf("Process: %v", err)
	}
	for _, key := range []string{"table_format", "extract_header", "extract_footer", "include_image_base64"} {
		if strings.Contains(string(rawBody), `"`+key+`"`) {
			t.Errorf("body should omit %q when zero, got: %s", key, string(rawBody))
		}
	}
}

func TestProcessHTTP401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"invalid api key"}`)
	}))
	defer srv.Close()

	c := New(Config{APIKey: "bad-key", BaseURL: srv.URL + "/v1"})
	_, err := c.Process(context.Background(), ProcessInput{PDFBytes: []byte("x")})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "HTTP 401") {
		t.Errorf("err = %v, want HTTP 401", err)
	}
	if strings.Contains(err.Error(), "bad-key") {
		t.Errorf("error leaks API key: %v", err)
	}
}

func TestProcessHTTP500SnippetCapped(t *testing.T) {
	long := strings.Repeat("X", 5000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, long)
	}))
	defer srv.Close()

	c := New(Config{APIKey: "k", BaseURL: srv.URL + "/v1"})
	_, err := c.Process(context.Background(), ProcessInput{PDFBytes: []byte("x")})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("err = %v", err)
	}
	// Snippet must be capped (256 chars + ellipsis), not the whole 5000-char body.
	if len(err.Error()) > 600 {
		t.Errorf("error too long (%d chars), snippet cap not applied", len(err.Error()))
	}
}

func TestProcessBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `not json at all`)
	}))
	defer srv.Close()

	c := New(Config{APIKey: "k", BaseURL: srv.URL + "/v1"})
	_, err := c.Process(context.Background(), ProcessInput{PDFBytes: []byte("x")})
	if err == nil || !strings.Contains(err.Error(), "decode response") {
		t.Errorf("err = %v, want decode error", err)
	}
}

func TestProcessEmptyBytesValidation(t *testing.T) {
	c := New(Config{APIKey: "k", BaseURL: "https://example.com/v1"})
	_, err := c.Process(context.Background(), ProcessInput{PDFBytes: nil})
	if err == nil || !strings.Contains(err.Error(), "empty pdf") {
		t.Errorf("err = %v, want empty pdf", err)
	}
}

func TestProcessNoBaseURL(t *testing.T) {
	c := New(Config{APIKey: "k", BaseURL: ""})
	_, err := c.Process(context.Background(), ProcessInput{PDFBytes: []byte("x")})
	if err == nil || !strings.Contains(err.Error(), "base url") {
		t.Errorf("err = %v, want base url error", err)
	}
}

func TestProcessTrailingSlashTolerated(t *testing.T) {
	got := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
		_, _ = io.WriteString(w, `{"pages":[]}`)
	}))
	defer srv.Close()

	c := New(Config{APIKey: "k", BaseURL: srv.URL + "/v1//"})
	if _, err := c.Process(context.Background(), ProcessInput{PDFBytes: []byte("x")}); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got != "/v1/ocr" {
		t.Errorf("path = %q, want /v1/ocr", got)
	}
}

func TestNewDefaultsModel(t *testing.T) {
	c := New(Config{APIKey: "k", BaseURL: "https://x/v1"})
	if c.model != "mistral-ocr-latest" {
		t.Errorf("default model = %q", c.model)
	}
}
