# Bug Report And Correctness Findings

> **Historical source register.** Finding descriptions below preserve the
> audited 2026-06-21 state. Current one-by-one disposition and evidence live in
> [definitive-closure-ledger-2026-07-31.md](definitive-closure-ledger-2026-07-31.md).

## [P1] F-001 Full-host shell and filesystem tools lack an industrial capability boundary

- Evidence:
  - File: `internal/agent/tools/shell_exec.go`
  - Location: comments and spec around `Tool.Exec`
  - Relevant component: shell execution
- Problem:
  `shell_exec` is documented as running in the host shell with the operator's privileges and no sandbox. Filesystem tools also resolve absolute paths directly and explicitly state there is no general filesystem boundary.
- Impact:
  Prompt injection, model error, or compromised upstream context can cause host-wide reads, writes, deletes, process launches, network access, or credential exposure. This is acceptable only for trusted local-operator use, not industrial server production.
- Reproduction or failure scenario:
  A retrieved document instructs the model to run a destructive shell command or read a sensitive absolute path. The runtime has no central capability gate that rejects the action by actor, path, workspace, command class, or runtime profile.
- Recommended fix:
  Add a tool gateway with runtime profiles, workspace path fences, command capability classes, network egress policy, per-tool approval requirements, and optional container/process isolation. Refuse production server mode unless dangerous tools are sandboxed or disabled.
- Suggested test coverage:
  Unit tests for path policy, command class policy, runtime profile validation, and approval decisions. Integration tests showing prompt-injected shell/filesystem requests are denied in production mode.

## [P1] F-002 Sample environment disables destructive shell approval by using an empty override

- Evidence:
  - File: `.env.example`
  - Location: `AURA_SHELL_DESTRUCTIVE_PATTERNS=`
  - File: `internal/agent/tools/shell_exec_env.go`
  - Location: `DestructivePatternsFromEnv`
  - Relevant component: shell guardrails
- Problem:
  `shell_exec_env.go` treats an unset variable as "use default destructive patterns", but treats an empty string or `off` as "disable the gate". `.env.example` sets `AURA_SHELL_DESTRUCTIVE_PATTERNS=`. Copying the sample to `.env` disables the default approval gate.
- Impact:
  A normal setup step can silently weaken shell safety. Destructive commands that should require approval can run without the expected guardrail.
- Reproduction or failure scenario:
  Copy `.env.example` to `.env`, run Aura, and evaluate shell command policy. Because the variable is present and empty, the default deny/approval pattern list is not used.
- Recommended fix:
  Remove the variable assignment from `.env.example`, comment it out, or change parsing so an empty value means "use defaults". Add an explicit value such as `off` only for deliberate disablement.
- Suggested test coverage:
  Tests for unset, empty, whitespace, `off`, custom pattern, and copied-sample behavior. A config smoke test should prove defaults remain active after copying `.env.example`.

## [P1] F-003 Terminal `text_response` can execute after mutating sibling tools

- Evidence:
  - File: `internal/agent/llm_agent_dispatch.go`
  - Location: `splitTerminalCall`, `runRunnableBatch`, terminal execution path
  - File: `internal/agent/llm_agent.go`
  - Location: dispatch comment around sibling tools and terminal execution
  - Relevant component: agent loop dispatch
- Problem:
  The first `text_response` is treated as terminal, but sibling tool calls are still executable. Runnable sibling tools execute first; the terminal response executes afterward.
- Impact:
  The model can produce a final answer while also causing side effects. Completion-gate rejection can occur after the side effect already happened. This weakens determinism, auditability, and operator expectations.
- Reproduction or failure scenario:
  A model response includes `text_response("done")` plus `shell_exec` or `fs_write`. The shell/write runs before the terminal text response is handled.
- Recommended fix:
  Make terminal tools exclusive in a model step, or allow only read-only siblings. Reject or ask for a replan if `text_response` appears with any mutating/deferred tool.
- Suggested test coverage:
  Regression tests for `text_response` with mutating sibling, read-only sibling, multiple terminal calls, and completion-gate rejection after attempted side effect.

## [P1] F-004 Batch pause resume injects answers before atomically claiming pauses

- Evidence:
  - File: `internal/runner/runner_resume.go`
  - Location: `SubmitAnswer` and `SubmitAnswers`
  - File: `internal/askuser/store.go`
  - Location: `MarkResumedBatch`
  - Relevant component: human-in-the-loop resume
- Problem:
  `SubmitAnswer` marks a pause resumed before appending the tool answer, preventing duplicate injection. `SubmitAnswers` resolves pending pauses, appends answers, then calls `MarkResumedBatch`.
- Impact:
  If `MarkResumedBatch` fails after answer injection, or if duplicate batch resume races, the conversation can contain orphan or duplicate `RoleTool` answers for pauses that were not atomically claimed.
- Reproduction or failure scenario:
  Two clients submit the same batch or the database rejects one pause as already resumed. The batch path can append answers before the state transition fails.
- Recommended fix:
  Reorder `SubmitAnswers` to atomically mark all pauses resumed before injecting answers, or wrap pause claiming and conversation append in a single transaction with idempotency keys.
- Suggested test coverage:
  Batch duplicate tests mirroring the existing single-answer atomic test. Inject store failures after append and verify no orphan tool turns are persisted.

## [P1] F-005 Conversation sidecar loading trusts DB-stored paths

- Evidence:
  - File: `internal/conversations/store.go`
  - Location: `loadTurns`
  - File: `internal/conversations/store_branch.go`
  - Location: branch turn loading
  - File: `internal/conversations/store_helpers.go`
  - Location: `turnSidecarPath`, `validateID`
  - File: `internal/db/migrations/0005_conversations.up.sql`
  - Location: `content_sidecar_path text`
  - Relevant component: conversation persistence
- Problem:
  Writes construct sidecar paths from validated IDs, but reads call `os.ReadFile(t.ContentSidecarPath)` directly. The DB column stores a free text path without a constraint tying it to the conversation sidecar directory.
- Impact:
  A corrupted DB row, migration bug, or DB compromise can cause Aura to read arbitrary local files into conversation history and expose them to the model or user.
- Reproduction or failure scenario:
  Insert a turn row with `content_sidecar_path` pointing to a sensitive local file. Loading the conversation reads that path without reconstructing or fencing it.
- Recommended fix:
  Store only a sidecar key or sequence number, reconstruct the expected path from `runDir/conversations/<conversationID>/<seq>.content`, require absolute path containment, and reject symlinks if the sidecar directory is security-sensitive.
- Suggested test coverage:
  Tests for absolute path outside the sidecar root, relative path traversal, symlink sidecar, missing sidecar, and valid sidecar rehydration.

## [P1] F-006 Command hooks default to fail-open

- Evidence:
  - File: `internal/agent/hooks.go`
  - Location: `HookFailPolicy`, `hookFault`
  - File: `internal/agent/hooks_command.go`
  - Location: `CommandHookManagerFromEnv`, `commandHookFailPolicy`
  - Relevant component: command hook policy
- Problem:
  Hook execution supports fail-open and fail-closed modes, but environment-based command hooks default to fail-open unless `AURA_COMMAND_HOOK_FAIL_POLICY=fail_closed` is explicitly set.
- Impact:
  A hook intended as a security gate can allow execution if the hook crashes, times out, is misconfigured, or returns a transient infrastructure failure.
- Reproduction or failure scenario:
  Configure a command hook but omit fail policy. Make the hook executable sleep past timeout or fail to start. The runtime logs/records the hook fault and allows execution.
- Recommended fix:
  Default configured command hooks to fail-closed, or require an explicit fail policy when `AURA_COMMAND_HOOK` is set. At minimum, fail-open should be limited to non-security enrichment hooks.
- Suggested test coverage:
  Env configuration tests for omitted policy, `fail_open`, `fail_closed`, timeout, nonzero exit, and hook crash.

## [P1] F-007 Static object-store and Garage credentials are accepted as defaults

- Evidence:
  - File: `internal/config/config.go`
  - Location: object store defaults and `Validate`
  - File: `compose.yaml`
  - Location: Garage/object-store environment defaults
  - File: `scripts/garage_bootstrap.sh`
  - Location: default bucket/access/secret key import
  - Relevant component: object storage and deployment configuration
- Problem:
  The code and Compose setup use static development credentials for object storage and Garage RPC secrets. Config validation checks database and Neo4j requirements but does not reject object-store default credentials.
- Impact:
  If reused beyond local development, artifact storage can be compromised. Static credentials also make environment drift hard to detect.
- Reproduction or failure scenario:
  Start the Compose stack without overriding object-store credentials. The system uses predictable access/secret keys and Garage secret defaults.
- Recommended fix:
  Add explicit runtime profiles. In non-development profiles, fail validation if object-store credentials, Garage RPC secret, bucket, or endpoint are default sample values. Generate local dev secrets during bootstrap.
- Suggested test coverage:
  Config validation tests for dev profile acceptance, production profile rejection, missing secret, default secret, and rotated secret.

## [P1] F-008 AG-UI listener failure can leave the daemon and container apparently healthy

- Evidence:
  - File: `cmd/aura/serve.go`
  - Location: AG-UI HTTP server goroutine and `ListenAndServe` error handling
  - File: `compose.yaml`
  - Location: service healthcheck uses `aura version`
  - Relevant component: serving lifecycle and health checks
- Problem:
  If the AG-UI listener exits with an error, the error is logged but the wider daemon can continue. The Compose healthcheck only runs `aura version`, so it can remain green while the HTTP API is down.
- Impact:
  Orchestrators and operators can treat the service as healthy while users cannot reach the API. This breaks production availability semantics and incident detection.
- Reproduction or failure scenario:
  Start with a port conflict or force the listener to fail. The process can continue running, and the container healthcheck still passes because it does not query readiness.
- Recommended fix:
  Treat listener startup/runtime failure as fatal to the serving process, or wire listener state into readiness and have Compose/Kubernetes probe `/readyz`.
- Suggested test coverage:
  Startup tests for port conflict, listener crash, readiness false on listener failure, and Compose healthcheck command validation.

## [P2] F-009 Outside-workspace `send_file` approval is advertised but not wired to resume hooks

- Evidence:
  - File: `internal/agent/tools/send_file.go`
  - Location: outside-workspace result and `resume_context`
  - File: `cmd/aura/serve_adapters.go`
  - Location: `chainResumeHooks`, `newSkillApprovalResumeHook`, `newShellApprovalResumeHook`
  - File: `internal/runner/runner_resume.go`
  - Location: resume hook invocation
  - Relevant component: file transfer and HITL approval
- Problem:
  The tool tells the model to ask the user with a `send_file_outside_workspace` resume context, but the server-side resume hooks only handle skill approval and shell approval contexts.
- Impact:
  User approval cannot complete the advertised flow. The agent may loop, fail, or ask repeatedly.
- Reproduction or failure scenario:
  Attempt to send an absolute path outside the workspace through `send_file`. The tool returns a request for user approval, but there is no hook to convert that approval into executable state.
- Recommended fix:
  Implement a send-file resume hook or remove the advertised approval route until it is supported. The hook must authorize one path, one session, and one expiry window.
- Suggested test coverage:
  Integration test for outside-workspace send request, approval, expiry, rejection, and path mismatch.

## [P2] F-010 `fs_write` bypasses the atomic write helper

- Evidence:
  - File: `internal/agent/tools/fs_write.go`
  - Location: direct `os.WriteFile`
  - File: `internal/agent/tools/fs.go`
  - Location: `atomicWriteFile`
  - File: `internal/agent/tools/fs_edit.go`
  - Location: `atomicWriteFile` usage
  - Relevant component: filesystem tools
- Problem:
  `fs_edit` uses the atomic write helper, while `fs_write` writes directly to the final path.
- Impact:
  A crash, kill, or disk error during write can leave a truncated or partially updated file.
- Reproduction or failure scenario:
  Start a large `fs_write` and terminate the process mid-write. The target path may contain partial content.
- Recommended fix:
  Use `atomicWriteFile` in `fs_write` and preserve the desired mode/permission behavior.
- Suggested test coverage:
  Unit tests for successful atomic create, overwrite, permission preservation, and simulated temp-file failure without target corruption.

## [P2] F-011 Mutating tool invocation ledger is best-effort

- Evidence:
  - File: `internal/runner/runner_persist.go`
  - Location: ledger insert failure logging and nil-ledger no-op
  - File: `internal/toolinvocations/store.go`
  - Location: `Insert`, argument/result redaction and capping
  - Relevant component: audit ledger
- Problem:
  Tool invocation persistence can fail or be absent while tool execution continues. This is a reasonable local convenience but weak for industrial forensic guarantees.
- Impact:
  High-risk shell/filesystem/MCP side effects can occur without a durable audit record.
- Reproduction or failure scenario:
  Break the tool-invocation store or run with a nil ledger, then execute a mutating tool. The system logs and proceeds.
- Recommended fix:
  Require successful pre-execution ledger reservation for mutating tools in production mode. Mark invocation status as started/succeeded/failed with durable IDs.
- Suggested test coverage:
  Tests that mutating tools are blocked when ledger reservation fails in production mode, while read-only tools degrade according to policy.

## [P2] F-012 Background shell jobs lack TTL and owner-scoped control policy

- Evidence:
  - File: `internal/agent/tools/shell_bg.go`
  - Location: background process start, max-running cap, shutdown
  - Relevant component: background shell execution
- Problem:
  Background jobs are capped by count and shut down on process shutdown, but they are started from a detached context and do not have a mandatory TTL, per-session ownership, or explicit production policy.
- Impact:
  Long-running or forgotten jobs can consume resources, retain secrets in process state, or survive longer than the initiating task expects.
- Reproduction or failure scenario:
  Start several long-running background commands. They remain active until completion or shutdown, subject mostly to global max-running count.
- Recommended fix:
  Add default TTL, owner/session/task IDs, explicit kill permissions, job age metrics, and production-mode policy limiting allowed background commands.
- Suggested test coverage:
  Tests for TTL expiry, owner mismatch denial, shutdown cancellation, max-running enforcement, and metrics.

## [P2] F-013 Streamable HTTP MCP entries with empty trust become runnable remote servers

- Evidence:
  - File: `internal/mcp/manager/runtime.go`
  - Location: `normalizedTrustForServer`
  - File: `internal/mcp/managed_config.go`
  - Location: `NormalizedTrust`
  - Relevant component: MCP trust management
- Problem:
  Managed servers with empty trust and Streamable HTTP/URL transport normalize to `remote_http`, which is runnable.
- Impact:
  Imported or hand-authored config can enable remote MCP connectivity without an explicit approval field. This is less dangerous than model-created trust, but it is still weak for production configuration governance.
- Reproduction or failure scenario:
  Add a Streamable HTTP MCP server without a trust class. Runtime normalization treats it as remote HTTP rather than blocked.
- Recommended fix:
  Require explicit trust for all remote MCP transports. Empty trust should mean blocked unless the entry came from a verified interactive approval flow.
- Suggested test coverage:
  Config tests for empty trust local command, empty trust remote URL, explicit blocked, explicit approved, and imported managed config.

## [P2] F-014 Legacy `AURA_MCP_SERVERS_JSON` bypasses managed MCP governance metadata

- Evidence:
  - File: `internal/config/config.go`
  - Location: `parseMCPServersJSON`
  - Relevant component: legacy MCP server configuration
- Problem:
  Legacy MCP server JSON validates command presence but does not use the newer managed-server trust metadata and approval workflow.
- Impact:
  Operators can configure process-spawning MCP servers outside the managed governance path. This is acceptable as a legacy escape hatch only if documented and production-gated.
- Reproduction or failure scenario:
  Set `AURA_MCP_SERVERS_JSON` to a server command. The runtime can load it without managed trust metadata.
- Recommended fix:
  Deprecate or production-disable the legacy env path, or translate entries into managed config with explicit trust class and audit metadata.
- Suggested test coverage:
  Production profile tests rejecting legacy env config unless an explicit compatibility flag is set.

## [P2] F-015 CI uses raw `./...` despite the repository's package filter

- Evidence:
  - File: `scripts/go_packages.sh`
  - Location: filters out `web/node_modules`
  - File: `Makefile`
  - Location: `GO_PACKAGES := $(shell bash scripts/go_packages.sh)`
  - File: `.github/workflows/ci.yml`
  - Location: raw `go test ./...`, `go build ./...`, `govulncheck ./...`
  - Relevant component: CI/test reproducibility
- Problem:
  Local Makefile tests use a filtered package list because local frontend dependencies can contain Go examples. CI still uses raw `./...` in several places.
- Impact:
  Test behavior differs between local and CI. Any checkout with generated or restored frontend dependencies can accidentally include third-party Go packages.
- Reproduction or failure scenario:
  Run `go list ./...` after frontend dependencies exist under `web/node_modules`; unrelated packages can be discovered.
- Recommended fix:
  Reuse `scripts/go_packages.sh` in all Go build/test/vulnerability CI jobs, or prevent frontend dependency directories from existing during Go package discovery.
- Suggested test coverage:
  CI lint that rejects raw `go test ./...` and `govulncheck ./...` in this repository.

## [P2] F-016 Config parsing silently falls back on malformed environment values

- Evidence:
  - File: `internal/config/config_env.go`
  - Location: `envIntDefault`, `envBoolDefault`
  - Relevant component: configuration management
- Problem:
  Invalid integer and boolean values silently use defaults.
- Impact:
  Operators can believe they changed timeout, concurrency, or security-related settings while the runtime silently ignores them.
- Reproduction or failure scenario:
  Set an invalid numeric env value for a critical knob. The app uses the default without a validation error or warning.
- Recommended fix:
  Add a config diagnostics report and fail-fast mode for production. Invalid env should be warnings in dev and errors in production for security/reliability knobs.
- Suggested test coverage:
  Table tests for malformed int/bool values across dev and production profiles.

## [P2] F-017 Docker healthcheck does not query application readiness

- Evidence:
  - File: `compose.yaml`
  - Location: healthcheck `aura version`
  - File: `internal/agui/server.go`
  - Location: `/healthz`, `/readyz`
  - Relevant component: deployment health checks
- Problem:
  The container healthcheck validates that the binary runs, not that the HTTP service, database dependencies, or readiness state are healthy.
- Impact:
  Containers can be marked healthy while the served API is unavailable.
- Reproduction or failure scenario:
  Break the HTTP listener or an essential dependency. `aura version` can still pass.
- Recommended fix:
  Use an HTTP readiness probe against `/readyz` and ensure readiness reflects listener and dependency state.
- Suggested test coverage:
  Compose smoke test that fails when `/readyz` is unavailable.

## [P2] F-018 Garage local topology defaults to replication factor 1

- Evidence:
  - File: `docker/garage/garage.toml`
  - Location: `replication_factor = 1`
  - Relevant component: object storage durability
- Problem:
  The object-store topology is single-replica by default.
- Impact:
  Artifact data has no storage redundancy in that profile. This may be fine for development but is not production durable.
- Reproduction or failure scenario:
  Lose the single Garage node or volume. Artifact data is unavailable or lost.
- Recommended fix:
  Document as development-only and require a production object-store backend or multi-node Garage topology for production profile.
- Suggested test coverage:
  Config/deployment validation that rejects replication factor 1 in production profiles.

## [P2] F-019 Missing load, chaos, and security regression gates for agent loops

- Evidence:
  - File: repository tests and `Makefile`
  - Location: unit/race/test targets exist; no mandatory load/chaos/security-agent regression gate was found
  - Reference: `D:\tmp\agent-infra-sandbox\evaluation\README.md`
  - Relevant component: quality and release gating
- Problem:
  The repository has meaningful unit and integration coverage, but no mandatory release gate for adversarial prompt/tool safety, loop load, chaos cancellation, or multi-tool workflow evaluation.
- Impact:
  Regressions in prompt-injection defense, loop liveness, cancellation, or high-concurrency behavior can ship unnoticed.
- Reproduction or failure scenario:
  Change terminal dispatch or tool policy and run normal tests. Without adversarial scenario tests, side-effect regressions can pass.
- Recommended fix:
  Add a capability-based evaluation suite for shell/files/browser/MCP/error/workflow classes, with production-safety expectations.
- Suggested test coverage:
  Golden tests for prompt-injection, terminal sibling rejection, runaway loop budgets, pause/resume races, MCP timeout, shell cancellation, and background job TTL.

## [P2] F-020 Tool side-effect transaction boundaries are inconsistent

- Evidence:
  - File: `internal/agent/llm_agent_dispatch.go`
  - Location: runnable batch execution
  - File: `internal/runner/runner_persist.go`
  - Location: best-effort ledger persistence
  - File: `internal/agent/tools/fs_write.go`
  - Location: direct file write
  - Relevant component: tool execution consistency
- Problem:
  Different tools and runner layers have different durability semantics. Some writes are atomic, some are direct, and audit logging can be best-effort.
- Impact:
  Failure recovery can leave side effects without durable state, state without side effects, or partially written artifacts.
- Reproduction or failure scenario:
  Kill the process during a multi-tool step containing filesystem writes and ledger writes. Recovery cannot reliably infer which side effects occurred.
- Recommended fix:
  Define a tool execution state machine: planned, authorized, started, side-effect committed, result persisted, visible to model. Require idempotency keys and recovery handlers for mutating tools.
- Suggested test coverage:
  Fault-injection tests at each state transition for mutating tools.

## [P3] F-021 Reasoning trace full mode needs explicit retention and encryption policy

- Evidence:
  - File: `internal/reasoningtrace/reasoningtrace.go`
  - Location: full trace mode and redaction helpers
  - Relevant component: reasoning trace observability
- Problem:
  Default behavior redacts/summarizes sensitive fields, but full trace mode intentionally records more data. Retention, encryption, and operator warning policy are not enforceable from code alone.
- Impact:
  Full traces can become sensitive records if enabled during production incidents.
- Reproduction or failure scenario:
  Enable full reasoning trace in a production-like environment and store logs on local disk.
- Recommended fix:
  Add explicit production warning/fail-fast, retention configuration, and optional encrypted trace sink.
- Suggested test coverage:
  Tests for full-mode config warnings and redaction invariants.

## [P2] F-022 Permissive CORS mode can expose unauthenticated loopback AG-UI

- Evidence:
  - File: `internal/agui/server_cors.go`
  - Location: permissive CORS headers
  - File: `internal/agui/auth.go`
  - Location: no-op auth path when no secret is configured
  - File: `internal/config/config.go`
  - Location: bind guard and Web UI auth configuration
  - Relevant component: Web UI HTTP middleware
- Problem:
  Permissive CORS is useful for development, but production policy is not enforced by runtime profile. In local no-auth mode, enabling permissive CORS can allow arbitrary browser origins to preflight, create/list conversations, submit `/agent/run`, and read streamed responses from `http://127.0.0.1:9080`.
- Impact:
  A drive-by web page can interact with a developer's local trusted Aura instance if permissive CORS and no-auth loopback mode are combined.
- Reproduction or failure scenario:
  Enable `AURA_AGUI_CORS_PERMISSIVE=true` with no Web UI auth secret on loopback. A malicious page uses browser fetches against the local AG-UI origin.
- Recommended fix:
  Replace wildcard mode with explicit origin allowlists, refuse permissive CORS when auth is disabled except under an explicit development profile, set `Vary: Origin`, and keep allowed methods in sync with registered routes.
- Suggested test coverage:
  Cross-origin preflight to `/api/conversations` and `/agent/run` is denied by default/no-auth; an allowlisted origin succeeds; governance `PATCH` preflight is covered when write routes are enabled.

## [P3] F-023 Metrics exist but no SLO dashboard or alert pack was found

- Evidence:
  - File: `internal/agui/server.go`
  - Location: `/metrics` and `/debug/vars` route registration
  - Relevant component: observability
- Problem:
  Metrics endpoints exist, but source review did not find a production dashboard/alert pack that defines SLOs for loop errors, tool failures, queue lag, LLM latency, MCP timeout rate, resume failures, or listener health.
- Impact:
  Operators lack a ready production monitoring baseline.
- Reproduction or failure scenario:
  Deploy the service and ingest metrics. Alerts still need to be invented manually.
- Recommended fix:
  Provide Prometheus alert rules and Grafana dashboards tied to production SLOs.
- Suggested test coverage:
  Static validation for alert rule syntax and dashboard JSON.

## [P3] F-024 Sidecar and trace retention need explicit cleanup operations

- Evidence:
  - File: `internal/agent/tools/result.go`
  - Location: result sidecar writing
  - File: `internal/conversations/store_helpers.go`
  - Location: conversation content sidecar writing
  - Relevant component: local persistence
- Problem:
  Sidecars are useful for large content, but retention and cleanup policy are not a first-class operational workflow in the audited paths.
- Impact:
  Long-running systems can accumulate sensitive and large local files.
- Reproduction or failure scenario:
  Run long conversations with large tool outputs. Sidecar directories grow without an explicit lifecycle policy.
- Recommended fix:
  Add retention configuration, cleanup command, disk usage metrics, and per-conversation export/delete operations.
- Suggested test coverage:
  Cleanup tests for age, size, active conversation exclusion, and dry-run reporting.

## [P3] F-025 Reference comparison is not continuously tracked

- Evidence:
  - Reference: `D:\tmp\adk-go-study`, `D:\tmp\agent-memory`, `D:\tmp\agent-infra-sandbox`
  - Relevant component: architecture governance
- Problem:
  Useful patterns exist in nearby experiments and references, but there is no documented decision record connecting them to Aura's architecture.
- Impact:
  Good patterns can be lost or reintroduced inconsistently.
- Reproduction or failure scenario:
  Engineers separately copy pieces of reference code without preserving why a pattern was selected or rejected.
- Recommended fix:
  Add ADRs for loop budgeting, session event persistence, memory provenance, and tool evaluation taxonomy.
- Suggested test coverage:
  Documentation check requiring ADR references for major architecture changes.

## [P3] F-026 Industrial deployment profile is not documented as a contract

- Evidence:
  - File: `README.md`
  - Location: local-first positioning
  - File: `docs/ARCHITECTURE.md`
  - Location: architecture descriptions
  - Relevant component: deployment contract
- Problem:
  Documentation explains the system, but the exact contract for "production mode" versus "trusted local mode" is not expressed as enforceable configuration.
- Impact:
  Operators can accidentally deploy local-trusted defaults in higher-risk environments.
- Reproduction or failure scenario:
  Deploy with Compose defaults and assume the auth/health/tool posture is production-ready.
- Recommended fix:
  Define and enforce deployment profiles with explicit requirements for auth, secrets, tool capabilities, sandboxing, object storage, health checks, TLS, and observability.
- Suggested test coverage:
  Profile validation tests and a production-readiness command that fails on unmet requirements.

## Subagent Delta Findings

The following findings were added after the explicit subagent review request. Six read-only explorers covered agent loop/runner, tools/security, persistence/memory, MCP/governance, AG-UI/Web/API, and infrastructure/operations. The testing/CI slice was covered locally after the seventh explorer could not be spawned because the agent-thread limit was reached.

## [P1] F-027 Mixed URL+command MCP entries can bypass local-command trust blocking

- Evidence:
  - File: `internal/mcp/manager/runtime.go`
  - Location: `normalizedTrustForServer`, `RunnableManagedServers`
  - File: `internal/mcp/managed_config.go`
  - Location: `normalizedServerType`
  - File: `internal/agent/mcptools/mount.go`
  - Location: `MountManagedServer`
  - File: `internal/mcp/transport.go`
  - Location: `OpenServer`
  - Relevant component: managed MCP trust and transport classification
- Problem:
  Trust classification treats any server with a non-empty URL as remote HTTP trust, while type normalization can still classify a server with both `url` and `command` as stdio. This creates inconsistent policy and open behavior for ambiguous managed MCP entries.
- Impact:
  A hand-authored or imported entry can appear eligible as remote HTTP but still launch a local command path.
- Reproduction or failure scenario:
  A managed MCP entry contains both `url: "https://decoy"` and `command: "powershell"` with no explicit trust class. Runtime eligibility can be inferred from URL presence while the mount/open path can fall through to stdio execution.
- Recommended fix:
  Use one canonical transport classifier for validation, trust normalization, runtime eligibility, mounting, and opening. Reject mixed `url` + `command` unless an explicit type disambiguates and the trust class matches that type.
- Suggested test coverage:
  Config with URL+command and empty trust must be blocked and must not call stdio `mcp.Open`. Explicit streamable HTTP with command present should fail validation.

## [P1] F-028 Multi-user Web/API data is not consistently scoped to authenticated identity

- Evidence:
  - File: `internal/agui/conversations_api.go`
  - Location: `handleListConversations`, create/get/archive/delete routes
  - File: `internal/agui/approvals_api.go`
  - Location: `handleListApprovals`
  - File: `internal/runner/runner_conversation.go`
  - Location: `NewConversation`, `NewConversationWithID`
  - Relevant component: AG-UI/Web identity isolation
- Problem:
  Conversation and approval APIs list and mutate global stores rather than consistently filtering by the authenticated principal. New Web conversations are created under the seeded `local` identity, while the runner later enforces context identity against conversation identity.
- Impact:
  Provisioned users can see or mutate conversations and approvals outside their identity scope, and newly created Web conversations can fail later with identity mismatch.
- Reproduction or failure scenario:
  Identity B lists all conversations and approval prompts, archives or deletes Identity A's conversation, or attempts to resolve another user's approval. A new chat created by B is owned by `local`, then `/agent/run` fails when the runner scopes to B.
- Recommended fix:
  Add owner-aware store and API methods; filter lists/search/approvals by principal; return 404 or 403 for cross-principal get/mutation; make `NewConversation` use `identityctx.IdentityID(ctx)` with `local` only for CLI/no-principal fallback.
- Suggested test coverage:
  Two authenticated identities with separate sessions: B cannot list, get, delete, archive, or resolve A's data; B-created conversation is owned by B and can run.

## [P2] F-029 Single resume claim and answer append are not atomic

- Evidence:
  - File: `internal/runner/runner_resume.go`
  - Location: `SubmitAnswer`
  - Relevant component: human-in-the-loop resume
- Problem:
  The single-answer path correctly claims the pause before appending the answer, but the claim and append are still separate operations. If `AppendTurn` fails after `MarkResumed`, the pause disappears without a matching persisted tool answer.
- Impact:
  A resolved pause can lose the answer required to make resumed model history wire-valid and understandable.
- Reproduction or failure scenario:
  Inject a conversation append failure after `MarkResumed` succeeds. The pending state is no longer retryable, but the matching `RoleTool` answer is absent.
- Recommended fix:
  Use one transaction for pause claim and answer append, or introduce an idempotent resume ledger that can repair or replay answer injection after append failure.
- Suggested test coverage:
  Fail `AppendTurn` after `MarkResumed`; assert the pause is still pending or a repairable resume-injection record exists.

## [P2] F-030 Pause flush failure is hidden after a consumer stops on pause

- Evidence:
  - File: `internal/runner/runner.go`
  - Location: deferred `flushOnce`
  - File: `internal/runner/runner_persist.go`
  - Location: `persistPause`, `flushPause`
  - Relevant component: pause persistence and resume history
- Problem:
  When a consumer stops iterating on a pause event, deferred `flushPause` runs and only logs failure. The pause row may already exist while the matching assistant `ask_user` tool-call turn is missing.
- Impact:
  Resume can inject tool answers that have no matching assistant tool call in history, causing malformed history or repeated asks.
- Reproduction or failure scenario:
  AG-UI stops on pause, `flushPause` append fails, the deferred logger records an error, and later resume injects orphan tool answers.
- Recommended fix:
  Persist assistant pause tool-call turn and pause rows atomically before exposing the pause, or make pause exposure conditional on durable wire-valid history.
- Suggested test coverage:
  Consumer returns false on pause and `AppendTurn` fails; assert no pending pause is visible without corresponding assistant tool-call history.

## [P2] F-031 Mutating panic path loses mutating classification

- Evidence:
  - File: `internal/agent/llm_agent_parallel.go`
  - Location: `runToolRecovering`
  - File: `internal/agent/llm_agent_dispatch.go`
  - Location: mutating completion-gate check
  - Relevant component: parallel tool execution and completion gate
- Problem:
  The panic recovery result does not preserve the tool's mutating classification. A mutating tool can perform a side effect and then panic, but the recovered result can be treated as non-mutating by the completion gate.
- Impact:
  The post-side-effect completion critic can be skipped after a partial side effect followed by panic.
- Reproduction or failure scenario:
  A mutating fake tool writes state and panics. `runToolRecovering` returns a recovered error result without `Mutating=true`; `a.sideEffected` is not armed.
- Recommended fix:
  Resolve and preserve the tool descriptor's mutating flag before executing the tool and copy it into panic recovery results.
- Suggested test coverage:
  Mutating fake tool panics after side effect; assert `sideEffected`/completion gate is armed and the event marks the invocation as mutating.

## [P2] F-032 Background shell IDs are predictable and not session-bound

- Evidence:
  - File: `internal/agent/tools/shell_bg.go`
  - Location: `BackgroundShells.start`, `ShellPoll.Execute`, `ShellKill.Execute`
  - Relevant component: background shell registry
- Problem:
  Background shell IDs are sequential (`sh_1`, `sh_2`, ...), process-scoped, and not tied to a session or actor. Poll and kill operations accept only the shell ID.
- Impact:
  Another conversation in the same daemon can guess, poll, or terminate a background job and view sensitive output.
- Reproduction or failure scenario:
  User A starts a background build that prints secrets. User B guesses `sh_1` or enumerates recent IDs and uses `shell_poll` or `shell_kill`.
- Recommended fix:
  Use random unguessable IDs and bind each job to session/actor metadata. Require matching session/actor for poll and kill unless an administrative capability is present.
- Suggested test coverage:
  Session B cannot poll or kill a shell started by session A; admin capability can inspect if explicitly allowed.

## [P2] F-033 MCP boot mount lacks per-server timeout

- Evidence:
  - File: `cmd/aura/main.go`
  - Location: MCP mounting path
  - File: `cmd/aura/chat.go`
  - Location: chat MCP mount
  - File: `internal/agent/mcptools/mount.go`
  - Location: `MountServer`
  - File: `internal/mcp/client.go`
  - Location: stdio open/initialize flow
  - Relevant component: MCP boot lifecycle
- Problem:
  MCP mount/open and initial `tools/list` can inherit broader startup contexts without a per-server mount deadline.
- Impact:
  A hung MCP process can delay or wedge `serve`, `chat`, or `aura tools` startup despite fail-soft intentions.
- Reproduction or failure scenario:
  A stdio MCP helper starts but never emits a JSON-RPC response. Startup waits until the outer context is canceled.
- Recommended fix:
  Wrap each MCP mount in a bounded per-server context with a configurable default and reap the process on timeout.
- Suggested test coverage:
  Hung helper server is dropped and registry construction returns within the configured deadline.

## [P2] F-034 Stdio MCP response frames are uncapped

- Evidence:
  - File: `internal/mcp/client.go`
  - Location: stdio scanner/reader framing around line reads
  - Relevant component: MCP stdio transport
- Problem:
  Stdio MCP responses are line-delimited but the audited path does not enforce a maximum frame/body size before parsing.
- Impact:
  A malicious or buggy MCP server can send a very large single JSON line and force memory growth before schema/result preview caps apply.
- Reproduction or failure scenario:
  MCP server writes hundreds of MB without a newline during `tools/list` or `tools/call`.
- Recommended fix:
  Add a maximum stdio frame size comparable to HTTP body caps and abort the transport deterministically on overflow.
- Suggested test coverage:
  Oversized stdio frame returns a transport error without large allocation and closes the server transport.

## [P2] F-035 MCP transport shutdown can hang or leave child processes

- Evidence:
  - File: `internal/mcp/http_client.go`
  - Location: HTTP `Close`
  - File: `internal/mcp/client.go`
  - Location: stdio `Close`/abort process cleanup
  - Relevant component: MCP transport lifecycle
- Problem:
  HTTP close can use an unbounded background context, and stdio termination focuses on the direct process rather than the entire process tree.
- Impact:
  Shutdown can hang on remote DELETE/session cleanup or leave child processes after local MCP termination.
- Reproduction or failure scenario:
  Remote Streamable HTTP endpoint never completes close, or local MCP spawns a child that survives parent process kill.
- Recommended fix:
  Bound HTTP close with a timeout and use process groups/job objects for stdio process-tree termination.
- Suggested test coverage:
  Blocking HTTP close returns within the cap; child process spawned by a local MCP is gone after close timeout.

## [P2] F-036 Docker MCP network allowlist is advisory, not enforced

- Evidence:
  - File: `internal/mcp/manager/runtime.go`
  - Location: Docker runtime network handling
  - Relevant component: Docker-backed MCP runtime
- Problem:
  Non-empty runtime network configuration can switch Docker to a bridge network while `AURA_MCP_NETWORK_ALLOW` is passed as environment data that the container may ignore.
- Impact:
  A "sandboxed" MCP with a nominal allowlist may still reach arbitrary bridge-network destinations.
- Reproduction or failure scenario:
  Configure a Docker MCP with allowlist `api.example.com`; the container runs with bridge networking and can make other egress requests unless the image voluntarily enforces the env var.
- Recommended fix:
  Enforce egress with a proxy/firewall/network policy backend, or keep `--network none` unless an enforceable network policy is available.
- Suggested test coverage:
  Integration test proves configured allowlist cannot reach a disallowed host.

## [P2] F-037 CLI MCP mutations bypass audited atomic writer

- Evidence:
  - File: `cmd/aura/mcp.go`
  - Location: add/trust/enable/disable/remove commands
  - File: `cmd/aura/mcp_profile.go`
  - Location: profile mutations
  - File: `internal/mcp/manager/configwrite.go`
  - Location: audited `WriteConfigWithAudit`
  - Relevant component: MCP governance writes
- Problem:
  Governance HTTP writes use an atomic config+audit path, but CLI MCP mutations use direct managed-config saves.
- Impact:
  Trust approval and enable/disable changes can land without `mcp_audit` rows and without the same crash-atomic behavior as governance writes.
- Reproduction or failure scenario:
  Run `aura mcp trust` through CLI. The config changes, but no audit row is inserted through `WriteConfigWithAudit`.
- Recommended fix:
  Route CLI mutations through the same audited writer or explicitly mark CLI config writes as unaudited and disallow them in production profile.
- Suggested test coverage:
  CLI `mcp trust` appends audit row and simulated audit failure leaves config unchanged.

## [P2] F-038 MCP trust endpoint accepts empty body and defaults to `trusted_local`

- Evidence:
  - File: `internal/agui/governance_write_api.go`
  - Location: `handleMCPTrust`
  - File: `cmd/aura/serve_governance_write.go`
  - Location: `TrustApprove`
  - Relevant component: MCP governance API
- Problem:
  `TrustApprove` trims class and defaults an empty class to `trusted_local`; the API accepts decoded empty JSON bodies and passes blank reason through to audit.
- Impact:
  A governance-write caller can trust a blocked custom MCP server with an empty or `{}` body, producing an executable local-trusted server with weak audit context.
- Reproduction or failure scenario:
  Install a custom blocked local-command MCP server, then POST `/api/governance/mcp/{name}/trust` with `{}`. The provider stores `trusted_local`.
- Recommended fix:
  Require explicit known `class` and non-empty `reason`; reject empty body, unknown class, blank reason, and type/class mismatches.
- Suggested test coverage:
  Empty body, `{}`, blank reason, and unknown class return 400 and do not alter trust; valid class+reason writes one audit row.

## [P2] F-039 Conversation delete bypasses in-memory session eviction

- Evidence:
  - File: `internal/agui/conversations_api.go`
  - Location: delete handler
  - File: `internal/channels/telegram/commands.go`
  - Location: clear command
  - File: `cmd/aura/chat.go`
  - Location: CLI clear/delete flow
  - File: `internal/runner/runner_resume.go`
  - Location: `Runner.Stop` and session eviction
  - Relevant component: conversation lifecycle and in-memory tool state
- Problem:
  Some delete/clear flows delete persisted conversation state directly instead of routing through a runner-owned lifecycle method that evicts session-scoped tool state.
- Impact:
  Todo state, shell cwd, approval maps, and background-shell buffers can survive after the persisted conversation is deleted.
- Reproduction or failure scenario:
  Telegram `/clear` deletes the DB row for a deterministic conversation ID. A later chat reuses the same ID and inherits stale in-memory tool state.
- Recommended fix:
  Route all conversation deletion through a runner lifecycle method that cancels active work, auto-resolves or expires pending pauses, evicts session tools, handles background jobs, and then deletes persistence.
- Suggested test coverage:
  Seed session tool state, call AG-UI delete and Telegram clear, assert all registered `SessionEvictor`s are invoked and running jobs are handled by policy.

## [P2] F-040 Crash-created sidecars inside live conversation directories are not reclaimed

- Evidence:
  - File: `internal/conversations/store_append.go`
  - Location: append transaction flow
  - File: `internal/conversations/store_helpers.go`
  - Location: sidecar spill/write
  - File: `internal/conversations/orphan_scan.go`
  - Location: orphan scanning
  - File: `internal/conversations/sweeper.go`
  - Location: sweeper
  - Relevant component: conversation sidecar lifecycle
- Problem:
  If the process writes a sidecar file and crashes before the database row commits, the sidecar sits in a live conversation directory. Existing orphan scanning can preserve the whole live directory rather than reconciling unreferenced `.content` files inside it.
- Impact:
  Long-running systems can accumulate sensitive and unbounded sidecar files after crash windows.
- Reproduction or failure scenario:
  Process writes `99.content`, crashes before commit, and future sweeps keep the conversation directory because the conversation itself is live.
- Recommended fix:
  Reconcile live conversation directories against committed `conversation_turns.content_sidecar_path` with an age grace period, or use temp-file plus DB-committed rename semantics.
- Suggested test coverage:
  Create a live conversation directory with an unreferenced `.content` file; after grace, scan reports or removes it while preserving referenced sidecars.

## [P2] F-041 Relative `AURA_RUN_DIR` can make sidecars cwd-dependent and unreadable

- Evidence:
  - File: `internal/config/config.go`
  - Location: run directory loading/defaults
  - File: `internal/agent/tools/result.go`
  - Location: tool result sidecar path generation
  - File: `internal/agent/tools/read_tool_output.go`
  - Location: absolute runDir requirement
  - File: `internal/conversations/store.go`
  - Location: conversation sidecar root
  - Relevant component: runtime storage configuration
- Problem:
  `AURA_RUN_DIR` can be relative in configuration paths that write sidecars, while `read_tool_output` refuses relative run directories and restarts under different cwd can resolve paths differently.
- Impact:
  Tool outputs and conversation sidecars can become unreadable or resolve to different filesystem locations after restart.
- Reproduction or failure scenario:
  Set `AURA_RUN_DIR=run`, write a tool output sidecar from one cwd, restart from another cwd, then attempt `read_tool_output`.
- Recommended fix:
  Normalize `RunDir` to an absolute path during config load or reject non-absolute values in validation and constructors.
- Suggested test coverage:
  Relative `AURA_RUN_DIR` fails config validation or round-trips through normalized absolute sidecar paths.

## [P2] F-042 Scheduler jobs cancel immediately on SIGTERM despite drain comments

- Evidence:
  - File: `cmd/aura/serve.go`
  - Location: serve root context and signal handling
  - File: `internal/cron/scheduler.go`
  - Location: scheduler stop/drain
  - File: `internal/cron/dispatch.go`
  - Location: handler context dispatch
  - Relevant component: scheduler shutdown
- Problem:
  In-flight scheduler work appears tied to the same cancellation path used to stop admission. On SIGTERM, handler contexts can be canceled immediately instead of receiving a bounded drain window.
- Impact:
  Backup or agent jobs can fail mid-work despite comments implying graceful drain.
- Reproduction or failure scenario:
  Send SIGTERM during a long backup or agent job. The handler receives canceled context and marks work failed.
- Recommended fix:
  Separate stop-admission context from job-work context and apply an explicit drain deadline before canceling in-flight handlers.
- Suggested test coverage:
  Fake long handler receives SIGTERM; assert handler context remains live until drain deadline and no new jobs are admitted.

## [P2] F-043 Stop budgets are inconsistent with backup runtime

- Evidence:
  - File: `deploy/aura-scheduler.service`
  - Location: `TimeoutStopSec`
  - File: `internal/cron/handlers/backup.go`
  - Location: backup handler timeout/duration
  - Relevant component: systemd deployment and backup job lifecycle
- Problem:
  The systemd stop timeout is shorter than the maximum backup job duration.
- Impact:
  systemd can SIGKILL the scheduler while `pg_dump` or related backup work is still running, leaving partial output.
- Reproduction or failure scenario:
  Restart service during a large backup. The service stop budget expires before backup max duration.
- Recommended fix:
  Align `TimeoutStopSec` with the longest handler duration plus grace, or reduce backup max duration and write dumps atomically via temp file/rename.
- Suggested test coverage:
  Static test asserts service stop timeout exceeds longest configured handler duration; integration test kills during backup and verifies partial artifacts are not promoted.

## [P3] F-045 Timed worker wait can leak waiter goroutines

- Evidence:
  - File: `internal/runner/runner_resume.go`
  - Location: `waitWorkers`, `Stop`
  - Relevant component: runner worker shutdown
- Problem:
  Timed wait can spawn a goroutine waiting on `WaitGroup.Wait`. If the worker never returns, repeated stop calls can accumulate blocked waiter goroutines.
- Impact:
  A pathological title/worker hang can leak goroutines across repeated shutdown attempts.
- Reproduction or failure scenario:
  Title worker ignores context and never returns. Multiple `Stop` calls each create a waiter blocked on the same `WaitGroup`.
- Recommended fix:
  Use one lifecycle-owned done channel or ensure the timed wait goroutine is created once and reused.
- Suggested test coverage:
  Hung worker with tiny stop timeout; repeated `Stop` calls do not increase goroutine count.

## [P3] F-046 HTTP MCP probe reports configured endpoints as healthy without dialing

- Evidence:
  - File: `internal/mcp/probe.go`
  - Location: HTTP MCP probe path
  - Relevant component: governance/doctor MCP status
- Problem:
  Probe/status can report a configured HTTP MCP endpoint as OK without dialing and listing tools.
- Impact:
  Governance UI or doctor output can show dead or typoed HTTP endpoints as healthy until actual runtime mount or tool call fails.
- Reproduction or failure scenario:
  Configure a URL typo; probe reports OK because it validates configuration shape only.
- Recommended fix:
  Use `OpenServer` plus `ListTools` under the existing short probe deadline for HTTP endpoints too.
- Suggested test coverage:
  Dead HTTP URL returns `OK=false`; live test server returns actual tool count.

## [P3] F-047 Validation console can expose token-injecting integration proxy on non-loopback bind

- Evidence:
  - File: `cmd/aura/integrations_console.go`
  - Location: console address binding
  - File: `cmd/aura/integrations_proxy.go`
  - Location: sidecar token proxy
  - Relevant component: integration validation console
- Problem:
  The validation console can be bound to non-loopback addresses and serves a proxy that injects a calendar admin token.
- Impact:
  If exposed on a LAN or remote interface, another host can drive sidecar admin APIs through the proxy.
- Reproduction or failure scenario:
  Start `aura mcp console --addr 0.0.0.0:...`; remote caller reaches the unauthenticated proxy.
- Recommended fix:
  Refuse non-loopback bind unless an explicit unsafe flag and authentication are configured.
- Suggested test coverage:
  Non-loopback `--addr` fails by default; explicit unsafe mode requires auth and logs a warning.

## [P3] F-048 Spilled conversation content is not searchable

- Evidence:
  - File: `internal/conversations/store_helpers.go`
  - Location: `maybeSpill`
  - File: `internal/db/queries/conversation_turns.sql`
  - Location: search query over inline content
  - File: `internal/conversations/store.go`
  - Location: `SearchConversationTurns`
  - Relevant component: conversation search
- Problem:
  Large content that spills to sidecar can have `content=NULL`, while search queries only inspect inline content.
- Impact:
  Important terms in large persisted turns disappear from conversation search.
- Reproduction or failure scenario:
  Append oversized content with a unique token. Search for that token returns no result because the token lives only in a sidecar.
- Recommended fix:
  Persist a searchable preview/text column, sidecar index table, or explicitly document that sidecar content is excluded from search.
- Suggested test coverage:
  Oversized content with unique token is searchable through the intended search path or documented as excluded.

## [P3] F-049 Learning memory stores have no retention cap

- Evidence:
  - File: `internal/activelearn/learner.go`
  - Location: in-process `seen` map
  - File: `internal/reasoningstore/store.go`
  - Location: reasoning examples
  - File: `internal/toolselectstore/store.go`
  - Location: tool selection examples
  - Relevant component: learning memory retention
- Problem:
  Learning stores and in-memory de-duplication can grow without a visible retention cap.
- Impact:
  Long-running learning deployments can accumulate heap entries and Neo4j rows without bound.
- Reproduction or failure scenario:
  High-cardinality inputs create one permanent in-process hash per text and later load all examples into memory.
- Recommended fix:
  Add max examples per label/tool, TTL/compaction, bounded `seen`, and metrics.
- Suggested test coverage:
  Feed more than configured cap and assert compaction plus bounded load size.

## [P3] F-050 Long-lived access token is accepted in URLs

- Evidence:
  - File: `caddy/Caddyfile`
  - Location: token forwarding/auth ingress
  - File: `scripts/install.sh`
  - Location: install summary URL
  - File: `internal/setup/server.go`
  - Location: setup token handling
  - Relevant component: setup/auth ingress
- Problem:
  Long-lived access tokens can be accepted or advertised through query strings.
- Impact:
  Tokens in URLs can leak through browser history, copied URLs, logs, and reverse proxy access logs.
- Reproduction or failure scenario:
  Installer prints or operator shares a URL containing `?token=...`.
- Recommended fix:
  Reserve query tokens for short-lived setup-only bootstrap and prefer headers or secure cookies for long-lived access tokens.
- Suggested test coverage:
  Auth rejects query token after setup bootstrap or after TTL; logs never include token query values.

## [P3] F-051 CI/release uses mutable action and tool references

- Evidence:
  - File: `.github/workflows/ci.yml`
  - Location: third-party actions and tool install steps
  - File: `.github/workflows/release.yml`
  - Location: release action references
  - Relevant component: supply-chain CI/CD
- Problem:
  Some workflow steps use mutable major action tags or install tools at latest/semver-floating versions.
- Impact:
  Changed upstream action tags or tool versions can execute unexpected code, including in release jobs with write/package permissions.
- Reproduction or failure scenario:
  A compromised or moved action tag changes behavior during CI/release.
- Recommended fix:
  Pin third-party actions to SHAs, pin Go/Node tools to exact versions, and add a workflow lint gate.
- Suggested test coverage:
  CI policy fails on `@latest`, semver ranges, and unpinned non-allowlisted actions.

## [P3] F-052 JSON request validation accepts trailing values and unknown fields across privileged routes

- Evidence:
  - File: `internal/agui/server_run_request.go`
  - Location: `decodeRunAgentRequest`
  - File: `internal/agui/governance_write_api.go`
  - Location: `decodeMCPBody`
  - File: `internal/agui/governance_write_skills_api.go`
  - Location: `decodeSkillsBody`
  - Relevant component: Web/API request validation
- Problem:
  Several privileged handlers decode the first JSON value, ignore unknown fields, or allow empty bodies for mutation shapes.
- Impact:
  Malformed or ambiguous client requests can be accepted, and typoed security-relevant fields are silently ignored.
- Reproduction or failure scenario:
  Request body `{"class":"trusted_local"}{"ignored":true}` is accepted as the first object; unknown fields are ignored.
- Recommended fix:
  Centralize strict JSON decoding with size cap, content-type check, `DisallowUnknownFields`, single-decode EOF check, and per-route explicit `allowEmpty`.
- Suggested test coverage:
  Trailing JSON, unknown fields, wrong content type, and empty body fail on `/agent/run`, approvals resolve, onboarding, assets, and governance writes.
