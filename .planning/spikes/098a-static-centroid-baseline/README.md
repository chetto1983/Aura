---
spike: 098a
name: static-centroid-baseline
type: comparison
validates: "Given the measured Qwen reward surface, when Aura's no-feedback static/centroid-family policy is replayed, then it provides the fixed paired baseline for adaptive policy claims"
verdict: VALIDATED_BASELINE
related: [097-qwen-multidomain-reward-surface, 052-reasoning-tier-embed-classifier, 058-unified-embedding-index]
tags: [adaptive-learning, baseline, centroid, benchmark]
---

# Spike 098a: Static/centroid baseline

This comparison arm uses no observed outcomes. Reasoning follows a fixed
complexity heuristic, tools and skills use Granite semantic top-1, and knowledge
always graph-expands. It is intentionally close to Aura's current family of
deterministic policies and is evaluated in the shared 098d replay harness.

## How to Run

`go run ./.planning/spikes/098d-graph-prior-linucb`

## Results

**VALIDATED BASELINE, but decisively outperformed.** Over 600 decisions and 200
paired seeds, balanced cumulative regret is 88.39 (stationary and delay-8),
mean reward 0.793, and harmful-action rate 15.91%. The fixed policy's high
75.1% oracle-action match is misleading: repeated semantic top-1 misses carry
large quality regret. This is the control, not the adoption candidate.
