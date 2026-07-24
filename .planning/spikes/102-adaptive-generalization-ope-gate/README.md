---
spike: 102
name: adaptive-generalization-ope-gate
type: standard
validates: "Given training outcomes from spike 097, when graph-kNN is evaluated on unseen context families and changed catalogs with independent live Qwen runs, then its family-bootstrap lower bound beats Aura's operational static policy and supported OPE recovers the known target value"
verdict: INVALIDATED_GENERALIZATION_VALIDATED_OPE
related: [097-qwen-multidomain-reward-surface, 098b-graph-knn-policy, 100-adaptive-policy-governance-stress, 101-aura-adaptive-shadow-e2e]
tags: [adaptive-learning, holdout, generalization, ope, contextual-bandit, qwen3.5]
---

# Spike 102: Adaptive generalization and OPE gate

## What This Validates

The earlier replay repeatedly sampled the same 44 contexts. This spike freezes those
contexts as training data and executes a disjoint live holdout:

- 38 new prompts across reasoning, tool, skill, and knowledge domains;
- 19 context families;
- expanded tool and skill catalogs containing deliberately confusing near-neighbors;
- new private entities and graph relationships;
- two fresh executions of every context/action cell against the exact Qwen3.5 GGUF.

The primary policy comparison is paired by context family. The graph-kNN candidate may
not observe any holdout counterfactual before choosing. Its utility delta is bootstrapped
over context families rather than replay seeds.

The OPE test uses a safe randomized logging policy with non-zero support for every
candidate action. IPS, self-normalized IPS, and doubly robust estimates are compared
against the full-information live surface. A deterministic logger is a negative control:
the evaluator must reject it for missing support.

## Research

| Approach | Evidence | Strength | Limitation | Status |
|---|---|---|---|---|
| Direct reward-model evaluation | Vowpal Wabbit OPE documentation | Low variance | Biased under partial feedback | Rejected as promotion evidence |
| IPS | Vowpal Wabbit; Wang, Agarwal, Dudik (ICML 2017) | Unbiased with correct propensities and overlap | High variance | Diagnostic |
| Self-normalized IPS | Standard contextual-bandit OPE | More stable finite-sample estimate | Small finite-sample bias | Required diagnostic |
| Doubly robust | Vowpal Wabbit OPE | Combines logged rewards and an independent reward model | Still needs overlap; model errors can raise variance | Primary estimator |
| SWITCH | Wang, Agarwal, Dudik (ICML 2017) | Better bias/variance under extreme weights | Extra tuning not justified with three bounded actions | Future challenger |

Primary sources:

- <https://vowpalwabbit.org/docs/vowpal_wabbit/python/latest/tutorials/off_policy_evaluation.html>
- <https://www.microsoft.com/en-us/research/publication/optimal-adaptive-off-policy-evaluation-contextual-bandits/>
- <https://arxiv.org/abs/2210.10768>

**Chosen approach:** family holdout plus supported randomized logging, with doubly
robust and self-normalized IPS required to agree with known live-surface truth.

## How to Run

Required live services:

- llama.cpp serving `Qwen3.5-2B-Q4_K_M.gguf` on `127.0.0.1:18080`;
- Aura Granite embeddings on `127.0.0.1:8081`;
- Aura Neo4j on `127.0.0.1:7687`.

```powershell
$env:NEO4J_PASSWORD = (docker inspect aura-neo4j --format '{{range .Config.Env}}{{println .}}{{end}}' |
  Select-String '^NEO4J_AUTH=').Line.Split('/')[1]
go test ./.planning/spikes/102-adaptive-generalization-ope-gate
go run ./.planning/spikes/102-adaptive-generalization-ope-gate
```

`AURA_ADAPTIVE_PROOF_REPEATS` defaults to 2. Reducing it is permitted for debugging
but cannot satisfy the evidence gate.

## Acceptance

- Every holdout context/action cell has two error-free live executions.
- No holdout prompt, expected answer, private entity, or catalog description occurs in
  the training corpus.
- Graph-kNN's family-bootstrap 95% lower bound for balanced utility delta is above zero.
- Accuracy does not regress and incorrect-action rate does not increase.
- SNIPS and doubly robust OPE have effective sample size at least 1,000, absolute error
  at most 0.03, and confidence intervals covering the known target value.
- Deterministic unsupported logs are rejected rather than scored.

## Observability

The harness writes every live outcome, embeddings, policy choices, family-bootstrap
intervals, OPE diagnostics, overlap, effective sample size, maximum importance weight,
and limitations to:

`artifacts/generalization-ope-proof.json`

## Investigation Trail

- The holdout corpus was authored before running the policy and uses changed catalogs.
- Aura's actual Granite reasoning classifier supplies the reasoning baseline.
- Tool, skill, and knowledge baselines match the closest current operational behavior;
  they are named `operational_static`, not claimed as byte-identical implementations.
- The exact candidate is fixed before evidence: top-5 per action, cosine similarity
  cubed, two independent spike-097 live samples, balanced reward profile.

## Results

The exact llama.cpp Qwen spike completed 228 error-free live action executions.
Qwen3.5 2B is a portability and mechanism probe only; it is not Aura's production
model and these numbers are not production answer-quality evidence.

| Policy | Balanced utility | Accuracy | Harmful rate |
|---|---:|---:|---:|
| operational static | 0.6099 | 67.1% | 32.9% |
| frozen graph kNN | 0.7778 | 82.9% | 17.1% |

The point estimate improved by `+0.1679`, but the required context-family bootstrap
95% interval was `[-0.0168, +0.3198]`. The lower bound crossed zero, so unseen-family
generalization is **INVALIDATED**, not promoted.

The supported OPE mechanism passed:

| Estimator | Estimate | Truth | Absolute error | ESS | Covers truth |
|---|---:|---:|---:|---:|---|
| IPS | 0.8033 | 0.7778 | 0.0256 | 5605 | no |
| SNIPS | 0.7774 | 0.7778 | 0.0004 | 5605 | yes |
| doubly robust | 0.7727 | 0.7778 | 0.0051 | 5605 | yes |

The deterministic logger negative control was rejected for missing support. This
validates the SNIPS/DR evaluation plumbing over a known live surface, not the graph
kNN policy's production benefit. Production activation still requires a randomized
canary on the real configured model and real Aura traffic.
