---
phase: 32-quality-cleanup-dead-code-shared-helpers
plan: 03
subsystem: testing
tags: [go-modules, telebot, dead-code, refactor, prompt-builder, adaptive-reasoning]

# Dependency graph
requires:
  - phase: 13-channels
    provides: "internal/channels/telegram/bot.go genuine telebot.v4 consumer that keeps the go.mod pin DIRECT"
  - phase: 32-01
    provides: "QUAL-02 dead-code triage baseline (assets.Status escalation resolved)"
provides:
  - "internal/channels/deps.go deleted; package doc lives in internal/channels/doc.go (telebot anchor gone, pin stays DIRECT via the genuine consumer)"
  - "internal/agent buildRequest helper — exactly one request builder runs per turn (discarded Build() eliminated, request byte-identical per branch)"
  - "Branch-parity regression guard (llm_agent_buildreq_internal_test.go)"
affects: [32-04, channels, agent, prompt]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "White-box *_internal_test.go branch-parity test pinning a refactor to byte-identical output per branch"
    - "Builder-selection extracted to a single if/else helper so no turn runs two builders"

key-files:
  created:
    - internal/channels/doc.go
    - internal/agent/llm_agent_buildreq_internal_test.go
  modified:
    - internal/agent/llm_agent.go
    - go.mod
  deleted:
    - internal/channels/deps.go

key-decisions:
  - "Open Question #4: deleted deps.go and moved the package doc to internal/channels/doc.go (matches the project doc.go idiom) rather than keeping deps.go doc-only."
  - "Parity test placed in a new package-agent white-box file (*_internal_test.go convention) because buildRequest is unexported and llm_agent_test.go is package agent_test — it cannot reach the helper."

patterns-established:
  - "Branch-parity test: assert buildRequest(...) deep-equals the corresponding builder output per branch, plus a discriminating guard that the branches diverge (req.Reasoning) so the parity check is not trivially passing."

requirements-completed: []  # QUAL-02 intentionally NOT marked complete — remaining items land in 32-04 (AURA_MEMORY_EMBED_* removal).

coverage:
  - id: D1
    description: "Redundant telebot blank-import anchor removed; go mod tidy keeps gopkg.in/telebot.v4 DIRECT (via telegram/bot.go) and build stays green."
    requirement: "QUAL-02"
    verification:
      - kind: integration
        ref: "go mod tidy && grep -q 'gopkg.in/telebot.v4' go.mod && go build $(bash scripts/go_packages.sh)"
        status: pass
      - kind: other
        ref: "rg -n 'telebot' .github/ scripts/ Makefile  (empty => false pin-gate confirmed)"
        status: pass
    human_judgment: false
  - id: D2
    description: "Discarded Build() at llm_agent.go:235 restructured to an if/else (buildRequest); exactly one builder runs per turn; chosen request byte-identical per branch."
    requirement: "QUAL-02"
    verification:
      - kind: unit
        ref: "internal/agent/llm_agent_buildreq_internal_test.go#TestBuildRequest_BranchParity"
        status: pass
      - kind: unit
        ref: "go test -race ./internal/agent/ (full package, 20.8s)"
        status: pass
    human_judgment: false

# Metrics
duration: 22min
completed: 2026-06-30
status: complete
---

# Phase 32 Plan 03: QUAL-02 Redundant-Code Removal Summary

**Deleted the false-justified telebot blank-import anchor (pin stays DIRECT via the genuine telegram consumer) and eliminated the per-turn discarded Build() in the agent loop via a branch-parity-tested buildRequest helper.**

## Performance

- **Duration:** ~22 min
- **Started:** 2026-06-30T07:30Z (approx)
- **Completed:** 2026-06-30T07:52Z (approx)
- **Tasks:** 2
- **Files modified:** 4 (1 created doc.go, 1 created test, 1 modified llm_agent.go, 1 modified go.mod, 1 deleted deps.go)

## Accomplishments

- Removed the redundant `_ "gopkg.in/telebot.v4"` blank import from `internal/channels/deps.go`. The in-code "amendment-#58 CI pin gate" justification was confirmed FALSE — `rg telebot` over `.github/`, `scripts/`, `Makefile` returns nothing. The genuine consumer `internal/channels/telegram/bot.go:18` keeps the `go.mod` pin DIRECT; `go mod tidy` confirmed `gopkg.in/telebot.v4 v4.0.0-beta.9` remained in the DIRECT require block and the build stayed green.
- Restructured the discarded `Build()` at `llm_agent.go:235`: the old code ran `Build()` AND `BuildWithReasoningTier()` whenever `adaptiveTierOK`, throwing the first away (a wasted `RenderToolDefs()` every turn). Extracted a `buildRequest(budget, tier, tierOK)` if/else helper so exactly one builder runs, with the chosen request byte-identical to the old chosen request per branch.
- Added a white-box branch-parity regression test that pins each branch to its builder's output and proves the branches genuinely diverge (`req.Reasoning`) so the parity assertions are discriminating, not trivially equal.

## Task Commits

Each task was committed atomically (D-11: one commit per item), via direct `git commit` (the `gsd` commit wrapper times out on the file-size hook):

1. **Task 1: Remove redundant telebot blank import** - `5d6066be` (refactor)
2. **Task 2: Restructure discarded Build() (branch parity)** - `0428121e` (refactor)

_Task 2 is `tdd="true"` but behaviour-preserving — see TDD Gate Compliance below._

## Files Created/Modified

- `internal/channels/doc.go` - New package-doc file holding the `package channels` framework comment (replaces deps.go's doc).
- `internal/channels/deps.go` - Deleted (was the lone telebot blank-import anchor).
- `internal/agent/llm_agent.go` - Inline two-builder selection replaced by `req := a.buildRequest(...)`; new `buildRequest` if/else helper added.
- `internal/agent/llm_agent_buildreq_internal_test.go` - New white-box `TestBuildRequest_BranchParity`.
- `go.mod` - `go mod tidy` after the anchor removal (see Issues for the incidental x/crypto promotion).

## Decisions Made

- **Open Question #4 (executor's call):** deleted `deps.go` and moved the package doc into a new `internal/channels/doc.go`, matching the project's `doc.go` idiom, rather than keeping `deps.go` as a doc-only file.
- **Test placement:** the parity test went into a new `*_internal_test.go` (`package agent`) file because `buildRequest` is unexported and the plan-named `llm_agent_test.go` is `package agent_test`, which cannot reach the helper. This follows the package's existing white-box convention (`llm_agent_finalize_internal_test.go`, `llm_agent_breaker_internal_test.go`, …).

## Deviations from Plan

### Deviations

**1. [Convention follow-through] Parity test in a new internal test file, not llm_agent_test.go**
- **Found during:** Task 2 (restructure discarded Build())
- **Issue:** The plan's `files` lists `internal/agent/llm_agent_test.go` for the parity test, but that file is `package agent_test` and the testable unit (`buildRequest`) is unexported.
- **Fix:** Added `internal/agent/llm_agent_buildreq_internal_test.go` (`package agent`), matching the package's established `*_internal_test.go` white-box convention. No change to public API.
- **Files modified:** internal/agent/llm_agent_buildreq_internal_test.go (created)
- **Verification:** `go test -race ./internal/agent/` green (20.8s), `go vet` clean.
- **Committed in:** `0428121e` (Task 2 commit)

---

**Total deviations:** 1 (convention follow-through — pattern-consistent test placement).
**Impact on plan:** None on behavior or scope; the parity test exists and passes exactly as the plan requires, just in the project's idiomatic white-box file.

## TDD Gate Compliance

Task 2 is flagged `tdd="true"`, but the change is a **behaviour-preserving refactor** whose entire point is parity: the chosen request is byte-identical to the old chosen request, so there is no observable behaviour to capture as a failing RED test (output does not change). A pre-implementation RED referencing the not-yet-existing `buildRequest` helper would only fail to compile, and the pre-commit `vet` hook rejects non-compiling commits — so it could not be committed as a separate RED. The task therefore landed as a single `refactor(...)` commit pairing the restructure with its parity regression guard. The parity test is discriminating (the two branches diverge on `req.Reasoning`), so it would catch a wrong-builder regression.

## Issues Encountered

- **`go mod tidy` promoted `golang.org/x/crypto` to the DIRECT require block.** This is the concurrent Codex session's in-flight code (it imports x/crypto directly) being picked up by the whole-module tidy — a legitimate, correct tidy outcome, NOT reverted per the parallel-session coordination rules. The hard assertion held: `gopkg.in/telebot.v4` stayed DIRECT and the build stayed green. The telebot removal itself contributes only the deletion of the anchor; the x/crypto line is incidental and noted in the Task 1 commit body.
- **Parallel session active on master throughout.** All commits were scoped with explicit pathspec `git commit <files>` and verified with `git show --stat HEAD` to contain only declared files; the concurrent session's staged/untracked `internal/documents/*`, `cmd/aura/*`, and `internal/db/*` work was never staged or touched.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- QUAL-02 is **partially** complete: the telebot anchor and discarded Build() are resolved (this plan); the `AURA_MEMORY_EMBED_*` full-stack removal remains in **32-04**. QUAL-02 is intentionally NOT marked complete.
- Wave 1 of Phase 32 advances to 32-04 next (still QUAL-02 dead-code clean-slate).

## Self-Check: PASSED

- FOUND: internal/channels/doc.go
- FOUND: internal/agent/llm_agent_buildreq_internal_test.go
- FOUND: .planning/phases/32-quality-cleanup-dead-code-shared-helpers/32-03-SUMMARY.md
- DELETED-OK: internal/channels/deps.go
- FOUND commit: 5d6066be (Task 1 — telebot anchor removal)
- FOUND commit: 0428121e (Task 2 — discarded Build() restructure)

---
*Phase: 32-quality-cleanup-dead-code-shared-helpers*
*Completed: 2026-06-30*
