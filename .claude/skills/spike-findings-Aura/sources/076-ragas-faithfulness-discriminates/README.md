---
spike: 076
name: ragas-faithfulness-discriminates
type: standard
validates: "Given a QA set over an ingested doc with grounded/hallucinated/partial answers, when scored with RAGAS faithfulness + answer_correctness, then grounded scores high and hallucinated clearly lower, repeatably — a calibrated replacement for the single 0-10 '9.8' target"
verdict: VALIDATED
related:
  - 075-image-ocr-searchable-chunks
  - internal/eval/retrieval_eval.go
  - internal/llm/config.go
tags: [item-3, ragas, eval, faithfulness, answer-correctness, llm-judge, construct-validity]
---

# Spike 076 — RAGAS Faithfulness Discriminates

## What This Validates

**Item 3**: reframe the fragile single 0–10 LLM-judge ">9.8" target as RAGAS
**faithfulness + answer_correctness + a rubric**. Two risks: (1) *tooling feasibility* —
can RAGAS run in Aura's stack (OpenRouter judge + local granite embeddings)? and
(2) *construct validity* — do the metrics **discriminate** a grounded answer from a
hallucinated one, repeatably, with calibrated numbers — unlike a single 0–10 score?

## How to Run

```bash
# WSL, stack up. First run provisions an isolated Python 3.12 venv (uv) + installs RAGAS.
# Judge = OpenRouter DeepSeek (paid, bounded ~35 calls/run); embeddings = local granite.
wsl -d Ubuntu -e bash /mnt/d/Aura/.planning/spikes/076-ragas-faithfulness-discriminates/run.sh
```

`ragas_probe.py` scores grounded / hallucinated / partial answers over two doc contexts
(the G220 datasheet from spike 075 + a coffee-machine doc), measures faithfulness
repeatability (×3), and contrasts a **context-free single-0–10 judge** — the construct the
reframe replaces.

## Research / Tooling Findings (feasibility risk)

- **System Python is 3.14 → no RAGAS-stack wheels.** `uv venv --python 3.12` provisions an
  isolated interpreter; all wheels resolve. (Same lesson as spike 072's bleeding-edge-Python.)
- **RAGAS dependency churn is real and must be pinned.** `uv` first resolved the new
  **langchain 1.3 / langchain-community 0.4**, and `ragas==0.2.15` died importing
  `langchain_community.chat_models.vertexai` (removed in 0.4). Fix: pin the whole stack to the
  0.3.x era ragas 0.2.15 was built against — `ragas==0.2.15` + `langchain/-core/-community
  >=0.3,<0.4` + `langchain-openai >=0.2,<0.3`. **This pin is load-bearing for any productized
  RAGAS eval.**
- **Aura's stack wires cleanly:** judge = `ChatOpenAI(base_url=openrouter.ai/api/v1,
  model=deepseek/deepseek-v4-flash)`; embeddings = local granite via
  `OpenAIEmbeddings(base_url=:8081/v1, check_embedding_ctx_length=False)` — **free, no extra
  sidecar**. `AnswerCorrectness` needs its `AnswerSimilarity(embeddings=…)` sub-metric wired
  explicitly (else `AssertionError: AnswerSimilarity must be set`).

## Results (live, 2026-06-28, OpenRouter DeepSeek judge + granite embeddings)

| Scenario | faithfulness | answer_correctness |
|---|---|---|
| g220 grounded | **1.000** | **0.980** |
| g220 hallucinated | **0.000** | **0.229** |
| g220 partial (faithful but incomplete) | 1.000 | 0.734 |
| coffee grounded | 1.000 | 0.988 |
| coffee hallucinated | 0.000 | 0.243 |

- **Construct validity — faithfulness:** perfect discrimination (grounded **1.000** vs
  hallucinated **0.000**) on BOTH docs, and **repeatable** (×3 each, stdev **0.000**). It
  decomposes the answer into atomic claims and NLI-checks each against the retrieved context,
  so the number is a *meaningful fraction of supported claims*, not a vibe.
- **Construct validity — answer_correctness:** grounded **0.980** vs hallucinated **0.229**
  (margin **0.752**). The **partial** answer is the key signal: faithfulness **1.0** (it is
  faithful) but answer_correctness **0.734** (incomplete vs the reference) — the two metrics are
  **orthogonal**, which is exactly why the reframe needs both (faithfulness = "no hallucination",
  answer_correctness = "actually answers the question").
- **Why the old target was fragile — the naive judge:** a context-free single-0–10 judge scored
  **10/10 to BOTH** the grounded AND the hallucinated answer (×3 each). A single fluency score
  cannot catch a fluent hallucination and is not calibrated — a "9.8" means nothing. RAGAS's
  decomposed, context-grounded metrics catch it cleanly (1.0 vs 0.0).
- **Cost:** ~35 bounded OpenRouter calls/run on `deepseek/deepseek-v4-flash` (cheap;
  faithfulness ≈ 2 calls/sample, answer_correctness ≈ 1–2). Well under a cent per run.

## Investigation Trail

1. First install resolved langchain 1.x → ragas import crash (`...chat_models.vertexai`). Pinned
   the 0.3.x stack → imports clean.
2. First scored run: faithfulness perfect (1.0/0.0, stdev 0) but `AnswerCorrectness` asserted
   `AnswerSimilarity must be set`. Wired `answer_similarity=AnswerSimilarity(embeddings=emb)` →
   answer_correctness computes (0.980 / 0.229 / 0.734).

## Verdict

**VALIDATED.** RAGAS faithfulness + answer_correctness run in Aura's stack (OpenRouter judge +
free local granite embeddings, pinned 0.3.x langchain) and **discriminate grounded from
hallucinated answers repeatably and meaningfully**, where a single 0–10 judge cannot. Item 3's
reframe is sound: replace the single fragile score with a **gated eval** — faithfulness ≈ 1.0
(no hallucination) + answer_correctness (actually answers) + a task rubric — and stop chasing a
literal "9.8". Operationalization notes: pin the RAGAS/langchain stack, run under a uv-managed
3.12 venv, judge via the configured OpenRouter model, embed via granite.
