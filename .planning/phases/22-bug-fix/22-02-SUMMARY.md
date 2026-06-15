---
phase: 22-bug-fix
plan: "02"
subsystem: agent-secrets-observability
tags: [secrets, redaction, reasoning-trace, metrics, slog, tracing]

requires:
  - phase: 22-bug-fix
    provides: crash firewall and panic metric sites from 22-01
provides:
  - DSN/key-value secret classification for shell and hook subprocess env
  - default-private reasoning trace rows with explicit full-trace mode
  - turn, LLM, tool, hook, token, cost, panic, and span failure observability
  - never-panic span ID telemetry fallback
affects: [agent-runtime, shell-exec, command-hooks, reasoningtrace, observability, tracing]

tech-stack:
  added: []
  patterns:
    - reusable agentMetrics bundle with safe custom Prometheus registry registration
    - child env filtering through secret.IsSecretEnvVar
    - default trace summaries using redacted length/hash metadata

key-files:
  created:
    - internal/agent/metrics_observability_test.go
  modified:
    - internal/secret/envkey.go
    - internal/secret/envkey_test.go
    - internal/agent/tools/shell_exec.go
    - internal/agent/tools/shell_exec_env.go
    - internal/agent/tools/shell_exec_mergeenvcap_test.go
    - internal/agent/tools/shell_exec_test.go
    - internal/agent/hooks_command.go
    - internal/agent/hooks_command_test.go
    - internal/reasoningtrace/reasoningtrace.go
    - internal/reasoningtrace/reasoningtrace_test.go
    - internal/agent/hooks.go
    - internal/agent/llm_agent.go
    - internal/agent/llm_agent_completion.go
    - internal/agent/llm_agent_consume.go
    - internal/agent/llm_agent_finalize.go
    - internal/agent/llm_agent_parallel.go
    - internal/agent/llm_agent_reasoning.go
    - internal/agent/llm_agent_stream_retry.go
    - internal/agent/metrics.go
    - internal/agent/tracing.go

key-decisions:
  - "Shell and command-hook children filter both inherited and explicit secret-shaped env because the wave hard requirement is that credentials never cross subprocess boundaries."
  - "AURA_REASONING_TRACE=full remains available as the explicit PII-risk mode; default enabled tracing stores summaries for prompt/history fields."
  - "Metric registration is centralized in agentMetrics so tests and future embedders can use non-default Prometheus registries safely."

patterns-established:
  - "Boundary metrics are emitted where errors are classified: turn defer, LLM stream drain/open, tool execution, hook manager, tracing exporter, and panic recovery."
  - "Trace privacy defaults prefer hash/count/byte summaries over raw user/history payloads."

requirements-completed: [HARDEN-01, HARDEN-04, HARDEN-05, HARDEN-12]

duration: 18min
completed: 2026-06-15
---

# Phase 22-02: Secret Boundary + Observability Minimum Summary

**Subprocess credentials are stripped/redacted by default, reasoning trace rows stop dumping history verbatim, and agent runtime failures now have metric/log signals.**

## Performance

- **Duration:** 18 min
- **Started:** 2026-06-15T14:35:56Z
- **Completed:** 2026-06-15T14:53:29Z
- **Tasks:** 2
- **Files modified:** 21

## Accomplishments

- Closed AG-010/AG-047 by adding DSN/URL/URI/CONN/PWD/COOKIE/SESSION/JWT classification plus credential-bearing URL detection.
- Closed the AG-003 minimal-env slice by changing command hooks to inherit only minimal safe parent env plus non-secret explicit hook env.
- Closed AG-009 by making default reasoning trace rows summarize `history`, `messages`, `user`, `prompt`, and `input` fields as redacted hash/size metadata; `AURA_REASONING_TRACE=full` is the explicit verbatim mode.
- Closed AG-012/AG-013/AG-033/AG-056/AG-057 by adding turn, LLM duration/error, tool error, hook, token, cost, span-export-failure, and entropy-fallback metrics plus structured `slog` at key boundaries.

## Metric Names

- `aura_agent_turn_total{outcome}`
- `aura_agent_llm_call_duration_seconds`
- `aura_agent_llm_errors_total{kind}`
- `aura_agent_tool_errors_total{tool}`
- `aura_agent_hook_total{point,outcome}`
- `aura_agent_prompt_tokens_total`
- `aura_agent_completion_tokens_total`
- `aura_agent_cached_tokens_total`
- `aura_agent_cost_usd_total`
- `aura_agent_panic_total{site}`
- `aura_agent_span_export_failures_total`
- `aura_agent_span_id_entropy_failures_total`

## Task Commits

1. **Task 1: Secret boundary and default trace privacy** - `408d841d` (fix)
2. **Task 2: Observability minimum and never-panic telemetry** - `d94919a4` (fix)

## Files Created/Modified

- `internal/secret/envkey.go` - key and key/value secret classification, including credential-bearing URL detection.
- `internal/agent/tools/shell_exec.go` - shell child env filtering for inherited and explicit env.
- `internal/agent/tools/shell_exec_env.go` - DSN credential output redaction.
- `internal/agent/hooks_command.go` - minimal command-hook child env.
- `internal/reasoningtrace/reasoningtrace.go` - default-private trace summaries, field caps, and explicit full mode.
- `internal/agent/metrics.go` - reusable metric bundle and new counters/histograms.
- `internal/agent/tracing.go` - exporter boot/failure logs and entropy fallback.
- `internal/agent/llm_agent*.go`, `internal/agent/hooks.go` - turn, LLM, tool, hook, and usage emission points.
- `internal/agent/metrics_observability_test.go` and related tests - metric, fallback, secret, redaction, and trace privacy contracts.

## Verification

- `go test ./internal/secret -run 'TestIsSecretEnvKey|TestSecret' -count=1`
- `go test ./internal/agent/tools -run 'TestShellExec.*Env|TestRedact.*DSN|TestMergeEnv' -count=1`
- `go test ./internal/agent -run 'TestCommandHook.*Env|TestCommandHook.*Secret|TestLlmAgentHooks' -count=1`
- `go test ./internal/reasoningtrace -run 'Test.*History|Test.*Redact|Test.*RowCap' -count=1`
- `go test ./internal/agent -run 'Test.*Metric|Test.*Turn.*Counter|Test.*LLM.*Duration|Test.*Tool.*Error|Test.*Hook.*Metric' -count=1`
- `go test ./internal/agent -run 'TestMintSpanID|TestTracing|TestSpan' -count=1`
- `go test ./internal/agent -count=1`
- `go test ./internal/secret ./internal/reasoningtrace -count=1`
- `go test ./internal/agent/tools -run 'TestShellExec.*Env|TestRedact|TestMergeEnv' -count=1`
- `go test ./internal/agent -run 'TestCommandHook|Test.*Metric|TestTracing|TestMintSpanID' -count=1`
- `go test ./internal/agent/... -count=1`
- Pre-commit hooks on both commits: `gofmt`, `go vet`, and Go file-size check.

## AG Ledger Status

- **AG-003 minimal-env slice:** Fixed - command hook subprocesses no longer inherit full `os.Environ()`.
- **AG-009:** Fixed - default reasoning trace does not write full history or raw user prompt fields verbatim.
- **AG-010:** Fixed - DB/DSN credential env is stripped from shell children.
- **AG-012:** Fixed - turn, LLM, tool, token/cost, and hook metrics exist.
- **AG-013:** Fixed - structured logs exist at turn, LLM, tool, hook, panic, and tracing boundaries; tracing no longer silently fails setup/fallback paths.
- **AG-033:** Fixed - `mintSpanID` uses a zero-ID fallback and records a failure signal instead of panicking.
- **AG-047:** Fixed - shell output redacts DSN credentials.
- **AG-056:** Fixed - tracing exporter setup/export failures are logged/counted.
- **AG-057:** Fixed - metric registration is centralized and custom registries can be used safely in tests.

## Decisions Made

- Explicit `shell_exec` env is filtered with the same secret classifier as inherited env. This tightens a legacy test expectation but matches the wave hard requirement.
- Added `aura_agent_span_id_entropy_failures_total` alongside `aura_agent_span_export_failures_total` so the entropy fallback has a precise signal without overloading export-failure semantics.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Explicit shell env also needed secret filtering**
- **Found during:** Task 1 (secret boundary)
- **Issue:** Existing behavior preserved explicit per-call secret-shaped shell env, but the wave hard requirement says credentials never cross into shell children.
- **Fix:** `mergeEnv` now filters both inherited and explicit env with `secret.IsSecretEnvVar`; the legacy test was updated to assert the stricter boundary.
- **Files modified:** `internal/agent/tools/shell_exec.go`, `internal/agent/tools/shell_exec_test.go`
- **Verification:** `go test ./internal/agent/tools -run 'TestShellExec.*Env|TestRedact|TestMergeEnv' -count=1`
- **Committed in:** `408d841d`

---

**Total deviations:** 1 auto-fixed (1 missing critical)
**Impact on plan:** Security-tightening required by the wave must-have. No scope creep.

## Issues Encountered

- Red tests first reproduced the known gaps: `IsSecretEnvVar` did not exist, DSN env/output leaked, command hooks inherited provider secrets, and trace rows wrote raw history/user text. All are resolved.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Wave 3 can rely on crash-contained, credential-safe, observable agent boundaries while it hardens MCP resilience, reasoning-router bounds, and active budget/wallclock behavior.

---
*Phase: 22-bug-fix*
*Completed: 2026-06-15*
