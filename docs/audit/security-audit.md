# Security Audit

## Security Posture

Aura currently behaves like a trusted local coding assistant with powerful capabilities inside its runtime container. It has valuable prompt-injection mitigations, but it does not have an enforceable production permission boundary for arbitrary shell/filesystem operations or model-authored self-extension. Containerization reduces host blast radius only if the container is hardened and sensitive mounts/secrets/network paths are constrained.

## Confirmed Security Strengths

- Untrusted tool outputs are wrapped by default in `internal/agent/trust.go`.
- Unknown tools default to untrusted.
- Web fetch has SSRF checks, redirect revalidation, content-type restrictions, timeouts, and size caps in `internal/web`.
- Shell output redacts common environment-secret values.
- `fs_write` and `fs_edit` deny writes into `SkillsDir`.
- Scheduled `agent_job` tasks are gated to `pending_approval` in `internal/agent/tools/task.go` lines 204-212.
- `ask_user` is treated as a controlled pause path.

## Critical Security Risks

## Full Container/Runtime Execution

Evidence:

- `internal/agent/tools/shell_exec.go` lines 20-23 describes shell as a full terminal with no sandbox.
- `internal/agent/tools/fs.go` lines 14-18 describes full filesystem access with no path fence; in the container deployment this applies to container-visible files and mounts.
- `cmd/aura/main.go` lines 150-163 registers shell and FS tools into the main registry.

Risk:

- Any path that can influence model tool choice can reach command and file operations inside the Aura container, including mounted volumes and injected secrets.

Mitigation:

- Central policy engine plus container-aware sandboxed execution. Remote/server contexts should not get full-runtime tools by default.

## Model Self-Extension

Evidence:

- `internal/skilladapters/skilladapters.go` lines 23-25 labels tool writes as actor `model`.
- `internal/skills/writer.go` lines 163-166 bypasses gates for model create/update when `always=false`.
- `internal/skills/writer.go` lines 136-142 auto-activates the pending mutation.

Risk:

- The model can install or update its own active instructions. This increases persistence risk after prompt injection.

Mitigation:

- Require human approval for all model-authored skill changes unless an explicit disposable sandbox profile is selected.

## Prompt Injection Surfaces

Surfaces:

- Web search/fetch output.
- Files read from the local host.
- MCP tool output and descriptions.
- Shell output.
- Skill bodies and generated skills.
- Conversation history and memory recall.

Existing mitigation:

- `renderToolResultForPrompt` wraps untrusted output with nonce-tagged XML-like boundaries.

Remaining gap:

- Wrapping reduces instruction-following risk but does not stop the model from voluntarily calling dangerous tools after reading malicious content.

Mitigation:

- Combine untrusted-output framing with policy enforcement. Dangerous tools should require approval/capability regardless of model reasoning.

## Unsafe File Operations

Confirmed:

- `FSWrite` direct `os.WriteFile` can partially overwrite files.
- Sidecars use plaintext `os.WriteFile`.
- FS tools can address absolute paths.

Mitigation:

- Atomic writes, workspace jail, allowlist/denylist roots, encrypted sidecars, retention policy.

## Unsafe Subprocess Execution

Confirmed:

- Shell execution runs in the host shell.
- Destructive approval regexes are advisory.
- Background shell jobs detach from request context.

Mitigation:

- Parse command into policy units, enforce deny/prompt/allow decisions, add sandbox, disable network by default, require TTL and owner.

## Secret Leakage

Known mitigations:

- Shell env secret redaction exists.
- Reasoning trace redacts private fields by default.

Risks:

- Full reasoning trace persists plaintext prompt/history.
- Tool sidecars and conversation sidecars can contain secrets.
- Shell commands can place secrets in argv, files, or child process output.

Mitigation:

- Secret scanner at ledger/sidecar boundaries, encrypted storage, trace retention policy, and production guard against full trace.

## Permission Boundaries

Current boundary:

- Mostly trusted-local-operator conventions plus the external container boundary.

Required boundary:

- Identity-aware capability profiles.
- Central authorization before tool dispatch.
- Auditable human approval records for high-risk actions.
- OS-level or container-level enforcement for filesystem, process, and network operations.
- Explicit container hardening requirements: non-privileged, no Docker socket, least-privilege mounts, non-root user where possible, read-only root filesystem where possible, egress policy, and scoped secrets.

## Security Recommendations

1. Introduce `ExecutionPolicy.Evaluate(ctx, ToolCall) Decision`.
2. Attach identity, channel, session, capability profile, and workspace root to `InvocationContext`.
3. Restrict remote/server profiles to no shell, no arbitrary FS, no skill activation.
4. Require approval for model-authored skill changes in production profile.
5. Make MCP mutability local-policy-driven, not server-hint-driven.
6. Add high-signal security tests for prompt injection, path traversal, command policy bypass, and sidecar secret leakage.
