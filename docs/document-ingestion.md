# Aura Document Ingestion

Ingestion registers a file in a catalog. It does **not** read it.

```text
file -> size + extension gate -> content hash -> Postgres catalog row + job -> searchable
```

There is no extractor call at ingest, no chunking, no passage store, and no
embedding queue. Everything the old pipeline built existed to answer *"what does
this document say"* from a ranked fragment — and that question is now answered by
handing the agent the original file (`document_open`) and letting it compute on it
with `shell_exec`. What ingestion still owes the rest of the system is the row
`document_search` ranks and `document_open` resolves.

Files enter through the CLI, Telegram, or the shared asset pipeline the web
cockpit uses. All three cross the same allowlist and the same size ceiling.

## Stores

Postgres, and only Postgres.

- `aura.document_ingest_jobs` — job state: status, source id, document id, timing,
  error.
- `aura.documents` — one row per file: identity owner, title, tags, content hash,
  metadata (including the `doc_<hex>` search id), and the **digest**.

The digest is what makes a file findable. It is not computed at upload: the agent
writes it with `document_describe` after it has actually opened the document — its
own note about a file it has read, in the same spirit as a memory fact. Ranking is
a weighted `tsvector` over **title (A) / tags (B) / digest (C)** behind a GIN index
(migrations `0080`–`0082`); tags are weighted between title and digest because they
are the one part of a document's description a *person* wrote.

Every read and write is identity-scoped in SQL, and an empty or malformed identity
is rejected before a statement runs — an unresolved principal returns nothing
rather than everything.

The memory MCP server is a separate store (ArcadeDB, one database per identity)
and holds conversational facts and entities. It has never been the document store.

## Status lifecycle

```text
accepted -> searchable
         \-> failed
```

`searchable` means the catalog row exists and the file is routable. (`complete` is
still recognized by the store's SQL for older rows, but nothing writes it.)

## Supported files

A single allowlist (`internal/documents/extensions.go`) gates every entry path:

`.pdf` · `.docx` · `.pptx` · `.xlsx` · `.xlsm` · `.csv` · `.html` · `.htm` · `.md`
· `.markdown` · `.txt` · `.json` · `.xml` · `.epub` · `.png` · `.jpg` · `.jpeg`
· `.gif` · `.webp`

A name with no extension is refused. The size ceiling is
`DefaultMaxIngestBytes` (50 MiB) and applies to all of them; the Telegram path
takes files up to 5 MiB synchronously and larger ones through the accepted
large-document path.

## CLI

```powershell
docker compose up -d postgres
go run ./cmd/aura db migrate

go run ./cmd/aura docs ingest "C:\path\to\manual.pdf"
go run ./cmd/aura docs search "safety reset" --limit 5
go run ./cmd/aura docs list
go run ./cmd/aura docs status <job-id>
```

`docs search` is identity-scoped: `runDocs` resolves the operator onto the context
first, exactly as the `document_search` tool does, so an unresolved principal gets
an empty library rather than someone else's.

## Telegram

When `DocumentIngest` is wired by `aura serve`, Telegram document uploads go
through the shared asset pipeline and reply when the file is catalogued. The asset
service stores the original object and records lifecycle state in Postgres.
(Telegram *conversion* to markdown for inline reading still uses the markitdown
sidecar — that is the multimodal path, not the ingest path.)

## Web and asset uploads

The cockpit uploads through `/api/assets`. A document asset is usable once its
status reaches `searchable`. See [Aura Asset Pipeline](asset-pipeline.md) for
object-store setup, upload smoke testing, and asset troubleshooting.

## Agent tools

Four tools, all deferred:

| Tool | What it does |
|---|---|
| `document_search` | Ranks the identity's library and returns **documents** — id, title, tags, digest. Never passages. Empty query lists the library, newest first, which is what "the file I just uploaded" means. |
| `document_open` | Writes the real file into `/workspace/documents/` so the agent can convert and compute on it (LibreOffice, python with openpyxl/pandas, PyMuPDF, pdftotext are installed in the box). |
| `document_index` | Adds a file the agent itself created in the workspace to the library. |
| `document_describe` | Records what a document contains, which is what makes it findable later. |

### Why files and not passages

The two-stage pipeline this replaced (dense seed → cross-encoder rerank → 1-hop
graph expansion) was measured on a 5889-row customer list: an exact lookup scored
100% and **every aggregate scored 0% at every k**, because "how many customers in
Torino" is a property of the whole document and lives in no passage. The same held
for a 29 MB manual with 616 distinct parameters. Handing over the file is not a
degradation of that design; it is the answer to the question the design could not
reach.

The cross-encoder client (`internal/rerank`) and the optional `aura-rerank` GPU
sidecar still exist in the tree and in `compose.yaml`, but **nothing calls them** —
their only consumer was that pipeline.

## Troubleshooting

Catalog unreachable:

```powershell
go run ./cmd/aura db ping
go run ./cmd/aura doctor
```

A file ingests but `document_search` cannot find it:

- It has no digest yet. Ingestion does not write one — the agent does, via
  `document_describe`, after opening the file. Until then the row is findable only
  by title and tags.
- Check the owning identity: the query filters on it, and a document ingested
  under a different principal is invisible by design.

Upload rejected:

- Extension not in the allowlist above, or the file is over 50 MiB.
