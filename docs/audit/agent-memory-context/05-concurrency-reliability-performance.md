# Concurrency, Reliability, and Performance

## Overall assessment

Aura's Go MCP layer is conservative about ambiguous mutation replay and has
bounded frame/body handling. The memory domain below it does not provide an
atomic semantic transaction or canonical concurrency key for facts/preferences.
The result is a failure-amplification chain in which partial state can be
reported, cached, and monitored as success.

## Concurrency model and races

### Memory writes

Model tool batches may execute concurrently
(`internal/agent/llm_agent_dispatch.go:88-110`) with a default pool of four.
Facts and preferences search for duplicates, then issue a separate create. The
schema provides UUID uniqueness but no owner-scoped canonical semantic key for
those types.

Concurrent identical writes can both observe no match and create two nodes.
Concurrent metadata updates read/merge/write without a version precondition.
Concurrent short-term writers can select the same tail and create branching
`NEXT_MESSAGE` edges; message reads order by timestamp without a stable
tie-break.

See [CON-001](08-findings-register.md#con-001-concurrent-memory-writers-can-duplicate-branch-and-lose-updates).

### MCP client serialization

Both common clients check context, then block on `sync.Mutex.Lock`. Deadline
expiration while waiting does not unblock the caller, and HTTP close creates
its timeout only after acquiring the same lock. A hung/unbounded first call can
retain later callers and shutdown despite their deadlines
([MCP-002](08-findings-register.md#mcp-002-mcp-session-lock-wait-ignores-cancellation)).

### Observer tasks

Short-term `store_message` launches untracked `asyncio.create_task` work.
Observer state is an unbounded dictionary keyed only by session ID; observations
and reflections append without a bound and reset has no located production
caller. Restart loses work and session collisions can mix summaries
([REL-001](08-findings-register.md#rel-001-short-term-observer-work-is-untracked-and-unbounded)).

## Transaction and partial-failure behavior

Each Neo4j `execute_write` is atomic with its corpus-epoch increment. A logical
memory operation uses several such calls:

- create node;
- set validity/metadata;
- link owner;
- link subject/entity or update embedding/edges.

A failure after an early commit creates an indexed orphan or partially updated
node. Owner-scoped dedup then cannot see an orphan, so retry can create another
node. This is [MEM-001](08-findings-register.md#mem-001-logical-memory-mutations-are-not-atomic).

Integration methods catch exceptions and return error-shaped content. That
makes the partial operation appear successful to the bridge and operation
registry. The 30-day replay record can prevent repair, while health/alerts stay
green. See [MCP-004](08-findings-register.md#mcp-004-domain-errors-become-successful-idempotent-mcp-results)
and [OBS-001](08-findings-register.md#obs-001-semantic-memory-outages-remain-healthy-and-unalerted).

## Timeout, cancellation, retry, and restart

Positive behavior:

- mount retry is bounded and transient-aware;
- read-only reconnect/replay is explicit;
- mutating calls are not replayed after ambiguous transport send;
- HTTP session expiration recovery distinguishes idempotency-scoped mutations;
- common transports cap frames/bodies and bound most close paths;
- process-owned MCP mount shutdown is aggregated and fail-soft.

Gaps:

- lock acquisition ignores waiting caller cancellation;
- memory semantic timeout state is not distinguished from success/partial;
- automatic recall uses a separate fresh session and bypasses mounted breaker
  state;
- reconnect does not add/remove registry tools;
- short-term background tasks are not drained;
- core-memory loss does not make readiness degraded.

## Backpressure and denial-of-service bounds

Aura caps generic tool-result previews and spills larger results to retrievable
sidecars. The memory MCP application schemas themselves have no meaningful
length/range limits for query, content, collections, search limit, or
`max_items`, and Compose declares no sidecar CPU/memory limit. A reachable local
caller can impose large embedding, reranking, Neo4j, LLM-extraction, and
response workloads before Aura's result cap is relevant.

See [SEC-003](08-findings-register.md#sec-003-agent-memory-inputs-and-results-lack-application-level-bounds).

## Context performance and storage growth

Every turn loads and tokenizes the full PostgreSQL conversation before dropping
old rounds. `HistoryHardCapTurns` has no runtime consumer and L2.5 does not
compact/delete persisted rows. Long-lived sessions therefore have O(N)
preprocessing and unbounded turn/rot-event storage
([CTX-006](08-findings-register.md#ctx-006-advertised-history-hard-cap-is-not-wired)).

Within a turn, up to 25 tool-loop steps and 30,000-byte previews can accumulate
after the history cap. Final synthesis can send the grown history without
preflight. This is both a correctness and latency/cost risk.

## Performance evidence

Current production-scale retrieval performance is
[PERF-001, NOT ASSESSABLE](08-findings-register.md#perf-001-current-production-scale-memory-performance-is-not-assessable).
`docs/aura-quality-snapshot.md:25` invalidates the 100K GraphRAG figure after an
embedding-model change. Memory recall figures at lines 47-58 are advisory,
small-corpus measurements from 2026-06-12.

Required evidence before a performance go:

- current embedding/reranker/model versions;
- representative tenant/corpus distribution;
- cold and warm p50/p95/p99 for reads and writes;
- 10-100 concurrent sessions with mixed reads/writes;
- Neo4j pool/index saturation and sidecar CPU/RSS;
- recall, nDCG, stale-result, and empty-recall rates;
- automatic recall session churn versus a shared managed transport;
- large-context tokenizer, assembly, and provider latency.

## Failure scenarios to test

1. Fail after each graph sub-write; assert rollback or a durable incomplete
   operation that reconciliation repairs.
2. Barrier-start 50 identical fact/preference writes; assert one active node.
3. Hold the first MCP call; expire a second caller and close concurrently;
   assert deadlines and no goroutine leak.
4. Kill the sidecar with observer tasks in flight; restart and verify durable
   bounded recovery or explicit loss accounting.
5. Fail Neo4j, embedder, and reranker independently; verify readiness, domain
   metric, trace, and alert identify the dependency.
6. Grow tools/results/history to the exact model limit; prove no oversized
   request reaches the provider.
