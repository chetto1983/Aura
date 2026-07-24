---
spike: 098d
name: graph-prior-linucb
type: comparison
validates: "Given the fixed live Qwen reward surface, when static, graph-kNN, LinUCB, and decoupled graph-prior LinUCB receive bandit-only feedback, then paired seeded replay identifies whether any adaptive method lowers regret with statistically bounded evidence"
verdict: RUNNER_UP
related: [097-qwen-multidomain-reward-surface, 053-reasoning-classifier-active-learning, 057-toolselection-oracle-signal]
tags: [adaptive-learning, contextual-bandit, linucb, graph-prior, benchmark]
---

# Spike 098d: Adaptive-policy head-to-head

## What This Validates

The benchmark replays the two complete Qwen reward surfaces for 200 paired seeds,
600 decisions per seed, three quality/cost preference profiles, and immediate
versus eight-step delayed feedback. A learner sees only its chosen action's
sampled reward. Counterfactual outcomes are used exclusively by the evaluator to
compute expected regret.

## Research

| Approach | Primary evidence | Benchmark role |
|---|---|---|
| Static/centroid | Aura spikes 052/053/058 | no-feedback baseline |
| Contextual bandit | [BaRP](https://arxiv.org/abs/2510.07429), [MixLLM](https://arxiv.org/abs/2502.18482) | standard online learner |
| Decoupled surrogate bandit | [Correlation-Aware Contextual Bandits](https://arxiv.org/abs/2607.09015) | graph-prior candidate |
| Off-policy evaluation | [Adaptive weighting for contextual-bandit OPE](https://arxiv.org/abs/2106.02029), [SWITCH estimator](https://arxiv.org/abs/1612.01205) | motivates logged propensities and confidence intervals |

The graph-prior candidate maintains separate kNN and LinUCB predictions. Their
mixing weight follows observed prediction error; a misspecified graph prior can
fall to 10% influence instead of corrupting the reward estimator.

## How to Run

```powershell
go test ./.planning/spikes/098d-graph-prior-linucb
go run ./.planning/spikes/098d-graph-prior-linucb
```

## What to Expect

4,800 seeded replays (2 conditions × 3 profiles × 4 policies × 200 seeds),
bootstrap 95% confidence intervals, paired regret differences, harmful-action
rate, oracle-action match, and decision latency in:

`artifacts/policy-benchmark.json`

The artifact retains all 4,800 seed-level rows so confidence intervals and
alternative paired comparisons are independently reproducible.

## Investigation Trail

- The loader recomputes correctness and scalar rewards from raw outputs across
  both Qwen runs, so the corrected knowledge normalizer applies to run 1 too.
- 768-dimensional Granite embeddings are deterministically projected to 24
  dimensions, then augmented with domain and preference features.
- All adaptive policies use 4% epsilon exploration and log exact propensities,
  preserving support for later off-policy evaluation.

## Results

**RUNNER-UP; not the first production policy.** The graph-prior LinUCB is a
large improvement over the static baseline and plain LinUCB, but it does not
beat the simpler graph kNN policy.

### Balanced profile

| Condition | Static | LinUCB | Graph kNN | Graph-prior LinUCB |
|---|---:|---:|---:|---:|
| Stationary cumulative regret | 88.39 | 56.78 | **40.60** | 40.62 |
| Delay-8 cumulative regret | 88.39 | 55.51 | **38.40** | 40.09 |
| Stationary final-100 regret | 14.51 | 6.92 | 4.63 | **4.17** |
| Delay-8 harmful-action rate | 15.91% | 5.51% | **3.05%** | 3.32% |

The hybrid's lower stationary late-stage regret is useful evidence for a future
large-corpus upgrade, but stationary cumulative regret is statistically tied
with kNN and delay-8 regret is significantly worse in all three preference
profiles. Aura should ship graph kNN behind the common policy interface and
retain LinUCB/hybrid as shadow challengers.

### Statistical result

Across 200 paired seeds, graph-prior LinUCB reduces cumulative regret versus
the static baseline by 47.77 balanced points stationary
(95% CI [46.27, 49.25]) and 48.30 with delay-8
([46.77, 49.77]). It also beats plain LinUCB by 16.17 stationary
([14.85, 17.52]) and 15.42 delayed ([14.17, 16.70]).

The direct kNN-minus-hybrid interval is the adoption gate:

- stationary balanced: -0.019, 95% CI [-0.991, 0.967] — tied;
- delay-8 balanced: -1.687, 95% CI [-2.590, -0.821] — kNN wins.

The Windows wall-clock timer reports many sub-tick decisions as zero, so the
artifact's p95 timing field is not used for the verdict. Mean decision cost is
still sub-millisecond for every policy; a dedicated benchmark belongs in the
implementation phase.
