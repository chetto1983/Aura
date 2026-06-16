# Architecture Review

## Current Architecture

The agent package is centered on `LlmAgent`, an iterator-style loop that builds a model request from in-memory history, streams model output, dispatches tool calls, appends tool results, and terminates on `text_response` or a fallback finalization path.

Key components:

- `internal/agent/agent.go`: common `Agent` interface and `InvocationContext`.
- `internal/agent/llm_agent.go`: main LLM loop, timeout wrapping, stream handling, tool dispatch handoff, terminal response handling.
- `internal/agent/llm_agent_dispatch.go`: partitions terminal and non-terminal tool calls and executes runnable calls.
- `internal/agent/budget.go` and `budget_dedup.go`: step budgets, wallclock deadlines, per-branch budgets, deduplication.
- `internal/agent/tools`: native tool implementations for shell, filesystem, web, tasks, skills, and result sidecars.
- `internal/agent/mcptools`: MCP tool bridge, schema caps, reconnecting transport, call timeouts.
- `internal/runner`: production conversation runner that persists events and rehydrates history.
- `internal/conversations`: durable turn store, sidecar spills, context compaction, and repair of invalid tool-call pairs.

## Agent Loop Design Analysis

The main loop in `internal/agent/llm_agent.go` consumes one budget step per iteration at line 251, derives an LLM call timeout from `TotalTimeoutSec` at line 263, builds a request from `a.history`, opens and drains the LLM stream, and dispatches tool calls at line 452. Tool execution uses a bounded worker pool in `internal/agent/llm_agent_parallel.go` lines 30-64.

Good properties:

- Step and wallclock budgets exist.
- LLM stream open retry is conservative and classifies transient errors.
- Tool results are appended in provider-compatible assistant/tool order.
- Deduplication occurs before execution.
- Panics inside the loop and tool worker are recovered and recorded.
- Production `Runner` constructs fresh agents from persisted history on each turn.

Weaknesses:

- `Run` assumes a non-nil budget and a context already bounded by `Budget.WithDeadline`.
- Per-tool node timeout defaults to disabled.
- `text_response` is treated as terminal only after sibling runnable tools have executed.
- Side-effect idempotency is not a first-class contract.
- Tool policy is mostly encoded per-tool, not enforced by a central capability profile.

## Tooling And Action Execution

Native shell and filesystem tools are intentionally powerful. `cmd/aura/main.go` registers `ShellExec` with the process working directory and registers filesystem tools with no `WorkspaceRoot` at lines 150-163. Comments in `internal/agent/tools/shell_exec.go` lines 20-23 and `internal/agent/tools/fs.go` lines 14-18 state that the tools have full access with no sandbox/path fence; in the known container deployment, that means the Aura container namespace and mounted resources.

The code has useful mitigations: destructive-shell approval patterns, secret redaction in shell output, skills-directory write fencing, SSRF controls for web fetches, and untrusted output envelopes. Because Aura runs in a container, the immediate blast radius is the container namespace and its mounts, assuming the container is not privileged and does not mount host-sensitive resources. These mitigations plus containerization are still not sufficient production permission boundaries by themselves.

## Memory And Context Management

`LlmAgent.history` is in-memory only (`internal/agent/llm_agent.go` lines 36-52). Production durability is owned by `Runner`, which loads managed history and creates a fresh `LlmAgent` per turn (`internal/runner/runner.go` lines 351-363 and 549-585). Conversation history uses a context ladder that evicts old tool output to `read_tool_output` pointers.

The design is reasonable, but memory recall/write behavior is mostly prompt-directed (`internal/agent/prompt.go` lines 61-67). It is not a deterministic runtime middleware that guarantees recall before relevant turns or write after durable preference extraction.

## Comparison With Reference Code

The sampled ADK reference under `D:\tmp\adk-go-study` models human confirmation as part of tool context. `agent/context.go` lines 151-189 exposes `ToolConfirmation` and `RequestConfirmation`; `agent/callback_context.go` lines 192-208 records confirmation requests and stops summarization until the user responds.

The sampled Codex reference under `D:\tmp\codex` models sandbox policy and execution policy separately. `codex-rs/windows-sandbox-rs/src/resolved_permissions.rs` includes filesystem and network sandbox policy resolution, and `codex-rs/execpolicy/examples/example.codexpolicy` shows command policy rules with `forbidden` and `prompt` decisions.

These references support a target design where Aura treats permissions as runtime configuration, not scattered tool-specific checks.

## Suggested Target Architecture

Move toward:

- An `ExecutionPolicy` service that evaluates every tool call before dispatch.
- A `CapabilityProfile` per identity/channel/session: local trusted, remote read-only, workspace-write, full-runtime break-glass.
- A container-aware sandbox executor for shell and filesystem operations with workspace roots, read-only roots, writable roots, and network policy.
- A durable `ToolTransaction` abstraction with idempotency key, start record, result record, and recovery semantics.
- A deterministic memory middleware that performs recall and write classification outside model discretion.
- Terminal exclusivity in the dispatcher.
- First-class health/readiness checks for model, database, MCP, embedder, scheduler, and sidecar directories.
