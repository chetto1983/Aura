package documents

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/config"
)

const testDoclingAPIKey = "test-docling-key-do-not-log"

func testDocumentConfig(baseURL string) config.DocumentConfig {
	return config.DocumentConfig{
		DoclingBaseURL:  baseURL,
		DoclingAPIKey:   testDoclingAPIKey,
		DoclingImage:    config.DoclingImageV1290,
		ConvertTimeout:  2 * time.Second,
		MaxRequestBytes: 1 << 20,
		MaxPages:        100,
		ChunkMaxTokens:  256,
		ChunkTokenizer:  config.DoclingTokenizer,
	}
}

func writeDoclingJSON(t *testing.T, writer http.ResponseWriter, body string) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(writer, body)
}

func taskStatusJSON(status string) string {
	return fmt.Sprintf(`{
  "task_id":"task-123","task_type":"chunk","task_status":%q,
  "task_position":null,"task_meta":{"num_docs":1,"num_processed":0,
  "num_succeeded":0,"num_partially_succeeded":0,"num_failed":0},
  "error_message":null,"failure":null
}`, status)
}

func successResultJSON(status string) string {
	return fmt.Sprintf(`{
  "chunks":[{"filename":"invoice.pdf","chunk_index":0,"text":"Heading\nbody",
    "raw_text":"body","num_tokens":4,"headings":["Heading"],"captions":null,
    "doc_items":["#/texts/0"],"page_numbers":[1],"metadata":{"origin":"body"}}],
  "documents":[{"kind":"ExportResult","content":{"filename":"invoice.pdf",
    "md_content":null,"json_content":{"name":"invoice"},"html_content":null,
    "text_content":null,"doctags_content":null,"doclang_content":null},
    "status":%q,"errors":[],"timings":{},"confidence":null}],
  "processing_time":0.25
}`, status)
}

func TestDoclingClientChunkFileAsyncContract(t *testing.T) {
	t.Parallel()
	var polls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Api-Key") != testDoclingAPIKey {
			t.Errorf("X-Api-Key was not set")
		}
		if request.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept = %q", request.Header.Get("Accept"))
		}
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/v1/chunk/hybrid/file/async":
			if err := request.ParseMultipartForm(2 << 20); err != nil {
				t.Errorf("ParseMultipartForm: %v", err)
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			file, header, err := request.FormFile("files")
			if err != nil {
				t.Errorf("FormFile: %v", err)
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			defer func() { _ = file.Close() }()
			content, _ := io.ReadAll(file)
			if header.Filename != "invoice.pdf" || string(content) != "private document" {
				t.Errorf("file = (%q, %q)", header.Filename, content)
			}
			expected := map[string]string{
				"include_converted_doc":             "true",
				"target_type":                       "inbody",
				"convert_pipeline":                  "standard",
				"convert_document_timeout":          "2",
				"convert_abort_on_error":            "false",
				"convert_image_export_mode":         "placeholder",
				"convert_do_ocr":                    "true",
				"convert_do_table_structure":        "true",
				"convert_do_pdf_heading_hierarchy":  "false",
				"convert_include_images":            "false",
				"convert_include_page_images":       "false",
				"convert_do_code_enrichment":        "false",
				"convert_do_formula_enrichment":     "false",
				"convert_do_picture_classification": "false",
				"convert_do_chart_extraction":       "false",
				"convert_do_picture_description":    "false",
				"chunking_max_tokens":               "256",
				"chunking_tokenizer":                config.DoclingTokenizer,
				"chunking_merge_peers":              "true",
				"chunking_include_raw_text":         "true",
				"chunking_use_markdown_tables":      "true",
			}
			for key, want := range expected {
				if got := request.FormValue(key); got != want {
					t.Errorf("field %s = %q, want %q", key, got, want)
				}
			}
			writeDoclingJSON(t, writer, taskStatusJSON("pending"))
		case request.Method == http.MethodGet && request.URL.Path == "/v1/status/poll/task-123":
			if request.URL.Query().Get("wait") != "0" {
				t.Errorf("wait = %q", request.URL.Query().Get("wait"))
			}
			status := "started"
			if polls.Add(1) > 1 {
				status = "success"
			}
			writeDoclingJSON(t, writer, taskStatusJSON(status))
		case request.Method == http.MethodGet && request.URL.Path == "/v1/result/task-123":
			writeDoclingJSON(t, writer, successResultJSON("success"))
		default:
			t.Errorf("unexpected request %s %s", request.Method, request.URL.String())
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := NewDoclingClient(testDocumentConfig(server.URL), WithDoclingHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewDoclingClient: %v", err)
	}
	client.pollPeriod = time.Millisecond
	result, err := client.ChunkFile(context.Background(), DoclingFile{
		Name: "../../private/invoice.pdf", ContentType: "application/pdf",
		Size: int64(len("private document")), Reader: strings.NewReader("private document"),
	})
	if err != nil {
		t.Fatalf("ChunkFile: %v", err)
	}
	if len(result.Chunks) != 1 || result.Chunks[0].ChunkIndex != 0 || result.Partial {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(string(result.DocumentJSON), `"name":"invoice"`) {
		t.Fatalf("DocumentJSON = %s", result.DocumentJSON)
	}
}

func TestDoclingClientReturnsUsablePartialResult(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodPost:
			_, _ = io.Copy(io.Discard, request.Body)
			writeDoclingJSON(t, writer, taskStatusJSON("pending"))
		case strings.HasPrefix(request.URL.Path, "/v1/status/poll/"):
			writeDoclingJSON(t, writer, taskStatusJSON("partial_success"))
		case strings.HasPrefix(request.URL.Path, "/v1/result/"):
			writeDoclingJSON(t, writer, successResultJSON("partial_success"))
		}
	}))
	defer server.Close()
	client, err := NewDoclingClient(testDocumentConfig(server.URL), WithDoclingHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewDoclingClient: %v", err)
	}
	result, err := client.ChunkFile(context.Background(), DoclingFile{
		Name: "invoice.pdf", Size: 1, Reader: strings.NewReader("x"),
	})
	var doclingErr *DoclingError
	if !errors.As(err, &doclingErr) || doclingErr.Class != DoclingErrorPartialConversion || !result.Partial {
		t.Fatalf("result partial=%v, err=%v", result.Partial, err)
	}
	if len(result.Chunks) != 1 {
		t.Fatalf("chunks = %d", len(result.Chunks))
	}
}

func TestDoclingClientFailureClassificationAndSafeErrors(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writeDoclingJSON(t, writer, `{
  "task_id":"task-123","task_type":"chunk","task_status":"failure",
  "task_position":null,"task_meta":null,"error_message":"private document failed",
  "failure":{"category":"capacity","message":"secret upstream detail",
    "retryable":true,"phase":"admission","details":{"queue":"full"}}
}`)
	}))
	defer server.Close()
	client, err := NewDoclingClient(testDocumentConfig(server.URL), WithDoclingHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewDoclingClient: %v", err)
	}
	_, err = client.Wait(context.Background(), "task-123")
	var doclingErr *DoclingError
	if !errors.As(err, &doclingErr) || doclingErr.Class != DoclingErrorCapacity || !doclingErr.Retryable {
		t.Fatalf("err = %#v", err)
	}
	for _, forbidden := range []string{"secret", "private document", testDoclingAPIKey, server.URL} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("safe error contains %q: %v", forbidden, err)
		}
	}
}

func TestDoclingClientRejectsUnsafeTaskIDAndMismatchedSize(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		_, _ = io.Copy(io.Discard, request.Body)
		writeDoclingJSON(t, writer, taskStatusJSON("pending"))
	}))
	defer server.Close()
	client, err := NewDoclingClient(testDocumentConfig(server.URL), WithDoclingHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewDoclingClient: %v", err)
	}
	_, err = client.Result(context.Background(), "../task-123")
	var doclingErr *DoclingError
	if !errors.As(err, &doclingErr) || doclingErr.Class != DoclingErrorInvalidTaskID || requests.Load() != 0 {
		t.Fatalf("unsafe ID: requests=%d err=%v", requests.Load(), err)
	}
	_, err = client.SubmitFile(context.Background(), DoclingFile{
		Name: "short.pdf", Size: 4, Reader: strings.NewReader("abc"),
	})
	if !errors.As(err, &doclingErr) || doclingErr.Class != DoclingErrorPolicy {
		t.Fatalf("short upload err = %v", err)
	}
	_, err = client.SubmitFile(context.Background(), DoclingFile{
		Name: "long.pdf", Size: 3, Reader: strings.NewReader("abcd"),
	})
	if !errors.As(err, &doclingErr) || doclingErr.Class != DoclingErrorRequestTooLarge {
		t.Fatalf("long upload err = %v", err)
	}
}
