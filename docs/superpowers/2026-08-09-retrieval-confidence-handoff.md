# Handoff — 2026-08-09, retrieval confidence, and two documents that vanished

`master` at `8efc9368e` + the PRD amendment. Supersedes the §Abstention section of
`2026-08-08-retrieval-fusion-handoff.md`; everything that handoff left open and this
session did not touch is carried forward verbatim in §Open.

**The abstention work the previous handoff specified should not be built.** It was
specified on an assumption — that `vector.fuse` cannot carry a confidence signal — and the
measurement says the opposite. The signal is in the RRF score we already compute
(nAUC **0.809**, ROC AUC **0.880**), and the agent already abstains on content without it
(**0 confabulations in 14 completed unanswerable turns**). Ratified as PRD **amendment
#119**.

## What shipped

| | commit |
|---|---|
| `/tokenize` omits BOS/EOS; the embeddings endpoint adds them. Two tokens, one lost document | `35704eace` |
| `_window_split` never measured what it emitted; up to 2350 tokens against a 2048 ceiling | `8efc9368e` |
| Amendment #119 — retrieval confidence measured, abstention gate refused | this commit |

Recovered by the two fixes, measured end to end on the 130-document Italian corpus:

| | indexed / uploaded |
|---|---|
| before | 125 / 130 — **2 lost in silence**, exit code 0 |
| after | **127 / 130**, 0 failures |

The 3 still missing are scanned PDFs with no extractable text — an honest zero.

## The measurement

Harness `internal/documents/retrieval_abstention_eval_test.go` (tag `retrieval_eval`)
drives the SHIPPED path — `FusedCandidates`, RRF, `groupBy: document_id`,
`groupSize: 1`, 200 dense neighbours — and emits, per query, the fused document order, the
fused score, and the full-precision similarity of the same candidates via the engine's own
`vector.rerank` (4-arg signature, confirmed against the server, not the javadoc).

Scorers and baselines are `artefactory/abstention-reranker` (MIT) imported **verbatim**;
`authors_shim.py` stubs torch/xgboost/datasets so their files stay byte-identical instead
of being reimplemented. They run in a disposable scoring container, never in Aura.

**Arm A — `vectara/open_ragbench`**, real qrels: 1000 arXiv documents (400 gold-bearing,
600 hard negatives the dataset asserts irrelevant to every query), 20,743 passages, 1,914
text-only queries, 0 ingest failures.

Retrieval: gold in top-20 **96.9%**, gold at rank 1 **71.7%**, NDCG@20 **0.841**.

nAUC predicting the quality of the ranking Aura ships (1.0 = oracle, 0.0 = random):

| confidence from | max | std | top1−top2 | linreg |
|---|---|---|---|---|
| similarity (`vector.rerank`) | 0.330 | 0.195 | 0.447 | 0.408 |
| **fused RRF score** | **0.809** | 0.559 | 0.691 | 0.779 |

The previous handoff's claim that RRF "discards magnitude" is half true and the half it
misses is the whole point: `Σ 1/(60+rank)` encodes **how many legs found the passage and
at what rank**. `2/61` against `1/61` is information. A full-precision rerank costs an
extra query and predicts the shipped ranking *worse*.

Coherence check passed — each score predicts the ranking it produces (similarity →
rerank-ranking 0.683–0.857; fused → fused-ranking 0.809). A different result would have
indicted the harness.

**Arm B — 130 Italian open-data documents**: a much dirtier instrument, and useful mainly
for what it revealed about itself. Gold at rank 1 **42.7%**, NDCG **0.400**. Restricting
to the 55 PDFs raised nAUC from 0.234 to 0.378 — not because CSV retrieval is worse (it is
identical, 42.7% both) but because **RAG-on-spreadsheets is not the design**:
`document_search` finds WHICH file, `document_open` hands it over, the agent computes with
soffice/pandas. `document_open.go` says it outright: *"chunked text cannot answer
aggregates at any relevance."* Scoring passage-answering over 75 CSVs measured a
configuration the product does not use.

## Driving the real agent — the result that decided it

`aura shell` was driven one question per invocation against the corpus, on a **separate
control-plane database** so the operator's plane was never written to. 20 answerable + 20
unanswerable; 6 unanswerable turns exceeded a 420 s cap and are reported as **no verdict**,
never as abstentions.

| | outcome |
|---|---|
| answerable (20/20) | 19 correct, 0 wrong, **1 false refusal** |
| unanswerable (14/20 completed) | **0 confabulations**, 4 clean refusals, 5 answerable-after-all, 3 answered from the web, 2 borderline |

The best refusal did better than refusing — it rejected the question's premise: the NOx
critical value of 30 μg/m³ is for *vegetation*; for human health the limit is 40 μg/m³ of
NO₂. **No scalar threshold can represent that.**

The spreadsheet contract works: **7 of 10** answerable CSV questions ran
`document_search → document_open → shell_exec` and computed on the file. Zero aggregates
answered wrongly from chunks.

Three things worth more than the counts:

1. **The one bad case is not a retrieval failure.** The subagent diagnosed it as one; it is
   wrong and the check is in `records_italia.json`. For *"contributo di 1.846,00 € a quale
   comune"* the shipped fused ranking put the gold document at **rank 2 with the highest
   similarity in the list** (0.4768 against rank 1's 0.4270). `groupSize: 1` returns ONE
   slab per document, the CSV has exactly one passage containing `1.846,00`, and the agent
   got a different slab — then spent 24 tool calls and refused instead of opening the file.
   **This is the consumer for the confidence signal**: low confidence must steer toward
   `document_open`, never toward silence.
2. **When the corpus is silent the agent goes to the web, not to silence.** 34 web calls
   across the unanswerable set against 2 across the answerable one — and in one case it
   answered from the web while the gold document contained the sentence verbatim. Any
   abstention metric assuming "no corpus evidence → refusal" will never match the shipped
   agent.
3. **Grinding is the agent's own uncertainty signal.** Unanswerable questions cost 2.4× the
   tool calls, and 6/20 never terminated inside the cap. Free, already available, and no
   score exposes it.

## Corrections to things previously written down

- **`aura chat`/`aura shell` run as the OPERATOR identity**, not `aura-cli`.
  `chat_boot.go:100` → `identityctx.OperatorIdentity`. `CLIServiceIdentity` (`…0039`) only
  owns durable CLI idempotency records. Assuming otherwise ingests a benchmark corpus into
  a plane the agent cannot see.
- **The shipped agent runs on OpenRouter `deepseek/deepseek-v4-flash-0731`** (1M context),
  from `aura.settings` — not the local Qwen. Any note claiming Aura is fully local is stale.
- **"Abstain on rerank score, never on distance" is void here.** `rerank ≡ 1 − distance`
  exactly on this deployment (NONE-quantized passage index; verified on four rows), so the
  two are the same number, and neither is the best signal — the fused score is.
- **`arcadedb_integration` runs in CI** (Agent Memory MRS). Already corrected on 2026-08-08;
  restated because two handoffs carried the false version.

## Open — everything to fix

### From this session

1. **`compose.yaml` claims catch-up mode fails the run on an extraction error. It does
   not.** Measured: CocoIndex prints `component build failed`, the pass continues, the
   container exits **0**, and the document leaves no passage. The loss is silent in BOTH
   modes, not just under `AURA_INGEST_LIVE=true` as the comment at `compose.yaml:763-766`
   says. Fix the comment, and decide whether a catch-up pass should fail closed — an ingest
   that loses documents and reports success is the failure mode this whole session kept
   hitting.
2. **`_embed` swallows the server's message.** `app.py:202` raises a bare
   `HTTPError: HTTP Error 500` with no body, so "which chunk, how many tokens, why" costs a
   separate reproduction every time — it cost two today. Include the response body and the
   measured token count in the raised error.
3. **Nothing runs the ingest tests.** No CI job, no Makefile target. To run them you need
   `services/ingest` mounted at `/app/ingest`, the fixtures at `/fx`
   (`scripts/fixtures/document_pipeline_e2e`), `ARCADEDB_PASSWORD` + `ARCADE_HTTP` +
   `AURA_EMBED_BASE_URL`, and `pip install pytest` (the image has no pytest). Without `/fx`
   25 of 53 fail; without the env, `test_arcade_integration.py` reads `os.environ` at import
   and fails **collection for the whole suite**. Current state: **53 passed** against the
   live stack.
4. **Two `retrieval_eval` harnesses are committed and nothing runs them** —
   `retrieval_fusion_bench_test.go` and now `retrieval_abstention_eval_test.go`. They are
   compile-checked by `scripts/tagged_tier_compile.sh` and never executed.
5. **The agent skips the corpus.** `answerable_pdf_07`: tools were `tool_search, web_search,
   web_fetch` — zero `document_search` — while the gold PDF states the answer verbatim. A
   correct answer from the wrong source is still a bug: it cannot be cited, and it will
   diverge from the corpus silently.
6. **The agent refuses instead of opening.** See §Driving, item 1. One in ten.
7. **6 of 20 unanswerable turns never terminated** inside 420 s. Worth measuring properly:
   an agent that grinds without bound on an unanswerable question is a cost and a latency
   problem before it is an abstention problem.
8. **Do not reuse the Italian question set as-is.** 5 of 14 "unanswerable" questions are
   answerable by a sibling document, verified row by row. An "unanswerable" generated from
   one document is not unanswerable by a 127-document same-domain corpus.
9. **Licences to settle before any pipeline fetches them.** `open_ragbench` is
   **CC-BY-NC-4.0** (non-commercial); `snap-research/locomo` is **NOASSERTION**. Both live
   only in disposable scratchpads today.

### Carried forward, untouched this session

10. **A chat attachment shows as `<assetID>.pdf`.** The key carries the extension, not the
    name; the reconciler names a record from its key. S3 object metadata is the channel.
11. **CSRF defence-in-depth thinned.** The file-manager writes no longer gate on
    Content-Type. Not exploitable behind `SameSite=Strict`. Restore by refusing
    `application/x-www-form-urlencoded` and `multipart/form-data` while still accepting the
    `text/plain` the widget sends. ~5 lines, needs live verification.
12. **`internal/documents` catalog store/service + the drop migration**, ~4.5k LOC.
    Migration slot = whatever `ls internal/db/migrations/ | tail -1` says at the time.
13. **filecard: 442 LOC in `xlsx.go` + 270 in `ooxml.go`** against excelize (BSD-3).
14. **Images and audio as their own families** — CocoIndex's `live-image-search` and
    `audio-to-text` are additions to `process_file`, not rewrites.
15. **LOCOMO provisioning is solved but not wired.** `AURA_LOCOMO_DIR` needs a file named
    literally `locomo_dataset.json`; `snap-research/locomo`'s `data/locomo10.json` is the
    exact shape and only needs renaming. `TestLocomoEnglishAnalyzerRecall` passes in 11.7 s
    with it. The other six were NOT run. `AURA_LOCOMO_FACTS` is a derived artifact the
    public dataset does not carry, so the facts tests stay skipped.
16. **`internal/agenteval/live_test.go`** carries `arcadedb_integration` in a package the
    evaluator never runs: compiled, never executed.
17. **`docker_integration` feeds no coverage.**
18. **`scripts/quality_snapshot_gate.sh` calls `python`**, which WSL lacks (python3 only).
19. **Ingest package-name trap.** The image COPYs `services/ingest` → `/app/ingest`, so the
    deployed package is `ingest`. A module must use `ingest.` or a relative import.
20. **`extract.py`'s soffice-exits-0-but-produced-no-file branch has no test**; `chunk.py`
    is now ~170 LOC against a 120 instruction (600 is the project cap).
21. **`TestStageBoxArtifact_ExtractsRegularFile`** fails on Windows only (0600 vs 0666) and
    passes under WSL.

## Disposable resources to clean up

Everything below is outside production. The operator's `aura` database, the
`aura-b130c94d-…` bucket and `mem_b130c94d…` were never written to; the operator's
`aura-box-b130c94d` / `aura-egress-b130c94d` containers are untouched.

- ArcadeDB: `mem_17815d5a_…` (Italian, 127 docs), `mem_21cdc28c_…` (ragbench, 1000 docs),
  `mem_4ac9d11d_…` (MMDocIR, previous session), `mem_00000000_…_0001` (agent-driving run).
- Garage buckets `aura-17815d5a-…`, `aura-21cdc28c-…`, `aura-4ac9d11d-…`,
  `aura-00000000-…-0001`, plus key `bench-mmdocir`.
- Postgres database `aura_bench` (separate control plane for the agent-driving run).
- Volumes `aura-ingest-state-{italia,ragbench,abstention}`; exited containers
  `aura-ingest-{italia,ragbench,abstention}`.
- Images `aura-abstention-score:local`, `aura-webbuild:local`.

## Environment notes

- `aura-ingest` sits behind `profiles: [ingest]`; a bare `compose up -d` never starts it.
  Run it as a one-off `docker run` with `AURA_INGEST_LIVE=false`.
- The embed server is **`-np 1`** (`AURA_EMBED_SLOTS`): one request at a time, ~87–110 ms
  per chunk, ≈10 embeddings/s. Raising it is not free — llama.cpp divides `-c` across
  slots, so two slots against `-c 2048` would silently truncate 2048-token chunks.
- **One GPU.** RTX 3060 12 GB, shared by both llama.cpp containers (`count: all`). The
  Intel UHD 630 is invisible to CUDA builds and would be slower anyway.
- `count(DISTINCT x)` answers **500** on ArcadeDB — use `GROUP BY`. Multi-type
  `FROM A, B` is a parse error.
- Git Bash rewrites POSIX paths inside `docker` and `gh` arguments; prefix
  `MSYS_NO_PATHCONV=1`. Windows Python cannot open Git Bash's `/tmp/...`.
- The Windows console is cp1252: it renders `À` as `�` and raises `UnicodeEncodeError` on
  a real U+FFFD. Set `PYTHONIOENCODING=utf-8` and write to a file before concluding
  anything about encoding. (Measured: the indexed corpus carries 77 replacement characters
  across 3,284 passages — 4 documents, 0.01% of text. Not a systematic bug.)

## The lesson this session cost

Yesterday's was "verify one thing and ship another". Today's is its sibling: **I measured
for six hours without ever reading what the system returned.** I generated 295 questions
with a 9B model, computed nAUC and ROC on them, declared the result unshippable — and had
not looked at a single retrieved passage. The first four I read showed the labels were
wrong, and the operator had to point out that RAG-on-spreadsheets was decided against weeks
ago, in a tool description I had already read and skimmed past. A number computed on
unexamined labels is not a measurement; it is a rumour with a decimal point.
