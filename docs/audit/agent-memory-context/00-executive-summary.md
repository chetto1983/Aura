# Executive Summary

## Post-audit P0 remediation update

Both original P0 findings are **REMEDIATED AND LIVE-VERIFIED** as of
2026-07-29:

- **SEC-001:** the Memory MCP now requires short-lived HS256 JWTs with fixed
  issuer, audience, and scope; derives the tenant from the authenticated
  subject; overwrites payload identity; rejects service-subject tool calls; and
  exposes no global resources in authenticated mode.
- **SEC-002:** conversation identity is now `(user_identifier, session_id)`,
  database constrained and atomically created; foreign or ambiguous legacy
  ownership is rejected instead of repaired or reassigned.

Adversarial live tests proved unauthenticated rejection, payload-forgery
resistance, and same-session two-tenant isolation. A paid E2E then drove the
real OpenRouter-backed `LlmAgent` through an actual
`memory__memory_search` call against the rebuilt sidecar and Neo4j, and verified
the seeded marker in both the tool result and final answer.

The scoped P0 remediation score is **10.0 / 10**. The original overall score
below remains the audit-at-cutoff score, and the overall recommendation remains
**NO-GO** because 16 P1 findings are outside this remediation scope. Full
commands and evidence are in
[12-p0-remediation-evidence.md](12-p0-remediation-evidence.md).

## Production-readiness assessment

Aura has several strong defensive building blocks, but the composed Agent
Memory and context system is **NO-GO** for production multi-user autonomous
workloads. The principal problem is not one isolated bug: security scope,
memory transaction semantics, final-request invariants, and health reporting
are enforced in different layers with incompatible assumptions.

The current system can disclose or corrupt memory across principals through the
direct MCP surface, can durably record partial memory mutations as successful,
can silently omit required user context from an LLM request, and can exceed a
model window after the context ladder reports that history fits.

## Five highest-risk findings

1. **[SEC-001](08-findings-register.md#sec-001-unauthenticated-memory-mcp-exposes-global-and-forgeable-tenant-scope), P0:** the sidecar has no transport authentication, exposes global resources, accepts caller-selected identity, and runs with fail-open tenant behavior.
2. **[SEC-002](08-findings-register.md#sec-002-session-claiming-can-cross-user-short-term-memory), P0:** a second principal can claim an existing session identifier, overwrite its owner field, and read the first principal's messages.
3. **[CTX-001](08-findings-register.md#ctx-001-model-only-user-context-substitution-mutates-a-copy), P1:** model-only attachment, pinned-skill, and document context is not placed on the exact LLM request; the focused regression test fails.
4. **[MCP-004](08-findings-register.md#mcp-004-domain-errors-become-successful-idempotent-mcp-results), P1:** partial Neo4j failures are encoded as successful text, accepted by Aura, persisted as completed idempotent results, and excluded from error telemetry.
5. **[PRIV-001](08-findings-register.md#priv-001-production-deprovisioning-retains-agent-memory-and-conversations), P1:** production deprovisioning deletes identity while graph and conversation purgers are unwired, leaving owned memory and PII behind.

## Five strongest existing controls

1. Canonically named memory tools are stamped with the authenticated Aura
   identity and model-provided identity is overwritten
   (`internal/agent/mcptools/bridge.go:123-164`).
2. Generic MCP output and automatic recall are marked and fenced as untrusted
   data (`bridge.go:167-176`; `internal/runner/dynamic_recall.go:118-125`).
3. Mutating MCP calls are not automatically replayed after an ambiguous
   transport failure (`internal/agent/mcptools/bridge_reconnect.go:146-179`).
4. Dynamic recall validates corpus epochs, ordered IDs, retrieval revisions,
   exact placement, and its dynamic-tail budget before exposure
   (`internal/agent/dynamic_tail.go:104-239`).
5. Initial MCP mounts are deterministic, collision-aware, and all-or-nothing;
   the common stdio/HTTP clients cap frames and bodies
   (`cmd/aura/main.go:352-405`; `internal/agent/mcptools/bridge.go:467-545`).

These controls materially reduce risk on their intended paths. They do not
establish authorization at the sidecar boundary, cover aliased memory mounts,
make logical writes atomic, or validate the exact final LLM request.

## Systemic root causes

- **Client shaping is treated as authorization.** Aura stamps identity in the
  canonical bridge, while the server itself authenticates nobody and accepts
  missing or forged identity.
- **Logical success is conflated with transport success.** The Python
  integration returns `{"error": ...}` as normal MCP content and the Go
  gateway completes idempotency on a nil transport error.
- **Logical operations span multiple storage transactions.** Nodes, owner
  edges, validity, embeddings, and relationships can commit independently.
- **Context is budgeted before it exists in final form.** System text, volatile
  hints, tool schemas, hooks, and within-turn tool growth are added after the
  history ladder.
- **Security and behavior policy is keyed by names and optional wiring.**
  Recipe source classification, namespace enforcement, readiness, retention,
  and lifecycle ownership do not share a typed contract.

## Immediate actions

1. Remove network reachability to the Agent Memory MCP from untrusted local and
   sibling-container callers until authenticated, server-derived tenant scope
   exists; disable global resources and fail closed on missing scope.
2. Disable or isolate short-term MCP resources/tools until conversation
   identity is `(principal, session)` and database constrained.
3. Correct and validate the current model-only user-context regression before
   any release.
4. Stop treating text-encoded domain errors as success; gate idempotency
   completion and onboarding completion on typed domain success.
5. Add an exact, post-hook, post-tool-manifest request preflight and preserve
   the protected system prefix and active round as immutable invariants.
6. Wire owner-scoped graph/conversation purge before identity deletion and
   define deletion receipts and retry behavior.

## Readiness scorecard

Scores describe the audited implementation, not the planned design.

| Area | Score | Evidence and main gap | Condition for next level |
|---|---:|---|---|
| Architecture clarity | 6.0 | Clear packages and ownership comments, but host memory has separate lifecycles and the active knowledge client bypasses common MCP transport. | One typed MemoryService and shared lifecycle/policy boundary. |
| MCP contract quality | 4.0 | Bounded transports and careful replay policy; initialize capability validation and typed domain errors are absent. | Versioned typed initialize/result/error contracts and contract tests. |
| Memory correctness | 3.0 | Native graph/vector storage and epoch control exist; writes are multi-transaction, race-prone, and stale preferences remain recallable. | Atomic semantic writes, canonical keys, active-time filtering, reconciliation. |
| Context correctness | 3.0 | Explicit ladder and dynamic-tail checks; current model-only context is dropped, active resume state can be evicted, hooks can rewrite protected state. | Exact-wire invariants and protected active-round behavior. |
| Token-budget safety | 2.0 | Approximate history cap exists; exact request and within-turn growth are unbudgeted and invalid config can disable the guard. | Model-aware final-request preflight on every call. |
| Concurrency safety | 4.0 | Some gateway at-most-once controls; memory dedup and metadata updates are check-then-write and MCP waiters are not cancellable. | Database uniqueness/MERGE, versions, cancellable queues, race tests. |
| Failure resilience | 3.0 | Read-only reconnect and breaker controls exist; partial writes can become durable false success and observer work is untracked. | Typed failure state, atomic/saga recovery, durable bounded workers. |
| Security and privacy | 2.0 | Untrusted-data framing is strong; direct MCP lacks authentication, deprovision retains PII, and a legacy client exposes credentials in argv. | Authenticated service identity, fail-closed scope, verified erasure. |
| Isolation | 1.0 | Aura's canonical bridge scopes calls, but direct resources, session claiming, and recipe aliases bypass the boundary. | Server-derived principal and adversarial two-user tests on every surface. |
| Performance and scalability | 4.0 | Request/body caps exist in the Go client, but sidecar inputs and observer state are unbounded and current-scale benchmarks are invalidated. | Enforced limits and current-model workload benchmarks. |
| Observability | 3.0 | Generic MCP counters/alerts exist; semantic failures, recall quality, purge backlog, and divergence are invisible. | Domain SLIs, functional readiness, tracing, and dependency alerts. |
| Test completeness | 4.0 | Broad CI and some live/race tests; critical two-user, fault-step, hook, reconnect, and semantic-error cases are absent; one focused test currently fails. | Fail-first regression matrix covering all P0/P1 chains. |
| Operability | 4.0 | Bounded startup and generic runbooks; readiness can be green without core memory and retention has no production operator. | Dependency state model, deletion/consolidation jobs, recovery runbooks. |
| Documentation accuracy | 5.0 | Architecture intent is substantial; profile/SSRF, memory surface, caps, and readiness comments diverge from runtime behavior. | Executable contracts and docs generated/checked against configuration. |

Overall: **3.4 / 10** (simple mean, rounded to one decimal).

## Final recommendation

**NO-GO for the complete audited system.** The two P0 isolation paths are now
closed and verified, but the remaining realistic P1 correctness chains still
violate minimum release criteria. A conditional go is appropriate only after
exact-request correctness, typed mutation success, verified erasure, and the
remaining P1 fail-first regression tests are demonstrated in the supported
deployment topology.
