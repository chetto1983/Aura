# Wiki Graph Migration Plan

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

## Implementation Order

### Step 1: Schema changes — `internal/wiki/schema.go`
- Replace `Content` with `Body` (`yaml:"-"`)
- Add `Category`, `Related []string`, `Sources []string` fields
- Bump `CurrentSchemaVersion` to 2
- Update `Validate()`: check `Body` instead of `Content`, validate `Related` slugs, `Sources` max 10
- Add `ExtractWikiLinks(body string) []string` using regex `\[\[([a-z0-9-]+)\]\]`

### Step 2: Parser changes — `internal/wiki/parser.go`
- Add `ParseMD(data []byte) (*Page, error)` — parses `---\n<frontmatter>\n---\n<body>` format
- Update `parseYAML` → tries MD format first, falls back to YAML
- Update `WriteFromLLMOutput` to detect and parse MD format
- Update retry prompt to reference MD format
- Legacy YAML pages have `Content` mapped to `Body` during read

### Step 3: Store changes — `internal/wiki/store.go`
- `WritePage`: serialize frontmatter + `---` + body → `<slug>.md`
- `ReadPage`: try `.md` first, fall back to `.yaml` (backward compat during migration)
- `DeletePage`: delete `.md` (or `.yaml` fallback)
- `ListPages`: scan for both `.md` and `.yaml`
- Add `updateIndex(ctx)` — regenerates `index.md` grouped by category
- Add `appendLog(ctx, action, slug)` — appends to `log.md` markdown table
- Add `MigrateYAMLToMD(ctx) (int, error)` — one-time startup migration
- Add `Lint(ctx) ([]LintIssue, error)` — checks broken links, orphans, missing categories
- Add `indexMu sync.Mutex` and `logMu sync.Mutex` to `Store` for concurrent safety

### Step 4: Search changes — `internal/search/search.go` + `sqlite.go`
- `IndexWikiPages`: scan `.md` files, parse frontmatter+body, index `title + body`
- Skip `index.md` and `log.md` from indexing
- `ReindexWikiPage`: handle `.md` format with body content
- `FormatResults`: include first 200 chars of content as excerpt, `[[slug]]` format

### Step 5: System prompt — `internal/conversation/system_prompt.go`
- Rewrite `## Wiki Writing` section with MD format and `[[slug]]` links
- Add `category` and `related` fields to the example
- Instruct LLM to link to existing pages and create new ones

### Step 6: Bot changes — `internal/telegram/bot.go`
- Rename `looksLikeWikiYAML` → `looksLikeWikiContent` (detects both formats)
- Call `wikiStore.MigrateYAMLToMD()` on startup
- Rest of the flow unchanged (WriteFromLLMOutput handles format detection)

### Step 7: SCHEMA.md — `wiki/SCHEMA.md`
- Document new MD format, frontmatter fields, `[[links]]` syntax, special files

### Step 8: Test updates
- All `Page` struct literals: `Content` → `Body`
- New tests: `ParseMD`, `ExtractWikiLinks`, `updateIndex`, `appendLog`, `MigrateYAMLToMD`, `Lint`
- Store tests: `.md` file assertions, backward-compatible `.yaml` read
- Search tests: `.md` indexing, content excerpts
- Snapshot tests: MD format parsing determinism

### Step 9: Integration test
- Create MD page → verify file written with frontmatter + body
- Verify `index.md` updated, `log.md` appended
- Search finds the page by body content
- Lint reports no issues for well-formed graph
- Broken link detection works

## Files to Modify
- `internal/wiki/schema.go` — Page struct, Validate, Slug, ExtractWikiLinks
- `internal/wiki/parser.go` — ParseMD, parseYAML→parseMD priority, WriteFromLLMOutput
- `internal/wiki/store.go` — WritePage, ReadPage, DeletePage, ListPages, updateIndex, appendLog, MigrateYAMLToMD, Lint
- `internal/search/search.go` — IndexWikiPages, ReindexWikiPage, FormatResults
- `internal/search/sqlite.go` — indexDocument for .md files
- `internal/conversation/system_prompt.go` — Wiki Writing section
- `internal/telegram/bot.go` — looksLikeWikiContent, startup migration
- `wiki/SCHEMA.md` — Document new format
- All test files in `internal/wiki/` and `internal/search/`

## Verification
1. `go build ./...` — compiles without errors
2. `go test ./internal/wiki/... ./internal/search/... ./internal/conversation/...` — all tests pass
3. Create a test MD page manually, verify `index.md` and `log.md` are generated
4. Run migration on empty wiki (no-op), verify no errors
5. Create `.yaml` file, run migration, verify `.md` created and `.yaml` removed
6. Verify search indexes body content, not just title
7. Verify `Lint` detects broken `[[links]]`