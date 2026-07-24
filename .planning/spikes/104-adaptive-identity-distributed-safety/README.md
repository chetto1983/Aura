---
spike: 104
name: adaptive-identity-distributed-safety
type: integration
verdict: VALIDATED_SAFETY_MECHANISM
tags: [privacy, identity, rollback, multi-instance, partitions]
---

# Spike 104: Identity and distributed rollback safety

## Claim

Adaptive evidence remains owner-scoped through PostgreSQL and Neo4j, deletion cannot
be undone by recreating an identity UUID, and a rollback in one serving process is
observed by every other process before its next adaptive action.

## Design

PostgreSQL is authoritative:

- adaptive rows carry identity foreign keys and owner isolation;
- permanent identity tombstones reject UUID resurrection;
- `adaptive_policy_state` is a singleton epoch with compare-and-swap transitions;
- each policy decision reads the current epoch; a PostgreSQL partition returns a
  baseline-only gate instead of a cached adaptive action.

Neo4j is a replayable private projection:

- Aura reuses agent-memory's existing `(:User {identifier})` ownership anchor;
- private `AdaptiveEpisode` and `AdaptiveEvent` labels are not queried by the
  LLM-facing agent-memory MCP;
- deprovisioning raises a permanent tombstone in a journaled `adaptive_fence` leg
  before the distinct `adaptive_graph` purge leg;
- the projector checks deletion after graph write and purges a late projection that
  raced with the deletion fence;
- the adaptive purge deletes neither the shared `User` nor memory facts.

## Live proofs

```powershell
go test -tags db_integration ./internal/adaptive `
  -run "TestStoreRejectsRecreatedDeletedOwner|TestPolicyServiceBindsEvidenceCapabilityAuditAndDistributedRollback|TestProjectorDeletionFenceClosesLiveTOCTOUInterleaving"

$env:AURA_MCP_NEO4J_CYPHER_BIN = (Resolve-Path scripts/mcp-neo4j-cypher-docker.cmd).Path
$env:AURA_NEO4J_BOLT_URL = "bolt://aura-neo4j:7687"
$env:AURA_AGENT_MEMORY_MCP_URL = "http://127.0.0.1:8091/mcp/"
go test -tags "adaptive_live db_integration" ./internal/adaptive `
  -run TestAdaptiveProjectorLiveReusesMemoryUserWithoutMemoryRecallLeak

go test ./internal/agui ./cmd/aura
```

Validated cases:

- deleting an identity cascades its PostgreSQL adaptive rows;
- recreating the same UUID cannot append new evidence;
- two policy-controller instances observe the same audited rollback epoch;
- a stale policy-service transition cannot reactivate an older policy;
- canary/active transitions require `adaptive.manage`, a configured production-model
  match, evaluated gate evidence, and immutable transition audit;
- policy-store unavailability fails closed to the baseline;
- agent-memory context recall does not return an adaptive marker or event ID;
- purge removes only the private adaptive projection and retains the shared user;
- a controlled deletion/projector interleaving leaves no resurrected graph node and
  does not acknowledge the raced outbox event;
- a graph purge failure stops the saga before hard identity deletion and is retried.

## Measured rollback bound

The implementation uses no serving cache for the critical policy epoch. The bound is
one authoritative PostgreSQL read before the next adaptive action plus that query's
latency. A partition cannot extend stale adaptive serving because the read failure
disables adaptation.

## Verdict boundary

`VALIDATED_SAFETY_MECHANISM`: isolation, deletion, distributed rollback, and
agent-memory coexistence are proved on Aura's live infrastructure. It does not prove
that any learned policy improves production quality.
