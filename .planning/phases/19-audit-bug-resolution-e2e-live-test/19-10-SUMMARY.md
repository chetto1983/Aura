---
phase: 19-audit-bug-resolution-e2e-live-test
plan: 10
subsystem: trust-boundaries
tags: [env, skills, conversations, web-tools, trust-boundary]
requires: []
provides:
  - central dotenv load at main dispatch
  - bounded and SIGINT-cancelable skills snippet exec
  - Activate and DiscardPending skill name guards
  - deleted-conversation exclusion for search wrapper
  - pre-engine empty query/url rejection for web tools
  - documented bundled-script INFO trust boundary
affects: [cli, skills, conversations, web-tools]
tech-stack:
  added: []
  patterns: [composition-root env load, lifecycle self-guard, wrapper-level post-filter]
key-files:
  created:
    - cmd/aura/main_env_test.go
    - cmd/aura/skills_snippet_test.go
    - docs/audit/trust-boundary-info-2026-06-10.md
  modified:
    - cmd/aura/main.go
    - cmd/aura/skills_snippet.go
    - internal/skills/writer_activate.go
    - internal/skills/resume.go
    - internal/skills/writer_test.go
    - internal/conversations/store.go
    - internal/conversations/store_test.go
    - internal/agent/tools/web_search.go
    - internal/agent/tools/web_search_test.go
    - internal/agent/tools/web_fetch.go
    - internal/agent/tools/web_fetch_test.go
key-decisions:
  - "Load .env once at main startup so every operator subcommand sees the same env as serve."
  - "Keep the locked SearchConversationTurns SQL unchanged and filter deleted conversation hits at the Store wrapper boundary."
  - "Document bundled skill scripts as an accepted full-host trust boundary; do not add scanner logic to loader.go."
patterns-established:
  - "Required web tool strings are trimmed and rejected before engine calls."
  - "Skill lifecycle methods self-guard names before joining into removable paths."
requirements-completed: [M-i, L1, L2, L4, L6, INFO]
duration: 40 min
completed: 2026-06-10
---

# Phase 19 Plan 10: Env and LOW Trust Boundary Summary

**Operator env loading is centralized and the remaining LOW trust-boundary gaps are closed.**

## Performance

- **Duration:** 40 min
- **Completed:** 2026-06-10
- **Tasks:** 3
- **Files modified:** 14

## Accomplishments

- Added `_ = godotenv.Load()` at the start of `main()`, before CLI dispatch, so `aura mcp` and other operator subcommands see `.env`.
- Added subprocess regressions proving `.env` `AURA_MCP_CONFIG` reaches the real MCP dispatch and process-set env wins over `.env`.
- Wrapped snippet execution in a timeout plus `signal.NotifyContext` and extracted a testable process runner.
- Added `SanitizeName(name, name)` guards to `Writer.Activate` and `Writer.DiscardPending`.
- Kept the locked FTS SQL unchanged and filtered `status='deleted'` conversations in `SearchConversationTurns`.
- Added empty `query` / `url` rejection before `web_search` / `web_fetch` call their engines.
- Documented the bundled-script INFO finding as an accepted trust boundary in `docs/audit/trust-boundary-info-2026-06-10.md`; `internal/skills/loader.go` is unchanged.

## Task Commits

1. **Tasks 1-3: Env load, LOW fixes, and INFO doc** - `d9f92bd8` (fix)

**Plan metadata:** this SUMMARY.md commit.

## Files Created/Modified

- `cmd/aura/main.go` - central `.env` load before dispatch.
- `cmd/aura/main_env_test.go` - real `main()` MCP dispatch env regressions.
- `cmd/aura/skills_snippet.go` and `cmd/aura/skills_snippet_test.go` - bounded snippet execution.
- `internal/skills/writer_activate.go`, `internal/skills/resume.go`, `internal/skills/writer_test.go` - lifecycle name guards.
- `internal/conversations/store.go`, `internal/conversations/store_test.go` - deleted search exclusion without changing locked SQL.
- `internal/agent/tools/web_search.go`, `web_fetch.go`, and tests - empty arg rejection before engine calls.
- `docs/audit/trust-boundary-info-2026-06-10.md` - INFO trust-boundary record.

## Decisions Made

No per-subcommand env readers were added. The env load lives at the composition root, and the existing lower-level loads remain harmless/idempotent.

No bundled-script scanner was added. The INFO item is accepted by design under amendment #50 / D-15c.

## Deviations from Plan

The L4 status guard is implemented as a cached wrapper-level status lookup rather than a SQL join, preserving the locked query byte-for-byte.

## Issues Encountered

None.

## Verification

- `go test -run 'TestMainLoadsDotEnvForMCPDispatch|TestMainDotEnvDoesNotOverrideProcessEnv|TestSnippetExecTimeout' ./cmd/aura/` - passed.
- `go test -run 'TestActivateAndDiscardPendingRejectBadName|TestSetAlwaysRejectsBadName|TestArchiveRejectsBadName|TestDeleteRejectsBadName' ./internal/skills/` - passed.
- `go test -run 'TestWebSearchRejectsEmptyQueryBeforeEngine|TestWebFetchRejectsEmptyURLBeforeEngine|TestWebSearch_Success|TestWebFetch_Spillover|TestWeb_SanitizedInlineError' ./internal/agent/tools/` - passed.
- `go test -tags db_integration -run 'TestSearchConversationTurns|TestStoreMethods_DBErrorWrapping' ./internal/conversations/` - passed.
- `go build ./cmd/aura/ ./internal/skills/ ./internal/conversations/ ./internal/agent/tools/ ./internal/mcp/` - passed.
- `go vet ./cmd/aura/ ./internal/skills/ ./internal/conversations/ ./internal/agent/tools/ ./internal/mcp/` - passed.
- `go test ./cmd/aura/ ./internal/skills/ ./internal/conversations/ ./internal/agent/tools/ ./internal/mcp/` - passed.
- `go test -tags db_integration -run 'TestWriterActivateAuditRow|TestResumeAcceptActivates|TestResumeDeclineDiscards|TestLifecycleAuditFailureSurfaces|TestSnippetExec|TestActivate|TestDiscard|TestSanitize' ./internal/skills/` - passed.
- `go test -tags db_integration -run 'TestSnippet|TestActivate|TestDiscard|TestSearch|TestWebSearch|TestWebFetch|TestSanitize|TestMainLoadsDotEnv|TestMainDotEnv' ./cmd/aura/ ./internal/skills/ ./internal/conversations/ ./internal/agent/tools/` - passed.
- `go test -race ./cmd/aura/ ./internal/skills/ ./internal/conversations/ ./internal/agent/tools/ ./internal/mcp/` - passed.
- `go build ./...` - passed.

## User Setup Required

None - `godotenv` was already present in `go.mod`.

## Next Phase Readiness

Wave 2 can rely on consistent operator `.env` visibility, bounded snippet exec, self-guarded skills lifecycle methods, and web tools that reject blank required args before external calls.

---
*Phase: 19-audit-bug-resolution-e2e-live-test*
*Completed: 2026-06-10*
