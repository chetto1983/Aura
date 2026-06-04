---
phase: 09-swarm-minimal
plan: 02
subsystem: agent-runtime
tags: [swarm, errgroup, goleak, rapid, budget, concurrency, parallel]

# Dependency graph
requires:
  - phase: 09-01
    provides: locked CONTEXT decisions D-01..D-25 (swarm v1 shape, PRD-amended)
  - phase: 03 (Slice 1)
    provides: LlmAgent + NewLlmAgent worker construction, AwaitingInput pause Event, tools.NewResult spillover
  - phase: 02 (Slice 0.9)
    provides: ParallelAgent leak-safety idioms, Budget tree (shared *atomic.Int32), Budget.Child
provides:
  - Ephemeral per-call swarm runner (internal/swarm.Run) — fan-out + waves + per-child isolation + budget pre-flight + ordered []ChildReport
  - ChildReport contract (D-15), structuredBrief builder (D-06/D-07), Without registry helper (D-08/D-10)
  - Per-child transcript dump (D-18, flat-id direct write)
  - AURA_SWARM_MAX_DEPTH code guard (D-10 forward-compat)
  - 3 new config env vars (AURA_SWARM_MAX_GOALS, AURA_SWARM_CHILD_TIMEOUT_SEC, AURA_SWARM_MAX_CONCURRENT)
affects: [09-03, 09-04, 09-05, swarm_spawn tool, ask_user proxied wiring]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Bypass-but-copy: reuse ParallelAgent leak idioms verbatim, invert its cancel-siblings semantics (D-02 partial results)"
    - "Pause-as-report: worker AwaitingInput Event becomes a {needs_user_input} report slot, no child suspend (D-04)"
    - "Flat session-id transcript: <conv>-swarm-w<i> SessionID + separate <runDir>/<conv>/swarm/w<i>.jsonl direct write (Pitfall 4)"

key-files:
  created:
    - internal/swarm/swarm.go
    - internal/swarm/swarm_depth.go
    - internal/swarm/report.go
    - internal/swarm/brief.go
    - internal/swarm/registry.go
    - internal/swarm/swarm_test.go
    - internal/swarm/swarm_property_test.go
    - internal/swarm/main_test.go
    - internal/swarm/report_test.go
    - internal/swarm/brief_registry_test.go
  modified:
    - internal/config/config.go
    - internal/config/config_test.go

key-decisions:
  - "Run returns (jsonReports, error); domain rejections (depth/goals/budget) ride in a model-readable error string, not the Go error slot (D-15)"
  - "Added AURA_SWARM_MAX_CONCURRENT to config (Rule 3): the runner needs the wave-width cap and it was absent from config.go despite the PRD catalog"
  - "Worker framing rides in messages[1]/UserTurns (D-06 byte-stable messages[0]), no LlmAgentConfig.SystemOverlay field"
  - "D-11 timeout normalized to a uniform {failed,'timeout'} regardless of how the worker surfaced the cancellation"

patterns-established:
  - "Pattern: goleak TestMain (main_test.go) + per-test defer goleak.VerifyNone(t) across the concurrent runner tier (D-25)"
  - "Pattern: routerClient test fixture — one goroutine-safe llm.Client routes per-goal outcomes by brief substring, driving N concurrent workers deterministically"

requirements-completed: [CAP-03]

# Metrics
duration: ~50min
completed: 2026-06-04
---

# Phase 9 Plan 02: Ephemeral Swarm Runner Summary

**Budget-bounded leak-safe swarm engine (internal/swarm.Run) that fans goals out as LlmAgent workers in AURA_SWARM_MAX_CONCURRENT waves, isolates per-child failures with NO sibling cancellation (D-02), and collects an ordered []ChildReport — full unit + rapid tier green under -race + goleak.**

## Performance

- **Duration:** ~50 min
- **Completed:** 2026-06-04
- **Tasks:** 2 (both TDD)
- **Files modified:** 12 (10 created, 2 modified)

## Accomplishments
- Ephemeral runner with D-09 budget pre-flight (snapshot-once), D-13 goals cap, D-10 depth guard, then wave-split fan-out
- D-02 failure isolation: a child error sets reports[i]={failed} and the goroutine returns NIL, so egCtx never cancels siblings (the inverse of ParallelAgent) while copying its leak-safety idioms verbatim (errgroup.WithContext, defer cancel, #61611 spawn-loop guard, per-child WithTimeout+defer)
- D-04 pause-as-report, D-11 per-child timeout → {failed,"timeout"}, D-18 flat-id transcript dump (best-effort, swallows write errors)
- Full test tier: SC#1 timing, SC#3 5-pause, SC#4 shared-budget tree bound, D-02/D-09/D-10/D-11/D-18 + D-25 rapid properties (goals 1..8, randomized outcome mix) — green under `go test -race` + goleak

## Task Commits

1. **Task 1: ChildReport + brief + Without + config** - `d4ccf6d8` (feat)
2. **Task 2: Ephemeral runner + depth guard + SC/D tests** - `26e980c4` (feat)

_TDD note: tests and implementation landed together per task (the package is new; RED would have been a no-compile). Both commits carry the test tier alongside the code._

## Files Created/Modified
- `internal/swarm/swarm.go` - ephemeral runner: pre-flight, waves, per-child isolation, pause-as-report, transcript dump, slog lifecycle (194 LOC)
- `internal/swarm/swarm_depth.go` - AURA_SWARM_MAX_DEPTH code guard (MAX_SPAWN_DEPTH=<cap> exceeded)
- `internal/swarm/report.go` - ChildReport contract (D-15) + dumpTranscript (D-18) + marshalReports
- `internal/swarm/brief.go` - D-06 worker overlay + D-07 4-part structured brief (load-bearing literals)
- `internal/swarm/registry.go` - Without(parent, names...) fresh-registry-minus-named helper (D-08/D-10)
- `internal/swarm/swarm_test.go` - routerClient fixture + SC#1/#3/#4 + D-02/D-09/D-10/D-11/D-18 tests
- `internal/swarm/swarm_property_test.go` - D-25 rapid properties
- `internal/swarm/{main,report,brief_registry}_test.go` - goleak TestMain + Task 1 unit tests
- `internal/config/config.go` + `config_test.go` - 3 swarm env vars (defaults 8 / 120 / 4) + tests

## Decisions Made
- **Run signature:** free function `Run(ctx, RunConfig, goals) (string, error)`. RunConfig bundles ParentBudget/ParentRegistry/Client/LLM/Cfg/ConvID/Depth so the param list stays clean and 09-05 can wire the swarm_spawn tool to it directly. The marshaled `[]ChildReport` JSON is returned as a string for the caller to wrap in `tools.NewResult` (D-15, the only spillover).
- **Worker overlay in messages[1]:** picked RESEARCH option (b) — the D-06 worker framing rides in the D-07 user brief, keeping messages[0] byte-identical across workers (KV-cache invariant). No new `LlmAgentConfig.SystemOverlay` field.
- **Timeout normalization:** D-11 sets `{failed,"timeout"}` whenever `ctx.Err()==DeadlineExceeded`, regardless of whether the worker surfaced the cancellation as a stream error — a uniform, model-readable signal.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Added AURA_SWARM_MAX_CONCURRENT to config**
- **Found during:** Task 1 (config) / needed by Task 2 (runner waves)
- **Issue:** The plan's runner action and RESEARCH (D-12) require waves of ≤ `AURA_SWARM_MAX_CONCURRENT`, and PATTERNS asserts it "already exists (PRD line 4766)" — but it was NOT present in `internal/config/config.go`. The runner cannot wave-split without a config field.
- **Fix:** Added `MaxSwarmConcurrent int` to the Config struct + `envIntDefault("AURA_SWARM_MAX_CONCURRENT", 4)` in the load block, alongside the two plan-specified vars, and covered all three in `TestSwarmConfigDefaultsAndOverrides` + the clear list.
- **Files modified:** internal/config/config.go, internal/config/config_test.go
- **Verification:** `go test ./internal/config/` green (defaults 8/120/4 + overrides); the runner reads `rc.Cfg.MaxSwarmConcurrent` for wave width.
- **Committed in:** d4ccf6d8 (Task 1 commit)

**2. [Rule 1 - Bug] gosec G301/G302/G304 hardening on the transcript write**
- **Found during:** Task 2 (golangci-lint, Gate 2)
- **Issue:** `internal/swarm` is not in the `.golangci.yml` gosec exclusion (unlike `internal/agent/tools`), so the transcript dump's `0o755`/`0o644` perms (G301/G302) and the variable path (G304) failed the lint gate.
- **Fix:** Tightened to `0o750`/`0o600` and added a targeted `//nolint:gosec // G304: childID is a controlled flat id, path is not model-traversable` (childID is the internally-generated flat worker id, runDir/convID are operator config).
- **Files modified:** internal/swarm/report.go
- **Verification:** `golangci-lint run ./internal/swarm/` → 0 issues.
- **Committed in:** 26e980c4 (Task 2 commit)

---

**Total deviations:** 2 auto-fixed (1 blocking config gap, 1 lint-gate hardening)
**Impact on plan:** Both necessary — the config field is load-bearing for the wave loop, the perms are a hard lint gate. No scope creep beyond the plan's files_modified.

## Issues Encountered
- **Test router matched the wrong message.** The prompt builder tail-injects a `<budget>` RoleUser message, so the test `routerClient` (which initially keyed off the LAST user message) routed every worker to the empty fallback. Fixed by scanning ALL user messages for the goal substring. Caught via a throwaway spy client; verified by the now-passing SC#3/SC#4 tier.
- **rapid.T is not testing.TB.** `*rapid.T` lacks the unexported seal and `TempDir`. Introduced a minimal `testLike` interface (Fatalf/Errorf/Cleanup) + a manual `tempDir` helper so the shared `testRunConfig`/`parseReports` run under both `*testing.T` and the property test.

## Known Stubs
None — the runner is fully wired against real LlmAgent workers (driven by FakeClient/routerClient in tests). The swarm_spawn tool that calls `Run` is intentionally out of scope (09-05 wires it); that is a planned decoupling, not a stub.

## User Setup Required
None - no external service configuration required. Three new env vars (`AURA_SWARM_MAX_GOALS`=8, `AURA_SWARM_CHILD_TIMEOUT_SEC`=120, `AURA_SWARM_MAX_CONCURRENT`=4) all have safe defaults.

## Next Phase Readiness
- The engine is independently testable and green; 09-05 can construct `swarm.RunConfig` inside `swarm_spawn.Execute` and wrap the returned JSON in `tools.NewResult`.
- The `ToolCallID` carried on `needs_user_input` reports is the ground-truth for the D-05 `proxied_tool_call_id` plumb (09-04's concern).
- Race tier was run locally via the w64devkit shim; the post-merge WSL gate re-runs `-race` + coverage + mutation per CLAUDE.md.

## Self-Check: PASSED
- Created files verified present (swarm.go, swarm_depth.go, report.go, brief.go, registry.go + tests, config.go).
- Commits verified in git log: d4ccf6d8 (Task 1), 26e980c4 (Task 2).

---
*Phase: 09-swarm-minimal*
*Completed: 2026-06-04*
