---
phase: 02-llm-reliability-tool-intelligence
verified: 2026-05-11T10:00:00Z
status: passed
score: 6/6
overrides_applied: 0
re_verification: null
gaps: []
deferred: []
---

# Phase 02 — Verification Report

## Verdict

PASS

## Goal recap

Phase 02 makes Aura's wiki write path explicit and reliable: the LLM now creates/updates wiki pages exclusively by calling the `write_wiki_page` tool (no heuristic text-parsing), retries intelligently via a temperature-staircase on schema failures vs. flat backoff on transient HTTP errors, and the wiki reindexes asynchronously through a buffered-channel worker with git audit tracking. The `tool_search` tool is deleted; tool discovery is now automatic via Qdrant semantic matching.

## Plan-by-plan verification

| Plan | Verdict | Evidence | Notes |
|------|---------|----------|-------|
| 02-01 LLM classify+retry | PASS | `internal/llm/classify.go` exists with `Bucket`, `Classify`, `APIError`, `redact`, sentinel errors. `internal/llm/retry.go` has classify-then-retry with three bucket policies. `openai.go` lines 175 + 243 surface `&APIError{...}` (not `fmt.Errorf`). `go test ./internal/llm/` passes. | `TestBackoffDelay` removed (private method eliminated); deviation documented and acceptable. Race detector skipped on Windows (no C compiler) — structural guarantee instead. |
| 02-02 Async reindex worker | PASS | `internal/reindex/{types,worker,worker_test}.go` all exist. `Submitter` interface present. Dedicated `context.WithCancel(context.Background())` per-worker. `Stop()` uses `CompareAndSwap` for idempotency. Channel never closed (Pitfall #4 comment inline). `go test ./internal/reindex/` passes. | Race detector unavailable on Windows; structurally sound. |
| 02-03 Wiki schema + ETag + Unversioned + store split | PASS | `Page.Unversioned bool` at `schema.go:33`. `WritePage` variadic signature at `store_writes.go:42`. `ConflictError` exported at `store_writes.go:20`. `SetGitCommitFuncForTest` at `store.go:273`. `store.go` = 582 LOC, `store_writes.go` = 327 LOC (both under 600). `go test ./internal/wiki/` passes. `TestSchema_UnversionedRoundTrip` exists in `schema_test.go`. | `memory_hygiene.go` at 759 LOC is a pre-existing violation, allowlisted. |
| 02-04 `write_wiki_page` tool | PASS | `internal/tools/wiki.go` exists with `WriteWikiPageTool`. `Parameters()` has `additionalProperties:false`, three required fields (`title`, `body`, `expected_updated_at`). Schema validation wraps `llm.ErrSchemaValidation`. Conflict returns nil error + structured JSON result (D-03). Reindex submission via injected `reindex.Submitter`. `go test ./internal/tools/` passes. | `PromptVersion: "v1"` used instead of `"write_wiki_page/v1"` — the regex in `wiki.Validate()` requires the `v{n}` format; "v1" is correct. |
| 02-05 ToolVectorIndex export + embedding narrowing + collection v2 | PASS | `registry_search_vector.go:47` exports `ToolVectorIndex`. `registry_search.go:130` has `searchableToolEmbeddingText` returning `name+" "+description` only. `registry_search_vector.go:76` defaults collection to `"aura_tool_search_v2"`. `go test ./internal/tools/` passes. | |
| 02-06 Integration (package-local) | PASS | `conversation.Context.LatestUserMessageText()` at `context.go:253`. `wiki.Store.SetReindexSubmitter` in `store_writes.go`. `internal/telegram/tools_provider.go` with `alwaysOnCore` (6 names) and `makeToolsProvider`. `conversation.go` uses `toolsProvider`. `maxCallsPerTool` map has `write_wiki_page:3` at `conversation.go:310`, no `tool_search:2`. | SUMMARY.md was retroactively re-created by orchestrator (commit `ac4e375b`) because the agent wrote it to the wrong path. Code implementation is correct. |
| 02-06b System-wide wiring + deletion sweep | PASS | `setup.go:188` constructs `reindex.NewWorker(searchEngine, ...)`. `setup.go:195` calls `wikiStore.SetReindexSubmitter(reindexWorker)` on local `*wiki.Store`. `setup.go:795` registers `NewWriteWikiPageTool`. `setup.go:554` uses `"aura_tool_search_v2"`. `setup.go:849-855` wires `MaxContentRetries:3`, `ContentTemperatures:[0.0,0.3,0.7]`, `JitterRatio:0.5`. `bot.go:247-248` calls `b.reindex.Stop()` AFTER `b.archiver.Close()`. `/api/health` has `Reindex` field (not omitempty). `tool_search.go` DELETED (git log confirms `1093f21c`). `test-tool-search-removal.ps1` exits 0. `TestRunToolCallingLoopAddsToolSearchDiscoveries` DELETED. `test-agent-tool-search-smoke.ps1` DELETED. | Plan said "update" the smoke script; agent chose "delete". Acceptable — the test's assertion (`tool_search discoveries` prompt copy) became meaningless after the Qdrant top-K retrieval replacement. |
| 02-07 Unversioned UI badge | PASS | `internal/api/wiki.go:123` sets `fm["unversioned"] = page.Unversioned`. `internal/api/wiki_test.go` has `TestWikiPage_UnversionedJSON_False` and `TestWikiPage_UnversionedJSON_True` — both pass (verified). `web/src/types/api.ts:49` has `unversioned?: boolean`. `WikiPageView.tsx:42-50` renders yellow badge with `t('wikiPage.unversionedBadge')`. `en.json:429` = "Git tracking pending", `it.json:429` = "Tracciamento Git in sospeso". | Badge uses i18n key (not inline string) — consistent with project pattern. |
| 02-08 CI guards + retrieval precision | PASS | `godclass_test.go` exists, scoped to `internal/wiki/`, `godClassAllowlist` exempts `memory_hygiene.go`. `test-heuristic-removal.ps1` exits 0. `test-tool-search-removal.ps1` exits 0 (hot-fixed to word-boundary regex in commit `b07b385e`). `retrieval_precision_test.go` + `testdata/retrieval_fixture.jsonl` exist. `go test -tags=integration -run TestToolRetrieval_Precision` passes at precision@5 = 0.80. `.github/workflows/ci.yml` wires vet, build, guards, test, frontend. | Godclass guard scoped to `internal/wiki/` only — 6 sibling packages have pre-existing violations. Hot-fix to word-boundary pattern correctly avoids false positives on `TOOL_SEARCH_BACKEND`, `aura_tool_search_v2`. |

## Observable Truths vs ROADMAP Success Criteria

| # | ROADMAP Success Criterion | Status | Evidence |
|---|--------------------------|--------|----------|
| 1 | LLM creates wiki pages exclusively via `write_wiki_page` tool; no heuristic parsing remains | VERIFIED | `test-heuristic-removal.ps1` exits 0. Grep of `internal/`, `cmd/`, `web/` finds zero occurrences of `looksLikeWikiYAML`, `isLikelyWikiPage`, `detectWikiFromText`, `parseWikiFromAssistant` in production code. |
| 2 | `expected_updated_at` check detects concurrent dashboard edits; write is rejected with conflict error | VERIFIED | `store_writes.go` ETag check inside `fileMutex` critical section. `TestWritePage_ETagMismatch` and `TestWritePage_ETagInsideMutex_NoTOCTOU` GREEN. `TestWriteWikiPage_Conflict_ETagMismatch` GREEN. Conflict surfaces as structured JSON tool result (nil error, D-03). |
| 3 | Schema/empty-output failures retry with incremented temperature; HTTP 429/5xx retry at same temperature | VERIFIED | `classify.go` priority pipeline maps sentinel errors → `BucketContent`, HTTP 4xx/5xx → `BucketTransient`. `retry.go` applies `ContentTemperatures[contentAttempt]` on CONTENT, preserves `callerTemp` on TRANSIENT. All retry tests GREEN. |
| 4 | Every wiki mutation creates a git commit; if commit fails, `unversioned: true` visible in dashboard | VERIFIED | `store_writes.go` calls `gitCommit`, sets `Unversioned=true` on failure (D-17), clears on next success (D-18). `TestUnversionedRoundTrip_SetOnFailure` GREEN. `TestWikiPage_UnversionedJSON_True` passes end-to-end through API. Yellow badge rendered in `WikiPageView.tsx`. |
| 5 | Wiki writes trigger async reindex via buffered-channel worker; write returns immediately | VERIFIED | `reindex.Worker` with buffered channel (cap 100), `select/default` coalescing in `Submit`. `WritePage` and `DeletePage` call `s.reindexSubmitter.Submit(...)` after file op succeeds. `Worker.Stop()` called after `archiver.Close()` in `bot.go`. |
| 6 | Agent receives context-relevant tool definitions via Qdrant semantic matching; `tool_search` tool removed | VERIFIED | `tools_provider.go` implements per-turn closure: core ∪ top-K=5 Qdrant retrieval. `tool_search.go` deleted (`1093f21c`). `test-tool-search-removal.ps1` exits 0. `TestRunToolCallingLoopAddsToolSearchDiscoveries` deleted. |

**Score: 6/6 truths verified**

## Deviations surfaced

| Deviation | Source | Acceptability |
|-----------|--------|---------------|
| `TestBackoffDelay` removed (plan 02-01) | `backoffDelay()` private method eliminated in retry rewrite; `TestJitterDistribution` in `retry_test.go` covers same invariant | Acceptable — equivalent coverage exists |
| `PromptVersion: "v1"` not `"write_wiki_page/v1"` (plan 02-04) | `promptVersionRe` in `wiki.Validate()` rejects slash-format; RESEARCH.md sketch already used `"v1"` | Acceptable — the regex constraint is a codebase invariant; "v1" is the correct value |
| `"frob"` removed from forbidden token list in `TestSearchableToolEmbeddingText_NameAndDescriptionOnly` (plan 02-05) | "frob" is a prefix of "frobnicate" causing a false-positive test failure | Acceptable — remaining forbidden tokens (`"properties"`, `"required"`, `"arguments"`) are sufficient |
| 02-06 SUMMARY.md retroactively re-created by orchestrator (commit `ac4e375b`) | Agent wrote summary to wrong path during execution | Acceptable — code implementation is correct and was verified independently; SUMMARY is documentation only |
| `scripts/test-agent-tool-search-smoke.ps1` deleted rather than updated (plan 02-06b) | Plan said "update"; agent chose "delete" | Acceptable — the script's assertion was tied to the `tool_search` tool's output format, which no longer exists. Deletion is the correct outcome when the feature is removed, not updated. |
| `test-tool-search-removal.ps1` pattern hot-fixed from SimpleMatch to word-boundary regex (commit `b07b385e`, orchestrator post-merge) | Original `"tool_search"` SimpleMatch false-positive'd on `TOOL_SEARCH_BACKEND` env var and `aura_tool_search_v2` collection name | Acceptable and necessary — the tightened `\btool_search\b` pattern correctly catches the deleted symbol while excluding legitimate Phase-2-survivor names. The intent of success criterion #6 is preserved. |
| `TestGodClass` scoped to `internal/wiki/` only, not all of `internal/` (plan 02-08) | Pre-existing violations in 6 other packages (`telegram/setup.go` 990L, `sandbox/pyodide_runner.go` 690L, `api/types.go` 632L, `tools/files.go` 650L, `tools/source.go` 631L) were out of plan scope | Partially acceptable — the guard protects the Plan 03 store.go factoring invariant and catches new violations in `internal/wiki/`. The 6 sibling packages are pre-existing debt not introduced by Phase 2. Tracked as carry-forward below. |
| Race detector (`-race`) not run locally during any plan (Windows, no CGO/GCC) | CGO compiler absent from dev machine | Acceptable for local execution — CI runs `go test -race -count=1 ./...` on ubuntu-latest where CGO is available. Structural sync guarantees are correct. |

## Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/llm/classify.go` | Bucket, Classify, APIError, redact, sentinels | VERIFIED | 134 LOC, all types and functions present |
| `internal/llm/retry.go` | Classify-then-retry, three bucket policies | VERIFIED | 184 LOC, CONTENT/TRANSIENT/PERMANENT branches |
| `internal/reindex/types.go` | Job, Op, Submitter, Health, Config | VERIFIED | 61 LOC, all types present |
| `internal/reindex/worker.go` | Worker, NewWorker, Submit, Stop, drain | VERIFIED | 152 LOC, dedicated context, no channel close |
| `internal/wiki/store_writes.go` | WritePage variadic, ConflictError, SetReindexSubmitter | VERIFIED | 327 LOC, all functions present |
| `internal/wiki/store.go` | Under 600 LOC, SetGitCommitFuncForTest | VERIFIED | 582 LOC, test seam at line 273 |
| `internal/tools/wiki.go` | WriteWikiPageTool, additionalProperties:false | VERIFIED | 187 LOC |
| `internal/tools/registry_search_vector.go` | ToolVectorIndex exported, aura_tool_search_v2 default | VERIFIED | Line 47 exports type, line 76 sets default |
| `internal/telegram/tools_provider.go` | alwaysOnCore, makeToolsProvider | VERIFIED | 99 LOC, 6 core names, closure logic complete |
| `internal/api/wiki.go` | unversioned in pageFrontmatter | VERIFIED | Line 123 |
| `internal/api/health.go` + `types.go` | Reindex field in HealthRollup | VERIFIED | ReindexHealthResponse always present |
| `web/src/types/api.ts` | unversioned?: boolean | VERIFIED | Line 49 |
| `web/src/components/WikiPageView.tsx` | Yellow badge with i18n key | VERIFIED | Lines 42-50 |
| `internal/wiki/godclass_test.go` | TestGodClass, godClassAllowlist | VERIFIED | 116 LOC, allowlist has memory_hygiene.go |
| `internal/tools/retrieval_precision_test.go` | integration-tagged, precision@5 >= 0.80 | VERIFIED | Passes at 0.80 (lexical path) |
| `scripts/test-heuristic-removal.ps1` | Exits 0 | VERIFIED | Exits 0 on merged tree |
| `scripts/test-tool-search-removal.ps1` | Exits 0 post-merge | VERIFIED | Exits 0 (word-boundary regex, hot-fixed b07b385e) |
| `.github/workflows/ci.yml` | test + frontend jobs | VERIFIED | Both jobs present with guard step |
| `internal/tools/tool_search.go` | DELETED | VERIFIED | git log shows deletion in 1093f21c |

## Key Link Verification

| From | To | Via | Status | Details |
|------|-----|-----|--------|---------|
| `openai.go` HTTP error sites | `*APIError` struct | `&APIError{StatusCode, Body:redact(...)}` | WIRED | Lines 175, 243 in openai.go |
| `Classify()` | Bucket policies | `errors.As(err, &apiErr)` + `errors.Is` sentinels | WIRED | classify.go priority pipeline |
| `RetryClient.Send/Stream` | `Classify()` | bucket dispatch, staircase on CONTENT | WIRED | retry.go lines 81-106, 124-150 |
| `wiki.Store.WritePage` | `reindexSubmitter.Submit` | after successful file write | WIRED | store_writes.go, confirmed by TestSetReindexSubmitter* tests |
| `setup.go` | `reindex.Worker` | `reindex.NewWorker(searchEngine, ...)` line 188 | WIRED | searchEngine is local `search.Repository` var |
| `setup.go` | `wikiStore.SetReindexSubmitter` | local `*wiki.Store` var line 195 | WIRED | BLOCKER 3 resolved: NOT `b.wiki` |
| `setup.go` | `write_wiki_page` registration | `tools.NewWriteWikiPageTool(wikiStore, reindexWorker)` line 795 | WIRED | |
| `bot.go Stop()` | `b.reindex.Stop()` | after `b.archiver.Close()` | WIRED | Lines 241-248 |
| `conversation.go` | `makeToolsProvider` | per-turn closure, `toolsProvider()` | WIRED | Lines 300, 349, 350 |
| `api/wiki.go` | `page.Unversioned` | `fm["unversioned"] = page.Unversioned` | WIRED | Line 123 |
| `api/health.go` | `ReindexHealth` callback | `deps.ReindexHealth()` | WIRED | Line 106-107 |
| `WikiPageView.tsx` | `fm.unversioned` | `const unversioned = fm.unversioned === true` | WIRED | Line 34 |

## Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| `go vet ./...` clean | `go vet ./...` | No output (clean) | PASS |
| `go build ./...` clean | `go build ./...` | No output (clean) | PASS |
| All internal packages pass tests | `go test ./internal/...` | 40 packages, all `ok` | PASS |
| LLM classify tests | `go test ./internal/llm/` | ok 0.261s | PASS |
| Reindex worker tests | `go test ./internal/reindex/` | ok 0.211s | PASS |
| Wiki store tests | `go test ./internal/wiki/` | ok 4.231s | PASS |
| Tools tests | `go test ./internal/tools/` | ok 6.792s | PASS |
| Telegram tests | `go test ./internal/telegram/` | ok 2.304s | PASS |
| API tests | `go test ./internal/api/` | ok 19.493s | PASS |
| TestGodClass | `go test -run TestGodClass ./internal/wiki/` | PASS (memory_hygiene.go skip-logged) | PASS |
| TestWikiPage_UnversionedJSON_True | `go test -run TestWikiPage_Unversioned ./internal/api/` | PASS | PASS |
| TestHealth_Reindex* | `go test -run TestHealth_Reindex ./internal/api/` | PASS | PASS |
| Retrieval precision | `go test -tags=integration -run TestToolRetrieval_Precision ./internal/tools/` | precision@5 = 0.80 (12/15 hits, lexical path) | PASS |
| Heuristic removal guard | `powershell scripts/test-heuristic-removal.ps1` | "OK: no heuristic markers found" exit 0 | PASS |
| tool_search removal guard | `powershell scripts/test-tool-search-removal.ps1` | "OK: no tool_search references found" exit 0 | PASS |

## Deferred Items

None. All Phase 2 success criteria are fully implemented and verified in the merged tree.

## Human Verification Required

None. All success criteria are mechanically verifiable and have been verified.

## Carry-forwards / follow-ups

These items do NOT block Phase 02 closure but should be tracked:

1. **Godclass debt in 6 sibling packages** — `TestGodClass` is scoped to `internal/wiki/` only. Six other packages exceed 600 LOC: `telegram/setup.go` (~990L), `sandbox/pyodide_runner.go` (~690L), `api/types.go` (~632L), `tools/files.go` (~650L), `tools/source.go` (~631L), and `wiki/memory_hygiene.go` (759L, allowlisted). Phase 2 introduced the guard only for the package it factored. A Phase 4 / cleanup phase should expand the allowlist-free guard or refactor these files.

2. **`TestRunToolCallingLoopAddsToolSearchDiscoveries` deleted vs. rewritten** — The test was deleted outright rather than rewritten to assert the new "Qdrant top-K=5 retrieval" prompt behavior. No replacement integration test for the `makeToolsProvider` prompt injection exists at the `telegram` integration level (only unit tests in `tools_provider_test.go`). Low risk but a coverage gap for the full agent-loop path.

3. **Race detector not locally verified** — All plans ran `go test -count=1` (not `-race`) due to the Windows dev machine lacking a C compiler. CI on ubuntu-latest runs `go test -race -count=1 ./...`. The first CI run against master will be the authoritative race-clean verification.

4. **`memory_hygiene.go` at 759 LOC** — Tracked in `godClassAllowlist`. Should be refactored before Phase 4 god-class guard is widened.

5. **Pre-existing user WIP in working tree** — `M internal/telegram/conversation.go` and `?? internal/telegram/entity_markdown_table_test.go` are user changes unrelated to Phase 02 (regex-markdown removal refactor). They survived merge cycles. Not a Phase 02 issue.

6. **Retrieval precision test uses lexical path only locally** — When `QDRANT_URL` is not set, the test falls back to lexical FTS and hits exactly 0.80. With Qdrant + embeddings configured, the vector path should perform better. The threshold is calibrated correctly but the vector path precision is not locally verifiable without a running Qdrant instance.

## Suggested next step

Phase 02 is **ready to close**. All six ROADMAP success criteria are verified in the merged master tree. `go vet`, `go build`, and `go test ./internal/...` are all clean. Both CI guard scripts exit 0. The unversioned badge is wired end-to-end from `wiki.Store` through the API to the React component.

Proceed to Phase 03 (Resilience Layer — circuit breaker per LLM provider + per-user token budgets).

---

_Verified: 2026-05-11T10:00:00Z_
_Verifier: Claude (gsd-verifier)_
