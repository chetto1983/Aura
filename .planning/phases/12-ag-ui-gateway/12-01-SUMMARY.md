---
phase: 12-ag-ui-gateway
plan: 01
subsystem: api
tags: [ag-ui, sse, translator, iter-seq2, property-testing, go-list-deps, supply-chain-pin]

# Dependency graph
requires:
  - phase: 03-agent-runtime (Slice 0.9)
    provides: "internal/agent.Event / Actions / LLMResponse / ToolInvocation / AwaitingInput stream shape consumed one-way by the translator"
  - phase: 04-llm-client (Slice 1)
    provides: "per-token chunk Event emission (chunkEvent) + final Event (FinishReason) + tool-result preview overload (StateDelta tool_call_id marker)"
provides:
  - "internal/agui package: the pure AG-UI translator over iter.Seq2[*agent.Event,error]"
  - "agui.Translate(threadID, runID, idgen, seq) → iter.Seq2[events.Event,error] — chunk-coalescing state machine"
  - "agui.IDGenerator interface + default uuid-v4 impl (translator owns non-empty id minting)"
  - "agui.ConversationStore narrow interface (Get/LoadHistory) for Plan 03/04 server"
  - "agui.ValidateRunInput Aura-semantic RunAgentInput guard"
  - "AG-UI SDK pinned to immutable pseudo-version v0.0.0-20260514093510-e9e910b230b9"
  - "scripts/agui_boundary_check.sh + CI boundary (SC2) + pin (SC4) gates"
  - "internal/agui/testdata/golden-events.json — 21 verified AG-UI wire shapes"
affects: [12-02-fanout, 12-03-server, 12-04-serve-wiring, 13-telegram]

# Tech tracking
tech-stack:
  added:
    - "github.com/ag-ui-protocol/ag-ui/sdks/community/go v0.0.0-20260514093510-e9e910b230b9 (+ transitive github.com/sirupsen/logrus v1.9.3)"
  patterns:
    - "Pure iter.Seq2 translator (range-over-func), no goroutines/IO — property + golden testable in isolation"
    - "go list -deps closure gate for one-way import boundaries (D-17), lighter than depguard"
    - "Pseudo-version-literal CI grep for an untagged-subdir module pin (amendment #56, Pitfall 3)"

key-files:
  created:
    - "internal/agui/types.go — ValidateRunInput + ConversationStore + IDGenerator"
    - "internal/agui/translator.go — pure chunk-coalescing Event→AG-UI state machine"
    - "internal/agui/translator_test.go — rapid property + golden + behavior tests"
    - "internal/agui/main_test.go — goleak.VerifyTestMain"
    - "internal/agui/testdata/golden-events.json — 21 golden wire shapes (spike 015)"
    - "scripts/agui_boundary_check.sh — SC2 boundary gate"
  modified:
    - "go.mod / go.sum — AG-UI SDK pseudo-version pin + transitive logrus sum"
    - ".github/workflows/ci.yml — boundary step (build-and-lint) + pin-grep step (cache-invariant job)"

key-decisions:
  - "Run-boundary policy locked OQ1 (a): stream per-token deltas, treat the final Event (FinishReason) as TEXT_MESSAGE_END-only and do NOT re-emit its full Content — no double-stream"
  - "Tool-result preview disambiguated by the Actions.StateDelta[\"tool_call_id\"] marker BEFORE the prose branch (Pitfall 2) → TOOL_CALL_RESULT, never TEXT_MESSAGE"
  - "TOOL_CALL_RESULT messageId is a translator-minted correlation id (msg-tool-<callId>), not a wire contract — golden compare asserts non-empty, not byte-equal to the spike's illustrative msg-2"
  - "SC4 CI gate greps the pseudo-version literal with an exactly-1-match assertion; never the 40-hex SHA (invalid go.mod syntax, 0 matches forever)"

patterns-established:
  - "Pattern 1: chunk-coalescing textRunState — START lazily on first non-empty delta, CONTENT per non-empty delta, END on any run interruption (tool/state/final/pause/stream-end)"
  - "Pattern 2: AG-UI translator boundary one-way + go list -deps CI enforcement"

requirements-completed: [UX-01]

# Metrics
duration: 18 min
completed: 2026-06-06
---

# Phase 12 Plan 01: AG-UI Translator Core Summary

**Pure chunk-coalescing translator that maps Aura's per-token `*agent.Event` stream onto a validated AG-UI `events.Event` sequence, behind an immutable SDK pin and a `go list -deps` one-way-boundary CI gate.**

## Performance

- **Duration:** ~18 min
- **Started:** 2026-06-06T21:23:37Z
- **Completed:** 2026-06-06T21:31:00Z
- **Tasks:** 2
- **Files modified:** 9 (6 created, 3 modified)

## Accomplishments
- Pinned the AG-UI community Go SDK to the immutable pseudo-version literal `v0.0.0-20260514093510-e9e910b230b9` via the mandatory two-step `go get` (the second resolves the transitive `logrus` go.sum entry that nothing-yet-imports would otherwise miss).
- Stood up the SC2 boundary gate (`scripts/agui_boundary_check.sh`) and the SC4 pin gate in CI BEFORE any agui code — both green on a clean tree and after the package lands.
- Built the two genuinely-new artifacts: `types.go` (Aura-semantic validation + narrow `ConversationStore` interface + `IDGenerator`) and the pure `translator.go` state machine — the only non-trivial logic in the phase.
- Translator handles every Pitfall the research flagged: per-token delta coalescing (Pitfall 1), tool-result-preview disambiguation (Pitfall 2), empty-delta skip, non-empty id guarantee, sorted StateDelta keys, interrupt-then-stop, error-then-stop, REASONING_* (not deprecated THINKING_*).
- Property test (rapid, random 1..20 Event mix) + golden-shape tests over the 21 emitted types + 9 targeted behavior tests, all green under `-race`, goleak-clean, golangci-lint 0 issues.

## Task Commits

Each task was committed atomically (Task 2 is TDD — RED then GREEN):

1. **Task 1: Pin SDK + boundary/pin CI gates + golden fixtures (Wave 0)** — `8871011d` (feat)
2. **Task 2 (RED): failing translator property + golden tests** — `021b64c0` (test)
3. **Task 2 (GREEN): pure translator state machine + types.go** — `79d48490` (feat)

REFACTOR gate: not needed — translator.go landed at 228 LOC (cap 600), 0 lint issues, no duplication; comments confined to the non-obvious WHYs (chunk coalescing, tool-result marker, OQ1 policy) per CLAUDE.md.

## Files Created/Modified
- `internal/agui/types.go` — `ValidateRunInput` (threadId+messages guard; SDK owns JSON parse), `ConversationStore` narrow interface (`Get`/`LoadHistory`, D-A2-02), `IDGenerator` + default uuid-v4 impl.
- `internal/agui/translator.go` — pure `Translate`; `textRunState` chunk-coalescer; `emitToolInvocation`; `toolResultCallID` marker; `stateDeltaOps` (sorted keys); `interruptFrom` + `responseSchema`.
- `internal/agui/translator_test.go` — rapid property test + golden-shape assertions + behavior tests (coalescing, 200-delta, tool-result-≠-text, no-double-stream, interrupt, error, tool lifecycle, sorted keys).
- `internal/agui/main_test.go` — `goleak.VerifyTestMain`.
- `internal/agui/testdata/golden-events.json` — 21 golden wire shapes copied verbatim from spike 015.
- `scripts/agui_boundary_check.sh` — `go list -deps ./internal/agent/...` closure gate (executable bit set).
- `go.mod` / `go.sum` — SDK pseudo-version pin + transitive logrus sum.
- `.github/workflows/ci.yml` — boundary step in `build-and-lint`; pseudo-version-literal grep (exactly-1 assertion) in the Postgres-free `cache-invariant` job.

## Decisions Made
- **Run-boundary OQ1 policy (a):** stream the per-token deltas; the final Event (FinishReason set) closes the open text run as END-only and its full Content is NOT re-streamed as a CONTENT (avoids the double-stream Pitfall 1 warned about). Its usage StateDelta becomes a STATE_DELTA.
- **Tool-result marker first:** the `Actions.StateDelta["tool_call_id"]` branch is checked BEFORE the prose branch, so tool-output previews map to TOOL_CALL_RESULT, never to TEXT_MESSAGE (Pitfall 2).
- **SC4 gate shape:** greps the pseudo-version literal with `[ "$(grep -cF ... go.mod)" = "1" ]` — never the 40-hex SHA (invalid go.mod syntax, would be 0 matches forever — Pitfall 3 / amendment #56).
- **IDGenerator owns ids:** translator mints every messageId/toolCallId so the SDK's Validate() (which rejects empty ids and empty deltas) always passes.

## Deviations from Plan

None - plan executed exactly as written.

The one test refinement (asserting TOOL_CALL_RESULT's `messageId` is non-empty rather than byte-equal to the spike golden's illustrative `msg-2`) is a within-cycle GREEN-phase correction of an over-assertion, not a deviation from the planned behavior: the plan specifies the IDGenerator mints the tool-result id, so the spike's arbitrary value was never a wire contract. The change is documented in the GREEN commit body and in the test's comment.

## Issues Encountered
None. The two-step `go get` resolved the logrus transitive sum exactly as the research predicted; every SDK constructor signature matched the spike-verified set on first build.

## User Setup Required
None - no external service configuration required. (The live `aura serve` + `curl` operator smoke and the Postgres-backed server integration tier land in Plans 03/04.)

## Next Phase Readiness
- The pure translator is ready to back both consumers: Plan 02 in-process fanout (Phase-13 Telegram path) and Plan 03 HTTP server (`POST /agent/run` SSE).
- `ConversationStore` narrow interface is declared for the Plan 03/04 server; `events.Message = coretypes.Message` confirmed for the future `GET /threads/<id>/messages` MESSAGES_SNAPSHOT projection.
- Boundary + pin gates are wired; the `db_integration` agui tier is intentionally deferred to Plan 04 (once `server_test.go` exists) per the plan.
- No blockers.

## Self-Check: PASSED

- `internal/agui/types.go` — FOUND
- `internal/agui/translator.go` — FOUND
- `internal/agui/translator_test.go` — FOUND
- `internal/agui/main_test.go` — FOUND
- `internal/agui/testdata/golden-events.json` — FOUND
- `scripts/agui_boundary_check.sh` — FOUND
- commit `8871011d` (Task 1) — FOUND
- commit `021b64c0` (Task 2 RED) — FOUND
- commit `79d48490` (Task 2 GREEN) — FOUND
- `grep -cF 'v0.0.0-20260514093510-e9e910b230b9' go.mod` == 1 — PASS
- `bash scripts/agui_boundary_check.sh` exit 0 — PASS
- `go vet ./... && go build ./... && go test -race ./internal/agui/` — PASS
- `bash scripts/check-file-size.sh` + `golangci-lint run ./internal/agui/` (0 issues) — PASS

## TDD Gate Compliance

Task 2 followed RED → GREEN: a `test(12-01)` commit (`021b64c0`, 9 behavior tests failing against a stub translator) precedes the `feat(12-01)` GREEN commit (`79d48490`). REFACTOR gate skipped (clean on first GREEN: 228 LOC, 0 lint issues).

---
*Phase: 12-ag-ui-gateway*
*Completed: 2026-06-06*
