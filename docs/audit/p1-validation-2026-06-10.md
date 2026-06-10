# P1 Closure Validation - 2026-06-10

All P1 findings from the audit are closed in code by this remediation pass.

Validation:

- `go test -count=1 ./...`
- `go test ./internal/agent ./internal/llm ./internal/runner ./internal/conversations ./internal/agui ./internal/cron ./internal/agent/workflow ./internal/agent/tools ./internal/skills ./internal/mcp`

## Runtime and Reliability

- Budget wallclock is now applied to runner/CLI invocation contexts.
- Tool execution observes `NodeTimeout`.
- `shell_exec` requested timeouts are clamped.
- Provider stream open retries handle 429/5xx and bounded `Retry-After`.
- Mid-stream retryable errors are retried once without persisting partial answers.
- `internal/llm` includes a consecutive-failure breaker.
- Parallel tool fan-out is bounded by `AURA_LOOP_MAX_PARALLEL_TOOLS`.

## Tool and Security Surface

- `fs_edit` rejects empty `old_string`.
- Sync and background shell output buffers are bounded.
- Shell and MCP child environments filter inherited secret-shaped variables.
- Model-visible `task approve` is removed.
- `always:true` model-authored skills remain gated.
- `internal/agent/tools` is back inside the coverage gate.

## Persistence and Recovery

- ToolInvocation start/end events now persist assistant `tool_calls` and matching `RoleTool` turns.
- Load-time repair drops orphan `tool` messages and dangling assistant tool-call groups before provider replay.
- The crash-window orphan conversation no longer becomes a permanent 400 loop.

## Observability

- `GET /healthz` is mounted on the AG-UI mux.
- Health checks can run the daemon DB ping and include scheduler last-tick detail.
- Expvar counters were added for budget consumption, tool dispatch, LLM stream opens, and LLM stream retries.
- `/debug/vars` exposes those counters.

## Workflow Composition

- `Agent.Run` documents the single-budget-owner contract.
- `BudgetOwner` lets budget-aware children such as `LlmAgent` opt out of parent-side budget charging.
- `LoopAgent` checks context cancellation at loop boundaries.
- `LoopAgent` charges an empty non-budget-owned child pass, preventing `maxIter=0` hot spins.
