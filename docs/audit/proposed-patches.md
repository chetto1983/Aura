# Proposed Patch Recommendations

These are code-level recommendations only. No production source code was modified during this audit.

## Patch 1: Central execution policy before tool dispatch

- Affected file: `internal/agent/llm_agent.go`, `internal/agent/llm_agent_dispatch.go`, new `internal/agent/policy`
- Affected function/class: `LlmAgent.dispatch`, `LlmAgent.runTool`
- Reason for change: Shell, filesystem, MCP, skill, and task actions need one enforceable authorization point.
- Before behavior: Tool execution depends on per-tool checks and registry exposure.
- After behavior: Every tool call is evaluated as allow, prompt, or deny before execution.
- Suggested implementation approach:
  - Add `ExecutionPolicy` to `LlmAgentConfig` or `InvocationContext`.
  - Resolve `tools.Spec` before execution.
  - Call `policy.Evaluate(ctx, call, spec, subject)`.
  - Convert deny into a model-visible tool error.
  - Convert prompt into durable `ask_user`/approval pause.
  - Include policy decision in tool invocation events and spans.
- Tests required before merging:
  - Deny shell in remote profile, even though execution is containerized.
  - Allow read-only safe tools.
  - Prompt mutating tools in workspace profile.
  - Verify denied tools do not execute.
- Rollback considerations:
  - Feature-flag the policy with a default permissive local profile.
  - Keep existing tool-level checks during rollout.

## Patch 2: Gate model-authored skill create/update by default

- Affected file: `internal/skills/writer.go`, `internal/skilladapters/skilladapters.go`, `internal/agent/tools/skill_write.go`
- Affected function/class: `modelMutationBypassesGate`, `Writer.WriteMutation`, `SkillTool.writeAction`
- Reason for change: Model-authored active skills are persistent self-extension and should not auto-activate in production.
- Before behavior: Actor `model` with `always=false` bypasses gate for create/update and reaches `Activate(... ApprovalAuto ...)`.
- After behavior: Model create/update returns `pending_approval` unless explicit local sandbox profile allows auto-activation.
- Suggested implementation approach:
  - Add `SkillWritePolicy` or profile field to the writer adapter.
  - Change `modelMutationBypassesGate` to require `AllowModelSkillAutoActivate`.
  - Default config to false.
  - Update schema text in `skillParamsSchemaHonest`.
- Tests required before merging:
  - Model create/update returns pending by default.
  - Local sandbox override returns active.
  - Always-on and delete remain gated.
  - Pending skill is not visible to loader until approved.
- Rollback considerations:
  - Preserve a local-only env/config escape hatch during migration.
  - Log every auto-activation with profile and request ID.

## Patch 3: Make `FSWrite` atomic

- Affected file: `internal/agent/tools/fs_write.go`
- Affected function/class: `FSWrite.Execute`
- Reason for change: Direct overwrite can corrupt files on crash.
- Before behavior: `os.WriteFile(path, content, 0o644)`.
- After behavior: Write temp file in same directory, rename atomically, and preserve existing mode when possible.
- Suggested implementation approach:
  - Stat existing file; use its mode if present.
  - Call `atomicWriteFile(path, []byte(a.Content), mode)`.
  - Consider fsync in `atomicWriteFile` for stronger durability.
- Tests required before merging:
  - Existing content remains if temp write fails.
  - Existing mode preserved.
  - Create-new-file still works.
  - SkillsDir write denial still applies.
- Rollback considerations:
  - Atomic rename behavior on Windows should be tested first.
  - If replace semantics fail on a platform, fall back behind build-specific helper.

## Patch 4: Add background shell ownership and TTL

- Affected file: `internal/agent/tools/shell_bg.go`, `internal/agent/tools/shell_exec.go`, `internal/agent/tools/shell_poll.go`, `internal/agent/tools/shell_kill.go`
- Affected function/class: `BackgroundShells.Start`, `ShellPoll.Execute`, `ShellKill.Execute`
- Reason for change: Background jobs are detached from request context and process-scoped.
- Before behavior: Jobs use `context.Background`, have no TTL, and are addressable by `shell_id`.
- After behavior: Jobs carry owner session/identity, expire automatically, and enforce poll/kill authorization.
- Suggested implementation approach:
  - Add `OwnerSessionID`, `OwnerIdentityID`, `StartedAt`, `ExpiresAt` to `ShellJob`.
  - Derive owner from `toolCallCtx`.
  - Add default max runtime env/config with fail-fast parsing.
  - Reject poll/kill when owner does not match.
  - Add sweeper goroutine or check expiry on poll/start.
- Tests required before merging:
  - Expired shell is killed.
  - Cross-session poll and kill denied.
  - Process shutdown still kills all jobs.
  - Running job cap still works.
- Rollback considerations:
  - Existing shell IDs can be treated as legacy ownerless only for local profile.
  - Add migration note for UI clients holding shell IDs.

## Patch 5: Enforce budget deadlines inside `LlmAgent.Run`

- Affected file: `internal/agent/llm_agent.go`, `internal/agent/budget.go`
- Affected function/class: `LlmAgent.Run`, `LlmAgent.runTool`
- Reason for change: Direct package consumers can pass an unbounded context or nil budget.
- Before behavior: Production caller usually derives deadline, but `Run` assumes it.
- After behavior: `Run` validates context/budget and ensures context deadline no later than budget deadline.
- Suggested implementation approach:
  - Add `validateInvocationContext`.
  - If `ic.Ctx == nil`, use `context.Background`.
  - If `ic.Budget == nil`, yield controlled error and return.
  - If context has no deadline or later deadline, derive `Budget.WithDeadline(ic.Ctx)`.
  - Add default per-node timeout policy for external tools.
- Tests required before merging:
  - Nil budget yields error, no panic.
  - Blocking fake tool canceled by budget deadline.
  - Existing runner path still works.
- Rollback considerations:
  - Add this as a backwards-compatible safety net; should not affect callers already using `WithDeadline`.

## Patch 6: Reject mixed `text_response` plus mutating tool calls

- Affected file: `internal/agent/llm_agent_dispatch.go`
- Affected function/class: `LlmAgent.dispatch`
- Reason for change: A terminal response should not share a batch with side effects.
- Before behavior: Runnable tools execute before terminal `text_response`.
- After behavior: Mixed terminal plus mutating/unknown calls are rejected with model feedback.
- Suggested implementation approach:
  - During partition, inspect sibling specs.
  - If `terminalIdx >= 0 && len(runnable) > 0`, reject when any sibling is mutating or unknown.
  - Append model feedback asking for either tool calls or final response, not both.
  - Optionally allow safe read-only siblings only if explicitly desired.
- Tests required before merging:
  - `text_response` plus `fs_write` does not execute `fs_write`.
  - `text_response` alone still finalizes.
  - Multiple non-terminal calls still execute normally.
- Rollback considerations:
  - Feature-flag strict terminal mode for provider compatibility during rollout.

## Patch 7: Add MCP local mutability policy

- Affected file: `internal/agent/mcptools/bridge.go`, new MCP policy config
- Affected function/class: `BridgeTool.Spec`, `RefreshSpec`
- Reason for change: Server-provided `ReadOnlyHint` is not a reliable security boundary.
- Before behavior: `Mutating = !ReadOnlyHint`.
- After behavior: Local policy decides mutability; server hint is advisory.
- Suggested implementation approach:
  - Add per-server/per-tool policy: `mutating`, `read_only`, `requires_approval`, `disabled`.
  - Default external unknown tools to mutating.
  - Record conflicts between server hint and local policy.
- Tests required before merging:
  - Local mutating override wins over `ReadOnlyHint=true`.
  - Disabled MCP tool is not registered.
  - Conflict emits metric/log.
- Rollback considerations:
  - Start in warn-only mode, then enforce after manifests are created.

## Patch 8: Add durable tool ledger outbox

- Affected file: `internal/runner/runner_persist.go`, `internal/toolinvocations`
- Affected function/class: `persistToolInvocationLedger`
- Reason for change: Best-effort ledger insert can lose forensic records.
- Before behavior: Insert failure logs warning and continues.
- After behavior: Insert failure writes to durable outbox or marks turn as observability-degraded.
- Suggested implementation approach:
  - Add `tool_invocation_outbox` table or local append-only file.
  - On ledger insert failure, enqueue event with retry metadata.
  - Background worker drains outbox.
  - Export metrics for queued, failed, drained.
- Tests required before merging:
  - Inject insert failure and verify outbox row.
  - Restore DB and verify drain.
  - Health endpoint reports degraded while queue is non-empty beyond threshold.
- Rollback considerations:
  - Keep warning-only path behind config if outbox migration is unavailable.

## Patch 9: Harden reasoning trace for production

- Affected file: `internal/reasoningtrace/reasoningtrace.go`, config boot code
- Affected function/class: `Enabled`, `FullEnabled`, `Record`
- Reason for change: Full trace can persist sensitive prompt/history data.
- Before behavior: `AURA_REASONING_TRACE=full` writes capped plaintext private fields.
- After behavior: Production mode rejects full trace unless break-glass is enabled, and trace destination has retention controls.
- Suggested implementation approach:
  - Add `AURA_ENV=production` guard.
  - Require `AURA_REASONING_TRACE_ALLOW_FULL=1` for full mode in production.
  - Emit loud startup warning and metric.
  - Add optional encrypted trace writer.
- Tests required before merging:
  - Production full mode rejected without break-glass.
  - Default mode redacts private fields.
  - Env secret redaction still applies.
- Rollback considerations:
  - Operators can temporarily use break-glass with documented retention process.

## Patch 10: Add atomic/encrypted sidecar writer

- Affected file: `internal/conversations/store_helpers.go`, `internal/agent/tools/result.go`
- Affected function/class: `writeTurnSidecar`, `writeSidecar`
- Reason for change: Sidecars can contain sensitive data and direct writes are not crash-hardened.
- Before behavior: Plaintext `os.WriteFile` to local run directory.
- After behavior: Atomic write helper with optional encryption and retention metadata.
- Suggested implementation approach:
  - Create `internal/sidecar.Store`.
  - Implement `Write(ctx, key, bytes, options)` with atomic local backend.
  - Add encryption interface for production.
  - Store sidecar metadata for sweeper.
- Tests required before merging:
  - Atomic failure test.
  - Permission test.
  - Encryption round-trip test.
  - Retention sweeper test.
- Rollback considerations:
  - Keep legacy local writer behind config until migration is complete.
