# Architecture Review

## Current Architecture

Aura is a Go-based local-first agent platform with:

- A loop-oriented LLM agent in `internal/agent`.
- Built-in shell, filesystem, search, ask-user, and transfer tools under `internal/agent/tools`.
- MCP server management and tool bridging under `internal/mcp` and `internal/agent/mcptools`.
- Conversation, pause/resume, tool-invocation, and memory persistence under `internal/conversations`, `internal/askuser`, `internal/toolinvocations`, and memory-related packages.
- AG-UI/Web UI serving under `cmd/aura/serve*.go` and `internal/agui`.
- Postgres, Neo4j, Garage/S3-compatible object storage, scheduler, and Telegram/Web integrations through configuration and Compose.

## Agent Loop Design

The main loop in `internal/agent/llm_agent.go` has several industrially useful controls:

- Budget construction fails fast on invalid inputs and defaults to bounded max steps and wallclock duration (`internal/agent/budget.go:40-44`, `internal/agent/budget.go:110-179`).
- Each loop step consumes budget and checks wallclock expiration (`internal/agent/budget.go:225-239`).
- LLM stream calls use per-call timeout contexts (`internal/agent/llm_agent.go:204-207`).
- Tool dispatch handles completion gates, ask-user exclusivity, and truncation feedback (`internal/agent/llm_agent.go:344-410`).
- Parallel tool execution is bounded and recovers from panics (`internal/agent/llm_agent_parallel.go:21-87`).

The main architectural weakness is that tool authority is broader than loop authority. The loop is bounded, but a model-selected shell or filesystem action can still mutate the host outside Aura's workspace, spawn long-running background work, leak files, or rely on fail-open hooks.

## Main Components And Responsibilities

| Component | Responsibility | Assessment |
|---|---|---|
| `internal/agent/llm_agent.go` | LLM loop, model streaming, step dispatch | Solid budget/timeouts; terminal tool semantics need hardening. |
| `internal/agent/llm_agent_dispatch.go` | Splits terminal `text_response` from runnable tools | Allows runnable siblings before terminal response. |
| `internal/agent/llm_agent_parallel.go` | Bounded concurrent tool execution | Good baseline; needs stronger side-effect transaction model. |
| `internal/agent/tools/shell_exec.go` | Host shell execution | Powerful but production-unsafe without sandbox/capabilities. |
| `internal/agent/tools/fs*.go` | Host filesystem access | Useful locally; lacks workspace fence except for one skills-dir policy. |
| `internal/agent/hooks*.go` | Command hook policy enforcement | Hash-pinned hooks are good; default fail-open is risky. |
| `internal/mcp/*` | MCP process/runtime management | Better-than-basic timeouts and env filtering; remote trust defaults and legacy env path need hardening. |
| `internal/conversations/*` | Conversation persistence and sidecar loading | ID validation exists for writes; reads trust persisted sidecar paths. |
| `internal/runner/runner_resume.go` | Pause/resume answer injection | Single-answer path is atomic-first; batch path is not. |
| `internal/agui/*`, `cmd/aura/serve*.go` | HTTP/Web UI, auth, capabilities | Strong route gating when configured; listener failure and healthcheck need production semantics. |

## Architecture Weaknesses

1. **Capability model is implicit.** Tool descriptors declare mutating/deferred behavior, but there is no central policy engine that maps actor, runtime profile, path, command, network, and tool to a deny/approve/execute decision.
2. **Host mutation and audit are not atomic.** Mutating tool execution can proceed even when the invocation ledger insert fails.
3. **Terminal response is not an execution barrier.** `text_response` can be paired with side-effect tools.
4. **Persistence trust is asymmetric.** Conversation sidecar write paths are validated, but read paths trust DB state.
5. **Runtime profile is not explicit enough.** Local trusted operation and server production share too much configuration surface.
6. **Operational state is best-effort.** Background shell jobs, listener failures, and tool ledgers need stronger production failure semantics.

## Reference Comparison

Reference code under `D:\tmp` suggests useful patterns:

- `D:\tmp\adk-go-study\session\session_test\service_suite.go` includes explicit partial-event persistence tests and session access tests. Aura should add similarly direct regression tests for pause/resume atomicity and terminal/sibling tool semantics.
- `D:\tmp\agent-memory\tests\unit\test_provenance_tracking.py` tests source-message, extractor, confidence, and statistics provenance. Aura already has tool invocation previews and provenance tags; memory extraction should get similarly explicit provenance tests.
- `D:\tmp\agent-infra-sandbox\evaluation\README.md` organizes evaluations by tool capability classes such as file operations, shell, browser, package management, error handling, and multi-tool workflows. Aura should adopt this as an evaluation taxonomy for tool safety and reliability.
- `D:\tmp\go-swarm\pkg\agent\agent.go` is much simpler and less production-ready than Aura. It is useful mostly as a contrast: Aura has better budgeting and persistence, but it must now harden authority boundaries.

## Suggested Target Architecture

Move toward a policy-first runtime:

1. **Agent loop core:** deterministic step runner with durable checkpoints, explicit terminal semantics, replayable inputs/outputs, and resumable state.
2. **Tool gateway:** single enforcement point for capability checks, approvals, sandbox selection, dry-run, idempotency keys, timeouts, retries, result normalization, ledger writes, and rollback metadata.
3. **Runtime profiles:** strict separation between local trusted mode and server production mode.
4. **Persistence layer:** append-only conversation event log, validated sidecar references, transactionally coupled pause/resume and tool-ledger state.
5. **Observability layer:** OpenTelemetry traces, structured logs, metrics, audit ledger, SLO dashboards, and alert rules.
6. **Sandbox layer:** per-tool filesystem roots, subprocess isolation, network egress policy, resource limits, TTLs, and immutable audit records.

## Subagent Update: Architecture Deltas

The delegated module review added four architecture-level concerns:

1. **Identity boundary:** AG-UI/Web APIs need an explicit authenticated-principal boundary for conversations, approvals, and new conversation ownership. The runner already rejects identity mismatches during execution, but API list/mutation and creation paths must enforce ownership earlier.
2. **MCP transport boundary:** Managed MCP server type, trust, runtime, mount, and open behavior need one canonical classifier. Mixed transport definitions should be rejected before trust is evaluated.
3. **Pause/resume transaction boundary:** Pause rows, assistant `ask_user` tool-call turns, and resume answer turns should be one transaction or a recoverable state machine.
4. **Lifecycle boundary:** Conversation deletion, background shells, MCP processes, scheduler jobs, and sidecars need lifecycle ownership so persistence, in-memory state, and OS processes cannot drift apart.
