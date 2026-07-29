# Prioritized Roadmap

No calendar estimates are supplied. Effort is relative engineering scope.

## Phase 1 — Containment of P0/P1 risks

**2026-07-29 status:** the SEC-001 and SEC-002 work in this phase is complete
and live-verified. Phase 1 as a whole remains open because its P1 findings are
outside the P0 remediation scope. Evidence:
[12-p0-remediation-evidence.md](12-p0-remediation-evidence.md).

- Objective: stop reachable cross-user paths and current exact-context failure.
- Findings addressed: SEC-001, SEC-002, MCP-001, CTX-001, MEM-002, ARC-001.
- Dependencies: deployment owner, supported topology inventory, identity owner.
- Deliverables: network/profile containment; global/short-term resources
  disabled; memory alias rejected/canonicalized; server rejects absent scope;
  model-context copy defect corrected; stale preference filtered; knowledge
  password removed from argv.
- Acceptance criteria: unauthenticated/forged/same-session two-user calls fail;
  current focused runner test passes; old superseded preference is absent.
- Validation scenarios: focused Go runner test; two-identity live resource/tool
  matrix; CLI/governance alias test; argv inspection; exact recall wire test.
- Estimated effort: M for containment, L if authentication/schema is completed
  in this phase.
- Rollback considerations: feature-disable direct/short-term surfaces, retain
  previous client adapter without restoring insecure reachability; graph
  constraint migration needs export.
- Residual risk: write atomicity, false-success, token overflow, and incomplete
  erasure remain until later phases.

## Phase 2 — Correctness and data-integrity hardening

- Objective: make logical memory operations atomic, active-time correct, and
  explicitly recoverable.
- Findings addressed: MEM-001, MCP-004, CON-001, PRIV-001, MEM-003.
- Dependencies: authenticated owner key, operation schema, deletion/retention
  policy, read-only existing-data assessment.
- Deliverables: semantic transaction/outbox; typed outcomes; canonical
  fact/preference keys; versions; active predicates; transactional onboarding;
  owner purge adapter/receipt; tenant-safe consolidation.
- Acceptance criteria: step failure never leaves invisible orphan/false success;
  concurrent identical writes produce one active node; deprovision proves zero
  owner-reachable data.
- Validation scenarios: graph fault injection after every sub-write; barrier
  concurrency; onboarding semantic-error test; crash/retry purge with shared
  entity; two-owner consolidation.
- Estimated effort: XL.
- Rollback considerations: shadow canonical keys, export collision set, dual
  read before constraints, reversible writer flag; never undo tombstones before
  data-risk review.
- Residual risk: live existing corruption remains until approved repair.

## Phase 3 — Concurrency and failure resilience

- Objective: bound waiting/work and make restart behavior explicit.
- Findings addressed: MCP-002, REL-001, CON-001, ARC-003.
- Dependencies: semantic operation IDs and shared MemoryService design.
- Deliverables: context-cancellable request queue; bounded close/drain; durable
  tenant/session observer queue or production removal; optimistic versions and
  message sequence; managed recall transport/pool.
- Acceptance criteria: all waiters honor deadlines; no goroutine/process/task
  leak; observer state is bounded and restart semantics are proved; recall
  survives restart through one health/breaker contract.
- Validation scenarios: blocked-call/short-waiter/concurrent-close race+goleak;
  SIGTERM/restart under observer load; 100 concurrent recalls; out-of-order
  message tests.
- Estimated effort: L.
- Rollback considerations: keep old transport behind canary flag; drain new
  queue before rollback; operation records remain readable.
- Residual risk: performance capacity remains unknown until Phase 7 evidence.

## Phase 4 — MCP contract stabilization

- Objective: one explicit, versioned, stable MCP lifecycle and result contract.
- Findings addressed: MCP-004, MCP-005, MCP-006, MCP-007, MCP-001, ARC-001.
- Dependencies: typed identity/policy and semantic outcome.
- Deliverables: common initialize/capability model; supported version table;
  typed results/errors; registry generations; complete CLI mutation inventory
  and context propagation; shared hardened subprocess/session substrate.
- Acceptance criteria: incompatible servers fail at mount; registry exactly
  matches accepted generation; every mutation has operation identity and
  deterministic retry semantics.
- Validation scenarios: protocol matrix; malformed/missing capability; reconnect
  add/remove/rename/collision; CLI replay/conflict and `_meta`; transport parity.
- Estimated effort: L.
- Rollback considerations: negotiate old protocol through explicit adapter;
  pin prior registry generation during canary; never interpret legacy
  error-shaped content as success.
- Residual risk: third-party MCP compatibility needs a published support policy.

## Phase 5 — Context quality and token efficiency

- Objective: protect exact final request structure and fit for every model call.
- Findings addressed: CTX-002, CTX-003, CTX-004, CTX-005, CTX-006, MEM-004,
  MEM-005.
- Dependencies: model tokenizer/capability inventory, hook contract decision,
  provenance schema.
- Deliverables: immutable system/active-round model; constrained hook deltas;
  post-hook/post-tool exact preflight; validated config; round checkpoints or
  bounded queries; structured provenance; aggregate recall top-K.
- Acceptance criteria: protected bytes/state always survive; no above-window
  request reaches provider; long thread work is bounded; recall cap/provenance
  match configuration/evidence.
- Validation scenarios: malicious hooks; active resume overflow property tests;
  large schemas and 25-step tool loop; final synthesis; >N-turn load; reranker
  on/off exact wire.
- Estimated effort: XL.
- Rollback considerations: run exact gate in shadow first; reject only after
  tokenizer parity; retain raw history and old read adapter.
- Residual risk: summary/checkpoint quality needs separate evaluation if used.

## Phase 6 — Security and privacy hardening

- Objective: complete defense in depth after containment.
- Findings addressed: SEC-001–003, MCP-003, PRIV-001, ARC-001, CTX-003,
  MEM-004.
- Dependencies: deployment PKI/token service, privacy policy, capacity policy.
- Deliverables: authenticated service identity; privileged admin API; per-hop
  SSRF policy; body/field/rate/resource limits; PII/secret handling; audited
  provenance; verified deletion and debug retention.
- Acceptance criteria: adversarial tests cannot cross tenant/session/surface;
  strict topology allows only declared endpoints; oversized/abusive requests
  stay bounded; erasure and audit policy are demonstrable.
- Validation scenarios: auth spoof/replay; redirect/private/DNS rebinding;
  oversized property/load; DLP/redaction; deletion receipt audit.
- Estimated effort: L.
- Rollback considerations: security enforcement may need client-compatibility
  staging, but rollback cannot restore unauthenticated tenant APIs.
- Residual risk: model-semantic prompt-injection resistance still needs ongoing
  adversarial evaluation.

## Phase 7 — Testing and observability completion

- Objective: make semantic failures visible and every P0/P1 release-gated.
- Findings addressed: OBS-001, TST-001, ARC-002, PERF-001 and all validation
  dependencies.
- Dependencies: typed outcomes, functional readiness, reproducible live test
  environment.
- Deliverables: fail-first unit/contract/live/E2E matrix; domain SLIs/traces;
  required/degraded readiness; alerts/runbooks; existing-data read-only audit;
  current-model load/quality benchmark.
- Acceptance criteria: P0/P1 suite is mandatory green; injected dependency
  failure maps to correct readiness/metric/alert/runbook; SLO/quality thresholds
  pass on supported topology.
- Validation commands/scenarios: `go test -race` targeted packages; vendored
  pytest including previously excluded live tests; two-user E2E; chaos/fault
  matrix; benchmark artifact with versions/seeds/config.
- Estimated effort: L.
- Rollback considerations: alerts can stage record-only; readiness degradation
  must remain truthful; benchmark changes no production state.
- Residual risk: benchmark representativeness requires periodic review.

## Phase 8 — Strategic architecture evolution

- Objective: remove parallel lifecycles and make memory/context one governed
  product capability.
- Findings addressed: ARC-001–003, CTX-006, MEM-003–005, PERF-001.
- Dependencies: prior correctness/security gates and stable contracts.
- Deliverables: process-owned MemoryService; unified host/model adapters;
  versioned checkpoints/provenance; bounded cache by corpus epoch; optional
  adaptive recall promotion; capacity-driven partition/read strategy.
- Acceptance criteria: one identity/policy/lifecycle/health contract; no legacy
  knowledge/raw recall path; measured quality/latency improves without
  correctness regression.
- Validation scenarios: adapter parity, migration/canary/rollback, long-running
  soak, recall shadow comparison, disaster recovery exercise.
- Estimated effort: XL.
- Rollback considerations: versioned adapters and dual-read canary; preserve
  exported data and operation journal; remove legacy only after zero-use proof.
- Residual risk: adaptive relevance and long-horizon memory conflict remain
  empirical product problems requiring continuous evaluation.
