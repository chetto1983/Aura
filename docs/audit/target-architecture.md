# Target Architecture

## Design Goals

The target architecture should let Aura remain powerful for trusted local use while becoming safe, observable, and recoverable for industrial deployments.

Core goals:

- Deterministic and replayable agent loop.
- Explicit authority boundary for all tools.
- Durable audit trail for mutating actions.
- Runtime profile separation.
- Safe memory/context persistence.
- Production-grade observability and readiness.
- Testable security policy.

## Recommended Modules

### AgentLoop

Responsibilities:

- Own step budget, wallclock budget, cancellation, LLM call timeout, and deterministic dispatch.
- Enforce terminal semantics before tool execution.
- Produce replayable step records.
- Never execute tools directly.

Interfaces:

```go
type AgentLoop interface {
    Run(ctx context.Context, input RunInput) (RunResult, error)
}

type StepRecord struct {
    RunID string
    Step int
    ModelRequestDigest string
    ModelResponseDigest string
    PlannedToolCalls []ToolCallPlan
    Terminal bool
}
```

### ToolGateway

Responsibilities:

- Central policy decision for every tool.
- Approval resolution.
- Sandbox selection.
- Timeout/retry/idempotency enforcement.
- Ledger reservation and finalization.
- Result normalization and redaction.

Interfaces:

```go
type ToolGateway interface {
    Execute(ctx context.Context, call ToolCall, actor ActorContext) (ToolResult, error)
}

type ToolPolicyDecision struct {
    Decision string // allow, deny, require_approval
    Reason string
    Sandbox SandboxSpec
    RequiresLedger bool
    IdempotencyKey string
}
```

### PolicyEngine

Responsibilities:

- Evaluate actor, runtime profile, tool, path, command class, MCP trust class, network egress, and approval state.
- Return deterministic decisions with reasons.
- Own default-deny production behavior.

Policy inputs:

- Actor identity and capabilities.
- Conversation/run/session ID.
- Runtime profile.
- Tool descriptor and mutating/read-only flags.
- Resource path or command.
- MCP server trust class.
- Approval token, expiry, and scope.

### SandboxManager

Responsibilities:

- Provide host-direct execution only in dev/local trusted profiles.
- Provide restricted workspace mounts for hardened/server profiles.
- Apply CPU, memory, process, wallclock, and network limits.
- Terminate process groups on cancellation/timeout.

Supported backends:

- `host_direct_dev`
- `workspace_process_restricted`
- `container_workspace`
- `disabled`

### PersistenceLayer

Responsibilities:

- Append-only conversation event log.
- Pause/resume state with idempotency keys.
- Tool invocation ledger.
- Sidecar content by computed IDs, not trusted raw paths.
- Retention metadata.

Important rule:

Any persisted file reference should be reconstructible from trusted IDs and a configured root. Raw absolute paths from DB rows should not be read without validation.

### ObservabilityLayer

Responsibilities:

- Structured logs.
- Metrics.
- OpenTelemetry traces.
- Audit events.
- Dashboards and alert rules.

Required identifiers:

- `run_id`
- `conversation_id`
- `step_id`
- `tool_invocation_id`
- `actor_id`
- `runtime_profile`
- `policy_decision_id`
- `mcp_server_id`

## Runtime Flow

1. User request enters through CLI, AG-UI, Telegram, or scheduler.
2. Runtime profile and actor context are resolved.
3. AgentLoop builds a model request from conversation state and memory.
4. Model response is parsed into terminal response and tool plans.
5. AgentLoop validates terminal semantics. A terminal response with mutating siblings is rejected before side effects.
6. Each tool plan is sent to ToolGateway.
7. ToolGateway asks PolicyEngine for a decision.
8. If approval is required, a scoped approval request is persisted and no side effect occurs.
9. If allowed, ToolGateway reserves a durable ledger entry.
10. SandboxManager executes the tool with limits.
11. ToolGateway normalizes, redacts, persists result, and finalizes ledger state.
12. AgentLoop appends tool results and continues or terminates.
13. ObservabilityLayer emits logs, metrics, traces, and audit records throughout.

## Failure Handling Model

| Failure | Target behavior |
|---|---|
| LLM timeout | Record timeout, consume step, return recoverable error or ask model to continue within budget. |
| Tool timeout | Kill process group or abort MCP transport, persist failed invocation. |
| Policy denial | Append normalized denial result; do not execute side effect. |
| Ledger reservation failure | Block mutating tools in production profile. |
| Sidecar write failure | Return explicit error for required persistence; do not silently lose large output in production. |
| Pause resume race | Idempotency key ensures one answer per pause. |
| Listener failure | Readiness false or process exit. |
| DB outage | Stop mutating actions requiring persistence; serve health as not ready. |
| Background job TTL expiry | Terminate job, persist status, emit metric. |

## Persistence And Checkpointing Strategy

Recommended durable records:

- Conversation event log.
- Step records.
- Tool plans.
- Policy decisions.
- Tool invocation states.
- Pause requests and responses.
- Sidecar content references.
- Memory extraction provenance.

Checkpointing:

- Checkpoint after each model response and after each tool result batch.
- Use idempotency keys for tool calls and pause answers.
- On restart, recover incomplete invocations by status:
  - `planned`: safe to discard or retry if idempotent.
  - `started`: inspect tool-specific recovery policy.
  - `succeeded_unpersisted`: reconcile if external proof exists.
  - `failed`: append normalized failure result if not already visible to model.

## Observability Model

Metrics:

- Loop runs started/completed/failed.
- Step count and wallclock budget exhaustion.
- LLM latency and timeout rate.
- Tool invocation count by tool/policy/status.
- MCP timeout/error rate by server.
- Pause/resume pending age and failure count.
- Background job count/age/status.
- Sidecar disk usage.
- Readiness dependency state.

Traces:

- One root span per run.
- Child spans for LLM calls, policy decisions, tool execution, MCP roundtrips, DB operations, and resume handling.

Logs:

- Structured JSON logs in production.
- No raw secrets.
- Link every high-risk action to actor, approval, policy, and ledger IDs.

## Production Runtime Profiles

| Profile | Intended use | Shell | Filesystem | MCP | Secrets validation | Health |
|---|---|---|---|---|---|---|
| `dev` | Local development | allowed with warnings | broad | permissive | sample allowed | basic |
| `local_trusted` | Single trusted operator | allowed with approvals | broad with warnings | managed trust | required for remote services | readyz |
| `single_user_hardened` | Sensitive local work | sandboxed | workspace default | explicit trust | strict | readyz + metrics |
| `server_production` | Shared/remote production | disabled or sandboxed | workspace only | explicit trust only | strict fail-fast | orchestrator readiness |

