---
spike: 103
name: adaptive-runner-outbox-proof
type: integration
verdict: VALIDATED_MECHANISM
tags: [runner, postgres, outbox, neo4j, idempotency]
---

# Spike 103: Real Runner and transactional outbox proof

## Claim

Aura's real Runner persistence control points can atomically commit terminal tool
results and assistant turns with immutable adaptive events in PostgreSQL, while a
separate idempotent projector copies those events to Neo4j.

This spike validates mechanism and durability. It does not prove answer-quality gain
or authorize adaptive serving.

## Implemented control points

- `internal/runner/runner_persist.go`: real tool-end and final-assistant persistence.
- `internal/adaptive/turn_committer.go`: one owner-scoped PostgreSQL transaction for
  the conversation turn, cache metric, aggregate sequence, and adaptive outbox event.
- `internal/adaptive/hook.go`: Aura's actual `agent.Hook` points before model and tool
  execution; prompts and raw tool arguments are excluded.
- `internal/adaptive/projector.go`: leased, ordered, retryable projection to Neo4j.

Operational tool completion and assistant completion are recorded with
`quality_observed:false`. Plumbing success never becomes a fabricated quality reward.

## Falsifiable tests

```powershell
go test ./internal/adaptive ./internal/runner
go test -tags db_integration ./internal/adaptive
$env:AURA_MCP_NEO4J_CYPHER_BIN = (Resolve-Path scripts/mcp-neo4j-cypher-docker.cmd).Path
$env:AURA_NEO4J_BOLT_URL = "bolt://aura-neo4j:7687"
$env:AURA_AGENT_MEMORY_MCP_URL = "http://127.0.0.1:8091/mcp/"
go test -tags "adaptive_live db_integration" ./internal/adaptive `
  -run TestAdaptiveProjectorLiveReusesMemoryUserWithoutMemoryRecallLeak
```

Validated cases:

- exact event retries create one row and one turn;
- same UUID with changed immutable payload hard-fails and rolls back the turn;
- 40 concurrent distinct events receive gap-free per-aggregate sequence `1..40`;
- multiple projector workers preserve per-aggregate head-of-line ordering while
  different aggregates progress concurrently;
- an expired lease is reclaimable and a stale worker cannot acknowledge it;
- retry exhaustion enters a durable dead-letter state;
- a live graph-outage interleaving persists attempt one as pending and attempt two as
  dead-letter with a typed failure timestamp;
- a Neo4j outage cannot create graph-only evidence or partially commit a turn;
- oversized spilled turns commit atomically, and a failed companion event removes the
  staged blob;
- projection replay is idempotent.

## Verdict boundary

`VALIDATED_MECHANISM`: the real persistence seams and failure invariants work. Adaptive
action selection remains shadow-only until spike 102 generalization is replaced by
real-model production evidence and spike 105's canary gate passes.
