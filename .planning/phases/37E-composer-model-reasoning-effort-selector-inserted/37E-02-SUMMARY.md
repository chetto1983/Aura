---
phase: 37E-composer-model-reasoning-effort-selector-inserted
plan: 02
subsystem: api
tags: [reasoning-effort, llm, openrouter, llamacpp, wire-contract, provider-config, thinking-budget]

# Dependency graph
requires:
  - phase: 37E-01
    provides: "PRD-amendment gate — the amended REQUIREMENTS/ROADMAP/prd (Amendment #82) that charter the 7-level effort-only engine, the AURA_LLM_PROVIDER knob, and the llama.cpp wire contract this plan implements"
provides:
  - "llm.ReasoningEffortMax const — the `max` vocabulary token, serializes 1:1 to OpenRouter reasoning.effort"
  - "llm.ReasoningTargetKind + llm.ReasoningTarget(provider,baseURL) — the neutral backend classifier (None/OpenRouter/LlamaCpp) importable by both prompt and openai_compat"
  - "AURA_LLM_PROVIDER env override (config.applyEnvOverrides) + settings AllowedKeys row — positive llama.cpp identification (OQ-1)"
  - "openai_compat target-aware buildWireRequest: llama.cpp chat_template_kwargs + thinking_budget_tokens branch; OpenRouter shape byte-unchanged"
  - "spike-095 budget consts (llamaCppBudget Low/Mid/High/Extra/Max = 512/2048/8192/16384/-1)"
affects: [37E-04, 37E-05, 37E-06, 37E-07]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Provider-neutral backend classifier in internal/llm (the shared package) so agent-policy and wire layers branch on one recognition without importing each other"
    - "Target-aware wire projection: switch on llm.ReasoningTarget to route reasoning knobs; OpenRouter/None keep the nested reasoning object, llama.cpp gets its own per-request fields with Reasoning nil"
    - "Fixed effort symbol → fixed budget const (no request-supplied N reaches the wire) — DoS-safe by construction"

key-files:
  created:
    - internal/llm/reasoning_target.go
    - internal/llm/reasoning_target_test.go
    - internal/llm/openai_compat/client_reasoning_wire_test.go
  modified:
    - internal/llm/client.go
    - internal/llm/config.go
    - internal/llm/config_test.go
    - internal/settings/settings.go
    - internal/settings/settings_test.go
    - internal/llm/openai_compat/client.go

key-decisions:
  - "llama.cpp keyed on explicit Provider=='llamacpp' (case-insensitive), never a baseURL heuristic (OQ-1) — a local vLLM/DGX path classifies as None so the llama.cpp branch cannot misfire"
  - "ReasoningTargetNone shares the OpenRouter branch (nested reasoning object) so every non-llamacpp provider keeps today's byte-identical wire; only an explicit llamacpp target diverges"
  - "Budget consts live in openai_compat as a llama.cpp wire detail, spike-095-validated, promotable to AURA_LLM_LLAMACPP_THINKING_BUDGET_* config later without a contract change"
  - "IsOpenRouterReasoningTarget in prompt is intentionally NOT refactored to delegate here — that generalization belongs to Wave-3 plan 37E-04 (which also adds IsReasoningTarget); the transient duplication of the OpenRouter string check is by phase design"

patterns-established:
  - "Neutral ReasoningTarget classifier: the single source of truth for backend recognition going forward"
  - "Target-aware buildWireRequest branch: the seam Wave-3+ extends for override + capability"

requirements-completed: []  # WEBMODEL-01/03 advanced at the engine layer only; they are phase-spanning (full e2e + coverage ≥85% land in Waves 4-5). Intentionally NOT marked — mirrors the 37E-01 / 37D-01..04 precedent where the terminal plan owns the mark.

# Metrics
duration: ~24min
completed: 2026-07-10
---

# Phase 37E Plan 02: LLM Reasoning-Effort Engine Summary

**Provider-neutral effort engine at the internal/llm layer: the `max` vocab const, a Provider-keyed ReasoningTarget classifier (None/OpenRouter/LlamaCpp), the AURA_LLM_PROVIDER env+settings knob, and a net-new llama.cpp wire branch (chat_template_kwargs + thinking_budget_tokens) — the OpenRouter wire stays byte-unchanged.**

## Performance

- **Duration:** ~24 min
- **Started:** 2026-07-10T21:26:00+02:00 (approx, after the 37E-01 docs commit `7bc7b6e0`)
- **Completed:** 2026-07-10T21:50:00+02:00 (approx)
- **Tasks:** 3
- **Files modified:** 9 (3 created, 6 modified)

## Accomplishments
- `llm.ReasoningEffortMax` ("max") added to the effort vocabulary — serializes 1:1 to OpenRouter's `reasoning.effort:"max"` token (spike 096); backs the Composer "max" UI level (D-02/D-09a).
- `internal/llm/reasoning_target.go`: the neutral `ReasoningTarget(provider, baseURL)` classifier both `prompt` and `openai_compat` can import without a layering smell. OpenRouter recognition lifts the exact historical `IsOpenRouterReasoningTarget` logic (behavior-preserving); llama.cpp is keyed on explicit `Provider=="llamacpp"` (OQ-1), so a local vLLM/DGX endpoint classifies as `None` and the llama.cpp branch can never misfire (T-37E-02-MISFIRE mitigated + tested).
- `AURA_LLM_PROVIDER` is now settable via env (`config.applyEnvOverrides`) and via the Settings page (`settings.AllowedKeys` KindString row); default stays `"openrouter"` (no regression). This gives the wire layer a positive llama.cpp key.
- `openai_compat.buildWireRequest` is target-aware: a llama.cpp target emits `chat_template_kwargs:{enable_thinking:false}` (OFF) or `thinking_budget_tokens:512/2048/8192/16384/-1` (low/mid/high/extra/max) and leaves `Reasoning` nil; OpenRouter and any unrecognized (None) target keep today's nested `reasoning` object UNCHANGED (xhigh/max serialize automatically via `string(r.Effort)`).
- Daemon-free, coverage-load-bearing table tests prove both the llama.cpp branch and the byte-unchanged OpenRouter shape (T-37E-02-COVLOSS mitigated — no live/container tag).

## Task Commits

Each task was committed atomically (all `feat` — see TDD Gate Compliance for the RED/GREEN handling):

1. **Task 1: ReasoningEffortMax const + neutral ReasoningTarget classifier** - `aa63055b` (feat)
2. **Task 2: AURA_LLM_PROVIDER env + settings knob** - `c70f591c` (feat)
3. **Task 3: target-aware wire branch — llama.cpp thinking fields** - `b86b3c1d` (feat)

**Plan metadata:** (this docs commit)

## Files Created/Modified
- `internal/llm/reasoning_target.go` (created) - `ReasoningTargetKind` + `ReasoningTarget(provider,baseURL)` neutral classifier.
- `internal/llm/reasoning_target_test.go` (created) - table test: openrouter/llamacpp/vllm/none + case-insensitivity + the `max` token.
- `internal/llm/openai_compat/client_reasoning_wire_test.go` (created) - pure `TestBuildWireRequestReasoningTarget`: OpenRouter unchanged + llama.cpp OFF/graduated/auto.
- `internal/llm/client.go` (modified) - added `ReasoningEffortMax` to the `ReasoningEffort` const block (lint-excluded skeleton, so vet-only).
- `internal/llm/config.go` (modified) - `envProvider` const + `AURA_LLM_PROVIDER` branch in `applyEnvOverrides`.
- `internal/llm/config_test.go` (modified) - `TestConfigEnvProviderOverride` (set→llamacpp / unset→openrouter) + `AURA_LLM_PROVIDER` added to `clearLLMEnv`.
- `internal/settings/settings.go` (modified) - `AURA_LLM_PROVIDER` AllowedKeys row (KindString).
- `internal/settings/settings_test.go` (modified) - `TestAllowed` assertion for the new key + `AURA_LLM_PROVIDER` added to the overlay env-clear helper.
- `internal/llm/openai_compat/client.go` (modified) - two optional `wireRequest` fields, target-aware `buildWireRequest`, `applyLlamaCppReasoning` + spike-095 budget consts + `intPtr` (253→309 LOC, <600).

## Decisions Made
- **llama.cpp recognition = explicit Provider key, not a URL guess** (OQ-1). The DGX/vLLM local path also emits reasoning and is non-openrouter.ai, so a baseURL heuristic would drop the reasoning object on the wrong backend. Keyed on `Provider=="llamacpp"`; a vLLM base URL → `None` (tested).
- **`None` collapses into the OpenRouter branch** at the wire layer. Every provider that isn't explicitly `llamacpp` keeps the exact historical nested `reasoning` object, so the change is a strict no-op for all existing deployments (the `TestRequestBody_Reasoning` test, which runs with `Provider=""`, still asserts `reasoning.effort`).
- **`IsOpenRouterReasoningTarget` left untouched.** The plan scopes this plan to `client.go`/`reasoning_target.go`/`config.go`/`settings.go`/`openai_compat/client.go`; the delegation refactor + `IsReasoningTarget` generalization is Wave-3 (37E-04). The transient duplication of the OpenRouter string check is intentional per the phase's wave sequencing — not a DRY miss.

## Deviations from Plan

None - plan executed exactly as written. All three tasks landed with the exact symbol names, wire fields, and budget constants the plan specified (so the Wave-3 consumers 37E-04/05 link). No auto-fixes (Rules 1-3) were needed; no architectural decisions (Rule 4) arose.

## TDD Gate Compliance

Tasks 1 and 3 are `tdd="true"`. The RED→GREEN cycle was performed and observed in the working tree for both:
- **Task 1 RED:** `reasoning_target_test.go` authored first; `go test` failed to compile (`undefined: llm.ReasoningTargetKind/…/ReasoningEffortMax`). **GREEN:** const + classifier implemented → test passed, `-race` clean.
- **Task 3 RED:** `client_reasoning_wire_test.go` authored first; `go test` failed to compile (`wireRequest has no field ChatTemplateKwargs/ThinkingBudgetTokens`). **GREEN:** two fields + target branch implemented → test passed, `-race` clean.

Each TDD task is a SINGLE atomic `feat` commit (test + implementation together) rather than separate `test(...)`/`feat(...)` commits. **Reason:** the repo's `lefthook` pre-commit hook runs `go vet ./...` + `golangci-lint` on every commit (no `--no-verify` allowed for this sequential run), which rejects a non-compiling RED-only test commit. Committing the RED test in isolation is impossible under that gate, so the codebase invariant "every commit compiles + passes vet/lint" is honored while the test-first discipline is preserved in authoring order and verified failing-then-passing locally. This is a justified, documented handling of the RED gate, not a skip.

## Issues Encountered
- **`-race` needs CGO on Windows** (no `gcc` on the Windows PATH). Resolved by running all `-race` verification in WSL Ubuntu (`/usr/local/go/bin/go`, gcc 15, deps warm-cached, repo reachable at `/mnt/d/Repo/Aura`) — CLAUDE.md's documented primary dev environment. Non-race tests, `go vet ./...`, `go build ./...`, and `golangci-lint` ran natively on Windows.

## Verification Evidence
- `go test ./internal/llm/... ./internal/settings/... -race` (WSL) → all packages `ok`.
- `go vet ./...` (Windows) → exit 0. `go build ./...` → exit 0.
- `golangci-lint run ./internal/llm/ ./internal/llm/openai_compat/` (WSL) → 0 issues; pre-commit hooks (gofmt/vet/lint/file-size) green on all 3 task commits.
- **Coverage (daemon-free, no tags):** `ReasoningTarget` 100%, `buildWireRequest` 100%, `applyLlamaCppReasoning` 100%, `intPtr` 100%, `settings.Allowed` 100%; `applyEnvOverrides` Provider branch exercised (both true/false paths). Package totals: `internal/llm` 91.6%, `internal/llm/openai_compat` 98.0% — new logic contributes to the ≥85% owned-surface floor via pure tests, not container-gated ones.
- **OpenRouter byte-unchanged:** the existing `TestRequestBody_Reasoning` (marshals the full wire body) still asserts `reasoning.effort`/`reasoning.exclude`; the nil llama.cpp fields are omitempty-dropped, so the OpenRouter body is identical.
- `client.go` (openai_compat) = 309 LOC — no god class.

## Known Stubs
None. Every symbol shipped is fully wired to real, spike-validated wire knobs. The budget consts are the empirically-settled spike-095 values (not placeholders); `-1` is the intended llama.cpp "unlimited" sentinel, reachable only via the capability-gated `max` symbol (gating lands in Wave-4 plan 37E-06, per the threat register).

## Next Phase Readiness
Wave-3 consumers can link against the exact symbols delivered:
- **37E-04 (override seam)** consumes `llm.ReasoningTarget` / `ReasoningEffortMax` and will generalize `prompt.IsOpenRouterReasoningTarget` to delegate here + add `IsReasoningTarget` (OpenRouter OR LlamaCpp) for the fixed path.
- **37E-05 (capability detection)** consumes the `AURA_LLM_PROVIDER` knob (llama.cpp `/props` source keys on it) and the effort vocabulary (incl. `max`).
- **37E-06** will capability-gate the `max`→`-1` unlimited budget at `/agent/run` before it can reach this wire branch (T-37E-02-BUDGET upstream gate).

No blockers. No new deps, migrations, or env beyond the documented `AURA_LLM_PROVIDER` (already catalogued in prd Amendment #82 / 37E-01).

## Self-Check: PASSED

Files (created) verified present on disk:
- internal/llm/reasoning_target.go — FOUND
- internal/llm/reasoning_target_test.go — FOUND
- internal/llm/openai_compat/client_reasoning_wire_test.go — FOUND

Commits verified in git log:
- aa63055b (Task 1) — FOUND
- c70f591c (Task 2) — FOUND
- b86b3c1d (Task 3) — FOUND

---
*Phase: 37E-composer-model-reasoning-effort-selector-inserted*
*Completed: 2026-07-10*
