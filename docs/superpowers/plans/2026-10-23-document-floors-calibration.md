# Document relevance floors for the cloud space Implementation Plan

**Status (2026-10-25):** Tasks 1 and 2 done (overrides `95514022d`; measurement
`docs/superpowers/verification/2026-10-23-document-floors-pplx.md`). Task 3's row (`1b70f7ffa`) and
the review's fixes (`52da38196`) are on master, CI green on `ea9c5969a`; the lab VM check waits for
the updater. Operator's decisions on 2026-10-23: "Frizioni + soglie ora", "CLI + override", and
"0.39" over 0.35.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for
> tracking. Commits on master.

**Goal:** measure the document relevance floors for the lab VM's embedding space,
`es1-b604719dc5fc0ce4` (`openrouter perplexity/pplx-embed-v1-4b`, 768d, recipe 1), and record them, so
that `document_search` abstains on the operator's own library.
- The space has never been calibrated. It runs on EmbeddingGemma's floors (0.72 / 0.32) and reports
  `uncalibrated_floors`.
- Aura's agent saw "fattura", filtered to two unrelated documents, admitted at 0.36 and 0.38.

**Architecture:**
- **The tool.** Two operator overrides for the document floors, `AURA_DOCUMENT_DENSE_MAX_DISTANCE_RATIO`
  and `AURA_DOCUMENT_MIN_RELEVANCE`. They mirror the memory pair the cockpit already has. With them set on
  one `aura docs search` process inside the container, the CLI shows every candidate's rerank cosine,
  including those the floor drops. The daemon is never touched.
- **What the CLI reads.** `aura docs search` already reads the operator's identity
  (`identityctx.OperatorIdentity`: the one enrolled identity). Measured on the VM: it returns the
  operator's `esempio.md`, so no `--identity` flag is needed.
- **The measurement.** Questions are written against the operator's own passages, following
  `feedback_measure_on_garage_documents` and spec §9's procedure (the 2026-09-02 and 2026-09-09
  calibrations). Each one has its top-1 rerank cosine read with the floors off.
- **The calibration.** Only once the measurement exists: the PRD and §9 amendment cites it, then the
  table gains the space's row.

**Tech Stack:** Go (`internal/config`, `internal/arcadedb`, `cmd/aura`); the lab VM.

**Spec:** `docs/superpowers/specs/2026-09-23-embedding-model-change-design.md` §9, the relevance floors.

## Global Constraints

- **Override semantics are memory's.** Zero means the floors measured for the space. A positive value
  wins.
  - The ratio is in (0, 2], and the relevance in (0, 1].
  - `floors_reason: uncalibrated_floors` is still reported for a space with no row, overridden or not.
- **No secret leaves the container.** The CLI embeds on the cockpit's own route and key (`816a1f6f3`).
- **No document is uploaded or changed.** The measurement reads the operator's library as it stands.
- **The memory family is not measured here.** A row must not claim memory floors that nobody measured.
  Task 3 decides how the table says so, once the measurement is in hand.

## Review Focus

- **An override outside its range** (negative, above 2 or 1, not a number) refuses the config at boot,
  like `AURA_DOCUMENT_RETRIEVAL_CANDIDATES`. It is never silently ignored.
- **The overrides reach both dense legs.** `bindDense` serves the fused passage leg and the card leg. An
  override on one only would measure half the retrieval.
- **An override does not mark the space calibrated.** `floors_reason` is unchanged.

---

### Task 1: the document floor overrides

**Files:**
- Modify:
  - `internal/config/config_document_retrieval.go` and its test;
  - `internal/arcadedb/document_schema.go` (`DocumentIndexConfig`);
  - `internal/arcadedb/document_retrieval.go` (`bindDense`) and `document_cards.go`, the callers;
  - `internal/arcadedb/relevance_floors.go`;
  - `cmd/aura/document_index_wiring.go`;
  - `prd.md`, in the env catalog.

**Interfaces:**
- `DocumentRetrievalConfig.DenseMaxDistance, MinRelevance float64`.
- `DocumentIndexConfig.DenseMaxDistance, MinRelevance float64`.
- `func (cfg DocumentIndexConfig) documentFloors(space string) denseFloors`. It replaces the free
  `documentFloors(space)` and applies an override when it is positive.

- [x] **Step 1: Tests.**
  - `TestDocumentFloorOverridesLoadAndValidate` (config):
    - unset gives 0 and 0;
    - `0.9` and `0.4` are kept;
    - `-1`, `2.5` for the ratio, `1.5` for the relevance and `abc` are each a `Fatal` violation.
  - `TestDocumentFloorsHonourOverrides` (arcadedb, pure):
    - with no override, the space's floors (EmbeddingGemma's for an unknown space);
    - with each override, that value;
    - `FloorsCalibrated` is unchanged.
  - Test `bindDense` through both legs' parameter maps: the overridden value reaches `max_distance` and
    `min_relevance`.
- [x] **Step 2: Run** `go test ./internal/config/ ./internal/arcadedb/`. Expected: FAIL.
- [x] **Step 3: Implement.** Wire the overrides through `newRuntimeDocumentIndex`. Add both variables to
  the PRD's env catalog, beside the `AURA_MEMORY_*` pair.
- [x] **Step 4: Run** `go test` and `-race` on both packages, `go vet ./cmd/aura/`, and the arcadedb live
  document tests. Expected: PASS.
- [x] **Step 5: Commit** `feat(documents): operator overrides of the document relevance floors`.

Measured: unit and race green on both packages; 53 live ArcadeDB document tests passed, none skipped;
`go vet` clean on `cmd/aura` and under the `retrieval_eval` and `arcadedb_integration` tags.

### Task 2: measure on the lab VM

Once the updater has applied Task 1's SHA.

- [x] **Step 1: Write the question set** against the operator's passages:
  - read the 8 documents' passages through the CLI;
  - write questions the library can answer, identifiers and file names among them;
  - write questions it cannot answer: the 2026-09-09 set's out-of-corpus five, plus questions near the
    library's topics that it does not answer.

  Keep the set in the scratchpad, never in the repository: it quotes the operator's documents.
- [x] **Step 2: Measure.** For each question, run
  `docker exec -e AURA_DOCUMENT_MIN_RELEVANCE=0.0001 -e AURA_DOCUMENT_DENSE_MAX_DISTANCE_RATIO=2 aura aura docs search "<q>" --limit 3`.
  - Record the top-1 `fused` score, the rerank cosine, and whether it is the right document.
  - Repeat the answerable ones with only the relevance override. That shows whether the 0.72 distance
    drops a true match from the dense leg.
- [x] **Step 3: Write** `docs/superpowers/verification/2026-10-23-document-floors-pplx.md`. It holds the
  bands, the midpoint, and what the measurement does not show: one library of 8 documents, one day,
  top-1 only, one model.

### Task 3: record the floors

Written after Task 2, from its numbers: the §9 and PRD amendment first, then the table row. Memory is not
measured, so the row's memory floors stay uncalibrated. The table gains a per-family measured flag, or
something the measurement shows is simpler. Then deploy, and continue the operator's conversation: the
agent's "fattura" with `Cliente` must abstain.

- [x] **The amendment:** `4a5a632b1` (§9, PRD, verification).
- [x] **The row:** `1b70f7ffa`, one table per family; the review's fixes in `52da38196`.
- [ ] **On the lab VM:** the agent's "fattura" with `Cliente` abstains. The VM was off from
  2026-10-23 evening to 2026-10-25 and still runs `95514022d`; this waits for the updater.

**Final review** (fresh reviewer, read-only, 2026-10-23): "With fixes", no Critical.
- Fixed in `52da38196`:
  - the cockpit's EN/IT sentence that floors were "measured for EmbeddingGemma only";
  - the preview checking memory in the documents' space;
  - the CLI not naming a malformed override;
  - `.env.example` presenting the overrides as daemon settings;
  - a test assertion that could not fail;
  - the cost of 0.39 attributed to the wrong band.
- Open: the VM check above.
- Deferred minor: the cockpit could name the uncalibrated family instead of saying "memory,
  documents or both", with two `*_floors_calibrated` fields.

## Decisions taken while writing this plan

- **No `--identity` flag.** `aura docs search` already resolves the operator's identity. The VM probe
  returned the operator's `esempio.md`.
- **Overrides on the CLI process only.** `docker exec -e` sets them for one command. The daemon and the
  agent keep the table's floors throughout the measurement.
- **`AURA_DOCUMENT_*`, not `AURA_DOCUMENTS_*`.** The package's other knobs are `AURA_DOCUMENT_RETRIEVAL_*`.

Taken while executing Task 1:
- **`.env.example` and spec §9, not the PRD.** `prd.md` has no env catalog any more, and the
  `AURA_MEMORY_*` pair is not in it either. `.env.example` lists the document knobs, so the pair went
  there, commented, and §9 names it.
- **An unparsable value loads as NaN.** The config cannot carry a parse error, and a silent zero would
  mean "the measured floors". `Validate` refuses NaN with the out-of-range values.
- **`DocumentIndexConfig` checks the ranges too.** `aura docs search` loads its config without the
  boot gates, so the index refuses an override outside its range on its own.
- **Not in the cockpit's settings.** The measurement needs one CLI process only. Whether the daemon
  should read them from `aura.settings` is Task 3's to decide.

Taken while executing Tasks 2 and 3:
- **Aura wrote the questions.** The operator's direction was "se collaborate te ed Aura sistemate
  tutto". In the operator's conversation, the agent listed its library and wrote 12 answerable
  questions and 12 hard negatives. It also asked to keep file-name lookups apart from semantic ones,
  and "topically relevant" apart from "holds the answer". The 2026-09-09 out-of-corpus five were added.
  - Cost if wrong: the positives may be easier than an operator's, since the agent had read the
    documents.
- **The score the floor decides on** is the top-1 document's higher leg, the passage or the card,
  because a document is admitted when either leg clears it.
- **One table per family.** `calibratedFloors` became `memoryFloorsBySpace` and
  `documentFloorsBySpace`. `FloorsCalibrated` became `MemoryFloorsCalibrated` and
  `DocumentFloorsCalibrated`; the cockpit's card and preview require both. A per-family flag on one
  struct was the alternative, but two flat maps say "measured" by having a row.
- **The library has 8 documents, not the 6 the agent found.** The video and the image surfaced only
  with the floors off: the agent had no way to list the library.
