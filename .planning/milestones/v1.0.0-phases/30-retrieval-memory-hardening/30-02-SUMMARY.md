---
phase: 30-retrieval-memory-hardening
plan: 02
subsystem: documents
tags: [ingest, markitdown, neo4j, chunking, next-chunk, pptx, html, csv, locator, allowlist, fail-soft, sidecar]

# Dependency graph
requires:
  - phase: 11-memory-ingestion (internal/documents)
    provides: the Service/Extractor/Indexer pipeline, ExtractClient sidecar contract, Chunk/Locator model, and the chunkUpsertQuery :HAS_CHUNK MERGE that this wave widens and extends
  - phase: 30-retrieval-memory-hardening (30-01 rerank foundation)
    provides: the fail-soft sidecar + NO-SKIP-AS-GREEN live-tier patterns mirrored here (this wave does not call RerankClient directly)
provides:
  - "internal/documents/extensions.go: single source-of-truth supportedDocumentExt allowlist + case-insensitive isSupportedDocument over filepath.Ext (pdf/docx/pptx/xlsx/xlsm/html/htm/csv/md/markdown/txt/json/xml/epub/png/jpg/jpeg/gif/webp)"
  - "indexer.go NEXT_CHUNK: idempotent (:Chunk)-[:NEXT_CHUNK]->(:Chunk) reading-order edges, chunk_count-1 per document, MATCH-then-MERGE on $-params (no fmt.Sprintf), written after every chunk node exists"
  - "markitdown /extract format-aware handlers _extract_pptx (slide+title locator), _extract_html (section/heading locator), _extract_csv (row-range locator) plus the retained _extract_markdown generic fallback for the long tail"
  - "Locator.Slide (json slide,omitempty) round-tripping the presentation locator with no schema migration"
  - "document_ingest_live tier: NO-SKIP-AS-GREEN (t.Fatal under $CI), pptx/html table cases, and a live :NEXT_CHUNK count == chunk_count-1 assertion"
affects: [30-03, 30-04, 30-05, gsd-verify-work, gsd-secure-phase]

# Tech tracking
tech-stack:
  added:
    - "python-pptx==1.0.2 pinned in docker/markitdown/Dockerfile (already transitive via markitdown[all], pinned explicitly for supply-chain reproducibility T-30-SC). No new Go module — slices stdlib only."
  patterns:
    - "Single source-of-truth extension allowlist (map[string]struct{}) gating ingest; the sidecar generic-markdown fallback covers anything readable beyond the allowlist"
    - "Connected-graph upsert: MATCH-then-MERGE (not inline-node MERGE) so re-ingest never duplicates nodes or edges; ordering derived in Go (slices.SortFunc by ChunkIndex) and passed as bound $pairs"
    - "Format-aware sidecar handler mirrors the pdf/xlsx/docx style (dispatch-before-generic, reuse _chunk_text/_payload, try/except -> HTTP 422), each emitting a format-appropriate locator"

key-files:
  created:
    - internal/documents/extensions.go
    - internal/documents/extensions_test.go
  modified:
    - internal/documents/service.go
    - internal/documents/service_test.go
    - internal/documents/indexer.go
    - internal/documents/indexer_test.go
    - internal/documents/types.go
    - internal/documents/document_ingest_live_test.go
    - docker/markitdown/app.py
    - docker/markitdown/Dockerfile
    - docs/document-ingestion.md

key-decisions:
  - "NEXT_CHUNK uses MATCH (a) MATCH (b) MERGE (a)-[:NEXT_CHUNK]->(b), NOT the plan's literal inline-node MERGE (a:Chunk{id})-[:NEXT_CHUNK]->(b:Chunk{id}). Inline-node MERGE of a whole path creates duplicate bare nodes when the relationship is absent (classic Neo4j footgun); MATCH-then-MERGE binds the existing unique-id nodes and merges only the edge, which is the idempotent, no-duplicate form the plan's own 'idempotent / no duplicate edges' criterion requires and mirrors the existing chunkUpsertQuery's MATCH (d:Document) pattern."
  - "HTML extraction reuses the in-tree markitdown converter (_md.convert -> markdown) then splits on ATX headings, so NO beautifulsoup4 dependency is added; CSV uses the stdlib csv module; only python-pptx is a new (pinned) dep. Smaller new supply-chain surface (T-30-SC)."
  - "Locator.Slide inserted between Sheet and RowStart; omitempty means existing page/sheet/section chunks marshal byte-identically (ChunkHash unchanged, no hash drift) — verified TestIndexerStoresLocatorAsJSON still asserts {\"page\":12}."
  - "Task 1 (tdd=true) RED->GREEN collapsed into one atomic feat commit: RED was demonstrated live (TestIsSupportedDocument + the NEXT_CHUNK test failed before extensions.go/the indexer write existed) but the lefthook pre-commit go-vet gate forbids a compile-failing commit without --no-verify (forbidden), so the GREEN commit is atomic — same convention as 30-01."

patterns-established:
  - "Extension allowlist as the single ingest gate; format handlers + generic fallback behind it"
  - "Idempotent reading-order graph edges via MATCH-then-MERGE on bound $pairs ordered in Go"

requirements-completed: [RET-03]

coverage:
  - id: D1
    description: "isSupportedDocument is the single source of truth over a widened allowlist (19 extensions, case-insensitive, empty/unknown rejected); service.go calls it (no inline switch)"
    requirement: "RET-03"
    verification:
      - kind: unit
        ref: "internal/documents/extensions_test.go#TestIsSupportedDocument (pptx/html/htm/csv/md/markdown/txt/json/xml/epub/images true; exe/zip/bin/noext/empty false; case-insensitive) — go test -race, pass"
        status: pass
      - kind: other
        ref: "grep: service.go has no `case \".pdf\"` switch; allowlist literal lives only in extensions.go"
        status: pass
    human_judgment: false
  - id: D2
    description: "Indexer writes chunk_count-1 sequential :NEXT_CHUNK edges in chunk_index order after all chunk nodes exist, idempotently (MERGE, not CREATE), and 0 edges for a 1-chunk document, with $-param-only Cypher"
    requirement: "RET-03"
    verification:
      - kind: unit
        ref: "internal/documents/indexer_test.go#TestIndexerWritesSequentialNextChunkEdges (4 pairs 0->1->2->3->4, ordering after last HAS_CHUNK, MERGE-not-CREATE) + #TestIndexerSingleChunkWritesNoNextChunkEdges — go test -race, pass"
        status: pass
      - kind: other
        ref: "grep: no fmt.Sprintf building Cypher in indexer.go; nextChunkUpsertQuery uses $pairs"
        status: pass
    human_judgment: false
  - id: D3
    description: "markitdown /extract emits format-aware chunks for PPTX (slide+title locator), HTML (section/heading locator), CSV (row-range locator), retains the generic-markdown fallback, and the slide locator round-trips through Locator.Slide"
    requirement: "RET-03"
    verification:
      - kind: integration
        ref: "in-container handler probe against the rebuilt aura-markitdown (real markitdown/python-pptx/csv libs): HTML 2 sections {section:Introduction/Details}, CSV {row_start:1,row_end:3}, PPTX {slide:1,section:Agenda}+{slide:2,section:Details} mime presentationml.presentation, .txt generic fallback 1 chunk {} — CONTAINER_PROBE_OK"
        status: pass
      - kind: other
        ref: "python3 -c ast.parse(docker/markitdown/app.py) APP_PY_OK; go test -race ./internal/documents/ (types.go Locator.Slide) pass; app.py 379 LOC <= 600"
        status: pass
    human_judgment: false
  - id: D4
    description: "document_ingest_live tier compiles, enforces NO-SKIP-AS-GREEN (os.Getenv(CI)->t.Fatal when no AURA_DOC_TEST_* set), adds pptx/html cases, and asserts chunks>=1 + live :NEXT_CHUNK count == chunk_count-1"
    requirement: "RET-03"
    verification:
      - kind: integration
        ref: "go vet -tags document_ingest_live ./internal/documents/ (compiles); grep: os.Getenv(\"CI\") t.Fatal branch present; AURA_DOC_TEST_PPTX/HTML cases present"
        status: pass
    human_judgment: false
  - id: D5
    description: "Full Postgres+Neo4j live E2E: ingest the G220 PDF AND a non-PDF (PPTX/HTML) to searchable with chunk_count>=1 and the live :NEXT_CHUNK count == chunk_count-1"
    requirement: "RET-03"
    verification:
      - kind: e2e
        ref: "AURA_DOC_TEST_PDF=<G220> AURA_DOC_TEST_HTML=<file> go test -tags document_ingest_live -run TestLiveDocumentIngestE2E ./internal/documents -v (host with full env)"
        status: unknown
    human_judgment: true
    rationale: "The Go live tier opens Postgres AND Neo4j; the Neo4j password lives only in the walled-off .env (not in the aura-neo4j container env), and `go test` sets cwd to the package dir so godotenv cannot auto-load the repo-root .env. The walled secret was not circumvented. The sidecar half of the E2E (the genuinely-new PPTX/HTML/CSV handlers + generic fallback) WAS live-verified end-to-end inside the rebuilt container; the indexer NEXT_CHUNK half is unit-proven (race). Run on a host with the composed AURA_DB_URL/AURA_DB_MIGRATE_URL + NEO4J_PASSWORD exported per docs/document-ingestion.md to close this."

# Metrics
duration: 24min
completed: 2026-06-28
status: complete
---

# Phase 30 Plan 02: Widened ingest + connected-graph NEXT_CHUNK edges Summary

**Every markitdown-readable format now ingests behind a single extension allowlist with format-aware locators (PPTX slide, HTML/section, CSV row-range) and a generic-markdown fallback, and the indexer writes idempotent chunk_count-1 (:Chunk)-[:NEXT_CHUNK]->(:Chunk) reading-order edges — the connected graph Wave 3 two-stage retrieval expands over (RET-03).**

## Performance

- **Duration:** 24 min
- **Started:** 2026-06-28T04:41:52Z
- **Completed:** 2026-06-28T05:06:09Z
- **Tasks:** 2 (Task 1 tdd=true)
- **Files modified:** 11 (2 created, 9 modified)
- **Gates:** `go vet ./...` + `go build ./...` clean (native lefthook pre-commit, both commits); `go test -race ./internal/documents/` green (91.7% package coverage); `go vet -tags document_ingest_live ./internal/documents/` compiles the live tier; `python3 ast.parse(app.py)` APP_PY_OK; lefthook pre-commit (gofmt + vet + file-size<=600) green on both commits; app.py 379 LOC, indexer.go 255 LOC, extensions.go 44 LOC — all <= 600.

## Accomplishments
- **Single allowlist + NEXT_CHUNK (Task 1, tdd):** Lifted the 4-format inline switch out of `service.go` into `extensions.go`'s `supportedDocumentExt` (19 extensions, case-insensitive over `filepath.Ext`); `service.go` now calls the shared `isSupportedDocument`. Added idempotent `:NEXT_CHUNK` edges: after all chunk batches upsert, `nextChunkPairs` derives chunk_count-1 `{prev,next}` pairs ordered by `ChunkIndex` (`slices.SortFunc`) and `nextChunkUpsertQuery` MATCH-then-MERGEs them on bound `$pairs` (mojibake-safe, no `fmt.Sprintf`).
- **Widened sidecar handlers + slide locator + live tier (Task 2):** `_extract_pptx` (python-pptx, one chunk/slide, `{slide,section}` locator + title heading_path), `_extract_html` (markitdown HTML->md split on ATX headings, `{section}` locator, heading text retained in body for retrieval), `_extract_csv` (stdlib csv, row-range batching mirroring `_extract_xlsx`); `_extract_markdown` kept as the final fallback. `Locator.Slide` added (omitempty, no hash drift). The live tier became NO-SKIP-AS-GREEN with pptx/html cases and a live `:NEXT_CHUNK` count invariant.
- **Live sidecar verification:** Rebuilt `aura-markitdown` incrementally (cache-preserving; python-pptx==1.0.2 installed in 33s) and exercised all three new handlers + the generic fallback end-to-end against the real libs inside the running container — correct slide/section/row-range locators and the `.txt` long-tail fallback.

## Task Commits

Each task was committed atomically:

1. **Task 1: single extension allowlist + :NEXT_CHUNK reading-order edges** — `0da097c3` (feat) — TDD RED demonstrated live (TestIsSupportedDocument + the NEXT_CHUNK test failed before the code existed), collapsed to one atomic feat commit (hook forbids a compile-failing RED commit; see Decisions)
2. **Task 2: widen markitdown /extract (pptx/html/csv) + slide locator + live tier** — `8d1ce78a` (feat) — app.py handlers + Dockerfile pin + types.go + live tier + docs

**Plan metadata:** this SUMMARY + STATE/ROADMAP (docs commit).

_Note: Task 1 is tdd=true; RED->GREEN collapsed into one feat commit due to the pre-commit `go vet ./...` gate (a compile-failing RED commit cannot pass it without --no-verify)._

## Files Created/Modified
- `internal/documents/extensions.go` — `supportedDocumentExt` allowlist (19 ext) + case-insensitive `isSupportedDocument` (44 LOC)
- `internal/documents/extensions_test.go` — table-driven allowlist coverage (supported/rejected/case-insensitive/empty)
- `internal/documents/service.go` — deleted the inline 4-format switch; calls the shared helper
- `internal/documents/service_test.go` — unsupported-path fixture `notes.txt` -> `notes.exe` (.txt is now allowlisted by design)
- `internal/documents/indexer.go` — `nextChunkPairs` + `nextChunkUpsertQuery` + the post-batch NEXT_CHUNK write (`slices` import); 255 LOC
- `internal/documents/indexer_test.go` — NEXT_CHUNK ordering/idempotency/1-chunk tests + helpers; updated the searchable-after-chunks write count 4->5
- `internal/documents/types.go` — `Locator.Slide int json:"slide,omitempty"`
- `internal/documents/document_ingest_live_test.go` — NO-SKIP-AS-GREEN gate over `docTestEnvs`, pptx/html cases, chunks>=1 + live NEXT_CHUNK count assertion
- `docker/markitdown/app.py` — `_extract_pptx`/`_slide_title`/`_extract_html`/`_split_markdown_sections`/`_extract_csv` + dispatch wiring; 379 LOC
- `docker/markitdown/Dockerfile` — pinned `python-pptx==1.0.2` (supply-chain note)
- `docs/document-ingestion.md` — full allowlist + generic-fallback note, `:NEXT_CHUNK` edge in the stores list, pptx/html in the live-E2E recipe + rebuild note

## Decisions Made
See `key-decisions` frontmatter. Load-bearing: (1) MATCH-then-MERGE for NEXT_CHUNK instead of the plan's literal inline-node MERGE, to guarantee the idempotent/no-duplicate-node behavior the plan itself requires; (2) HTML via the in-tree markitdown converter (no bs4 dep) and CSV via stdlib, so python-pptx is the only added (pinned) dep; (3) `Locator.Slide` omitempty keeps existing chunk hashes stable; (4) Task-1 RED->GREEN collapsed into one atomic feat commit (lefthook go-vet pre-commit).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `service_test.go` unsupported-path fixture used `.txt`, now allowlisted**
- **Found during:** Task 1 (GREEN run)
- **Issue:** `TestServiceRejectsUnsupportedAndDirectoryPaths` wrote `notes.txt` and asserted an "unsupported" error; the widened allowlist makes `.txt` supported by design, so the test failed.
- **Fix:** changed the fixture to `notes.exe` (genuinely unsupported), preserving the test's intent (reject unsupported types). Added a one-line why-comment.
- **Files modified:** internal/documents/service_test.go
- **Verification:** `go test -race ./internal/documents/` green; rejection path still exercised with `.exe`.
- **Committed in:** `0da097c3` (Task 1)

**2. [Rule 1 - Bug] `indexer_test.go` write-count assertion was stale after the NEXT_CHUNK write**
- **Found during:** Task 1 (GREEN run)
- **Issue:** `TestIndexerSetsDocumentSearchableAfterChunkWrites` asserted exactly 4 write calls for a 3-chunk doc; the new NEXT_CHUNK write (between chunk batches and the searchable mark) makes it 5.
- **Fix:** updated the count 4->5 with a clarifying comment; the test's real intent — the searchable mark is the LAST write — is unchanged and still asserted.
- **Files modified:** internal/documents/indexer_test.go
- **Verification:** `go test -race ./internal/documents/` green.
- **Committed in:** `0da097c3` (Task 1)

---

**Total deviations:** 2 auto-fixed (both Rule 1 — stale-test maintenance forced by the plan's own intended behavior change). No scope creep — both updates preserve each test's original intent and follow CLAUDE.md "rewrite the test with explicit justification".

## Issues Encountered
- **Full Postgres+Neo4j Go E2E could not be run by the agent:** `go test` sets cwd to the package dir so `godotenv.Load()` cannot find the repo-root `.env`, and the Neo4j password is NOT in the aura-neo4j container env (only in the walled-off `.env`, which is permission-blocked for the agent's tools). The `.env` boundary was respected (not circumvented). Mitigation: the genuinely-new sidecar handlers were live-verified end-to-end inside the rebuilt container (D3), the live tier compiles + enforces no-skip-as-green (D4), and the NEXT_CHUNK indexer logic is unit-proven (D2). The full DB E2E is the documented host step (D5) — run with the composed `AURA_DB_URL`/`AURA_DB_MIGRATE_URL` + `NEO4J_PASSWORD` exported per docs.
- **MSYS path mangling** rewrote `/app` for `docker exec -w`; resolved by relying on the image's `WORKDIR /app` with a relative script name (no slashes to mangle).

## User Setup Required
None for boot. The running `aura-markitdown` sidecar was rebuilt to this revision (`docker compose up -d --build markitdown`) so the live stack already matches the committed code; the change is backward-compatible (pdf/xlsx/docx dispatch unchanged). To re-run the full live E2E: bring the stack up and export `AURA_DOC_TEST_PDF` + `AURA_DOC_TEST_PPTX`/`AURA_DOC_TEST_HTML` plus the DB/Neo4j env (CLAUDE.md §"Quality tooling & gates"), then `go test -tags document_ingest_live ./internal/documents -v`.

## Known Stubs
None. `extensions.go`, the NEXT_CHUNK indexer path, and the three new sidecar handlers are complete implementations. The generic-markdown fallback is the INTENDED degraded path for the long-tail formats (>= 1 chunk, empty-but-valid locator), not a stub — it is the documented RET-03 contract and is live-proven for `.txt`.

## Threat Flags
None. The change stays within the plan's `<threat_model>`: all Neo4j writes remain bound-parameter (T-30-05), `DefaultMaxIngestBytes`/`_max_chunk_chars` ceilings are preserved (T-30-04), the sidecar `/extract` try/except -> HTTP 422 pattern is retained (T-30-06), and the only added dep (python-pptx) is pinned and already transitive via markitdown[all] (T-30-SC). No new network endpoint, auth path, or trust boundary introduced.

## Next Phase Readiness
- The connected (:NEXT_CHUNK) graph + the full-format ingest surface are ready for Wave 3 (30-03 two-stage retrieval): vector/BM25 seed -> graph-expand around matched seeds along the reading-order chain, then rerank (30-01).
- One human/host follow-up (D5): run the full Postgres+Neo4j `document_ingest_live` tier with the live env to confirm the G220 PDF + a non-PDF format reach `searchable` and the live `:NEXT_CHUNK` count == chunk_count-1.
- No open blockers.

## Self-Check: PASSED

---
*Phase: 30-retrieval-memory-hardening*
*Completed: 2026-06-28*
