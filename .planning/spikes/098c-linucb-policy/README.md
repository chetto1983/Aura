---
spike: 098c
name: linucb-policy
type: comparison
validates: "Given projected Granite context features and bandit-only outcomes, when disjoint LinUCB chooses among domain actions with bounded epsilon exploration, then measure regret, harm, and decision latency"
verdict: PARTIAL
related: [097-qwen-multidomain-reward-surface]
tags: [adaptive-learning, contextual-bandit, linucb, benchmark]
---

# Spike 098c: Disjoint LinUCB

This is the standard contextual-bandit baseline: one linear reward model per
domain/action, Sherman-Morrison online updates, UCB exploration, and explicit
logged propensities.

## How to Run

`go run ./.planning/spikes/098d-graph-prior-linucb`

## Results

**PARTIAL.** LinUCB learns and beats the static baseline on balanced and
quality-first utility, but materially trails graph kNN:

- stationary balanced regret 56.78, versus kNN 40.60;
- delay-8 balanced regret 55.51, versus kNN 38.40;
- harmful-action rate 5.5–6.5%, versus kNN 3.0–3.5%.

The linear reward assumption is too restrictive for this small, heterogeneous
44-context surface. Keep the policy interface and logged propensity contract,
but do not ship LinUCB as Aura's first learner.
