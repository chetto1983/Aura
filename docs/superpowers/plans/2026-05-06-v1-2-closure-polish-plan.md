# v1.2 Closure and Polish Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close Aura's active v1.2 universal-ingestion and frontend-polish work by fixing format truth, wiring XLSX extraction, verifying source-to-wiki completion, and reconciling `.planning`.

**Architecture:** Keep the current source pipeline. Use `internal/source` as the format and extraction boundary, route XLSX through the existing sandbox/Pyodide runner, keep DOCX deferred until a real extractor exists, and keep `internal/ingest` as the source-to-wiki compiler. Frontend work stays in existing E2E/i18n audit surfaces.

**Tech Stack:** Go 1.24+, SQLite-backed Aura stores, existing `internal/source`, `internal/ingest`, `internal/sandbox`, React 19, Vite, Playwright, existing `.planning` docs.

---

## File Structure

- Modify `internal/source/formats.go`: make supported upload formats match implemented extraction behavior; remove DOCX from active acceptance until a DOCX extractor exists.
- Modify `internal/source/formats_test.go`: lock supported formats and DOCX rejection.
- Create `internal/source/extract_auto.go`: select Go extractors for text-like files and Pyodide extraction for XLSX.
- Modify `internal/source/extract_test.go`: cover the new extractor selector and missing-runner behavior.
- Modify `internal/api/router.go`: expose a sandbox extraction runner in `api.Deps`.
- Modify `internal/api/upload.go`: call the format-neutral extractor selector for non-PDF uploads.
- Modify `internal/api/upload_test.go`: add XLSX upload extraction coverage with a fake runner and assert DOCX rejection.
- Modify `internal/telegram/documents.go`: pass an extraction runner into the document handler and use the shared extractor selector.
- Modify `internal/telegram/documents_test.go`: add XLSX extraction coverage with a fake runner and assert DOCX rejection.
- Modify `internal/telegram/setup.go`: wire `sandboxMgr` into API and Telegram document upload extraction.
- Modify `web/src/components/SourceInbox.tsx`: remove DOCX from accepted upload UI until DOCX extraction is real.
- Modify `web/src/types/api.ts`: keep API source kinds truthful; remove `docx` from upload affordances if present.
- Modify `web/src/i18n/locales/en.json` and `web/src/i18n/locales/it.json`: make source upload copy list only active formats.
- Modify `web/e2e/source-universal-upload.spec.ts`: assert `.xlsx` is accepted and `.docx` is not advertised.
- Modify `.planning/STATE.md`, `.planning/ROADMAP.md`, `.planning/PROJECT.md`, `.planning/REQUIREMENTS.md`, and `docs/implementation-tracker.md`: record v1.2 closure truth.
- Create `.planning/phases/02-v1-2-closure-polish/VALIDATION.md`: record verification commands and results.

---

### Task 1: Make Supported Formats Truthful

**Files:**
- Modify: `internal/source/formats.go`
- Modify: `internal/source/formats_test.go`
- Modify: `web/src/components/SourceInbox.tsx`
- Modify: `web/src/i18n/locales/en.json`
- Modify: `web/src/i18n/locales/it.json`
- Test: `internal/source/formats_test.go`
- Test: `web/e2e/source-universal-upload.spec.ts`

- [ ] **Step 1: Write the failing backend format-policy test**

In `internal/source/formats_test.go`, change the accepted-format table so it excludes DOCX and includes XLSX. Add this rejection assertion:

```go
func TestDetectUploadFormatRejectsDeferredDOCX(t *testing.T) {
	_, err := DetectUploadFormat("memo.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	if err == nil || !strings.Contains(err.Error(), "unsupported file type") {
		t.Fatalf("err = %v, want unsupported file type for deferred DOCX", err)
	}
}
```

If the file does not already import `strings`, add it:

```go
import (
	"strings"
	"testing"
)
```

- [ ] **Step 2: Run the focused source test and verify it fails**

Run:

```powershell
go test ./internal/source -run "TestDetectUploadFormat" -count=1
```

Expected: FAIL because `DetectUploadFormat("memo.docx", ...)` still succeeds.

- [ ] **Step 3: Remove DOCX from the active backend policy**

In `internal/source/formats.go`, remove the `".docx"` entry from `formatsByExt` and remove both DOCX entries from `SupportedUploadAccept()`. The active accept string should become:

```go
func SupportedUploadAccept() string {
	return ".pdf,.txt,.md,.json,.csv,.xlsx,application/pdf,text/plain,text/markdown,application/json,text/csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
}
```

Keep `KindDOCX` in `internal/source/source.go`; generated DOCX artifacts and future DOCX ingestion can still use the kind once an extractor exists.

- [ ] **Step 4: Update frontend advertised formats**

In `web/src/components/SourceInbox.tsx`, ensure the accepted file string lists only active formats:

```ts
const ACCEPTED_SOURCE_INPUT = '.pdf,.txt,.md,.json,.csv,.xlsx,application/pdf,text/plain,text/markdown,application/json,text/csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet';
```

If `SourceInbox.tsx` has a local extension list for client-side filtering, make it:

```ts
const SUPPORTED_SOURCE_EXTENSIONS = ['.pdf', '.txt', '.md', '.json', '.csv', '.xlsx'];
```

- [ ] **Step 5: Update source upload labels**

In `web/src/i18n/locales/en.json`, set the source upload idle label to:

```json
"sources.drop.idle": "Drag PDF, TXT, MD, JSON, CSV, or XLSX files here, or click to browse"
```

In `web/src/i18n/locales/it.json`, set the Italian label to:

```json
"sources.drop.idle": "Trascina file PDF, TXT, MD, JSON, CSV o XLSX qui, oppure clicca per sfogliare"
```

- [ ] **Step 6: Update the frontend upload E2E assertion**

In `web/e2e/source-universal-upload.spec.ts`, extend the accept assertions:

```ts
await expect(input).toHaveAttribute('accept', /\.xlsx/);
await expect(input).not.toHaveAttribute('accept', /\.docx/);
```

- [ ] **Step 7: Run focused checks and commit**

Run:

```powershell
go test ./internal/source -run "TestDetectUploadFormat|TestExtractionStatuses" -count=1
npm --prefix web run i18n:check
npm --prefix web run e2e -- source-universal-upload.spec.ts
```

Expected: all commands PASS.

Commit:

```powershell
git add internal/source/formats.go internal/source/formats_test.go web/src/components/SourceInbox.tsx web/src/i18n/locales/en.json web/src/i18n/locales/it.json web/e2e/source-universal-upload.spec.ts
git commit -m "fix: make v1.2 upload formats truthful"
```

---

### Task 2: Add a Shared Non-PDF Extraction Selector

**Files:**
- Create: `internal/source/extract_auto.go`
- Modify: `internal/source/extract_test.go`
- Test: `internal/source/extract_test.go`

- [ ] **Step 1: Write the failing selector tests**

Append these tests to `internal/source/extract_test.go`:

```go
type fakeExtractionRunner struct {
	stdout string
	calls  int
}

func (r *fakeExtractionRunner) Execute(context.Context, string, bool) (*sandbox.Result, error) {
	r.calls++
	return &sandbox.Result{OK: true, Stdout: r.stdout}, nil
}

func TestExtractUploadedSourceUsesGoForTextLikeKinds(t *testing.T) {
	res, err := ExtractUploadedSource(context.Background(), nil, ExtractInput{
		Source: &Source{ID: "src_0123456789abcdef", Kind: KindCSV, Filename: "budget.csv"},
		Bytes:  []byte("item,cost\nsandbox,12\n"),
	})
	if err != nil {
		t.Fatalf("ExtractUploadedSource() error = %v", err)
	}
	if !strings.Contains(res.Markdown, "sandbox") || res.Metadata.ExtractorName != "go_csv" {
		t.Fatalf("result = %+v\n%s", res.Metadata, res.Markdown)
	}
}

func TestExtractUploadedSourceUsesPyodideForXLSX(t *testing.T) {
	runner := &fakeExtractionRunner{stdout: `{"markdown":"| item | cost |\n| --- | --- |\n| sandbox | 12 |\n","metadata":{"extractor_name":"pyodide_xlsx","extractor_version":"pyodide_xlsx_v1","text_bytes":54,"sheet_count":1,"row_count":1}}`}
	res, err := ExtractUploadedSource(context.Background(), runner, ExtractInput{
		Source: &Source{ID: "src_0123456789abcdef", Kind: KindXLSX, Filename: "budget.xlsx"},
		Bytes:  []byte("fake workbook bytes"),
	})
	if err != nil {
		t.Fatalf("ExtractUploadedSource() error = %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls = %d, want 1", runner.calls)
	}
	if res.Metadata.ExtractorName != "pyodide_xlsx" || !strings.Contains(res.Markdown, "sandbox") {
		t.Fatalf("result = %+v\n%s", res.Metadata, res.Markdown)
	}
}

func TestExtractUploadedSourceXLSXRequiresRunner(t *testing.T) {
	_, err := ExtractUploadedSource(context.Background(), nil, ExtractInput{
		Source: &Source{ID: "src_0123456789abcdef", Kind: KindXLSX, Filename: "budget.xlsx"},
		Bytes:  []byte("fake workbook bytes"),
	})
	if err == nil || !strings.Contains(err.Error(), "xlsx extraction requires Pyodide") {
		t.Fatalf("err = %v, want Pyodide requirement", err)
	}
}
```

Add the sandbox import to `internal/source/extract_test.go`:

```go
import "github.com/aura/aura/internal/sandbox"
```

- [ ] **Step 2: Run the selector tests and verify they fail**

Run:

```powershell
go test ./internal/source -run "TestExtractUploadedSource" -count=1
```

Expected: FAIL because `ExtractUploadedSource` does not exist.

- [ ] **Step 3: Implement the selector**

Create `internal/source/extract_auto.go`:

```go
package source

import (
	"context"
	"fmt"

	"github.com/aura/aura/internal/sandbox"
)

type PyodideRunner interface {
	Execute(context.Context, string, bool) (*sandbox.Result, error)
}

func ExtractUploadedSource(ctx context.Context, runner PyodideRunner, in ExtractInput) (ExtractResult, error) {
	if in.Source == nil {
		return ExtractResult{}, fmt.Errorf("source: nil source")
	}
	switch in.Source.Kind {
	case KindText, KindMarkdown, KindJSON, KindCSV:
		return ExtractGo(ctx, in)
	case KindXLSX:
		if runner == nil {
			return ExtractResult{}, fmt.Errorf("source: xlsx extraction requires Pyodide runtime")
		}
		return ExtractWithPyodide(ctx, runner, in)
	default:
		return ExtractResult{}, fmt.Errorf("source: no extractor for kind %s", in.Source.Kind)
	}
}
```

- [ ] **Step 4: Run tests and commit**

Run:

```powershell
go test ./internal/source -run "TestExtractUploadedSource|TestGoExtractors|TestPyodideXLSXExtractor" -count=1
```

Expected: PASS. `TestPyodideXLSXExtractor` may skip if the local Pyodide runtime is unavailable; that skip is acceptable for this focused unit run.

Commit:

```powershell
git add internal/source/extract_auto.go internal/source/extract_test.go
git commit -m "feat: route uploaded source extraction by format"
```

---

### Task 3: Wire XLSX Extraction Into API Uploads

**Files:**
- Modify: `internal/api/router.go`
- Modify: `internal/api/upload.go`
- Modify: `internal/api/upload_test.go`
- Modify: `internal/telegram/setup.go`
- Test: `internal/api/upload_test.go`

- [ ] **Step 1: Write failing API tests for XLSX and DOCX truth**

Append these tests to `internal/api/upload_test.go`:

```go
func TestSourceUploadXLSXUsesPyodideExtraction(t *testing.T) {
	e := newTestEnv(t)
	runner := &fakeUploadPyodideRunner{stdout: `{"markdown":"| item | cost |\n| --- | --- |\n| sandbox | 12 |\n","metadata":{"extractor_name":"pyodide_xlsx","extractor_version":"pyodide_xlsx_v1","text_bytes":54,"sheet_count":1,"row_count":1}}`}
	e.router = NewRouter(Deps{
		Wiki:      e.wiki,
		Sources:   e.sources,
		Scheduler: e.sched,
		Extractor: runner,
	})

	rr := e.uploadFile("budget.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", []byte("fake workbook bytes"))
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
	if src.Kind != source.KindXLSX || src.Extract == nil || src.Extract.ExtractorName != "pyodide_xlsx" {
		t.Fatalf("source = %+v, want xlsx with pyodide metadata", src)
	}
	extract, err := os.ReadFile(e.sources.Path(got.ID, source.ExtractMarkdownFile))
	if err != nil {
		t.Fatalf("read extract.md: %v", err)
	}
	if !strings.Contains(string(extract), "sandbox") {
		t.Fatalf("extract.md = %q", extract)
	}
}

func TestSourceUploadRejectsDeferredDOCX(t *testing.T) {
	e := newTestEnv(t)
	rr := e.uploadFile("memo.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", []byte("docx"))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("docx upload status = %d, want 400 body=%s", rr.Code, rr.Body.String())
	}
}

type fakeUploadPyodideRunner struct {
	stdout string
	calls  int
}

func (r *fakeUploadPyodideRunner) Execute(context.Context, string, bool) (*sandbox.Result, error) {
	r.calls++
	return &sandbox.Result{OK: true, Stdout: r.stdout}, nil
}
```

Add imports if missing:

```go
import (
	"context"

	"github.com/aura/aura/internal/sandbox"
)
```

- [ ] **Step 2: Run the API tests and verify they fail**

Run:

```powershell
go test ./internal/api -run "TestSourceUploadXLSXUsesPyodideExtraction|TestSourceUploadRejectsDeferredDOCX" -count=1
```

Expected: FAIL because `api.Deps` has no `Extractor` field.

- [ ] **Step 3: Add extractor dependency to API deps**

In `internal/api/router.go`, import the selector interface through the existing source import and add this field to `Deps` near `OCR` and `Ingest`:

```go
	// Extractor runs fixed Pyodide-backed source extractors for formats that
	// need the sandbox runtime, currently XLSX. Nil keeps text-like extraction
	// working and makes XLSX fail with a clear extraction error.
	Extractor source.PyodideRunner
```

- [ ] **Step 4: Use the selector in upload handling**

In `internal/api/upload.go`, replace this call:

```go
res, err := source.ExtractGo(r.Context(), source.ExtractInput{Source: src, Bytes: body})
```

with:

```go
res, err := source.ExtractUploadedSource(r.Context(), deps.Extractor, source.ExtractInput{Source: src, Bytes: body})
```

Keep the existing failure behavior that updates the source to `failed` and returns HTTP 200 with an extraction failure note. That behavior is correct for accepted but malformed XLSX files.

- [ ] **Step 5: Wire production API dependency**

In `internal/telegram/setup.go`, pass the existing `sandboxMgr` into `api.NewRouter`:

```go
		Extractor:   sandboxMgr,
```

Place it near `OCR` and `Ingest` in the `api.Deps` literal.

- [ ] **Step 6: Run focused API tests and commit**

Run:

```powershell
go test ./internal/api -run "TestSourceUploadXLSXUsesPyodideExtraction|TestSourceUploadRejectsDeferredDOCX|TestSourceUploadTextCanBeIngested|TestSourceUploadAcceptsText" -count=1
```

Expected: PASS.

Commit:

```powershell
git add internal/api/router.go internal/api/upload.go internal/api/upload_test.go internal/telegram/setup.go
git commit -m "feat: extract xlsx dashboard uploads with pyodide"
```

---

### Task 4: Wire XLSX Extraction Into Telegram Uploads

**Files:**
- Modify: `internal/telegram/documents.go`
- Modify: `internal/telegram/documents_test.go`
- Modify: `internal/telegram/setup.go`
- Test: `internal/telegram/documents_test.go`

- [ ] **Step 1: Write failing Telegram XLSX and DOCX tests**

Append this test to `internal/telegram/documents_test.go`:

```go
func TestDocHandlerAuthorizedXLSXDocumentExtractsSource(t *testing.T) {
	var calls []telegramAPICall
	srv := newTelegramAPIServer(t, &calls)
	defer srv.Close()

	tb, err := tele.NewBot(tele.Settings{URL: srv.URL, Token: "test", Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	sources := newDocumentTestSourceStore(t)
	runner := &fakeDocumentPyodideRunner{stdout: `{"markdown":"| item | cost |\n| --- | --- |\n| sandbox | 12 |\n","metadata":{"extractor_name":"pyodide_xlsx","extractor_version":"pyodide_xlsx_v1","text_bytes":54,"sheet_count":1,"row_count":1}}`}
	h := newDocHandler(docHandlerConfig{
		Bot:       tb,
		Sources:   sources,
		Extractor: runner,
		Allowlist: func(string) bool { return true },
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(h.Stop)
	ctx := tele.NewContext(tb, tele.Update{Message: &tele.Message{
		Sender: &tele.User{ID: 123, Username: "owner"},
		Chat:   &tele.Chat{ID: 123},
		Document: &tele.Document{
			File:     tele.File{FileID: "doc-1", FileSize: int64(len(fakeTelegramPDFBytes))},
			MIME:     "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
			FileName: "budget.xlsx",
		},
	}})

	if err := h.onDocument(ctx); err != nil {
		t.Fatalf("onDocument() error = %v, want nil", err)
	}

	var stored []*source.Source
	waitUntil(t, time.Second, func() bool {
		var err error
		stored, err = sources.List(source.ListFilter{Kind: source.KindXLSX})
		return err == nil && len(stored) == 1 && stored[0].Status == source.StatusExtractComplete
	})
	h.Stop()

	if runner.calls != 1 {
		t.Fatalf("runner calls = %d, want 1", runner.calls)
	}
	if stored[0].Extract == nil || stored[0].Extract.ExtractorName != "pyodide_xlsx" {
		t.Fatalf("source extraction metadata = %+v", stored[0].Extract)
	}
	if _, err := os.Stat(sources.Path(stored[0].ID, source.ExtractMarkdownFile)); err != nil {
		t.Fatalf("extract.md missing: %v", err)
	}
}

func TestValidateDocumentRejectsDeferredDOCX(t *testing.T) {
	doc := &tele.Document{
		File:     tele.File{FileSize: 1024},
		MIME:     "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		FileName: "memo.docx",
	}
	_, err := validateDocument(doc, 100)
	if err == nil || !strings.Contains(err.Error(), "unsupported file type") {
		t.Fatalf("err = %v, want unsupported file type for deferred DOCX", err)
	}
}

type fakeDocumentPyodideRunner struct {
	stdout string
	calls  int
}

func (r *fakeDocumentPyodideRunner) Execute(context.Context, string, bool) (*sandbox.Result, error) {
	r.calls++
	return &sandbox.Result{OK: true, Stdout: r.stdout}, nil
}
```

Add imports if missing:

```go
import (
	"context"

	"github.com/aura/aura/internal/sandbox"
)
```

- [ ] **Step 2: Run Telegram tests and verify they fail**

Run:

```powershell
go test ./internal/telegram -run "TestDocHandlerAuthorizedXLSXDocumentExtractsSource|TestValidateDocumentRejectsDeferredDOCX" -count=1
```

Expected: FAIL because `docHandlerConfig` has no `Extractor` field.

- [ ] **Step 3: Add the extraction runner to document handler config**

In `internal/telegram/documents.go`, add the field:

```go
	Extractor source.PyodideRunner
```

to `docHandlerConfig`, and add:

```go
	extractor source.PyodideRunner
```

to `docHandler`.

In `newDocHandler`, copy the field:

```go
		extractor: cfg.Extractor,
```

- [ ] **Step 4: Use the shared extraction selector**

In `internal/telegram/documents.go`, replace:

```go
res, err := source.ExtractGo(ctx, source.ExtractInput{Source: src, Bytes: pdfBytes})
```

with:

```go
res, err := source.ExtractUploadedSource(ctx, h.extractor, source.ExtractInput{Source: src, Bytes: pdfBytes})
```

Keep the existing status update to `failed` and progress message when extraction fails.

- [ ] **Step 5: Wire production Telegram dependency**

In `internal/telegram/setup.go`, pass `sandboxMgr` into `newDocHandler`:

```go
		Extractor: sandboxMgr,
```

Place it near `OCR` and `AfterOCR`.

- [ ] **Step 6: Run focused Telegram tests and commit**

Run:

```powershell
go test ./internal/telegram -run "TestValidateDocument|TestDocHandlerAuthorized(Text|XLSX|PDF)DocumentStoresSource|TestDocHandlerAuthorizedTextDocumentExtractsSource" -count=1
```

Expected: PASS.

Commit:

```powershell
git add internal/telegram/documents.go internal/telegram/documents_test.go internal/telegram/setup.go
git commit -m "feat: extract xlsx telegram uploads with pyodide"
```

---

### Task 5: Verify Source-to-Wiki Closure

**Files:**
- Modify: `internal/api/upload_test.go`
- Modify: `internal/ingest/pipeline_test.go`
- Test: `internal/api/upload_test.go`
- Test: `internal/ingest/pipeline_test.go`

- [ ] **Step 1: Add an API upload-then-ingest XLSX test**

Append this test to `internal/api/upload_test.go`:

```go
func TestSourceUploadXLSXCanBeIngested(t *testing.T) {
	e := newTestEnv(t)
	pipeline, err := ingest.New(ingest.Config{Sources: e.sources, Wiki: e.wiki})
	if err != nil {
		t.Fatalf("ingest.New: %v", err)
	}
	runner := &fakeUploadPyodideRunner{stdout: `{"markdown":"| item | cost |\n| --- | --- |\n| sandbox | 12 |\n","metadata":{"extractor_name":"pyodide_xlsx","extractor_version":"pyodide_xlsx_v1","text_bytes":54,"sheet_count":1,"row_count":1}}`}
	e.router = NewRouter(Deps{
		Wiki:      e.wiki,
		Sources:   e.sources,
		Scheduler: e.sched,
		Ingest:    pipeline,
		Extractor: runner,
	})

	uploadRR := e.uploadFile("budget.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", []byte("fake workbook bytes"))
	if uploadRR.Code != http.StatusOK {
		t.Fatalf("xlsx upload status = %d body=%s", uploadRR.Code, uploadRR.Body.String())
	}
	var upload UploadResponse
	if err := json.Unmarshal(uploadRR.Body.Bytes(), &upload); err != nil {
		t.Fatalf("decode upload: %v", err)
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
	if !strings.Contains(page.Body, "Kind: xlsx") || !strings.Contains(page.Body, "extract.md") || !strings.Contains(page.Body, "sandbox") {
		t.Fatalf("page body missing XLSX extracted evidence:\n%s", page.Body)
	}
}
```

- [ ] **Step 2: Add an ingest metadata assertion**

In `internal/ingest/pipeline_test.go`, update `TestCompile_ExtractCompleteSource` so the expected page body includes the extracted markdown pointer and source kind:

```go
for _, want := range []string{
	"Kind: text",
	"wiki/raw/" + src.ID + "/extract.md",
	"Aura should remember text uploads.",
} {
	if !strings.Contains(page.Body, want) {
		t.Fatalf("page body missing %q:\n%s", want, page.Body)
	}
}
```

If that assertion already exists, leave it unchanged and add no duplicate test code.

- [ ] **Step 3: Run source-to-wiki tests and commit**

Run:

```powershell
go test ./internal/api ./internal/ingest -run "TestSourceUpload(XLSX|Text).*Ingested|TestCompile_ExtractCompleteSource" -count=1
```

Expected: PASS.

Commit:

```powershell
git add internal/api/upload_test.go internal/ingest/pipeline_test.go
git commit -m "test: cover extracted source ingest closure"
```

---

### Task 6: Run and Repair Frontend Polish Gate

**Files:**
- Modify only files needed by failed frontend checks.
- Expected likely files: `web/src/components/*.tsx`, `web/src/i18n/locales/en.json`, `web/src/i18n/locales/it.json`, `web/e2e/*.spec.ts`
- Test: frontend audit commands

- [ ] **Step 1: Run i18n check**

Run:

```powershell
npm --prefix web run i18n:check
```

Expected: PASS.

If it fails with a missing key such as `sources.drop.idle`, add the same key to both `web/src/i18n/locales/en.json` and `web/src/i18n/locales/it.json`, then rerun the command.

- [ ] **Step 2: Run all-page E2E**

Run:

```powershell
npm --prefix web run e2e:pages
```

Expected: PASS.

If a page fails because a heading is missing, add a semantic heading in that page component. Use this shape for hidden headings:

```tsx
<h1 className="sr-only">{t('page.titleKey')}</h1>
```

Add `page.titleKey` to both locale files with the page name users expect.

- [ ] **Step 3: Run full dashboard E2E**

Run:

```powershell
npm --prefix web run e2e
```

Expected: PASS.

If `source-universal-upload.spec.ts` fails because upload copy or accepted formats changed, update the spec to match the active list: PDF, TXT, MD, JSON, CSV, XLSX.

- [ ] **Step 4: Run frontend audit command**

Run:

```powershell
npm --prefix web run audit:frontend
```

Expected: PASS.

If the command fails because the local Aura server cannot start due to occupied ports, rerun after stopping the local server process. If it fails because a test assertion is stale, fix the component or test so the assertion reflects the active UI behavior.

- [ ] **Step 5: Rebuild embedded dashboard assets**

Run:

```powershell
npm --prefix web run build
```

Expected: PASS and `internal/api/dist` updates if frontend code changed.

- [ ] **Step 6: Commit frontend fixes**

If files changed, commit them:

```powershell
git add web internal/api/dist
git commit -m "fix: preserve v1.2 frontend polish gate"
```

If no frontend files changed, record the passing frontend commands in Task 8 validation and do not create an empty commit.

---

### Task 7: Reconcile Planning Documents

**Files:**
- Modify: `.planning/STATE.md`
- Modify: `.planning/ROADMAP.md`
- Modify: `.planning/PROJECT.md`
- Modify: `.planning/REQUIREMENTS.md`
- Modify: `docs/implementation-tracker.md`
- Test: docs grep and git diff check

- [ ] **Step 1: Update `.planning/STATE.md`**

Replace the current position block with:

```markdown
## Current Position

Current milestone: v1.2 Closure and Polish
Current phase: Release Gate Lite
Current focus: close active universal-ingestion and frontend-polish work
Status: implementation closure in progress
Last activity: 2026-05-06 - Approved v1.2 Closure and Polish design and implementation plan
```

Keep the existing recent activity log below it and prepend a new entry:

```markdown
[2026-05-06] Started v1.2 Closure and Polish. Scope is active v1.2 universal ingestion plus recent frontend polish only; broad memory scorecard, retrieval/proposal upgrades, and full DOCX extraction remain deferred.
```

- [ ] **Step 2: Replace `.planning/ROADMAP.md` with v1.2 closure roadmap**

Write this content:

```markdown
# Roadmap: Aura v1.2 - Closure and Polish

**Created:** 2026-05-06
**Milestone:** v1.2 Closure and Polish
**Total phases:** 4

## Milestone Goal

Close Aura's active universal-ingestion and frontend-polish work by making source upload support truthful, completing the extracted-source ingest loop, preserving dashboard audit fixes, and reconciling planning docs.

## Boundary

In scope:
- PDF, TXT, Markdown, JSON, CSV, and XLSX upload truth.
- DOCX documented as deferred unless a verified extractor lands during closure.
- Normalized `extract.md` and `extract.json` evidence.
- `extract_complete` source ingest into wiki pages.
- Dashboard source inbox and page-audit polish.
- Focused backend, frontend, and docs release gate.

Out of scope:
- Mixed-source memory scorecard.
- Retrieval ranking changes.
- Wiki proposal deduplication.
- Source-backed proposal text redesign.
- Broad refactors, settings encryption, and new source families.

## Phases

### Phase 1: Pending Source Loop Closure

**Addresses:** V12-CLOSE-01, V12-CLOSE-02
**Depends on:** -
**Success criteria:**
- Backend and frontend supported-format lists match.
- DOCX is not advertised as active without a verified extractor.
- XLSX uses the existing Pyodide extractor path where the sandbox runtime is available.
- TXT, Markdown, JSON, CSV, XLSX, and PDF either complete the source loop or report a clear durable failure.

### Phase 2: Frontend Polish Gate

**Addresses:** V12-CLOSE-03
**Depends on:** Phase 1
**Success criteria:**
- `npm --prefix web run i18n:check` passes.
- `npm --prefix web run e2e:pages` passes.
- `npm --prefix web run e2e` passes.
- `npm --prefix web run audit:frontend` passes or records an environmental blocker with an independent check.

### Phase 3: Planning Reconciliation

**Addresses:** V12-CLOSE-04
**Depends on:** Phases 1-2
**Success criteria:**
- `.planning/STATE.md`, `.planning/ROADMAP.md`, `.planning/PROJECT.md`, and `.planning/REQUIREMENTS.md` describe v1.2 closure accurately.
- `docs/implementation-tracker.md` records shipped behavior, verification, and known gaps.

### Phase 4: Release Gate Lite

**Addresses:** V12-CLOSE-05
**Depends on:** Phases 1-3
**Success criteria:**
- Focused Go tests pass.
- `go test ./...` and `go build ./...` pass.
- Frontend build and audit checks pass or have documented local blockers.
- `.planning/phases/02-v1-2-closure-polish/VALIDATION.md` records commands and results.
```

- [ ] **Step 3: Update `.planning/REQUIREMENTS.md`**

Add a new section after v1.1 requirements:

```markdown
## Milestone v1.2 Requirements

v1.2 closes the active universal-ingestion and frontend-polish thread. It does not reopen the older broad memory-quality plan.

- [ ] **V12-CLOSE-01 Source Loop Closure:** Active source formats complete upload, normalized extraction, ingest, and wiki source page creation, or have explicit known-gap status.
- [ ] **V12-CLOSE-02 Format Policy Truth:** Dashboard, Telegram, API, and frontend copy agree on supported upload formats; DOCX is deferred unless verified.
- [ ] **V12-CLOSE-03 Frontend Polish Verification:** Dashboard i18n, page sweep, source inbox, graph, conversation, and secondary-page audit checks remain green.
- [ ] **V12-CLOSE-04 Planning Reconciliation:** Active planning docs and implementation tracker describe shipped v1.2 behavior and deferrals.
- [ ] **V12-CLOSE-05 Release Gate Lite:** Focused Go tests, broad Go verification, frontend verification, and validation docs pass before closure.
```

Add v1.2 rows to the coverage table:

```markdown
| v1.2 Closure and Polish | 5 | 0 | 5 | 5 | 0 |
```

- [ ] **Step 4: Update `.planning/PROJECT.md`**

Add this section after completed v1.1:

```markdown
### Active v1.2 Closure and Polish

- [ ] Source upload support is truthful across Telegram, dashboard, API, and frontend copy.
- [ ] PDF, TXT, Markdown, JSON, CSV, and XLSX paths are verified against the source-to-wiki loop.
- [ ] DOCX remains deferred unless a real extractor is verified during closure.
- [ ] Recent frontend E2E and i18n audit fixes remain covered by repeatable commands.
- [ ] Planning docs and implementation tracker are reconciled with shipped behavior.
```

Add this decision to the key decisions table:

```markdown
| DOCX deferred from active v1.2 upload support until extractor exists | Avoids advertising a source format that cannot complete normalized extraction | Pending closure |
```

- [ ] **Step 5: Add tracker entry**

Prepend this entry to `docs/implementation-tracker.md`:

```markdown
2026-05-06 v1.2 Closure and Polish kickoff: active closure scope is universal ingestion plus recent frontend polish only. Closure must make upload format support truthful, wire XLSX through the existing Pyodide extraction path, keep DOCX deferred unless a verified extractor lands, prove `extract_complete` sources can become wiki pages, run frontend i18n/E2E audit commands, and reconcile `.planning/`. Deferred beyond this closure: mixed-source memory scorecard, retrieval/proposal upgrades, source-backed proposal redesign, and broad refactors.
```

- [ ] **Step 6: Check docs and commit**

Run:

```powershell
Select-String -Path '.planning\*.md','docs\implementation-tracker.md' -Pattern 'v1.2 Closure and Polish','V12-CLOSE'
git diff --check
```

Expected: `Select-String` returns the new v1.2 entries; `git diff --check` exits 0.

Commit:

```powershell
git add .planning/STATE.md .planning/ROADMAP.md .planning/PROJECT.md .planning/REQUIREMENTS.md docs/implementation-tracker.md
git commit -m "docs: reconcile v1.2 closure planning"
```

---

### Task 8: Run Release Gate Lite and Record Validation

**Files:**
- Create: `.planning/phases/02-v1-2-closure-polish/VALIDATION.md`
- Modify: `.planning/STATE.md`
- Modify: `.planning/PROJECT.md`
- Modify: `.planning/REQUIREMENTS.md`
- Modify: `docs/implementation-tracker.md`

- [ ] **Step 1: Run focused backend tests**

Run:

```powershell
go test ./internal/source ./internal/ingest ./internal/api ./internal/telegram -count=1
```

Expected: PASS.

- [ ] **Step 2: Run full Go verification**

Run:

```powershell
go test ./...
go build ./...
```

Expected: both commands PASS.

- [ ] **Step 3: Run frontend verification**

Run:

```powershell
npm --prefix web run i18n:check
npm --prefix web run e2e:pages
npm --prefix web run e2e
npm --prefix web run build
```

Expected: all commands PASS.

Run this if local ports and credentials allow the audit wrapper:

```powershell
npm --prefix web run audit:frontend
```

Expected: PASS. If it fails because the local server cannot bind or local credentials are unavailable, record the blocker and the passing component commands in validation.

- [ ] **Step 4: Run packaging check only if needed**

Check whether packaging or embedded assets changed:

```powershell
git diff --name-only HEAD~1..HEAD
```

If `.goreleaser.yml`, `runtime/pyodide`, `internal/api/dist`, or release scripts changed, run:

```powershell
go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean
```

Extract the Windows snapshot artifact and run:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\check-windows-gui-subsystem.ps1 <path-to-extracted-aura.exe>
```

Expected: `windows gui subsystem ok`.

- [ ] **Step 5: Write validation record**

Create `.planning/phases/02-v1-2-closure-polish/VALIDATION.md`:

```markdown
# v1.2 Closure and Polish Validation

Date: 2026-05-06

## Commands

- `go test ./internal/source ./internal/ingest ./internal/api ./internal/telegram -count=1`
- `go test ./...`
- `go build ./...`
- `npm --prefix web run i18n:check`
- `npm --prefix web run e2e:pages`
- `npm --prefix web run e2e`
- `npm --prefix web run build`
- `npm --prefix web run audit:frontend`

## Result

PASS for all commands listed above.

## Active Supported Upload Formats

- PDF
- TXT
- Markdown
- JSON
- CSV
- XLSX

## Known Gaps

- DOCX upload ingestion is deferred until Aura has a verified DOCX extractor.
- Images, audio, video, PPTX, email, websites, and cloud connectors remain outside v1.2.
- Mixed-source memory scorecard, retrieval ranking changes, and proposal dedupe remain outside this closure milestone.

## Notes

XLSX extraction uses the existing fixed Pyodide extractor and requires the sandbox runtime to be available. Text-like formats use Go-native extractors. PDF keeps the Mistral OCR path and writes normalized extraction evidence.
```

If `audit:frontend` is blocked by local environment, replace the result section with:

```markdown
## Result

PASS for focused backend, full Go, frontend i18n, all-page E2E, full E2E, and frontend build. `npm --prefix web run audit:frontend` was not completed because the local audit wrapper could not start the app server in this environment; the component commands it wraps passed independently.
```

- [ ] **Step 6: Mark v1.2 closure complete**

In `.planning/STATE.md`, set:

```markdown
Current milestone: v1.2 Closure and Polish
Current phase: Complete
Current focus: ready to choose the next milestone
Status: v1.2 Closure and Polish complete
Last activity: 2026-05-06 - Completed v1.2 closure release gate and planning reconciliation
```

In `.planning/REQUIREMENTS.md`, mark all five v1.2 requirements as `[x]`.

In `.planning/PROJECT.md`, change `Active v1.2 Closure and Polish` to `Completed v1.2 Closure and Polish` and mark its checklist items `[x]`, except the DOCX line should read:

```markdown
- [x] DOCX remains deferred until a real extractor is verified.
```

Prepend this closure entry to `docs/implementation-tracker.md`:

```markdown
2026-05-06 v1.2 Closure and Polish complete: upload format support is truthful across backend and frontend, DOCX is deferred until a verified extractor exists, XLSX routes through the fixed Pyodide extractor when the sandbox runtime is available, `extract_complete` sources can be ingested into wiki pages, and the recent dashboard frontend polish gate is recorded. Verification: focused source/ingest/API/Telegram tests, `go test ./...`, `go build ./...`, frontend i18n/page/E2E/build checks, and validation record in `.planning/phases/02-v1-2-closure-polish/VALIDATION.md`.
```

- [ ] **Step 7: Commit closure**

Run:

```powershell
git add .planning/STATE.md .planning/REQUIREMENTS.md .planning/PROJECT.md .planning/phases/02-v1-2-closure-polish/VALIDATION.md docs/implementation-tracker.md
git commit -m "docs: close v1.2 closure polish"
```

---

## Self-Review

Spec coverage:

- V12-CLOSE-01 is covered by Tasks 2, 3, 4, 5, and 8.
- V12-CLOSE-02 is covered by Tasks 1, 3, 4, 7, and 8.
- V12-CLOSE-03 is covered by Task 6 and Task 8.
- V12-CLOSE-04 is covered by Task 7 and Task 8.
- V12-CLOSE-05 is covered by Task 8.

Placeholder scan:

- No placeholder markers or empty steps.
- Each task names exact files, commands, expected outcomes, and commit scope.
- Conditional branches name concrete actions and validation wording.

Type consistency:

- The extraction runner interface is `source.PyodideRunner`.
- The API dependency field is `Extractor source.PyodideRunner`.
- The Telegram document handler fields are `Extractor source.PyodideRunner` in config and `extractor source.PyodideRunner` in the handler.
- Active supported upload formats are PDF, TXT, Markdown, JSON, CSV, and XLSX.
- DOCX remains a `source.Kind` for generated artifacts and future support, but it is not an active upload format in v1.2 closure.
