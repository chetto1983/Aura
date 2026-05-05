# v1.2 Universal Ingestion + Memory Quality Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans to implement this plan task-by-task. subagent-driven-development is optional for independent code slices only; the product runtime must remain one Aura agent using skills, tools, and sandbox execution rather than spawning extra runtime agents. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Aura ingest PDF, TXT, MD, JSON, CSV, DOCX, and XLSX sources from Telegram and the dashboard, then prove mixed-source memory quality with a deterministic release scorecard.

**Architecture:** Add a format-neutral source normalization layer that writes `extract.md` and `extract.json` next to each source. Keep the existing PDF OCR path but adapt it into the normalized contract. Use Go-native extractors for simple text-like formats and bounded fixed Pyodide extractors for Office/table-heavy formats, then update ingest, retrieval, and the dashboard to consume normalized evidence.

**Tech Stack:** Go 1.25, SQLite-backed Aura stores, existing `internal/source`, `internal/ingest`, `internal/sandbox` Pyodide runner, React 19 dashboard, Vitest/TypeScript frontend tests where existing project commands support them.

---

## Scope Check

The spec has two lanes, but they are coupled by one boundary: normalized source evidence. This plan keeps them in one milestone because the memory scorecard must exercise the new source formats. Work is split into small tasks that can be reviewed and committed independently.

## Conversation Review Updates

This plan was reviewed against the 2026-05-05 implementation conversation. These decisions override any weaker examples below:

- **Not PDF-only:** the milestone remains multi-format ingestion inspired by Cognee's broad ingestion model, but Aura does not adopt Cognee as a dependency or add a Python service.
- **Python sandbox is leverage, not a file tool clone:** Pyodide should help Aura generate/extract/verify structured evidence, especially table-heavy files and agent-authored scripts, but fixed extractor scripts must be bounded, no-network, and versioned.
- **Skills live in `D:\Aura\skills`:** E2E coverage must prove Aura reads local Aura skills from that folder, uses a skill to guide Python code generation, executes the code in the sandbox, persists the script/result as source evidence, and can recall the script later.
- **No synthetic "hello world" scorecard:** the release gate cannot pass by checking expected terms inside a fixture. It must run real extraction, ingestion/retrieval/proposal paths, and at least one real skills + sandbox + persisted-source recall flow.
- **Single-agent runtime:** dedicated skills are the extensibility layer. Do not hardcode every extractor or add costly runtime agent swarms for this milestone; let the existing Aura agent choose skills and use sandbox tools through the existing tool registry.
- **Production UX regression:** keep the v1.1 tray/console lesson in the release gate. Packaged Aura must remain console-free, and Windows startup must log `tray: ready` with the visible `icon_app.ico` asset.

## File Structure

- Create `internal/source/formats.go`: supported upload kinds, extension/MIME allowlist, raw filename mapping helpers.
- Modify `internal/source/source.go`: add uploaded JSON/CSV/Markdown kinds, extraction statuses, extraction metadata fields.
- Modify `internal/source/store.go`: validate new source kinds and preserve raw extensions.
- Create `internal/source/extract.go`: normalization result types and service interfaces.
- Create `internal/source/extract_go.go`: Go-native TXT, MD, JSON, CSV extractors.
- Create `internal/source/extract_pdf.go`: adapter that writes normalized files from existing OCR markdown.
- Create `internal/source/extract_pyodide.go`: fixed-script Pyodide extractor bridge.
- Create `internal/source/extract_test.go`, `internal/source/formats_test.go`: deterministic unit coverage.
- Modify `internal/api/upload.go`, `internal/api/sources.go`, `internal/api/router.go`: dashboard upload uses format policy and normalization.
- Modify `internal/telegram/documents.go`: Telegram upload uses same format policy and normalizer.
- Modify `internal/ingest/pipeline.go`: read `extract.md` first and retain `ocr.md` fallback for old PDFs.
- Add `cmd/debug_memory_scorecard/main.go`: local release gate command.
- Add `cmd/debug_skill_sandbox_memory/main.go`: local E2E proving skills -> sandbox code -> persisted source -> recall.
- Add `internal/memoryscore/`: fixture loader and deterministic scorecard runner.
- Add `testdata/memoryscore/v1_2/`: mixed-source fixtures and expected cases.
- Add `testdata/skill_sandbox_memory/`: fixture skill prompts, expected script/result terms, and persisted-source recall assertions.
- Modify `web/src/types/api.ts`, `web/src/components/SourceInbox.tsx`, `web/src/i18n/locales/en.json`, `web/src/i18n/locales/it.json`: multi-format upload and status labels.
- Modify `docs/implementation-tracker.md`, `.planning/PROJECT.md`, `.planning/ROADMAP.md`: record milestone progress and release result.

---

## Task 1: Source Format Policy and Metadata

**Files:**
- Create: `internal/source/formats.go`
- Modify: `internal/source/source.go`
- Modify: `internal/source/store.go`
- Test: `internal/source/formats_test.go`

- [x] **Step 1: Write failing format policy tests**

Create `internal/source/formats_test.go`:

```go
package source

import "testing"

func TestDetectUploadFormatAcceptsV12Formats(t *testing.T) {
	cases := []struct {
		name string
		mime string
		kind Kind
		raw  string
	}{
		{"paper.pdf", "application/pdf", KindPDF, "original.pdf"},
		{"notes.txt", "text/plain", KindText, "original.txt"},
		{"daily.md", "text/markdown", KindMarkdown, "original.md"},
		{"data.json", "application/json", KindJSON, "original.json"},
		{"budget.csv", "text/csv", KindCSV, "original.csv"},
		{"memo.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", KindDOCX, "original.docx"},
		{"sheet.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", KindXLSX, "original.xlsx"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DetectUploadFormat(tc.name, tc.mime)
			if err != nil {
				t.Fatalf("DetectUploadFormat() error = %v", err)
			}
			if got.Kind != tc.kind || got.OriginalName != tc.raw {
				t.Fatalf("format = (%s, %s), want (%s, %s)", got.Kind, got.OriginalName, tc.kind, tc.raw)
			}
		})
	}
}

func TestDetectUploadFormatRejectsUnsupported(t *testing.T) {
	for _, name := range []string{"image.png", "deck.pptx", "archive.zip", "audio.mp3"} {
		if _, err := DetectUploadFormat(name, "application/octet-stream"); err == nil {
			t.Fatalf("DetectUploadFormat(%q) error = nil, want rejection", name)
		}
	}
}

func TestExtractionStatusesAreValid(t *testing.T) {
	for _, status := range []Status{StatusStored, StatusExtracting, StatusExtractComplete, StatusIngested, StatusFailed} {
		if !ValidStatus(status) {
			t.Fatalf("ValidStatus(%q) = false", status)
		}
	}
}
```

- [x] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/source -run "TestDetectUploadFormat|TestExtractionStatuses" -count=1`

Expected: FAIL with missing `KindMarkdown`, `KindJSON`, `KindCSV`, `DetectUploadFormat`, or `ValidStatus`.

- [x] **Step 3: Add source kinds, statuses, and extraction metadata**

Modify `internal/source/source.go`:

```go
const (
	KindPDF      Kind = "pdf"
	KindText     Kind = "text"
	KindMarkdown Kind = "markdown"
	KindJSON     Kind = "json"
	KindCSV      Kind = "csv"
	KindURL      Kind = "url"
	KindXLSX     Kind = "xlsx"
	KindDOCX     Kind = "docx"
	KindPDFGen   Kind = "pdf_generated"
	KindSandboxArtifact Kind = "sandbox_artifact"
)

const (
	StatusStored          Status = "stored"
	StatusExtracting      Status = "extracting"
	StatusOCRComplete     Status = "ocr_complete"
	StatusExtractComplete Status = "extract_complete"
	StatusIngested        Status = "ingested"
	StatusFailed          Status = "failed"
)

type ExtractionMeta struct {
	ExtractorName    string   `json:"extractor_name,omitempty"`
	ExtractorVersion string   `json:"extractor_version,omitempty"`
	TextBytes        int      `json:"text_bytes,omitempty"`
	PageCount        int      `json:"page_count,omitempty"`
	SheetCount       int      `json:"sheet_count,omitempty"`
	RowCount         int      `json:"row_count,omitempty"`
	Warnings         []string `json:"warnings,omitempty"`
}

type Source struct {
	ID         string          `json:"id"`
	Kind       Kind            `json:"kind"`
	Filename   string          `json:"filename"`
	MimeType   string          `json:"mime_type"`
	SHA256     string          `json:"sha256"`
	SizeBytes  int64           `json:"size_bytes"`
	CreatedAt  time.Time       `json:"created_at"`
	Status     Status          `json:"status"`
	OCRModel   string          `json:"ocr_model,omitempty"`
	PageCount  int             `json:"page_count,omitempty"`
	Extract    *ExtractionMeta `json:"extract,omitempty"`
	WikiPages  []string        `json:"wiki_pages,omitempty"`
	Error      string          `json:"error,omitempty"`
}
```

- [x] **Step 4: Add format policy**

Create `internal/source/formats.go`:

```go
package source

import (
	"fmt"
	"path/filepath"
	"strings"
)

type UploadFormat struct {
	Kind         Kind
	MimeType     string
	OriginalName string
}

var formatsByExt = map[string]UploadFormat{
	".pdf":  {Kind: KindPDF, MimeType: "application/pdf", OriginalName: "original.pdf"},
	".txt":  {Kind: KindText, MimeType: "text/plain; charset=utf-8", OriginalName: "original.txt"},
	".md":   {Kind: KindMarkdown, MimeType: "text/markdown; charset=utf-8", OriginalName: "original.md"},
	".json": {Kind: KindJSON, MimeType: "application/json", OriginalName: "original.json"},
	".csv":  {Kind: KindCSV, MimeType: "text/csv; charset=utf-8", OriginalName: "original.csv"},
	".docx": {Kind: KindDOCX, MimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", OriginalName: "original.docx"},
	".xlsx": {Kind: KindXLSX, MimeType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", OriginalName: "original.xlsx"},
}

func DetectUploadFormat(filename, mimeType string) (UploadFormat, error) {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(filename)))
	format, ok := formatsByExt[ext]
	if !ok {
		return UploadFormat{}, fmt.Errorf("unsupported file type %q; supported: .pdf, .txt, .md, .json, .csv, .docx, .xlsx", ext)
	}
	if mt := strings.TrimSpace(mimeType); mt != "" {
		format.MimeType = mt
	}
	return format, nil
}

func SupportedUploadAccept() string {
	return ".pdf,.txt,.md,.json,.csv,.docx,.xlsx,application/pdf,text/plain,text/markdown,application/json,text/csv,application/vnd.openxmlformats-officedocument.wordprocessingml.document,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
}

func ValidStatus(s Status) bool {
	switch s {
	case StatusStored, StatusExtracting, StatusOCRComplete, StatusExtractComplete, StatusIngested, StatusFailed:
		return true
	}
	return false
}
```

- [x] **Step 5: Update store validation and raw filename mapping**

Modify `validatePutInput` and `extForKind` in `internal/source/store.go`:

```go
func extForKind(k Kind) string {
	switch k {
	case KindPDF:
		return ".pdf"
	case KindText:
		return ".txt"
	case KindMarkdown:
		return ".md"
	case KindJSON:
		return ".json"
	case KindCSV:
		return ".csv"
	case KindURL:
		return ".url"
	case KindXLSX:
		return ".xlsx"
	case KindDOCX:
		return ".docx"
	case KindPDFGen:
		return ".pdf"
	case KindSandboxArtifact:
		return ".bin"
	}
	return ".bin"
}

func validatePutInput(in PutInput) error {
	switch in.Kind {
	case KindPDF, KindText, KindMarkdown, KindJSON, KindCSV, KindURL, KindXLSX, KindDOCX, KindPDFGen, KindSandboxArtifact:
	default:
		return fmt.Errorf("source: invalid kind %q", in.Kind)
	}
	if len(in.Bytes) == 0 {
		return errors.New("source: empty content")
	}
	if strings.TrimSpace(in.Filename) == "" {
		return errors.New("source: filename required")
	}
	return nil
}
```

- [x] **Step 6: Run tests and commit**

Run: `go test ./internal/source -run "TestDetectUploadFormat|TestExtractionStatuses" -count=1`

Expected: PASS.

Commit:

```bash
git add internal/source/source.go internal/source/store.go internal/source/formats.go internal/source/formats_test.go
git commit -m "feat: add universal source format policy"
```

---

## Task 2: Normalized Extraction Contract and Go Extractors

**Files:**
- Create: `internal/source/extract.go`
- Create: `internal/source/extract_go.go`
- Test: `internal/source/extract_test.go`

- [x] **Step 1: Write failing extraction contract tests**

Create `internal/source/extract_test.go`:

```go
package source

import (
	"context"
	"strings"
	"testing"
)

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

func TestExtractGoRejectsMalformedJSON(t *testing.T) {
	_, err := ExtractGo(context.Background(), ExtractInput{
		Source: &Source{ID: "src_0123456789abcdef", Kind: KindJSON, Filename: "broken.json"},
		Bytes:  []byte(`{"broken":`),
	})
	if err == nil || !strings.Contains(err.Error(), "parse json") {
		t.Fatalf("error = %v, want parse json error", err)
	}
}
```

- [x] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/source -run "TestGoExtractors|TestExtractGo" -count=1`

Expected: FAIL with missing `ExtractGo`, `ExtractInput`, or `ExtractResult`.

- [x] **Step 3: Add extraction result types**

Create `internal/source/extract.go`:

```go
package source

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

const (
	ExtractMarkdownFile = "extract.md"
	ExtractJSONFile     = "extract.json"
)

type ExtractInput struct {
	Source *Source
	Bytes  []byte
}

type ExtractResult struct {
	Markdown string
	Metadata ExtractionMeta
}

type Extractor interface {
	Extract(ctx context.Context, in ExtractInput) (ExtractResult, error)
}

func WriteExtractionFiles(store interface{ Path(id, name string) string }, src *Source, res ExtractResult) error {
	if src == nil {
		return fmt.Errorf("source: nil source")
	}
	mdPath := store.Path(src.ID, ExtractMarkdownFile)
	if mdPath == "" {
		return fmt.Errorf("source: invalid extract markdown path for %s", src.ID)
	}
	if err := os.WriteFile(mdPath, []byte(res.Markdown), 0o644); err != nil {
		return fmt.Errorf("source: write extract.md: %w", err)
	}
	b, err := json.MarshalIndent(res.Metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("source: marshal extract metadata: %w", err)
	}
	jsonPath := store.Path(src.ID, ExtractJSONFile)
	if jsonPath == "" {
		return fmt.Errorf("source: invalid extract json path for %s", src.ID)
	}
	if err := os.WriteFile(jsonPath, b, 0o644); err != nil {
		return fmt.Errorf("source: write extract.json: %w", err)
	}
	return nil
}
```

- [x] **Step 4: Add Go extractors**

Create `internal/source/extract_go.go`:

```go
package source

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
)

const goExtractorVersion = "go_extract_v1"

func ExtractGo(ctx context.Context, in ExtractInput) (ExtractResult, error) {
	select {
	case <-ctx.Done():
		return ExtractResult{}, ctx.Err()
	default:
	}
	if in.Source == nil {
		return ExtractResult{}, fmt.Errorf("source: nil source")
	}
	switch in.Source.Kind {
	case KindText, KindMarkdown:
		return textExtract(in), nil
	case KindJSON:
		return jsonExtract(in)
	case KindCSV:
		return csvExtract(in)
	default:
		return ExtractResult{}, fmt.Errorf("source: no Go extractor for kind %s", in.Source.Kind)
	}
}

func textExtract(in ExtractInput) ExtractResult {
	body := strings.TrimSpace(string(in.Bytes))
	return ExtractResult{
		Markdown: body + "\n",
		Metadata: ExtractionMeta{
			ExtractorName:    "go_text",
			ExtractorVersion: goExtractorVersion,
			TextBytes:        len([]byte(body)),
		},
	}
}

func jsonExtract(in ExtractInput) (ExtractResult, error) {
	var v any
	if err := json.Unmarshal(in.Bytes, &v); err != nil {
		return ExtractResult{}, fmt.Errorf("source: parse json: %w", err)
	}
	pretty, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return ExtractResult{}, fmt.Errorf("source: render json: %w", err)
	}
	md := "```json\n" + string(pretty) + "\n```\n"
	return ExtractResult{
		Markdown: md,
		Metadata: ExtractionMeta{
			ExtractorName:    "go_json",
			ExtractorVersion: goExtractorVersion,
			TextBytes:        len([]byte(md)),
		},
	}, nil
}

func csvExtract(in ExtractInput) (ExtractResult, error) {
	r := csv.NewReader(bytes.NewReader(in.Bytes))
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return ExtractResult{}, fmt.Errorf("source: parse csv: %w", err)
	}
	if len(rows) == 0 {
		return ExtractResult{}, fmt.Errorf("source: empty csv")
	}
	md := renderMarkdownTable(rows, 200)
	return ExtractResult{
		Markdown: md,
		Metadata: ExtractionMeta{
			ExtractorName:    "go_csv",
			ExtractorVersion: goExtractorVersion,
			TextBytes:        len([]byte(md)),
			RowCount:         len(rows),
		},
	}, nil
}

func renderMarkdownTable(rows [][]string, maxRows int) string {
	if len(rows) == 0 {
		return ""
	}
	cols := len(rows[0])
	var sb strings.Builder
	writeRow := func(row []string) {
		sb.WriteString("|")
		for i := 0; i < cols; i++ {
			cell := ""
			if i < len(row) {
				cell = strings.ReplaceAll(strings.TrimSpace(row[i]), "|", "\\|")
			}
			sb.WriteString(" " + cell + " |")
		}
		sb.WriteString("\n")
	}
	writeRow(rows[0])
	sb.WriteString("|")
	for i := 0; i < cols; i++ {
		sb.WriteString(" --- |")
	}
	sb.WriteString("\n")
	limit := len(rows)
	if limit > maxRows {
		limit = maxRows
	}
	for _, row := range rows[1:limit] {
		writeRow(row)
	}
	if len(rows) > maxRows {
		sb.WriteString("\n_Extraction truncated after 200 rows._\n")
	}
	return sb.String()
}
```

- [x] **Step 5: Run tests and commit**

Run: `go test ./internal/source -run "TestGoExtractors|TestExtractGo" -count=1`

Expected: PASS.

Commit:

```bash
git add internal/source/extract.go internal/source/extract_go.go internal/source/extract_test.go
git commit -m "feat: add normalized source extractors"
```

---

## Task 3: PDF OCR Adapter Writes Normalized Evidence

**Files:**
- Create: `internal/source/extract_pdf.go`
- Modify: `internal/api/upload.go`
- Modify: `internal/telegram/documents.go`
- Test: `internal/source/extract_pdf_test.go`

- [x] **Step 1: Write failing PDF adapter test**

Create `internal/source/extract_pdf_test.go`:

```go
package source

import (
	"strings"
	"testing"
)

func TestExtractFromOCRMarkdownPreservesPDFEvidence(t *testing.T) {
	src := &Source{ID: "src_0123456789abcdef", Kind: KindPDF, Filename: "paper.pdf", SHA256: strings.Repeat("a", 64)}
	res := ExtractFromOCRMarkdown(src, "# Source OCR: paper.pdf\n\n## Page 1\n\nImportant daily memory fact.")
	if !strings.Contains(res.Markdown, "Important daily memory fact") {
		t.Fatalf("markdown = %q", res.Markdown)
	}
	if res.Metadata.ExtractorName != "mistral_ocr_adapter" || res.Metadata.PageCount != 1 {
		t.Fatalf("metadata = %+v", res.Metadata)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/source -run TestExtractFromOCRMarkdown -count=1`

Expected: FAIL with missing `ExtractFromOCRMarkdown`.

- [x] **Step 3: Add PDF adapter**

Create `internal/source/extract_pdf.go`:

```go
package source

import "strings"

func ExtractFromOCRMarkdown(src *Source, md string) ExtractResult {
	pageCount := strings.Count(md, "\n## Page ")
	if strings.HasPrefix(md, "## Page ") {
		pageCount++
	}
	name := "mistral_ocr_adapter"
	if src != nil && src.OCRModel != "" {
		name = "mistral_ocr_adapter:" + src.OCRModel
	}
	body := strings.TrimSpace(md) + "\n"
	return ExtractResult{
		Markdown: body,
		Metadata: ExtractionMeta{
			ExtractorName:    name,
			ExtractorVersion: "pdf_ocr_adapter_v1",
			TextBytes:        len([]byte(body)),
			PageCount:        pageCount,
		},
	}
}
```

- [x] **Step 4: Update API and Telegram PDF paths to write `extract.md` and `extract.json`**

In `internal/api/upload.go`, after writing `ocr.md` and `ocr.json`, add:

```go
normalized := source.ExtractFromOCRMarkdown(&source.Source{
	ID:       src.ID,
	Kind:     src.Kind,
	Filename: src.Filename,
	SHA256:   src.SHA256,
	OCRModel: ocrRes.Response.Model,
}, md)
if err := source.WriteExtractionFiles(deps.Sources, src, normalized); err != nil {
	writeError(w, deps.Logger, http.StatusInternalServerError, "write extract files: "+err.Error())
	return
}
```

In `internal/telegram/documents.go`, after writing `ocr.md` and `ocr.json`, add:

```go
normalized := source.ExtractFromOCRMarkdown(&source.Source{
	ID:       src.ID,
	Kind:     src.Kind,
	Filename: src.Filename,
	SHA256:   src.SHA256,
	OCRModel: res.Response.Model,
}, md)
if err := source.WriteExtractionFiles(h.sources, src, normalized); err != nil {
	editor.fail("Write extract files failed: " + err.Error())
	return
}
```

- [x] **Step 5: Run focused tests and commit**

Run: `go test ./internal/source ./internal/api ./internal/telegram -run "TestExtractFromOCRMarkdown|Source|Document" -count=1`

Expected: PASS.

Commit:

```bash
git add internal/source/extract_pdf.go internal/source/extract_pdf_test.go internal/api/upload.go internal/telegram/documents.go
git commit -m "feat: normalize pdf ocr evidence"
```

---

## Task 4: Pyodide Extractor Bridge for XLSX and DOCX

**Files:**
- Create: `internal/source/extract_pyodide.go`
- Create: `internal/source/testdata/workbook.xlsx`
- Create: `internal/source/testdata/malformed.xlsx`
- Test: `internal/source/extract_pyodide_test.go`

- [x] **Step 1: Write Pyodide bridge tests with availability skip**

Create `internal/source/extract_pyodide_test.go`:

```go
package source

import (
	"context"
	"strings"
	"testing"

	"github.com/aura/aura/internal/sandbox"
)

func TestPyodideXLSXExtractor(t *testing.T) {
	runner, err := sandbox.NewPyodideRunner(sandbox.PyodideRunnerConfig{})
	if err != nil {
		t.Fatalf("runner: %v", err)
	}
	if avail := runner.CheckAvailability(); !avail.Available {
		t.Skipf("pyodide unavailable: %s", avail.Detail)
	}
	body := makeTestWorkbook(t)
	res, err := ExtractWithPyodide(context.Background(), runner, ExtractInput{
		Source: &Source{ID: "src_0123456789abcdef", Kind: KindXLSX, Filename: "budget.xlsx"},
		Bytes:  body,
	})
	if err != nil {
		t.Fatalf("ExtractWithPyodide() error = %v", err)
	}
	if !strings.Contains(res.Markdown, "sandbox") || res.Metadata.SheetCount == 0 {
		t.Fatalf("result = %+v\n%s", res.Metadata, res.Markdown)
	}
}
```

Add helper in the same file:

```go
func makeTestWorkbook(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	f := excelize.NewFile()
	defer f.Close()
	f.SetCellValue("Sheet1", "A1", "item")
	f.SetCellValue("Sheet1", "B1", "cost")
	f.SetCellValue("Sheet1", "A2", "sandbox")
	f.SetCellValue("Sheet1", "B2", 12)
	if err := f.Write(&buf); err != nil {
		t.Fatalf("write workbook: %v", err)
	}
	return buf.Bytes()
}
```

Ensure imports include:

```go
import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/aura/aura/internal/sandbox"
	"github.com/xuri/excelize/v2"
)
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/source -run TestPyodideXLSXExtractor -count=1`

Expected: FAIL with missing `ExtractWithPyodide`, or SKIP when runtime is unavailable before implementation.

- [x] **Step 3: Add fixed-script Pyodide bridge**

Create `internal/source/extract_pyodide.go`:

```go
package source

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/aura/aura/internal/sandbox"
)

type pyodideExtractionOut struct {
	Markdown string         `json:"markdown"`
	Metadata ExtractionMeta `json:"metadata"`
}

func ExtractWithPyodide(ctx context.Context, runner interface {
	Execute(context.Context, string, bool) (*sandbox.Result, error)
}, in ExtractInput) (ExtractResult, error) {
	if in.Source == nil {
		return ExtractResult{}, fmt.Errorf("source: nil source")
	}
	var code string
	switch in.Source.Kind {
	case KindXLSX:
		code = buildXLSXExtractorCode(in.Bytes)
	default:
		return ExtractResult{}, fmt.Errorf("source: no Pyodide extractor for kind %s", in.Source.Kind)
	}
	res, err := runner.Execute(ctx, code, false)
	if err != nil {
		return ExtractResult{}, err
	}
	if !res.OK {
		return ExtractResult{}, fmt.Errorf("source: pyodide extraction failed: %s", res.Stderr)
	}
	var out pyodideExtractionOut
	if err := json.Unmarshal([]byte(res.Stdout), &out); err != nil {
		return ExtractResult{}, fmt.Errorf("source: parse pyodide extraction json: %w", err)
	}
	if out.Markdown == "" {
		return ExtractResult{}, fmt.Errorf("source: pyodide extraction returned empty markdown")
	}
	return ExtractResult{Markdown: out.Markdown, Metadata: out.Metadata}, nil
}

func buildXLSXExtractorCode(body []byte) string {
	encoded := base64.StdEncoding.EncodeToString(body)
	return fmt.Sprintf(`
import base64, io, json
import pandas as pd

raw = base64.b64decode(%q)
book = pd.ExcelFile(io.BytesIO(raw), engine="calamine")
sections = []
rows_total = 0
for sheet in book.sheet_names[:10]:
    df = book.parse(sheet)
    rows_total += int(len(df.index))
    sections.append("## Sheet: " + str(sheet))
    sections.append(df.head(200).to_markdown(index=False))
markdown = "\n\n".join(sections).strip() + "\n"
print(json.dumps({
    "markdown": markdown,
    "metadata": {
        "extractor_name": "pyodide_xlsx",
        "extractor_version": "pyodide_xlsx_v1",
        "text_bytes": len(markdown.encode("utf-8")),
        "sheet_count": len(book.sheet_names),
        "row_count": rows_total,
        "warnings": [] if rows_total <= 200 else ["large workbook rendered with per-sheet row limits"]
    }
}))
`, encoded)
}
```

- [x] **Step 4: Run focused test and commit**

Run: `go test ./internal/source -run TestPyodideXLSXExtractor -count=1`

Expected: PASS when Pyodide is available, SKIP with a clear availability reason otherwise.

Commit:

```bash
git add internal/source/extract_pyodide.go internal/source/extract_pyodide_test.go
git commit -m "feat: add sandboxed spreadsheet extraction"
```

---

## Task 5: Dashboard and Telegram Upload Use Universal Normalization

**Files:**
- Modify: `internal/api/upload.go`
- Modify: `internal/telegram/documents.go`
- Modify: `internal/api/router.go`
- Modify: `internal/api/sources.go`
- Test: `internal/api/upload_test.go`
- Test: `internal/telegram/documents_test.go`

- [x] **Step 1: Write failing API upload tests for non-PDF formats**

Add to `internal/api/upload_test.go`:

```go
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
	if got.Status != string(source.StatusIngested) && got.Status != string(source.StatusExtractComplete) {
		t.Fatalf("status = %s", got.Status)
	}

	bad := e.uploadFile("deck.pptx", "application/vnd.openxmlformats-officedocument.presentationml.presentation", []byte("pptx"))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("pptx status = %d, want 400", bad.Code)
	}
}
```

- [x] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api -run TestSourceUploadAcceptsTextAndRejectsUnsupported -count=1`

Expected: FAIL because upload handler still accepts only PDF.

- [x] **Step 3: Refactor API upload to detect format and normalize**

In `internal/api/upload.go`, replace the PDF-only suffix check with:

```go
format, err := source.DetectUploadFormat(filename, header.Header.Get("Content-Type"))
if err != nil {
	writeError(w, deps.Logger, http.StatusBadRequest, err.Error())
	return
}
```

When storing, use:

```go
src, dup, err := deps.Sources.Put(r.Context(), source.PutInput{
	Kind:     format.Kind,
	Filename: filename,
	MimeType: format.MimeType,
	Bytes:    body,
})
```

For non-PDF kinds, add this branch after duplicate handling:

```go
if format.Kind != source.KindPDF {
	res, err := source.ExtractGo(r.Context(), source.ExtractInput{Source: src, Bytes: body})
	if err != nil {
		_, _ = upsertSourceStatus(deps.Sources, src.ID, source.StatusFailed, err.Error())
		resp.Status = string(source.StatusFailed)
		resp.Note = "Extraction failed: " + err.Error()
		writeJSON(w, deps.Logger, http.StatusOK, resp)
		return
	}
	if err := source.WriteExtractionFiles(deps.Sources, src, res); err != nil {
		writeError(w, deps.Logger, http.StatusInternalServerError, "write extract files: "+err.Error())
		return
	}
	updated, err := deps.Sources.Update(src.ID, func(s *source.Source) error {
		s.Status = source.StatusExtractComplete
		s.Extract = &res.Metadata
		s.Error = ""
		return nil
	})
	if err != nil {
		writeError(w, deps.Logger, http.StatusInternalServerError, "status update: "+err.Error())
		return
	}
	resp.Status = string(updated.Status)
	if deps.Ingest != nil {
		ingCtx, ingCancel := context.WithTimeout(r.Context(), uploadOCRTimeout)
		note, err := deps.Ingest.AfterExtract(ingCtx, updated)
		ingCancel()
		if err != nil {
			resp.Note = "Extraction done; ingest failed: " + err.Error()
		} else {
			resp.IngestNote = note
			resp.Note = "ingested · " + note
			if fresh, ferr := deps.Sources.Get(src.ID); ferr == nil {
				resp.Status = string(fresh.Status)
				resp.WikiPages = fresh.WikiPages
			}
		}
	} else {
		resp.Note = "extracted · ready for ingest"
	}
	writeJSON(w, deps.Logger, http.StatusOK, resp)
	return
}
```

- [x] **Step 4: Update Telegram validation to shared format policy**

Rename `validatePDF` to `validateDocument` in `internal/telegram/documents.go` and return `source.UploadFormat`:

```go
func validateDocument(doc *tele.Document, maxFileMB int) (source.UploadFormat, error) {
	if doc == nil {
		return source.UploadFormat{}, errors.New("no document attached")
	}
	format, err := source.DetectUploadFormat(doc.FileName, doc.MIME)
	if err != nil {
		return source.UploadFormat{}, err
	}
	if maxFileMB > 0 {
		max := int64(maxFileMB) * 1024 * 1024
		if doc.FileSize > max {
			return source.UploadFormat{}, fmt.Errorf("file too large: %s exceeds %d MB cap", formatSize(doc.FileSize), maxFileMB)
		}
	}
	return format, nil
}
```

Thread the detected format into `process`, store with `format.Kind`, and use the same non-PDF extraction branch as the API path. Keep the PDF branch on OCR.

- [x] **Step 5: Update raw download and filter validation**

In `internal/api/sources.go`, add raw assets for text-like kinds:

```go
source.KindText:     {filename: "original.txt", contentType: "text/plain; charset=utf-8", disposition: "attachment"},
source.KindMarkdown: {filename: "original.md", contentType: "text/markdown; charset=utf-8", disposition: "attachment"},
source.KindJSON:     {filename: "original.json", contentType: "application/json", disposition: "attachment"},
source.KindCSV:      {filename: "original.csv", contentType: "text/csv; charset=utf-8", disposition: "attachment"},
```

Update `validKind` to include `KindMarkdown`, `KindJSON`, and `KindCSV`. Update `validStatus` to call `source.ValidStatus`.

- [x] **Step 6: Run focused tests and commit**

Run: `go test ./internal/api ./internal/telegram ./internal/source -count=1`

Expected: PASS.

Commit:

```bash
git add internal/api/upload.go internal/api/sources.go internal/api/router.go internal/api/upload_test.go internal/telegram/documents.go internal/telegram/documents_test.go
git commit -m "feat: accept universal source uploads"
```

---

## Task 6: Ingest Pipeline Reads Normalized Evidence

**Files:**
- Modify: `internal/ingest/pipeline.go`
- Test: `internal/ingest/pipeline_test.go`

- [ ] **Step 1: Write failing ingest test for extracted text source**

Add to `internal/ingest/pipeline_test.go`:

```go
func TestCompileExtractCompleteTextSource(t *testing.T) {
	env := newPipelineTestEnv(t)
	src, _, err := env.sources.Put(context.Background(), source.PutInput{
		Kind:     source.KindText,
		Filename: "notes.txt",
		MimeType: "text/plain",
		Bytes:    []byte("daily memory source"),
	})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := os.WriteFile(env.sources.Path(src.ID, source.ExtractMarkdownFile), []byte("Important normalized memory."), 0o644); err != nil {
		t.Fatalf("write extract: %v", err)
	}
	if _, err := env.sources.Update(src.ID, func(s *source.Source) error {
		s.Status = source.StatusExtractComplete
		return nil
	}); err != nil {
		t.Fatalf("status: %v", err)
	}
	res, err := env.pipeline.Compile(context.Background(), src.ID)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	page, err := env.wiki.ReadPage(res.Slug)
	if err != nil {
		t.Fatalf("read page: %v", err)
	}
	if !strings.Contains(page.Body, "Important normalized memory") {
		t.Fatalf("page body = %s", page.Body)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ingest -run TestCompileExtractCompleteTextSource -count=1`

Expected: FAIL because `Compile` only accepts `ocr_complete` or `ingested`.

- [ ] **Step 3: Accept `extract_complete` and read `extract.md` first**

In `internal/ingest/pipeline.go`, replace the status check with:

```go
if src.Status != source.StatusOCRComplete && src.Status != source.StatusExtractComplete && src.Status != source.StatusIngested {
	return Result{}, fmt.Errorf("ingest: source %s status is %s, want extract_complete", sourceID, src.Status)
}
```

Replace direct `ocr.md` read with helper:

```go
rawMD, evidenceLabel, err := p.readSourceEvidence(sourceID)
if err != nil {
	return Result{}, err
}
preview := buildPreview(string(rawMD), previewMaxChars)
body := buildSummaryBody(src, preview, evidenceLabel)
```

Add helper:

```go
func (p *Pipeline) readSourceEvidence(sourceID string) ([]byte, string, error) {
	for _, candidate := range []struct {
		name  string
		label string
	}{
		{source.ExtractMarkdownFile, "normalized extraction"},
		{"ocr.md", "raw OCR"},
	} {
		path := p.sources.Path(sourceID, candidate.name)
		if path == "" {
			continue
		}
		b, err := os.ReadFile(path)
		if err == nil {
			return b, candidate.label, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, "", fmt.Errorf("ingest: read %s: %w", candidate.name, err)
		}
	}
	return nil, "", fmt.Errorf("ingest: extract.md missing for %s; run extraction first", sourceID)
}
```

Update `buildSummaryBody` signature:

```go
func buildSummaryBody(src *source.Source, preview string, evidenceLabel string) string
```

Replace the Raw OCR section title/body with:

```go
sb.WriteString("\n## Source Evidence\n\n")
fmt.Fprintf(&sb, "Full %s markdown lives at `wiki/raw/%s/%s` (read via the `read_source` tool).\n", evidenceLabel, src.ID, source.ExtractMarkdownFile)
```

- [ ] **Step 4: Add AfterExtract hook**

Add to `internal/ingest/pipeline.go`:

```go
func (p *Pipeline) AfterExtract(ctx context.Context, src *source.Source) (string, error) {
	res, err := p.Compile(ctx, src.ID)
	if err != nil {
		return "", err
	}
	return res.PageNote, nil
}
```

- [ ] **Step 5: Run tests and commit**

Run: `go test ./internal/ingest ./internal/api ./internal/telegram -count=1`

Expected: PASS.

Commit:

```bash
git add internal/ingest/pipeline.go internal/ingest/pipeline_test.go
git commit -m "feat: ingest normalized source evidence"
```

---

## Task 7: Dashboard Multi-Format Source Inbox

**Files:**
- Modify: `web/src/types/api.ts`
- Modify: `web/src/components/SourceInbox.tsx`
- Modify: `web/src/i18n/locales/en.json`
- Modify: `web/src/i18n/locales/it.json`

- [ ] **Step 1: Update TypeScript source unions**

In `web/src/types/api.ts`, update `SourceSummary`:

```ts
export interface SourceSummary {
  id: string;
  kind: 'pdf' | 'text' | 'markdown' | 'json' | 'csv' | 'xlsx' | 'docx' | 'url' | 'pdf_generated' | 'sandbox_artifact';
  filename: string;
  status: 'stored' | 'extracting' | 'ocr_complete' | 'extract_complete' | 'ingested' | 'failed';
  created_at: string;
  page_count?: number;
  wiki_pages?: string[];
}
```

- [ ] **Step 2: Update source inbox status order and upload filtering**

In `web/src/components/SourceInbox.tsx`, replace `STATUS_ORDER`:

```ts
const STATUS_ORDER: SourceSummary['status'][] = ['failed', 'stored', 'extracting', 'ocr_complete', 'extract_complete', 'ingested'];
const ACCEPTED_SOURCE_EXTENSIONS = ['.pdf', '.txt', '.md', '.json', '.csv', '.docx', '.xlsx'];
const ACCEPTED_SOURCE_INPUT = '.pdf,.txt,.md,.json,.csv,.docx,.xlsx,application/pdf,text/plain,text/markdown,application/json,text/csv,application/vnd.openxmlformats-officedocument.wordprocessingml.document,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet';
```

Replace PDF-only filtering:

```ts
const accepted = list.filter((f) => {
  const lower = f.name.toLowerCase();
  return ACCEPTED_SOURCE_EXTENSIONS.some((ext) => lower.endsWith(ext));
});
const skipped = list.length - accepted.length;
if (skipped > 0) {
  toast.warning(t('sources.toast.uploadSkipped', { count: skipped }));
}
if (accepted.length === 0) return;
```

Loop over `accepted` instead of `pdfs`, and update input:

```tsx
accept={ACCEPTED_SOURCE_INPUT}
```

- [ ] **Step 3: Update action eligibility**

In `SourceActions`, replace `showDownload` and `ocrEligible`:

```ts
const downloadableKinds: SourceSummary['kind'][] = ['pdf', 'text', 'markdown', 'json', 'csv', 'xlsx', 'docx', 'pdf_generated', 'sandbox_artifact'];
const showDownload = downloadableKinds.includes(s.kind);
const ocrEligible = s.kind === 'pdf';
const extractEligible = ['text', 'markdown', 'json', 'csv', 'xlsx', 'docx'].includes(s.kind);
const showIngest = (ocrEligible && (s.status === 'ocr_complete' || s.status === 'failed')) || (extractEligible && s.status === 'extract_complete');
```

- [ ] **Step 4: Update English and Italian labels**

In `web/src/i18n/locales/en.json`, replace the source upload/status strings:

```json
"sources.emptyHint": "No sources yet - drop a supported file above, or upload one in Telegram.",
"sources.status.extracting": "Extracting",
"sources.status.extractComplete": "Extraction complete",
"sources.drop.idle": "Drag PDF, TXT, MD, JSON, CSV, DOCX, or XLSX files here, or click to browse",
"sources.drop.active": "Drop your source files to upload",
"sources.drop.hint": "Aura stores the original file, extracts normalized evidence, and ingests it into the wiki.",
"sources.uploadFileLabel": "Upload source files",
"sources.toast.uploadSkipped_one": "Skipped {{count}} unsupported file",
"sources.toast.uploadSkipped_other": "Skipped {{count}} unsupported files"
```

Add matching Italian keys in `web/src/i18n/locales/it.json` with clear text:

```json
"sources.emptyHint": "Nessuna fonte ancora - trascina un file supportato qui sopra oppure caricalo da Telegram.",
"sources.status.extracting": "Estrazione in corso",
"sources.status.extractComplete": "Estrazione completata",
"sources.drop.idle": "Trascina file PDF, TXT, MD, JSON, CSV, DOCX o XLSX qui, oppure clicca per sfogliare",
"sources.drop.active": "Rilascia i file sorgente per caricarli",
"sources.drop.hint": "Aura conserva il file originale, estrae evidenza normalizzata e la importa nella wiki.",
"sources.uploadFileLabel": "Carica file sorgente",
"sources.toast.uploadSkipped_one": "Saltato {{count}} file non supportato",
"sources.toast.uploadSkipped_other": "Saltati {{count}} file non supportati"
```

- [ ] **Step 5: Run frontend checks and commit**

Run: `npm --prefix web run build`

Expected: PASS.

Commit:

```bash
git add web/src/types/api.ts web/src/components/SourceInbox.tsx web/src/i18n/locales/en.json web/src/i18n/locales/it.json
git commit -m "feat: show universal source ingestion in dashboard"
```

---

## Task 8: Mixed-Source Memory Scorecard

**Review correction:** this task must not be a self-referential fixture-term checker. A fixture loader is useful, but the release scorecard must execute real Aura paths: store source fixtures, extract normalized evidence, compile/ingest into wiki/search surfaces, run retrieval/proposal checks, and report which evidence was selected. The simplified `EvaluateDeterministic` skeleton below is acceptable only as a first failing-test scaffold for fixture validation; it is not sufficient for `REL-03`.

**Files:**
- Create: `internal/memoryscore/scorecard.go`
- Create: `internal/memoryscore/scorecard_test.go`
- Create: `cmd/debug_memory_scorecard/main.go`
- Create: `testdata/memoryscore/v1_2/cases.json`
- Create: `testdata/memoryscore/v1_2/sources/`

- [ ] **Step 1: Write failing scorecard tests**

Create `internal/memoryscore/scorecard_test.go`:

```go
package memoryscore

import (
	"path/filepath"
	"testing"
)

func TestLoadFixtureAndThreshold(t *testing.T) {
	fixture := filepath.Join("..", "..", "testdata", "memoryscore", "v1_2", "cases.json")
	card, err := Load(fixture)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(card.Cases) != 6 {
		t.Fatalf("cases = %d, want 6", len(card.Cases))
	}
	result := EvaluateDeterministic(card)
	if !result.Passed {
		t.Fatalf("scorecard failed: %+v", result)
	}
	if result.PassedCases < 5 {
		t.Fatalf("passed cases = %d, want >= 5", result.PassedCases)
	}
}
```

- [ ] **Step 2: Create fixture**

Create `testdata/memoryscore/v1_2/cases.json`:

```json
{
  "threshold": 5,
  "cases": [
    {"name": "project status recall", "requirement": "MEMEVAL-01", "query": "What is the current Aura milestone?", "expected_terms": ["v1.2", "universal", "ingestion"]},
    {"name": "decision recall", "requirement": "RET-01", "query": "What formats are in scope?", "expected_terms": ["pdf", "txt", "md", "json", "csv", "docx", "xlsx"]},
    {"name": "next action recall", "requirement": "MEMEVAL-01", "query": "What should happen before implementation?", "expected_terms": ["plan", "review", "scorecard"]},
    {"name": "stale fact resistance", "requirement": "RET-01", "query": "Is Aura still PDF-only?", "expected_terms": ["not only pdf", "multi-format"]},
    {"name": "table fact recall", "requirement": "RET-01", "query": "Which extractor handles spreadsheet tables?", "expected_terms": ["pyodide", "xlsx", "pandas"]},
    {"name": "proposal quality", "requirement": "PROP-02", "query": "What should wiki proposals cite?", "expected_terms": ["source", "evidence", "normalized"]}
  ]
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/memoryscore -count=1`

Expected: FAIL with missing package implementation.

- [ ] **Step 4: Implement deterministic scorecard**

Create `internal/memoryscore/scorecard.go` as a real-path runner. It may start with fixture loading and result aggregation, but before this task is complete it must:

- create an isolated temp source/wiki/search environment;
- load fixture files from `testdata/memoryscore/v1_2/sources/`;
- call the same extraction helpers used by Telegram/dashboard upload;
- call the ingest pipeline on extracted sources;
- query retrieval/proposal helpers for each case;
- pass or fail based on selected evidence and expected behavior, not on terms embedded in the case definition.

The early scaffold can look like:

```go
package memoryscore

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type Card struct {
	Threshold int    `json:"threshold"`
	Cases     []Case `json:"cases"`
}

type Case struct {
	Name          string   `json:"name"`
	Requirement   string   `json:"requirement"`
	Query         string   `json:"query"`
	ExpectedTerms []string `json:"expected_terms"`
}

type Result struct {
	Passed      bool
	PassedCases int
	TotalCases  int
	Failures    []string
}

func Load(path string) (Card, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Card{}, err
	}
	var card Card
	if err := json.Unmarshal(b, &card); err != nil {
		return Card{}, err
	}
	if card.Threshold <= 0 {
		return Card{}, fmt.Errorf("memoryscore: threshold must be positive")
	}
	if len(card.Cases) == 0 {
		return Card{}, fmt.Errorf("memoryscore: at least one case is required")
	}
	return card, nil
}

func EvaluateDeterministic(card Card) Result {
	res := Result{TotalCases: len(card.Cases)}
	for _, c := range card.Cases {
		haystack := strings.ToLower(c.Name + " " + c.Requirement + " " + c.Query + " " + strings.Join(c.ExpectedTerms, " "))
		ok := true
		for _, term := range c.ExpectedTerms {
			if !strings.Contains(haystack, strings.ToLower(term)) {
				ok = false
				res.Failures = append(res.Failures, c.Name+": missing "+term)
			}
		}
		if ok {
			res.PassedCases++
		}
	}
	res.Passed = res.PassedCases >= card.Threshold
	return res
}
```

- [ ] **Step 5: Add debug command**

Create `cmd/debug_memory_scorecard/main.go`:

```go
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/aura/aura/internal/memoryscore"
)

func main() {
	fixture := flag.String("fixture", "testdata/memoryscore/v1_2/cases.json", "scorecard fixture path")
	flag.Parse()
	card, err := memoryscore.Load(*fixture)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	res := memoryscore.EvaluateDeterministic(card)
	fmt.Printf("memory scorecard: %d/%d passed (threshold %d)\n", res.PassedCases, res.TotalCases, card.Threshold)
	for _, failure := range res.Failures {
		fmt.Println("FAIL:", failure)
	}
	if !res.Passed {
		os.Exit(1)
	}
}
```

- [ ] **Step 6: Run scorecard and commit**

Run:

```bash
go test ./internal/memoryscore -count=1
go run ./cmd/debug_memory_scorecard
```

Expected:

```text
memory scorecard: 6/6 passed (threshold 5)
```

Before commit, inspect one failure-mode run by temporarily removing a required fixture or expected term and confirm the command fails with the case name and missing evidence reason.

Commit:

```bash
git add internal/memoryscore cmd/debug_memory_scorecard testdata/memoryscore/v1_2
git commit -m "test: add mixed-source memory scorecard"
```

---

## Task 9: Source-Aware Retrieval and Proposal Evidence

**Files:**
- Modify: `internal/tools/source.go`
- Modify: `internal/tools/memory_search.go`
- Modify: `internal/tools/wiki_proposal.go`
- Modify: `internal/conversation/summarizer/proposals.go`
- Test: `internal/tools/source_test.go`
- Test: `internal/tools/wiki_test.go`
- Test: `internal/conversation/summarizer/proposals_test.go`

- [ ] **Step 1: Write failing read-source test for `extract.md`**

In the package that currently tests `read_source`, add:

```go
func TestReadSourcePrefersNormalizedExtraction(t *testing.T) {
	store := newTestSourceStore(t)
	src, _, err := store.Put(context.Background(), source.PutInput{
		Kind:     source.KindCSV,
		Filename: "budget.csv",
		MimeType: "text/csv",
		Bytes:    []byte("item,cost\nsandbox,12\n"),
	})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := os.WriteFile(store.Path(src.ID, source.ExtractMarkdownFile), []byte("| item | cost |\n| --- | --- |\n| sandbox | 12 |\n"), 0o644); err != nil {
		t.Fatalf("write extract: %v", err)
	}
	got, err := readSourceMarkdown(store, src)
	if err != nil {
		t.Fatalf("readSourceMarkdown() error = %v", err)
	}
	if !strings.Contains(got, "sandbox") {
		t.Fatalf("markdown = %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run the focused package test for `internal/tools`.

Expected: FAIL because `readSourceMarkdown` still prefers `ocr.md` for file sources.

- [ ] **Step 3: Update read-source evidence order**

In `internal/tools/source.go`, update `readSourceMarkdown` to read:

```go
for _, name := range []string{source.ExtractMarkdownFile, "ocr.md"} {
	path := store.Path(src.ID, name)
	if path == "" {
		continue
	}
	b, err := os.ReadFile(path)
	if err == nil {
		return string(b), nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
}
```

Keep existing fallback for text and URL sources only when no normalized file exists.

- [ ] **Step 4: Add proposal provenance evidence helpers**

In `internal/tools/wiki_proposal.go`, keep the existing `evidence` argument contract and add source examples to the tool description so the model can cite normalized source evidence:

```go
"evidence": map[string]any{
	"type": "array",
	"description": "Compact evidence refs. For normalized sources use kind=source, id=src_<id>, title=filename, snippet=relevant extracted text.",
	"items": map[string]any{
		"type": "object",
		"properties": map[string]any{
			"kind": map[string]any{"type": "string"},
			"id": map[string]any{"type": "string"},
			"title": map[string]any{"type": "string"},
			"snippet": map[string]any{"type": "string"},
		},
	},
},
```

In `internal/conversation/summarizer/proposals.go`, add a helper next to `cleanProvenance`:

```go
func compactEvidenceSnippet(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if max <= 0 || len(s) <= max {
		return s
	}
	return strings.TrimSpace(s[:max]) + "..."
}
```

In `internal/conversation/summarizer/proposals_test.go`, add:

```go
func TestCompactEvidenceSnippet(t *testing.T) {
	got := compactEvidenceSnippet("  normalized   source evidence from CSV table  ", 28)
	if got != "normalized source evidence..." {
		t.Fatalf("snippet = %q", got)
	}
}
```

In `internal/tools/wiki_test.go`, extend the existing provenance assertion with a normalized-source evidence case:

```go
if got.Provenance.Evidence[0].Kind != "source" || got.Provenance.Evidence[0].ID != "src_abc" {
	t.Fatalf("evidence = %+v", got.Provenance.Evidence)
}
```

- [ ] **Step 5: Run focused tests and commit**

Run: `go test ./internal/tools ./internal/conversation ./internal/wiki -run "Source|Proposal|Memory" -count=1`

Expected: PASS.

Commit:

```bash
git add internal/tools/source.go internal/tools/source_test.go internal/tools/wiki_proposal.go internal/tools/wiki_test.go internal/conversation/summarizer/proposals.go internal/conversation/summarizer/proposals_test.go
git commit -m "feat: ground memory proposals in normalized sources"
```

---

## Task 10: Skills + Sandbox + Memory E2E

**Purpose:** prove the agent can use procedural memory without hardcoding every workflow: read an Aura skill from `D:\Aura\skills`, produce useful Python code, execute it in the bundled sandbox, persist the script/result as source evidence, and recall it later.

**Files:**
- Create: `cmd/debug_skill_sandbox_memory/main.go`
- Create: `testdata/skill_sandbox_memory/README.md`
- Modify: `docs/implementation-tracker.md`
- Test: `cmd/debug_skill_sandbox_memory/main_test.go`

- [ ] **Step 1: Add or verify local Aura skills**

Ensure at least these skills exist under `D:\Aura\skills`:

- `aura-python-sandbox`: instructs the agent to write bounded, deterministic Python scripts for Pyodide with explicit stdout/result files.
- `aura-source-extraction`: instructs the agent to turn script outputs into source-backed evidence, not ad hoc chat text.

Do not install or depend on remote skill catalogs for the release gate. Remote catalogs can inspire skill shape, but the E2E must run from the local Aura skills folder.

- [ ] **Step 2: Write the E2E command**

Create `cmd/debug_skill_sandbox_memory/main.go` so it performs the full flow:

1. Load skills from the same roots as production, including `D:\Aura\skills`.
2. Read `aura-python-sandbox` and `aura-source-extraction`.
3. Generate or select a deterministic Python script that processes a mixed-source fixture, for example CSV/XLSX rows into a compact markdown summary.
4. Execute the script with `internal/sandbox` Pyodide using no network and bounded timeout.
5. Persist both the generated script and result markdown as source records.
6. Read those source records back through the source/tool path.
7. Print a compact PASS line with the skill names, script source ID, result source ID, and recall assertion.

Expected output shape:

```text
skill sandbox memory e2e: PASS skills=aura-python-sandbox,aura-source-extraction script=src_... result=src_... recall=ok
```

- [ ] **Step 3: Add tests for failure modes**

Add `cmd/debug_skill_sandbox_memory/main_test.go` with focused coverage for:

- missing skill returns a clear failure;
- sandbox runtime unavailable returns a clear skip/failure depending on command flags;
- persisted script/result are readable source evidence;
- binary sandbox artifacts are not treated as text recall evidence.

- [ ] **Step 4: Run real E2E**

Run:

```bash
go test ./cmd/debug_skill_sandbox_memory ./internal/skills ./internal/tools ./internal/sandbox -count=1
go run ./cmd/debug_skill_sandbox_memory --timeout 2m
```

Expected: tests pass and the command prints the PASS line above. This is the anti-"hello world" gate for the skills/sandbox autonomy part of the milestone.

- [ ] **Step 5: Commit**

```bash
git add cmd/debug_skill_sandbox_memory testdata/skill_sandbox_memory docs/implementation-tracker.md
git commit -m "test: prove skill-guided sandbox memory e2e"
```

---

## Task 11: Release Gate and Documentation

**Files:**
- Modify: `docs/implementation-tracker.md`
- Modify: `.planning/PROJECT.md`
- Modify: `.planning/ROADMAP.md`
- Modify: `.planning/STATE.md`
- Create: `.planning/phases/02-universal-ingestion-memory-quality/VALIDATION.md`

- [ ] **Step 1: Run release verification**

Run:

```bash
go test ./internal/source ./internal/ingest ./internal/api ./internal/telegram ./internal/tools ./internal/memoryscore -count=1
go run ./cmd/debug_memory_scorecard
go run ./cmd/debug_skill_sandbox_memory --timeout 2m
go test ./...
npm --prefix web run build
go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean
scripts\check-windows-gui-subsystem.ps1 <extracted snapshot aura.exe>
```

Expected:

```text
memory scorecard: 6/6 passed (threshold 5)
skill sandbox memory e2e: PASS ...
windows gui subsystem ok
```

All test/build commands exit 0.

- [ ] **Step 2: Write validation record**

Create `.planning/phases/02-universal-ingestion-memory-quality/VALIDATION.md`:

```markdown
# v1.2 Universal Ingestion + Memory Quality Validation

Date: 2026-05-05

## Commands

- `go test ./internal/source ./internal/ingest ./internal/api ./internal/telegram ./internal/tools ./internal/memoryscore -count=1`
- `go run ./cmd/debug_memory_scorecard`
- `go run ./cmd/debug_skill_sandbox_memory --timeout 2m`
- `go test ./...`
- `npm --prefix web run build`
- `go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean`
- `scripts\check-windows-gui-subsystem.ps1` against the extracted Windows snapshot `aura.exe`

## Result

PASS.

## Supported Upload Formats

- PDF
- TXT
- Markdown
- JSON
- CSV
- DOCX
- XLSX

## Scorecard

Mixed-source memory scorecard: 6/6 passed, threshold 5/6.

## Known Gaps

- Images, audio, video, PPTX, cloud connectors, and website crawling are outside v1.2.
- Mistral OCR remains the PDF extraction path.
- Wiki updates remain review-gated.
- Runtime remains a single Aura agent using skills and tools; no additional runtime agent swarm is required for universal ingestion.
```

- [ ] **Step 3: Update tracker**

Add to `docs/implementation-tracker.md`:

```markdown
## 2026-05-05 - v1.2 Universal Ingestion + Memory Quality

- Added universal source ingestion for PDF, TXT, MD, JSON, CSV, DOCX, and XLSX.
- Introduced normalized source evidence files: `extract.md` and `extract.json`.
- Kept PDF OCR compatible by adapting `ocr.md` into the normalized extraction contract.
- Used bounded Pyodide extraction for spreadsheet/table evidence.
- Added mixed-source memory scorecard and release gate.
- Proved skill-guided sandbox script creation, execution, persisted source recall, and result recall from local Aura skills.
- Verification: focused source/ingest/API/Telegram/tools/memoryscore tests, skill sandbox memory E2E, `go test ./...`, dashboard build, GoReleaser snapshot, Windows GUI subsystem check, and scorecard all passed.
```

- [ ] **Step 4: Update planning state**

In `.planning/PROJECT.md`, `.planning/ROADMAP.md`, and `.planning/STATE.md`, mark v1.2 complete with:

```markdown
v1.2 Universal Ingestion + Memory Quality: complete.
Release gate: multi-format extraction tests passed; mixed-source scorecard 6/6 passed at threshold 5/6; skill sandbox memory E2E passed; Windows GUI subsystem and tray startup regression checked.
```

- [ ] **Step 5: Commit closure docs**

Commit:

```bash
git add docs/implementation-tracker.md .planning/PROJECT.md .planning/ROADMAP.md .planning/STATE.md .planning/phases/02-universal-ingestion-memory-quality/VALIDATION.md
git commit -m "docs: close v1.2 universal ingestion milestone"
```

---

## Self-Review

Spec coverage:

- INGEST-01 is covered by Tasks 1, 5, and 7.
- EXTRACT-01 is covered by Tasks 2, 3, 4, and 6.
- SANDBOX-01 is covered by Task 4.
- SKILL-SANDBOX-E2E is covered by Task 10.
- MEMEVAL-01 is covered by Task 8.
- RET-01 is covered by Tasks 6, 8, and 9.
- PROP-01 and PROP-02 are covered by Task 9.
- REL-03 is covered by Task 11.

Placeholder scan:

- No placeholder markers.
- No empty task bodies.
- Every task has concrete files, commands, expected outcomes, and commit scope.

Type consistency:

- New source kinds are `markdown`, `json`, and `csv`.
- New statuses are `extracting` and `extract_complete`; existing `ocr_complete` remains for PDF compatibility.
- Normalized evidence filenames are consistently `extract.md` and `extract.json`.
