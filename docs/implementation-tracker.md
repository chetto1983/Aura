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
| 2 | Source store | pending | |
| 3 | OCR client | pending | |
| 4 | Telegram PDF handler | pending | |
| 5 | Source tools | pending | |
| 6 | Ingestion | pending | |
| 7 | Wiki maintenance | pending | |
| 8 | Reminder/scheduler | pending | |
| 9 | Natural prompt tests for OCR/ingest | pending | |
| 10 | UI | pending | |

## Session Log

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
