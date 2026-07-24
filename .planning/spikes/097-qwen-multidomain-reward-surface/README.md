---
spike: 097
name: qwen-multidomain-reward-surface
type: standard
validates: "Given Qwen3.5-2B on llama.cpp plus Granite and Neo4j, when reasoning/tool/skill/knowledge routing alternatives are executed repeatedly, then the benchmark obtains a real quality-cost counterfactual surface rather than synthetic labels"
verdict: VALIDATED
related: [053-reasoning-classifier-active-learning, 057-toolselection-oracle-signal, 058-unified-embedding-index, 095-llama-cpp-reasoning-effort-wire-contract]
tags: [adaptive-learning, qwen3.5, llama-cpp, tools, skills, graphrag]
---

# Spike 097: Qwen multi-domain reward surface

## What This Validates

The proposed adaptive policy is only warranted if different actions win for
different contexts. This spike executes the exact local Qwen3.5 2B Q4_K_M model
over four decision domains:

- reasoning effort: off / 256-token thinking / 768-token thinking;
- tool routing: Granite semantic top-1 / Qwen over semantic top-3 / Qwen over
  the full tool catalog;
- skill routing: the same three routing strategies over a skill catalog;
- knowledge routing: no retrieval / Neo4j vector top-1 / vector seed plus graph
  expansion.

Every scenario/action pair is executed in a deterministic randomized order on
one inference slot. Qwen runs at temperature zero, so the primary pass executes
each cell once; repeated latency samples are reserved for the selected-policy
stress spike instead of duplicating deterministic answers. This is
full-information only for
measurement. Spike 098 replays it while revealing only the chosen action's
reward to the learner, which preserves the production bandit constraint while
letting the benchmark calculate true counterfactual regret.

## Research

| Approach | Evidence | Pros | Cons | Status |
|---|---|---|---|---|
| Static/centroid policy | Aura spikes 052/053/058 | Tiny, deterministic, already shipped | Optimizes labels rather than measured outcomes | Baseline |
| Contextual bandit | [BaRP](https://arxiv.org/abs/2510.07429), [MixLLM](https://arxiv.org/abs/2502.18482) | Learns from chosen-action feedback; supports quality/cost preferences | Needs safe exploration and logged propensities | Candidate |
| Correlation-aware surrogate bandit | [Correlation-Aware Contextual Bandits](https://arxiv.org/abs/2607.09015) | Can use semantic/graph priors while remaining robust to a bad surrogate | Very new; must be reproduced locally | Candidate |
| Neo4j GDS model pipeline | [Neo4j GDS ML pipelines](https://neo4j.com/docs/graph-data-science/current/machine-learning/machine-learning/) | Strong offline feature/model catalog | Beta/alpha surfaces and batch orientation make it a poor hot-path learner | Offline feature candidate |

**Chosen experiment:** create a measured reward surface with the actual model,
then compare policies in paired seeded replay. This avoids judging a router on
synthetic labels or on the same LLM's self-reported confidence.

## How to Run

```powershell
# Required services: llama.cpp Qwen on :18080, Granite on :8081, Neo4j on :7687.
# Set NEO4J_PASSWORD without printing it, then:
go run ./.planning/spikes/097-qwen-multidomain-reward-surface
```

Optional: `AURA_ADAPTIVE_BENCH_REPEATS=N` and
`AURA_ADAPTIVE_BENCH_WORKERS=N`. The scientific latency run uses one worker;
concurrency is evaluated separately in spike 100 so queueing cannot confound
per-action latency.

## What to Expect

Forensic `[RUN]` lines per execution, aggregated `[METRIC]` lines per
domain/action, and:

`artifacts/reward-surface.json`

The artifact includes 768-dimensional real Granite context embeddings, raw
Qwen outputs/tool calls, correctness, prompt/completion tokens, latency, and
three explicit reward scalarizations (quality-first, balanced, economy).

## Observability

The console log is timestamped and the JSON artifact is the lossless export.
Any model, embedding, or Neo4j failure makes the process non-zero; it cannot
silently pass with a partial matrix.

## Investigation Trail

- Reused the exact cached `Qwen3.5-2B-Q4_K_M.gguf` and the already-proven
  llama.cpp per-request thinking fields from spike 095.
- Uses Aura's production `documents.EmbeddingClient` and `semindex.Ranker`.
- Knowledge facts use a spike-owned Neo4j label and vector index; only
  spike-owned nodes are cleaned.
- Rejected the first four-worker timing calibration because short calls inherited
  queueing delay from long-thinking calls. The evidence run uses one randomized
  inference slot; concurrency moves to spike 100.
- A nominal 256-token thinking budget ran to the 1,024-token completion ceiling
  on a hard prompt. The budget is not a reliable hard cap for this Qwen template,
  so latency and completion ceilings remain governance inputs.
- Fixed and regression-tested a scorer false negative: "Tuesday at 02:00 UTC"
  is equivalent to "Tuesday 02:00 UTC." The corrected run is the canonical
  artifact.
- Preserved two complete live runs. A full-catalog tool call changed outcome
  between temperature-zero runs, demonstrating real runtime/model variance that
  spike 098 must sample rather than hide.

## Results

**VALIDATED.** All 132 scenario/action executions completed against Qwen,
Granite, and Neo4j with zero infrastructure errors. The measured surface has
different winners by context, so a conditional policy is justified; no single
static action dominates.

### Corrected canonical run

| Domain | Action | Accuracy | Mean tokens | Mean latency |
|---|---|---:|---:|---:|
| Reasoning | off | 33.3% | 54.0 | 194 ms |
| Reasoning | low 256 | 75.0% | 364.9 | 5,353 ms |
| Reasoning | high 768 | 83.3% | 577.3 | 9,194 ms |
| Tools | semantic top-1 | 58.3% | 0 | common embedding cost excluded |
| Tools | Qwen top-3 | **100%** | 452.8 | 715 ms |
| Tools | Qwen full | 83.3% | 749.4 | 659 ms |
| Skills | semantic top-1 | 91.7% | 0 | common embedding cost excluded |
| Skills | Qwen top-3 | 91.7% | 419.2 | 581 ms |
| Skills | Qwen full | **100%** | 535.1 | 602 ms |
| Knowledge | none | 12.5% | 50.8 | 151 ms |
| Knowledge | vector top-1 | 50.0% | 68.6 | 167 ms |
| Knowledge | graph expansion | **100%** | 76.1 | 177 ms |

### What the averages conceal

- Balanced-utility reasoning winners split across `off`, `low_256`, and
  `high_768`; high effort is not universally best.
- Semantic top-1 is the cheapest correct tool route for clear weather, finance,
  calendar, and web intents, but misses document, preference, and shell-like
  ambiguity. Qwen top-3 repairs all of those in the corrected run.
- Skill top-1 gets 11/12 but mistakes a research request for Go testing; the
  full catalog repairs it. Spending Qwen tokens on the other 11 is waste.
- Vector-only private retrieval solves direct facts but fails every tested
  two-hop relation. Graph expansion supplies the needed neighbor with only
  about eight extra prompt tokens.
- `none` guessed one private radio channel correctly by chance. Outcome learning
  therefore needs repeated evidence/confidence, not a one-shot success flag.

Artifacts:

- `artifacts/reward-surface-run1.json`
- `artifacts/reward-surface-run2.json`
- `artifacts/reward-surface.json` (canonical corrected run)
