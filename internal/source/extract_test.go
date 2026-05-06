package source

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/aura/aura/internal/sandbox"
)

type fakePyodideRunner struct {
	called       bool
	code         string
	allowNetwork bool
	result       *sandbox.Result
}

func (r *fakePyodideRunner) Execute(_ context.Context, code string, allowNetwork bool) (*sandbox.Result, error) {
	r.called = true
	r.code = code
	r.allowNetwork = allowNetwork
	return r.result, nil
}

func TestGoExtractorsProduceMarkdownAndMetadata(t *testing.T) {
	cases := []struct {
		name string
		kind Kind
		body []byte
		want string
	}{
		{"notes.txt", KindText, []byte("Alpha decision\nBeta action"), "Alpha decision"},
		{"daily.md", KindMarkdown, []byte("# Daily\n\n- ship v1.2"), "# Daily"},
		{"config.json", KindJSON, []byte(`{"owner":"Davide","milestone":"v1.2"}`), `"milestone": "v1.2"`},
		{"budget.csv", KindCSV, []byte("item,cost\nsandbox,12\nocr,34\n"), "| item | cost |"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := ExtractGo(context.Background(), ExtractInput{
				Source: &Source{ID: "src_0123456789abcdef", Kind: tc.kind, Filename: tc.name, SHA256: strings.Repeat("a", 64)},
				Bytes:  tc.body,
			})
			if err != nil {
				t.Fatalf("ExtractGo() error = %v", err)
			}
			if !strings.Contains(res.Markdown, tc.want) {
				t.Fatalf("markdown = %q, want substring %q", res.Markdown, tc.want)
			}
			if res.Metadata.ExtractorName == "" || res.Metadata.TextBytes == 0 {
				t.Fatalf("metadata incomplete: %+v", res.Metadata)
			}
		})
	}
}

func TestExtractUploadedSourceUsesGoForTextLikeFormats(t *testing.T) {
	res, err := ExtractUploadedSource(context.Background(), nil, ExtractInput{
		Source: &Source{ID: "src_0123456789abcdef", Kind: KindCSV, Filename: "budget.csv"},
		Bytes:  []byte("item,cost\nsandbox,12\n"),
	})
	if err != nil {
		t.Fatalf("ExtractUploadedSource() error = %v", err)
	}
	if !strings.Contains(res.Markdown, "| item | cost |") {
		t.Fatalf("markdown = %q, want CSV markdown table", res.Markdown)
	}
	if res.Metadata.ExtractorName != "go_csv" {
		t.Fatalf("extractor = %q, want go_csv", res.Metadata.ExtractorName)
	}
}

func TestExtractUploadedSourceUsesPyodideForXLSX(t *testing.T) {
	runner := &fakePyodideRunner{result: &sandbox.Result{
		OK:     true,
		Stdout: `{"markdown":"| item | cost |\n| --- | --- |\n| sandbox | 12 |\n","metadata":{"extractor_name":"pyodide_xlsx","sheet_count":1,"row_count":1}}`,
	}}
	res, err := ExtractUploadedSource(context.Background(), runner, ExtractInput{
		Source: &Source{ID: "src_0123456789abcdef", Kind: KindXLSX, Filename: "budget.xlsx"},
		Bytes:  []byte("xlsx bytes"),
	})
	if err != nil {
		t.Fatalf("ExtractUploadedSource() error = %v", err)
	}
	if !runner.called || runner.allowNetwork {
		t.Fatalf("runner called=%v allowNetwork=%v, want called without network", runner.called, runner.allowNetwork)
	}
	if !strings.Contains(runner.code, "pd.ExcelFile") {
		t.Fatalf("runner code missing XLSX extraction marker")
	}
	if res.Metadata.ExtractorName != "pyodide_xlsx" || !strings.Contains(res.Markdown, "sandbox") {
		t.Fatalf("result = %+v\n%s", res.Metadata, res.Markdown)
	}
}

func TestExtractUploadedSourceXLSXRequiresRunner(t *testing.T) {
	_, err := ExtractUploadedSource(context.Background(), nil, ExtractInput{
		Source: &Source{ID: "src_0123456789abcdef", Kind: KindXLSX, Filename: "budget.xlsx"},
		Bytes:  []byte("xlsx bytes"),
	})
	if err == nil || !strings.Contains(err.Error(), "pyodide runner") {
		t.Fatalf("error = %v, want pyodide runner requirement", err)
	}
}

func TestExtractGoRejectsMalformedJSON(t *testing.T) {
	_, err := ExtractGo(context.Background(), ExtractInput{
		Source: &Source{ID: "src_0123456789abcdef", Kind: KindJSON, Filename: "broken.json"},
		Bytes:  []byte(`{"broken":`),
	})
	if err == nil || !strings.Contains(err.Error(), "parse json") {
		t.Fatalf("error = %v, want parse json error", err)
	}
}

func TestWriteExtractionFiles(t *testing.T) {
	store, err := NewStore(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	src, _, err := store.Put(context.Background(), PutInput{
		Kind:     KindText,
		Filename: "notes.txt",
		MimeType: "text/plain",
		Bytes:    []byte("Alpha"),
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := WriteExtractionFiles(store, src, ExtractResult{
		Markdown: "Alpha\n",
		Metadata: ExtractionMeta{ExtractorName: "go_text", TextBytes: 5},
	}); err != nil {
		t.Fatalf("WriteExtractionFiles: %v", err)
	}
	md, err := os.ReadFile(store.Path(src.ID, ExtractMarkdownFile))
	if err != nil {
		t.Fatalf("read extract.md: %v", err)
	}
	if string(md) != "Alpha\n" {
		t.Fatalf("extract.md = %q", md)
	}
	metaBytes, err := os.ReadFile(store.Path(src.ID, ExtractJSONFile))
	if err != nil {
		t.Fatalf("read extract.json: %v", err)
	}
	var meta ExtractionMeta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		t.Fatalf("parse extract.json: %v", err)
	}
	if meta.ExtractorName != "go_text" || meta.TextBytes != 5 {
		t.Fatalf("metadata = %+v", meta)
	}
}
