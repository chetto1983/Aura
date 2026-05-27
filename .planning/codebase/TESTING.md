# Testing Patterns

**Analysis Date:** 2026-05-27

## Test Framework

**Go runner:**
- Standard library `testing` package — no `testify`, no `ginkgo`, no `gomock`. Pure stdlib `t.Errorf`, `t.Fatalf`, `t.Run`, `t.TempDir`, `t.Setenv`, `t.Cleanup`, `t.Helper`, `t.Parallel`.
- ~638 `_test.go` files (`find . -name "*_test.go" -not -path './.git/*' -not -path './node_modules/*'`).
- Race detector required on critical packages and recommended everywhere (`CLAUDE.md` "Build & Test Commands").
- Test commands (canonical, from `Makefile` + `CLAUDE.md`):

```powershell
go test ./...                                     # run all unit tests
go test -run TestFoo ./internal/...               # run a specific test
go test -race ./internal/{api,auth,mcp,skills}    # race-detector on critical pkgs
go test -race -count=1 ./...                      # CI invocation (.github/workflows/ci.yml:44)
```

**Frontend runner:**
- Vitest 4.1.6 (`web/package.json:74`) — `npm --prefix web run test` (= `vitest run`).
- Playwright 1.59 for browser E2E (`web/package.json:25`, `web/playwright.config.ts`).
- ESLint 10 + typescript-eslint 8.58 for lint (`npm --prefix web run lint`).

## Test File Organization

**Location:**
- Co-located with source. `<file>.go` ↔ `<file>_test.go` in the same package directory.
- Same Go package (white-box testing). Unexported helpers (`fakeAgentNoteStore`, `newFakeLoopState`) are package-private fixtures.

**Naming:**
- `Test<Subject><Behavior>` is the dominant pattern (`TestRunLoopNoToolCallsReturnsAssistantText`, `internal/agent/loop_test.go:17`).
- Underscore variant for grouping (`TestAliasIndex_Roundtrip`, `TestAliasIndex_NormalizationEdgeCases`, `internal/wiki/alias_index_test.go:12-72`).
- Helpers prefixed with the role (`newTestAgentNoteTool`, `fakeAgentNoteStore`, `internal/agent/tools/registry/agent_note_test.go:13-63`).

**Layout example (`internal/wiki/`):**
```
alias_index.go            alias_index_test.go
dedup.go                  dedup_test.go
graph_index.go            graph_index_test.go
godclass_test.go          (pure test file; god-class regression net)
live_memory_hygiene_test.go  (//go:build live_wiki — opt-in)
```

## Test Structure

**Pattern from `internal/agent/loop_test.go:17-37`:**

```go
func TestRunLoopNoToolCallsReturnsAssistantText(t *testing.T) {
    state := newFakeLoopState()
    client := &fakeLoopClient{responses: []ChatResponse{{Response: llm.Response{Content: "done"}}}}

    result, err := runLoop(context.Background(), client, ToolExecutorFunc(func(context.Context, []llm.ToolCall) ExecutionSummary {
        t.Fatal("executor should not run")
        return ExecutionSummary{}
    }), state, Options{MaxIterations: 3})
    if err != nil {
        t.Fatalf("runLoop returned error: %v", err)
    }
    if result.Text != "done" {
        t.Fatalf("Text = %q, want done", result.Text)
    }
    if result.Stats.LLMCalls != 1 || result.Stats.ToolCalls != 0 {
        t.Fatalf("stats = %+v", result.Stats)
    }
}
```

**Patterns:**
- Arrange → Act → Assert with no framework wrapper.
- `t.Fatalf(format, args...)` for terminal failures; `t.Errorf` for non-terminal observations.
- `%q` for strings (gives quoted, escaped output in the diff).
- Subtests via `t.Run(name, func(t *testing.T) { ... })` for table-driven cases (`internal/api/auth_test.go:200`).
- `t.Parallel()` used sparingly — 9 files (`grep -rln "t.Parallel" internal/`); shared SQLite state inhibits parallelism for most integration paths.

**Helpers:**
- `internal/testutil/dbhelpers.go` is the only shared test helper package. Two functions: `TempDBPath(t)` and `OpenTestDB(t, migrateFunc)` — opens a per-test SQLite DB and registers cleanup. Used everywhere a test needs a real DB.
- `logging.NewNopLogger()` (`internal/logging/zap_slog.go:157-159`) returns a discard logger for tests that don't care about output.

## Mocking Philosophy

**Mocks live in `_test.go` files. Never a `/mocks` directory.**

The repo uses hand-rolled fakes that implement a narrow interface defined right next to the consumer. Example: `agent_note.go` defines `agentNoteStore` as a 4-method interface; `agent_note_test.go` provides `fakeAgentNoteStore` that satisfies it with a `map[string]string` (`internal/agent/tools/registry/agent_note.go:13-19`, `agent_note_test.go:13-51`).

**What to mock:**
- Network I/O (LLM clients, HTTP endpoints, SearXNG, Mistral OCR, Qdrant).
- Process boundaries (`exec.Command` for `sqlite3`, MCP stdio transports).
- Time (`time.Now()` injected as a function field on the unit under test).

**What NOT to mock (CLAUDE.md hard rules):**
- **SQLite.** Use `internal/testutil.OpenTestDB(t, migrations.Run)` to get a fresh DB per test. The schema runs the real migrations. ~75 test files use `t.TempDir` for filesystem fixtures, and dozens spin up a real `*sql.DB` via the test helper.
- **The filesystem.** Use `t.TempDir()`. Wiki tests write real `.md` files and call real `os.Rename`.
- **The agent loop.** Tests inject a fake `ChatClient` returning canned `llm.Response` sequences (`fakeLoopClient`, `internal/agent/loop_test.go:18-19`) but run the real `runLoop`.
- **The wiki Store, agentnote.Store, identity store, memoryindex.Store.** Always real, backed by a temp SQLite or temp dir.
- **Probes never mock anything.** They hit the running binary over HTTP and verify SQLite + filesystem ground truth (`cmd/probe_chat/main.go:6-15`).

**Pattern for narrow interface mocks:**
```go
// production
type agentNoteStore interface { Get(...); Set(...); Append(...); Clear(...) }

// in _test.go
type fakeAgentNoteStore struct { notes map[string]string }
func (f *fakeAgentNoteStore) Get(...) ...
```

This style is preferred over codegen tools because the interface stays minimal (only the methods the consumer actually calls) and the fake stays in the test file where it's used.

## Probe Scripts (canonical "real artifact" verification)

Probes are the **CLAUDE.md-mandated** path for verifying Aura's runtime behavior. They are NOT unit tests — they hit a running Aura instance and cross-check every claim against ground truth. Per CLAUDE.md: `tool_calls: N` is necessary but never sufficient; the model can call a tool correctly and still hallucinate around the result.

**Available probes:**

| Probe | Purpose | Source |
|-------|---------|--------|
| `cmd/probe_chat` | Canonical E2E chat pipe. Runs ~50+ named cases against `/api/chat`, verifies SQLite + filesystem + API. | `cmd/probe_chat/main.go`, `cmd/probe_chat/cases.go` (1511 LOC of cases) |
| `cmd/probe_telegram_ui` | CDP-based real-Telegram-Web E2E. Attaches to a Chrome running with `--remote-debugging-port=9222`, sends a prompt, polls the message bubble, asserts the progressive-edit sequence. | `cmd/probe_telegram_ui/main.go` |
| `cmd/probe_doc` | Document-generation probes (xlsx/docx/pdf). | `cmd/probe_doc/` |
| `cmd/probe_ingest_e2e` | Source ingestion pipeline E2E. | `cmd/probe_ingest_e2e/` |
| `cmd/probe_reasoning` | Reasoning / chain-of-thought probes. | `cmd/probe_reasoning/` |
| `cmd/probe_webfetch` | Web fetch + summarize probe. | `cmd/probe_webfetch/` |

**Case structure (`cmd/probe_chat/types.go:19-29`):**

```go
type Case struct {
    Name     string
    Category string                                   // smoke-tier categorization
    Prompt   string
    PromptFn func() string                            // late-bound prompt
    ThreadID string
    Setup    func(env *Env) error
    Verify   func(reply ChatReply, env *Env) []string // required
    Metrics  func(reply ChatReply, env *Env) map[string]any
    Cleanup  func(env *Env)
}
```

**Verifier contract:**
- `Verify` returns one string per assertion violation. Empty slice = PASS.
- Verifiers MUST check ground truth — examples from `cmd/probe_chat/cases.go`:
  - **Reply substring** — `strings.Contains(strings.ToLower(r.Reply), strings.ToLower(taskName))`.
  - **DB row exists** — `env.fetchTask(taskName)` returns kind/status from the live `/api/tasks` endpoint (`cases.go:72-76`).
  - **Wiki page bytes** — `/api/wiki/page?slug=…` fetched back; assertions on frontmatter + body.
  - **Operational bounds** — `r.ToolCalls != 0` rejected for greetings; latency budget checked when relevant.
- The same `Verify` slice surfaces missing tool-call counts, wrong DB state, and content-mismatch all uniformly.

**Smoke tier categories (`cmd/probe_chat/main.go:63-81`):**
`tools-files`, `tools-skills`, `tools-memory`, `tools-source`, `tools-swarm`, `tools-web`, `tools-scheduler`, `tools-agent-note`, `tools-mcp`, `tools-dev`, `tools-sandbox`, `tools-registry`, `channels-web`, `channels-telegram`, `failure-modes-phantom`, `failure-modes-budget`, `markitdown`.

**Run:**
```bash
go run ./cmd/probe_chat                                  # all cases
go run ./cmd/probe_chat -case schedule-reminder          # single case
go run ./cmd/probe_chat -smoke=markitdown                # category subset
go run ./cmd/probe_chat -prompt "..." -raw               # ad-hoc one-shot
go run ./cmd/probe_chat -json                            # machine-readable output
```

**Env config (`cmd/probe_chat/main.go:39-44`):**
- `AURA_CHAT_URL` = `http://localhost:18080/api/chat`
- `AURA_CHAT_TOKEN` = bearer (required for non-loopback)
- `AURA_DB_PATH` = `./data/aura.db` (read-only)
- `AURA_API_BASE` = `http://localhost:18080/api` (for wiki ground truth)
- `AURA_PROBE_DB_DOCKER=0` to disable the docker-exec sqlite path (`cmd/probe_chat/live_db.go:22-27`).

**Docker SQLite access for in-container ground truth (`cmd/probe_chat/live_db.go:29-47`):**

```go
args := []string{"compose", "exec", "-T", "aura", "sqlite3", "-readonly", "-json",
                 "/data/aura.db", query}
exec.Command("docker", args...)
```

Because the bind-mount on Windows is unreliable (per memory `feedback_sqlite_wal_windows_corruption`), the probe defaults to `docker compose exec aura sqlite3` for read paths, and uses `OpenTestDB`-style write helpers for fixture seeding only.

## Bench / Quality Corpus

**`bench_dataset.json`** — 188 mined (query, tools[]) pairs from real conversations, 47% IT / 53% EN. Per memory note `reference_bench_dataset_in_tmp`, the corpus currently lives in `D:/tmp/` (out-of-tree) pending sanitization + alias-map work before promotion to the repo. It is the Phase-RAG Layer-1 seed.

**Live bench harness:** `cmd/quality_bench/main.go` (790 LOC; baselined in `.file-size-baseline.txt`).

**Strict-vs-loose pass discipline (CLAUDE.md "Lessons learned 2026-05-21"):**
A `reply.contains(expected_substring)` PASS is NOT a pass when the query took 42 s with 9 tool calls. Strict pass = substring AND latency-under-budget AND tool-count-under-budget AND no-forbidden-tool-leak. Pre-2026-05-21 the bench was loose-only; re-grading the same run moved 20/20 → 3/20. Always report BOTH numbers.

## CI Workflows

**`.github/workflows/ci.yml`** — runs on push/PR to `master`/`main`. Three parallel jobs:

### Job: `test` (Go test + Phase 2 guards)
Sequential steps on `ubuntu-latest`:
1. **Checkout** (`actions/checkout@v4`).
2. **Set up Go** from `go.mod` directive (currently `go 1.26.2`) with cache.
3. **Depguard architecture boundary** — `golangci/golangci-lint-action@v8` v2.12.2 with `--enable-only=depguard --new-from-rev=HEAD~1 ./...`. Only new violations vs the previous commit are reported.
4. **File-size linter** — `bash scripts/check-file-size.sh .file-size-baseline.txt`. Fails if any baselined file grew or any new non-test file exceeds 600 LOC.
5. **`go vet ./...`** — basic semantic checks.
6. **`go build ./...`** — every package must compile.
7. **Phase 2 regression guards** — `./scripts/test-heuristic-removal.ps1` (PowerShell). Greps every `.go/.ts/.tsx` for forbidden heuristic identifiers (`looksLikeWikiYAML`, `isLikelyWikiPage`, `detectWikiFromText`, `parseWikiFromAssistant`). Adding a pattern documents the regression contract.
8. **`go test -race -count=1 ./...`** — full test suite under the race detector, no test-result caching.

### Job: `deadcode`
1. **Install** `golang.org/x/tools/cmd/deadcode`.
2. **Run** `deadcode -test ./cmd/... ./internal/...` — whole-program analysis across commands plus internal package tests. Keeps command-only entrypoints and test-covered package contracts from being misclassified.
3. **Compare** finding count against `docs/deadcode-baseline-2026-05-22.json`. Fails if `NEW > BASELINE`.

### Job: `frontend` (build only — no tests yet)
1. **Set up Node 22** with `npm` cache keyed on `web/package-lock.json`.
2. **`npm install --no-audit --no-fund`** in `web/`. Uses `install` (not `ci`) due to cross-platform lock drift between Windows dev (no `@emnapi/*` Linux WASM bindings) and Linux CI — `npm ci` will fail. Tracked in memory `feedback_npm_lock_cross_platform_drift`.
3. **`npm run build`** — Vite production build. Output goes to `internal/api/dist/` and is `//go:embed`ed.

**`.github/workflows/docker-image.yml`** — tag-triggered (`v*`) + manual dispatch. Builds & pushes 4 images to `ghcr.io/chetto1983/`:
- `aura` (linux/amd64 + linux/arm64).
- `aura-whisper` (amd64 only — arm64 cross-compile of whisper.cpp v1.8.4 is broken under QEMU).
- `aura-pocket-tts` (linux/amd64 + linux/arm64).
- `aura-markitdown` (linux/amd64 + linux/arm64).

QEMU + Buildx; cache via `type=registry,...,mode=max`; SBOM + provenance attestations enabled.

## Linting & Static Analysis Stack

| Tool | Scope | Where |
|------|-------|-------|
| `go vet ./...` | Stdlib semantic checks | CI (`ci.yml:33-34`), Makefile (`vet` target). |
| `go fmt ./...` | Formatting | Makefile (`fmt` target). |
| `golangci-lint run` | Only `depguard` enabled. Architecture boundary contract from `prd.md §9`. | `.golangci.yml`; CI uses `--new-from-rev=HEAD~1`; lefthook uses `--new-from-rev=HEAD`. |
| `dupl -t 60` | Clone detection on staged Go files (threshold 60 tokens) | `lefthook.yml:18-22`; skipped silently when `dupl` not installed. |
| `deadcode -test` | Whole-program unreachable production functions | CI `deadcode` job; baselined to `docs/deadcode-baseline-2026-05-22.json`. |
| `scripts/check-file-size.sh` | 600-LOC cap with grandfathered baseline | CI + lefthook. |
| `scripts/test-heuristic-removal.ps1` | Banned-pattern grep (4 identifiers) | CI Phase 2 guard. |
| ESLint 10 + typescript-eslint 8.58 | Frontend lint | `web/eslint.config.js`; `npm --prefix web run lint`. |
| `npm --prefix web run i18n:check` | en/it locale parity + placeholder consistency | `web/scripts/check-i18n.mjs`. |
| `npm --prefix web run test:timezone` | Time-zone sanity for date pickers | `web/scripts/check-timezone.mjs`. |
| `go run ./cmd/module_health` | Cross-package coupling report | Makefile (`module-health` target). |

## Pre-Commit Gate (`lefthook.yml`)

Opt-in (`lefthook install` after clone). Bypass: `git commit --no-verify` (emergency only).

```yaml
pre-commit:
  parallel: false
  commands:
    lint:      golangci-lint run --new-from-rev=HEAD ./...
    dupl:      dupl -t 60 {staged_files}   (skipped when dupl absent)
    file-size: bash scripts/check-file-size.sh .file-size-baseline.txt
```

## Frontend Tests

**Unit tests (Vitest):**
- 5 test files in `web/src/`:
  - `web/src/components/SettingsPanel.test.ts`
  - `web/src/components/chat/AudioPlayback.helpers.test.ts`
  - `web/src/components/chat/PendingQuestion.helpers.test.ts`
  - `web/src/components/chat/SourceAttachmentAdapter.test.ts`
  - `web/src/components/chat/ToolCallComponent.helpers.test.ts`
- Pattern: pure-function helpers extracted into `<Component>.helpers.ts` and tested in `<Component>.helpers.test.ts` or `<Component>.test.ts`. Components themselves are not unit-tested at the React level; E2E covers integration.
- Example (`web/src/components/SettingsPanel.test.ts:1-13`):

```typescript
import { describe, expect, it } from 'vitest';
import { computeSourceBadge } from './SettingsPanel.helpers';

describe('computeSourceBadge', () => {
  it('returns "saved" badge for db source with non-secret item', () => {
    const badge = computeSourceBadge({ ...base, is_secret: false, active_value: '' }, false, t);
    expect(badge.label).toBe('settings.badge.saved');
  });
});
```

**E2E (Playwright, `web/e2e/`):**
- `all-pages.spec.ts` — every route loads.
- `confirm-modal.spec.ts`, `form-labels.spec.ts` — UI primitives.
- `dashboard.spec.ts`, `settings.spec.ts`, `tasks-and-cleanup.spec.ts`, `source-universal-upload.spec.ts`, `summaries-evidence.spec.ts`, `mcp-connectors.spec.ts`, `other-pages.spec.ts` — feature specs.
- `fixtures.ts` shared setup.
- Config (`web/playwright.config.ts`):
  - `fullyParallel: false` + `workers: 1` — dashboard reads shared SQLite, parallelism serialized.
  - `baseURL = process.env.AURA_DASHBOARD_URL ?? 'http://localhost:8081'`.
  - Bearer token via `AURA_E2E_TOKEN` env (mint via `request_dashboard_token` Telegram tool or write to `web/.e2e-token`).
  - CI: `retries: 2`, `reporter: 'github'`. Local: `retries: 0`, `reporter: 'list'`.
  - Traces / screenshots / video on failure only.
- Run: `npm --prefix web run e2e` (= `playwright test`).

**Frontend tests are NOT in CI yet** — the `frontend` job in `ci.yml` only runs `npm run build`. Vitest and Playwright must be invoked manually.

## E2E via Telegram CDP

**Setup (memory `reference_telegram_cdp_e2e_setup`):**
1. `scripts/launch_chrome_cdp.ps1` boots Chrome with `--remote-debugging-port=9222 --user-data-dir=%USERPROFILE%\.chrome-cdp-profile`.
2. Log into `https://web.telegram.org/a/` once in that Chrome.
3. Open the chat with `Aura_bot` (`@Aura_bot`, ID `#8700386532`).
4. Run `go run ./cmd/probe_telegram_ui -prompt "<text>" -timeout 60s`.

**What the probe asserts:**
- The `"🛠 Sto lavorando…"` status header appeared at least once (progressive edit hit).
- A known tool name (`search_memory` / `web_search` / `file`) appeared at least once in the bubble sequence.
- The final body is a clean answer with no chrome (`🛠` / `🧠` stripped before terminal state).

**Why CDP attach, not launch:** The user is already logged in. A fresh Chrome instance would need a new login + 2FA.

## Live / Integration / Opt-In Tests

**Build-tagged live tests** (excluded from default `go test ./...`):

| File | Tag | Run with |
|------|-----|----------|
| `internal/storage/sources/ingest/live_test.go` | `live_ingest` | `go test -tags live_ingest ./internal/storage/sources/ingest/` |
| `internal/storage/sources/ocr/live_test.go` | `live_ocr` | `go test -tags live_ocr ./internal/storage/sources/ocr/` |
| `internal/wiki/live_memory_hygiene_test.go` | `live_wiki` | `go test -tags live_wiki ./internal/wiki/` |

These hit real external dependencies (Mistral OCR API, full wiki on disk, LLM ingest pipeline) and require credentials.

**Platform-specific tests:**
- `internal/tray/tray_windows.go` / `tray_other.go` are split via build tags. Tests run on the matching platform.

## Common Patterns

**Async testing:**
- `context.Background()` is the default in tests. Use `context.WithTimeout(t.Context(), …)` only when timing is the assertion subject.
- Race detector enforces synchronization. The CI gate is `go test -race -count=1 ./...` (`ci.yml:44`).

**Error testing:**
```go
if err == nil {
    t.Fatalf("expected error, got nil")
}
if !errors.Is(err, ExpectedSentinel) {
    t.Fatalf("err = %v, want %v", err, ExpectedSentinel)
}
```

For custom error types with payload:
```go
var conflict *ConflictError
if !errors.As(err, &conflict) {
    t.Fatalf("err = %v, want *ConflictError", err)
}
if conflict.Slug != "expected-slug" { ... }
```

**Capturing logs in tests** (`internal/agent/loop_test.go:39-65`):
```go
var logs bytes.Buffer
logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
_, err := runLoop(ctx, client, executor, state, Options{Logger: logger})
if !strings.Contains(logs.String(), "agent: max_iterations_capped") {
    t.Fatalf("missing warning log")
}
```

**Real-DB pattern** (`internal/testutil/dbhelpers.go:21-34`):
```go
db := testutil.OpenTestDB(t, migrations.Run)   // full migrated SQLite, auto-cleanup
```

**Real-filesystem pattern:**
```go
dir := t.TempDir()                              // auto-cleanup after test
store := wiki.NewStore(dir, ...)
_ = store.WritePage(ctx, page)                  // writes real .md to disk
```

**Test fixtures via embed:**
```go
//go:embed fixtures
var fixturesFS embed.FS
//go:embed tests/fixtures/*.json
var tjFixtures embed.FS
```
Examples: `internal/storage/sources/ingest/fixtures_test.go:13`, `internal/tokenjuice/fixture_test.go:13`.

## Coverage Gaps

**Not enforced numerically.** No `go test -cover` gate in CI; coverage reports are not collected. The bar is functional (every named probe case must pass) + structural (race-detector clean, no new dead code, no new file-size violation).

**Visible weak spots:**
- **Frontend has no CI test job.** Vitest and Playwright exist but neither runs in `ci.yml` (only `npm run build`). A `frontend-test` job is missing.
- **No `frontend lint` CI step.** ESLint config exists but is never invoked by CI.
- **Probes are not in CI.** `cmd/probe_chat` requires a running Aura instance and live LLM credentials; it runs locally and via the GSD QA-pipeline skill (`.agents/skills/aura-qa-pipeline/`), not in GitHub Actions. Documented as a known gap; CI's race-detector test suite is the production gate.
- **Audio / TTS sidecars** (`aura-whisper`, `aura-pocket-tts`) are exercised only via integration probes (`probe_chat -smoke=…` with `-tts` flag). No unit-level test for the sidecar binary contracts.
- **Phase 2 regression guard uses PowerShell** — runs in CI via `pwsh` but cannot be run from a non-Windows dev box without installing PowerShell Core.

**By-design "not tested in unit" areas:**
- Telegram transport (`internal/telegram/`) — covered by `cmd/probe_telegram_ui` CDP probe.
- The actual LLM behavior — covered by `cmd/probe_chat` and `cmd/quality_bench`, never by unit mocks (CLAUDE.md "VALIDATE WITH VERIFIED BENCHMARKS").
- The Telegram bubble progressive-edit UX — covered only by CDP probe.

---

*Testing analysis: 2026-05-27*
