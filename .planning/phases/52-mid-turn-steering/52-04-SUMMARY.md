---
phase: 52-mid-turn-steering
plan: 04
subsystem: api
tags: [ag-ui, sse, cockpit, steer, idempotency, wire-protocol, go]

# Dependency graph
requires:
  - phase: 52-mid-turn-steering (plan 02)
    provides: "internal/steer.Inbox (bounded FIFO), agent.SteerInbox interface, drainSteer, wrapUserSteer marker, Actions.SteerDelta wire shape"
provides:
  - "POST /agent/runs/{runID}/steer behind the same owner-scoped 404-hiding ladder and mandatory Idempotency-Key registration as cancel"
  - "SteerEventName = \"aura.steer\" CUSTOM echo frame, ring-buffered and Last-Event-ID replayable like every other frame"
  - "persistSteerTurn: drain-time RAW-text RoleUser persistence, keyed on delivery form"
  - "One process-wide *steer.Inbox singleton wired from cmd/aura into both runner.Deps.Steer and agui.Server, gated on cfg.AGUISteer.Enabled"
affects: [52-05, 52-06, 52-07, 52-08]

actuals:
  tokens: 18800
  tasks: 2
  commits: 2

tech-stack:
  added: []
  patterns:
    - "Route reuses resolveRunSession verbatim (zero new resolution logic) — the established shape for every run-scoped mutation route since cancel."
    - "Route renders internal/steer's own sentinels (ErrEmpty/ErrTooLarge/ErrQueueFull/ErrClosed) without re-deriving classification; the cap values live only in config."
    - "Per-file steer test convention: translator_steer_test.go / server_run_steer_test.go / runner_steer_test.go mirror the existing artifact/display per-event-type test file split."
    - "Nil-interface-safe injection: steerInboxOrNil converts a possibly-nil *steer.Inbox into a genuinely-nil agent.SteerInbox, avoiding the classic Go nil-concrete-in-interface trap."

key-files:
  created:
    - internal/agui/server_run_steer.go
    - internal/agui/server_run_steer_test.go
    - internal/agui/server_run_steer_e2e_test.go
    - internal/agui/translator_steer_test.go
    - internal/runner/runner_steer.go
    - internal/runner/runner_steer_test.go
  modified:
    - internal/agui/idempotency_http.go
    - internal/agui/server.go
    - internal/agui/translator.go
    - internal/runner/runner.go
    - internal/runner/runner_persist.go
    - cmd/aura/serve_agui.go
    - cmd/aura/chat_boot.go
    - cmd/aura/chat_boot_test.go
    - .gitignore

key-decisions:
  - "aura.steer payload key set (load-bearing for 52-05/52-07/52-08): {conversation_id, round, steers: [{id, source, text, delivery}]}. delivery is \"tool_result_append\" or \"user_message_fallback\" today; \"auto_delivery_next_turn\" is reserved for 52-05 and is explicitly a no-op in persistSteerTurn (see next point)."
  - "persistSteerTurn keys the persistence branch on the delivery form, not merely on SteerDelta being present. Only the two mid-run drain forms (tool_result_append, user_message_fallback) persist here; the reserved auto_delivery_next_turn form is deliberately skipped because 52-05's leftover auto-delivery drives its own follow-on turn whose AppendTurn already persists it — persisting it here too would produce two byte-identical RoleUser rows (STEER-04)."
  - "Closed the plan's called-out blocking_input: internal/steer/inbox.go's package-level fallbacks (Max=32, MaxBytes=32768) are LEFT IN PLACE for a hypothetical unwired caller, but the one production caller (cmd/aura's newSteerInbox) now explicitly threads config.AGUISteer.Max/MaxBytes (8/16384) into steer.New — pinned by TestNewSteerInboxWiresConfigCaps, which fails on a zero Config or a re-drifted default. The HTTP route re-derives no cap of its own."
  - "The plain-user_message_fallback delivery branch was exercised ONLY against agenttest.FakeClient in both TestSteerEndToEndRedirectsNextRound and TestRehydratedSteeredHistoryIsWireValid — never against a real provider. This mirrors the primary tool_result_append branch's own HTTP-level proof (which IS a real nested HTTP round-trip against the same httptest.Server, discovering the live run via RunRegistry.LiveForThread) but the fallback's LLM-facing wire shape is fake-client-only; no real-provider round-trip was run for it in this plan."
  - "Post-edit wc -l headroom for the next plans: internal/runner/runner.go = 596/600 (essentially no headroom left; the next touch needs the refactor-on-touch split), internal/agui/server.go = 554/600, internal/agui/translator.go = 479/600. internal/agui/server_run_steer.go = 81, internal/runner/runner_steer.go = 92 (both far under cap, room to grow if 52-05/52-06 add to them instead of runner.go/server.go directly)."

patterns-established:
  - "Steer sentinel rendering: a route classifies nothing internal/steer already classified — HTTP route code only maps sentinel -> status code."
  - "One composition-root singleton, two consumers, gated on its own feature flag (cfg.AGUISteer.Enabled) exactly like RunRegistry is gated on AGUIRun.Detach — a disabled flag wires a nil seam, not a half-live feature."

requirements-completed: [STEER-01, STEER-03]

coverage:
  - id: D1
    description: "POST /agent/runs/{runID}/steer resolves through the identical 404-hiding ladder cancel uses (nil registry/malformed id/registry miss/owner mismatch all -> 404, never 403), carries the mandatory Idempotency-Key registration, and renders internal/steer's sentinels without re-deriving classification"
    requirement: STEER-01
    verification:
      - kind: unit
        ref: "internal/agui/server_run_steer_test.go#TestHandleRunSteerHides404Ladder"
        status: pass
      - kind: unit
        ref: "internal/agui/server_run_steer_test.go#TestSteerRouteIsIdempotencyRegistered"
        status: pass
      - kind: unit
        ref: "internal/agui/server_run_steer_test.go#TestSteerRouteRendersInboxSentinels"
        status: pass
    human_judgment: false
  - id: D2
    description: "SteerEventName (\"aura.steer\") CUSTOM echo frame emitted beside the artifact/display frames, additive and nil-safe"
    requirement: STEER-03
    verification:
      - kind: unit
        ref: "internal/agui/translator_steer_test.go#TestSteerFrameIsCustomEvent"
        status: pass
      - kind: unit
        ref: "internal/agui/translator_steer_test.go#TestSteerFrameNilDeltaIsIgnored"
        status: pass
    human_judgment: false
  - id: D3
    description: "Drain-time RAW-text RoleUser persistence via persistSteerTurn, keyed on delivery form, never the marker/nonce"
    requirement: STEER-01
    verification:
      - kind: unit
        ref: "internal/runner/runner_steer_test.go#TestPersistedSteerCarriesRawText"
        status: pass
    human_judgment: false
  - id: D4
    description: "One process-wide *steer.Inbox singleton wired into both runner.Deps.Steer and agui.Server.SetSteerInbox, gated on cfg.AGUISteer.Enabled, with the wired caps pinned to config (closing the blocking_input cap-drift defect)"
    requirement: STEER-01
    verification:
      - kind: unit
        ref: "cmd/aura/chat_boot_test.go#TestNewSteerInboxWiresConfigCaps"
        status: pass
      - kind: unit
        ref: "cmd/aura/chat_boot_test.go#TestNewSteerInboxDisabledLeavesFieldNil"
        status: pass
      - kind: unit
        ref: "internal/runner/runner_steer_test.go#TestSteerInboxOrNil"
        status: pass
    human_judgment: false
  - id: D5
    description: "End-to-end redirect: a steer POSTed to a live cockpit run (driven through the real agui HTTP handler and a real runner) returns 202 and lands in round 2's request, for both delivery branches"
    requirement: STEER-01
    verification:
      - kind: integration
        ref: "internal/agui/server_run_steer_e2e_test.go#TestSteerEndToEndRedirectsNextRound/tool-result-append_branch"
        status: pass
      - kind: integration
        ref: "internal/agui/server_run_steer_e2e_test.go#TestSteerEndToEndRedirectsNextRound/user-message_fallback_branch"
        status: pass
    human_judgment: false
  - id: D6
    description: "Rehydrated steered history is wire-valid: no RoleUser turn sits between an assistant tool_calls turn and its answering tool results (closes FA-2)"
    requirement: STEER-03
    verification:
      - kind: integration
        ref: "internal/runner/runner_steer_test.go#TestRehydratedSteeredHistoryIsWireValid"
        status: pass
    human_judgment: false
  - id: D7
    description: "A resume (Last-Event-ID) replays the aura.steer frame exactly where it landed: included when acknowledged through seq-1, omitted once acknowledged at seq (STEER-03's third leg)"
    requirement: STEER-03
    verification:
      - kind: integration
        ref: "internal/agui/server_run_steer_test.go#TestSteerFrameReplaysFromSeq"
        status: pass
    human_judgment: false

duration: ~45min measured between the Task 1 and Task 2 commits (dbec0dcc4 18:17:22+02:00 -> 03b7d7d32 19:02:02+02:00); does not include prior investigation time from an earlier, separately-billed session context.
completed: 2026-08-25
status: complete
---

# Phase 52 Plan 04: Mid-turn steering — the route, the echo, the persistence and the wiring Summary

**`POST /agent/runs/{runID}/steer` reaches the live agent inbox end to end: 404-hiding ladder + mandatory Idempotency-Key, an `aura.steer` CUSTOM echo frame that survives a Last-Event-ID resume, and drain-time RAW-text persistence — proven by a real HTTP round-trip fired from inside the agent's own concurrent tool-dispatch goroutine.**

## Performance

- **Duration:** ~45 min measured between the two task commits (see frontmatter `duration` for the exact caveat)
- **Completed:** 2026-08-25
- **Tasks:** 2/2
- **Files modified:** 15 (13 in Task 1, 4 in Task 2, 2 overlapping)

## Accomplishments

- `POST /agent/runs/{runID}/steer` resolves through `resolveRunSession` verbatim — the same 404-hiding ladder `handleRunCancel` uses, no 403 anywhere, registered in `httpMutationRoutes` as `agent_run_steer` (mandatory Idempotency-Key).
- `SteerEventName = "aura.steer"` CUSTOM frame added beside `aura.artifact`/`aura.display`/`aura.mcp_view`, ring-buffered like every other frame — proven to survive a `Last-Event-ID` resume in both directions (included at `seq-1`, excluded at `seq`).
- `persistSteerTurn` (new `internal/runner/runner_steer.go`) appends the operator's RAW text as a `RoleUser` turn at the drain point, keyed on delivery form so the reserved `auto_delivery_next_turn` form (52-05) is never double-persisted.
- One process-wide `*steer.Inbox` constructed in `cmd/aura/chat_boot.go`, gated on `cfg.AGUISteer.Enabled`, injected into both `runner.Deps.Steer` and `agui.Server.SetSteerInbox` — closing the plan's called-out blocking_input by explicitly threading `config.AGUISteer{Max, MaxBytes}` into the one production `steer.New` call site.
- Three integration proofs: a real run driven through the real agui HTTP handler with a genuine nested HTTP POST from inside the concurrent tool-dispatch goroutine (`TestSteerEndToEndRedirectsNextRound`), wire-valid rehydration through the real loader closing FA-2 (`TestRehydratedSteeredHistoryIsWireValid`), and a Last-Event-ID resume replay proof closing STEER-03's third leg (`TestSteerFrameReplaysFromSeq`).

## Task Commits

1. **Task 1: The route, the echo frame, the persistence and the wiring** - `dbec0dcc4` (feat)
2. **Task 2: The three proofs** - `03b7d7d32` (test)

_Note: no plan metadata commit yet — that follows this SUMMARY per the executor's final_commit step._

## Files Created/Modified

- `internal/agui/server_run_steer.go` - `handleRunSteer`: resolve → decode → `Push` → 202, or render a sentinel
- `internal/agui/server_run_steer_test.go` - 404 ladder, idempotency registration, sentinel rendering, resume replay proof
- `internal/agui/server_run_steer_e2e_test.go` - full-stack end-to-end redirect proof (new fakes for a cross-package real-HTTP drive)
- `internal/agui/translator_steer_test.go` - `aura.steer` CUSTOM emission tests (split out of `translator_test.go` for the 600-LOC cap)
- `internal/agui/idempotency_http.go` - `agent_run_steer` mutation-route registration
- `internal/agui/server.go` - `steer` field, `SetSteerInbox`, mux registration
- `internal/agui/translator.go` - `SteerEventName` constant + emission branch
- `internal/runner/runner_steer.go` - `persistSteerTurn`, `steerInboxOrNil`, delivery-form gate
- `internal/runner/runner_steer_test.go` - persistence unit tests + the two integration proofs (`TestRehydratedSteeredHistoryIsWireValid`, `TestSteerInboxOrNil`)
- `internal/runner/runner.go` - `steer` field, `Deps.Steer`, one line in `buildAgent`'s `LlmAgentConfig`
- `internal/runner/runner_persist.go` - one branch in `persistEvent` routing to `persistSteerTurn`
- `cmd/aura/serve_agui.go` - `SetSteerInbox` wiring
- `cmd/aura/chat_boot.go` - `chatEnv.steer`, `newSteerInbox`, gated construction
- `cmd/aura/chat_boot_test.go` - `TestNewSteerInboxWiresConfigCaps`, `TestNewSteerInboxDisabledLeavesFieldNil`
- `.gitignore` - `internal/agui/conversations/` (AURA_RUN_DIR-unset test runtime artifact)

## Decisions Made

See `key-decisions` in the frontmatter for the `aura.steer` payload shape, the delivery-form persistence gate, the blocking_input cap-wiring closure, the fallback-branch fake-vs-real-provider honesty note, and the post-edit `wc -l` headroom for `runner.go`/`server.go`/`translator.go`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] File-size pre-commit hook failure on Task 1**
- **Found during:** Task 1, first commit attempt
- **Issue:** Appending the `aura.steer` CUSTOM-frame tests directly into `internal/agui/translator_test.go` pushed it to 652 LOC, over the CLAUDE.md 600-LOC cap; the `check-file-size` pre-commit hook blocked the commit.
- **Fix:** Extracted the new tests into `internal/agui/translator_steer_test.go`, mirroring the existing `translator_artifact_test.go`/`translator_display_test.go` per-event-type test file convention. `translator_test.go` returned to its original 579-line content (net-zero diff vs HEAD).
- **Files modified:** `internal/agui/translator_test.go` (reverted), `internal/agui/translator_steer_test.go` (new)
- **Verification:** `wc -l` both files, re-ran `go test ./internal/agui/`, commit succeeded.
- **Committed in:** `dbec0dcc4`

**2. [Rule 1 - Bug] `fastReplyFor` "ciao" greeting fast-path collided with the shared `runPayload` test helper**
- **Found during:** Task 2, `TestSteerEndToEndRedirectsNextRound`
- **Issue:** The package's shared `runPayload(threadID)` helper hardcodes the user content to `"ciao"`, which `runner.fastReplyFor` intercepts as a trivial-greeting fast path BEFORE the LLM client is ever called — so the scripted `agenttest.FakeClient` was never invoked and the test observed a plain reply with no tool calls.
- **Fix:** Added a local `steerRunPayload(threadID string) string` helper in the new e2e test file using non-greeting text, and used it in place of `runPayload` for both subtests.
- **Files modified:** `internal/agui/server_run_steer_e2e_test.go`
- **Verification:** Both subtests of `TestSteerEndToEndRedirectsNextRound` pass.
- **Committed in:** `03b7d7d32`

**3. [Rule 1 - Bug] `steerViaHTTPTool.Execute` panicked on a nil `*RunRegistry`**
- **Found during:** Task 2, `TestSteerEndToEndRedirectsNextRound`/tool-result-append branch
- **Issue:** The test tool's `runs` field (`*RunRegistry`, needed to resolve the live run's own id via `LiveForThread`) was never assigned — only `baseURL` was set after constructing the test server — causing a nil-pointer panic inside the agent's concurrent tool-dispatch goroutine (caught and logged by the agent's panic-recovery wrapper as `ERROR agent recovered panic site=other`), which surfaced as `steer POST status = 0`.
- **Fix:** Added `tool.runs = s.runs` immediately after `tool.baseURL = srv.URL`.
- **Files modified:** `internal/agui/server_run_steer_e2e_test.go`
- **Verification:** Subtest passes; `go build ./...` and `go vet ./...` exit 0.
- **Committed in:** `03b7d7d32`

**4. [Rule 3 - Blocking] `readAllClose`'s single-`Read` insufficient for a full SSE drain**
- **Found during:** Task 2, while writing the end-to-end test
- **Issue:** Task 1's `readAllClose` helper (a single `Read()` into a 4096-byte buffer, adequate for short error bodies) would return before the detached run's SSE stream naturally closed at turn end, risking a truncated capture that might miss the `aura.steer` echo frame or return before the concurrent `tool.Execute` had actually run.
- **Fix:** Added a dedicated `readFullBody(t, resp) string` helper using `io.ReadAll(resp.Body)`, which blocks until the stream closes; used exclusively in the e2e test.
- **Files modified:** `internal/agui/server_run_steer_e2e_test.go`
- **Verification:** Both e2e subtests observe the complete frame set.
- **Committed in:** `03b7d7d32`

**5. [Rule 1/3 - Lint] `golangci-lint` modernize findings blocked Task 2's commit**
- **Found during:** Task 2, pre-commit hook
- **Issue:** `string += string` in a loop (`stringsbuilder`, 2 sites) and a classic-index loop eligible for `range` (`rangeint`, 1 site) tripped the lint gate.
- **Fix:** Added a `joinMessageContents([]llm.Message) string` helper using `strings.Builder`, used at both sites in `server_run_steer_e2e_test.go`; rewrote the classic `for i := 0; i < len(hist); i++` in `runner_steer_test.go`'s `assertWireValidHistory` as `for i := range hist`.
- **Files modified:** `internal/agui/server_run_steer_e2e_test.go`, `internal/runner/runner_steer_test.go`
- **Verification:** `golangci-lint` (via the pre-commit hook) reports 0 issues; tests still pass.
- **Committed in:** `03b7d7d32`

**6. [Rule 2/3 - Missing gitignore entry] `internal/agui/conversations/` runtime artifact**
- **Found during:** Task 2, post-test `git status` check
- **Issue:** Running the new real-tool-dispatch e2e test wrote sidecar tool-result files to a relative `internal/agui/conversations/` directory (the `AURA_RUN_DIR`-unset fallback path), which is the same runtime-artifact pattern already gitignored for `cmd/aura/conversations/` and `internal/runner/conversations/*` — but this is the first test in `internal/agui` to trigger it.
- **Fix:** Added `internal/agui/conversations/` to `.gitignore` beside the existing entries; deleted the generated directory rather than committing it.
- **Files modified:** `.gitignore`
- **Verification:** `git status --short` clean after deletion; the directory regenerates and is ignored on subsequent test runs.
- **Committed in:** `03b7d7d32`

---

**Total deviations:** 6 auto-fixed (1 blocking file-size, 2 bugs, 1 blocking truncated-read risk, 1 blocking lint, 1 missing gitignore entry).
**Impact on plan:** All auto-fixes were necessary to land a correct, lint-clean, cap-compliant commit. No scope creep — every fix was directly caused by this plan's own new code or new tests.

## Issues Encountered

None beyond the deviations above — no blockers that required stopping and asking, no architectural changes, no authentication gates.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- The `aura.steer` payload key set (`{conversation_id, round, steers: [{id, source, text, delivery}]}`) and the `tool_result_append`/`user_message_fallback` delivery values are load-bearing for 52-05 (adds `auto_delivery_next_turn`), 52-06 (Telegram's own sentinel rendering), 52-07 (cockpit consumption) and 52-08.
- `internal/runner/runner.go` is at 596/600 LOC — the next plan touching it must do the refactor-on-touch split before adding anything. `internal/agui/server.go` (554/600) and `internal/agui/translator.go` (479/600) have more but not unlimited headroom.
- `persistSteerTurn`'s delivery-form gate already anticipates 52-05's reserved `auto_delivery_next_turn` value and explicitly skips it — 52-05 does not need to retrofit this branch.
- The plain-`user_message_fallback` branch has never been exercised against a real LLM provider, only `agenttest.FakeClient` — a future plan or manual QA pass may want a real-provider smoke of that specific branch before it is fully trusted end to end.

## Self-Check: PASSED

- FOUND: internal/agui/server_run_steer.go
- FOUND: internal/agui/server_run_steer_test.go
- FOUND: internal/agui/server_run_steer_e2e_test.go
- FOUND: internal/agui/translator_steer_test.go
- FOUND: internal/runner/runner_steer.go
- FOUND: internal/runner/runner_steer_test.go
- FOUND: commit dbec0dcc4
- FOUND: commit 03b7d7d32

---
*Phase: 52-mid-turn-steering*
*Completed: 2026-08-25*
