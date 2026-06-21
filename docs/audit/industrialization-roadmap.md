# Industrialization Roadmap

## Phase 0: Stabilization

Priority: P0/P1

Effort: 1-2 weeks

Impact: Prevents the most likely production-blocking safety and correctness failures.

Dependencies: None.

Work:

- Fix `.env.example` destructive shell pattern behavior.
- Reject `text_response` with mutating sibling tools.
- Reorder or transactionally fix `SubmitAnswers`.
- Validate conversation sidecar read paths.
- Default configured command hooks to fail-closed or require explicit policy.
- Reject static object-store credentials in production profile.
- Make AG-UI listener failure affect readiness or process exit.
- Reject ambiguous managed MCP definitions that mix `url` and `command`; require explicit transport-compatible trust.
- Scope AG-UI conversations, approvals, and new conversation ownership to the authenticated identity.

Acceptance criteria:

- New tests fail on old behavior and pass on fixed behavior.
- Production validation fails when defaults are unsafe.
- No mutating sibling tool can run in the same model step as terminal text.
- Batch resume cannot append answers unless pause claim succeeds.
- Mixed MCP transport definitions are blocked before launch.
- Two-identity AG-UI tests prove conversation and approval isolation.

## Phase 1: Observability And Reliability

Priority: P1/P2

Effort: 2-4 weeks

Impact: Makes failures visible and recoverable.

Dependencies: Phase 0 fixes for stable semantics.

Work:

- Add durable tool invocation state machine for mutating tools.
- Add OpenTelemetry spans for LLM, tool, MCP, pause/resume, DB, and scheduler work.
- Add Prometheus alert rules and Grafana dashboards.
- Add background shell TTL, owner/session IDs, and age metrics.
- Add readiness probes for database, listener, migration state, and scheduler state.
- Add sidecar/trace retention metrics and cleanup command.

Acceptance criteria:

- Mutating tool execution has durable started/succeeded/failed records.
- Dashboards show loop error rate, tool timeout rate, queue lag, pause/resume failures, and listener state.
- Background jobs expire by TTL and expose owner/status.
- `/readyz` fails when critical serving dependencies fail.

## Phase 2: Architecture Hardening

Priority: P1/P2

Effort: 4-8 weeks

Impact: Creates a production-grade separation between agent reasoning and host authority.

Dependencies: Phase 0 safety fixes and Phase 1 ledger basics.

Work:

- Introduce `ToolGateway` as the single policy enforcement point.
- Add runtime profiles: `dev`, `local_trusted`, `single_user_hardened`, `server_production`.
- Add workspace path fence and explicit absolute-path grants.
- Add command capability classes and approval requirements.
- Add idempotency keys for mutating tools.
- Convert legacy MCP config into managed trust metadata or production-disable it.

Acceptance criteria:

- Every tool call passes through a policy decision that is logged and testable.
- `server_production` denies host shell/filesystem by default.
- Remote MCP requires explicit trust.
- Mutating tools are idempotent or explicitly non-retryable with durable state.

## Phase 3: Security Hardening

Priority: P1/P2

Effort: 4-6 weeks

Impact: Reduces prompt-injection, privilege, secret, and supply-chain risk.

Dependencies: ToolGateway and runtime profiles.

Work:

- Add subprocess sandbox backend for shell/MCP local commands.
- Add network egress policy.
- Add secret redaction for tool output and traces.
- Add encrypted trace/sidecar storage option.
- Add security regression suite for prompt injection and tool policy bypass.
- Add SBOM, dependency vulnerability, and license checks.

Acceptance criteria:

- Prompt-injected shell/file/network requests are denied in production tests.
- Sandbox prevents writes outside workspace in hardened profile.
- Secret-like values are redacted before persistence.
- CI blocks high-severity known vulnerabilities unless waived.

## Phase 4: Scalability And Production Operations

Priority: P2

Effort: 6-10 weeks

Impact: Prepares the runtime for higher concurrency and operational support.

Dependencies: Phase 1 observability and Phase 2 policy architecture.

Work:

- Add queue/backpressure model for long-running agent tasks.
- Add distributed lock or lease strategy for schedulers/workers.
- Add backup/restore automation with RPO/RTO targets.
- Add load and chaos tests.
- Add production object-store topology guidance and validation.
- Add deployment manifests with TLS/auth/health/metrics defaults.

Acceptance criteria:

- Load tests define supported concurrency and degradation behavior.
- Chaos tests cover DB outage, MCP timeout storm, object-store outage, and process kill during write.
- Backup restore is tested and documented.
- Production deployment fails validation when using development storage topology.

## Phase 5: Long-Term Maintainability

Priority: P2/P3

Effort: ongoing

Impact: Keeps the system understandable and evolvable.

Dependencies: Stabilized architecture.

Work:

- Add architecture decision records for loop semantics, memory provenance, tool policy, and deployment profiles.
- Add ownership map for agent loop, tools, MCP, memory, Web UI, and infra.
- Add generated API/tool contract docs.
- Add reference-comparison notes for patterns adopted from `D:\tmp`.
- Add release readiness checklist.

Acceptance criteria:

- New tool types require policy, ledger, timeout, and test declarations.
- ADRs exist for major architectural choices.
- Release checklist includes security, load, backup, observability, and rollback gates.
