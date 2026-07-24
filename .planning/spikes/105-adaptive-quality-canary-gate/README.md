---
spike: 105
name: adaptive-quality-canary-gate
type: standard
verdict: VALIDATED_GATE_NOT_MODEL
tags: [reward, calibration, canary, sequential-testing, quality]
---

# Spike 105: Calibrated production canary gate

## Claim

Promotion can be mechanically prevented unless a closed, randomized production
cohort provides independently evaluated quality and safety evidence with adequate
sample size and a preregistered sequence of statistical looks.

This spike validates the gate. Qwen3.5 2B is only a separate spike instrument; it is
not Aura's real model or a production candidate. This proof uses the explicit subject
ID `synthetic-model-not-production` and does not claim that any model wins.

## Gate

- only `production_canary` evidence is eligible;
- spike/offline evidence cannot promote;
- model self-report and operational success proxies are rejected;
- deterministic, human, or calibrated-judge outcomes require a calibration/rubric ID;
- every assignment logs a non-zero propensity;
- duplicate decisions, missing model/policy/cohort identity, excessive censoring,
  differential censoring, and low effective sample size block promotion;
- five preregistered looks share total alpha `0.05` by Bonferroni correction;
- conservative Wilson/Newcombe bounds require quality uplift lower bound `> 0`;
- harm-increase upper bound must be `<= 0.02`.

With that strict 2-point harm margin, the preregistered synthetic proof uses 2,500
assignments per arm. This is intentionally not weakened for a low-volume deployment.

## Reproduce

```powershell
go test ./internal/adaptive -run TestPromotionGate -count=1 -v
go run ./.planning/spikes/105-adaptive-quality-canary-gate
```

Artifact: `artifacts/quality-canary-statistical-proof.json`

Expected artifact verdict: `VALIDATED_GATE_NOT_MODEL`.

Seeded result over 1,000 trials per scenario:

| Scenario | Promotion rate |
|---|---:|
| null: equal 65% quality, equal 1% harm | 0/1000 = 0.0% |
| safe: 65% to 78% quality, equal 1% harm | 1000/1000 = 100.0% |
| unsafe: same quality win, harm rises 1% to 8% | 0/1000 = 0.0% |

Spike evidence, self-report, duplicate decisions, and differential censoring were all
rejected by the negative controls.

## Verdict boundary

`VALIDATED_GATE_NOT_MODEL`: the seeded test validates false-promotion control, power
for a large safe effect, harm blocking, and evidence-poison defenses. Production
effectiveness remains `UNPROVED` until the real configured model and real Aura traffic
complete this exact randomized gate.
