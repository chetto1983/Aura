---
phase: 32-quality-cleanup-dead-code-shared-helpers
plan: 10
subsystem: test-gap-closure
tags: [qual-05, test-gap, telegram, onboarding, utf8, truncate, memory-integration, ci, race]

# Dependency graph
requires:
  - phase: 32-05
    provides: "envutil/pgnumeric leaf extractions landed — clean baseline on the touched agent/telegram packages"
  - phase: 32-06
    provides: "same-package agent extractions done — no overlap with this plan's agent test file"
  - phase: 32-07
    provides: "prior cleanup wave settled; agent/telegram surfaces stable for test-only additions"
  - phase: 32-09
    provides: "first QUAL-05 test-gap wave (throttle/setup-SSE/Authula-DSN) closed; this plan closes the remaining three items"
provides:
  - "internal/channels/telegram/profile_onboarding_test.go — TestAnswersFromTextKeywordFallback: Italian-keyword FALLBACK table for answersFromText (lang/timezone/tone/length-precedence/voice + zero-value no-match), the only coverage of the keyword map (the other tests exercise only the LLM-extractor path)"
  - "internal/agent/llm_agent_completion_internal_test.go — TestTruncateTailBytes + TestTruncateBytesKeepingTail: UTF-8 boundary tables mirroring the sibling TestTruncateBytes (n<=0, len<=n, ASCII tail, multibyte mid-rune walk-back, whole-rune-kept, n<=marker passthrough, head+marker+tail composition), every output asserted valid UTF-8"
  - "Documented verdict: the memory_integration CI leg already runs live (ci.yml:606-719) with no-skip-as-green discipline — QA-A-09 is stale, no redundant matrix entry added (D-12)"
affects: [telegram, agent, memory, ci]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Keyword-fallback characterization (telegram): reflect.DeepEqual against a zero-value-aware expected Answers (incl. a *bool VoiceMode via ptrBool) pins the short>concise precedence branch and the no-match-injects-nothing invariant."
    - "UTF-8 tail-clamp boundary table (agent): mirror the head-clamp sibling and additionally assert utf8.ValidString on every output, so a mid-rune walk-back can never leak a split rune into the critic digest."
    - "Verify-and-document, do-not-duplicate (CI): an already-live integration leg is confirmed by direct read of the job env+steps + the tagged test's build-tag and $CI t.Fatal, and recorded as evidence — never re-added as a redundant matrix entry."

key-files:
  created:
    - .planning/phases/32-quality-cleanup-dead-code-shared-helpers/32-10-SUMMARY.md
  modified:
    - internal/channels/telegram/profile_onboarding_test.go
    - internal/agent/llm_agent_completion_internal_test.go
  deleted: []

key-decisions:
  - "Task 2 landed in the existing white-box internal/agent/llm_agent_completion_internal_test.go (package agent), NOT the plan's literal internal/agent/llm_agent_completion_test.go — that file is package agent_test (black-box) and cannot reach the unexported truncateTailBytes/truncateBytesKeepingTail. The chosen file already tests the same production file's unexported helpers, matching the finalize-sibling _internal_test.go convention."
  - "memory_integration is verified ALREADY-LIVE and only documented (D-12). The contrary-to-research STOP condition did not trigger: the leg sets CI:\"true\", exports AURA_AGENT_MEMORY_MCP_URL, compiles AND runs -tags memory_integration against the live sidecar; QA-A-09 is stale."
  - "Test-only closures of already-shipped functions: no production code changed in telegram or agent; commits are correctly typed test(...) (no feat gate)."

patterns-established:
  - "ptrBool helper + reflect.DeepEqual for tables whose expected struct carries a pointer field."
  - "utf8.ValidString assertion on every truncation output as the standing UTF-8-safety pin."

requirements-completed: []  # QUAL-05 partial — orchestrator/verifier owns the flip.

# Coverage metadata (#1602)
coverage:
  - id: D1
    description: "answersFromText Italian-keyword fallback path is pinned (lang/timezone/tone/short>concise precedence/voice + zero-value no-match); production unchanged."
    requirement: "QUAL-05"
    verification:
      - kind: unit
        ref: "internal/channels/telegram/profile_onboarding_test.go#TestAnswersFromTextKeywordFallback (go test -race ./internal/channels/telegram/)"
        status: pass
    human_judgment: false
  - id: D2
    description: "truncateTailBytes + truncateBytesKeepingTail UTF-8 boundary behavior is pinned (mid-rune walk-back yields valid UTF-8; head+marker+tail composition); production unchanged."
    requirement: "QUAL-05"
    verification:
      - kind: unit
        ref: "internal/agent/llm_agent_completion_internal_test.go#TestTruncateTailBytes,TestTruncateBytesKeepingTail (go test -race -run Truncate ./internal/agent/)"
        status: pass
    human_judgment: false
  - id: D3
    description: "memory_integration CI leg verified already-live (CI:true + AURA_AGENT_MEMORY_MCP_URL exported + compile + live -tags run) with no-skip-as-green t.Fatal; documented, no redundant leg added."
    requirement: "QUAL-05"
    verification:
      - kind: other
        ref: ".github/workflows/ci.yml:606-719 (job @606, CI:\"true\" @622, URL @642, vet @656, run @715) + internal/agent/memory_recall_integration_test.go:1 (//go:build memory_integration) + :52 (t.Fatal under $CI)"
        status: pass
    human_judgment: false

# Metrics
duration: ~25min (sequential, no worktree; concurrent-Codex isolation held)
completed: 2026-06-30
status: complete
---

# Phase 32 Plan 10: QUAL-05 Test-Gap Closure (Telegram keyword fallback / truncateTailBytes UTF-8 / memory_integration verdict) Summary

**Two test-only gap closures against already-shipped production helpers plus one verify-and-document task, each committed atomically and green under `-race`: (1) `TestAnswersFromTextKeywordFallback` pins the Italian-keyword FALLBACK branch of `answersFromText` (profile_onboarding.go:362) — the parser used when the LLM `AnswerExtractor` is absent or errors and the only coverage of the keyword map (lang `ital`, timezone `europe/rome|roma`, tone `tecnic|technical`, the short>concise length precedence, voice `voce|voice`, and the no-match zero-value case); (2) `TestTruncateTailBytes` + `TestTruncateBytesKeepingTail` mirror the sibling `TestTruncateBytes` for the unexported TAIL clamp and its composing caller in `llm_agent_completion.go` (n<=0, len(s)<=n, ASCII tail, multibyte mid-rune walk-back, whole-rune-kept, the n<=marker passthrough, and the head+marker+tail composition), with every output asserted valid UTF-8 so the critic digest can never carry a split rune; (3) the `memory_integration` CI leg is verified ALREADY-LIVE (ci.yml:606-719) with no-skip-as-green discipline and documented — `QA-A-09` is stale and NO redundant matrix entry was added (D-12). All three helpers reach 100.0% statement coverage. No production code changed in telegram or agent.**

## Accomplishments

- **Task 1 — `internal/channels/telegram/profile_onboarding_test.go` (extended):** `TestAnswersFromTextKeywordFallback`, a 5-case table (`full_italian_phrase`, `roma_alias_and_concisa`, `english_keywords`, `short_beats_concise_precedence`, `no_keyword_match_is_zero_value`) driving `answersFromText` directly and comparing via `reflect.DeepEqual` (with a `ptrBool` helper for the `*bool VoiceMode`). Pins the short>concise precedence and the invariant that a non-matching free-text answer injects NO preference (zero-value `Answers`). `answersFromText` reaches **100.0%**.
- **Task 2 — `internal/agent/llm_agent_completion_internal_test.go` (extended):** `TestTruncateTailBytes` (7 cases) and `TestTruncateBytesKeepingTail` (5 cases) mirror the head-clamp sibling for the TAIL clamp. The multibyte cases prove a byte budget landing inside a rune walks forward to the next rune start (`"àbc",3 → "bc"`) or keeps the whole rune (`"abà",2 → "à"`); the composition cases pin `head + "\n...[truncated]...\n" + tail` for ASCII and multibyte. Every output is asserted `utf8.ValidString`. `truncateTailBytes` and `truncateBytesKeepingTail` both reach **100.0%**.
- **Task 3 — memory_integration verdict (documented here):** the live CI job is confirmed by direct read; QA-A-09 is recorded stale; no CI was added.

## Task Commits

Each gap committed atomically (D-11), direct `git commit -o -F <msgfile> -- <paths>` (explicit `--only` pathspecs to stay isolated from the concurrent Codex session on master):

1. **Task 1 — telegram keyword-fallback pin** — `981d83e8` `test(32-10): table-test telegram answersFromText Italian keyword fallback` (`internal/channels/telegram/profile_onboarding_test.go`, +58).
2. **Task 2 — truncateTailBytes UTF-8 pin** — `c4011bd2` `test(32-10): UTF-8 boundary table for truncateTailBytes + KeepingTail` (`internal/agent/llm_agent_completion_internal_test.go`, +71).
3. **Task 3 — memory_integration verdict (doc)** — this SUMMARY (the plan-metadata commit).

_Note: this is a `type: tdd` plan but both code tasks are test-only characterizations of already-shipped functions — no new behavior to RED→GREEN, so the commits are correctly typed `test(...)` with no `feat` gate (see TDD Gate Compliance)._

## memory_integration CI leg — VERDICT: already-live, no-skip-as-green (D-12 / QA-A-09 stale)

Re-verified one last time by direct read. The leg runs live and honestly; **no redundant matrix entry was added.**

**`.github/workflows/ci.yml` — job "Memory MCP (memory_integration tier, live agent-memory sidecar)":**
- `:606` — job declared (`name: Memory MCP (memory_integration tier, live agent-memory sidecar)`).
- `:622` — `CI: "true"  # arms the no-skip-as-green t.Fatal in the tier`.
- `:642` — `AURA_AGENT_MEMORY_MCP_URL: http://127.0.0.1:8091/mcp/` exported so the tier RUNS (not skips).
- `:653-656` — always-compile floor: `go vet -tags memory_integration ./internal/agent/mcptools/ ./cmd/aura/ ./internal/agent/`.
- `:687-709` — brings up Postgres+migrate, Neo4j+embed sidecar, the agent-memory MCP sidecar, and waits for it healthy.
- `:711-715` — live run: `go test -race -tags memory_integration -count=1 -p 1 ./internal/agent/mcptools/ ./cmd/aura/ ./internal/agent/`.

**`internal/agent/memory_recall_integration_test.go` — no-skip-as-green guard:**
- `:1` — `//go:build memory_integration` (the file is tag-gated and compiles into the tier above).
- `:52` — `t.Fatal("AURA_AGENT_MEMORY_MCP_URL (or _PORT) must be set under CI — a skipped memory_integration tier is never a silent pass (CLAUDE.md no-skip-as-green)")` fires when the URL is unset AND `$CI` is set.

Because `ci.yml` exports both `CI:"true"` and `AURA_AGENT_MEMORY_MCP_URL`, the tier ACTUALLY RUNS against the rebuilt live sidecar; were the URL ever dropped, the `:52` `t.Fatal` would fail the gate rather than green-skip. `mcptools/memory_integration_test.go` is covered by the same `./internal/agent/mcptools/` run-set. This matches the 32-RESEARCH "D-12 Verdict" exactly. **QA-A-09 ("memory tests don't run in CI") is STALE.** Verify grep (Task 3 automated check) confirms all three anchors (`memory_integration` job, `//go:build memory_integration`, `t.Fatal`-under-`$CI`).

## Decisions Made

- **Task 2 file/package deviation (see Deviations).** The plan's literal target `internal/agent/llm_agent_completion_test.go` is `package agent_test` (black-box) and cannot call the unexported `truncateTailBytes`/`truncateBytesKeepingTail`. The tests landed in the existing white-box `internal/agent/llm_agent_completion_internal_test.go` (`package agent`) — which already pins the same production file's unexported completion-gate helpers — mirroring the `truncateBytes` sibling's home (`llm_agent_finalize_internal_test.go`).
- **Verify-and-document, never duplicate (memory leg).** Per D-12 and the "Don't Hand-Roll" guidance, an already-live integration tier is confirmed and recorded, not re-added. The contrary-to-research STOP condition did not apply — the leg is wired and runs.
- **Pointer-aware table comparison.** `answersFromText` returns an `Answers` with a `*bool VoiceMode`; the table uses `reflect.DeepEqual` (value-deref for the pointer, nil-slice equality) with a `ptrBool` helper rather than field-by-field asserts, keeping the table-test idiom (32-PATTERNS § Shared Pattern B).

## Deviations from Plan

**1. [Rule 3 - Blocking] Task 2 test placed in the white-box `_internal_test.go` file (package), not the plan's literal black-box filename**
- **Found during:** Task 2 — the plan names `internal/agent/llm_agent_completion_test.go`, but that file is `package agent_test` (black-box). The functions under test, `truncateTailBytes` and `truncateBytesKeepingTail`, are **unexported** in `package agent`, so a black-box file cannot reference them (it would not compile).
- **Fix:** Added the two tables to the existing `internal/agent/llm_agent_completion_internal_test.go` (`package agent`), which already white-box-tests the same production file's unexported helpers (`parseCriticVerdict`, `sideEffectDigest`, …) and matches the established `<file>_internal_test.go` convention used by the `truncateBytes` sibling in `llm_agent_finalize_internal_test.go`. Added a `unicode/utf8` import for the `ValidString` assertions.
- **Files modified:** `internal/agent/llm_agent_completion_internal_test.go` (extended). **Verification:** `go test -race -run Truncate ./internal/agent/` exits 0; full untagged `go test -race ./internal/agent/` exits 0. **Committed in:** `c4011bd2`.
- **Impact:** Faithful to the plan's intent (UTF-8 boundary table for `truncateTailBytes` mirroring `TestTruncateBytes`, in `internal/agent`, containing `truncateTailBytes`); only the file infix differs (`_internal_test.go`). No production change.

---

**Total deviations:** 1 (Rule 3 — blocking: wrong test package in the named file). No Rule 1/2 fixes and no Rule 4 architectural decisions arose.
**Impact on plan:** The deviation is necessary for compilation (unexported helpers require a white-box file) and does not change scope. Both code tasks remain pure test additions; production untouched.

## TDD Gate Compliance

The plan is `type: tdd`, but both code tasks are **test-only characterizations of already-shipped production functions** (`answersFromText`, `truncateTailBytes`/`truncateBytesKeepingTail`) — there is no new behavior to RED→GREEN. Accordingly the commits are correctly typed `test(...)` (no `feat` gate), and the production functions were not modified. The tests pass against the current code (the D-09/D-10 characterization-test pattern), and the multibyte mid-rune cases are the load-bearing assertions (a regression that split a rune would turn them RED).

## Coverage

| ID | Surface | Verification | Status |
|----|---------|--------------|--------|
| D1 | telegram `answersFromText` Italian-keyword fallback | `go test -race ./internal/channels/telegram/` green; `answersFromText` **100.0%**; 5 cases incl. short>concise precedence + zero-value no-match. | pass |
| D2 | agent `truncateTailBytes` + `truncateBytesKeepingTail` UTF-8 | `go test -race -run Truncate ./internal/agent/` green; both helpers **100.0%**; mid-rune walk-back asserted `utf8.ValidString`; head+marker+tail composition pinned. | pass |
| D3 | `memory_integration` CI leg already-live | `ci.yml:606-719` (CI:"true" @622, URL @642, vet @656, live run @715) + `memory_recall_integration_test.go:1,52`; verify grep confirms job+tag+`t.Fatal`. | pass |

Combined: `go test -race ./internal/channels/telegram/ ./internal/agent/` exits 0; the two pinned helper-pairs are at 100% statement coverage. The memory leg is verified live (no new CI added).

## Issues Encountered

- **Concurrent Codex session on master:** owns `internal/agui/**`, `internal/objectstore/**`, document/graphrag work, and `.planning/graphs/*`. Every commit here used explicit `--only` pathspecs; `git show --stat` confirmed each commit lists ONLY this plan's files (zero `internal/agui/**`, `internal/objectstore/**`, or `.planning/graphs/**` swept in). The parallel session's uncommitted `.planning/graphs/*` changes were left untouched.
- **WSL `go env GOROOT` empty / `.exe` AV block:** all `go test`/`go vet`/`go fmt` ran in WSL against the main tree (never a native Windows `.exe`). The `go` shim auto-resolves the go1.26.4 toolchain and returns an empty `GOROOT` in a bare subshell, so formatting was done via the toolchain-integrated `go fmt` (idempotent — no changes) instead of `$(go env GOROOT)/bin/gofmt`. The pre-commit hook (gofmt + vet + file-size) passed on both code commits.

## User Setup Required

None — test-only additions against existing, unchanged production functions plus a documentation verdict. No env, schema, CI, or external-service changes.

## Next Phase Readiness

- **QUAL-05 is now fully closed across plans 32-09 + 32-10:** web/throttle, setup-SSE ordering, Authula-DSN (32-09) and Telegram keyword fallback, `truncateTailBytes` UTF-8, plus the documented `memory_integration` already-live verdict (32-10).
- No new package created (both tests live in existing coverage-gated packages), so no `scripts/coverage_gate.sh` registration is needed.
- STATE.md / ROADMAP.md NOT modified here — the orchestrator owns those writes.

## Self-Check: PASSED

- FOUND: internal/channels/telegram/profile_onboarding_test.go `TestAnswersFromTextKeywordFallback` (contains `answersFromText`; 5 cases; `answersFromText` 100.0%)
- FOUND: internal/agent/llm_agent_completion_internal_test.go `TestTruncateTailBytes` + `TestTruncateBytesKeepingTail` (contains `truncateTailBytes`; both helpers 100.0%; `utf8.ValidString` asserted)
- CONFIRMED: production `internal/channels/telegram/profile_onboarding.go` and `internal/agent/llm_agent_completion.go` unchanged (empty `git diff HEAD`)
- CONFIRMED: memory_integration leg live — ci.yml:606 (job), :622 (CI:"true"), :642 (AURA_AGENT_MEMORY_MCP_URL), :656 (vet -tags), :715 (test -race -tags); memory_recall_integration_test.go:1 (build tag), :52 (t.Fatal under $CI); NO new CI matrix entry added; QA-A-09 stale
- FOUND commit: 981d83e8 (Task 1 — telegram, isolated: 1 test file, 0 agui/objectstore/graphs)
- FOUND commit: c4011bd2 (Task 2 — agent, isolated: 1 test file, 0 agui/objectstore/graphs)
- CONFIRMED: STATE.md and ROADMAP.md NOT modified (orchestrator owns those writes)

---
*Phase: 32-quality-cleanup-dead-code-shared-helpers*
*Completed: 2026-06-30*
