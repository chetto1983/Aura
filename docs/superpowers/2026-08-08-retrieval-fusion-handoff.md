# Handoff — 2026-08-08, retrieval fusion and the abstention hole

`master` at `a25666202`. Supersedes the ranking sections of
`2026-08-08-document-plane-handoff.md`; every item that handoff left open and this session
did not touch is carried forward verbatim below.

**The ranking is fixed and measured. The abstention does not exist**, and until it does a
retrieval benchmark measures half the problem — a system that always answers with maximum
confidence cannot be trusted by an agent. That is the next session's work and it is fully
specified in §Abstention.

## What shipped

| | commit |
|---|---|
| The file-manager e2e spec targeted the wrong DOM node | `79d0ad5da` |
| nanoid DoS (GHSA-28wg-ghj8-5hjv) + Linux dist rebuild | `ac2046898` |
| The chunk budget ignored the embedder's prefix; the ingest tests ran nowhere | `167567d71` |
| The measurement harness for the ranking nobody had measured | `a9bd4e9b1` |
| ArcadeDB ranks; the invented tier ladder is deleted | `a25666202` |

CI went from one red job to **24/24 green**.

## The measurement that decided it

`retrieval_rank.go` fused the two legs with an invented tier ladder — lexical scores in
`[3,4)`, dense in `[2,3)` — so ANY lexical hit outranked EVERY dense one. Nobody had ever
put a number on it. Corpus: MMDocIR (Apache-2.0, expert-annotated), 25 Laws documents, 909
passages, 20 text-only questions naming neither file nor content, 15 of the 25 documents
carrying no question so a hit on them is a real miss. Scored with `pytrec_eval`.

```
arm                recall@1  recall@3  recall@5    MRR  nDCG@10
tier ladder (was)     0.300     0.500     0.700  0.464    0.568
vector.fuse RRF       0.850     0.850     0.950  0.881    0.909
vector.fuse DBSF      0.850     0.900     0.900  0.875    0.882
vector.fuse LINEAR    0.850     1.000     1.000  0.925    0.945
```

RRF is the shipped default for the reason the manual gives: it ranks by position, so it
never reconciles a cosine distance against a Lucene score — which is precisely the
reconciliation that scored 0.300. LINEAR ranks the top three perfectly but the manual
reserves it for tuned weights, and one pilot in one domain is not a tuning campaign.

The harness is committed (`internal/documents/retrieval_fusion_bench_test.go`, build tag
`retrieval_bench`) and runs in ~8s against a live corpus.

## What broke, and why it is worth reading

Three defects cost most of the session. All three are the same shape: **something was
verified in one place and shipped from another.**

**1. I measured one query and shipped a different one.** The 0.850 measurement used
`groupSize: 1`, 200 dense neighbours, `LIMIT 10`, no RID filter. The first implementation
shipped `groupSize: 3`, 800 neighbours, `LIMIT 200`, plus a filter and an `AND active` —
five drifts at once — and scored **0.000**. The query's parameters are now constants in
`document_retrieval.go` with the reason attached, not knobs.

**2. `decodeCandidates` re-sorted the engine's ranking away.** It ran a comparator with a
branch per leg; the fused leg matched none of them and fell through to `document_id ASC,
ordinal ASC`. The ranking arrived correct over HTTP and left grouped by document and
ascending. I spent an hour proving the SQL was right — six variants, both endpoints, bound
parameters — while ten lines of Go downstream were discarding it. `vector.fuse` orders its
own output and the engine asserts it in `SQLFunctionVectorFuseTest`; Go keeps that order now.

**3. The fused score reads back as `1/(60+rank)` per contributing source.** This is the
cheapest diagnostic in the system and it was sitting in plain sight the whole time: a top
score of `1/61` means ONE source contributed; `~1/209` means the right passage arrived at
rank 209. Read the arithmetic before theorising about transports.

A fourth, smaller: **a Javadoc is not a contract.** `vector.rerank`'s doc comment implies an
options map; the server rejects it and prints the real signature,
`vector.rerank(<source>, <queryVector>, <embeddingProperty>, <k>)`. One 500 gave the truth
that reading the comment did not.

## The ingest defect this surfaced

Driving 25 real PDFs through the real pipeline exposed a live bug the canary fixtures never
could. `chunk()` guarantees text of at most 2048 tokens, then `_embed` sends
`"title: none | text: " + text` — the prefix is **7 tokens** by the server's own `/tokenize`
— so any chunk near the top of the budget overflows and the embed server answers HTTP 500.

- Before: 19/25 documents, 528 passages, 6 rejections. **Six documents silently absent from
  the index.**
- After: **25/25 documents, 909 passages, 0 rejections.**

Production runs `AURA_INGEST_LIVE=true`, where that exception is swallowed into the next
cycle, so the loss is silent — an unindexed document looks exactly like a document with
nothing to say. It has not fired on the live corpus yet: it needs a chunk reaching the last
few tokens of the budget, which long statutes do and the three files in the live bucket do
not. Latent, not harmless.

The test that should have caught it asserted `count_tokens(chunk) <= 2048` — the text alone,
never the payload. And it could not have caught anything anyway: **nothing runs the ingest
tests**, no CI job and no Makefile target, so both other test modules had rotted into
ImportErrors that failed collection for the whole suite. Fixed; the suite is **52 green**.
Wiring it into CI is still open.

## Abstention — the next session's work

Measured on the live stack, one Italian document, three questions:

| question | rerank | dense distance |
|---|---|---|
| answerable | 0.584 | 0.416 |
| unanswerable, same domain | 0.471 | 0.529 |
| off-topic | 0.121 | 0.879 |

Two findings:

- **`vector.fuse` cannot abstain, by construction.** RRF ranks by position and discards
  magnitude; DBSF and LINEAR normalise *within the returned set*. The top result gets the
  maximum score even when it is the least-bad of a set of irrelevant documents. Measured: an
  answerable and an unanswerable question returned the same document at the identical score
  `0.032787` (= `1/61 + 1/61`). The fused row carries `distance: null` —
  `SQLFunctionVectorFuse` consumes distance (`return -n.floatValue()`, line 262) and never
  re-emits it.
- **`rerank ≡ 1 − distance` in this deployment**, exactly, on all three rows. The passage
  index is full precision (NONE-quantized), so rerank recomputes the same cosine. The note in
  memory saying *"abstain on rerank score, never on distance"* came from a quantized index
  and does not apply here — **correct that memory**.

The method to use is **arXiv 2402.12997, "Towards Trustworthy Reranking: A Simple yet
Effective Abstention Mechanism"**. It does not detect unanswerable questions; it estimates
whether the *ranking* is reliable, from the score distribution: max, standard deviation, and
the **top1−top2 gap**, plus a linear regressor over sorted scores that needs only ~38
labelled examples and costs +1.2% at inference. That is why the single-document pilot could
not settle anything: with one candidate, std and the gap do not exist.

Do not implement the maths. The authors' code is **`artefactory/abstention-reranker`**,
MIT: `abst_utils.py` has `get_scorer_max`, `get_scorer_std`, `get_scorer_1_minus_2`,
`get_scorer_linreg`; `eval_utils.py` has `compute_naucs`. The scorers take plain arrays, so
they are independent of the repo's data loaders. Caveats: 5 stars, last push 2024-09,
research code not a maintained library, and `abst_utils` imports torch and xgboost at module
level for scorers we do not need — install it in the disposable scoring container (the one
already used for `pytrec_eval`), never in Aura's runtime.

**Plan, in order.** Corpus is downloaded and extracted already (see below).

1. Ingest the 130 Italian documents into the disposable identity.
2. Generate questions on all of them — answerable and unanswerable — with the validated
   generator.
3. Collect the FULL sorted score vector per query, not just top-1: std and the top1−top2 gap
   only exist that way.
4. Score with the authors' code: three reference-free scorers + the regressor, reported as
   **nAUC**. Train the regressor on one slice and evaluate on another, or it grades itself.
5. Only then decide what reaches Aura's code.

Two things to state in the report rather than discover late: the paper measures abstention
against *ranking quality*, not against an unanswerable/answerable label, so the unanswerable
questions enter as zero-quality cases and that mapping must be written down. And an
"unanswerable" question is only unanswerable *by its generating document* — with 130
same-domain documents a sibling sometimes answers it, so a retrieval hit is not automatically
a failure to abstain and each one needs reading before it is scored.

## Corpus and tooling already built

- **130 Italian documents** from dati.gov.it (CKAN, `https://dati.gov.it/opendata/api/3/action/`,
  no auth): 55 PDF + 75 CSV, 113 MB, 130 distinct datasets, 81 organisations, deduplicated by
  SHA-256, every file content-validated. In
  `…/scratchpad/italia/docs/` with a 9-key `manifest.json` carrying the official Italian
  title, notes, tags and organisation per file. 77 candidates were discarded: 40 dead links
  (a third of resource URLs fail — the portal is a catalogue over ~40 regional sites, not a
  file host), 14 content-validation failures, 11 duplicates, 7 oversize, 5 degenerate stubs.
- **Text extracted** for all of them with Aura's own extractor (`services/ingest/extract.py`)
  so the questions are written from the same text the pipeline indexes. Five of the 55 PDFs
  carry no `/Font` object — scanned images, no text without OCR.
- **Question generator** at `…/scratchpad/gen_questions.py`, validated end to end on one
  document. Two traps it already encodes: Qwen3.5 is a reasoning model and returns an EMPTY
  `content` while spending the whole budget in `reasoning_content` (measured at 600 AND 2500
  tokens) — `chat_template_kwargs: {enable_thinking: false}` fixes it in ~43 tokens; and the
  model readily produces meta-questions ("da quale fonte sono tratti i dati nel testo?")
  which identify no document and are filtered.
- **printing-press** is installed (binary v4.30.1 + 9 skills). It was NOT used for the
  harvest, deliberately: it generates a reusable Go CLI over an API, and the difficulty here
  was never the API surface — three CKAN endpoints, no auth — but the dead-link tail across
  regional portals, which it does not address. Reasonable if dati.gov.it becomes a recurring
  interactive source.

## Disposable resources to clean up

Created this session, all outside production:

- ArcadeDB databases `mem_4ac9d11d_7a9c_4ab1_be02_e12b3f99e60a` (MMDocIR, 25 docs) and
  `mem_17815d5a_ed64_4c4c_b9a4_424fba3defa4` (Italian pilot, 1 doc).
- Garage buckets `aura-4ac9d11d-…` and `aura-17815d5a-…`, plus key `bench-mmdocir`.
- Docker image `aura-webbuild:local`.

The live `aura` identity, its bucket and its database were never touched.

## Open — carried forward, untouched this session

1. **A chat attachment shows as `<assetID>.pdf`.** The key carries the extension, not the
   name; the reconciler names a record from its key. S3 object metadata is the channel.
2. **CSRF defence-in-depth thinned.** The file-manager writes no longer gate on Content-Type.
   Not exploitable behind `SameSite=Strict`, but one control fewer. Restore by refusing
   `application/x-www-form-urlencoded` and `multipart/form-data` while still accepting the
   `text/plain` the widget sends. ~5 lines, needs live verification.
3. **`internal/documents` catalog store/service + the drop migration**, ~4.5k LOC. Migration
   slot = whatever `ls internal/db/migrations/ | tail -1` says at the time.
4. **filecard: 442 LOC in `xlsx.go` + 270 in `ooxml.go`** against excelize (BSD-3).
5. **Images and audio as their own families** — CocoIndex's `live-image-search` and
   `audio-to-text` are additions to `process_file`, not rewrites.
6. **`arcadedb_integration` runs NOWHERE.** Ten test files carry the tag — the whole LOCOMO
   memory suite included — and neither CI, the Makefile nor the coverage scripts execute it.
   Not even `go vet`-compiled, so it rots silently.
7. **`docker_integration` feeds no coverage.**
8. **`scripts/quality_snapshot_gate.sh` calls `python`**, which WSL lacks (python3 only).
9. **Ingest package-name trap.** The image COPYs `services/ingest` → `/app/ingest`, so the
   deployed package is `ingest`. A module must use `ingest.` or a relative import.
10. **`extract.py`'s soffice-exits-0-but-produced-no-file branch has no test**; `chunk.py` is
    155 LOC against a 120 instruction.
11. **`TestStageBoxArtifact_ExtractsRegularFile`** fails on Windows only (0600 vs 0666) and
    passes under WSL. It is the only red in `./internal/... ./cmd/...`.

## Environment notes

- `aura-ingest` is behind `profiles: [ingest]`; a bare `compose up -d` never starts it. The
  benchmark runs it as a one-off `docker run` with `AURA_INGEST_LIVE=false`, so extraction
  errors propagate instead of being swallowed.
- ArcadeDB is **26.7.3**, above the 26.5.1 floor `vector.fuse` and `groupBy` require.
- `docs.arcadedb.com` returns the index shell to a fetch; read the asciidoc source with
  `gh api repos/ArcadeData/arcadedb-docs/contents/src/main/asciidoc/<path>.adoc --jq .content | base64 -d`.
  Deep pages (`/use-cases/knowledge-graph`) do fetch correctly.
- Git Bash rewrites POSIX paths in `docker` and `gh api` arguments; prefix with
  `MSYS_NO_PATHCONV=1` or omit the leading slash.
- Windows Python cannot open Git Bash's `/tmp/...`; write to the scratchpad instead.

## The lesson this session cost

Every hour lost went to the same mistake in a different costume: **verifying one thing and
shipping another.** A query measured at one set of parameters, shipped at another. An SQL
statement proven correct over HTTP, then re-sorted in Go. A signature read from a comment
instead of from the server. The habit that broke each of them was the same and it is cheap:
run the real thing, read the number it produces, and make the shipped artifact byte-identical
to the one that was measured.
