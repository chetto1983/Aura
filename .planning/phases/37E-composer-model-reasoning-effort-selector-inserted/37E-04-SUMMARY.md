---
phase: 37E-composer-model-reasoning-effort-selector-inserted
plan: 04
subsystem: api
tags: [reasoning-effort, llm, openrouter, llamacpp, override-seam, ctx-value, prompt-builder, agent-loop]

# Dependency graph
requires:
  - phase: 37E-02
    provides: "llm.ReasoningTarget(provider,baseURL) neutral classifier (None/OpenRouter/LlamaCpp) + llm.ReasoningEffortMax — the symbols this plan consumes to generalize IsOpenRouterReasoningTarget and to project the fixed effort"
provides:
  - "prompt.ApplyFixedReasoning(req, provider, cfg, effort) — the fixed per-turn effort projection: forces req.Reasoning{Effort,Exclude} on OpenRouter OR llama.cpp (D-08), exclude from cfg.ShowReasoning (D-10), orthogonal to cfg.AdaptiveReasoning"
  - "prompt.IsReasoningTarget(provider,baseURL) — generalized OpenRouter-or-llama.cpp gate; IsOpenRouterReasoningTarget now delegates to llm.ReasoningTarget (behavior-preserving)"
  - "prompt.PromptBuilder.BuildWithReasoningOverride — fixed-effort sibling of BuildWithReasoningTier"
  - "runner.WithReasoningOverride(ctx, llm.ReasoningEffort) + reasoningOverride(ctx) — the ctx-value seam plan 06 sets; private struct{} key"
  - "LlmAgent.reasoningOverride + LlmAgentConfig.ReasoningOverride — the fixed override threaded request→agent; when set, Run bypasses the adaptive classifier"
affects: [37E-06, 37E-07]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Override-seam sibling family: ApplyFixedReasoning mirrors ApplyAdaptiveReasoning; BuildWithReasoningOverride mirrors BuildWithReasoningTier — one exclude rule, one target gate, no drift"
    - "ctx-value → config-field → builder-seam threading (mirrors WithThreadLockHeld) so a per-turn scalar reaches the deep request builder without a Turn-variant method explosion"
    - "Skip-when-fixed branch at the Run call site: a non-empty override skips adaptiveReasoningTier entirely; empty is byte-identical auto (D-04)"

key-files:
  created:
    - internal/runner/runner_reasoning.go
    - internal/runner/runner_reasoning_test.go
    - internal/agent/llm_agent_tool.go
    - internal/agent/llm_agent_reasoning_override_test.go
  modified:
    - internal/agent/prompt/reasoning_policy.go
    - internal/agent/prompt/reasoning_policy_test.go
    - internal/agent/prompt/builder.go
    - internal/agent/llm_agent.go
    - internal/agent/llm_agent_construct.go
    - internal/runner/runner.go

key-decisions:
  - "The fixed override carries an llm.ReasoningEffort (has medium/max), NOT a prompt.ReasoningTier (only none/low/high) — threading a tier would silently lose mid/extra/max (RESEARCH §1)"
  - "Adaptive path stays OpenRouter-only (IsOpenRouterReasoningTarget); only the FIXED path uses the generalized IsReasoningTarget (D-04 keeps auto byte-identical; D-08 reaches llama.cpp only for an explicit selection)"
  - "ApplyFixedReasoning does NOT gate on cfg.AdaptiveReasoning — an explicit selection must fire even when adaptive tiering is off (the fixed override is orthogonal to the adaptive toggle)"
  - "exclude is derived from cfg.ShowReasoning one layer BELOW the server (in ApplyFixedReasoning), reusing ReasoningTier.reasoning()'s exact rule — the server owns effort authority (D-05 spirit), the builder owns visibility parity (D-10), simultaneously"
  - "Classifier-bypass proven structurally: with no embedder the adaptive path makes a separate tool-free router LLM call FIRST; a single client call (no router request) is the bypass proof"

patterns-established:
  - "Override-seam sibling family (ApplyFixedReasoning/BuildWithReasoningOverride) — the canonical shape any future forced-request-hint follows"
  - "runner_reasoning.go ctx accessors — the per-turn override channel Wave-4 plan 06 writes to"

requirements-completed: []  # WEBMODEL-01/03 ADVANCED at the agent layer only (effort reaches req.Reasoning; server-owned map, no client ReasoningConfig). They are phase-spanning — the Composer UI, per-conversation persistence, /agent/run enum gate, and full e2e + coverage land in Waves 4-5. Marking is owned by the terminal plan (37E-02 / 37D precedent).

# Metrics
duration: 23min
completed: 2026-07-10
---

# Phase 37E Plan 04: Per-Turn Reasoning-Effort Override Seam Summary

**The RESEARCH crux: a FIXED `llm.ReasoningEffort` rides ctx (`runner.WithReasoningOverride`) → `LlmAgentConfig.ReasoningOverride` → `ApplyFixedReasoning`, forcing `req.Reasoning` on OpenRouter OR llama.cpp (D-08) with `exclude` from `cfg.ShowReasoning` (D-10), while `auto`/absent leaves today's adaptive classifier path byte-identical (D-04, zero regression).**

## Performance

- **Duration:** ~23 min
- **Started:** 2026-07-10T20:21:39Z
- **Completed:** 2026-07-10T20:44:27Z
- **Tasks:** 3
- **Files modified:** 10 (4 created, 6 modified)

## Accomplishments
- `ApplyFixedReasoning` + generalized `IsReasoningTarget`: a fixed per-turn effort forces `req.Reasoning{Effort,Exclude}` on ANY reasoning target (OpenRouter OR llama.cpp), no-ops off-target, derives `exclude` byte-identically to `ReasoningTier.reasoning()` (D-10), and is orthogonal to `cfg.AdaptiveReasoning`. `IsOpenRouterReasoningTarget` now delegates to `llm.ReasoningTarget` (37E-02) — the transient duplication that plan flagged is folded out.
- `BuildWithReasoningOverride`: the symmetric sibling of `BuildWithReasoningTier` — same body, `ApplyFixedReasoning` between `buildBase` and `injectCacheControl`; an empty effort is byte-identical to a plain `Build`.
- `runner_reasoning.go`: `WithReasoningOverride`/`reasoningOverride` ctx accessors (private `struct{}` key, T-37E-04-CTX) mirroring `WithThreadLockHeld`; `buildAgent` reads the override into `LlmAgentConfig.ReasoningOverride`.
- `LlmAgent` skip-when-fixed: when the override is set, `Run` SKIPS `adaptiveReasoningTier` (no reasoning-router round-trip) and `buildRequest` routes to the fixed branch; when empty, the adaptive path is byte-identical (D-04). The fixed override reaches llama.cpp where the adaptive path (OpenRouter-only) never would.
- Every new symbol is 100% covered by daemon-free pure/agent tests (no container/live tag) — coverage-load-bearing per the CI gate rule.

## Task Commits

Each task was committed atomically (Tasks 1 & 3 are `tdd="true"` — see TDD Gate Compliance):

1. **Task 1: ApplyFixedReasoning + generalized IsReasoningTarget + BuildWithReasoningOverride** - `91178e42` (feat)
2. **Task 2: ctx-thread the override into LlmAgentConfig (+ llm_agent_tool.go extraction)** - `b13c6676` (feat)
3. **Task 3: fixed override bypasses the adaptive classifier in LlmAgent** - `6a8987cc` (feat)

**Plan metadata:** (this docs commit)

## Files Created/Modified
- `internal/agent/prompt/reasoning_policy.go` (modified) - `ApplyFixedReasoning`; `IsReasoningTarget`; `IsOpenRouterReasoningTarget` refactored to delegate to `llm.ReasoningTarget`.
- `internal/agent/prompt/reasoning_policy_test.go` (modified) - `TestIsReasoningTarget` (+ IsOpenRouterReasoningTarget regression), `TestApplyFixedReasoning`, `TestBuildWithReasoningOverride`.
- `internal/agent/prompt/builder.go` (modified) - `PromptBuilder.BuildWithReasoningOverride`.
- `internal/runner/runner_reasoning.go` (created) - `WithReasoningOverride` + `reasoningOverride` ctx accessors.
- `internal/runner/runner_reasoning_test.go` (created) - `TestWithReasoningOverride` round-trip.
- `internal/runner/runner.go` (modified) - `buildAgent` reads `reasoningOverride(ctx)` → `LlmAgentConfig.ReasoningOverride`.
- `internal/agent/llm_agent.go` (modified) - `LlmAgentConfig.ReasoningOverride` (Task 2) + `LlmAgent.reasoningOverride` field + Run skip-branch + `buildRequest` fixed branch (Task 3); tool-execution helpers extracted out.
- `internal/agent/llm_agent_construct.go` (modified) - constructor wires `reasoningOverride: cfg.ReasoningOverride`.
- `internal/agent/llm_agent_tool.go` (created) - single-tool execution + terminal helpers extracted from `llm_agent.go` (refactor-on-touch, ≤600 LOC).
- `internal/agent/llm_agent_reasoning_override_test.go` (created) - `TestReasoningOverride`: fixed-on-OpenRouter bypass, fixed-on-llama.cpp fires, auto-empty parity.

## Decisions Made
See frontmatter `key-decisions`. In short: the override carries `llm.ReasoningEffort` (not a `ReasoningTier`, which lacks `medium/max`); the adaptive path stays OpenRouter-only while only the fixed path is generalized to llama.cpp; `ApplyFixedReasoning` is orthogonal to `cfg.AdaptiveReasoning`; `exclude` parity is derived one layer below the server so D-05 (server owns effort) and D-10 (exclude from `cfg.ShowReasoning`) hold at once.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Config field `LlmAgentConfig.ReasoningOverride` added in Task 2, not Task 3**
- **Found during:** Task 2 (ctx-thread the override)
- **Issue:** The plan assigned the `LlmAgentConfig.ReasoningOverride` field to Task 3, but Task 2's `buildAgent` change populates that field — so committing Task 2 in isolation would fail the lefthook `go vet`/build pre-commit gate (`ReasoningOverride` undefined; no `--no-verify` for this sequential run, CLAUDE.md).
- **Fix:** Added the inert config struct field in Task 2 (populated by `buildAgent`, consumed by nothing yet); Task 3 then added the `LlmAgent.reasoningOverride` struct field + constructor wiring + Run/buildRequest consumption. Every commit compiles + passes vet/lint (the codebase invariant).
- **Files modified:** internal/agent/llm_agent.go
- **Verification:** `go build ./...` + `go vet ./...` green at Task 2 commit `b13c6676`.
- **Committed in:** `b13c6676` (Task 2 commit)

**2. [Rule 3 - Blocking / refactor-on-touch] Extracted single-tool execution helpers into `llm_agent_tool.go`**
- **Found during:** Task 2 (committing the config-field addition)
- **Issue:** The 6-line config-field addition pushed `llm_agent.go` to 604 LOC — over the CLAUDE.md 600-LOC god-class cap; the `file-size` pre-commit hook rejected the commit. Task 3 then adds ~18 more lines to the same file.
- **Fix:** Refactor-on-touch (CLAUDE.md "DEEP REFACTOR ON TOUCH"): moved the cohesive single-tool execution + terminal-call helpers (`runTerminal`, `runTool`, `hookToolRunResult`, `toolRunResult`, `appendToolError`, `appendSyntheticToolResults`) into a new `llm_agent_tool.go` (166 LOC) — a pure move, no behavior change. `llm_agent.go` dropped to 455 LOC (472 after Task 3), with durable headroom. Removed the now-unused `encoding/json` import.
- **Files modified:** internal/agent/llm_agent.go, internal/agent/llm_agent_tool.go (created)
- **Verification:** `file-size` hook green on `b13c6676`/`6a8987cc`; full agent suite + `-race` green (pure move).
- **Committed in:** `b13c6676` (Task 2 commit)

**3. [File-set refinement — not behavioral] Task 3 constructor wiring landed in `llm_agent_construct.go`; `llm_agent_reasoning.go` untouched**
- **Found during:** Task 3
- **Issue:** The plan listed `llm_agent_reasoning.go` under Task 3's files, but the classifier-skip is cleanest at the `Run` call site (`if a.reasoningOverride == "" && !adaptiveTierSet`), and `NewLlmAgent` lives in `llm_agent_construct.go`, not `llm_agent.go`.
- **Fix:** Wired the field in `llm_agent_construct.go`; left `adaptiveReasoningTier` (and its OpenRouter gate) in `llm_agent_reasoning.go` UNCHANGED — the plan's "SKIP the adaptiveReasoningTier computation" was achieved at the call site, honoring D-04 (the adaptive gate is untouched). No behavior difference from the plan's intent.
- **Files modified:** internal/agent/llm_agent_construct.go (instead of llm_agent_reasoning.go)
- **Verification:** `TestReasoningOverride` all three rows green; all pre-existing adaptive tests green.
- **Committed in:** `6a8987cc` (Task 3 commit)

---

**Total deviations:** 3 (2 Rule-3 blocking incl. one refactor-on-touch, 1 file-location refinement)
**Impact on plan:** All three preserve the plan's exact symbol names, wire semantics, and D-04/D-08/D-10 behavior. No scope creep; the extraction is a pure move; the config-field re-ordering and constructor-file choice are mechanical consequences of the commit-compiles-and-fits invariant.

## Threat Model Compliance

All three registered mitigations are implemented and tested:
- **T-37E-04-VIS** (CoT visibility): `ApplyFixedReasoning` sets `exclude` ONLY from `cfg.ShowReasoning` (reusing the tier's rule); the override carries no visibility control. Tested by `exclude_parity_with_tier_reasoning`.
- **T-37E-04-REG** (adaptive regression): the override applies ONLY when non-empty; the auto path is asserted byte-identical (`auto_empty_override_runs_adaptive_unchanged` + all pre-existing `TestLlmAgent_AdaptiveReasoning*` / `TestAdaptiveReasoning*` pass unchanged).
- **T-37E-04-CTX** (ctx-key collision): `reasoningOverrideKey struct{}` — a private struct key, never a string. Tested by the round-trip.

## TDD Gate Compliance

Tasks 1 and 3 are `tdd="true"`. The RED→GREEN cycle was performed and observed in the working tree for both:
- **Task 1 RED:** `reasoning_policy_test.go` authored first; `go test` failed to compile (`undefined: IsReasoningTarget/ApplyFixedReasoning`, `BuildWithReasoningOverride undefined`). **GREEN:** the three functions implemented → suite passed, `-race` clean.
- **Task 3 RED:** `llm_agent_reasoning_override_test.go` authored first; ran and FAILED at runtime (openrouter got 5 client calls instead of 1 — the classifier still ran; llama.cpp `Reasoning.Effort` empty). **GREEN:** struct field + Run skip + `buildRequest` fixed branch → all three rows passed, `-race` clean.

Each TDD task is a SINGLE atomic `feat` commit (test + implementation together) rather than separate `test(...)`/`feat(...)` commits. **Reason:** the repo's `lefthook` pre-commit hook runs `go vet ./...` + `golangci-lint` on every commit (no `--no-verify` for this sequential run), which rejects a non-compiling RED-only test commit. This is the SAME documented handling as 37E-02 — the test-first discipline is preserved in authoring order and verified failing-then-passing locally, while the "every commit compiles" invariant holds.

## Issues Encountered
- **`-race` needs CGO on Windows** (no `gcc` on the Windows PATH). All `-race` verification ran in WSL Ubuntu (`/usr/local/go/bin/go`, gcc 15.2, repo at `/mnt/d/Repo/Aura`) — CLAUDE.md's documented primary dev environment. `go vet ./...`, `go build ./...`, non-race tests, and `gofmt` ran natively on Windows; `golangci-lint` ran in WSL.

## Verification Evidence
- `go test ./internal/agent/... ./internal/agent/prompt/... ./internal/runner/... -race` (WSL) → all packages `ok`.
- `go vet ./...` (Windows) → exit 0. `go build ./...` → exit 0.
- `golangci-lint run ./internal/agent/ ./internal/agent/prompt/ ./internal/runner/` (WSL) → 0 issues; pre-commit hooks (gofmt/vet/lint/file-size) green on all 3 task commits.
- **D-04 zero-regression:** the pre-existing `TestAdaptiveReasoning*` (prompt) and `TestLlmAgent_AdaptiveReasoning*` (agent) suites pass UNCHANGED; `auto_empty_override_runs_adaptive_unchanged` asserts the router+tier parity.
- **Coverage (daemon-free, no tags):** `ApplyFixedReasoning` 100%, `IsReasoningTarget` 100%, `IsOpenRouterReasoningTarget` 100%, `BuildWithReasoningOverride` 100%, `WithReasoningOverride` 100%, `reasoningOverride` 100%, `buildRequest` (with the fixed branch) 100% — the new seam is fully exercised by pure/agent tests, contributing to the ≥85% owned-surface floor without a container/live tag.
- **No god class:** every touched file ≤600 LOC (largest: `llm_agent.go` 472, `runner.go` 579).

## Known Stubs
None. Every symbol shipped is fully wired: `ApplyFixedReasoning`/`BuildWithReasoningOverride` project real `llm.ReasoningConfig`; the ctx seam is consumed by `buildAgent`; the agent branch forces `req.Reasoning`. The upstream setter of the ctx value (`runner.WithReasoningOverride`) is invoked by Wave-4 plan 37E-06 (the `/agent/run` enum gate) — that is the documented cross-plan seam, not a stub.

## Next Phase Readiness
Wave-4 plan 37E-06 links against the exact symbols delivered:
- **37E-06** calls `runner.WithReasoningOverride(ctx, llm.ReasoningEffort)` after validating the `/agent/run` effort symbol against the active model's capability set (37E-05), and capability-gates `max`→`-1` before it can reach the wire.
- The fixed override is inert until then: absent ctx value ⇒ `LlmAgentConfig.ReasoningOverride == ""` ⇒ today's adaptive path (D-04). No behavior change ships to live before plan 06 wires the HTTP setter.

No blockers. No new deps, migrations, or env.

## Self-Check: PASSED

Files (created) verified present on disk:
- internal/runner/runner_reasoning.go — FOUND
- internal/runner/runner_reasoning_test.go — FOUND
- internal/agent/llm_agent_tool.go — FOUND
- internal/agent/llm_agent_reasoning_override_test.go — FOUND

Commits verified in git log:
- 91178e42 (Task 1) — FOUND
- b13c6676 (Task 2) — FOUND
- 6a8987cc (Task 3) — FOUND

---
*Phase: 37E-composer-model-reasoning-effort-selector-inserted*
*Completed: 2026-07-10*
