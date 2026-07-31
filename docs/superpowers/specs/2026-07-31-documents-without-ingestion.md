# Documents without ingestion — and the removal of Neo4j

**Date:** 2026-07-31
**Status:** spec. Phase 0 shipped (`bd7e46156`, `8e2fd300c`); phases 1-3 pending.
**Follows:** `2026-07-31-document-open-handle-not-passage.md`, which established
`document_open` and measured why aggregates are unanswerable from chunks.

## The sentence this rests on

> *"te mica ingerisci i file, li leggi"*

Nobody built Claude Code a document index. It is given a path and it reads the
file. Every capability Aura was building — extract, chunk, embed, rank, rerank,
graph-expand — exists to substitute for an ability she already has, in a
container that already carries LibreOffice, pandas, openpyxl, PyMuPDF and
poppler, with `/workspace` durable and `shell_exec` live.

The measurements that make this more than an aesthetic preference are in the
previous spec. In one line: an aggregate over a document — *how many customers in
Torino*, *how many parameters does this manual document* — scores **0% at every
k**, on tabular content and on prose alike, because the answer is a property of
the whole document and lives in no passage. Handing over the file removes the
ceiling; nothing else does.

## End state

| moment | what happens |
|---|---|
| upload | store the bytes in Garage, write one catalog row: title, tags, mime, size, sha256. **Nothing is read.** |
| "which file?" | list the library — title, tags, size, kind, and the description if one exists |
| "what's in it?" | `document_open` → the real file in `/workspace` → LibreOffice/python |
| after reading | she writes what she saw into `documents.digest` — an `ls` line that improves with use |

There is no extraction step, no chunk table, no vector, no reranker, no graph.
The `digest` column shipped in `8e2fd300c` changes meaning under this spec: it is
not a pipeline product computed for every document at upload (most of which are
never opened), it is **her own note about a file she has actually read**, in the
same spirit as a memory fact.

## Phases

### Phase 0 — shipped

- `document_open` (`bd7e46156`): resolves a document id → catalog → asset →
  Garage, streams the original into `/workspace/documents/`, two identity gates.
  Verified live: `tool_search → document_search → document_open → shell_exec`,
  "quanti clienti a TORINO" → **699**, exact; materialized sha256 == catalog's.
- The digest column, weighted `tsvector` (title A / tags B / digest C), GIN
  index, `SearchDigests` (`8e2fd300c`). `simple` config, no stemming — a single
  stemmer over a mixed-language corpus was measured to halve recall.
- The `tool_not_loaded` dispatch bounce now returns the schema instead of an
  errand (`db65ab638`), saving one LLM round trip per deferred tool.

### Phase 1 — `document_search` becomes the library

`document_search` stops calling the two-stage retrieval pipeline and calls
`SearchDigests`. It returns documents, not passages: `document_id` (the id
`document_open` takes), title, tags, digest, rank. Its description already tells
the model to call `document_open` for anything needing the whole file; it now
tells it that hits ARE files.

Add `document_describe(document_id, description)` — one call, writing
`documents.digest`. This is how the library learns. It is the write-back half of
"she reads the file"; without it every question about a badly-named file starts
from zero forever.

**Acceptance.** Over the golden corpus in `D:/tmp/baseline-corpus` (12 files, six
of them spreadsheets, mixed Italian and English), with only title and tags
populated: a query naming a document by its subject returns that document first.
Then, after `document_describe` has run on three of them, a query naming a
*column* ("ragione sociale", "venditore") returns the right spreadsheet first —
which title-only ranking cannot do.

### Phase 2 — validate memory recall on ArcadeDB, with the live agent

ArcadeDB is long-term memory and nothing else (operator decision, 2026-07-31).
Before deleting the alternative, the memory path is validated end to end with the
running agent, not with a unit test: 24 real facts are re-seeded (done), and the
agent is asked paraphrased recall questions whose words are NOT the facts' words.

**Acceptance.** She answers from memory, and the trace shows the memory tool
being called. A failure here stops phase 3 — deleting Neo4j while recall is
broken would leave her with nothing.

**RUN 2026-08-01: FAILED. Phase 3 is blocked.** Asked a paraphrased recall
question, the agent called `memory_search` six times and `memory_get_entity`
three, and every one returned "memory operation failed". Two causes, both found
in the sidecar logs:

1. **Aura's memory is not on ArcadeDB.** The mounted `memory` MCP server is
   `agent-memory-mcp`, configured with `NEO4J_URI=bolt://neo4j:7687`. The
   ArcadeDB memory built on 2026-07-31 is reachable from a developer session but
   is not wired into the agent. So this phase is not "validate ArcadeDB recall",
   it is "move memory to ArcadeDB first" — a slice of its own, and a bigger one
   than this spec assumed.
2. **The embedding layer has drifted apart.** `agent-memory-mcp` embeds through
   `OPENAI_BASE_URL=http://aura-llama-embed:8081/v1`; that container is in a
   restart loop, unable to download `Qwen/Qwen3-Embedding-0.6B-GGUF` (no network)
   and unable to bind 8081 because a hand-started `aura-embed-gemma` holds it.
   That replacement is NOT in the compose project — it sits on the default
   `bridge` network with no compose labels, so no service can reach it by name,
   and its own healthcheck probes port 8080 while it serves on 8081. Exactly the
   `*_BASE_URL` drift class: it degrades silently and only a live run shows it.

Neither is caused by this migration, and neither can be worked around by
deleting more code. Phase 3 stands down until memory answers.

### Phase 3 — remove Neo4j

Measured at spec time:

| | |
|---|---|
| `internal/knowledge` (non-test) | 1523 LOC |
| document retrieval machinery in `internal/documents` | 2968 LOC |
| its tests | 3203 LOC |
| Cypher migrations | 8 |
| non-spike Go files importing `internal/knowledge` | 15 |

The 15 callers: 11 in `cmd/aura` (`neo4j.go`, `docs.go`,
`docs_runtime_searcher.go`, `doctor.go`, `document_graph_deactivator.go`,
`documents_backfill.go`, `embedding_handler.go`, `serve_agui.go`,
`serve_provisioning.go`, `serve_deprovision_purgers.go`, `chat_adaptive.go`),
plus `internal/agui/graph_api.go`, `internal/config/config.go`,
`internal/documents/embedder.go`, and the adaptive benchmark fixtures. Spikes
under `.planning/` and `.claude/skills/` are frozen artifacts and are left alone.

**One casualty to repoint, not discover later:** `internal/agui/graph_api.go` is
the cockpit's graph view. It does not die with Neo4j — it moves to ArcadeDB,
which holds the graph. That is work, not a detail.

Then: the `neo4j` and `mcp-neo4j-cypher` services leave `compose.yaml`, their env
vars leave `.env.example` and every service that carries them (per
[[reference_cloud_profile_forgets_sidecars]], check EVERY `*_BASE_URL`, not just
the obvious one), and the `neo4j_integration` build tag leaves the CI matrix and
`scripts/coverage_gate.sh`.

**Acceptance.** `make quality-full` green with the Neo4j stack **down**; the
live agent answers a document question and a memory question with no Neo4j
container running; `grep -ri neo4j` returns only historical documents and frozen
spikes.

## What does NOT get deleted

- **The embedder.** Memory recall uses it, and the reasoning classifier embeds 27
  anchors per turn. What goes to zero is embedding *of document chunks*.
- **`document_index`.** A file the agent produced still needs a way into the
  library; it just writes a catalog row now instead of chunks.
- **The asset chain.** Presign → upload → finalize → Garage is what makes the
  original retrievable, and `TestAssetRoundTripKeepsTheOriginal` guards it.

## Order, and why it is not negotiable

Each phase is what makes the next one safe:

1. Without the library listing, deleting retrieval leaves nothing able to name a
   file, and `document_open` has no id to open.
2. Without validated memory recall, deleting Neo4j may delete the only working
   recall path.
3. Only then does the deletion touch code that nothing depends on.
