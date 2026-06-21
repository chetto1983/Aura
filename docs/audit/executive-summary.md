# Executive Summary

## Assessment

Aura is a strong local-first agent platform with serious engineering investment in loop budgeting, tool timeout handling, parallel execution limits, MCP integration, auth-aware Web UI routes, and persistence. It is not yet ready to be operated as an industrial multi-user or high-trust production system.

The core production gap is not one missing feature. It is the mismatch between Aura's current trust model and an industrial deployment model. The code is explicit that shell and filesystem tools run with the operator's full host privileges. That can be acceptable for a single trusted local operator, but it is a production blocker for shared services, remote operators, regulated environments, or any runtime where prompt-injected tool use must be contained.

Production readiness score: **4.6 / 10**

## Finding Counts

- P0: 0
- P1: 10
- P2: 28
- P3: 13
- Total: 51

## Top Risks

1. **Unbounded host authority for model-selected tools.** `shell_exec` and filesystem tools can access the full host unless the operator adds external containment.
2. **Misleading sample configuration weakens shell guardrails.** `.env.example` sets an empty destructive-pattern override that disables default approval patterns when copied into `.env`.
3. **Terminal response semantics allow side effects first.** A final `text_response` is not exclusive; sibling mutating tools run before the terminal response.
4. **Resume transaction boundaries can corrupt conversation state.** Single resume can claim without a durable answer on append failure, and batch resume appends answers before atomically claiming pending pauses.
5. **Stored sidecar paths can rehydrate arbitrary local files if DB rows are corrupted or compromised.**
6. **Security hooks fail open by default.** Hook process failure, timeout, or crash can allow commands that a hook was meant to deny.
7. **Static Garage/S3 credentials are accepted as defaults.**
8. **Service availability can be falsely reported.** AG-UI listener failure does not necessarily fail the daemon/container healthcheck.
9. **Managed MCP transport classification can be inconsistent.** A `url` + `command` entry can be admitted as remote HTTP trust but opened as local stdio command execution.
10. **Provisioned Web/API identities are not consistently enforced.** Conversation and approval list/mutation surfaces can cross identity boundaries.

## Immediate Actions

1. Add an explicit runtime profile: `local_trusted`, `single_user_hardened`, and `server_production`. Refuse `server_production` if shell/filesystem tools are unfenced.
2. Fix `.env.example` so destructive shell patterns are unset by default, not set to an empty override.
3. Make `text_response` mutually exclusive with mutating or runnable sibling tools, or reject such batches before execution.
4. Reorder batch resume to claim pauses before injecting answers, or move pause claiming and conversation append into one transaction.
5. Validate conversation sidecar paths against the computed conversation sidecar directory before reading.
6. Change command-hook default policy to fail-closed for configured hooks, or require explicit `AURA_COMMAND_HOOK_FAIL_POLICY`.
7. Fail configuration validation when object-store credentials are default static values outside a development profile.
8. Make AG-UI listener failure fatal to the daemon or expose it through readiness and Docker healthchecks.
9. Reject ambiguous MCP server definitions that mix `url`, `command`, and type, and require explicit trust for every runnable transport.
10. Scope AG-UI conversations, approvals, and new conversation ownership to the authenticated principal.

## Important Positives

- Loop budgets have fail-fast construction and wallclock/step controls (`internal/agent/budget.go`).
- LLM calls are wrapped in per-call timeouts (`internal/agent/llm_agent.go`).
- Parallel tool execution has a bounded worker pool and panic recovery (`internal/agent/llm_agent_parallel.go`).
- MCP calls have default timeouts and transport abort on timeout (`internal/agent/mcptools/timeout.go`, `internal/mcp/client.go`).
- AG-UI routes have authentication and capability gates when a secret is configured (`internal/agui/auth.go`, `cmd/aura/serve_webui.go`).
- Tool result previews, sidecars, and `read_tool_output` are bounded and path-validated (`internal/agent/tools/result.go`, `internal/agent/tools/read_tool_output.go`).

## Unknowns And Needs Confirmation

- Live deployment posture, secret rotation procedures, backup restore objectives, and operational SLOs were not verifiable from source alone.
- A formal delegated deep security scan with independent subagents was not run because the current tool policy did not allow spawning subagents without an explicit user request for delegation. This report is a manual deep audit.
- After the user's explicit follow-up request, six read-only subagents were spawned across module slices and their evidence was merged into this update. This still was not a live exploit exercise or dynamic security test.
- Load, chaos, recovery-time, and disaster-recovery behavior require execution against a deployed environment.
