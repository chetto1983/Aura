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

**Prose was expected to be the exception. Measured, it mostly is not.** The
29 MB G220 manual, taken the same way:

| step | cost |
|---|---|
| `pdftotext -layout` over 29 MB → 3.57M characters | **2.1 s** |
| count every distinct fault, alarm and parameter code | **0.21 s** |
| result | 38 faults, 26 alarms, **616 parameters** |

"How many parameters does this manual document" has no k at which retrieval
answers it — same shape as the spreadsheet aggregate, on prose. And the file
carries its own table of contents, so "where is the factory setting of the
terminal strips" resolves to §7.2.7 p.182 out of the text itself.

What retrieval still does better is **paraphrase**: a literal search for
`factory reset` returns 0 hits where `factory setting` returns 79. But an agent
holding the file probed five synonyms in ONE shell call in 0.2 s, where five
embedding searches are five tool round trips. The advantage is narrower than it
looks, and it is about ranking, not reach.

So the split is by QUESTION SHAPE, not by content type:

| question | answered by |
|---|---|
| "how many / list all / which is the largest" | the file — retrieval scores 0 at every k |
| "what does it say about X", exact term known | either; the file is cheaper |
| "what does it say about X", words unknown | retrieval ranks better; the agent can still probe |

Expected saving on the current corpus: ~1.9M characters of stored chunk text and
~440 embeddings across the six spreadsheets, plus 828 chunks and their vectors
for the manual — replaced by seven digests. On the document path specifically,
the embedder and the reranker go to zero. They are NOT removed from Aura: agent
memory recall still uses them, and picking WHICH document out of a library still
needs a ranked digest — but a library of tens of digests does not need HNSW.

Corroborating accident: in the live validation run the reranker was degraded
(`rerank: degraded to identity order — sidecar transport error`) and the answer
was still exact, because the answer never came from retrieval.

## Part 3 — the converter becomes a tool, not a pipeline stage

Once the agent holds the file, conversion stops being something the ingestion
pipeline does to a document and becomes something the agent asks for when it
wants it. Microsoft ships this already:
[`markitdown-mcp`](https://github.com/microsoft/markitdown/tree/main/packages/markitdown-mcp)
— one tool, `convert_to_markdown(uri)`, accepting `file:` URIs, over stdio or
streamable HTTP (`--http --host 127.0.0.1 --port 3001`), installed with a single
`pip install markitdown-mcp`. A `file:` URI is exactly what `document_open`
produces.

That is a compose service plus an MCP recipe entry — no Go code, and it retires
the reason our own `docker/markitdown` FastAPI wrapper exists for the agent
path. The wrapper still serves ingestion's `/extract` until Part 2 shrinks that
to a digest.

Its README warns: *"The server does not support authentication, and runs with
the privileges of the user running it… DO NOT bind the server to other
interfaces."* Both conditions are already met by Aura's deployment shape —
sidecars run in the container network and are not published to the host.

Note what this does NOT need: none of the spreadsheet MCP servers surveyed
above. LibreOffice 7.4.7.2, openpyxl, pandas, PyMuPDF and poppler are already in
the image, and `shell_exec` already reaches them.

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
