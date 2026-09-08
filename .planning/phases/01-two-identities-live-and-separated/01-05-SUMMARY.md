---
phase: 01-two-identities-live-and-separated
plan: 05
subsystem: isolation-proof
tags: [concurrency, race-detector, goleak, gateway-ledger, sidecar-spillover, steer-inbox, kv-cache-prefix, iso-05]

# Dependency graph
requires:
  - phase: 01-two-identities-live-and-separated (plan 01)
    provides: "the eager sandbox provisioning leg and provisioned second identity this phase's isolation proofs assume exist"
provides:
  - "internal/agent/two_identity_concurrent_harness_test.go — the two-agent-under-two-identities construction mirroring runner.buildAgent, the identityRoutedClient demultiplexer, collisionTool/bigOutputTool test fixtures"
  - "internal/agent/two_identity_concurrent_test.go — TestTwoIdentityConcurrentRunsDoNotCross (5 subtests: collision, budget, steer, registry, result_attribution), TestTwoIdentityConcurrentSidecarPathsAreDisjoint, TestTwoIdentityConcurrentStablePrefixIsByteIdentical, TestTwoIdentityConcurrentHistoriesDoNotInterleave"
  - "internal/gateway/reservation_cross_identity_test.go — TestReservationKeyCrossIdentityDoesNotMerge (3 subtests), a uniqueKeyStore mimicking the production reservation UNIQUE-constraint"
  - "A recorded verdict for all four D-15 surfaces, each with a deliberate break proving the assertion actually catches the property it claims to guard"
affects: ["01-04 (make musr-e2e + CI must not assume a mutation-testing CI job already exists for internal/gateway — none is wired yet, per this plan's Manual-Only gap)", "01-06 (the phase-closing live run's execution-isolation half is now covered by this plan's evidence)"]

# Actuals (#2632)
actuals:
  tokens: 10462
  tasks: 3
  commits: 2
  plan_head_before: a7071bfbe265b03cfc86493b5df4a82579d613ff

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "identityRoutedClient: a thin llm.Client demultiplexer over TWO real agenttest.FakeClient instances, keyed on req.SessionID (not on ctx-derived identity), because a single shared FakeClient's global FIFO script ordinal cannot deterministically serve two concurrently-racing conversations"
    - "Deterministic forced-overlap via a 2-count sync.WaitGroup rendezvous barrier inside the tool under test (Done() then Wait()) rather than a start-signal alone — both calls are provably IN Execute together before either returns, which is what makes the collision test's 'overlapping in time' claim true by construction rather than by luck"
    - "uniqueKeyStore (internal/gateway/reservation_cross_identity_test.go): an in-memory store enforcing the SAME (conversation_id, request_id, tool_call_id) uniqueness the production ledger's UNIQUE index enforces, used instead of the package's existing fakeStore (which always reports acquired=true) specifically because a store that never denies anything would make a non-merge assertion vacuous"

key-files:
  created:
    - internal/agent/two_identity_concurrent_harness_test.go
    - internal/agent/two_identity_concurrent_test.go
    - internal/gateway/reservation_cross_identity_test.go
  modified: []

key-decisions:
  - "The 596-line measurement in the plan's own acceptance criterion for llm_agent.go was taken at plan time, BEFORE a same-day 10:43 CEST refactor (commit 96bd4bab6, a parallel non-GSD session) removed the paid-completion-critic code path. Re-measured at execution time: llm_agent.go is 576 lines, runner.buildAgent's construction (fresh agent.NewBudget + fresh agent.NewLlmAgent, shared registry/gateway/breaker/classifier/steer) is UNCHANGED by that refactor — confirmed by reading internal/runner/runner.go:375-419 directly rather than trusting the plan's stale citation. The plan's premise holds; only the line-count acceptance criterion's baseline number is stale, and the criterion itself (git diff --exit-code on llm_agent.go) still passes because this plan added no production code to it."
  - "The two-agent harness lives in package agent_test (external/black-box), not internal package agent, specifically to avoid an import cycle: internal/agent/agenttest imports internal/agent, so an internal white-box _test.go file in package agent cannot import agenttest without a build cycle — verified no existing file in the package does. Every assertion this plan needs (recorded LLM requests, tool results, Registry.All, Budget.ConsumeStep, SteerInbox.Drain) is reachable through LlmAgent's EXPORTED surface, so the black-box constraint cost nothing."
  - "Task 1's and Task 2's commits were split along the plan's declared file scope by temporarily truncating two_identity_concurrent_test.go to Task 1's five subtests only, committing, then restoring Task 2's three additional test functions and re-committing — because both tasks land in the same two files and a real TDD RED-before-GREEN split was not naturally available (see TDD Gate Compliance)."
  - "The mutation spot-check (Task 3b) was NOT completed locally this session. A first attempt (default exec-timeout, external 600s timeout) covered only 1 of internal/gateway's 11 files in the time given; a second attempt was slower still. The operator explicitly stopped the local run mid-session, directing it to CI instead. No score is reported — a fabricated or partial number would be worse than an honest gap. See 'Known Gaps' below."
  - "Discovered and reverted DURING this plan, not part of it: go-mutesting mutates its target source file IN PLACE in the real working tree and normally restores it before the next mutant; forcibly killing the process left internal/gateway/approvals.go mutated on disk (a broken condition, an invalid statement, an empty if-block). Caught only because the operator explicitly asked to verify the codebase was left clean; fixed with git checkout -- internal/gateway/approvals.go, confirmed byte-identical to HEAD and green under go vet/build/test afterward. No commit ever touched the mutated file."

requirements-completed: [ISO-05]  # declared ONLY by this plan (verified: no sibling plan in this phase's PLAN.md files declares ISO-05) — the shared-ID gate is a no-op here.

coverage:
  - id: D1
    description: "Two LlmAgent runs under two identityctx principals, sharing ONE tools.Registry/gateway.Gateway/llm.Client/SteerInbox, raced into the SAME tool with byte-identical arguments overlapping in time (deterministic rendezvous barrier) — the tool executes exactly twice, never replayed or shared, and each run's own result/history stays its own"
    requirement: "ISO-05"
    verification:
      - kind: unit
        ref: "internal/agent TestTwoIdentityConcurrentRunsDoNotCross/collision, /result_attribution (-race, 5x repeated, no goleak block via main_test.go's TestMain)"
        status: pass
    human_judgment: false
  - id: D2
    description: "Budget is per-turn, not process-wide: run A's MaxSteps=1 does not shorten run B's own step count (D-15 surface 3's Budget refinement, asserted rather than merely noted)"
    requirement: "ISO-05"
    verification:
      - kind: unit
        ref: "internal/agent TestTwoIdentityConcurrentRunsDoNotCross/budget (-race, 5x repeated)"
        status: pass
    human_judgment: false
  - id: D3
    description: "SteerInbox.Drain is conversation-keyed: a message pushed for A's conversation is drained by A alone, never delivered to B, and the shared tools.Registry is unmutated by either run"
    requirement: "ISO-05"
    verification:
      - kind: unit
        ref: "internal/agent TestTwoIdentityConcurrentRunsDoNotCross/steer, /registry (-race, 5x repeated)"
        status: pass
    human_judgment: false
  - id: D4
    description: "sidecarPath disjointness and cross-identity unreachability (with a positive control) through the real tools.NewResult/tools.ReadToolOutput production surface, plus path-escape refusal for a crafted spill id"
    requirement: "ISO-05"
    verification:
      - kind: unit
        ref: "internal/agent TestTwoIdentityConcurrentSidecarPathsAreDisjoint/paths_disjoint, /unreachable, /path_escape_refused (-race, 3x repeated)"
        status: pass
    human_judgment: false
  - id: D5
    description: "messages[0] (the byte-stable system prompt) is byte-identical between the two runs and carries no identity/session value; each run's own recorded history contains only its own turns, never the other's"
    requirement: "ISO-05"
    verification:
      - kind: unit
        ref: "internal/agent TestTwoIdentityConcurrentStablePrefixIsByteIdentical, TestTwoIdentityConcurrentHistoriesDoNotInterleave (-race, 3x repeated)"
        status: pass
    human_judgment: false
  - id: D6
    description: "ReservationKey values built from two identities' calls never merge, even with byte-identical tool+args and the SAME request_id+tool_call_id, because conversation_id differs — proven on the marshalled gatewayApprovalContext bytes and against a store enforcing the real uniqueness constraint, with a positive control"
    requirement: "ISO-05"
    verification:
      - kind: unit
        ref: "internal/gateway TestReservationKeyCrossIdentityDoesNotMerge/struct_disjoint, /approval_context_disjoint, /ledger_non_merge (-race, 3x repeated)"
        status: pass
    human_judgment: false
  - id: D7
    description: "Mutation spot-check ≥70% killed on internal/gateway's reservation-key machinery"
    requirement: "ISO-05"
    verification: []
    human_judgment: true
    rationale: "Not completed locally this session (see Known Gaps) — the operator directed it to CI. A human/CI operator must run go-mutesting ./internal/gateway/ to completion and record the score before this deliverable can be marked proven."

duration: ~2h20min
completed: 2026-09-08
status: complete
---

# Phase 01 Plan 05: Two-Identity Concurrent-Runner Proof (ISO-05) Summary

**Two `LlmAgent` runs, two identities, one process, one shared `tools.Registry`/`gateway.Gateway`/`llm.Client`/`SteerInbox` — raced into the same tool with byte-identical arguments on a deterministic rendezvous barrier, and every one of D-15's four shared surfaces asserted disjoint, with each assertion shown to fail when the property it guards is deliberately broken.**

## Performance

- **Duration:** ~2h20min
- **Tasks:** 3 completed (Task 1 collision/budget/steer/registry, Task 2 sidecar/prefix/interleaving + gateway ledger, Task 3 verdict + break-proof + mutation attempt)
- **Files modified:** 3 (all created, 0 production files touched)
- **Commits:** 2 own (`c5c7c75f5`, `ec0fb1f64`) — see Deviations for the interleaved foreign commit note

## Accomplishments

- `internal/agent/two_identity_concurrent_harness_test.go` builds two `LlmAgent`s EXACTLY the way `runner.buildAgent` does (fresh `agent.NewBudget` + fresh `agent.NewLlmAgent` per run), sharing ONE `*tools.Registry`, ONE `*gateway.Gateway`, ONE `SteerInbox` and ONE `llm.Client` — the four D-15 process-wide surfaces — while two distinct `identityctx` principals and (by default) distinct conversation UUIDs keep the runs genuinely separate. An `identityRoutedClient` demultiplexes the one shared client's `Stream` calls by `req.SessionID` because `agenttest.FakeClient`'s own script is a single global FIFO ordinal, unusable for two conversations racing into one object with deterministic per-run scripts.
- `TestTwoIdentityConcurrentRunsDoNotCross` (5 subtests): both runs call the SAME tool with byte-identical arguments, rendezvous-barriered (`sync.WaitGroup`, Done-then-Wait) so both are provably in `Execute` together — the tool runs exactly twice, never replayed or shared; run A's `MaxSteps=1` budget does not shorten run B's own 3-step script; a steer message pushed for A's conversation is drained by A alone; the shared registry is unmutated after both runs; each run's own final answer and tool-result content never cross over.
- `TestTwoIdentityConcurrentSidecarPathsAreDisjoint` proves the two D-15 surfaces separated only by UUID uniqueness, through the REAL `tools.NewResult`/`tools.ReadToolOutput`/`tools.WithToolCallContext` production surface (never the unexported `sidecarPath`/`validateID` directly): distinct sidecar paths under the fixed `conversations/` prefix, a positive control that A can read its own spill, and the actual assertion that B's `read_tool_output`, given A's spill id, resolves under B's OWN session and finds nothing — plus path-escape refusal for a crafted spill id.
- `TestTwoIdentityConcurrentStablePrefixIsByteIdentical` and `TestTwoIdentityConcurrentHistoriesDoNotInterleave` prove `messages[0]` is byte-identical between the two runs' first request with no identity/session value in it, and that each run's final recorded history contains only its own content.
- `internal/gateway/reservation_cross_identity_test.go`'s `TestReservationKeyCrossIdentityDoesNotMerge` proves a byte-identical tool+args dispatched under the SAME `request_id`+`tool_call_id` but a DIFFERENT `conversation_id` produces two distinct `ReservationKey`s, two distinct marshalled `gatewayApprovalContext` payloads, and two non-merged ledger reservations — against a `uniqueKeyStore` that enforces the SAME uniqueness the production store does (with a positive control proving the store actually denies a genuine repeat, so the non-merge result isn't vacuous).
- Task 3(a): all four surfaces were deliberately broken and reverted, each catching a named assertion — see the frontmatter's `key-decisions` and `01-VALIDATION.md`'s Manual-Only table for the full break-to-assertion mapping.

## Task Commits

1. **Task 1: two identities raced into the same tool, budget, steer, registry** - `c5c7c75f5` (test)
2. **Task 2: sidecar disjointness, stable prefix, ledger non-merge** - `ec0fb1f64` (test)
3. **Task 3: verdict recording + deliberate breaks + mutation attempt** - no code commit (Task 3 is verification + documentation; its only durable output is this SUMMARY and the `01-VALIDATION.md`/`.planning/WINDOWS.md` updates in the plan-metadata commit below)

**Plan metadata:** committed separately below (docs).

## Files Created/Modified

- `internal/agent/two_identity_concurrent_harness_test.go` (339 lines) — two-agent construction, `identityRoutedClient`, `collisionTool`, `bigOutputTool`/`bigOutputRecorder`, rendezvous helpers
- `internal/agent/two_identity_concurrent_test.go` (550 lines) — all seven test functions / thirteen subtests
- `internal/gateway/reservation_cross_identity_test.go` (144 lines) — `uniqueKeyStore` + `TestReservationKeyCrossIdentityDoesNotMerge`

## Decisions Made

See `key-decisions` in frontmatter for full detail. In prose: the plan's stale 596-line citation for `llm_agent.go` was re-verified against HEAD rather than trusted (576 lines now, `runner.buildAgent`'s construction unchanged by the same-day refactor); the harness lives in the external `agent_test` package to sidestep a real import cycle with `agenttest`; Task 1/Task 2 commits were split by temporarily truncating and restoring the shared test file along the plan's declared file boundaries; and the mutation spot-check was stopped mid-run by explicit operator instruction and is honestly reported as not completed rather than estimated or fabricated.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `tools.Registry.All()`'s documented unstable map-iteration order made the registry-unmutated assertion flaky**
- **Found during:** Task 1 (first full-suite run of `TestTwoIdentityConcurrentRunsDoNotCross`)
- **Issue:** The `registry` subtest compared two `tools.Registry.All()` snapshots without sorting; `Registry.All`'s own doc comment states the order is "deliberately not stable," so the comparison intermittently failed on pure iteration-order difference, not a real registry mutation (observed: PASS in isolation, FAIL in the full suite).
- **Fix:** Added `sort.Strings` before comparison in `registryToolNames` (harness file).
- **Files modified:** `internal/agent/two_identity_concurrent_harness_test.go`
- **Verification:** 5x repeated `-race` runs, all green.
- **Committed in:** `c5c7c75f5` (found and fixed before the commit, not as a separate patch)

**2. [Rule 1 - Bug] Steer-nonce regex matched the system prompt's OWN documentation example**
- **Found during:** Task 1 (steer subtest)
- **Issue:** `json.Marshal` HTML-escapes `<`/`>` by default, so a naive regex over marshalled JSON never matched the literal envelope; switching to plain-text concatenation surfaced a second bug — the system prompt's own `<steer_channel>` documentation contains the literal example `<user_steer nonce="...">` (three dots), which a permissive `[^"]+` capture matched before the real hex nonce.
- **Fix:** Search over plain-text message content (not `json.Marshal`'s HTML-escaped form) with a hex-only nonce capture `[0-9a-f]+`.
- **Files modified:** `internal/agent/two_identity_concurrent_test.go`
- **Verification:** 5x repeated `-race` runs, all green.
- **Committed in:** `c5c7c75f5` (found and fixed before the commit)

**3. [Rule 3 - Blocking] `go-mutesting` left a mutated file in the real working tree after a manual interrupt**
- **Found during:** Task 3(b), after the operator asked to verify the codebase was left clean
- **Issue:** `go-mutesting` mutates its target source file in place per mutant and normally restores it before the next; `pkill -f go-mutesting` (used to stop the run per the operator's explicit instruction) killed it before a restore, leaving `internal/gateway/approvals.go` with a broken condition, an invalid statement, and an empty `if {}` block — a non-compiling file in the real tree.
- **Fix:** `git checkout -- internal/gateway/approvals.go`.
- **Files modified:** `internal/gateway/approvals.go` (reverted, not modified — verified byte-identical to HEAD via `git diff --exit-code`)
- **Verification:** `go vet`/`go build`/`go test -race` on `internal/gateway` all green after the revert.
- **Committed in:** n/a — no commit ever touched the mutated file; this was caught and fixed before any staging.

---

**Total deviations:** 3 auto-fixed (2 test-only bugs, 1 blocking tool-interrupt cleanup). **Impact on plan:** All three were necessary for the plan's own evidence to be genuine rather than accidentally green (deviations 1–2) or for the working tree to be left clean (deviation 3). None touched production behavior; deviation 3 touched a production FILE only in the sense of reverting an accidental external mutation back to its committed state.

## Known Gaps

- **Mutation spot-check (Task 3b, plan's own `<verify>` block, CLAUDE.md gate) was NOT completed.** `go-mutesting ./internal/gateway/` is slow enough that a single 600-second attempt covered only 1 of 11 files in the package (91 mutations on `approvals.go`, not representative of the reservation-key machinery `reserve.go`/`approve.go`/`decide.go` this plan's evidence actually rests on); the operator explicitly stopped the local run mid-session and directed it to CI. **No score is reported.** Recorded in `.planning/WINDOWS.md` (id 27, kind `unrun-verify`, phase 01) and in `01-VALIDATION.md`'s Manual-Only table. This is a genuine open item for whichever CI job or follow-up session runs `go-mutesting` to completion against `internal/gateway`.
- **The sampled collision shape is one shape only (D-14's own stated limit, not new to this plan):** same tool, same arguments, overlapping in time. A leak visible only under a different collision shape (two different tools racing, a three-way race) is not covered — randomized interleaving was explicitly deferred by D-14, not hidden.

## TDD Gate Compliance

`gsd_run query task.is-behavior-adding` resolved this plan's tasks as `is_behavior_adding: false` ("tdd=\"true\" frontmatter absent" — the plan uses `<task type="tdd">` at the task level rather than a literal `tdd="true"` attribute, and every declared file is a `_test.go` file with zero production symbols). No RED-before-GREEN commit pair was required or produced.

A literal RED phase was not naturally available for most of this work in the sense the TDD reference means: the properties under test (per-turn `Budget`, conversation-keyed `SteerInbox`/`ReservationKey`, session-keyed `sidecarPath`, byte-stable `messages[0]`) are ALL already correctly implemented in production — writing the assertions and watching them fail first would have meant an artificial break (an undefined helper, a compile error), which the TDD reference's own fail-fast rule (#3770) classifies as INVALID_RED, not a real one; this mirrors the same finding recorded in `01-01-SUMMARY.md` and `01-03-SUMMARY.md` for the same reason.

Genuine RED evidence for every one of the four D-15 surfaces was instead produced the way Task 3(a) specifies: by deliberately breaking each production-observable property (shared `SessionID`, shared `*Budget`, session-id injection into the system message) and confirming the corresponding assertion actually goes red, then reverting. See `key-decisions` and `01-VALIDATION.md`'s Manual-Only table for the full mapping. No test was weakened to make anything pass.

Both commits are typed `test(01-05)`, touch only `_test.go` files, and land no production symbol.

## Issues Encountered

None beyond what is documented in Deviations and Known Gaps above.

## User Setup Required

None — no external service configuration required.

## Next Phase Readiness

- ISO-05's D-15 surfaces are now asserted with recorded verdicts; plan 01-06 (the phase-closing live run) can rely on this evidence for the execution-isolation half of its own closing argument.
- Plan 01-04 (`make musr-e2e` + CI) should NOT assume a CI mutation-testing job for `internal/gateway` already exists — none is wired; the Known Gaps entry above is the authoritative record until one runs.
- No blockers for the next plan in this phase's wave.

---
*Phase: 01-two-identities-live-and-separated*
*Completed: 2026-09-08*

## Self-Check: PASSED

- FOUND: `internal/agent/two_identity_concurrent_harness_test.go`
- FOUND: `internal/agent/two_identity_concurrent_test.go`
- FOUND: `internal/gateway/reservation_cross_identity_test.go`
- FOUND: commit `c5c7c75f5`
- FOUND: commit `ec0fb1f64`
- Plan-level `<verification>` re-run: `go test -race -count=1 ./internal/agent/... ./internal/gateway/ ./internal/steer/...` — all packages `ok`, no `DATA RACE` banner, no goleak block
- `git diff --exit-code -- internal/agent/llm_agent.go internal/agent/tools/result.go internal/gateway/approve.go scripts/coverage_package_policy.json` exit 0 — no production behavior changed
- `bash scripts/check-file-size.sh` exit 0 (2925 tracked files, all ≤600 LOC)
- `git status --porcelain -- internal/agent/ internal/gateway/` clean of any file this plan did not intentionally create (the `go-mutesting`-mutated `approvals.go` was caught and reverted before this check, confirmed via `git diff --exit-code`)
