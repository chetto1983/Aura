---
spike: 101
name: aura-adaptive-shadow-e2e
type: integration
validates: "Given Qwen llama.cpp, Granite, Neo4j, and Aura's existing semindex/activelearn seams, a cross-domain shadow stream records chosen-action outcomes without changing the served action and emits promotion evidence"
verdict: VALIDATED
related: [097-qwen-multidomain-reward-surface, 098b-graph-knn-policy, 099-neo4j-outcome-policy-graph, 100-adaptive-policy-governance-stress]
tags: [aura, qwen3.5, llama-cpp, granite, neo4j, shadow-mode, adaptive-intelligence]
---

# Spike 101: Aura adaptive shadow E2E

## What This Validates

This is the integration gate for the proposed method, not another offline replay.
Eight interleaved reasoning, tool, skill, and knowledge turns run through the
exact local Qwen3.5 GGUF. Their prompts are re-embedded live through Aura's
production `documents.EmbeddingClient`; the candidate policy uses Aura's
production `semindex.Ranker`; chosen-action outcomes cross Aura's production
`activelearn.Learner`; and Neo4j stores the decision, alternatives, outcome, and
candidate snapshot.

The served action is computed before and independently from the shadow action.
The graph stores both plus an immutable initial served value, and the acceptance
query requires all eight served values to remain unchanged.

## How to Run

```powershell
$env:NEO4J_PASSWORD = (docker inspect aura-neo4j --format '{{range .Config.Env}}{{println .}}{{end}}' |
  Select-String '^NEO4J_AUTH=').Line.Split('/')[1]
go test ./.planning/spikes/101-aura-adaptive-shadow-e2e
go run ./.planning/spikes/101-aura-adaptive-shadow-e2e
```

Required live services:

- llama.cpp Qwen3.5 on `127.0.0.1:18080`;
- Aura Granite embeddings on `127.0.0.1:8081`;
- Aura Neo4j on Bolt `127.0.0.1:7687`;
- Aura agent-memory MCP on `127.0.0.1:8091`.

## Acceptance

- 8/8 decisions and chosen-action outcomes persisted asynchronously;
- 8/8 served actions byte-identical before/after shadow recommendation;
- `activelearn.Observe` p95 remains sub-millisecond;
- one candidate snapshot reaches `promotion_eligible` but is not activated;
- agent-memory stays healthy;
- all spike-owned Neo4j nodes are removed after evidence is read;
- no Qwen, Granite, Neo4j, or async-learning error.

## Results

**VALIDATED.** The exact Qwen3.5 GGUF completed all eight live turns with no
infrastructure error (7/8 answer/task correctness; the bat-and-ball low-budget
reasoning turn missed, consistent with spike 097's measured action variance).

- Neo4j contained 8 decisions, 8 chosen-action outcomes, and 8/8 unchanged
  served actions before the evidence snapshot was read.
- Aura's `activelearn` core accepted and completed 8/8 writes with zero errors
  and fired 8 refresh callbacks.
- Shadow and served actions disagreed on 4/8 turns. Those disagreements never
  changed the served action.
- Mean balanced live reward was 0.8393.
- `Observe` p95 was below the Windows timer tick (reported as 0.0000 ms); this
  proves no measurable synchronous I/O, not literal zero execution time.
- The candidate snapshot reached `promotion_eligible`; it was not activated.
- Agent-memory TCP health was true before and after the run.
- Cleanup removed all 44 spike-owned Neo4j nodes.

Artifact: `artifacts/shadow-e2e.json`.
