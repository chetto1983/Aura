# document_open — the index returns a handle, not a passage

**Date:** 2026-07-31
**Status:** spec + Part 1 implemented
**Supersedes nothing.** Extends the document pipeline landed in
`2026-07-22-document-search-consolidation.md`.

## The measurement this comes from

A day of retrieval benchmarking on a real 5889-row customer spreadsheet
(`Clienti.xlsx`) and a 828-chunk technical manual produced one number that no
amount of tuning moves:

| question shape | best retrieval configuration | recall |
|---|---|---|
| "who serves 173 DIVISIONE ELETTRICA SRL" | BM25 | 100% |
| "who serves 173 DIVISIONE ELETTRICA SRL" | dense (Gemma-300m, 768d) | 48% |
| **"how many customers in TORINO"** | **any, at any k** | **0%** |

The third row is not a tuning target. The answer is not in any passage — it is a
property of the whole set — so no retriever returns it at any recall, ever. The
same holds for "which sales rep has the most customers", "how many distinct
towns", and every other aggregate a person actually asks a spreadsheet.

Two further facts from the same day:

- Chunking a spreadsheet into text costs information at every step. One chunk is
  fifty unrelated customers averaged into a single vector. The extractor's
  markdown for `Clienti.xlsx` is 624 082 characters over 118 chunks; the file is
  331 239 bytes.
- The original bytes survive the whole pipeline intact. `TestAssetRoundTripKeeps
  TheOriginal` (tag `garage_integration`) walks presign → upload → finalize →
  process → reopen and asserts `bytes.Equal`. Garage holds the real file.

And one fact about the runtime, verified in the live container:

- **LibreOffice 7.4.7.2 is already installed in the `aura` image**
  (`docker/aura/Dockerfile:114`, headless `-writer`/`-calc`/`-impress`), along
  with python + openpyxl + pandas + PyMuPDF. It converted `Clienti.xlsx` (5889
  rows), `formule.xlsx` and a `.ods` in **~1 second each** — including `.ods`,
  which markitdown refuses outright ("No converter attempted a conversion").
- `/workspace` is the durable working root (`AURA_WORKSPACE_DIR=/workspace`), and
  `shell_exec` runs in that same container.

So the agent already has an office suite, a spreadsheet library, and a place to
work. What it does not have is **the file**.

## The change

`document_search` today answers "what does the document say" by returning text.
For tabular content that is the wrong question. The right one is "which file",
and then the agent opens it.

    before   question → document_search → chunks of text → answer from text
    after    question → document_search → which document → document_open →
             the real file in /workspace → LibreOffice/python → answer

This adds **one tool**. It is the exact inverse of `document_index`, which
already takes a workspace file into the index:

| tool | direction |
|---|---|
| `document_index` | `/workspace/report.xlsx` → searchable index |
| `document_open` | indexed document → `/workspace/documents/report.xlsx` |

## Part 1 — `document_open` (this spec implements this)

### Contract

```jsonc
// in
{"document_id": "doc_9f2c…", "path": "clienti.xlsx"}   // path optional
// out
{"path": "/workspace/documents/Clienti.xlsx", "file_name": "Clienti.xlsx",
 "mime_type": "application/vnd.openxmlformats-…sheet", "size_bytes": 331239,
 "sha256": "…", "document_id": "doc_9f2c…"}
```

`document_id` accepts **either** id namespace, because two exist and the agent
holds whichever its caller gave it:

- the **search** id (`doc_<hex>`, content-derived) — what `document_search` hits
  carry in `SearchHit.DocumentID`
- the **catalog** uuid — what the web UI, attachments and the REST catalog use

A value that parses as a UUID is treated as a catalog id; anything else is
resolved through `metadata->>'search_document_id'`. That mapping already exists
(`PostgresCatalogStore.catalogIDForSearchDocument`, used by the embedding
worker's status correction); this spec only makes it public.

### Resolution chain

    document_id (search or catalog)
      → aura.documents row              [catalog uuid]
      → CatalogService.GetDocument(identityID, uuid)   ← IDENTITY GATE 1
      → DocumentDetail.Versions[active].AssetID
      → assets.OpenForIdentity(assetID, identityID)    ← IDENTITY GATE 2
      → io.ReadCloser over the Garage object
      → streamed to <WorkspaceRoot>/documents/<file_name>

**Two identity gates, not one.** `GetDocument` is identity-scoped and
`OpenForIdentity` re-checks ownership before any object-store read (the T-IDOR
guard, D-12/D-09). A non-owner never reaches Garage. The tool never returns a
presigned or direct store URL — it streams through, same as D-09 requires of the
HTTP path.

### Fences and limits

- The destination is always inside `WorkspaceRoot`, under a fixed `documents/`
  subdirectory. An absolute `path`, a `..` escape, or a symlink out is rejected —
  the same `withinDir` check `document_index` uses, with `EvalSymlinks` applied
  when the target exists.
- `path` may only name a file, never a directory tree to create outside that root.
- Size is bounded by the asset limits already enforced at `Finalize`; the tool
  does not re-implement them. A partial write is removed rather than left behind.
- Re-opening the same document overwrites its previous materialization. The file
  in `/workspace` is a working copy, not a second source of truth — Garage stays
  authoritative.

### Why not an MCP server

Surveyed 2026-07-31: [PSU3D0/spreadsheet-mcp](https://github.com/PSU3D0/spreadsheet-mcp)
(Rust, Apache-2.0, region detection, pagination — the best of them),
[haris-musa/excel-mcp-server](https://github.com/haris-musa/excel-mcp-server)
(4.1k stars but an authoring tool, silent on large-sheet handling),
[unoserver](https://github.com/unoconv/unoserver) (MIT, the real successor to
unoconv, LibreOffice in listener mode, 50-75% less CPU per conversion), and
several LibreOffice MCP wrappers. `sagacient/libreoffice-mcp-server` is a dead
link (404). `xlsx-for-ai` auto-registers a UUID against a metered SaaS and is
disqualified for an ingestion path.

None of them are needed for Part 1: every capability they expose, Aura's own
container already has locally, and the agent reaches it through `shell_exec`.
`unoserver` becomes worth revisiting only if conversion volume makes per-document
`soffice` startup a bottleneck — at ~1s per file it is not one today.

## Part 2 — the tabular digest (speced, NOT implemented)

Part 1 delivers the capability. Part 2 removes the waste, and is a separate
slice because it touches ingestion, the sidecar and the index.

Today a spreadsheet is chunked, embedded and stored as text — 118 chunks and 118
vectors for `Clienti.xlsx`, none of which can answer an aggregate. Once
`document_open` exists, that work buys nothing. Replace it with **one digest
record per tabular document**:

    title, file name, mime type, size, sha256
    per sheet: name, header labels, row count, a handful of sample rows

The digest is what gets embedded and searched — enough to pick the right file
out of a library, and nothing more. The routing signal already exists: the
markitdown sidecar marks tabular chunks `kind: "rows"` (see
`docker/markitdown/app.py:_table_chunks`).

**Prose does not follow this rule.** On a 30 MB manual, handing over the file
means the agent re-reads 828 chunks' worth of PDF to answer one question; dense
retrieval measured 83% there and earns its cost. The split is:

| content | index holds | agent gets |
|---|---|---|
| tabular (`kind: "rows"`) | one digest | the file |
| prose (`section`/`markdown`) | chunks + vectors, as today | passages |

Expected saving on the current corpus: ~1.9M characters of stored chunk text and
~440 embeddings across the six spreadsheets, replaced by six digests.

## Acceptance

Part 1 is done when all of these hold:

1. `document_open` returns a path under `/workspace/documents/` and the file at
   that path is **byte-identical** to the uploaded original (sha256 compared
   against `document_versions.sha256`).
2. A second identity calling `document_open` with another identity's
   `document_id` gets a not-found error and no object-store read occurs.
3. A `path` argument containing `..`, an absolute path, or a symlink pointing
   outside the workspace is rejected.
4. `go vet`, `go build`, and `go test -race` pass on every touched package.
5. Live: the agent is asked an aggregate over `Clienti.xlsx` — "quanti clienti a
   Torino" — and answers it correctly by opening the file and computing, with the
   tool trace showing `document_search` → `document_open` → `shell_exec`. This is
   the question that scored 0% at every k through retrieval.

## Files

| file | change |
|---|---|
| `internal/documents/catalog_store.go` | make the search-id → catalog-uuid resolver public |
| `internal/documents/open.go` | new — `OpenService`, the two-gate resolution chain |
| `internal/agent/tools/document_open.go` | new — the tool |
| `internal/agent/tools/document_search.go` | description points at `document_open` for tabular |
| `cmd/aura/main.go` | register the tool where `document_index` registers |
