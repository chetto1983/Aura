# Phase-WIKI-FIX — Substrate Bug Sweep

**Status:** 🔴 priority-0 blocker for Phase-WIKI-B Wave A and every downstream phase
**Provenance:** Direct retrieval substrate bench 2026-05-22 (`project_2026-05-22_substrate_bench_diagnosis`)
**Estimated effort:** ~1-2 sessions Ralph atomic, 8 stories
**LOC delta:** ~+500 / -50 = +450

---

## Why this phase

The 2026-05-22 substrate bench (direct `/api/wiki/search` from inside docker network on 20 fixture queries) surfaced TEN distinct substrate bugs. Latency is fine (17ms p50 inside docker), Recall@5 is ~90-100%, but the rank fusion silently runs on 1/3 of its channels because the SQLite FTS5 mirror has 17 rows vs 150+ on disk. Plus a chain of dim-change friction (8 manual steps to bump 256→768) that proves the operator surface for embedding changes is broken.

These bugs are NOT in the agent loop. They are NOT graph-level. They are NOT prompt-level. They are **all in `internal/storage/search` + the index sync paths**. Until they ship, Phase-WIKI-B Wave A (hybrid fusion + heading subnodes + wiki_subgraph) cannot be meaningfully measured — RRF over a missing FTS5 channel just gives RRF over 1 channel.

**Discovery sequence (live):**
- p50 17ms in container, 2130ms from Windows host = Hyper-V port forwarding lie ([[feedback-hyperv-port-forwarding-lie]])
- All 20 queries return score 0.0131 ≈ 0.8/61 = vector channel rank-0 RRF contribution; exact and FTS = 0 across the board
- `SELECT count(*) FROM wiki_documents` = 17, mostly legacy `graph:node:*` entries
- `aura-operating-memory` (system page) ranks top-1 for `wiki-dante q2`
- `davide` user-profile appears ×2 in autori-json q2 top-2
- Embed cache key `(sha, model)` did not include output_dim → cache poisoned after 256→768 swap

---

## Stories

### US-WIKI-FIX-01 — FTS5 mirror auto-sync on wiki write/delete

- **Scope:** When [internal/wiki/store_writes.go](internal/wiki/store_writes.go) writes/deletes a page, the SQLite `wiki_documents` FTS5 mirror must update synchronously. Today only 17 stale entries (mostly legacy `graph:node:*` prefix) exist while Qdrant has 290 points and disk has 150+ pages. The hybrid fusion `mergeHybridResults` already exists and is wired into `/api/wiki/search`, but the FTS channel returns zero because the mirror is empty.
- **Files:** MODIFY [internal/storage/search/sqlite.go](internal/storage/search/sqlite.go) (or the FTS5 owner — grep `wiki_documents` writes); MODIFY [internal/wiki/store_writes.go](internal/wiki/store_writes.go) (call FTS5 upsert/delete next to Qdrant submitter); audit [internal/reindex/](internal/reindex/) reconciler.
- **LOC delta:** +120 / -20 = +100.
- **Acceptance:**
  - After `WritePage(slug)`, `SELECT id FROM wiki_documents WHERE id=?` returns the new slug.
  - After `DeletePage(slug)`, the row is gone.
  - Boot-time reconciler: `wiki_documents` count == on-disk page count (modulo system pages — see US-WIKI-FIX-05).
  - Direct query: `MATCH 'galileo'` returns `galileo-galilei` and other pages that mention galileo in title or body.
  - Bench re-run shows `score_fts > 0` on at least 15/20 queries (today: 0/20).
- **Verification probe (copy-pasteable):**
  ```bash
  docker exec aura-aura-1 sqlite3 /data/aura.db "SELECT count(*) FROM wiki_documents"  # ≥ on-disk page count
  docker exec aura-aura-1 sqlite3 /data/aura.db "SELECT id FROM wiki_documents WHERE wiki_documents MATCH 'galileo' LIMIT 3"
  ```
- **Single atomic commit:** `fix(wiki): auto-sync FTS5 mirror on page write/delete (US-WIKI-FIX-01)`
- **Priority:** P0 — the single most impactful fix; unblocks Phase-WIKI-B Wave A measurement.

### US-WIKI-FIX-02 — Embed cache key includes output_dim

- **Scope:** Today [internal/storage/search/embed_cache.go](internal/storage/search/embed_cache.go) keys are `(content_sha, model)`. When `EMBEDDING_OUTPUT_DIM` changes (e.g. 256 → 768) the cache returns OLD vectors, which fail at the rebuild dim-consistency check or worse poison Qdrant with mixed dims. Witnessed live during the 2026-05-22 bench. Fix: extend the key to `(content_sha, model, output_dim)` via SQLite migration.
- **Files:** NEW migration in [internal/db/migrations/](internal/db/migrations/) (add `output_dim` column + composite PK); MODIFY [internal/storage/search/embed_cache.go](internal/storage/search/embed_cache.go) (lookup + insert paths).
- **LOC delta:** +80 / -10 = +70 (mostly migration + schema change).
- **Acceptance:**
  - Migration adds `output_dim INTEGER NOT NULL DEFAULT 0` to `embedding_cache` + drops old PK + creates `(content_sha, model, output_dim)` PK.
  - `SELECT count(*) FROM embedding_cache WHERE output_dim = 0` populated correctly post-migration (legacy rows tagged with current cfg dim or with sentinel 0).
  - Test: insert vector at dim=256, then at dim=768 same sha+model → 2 distinct rows.
  - Live: bump EMBEDDING_OUTPUT_DIM, restart aura → cache miss, re-embed at new dim, Qdrant rebuild succeeds without dim mismatch.
- **Single atomic commit:** `fix(embed): include output_dim in cache key (US-WIKI-FIX-02)`

### US-WIKI-FIX-03 — Wiki rebuild skip-and-continue on per-page embed error

- **Scope:** [internal/storage/search/qdrant.go](internal/storage/search/qdrant.go) `rebuildQdrantWikiDocumentsWithClient` (line ~414) bails on the FIRST embed error. Witnessed: one wiki page exceeds embed sidecar context size (1247 tokens vs old 1024 limit) → entire wiki collection left empty. Should log + skip + continue + report skipped count.
- **Files:** MODIFY [internal/storage/search/qdrant.go](internal/storage/search/qdrant.go) lines 478-510.
- **LOC delta:** +30.
- **Acceptance:**
  - Single embed failure does NOT abort the rebuild.
  - `QdrantRebuildReport` grows a `SkippedDocs []SkippedDoc{DocID, Reason}` field.
  - Logged via `logger.Warn("rebuild: skipping doc", "id", id, "error", err)` per skip.
  - Test with mock embed func that returns error for one specific doc — verify others are indexed.
- **Single atomic commit:** `fix(qdrant): skip-and-continue on per-page embed error during rebuild (US-WIKI-FIX-03)`

### US-WIKI-FIX-04 — Admin `/api/wiki/reindex` endpoint + dim-mismatch auto-detect

- **Scope:** Two infra gaps in one story:
  - (a) No HTTP endpoint to force a Qdrant rebuild. Today the only path is `cmd/debug_qdrant`. Add `POST /api/wiki/reindex` (admin-token gated) that calls `RebuildQdrantWikiDocuments`.
  - (b) At boot, probe Qdrant collection's vector size; if mismatch with `cfg.EmbeddingOutputDim` (or model's native dim when cfg=0), DROP + recreate collection BEFORE indexing. Today the warm-cache short-circuit (`info.PointsCount > 0` ⇒ skip rebuild) hides dim drift.
- **Files:** MODIFY [internal/api/wiki.go](internal/api/wiki.go) (new `handleWikiReindex` + router entry); MODIFY [internal/storage/search/qdrant.go](internal/storage/search/qdrant.go) (extend `CollectionInfo` parsing to capture vector_size if Qdrant exposes it, or add a sentinel probe vector); MODIFY [internal/api/router.go](internal/api/router.go).
- **LOC delta:** +120.
- **Acceptance:**
  - `POST /api/wiki/reindex` returns `{collection, points_indexed, pages_indexed, skipped, elapsed_ms, vector_size}` JSON.
  - Boot-time mismatch: change `EMBEDDING_OUTPUT_DIM` setting → restart → collection auto-recreated with new dim, no operator action.
  - Probe via Qdrant: `GET /collections/<name>` returns `params.vectors.size == cfg.EmbeddingOutputDim` (or native dim if 0).
- **Single atomic commit:** `feat(wiki): admin reindex endpoint + dim-mismatch auto-detect (US-WIKI-FIX-04)`

### US-WIKI-FIX-05 — Filter system/internal pages out of `/api/wiki/search`

- **Scope:** Pages with frontmatter `category=system` (e.g. `aura-operating-memory`, future `wiki-index`) leak into user search results. Witnessed: `aura-operating-memory` ranked top-1 for `wiki-dante q2`. Filter them at the search layer; expose via `?include_system=1` for the dashboard / debug only.
- **Files:** MODIFY [internal/api/wiki.go](internal/api/wiki.go) (handleWikiSearch query handling); MODIFY [internal/storage/search/](internal/storage/search/) backends (add `IncludeSystemPages bool` option).
- **LOC delta:** +50.
- **Acceptance:**
  - Default `/api/wiki/search?q=dante` does NOT return `aura-operating-memory` even if FTS5 matches.
  - `/api/wiki/search?q=dante&include_system=1` includes it.
  - Existing pages tagged with `category=system` (or `tags` containing "system") in their frontmatter are filtered; pages without that flag pass through.
- **Single atomic commit:** `feat(wiki): filter system-category pages from default search (US-WIKI-FIX-05)`

### US-WIKI-FIX-06 — Dedupe Qdrant index by slug

- **Scope:** Witnessed: `autori-json q2` top-2 hits are both `davide`. Same slug appears as TWO points in Qdrant. Investigation needed: is it the wiki page + a `graph:node:davide` duplicate? Or a stale point that wasn't deleted on rebuild? Fix: ensure single point per slug; merge graph-node + page-content into one payload OR namespace them so dedup is explicit at query time.
- **Files:** AUDIT [internal/storage/search/qdrant.go](internal/storage/search/qdrant.go) upsert logic + `loadWikiDocuments`; MODIFY whichever path emits both prefixes.
- **LOC delta:** +40 / -20 = +20.
- **Acceptance:**
  - `GET /collections/aura_memory_v1/points/count?count_filter={match: {slug: "davide"}}` returns ≤1 per slug.
  - Bench re-run: no duplicate slug in any top-5 result set.
- **Single atomic commit:** `fix(qdrant): dedupe wiki index by slug (US-WIKI-FIX-06)`

### US-WIKI-FIX-07 — Settings INSERT auto-default `updated_at`

- **Scope:** Today `INSERT INTO settings(key, value) VALUES(...)` fails with `NOT NULL constraint failed: settings.updated_at`. Add DB default `DEFAULT CURRENT_TIMESTAMP` to the column so manual + dashboard + migration writes can omit it. Witnessed during 2026-05-22 dim bump.
- **Files:** NEW migration in [internal/db/migrations/](internal/db/migrations/).
- **LOC delta:** +30 (migration + test).
- **Acceptance:**
  - `INSERT INTO settings(key, value) VALUES('TEST_KEY', 'X')` succeeds without `updated_at`.
  - Existing rows preserved.
  - Migration test green.
- **Single atomic commit:** `fix(db): settings.updated_at default to CURRENT_TIMESTAMP (US-WIKI-FIX-07)`

### US-WIKI-FIX-08 — Embed sidecar n_ctx default sane + smoke

- **Scope:** Embed sidecar `--ctx-size 2048` declared but effective `n_ctx` was 1024 (server divides by `--parallel 2`). Bumped to 4096/2=2048 effective during 2026-05-22 bench. Document this in [compose.yaml](compose.yaml) and add a boot smoke probe that fails fast if `/props` returns n_ctx < 2048.
- **Files:** MODIFY [compose.yaml](compose.yaml) (comment + verify); ADD smoke probe to `aura-init-models` or boot health check.
- **LOC delta:** +20 (mostly comments + probe).
- **Acceptance:**
  - `compose.yaml` comment explains the per-slot math (`--ctx-size / --parallel = per-slot n_ctx`).
  - Boot probe: `curl http://aura-llama-embed:8080/props` → assert `default_generation_settings.n_ctx >= 2048`; if not, log error and exit non-zero from aura-init-models.
  - Long wiki page (1247 tokens) embeds without 400.
- **Single atomic commit:** `chore(embed): document n_ctx per-slot math + boot smoke probe (US-WIKI-FIX-08)`

---

## Sequencing

| # | Story | Depends on | Why this order |
|---|---|---|---|
| 1 | US-WIKI-FIX-01 (FTS5 sync) | nothing | THE single most impactful fix; unblocks measurement of everything else |
| 2 | US-WIKI-FIX-07 (settings default) | nothing | Trivial; needed for FIX-04 operator ergonomics |
| 3 | US-WIKI-FIX-03 (skip-continue) | nothing | Trivial; needed for FIX-04 to be robust |
| 4 | US-WIKI-FIX-08 (n_ctx) | nothing | Document + smoke; no behavior change beyond the bump already done |
| 5 | US-WIKI-FIX-02 (cache key dim) | nothing | Standalone migration; independent of FTS5 |
| 6 | US-WIKI-FIX-05 (system page filter) | FIX-01 (FTS5 sync) | Filter relies on FTS5 having the rows to filter |
| 7 | US-WIKI-FIX-06 (dedupe) | FIX-04 (admin reindex helpful for cleanup) | Can ship parallel but reindex API makes the cleanup atomic |
| 8 | US-WIKI-FIX-04 (admin reindex + auto-detect) | FIX-02, FIX-03 (skip-continue and cache-key needed for safe reindex) | Largest story; combines two infra gaps |

**One story = one commit per [`feedback_one_module_per_slice`].**

**Ralph mode preferred:** Each story is self-contained, has explicit file paths, has copy-pasteable verification probes. Queue staged in `scripts/ralph/prd-phase-wiki-fix-staged.json`.

---

## Risks

- **R1 (FIX-01):** FTS5 sync hooks might be called from multiple write paths (WritePage, BulkUpsert, migration). Mitigation: grep `wiki_documents` writes and centralize all into a single helper before adding the hook.
- **R2 (FIX-02):** SQLite migration on `embedding_cache` (4742 rows pre-bench, nuked since) — schema change must preserve any rows + reset PK. Mitigation: legacy rows get `output_dim = 0` and a corresponding cache-miss until they re-embed.
- **R3 (FIX-04):** Auto-recreate on dim mismatch is destructive — drops the collection. Mitigation: log loudly, refuse if `--no-rebuild-on-mismatch` env set, expose dry-run via `?dry_run=1`.
- **R4 (FIX-05):** Filter relies on frontmatter category. If `aura-operating-memory` lacks `category=system`, the filter doesn't fire. Mitigation: audit all known system pages, add the field as part of this story.
- **R5 (FIX-06):** Investigation may surface a deeper bug (e.g. graph-node + page-content emit duplicate Qdrant points by design). If so, the dedup story expands. Mitigation: time-box investigation to 30 min; if scope balloons, split into FIX-06a (audit) + FIX-06b (fix).

---

## Verification — phase exit criteria

After all 8 stories ship, the 2026-05-22 bench must show:

1. `score_fts > 0` on ≥15/20 queries (today: 0/20).
2. Top-1 hit rate (qualitative) ≥ 16/20 (today: 8-10/20).
3. No duplicate slug in any top-5 result.
4. No `category=system` page in default search top-5.
5. `EMBEDDING_OUTPUT_DIM` swap (e.g. 768 → 256 via DB UPDATE → restart) reindexes without manual collection drop or cache nuke.
6. `POST /api/wiki/reindex` returns `points_indexed >= pages_on_disk - skipped` reliably.
7. Latency p95 stays ≤ 100ms (currently 55ms @ 256d, 85ms @ 768d).

Bench rerun command (per memory `project_2026-05-22_substrate_bench_diagnosis`):

```bash
TOKEN=$(cat .planning/qa/token.txt)
docker cp docs/quality-bench/queries.json aura-aura-1:/tmp/q.json
docker exec -e TOKEN="$TOKEN" -i aura-aura-1 python3 -c "..."  # script in memory
```

---

*Updated 2026-05-22. Per CLAUDE.md DEEP REFACTOR ON TOUCH: every story commit must include golangci-lint clean + dupl clean + LOC ≤600 + dead code removed on touched files.*
