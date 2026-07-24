---
spike: 098b
name: graph-knn-policy
type: comparison
validates: "Given logged outcome neighbors, when a cosine kNN policy reuses the five closest chosen-action outcomes, then measure sample efficiency and regret without a parametric reward model"
verdict: WINNER
related: [097-qwen-multidomain-reward-surface, 058-unified-embedding-index]
tags: [adaptive-learning, graph, knn, benchmark]
---

# Spike 098b: Graph-neighbor kNN

This arm stores chosen-action outcomes and predicts from the five closest
context neighbors. It is the simplest graph-memory policy and the nonparametric
comparison for LinUCB.

## How to Run

`go run ./.planning/spikes/098d-graph-prior-linucb`

## Results

**WINNER.** Balanced cumulative regret is 40.60 stationary and 38.40 with an
eight-decision outcome delay, versus 88.39 for the static baseline. Harmful
choices fall from 15.91% to 3.25% / 3.05%. It is statistically tied with the
hybrid in stationary replay, then significantly better under delayed feedback:

| Profile | delay-8 kNN regret minus hybrid | Paired 95% CI |
|---|---:|---:|
| quality-first | -0.766 | [-1.454, -0.077] |
| balanced | -1.687 | [-2.590, -0.821] |
| economy | -1.889 | [-2.818, -0.920] |

Negative is better. The simplest graph-neighbor learner wins the first adoption
gate; the bandit remains a future scale-up option.
