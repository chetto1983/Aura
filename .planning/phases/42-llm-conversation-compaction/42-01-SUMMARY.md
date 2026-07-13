---
phase: 42-llm-conversation-compaction
plan: 01
subsystem: conversations
tags: [go, compaction, token-budget, provider-capabilities, semantic-units, telemetry]
requires:
  - phase: 37E
    provides: provider-neutral LLM target and request seams
provides:
  - versioned fail-closed provider compaction capability registry
  - exact integer token budget and activation savings gates
  - non-authoritative internal historical-context envelope
  - semantic-unit lifecycle selector with disjoint bounded-tail manifests
  - bounded-label content-free shadow telemetry contract
affects: [42-02, 42-03, 42-04, 42-05, compaction, provider-adapters]
tech-stack:
  added: []
  patterns: [validated capability table, pure integer budget transform, atomic semantic lifecycle, bounded telemetry vocabulary]
key-files:
  created: [internal/llm/capabilities.go, internal/conversations/compaction_budget.go, internal/conversations/semantic_units.go, internal/conversations/compaction_metrics.go]
  modified: [internal/agent/prompt/builder.go]
key-decisions:
  - "Unknown or incomplete compatibility adapters remain usable only outside compaction and fail compaction preflight with a typed error."
  - "Historical summary data is base64-wrapped under a fixed non-authoritative envelope so transcript delimiters cannot become provider roles."
  - "Semantic selection classifies protected and unsafe lifecycle units explicitly and never splits an eligible unit to satisfy tail capacity."
patterns-established:
  - "Budget arithmetic checks fixed-plus-pending capacity before any ratio or percentage-derived calculation."
  - "Commit-hook rejection is an explicit gate retry boundary: report failing hook/rule and remediation, then rerun hooks without bypass."
requirements-completed: [IC-01, IC-02, IC-13, IC-14]
coverage:
  - id: D1
    description: Versioned provider capabilities and exact fail-closed budgets
    requirement: IC-01
    verification:
      - kind: unit
        ref: "go test -race ./internal/llm ./internal/llm/openai_compat ./internal/agent/prompt ./internal/conversations -run 'Capability|Budget|Estimator|Forecast|InternalContext|Envelope' -count=1"
        status: pass
    human_judgment: false
  - id: D2
    description: Atomic semantic units, recent tail, typed L1 edits, and disjoint manifests
    requirement: IC-02
    verification:
      - kind: unit
        ref: "go test -race ./internal/conversations -run 'SemanticUnit|RecentTail' -count=1"
        status: pass
    human_judgment: false
  - id: D3
    description: Structurally redacted bounded-label shadow telemetry with activation unchanged
    requirement: IC-13
    verification:
      - kind: unit
        ref: "internal/conversations/compaction_metrics_test.go#TestCompactionMetricBoundedAndRedacted"
        status: pass
    human_judgment: false
duration: 42min
completed: 2026-07-13
status: complete
---

# Phase 42 Plan 01: Deterministic Compaction Foundation Summary

**Versioned adapter preflight, exact token budgets, injection-safe internal context, atomic semantic selection, and redacted shadow telemetry with activation still disabled**

## Performance

- **Duration:** 42 min
- **Started:** 2026-07-13T12:38:00+02:00
- **Completed:** 2026-07-13T13:20:03+02:00
- **Tasks:** 2
- **Files modified:** 11

## Accomplishments

- Added exhaustive current-adapter capability rows for OpenRouter, llama.cpp, vLLM, and generic OpenAI-compatible transports, with typed fail-closed preflight.
- Implemented the normative integer budget formula, forecast/calibration thresholds, capacity failures, and absolute/relative/target activation gates without double counting.
- Replaced pair-level selection assumptions with a lifecycle-aware semantic unit model, backward atomic recent tail, complete disjoint manifests, and authorized-reference-only L1 edits.
- Added structural telemetry whose label vocabulary excludes content, identities, artifact names, and secrets.

## Task Commits

1. **Task 1: Define and validate provider capabilities and exact budgets** - `0bc153ac6` (feat)
2. **Task 2: Build semantic-unit selection and bounded recent-tail manifests** - `eeb619b5d` (feat)

TDD RED was observed before each implementation. RED-only commits were not possible because repository pre-commit vet/lint requires the new symbols to compile; each task therefore landed as one atomic tested feature commit with normal hooks.

## Files Created/Modified

- `internal/llm/capabilities.go` - Versioned provider capability and retention contract.
- `internal/conversations/compaction_budget.go` - Pure exact budget and activation validation.
- `internal/conversations/semantic_units.go` - Semantic lifecycle normalization and manifest selection.
- `internal/conversations/compaction_metrics.go` - Redacted bounded telemetry boundary.
- `internal/agent/prompt/builder.go` - Escaped non-authoritative historical context envelope.
- Paired focused tests cover adapter rows, budget boundaries, envelope injection, lifecycle closure, tail atomicity, partitions, L1 eligibility, and telemetry labels.

## Decisions Made

- Provider capability is runtime data resolved at one table boundary, not model-name conditionals scattered through the coordinator.
- Unknown/incomplete adapters fail only compaction activation, preserving ordinary compatibility requests while activation remains disabled.
- The internal context renderer encodes structured historical JSON before insertion and marks it non-authoritative in a fixed versioned envelope.
- Open, cancelled, retried, malformed, missing-result, duplicate-result, and partial-stream units are excluded unless a named legacy normalization closes them.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Co-located focused tests in narrow companion files**
- **Found during:** Task 1
- **Issue:** Adding every adapter-contract case to already broad historical test files would reduce responsibility clarity while the plan requires one atomic adapter contract.
- **Fix:** Added `builder_compaction_test.go` and `client_compaction_test.go` beside the specified packages while preserving all required production files and verification commands.
- **Verification:** Focused and plan-level WSL race suites pass.
- **Committed in:** `0bc153ac6`

**Total deviations:** 1 auto-fixed (blocking file-location refinement). **Impact:** No contract or scope change; test ownership is narrower.

## Issues Encountered

- **Gate retry — Task 1:** lefthook `lint`/revive rejected missing comments on 23 exported symbols. Remediation added API documentation; rerun passed gofmt, vet, lint (0 issues), and file-size.
- **Gate retry — Task 2:** lefthook `lint`/revive rejected comments on two exported constant blocks. Remediation added block comments; rerun passed all hooks.
- Native Windows `go test -race` reported `-race requires cgo`; the exact plan command ran under the repository's WSL/CGO path and all four packages passed.
- Operational learning for later executors: on any hook rejection, immediately report `gate retry`, the failing hook/rule, and remediation before retrying; never use `--no-verify`.

## Known Stubs

None. Activation intentionally remains unchanged/disabled by phase design; this plan provides pure foundations consumed by later plans.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Plan 42-02 can persist capability snapshots, manifests, claims, checkpoints, active pointers, and recovery state against these stable pure contracts.
- No implementation blocker remains. Provider pricing/retention declarations still require live revalidation at rollout time as specified by later plans.

## Self-Check: PASSED

- Created files exist and task commits `0bc153ac6` and `eeb619b5d` are present.
- Exact WSL/CGO race verification passed for llm, openai_compat, prompt, and conversations.
- No accidental tracked-file deletion or new dependency occurred.

---
*Phase: 42-llm-conversation-compaction*
*Completed: 2026-07-13*
