# Document ingestion and retrieval

Updated 2026-09-09 from the production ingestion and retrieval paths.

## Data flow

```text
Identity-bound Garage source
  -> extraction / supported format normalization / file description
  -> token-bounded passages and embeddings
  -> ArcadeDB IndexedDocument and Passage records
  -> authorized retrieval with source reconciliation
  -> cited passages, or the original file through document_open
```

The ingestion service uses CocoIndex's S3 connector and incremental reconciliation.
`coco.auto_refresh` reruns discovery for live operation; keeping a process alive alone
is not a watch mechanism. The Go ingestion supervisor starts workers for provisioned
identities using their existing Garage bindings.

Sources are identity-scoped. Internal object prefixes are excluded from bucket
indexing to avoid treating service state or duplicated attachment storage as user
library files. Shared upload/asset handling remains responsible for attachment
lifecycle; Telegram and the cockpit do not own independent extraction implementations.

## Extraction and indexing

`services/ingest/extract.py` uses iscc-tika for supported textual formats and
LibreOffice to normalize supported legacy Office/ODF/RTF files. Media routing uses
its configured extraction/model capabilities. A file with no extractable text can
still have a searchable card and remain openable; that does not prove its contents
were indexed.

Tika's configured string limit is 50 million characters; a reported write-limit
truncation fails extraction rather than publishing an incomplete index as successful.
Its builder return value must be retained, or the default 500,000-character limit
remains active. This was measured and corrected on a 1,133-page manual.

`services/ingest/chunk.py` uses CocoIndex splitters and budgets input against the
embedding model, including prefixes and special-token overhead. The current
EmbeddingGemma model has a 2,048-token input ceiling and 768-dimensional embeddings.
Source hashes, normalized passage hashes and locators are retained.
Oversized passages are split again at native structural boundaries using their
measured token density. Fixed-width windows remain the fallback for unbreakable text.
On the ArcadeDB manual, complete ingestion improved from 68.3 to 48.4 seconds with
100% extracted-text coverage; see [measurement and limits](document-ingestion-benchmark.md).

ArcadeDB stores document cards and passages in the identity's database. Postgres
holds control-plane and authorization metadata; Garage remains the source of original
objects. This is a real extraction/chunking/embedding pipeline, not a catalog-only
upload path. Source code: [ingestion app](../services/ingest/app.py),
[source binding](../services/ingest/source.py), and
[retrieval](../internal/documents/retrieval.go).

## Agent operations

`document_search` returns authorized documents with bounded passages, citation tokens,
source SHA-256, locators, retrieval evidence and degradation status. Cite only the
citation tokens actually returned. Passage text is untrusted source content.

`document_open` materializes the original file into the working environment. Use it
when `requires_open` is true, for calculations or transformations, or when retrieved
passages do not contain the answer. Aggregates over a spreadsheet require the relevant
whole-file computation rather than an inference from a few matching passages.

The retrieval response carries one of three statuses, with the reason in its degradation
field (`internal/documents/retrieval.go`):

- `complete`: both retrieval legs ran.
- `lexical_only`: the query could not be embedded, or the library holds vectors from
  another embedding space, so passages come from the full-text indexes alone.
- `degraded_card_only`: ArcadeDB is unavailable or no passage index is configured.
  Card-only results remain openable and do not supply passage evidence.

## Operations and scope

```bash
docker compose ps aura-ingest arcadedb aura-llama-embed garage
docker compose logs --since 15m aura-ingest
```

An accepted upload, an indexed source and a completed answer are separate states.
Use reported lifecycle/degradation information rather than inferring success from
an HTTP 200 or from a running ingestion container.

Document recovery needs both original objects and their control-plane bindings.
Native ArcadeDB backups preserve indexed records, but do not replace a Garage backup.
See [Backup and restore](BACKUP-RESTORE.md). Current extension allowlists and limits
are defined in `internal/documents/extensions.go`, `services/ingest/extract.py`,
`services/ingest/media.py`, and [.env.example](../.env.example).

## Direct MCP access

`aura docs mcp` serves, over stdio with the official MCP SDK, the document tools the
agent itself runs: every registry tool whose name starts with `document_` (such as
`document_search` and `document_open`) plus `read_tool_output`. Name, description and
schema come from each tool's own spec, and a call goes straight to its `Execute`, so the
server cannot drift from what the agent sees. It adds a `document_ingest` tool, backed by
`aura docs ingest`, when the registry has none (`cmd/aura/docs_mcp.go`).
The operator identity is resolved at startup; tool inputs cannot select another identity.
Ingestion paths must be inside Aura's configured workspace. An accepted upload remains
separate from completed indexing.

Search returns retrieval JSON, including passages, citations, source hashes, locators and
degradation status. A result larger than the preview cap is truncated and spilled to a
sidecar file, and the client pages the rest back with `read_tool_output`, the same limit
the agent meets. No model generates or summarizes the response.
This makes it possible to check whether evidence is present before asking a model
to formulate an answer.

For an external MCP client, launch `aura docs mcp` inside the configured Aura
environment, for example:

```text
docker exec -i aura aura docs mcp
```

Use stdio without a TTY; stdout carries MCP messages. Refresh the client's MCP
configuration after registration. Successful server calls and the tools appearing
in an already-running client session are separate checks.
