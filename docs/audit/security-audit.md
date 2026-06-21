# Security Audit

## Security Posture Summary

Aura is suitable today for a trusted local operator who understands that the model can request powerful host actions. It is not suitable as-is for an industrial server or shared environment where users, retrieved content, MCP servers, or browser-delivered input may be adversarial.

Primary security blockers:

- Full host shell/filesystem authority.
- Destructive shell guardrail disabled by copied sample env.
- Fail-open command hooks.
- Trust of DB-stored sidecar paths.
- Static object-store credentials.
- Remote/legacy MCP configuration paths that need stricter production governance.

## Prompt Injection Surfaces

Surfaces:

- User prompts and conversation history.
- Tool results, including shell output, filesystem reads, search results, MCP tool results, and `read_tool_output`.
- Files read from workspace or absolute paths.
- Remote MCP servers and HTTP MCP tool schemas/results.
- Memory/context retrieval.
- Human-in-the-loop resume context.

Existing protections:

- Tool results are marked with untrusted provenance in MCP bridging.
- Tool result previews and sidecars cap large content.
- `ask_user` has exclusivity handling in the agent loop.
- Loop budget and per-call timeouts limit runaway execution.

Gaps:

- Prompt-injected instructions can still request host shell or filesystem side effects.
- Terminal `text_response` can be batched with mutating siblings.
- There is no central policy engine that evaluates prompt-injection risk by tool, actor, path, and runtime profile.

Mitigations:

1. Add a tool policy gateway before all tool execution.
2. Require explicit approval for any high-risk prompt-influenced mutating action.
3. Make terminal response exclusive with mutating sibling calls.
4. Add adversarial prompt-injection regression tests.

## Unsafe File Access

Confirmed:

- Filesystem tools resolve absolute paths directly.
- Only skills directory writes have a targeted fence.
- Conversation sidecar loading trusts `content_sidecar_path` from the database.

Risks:

- Host-wide file reads/writes.
- Path traversal if future call sites bypass existing ID validation.
- Arbitrary file read via compromised sidecar path.

Mitigations:

1. Introduce workspace-root enforcement by default.
2. Support explicit allowlisted absolute paths with per-action approval.
3. Store sidecar references as IDs/sequence numbers, not raw paths.
4. Reject symlink escapes for sensitive persistence paths.
5. Use atomic writes consistently.

## Unsafe Subprocess Execution

Confirmed:

- `shell_exec` runs host shell commands.
- MCP stdio servers spawn local commands.
- Background shell jobs can run long-lived processes.
- Command hooks are hash-pinned, but default fail-open.

Risks:

- Command injection through prompt/tool context.
- Privilege escalation via inherited operator privileges.
- Secrets exposure through process environment or command output.
- Long-lived background resource consumption.

Mitigations:

1. Add command capability classes: read-only, build/test, network, package-manager, destructive, privileged.
2. Require sandbox selection before execution.
3. Add production deny-by-default policy for shell.
4. Default command hooks to fail-closed.
5. Add TTL and resource limits for foreground and background commands.

## MCP Security

Confirmed:

- Managed local command trust can be blocked or approved.
- MCP env forwarding filters obvious secret names.
- MCP calls have default timeout and transport abort behavior.
- Tool schemas are capped.

Risks:

- Empty trust on remote HTTP normalizes to runnable `remote_http`.
- Legacy `AURA_MCP_SERVERS_JSON` bypasses managed trust metadata.
- Remote MCP tool output is untrusted but can still influence subsequent model-selected tools.

Mitigations:

1. Require explicit trust for all remote MCP transports.
2. Deprecate or production-gate legacy MCP env config.
3. Add per-server capabilities and egress policy.
4. Add MCP tool allowlists by actor/profile.
5. Log MCP server identity, trust class, tool name, invocation ID, and policy decision.

## Secrets Handling

Confirmed:

- Static object-store defaults exist.
- Garage bootstrap imports default access/secret keys.
- MCP env filtering avoids forwarding common secret patterns.

Risks:

- Default object-store credentials.
- Full reasoning trace mode may store sensitive content.
- Shell command output can contain secrets and be persisted or shown to the model.

Mitigations:

1. Reject default secrets in production.
2. Add secret-source metadata and rotation procedure.
3. Add output redaction for known secret patterns before persistence.
4. Add encrypted trace and sidecar storage options.
5. Add "sensitive mode" that disables full traces and host reads by default.

## Permission Boundaries

Current boundary:

- Mainly operator trust plus some tool metadata, approvals, auth gates, and targeted fences.

Required industrial boundary:

- Actor identity and capability.
- Runtime profile.
- Tool class.
- Resource scope.
- Approval state.
- Ledger reservation.
- Sandbox policy.
- Network egress policy.
- Retention policy.

Recommended enforcement point:

All tool calls should pass through one `ToolGateway` before execution and one `ToolResultNormalizer` after execution. Individual tools should not each reinvent policy.

## Security Priorities

1. Make `server_production` deny host shell/filesystem by default.
2. Fix `.env.example` destructive shell pattern behavior.
3. Change terminal response exclusivity.
4. Validate sidecar reads.
5. Default command hooks to fail-closed.
6. Reject default object-store secrets.
7. Require explicit trust for remote MCP.
8. Add security regression tests for prompt injection and tool policy bypass.

## Subagent Update: Additional Security Priorities

1. Reject managed MCP definitions that mix `url` and `command`; the trust decision must match the actual transport opened.
2. Scope conversations and approvals to the authenticated identity in AG-UI/Web APIs.
3. Require explicit class and reason when trusting MCP servers through governance APIs.
4. Bind background shell jobs to session/actor and replace sequential IDs with random unguessable IDs.
5. Treat Docker MCP network allowlists as unenforced until backed by a real egress-control mechanism.
6. Replace permissive CORS wildcard behavior with explicit origin allowlists, especially in no-auth loopback mode.
7. Reject query-string long-lived access tokens outside short-lived setup bootstrap flows.
8. Use strict JSON decoding for privileged mutation routes.
