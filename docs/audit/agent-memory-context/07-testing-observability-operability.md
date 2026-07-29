# Testing, Observability, and Operability

## Overall assessment

Aura has broad CI, race testing, a rebuilt vendored memory image, generic MCP
metrics, and useful transport runbooks. The highest-risk semantic and
cross-plane behaviors are not protected. A sidecar can return errors for every
memory call while TCP health, MCP error counters, and readiness remain green.

## Existing test controls

- CI rebuilds the Agent Memory image from the checked-out vendored source.
- Vendored pytest and a live sidecar stage exist.
- Go race tests cover substantial Aura code.
- Frame/body, transport recovery, mutation replay, schema collision, dynamic
  recall metadata, context boundary, and ownership-query regressions have
  focused tests.
- `tests/test_scoped_query_where_placement.py` provides a package-wide AST guard
  for a previously dead ownership predicate.
- Dynamic recall tests validate exact placement/bytes, epoch coherence, ordered
  IDs, revisions, and limits.

## Current test failure

The lead ran:

```powershell
go test ./internal/runner -run '^TestTurnWithModelUserMessagePersistsVisibleTextAndSendsModelContext$' -count=1
```

with `GOCACHE` isolated under `D:\tmp`. It failed because the LLM request did
not include the model-context user message. This confirms CTX-001 against the
audited working tree.

## Critical missing regression coverage

Repository/CI searches did not locate automated protection for:

- unauthenticated/global MCP resources and forged identity;
- same-session/two-user short-term takeover;
- aliased memory recipe scoping/hiding;
- concurrent first-write and identical fact/preference dedup;
- failure after each logical graph sub-write;
- text-encoded semantic error versus gateway idempotency completion;
- production deprovision graph/conversation erasure;
- stale superseded preference on exact final LLM wire;
- malicious hook removal/reordering of protected request state;
- exact final token count with system + framing + tools + hooks + tool growth;
- MCP lock waiter cancellation and concurrent close;
- reconnect tool add/remove/rename generations;
- observer shutdown drain, cap, and tenant/session collision;
- semantic dependency failure reflected in readiness/metrics/alerts.

CI serializes live graph writes with `-p 1`, which avoids rather than tests key
concurrency behavior. Three live Python tests are excluded from the normal
pytest selection with no separate invocation found. See
[TST-001](08-findings-register.md#tst-001-critical-cross-plane-behaviors-lack-automated-protection).

## Required test pyramid

### Unit/property/fuzz

- canonical owner-scoped memory keys and normalization;
- active-time/supersession query predicates;
- context structural invariants and exact token accounting;
- hook adversarial transformations;
- input size/range schemas;
- deterministic ranking tie-break and provenance formatting;
- arbitrary tool set reconnect diffs;
- config invariants for context/output reservations.

### Contract

- typed MCP initialize version/capability matrix;
- typed success/error/partial/indeterminate result envelope;
- authenticated principal propagation and payload-spoof rejection;
- all tool/resource schemas and model-visible/host-only surface snapshots;
- CLI/onboarding/recall behavior through one MemoryService contract.

### Live integration

- two identities across every CRUD/search/resource/session operation;
- atomic failure injection after every graph step;
- 10-100 concurrent identical/conflicting mutations;
- sidecar/Neo4j/embedder/reranker restart and degradation;
- deprovision crash/retry/receipt/shared-entity preservation;
- exact-wire context tests with real tokenizer and tool manifest.

### End-to-end

- provision -> learn -> retrieve -> correct -> recall -> delete;
- attachment/pinned skill/document context appears exactly once on the provider
  request while only visible text persists;
- memory outage produces the intended degraded product state and alert;
- supported deployment topology enforces network/auth/rate limits.

## Current observability

Generic controls include:

- MCP call/error/duration counters;
- timeout/error ratio rules and alerts;
- structured source/trust on ToolResult;
- corpus epoch/retrieval metadata in dynamic recall's adaptive ledger;
- context-rot warning/event;
- transport-oriented LLM/tools runbooks.

Missing domain observability:

- semantic `{"error": ...}` outcomes;
- logical memory mutation ID and final state;
- partial/orphan/reconciliation counts;
- owner-scoped read/write/update/delete audit events;
- retrieval empty/stale/quality/reranker-degraded metrics;
- corpus/index/ownership divergence checks;
- recall fallback reason and mounted tool generation;
- observer queue/task/state size and shutdown loss;
- deletion/retention/consolidation backlog and receipts;
- exact request tokens by source, reserve, and discarded rounds;
- protected-prefix/current-round invariant violations.

`compose.yaml` health opens a TCP socket only. Aura readiness probes PostgreSQL
and Neo4j but not the memory MCP contract, embedder, reranker, or required tool
generation. This is
[OBS-001](08-findings-register.md#obs-001-semantic-memory-outages-remain-healthy-and-unalerted)
and [ARC-002](08-findings-register.md#arc-002-readiness-excludes-the-default-on-memory-capability).

## Target telemetry

Use bounded, non-PII dimensions:

- `memory_operation_total{kind,outcome}` with outcomes
  `success|rejected|partial|indeterminate|error`;
- `memory_reconciliation_backlog`, `memory_orphan_detected_total`;
- `memory_retrieval_items`, `memory_retrieval_empty_total`,
  `memory_retrieval_stale_total`, latency histograms by stage;
- `memory_authz_denied_total{surface}`;
- `memory_purge_backlog`, `memory_purge_age`, deletion receipt status;
- `mcp_server_generation`, required tool count, breaker/readiness state;
- `context_tokens{source}`, exact total/reserve/available, eviction rounds;
- `context_invariant_violation_total{invariant}`;
- `observer_queue_depth`, active tasks, evictions, shutdown incomplete;
- trace correlation using request/conversation/operation IDs, never raw memory.

Functional readiness should perform an authenticated, bounded synthetic
read/contract check and separately expose degraded versus unready state.

## Operability and runbooks

Required runbooks:

1. memory semantic outage with healthy transport;
2. cross-tenant isolation alert and containment;
3. partial-write/reconciliation backlog;
4. deletion saga stuck before identity deletion;
5. stale/duplicate memory investigation;
6. context exact-budget/invariant rejection;
7. MCP reconnect generation mismatch;
8. observer backlog/restart;
9. credential exposure rotation for legacy knowledge subprocess;
10. safe read-only graph audit and repair approval workflow.

Every runbook should name the SLI, alert, query/dashboard, safe containment,
escalation owner, data-risk decision, recovery proof, and rollback boundary.

## Operational release evidence still required

- Actual deployment network policy and sidecar authentication configuration.
- Runtime profile and `AURA_MCP_SSRF_ENFORCE` state by supported topology.
- Functional readiness and domain alert failure-injection output.
- Live two-user isolation and deprovision erasure results.
- Current-model performance/retrieval quality benchmark.
- Read-only existing-data audit for cross-owner conversations, orphans,
  duplicates, stale preferences, and exposed debug content.
