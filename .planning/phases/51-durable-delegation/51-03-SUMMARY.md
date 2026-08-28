---
phase: 51-durable-delegation
plan: 03
subsystem: swarm
tags: [tool-schema, json-schema, prompt-engineering, encoding-json]

requires:
  - phase: 51-durable-delegation
    provides: "plan 51-01's DelegationPayload.Context field (forward-compat placeholder) and the enqueue path this plan wires up"
provides:
  - "structuredBrief(goal, context) — a distinct ## Context section, never concatenated into ## Objective"
  - "swarm_spawn's Spec().Parameters rendered at call time from an injected SwarmCaps struct (goal/concurrency/timeout/depth), never a static json.RawMessage literal"
  - "swarmRunner interface widened to Run(ctx, goals, context) — the single delegation seam, extended in place"
  - "swarm.MaxDepth() — exported so the composition root can read AURA_SWARM_MAX_DEPTH for the tool schema without adding it to the Tier A/B knob registry"
affects: [51-05-nested-delegation, 51-09-inactivity-watchdog]

actuals:
  tokens: 8700
  tasks: 2
  commits: 4

tech-stack:
  added: []
  patterns:
    - "A tool's Spec() renders its JSON Parameters from an injected caps struct via a typed Go struct + encoding/json.Marshal, never a hand-built string or a package-level literal — this is the first Spec() in the codebase to do so (51-PATTERNS.md's own 'No Analog Found' entry)"
    - "A model-facing tool schema and the operator-facing Tier A/B knob registry (config_knobs_test.go) are different catalogs with different consumers — a cap can be rendered by VALUE into the former without being added to the latter"

key-files:
  created:
    - internal/swarm/brief_context_test.go
    - internal/agent/tools/swarm_spawn_schema_test.go
  modified:
    - internal/swarm/brief.go
    - internal/swarm/brief_registry_test.go
    - internal/swarm/swarm.go
    - internal/swarm/delegation_queue.go
    - internal/swarm/swarm_depth.go
    - internal/swarm/runner_adapter.go
    - internal/swarm/runner_adapter_test.go
    - internal/agent/tools/swarm_spawn.go
    - internal/agent/tools/swarm_spawn_test.go
    - cmd/aura/main.go

key-decisions:
  - "Widened the EXISTING swarmRunner interface (Run(ctx, goals, context)) instead of adding a second interface or a background bool — matches 51-PATTERNS.md's explicit instruction and keeps the tools package cycle-free"
  - "Exported swarm.MaxDepth() (wrapping the existing unexported maxDepth()) so cmd/aura, which already imports internal/swarm, can read the live depth cap for the tool schema — the alternative (re-reading AURA_SWARM_MAX_DEPTH via os.Getenv in cmd/aura) would have duplicated the existing parse/fallback logic, which CLAUDE.md forbids"
  - "Exported SwarmCaps (not swarmCaps as the plan's prose spelled it) because cmd/aura's composition root must construct the struct literal by name when registering the tool — an unexported type name is not referenceable from another package in Go, so this is a naming-casing necessity, not a scope change"
  - "Put all four live caps (goals/concurrency/timeout/depth) into the JSON schema's own root-level \"description\" field, and the live goals cap into the goals property's own description, so the SWARM-02 must_have (\"the rendered JSON schema names the live goal cap, concurrency cap, depth cap and per-worker time bound\") is satisfied inside Parameters itself, not just the tool's static Description"
  - "Context is a single string shared across all goals in one swarm_spawn call (RunConfig.Context), not a per-goal field — matches the plan's literal swarmRunner signature (Run(ctx, goals, context string), one context for the whole call)"

requirements-completed: [SWARM-01, SWARM-02]

coverage:
  - id: D1
    description: "structuredBrief(goal, context) renders the goal and the supplied context as two distinct sections; an empty context emits zero context-section markers, not an empty section"
    requirement: "SWARM-01"
    verification:
      - kind: unit
        ref: "internal/swarm/brief_context_test.go#TestStructuredBriefEmptyContextOmitsSection"
        status: pass
      - kind: unit
        ref: "internal/swarm/brief_context_test.go#TestStructuredBriefSeparatesContextFromGoal"
        status: pass
      - kind: unit
        ref: "internal/swarm/brief_context_test.go#TestStructuredBriefSectionOrder"
        status: pass
    human_judgment: false
  - id: D2
    description: "Context text containing a literal '## Objective' line does not create a second AUTHORITATIVE objective section — the real objective section (preceding the context section) carries only the real goal"
    requirement: "SWARM-01"
    verification:
      - kind: unit
        ref: "internal/swarm/brief_context_test.go#TestStructuredBriefContextCannotForgeSectionHeader"
        status: pass
    human_judgment: false
  - id: D3
    description: "structuredBrief is pure — N concurrent goroutines with distinct goal/context pairs never interleave output (-race, goleak-clean)"
    requirement: "SWARM-01"
    verification:
      - kind: unit
        ref: "internal/swarm/brief_context_test.go#TestStructuredBriefConcurrentCallsDoNotInterleave"
        status: pass
    human_judgment: false
  - id: D4
    description: "The context arg threads end-to-end from swarm_spawn's Execute through the widened swarmRunner seam to the actual worker brief, for both the synchronous path and the background-delegation enqueue payload"
    requirement: "SWARM-01"
    verification:
      - kind: unit
        ref: "internal/agent/tools/swarm_spawn_test.go#TestSwarmSpawnDelegatesContext"
        status: pass
      - kind: unit
        ref: "internal/swarm/runner_adapter_test.go#TestRunnerAdapterThreadsContextToWorkerBrief"
        status: pass
      - kind: unit
        ref: "internal/swarm/runner_adapter_test.go#TestRunnerAdapterBackgroundsWhenEnqueuerConfigured"
        status: pass
    human_judgment: false
  - id: D5
    description: "swarm_spawn's Spec().Parameters is built from an injected SwarmCaps struct at call time (never a static literal); two tools constructed with different caps render two different schemas; the schema names the goal/concurrency/timeout/depth caps by VALUE, never by env var name"
    requirement: "SWARM-02"
    verification:
      - kind: unit
        ref: "internal/agent/tools/swarm_spawn_schema_test.go#TestSwarmSpawnSpecReflectsConfig"
        status: pass
      - kind: unit
        ref: "internal/agent/tools/swarm_spawn_schema_test.go#TestSwarmSpawnSpecNoStaticParamsLiteral"
        status: pass
    human_judgment: false
  - id: D6
    description: "AURA_SWARM_MAX_DEPTH stays out of the Tier A/B operator knob registry; the tool schema is a separate catalog"
    requirement: "SWARM-02"
    verification:
      - kind: unit
        ref: "internal/config/config_knobs_test.go#TestKnobRegistry (byte-unchanged file, re-run to confirm still green)"
        status: pass
    human_judgment: false

duration: ~55 min
completed: 2026-08-28
status: complete
---

# Phase 51 Plan 03: Goal/Context Split and Live-Rendered Tool Caps Summary

**`structuredBrief` now separates a worker's objective from its context under distinct sections, and `swarm_spawn`'s rendered JSON schema builds its `Parameters` from an injected `SwarmCaps` struct via `encoding/json.Marshal` instead of a static literal — both changes verified via a real RED/GREEN TDD cycle with genuine compile-vs-assertion failures at each RED step.**

## Performance

- **Duration:** ~55 min
- **Tasks:** 2 (SWARM-01 goal/context split, SWARM-02 live-cap schema render)
- **Files modified:** 12 (2 new test files, 10 modified)

## Accomplishments
- `structuredBrief(goal, context string)` renders the file paths/error messages/constraints under a new `## Context` section placed after `## Objective`, never concatenated into the objective; an empty context emits zero context-section markers.
- `RunConfig.Context` threads the context string through both the synchronous `runChild` path and the background `EnqueueDelegation`/`DelegationPayload` path (plan 51-01's `Context` field, previously always empty, now populated).
- `swarm_spawn`'s `Spec().Parameters` is built by `renderSwarmSpawnParams(caps SwarmCaps)` from a typed Go struct marshaled via `encoding/json` — no static `json.RawMessage` literal remains. The rendered schema names the live goal/concurrency/timeout/depth caps **by value**, never by env var name.
- The `swarmSpawnArgs` schema grew a `context` property beside `goals`; the widened `swarmRunner` interface (`Run(ctx, goals, context)`) is the SAME interface extended in place, not a second one.
- `swarm.MaxDepth()` exports the existing `AURA_SWARM_MAX_DEPTH` reader so the composition root can inject the live depth cap into the tool schema without adding it to the Tier A/B operator knob registry (`config_knobs_test.go` stays byte-unchanged).

## Task Commits

Each RED/GREEN step was committed atomically, per the plan's literal messages:

1. **Feature 1 RED: failing brief context split test** - `0c4c251a8` (test) — genuine compile-time RED was impossible under this repo's pre-commit `go vet ./...` gate, so the RED commit widens `structuredBrief`'s signature (and the one existing call site in `swarm.go`) with a naive body that ignores `context`; 4/5 new tests then fail on real assertions, not a build error.
2. **Feature 1 GREEN: separate worker goal from worker context in the brief** - `4c1e026a3` (feat) — implements the actual section split, threads `RunConfig.Context` through `runChild`, the enqueue branch's `DelegationPayload`, and `DelegationClaimLoop.runWithHeartbeat`.
3. **Feature 2 RED: failing swarm_spawn live-cap schema test** - `c9993fa9d` (test) — same constraint: widens `swarmRunner`, adds `SwarmCaps`/`SwarmSpawn.Caps`/`swarmSpawnArgs.Context`, updates every call site (`swarm_spawn_test.go`'s `fakeSwarmRunner`, `runner_adapter.go`, `cmd/aura/main.go`) so the repo compiles, but `renderSwarmSpawnParams` is a naive stub ignoring `caps` — the new `TestSwarmSpawnSpecReflectsConfig` fails on real assertions (schemas equal, no `context` property, no cap values).
4. **Feature 2 GREEN: render swarm_spawn schema from the operator's live caps** - `ed42285c1` (feat) — implements the typed-struct + `encoding/json.Marshal` render.

**Plan metadata:** (this commit) `docs(51-03): complete goal/context split and live-cap schema plan`

_Note: no separate REFACTOR commit — `brief.go` and `swarm_spawn.go` stayed clean (no dead code, comments updated in the same GREEN commits) after each GREEN step, so there was nothing left to clean up._

## Files Created/Modified
- `internal/swarm/brief.go` - `structuredBrief(goal, context)`, new `briefContext` marker, empty-context-omits-section behavior
- `internal/swarm/brief_context_test.go` - the 5 new SWARM-01 tests (empty-context, separation, order, forged-header backstop, concurrency)
- `internal/swarm/brief_registry_test.go` - call-site update for the existing `TestStructuredBrief` (two-arg signature)
- `internal/swarm/swarm.go` - `RunConfig.Context` field; `runChild` and the enqueue branch both read it
- `internal/swarm/delegation_queue.go` - `runWithHeartbeat` threads `payload.Context` back into the reconstructed `RunConfig`
- `internal/swarm/swarm_depth.go` - exported `MaxDepth()` wrapping the existing `maxDepth()`
- `internal/swarm/runner_adapter.go` - `RunnerAdapter.Run(ctx, goals, context)` — the widened seam's concrete implementation
- `internal/swarm/runner_adapter_test.go` - updated call sites; added `TestRunnerAdapterThreadsContextToWorkerBrief` and a context-payload assertion in `TestRunnerAdapterBackgroundsWhenEnqueuerConfigured`
- `internal/agent/tools/swarm_spawn.go` - `SwarmCaps`, `SwarmSpawn.Caps`, `swarmSpawnArgs.Context`, `renderSwarmSpawnParams`, widened `swarmRunner`
- `internal/agent/tools/swarm_spawn_test.go` - updated `fakeSwarmRunner` + tool literals for `Caps`; added `TestSwarmSpawnDelegatesContext`
- `internal/agent/tools/swarm_spawn_schema_test.go` - the 2 new SWARM-02 tests (live-cap reflection, no-static-literal)
- `cmd/aura/main.go` - `SwarmSpawn{..., Caps: tools.SwarmCaps{...}}` registration, reading `cfg.MaxSwarmGoals`/`MaxSwarmConcurrent`/`SwarmChildTimeoutSec` and `swarm.MaxDepth()`

## Decisions Made
See `key-decisions` in the frontmatter. In short: the widened interface stayed singular (per 51-PATTERNS.md), `SwarmCaps` is exported (Go requires it for cross-package construction), `swarm.MaxDepth()` is a thin export rather than a duplicated env-var reader, and all four live caps land in the JSON schema's own `description` fields (not just the tool's static `Description`) to satisfy the plan's must-have literally.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1/3 — corrected file target] The plan named `cmd/aura/swarm_demo.go` as "the concrete adapter"; the actual concrete adapter is `internal/swarm/runner_adapter.go`**
- **Found during:** Feature 2 read_first — reading `swarm_spawn.go`'s `swarmRunner` interface and grepping for `func.*Run(ctx context.Context, goals \[\]string)` implementers.
- **Issue:** The plan's prohibition text says "the concrete adapter in `cmd/aura/swarm_demo.go`" must move together with the widened seam. Reading both files: `swarm_demo.go` calls `swarm.Run(ctx, rc, goals)` (the engine function) directly and implements no interface at all; `internal/swarm/runner_adapter.go`'s `RunnerAdapter.Run(ctx, goals []string) (tools.ToolResult, error)` is the actual concrete type satisfying `swarmRunner`, wired at `cmd/aura/main.go`. Per CLAUDE.md's "NEVER SUPPOSE" / "READ THE DOCUMENTATION FIRST", I read both files before writing anything rather than trusting the plan's file name.
- **Fix:** Widened `RunnerAdapter.Run` to `Run(ctx, goals, context string)` and threaded `context` into `RunConfig.Context`. `swarm_demo.go` needed NO change — it never touches the tool/adapter layer, and `RunConfig.Context`'s zero-value default (`""`) keeps its existing literal compiling and behaving identically.
- **Files modified:** `internal/swarm/runner_adapter.go`, `internal/swarm/runner_adapter_test.go` (not in the plan's `files_modified` list, but required for the widened interface to compile at all — omitting it would leave `RunnerAdapter` no longer satisfying `swarmRunner`, breaking `cmd/aura/main.go`'s registration).
- **Verification:** `go build ./...` (whole repo); `TestRunnerAdapterThreadsContextToWorkerBrief` (new) proves the context reaches the real worker brief through this exact file.
- **Committed in:** `4c1e026a3` (RunConfig.Context field) / `c9993fa9d` and `ed42285c1` (interface widening + call sites).

**2. [Rule 3 — blocking, compile necessity] `SwarmCaps` exported (not `swarmCaps` as the plan's prose spelled it)**
- **Found during:** Feature 2 GREEN wiring — `cmd/aura/main.go` (package `main`) needs to construct the caps struct literal by name when registering the tool.
- **Issue:** An unexported Go type name is not referenceable from another package under any circumstance — `tools.swarmCaps{...}` from `package main` is a compile error, not a style choice. The plan's own "Artifacts this phase produces" section names it lowercase.
- **Fix:** Exported the type as `SwarmCaps`. All in-package (`internal/agent/tools`) test usages work identically either way; only the composition-root construction required the export.
- **Files modified:** `internal/agent/tools/swarm_spawn.go`, `cmd/aura/main.go`.
- **Verification:** `go build ./...` succeeds; `TestSwarmSpawnSpecReflectsConfig` constructs `SwarmCaps{...}` directly from the `tools_test` package.
- **Committed in:** `c9993fa9d` (RED, type introduced) / `ed42285c1` (GREEN).

**3. [Rule 3 — blocking, tooling constraint] TDD RED commits compile (assertion-level RED, not build-level RED)**
- **Found during:** First commit attempt for Feature 1's RED step.
- **Issue:** This repo's `lefthook` pre-commit hook runs `go vet ./...` (and `golangci-lint`) on every commit and BLOCKS a commit that fails to compile. A literal "write only the test, signature doesn't exist yet" RED commit is therefore impossible here — confirmed by reading this repo's own prior TDD history (`git log --grep='^test(51-'`), where every `test(51-XX):` commit lands AFTER its corresponding `feat(51-XX):` commit, never before a compiling state.
- **Fix:** Each RED commit widens the signature/adds the new type with a deliberately NAIVE/incomplete body (ignoring the new parameter, or returning the old static content) so the package compiles but the NEW test's assertions genuinely fail (confirmed failure output captured before each RED commit, not just "it doesn't compile so it must be red"). The following GREEN commit implements the real behavior.
- **Files modified:** N/A (methodology, not scope) — see the 4 commits listed under Task Commits.
- **Verification:** Each RED step's test output was captured and inspected line-by-line to confirm genuine assertion failures (not compile errors) before committing; each GREEN step's test output was then re-captured to confirm all pass.
- **Committed in:** `0c4c251a8`, `c9993fa9d` (RED); `4c1e026a3`, `ed42285c1` (GREEN).

---

**Total deviations:** 3 (2 Rule 1/3 auto-fixes on the critical path for compilation and correctness, 1 documented methodology adaptation to this repo's tooling). No scope creep: deviation #1's extra file (`runner_adapter.go`) is the actual location of the call site the plan meant; deviation #2 is a one-word casing fix required by Go's visibility rules; deviation #3 changed no production behavior, only which commit each line of code lands in.
**Impact on plan:** All three were on the critical path for the plan's own acceptance criteria (`go build ./...` succeeding, `swarmRunner` having exactly one interface, `cmd/aura` compiling with the widened `SwarmCaps`). None expanded scope beyond the two named features.

## Issues Encountered
- **`go test -race` requires CGO, unavailable natively on this Windows host.** Ran the full `-race` matrix for `internal/swarm/...` and `internal/agent/...` in WSL (Ubuntu, CGO_ENABLED=1) instead — both packages pass clean under `-race`. Plain `go test` (no `-race`) also passes natively on Windows for every touched package.
- **Two pre-existing test failures unrelated to this plan, confirmed platform-specific:** `TestStageBoxArtifact_ExtractsRegularFile` (`internal/agent/tools`, Windows POSIX-file-mode artifact) and `TestTempScriptRecordsAdHocEvidenceWithoutCanonicalSuite` / `TestNoSuiteNudgeNamesTheDirectoryTheClassifierAccepts` (`internal/agent`) fail on Windows but pass cleanly in WSL — confirmed by running the full suite in both environments. None touch swarm/brief/tool-schema code; not fixed (out of scope, per CLAUDE.md's guidance not to report Windows-only artifacts as regressions).
- **Shared working tree hazard observed mid-session:** an unrelated commit (`da008f83f "update state"`, touching only `.planning/STATE.md`) and a set of staged-but-uncommitted changes (`.github/workflows/ci.yml`, `.planning/ROADMAP.md`, `.planning/codebase/CONCERNS.md`, `internal/agent/tools/document_extract*.go`, `scripts/check_planning_consistency.py`) appeared in this checkout during execution, none authored by this session. This confirms a concurrent process/session is active on the SAME working tree (matching the project's own "Shared tree → empty commits" caution). Every `git add` in this session named specific files explicitly (never `-A`/`.`), so none of that concurrent work was staged or committed by this plan's commits — but the state-update step below (`gsd_run query state.*`) reads/writes `.planning/STATE.md` from whatever is currently on disk, which may include that other session's concurrent edits. Flagging for the orchestrator/human: if a wave-parallel or worktree-isolated execution mode is available for this project, this phase would benefit from it — sequential mode on a shared tree is racing another live session right now.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Plan 51-05 (nested delegation) can widen `runChild`'s registry restriction without needing to touch the goal/context brief shape again — `structuredBrief` already accepts both.
- Plan 51-09 (retiring `AURA_SWARM_CHILD_TIMEOUT_SEC` for `AURA_SWARM_CHILD_IDLE_SEC`) can rename the env var read at the composition root without touching `swarm_spawn.go`'s render code — the schema renders the VALUE, never the name.
- The background-delegation payload (`DelegationPayload.Context`, plan 51-01's forward-compat placeholder) is now genuinely populated end-to-end; a live delegated worker's brief will carry its context once dispatched through the daemon claim loop.

## Self-Check: PASSED
- `internal/swarm/brief.go` — FOUND
- `internal/swarm/brief_context_test.go` — FOUND
- `internal/swarm/brief_registry_test.go` — FOUND
- `internal/swarm/swarm.go` — FOUND
- `internal/swarm/delegation_queue.go` — FOUND
- `internal/swarm/swarm_depth.go` — FOUND
- `internal/swarm/runner_adapter.go` — FOUND
- `internal/swarm/runner_adapter_test.go` — FOUND
- `internal/agent/tools/swarm_spawn.go` — FOUND
- `internal/agent/tools/swarm_spawn_test.go` — FOUND
- `internal/agent/tools/swarm_spawn_schema_test.go` — FOUND
- `cmd/aura/main.go` — FOUND
- Commits `0c4c251a8`, `4c1e026a3`, `c9993fa9d`, `ed42285c1` — all FOUND in `git log --oneline`

---
*Phase: 51-durable-delegation*
*Completed: 2026-08-28*
