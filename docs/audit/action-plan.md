# Action Plan

## Immediate P0/P1 Fixes

### Fix destructive shell pattern sample configuration

- Description: Remove or comment out `AURA_SHELL_DESTRUCTIVE_PATTERNS=` in `.env.example`, or change parser semantics so empty means defaults.
- Owner role: Platform engineer.
- Expected outcome: Copying the sample env preserves default destructive command approval.
- Acceptance criteria: Tests cover unset, empty, `off`, and custom patterns.

### Make terminal response exclusive with mutating tools

- Description: Reject or replan any model step containing `text_response` plus mutating/deferred siblings.
- Owner role: Agent-loop engineer.
- Expected outcome: Final answer semantics cannot hide side effects.
- Acceptance criteria: Regression test proves mutating sibling tools do not execute.

### Fix batch resume atomicity

- Description: Mark all batch pauses resumed before injecting answers, or use one DB transaction for claim and append.
- Owner role: Persistence engineer.
- Expected outcome: Duplicate or failed batch resume cannot corrupt conversation state.
- Acceptance criteria: Concurrent duplicate batch resume test produces one answer per pause.

### Fix single resume atomicity

- Description: Couple `MarkResumed` and answer `AppendTurn` through one transaction or a repairable idempotency ledger.
- Owner role: Persistence engineer.
- Expected outcome: A pause cannot be marked resolved without a durable matching tool answer.
- Acceptance criteria: Injected append failure after claim leaves the pause retryable or creates a recoverable resume-injection record.

### Fence conversation sidecar reads

- Description: Store/reconstruct sidecar references from conversation ID and sequence instead of trusting DB paths.
- Owner role: Persistence/security engineer.
- Expected outcome: Corrupted DB rows cannot read arbitrary local files.
- Acceptance criteria: Outside-root, traversal, and symlink tests are rejected.

### Change command-hook failure policy

- Description: Default configured hooks to fail-closed in production or require explicit fail policy.
- Owner role: Security engineer.
- Expected outcome: Security hooks cannot silently allow execution on hook failure.
- Acceptance criteria: Timeout/crash/nonzero hook behavior matches configured policy.

### Reject default object-store credentials in production

- Description: Add profile validation for object-store keys, Garage RPC secret, and development bucket defaults.
- Owner role: Infrastructure engineer.
- Expected outcome: Production deployments cannot use sample secrets.
- Acceptance criteria: `server_production` validation fails with defaults and passes with supplied secrets.

### Fix AG-UI listener health semantics

- Description: Make listener failure fatal or reflected in readiness, and update Compose healthcheck.
- Owner role: Runtime engineer.
- Expected outcome: Orchestrators detect API unavailability.
- Acceptance criteria: Port-conflict test fails startup or readiness.

### Reject ambiguous MCP transports

- Description: Reject managed MCP entries that mix `url` and `command` without a consistent explicit type and trust class.
- Owner role: MCP platform engineer.
- Expected outcome: Remote trust cannot mask local command execution.
- Acceptance criteria: URL+command empty-trust config is blocked and does not call stdio open.

### Scope Web/API data by authenticated identity

- Description: Add owner-aware conversation and approval store methods and make new Web conversations inherit `identityctx.IdentityID(ctx)`.
- Owner role: Web/API security engineer.
- Expected outcome: Provisioned users cannot see or mutate other users' conversations or approvals.
- Acceptance criteria: Two-identity integration test proves isolation for list/get/delete/archive/approval resolve and successful B-owned new chat.

## Short-Term Improvements

### Add production runtime profiles

- Description: Introduce explicit dev/local/hardened/server profiles with validation.
- Owner role: Principal engineer.
- Expected outcome: Operators cannot accidentally run local-trusted defaults in production.
- Acceptance criteria: `aura config validate --profile server_production` reports all unmet requirements.

### Add durable mutating tool ledger

- Description: Require started/succeeded/failed records for mutating tools.
- Owner role: Reliability engineer.
- Expected outcome: Side effects have forensic audit records.
- Acceptance criteria: Mutating tool is blocked in production if ledger reservation fails.

### Add background shell TTL

- Description: Add default TTL, owner/session ID, and kill/cleanup policy for background jobs.
- Owner role: Runtime engineer.
- Expected outcome: Long-running jobs are bounded and attributable.
- Acceptance criteria: TTL expiry records status and terminates process group.

### Bind background shell jobs to sessions

- Description: Replace sequential shell IDs with random IDs and require matching session/actor for poll and kill.
- Owner role: Runtime/security engineer.
- Expected outcome: One conversation cannot guess, read, or terminate another conversation's background job.
- Acceptance criteria: Session B cannot poll or kill a shell started by Session A.

### Harden MCP lifecycle limits

- Description: Add per-server mount timeout, stdio frame cap, bounded close, and process-tree termination.
- Owner role: MCP platform engineer.
- Expected outcome: MCP servers cannot wedge startup, exhaust memory with one frame, or leave child processes.
- Acceptance criteria: Hung mount, oversized frame, blocking HTTP close, and child-process leak tests pass.

### Require explicit MCP trust request bodies

- Description: Reject empty or blank MCP trust bodies and require explicit known class plus non-empty reason.
- Owner role: Governance/API engineer.
- Expected outcome: Blocked custom servers cannot become `trusted_local` through empty-body trust calls.
- Acceptance criteria: Empty body, `{}`, blank reason, and unknown class return 400 without config/audit changes.

### Wire or remove outside-workspace send-file approval

- Description: Implement a send-file resume hook or change the tool message to say the flow is unsupported.
- Owner role: Product/runtime engineer.
- Expected outcome: User approval flow matches actual behavior.
- Acceptance criteria: Approval integration test passes or unsupported path returns deterministic error.

## Medium-Term Architecture Work

### Build ToolGateway

- Description: Centralize policy, approval, sandbox selection, timeout, retry, ledger, and result normalization.
- Owner role: Agent platform architect.
- Expected outcome: All tool calls have a single enforceable authority boundary.
- Acceptance criteria: No tool executes without a recorded policy decision.

### Add workspace and absolute-path grants

- Description: Default file access to workspace root, with explicit time-bound grants for outside paths.
- Owner role: Security engineer.
- Expected outcome: File tools are safe by default in hardened/server profiles.
- Acceptance criteria: Absolute path access requires grant and is logged.

### Harden MCP trust model

- Description: Require explicit trust for remote MCP and migrate/deprecate legacy env config.
- Owner role: MCP platform engineer.
- Expected outcome: Remote and local process MCP servers have auditable trust metadata.
- Acceptance criteria: Empty remote trust is blocked in tests.

### Add capability evaluation suite

- Description: Create scenario tests for shell, filesystem, MCP, memory, pause/resume, error handling, and workflow classes.
- Owner role: QA/agent evaluation engineer.
- Expected outcome: Agent safety and reliability can be tracked across releases.
- Acceptance criteria: CI publishes pass/fail evaluation report.

## Long-Term Industrialization

### Add sandbox backend

- Description: Run shell and local MCP commands in isolated containers or restricted process environments.
- Owner role: Infrastructure/security engineer.
- Expected outcome: Host compromise blast radius is reduced.
- Acceptance criteria: Hardened profile cannot write outside mounted workspace.

### Add production observability pack

- Description: Ship dashboards, alerts, runbooks, and trace sampling defaults.
- Owner role: SRE.
- Expected outcome: Operators can detect and diagnose failures quickly.
- Acceptance criteria: Alert tests and dashboard JSON validation pass in CI.

### Add backup and disaster recovery validation

- Description: Automate backup/restore for Postgres, Neo4j, sidecars, and object storage.
- Owner role: SRE/data engineer.
- Expected outcome: Recovery objectives are measured, not assumed.
- Acceptance criteria: CI or scheduled job performs restore drill with documented RPO/RTO.

### Add architecture decision records

- Description: Record decisions for loop semantics, tool policy, memory provenance, MCP trust, and deployment profiles.
- Owner role: Technical lead.
- Expected outcome: Future work has stable design context.
- Acceptance criteria: Major changes reference an ADR.
