# Industrialization Roadmap

## Phase 0: Stabilization

Priority: P1

Effort: 1-2 weeks

Impact: Prevent the highest-risk production failures.

Dependencies:

- Agreement on deployment profile: local-only, remote single-tenant, or multi-tenant.

Work:

- Disable model-authored skill auto-activation for production profile.
- Make `FSWrite` atomic.
- Enforce terminal-tool exclusivity.
- Validate `InvocationContext` at `LlmAgent.Run` entry.
- Add background shell TTL and owner fields.

Acceptance criteria:

- All P1 findings have tests.
- Remote/server profile cannot access shell/full FS/skill activation.
- Mixed terminal plus mutating tool is rejected.

## Phase 1: Observability And Reliability

Priority: P1/P2

Effort: 2-4 weeks

Impact: Make failures visible and recoverable.

Dependencies:

- Metrics naming conventions and deployment monitoring target.

Work:

- Add health/readiness endpoints.
- Add ledger outbox/retry.
- Add dependency state reporting for DB, LLM, MCP, embedder, scheduler, sidecar FS, and tracing.
- Add dashboards and alerts.
- Fail fast on all malformed config, including max parallel tools.

Acceptance criteria:

- Operators can tell whether the agent is healthy, degraded, or unsafe to serve traffic.
- Tool ledger failures are queued or explicitly marked degraded.

## Phase 2: Architecture Hardening

Priority: P1/P2

Effort: 4-8 weeks

Impact: Move from conventions to enforceable runtime contracts.

Dependencies:

- Execution policy model and identity/session propagation.

Work:

- Implement `ExecutionPolicy`.
- Add capability profiles.
- Make registry snapshots immutable.
- Add idempotency contract for mutating tools.
- Add deterministic memory recall/write middleware.

Acceptance criteria:

- Every tool call is evaluated before execution.
- Mutating tools declare idempotency behavior.
- Memory recall can be tested without relying on model choice.

## Phase 3: Security Hardening

Priority: P1/P2

Effort: 4-10 weeks

Impact: Establish production trust boundaries.

Dependencies:

- Chosen sandbox approach per OS/deployment.

Work:

- Add sandboxed shell runner and workspace-jail FS adapter.
- Add network egress policy.
- Add MCP local policy manifests.
- Encrypt or externally secure sidecar storage.
- Add production guardrails for reasoning traces.

Acceptance criteria:

- Remote profile cannot read/write outside workspace even if the model requests it.
- Shell commands execute with enforced filesystem/network permissions.
- Prompt injection tests cannot escalate capability.

## Phase 4: Scalability And Production Operations

Priority: P2

Effort: 6-12 weeks

Impact: Support sustained production usage.

Dependencies:

- Durable queue/outbox and health model.

Work:

- Add global and per-tenant concurrency limits.
- Add queueing/backpressure for turns and tool batches.
- Add rate limiting per tool/provider.
- Add storage retention and sweeper policies.
- Add load/soak test pipeline.

Acceptance criteria:

- System sheds load predictably.
- Sidecar and ledger storage growth are bounded.
- Provider rate limits do not cascade into process failure.

## Phase 5: Long-Term Maintainability

Priority: P2/P3

Effort: ongoing

Impact: Keep the agent understandable and evolvable.

Dependencies:

- Architecture decision records and ownership boundaries.

Work:

- Document trust model and deployment profiles.
- Keep skill governance comments aligned with code.
- Add package-level ownership docs.
- Add mutation and property tests for loop invariants.
- Maintain a production-readiness checklist for new tools.

Acceptance criteria:

- New tool additions require policy, risk tier, timeout, idempotency, observability, and tests.
- Architecture docs match live behavior.

