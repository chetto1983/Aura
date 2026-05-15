# Wave 2.9 — Markitdown Integration (PLAN)

> **Status: SHIPPED 2026-05-13 in commits `b8991a64 b7ad1aa2 23a52edc`. Plan preserved as evidence; implementation is in the codebase.**

**Status:** plan (not yet implemented)
**Date drafted:** 2026-05-13
**Predecessor:** Wave 2.7f (source action-enum consolidation done)
**Successor:** Wave 2.9.5 (OCR backend abstraction + GLM-OCR sidecar)

---

## 1. Goal

Replace Aura's hand-rolled extractor zoo (`ExtractGo` for text/md/json/csv + Python sandbox for xlsx/docx) with **Microsoft's [markitdown](https://github.com/microsoft/markitdown)** as a sidecar service, and use the same sidecar to add **6 new ingestible formats** (PPTX, EPUB, HTML, ZIP, URL, image-as-text).

**Why markitdown:**
- 22 modular converters maintained upstream (we stop owning extraction code for office formats).
- Pure-Python, no GPU, no LLM required for the formats we care about in 2.9 (image + pptx slide-image OCR is gated to 2.9.5).
- Native MCP server `markitdown-mcp` — fits Aura's existing MCP transport, zero new wire protocol.
- Apache-2.0, actively maintained, used by Microsoft Copilot ingest.

**Why now:** Wave 2.7f shipped the unified `source` tool with action enum `{list, read, store, reprocess, delete, lint}`. The `store` action accepts arbitrary bytes; only `reprocess` needs to know *how* to extract. Today `reprocess` dispatches across `ExtractGo` (4 formats) and `SandboxExtractor` (2 formats). Markitdown collapses both behind one HTTP call and unlocks 6 more formats with zero new dispatch code on Aura's side.

---

## 2. Scope split — 2.9 vs 2.9.5

Wave 2.9 (this plan) ships **everything except image OCR and image-in-image**. Wave 2.9.5 layers OCR on top.

| Format | Wave 2.9 | Wave 2.9.5 | Notes |
|--------|----------|------------|-------|
| `.txt`, `.md`, `.csv`, `.json` | ✅ via markitdown | — | replaces `ExtractGo` |
| `.xlsx` | ✅ via markitdown | — | replaces sandbox Python |
| `.docx` | ✅ via markitdown | — | replaces sandbox Python |
| `.pptx` (text + tables) | ✅ via markitdown | — | new format |
| `.epub` | ✅ via markitdown | — | new format |
| `.html` | ✅ via markitdown | — | new format |
| `.zip` (recurse) | ✅ via markitdown | — | new format |
| URL fetch → markdown | ✅ via markitdown | — | new format, SSRF-gated |
| `.pdf` (digital text) | ✅ via markitdown (pdfminer) | — | fast path, no OCR |
| `.pdf` (scanned/image) | ↪ fallback to Mistral OCR (today) | ↪ fallback to `OCRBackend` (Mistral OR GLM-OCR) | dispatch unchanged in 2.9 |
| `.png`, `.jpg`, `.webp` (text extraction) | ❌ | ✅ via `OCRBackend` | gated to 2.9.5 |
| `.pptx` with slide images | ⚠️ text only (slide images skipped) | ✅ slide images OCR'd | upgrade in 2.9.5 |
| `.wav`, `.mp3` | ❌ | ❌ | out of scope; would need Whisper |

**Cut line rationale:** markitdown's image + audio converters depend on an LLM (image caption via `client.chat.completions.create`) or Whisper. Without `OCRBackend` selectable (Wave 2.9.5), image support today would force the choice "use Mistral and lose self-host" or "ship nothing." Splitting the wave keeps 2.9 a clean infrastructure change with no LLM dependency and lets 2.9.5 land OCR independently with a marketing-friendly tier story.

---

## 3. Architecture

### 3.1 Sidecar service

New Compose service `aura-markitdown` running the upstream image:

```yaml
aura-markitdown:
  image: mcr.microsoft.com/markitdown:${MARKITDOWN_TAG:-latest}
  # fallback if upstream image not available: build from Dockerfile.markitdown
  restart: unless-stopped
  command: ["markitdown-mcp", "--http", "--host", "0.0.0.0", "--port", "3001"]
  ports:
    - "127.0.0.1:${MARKITDOWN_HOST_PORT:-3001}:3001"
  environment:
    # No LLM credentials in 2.9 — image converter disabled at runtime.
    MARKITDOWN_DISABLE_IMAGE_CONVERTER: "true"
  read_only: true
  tmpfs:
    - /tmp:size=256m
  cap_drop: [ALL]
  security_opt:
    - no-new-privileges:true
```

**Transport choice:** Streamable HTTP (`markitdown-mcp --http`), not stdio. Reasons:
1. Aura's MCP client already supports both — HTTP is operationally simpler (no process supervision).
2. Container restart isolation: if markitdown OOMs on a malformed file, only the sidecar restarts.
3. Sidecar can be CPU-pinned independently (`cpu_count: 2`) without throttling Aura.

**SSRF gate at the Aura boundary:** markitdown's URL converter would otherwise let the LLM scrape `http://169.254.169.254/`. We never expose the URL converter directly. Instead Aura's `web_fetch` (already SSRF-gated, see [internal/tools/web_common.go](internal/tools/web_common.go)) fetches the bytes, *then* hands them to markitdown by content-type. The sidecar never opens its own socket.

### 3.2 Go-side client

New package `internal/markitdown/`:

```
internal/markitdown/
  client.go        # HTTP client, Convert(ctx, bytes, mime) → ConvertResult
  client_test.go
  types.go         # ConvertResult { Markdown, Metadata, Warnings }
```

Single interface surface used everywhere extraction happens:

```go
type Converter interface {
    Convert(ctx context.Context, in ConvertInput) (ConvertResult, error)
}

type ConvertInput struct {
    Bytes    []byte
    MimeType string // authoritative; markitdown dispatches on this
    Filename string // hint only, for converter heuristics
}

type ConvertResult struct {
    Markdown string
    Metadata source.ExtractionMeta
    Warnings []string
}
```

The client is the sole thing that knows the sidecar URL. Construction reads from `internal/settings` first, env fallback (`MARKITDOWN_URL`), default `http://aura-markitdown:3001` in compose / `http://127.0.0.1:3001` for `go run`.

### 3.3 Source pipeline rewiring

[internal/source/extract_auto.go](internal/source/extract_auto.go) currently dispatches:

```go
switch in.Source.Kind {
case KindText, KindMarkdown, KindJSON, KindCSV:
    return ExtractGo(ctx, in)
case KindXLSX, KindDOCX:
    return runner.ExtractXLSX/DOCX(...)
}
```

Becomes one line:

```go
return mk.Convert(ctx, markitdown.ConvertInput{
    Bytes:    in.Bytes,
    MimeType: in.Source.MimeType,
    Filename: in.Source.Filename,
})
```

`ExtractGo` and `SandboxExtractor.ExtractXLSX/DOCX` are **deleted in this wave** along with their tests, fixtures, and call sites. No build tag, no fallback path. Markitdown becomes the only extractor; if the sidecar is unreachable, ingest fails loudly (status `failed`, dashboard surfaces the error) rather than silently degrading to a legacy path that diverges in output format.

### 3.4 New `source.Kind` constants

[internal/source/source.go:25-49](internal/source/source.go#L25-L49) gains:

```go
KindPPTX  Kind = "pptx"
KindEPUB  Kind = "epub"
KindHTML  Kind = "html"
KindZIP   Kind = "zip"
KindImage Kind = "image"   // reserved for 2.9.5; rejected at upload in 2.9
```

[internal/source/formats.go](internal/source/formats.go) gains corresponding rows in `formatsByExt`. `KindImage` is registered but `DetectUploadFormat` returns "supported in 2.9.5" until the OCR backend lands.

### 3.5 PDF dispatch (unchanged in 2.9, prepped for 2.9.5)

PDF stays on Mistral DAI via the existing [internal/ocr/client.go](internal/ocr/client.go) path. Markitdown's pdfminer-based converter is **not** used for PDF in 2.9 because:
- pdfminer fails silently on scanned PDFs (returns blank), and we have no signal to detect "blank == scanned" without a heuristic.
- The current Mistral path is battle-tested.

What 2.9 *does* prepare: extract the OCR dispatch into a `source.OCRDispatch` interface so Wave 2.9.5 can swap implementations without touching the ingest pipeline. See §6.

### 3.6 URL ingestion

New source kind path:

1. LLM calls `source` with `action=store, kind=url, url=https://...`.
2. Aura's `web_fetch` retrieves the bytes (SSRF-gated, size-capped at `WEB_FETCH_MAX_BYTES`).
3. Aura sniffs MIME type (`net/http.DetectContentType`).
4. Bytes are stored as a normal source under `wiki/raw/src_<sha16>/` with `original.<ext>` derived from MIME.
5. Markitdown converts to markdown using whichever converter matches the MIME.
6. URL is preserved in `Source.SourceURL` (new field on `Source` struct) for provenance and to enable later "refresh from URL" flows.

The URL converter inside markitdown is **never** exposed directly — we always go through `web_fetch` first.

### 3.7 ZIP ingestion

Markitdown's ZIP converter recurses, converts each member, and concatenates with separators. Aura wraps this with a safety cap:

- Max members: 50 (configurable `MARKITDOWN_ZIP_MAX_MEMBERS`).
- Max total uncompressed bytes: 50 MiB.
- Reject ZIP bombs at the Go side before handing bytes to the sidecar (check uncompressed-to-compressed ratio < 1000).

---

## 4. Backend interface — OCRBackend (designed in 2.9, used in 2.9.5)

To unblock 2.9.5 without re-plumbing in 2.9, ship the interface skeleton now:

```go
// internal/ocr/backend.go (new in 2.9)
type Backend interface {
    Name() string
    Process(ctx context.Context, in BackendInput) (*BackendResult, error)
}

type BackendInput struct {
    PDFBytes   []byte // present for whole-doc OCR
    ImageBytes []byte // present for image-in-image
    MimeType   string
}

type BackendResult struct {
    Markdown string
    PageCount int
    RawJSON  []byte
}
```

In 2.9 the only implementation is `MistralBackend` wrapping the current `Client`. In 2.9.5 we add `LlamaCppGLMOCRBackend` posting to `http://aura-llama-glmocr:8080/v1/chat/completions` with `prompt="OCR:"` and `image_url` content blocks. Selection happens in [internal/settings](internal/settings) via key `OCR_BACKEND` ∈ `{mistral, glmocr}`, dashboard-editable.

**No env-var hardcoding** for the backend choice — the user's existing "configurabile da frontend" requirement is met by routing through `settings.Store`.

---

## 5. Tool surface (no new tools)

The unified `source` action-enum tool from Wave 2.7f already covers the surface:

- `source action=store kind=pptx bytes=<base64>` — works automatically once markitdown is wired.
- `source action=store kind=url url=https://...` — needs URL kind plumbing (§3.6).
- `source action=reprocess id=src_... stages=[ingest]` — calls markitdown for non-PDF kinds, OCR for PDF.

Zero new tool registrations. The LLM's mental model doesn't change.

---

## 6. Settings rows (dashboard-editable)

New rows in [internal/settings/defaults.go](internal/settings/defaults.go):

| Key | Default | Purpose |
|-----|---------|---------|
| `MARKITDOWN_URL` | `http://aura-markitdown:3001` | sidecar address |
| `MARKITDOWN_TIMEOUT_SEC` | `120` | per-conversion timeout |
| `MARKITDOWN_ZIP_MAX_MEMBERS` | `50` | safety cap |
| `MARKITDOWN_ZIP_MAX_BYTES` | `52428800` | 50 MiB uncompressed |
| `OCR_BACKEND` | `mistral` | reserved for 2.9.5 (`mistral` or `glmocr`) |

These appear in the dashboard Settings tab automatically (existing `defaults.go` registration writes them).

---

## 7. File-by-file change list

### New files
- `internal/markitdown/client.go` — HTTP client to sidecar
- `internal/markitdown/client_test.go` — fake server, error envelopes
- `internal/markitdown/types.go` — ConvertInput/Result
- `internal/ocr/backend.go` — Backend interface (used in 2.9 only as MistralBackend wrapper)
- `internal/ocr/backend_mistral.go` — wraps existing Client
- `Dockerfile.markitdown` — fallback build if upstream image not published

### Modified files
- `internal/source/source.go` — add KindPPTX, KindEPUB, KindHTML, KindZIP, KindImage, add `SourceURL` field
- `internal/source/formats.go` — add ext→Kind rows; reject `KindImage` until 2.9.5
- `internal/source/extract_auto.go` — replace switch with single markitdown call
- `internal/telegram/setup.go` — construct markitdown client, inject into pipeline
- `internal/settings/defaults.go` — register new settings rows
- `compose.yaml` — add `aura-markitdown` service
- `cmd/probe_chat/main.go` — add probe cases per new format
- `docs/llm-wiki.md` — note that ingest pipeline now goes through markitdown

### Files deleted in 2.9

- `internal/source/extract_go.go` + tests
- `internal/source/extract_pdf.go` only if its callers moved to OCRBackend; otherwise kept (PDF path stays Mistral OCR in 2.9)
- Python sandbox xlsx/docx extractor entry points (script + Go wrappers in `internal/sandbox/`)
- Any `cmd/debug_*` harness that targeted the deleted extractors
- Tests fixtures that exercised the deleted paths only

---

## 8. Verification — disk-byte rule, no shortcuts

Per the CLAUDE.md "TESTS VERIFY QUALITY AND METRICS, NOT JUST 'DID IT RUN'" rule, each new format gets a `cmd/probe_chat` case that:

1. Uploads a real artifact (not a synthetic stub) via `source action=store`.
2. Waits for ingest completion via API polling.
3. **Reads the resulting `extract.md` from disk** under `wiki/raw/src_*/extract.md`.
4. Asserts substantive content keywords AND structural elements (e.g., for xlsx: assert sheet name appears as `## SheetName` markdown heading).
5. Asserts operational metrics: wall-clock < 30s for small files, < 120s for large.
6. PASS/FAIL with the actual content diff on mismatch.

Probe cases:

| Probe | Artifact | Ground truth |
|-------|----------|--------------|
| `markitdown-xlsx-roundtrip` | `testdata/sample.xlsx` (2 sheets, 3 cols, 5 rows each) | each sheet → `## Sheet1`/`## Sheet2`, cell values present, no `<table>` tags leaked |
| `markitdown-docx-roundtrip` | `testdata/sample.docx` (H1 + H2 + bullet list + table) | `# Title` / `## Subtitle` / `- bullet` / `\| col1 \| col2 \|` |
| `markitdown-pptx-roundtrip` | `testdata/sample.pptx` (3 slides, 1 with table) | three `## Slide N` blocks, table markdown |
| `markitdown-epub-roundtrip` | `testdata/sample.epub` (Project Gutenberg public domain) | chapter headings + paragraph text |
| `markitdown-html-roundtrip` | inline HTML string | tags stripped, links preserved as `[text](url)` |
| `markitdown-zip-recursion` | `testdata/two-docs.zip` with sample.docx + sample.csv | both members converted, separator line present |
| `markitdown-url-fetch` | small static page on `searxng` service (already in network) | SSRF gate blocks 169.254.169.254, valid URL succeeds |
| `markitdown-zip-bomb-reject` | crafted ZIP with 1KB → 1GB ratio | error contains "ratio too high", source marked failed |
| `markitdown-legacy-csv-parity` | existing `testdata/sample.csv` | output byte-identical to ExtractGo's output (parity gate) |

The parity gate (last row) is the critical migration check: if markitdown's CSV output diverges meaningfully from `ExtractGo`'s, we lose existing wiki search recall on already-ingested files until reindex. The probe must run both extractors and compare normalized markdown; small whitespace diffs OK, semantic diffs blocked.

---

## 9. Migration & rollback

**Migration:** no data migration needed. Existing `extract.md` files on disk stay valid. Reprocessing an existing source through markitdown re-writes `extract.md` and bumps `Source.Status`.

**Rollback:** flip `MARKITDOWN_URL` to empty in settings → extract pipeline falls back to legacy path (`ExtractGo` / sandbox). The legacy switch in `extract_auto.go` is preserved behind the `legacy_extractors` build tag for one release.

**Hard rollback:** `docker compose down aura-markitdown` + build Aura with `-tags legacy_extractors`. Takes <2 min.

---

## 10. Risks & open questions

### Risks
| Risk | Likelihood | Mitigation |
|------|------------|------------|
| markitdown sidecar OOMs on huge xlsx | medium | 256MB tmpfs, container memory cap at 1 GiB, timeout 120s |
| pdfminer extracts blank on scanned PDF and we miss the signal | high | 2.9 doesn't use markitdown for PDF — Mistral DAI stays primary |
| ZIP bomb | medium | ratio + member-count + uncompressed-byte caps enforced in Go before forwarding |
| URL converter exposes SSRF | high | URL converter never called directly; `web_fetch` is the only entry point |
| markitdown output drift breaks wiki search recall | medium | parity probe `markitdown-legacy-csv-parity`; reindex job after migration |
| Upstream image not published | low | `Dockerfile.markitdown` fallback (5-line: `FROM python:3.12-slim; pip install markitdown-mcp[all]; CMD ...`) |

### Open questions to close before kicking off implementation
1. **Reindex strategy after migration.** Should re-ingesting through markitdown auto-trigger a wiki rebuild? **Reco:** no — manual `POST /api/maintenance/reindex` after the first 24h of dual-path validation.
2. **CPU pinning the sidecar.** Mini-PC CPU budget rule (max 4 threads). **Reco:** `cpus: 2.0` on the markitdown service, leave Aura on the rest.
3. **PPTX with embedded images today.** Markitdown emits placeholder text. Accept this and let 2.9.5 fill the gap? **Reco:** yes — flag known limitation in the dashboard tooltip.

---

## 11. Done criteria

Wave 2.9 ships when **all** of:

1. `aura-markitdown` service runs healthy under `docker compose up`.
2. All 9 probe cases (§8) PASS with disk-byte verification.
3. New formats (PPTX, EPUB, HTML, ZIP, URL) appear in `SupportedUploadAccept()` and the dashboard upload UI.
4. Legacy `ExtractGo` + sandbox extractors gated behind `//go:build legacy_extractors`; default build doesn't reference them.
5. Settings rows visible and editable from dashboard.
6. `internal/ocr/backend.go` interface compiles with `MistralBackend` as sole implementation (skeleton for 2.9.5).
7. `docs/wave-2.9-markitdown.md` (this file) updated with "Status: shipped" + commit hash.

Out of scope (deferred to 2.9.5): image OCR, OCR backend selector, GLM-OCR sidecar, image caption inside markitdown.

---

## 12. Estimated effort

- Sidecar wiring (compose + client + smoke): **0.5 day**
- Pipeline rewire + Kind additions: **0.5 day**
- New format probes + testdata authoring: **1 day**
- SSRF/ZIP-bomb hardening + parity gate: **0.5 day**
- OCRBackend skeleton: **0.5 day**
- Dashboard settings rows + tooltips: **0.5 day**

Total: **~3.5 days** of focused work. Can ship in one sitting if testdata authoring goes smoothly.
