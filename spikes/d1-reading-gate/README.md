# Spike — a decision-model reading gate on Aura's passages (arXiv 2610.41172, E3)

**Question.** arXiv 2610.41172 ("Context as Infrastructure", InfiniFlow/RAGFlow, 2026-10-30) reports
that a small decision model gating how much a harness reads cuts the tokens the answering model sees
(−30% without compiled evidence, −52% with it, arm means on FRAMES). Does a local decision model,
LiquidAI d1-2B, separate the passages that answer a question from the ones that do not, on the
passages Aura's ingest actually writes, and how many tokens does dropping the rest save against how
many answers it loses?

**Out of scope, by decision of 2026-11-02.** The paper's claim–evidence compilation (E1/E2) needs a
generative extractor at ingest; this spike runs d1 alone, so it measures the gate over raw passages
and nothing about compilation.

**What is reused, not rewritten.**
- Passages: the production modules in `services/ingest` (`media.index_text`, `outline`, `app._card`
  calling `aura-filecard`, `rows.group` / `chunk.chunk`), cut by EmbeddingGemma's own tokenizer.
- Corpus and questions: the release fixtures `scripts/fixtures/document_retrieval_eval/`
  (`corpus` + `qrels.json`, `complex_corpus` + `complex_qrels.json`), which carry gold answers.
- Candidates: BM25 from `spikes/document-routing-benchmark/benchmark.py`, top 8 (Aura's default limit).
- Gate question: the `answers` question of `docs/superpowers/verification/2026-10-25-document-judge-decision-models.md`.

**Models, local, CPU only (4 cores), llama.cpp `server-b11960` (the build compose.cpu.yaml pins).**

| Role | Model | Pin |
|---|---|---|
| tokenizer | `ggml-org/embeddinggemma-300M-GGUF` Q8_0 | `scripts/fetch_embedding_model.sh` |
| gate | `LiquidAI/d1-2B-GGUF` Q4_K_M, `/v1/systemone` | commit `e968f657`, sha256 `adca5789…0b4b` |

## Steps

```bash
S=/tmp/spike  # any scratch dir
docker run -d --network host -v $MODELS:/m ghcr.io/ggml-org/llama.cpp:server-b11960 \
  -m /m/embeddinggemma-300M-Q8_0.gguf --embedding -c 2048 -ub 2048 --port 8081
docker run -d --network host -v $MODELS:/m ghcr.io/ggml-org/llama.cpp:server-b11960 \
  -m /m/d1-2B-Q4_K_M.gguf -b 8192 -ub 8192 -c 16384 --port 8080
# 1. passages, with the ingest venv (docker/aura-ingest/requirements.txt) and aura-filecard on PATH
PYTHONPATH=../../services AURA_EMBED_TOKENIZER_URL=http://127.0.0.1:8081 \
  $VENV/bin/python dump_passages.py $S/passages.jsonl
# 2. the questions the oracle can judge, with the reason for every one it cannot
python3 localize.py $S/passages.jsonl $S/cases.json
# 3. d1 over each question's top-8 BM25 candidates (needs openpyxl for the imported benchmark module)
python3 gate.py http://127.0.0.1:8080 $S/passages.jsonl $S/cases.json $S/gate.jsonl
# 4. numbers
python3 report.py $S/gate.jsonl
```

`results/` holds the committed run's artefacts; `FINDINGS.md` reads them.
