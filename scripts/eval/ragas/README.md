# RAG answer-quality eval (RAGAS)

The gated replacement for the single 0–10 "≥ 9.8/10" LLM-judge answer-quality target
(Item 3 / spike 076). It scores answers with two **decomposed, context-grounded** RAGAS
metrics instead of one fragile number:

- **faithfulness** — decomposes the answer into atomic claims and NLI-checks each against
  the retrieved context. "Did the answer hallucinate?" A meaningful fraction of supported
  claims, not a vibe.
- **answer_correctness** — factual F1 (claim overlap vs the reference) + semantic
  similarity (granite embeddings). "Did it actually answer the question?" Orthogonal to
  faithfulness — a faithful-but-incomplete answer scores high faithfulness, mid correctness.

Why not a single 0–10: spike 076 showed a naive context-free 0–10 judge rates a **fluent
hallucination 10/10**, identical to the grounded answer. Faithfulness separates them
cleanly (1.000 vs 0.000) and is repeatable at temperature 0 (stdev 0).

## Run

```bash
# WSL, stack up, OPENROUTER_API_KEY in ~/aura.env
scripts/eval/ragas/run.sh --dry-run   # validate dataset + wiring, no model calls (free)
scripts/eval/ragas/run.sh             # live scoring (paid judge) — the gate
```

The runner provisions an isolated **Python 3.12** venv via `uv` (system Python is 3.14 →
no ragas wheels) with a **pinned** stack — `ragas==0.2.15` + langchain `0.3.x` +
langchain-openai `0.2.x` (langchain 1.x breaks ragas 0.2.15's
`langchain_community.chat_models.vertexai` import). Judge = the configured OpenRouter model
(`deepseek/deepseek-v4-flash`); embeddings = the free local granite sidecar (`:8081`).

## The gate

`reference_qa.json` holds the reference set (grounded / hallucinated / partial answers over
two documents) and the thresholds. The eval PASSes when, for every case:

- grounded answers score **faithfulness ≥ 0.8**,
- hallucinated answers score **faithfulness ≤ 0.4**,
- mean grounded **answer_correctness** beats mean hallucinated by **≥ 0.2**.

Tune the set/thresholds by editing `reference_qa.json` (no code change). This is a
**gated/manual, paid** eval — NOT a CI step. Add real upload→chat cases as the product
grows; pair with a task rubric for human-facing answer quality.

## Validated baseline (spike 076, 2026-06-28, DeepSeek judge + granite embeddings)

| case | faithfulness | answer_correctness |
|---|---|---|
| g220 grounded | 1.000 | 0.980 |
| g220 hallucinated | 0.000 | 0.229 |
| g220 partial (faithful, incomplete) | 1.000 | 0.734 |
| coffee grounded | 1.000 | 0.988 |
| coffee hallucinated | 0.000 | 0.243 |

See `.planning/spikes/076-ragas-faithfulness-discriminates/` and `docs/rag-answer-eval.md`.
