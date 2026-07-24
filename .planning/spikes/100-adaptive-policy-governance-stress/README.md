---
spike: 100
name: adaptive-policy-governance-stress
type: standard
validates: "Given the winning graph-kNN policy, when feedback is delayed/noisy, actions drift, or Neo4j is unavailable, then exploration and rollback guards bound harm and recover within measured limits"
verdict: VALIDATED
related: [098b-graph-knn-policy, 099-neo4j-outcome-policy-graph]
tags: [safety, drift, delayed-feedback, rollback, adaptive-learning]
---

# Spike 100: Adaptive-policy governance stress

## What This Validates

The 098 winner is replayed under five hostile conditions: 32-decision feedback
delay, 10% corrupted rewards, abrupt degradation of one previously strong
action per domain, a 100-decision graph-store outage, and the combined case.

The final governed challenger deliberately preserves spike 098b's estimator:
top-5 cosine-neighbor mean, 256 observations per action, and 4% exploration.
It adds only:

- reward clipping to `[-0.25, 1]`;
- a versioned in-memory snapshot that keeps serving during a Neo4j outage;
- static fallback only for a genuinely cold process with no cached snapshot;
- action-local drift detection: compare 16 prior outcomes with 8 recent
  outcomes, quarantine for 100 decisions only after a large relative collapse,
  then relearn that action from fresh evidence.

## Research

The design applies the robustness lesson from
[Correlation-Aware Contextual Bandits](https://arxiv.org/abs/2607.09015):
surrogate/neighbor evidence must be separable and safely ignorable when wrong.
It also follows contextual-bandit deployment practice by bounding exploration
and retaining a deterministic fallback rather than treating online learning as
unrestricted reinforcement learning.

## How to Run

```powershell
go test ./.planning/spikes/100-adaptive-policy-governance-stress
go run ./.planning/spikes/100-adaptive-policy-governance-stress
```

## What to Expect

2,400 seeded 800-decision replays, bootstrap confidence intervals, harmful
action rate, fallback rate, post-drift adaptation lag, and rollback count in
`artifacts/governance-stress.json`.

## Investigation Trail

- Stress rewards are grounded in the two live Qwen surfaces. Drift/outage
  transformations are explicit synthetic interventions, not relabeled as live.
- The graph-outage window affects policy state only; the evaluator continues to
  track the counterfactual reward surface.
- Hypothesis v1 used an absolute global reward circuit. It is **invalidated**:
  legitimate low-cost rewards tripped it, causing 48–64% fallback and more
  regret. Its complete artifact is retained as
  `artifacts/governance-stress-v1-invalidated.json`.
- Hypothesis v2 used a 64-item robust median. It cut abrupt-drift regret from
  239.3 to 98.2, but regressed nominal regret from 49.6 to 60.7. It is retained
  as `artifacts/governance-stress-v2-partial.json`.
- The final v3.1 ablation removes both failed mechanisms and changes no nominal
  kNN choice until an outage or statistically large action-local collapse.

## Results

**VALIDATED.** Results are paired over 200 identical seeds per condition. A
negative delta favors the governed policy.

| Condition | Raw regret | Governed regret | Paired delta (95% bootstrap CI) | Governed harm | Governed rollbacks |
|---|---:|---:|---:|---:|---:|
| Nominal | 49.63 | 49.65 | +0.02 `[0.00, 0.05]` | 2.7% | 0.01 |
| Delay 32 | 48.59 | 48.60 | +0.01 `[0.00, 0.03]` | 3.3% | 0.01 |
| Reward noise 10% | 61.77 | 61.83 | +0.06 `[-0.02, 0.20]` | 4.6% | 0.02 |
| Neo4j outage | 54.77 | 49.65 | −5.12 `[-5.81, −4.34]` | 2.7% | 0.01 |
| Abrupt drift | 239.30 | 107.07 | −132.23 `[-140.33, −123.99]` | 10.4% | 2.83 |
| Combined | 215.67 | 128.83 | −86.84 `[-92.78, −80.66]` | 13.1% | 3.16 |

Nominal, delay, and noise behavior is operationally identical to raw graph-kNN;
the tiny positive nominal/delay delta comes from rare conservative
quarantines. During abrupt drift, governed harm falls by 14.5 percentage points
and adaptation lag falls from 398.9 to 213.0 decisions. Under combined stress,
harm falls by 9.2 points and lag by 177.4 decisions. A mid-run Neo4j outage
causes zero fallback because the versioned policy snapshot is already in
memory.

Production implication: do not ship the absolute circuit breaker or the
short-window median. Ship the proven graph-kNN unchanged, with bounded outcome
values, cached snapshots, and action-local relative drift quarantine.
