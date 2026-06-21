# Proposed Patches

These are code-level recommendations only. No production source code was modified by this audit.

## Patch 1: Preserve default destructive shell guardrails

- Affected file: `.env.example`
- Affected function/class: N/A
- Reason for change: The sample env sets `AURA_SHELL_DESTRUCTIVE_PATTERNS=` and disables default destructive command approval when copied.
- Before behavior: Unset env uses defaults; empty env disables defaults.
- After behavior: Copied sample env keeps defaults unless the operator explicitly sets `off`.
- Suggested implementation approach:
  - Comment out `AURA_SHELL_DESTRUCTIVE_PATTERNS` in `.env.example`.
  - Optionally change `DestructivePatternsFromEnv` so empty means defaults and only `off` disables.
- Tests required before merging:
  - Unset uses defaults.
  - Empty copied value uses defaults.
  - `off` disables only when explicitly set.
  - Custom pattern list overrides defaults.
- Rollback considerations:
  - Operators who relied on empty string to disable must migrate to `off`.

## Patch 2: Reject terminal response with mutating siblings

- Affected file: `internal/agent/llm_agent_dispatch.go`
- Affected function/class: `splitTerminalCall`, `runRunnableBatch` dispatch path
- Reason for change: Mutating sibling tools execute before terminal text, weakening determinism and safety.
- Before behavior: Runnable siblings execute, then the terminal `text_response` executes.
- After behavior: A model step containing `text_response` and mutating/deferred siblings is rejected before side effects. Read-only siblings may be rejected too for stricter determinism.
- Suggested implementation approach:
  - During split, inspect siblings after the first `text_response`.
  - If any sibling tool descriptor is mutating or deferred, return a normalized model error asking for either tool execution or final answer, not both.
  - Prefer making all terminal siblings invalid unless a clear read-only exception is required.
- Tests required before merging:
  - `text_response` + `shell_exec` does not execute shell.
  - `text_response` + `fs_write` does not write.
  - Multiple terminal calls produce deterministic rejection.
  - Existing plain terminal response still succeeds.
- Rollback considerations:
  - Some prompts may need adjustment if they relied on final text plus tool output in one step.

## Patch 3: Make batch resume atomic-first

- Affected file: `internal/runner/runner_resume.go`
- Affected function/class: `SubmitAnswers`
- Reason for change: Batch resume appends answers before `MarkResumedBatch`, unlike the safer single-answer path.
- Before behavior: Answers can be injected before pause claim failure is discovered.
- After behavior: Batch pauses are claimed before answer injection, or claim and append happen in one transaction.
- Suggested implementation approach:
  - Resolve and validate pending pause IDs.
  - Call `MarkResumedBatch` before appending answer tool turns.
  - If append fails after successful claim, persist a recoverable resume-injection status or transactionally couple both operations.
  - Longer term: move claim and append into one store-level transaction.
- Tests required before merging:
  - Concurrent duplicate batch resume appends one answer per pause.
  - `MarkResumedBatch` failure appends no answers.
  - Append failure after claim is observable and recoverable.
- Rollback considerations:
  - If transaction coupling is too large, first ship ordering fix with explicit recovery logging.

## Patch 4: Validate conversation sidecar reads

- Affected file: `internal/conversations/store.go`, `internal/conversations/store_branch.go`, `internal/conversations/store_helpers.go`
- Affected function/class: `loadTurns`, branch loading, sidecar helper functions
- Reason for change: Read path trusts DB-stored `content_sidecar_path`.
- Before behavior: Any path in the DB column is read directly.
- After behavior: Sidecar path is reconstructed or validated under the expected conversation sidecar root.
- Suggested implementation approach:
  - Store sidecar key/sequence instead of raw absolute path where possible.
  - Add helper `resolveTurnSidecarForRead(conversationID, seq, storedPath)` that:
    - requires validated conversation ID
    - computes expected directory
    - rejects paths outside directory after `filepath.Abs` and `EvalSymlinks` where appropriate
    - optionally ignores stored path and derives path from sequence
  - Use helper in all load paths.
- Tests required before merging:
  - Valid sidecar loads.
  - Outside absolute path rejected.
  - `..` traversal rejected.
  - Symlink escape rejected if symlinks are possible.
  - Missing sidecar returns clear error.
- Rollback considerations:
  - Existing rows with legacy absolute paths may need a migration or compatibility validator.

## Patch 5: Default configured command hooks to fail-closed

- Affected file: `internal/agent/hooks_command.go`, `internal/agent/hooks.go`
- Affected function/class: `commandHookFailPolicy`, `CommandHookManagerFromEnv`, `hookFault`
- Reason for change: Security hooks should not allow execution on infrastructure failure by default.
- Before behavior: Omitted fail policy defaults to fail-open.
- After behavior: Configured hooks default to fail-closed in production, or startup fails unless fail policy is explicit.
- Suggested implementation approach:
  - Add runtime profile awareness to hook manager.
  - In production profile, omitted policy returns fail-closed or validation error.
  - Keep explicit `fail_open` for local/dev enrichment use if needed.
- Tests required before merging:
  - Omitted policy in production fails closed or fails config validation.
  - Omitted policy in dev follows documented behavior.
  - Timeout/crash/nonzero hook behavior covered.
- Rollback considerations:
  - Operators with flaky hooks may see commands denied until hook reliability is fixed.

## Patch 6: Reject default object-store credentials outside development

- Affected file: `internal/config/config.go`, `.env.example`, `compose.yaml`, `scripts/garage_bootstrap.sh`
- Affected function/class: config defaults and validation
- Reason for change: Static development credentials are accepted as runtime defaults.
- Before behavior: Garage/S3 keys can remain predictable.
- After behavior: Production profile fails fast on defaults or missing secrets.
- Suggested implementation approach:
  - Add `RuntimeProfile` to config.
  - Define known default access key, secret key, bucket, and Garage RPC secret constants.
  - In `Validate`, reject defaults for `server_production` and `single_user_hardened`.
  - Generate local secrets in bootstrap scripts if absent.
- Tests required before merging:
  - Dev allows generated/default local values.
  - Production rejects known defaults and missing secrets.
  - Production accepts non-default supplied values.
- Rollback considerations:
  - Existing local setups may need migration docs.

## Patch 7: Make AG-UI listener failure visible to orchestration

- Affected file: `cmd/aura/serve.go`, `compose.yaml`, readiness implementation under `internal/agui`
- Affected function/class: server startup goroutine and healthcheck config
- Reason for change: Listener failure can be logged while daemon/container remains healthy.
- Before behavior: `aura version` healthcheck can pass even when HTTP API is unavailable.
- After behavior: Listener failure exits the serve process or makes readiness false; healthcheck queries `/readyz`.
- Suggested implementation approach:
  - Send `ListenAndServe` errors to a fatal error channel watched by the parent serve context.
  - Mark readiness false before shutdown.
  - Change Compose healthcheck to call local `/readyz`.
- Tests required before merging:
  - Port conflict causes serve command failure.
  - `/readyz` false when listener or dependency unavailable.
  - Compose healthcheck command works in container image.
- Rollback considerations:
  - Faster failure may restart containers that previously stayed alive; update runbooks.

## Patch 8: Implement or remove outside-workspace send-file approval

- Affected file: `internal/agent/tools/send_file.go`, `cmd/aura/serve_adapters.go`, `internal/runner/runner_resume.go`
- Affected function/class: send-file resume context and resume hooks
- Reason for change: The tool advertises `send_file_outside_workspace` approval, but resume hooks do not handle it.
- Before behavior: Approval flow is incomplete.
- After behavior: A scoped approval authorizes exactly one path/session/expiry, or the tool reports unsupported behavior.
- Suggested implementation approach:
  - Add `newSendFileApprovalResumeHook`.
  - Validate path, session, actor, expiry, and one-time token.
  - Store approval state in the same persistence layer used by pause/resume.
  - If not implementing now, remove the resume-context instruction and return a deterministic unsupported error.
- Tests required before merging:
  - Approve outside-workspace file succeeds once.
  - Reuse, expiry, path mismatch, and actor mismatch fail.
  - Rejection returns normalized result.
- Rollback considerations:
  - If approval introduces risk, keep feature disabled behind profile flag.

## Patch 9: Use atomic writes in `fs_write`

- Affected file: `internal/agent/tools/fs_write.go`
- Affected function/class: write tool execution
- Reason for change: Direct writes can leave partial files.
- Before behavior: `os.WriteFile` writes directly to target.
- After behavior: Write uses temp file, fsync where supported, and rename through existing atomic helper.
- Suggested implementation approach:
  - Replace direct `os.WriteFile` with `atomicWriteFile`.
  - Preserve permission semantics.
  - Consider fsync of parent directory on platforms where useful.
- Tests required before merging:
  - Create new file.
  - Overwrite existing file.
  - Simulated temp failure leaves target unchanged.
  - Permission behavior is expected.
- Rollback considerations:
  - Atomic rename semantics can differ on network filesystems; document limitations.

## Patch 10: Require explicit remote MCP trust

- Affected file: `internal/mcp/manager/runtime.go`, `internal/mcp/managed_config.go`
- Affected function/class: `normalizedTrustForServer`, `NormalizedTrust`
- Reason for change: Empty trust for Streamable HTTP becomes runnable `remote_http`.
- Before behavior: Hand-authored/imported remote entries can be runnable without explicit trust.
- After behavior: Empty trust means blocked unless an approval flow explicitly sets trust.
- Suggested implementation approach:
  - Change normalization for remote transports so empty maps to `TrustBlocked`.
  - Add migration or warning for existing configs.
  - Require `TrustRemoteHTTP` from explicit approval.
- Tests required before merging:
  - Empty remote trust blocked.
  - Explicit remote trust runnable.
  - Local recipe trust behavior unchanged.
  - Legacy config behavior documented and gated.
- Rollback considerations:
  - Existing remote MCP configs may need one-time trust approval.

## Patch 11: Make mutating tool ledger mandatory in production

- Affected file: `internal/runner/runner_persist.go`, `internal/toolinvocations/store.go`, tool gateway future code
- Affected function/class: tool invocation persistence
- Reason for change: Mutating side effects can occur without durable audit records.
- Before behavior: Ledger failure logs and execution continues.
- After behavior: Production profile blocks mutating tool execution if ledger reservation fails.
- Suggested implementation approach:
  - Add pre-execution reservation state.
  - Add finalization states: succeeded, failed, canceled, denied.
  - Only allow best-effort ledger for read-only tools or dev profile.
- Tests required before merging:
  - Mutating tool blocked when ledger unavailable.
  - Read-only tool follows configured degradation policy.
  - Ledger redaction/capping still applies.
- Rollback considerations:
  - Ledger outages will become user-visible production failures by design.

## Patch 12: Align CI package discovery with local filtering

- Affected file: `.github/workflows/ci.yml`
- Affected function/class: Go build/test/vulnerability steps
- Reason for change: CI uses raw `./...` while local Makefile uses `scripts/go_packages.sh`.
- Before behavior: Go package discovery can differ between local and CI.
- After behavior: All Go commands use the same filtered package list.
- Suggested implementation approach:
  - Add a CI step that computes `GO_PACKAGES="$(bash scripts/go_packages.sh)"`.
  - Replace raw `go test ./...`, `go build ./...`, and `govulncheck ./...`.
  - Add a lint check rejecting raw `./...` in CI for Go commands.
- Tests required before merging:
  - CI passes with `web/node_modules` present.
  - Lint catches new raw `./...` usage.
- Rollback considerations:
  - If CI environment lacks bash on Windows, use PowerShell equivalent or run the package filter through Go.

## Patch 13: Canonicalize MCP transport classification

- Affected file: `internal/mcp/managed_config.go`, `internal/mcp/manager/runtime.go`, `internal/agent/mcptools/mount.go`, `internal/mcp/transport.go`
- Affected function/class: `normalizedServerType`, `normalizedTrustForServer`, `MountManagedServer`, `OpenServer`
- Reason for change: Mixed `url` + `command` entries can receive remote HTTP trust treatment while still opening as stdio command execution.
- Before behavior: Different code paths infer server type/trust from slightly different predicates.
- After behavior: One canonical classifier decides type, trust eligibility, runtime launch, mount, and open behavior.
- Suggested implementation approach:
  - Introduce a single exported `ClassifyManagedServer(server)` helper returning type/runtime/errors.
  - Reject `url` + `command` unless explicit type resolves the conflict safely.
  - Require trust class compatibility with classified transport.
  - Use the helper in validation, runnable filtering, runtime launch, mount, and transport open.
- Tests required before merging:
  - URL+command empty-trust entry is blocked and never launches stdio.
  - URL-only empty trust is blocked unless explicitly approved.
  - Explicit HTTP with command present fails validation.
  - Explicit stdio with URL present fails validation.
- Rollback considerations:
  - Existing ambiguous configs may need migration warnings and one-time cleanup.

## Patch 14: Scope AG-UI data by authenticated identity

- Affected file: `internal/agui/conversations_api.go`, `internal/agui/approvals_api.go`, `internal/runner/runner_conversation.go`, conversation/approval store interfaces and queries
- Affected function/class: conversation list/get/create/archive/delete handlers, approval list/resolve handlers, `NewConversation`
- Reason for change: Provisioned users can access global conversation/approval data, and new Web conversations are created under `local`.
- Before behavior: APIs call global list/read/mutation methods and `NewConversation` resolves the `local` identity.
- After behavior: APIs filter and authorize by authenticated principal; Web-created conversations use the current identity.
- Suggested implementation approach:
  - Add `ListForIdentity`, `GetForIdentity`, `ArchiveForIdentity`, `DeleteForIdentity`, and approval list/resolve variants.
  - Update AG-UI handlers to use `identityctx.IdentityID(ctx)` and return 404/403 on cross-principal access.
  - Preserve `local` fallback only for CLI/no-principal contexts.
- Tests required before merging:
  - Two identities cannot list/get/delete/archive each other's conversations.
  - Approval list/resolve is principal-scoped.
  - New Web conversation is owned by the authenticated identity and can run.
- Rollback considerations:
  - Existing local conversations may need a migration or compatibility view for single-user mode.

## Patch 15: Make resume claim and answer append transactional

- Affected file: `internal/runner/runner_resume.go`, `internal/askuser/store.go`, conversation store interfaces
- Affected function/class: `SubmitAnswer`, `SubmitAnswers`, `MarkResumed`, `MarkResumedBatch`, `AppendTurn`
- Reason for change: Single and batch resume paths can leave claimed-without-answer or duplicate/orphan answer states.
- Before behavior: Pause claim and answer append are separate operations, with batch append currently happening before batch claim.
- After behavior: Claim and append are one idempotent transaction or a recoverable state machine.
- Suggested implementation approach:
  - Add `ResolvePauseWithAnswer` and `ResolvePausesWithAnswers` store methods sharing a transaction.
  - Use token/tool_call_id idempotency keys.
  - If cross-store transaction is not immediately possible, write a resume-injection ledger and repair incomplete records on startup.
- Tests required before merging:
  - Single append failure leaves retryable or repairable state.
  - Concurrent batch duplicate appends exactly one answer per pause.
  - Mark failure appends no answer turns.
- Rollback considerations:
  - Data migration may be required if a new resume-injection ledger table is added.

## Patch 16: Persist pause tool-call turns before exposing pending pauses

- Affected file: `internal/runner/runner.go`, `internal/runner/runner_persist.go`
- Affected function/class: `flushOnce`, `persistPause`, `flushPause`
- Reason for change: Deferred pause flush failure is logged after a pause row is visible, leaving resume history malformed.
- Before behavior: Pause rows are inserted as events arrive, and the combined assistant tool-call turn is flushed at round end or deferred on early return.
- After behavior: Pending pauses are exposed only after the matching assistant tool-call turn is durable.
- Suggested implementation approach:
  - Persist the assistant ask-user tool-call turn and pause rows in one transaction.
  - Alternatively stage pause rows as invisible until `flushPause` succeeds, then atomically mark visible.
- Tests required before merging:
  - Consumer stops on pause and assistant-turn append fails; no visible pending pause remains.
  - Multi-pause assistant turn remains wire-valid.
- Rollback considerations:
  - Must preserve the existing multi-ask-user single assistant turn contract.

## Patch 17: Preserve mutating metadata on tool panic

- Affected file: `internal/agent/llm_agent_parallel.go`, possibly tool registry accessors
- Affected function/class: `runToolRecovering`
- Reason for change: A mutating tool that panics after a side effect can return a non-mutating recovered result.
- Before behavior: Panic recovery constructs a result without preserving the tool descriptor's mutating flag.
- After behavior: Panic recovery result carries the mutating classification resolved before execution.
- Suggested implementation approach:
  - Resolve tool spec before execution in `runToolRecovering`.
  - Copy `Spec().Mutating` into normal and panic paths.
  - If spec lookup fails, fail closed for tools known to be high-risk or mark unknown as mutating in production profile.
- Tests required before merging:
  - Mutating fake tool writes then panics; completion gate is armed.
  - Non-mutating panic does not falsely arm side-effect gate unless fail-closed policy says so.
- Rollback considerations:
  - Completion gate may run more often after panic; this is expected safer behavior.

## Patch 18: Scope background shell IDs and lifecycle

- Affected file: `internal/agent/tools/shell_bg.go`, tool call context plumbing
- Affected function/class: `BackgroundShells.start`, `ShellPoll.Execute`, `ShellKill.Execute`, `Evict`
- Reason for change: Background shell IDs are predictable, process-scoped, and not owner-bound.
- Before behavior: IDs are sequential and poll/kill only require the ID.
- After behavior: IDs are random, jobs store session/actor ownership, and poll/kill enforce ownership plus TTL.
- Suggested implementation approach:
  - Generate cryptographically random shell IDs.
  - Thread session/actor metadata into background start/poll/kill.
  - Add default TTL, idle timeout, and session-end behavior.
  - Keep admin inspection behind an explicit capability.
- Tests required before merging:
  - Session B cannot poll/kill Session A's shell.
  - TTL expires and kills process group.
  - Session eviction handles running and finished jobs according to policy.
- Rollback considerations:
  - Existing `sh_N` IDs from live jobs cannot be recovered after migration; document restart behavior.

## Patch 19: Harden MCP transport lifecycle

- Affected file: `internal/agent/mcptools/mount.go`, `internal/mcp/client.go`, `internal/mcp/http_client.go`
- Affected function/class: mount/open/list-tools flow, stdio frame reader, `Close`
- Reason for change: MCP startup can hang, stdio frames can be huge, and close can block or leak children.
- Before behavior: Limits exist for calls and schemas, but mount/frame/close lifecycle is incomplete.
- After behavior: Per-server mount timeout, max stdio frame size, bounded HTTP close, and process-tree termination are enforced.
- Suggested implementation approach:
  - Add `AURA_MCP_MOUNT_TIMEOUT_SEC` and `AURA_MCP_STDIO_MAX_FRAME_BYTES`.
  - Wrap mount/open/list-tools in bounded context.
  - Use limited reader/scanner for stdio lines.
  - Use process groups/job objects for child cleanup.
  - Bound HTTP close with a short context.
- Tests required before merging:
  - Hung MCP initialize returns within timeout.
  - Oversized frame aborts transport without large allocation.
  - Blocking HTTP close returns within timeout.
  - Child process is killed when parent MCP closes.
- Rollback considerations:
  - Some slow MCP servers may need a documented timeout override.

## Patch 20: Enforce MCP governance trust request bodies

- Affected file: `internal/agui/governance_write_api.go`, `cmd/aura/serve_governance_write.go`
- Affected function/class: `handleMCPTrust`, `TrustApprove`, MCP body decoder
- Reason for change: Empty trust body defaults to `trusted_local` and can trust a blocked custom server with weak audit context.
- Before behavior: Blank class becomes `trusted_local`; reason can be empty.
- After behavior: Trust requires explicit known class and non-empty reason.
- Suggested implementation approach:
  - Make trust endpoint body mandatory.
  - Reject blank class/reason and unknown class.
  - Validate transport/class compatibility.
  - Use strict JSON decoding for privileged routes.
- Tests required before merging:
  - Empty body, `{}`, blank reason, trailing JSON, and unknown class fail without config/audit changes.
  - Valid class+reason writes config and audit row.
- Rollback considerations:
  - Existing UI/client calls that relied on empty trust body must be updated.

## Patch 21: Normalize run directory and clean unreferenced sidecars

- Affected file: `internal/config/config.go`, `internal/conversations/orphan_scan.go`, `internal/conversations/sweeper.go`, sidecar helpers
- Affected function/class: config load/validation, sidecar spill/sweep
- Reason for change: Relative run dirs can break sidecar reads; crash-created sidecars inside live dirs are not reclaimed.
- Before behavior: Relative paths can be accepted by writers, and live conversation dirs are preserved wholesale.
- After behavior: RunDir is absolute and sidecar sweeps reconcile committed DB references.
- Suggested implementation approach:
  - Normalize or reject non-absolute `AURA_RUN_DIR` at config load.
  - Write sidecars through temp names and promote only after DB commit where feasible.
  - Add a sweeper pass for unreferenced `.content` files inside live conversation dirs after grace period.
- Tests required before merging:
  - Relative run dir fails validation or round-trips after normalization.
  - Unreferenced live-dir sidecar is reported/removed after grace.
  - Referenced sidecar is preserved.
- Rollback considerations:
  - Sweeper should initially support dry-run/report-only mode.

## Patch 22: Scope conversation deletion through runner lifecycle

- Affected file: `internal/agui/conversations_api.go`, `internal/channels/telegram/commands.go`, `cmd/aura/chat.go`, runner lifecycle interfaces
- Affected function/class: delete/clear handlers, `Runner.Stop`, session evictors
- Reason for change: Direct persistence deletion can leave in-memory tool state alive for reused conversation IDs.
- Before behavior: Some handlers delete rows directly.
- After behavior: Deletion routes through a runner-owned lifecycle operation that cancels work and evicts session state before persistence delete.
- Suggested implementation approach:
  - Add `Runner.DeleteConversation(ctx, convID)` or `ConversationLifecycle` service.
  - Call registered session evictors for todos, shell cwd, approvals, background shells, and future stateful tools.
  - Decide policy for running background jobs on delete: kill, detach with admin-only access, or deny delete until stopped.
- Tests required before merging:
  - AG-UI delete and Telegram clear invoke evictors.
  - Reused deterministic conversation ID starts with clean tool state.
- Rollback considerations:
  - Deletion may become slower if it waits for cancellation; add bounded timeout.

## Patch 23: Separate scheduler admission stop from in-flight job drain

- Affected file: `cmd/aura/serve.go`, `internal/cron/scheduler.go`, `internal/cron/dispatch.go`, `deploy/aura-scheduler.service`
- Affected function/class: signal handling, scheduler stop, handler dispatch, systemd stop timeout
- Reason for change: SIGTERM can cancel in-flight jobs immediately, and backup duration can exceed service stop budget.
- Before behavior: Handler contexts can be canceled with the root serve context; systemd timeout may be shorter than backup max.
- After behavior: Shutdown stops new admissions, allows in-flight jobs to drain until deadline, then cancels; deployment stop timeout matches handler max duration.
- Suggested implementation approach:
  - Split admission context from job execution context.
  - Add explicit drain deadline and status metrics.
  - Align `TimeoutStopSec` with backup max plus grace, or reduce backup max and atomically promote backup artifacts.
- Tests required before merging:
  - Fake long handler survives until drain deadline after SIGTERM.
  - New work is not admitted during drain.
  - Static check verifies service stop timeout exceeds longest handler duration.
- Rollback considerations:
  - Longer service stop may slow deploys; document operational expectations.
