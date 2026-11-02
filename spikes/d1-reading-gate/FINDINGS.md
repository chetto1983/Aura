# A decision model as Aura's reading gate — 2026-11-02

**Verdict: PARTIAL.** On the passages Aura's ingest writes, a decision model keeps the passage that
answers and drops most of the rest: the answering model would read **84% fewer tokens with no answer
lost** (d1.1 on OpenRouter, threshold 0.5), against BM25's own ordering, which separates far worse.
The local d1-2B separates almost as well, but at 0.5 it loses 3 answers of 36, and on a 4-core CPU it
costs about a minute per search. PARTIAL because this is one synthetic Italian corpus, one run, an
oracle that reads tokens and not meaning, and BM25 candidates rather than Aura's fused retrieval.

Origin: arXiv 2610.41172 ("Context as Infrastructure", InfiniFlow/RAGFlow), its decision-gated reading
mode E3. Its claim–evidence compilation (E1/E2) is out of scope by the operator's decision of the same
day: it needs a generative extractor at ingest, and this spike runs decision models only.

## Protocol

- **Passages.** 561 passages from the 35 files of the two release fixture corpora
  (`scripts/fixtures/document_retrieval_eval/corpus` and `complex_corpus`), cut by the production
  ingest modules (`dump_passages.py`). The scanned PDF yields none: there is no vision route here.
- **Questions.** 38 of the 54 questions of `qrels.json` and `complex_qrels.json`. `localize.py` lists
  the 16 excluded and why: 10 have an answer too short for the oracle to judge, 4 are not stated in any
  one passage, 2 are computed over many rows and match by coincidence ("376" is "Riga 376").
- **Candidates.** The top 8 passages (Aura's default `limit`) of BM25 over all 561, the routing
  spike's implementation. 304 (question, passage) pairs; 36 questions have an answering passage among
  their 8, 2 do not.
- **Label.** A passage answers when it carries every content token of the gold answer and belongs to
  a gold document (`oracle.py`).
- **Gate.** One `noul` per pair, the `answers` question of the 2026-10-25 judge spike. The threshold is
  that spike's 0.5, not fitted here.
- **Models.**
  - `LiquidAI/d1-2B-GGUF` Q4_K_M, commit `e968f657`, llama.cpp `server-b11960`, 4 CPU threads, no GPU.
  - `liquid/d1.1` on OpenRouter's Decisions API (snapshot `liquid/d1.1-20261027`, provider Liquid),
    through `spikes/d1-document-labels/openrouter_shim.py`, which adds `model` and the noul criteria
    `{"true": "Yes", "false": "No"}` the hosted API requires.

## Results

| | BM25 | d1-2B Q4_K_M, CPU | d1.1, OpenRouter |
|---|---:|---:|---:|
| AUROC, answering vs not (50 vs 254 pairs) | 0.784 | 0.944 | **0.953** |
| tokens the answering model reads, at 0.5 | — | −87% | **−84%** |
| answers lost, at 0.5 (of 36) | — | 3 | **0** |
| false accepts, at 0.5 (of 268) | — | 8 | 7 |
| best threshold with no answer lost | — | 0.3: −83%, 21 false accepts | 0.7: −85%, 5 false accepts |
| latency per decision, median / max | — | 8.7 s / 25.0 s | 0.2 s / 0.8 s |

- **Lost by d1-2B at 0.5:** `contract-warranty`, `report-complaint`, `sds-un`. Each answer sits in one
  line of a long passage (1,794, 865 and 481 tokens). At 0.3 none is lost.
- **d1.1 is sharper.** Answering passages score a median 0.97, the rest 0.00. Its threshold band with no
  loss runs from 0.1 to 0.7.
- **Neither abstains reliably** on the 2 questions with no answering candidate: 1 of 2 at 0.5.

### The cacheable shape, concurrency and the hosted route

`throughput.py` moves the user's question out of the state into the noul instruction, so the state is
the passage alone and every question about one passage shares it as a prefix. Same 304 pairs:

| d1.1, OpenRouter | AUROC | answers lost at 0.5 | false accepts | wall time | decisions/s |
|---|---:|---:|---:|---:|---:|
| question in the state (gate.py) | 0.953 | 0 | 7 | — | — |
| question in the instruction, 1 in flight | 0.958 | 0 | 5 | 77.8 s | 3.9 |
| question in the instruction, 8 in flight | 0.962 | 0 | 4 | **10.0 s** | **30.3** |

The reshaped request loses nothing and makes a search's 8 decisions about 0.25 s of wall time.
The CPU rows of the same benchmark, cache off and on, are in `throughput.md` once measured.

## What this does not show

- **Aura's own candidates.** BM25 stands in for the fused, reranked `document_search`; a gate over
  candidates Aura already ranks better may save less.
- **Meaning.** The oracle checks tokens. A passage holding "36" for a 36-month warranty counts even if it
  is another 36; a paraphrased answer does not count.
- **Every answer a question needs.** A question counts as kept when one answering passage survives.
  For lists (all unpaid invoices) one surviving passage may not be the whole answer.
- **Answer quality.** No model answered anything: the saving is what the gate passes on, not a measured
  change in the final answer.
- **Out-of-sample thresholds.** 0.3, 0.5 and 0.7 are read off these 304 pairs.
- **Other languages, real operator documents, other corpora.** All 35 files are synthetic Italian
  office documents written for Aura's tests.
- **What `liquid/d1.1` is.** The catalogue does not say which size or weights it serves.
