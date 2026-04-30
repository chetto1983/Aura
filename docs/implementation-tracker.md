# Aura Implementation Tracker

Track work against `pdr.md` v4.0-next (Standalone Second Brain + PDF Ingestion).

## Slice Order (from PDR §12)

1. **Config**: Mistral OCR keys, model, base URL, limits, feature flag.
2. **Source store** (`internal/source`): source ID, raw file storage, `source.json` read/write, listing.
3. **OCR client** (`internal/ocr`): Mistral `/v1/ocr` client + fake-server tests.
4. **Telegram PDF handler** (`internal/telegram/documents.go`): MIME/size validation, download, store, OCR trigger.
5. **Source tools**: `store_source`, `ocr_source`, `read_source`, `list_sources`, `lint_sources`.
6. **Ingestion** (`internal/ingest`): `ingest_source` pipeline, source summary page, affected-page reindex.
7. **Wiki maintenance**: `append_log`, `rebuild_index`, `list_wiki`, `lint_wiki`.
8. **Reminder/scheduler tools**: SQLite `scheduled_tasks`, `schedule_task`, `list_tasks`, `cancel_task`.
9. **Natural prompt tests**: extend `cmd/debug_tools` or add `cmd/debug_ingest`.
10. **UI**: source inbox, PDF status, wiki graph and health dashboard.

Slices 1–7 must land before any UI work. Slice 8 (reminders) is independent and can land in parallel after slice 1.

## Current State (2026-04-29)

Working tree before this session:

- Embedding config moved to Mistral defaults (`EMBEDDING_BASE_URL=https://api.mistral.ai/v1`, `EMBEDDING_MODEL=mistral-embed`) — `internal/config/config.go`, `internal/config/config_test.go`, `.env.example` modified, not yet committed.
- `cmd/debug_tools/main.go` added (untracked) — natural prompt smoke harness for `write_wiki` / `read_wiki` / `search_wiki` and optional live web tools via `--live-web`.
- New product docs: `docs/picobot-tools-audit.md`, `docs/second-brain-consolidation-strategy.md`, `pdr.md`.
- Branch: `ralph/US-010-observability`.

Existing packages: `budget`, `config`, `conversation`, `health`, `llm`, `logging`, `orchestrator`, `search`, `skill`, `telegram`, `tools`, `tracing`, `wiki`. No `source`, `ocr`, `ingest` yet.

## Slice Status

| # | Slice | Status | Notes |
| - | ----- | ------ | ----- |
| 1 | Config (Mistral OCR) | done | Mistral OCR fields + defaults + tests. |
| 2 | Source store | done | `internal/source` with sha256 dedup, atomic source.json, per-id mutex, kind/status filter. |
| 3 | OCR client | done | `internal/ocr` Mistral client with wire-verified table_format/extract_header/extract_footer; render to PDR §4 ocr.md. |
| 4 | Telegram PDF handler | done | `internal/telegram/documents.go` non-blocking single-message progress, bounded concurrency=2, AfterOCR hook for slice 6. |
| 5 | Source tools | pending | |
| 6 | Ingestion | pending | |
| 7 | Wiki maintenance | pending | |
| 8 | Reminder/scheduler | pending | |
| 9 | Natural prompt tests for OCR/ingest | pending | |
| 10 | UI | pending | |

## Session Log

### 2026-04-30 — Multipage debug for `src_437ecedcb716dbbf`

- Symptom: 2-page Italian PMS PDF produced an `ocr.md` where `## Page 2` body is just `.`.
- Investigation:
  - `pdftotext -f 2 -l 2 wiki/raw/src_437ecedcb716dbbf/original.pdf` → empty output.
  - `pypdf` page 2: `extract_text() == ''`, no `/XObject`, no `/Resources`. Fully blank page in the source PDF.
  - `ocr.json` page 2: `markdown: "."`, empty `images`, `tables`, `hyperlinks`, header/footer null. Mistral correctly reported a near-empty page.
- Cause: not a flag interaction, not a Mistral bug — the source PDF really has a blank page 2. The flag re-test (`EXTRACT_HEADER=false EXTRACT_FOOTER=false INCLUDE_IMAGES=false`) would have shown the same `.` because those flags only affect header/footer/image extraction, never page-body text.
- No code change in this session; finding is for the renderer backlog.
- **Renderer follow-up (deferred, not slice 5):** detect "near-empty" pages (`strings.TrimSpace(body)` matches `.` or is empty) and render `## Page N (blank)` with no body, instead of literal `.`. This is a `internal/ocr/render.go` change only; leaves `ocr.json` untouched.
- **Re-render recipe (cheap, no new OCR calls):** since `ocr.json` is the raw Mistral response and `ocr.md` is a pure derivation, any renderer fix can be replayed against existing sources without API cost:

  ```go
  // pseudocode for a future cmd/rerender_ocr or similar
  for each dir in wiki/raw/*/:
      raw := read("ocr.json") // unmarshal into ocr.OCRResponse
      meta := ocr.RenderMeta{SourceID: id, Filename: source.Filename, Model: source.OCRModel}
      md   := ocr.RenderMarkdown(meta, raw)
      atomicWrite(dir/"ocr.md", md)
  ```

  Constraints: must reuse `internal/source.Store.Path` for containment, must atomic-rename, must skip dirs missing `ocr.json` (status=stored or failed). Add a `--dry-run` diff mode.

### 2026-04-29 — Slice 4 complete

- Slice 4 (Telegram PDF handler) done:
  - `internal/telegram/documents.go`: `docHandler` with bounded semaphore (`docConcurrencyLimit=2` simultaneous OCR jobs), single-message progress UX (initial reply → edits in place at each pipeline step → final ✅/❌), `AfterOCRHook` extension point for slice 6, validate-then-async pattern (handler returns within ~100ms; goroutine does the heavy lifting). `progressEditor` falls back to a fresh send if Edit fails (e.g. message deleted). Picobot/wiki conventions reused: per-key mutex (sync), atomic file writes via existing `source.Store.Path` containment.
  - `internal/telegram/bot.go`: `Bot` gained `sources`, `ocr`, `docs` fields. `New()` always builds a `source.Store` from `WIKI_PATH`; OCR client only when `OCR_ENABLED && MISTRAL_API_KEY != ""`. `registerHandlers` adds `tele.OnDocument` → `docs.onDocument` (gated on docs != nil so failures in source/OCR setup never break text handling).
  - `internal/telegram/documents_test.go`: 12 unit tests on pure functions — `validatePDF` (PDF/non-PDF/oversize/no-cap/nil/charset-suffixed mime), `safeName` (trim, empty, control chars, path chars, truncation), `formatSize` (B/KB/MB/GB rounding), `formatDuration` (ms / fractional s / s / m s), `pluralS`. Live Telegram round-trip is out of scope (needs actual Telegram session); the goroutine pipeline is exercised end-to-end already by the slice 3 follow-up `TestLiveE2E`.
- UX choices (single-message progress, bounded concurrency=2, dup-aware reply, error-as-final-edit) match the slice 4 design discussed before implementation.
- Verification: `go test ./...` PASS (incl. `internal/telegram` 12 new tests), `go build ./...` clean, `go vet ./...` clean.
- Files touched: `internal/telegram/documents.go` (new), `internal/telegram/documents_test.go` (new), `internal/telegram/bot.go` (modified — imports, struct, New, registerHandlers), `docs/implementation-tracker.md`.
- Pre-existing diagnostics in `bot.go` (unused `userID` param, `WriteString(fmt.Sprintf(...))` style hints in `onStatus`) are out of slice 4 scope; left for a future cleanup commit.
- **Live-tested end-to-end via the actual Telegram bot** (`go run ./cmd/aura`, real PDFs uploaded by chat):
  - 1-page Italian receipt (RICEVUTA, 19 KB) — OCR 1.4s, 4-file layout written.
  - 1-page Italian DDT delivery note (55 KB) — OCR 2.3s, 4-file layout written.
  - 2-page Italian PMS test scenario (3 KB) — OCR 0.8s, ocr.md correctly emits `## Page 1` and `## Page 2` headings.
  - Each upload produced `original.pdf`, `source.json` (status=ocr_complete, ocr_model=mistral-ocr-latest, page_count, sha256), `ocr.md` (PDR §4 layout), `ocr.json` (raw Mistral response) under `wiki/raw/<source_id>/`. Filename sanitization preserved spaces in display while sha256 dedup keyed off content. Single-message progress UX confirmed.
- Next slice: **5 — LLM-facing source tools (`store_source`, `ocr_source`, `read_source`, `list_sources`, `lint_sources`)**. Lets the LLM drive the same pipeline (re-OCR a stored source, list inbox, surface unprocessed sources) and read source content into context for slice 6 ingest.

### 2026-04-29 — Slice 3 complete

- Slice 3 (OCR client) done:
  - `internal/ocr/types.go`: `OCRRequest` (wire body — verified against [Mistral basic_ocr docs](https://docs.mistral.ai/capabilities/document_ai/basic_ocr/) — includes `table_format`, `extract_header`, `extract_footer`, `include_image_base64`), `Document`, `OCRResponse`, `Page` (with header/footer), `Usage`.
  - `internal/ocr/client.go`: `Client` + `Config`. Bearer auth, JSON post, base64 PDF in `data:application/pdf;base64,...` URL, capped 256-char error snippets, 256 MiB response cap. HTTP shape mirrors `internal/tools/ollama_web.go`.
  - `internal/ocr/render.go`: `RenderMarkdown` produces PDR §4 ocr.md layout (`# Source OCR: <filename>`, `Source ID:`, `Model:`, then `## Page N`). Index+1 → 1-based display; defensive fallback when all pages report index=0.
  - Tests: 13 across `client_test.go` (success path verifies model/base64/auth header; include_images flag; extraction flags sent on wire; flags omitted when zero-valued; HTTP 401 doesn't leak API key; HTTP 500 snippet capped; bad JSON; empty bytes; missing base URL; trailing slash; default model) and `render_test.go` (PDR layout, model override, empty pages kept, all-zero-index fallback, missing filename placeholder).
- Wire-format correction: discovered late that `table_format`, `extract_header`, `extract_footer` are wire-level Mistral params (not Aura render hints as I initially assumed). Added them to `OCRRequest` and `Config`, plumbed from constructor to body, with tests asserting both presence-when-set and omission-when-zero (so `omitempty` correctly hides them from the JSON when defaulted).
- Verification: `go test ./...` PASS, `go build ./...` clean, `go vet ./...` clean.
- Files touched: `internal/ocr/types.go`, `internal/ocr/client.go`, `internal/ocr/render.go`, `internal/ocr/client_test.go`, `internal/ocr/render_test.go`, `docs/implementation-tracker.md`.
- Next slice: **4 — Telegram PDF handler (`internal/telegram/documents.go`)**. Allowlist-gated PDF upload from Telegram, MIME/size validation against `OCR_MAX_FILE_MB`, download to `wiki/raw/<source_id>/`, `source.Store.Put`, then call `ocr.Client.Process` if `OCR_ENABLED`, write `ocr.md` + `ocr.json` via `source.Store.Path`, flip status to `ocr_complete`. No raw PDF text or base64 in logs (PDR §9).

### 2026-04-29 — Slice 2 complete

- Slice 2 (source store) done:
  - `internal/source/source.go`: `Kind` (pdf/text/url), `Status` (stored/ocr_complete/ingested/failed), `Source` struct matching PDR §4 schema.
  - `internal/source/store.go`: `Store` rooted at `<wiki>/raw/`. `Put` (sha256 dedup + atomic write), `Get`, `List` (kind/status filter, sorted desc), `Update` (mutator pattern), `Path` (containment-checked join), `RawDir`. Per-id mutex via `sync.Map`. Atomic temp+rename copied from `internal/wiki/store.go`. Regex ID validation pattern adapted from picobot's `isValidMemoryFile`.
  - `internal/source/store_test.go`: 10 test funcs — create, dedup, not-exist, invalid IDs (incl. traversal), list filters + bogus entries skipped, update persistence, mutator-error propagation, validation, path traversal rejection, all 3 kinds.
- Source ID format: `src_<first 16 hex of sha256>` — stable, dedupable, filesystem-safe. External IDs validated against `^src_[a-f0-9]{16}$` before any path join.
- Verification: `go test ./...` PASS (incl. `internal/source` 10 tests), `go build ./...` clean, `go vet ./...` clean.
- Files touched: `internal/source/source.go` (new), `internal/source/store.go` (new), `internal/source/store_test.go` (new), `docs/implementation-tracker.md`.
- Next slice: **3 — OCR client (`internal/ocr`)**. Mistral `/v1/ocr` request/response, base64 PDF path, fake-server tests. Integrates with `source.Store.Update` to flip status to `ocr_complete` and write `ocr.md` / `ocr.json` via `source.Store.Path`.

### 2026-04-29 — Slice 1 complete

- Created this tracker per `aura-implementation` skill First Actions.
- Slice 1 (config) done:
  - `internal/config/config.go`: added `MistralAPIKey`, `MistralOCRModel`, `MistralOCRBaseURL`, `MistralOCRTableFormat`, `MistralOCRIncludeImages`, `MistralOCRExtractHeader`, `MistralOCRExtractFooter`, `OCREnabled`, `OCRMaxPages`, `OCRMaxFileMB` with PDR §3 defaults. Keys deliberately separate from `LLM_API_KEY` and `EMBEDDING_API_KEY`.
  - `internal/config/config_test.go`: extended `TestLoadSuccess` to assert OCR defaults and unset OCR env vars.
  - `.env.example`: documented OCR section.
- Verification: `go test ./...` (all packages PASS), `go build ./...` (clean), `go vet ./...` (clean).
- Files touched: `internal/config/config.go`, `internal/config/config_test.go`, `.env.example`, `docs/implementation-tracker.md`.
- Next slice: **2 — Source store (`internal/source`)**. Needs source ID generation (sha256 + ULID), `wiki/raw/<source_id>/` layout, atomic `source.json` write, listing, and tests for dedupe by sha256.
- Pre-existing diagnostic noted (not introduced this slice): `internal/config/config.go:52` — `IsAllowlisted` loop could use `slices.Contains`. Out of scope.
