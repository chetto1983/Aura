# d1 labelling Aura's document cards, and what concurrency and the cache buy on CPU — 2026-11-02

**Verdict: PARTIAL.** From the card Aura keeps after ingest, a decision model names a document's type
where Aura today names none: 34 of 34 with the file name visible, 31 to 32 of 34 without it, against 22
to 23 for the rule an operator would write first. d1-2B on a 4-core CPU does it in 14 s per card, which
suits a background pass at ingest. On that CPU neither concurrency nor llama.cpp's prompt cache made it
faster. PARTIAL: 34 synthetic documents, one run, gold types partly assigned by the spike's author.

## Protocol

- **Documents.** The 35 files of the two release fixture corpora less one (34 with gold). Gold: the
  office corpus's library in `document_filter_questions.json` (type, counterparty, tags, written for an
  earlier acceptance test); the complex corpus's types in `gold_complex.json`, assigned by this spike's
  author before any prediction.
- **State.** `aura-filecard`'s card plus the workbook header pairs, what `IndexedDocument` holds.
  Condition `blind` replaces the file's stem with "documento", since the fixtures' names give the type
  away and real uploads are often `scan_0012.pdf`.
- **One call per card:** type (choice over the 18 types), counterparty (choice over the 5, plus
  "nessuno"), and one noul per tag. Criteria are the names alone, as docfields lists hold no
  descriptions.
- **Baselines.** Aura today with no rule: no type, no tag, the extractor's counterparty from a
  workbook's `Cliente`/`Fornitore` header pair. And the first rule an operator writes: each name as an
  exact word sequence, longest match wins.

## Labels

| | d1-2B Q4_K_M, CPU | d1.1, OpenRouter | first rule | Aura today |
|---|---:|---:|---:|---:|
| type, name visible | **34/34** | **34/34** | 23/34 | 0/34 |
| type, name hidden | **31/34** | **32/34** | 22/34 | 0/34 |
| counterparty | 32/34 | 34/34 | 29/34 | 33/34 |
| tags, office corpus: found / false | 3/4, 0 | 4/4, 3–5 | 1/4, 0 | — |
| latency per card, median / max | 14.2 s / 62.5 s | 0.24 s / 1.30 s | | |

- **Errors with the name hidden.** d1-2B: `magazzino_ricambi` → piano manutenzione, `turni_reparto` →
  registro movimenti, the scanned PDF → nota. d1.1: `note_pagamenti` → scadenzario, the scanned PDF →
  fattura. Tabular workbooks look alike from their card; the scanned PDF has no text at all and still
  gets a type — the case an explicit abstention option would catch (rizzo-flow's `__insufficient__`).
- **The extractor already holds the counterparty** for the office invoices, 33 of 34; d1 adds nothing
  there.
- Tags are scored on the office corpus only: `gold_complex.json` assigns no tags, so its "no tag" is
  unknown, not a negative.

## Concurrency and the prompt cache, d1-2B on 4 CPU threads

`throughput.sh`: 40 reading-gate pairs, the question in the instruction and the passage alone as state,
so 21 of the 40 reads repeat a passage already read. The server restarts before each run.

| run | wall | decisions/s | latency median / max | first read / repeat, median |
|---|---:|---:|---:|---:|
| no cache, 1 in flight | 265.0 s | 0.151 | 2.99 s / 22.2 s | 3.17 s / 2.69 s |
| no cache, 4 in flight | 250.8 s | 0.159 | 9.57 s / **250.8 s** | 10.63 s / 9.48 s |
| cache 4 GiB, 1 in flight | 252.0 s | 0.159 | 2.94 s / 19.7 s | 3.04 s / 2.84 s |
| cache 4 GiB, 4 in flight | 252.0 s | 0.159 | 8.59 s / **252.0 s** | 6.24 s / 9.15 s |

- **Concurrency buys nothing here** and starves requests: with 4 in flight, the requests that started on
  slots 2 and 3 at second 4 were released only at the end of the run (server log), so one request waited
  the whole 250 s. On this build and CPU, local decisions should be sent one at a time.
- **The cache does not make a repeated passage faster** (2.84 s against 3.04 s). The server logged a
  slot reuse by longest common prefix once in 40 requests. Why the passage prefix is not reused is not
  established: the layout llama.cpp's decision template gives state and questions was not read.
- The contended first attempt, while the centroid arm's embeddings shared the CPU, is kept apart in
  `results/throughput_cpu_partial.jsonl` and is not a measurement of either lever.
- On the hosted route, 8 in flight gave 30.3 decisions/s against 3.9 for one
  (`results/throughput_openrouter.jsonl`): there concurrency is the lever.

## What this does not show

- **Real operator libraries.** 34 synthetic files with descriptive content; the blind condition removes
  the name but not a title inside the card ("Manuale d'uso e manutenzione").
- **An empty or growing list.** The 18 types are given. On a fresh install the lists are empty and a
  decision model has nothing to choose from.
- **A GPU.** Latency and the cache were measured on 4 CPU threads only.
- **Counterparty beyond invoices**, and tags beyond the two in the office library.
