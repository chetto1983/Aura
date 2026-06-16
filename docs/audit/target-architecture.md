# Target Architecture

## Design Goals

- Enforce permissions outside model cooperation.
- Make every side effect auditable, bounded, and recoverable.
- Preserve the productive local-agent experience while adding production-safe profiles.
- Make memory, context, and tool selection deterministic where correctness matters.
- Support single-tenant and multi-tenant deployment without changing tool code.

## Recommended Modules

## `agent/runtime`

Owns the turn loop, dispatcher, event stream, and terminal semantics.

Responsibilities:

- Validate `InvocationContext`.
- Enforce budget deadline and per-node timeout.
- Require terminal exclusivity.
- Emit structured events for every transition.

## `agent/policy`

Owns authorization decisions for tool calls.

Interfaces:

```go
type Decision string

const (
    DecisionAllow  Decision = "allow"
    DecisionPrompt Decision = "prompt"
    DecisionDeny   Decision = "deny"
)

type ExecutionPolicy interface {
    Evaluate(ctx context.Context, call llm.ToolCall, spec tools.Spec, subject Subject) (PolicyDecision, error)
}
```

Inputs:

- Identity ID
- Channel type
- Session ID
- Workspace root
- Tool risk tier
- Mutability
- Requested path/network/process access
- MCP server identity

## `agent/sandbox`

Owns filesystem, process, and network enforcement.

Profiles:

- `read_only`: no writes, no shell, no network except approved web fetch.
- `workspace_write`: writes only under workspace roots.
- `network_disabled`: shell/process execution with egress blocked.
- `full_runtime_break_glass`: explicit local operator approval, logged and time-limited.

Implementation options:

- Harden the existing Aura container first: non-privileged, no Docker socket, minimal mounts, scoped secrets, non-root user where possible, read-only root filesystem where possible, and egress controls.
- Windows: restricted token, ACL materialization, Windows Filtering Platform.
- Linux: namespaces, Landlock, bubblewrap, seccomp, cgroups.
- Containers: per-turn or pooled tool runner with mounted workspace.

## `agent/tooltx`

Owns mutating tool transactions.

Interfaces:

```go
type ToolTransaction struct {
    RequestID     string
    ConversationID string
    ToolCallID    string
    ToolName      string
    IdempotencyKey string
    Status        string
}
```

States:

- `planned`
- `approved`
- `started`
- `committed`
- `failed`
- `unknown_after_crash`
- `compensated`

Requirements:

- Mutating tools declare idempotency behavior.
- External side-effect tools must supply idempotency keys or be non-retriable.
- Resume logic reads transaction state before allowing replay.

## `agent/memory`

Owns deterministic recall and write policy.

Flow:

1. Extract query intent and identity.
2. Retrieve relevant memories before model request.
3. Inject memory as scoped context with provenance.
4. After turn, classify durable memory candidates.
5. Require confidence threshold and safety filters before write.

## Runtime Flow

1. Runner receives turn and resolves identity/profile/workspace.
2. Runner loads managed history and deterministic memory context.
3. `LlmAgent.Run` validates context and derives budget deadline if needed.
4. Model response is streamed and parsed.
5. Dispatcher rejects mixed terminal/side-effect batches.
6. For each tool call:
   - registry snapshot resolves spec;
   - policy evaluates allow/prompt/deny;
   - approvals pause through durable pending state;
   - sandbox executes allowed call;
   - tool transaction records start/result;
   - event and ledger persist through durable outbox.
7. Final answer is persisted atomically with usage.

## Observability Model

Required telemetry:

- Turn span: identity class, profile, model, outcome, budget reason.
- Tool span: tool name, risk tier, mutating, policy decision, approval id, sandbox profile, duration.
- Metrics: success/error rates, policy denies/prompts, tool latency, LLM latency, sidecar bytes, background jobs, ledger queue depth.
- Logs: structured events with request ID, conversation ID, tool call ID, and redacted arguments.

## Failure Handling Model

- LLM transient failure: retry according to classifier and circuit breaker.
- Tool timeout: cancel sandbox/process group, record timeout result.
- Policy prompt: pause turn durably.
- Crash after tool start: resume from tool transaction state.
- Ledger failure: enqueue durable outbox, mark observability degraded.
- Sidecar write failure: fail the turn if output cannot be safely persisted or summarized.

## Persistence And Checkpointing Strategy

Durable stores:

- Conversation turns.
- Pending approvals.
- Tool transactions.
- Tool invocation ledger/outbox.
- Sidecar object store.
- Memory records.

Checkpoint rules:

- Persist assistant tool-call batch before executing tools.
- Persist mutating tool transaction `started` before side effect.
- Persist tool result or `unknown_after_crash` recovery state before model re-entry.
- Never blindly replay mutating tools on resume.
