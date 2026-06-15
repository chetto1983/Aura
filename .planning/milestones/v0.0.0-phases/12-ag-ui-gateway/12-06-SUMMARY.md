---
phase: 12-ag-ui-gateway
plan: 06
subsystem: api
tags: [ag-ui, reasoning, chain-of-thought, translator, cli-render, state-machine, property-testing, golden-testing]

# Dependency graph
requires:
  - phase: 12-ag-ui-gateway (Plan 01)
    provides: "internal/agui pure translator state machine (textRunState coalescer), IDGenerator interface + default uuid impl, golden-events.json (5 REASONING shapes pre-seeded from spike 015), agui_boundary_check.sh gate"
  - phase: 12-ag-ui-gateway (Plan 05)
    provides: "agent.LLMResponse.Reasoning field (stream-only CoT, omitempty, round-trip-symmetric) threaded from the SSE wire to the agent Event"
provides:
  - "agui translator REASONING lifecycle branch: REASONING_START + REASONING_MESSAGE_START + N*REASONING_MESSAGE_CONTENT + REASONING_MESSAGE_END + REASONING_END, coalesced per contiguous reasoning run, separate rsn- messageId"
  - "agui.IDGenerator.NewReasoningID() (rsn- prefix) on the interface + default uuid impl + deterministic test impl"
  - "reasoningRunState coalescer (mirror of textRunState with the extra START/END envelope the REASONING_* family requires)"
  - "interleave-before-text: a reasoning run fully closes before the first TEXT_MESSAGE_START of the turn; symmetric clean-close on any interruption (tool/state/final/pause/error)"
  - "cmd/aura renderRunnerTurn live dim 💭 reasoning render (renderReasoning helper) — stream-only, never enters the answer buffer"
affects: [12-02-fanout, 12-03-server, 13-telegram, cli-renderer]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Parallel coalescing state machine: reasoningRunState mirrors textRunState; a single closeRuns() helper drains whichever run is open at every interruption point"
    - "Mutually-exclusive run families never nest: opening one family first closes the other (interleave-before-text emerges from the close-on-other-family rule)"
    - "Stream-only CLI render: reasoning written straight to w, never to the prose/answer builder, so the returned answer stays reasoning-free (mirror of Plan 12-05's persistence guard)"
    - "Refactor-on-touch file split: reasoning tests moved to translator_reasoning_test.go to hold the 600-LOC cap"

key-files:
  created:
    - "internal/agui/translator_reasoning_test.go — reasoning behavior + golden tests + assertReasoningQuartets + reasoning() helper (split off translator_test.go for the 600-LOC cap)"
    - "cmd/aura/chat_render_reasoning_test.go — TestRenderRunnerTurnReasoning (live 💭 + stream-only invariant) + TestRenderReasoningPrefixOnce"
  modified:
    - "internal/agui/translator.go — reasoningRunState + closeRuns + the resp.Reasoning branch (REASONING lifecycle)"
    - "internal/agui/types.go — IDGenerator.NewReasoningID() (rsn- prefix) on interface + default impl"
    - "internal/agui/translator_test.go — fixedIDGen.NewReasoningID + property-test reasoning invariants (no nesting, balanced lifecycle, empty-delta skip, well-formed quartets)"
    - "cmd/aura/chat_render.go — live dim 💭 reasoning render arm + renderReasoning helper"

key-decisions:
  - "Single closeRuns() helper drains either open run at every interruption: reasoning and text never coexist, so closing both (no-op on the absent one) is the simplest correct rule"
  - "Opening a text run first closes any open reasoning run (rs.close before st.content), so reasoning always fully precedes text — interleave-before-text is an emergent property, not a special case"
  - "renderReasoning resets the dim ANSI style after EACH delta (\\x1b[2m💭 prefix once, \\x1b[0m per delta) so partially-streamed CoT stays readable; the text is therefore interspersed with escapes, which the tests assert per-delta"
  - "Reasoning tests split into translator_reasoning_test.go (refactor-on-touch): adding them inline pushed translator_test.go to 706 LOC, over the hook-enforced 600 cap"

patterns-established:
  - "Pattern: parallel run-state coalescers sharing one closeRuns() interruption drain"
  - "Pattern: stream-only render surface (write to w, never to the answer builder)"

requirements-completed: [UX-01]

# Metrics
duration: 24 min
completed: 2026-06-06
---

# Phase 12 Plan 06: Translator REASONING Lifecycle + CLI Live Reasoning Render Summary

**The consumer half of amendment #57: the AG-UI translator emits a coalesced, validated REASONING_* lifecycle (separate `rsn-` messageId, interleaved before TEXT_MESSAGE, cleanly closed on interruption) matching the spike-015 golden shapes, and `cmd/aura` streams live dim 💭 chain-of-thought to the operator — masking the reasoning-phase latency on today's CLI surface, stream-only.**

## Performance

- **Duration:** ~24 min
- **Started:** 2026-06-06
- **Completed:** 2026-06-06
- **Tasks:** 2
- **Files modified:** 6 (2 created, 4 modified)

## Accomplishments
- Added `reasoningRunState`, a parallel coalescer mirroring `textRunState` but with the extra START/END envelope the SDK's REASONING_* family requires: one `REASONING_START` + one `REASONING_MESSAGE_START(rsn-id, "assistant")` + N `REASONING_MESSAGE_CONTENT` (empty deltas skipped) + one `REASONING_MESSAGE_END` + one `REASONING_END` per contiguous reasoning run.
- Introduced a single `closeRuns()` helper that drains whichever run (reasoning OR text) is open at EVERY interruption point — tool call, state-delta, final Event, pause, error, stream end — so a dangling lifecycle never reaches the SDK's `Validate()`.
- Made the text-open path call `rs.close(yield)` first, so reasoning always fully precedes the first `TEXT_MESSAGE_START` of the turn (interleave-before-text emerges from the close-on-other-family rule, not a special case).
- Extended `IDGenerator` with `NewReasoningID()` (rsn- prefix) on the interface, the default uuid impl, and the deterministic test impl (`rsn-N`) for stable golden compares.
- Property test now mixes reasoning + reasoningEmpty Events and asserts: every REASONING event `.Validate()` passes, no empty rsn- id / empty delta, reasoning and text runs never nest, balanced lifecycle, and well-formed START→MESSAGE_START→CONTENT*→MESSAGE_END→END quartets.
- Golden assertions prove the emitted REASONING_* events marshal to the 5 shapes Plan 01 seeded from spike 015 (rsn- messageId, MESSAGE_START role "assistant", MESSAGE_CONTENT delta) — the fixture was asserted against, not edited.
- `cmd/aura/chat_render.go` `renderRunnerTurn` gained a `case resp.Reasoning != ""` arm that streams the CoT live via `renderReasoning` (dim ANSI, one-time 💭 prefix) straight to `w` and NEVER to the prose/answer builder — the returned `answer`/`finish`/`usage`/`paused` path is byte-unchanged.
- The `internal/agent` ⇸ `internal/agui` one-way boundary stays intact (boundary gate green).

## Task Commits

Each task committed atomically. Task 1 is TDD (RED then GREEN); Task 2 folds test + impl per the plan:

1. **Task 1 (RED): failing REASONING lifecycle property + golden tests** — `78e087dd` (test)
2. **Task 1 (GREEN): translator REASONING lifecycle state machine (rsn- messageId)** — `70c1247c` (feat)
3. **Task 2: live dim 💭 reasoning render in renderRunnerTurn** — `b9aff825` (feat)

REFACTOR gate (Task 1): not needed for behavior, but a file-split refactor-on-touch WAS required — adding the reasoning tests inline pushed `translator_test.go` to 706 LOC (over the 600 cap), so the reasoning tests + helpers landed in the new `translator_reasoning_test.go` within the RED commit. translator.go itself landed at 297 LOC, 0 lint issues.

## Files Created/Modified
- `internal/agui/translator.go` — `reasoningRunState` (content/close mirroring textRunState with the START/END envelope), `closeRuns()` helper, the `resp.Reasoning != ""` branch; text-open path closes any open reasoning run first.
- `internal/agui/types.go` — `IDGenerator.NewReasoningID()` on the interface + `uuidIDGenerator.NewReasoningID()` (rsn-+uuid); doc comments updated.
- `internal/agui/translator_test.go` — `fixedIDGen.NewReasoningID` (rsn-N) + the property-test reasoning invariants (no text/reasoning nesting, balanced lifecycle, empty-delta skip in `assertNonEmptyIDs`, quartet check call).
- `internal/agui/translator_reasoning_test.go` (new) — `reasoning()` helper, 5 reasoning behavior/golden tests, `assertReasoningQuartets`.
- `cmd/aura/chat_render.go` — `reasoningStarted` flag + the `resp.Reasoning` render arm + `renderReasoning(w, delta, &started)` helper (dim 💭, stream-only).
- `cmd/aura/chat_render_reasoning_test.go` (new) — `TestRenderRunnerTurnReasoning` (💭 + reasoning on writer, ABSENT from answer, `\x1b[2m` present) + `TestRenderReasoningPrefixOnce`.

## Decisions Made
- **One `closeRuns()` for both families:** reasoning and text never coexist (a single Event carries Content XOR Reasoning, Plan 12-05), so draining both at each interruption — a no-op on the absent one — is the simplest correct rule and keeps the branch switch unchanged in structure.
- **Interleave-before-text is emergent:** opening text first calls `rs.close(yield)`; opening reasoning first calls `st.close(yield)`. The two never nest, and reasoning fully precedes text, without a dedicated "is this before text?" check.
- **Dim style resets per delta:** `renderReasoning` writes `\x1b[2m💭 ` once then `<delta>\x1b[0m` per delta, so partially-streamed CoT is always closed/readable. Consequence: the reasoning text is interspersed with ANSI escapes — the tests assert per-delta presence, not a joined substring (an over-strict initial assertion was corrected within the GREEN cycle).
- **File split:** reasoning tests moved to `translator_reasoning_test.go` to hold the 600-LOC cap (refactor-on-touch, CLAUDE.md).

## Deviations from Plan

Plan executed as written. One within-cycle test-assertion correction (not a code or behavior change):

### Within-cycle correction

**1. [Rule 1 — Test fix] Over-strict joined-substring assertion on interspersed ANSI output**
- **Found during:** Task 2 GREEN (first test run)
- **Issue:** The test asserted the writer output `Contains("let me think")` / `Contains("abc")`, but `renderReasoning` resets the dim style after each delta, so the deltas are interspersed with `\x1b[0m` escapes (`let me \x1b[0mthink`, `a\x1b[0mb\x1b[0mc`). The render is correct; the assertion was wrong.
- **Fix:** assert each reasoning delta is present individually (per-delta `Contains`), matching the intentional per-delta dim reset. No code change to `renderReasoning`.
- **Files modified:** `cmd/aura/chat_render_reasoning_test.go` (test only)
- **Commit:** `b9aff825`

### Refactor-on-touch (mandated)

**2. [CLAUDE.md — 600-LOC cap] Split reasoning tests into translator_reasoning_test.go**
- **Found during:** Task 1 RED commit (pre-commit hook blocked the commit)
- **Issue:** Adding the reasoning tests inline pushed `translator_test.go` to 706 LOC, over the hook-enforced 600 cap.
- **Fix:** moved the 5 reasoning tests + `reasoning()` helper + `assertReasoningQuartets` into a new `translator_reasoning_test.go`; the property-test extension stays inline in `translator_test.go` (it is interleaved into `TestTranslatorProperty`).
- **Files modified:** `internal/agui/translator_test.go`, `internal/agui/translator_reasoning_test.go`
- **Commit:** `78e087dd`

**Total deviations:** 0 functional. 1 within-cycle test-assertion correction + 1 mandated refactor-on-touch file split.

## Verification Results

- `go vet ./internal/agui/ ./cmd/aura/` — clean
- `go build ./...` — clean
- `go test -race ./internal/agui/ ./cmd/aura/` — PASS (race-clean; run with the Windows toolchain fix `BASH_ENV=~/.aura-toolchain.sh`, CI runs native Linux race)
- Reasoning property invariants + the 5 REASONING golden shapes (against the Plan-01-seeded fixture) — PASS
- `TestRenderRunnerTurnReasoning` — PASS (💭 + reasoning on writer, ABSENT from returned answer, `\x1b[2m` present)
- `bash scripts/agui_boundary_check.sh` — exit 0 (internal/agent closure free of internal/agui)
- `bash scripts/check-file-size.sh` — all Go files ≤ 600 LOC (translator.go 297, chat_render.go 216)
- `golangci-lint run ./internal/agui/ ./cmd/aura/` — 0 issues
- `grep -c 'NewReasoningStartEvent' internal/agui/translator.go` = 1; `grep -c 'NewReasoningID' internal/agui/types.go` = 3; `grep -c 'resp.Reasoning' cmd/aura/chat_render.go` = 2; `grep -c '💭' cmd/aura/chat_render.go` = 2
- No `prose.WriteString` on the reasoning path (only the answer `emit` closure at line 33)

## Known Stubs
None — this plan is a pure additive consumer of the Plan 12-05 data plane; no placeholders, empty data sources, or unwired surfaces introduced.

## Issues Encountered
None beyond the within-cycle test-assertion correction (above). Every spike-015 constructor signature matched the SDK on first build.

## User Setup Required
None — no external service configuration required. (The live `aura serve` SSE smoke and a live `aura chat` reasoning visual check land with the HTTP server in Plans 03/04 and the operator UAT.)

## Next Phase Readiness
- The translator now fans `agent.LLMResponse.Reasoning` out to a coalesced REASONING_* AG-UI stream — ready for Plan 12-02 fanout and Plan 12-03 HTTP server to carry CoT over the loopback SSE wire.
- The CLI renders live CoT today, masking the longest part of a DeepSeek-V4 turn (amendment #57e satisfied).
- Stream-only invariant holds end-to-end: reasoning is never persisted (Plan 12-05) and never enters the CLI answer buffer (this plan).
- No blockers.

## Self-Check: PASSED

- `internal/agui/translator.go` — FOUND
- `internal/agui/types.go` — FOUND
- `internal/agui/translator_test.go` — FOUND
- `internal/agui/translator_reasoning_test.go` — FOUND
- `cmd/aura/chat_render.go` — FOUND
- `cmd/aura/chat_render_reasoning_test.go` — FOUND
- commit `78e087dd` (Task 1 RED) — FOUND
- commit `70c1247c` (Task 1 GREEN) — FOUND
- commit `b9aff825` (Task 2) — FOUND

## TDD Gate Compliance

Task 1 followed RED → GREEN: a `test(12-06)` commit (`78e087dd`, 5 reasoning tests failing against the reasoning-less translator) precedes the `feat(12-06)` GREEN commit (`70c1247c`). REFACTOR (behavior) skipped (clean on first GREEN: 297 LOC, 0 lint issues); a mandated refactor-on-touch file split landed inside the RED commit to hold the 600-LOC cap. Task 2 folds test + impl per the plan (the field already existed from Plan 12-05; this is a render arm + its test).

---
*Phase: 12-ag-ui-gateway*
*Completed: 2026-06-06*
