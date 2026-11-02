# Spike — knowledge compilation (claim–evidence pairs) on Aura's passages

**Question.** arXiv 2610.41172 ("Context as Infrastructure", InfiniFlow/RAGFlow, 2026-10-30) reports
that compiling each chunk into claim–evidence pairs at ingest, and handing the model the pairs in place
of the chunk (E2), cuts the tokens it reads by 38% on FRAMES (−91% when the pairs suffice, +1.9% when
they do not), with a decision model gating how deep to read (E3). Does the mechanism hold on the
passages Aura's ingest actually writes, Italian business documents and workbook rows, with an
extractor and a gate that run locally?

**What is reused, not rewritten.**
- Passages: the production modules in `services/ingest` (`media.index_text`, `outline`, `app._card`
  calling `aura-filecard`, `rows.group` / `chunk.chunk`), cut by EmbeddingGemma's own tokenizer.
- Corpus and questions: the release fixtures `scripts/fixtures/document_retrieval_eval/`
  (`corpus` + `qrels.json`, `complex_corpus` + `complex_qrels.json`), which carry gold answers.
- Gate question: the `answers` question of `docs/superpowers/verification/2026-10-25-document-judge-decision-models.md`.

**Models, all local, all on CPU (4 cores), llama.cpp `server-b11960` (the build compose.cpu.yaml pins).**

| Role | Model | Pin |
|---|---|---|
| tokenizer | `ggml-org/embeddinggemma-300M-GGUF` Q8_0 | `scripts/fetch_embedding_model.sh` |
| extractor | `unsloth/Qwen3-4B-Instruct-2507-GGUF` Q4_K_M | commit `a06e946b`, sha256 `3605803b…7e597` |
| gate | `LiquidAI/d1-2B-GGUF` Q4_K_M (`/v1/systemone`) | commit `e968f657`, sha256 `adca5789…0b4b` |

## Steps

```bash
S=/tmp/spike  # any scratch dir
# servers
docker run -d --network host -v $MODELS:/m ghcr.io/ggml-org/llama.cpp:server-b11960 \
  -m /m/embeddinggemma-300M-Q8_0.gguf --embedding -c 2048 -ub 2048 --port 8081
docker run -d --network host -v $MODELS:/m ghcr.io/ggml-org/llama.cpp:server-b11960 \
  -m /m/qwen3-4b-instruct-2507-Q4_K_M.gguf -c 8192 -np 1 --port 8082
docker run -d --network host -v $MODELS:/m ghcr.io/ggml-org/llama.cpp:server-b11960 \
  -m /m/d1-2B-Q4_K_M.gguf -b 8192 -ub 8192 -c 16384 --port 8080
# 1. passages, with the ingest venv (docker/aura-ingest/requirements.txt) and aura-filecard on PATH
PYTHONPATH=../../services AURA_EMBED_TOKENIZER_URL=http://127.0.0.1:8081 \
  $VENV/bin/python dump_passages.py $S/passages.jsonl
# 2. one case per question: the gold passage stating the answer
python3 localize.py $S/passages.jsonl $S/cases.json
# 3. claim-evidence pairs, evidence must be a verbatim span
python3 compile_claims.py http://127.0.0.1:8082 $S/passages.jsonl $S/cases.json $S/claims.jsonl
# 4. d1 over raw passage, compiled pairs, and another document's passage
python3 gate.py http://127.0.0.1:8080 $S/passages.jsonl $S/cases.json $S/claims.jsonl $S/gate.jsonl
# 5. numbers
PYTHONPATH=../../services AURA_EMBED_TOKENIZER_URL=http://127.0.0.1:8081 \
  $VENV/bin/python report.py $S/passages.jsonl $S/cases.json $S/claims.jsonl $S/gate.jsonl
```

`results/` holds the artefacts of the committed run; `FINDINGS.md` reads them.
