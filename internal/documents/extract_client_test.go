package documents

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestExtractClientStreamsMultipartWithoutBufferingFile(t *testing.T) {
	path := writeTempFile(t, strings.Repeat("a", 1024))
	var sawChunked bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/extract" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.ContentLength != -1 {
			t.Fatalf("content length = %d, want chunked/unknown", r.ContentLength)
		}
		sawChunked = true
		mr, err := r.MultipartReader()
		if err != nil {
			t.Fatalf("multipart reader: %v", err)
		}
		var fileBytes int
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("next part: %v", err)
			}
			if part.FormName() == "file" {
				n, err := io.Copy(io.Discard, part)
				if err != nil {
					t.Fatalf("read file part: %v", err)
				}
				fileBytes = int(n)
			}
		}
		if fileBytes != 1024 {
			t.Fatalf("file bytes = %d", fileBytes)
		}
		_ = json.NewEncoder(w).Encode(ExtractorResponse{
			Title: "ok",
			Stats: ExtractorStats{Chunks: 1},
			Chunks: []ExtractedChunk{
				{Kind: "text", Text: "hello", Locator: Locator{Page: 1}},
			},
		})
	}))
	defer srv.Close()

	client := &ExtractClient{BaseURL: srv.URL, Client: srv.Client()}
	resp, err := client.ExtractFile(t.Context(), path, IngestRequest{FileName: "doc.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if !sawChunked {
		t.Fatal("server did not observe chunked upload")
	}
	if resp.Chunks[0].Text != "hello" {
		t.Fatalf("chunk text = %q", resp.Chunks[0].Text)
	}
}

func TestExtractClientRejectsEmptyChunkText(t *testing.T) {
	path := writeTempFile(t, "payload")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		drainMultipart(t, r)
		_ = json.NewEncoder(w).Encode(ExtractorResponse{
			Chunks: []ExtractedChunk{{Kind: "text", Text: "   "}},
		})
	}))
	defer srv.Close()

	client := &ExtractClient{BaseURL: srv.URL, Client: srv.Client()}
	_, err := client.ExtractFile(t.Context(), path, IngestRequest{FileName: "doc.txt"})
	if err == nil || !strings.Contains(err.Error(), "empty chunk") {
		t.Fatalf("want empty chunk error, got %v", err)
	}
}

func TestExtractClientPropagatesSidecarError(t *testing.T) {
	path := writeTempFile(t, "payload")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		drainMultipart(t, r)
		http.Error(w, "bad", http.StatusUnprocessableEntity)
	}))
	defer srv.Close()

	client := &ExtractClient{BaseURL: srv.URL, Client: srv.Client()}
	_, err := client.ExtractFile(t.Context(), path, IngestRequest{FileName: "doc.txt"})
	if err == nil || !strings.Contains(err.Error(), "HTTP 422") {
		t.Fatalf("want HTTP 422 error, got %v", err)
	}
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "doc-*")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteString(content); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	return f.Name()
}

func drainMultipart(t *testing.T, r *http.Request) {
	t.Helper()
	mr, err := r.MultipartReader()
	if err != nil {
		t.Fatalf("multipart reader: %v", err)
	}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatalf("next part: %v", err)
		}
		if _, err = io.Copy(io.Discard, part); err != nil {
			t.Fatalf("drain part: %v", err)
		}
	}
}
