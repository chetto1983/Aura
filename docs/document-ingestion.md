# Aura Document Ingestion

Aura indexes large PDF, XLSX, and DOCX files through a two-lane pipeline. Files
can enter through the CLI, Telegram, or the shared asset pipeline used by the web
cockpit:

```text
file -> extractor sidecar /extract -> Postgres job state
     -> Neo4j Document/Chunk fulltext upsert -> searchable
     -> embedding sidecar -> Neo4j vector properties -> complete
```

## Stores

Postgres stores ingestion jobs in `aura.document_ingest_jobs`: status, progress,
errors, source id, document id, and timing.

Neo4j stores document data:

- `(:Document)` metadata and lifecycle.
- `(:Chunk)` text, locator JSON, hashes, optional embedding.
- `(:Document)-[:HAS_CHUNK]->(:Chunk)`.

The agent-memory MCP server is separate. It is for conversational memory,
entities, preferences, and facts. It is not the primary store for document
chunks.

## Status Lifecycle

```text
accepted -> extracting -> searchable -> embedding -> complete
                       \-> failed
```

`searchable` means sparse fulltext retrieval is ready. Dense embeddings are a
background enhancement and must not block user questions.

## Supported Files

- `.pdf`
- `.xlsx`
- `.xlsm`
- `.docx`

Default size policy:

- `<= 5 MiB`: synchronous Telegram path.
- `> 5 MiB` and `<= 50 MiB`: accepted large-document path.
- `> 50 MiB`: refused by default.

## CLI

Start dependencies:

```powershell
docker compose up -d neo4j aura-llama-embed markitdown
```

Apply Neo4j schema:

```powershell
go run ./cmd/aura neo4j migrate
```

Ingest:

```powershell
go run ./cmd/aura docs ingest "C:\Users\Davide\OneDrive - Sonepar\Documenti\G220_op_instr_0824_en-US.pdf"
```

Search:

```powershell
go run ./cmd/aura docs search "safety reset" --limit 5
```

Benchmark:

```powershell
go run ./cmd/aura docs bench "C:\Users\Davide\Desktop\Gestito Linea Automazione 2025.xlsx" --query "automazione linea"
```

Live E2E:

```powershell
$env:AURA_DOC_TEST_PDF='C:\Users\Davide\OneDrive - Sonepar\Documenti\G220_op_instr_0824_en-US.pdf'
$env:AURA_DOC_TEST_XLSX='C:\Users\Davide\Desktop\Gestito Linea Automazione 2025.xlsx'
$env:AURA_DOC_TEST_DOCX='C:\Users\Davide\OneDrive - Sonepar\Documenti\Corso Robot\Corso Base Robot.docx'
$env:AURA_DOC_TEST_RESET='1'
go test -tags document_ingest_live ./internal/documents -run TestLiveDocumentIngestE2E -count=1 -v
```

## Telegram

When `DocumentIngest` is wired by `aura serve`, Telegram document uploads use the
shared asset pipeline and reply when the file is indexed. The asset service stores
the original object, records lifecycle state in Postgres, and hands supported
documents to this ingestion pipeline.

## Web And Asset Uploads

The web cockpit uploads documents through `/api/assets`. A document asset becomes
usable for questions when its asset status reaches `searchable` or `complete`.
`searchable` has the same meaning as in the direct ingestion path: fulltext
retrieval is ready, while embeddings may still be catching up.

See [Aura Asset Pipeline](asset-pipeline.md) for object-store setup, upload
smoke testing, and asset troubleshooting.

## Agent Tool

The deferred `document_search` tool searches indexed chunks and returns cited
results with document id, chunk id, file name, locator, score, and text.

## Troubleshooting

Neo4j unreachable:

```powershell
go run ./cmd/aura neo4j ping
```

Missing MCP binary:

```powershell
pip install mcp-neo4j-cypher==0.6.0
```

Sidecar down:

```powershell
curl http://127.0.0.1:8083/health
```

Embedding dimension mismatch:

- Check `AURA_EMBED_DIMENSIONS`.
- The default Neo4j vector index expects 384 dimensions.

Fulltext index missing:

```powershell
go run ./cmd/aura neo4j migrate
go run ./cmd/aura neo4j status
```
