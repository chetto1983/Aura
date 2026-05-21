# Aura Quality Bench

Reusable, repeatable Q&A benchmark for Aura's wiki ingest+retrieval substrate.

**Goal**: one number you can read every wave to know if Aura is actually improving.

## Why this exists

Until 2026-05-21 we shipped phases by passing `probe_chat` smoke tests. We never measured **retrieval quality** as a number. After the xlsx pain (105s → 30s, but still ranking by brute-force) we agreed: **no wave closes without a metric**.

This bench answers the only question that matters: *given a real file, can Aura recover specific facts from it after ingest?*

## Structure

```
docs/quality-bench/
├── README.md                 # this file
├── queries.json              # 10 fixtures × ~2 queries each — committed
├── ground-truth.json         # extracted facts per fixture — committed
├── fixtures/                 # the actual files
│   ├── LICENSE.md            # per-file attribution (Apache-2.0 / PD / CC-BY)
│   ├── tika-testEXCEL.xlsx   # Apache Tika
│   ├── tika-testWORD.docx    # Apache Tika
│   ├── tika-testPPT.pptx     # Apache Tika
│   ├── istat-popolazione.csv # ISTAT, CC-BY
│   ├── arxiv-NNNN.pdf        # arXiv IT NLP paper
│   ├── gutenberg-promessi.txt
│   ├── gutenberg-pinocchio.epub
│   ├── wiki-galileo.md
│   ├── wiki-galileo.html
│   └── autori-italiani.json  # hand-built ~20 entry
└── runs/                     # output of each run — committed
    ├── 2026-05-21-pre-wave-a.json
    ├── 2026-05-22-post-wave-a.json
    └── ...
```

## File types covered

One fixture per Aura-accepted format (10 total). Image formats skipped — Wave 2.9.5 gate still closed.

| Type | Pipeline | Fixture | License |
|---|---|---|---|
| `.pdf` | Mistral OCR | arXiv paper IT | arXiv non-exclusive |
| `.txt` | passthrough | Gutenberg IT (Manzoni cap. 1) | Public Domain |
| `.md` | passthrough | Wikipedia IT (Galileo) | CC-BY-SA 4.0 |
| `.json` | markitdown | autori italiani (20 entry) | self-built CC0 |
| `.csv` | markitdown | ISTAT popolazione regioni 2024 | CC-BY 3.0 IT |
| `.xlsx` | markitdown | Apache Tika testEXCEL | Apache-2.0 |
| `.docx` | markitdown | Apache Tika testWORD | Apache-2.0 |
| `.pptx` | markitdown | Apache Tika testPPT | Apache-2.0 |
| `.epub` | markitdown | Gutenberg IT (Pinocchio cap. 1) | Public Domain |
| `.html` | markitdown | Wikipedia IT (Galileo) saved | CC-BY-SA 4.0 |

## Run procedure

```powershell
# 1. Build current Aura
docker compose up -d --build aura

# 2. Run the harness (TBD path)
go run ./cmd/quality_bench \
  --queries docs/quality-bench/queries.json \
  --ground-truth docs/quality-bench/ground-truth.json \
  --fixtures docs/quality-bench/fixtures/ \
  --out docs/quality-bench/runs/$(date +%Y-%m-%d)-<label>.json

# 3. Snapshot update
go run ./cmd/quality_bench --snapshot-append \
  --run docs/quality-bench/runs/$(date +%Y-%m-%d)-<label>.json \
  --snapshot docs/aura-quality-snapshot.md
```

What the harness does per fixture:

1. POST file to `/api/sources` → wait for `StatusIngested`
2. For each query: POST `/api/chat` → assert reply contains `expected_substring`
3. Hit `/api/wiki/search?q=<query>` → assert ground-truth slug in top-5
4. Record: pass/fail, latency, tool-call count
5. After all 10 fixtures: aggregate into 4 KPIs

## The 4 KPIs

| Metric | What it measures | Aura "Good" target |
|---|---|---|
| **Pass rate /20** | Reply contains expected substring | ≥16/20 |
| **Recall@5** | Ground-truth slug in top-5 search results | ≥85% |
| **p95 end-to-end** | POST `/api/chat` round-trip wall time | ≤15s |
| **avg tool-calls** | LLM tool invocations per query (lower = found faster) | ≤3 |

Industry context (BEIR 2026): top dense+rerank gets nDCG@10 = 57-60 on heterogeneous. Aura is narrow-domain (~200-500 pages, single-user) so we aim higher than the open-domain median.

## Target progression across waves

| Wave | When | Pass /20 | Recall@5 | p95 E2E |
|---|---|---|---|---|
| Pre-A baseline | post-vector-only | est. 10 | est. 50% | est. 60s |
| Post-A (hybrid RRF) | first run | ≥12 | ≥70% | ≤30s |
| Post-B (markitdown per-row) | +1 wave | ≥16 | ≥85% | ≤15s |
| Post-C (reranker) | +2 waves | ≥18 | ≥90% | ≤10s |

A wave that misses its target = **do not advance** to the next. Re-plan instead.

## Rules

- **No mock**, no synthetic fixtures. Every file is a real public document.
- **Deterministic ground truth** — every `expected_substring` is verifiable by reading the source file
- **License-attributed** — `fixtures/LICENSE.md` lists every file's origin + license
- **Output committed** — runs are committed so the trend is reviewable via `git log`
- **One bench run per wave** — not per commit; runs cost API + time
