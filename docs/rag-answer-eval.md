# RAG answer-quality eval (RAGAS) — the gate, not a single score

Aura's upload→chat answers are graded by a **decomposed, context-grounded** eval, not a
single 0–10 LLM-judge score. This is the Item 3 reframe of the fragile ">= 9.8/10" answer
target, validated by spike 076 (`.planning/spikes/076-ragas-faithfulness-discriminates/`).

## Why not a single 0–10

A single "rate this answer 0–10" judge is not calibrated for fine-grained numbers and
cannot see whether the answer is grounded in the retrieved context. Spike 076 ran a naive
context-free 0–10 judge over the same answers and it scored a **fluent hallucination 10/10**
— identical to the grounded answer. A "9.8" therefore means nothing about faithfulness.

## The two metrics (RAGAS)

| Metric | Question | How |
|---|---|---|
| **faithfulness** | Did the answer hallucinate? | Decompose the answer into atomic claims; NLI-check each against the retrieved context. Score = supported / total. |
| **answer_correctness** | Did it actually answer? | Factual F1 (claim overlap vs the reference) + semantic similarity (granite embeddings). |

They are **orthogonal** — a faithful-but-incomplete answer scores high faithfulness, mid
correctness (the spike's "partial" case: faithfulness 1.000 / correctness 0.734). The gate
needs both: faithfulness = "no hallucination", answer_correctness = "answers the question".

## The gate

`scripts/eval/ragas/` is the runnable, reusable harness over a committed reference set
(`reference_qa.json`). It PASSes when, for every case:

- grounded answers score **faithfulness ≥ 0.8**,
- hallucinated answers score **faithfulness ≤ 0.4**,
- mean grounded **answer_correctness** beats mean hallucinated by **≥ 0.2**.

Plus a task **rubric** for human-facing answer quality (tone, citation, completeness) — the
rubric is the qualitative complement; RAGAS is the quantitative, regression-catchable gate.

```bash
scripts/eval/ragas/run.sh --dry-run   # validate dataset + wiring (free)
scripts/eval/ragas/run.sh             # live scoring (paid judge) — the gate
```

## Operationalization (load-bearing, from spike 076)

- **Python 3.12 venv via `uv`** — the system Python is 3.14 and has no ragas-stack wheels.
- **Pin the stack**: `ragas==0.2.15` + langchain `0.3.x` + langchain-openai `0.2.x`.
  langchain 1.x breaks ragas 0.2.15's `langchain_community.chat_models.vertexai` import.
- **Judge** = the configured OpenRouter model (`deepseek/deepseek-v4-flash`); **embeddings**
  = the free local granite sidecar (`:8081`). `AnswerCorrectness` needs its
  `AnswerSimilarity(embeddings=…)` sub-metric wired explicitly.
- **Gated/manual, paid — NOT CI.** Run it on retrieval/answer-path changes and before a
  release; it is not a per-PR step. Grow `reference_qa.json` with real upload→chat cases.

## Validated baseline (spike 076, 2026-06-28, DeepSeek judge + granite embeddings)

| case | faithfulness | answer_correctness |
|---|---|---|
| g220 grounded | **1.000** | 0.980 |
| g220 hallucinated | **0.000** | 0.229 |
| g220 partial (faithful, incomplete) | 1.000 | 0.734 |
| coffee grounded | 1.000 | 0.988 |
| coffee hallucinated | 0.000 | 0.243 |

Faithfulness perfectly separated grounded from hallucinated on both documents, repeatable at
temperature 0 (stdev 0); answer_correctness margin 0.75. This replaces any single 0–10
">= 9.8" answer score.
