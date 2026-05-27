# Coding Conventions

**Analysis Date:** 2026-05-27

## Naming Patterns

**Go packages:**
- All lower-case, no underscores. Hierarchical under `internal/` and `cmd/` (`internal/agent/tools/registry`, `cmd/probe_chat`).
- The package directive in every `.go` file inside a subdirectory matches the directory name. Exception: tool registry uses `package tools` under `internal/agent/tools/registry/` (`internal/agent/tools/registry/registry.go:1`).
- Module path is `github.com/aura/aura` (`go.mod:1`). Imports are absolute.

**Go files:**
- `snake_case.go` (e.g. `internal/wiki/store_writes.go`, `internal/agent/tools/registry/agent_note.go`).
- Tests live alongside source as `<file>_test.go` (`internal/wiki/store_writes_test.go`).
- Build-tagged "live" tests use a `live_<scope>` tag: `//go:build live_ingest`, `//go:build live_wiki` (`internal/storage/sources/ingest/live_test.go:1`, `internal/wiki/live_memory_hygiene_test.go:1`).

**Go identifiers:**
- Exported: `PascalCase` (`Tool`, `Registry`, `ToolDefinition`, `WritePage`).
- Unexported: `camelCase` (`stringArg`, `argKeys`, `normalizeToolDefinition`).
- Acronyms keep case: `LLM`, `MCP`, `API`, `URL`, `ID` (`LLMCalls`, `ToolCallExample`, `UserIDFromContext`).
- Context keys are unexported empty structs: `type userIDKey struct{}`, `type conversationIDKey struct{}` (`internal/agent/tools/registry/context.go:13-15`).
- Interfaces ending in `-er`: `Tool`, `ChatClient`, `ToolExecutor`, `ToolDefinitionProvider`, `CategorizedTool`.

**TypeScript / React:**
- Components: `PascalCase.tsx` (`web/src/components/SettingsPanel.tsx`, `web/src/components/Shell.tsx`).
- Hooks: `camelCase.ts` starting with `use` (`web/src/hooks/useApi.ts`, `useAppTheme.ts`, `useLocale.ts`).
- Helper modules colocated next to the component they support: `SettingsPanel.helpers.ts`, `SettingsPanel.test.ts` (`web/src/components/`).
- Path alias `@/*` resolves to `web/src/*` (used everywhere — `web/src/App.tsx:4-11`).

## Code Style

**Go formatting:**
- `go fmt ./...` is the source of truth (`Makefile:33`). Tabs for indent, gofmt-standard braces.
- CRLF tolerated in some scripts but Go files are LF; the file-size linter explicitly strips `\r` (`scripts/check-file-size.sh:33`).
- Imports grouped: stdlib first, then module-local (`github.com/aura/aura/...`), then third-party (`internal/wiki/store_writes.go:3-15`, `internal/agent/loop.go:5-19`).
- Doc comments precede the declaration they document and begin with the identifier name (`// Registry stores tools and dispatches tool calls by name.`, `internal/agent/tools/registry/registry.go:88`).

**Go linting:**
- `golangci-lint` v2.12.2 with module-scoped `--new-from-rev` in CI (`.github/workflows/ci.yml:24-28`).
- `lefthook.yml:14-22` runs `golangci-lint run --new-from-rev=HEAD` + optional `dupl -t 60` on staged files at pre-commit.
- Only `depguard` is enabled, but it encodes the architectural boundary contract from `prd.md §9` (`.golangci.yml:3-71`). Boundaries enforced:
  - `internal/agent/**` must not import `channels`, `api`, `telegram`, concrete `storage/qdrant`, concrete `storage/sources`, or `db`.
  - `internal/agentnote`, `internal/wiki`, `internal/storage/memoryindex` must not import `channels`.
  - `internal/storage/**` and `internal/db/**` must not import `agent`.
  - `internal/storage/search/**` and `internal/learning/**` must not import `channels`.
  - `internal/agent/tools/**` must not import `chat`.
- `go vet ./...` runs in CI before tests (`.github/workflows/ci.yml:33-34`).
- File-size cap enforced via `scripts/check-file-size.sh` against `.file-size-baseline.txt` — runs in CI (`.github/workflows/ci.yml:30-31`) and pre-commit (`lefthook.yml:24-27`).

**TypeScript:**
- `web/eslint.config.js` extends `@eslint/js` recommended, `typescript-eslint`, `eslint-plugin-react-hooks`, `eslint-plugin-react-refresh/vite`.
- TypeScript ~6.0 with strict mode via `web/tsconfig.app.json`.
- Build via Vite 8 (`web/vite.config.ts`); produced output goes to `internal/api/dist/` and is `//go:embed`ed (`internal/api/static.go:12`).

## Import Organization

**Go order (enforced by gofmt + convention):**
1. Standard library (`context`, `fmt`, `log/slog`, `database/sql`, `net/http`).
2. Blank line.
3. Module-local (`github.com/aura/aura/internal/...`).
4. Blank line.
5. Third-party (`go.uber.org/zap`, `github.com/google/uuid`, `modernc.org/sqlite`).

Example: `internal/wiki/store_writes.go:3-15`, `internal/agent/loop.go:5-19`.

**TypeScript order:**
1. Bare-specifier libs (`react`, `react-router-dom`).
2. Path-alias modules (`@/components/Shell`, `@/hooks/useApi`).
3. Relative imports.

Example: `web/src/App.tsx:1-11`, `web/src/api.ts:1-53`.

## Error Handling

**Go conventions:**
- Wrap with `%w` everywhere (~919 occurrences under `internal/`). Pattern: `return nil, fmt.Errorf("doing X: %w", err)` (`internal/wiki/store_writes.go:62-63, 129`).
- Inspect with `errors.Is` / `errors.As` (~213 occurrences). Custom error types implement `Error()` and are returned as pointers: `*ConflictError` with `Slug/Expected/Actual` fields (`internal/wiki/store_writes.go:29-37`).
- `panic()` is forbidden in production paths — only 5 occurrences in the whole codebase, and 3 are in tests / agentdef boot wiring where program startup cannot proceed (`internal/agent/agentdef/builtin.go:41`, `internal/identity/store_helpers.go:361`).
- Boot is fault-tolerant: optional sidecars (Qdrant, Garage, MCP servers) log and degrade rather than abort (`CLAUDE.md` Key Conventions, "Boot non-fatal").
- Tool errors carry a low-cardinality `error_class` enum (timeout, not_found, validation, …) so log volume stays bounded without leaking message content (`internal/agent/tools/registry/error.go`, referenced from `internal/agent/tools/registry/README.md:115-118`).

**Context propagation:**
- Every function that may do I/O, hit the LLM, or block takes `ctx context.Context` as the first argument (`Tool.Execute(ctx context.Context, args map[string]any)`, `Store.WritePage(ctx context.Context, page *Page, ...)`).
- Per-tool default execution timeout is 5 minutes, attached by `Registry.Execute` when the caller did not (`internal/agent/tools/registry/registry.go`, README "Concurrency" section).
- Long-running streaming uses cancellable child contexts and explicit `defer iterCancel()` (`internal/agent/loop.go:71-76`).

**Context-carried values:**
- `WithUserID` / `UserIDFromContext`, `WithConversationID` / `ConversationIDFromContext`, `WithAllowedToolNames` / `AllowedToolNamesFromContext` — all use unexported `struct{}` keys (`internal/agent/tools/registry/context.go`).
- Tools that need user/conversation identity read from context; they reject the call rather than guess when unset.

## Logging

**Stack:**
- Structured logging via `log/slog` (stdlib) backed by `go.uber.org/zap` (`internal/logging/zap_slog.go:1-61`).
- One configuration call wires the stack: `logging.Setup(level, logDir)` returns `(*slog.Logger, cleanup)`. Output is JSON to stdout AND a daily-rotating file (`dailyWriter`, `internal/logging/daily_writer.go`).
- Daily rotation keeps 24h of logs (`internal/logging/zap_slog.go:36`).
- `slog.SetDefault()` is set in `Setup` so any package using `slog.Default()` inherits the sanitizing wrapper (`zap_slog.go:53`).

**Sanitization (mandatory boundary):**
- `internal/logging/sanitize.go` wraps the inner handler. `SanitizeHandler` replaces any attribute value whose KEY matches a secret pattern with `[REDACTED]` (`internal/logging/sanitize.go:23-87`).
- Exact-match secret keys: `token`, `auth`, `cookie`, `secret`, `credential`, `password`, `apikey`, `api_key`, `api-key` (`internal/logging/sanitize.go:59-68`).
- Prefix-matched secret keys: `token_`, `api_key_`, `auth_`, `secret_`, `password_` (`internal/logging/sanitize.go:74-86`).
- `internal/logging/sanitize_test.go` is the regression net for this list.

**Tool-argument privacy rule (CLAUDE.md):**
- Only tool names and argument *keys* are logged — never values, URLs with tokens, base64 payloads, or source text.
- Implementation: `argKeys(args)` (`internal/agent/tools/registry/registry.go:430-441`) iterates keys, runs each against `sensitiveArgKeyRe` (`regexp` matching `password|passwd|secret|token|api[_-]?key|auth|credential|bearer|session[_-]?id|cookie`), and replaces matches with the literal string `<redacted>`. Keys are sorted before logging for deterministic output.
- Single canonical log site: `r.logger.Info("tool started", "tool", name, "arg_keys", argKeys(args))` and `r.logger.Info("tool completed", "tool", name, "elapsed", elapsed, "bytes", len(result))` (`internal/agent/tools/registry/registry.go:362, 390`).

**Level conventions:**
- `Debug` — internal state, loop iterations (`agent: run start`, `internal/agent/loop.go:67`).
- `Info` — observable events (`tool started`, `tool completed`, `wiki page written`).
- `Warn` — recoverable degradation (`max_iterations_capped`, `FTS5 sync failed on write`, `freshness bump failed`).
- `Error` — unrecoverable failure paths that still let the program continue (`git commit failed for wiki page`, `internal/wiki/store_writes.go:158`).

## Tool Design Pattern

**Canonical interface (`internal/agent/tools/registry/registry.go:20-25`):**

```go
type Tool interface {
    Name() string
    Description() string
    Parameters() map[string]any        // JSON Schema fragment
    Execute(ctx context.Context, args map[string]any) (string, error)
}
```

**Layered providers (opt-in):**
- `ToolDefinitionProvider` — own a curated `ToolDefinition` with `Examples`, `RequiredCapability`, MCP hints (`ReadOnlyHint`, `DestructiveHint`, `IdempotentHint`, `OpenWorldHint`), `VisibilityTier`, `OutputCap`. See `internal/agent/tools/registry/definition.go:30-53`.
- `ToolCapabilityProvider` — declare a narrower `identity.Capability` than the default `CapabilityToolExecute`.
- `ToolAvailabilityProvider` — let dynamic tools (MCP, skill-backed) disappear from the LLM catalogue without unregistering.
- `CategorizedTool` / `MultiCategorizedTool` — opt into category gating (`CategoryMCP`, `CategoryAutonomous`).

**Visibility tiers (`definition.go:19-26`):**
- `VisibilityAlwaysOn` — always in the LLM tool pool.
- `VisibilityActiveTurn` — in the pool, may be trimmed under budget pressure (default).
- `VisibilityDeferred` — excluded from default pool, discovered on demand.

**Action-enum dispatch convention:**
Modern tools take a single `action` string + per-action argument keys instead of N narrow tools. See `AgentNoteTool` (`internal/agent/tools/registry/agent_note.go:73-97`): one tool, four actions (`set`, `append`, `get`, `clear`), each with a documented `RequiredKeys` declared via `ActionVariant` so the JSON Schema `oneOf` blocks invalid combinations at the provider layer when supported. Other examples: `search` (8 actions), `web` (2), `file` (12+), `source` (6), `task` (4), `wiki_page` (4). See `CLAUDE.md` Architecture > Tools table.

**Argument extraction:**
- Use shared helpers from `internal/agent/tools/registry/args.go`: `stringArg`, `stringSliceArg`, `cleanStrings`. All trim whitespace at the boundary.
- Numeric / URL helpers live in `web_common.go`.

## Deterministic Wiki Writes

The wiki is bedrock; writes are atomic and versioned. See `internal/wiki/store_writes.go` for the canonical `WritePage` implementation and `CLAUDE.md` "Wiki" section.

**Invariants enforced inside `Store.WritePage`:**
1. **`temperature=0`** for the LLM call that produces page content — set by the wiki tool layer before invoking the writer.
2. **Per-slug mutex.** `s.fileMutex(slug)` is acquired before any read OR write inside `writePageLocked` (`store_writes.go:89-91`). Lock targets in cross-page maintenance (`maintainBacklinks`) are sorted slug order to prevent A↔B deadlocks (`store_writes.go:212-218`).
3. **Optimistic concurrency / ETag check INSIDE the mutex.** `expectedUpdatedAt` parameter supports three modes: trust-caller (no arg), create-only-if-absent (`""` sentinel), update-if-on-disk-matches. ETag check and atomic write are both inside the critical section to close the TOCTOU window (`store_writes.go:103-117`).
4. **Atomic temp+rename.** `writeAtomic(s.dir, slug, path, data)` writes to a temp file in the same directory and `os.Rename`s into place; never a direct write (`store_writes.go:132-135`).
5. **Git commit.** Every change goes through `s.gitCommit(ctx, filename, "update")` using `go-git/go-git/v5`. Commit failure flips `page.Unversioned=true` via a re-read + re-marshal + re-write (no recursive commit) so the wiki visibly degrades but never loses the page (`store_writes.go:156-189`).
6. **FTS5 mirror sync (synchronous).** FTS5 is authoritative for the keyword channel and must not lag disk (`store_writes.go:138-143`).
7. **Async TOC rebuild** kicked via `go s.RebuildTOC()` so the next turn sees the index (`store_writes.go:77`).
8. **Bidirectional backlink maintenance.** New `[[slug]]` links in the body cause each target page to gain the writer's slug in its `related:` frontmatter — additive only, runs OUTSIDE the critical section to avoid cross-page deadlocks (`store_writes.go:62-78, 215-218`).
9. **Reindex enqueue.** A non-blocking `reindex.Job{Slug, Op: OpUpsert}` is submitted after disk write regardless of git outcome (`store_writes.go:194-196`).
10. **Versioned frontmatter.** YAML frontmatter carries `schema_version` and `prompt_version` so the format and the prompt that generated the content are both traceable (`CLAUDE.md` "Wiki" section).

## React / TypeScript Conventions

**Stack:**
- React 19.2 + `react-router-dom` v7 with `<BrowserRouter>` (`web/src/App.tsx:2, 56`).
- Vite 8 build; output → `internal/api/dist/` (Go-embedded). Dev server runs at `localhost:5173` via `npm --prefix web run dev`.
- Vitest 4 for unit tests (`web/package.json:74`, `web/src/components/SettingsPanel.test.ts:1`).
- Playwright 1.59 for E2E against the running dashboard (`web/playwright.config.ts`).

**State / data:**
- No `@tanstack/react-query` or Redux. Direct `useState` + `useEffect` + a thin fetch wrapper in `web/src/api.ts` is the project pattern.
- `web/src/api.ts` exposes typed wrappers around `/api/*`. Bearer token comes from `getToken()` / `clearToken()` in `web/src/lib/auth.ts`. `ApiError` is thrown on non-2xx with status code preserved (`web/src/api.ts:58-60`).
- On 401, `api.ts` clears the token and redirects to `/login?expired=1` (per `CLAUDE.md` API auth section); `RequireAuth` in `App.tsx:40-46` is the second gate.

**Code splitting (mandatory pattern):**
- Only `Shell`, `HealthDashboard`, `Login` are eagerly imported.
- Every other panel uses `lazy()` + `Suspense` with `PanelLoading` fallback. Pre-split bundle was 580 KB; lazy-loading drops the home-route entry to React + shell + dashboard skeleton (`web/src/App.tsx:14-34`).

**i18n:**
- `i18next` + `react-i18next` + `i18next-browser-languagedetector`. Two locales: `en`, `it` (`web/src/i18n/index.ts:4-21`).
- Detection order: `navigator` only, no caches. Fallback `en`.
- `npm --prefix web run i18n:check` walks `src/**/*.{ts,tsx,js,jsx}`, extracts keys, and validates `en`+`it` JSON cover them with matching placeholders (`web/scripts/check-i18n.mjs`).
- `useLocale()` hook hides the i18next plumbing from components (`web/src/hooks/useLocale.ts`).

**Theming:**
- TailwindCSS v4 via `@tailwindcss/vite` plugin (`web/package.json:71`).
- Dark/light/contrast class is applied to `<html>` by `useAppTheme()` in App root (`web/src/App.tsx:10, 54`).

## Comment Policy

**Default: no comments.**
- Most Go declarations have no comment. The wiki writer is heavily commented because every invariant matters (`internal/wiki/store_writes.go`).
- Comments explain **WHY**, not **WHAT**. Recurring patterns visible in the codebase:
  - Pointers back to story IDs / PRD sections (`// Phase 2 (GRAPH-01): bidirectional backlink maintenance.`, `store_writes.go:72`).
  - Production incident citations (`// live 2026-05-21: xlsx voice-memo test saw 22 LLM rounds in 105s, all reading the same wiki page over and over.`, `internal/agent/loop.go:55-59`).
  - Pitfall / TOCTOU notes (`// ETag check INSIDE the critical section (Pitfall #1 TOCTOU prevention).`, `store_writes.go:103`).
  - Boundary justifications (`// Backlink writes happen OUTSIDE the per-slug critical section to avoid cross-page deadlocks (lock targets in sorted slug order).`, `store_writes.go:57-60`).
- File-header comments describe the file's role in 1–2 lines (`// loop.go runs one assistant turn: alternating LLM calls and tool execution`, `internal/agent/loop.go:1-2`).

**Doc-comment style (Go):**
- Begin with the identifier name (`// Registry stores tools and dispatches tool calls by name.`).
- Describe contracts, not internals.
- Tool descriptions (which the LLM reads) include `EXAMPLES — copy the shape exactly:` blocks with concrete JSON calls (`internal/agent/tools/registry/agent_note.go:42-63`).

**TODO/FIXME discipline:**
- 3 TODO/FIXME/HACK strings in production `internal/` (`grep -rn "TODO\|FIXME\|HACK\|XXX" internal/ --include="*.go"`), and all three are inside tool-description example strings — not real debt markers (`internal/agent/tools/registry/file.go:162-165`, `internal/agent/tools/registry/tool_definitions.go:99`).
- Real debt goes into `scripts/ralph/*-staged.json` or `.planning/*` per the CLAUDE.md "BUGS ARE ALWAYS FIXED WHEN FOUND" rule.

## God-Class / File-Size Rule

**Cap:** 600 LOC per non-test, non-generated `.go` file. Enforced by `scripts/check-file-size.sh` (`scripts/check-file-size.sh:19`).

**Exclusions:**
- `*_test.go`, `*_gen.go`, `vendor/`, `web/dist/`, `.claude/` are never checked.
- Per-file exemption via a `// file-size-exempt: <reason>` marker on one of the first 10 lines (no file currently uses this).

**Grandfathered baseline (`.file-size-baseline.txt`):**
File listed → CI fails if it grows BEYOND the recorded count. Not listed → CI fails if > 600.
Baseline as of 2026-05-22 (5 entries):

| LOC | File | Status |
|-----|------|--------|
| 1587 | `cmd/probe_chat/cases.go` | Test corpus, intentionally large. Current: 1511. |
| 790 | `cmd/quality_bench/main.go` | Benchmark harness. Current: 790. |
| 689 | `internal/channels/telegram/invocation_builder.go` | Slimmed since baseline. Current: 596 — back under cap. |
| 650 | `cmd/aura/web_chat.go` | Slimmed since baseline. Current: 386 — back under cap. |
| 621 | `internal/agent/loop.go` | Slimmed since baseline. Current: 594 — back under cap. |

**Current real top non-test files (after the baseline shrinkage):**
- `cmd/probe_chat/cases.go` (1511) — baselined.
- `cmd/quality_bench/main.go` (790) — baselined.
- `internal/api/files.go` (600) — at cap; touching it triggers a mandatory split.
- `internal/channels/telegram/invocation_builder.go` (596).
- `internal/cron/store.go`, `internal/agent/loop.go` (594).
- `internal/config/config.go` (590).
- `internal/agent/tools/registry/scheduler.go` (587).

**Phase-MODERNIZE Wave B** (US-MOD-SPLIT-01..10) is the queued story queue for tackling the remaining baselined files.

## Behavioral Rules (from `CLAUDE.md`)

These are mandatory for every code change. Surfaced here so the rules are visible to future Claude / Codex instances reading this map. Verbatim hard rules:

- **NEVER SUPPOSE.** If uncertain about code behavior, API contract, or module wiring, STOP and ASK.
- **READ BEFORE EDIT.** Always read the file before modifying. Re-read if last read was >5 messages ago.
- **THINK BEFORE TRANSITION.** Before switching from research to editing, pause and state the plan.
- **3-STRIKE RULE.** Do not retry the same failing approach more than 3 times.
- **NEVER MODIFY TESTS TO MAKE THEM PASS.** Fix the code, not the tests (unless the task explicitly asks or tests are genuinely broken).
- **SCOPE CONTROL.** Do exactly what was asked. No bonus features.
- **FOLLOW EXISTING PATTERNS.** Never invent new approaches when codebase patterns exist.
- **INFORMATION TRUST HIERARCHY.** Existing codebase > docs > web search > training data. Never hallucinate APIs.
- **NEVER GUESS PARAMETERS.** If a parameter depends on a previous call's output, execute sequentially.
- **DISCUSSION-FIRST DEFAULT.** No explicit action verb in request → discuss first.
- **NEVER CREATE FILES UNLESS NECESSARY.** Prefer editing existing files; never proactively create docs.
- **GIT PUSH DISCIPLINE.** Never push unless explicitly asked in the current turn.
- **GOD CLASS.** Never create a file >600 LOC; if you find one, refactor.
- **REUSABLE CODE.** Never duplicate; create a reusable helper.
- **DEEP REFACTOR ON TOUCH.** Every module touched in a story/fix/edit MUST be cleaned in the SAME commit. Dead code removed, duplicates folded, legacy patterns translated, comments updated, file size ≤600 LOC, tests aligned, lint+dupl clean, commit body lists LOC delta + dead-code removed + tests touched.
- **VALIDATE WITH VERIFIED BENCHMARKS.** Every E2E test must capture the full assistant reply AND cross-check every factual claim against ground truth (SQLite, filesystem, structured tool result).
- **TESTS VERIFY QUALITY AND METRICS.** A pass = substantive content asserted (keywords, length, structure) AND operational bounds (token budget, wall-clock, tool-call count).
- **NEVER BE SUPERFICIAL.** When a tool produces a file (xlsx, docx, pdf, wiki page), the test MUST fetch the artifact back and verify its bytes and structure.
- **BUGS ARE ALWAYS FIXED WHEN FOUND.** Inline fix or explicit story in `scripts/ralph/*-staged.json` — never a vague note.
- **PROMPTS ARE EN-ONLY.** All instructional prompt text (overlays `AGENT.md`, `SOUL.md`, `TOOLS.md`, `USER.md`; tool descriptions; base prompts) is English. Output language is set by an explicit "Always respond in {language}" directive in the prompt.

## Function Design

**Size:** Small enough to fit on one screen is preferred. The largest production-path function in the repo is `runLoop` (~570 LOC across `internal/agent/loop.go`) — and it is being actively pruned.

**Parameters:**
- `context.Context` first when present.
- Options bag (struct) for >3 parameters (`Options` in `internal/agent/loop.go:21`, used as `runLoop(ctx, client, executor, state, opts)`).
- Optional behavior via variadic with documented semantics (`WritePage(ctx, page *Page, expectedUpdatedAt ...string)` — three modes documented in the doc comment, `internal/wiki/store_writes.go:44-60`).

**Return values:**
- `(value, error)` for fallible operations.
- Custom error types as pointers (`*ConflictError`) so `errors.As` works.
- Tool `Execute` returns `(string, error)` — the string is the LLM-visible result; errors are surfaced separately so the registry can wrap them with `FormatToolError` + `classifyToolError` (`internal/agent/tools/registry/error.go`).

## Module Design

**Layered package layout:**
- `cmd/*` — binary entrypoints, one `main.go` per binary. Includes production (`aura`, `aura_mcp_server`, `aura-init-models`), probes (`probe_chat`, `probe_telegram_ui`, `probe_doc`, `probe_ingest_e2e`, `probe_reasoning`, `probe_webfetch`), debug harnesses (`debug_llm`, `debug_searxng`, `debug_ingest`, `debug_tools`, `debug_xlsx/pdf/docx`, `debug_backup`, `debug_sandbox`, `debug_qdrant`, `debug_telegram`, `debug_reconcile`, `debug_convdump`), benchmarks (`quality_bench`, `bench_ctx`), and one-shot utilities (`build_icon`, `seed_e2e_env`, `module_health`).
- `internal/*` — packages not exposed to external Go consumers. The whole module is internal-by-default; only `cmd/*` binaries are public surface.
- `web/*` — frontend. No Go code; only TypeScript + assets. Build output is embedded back into the Go binary via `//go:embed all:dist` in `internal/api/static.go`.

**Exports:**
- Minimum surface area. Internal helpers like `definitionForTool`, `normalizeToolDefinition`, `requiredCapabilityForTool` stay unexported (`internal/agent/tools/registry/definition.go:80-117`).
- Constructors live in the package they build for (`NewRegistry`, `NewAgentNoteTool`, `NewSanitizeHandler`).

**Barrel files:** None in Go. In TypeScript, only `web/src/i18n/index.ts` (i18next init).

**Embed directives:**
- `//go:embed all:dist` in `internal/api/static.go:12` — entire SPA build.
- `//go:embed defaults/AGENT.md defaults/skills/*/SKILL.md` in `internal/config/bootstrap.go:13` — first-run defaults.
- `//go:embed builtin/*/agent.json builtin/*/prompt.md` in `internal/agent/agentdef/builtin.go:10` — agent definitions.
- `//go:embed announce.md` in `internal/agent/agentdef/announce.go:12`.
- `//go:embed setup_page.html` in `internal/api/setup_server.go:19`.
- `//go:embed icon_app.ico` / `icon.ico` in `internal/tray/tray_windows.go:13,16`.
- Test fixtures: `//go:embed fixtures` in `internal/storage/sources/ingest/fixtures_test.go:13`, `//go:embed tests/fixtures/*.json` in `internal/tokenjuice/fixture_test.go:13`.

---

*Convention analysis: 2026-05-27*
