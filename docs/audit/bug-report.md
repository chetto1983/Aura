# Bug Report And Correctness Findings

## [P1] F-001: Native shell and filesystem tools expose full container/runtime access

- Evidence:
  - File: `internal/agent/tools/shell_exec.go`
  - Location: `ShellExec`, lines 20-23
  - Relevant component: host command execution
  - File: `internal/agent/tools/fs.go`
  - Location: package comment and `resolveFSPath`, lines 14-29
  - File: `cmd/aura/main.go`
  - Location: registry wiring, lines 150-163
- Problem:
  - The shell tool is explicitly a full terminal with no sandbox hop, and native filesystem tools have no general path fence. The composition root registers `FSRead`, `FSWrite`, `FSEdit`, `FSGrep`, and `FSGlob` without a workspace root. Because Aura runs in a container, this means full access to the container filesystem/runtime and any mounted resources.
- Impact:
  - Prompt injection, model error, compromised MCP output, or remote-channel abuse can read/write arbitrary container-visible files or execute arbitrary commands with Aura's process privileges. If the container has sensitive mounts, injected secrets, host networking, privileged mode, or Docker socket access, the blast radius can extend beyond the container.
- Reproduction or failure scenario:
  - A malicious web page instructs the model to call `fs_read` on a mounted secret path or `shell_exec` to exfiltrate files. Untrusted output framing helps, but the model can still decide to execute the tool.
- Recommended fix:
  - Introduce a central execution policy and container-aware sandbox. Default server/remote sessions to read-only or workspace-only access. Require explicit human approval and elevated profile for full-runtime commands.
- Suggested test coverage:
  - Unit tests for denied absolute paths outside workspace.
  - Integration tests proving remote identities cannot obtain `shell_exec` or unrestricted FS tools.
  - Policy regression tests for destructive and exfiltration command patterns.

## [P1] F-002: Model-authored skill create/update can auto-activate

- Evidence:
  - File: `internal/skilladapters/skilladapters.go`
  - Location: `modelActor` and `Writer.WriteMutation`, lines 23-25 and 102-103
  - File: `internal/skills/writer.go`
  - Location: `modelMutationBypassesGate`, lines 100 and 163-166
  - File: `internal/skills/writer.go`
  - Location: auto-activation path, lines 136-142
  - File: `internal/agent/tools/skill_write.go`
  - Location: active status branch, lines 159-170
- Problem:
  - For model-authored create/update where `always=false`, `modelMutationBypassesGate` disables the gate and `WriteMutation` calls `Activate(... ApprovalAuto ...)`.
- Impact:
  - The model can modify its own future capabilities without a human approval step. This is a production blocker for shared, remote, or sensitive deployments.
- Reproduction or failure scenario:
  - The model calls `skill` with `action=create`, `always=false`, and a malicious or unsafe instruction body. If validation passes, the skill is materialized as active.
- Recommended fix:
  - Gate all model-authored skill create/update/delete by default. Allow auto-activation only under an explicit local disposable-sandbox profile.
- Suggested test coverage:
  - Regression test that model actor returns `pending_approval` under production profile.
  - Capability-profile tests for local sandbox override.
  - End-to-end test that a pending skill cannot affect the next turn until approved.

## [P1] F-003: `FSWrite` overwrites files non-atomically

- Evidence:
  - File: `internal/agent/tools/fs_write.go`
  - Location: `FSWrite.Execute`, line 61
  - File: `internal/agent/tools/fs.go`
  - Location: `atomicWriteFile`, lines 111-145
  - File: `internal/agent/tools/fs_edit.go`
  - Location: `FSEdit.Execute`, line 85
- Problem:
  - `FSWrite` uses `os.WriteFile` directly, while the package already has an atomic temp-file-plus-rename helper used by `FSEdit`.
- Impact:
  - A crash or power loss during overwrite can truncate or partially write a production file.
- Reproduction or failure scenario:
  - The agent overwrites a config, source, or data file and the process crashes after truncate but before complete write.
- Recommended fix:
  - Use `atomicWriteFile` for `FSWrite`; preserve existing file mode when overwriting; optionally fsync the file and parent directory.
- Suggested test coverage:
  - Unit test with injected write failure proving the original file remains intact.
  - Windows-specific test for replace behavior.

## [P1] F-004: Background shell jobs are detached and lack TTL or ownership enforcement

- Evidence:
  - File: `internal/agent/tools/shell_bg.go`
  - Location: `Start`, line 148
  - File: `internal/agent/tools/shell_bg.go`
  - Location: eviction only removes finished jobs, lines 212-225
  - File: `internal/agent/tools/shell_bg.go`
  - Location: max running cap, lines 433-455
- Problem:
  - Background shells use `context.WithCancel(context.Background())`, so they are detached from the turn context. There is a running-count cap, but no TTL, max runtime, per-session owner check, or automatic cancellation when a conversation ends.
- Impact:
  - Jobs can outlive the request, continue consuming CPU/network/filesystem access, and be polled or killed by any agent path that knows the shell id inside the same process.
- Reproduction or failure scenario:
  - A model starts a background shell with a long-running command. The turn is canceled or the session disappears; the command remains alive until explicit kill or process shutdown.
- Recommended fix:
  - Add owner identity/session to each job, enforce poll/kill authorization, add TTL/max runtime, and attach jobs to a supervised process manager.
- Suggested test coverage:
  - Cancellation and TTL tests.
  - Cross-session poll/kill denial tests.
  - Shutdown tests proving process-tree cleanup.

## [P1] F-005: Agent deadline enforcement depends on callers and per-node timeout defaults to disabled

- Evidence:
  - File: `internal/agent/llm_agent.go`
  - Location: budget consumption, line 251; model timeout, line 263; node timeout use, lines 568-572
  - File: `internal/agent/budget.go`
  - Location: `AURA_LOOP_NODE_TIMEOUT_SEC` default, line 144; `WithDeadline`, lines 357-359; `NodeTimeout`, line 363
  - File: `internal/runner/runner.go`
  - Location: production `buildAgent`, lines 561-585
- Problem:
  - Production `Runner` correctly calls `Budget.WithDeadline`, but `LlmAgent.Run` itself does not enforce this contract or validate a non-nil budget. Tool node timeouts are disabled by default.
- Impact:
  - Direct package consumers can create unbounded in-flight tool calls or nil-budget panics. A hanging tool can block `executeBatch` until the parent context is canceled.
- Reproduction or failure scenario:
  - Construct `InvocationContext{Ctx: context.Background(), Budget: budgetWithNoNodeTimeout}` and call a blocking tool; the wallclock budget prevents new steps only after the blocking tool returns unless the caller derived a deadline context.
- Recommended fix:
  - Validate `InvocationContext` at `Run` entry. If no context deadline exists, derive one from `Budget.WithDeadline`. Set a conservative default node timeout or require every mutating/external tool to declare one.
- Suggested test coverage:
  - Test that `Run` rejects nil budget.
  - Test that a blocking fake tool is canceled by budget deadline without caller help.

## [P1] F-006: `text_response` does not fence off sibling side effects

- Evidence:
  - File: `internal/agent/llm_agent_dispatch.go`
  - Location: partition logic, lines 16-27; runnable execution, lines 65-112; terminal execution, lines 115-117
- Problem:
  - If a model emits `text_response` and other tool calls in the same assistant message, Aura executes non-terminal runnable tools first and finalizes afterward.
- Impact:
  - A response that appears terminal can still carry side effects. This complicates approval semantics and makes final-turn behavior less deterministic.
- Reproduction or failure scenario:
  - The model returns `fs_write` plus `text_response` in one batch. Aura writes the file, then emits the final response.
- Recommended fix:
  - Enforce terminal exclusivity. If `text_response` is present with other calls, either reject the whole batch with model feedback or run only `text_response` when all siblings are known safe/non-mutating.
- Suggested test coverage:
  - Unit tests for mixed terminal plus mutating tool.
  - Provider-wire regression test for final-only turn.

## [P2] F-007: Memory recall and write behavior is prompt-directed, not deterministic

- Evidence:
  - File: `internal/agent/prompt.go`
  - Location: memory guidance, lines 61-67
  - File: `internal/agent/memory_recall_integration_test.go`
  - Location: build-tagged fake-client script, comments around lines 8-9 and scripted calls around lines 112-115
- Problem:
  - The runtime asks the model to recall or write memory rather than enforcing a policy-driven memory middleware.
- Impact:
  - Important facts may be missed; stale or irrelevant memory may be used; memory behavior depends on model compliance.
- Reproduction or failure scenario:
  - A turn asks about a user preference that exists in memory, but the model does not call the memory search tool.
- Recommended fix:
  - Add deterministic recall prepass and write classifier/postpass with explicit provenance.
- Suggested test coverage:
  - Tests where the model omits memory search but middleware injects relevant memory.
  - Tests rejecting low-confidence or unsafe memory writes.

## [P2] F-008: MCP `ReadOnlyHint` controls mutability classification

- Evidence:
  - File: `internal/agent/mcptools/bridge.go`
  - Location: mutating flag assignment, lines 65 and 164
- Problem:
  - Aura trusts MCP server metadata: `Mutating` is set to the inverse of `ReadOnlyHint`.
- Impact:
  - A buggy or malicious MCP server can label a side-effecting tool read-only, affecting retries, completion gates, and risk classification.
- Reproduction or failure scenario:
  - External MCP tool deletes or sends data while advertising `ReadOnlyHint=true`; Aura treats it as non-mutating.
- Recommended fix:
  - Add local MCP policy manifests. Default unknown external MCP tools to mutating unless explicitly allowlisted.
- Suggested test coverage:
  - MCP bridge test proving local policy overrides server metadata.
  - Retry test proving misclassified mutating tools are not retried.

## [P2] F-009: Tool invocation ledger is best-effort and can lose forensic evidence

- Evidence:
  - File: `internal/runner/runner_persist.go`
  - Location: non-fatal ledger insert, lines 74-87
  - File: `internal/runner/runner_persist.go`
  - Location: nil ledger handling, lines 180-188
- Problem:
  - Conversation history persistence is fatal, but append-only tool invocation ledger writes are intentionally non-fatal.
- Impact:
  - During DB hiccups or pool exhaustion, production may lose the forensic record of executed tools.
- Reproduction or failure scenario:
  - Ledger insert fails while a dangerous tool succeeds; the user-facing turn continues and only history preview remains.
- Recommended fix:
  - Add a durable local outbox or mark the turn as observability-degraded. Export a health signal when ledger persistence fails.
- Suggested test coverage:
  - Failure-injection test that queues ledger events and later drains them.
  - Alerting test for ledger degradation.

## [P2] F-010: Full reasoning trace can persist sensitive history to disk

- Evidence:
  - File: `internal/agent/llm_agent.go`
  - Location: request trace includes `history`, lines 292-301
  - File: `internal/reasoningtrace/reasoningtrace.go`
  - Location: full mode warning, lines 29-49; redaction bypass for full mode, lines 165-174
- Problem:
  - Default trace mode summarizes private fields, but `AURA_REASONING_TRACE=full` writes capped plaintext prompt/history fields to a file.
- Impact:
  - Debug traces can contain PII, secrets typed by users, file contents, profile data, or tool output.
- Reproduction or failure scenario:
  - Operator enables full trace in production and the default temp path persists sensitive JSONL rows.
- Recommended fix:
  - Disable full trace in production builds unless an explicit break-glass flag is set. Add encryption, retention, and access controls for trace files.
- Suggested test coverage:
  - Test that production config rejects full trace.
  - Redaction test for private fields and environment secrets.

## [P2] F-011: Sidecar content is plaintext and written with direct `os.WriteFile`

- Evidence:
  - File: `internal/conversations/store_helpers.go`
  - Location: `writeTurnSidecar`, lines 127-133
  - File: `internal/agent/tools/result.go`
  - Location: sidecar write, line 212
- Problem:
  - Conversation/tool sidecars are direct plaintext filesystem writes. They are permissioned `0600`, but not encrypted, fsynced, or atomically replaced.
- Impact:
  - Sensitive large outputs can remain on disk; crash consistency and confidentiality depend on the host filesystem.
- Reproduction or failure scenario:
  - A tool reads a secret file, output spills to a sidecar, and the run directory is later backed up or exposed.
- Recommended fix:
  - Add atomic sidecar writer, optional encryption-at-rest, retention policy, and secure deletion for expired runs.
- Suggested test coverage:
  - Sidecar permission and atomicity tests.
  - Retention sweeper tests.

## [P2] F-012: Crash recovery after side-effecting tools is not a full idempotency boundary

- Evidence:
  - File: `internal/runner/runner_persist.go`
  - Location: tool start flush and tool result append, lines 95-125 and 161-175
  - File: `internal/conversations/store_helpers.go`
  - Location: repaired unknown tool result, lines 215-223 and 307
- Problem:
  - Aura repairs dangling assistant tool-call groups by synthesizing "previous result unknown" tool messages, but that is not equivalent to a durable tool-result transaction or idempotency guarantee.
- Impact:
  - A crash after a side-effecting tool executes but before its result is persisted leaves an unknown result. The model is warned, but external side effects may already have occurred.
- Reproduction or failure scenario:
  - Process crashes after a payment/send/write tool returns but before `ToolInvocationEnd` is persisted.
- Recommended fix:
  - Add tool idempotency keys, begin/commit records, and per-tool recovery handlers. Require mutating tools to be idempotent or compensatable.
- Suggested test coverage:
  - Crash-injection test between start, side effect, and result append.
  - Mutating tool replay prevention test.

## [P2] F-013: Tool registry mutability is not concurrency-safe

- Evidence:
  - File: `internal/agent/tools/spec.go`
  - Location: registry map and `Register`, lines 92-117
- Problem:
  - Registry mutation is assumed to happen at boot. The map is not protected by locks and `Register` panics on duplicate names.
- Impact:
  - Future dynamic tool loading or MCP refresh paths can race or panic if they share a registry instance.
- Reproduction or failure scenario:
  - A dynamic plugin refresh registers while a turn reads `Registry.All`.
- Recommended fix:
  - Make the registry immutable after build, or add copy-on-write snapshots with explicit refresh semantics.
- Suggested test coverage:
  - Race test with concurrent snapshot reads and registry refresh.

## [P2] F-014: Shell approvals are pattern-based, not a complete policy engine

- Evidence:
  - File: `internal/agent/tools/shell_exec_env.go`
  - Location: advisory destructive-command comments, lines 78-88 and 102-108
  - File: `internal/agent/tools/shell_approval.go`
  - Location: `requireShellApproval`, lines 127-143
- Problem:
  - Destructive shell approval is useful, but it is not a robust command policy. Patterns can be incomplete, disabled, or bypassed by indirection.
- Impact:
  - Sensitive commands may execute without approval if they are not matched.
- Reproduction or failure scenario:
  - A destructive action is wrapped in a script, interpreter command, or alias that avoids configured regexes.
- Recommended fix:
  - Add a command policy engine with parsed argv, prefix rules, workspace/network policy, and deny/prompt/allow decisions.
- Suggested test coverage:
  - Policy tests for shell indirection, interpreter wrappers, and environment-driven bypasses.

## [P2] F-015: Production dependency health is incomplete

- Evidence:
  - File: `internal/runner/runner.go`
  - Location: embedder boot probe is non-fatal around lines 235-247
  - File: `internal/agent/tracing.go`
  - Location: OTLP exporter setup/logging, lines 31-78
- Problem:
  - Some dependencies degrade softly, which is acceptable locally but insufficient for production readiness without explicit health and readiness signals.
- Impact:
  - Operators may run with degraded tool search, tracing, or background services without a clear readiness failure.
- Reproduction or failure scenario:
  - Embedder or OTLP collector is unavailable; the process continues and some capabilities degrade.
- Recommended fix:
  - Add health/readiness endpoints and a dependency matrix with required, optional, and degraded states.
- Suggested test coverage:
  - Health endpoint tests for DB, LLM, MCP, embedder, scheduler, trace exporter, and sidecar filesystem.

## [P3] F-016: Invalid `AURA_LOOP_MAX_PARALLEL_TOOLS` silently falls back

- Evidence:
  - File: `internal/agent/llm_agent_parallel.go`
  - Location: `maxParallelTools`, lines 89-99
- Problem:
  - Invalid or non-positive env values silently use the default instead of failing fast.
- Impact:
  - Operational misconfiguration can go unnoticed.
- Reproduction or failure scenario:
  - `AURA_LOOP_MAX_PARALLEL_TOOLS=not-a-number` starts successfully with default 4.
- Recommended fix:
  - Parse this setting with the same fail-fast discipline used by budget env knobs.
- Suggested test coverage:
  - Env parsing tests for invalid and non-positive values.

## [P3] F-017: Tool-output nonce generation panics on entropy failure

- Evidence:
  - File: `internal/agent/trust.go`
  - Location: `toolOutputNonce`, lines 60-66
- Problem:
  - `crypto/rand` failure panics. The loop recovers, but this turns telemetry/security wrapping failure into a turn-level panic path.
- Impact:
  - Low probability, but avoidable reliability degradation.
- Reproduction or failure scenario:
  - Entropy source fails in tests or constrained runtime; untrusted output wrapping panics.
- Recommended fix:
  - Return an error or use a safe fallback nonce with metric logging.
- Suggested test coverage:
  - Inject failing reader and assert non-panic behavior.

## [P3] F-018: Filesystem tool working-directory semantics are ambiguous

- Evidence:
  - File: `cmd/aura/main.go`
  - Location: FS tool registration, lines 160-164
  - File: `internal/agent/tools/fs.go`
  - Location: `resolveFSPath`, lines 20-29
- Problem:
  - FS tools registered with empty `WorkspaceRoot` resolve relative paths against the process working directory, while shell gets an explicit workspace root.
- Impact:
  - Behavior depends on process launch directory and can surprise operators or tests.
- Reproduction or failure scenario:
  - Start Aura from a different directory; relative `fs_read` and `fs_write` operate somewhere unexpected.
- Recommended fix:
  - Register FS tools with explicit workspace root or require absolute paths in full-runtime mode.
- Suggested test coverage:
  - Composition-root test for FS tool workspace root.

## [P3] F-019: Critical production test categories are missing

- Evidence:
  - File: `internal/runner/live_e2e_test.go`
  - Location: live tests are build-tagged and paid/DSN gated, lines 1-31
  - File: `internal/agent/memory_recall_integration_test.go`
  - Location: build-tagged integration, comments around lines 8-9
- Problem:
  - The unit suite is substantial, but production-only failure modes are not continuously exercised by default.
- Impact:
  - Regressions in crash recovery, remote policy, long-running shells, live provider behavior, and DB failover may escape normal CI.
- Reproduction or failure scenario:
  - A change breaks live pause/resume or crash replay, but default `go test ./...` remains green.
- Recommended fix:
  - Add CI lanes for integration, live smoke, race, fuzz, crash injection, and policy tests.
- Suggested test coverage:
  - See `testing-strategy.md`.

## [P3] F-020: Skill governance comments contradict live behavior

- Evidence:
  - File: `internal/skills/writer.go`
  - Location: top comment says mutations never self-activate, lines 14-21; later code bypasses gate, lines 100 and 136-142
  - File: `internal/agent/tools/skill_write.go`
  - Location: comments describe both gated and ungated flows, lines 48-64 and 165-170
- Problem:
  - Comments preserve older governance language while tests and code confirm in-box auto-activation.
- Impact:
  - Future maintainers may misunderstand the security boundary and make unsafe policy changes.
- Reproduction or failure scenario:
  - An engineer relies on the top-level "never self-activates" comment during a security review.
- Recommended fix:
  - Rewrite comments around the current policy and explicitly state the deployment modes where auto-activation is allowed.
- Suggested test coverage:
  - No runtime test required; add architecture decision record and policy tests for the intended gate matrix.
