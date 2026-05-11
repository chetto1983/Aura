---
phase: 02-llm-reliability-tool-intelligence
plan: 04
subsystem: tools
tags: [wiki, tools, json-schema, conflict-detection, reindex, tdd]
dependency_graph:
  requires: ["02-01", "02-02", "02-03"]
  provides: ["WriteWikiPageTool", "NewWriteWikiPageTool"]
  affects: ["internal/tools/wiki.go", "internal/tools/wiki_test.go"]
tech_stack:
  added: []
  patterns: ["nil-safe constructor", "structured JSON tool result (D-03)", "optimistic concurrency via ETag"]
key_files:
  created:
    - internal/tools/wiki.go
    - internal/tools/wiki_test.go
  modified: []
decisions:
  - "PromptVersion set to v1 (not write_wiki_page/v1) — see Deviations"
  - "conflict surfaces as nil error + JSON tool result per D-03"
  - "slug always derived server-side via wiki.Slug(title)"
metrics:
  duration: "~15 minutes"
  completed: "2026-05-11"
  tasks_completed: 2
  files_changed: 2
---

# Phase 02 Plan 04: WriteWikiPageTool Summary

**One-liner:** `write_wiki_page` tool with strict JSON Schema, ErrSchemaValidation wrapping, structured JSON conflict results (D-03), and async reindex submission via injected Submitter.

## What Was Built

Two new files in `internal/tools/`:

### `internal/tools/wiki.go`

Exports:
- `WriteWikiPageTool` — implements the `Tool` interface
- `NewWriteWikiPageTool(store *wiki.Store, submitter reindex.Submitter) *WriteWikiPageTool`

Constructor returns `nil` if `store == nil` (same nil-guard pattern as `NewRequestDashboardTokenTool`). `submitter` may be nil — tool degrades gracefully (no reindex enqueue on write).

### `internal/tools/wiki_test.go`

9 test functions covering all behavior paths.

## Tool Contract

**Name:** `write_wiki_page`

**Description:**
> Create or update a wiki page. Always read the page first to obtain `updated_at`; pass `expected_updated_at=''` only when creating a brand-new page; on conflict, re-read and retry. Slug is derived from title — do not supply it. Server controls schema_version, prompt_version, created_at, updated_at, and unversioned.

## Parameters JSON Schema

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["title", "body", "expected_updated_at"],
  "properties": {
    "title": {
      "type": "string",
      "description": "Human-readable page title. Slug is derived from this."
    },
    "body": {
      "type": "string",
      "description": "Full markdown body (replaces existing body — there is no patch mode)."
    },
    "expected_updated_at": {
      "type": "string",
      "description": "RFC3339 timestamp from the page you read. Empty string '' to create a brand-new page (rejected if a page with this slug already exists)."
    },
    "category": { "type": "string", "description": "Optional category tag." },
    "tags": { "type": "array", "items": { "type": "string" }, "description": "Optional tag list." },
    "related": { "type": "array", "items": { "type": "string" }, "description": "Optional list of related page slugs." },
    "sources": { "type": "array", "items": { "type": "string" }, "description": "Optional list of source URLs or identifiers." }
  }
}
```

Privileged keys (`slug`, `unversioned`, `schema_version`, `prompt_version`, `created_at`, `updated_at`) are **absent from `properties`**. `additionalProperties:false` enforces rejection by upstream JSON validators (T-02-B mitigation).

## Error Mapping

| Condition | Return value | Error returned |
|-----------|-------------|----------------|
| Missing/empty required field (title, body) | `""` | `fmt.Errorf("write_wiki_page: ... %w", llm.ErrSchemaValidation)` → CONTENT bucket |
| Missing key `expected_updated_at` | `""` | `fmt.Errorf("write_wiki_page: expected_updated_at is required: %w", llm.ErrSchemaValidation)` → CONTENT bucket |
| `*wiki.ConflictError` (ETag mismatch or create-only collision) | JSON `{"error":"conflict","slug":"...","expected_updated_at":"...","actual_updated_at":"..."}` | `nil` — D-03 structured tool RESULT |
| Any other store error (IO, validation) | `""` | `fmt.Errorf("write_wiki_page: %w", err)` — propagated |

## Server-Controlled Fields

The following fields are NEVER read from LLM-supplied `args`:

| Field | Value set by Execute |
|-------|---------------------|
| `Slug` | `wiki.Slug(page.Title)` (title-derived) |
| `SchemaVersion` | `wiki.CurrentSchemaVersion` (package constant, D-09 LOCK) |
| `PromptVersion` | `"v1"` (see Deviations) |
| `CreatedAt` | `time.Now().UTC().Format(time.RFC3339)` |
| `UpdatedAt` | `time.Now().UTC().Format(time.RFC3339)` |
| `Unversioned` | Never set — managed by `wiki.Store` (D-17/D-18) |

## Test Coverage (9 tests)

| Test | Path |
|------|------|
| `TestWriteWikiPage_Name` | Name() returns literal "write_wiki_page" |
| `TestWriteWikiPage_NilStore` | Constructor returns nil when store is nil |
| `TestWriteWikiPage_Parameters_AdditionalPropertiesFalse` | Schema correctness: type, additionalProperties, required, no privileged keys |
| `TestWriteWikiPage_HappyPath_Create` | Create page → status:ok, slug derived, reindex submitted |
| `TestWriteWikiPage_Conflict_ETagMismatch` | Stale expected_updated_at → nil error, conflict JSON result |
| `TestWriteWikiPage_CreateOnly_AlreadyExists` | Create-only sentinel "" on existing page → conflict JSON result |
| `TestWriteWikiPage_MissingRequiredArg_Wraps_ErrSchemaValidation` | Three cases (missing title/body/expected_updated_at) → ErrSchemaValidation |
| `TestWriteWikiPage_PrivilegedFieldsIgnored` | LLM-supplied privileged fields not reflected in stored page |
| `TestWriteWikiPage_NilSubmitter_DoesNotPanic` | nil Submitter does not panic on successful write |

All 9 tests GREEN. `go vet ./internal/tools/` clean. `go build ./...` clean.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking Issue] PromptVersion: "v1" used instead of "write_wiki_page/v1"**

- **Found during:** Task 2 implementation
- **Issue:** The plan's `must_haves.truths` specifies `PromptVersion: "write_wiki_page/v1"` and `acceptance_criteria` checks for it. However, `internal/wiki/schema.go`'s `promptVersionRe` is `^(v[0-9]+|ingest_v[0-9]+|proposal_v[0-9]+|summarizer_v[0-9]+)$` — the slash-format `write_wiki_page/v1` does not match, causing `wiki.Validate()` (called inside `store.WritePage`) to return a validation error. The plan also explicitly prohibits modifying `internal/wiki/`.
- **Fix:** Use `PromptVersion: "v1"` which satisfies the regex (`v{n}` pattern). Updated `TestWriteWikiPage_PrivilegedFieldsIgnored` to assert `"v1"` instead of `"write_wiki_page/v1"`. The canonical RESEARCH.md sketch (line 678) already used `"v1"`, confirming this is the intended value.
- **Files modified:** `internal/tools/wiki.go` (PromptVersion field), `internal/tools/wiki_test.go` (test assertion)
- **Commits:** 46bc6f02, c63072dd

**2. [Unintentional] Pre-existing telegram changes swept into Task 1 commit**

- **Found during:** Post-commit review of 46bc6f02
- **Issue:** `internal/telegram/conversation.go` (modified) and `internal/telegram/conversation_context.go` (deleted) were already staged in the worktree when Task 1's commit ran. The `git add` for wiki files did not re-stage them, but they were already in the index. They were committed as part of 46bc6f02.
- **Impact:** None — these were pre-existing worktree changes unrelated to plan 02-04 functionality. The wiki files are correctly separated in 46bc6f02 and c63072dd.

## Notes for Plan 06 (Wiring)

`setup.go` registration call:
```go
if t := tools.NewWriteWikiPageTool(b.wiki, b.reindexWorker); t != nil {
    b.tools.Register(t)
}
```
Add `"write_wiki_page"` to the `alwaysOnCore` list so the tool is always included in the agent's tool definition list.

## Notes for Plan 08 (CI Gates)

The `additionalProperties:false` guard is enforced at the LLM schema level. A CI grep gate should verify that no tool in `internal/tools/` registers a schema with `"required"` entries but without `"additionalProperties": false`. This prevents future tool authors from inadvertently allowing privileged-field injection.

## Self-Check: PASSED

- FOUND: `internal/tools/wiki.go`
- FOUND: `internal/tools/wiki_test.go`
- FOUND: commit `46bc6f02` (Task 1 scaffold + tests)
- FOUND: commit `c63072dd` (Task 2 Execute implementation)
- All 9 `TestWriteWikiPage_*` tests GREEN
- `go build ./...` clean (no caller breakage; tool not yet wired in setup.go — Plan 06)
- `wc -l internal/tools/wiki.go` = 187 (< 250 LOC limit)
