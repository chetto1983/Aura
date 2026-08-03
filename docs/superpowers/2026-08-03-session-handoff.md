# Session handoff — the v2 purge closed, seven items open

**Date:** 2026-08-03 · **Branch:** `master` (master-direct) · **HEAD:** `f09fa5c99`
**Working tree:** CLEAN. **Migration head:** 0089. **Live `aura` DB:** at 87 — see §Live drift.

Supersedes [2026-08-02-milestone-purge-session-handoff.md](2026-08-02-milestone-purge-session-handoff.md),
which stays as the record of the deletion arc. `.planning/` was retired at milestone
close and a parallel Codex session has begun regenerating it; until it is whole,
**this file is the board**.

Range totals across 2026-08-02/03: **+29,397 / −784,571 lines**.

---

## What shipped

| | commit |
|---|---|
| Neo4j leaves the codebase, and the chunk document plane with it | `5908b3e6f` |
| The WORD leaves too, plus 13 neo4j-* skills; three defects it was hiding | `1dcd4d406` |
| Reranker leg + `.planning/` + `docs/retrieval-eval.md` | `9f9f2d974` |
| The adaptive plane and the eval oracle; the reasoning tier kept | `acd029d47` |
| Memory rescued, `remember` fixed, `memory_reembed` given a caller | `f0214aad4` |
| The reasoning gate bites and runs: 45 prompts, 95%, CI job | `3ee76a666` |
| RLS fails closed; Telegram becomes a wrapper; markitdown deleted | `3341e0cc1` |
| A machine-written card per file; identity reaches the last stores | `13b64c6c2` |
| PRD amendment #107 | `c688d909e` |
| The 0087 outage, closed in two commits | `7974f8a6a`, `f09fa5c99` |

**The three numbers worth carrying forward:**

- **Card recall@1: 18.2% → 81.8%** (MRR 0.182 → 0.841), 22 Italian queries over the
  17-file baseline corpus against the real `SearchDocumentDigests` SQL.
- **Reasoning tier: 95% / 95%** over 45 held-out prompts, floors 93/93, and it now RUNS
  in CI. It was 24/24 against floors of 90/92 — saturated, and executed by nothing.
- **RLS: 64 × SQLSTATE 42501 → 0** across the four affected packages, with the policy
  provably unchanged.

---

## Open work — the operator's list, in their order

### 1. Phase 1 — the prose leg on ArcadeDB (`Document → Section`, ANN filtered by traversal)

Spec: [plans/2026-08-02-graph-native-document-retrieval.md](plans/2026-08-02-graph-native-document-retrieval.md) §Phase 1. Read its §Grounding first — it corrects the "chunking is dead" reading that this leg exists to undo.

The lever, from ArcadeDB's own docs: **`vector.neighbors` takes `{ filter: <RIDs> }`**, so a graph traversal picks the candidate set BEFORE any ANN runs. That is the answer to SIGMOD 2026 (arXiv 2603.23710) — filtered vector search is decided by page accesses, not distances, and published ANN benchmarks do not predict `WHERE tenant = ?`.

ArcadeDB and not a second engine: it is already the memory engine, so a remembered fact and a manual section land in the same graph and can share an entity vertex. Postgres keeps the catalog, the weighted tsvector and the RLS.

Acceptance is a recall@1 number against `D:/tmp/baseline-corpus`, compared to today's baseline of `document_open` + `pdftotext` + grep. If the graph leg does not beat that, say so.

### 2. Phase 2 — weighted fusion, measured

`vector.multiscore(scores, 'WEIGHTED', weights)`, `vector.hybridscore(s1, s2, alpha)`, `vector.rrfscore(ranks…, { k })` and `vector.normalizescores()` all exist server-side. The study measured that equal-weight RRF **hurts** when one leg dominates: dense 8/8 → hybrid 5/8, routing 0.751 → 0.532.

**Sweep alpha and report the curve, not the winner.** And the memory path's RRF, added 2026-08-01, is STILL UNMEASURED — measure it in the same pass or state that you did not.

### 3. Phase 4 — tabular ETL

Measured, `spikes/document-routing-scale/results_scale.txt`, 400 invoices:

```
open+compute (per query)   1193.4 ms   re-reads all 400 files EVERY query
ETL build (once, ingest)   1222.7 ms
SQL query (per query)          0.2 ms
results identical: True
```

Not an approximation that is faster — the same answer. Nothing tabular exists in `internal/db/migrations`.

Budget for **~25% of files not landing**: Auto-Tables (Microsoft, VLDB) reaches ~75% top-3 on 244 real cases, and <3% of real spreadsheets have a predefined data model. The fallback (open-and-compute) is a first-class path, not an error. Two hard don'ts: no **partitions** per file (threshold "hundreds") and no **unlogged** landing tables past ~1k.

Acceptance: `Clienti.xlsx`'s 699 TORINO clients answered by SQL, identical to open-and-compute, both timings reported.

### 4. A listable catalog tool

**"Che documenti ho caricato?" has nothing to call.** The four document tools are `search`, `open`, `index`, `describe`; `aura docs list` lists ingest JOBS, not the catalog. Found by reading `yairwein/document-mcp`, whose `get_catalog` is the shape.

It is the natural companion to the mechanical card: now that every file is carded by the machine, listing them is cheap and honest. Note this also explains a reasoning-tier residual — the classifier over-rates that prompt partly because there is no tool behind it.

### 5. A PDF text extractor at ingest

**The five Normattiva decrees are unroutable by subject.** They carry no Info title (producer "iText") and their text is Type0/CID glyph indices needing a ToUnicode CMap, so their cards are name + size + page count — which the card SAYS, rather than faking. They are routable by filename only, and their filenames differ solely by decree number and date.

Two routes: a real PDF extractor (600–1000 LOC of fragile parsing) or a sandbox `pdftotext` pass at ingest. **The container already has both.** The sandbox pass is almost certainly the right first move; measure the added ingest time against the 0.57s the whole 56 MB corpus currently takes.

### 6. Known debt — `MigrateSteps` down dies on a function 0086 dropped

`internal/cron`'s `TestDispatchPendingNotificationIdentityRoundTrip` runs `MigrateSteps down -1`, fails on `function aura.apply_adaptive_policy_transition(...) does not exist`, and **leaves the schema torn down** — every later package then fails on missing columns. That cascade is 13 packages / ~176 tests of noise and makes the whole-tree `db_integration` count unusable.

Related and already paid once: `0084`'s down ran a bare `COMMENT ON` against a table `0086` had dropped, wedging the rollback at 83 and cascading ~177 failures. It is existence-guarded now. **The downward walk still dies at 0075.**

A one-way-door migration is allowed, but its declared irreversibility must not silently break every later package's `Migrate`.

### 7. `internal/assets` — next to break, and it must go WITH its migration

`aura.assets` and `asset_events` are outside 0087's floor, so nothing fails today. All ten `assets.Store` methods already take `identityID`; only `NewStore` takes a bare `sqlc.DBTX`, so the reshape is the same type-assert used in `idempotency.New`.

**Do not land the reshape alone.** The pool field without the migration is dark code and no test would change colour, so it would ship unverified. That is the 0087 mistake inverted, and #107 records the rule: *a fail-closed migration must land WITH the identity plumbing of every store it covers, never before it.*

---

## Live drift — read before deploying

The live `aura` database is at **migration 87**; the tree is at **89**. Something migrated it mid-session. `CheckMigrationHead` means the daemon REFUSES to boot on a mismatch — a safe failure, not a silent one — so the next deploy must migrate first.

Also on the cluster: ~10 leftover `aura_cli_reset_<nanos>` databases from `cmd/aura`'s reset tests, plus `aura_migrate*_drill` databases from older sessions. Test hygiene, harmless, worth a sweep.

---

## Standing rules that earned themselves this session

- **Measure or say you did not.** Every retrieval claim here has a number and a corpus.
- **Disposable databases only.** The tier refuses `aura` because of a real data-loss incident. Create, prove, drop.
- **Migration numbers from `ls internal/db/migrations/ | tail -1` at write time.** Two agents collided on 0088 in one night; the second renumbered.
- **A tier that runs nowhere is not a gate.** Two were found this session — `arcadedb_integration` and `reasoning_live` — both wired, both with skip-helpers that now `t.Fatal` under `$CI`.
- **Fix the store, never the policy.** When a fail-closed policy blocks code, the code is what is wrong.
- **`.env` is CRLF on this host.** `tr -d '\r'` into a temp file before sourcing, or the DSN will not parse.
- **ArcadeDB docs:** `docs.arcadedb.com` returns near-empty pages to a fetch. Read the asciidoc source: `gh api repos/ArcadeData/arcadedb-docs/contents/src/main/asciidoc/<path>.adoc --jq .content | base64 -d`.
