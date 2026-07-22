# Aura LLM, tools, MCP, and idempotency runbook

Applies to `AuraLLMErrorBudgetBurn`, `AuraLLMLatencyBudgetBurn`, `AuraToolErrorRateHigh`, `AuraMCPErrorRateHigh`, `AuraMCPLatencyHigh`, `AuraIdempotencyConflictGrowth`, `AuraIdempotencyInProgressGrowth`, and `AuraIdempotencyIndeterminate`.

## Meaning

The LLM alerts use five-minute and one-hour 14.4x burn windows against a one-percent error/slow-call budget; they page only when both windows remain above the cap. Tool, MCP, and idempotency alerts identify warning-level causes. `indeterminate` means Aura cannot prove whether a remote mutation completed and will not automatically reinvoke it.

## Drilldown and correlation

Use `aura-agents` for LLM burn/latency and `aura-tools-mcp` for tool class, MCP transport, and idempotency state. Drill down only with the finite labels `operation`, `tool_class`, `transport`, `outcome`, `error_class`, and `state`. Correlate the time range to Tempo spans. Server names, tool names, identities, request/conversation IDs, operation keys, prompts, arguments, results, URLs, paths, and raw errors must remain absent from Prometheus.

## Immediate safe actions

1. Separate provider errors from local timeout/cancel outcomes using `error_class` and verify whether only one transport is affected.
2. For latency burn, check downstream health and queueing before raising timeouts. Do not convert a bounded timeout into an unbounded call.
3. For MCP errors, probe the managed server and trust configuration. Read-only operations may use the established reconnect policy; mutating calls must never be replayed after ambiguous transport failure.
4. For tool warnings, isolate the bounded `tool_class`; do not add the raw tool name as a label.
5. For conflicts, verify callers reuse a stable key only for the same normalized payload.
6. For in-progress growth, inspect operation age/retry guidance and reconcile crash orphans. Never steal ownership based only on elapsed retry time.
7. For `indeterminate`, require operator/domain confirmation before any compensating action.

## Escalation

Page the runtime owner when either LLM burn alert persists beyond ten minutes, multiple transports degrade together, or an indeterminate mutation affects durable user data. Include dashboard/trace links and bounded classifications only. Escalate to the downstream owner when Aura remains healthy and a single provider/managed MCP server is the isolated cause.

## Recovery evidence

Require both LLM burn windows below 14.4, p95 latency below 30 seconds, tool/MCP error ratios below ten percent, MCP p95 below five seconds, no new conflicts/indeterminate outcomes, and one successful synthetic read plus idempotent mutation replay. Preserve the terminal operation record used to prove that no duplicate effect occurred.

