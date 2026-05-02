# Wiki Graph Migration Plan

> **STATUS — 2026-05-02: IMPLEMENTED & MIGRATED.** YAML → MD+frontmatter migration complete. `[[wiki-links]]`, `index.md`, `log.md`, schema validation, atomic writes, and Git versioning are all live. The `MigrateYAMLToMD` one-shot has run on every existing install. This document is preserved for historical context. Current wiki contract lives in [`wiki/SCHEMA.md`](../wiki/SCHEMA.md) and the source code under [`internal/wiki/`](../internal/wiki/).

## Context
Aura currently stores wiki pages as flat YAML files (`.yaml`) with a `Page` struct containing `title`, `content`, `tags`, etc. The Karpathy wiki pattern calls for a compounding knowledge graph: markdown files with YAML frontmatter, `[[wiki-links]]` between pages, auto-generated `index.md` for routing, and `log.md` for audit trail. This makes the LLM smarter because it can explore the graph instead of doing flat RAG retrieval.

## Key Changes
1. **Page format**: `.yaml` → `.md` with YAML frontmatter + markdown body
2. **Page struct**: `Content` → `Body`, add `Category`, `Related`, `Sources`
3. **New files**: `index.md` (auto-generated catalog), `log.md` (append-only audit trail)
4. **Graph links**: `[[slug]]` syntax in markdown body, `ExtractWikiLinks()` to parse them
5. **Search**: Index markdown body content (not just title), include excerpts in results
6. **System prompt**: Updated to instruct LLM about MD format and `[[links]]`
7. **Migration**: One-time startup migration from `.yaml` → `.md`, remove `Content` field

## Implementation Status: COMPLETED

### Step 1: Schema changes — `internal/wiki/schema.go` ✅
- `Content` → `Body` (`yaml:"-"`)
- Added `Category`, `Related []string`, `Sources []string` fields
- `CurrentSchemaVersion` bumped to 2
- `Validate()` checks `Body` instead of `Content`, validates `Related` slugs, `Sources` max 10
- `ExtractWikiLinks(body string) []string` using regex `\[\[([a-z0-9-]+)\]\]`

### Step 2: Parser changes — `internal/wiki/parser.go` ✅
- `ParseMD(data []byte) (*Page, error)` — parses `---\n<frontmatter>\n---\n<body>` format
- `parseYAML` tries MD format first, falls back to YAML
- `WriteFromLLMOutput` detects and parses MD format
- Fixed bug: body offset calculation from frontmatter
- Fixed bug: validation error now passed to retryWithFeedback

### Step 3: Store changes — `internal/wiki/store.go` ✅
- `WritePage`: serializes frontmatter + `---` + body → `<slug>.md`, removes `.yaml` legacy
- `ReadPage`: tries `.md` first, falls back to `.yaml`
- `DeletePage`: handles both `.md` and `.yaml`
- `ListPages`: scans both `.md` and `.yaml`, skips `index` and `log`
- `updateIndex(ctx)` — regenerates `index.md` grouped by category
- `appendLog(ctx, action, slug)` — appends to `log.md` markdown table
- `MigrateYAMLToMD(ctx) (int, error)` — one-time startup migration
- `Lint(ctx) ([]LintIssue, error)` — checks broken links, orphans, missing categories
- `indexMu sync.Mutex` and `logMu sync.Mutex` for concurrent safety

### Step 4: Search changes — `internal/search/search.go` + `sqlite.go` ✅
- `IndexWikiPages` / `indexWikiDir`: scans `.md`, skips `index.md`/`log.md`, prefers `.md` over `.yaml`
- `ReindexWikiPage`: handles `.md` format with body content
- `FormatResults`: uses `[[slug]]` format, includes excerpt (first 200 chars)
- Added: `extractFromMD`, `findMDBodyEnd`, `truncateExcerpt`

### Step 5: System prompt — `internal/conversation/system_prompt.go` ✅
- `## Wiki Writing` section updated for MD format with `[[slug]]` links
- Added `category` and `related` fields to example
- Instructs LLM to link to existing pages and create new ones

### Step 6: Bot changes — `internal/telegram/bot.go` ✅
- `looksLikeWikiYAML` → `looksLikeWikiContent` (detects `---` frontmatter)
- Calls `MigrateYAMLToMD()` on startup

### Step 7: SCHEMA.md — `wiki/SCHEMA.md` ✅
- Documented new MD format, frontmatter fields, `[[links]]` syntax, special files

### Step 8: Test updates ✅
- All `Page` struct literals: `Content` → `Body`
- `schema_version: 1` → `2`, `.yaml` → `.md`
- Validation error messages updated
- All tests passing

### Step 9: Integration test ✅
- Build: `go build ./...` — compiles without errors
- Tests: `go test ./internal/wiki/... ./internal/search/... ./internal/conversation/...` — all passing

## Files Modified
- `internal/wiki/schema.go` — Page struct, Validate, Slug, ExtractWikiLinks
- `internal/wiki/parser.go` — ParseMD, parseYAML→parseMD priority, WriteFromLLMOutput
- `internal/wiki/store.go` — WritePage, ReadPage, DeletePage, ListPages, updateIndex, appendLog, MigrateYAMLToMD, Lint
- `internal/search/search.go` — IndexWikiPages, ReindexWikiPage, FormatResults
- `internal/search/sqlite.go` — indexDocument for .md files
- `internal/conversation/system_prompt.go` — Wiki Writing section
- `internal/telegram/bot.go` — looksLikeWikiContent, startup migration
- `wiki/SCHEMA.md` — Documented new format
- All test files in `internal/wiki/` and `internal/search/`