package source

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aura/aura/internal/markitdown"
)

// fakeConverter captures Convert input and returns canned markdown.
type fakeConverter struct {
	calls    int
	lastIn   markitdown.ConvertInput
	response markitdown.ConvertResult
	err      error
}

func (f *fakeConverter) Convert(_ context.Context, in markitdown.ConvertInput) (markitdown.ConvertResult, error) {
	f.calls++
	f.lastIn = in
	if f.err != nil {
		return markitdown.ConvertResult{}, f.err
	}
	return f.response, nil
}

func TestExtractUploadedSource_DispatchesByMime(t *testing.T) {
	cases := []struct {
		name     string
		kind     Kind
		mime     string
		expected string // expected ExtractorName
	}{
		{"csv", KindCSV, "text/csv", "markitdown_csv"},
		{"xlsx", KindXLSX, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "markitdown_xlsx"},
		{"docx", KindDOCX, "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "markitdown_docx"},
		{"pptx", KindPPTX, "application/vnd.openxmlformats-officedocument.presentationml.presentation", "markitdown_pptx"},
		{"epub", KindEPUB, "application/epub+zip", "markitdown_epub"},
		{"html", KindHTML, "text/html", "markitdown_html"},
		{"zip", KindZIP, "application/zip", "markitdown_zip"},
		{"text", KindText, "text/plain", "markitdown_text"},
		{"markdown", KindMarkdown, "text/markdown", "markitdown_md"},
		{"json", KindJSON, "application/json", "markitdown_json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fc := &fakeConverter{response: markitdown.ConvertResult{Markdown: "# hello\n"}}
			res, err := ExtractUploadedSource(context.Background(), fc, ExtractInput{
				Source: &Source{ID: "src_0123456789abcdef", Kind: tc.kind, MimeType: tc.mime, Filename: "x." + tc.name},
				Bytes:  []byte("payload"),
			})
			if err != nil {
				t.Fatalf("ExtractUploadedSource: %v", err)
			}
			if fc.calls != 1 {
				t.Fatalf("converter calls = %d, want 1", fc.calls)
			}
			if fc.lastIn.MimeType != tc.mime {
				t.Fatalf("mime forwarded = %q, want %q", fc.lastIn.MimeType, tc.mime)
			}
			if res.Metadata.ExtractorName != tc.expected {
				t.Fatalf("extractor name = %q, want %q", res.Metadata.ExtractorName, tc.expected)
			}
			if !strings.HasSuffix(res.Markdown, "\n") {
				t.Fatalf("markdown does not end with newline: %q", res.Markdown)
			}
			if res.Metadata.TextBytes == 0 {
				t.Fatal("text bytes should be set")
			}
		})
	}
}

func TestExtractUploadedSource_RejectsImageUntil295(t *testing.T) {
	fc := &fakeConverter{}
	_, err := ExtractUploadedSource(context.Background(), fc, ExtractInput{
		Source: &Source{ID: "src_0123456789abcdef", Kind: KindImage, MimeType: "image/png"},
		Bytes:  []byte("PNGDATA"),
	})
	if err == nil {
		t.Fatal("expected image rejection (Wave 2.9.5 gate)")
	}
	if fc.calls != 0 {
		t.Fatalf("converter should not be called for image, got %d", fc.calls)
	}
}

func TestExtractUploadedSource_RejectsUnknownKind(t *testing.T) {
	fc := &fakeConverter{}
	_, err := ExtractUploadedSource(context.Background(), fc, ExtractInput{
		Source: &Source{ID: "src_0123456789abcdef", Kind: Kind("mystery"), MimeType: "application/x-unknown"},
		Bytes:  []byte("payload"),
	})
	if err == nil || !strings.Contains(err.Error(), "no markitdown route") {
		t.Fatalf("expected unknown-kind error, got %v", err)
	}
	if fc.calls != 0 {
		t.Fatalf("converter should not be called for unknown kind, got %d", fc.calls)
	}
}

func TestExtractUploadedSource_RequiresConverter(t *testing.T) {
	_, err := ExtractUploadedSource(context.Background(), nil, ExtractInput{
		Source: &Source{ID: "src_0123456789abcdef", Kind: KindCSV, MimeType: "text/csv"},
		Bytes:  []byte("payload"),
	})
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("expected converter-required error, got %v", err)
	}
}

func TestExtractUploadedSource_RejectsNilSource(t *testing.T) {
	fc := &fakeConverter{}
	_, err := ExtractUploadedSource(context.Background(), fc, ExtractInput{Source: nil, Bytes: []byte("x")})
	if err == nil {
		t.Fatal("expected nil-source error")
	}
}

func TestExtractUploadedSource_PropagatesConverterError(t *testing.T) {
	fc := &fakeConverter{err: errors.New("sidecar offline")}
	_, err := ExtractUploadedSource(context.Background(), fc, ExtractInput{
		Source: &Source{ID: "src_0123456789abcdef", Kind: KindCSV, MimeType: "text/csv"},
		Bytes:  []byte("payload"),
	})
	if err == nil || !strings.Contains(err.Error(), "sidecar offline") {
		t.Fatalf("expected sidecar error to propagate, got %v", err)
	}
}

func TestExtractUploadedSource_NormalizesTrailingNewline(t *testing.T) {
	fc := &fakeConverter{response: markitdown.ConvertResult{Markdown: "# hello"}}
	res, err := ExtractUploadedSource(context.Background(), fc, ExtractInput{
		Source: &Source{ID: "src_0123456789abcdef", Kind: KindCSV, MimeType: "text/csv"},
		Bytes:  []byte("payload"),
	})
	if err != nil {
		t.Fatalf("ExtractUploadedSource: %v", err)
	}
	if res.Markdown != "# hello\n" {
		t.Fatalf("markdown = %q, want trailing newline normalized", res.Markdown)
	}
}
