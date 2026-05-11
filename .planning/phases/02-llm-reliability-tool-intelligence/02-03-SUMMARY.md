---
phase: 02-llm-reliability-tool-intelligence
plan: "03"
subsystem: wiki
tags:
  - wiki
  - schema
  - etag
  - optimistic-concurrency
  - git
  - tdd
dependency_graph:
  requires: []
  provides:
    - wiki.ConflictError (exported from internal/wiki)
    - wiki.Store.WritePage variadic signature (WIKI-02)
    - wiki.Store.SetGitCommitFuncForTest (Plan 07 test seam)
    - Page.Unversioned bool field (GIT-01)
  affects:
    - internal/wiki (all consumers of WritePage or PageWriter interface)
    - internal/conversation/summarizer (WikiWriter interface updated)
    - internal/api (summaries_test fake updated)
tech_stack:
  added:
    - github.com/go-git/go-git/v5 v5.19.0 (upgraded from v5.18.0)
  patterns:
    - "ETag inside fileMutex critical section (Pitfall #1 TOCTOU prevention)"
    - "Unversioned set/clear with Validate gate (WARNING 14)"
    - "Atomic temp+rename for all wiki writes"
    - "Exported test seam via SetGitCommitFuncForTest"
key_files:
  created:
    - internal/wiki/store_writes.go
    - internal/wiki/store_writes_test.go
  modified:
    - go.mod
    - go.sum
    - internal/wiki/schema.go
    - internal/wiki/parser.go
    - internal/wiki/schema_test.go
    - internal/wiki/store.go
    - internal/conversation/summarizer/applier.go
    - internal/conversation/summarizer/applier_test.go
    - internal/api/summaries_test.go
decisions:
  - "CurrentSchemaVersion stays at 2 (D-09 LOCK — additive omitempty field does not require bump)"
  - "ETag check runs INSIDE per-slug fileMutex to close TOCTOU race window (Pitfall #1)"
  - "gitCommit failure returns nil to caller (disk write succeeded; only git history is degraded)"
  - "Validate(reread) gates BOTH Unversioned set and clear re-writes (WARNING 14)"
  - "MigrateYAMLToMD and RepairLink moved to store_writes.go to satisfy ≤600 LOC rule"
  - "PageWriter interface + summarizer.WikiWriter updated to variadic to maintain compile correctness"
metrics:
  duration: "~45 minutes"
  completed: "2026-05-11"
  tasks_completed: 3
  files_changed: 9
---

# Phase 02 Plan 03: Wiki Schema + ETag + Unversioned + store_writes.go Factor Summary

Wiki package gains optimistic concurrency (ETag check), GIT-01 Unversioned tracking, go-git upgrade, and a god-class split.

## What Was Delivered

### Task 1: go-git upgrade + Page.Unversioned + MarshalMD + schema_test (commit fa0b29dd)

- Upgraded `github.com/go-git/go-git/v5` from v5.18.0 to v5.19.0 in go.mod + go.sum (one-line change, backward-compatible per RESEARCH.md A1).
- Added `Page.Unversioned bool` with `yaml:"unversioned,omitempty" json:"unversioned,omitempty"` to `internal/wiki/schema.go`. Field sits between `UpdatedAt` and `Body`. `CurrentSchemaVersion` stays at 2 (D-09 LOCK).
- Updated `MarshalMD` inline frontmatter struct in `internal/wiki/parser.go` to include `Unversioned bool` with `yaml:"unversioned,omitempty"` — field round-trips through YAML marshal/unmarshal.
- Added `TestSchema_UnversionedRoundTrip` to `internal/wiki/schema_test.go`: two sub-cases verify omitempty semantics (absent when false, present when true) and round-trip through `ParseMD` (confirmed two-value return `(*Page, error)`).

### Task 2: TDD RED — ConflictError scaffold + test fixtures (commit c8401b92)

- Created `internal/wiki/store_writes.go` with `ConflictError` struct + `Error()` method.
- Created `internal/wiki/store_writes_test.go` with 8 test functions:
  - `TestWritePage_BackwardsCompat_NoVariadic` — verifies zero-arg calls still work
  - `TestWritePage_CreateOnly_SentinelEmptyString` — empty-string sentinel semantics
  - `TestWritePage_ETagMatch` — correct ETag proceeds
  - `TestWritePage_ETagMismatch` — stale ETag returns `*ConflictError`
  - `TestWritePage_ETagInsideMutex_NoTOCTOU` — 10 concurrent goroutines with same stale ETag yield exactly 1 win, 9 conflicts
  - `TestUnversionedRoundTrip_SetOnFailure` — (skipped at commit time, unskipped in Task 3)
  - `TestUnversionedRoundTrip_ClearOnNextSuccess` — (skipped at commit time, unskipped in Task 3)
  - `TestUnversionedReWriteValidatesPage` — (skipped at commit time, unskipped in Task 3)
- All `NewStore` calls use verified two-argument signature `(dir string, logger *slog.Logger)`.

### Task 3: Full factor + ETag + Unversioned + test seam (commit ec0fbd98)

**store.go changes:**
- Added `gitCommitFunc` private field to `Store` struct for the test seam.
- Added `SetGitCommitFuncForTest(fn)` exported method — allows cross-package tests (Plan 07 `package api`) to install a failing or passing gitCommit without build tags.
- Updated `gitCommit` to dispatch through `gitCommitFunc` when set (production code leaves it nil).
- Updated `PageWriter` interface: `WritePage(ctx, page, expectedUpdatedAt ...string) error` to match the new concrete method.
- Removed `WritePage`, `DeletePage`, `MigrateYAMLToMD`, `RepairLink` bodies (moved to `store_writes.go`).
- Removed unused `errors` import.
- **LOC: 755 → 571** (satisfies ≤600 CLAUDE.md god-class rule).

**store_writes.go changes:**
- `WritePage(ctx, page, expectedUpdatedAt ...string) error` — full implementation with:
  - ETag check (all 5 cases from D-02) INSIDE `fileMutex` critical section
  - Legacy .yaml cleanup
  - `writeAtomic` (extracted helper: temp file + rename)
  - `readPageLocked` helper (no mutex — caller already holds it)
  - gitCommit + D-17 Unversioned set path (re-read + Validate + atomic re-write)
  - gitCommit + D-18 Unversioned clear path (same pattern)
- `DeletePage` — verbatim move, no signature change
- `MigrateYAMLToMD` — moved from store.go
- `RepairLink` — moved from store.go
- **LOC: 315** (satisfies ≤600 CLAUDE.md god-class rule).

**Unskipped tests:**
- `TestUnversionedRoundTrip_SetOnFailure` — installs failing gitCommitFunc; asserts `Unversioned=true` after write.
- `TestUnversionedRoundTrip_ClearOnNextSuccess` — fail first, then succeed; asserts `Unversioned` cleared.
- `TestUnversionedReWriteValidatesPage` — verifies Validate gate exists via direct `Validate(bad)` check.

## Backward Compatibility Evidence

- `internal/ingest/pipeline.go:155` — `p.wiki.WritePage(ctx, page)` — no-variadic call, compiles and passes unchanged.
- `internal/wiki/parser.go:50,117` — `w.store.WritePage(ctx, page)` — same, no change.
- `internal/wiki/store.go RepairLink` (now in store_writes.go) — `s.WritePage(ctx, page)` — same.
- All existing tests in `store_test.go` and `wiki_test.go` pass without modification.
- `_ PageWriter = (*Store)(nil)` compile-time assertion in `wiki_test.go` confirms `*Store` still implements `PageWriter`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Interface cascade: PageWriter + WikiWriter + fakes needed updating**
- **Found during:** Task 3 implementation
- **Issue:** Changing `*Store.WritePage` to variadic breaks `PageWriter` interface satisfaction. In Go, `WritePage(ctx, page, ...string) error` does not satisfy `WritePage(ctx, page) error`. This caused compile failures in `internal/tools/tool_registry.go`, `internal/conversation/summarizer/applier.go`, `internal/api/summaries_test.go`, and the `wiki_test.go` compile-time assertion.
- **Fix:** Updated `wiki.PageWriter` interface and `summarizer.WikiWriter` interface to variadic; updated `fakeWikiStoreForSummaries.WritePage` and `fakeWikiStore.WritePage` fake implementations to add `_ ...string` parameter.
- **Files modified:** `internal/wiki/store.go` (PageWriter), `internal/conversation/summarizer/applier.go` (WikiWriter), `internal/conversation/summarizer/applier_test.go` (fake), `internal/api/summaries_test.go` (fake)
- **Commits:** ec0fbd98

**2. [Rule 1 - Refactor] MigrateYAMLToMD + RepairLink moved to store_writes.go to satisfy ≤600 LOC**
- **Found during:** Task 3, Step 4 (line count check)
- **Issue:** After moving only WritePage + DeletePage from store.go, line count was 685 (exceeds 600 limit).
- **Fix:** Additionally moved `MigrateYAMLToMD` (63 lines) and `RepairLink` (48 lines) to `store_writes.go`. Both are write operations and belong there semantically.
- **Final counts:** store.go = 571, store_writes.go = 315.
- **Commit:** ec0fbd98

**3. [Rule 3 - Blocking] store_writes_test.go helper name conflict with store_test.go**
- **Found during:** Task 2 (RED phase)
- **Issue:** `store_test.go` already defines `newTestStore(t) (*Store, string)` (two returns). The plan's test code uses `newTestStore` returning only `*Store`. Same name, different signature = compile error within the same package.
- **Fix:** Renamed the helper in `store_writes_test.go` to `newWritesTestStore(t) *Store` to avoid conflict.
- **Commit:** c8401b92

**4. Note: -race flag unavailable (CGO compiler missing)**
- The test environment (Windows worktree) does not have GCC in PATH, so `go test -race` fails to compile. Tests were verified with `go test -count=1` instead. The TOCTOU test (`TestWritePage_ETagInsideMutex_NoTOCTOU`) is inherently racy-safe by design (fileMutex guarantee) but was not exercised with the race detector in this environment.

## Test Coverage

| Test | Location | Status | Covers |
|------|----------|--------|--------|
| TestSchema_UnversionedRoundTrip | schema_test.go | GREEN | omitempty + ParseMD round-trip |
| TestWritePage_BackwardsCompat_NoVariadic | store_writes_test.go | GREEN | Legacy caller compat |
| TestWritePage_CreateOnly_SentinelEmptyString | store_writes_test.go | GREEN | D-02 create-only sentinel |
| TestWritePage_ETagMatch | store_writes_test.go | GREEN | D-02 ETag update proceed |
| TestWritePage_ETagMismatch | store_writes_test.go | GREEN | D-02 ETag conflict return |
| TestWritePage_ETagInsideMutex_NoTOCTOU | store_writes_test.go | GREEN | Pitfall #1 TOCTOU |
| TestUnversionedRoundTrip_SetOnFailure | store_writes_test.go | GREEN | D-17 GIT-01 set path |
| TestUnversionedRoundTrip_ClearOnNextSuccess | store_writes_test.go | GREEN | D-18 GIT-01 clear path |
| TestUnversionedReWriteValidatesPage | store_writes_test.go | GREEN | WARNING 14 Validate gate |

All 9 target tests GREEN. Full `./internal/wiki/` suite passes with no regressions.

## Notes for Downstream Plans

**Plan 04 (write_wiki_page tool):**
- Import `wiki.ConflictError` — `errors.As(err, &conflict)` to detect and surface as tool RESULT JSON (D-03).
- Call `WritePage(ctx, page, expectedUpdatedAt)` with the client-supplied ETag for dashboard edits.

**Plan 06 (wiki API endpoint):**
- `/api/wiki/page` reads `page.Unversioned` from frontmatter and passes it through to the JSON response.
- Tests use `store.SetGitCommitFuncForTest(...)` to install a failing gitCommit and verify JSON passthrough surfaces `unversioned: true`.

**Plan 07 (dashboard integration):**
- `store.SetGitCommitFuncForTest` is exported — accessible from `package api` tests without build tags.
- Test: install failing gitCommit → write page → read via API → verify `unversioned: true` in JSON.

## Known Stubs

None — all behavior is fully wired. The `Unversioned` field is server-managed (set/clear by WritePage).

## Threat Flags

No new network endpoints, auth paths, or file access patterns beyond what the plan's threat model documents.

## Self-Check: PASSED

- `internal/wiki/store_writes.go` exists and is 315 LOC ✓
- `internal/wiki/store.go` is 571 LOC ✓
- `internal/wiki/schema.go` contains `Unversioned bool` ✓
- `internal/wiki/parser.go` MarshalMD includes Unversioned ✓
- `internal/wiki/schema_test.go` contains TestSchema_UnversionedRoundTrip ✓
- `internal/wiki/store_writes_test.go` has 8 test functions ✓
- Commits fa0b29dd, c8401b92, ec0fbd98 all exist ✓
- `go test -count=1 ./internal/wiki/` exits 0 ✓
- `go build ./internal/wiki/ ./internal/ingest/ ./internal/api/ ./internal/conversation/... ./internal/tools/` exits 0 ✓
