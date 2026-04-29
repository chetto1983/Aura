# Aura Next PDR: Standalone Second Brain + PDF Ingestion

**Version:** 4.0-next  
**Date:** 2026-04-29  
**Status:** Ready for implementation planning  
**Primary goal:** Make Aura a standalone second brain that can ingest user PDFs, convert them into source-grounded markdown with Mistral OCR, and compile them into the maintained wiki.

## 1. Decision

Aura should prioritize the next work in this order:

1. Source store and PDF ingestion.
2. Mistral OCR client for user-provided PDFs.
3. `ingest_source` tool that turns OCR output into wiki updates.
4. SQLite-backed reminders and scheduled maintenance.
5. Wiki maintenance tools: `list_wiki`, `append_log`, `rebuild_index`, `lint_wiki`.
6. Review queue and standalone UI.

This keeps the product aligned with `docs/llm-wiki.md`: raw sources are immutable, the wiki is maintained by the LLM, and Aura replaces Obsidian with first-party tools and screens.

## 2. Product Scope

Aura must support PDFs inserted by the user through:

- Telegram document upload.
- Local import folder.
- Future web UI upload.

Aura must not treat PDFs as transient chat attachments. A PDF becomes a durable source with a stable source ID, metadata, extracted markdown, and links to generated wiki pages.

## 3. Mistral OCR Requirement

Use Mistral Document AI OCR for PDFs.

Recommended config:

```env
MISTRAL_API_KEY=
MISTRAL_OCR_MODEL=mistral-ocr-latest
MISTRAL_OCR_BASE_URL=https://api.mistral.ai/v1
MISTRAL_OCR_TABLE_FORMAT=markdown
MISTRAL_OCR_INCLUDE_IMAGES=false
MISTRAL_OCR_EXTRACT_HEADER=false
MISTRAL_OCR_EXTRACT_FOOTER=false
OCR_MAX_PAGES=500
OCR_MAX_FILE_MB=100
```

Notes from Mistral docs:

- OCR 3 is Mistral's Document AI OCR service and uses the `/v1/ocr` capability.
- The OCR 3 model card identifies the underlying model as `mistral-ocr-2512+1`; the public OCR processor examples use `mistral-ocr-latest`.
- The OCR processor extracts markdown while preserving document structure, including headers, paragraphs, lists, tables, hyperlinks, and complex layouts.
- OCR output contains `pages`, each with `index`, `markdown`, images, tables, hyperlinks, optional header/footer, and dimensions.
- Mistral supports PDF OCR by public URL, base64-encoded PDF, or uploaded PDF.
- Current model-card pricing is $2 / 1000 OCR pages and $3 / 1000 annotated pages.

Implementation choice:

- MVP uses base64 PDF upload to `/v1/ocr` for Telegram and local files.
- Large-file path adds Mistral uploaded-file flow or signed temporary URL later.
- Use normal OCR first. Structured annotations are v2, only after raw OCR ingestion is stable.

## 4. Storage Model

Keep raw source files out of normal git tracking. `.gitignore` already excludes `wiki/raw/`.

Proposed layout:

```text
wiki/
  raw/
    <source_id>/
      original.pdf
      source.json
      ocr.md
      ocr.json
      assets/
  <wiki-pages>.md
  index.md
  log.md
```

`source.json`:

```json
{
  "id": "sha256-or-ulid",
  "kind": "pdf",
  "filename": "paper.pdf",
  "mime_type": "application/pdf",
  "sha256": "...",
  "size_bytes": 12345,
  "created_at": "2026-04-29T00:00:00Z",
  "status": "stored|ocr_complete|ingested|failed",
  "ocr_model": "mistral-ocr-latest",
  "page_count": 12,
  "wiki_pages": ["example-page"],
  "error": ""
}
```

`ocr.md` format:

```markdown
# Source OCR: paper.pdf

Source ID: source_...
Model: mistral-ocr-latest

## Page 1

...

## Page 2

...
```

## 5. New Packages

Add these packages with narrow responsibilities:

- `internal/source`: source ID generation, raw file storage, metadata read/write, source listing.
- `internal/ocr`: Mistral OCR client, request/response structs, markdown extraction.
- `internal/ingest`: source-to-wiki pipeline orchestration.
- `internal/tools/source.go`: LLM-facing source tools.
- `internal/telegram/documents.go`: Telegram PDF handling.
- `cmd/debug_ingest`: local smoke harness for OCR and ingest.

Do not mix OCR code into `internal/wiki` or `internal/search`. The wiki stores durable synthesis; OCR stores raw extracted source material.

## 6. New Tools

Aura must consolidate Picobot's tool surface, but translate memory and files into Aura's source/wiki model instead of copying names blindly.

### Picobot Tool Parity

Picobot reference path: `D:\tmp\picobot\internal\agent\tools`.

Full audit: `docs/picobot-tools-audit.md`.

| Picobot tool | Purpose | Aura status | Aura equivalent / required action |
| --- | --- | --- | --- |
| `web_search` | Search the web | Exists | Keep `web_search`; already Ollama-backed. |
| `web` | Fetch URL content | Partial | Aura has `web_fetch`; keep that name and do not add duplicate `web`. |
| `write_memory` | Append/overwrite daily or long-term memory | Partial | Covered by `write_wiki`, but add source/journal rules before replacing fully. |
| `list_memory` | List memory files | Missing | Implement as `list_wiki` plus `list_sources`. |
| `read_memory` | Read daily or long-term memory | Partial | Covered by `read_wiki`; add `read_source` for raw/OCR sources. |
| `edit_memory` | Find/replace memory content | Missing | Implement safer `update_wiki` or `propose_wiki_change`, not raw replace. |
| `delete_memory` | Delete daily memory file | Missing | Implement `delete_source` / `archive_wiki` later with review, not hard delete first. |
| `filesystem` | Read/write/list workspace files | Missing | Implement restricted `filesystem` only after source/wiki tools; use path containment and read-first policies. |
| `exec` | Run restricted commands | Missing | Implement only as trusted admin/debug tool, disabled by default. |
| `cron` | Schedule/list/cancel reminders/tasks | Missing | Implement SQLite-backed `schedule_task`, `list_tasks`, and `cancel_task` for reminders and periodic maintenance. |
| `message` | Send a message to current chat | Missing | Implement only if tool loop needs multi-message progress; Telegram currently sends final responses directly. |
| `spawn` | Spawn background subagent stub | Missing | Defer; Aura should first use deterministic jobs before subagents. |
| `create_skill` | Create local skill | Missing | Defer or implement under admin-only skill management. |
| `list_skills` | List local skills | Missing | Defer or implement admin-only. |
| `read_skill` | Read local skill | Missing | Defer or implement admin-only. |
| `delete_skill` | Delete local skill | Missing | Defer; destructive and needs review gate. |
| `mcp_<server>_<tool>` | Delegate to MCP server tools | Missing | Implement generic MCP adapter after core source/wiki tools. |

Aura-only tools required by the second-brain design:

| Aura tool | Purpose | Priority |
| --- | --- | --- |
| `store_source` | Store PDFs, URLs, text, and future uploads as immutable sources | P0 |
| `ocr_source` | Run Mistral OCR over stored PDF sources | P0 |
| `read_source` | Read source metadata, OCR markdown, or excerpts | P0 |
| `ingest_source` | Compile an OCR/text source into wiki updates | P0 |
| `list_sources` | List source inbox/status | P0 |
| `lint_sources` | Find unprocessed or inconsistent source records | P1 |
| `append_log` | Append parseable entries to `wiki/log.md` | P0 |
| `rebuild_index` | Regenerate `wiki/index.md` | P0 |
| `lint_wiki` | Report broken links, orphans, stale pages, missing sources, duplicates | P0 |
| `graph_query` | Query links, backlinks, hubs, orphans, and paths | P1 |

Minimum must-have set before UI work:

- Existing: `web_search`, `web_fetch`, `write_wiki`, `read_wiki`, `search_wiki`.
- Add next: `store_source`, `ocr_source`, `read_source`, `ingest_source`, `list_sources`, `append_log`, `rebuild_index`, `list_wiki`, `lint_wiki`.
- Then add: `update_wiki`, `propose_wiki_change`, `apply_wiki_change`, `lint_sources`, `graph_query`, `schedule_task`, `list_tasks`, `cancel_task`.

### Reminder and Scheduler Tools

Aura should keep Picobot's `cron` capability, but implement it with SQLite persistence instead of only in-memory timers.

Required tools:

- `schedule_task`: schedule one-time or recurring reminders and maintenance jobs.
- `list_tasks`: list pending, completed, failed, and recurring tasks.
- `cancel_task`: cancel a task by ID or name.

SQLite table:

```sql
CREATE TABLE IF NOT EXISTS scheduled_tasks (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  kind TEXT NOT NULL,
  message TEXT NOT NULL,
  channel TEXT NOT NULL,
  chat_id TEXT NOT NULL,
  fire_at TEXT NOT NULL,
  recurring INTEGER NOT NULL DEFAULT 0,
  interval_seconds INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  last_error TEXT NOT NULL DEFAULT ''
);
```

Task kinds:

- `reminder`: send a Telegram reminder to the originating chat.
- `wiki_lint`: run `lint_wiki` on a schedule.
- `source_lint`: run `lint_sources` on a schedule.
- `index_rebuild`: run `rebuild_index` on a schedule.

Rules:

- Store all tasks in the configured SQLite database path.
- On startup, load pending tasks and arm timers for due or future jobs.
- If a task is overdue after restart, fire it once and mark it completed, or reschedule it if recurring.
- Enforce a minimum recurring interval, default 2 minutes.
- Log only task IDs, names, status, and timing metadata.
- Do not store secrets in task messages.
- Telegram reminders must respect the allowlist and original chat context.

### `store_source`

Stores a user-provided file or URL as an immutable source record.

Inputs:

- `kind`: `pdf|url|text`
- `filename`
- `path` or `url` or `content`

Output:

- source ID
- stored path
- status

### `ocr_source`

Runs OCR on a stored PDF source.

Inputs:

- `source_id`
- `table_format`: optional, default `markdown`
- `include_images`: optional, default `false`

Output:

- page count
- markdown bytes
- estimated OCR cost
- status

### `read_source`

Reads extracted source markdown or metadata by source ID.

Inputs:

- `source_id`
- `mode`: `metadata|ocr|excerpt`

### `ingest_source`

Compiles an OCR-complete source into wiki updates.

Inputs:

- `source_id`
- `goal`: optional user instruction for what to extract
- `mode`: `propose|apply`

Behavior:

- Read `ocr.md`.
- Search existing wiki pages.
- Create or update source summary page.
- Update relevant entity/concept pages.
- Include source references.
- Append to `wiki/log.md`.
- Reindex affected wiki pages.

### `list_sources`

Lists sources by status, kind, date, or filename.

### `lint_sources`

Reports sources that are stored but not OCRed, OCRed but not ingested, missing metadata, or missing generated wiki links.

## 7. Telegram PDF Flow

When an allowlisted user sends a PDF:

1. Bot validates MIME type and size.
2. Bot downloads the file to `wiki/raw/<source_id>/original.pdf`.
3. Bot writes `source.json` with status `stored`.
4. Bot replies: `PDF stored as source <source_id>. Running OCR...`
5. Bot runs `ocr_source`.
6. Bot replies with page count, estimated OCR cost, and status.
7. Bot asks the LLM to use `ingest_source` or prompts user for an ingest goal.

No raw PDF text or base64 content should be logged.

## 8. Ingestion Rules

The LLM must not dump full OCR text into a wiki page. It must compile.

For each source:

- Create one source summary page.
- Update existing pages before creating duplicates.
- Use `[[slug]]` links.
- Add source references to affected pages.
- Record the operation in `wiki/log.md`.
- Keep the raw OCR in `wiki/raw/<source_id>/ocr.md`.

Wiki page source references should use stable local source references when no public URL exists:

```yaml
sources:
  - source:source_abc123
```

This requires increasing source length limits later if needed.

## 9. Security and Privacy

- `MISTRAL_API_KEY` is required for OCR; do not reuse `LLM_API_KEY` or `EMBEDDING_API_KEY`.
- OCR is an external API call; the user should be able to disable it with `OCR_ENABLED=false`.
- PDFs from Telegram are trusted only after allowlist check.
- Reject unsupported file types.
- Enforce max file size and max page count.
- Store raw sources under ignored paths by default.
- Log only source IDs, filenames, sizes, page counts, and status.
- Never log base64, raw OCR text, API keys, or full document contents.

## 10. Cost Controls

Track OCR cost separately from LLM token cost.

Metrics:

- `ocr_pages_total`
- `ocr_cost_estimated_total`
- `ocr_requests_total`
- `ocr_failures_total`
- `ocr_latency_ms`
- `source_ingest_success_total`
- `source_ingest_failure_total`

Budget rules:

- Estimate OCR cost from page count when available.
- If page count is unknown, warn before processing large PDFs.
- Add `OCR_HARD_BUDGET` later if OCR usage grows.

## 11. Acceptance Criteria

### PDF Storage

- [ ] Telegram PDF upload from allowlisted user is saved under `wiki/raw/<source_id>/original.pdf`.
- [ ] `source.json` is written atomically.
- [ ] Duplicate PDFs are detected by sha256.
- [ ] Unsupported files are rejected with a clear message.

### OCR

- [ ] `internal/ocr` can call a fake Mistral-compatible test server.
- [ ] OCR request supports base64 PDF input.
- [ ] OCR response pages are converted to stable `ocr.md`.
- [ ] OCR metadata is saved as `ocr.json`.
- [ ] API key and raw document content never appear in logs.

### Source Ingestion

- [ ] `ingest_source` creates or updates wiki pages from OCR markdown.
- [ ] Generated wiki pages cite `source:<source_id>`.
- [ ] A source summary page is created for each ingested PDF.
- [ ] A `wiki/log.md` entry is appended for ingest operations.
- [ ] Affected pages are reindexed.

### Maintenance

- [ ] `list_sources` and `lint_sources` report source status.
- [ ] `list_wiki`, `append_log`, `rebuild_index`, and `lint_wiki` exist.
- [ ] Natural prompt smoke test covers OCR and source ingest.

### Verification

- [ ] `go test ./...`
- [ ] `go build ./...`
- [ ] `go vet ./...`
- [ ] `go test -race ./...`
- [ ] `go run ./cmd/debug_tools --live-web`
- [ ] `go run ./cmd/debug_ingest --sample-pdf <path>`

## 12. Implementation Order

1. Config:
   - Add `MISTRAL_API_KEY`, `MISTRAL_OCR_MODEL`, `MISTRAL_OCR_BASE_URL`, OCR limits, and OCR feature flags.

2. Source store:
   - Implement `internal/source` and tests.

3. OCR client:
   - Implement `internal/ocr` and tests with fake server responses.

4. Telegram PDF handler:
   - Add document handling, store PDFs, and trigger OCR.

5. Source tools:
   - Add `store_source`, `ocr_source`, `read_source`, `list_sources`, `lint_sources`.

6. Ingestion:
   - Add `ingest_source` pipeline.
   - Add source summary page creation and affected-page reindexing.

7. Wiki maintenance:
   - Add `append_log`, `rebuild_index`, `list_wiki`, `lint_wiki`.

8. Natural prompt tests:
   - Extend `cmd/debug_tools` or add `cmd/debug_ingest`.

9. UI after tools:
   - Source inbox.
   - PDF status page.
   - Wiki graph and health dashboard.

## 13. Non-Goals For This Next Slice

- No full web UI before source/OCR tools work.
- No OCR annotations until base OCR ingestion is stable.
- No batch OCR until single PDF flow is reliable.
- No automatic deletion of raw PDFs.
- No cloud database requirement.

## 14. References

- Mistral OCR 3 model card: https://docs.mistral.ai/models/model-cards/ocr-3-25-12
- Mistral OCR processor docs: https://docs.mistral.ai/capabilities/document_ai/basic_ocr/
- Mistral Document AI overview: https://docs.mistral.ai/capabilities/document_ai
- Mistral Document AI annotations: https://docs.mistral.ai/capabilities/document_ai/annotations
- Mistral Document QnA: https://docs.mistral.ai/capabilities/document_ai/document_qna
