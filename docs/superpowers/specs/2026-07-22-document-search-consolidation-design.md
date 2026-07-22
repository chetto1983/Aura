# Aura — Document search / ingestion consolidation (design + ADR)

**Design date:** 2026-07-22
**Related:** Amendment #88 (`docs/superpowers/specs/2026-07-21-aura-dedicated-workspace-garage-design.md`, the `/workspace` toolchain that makes agent-created documents common) · memories `[[l4-recall-injection-security-followup]]`, `[[e2e-real-not-smoke]]`.

## 0. Locked scope (operator, 2026-07-22)

Consolidate the agent's document surface so it stops confusing **uploaded/indexed documents** (a searchable Neo4j knowledge base) with **files the agent creates in `/workspace`** (the filesystem), and give it a deliberate bridge between the two. Approved scope:

- **Durable steering** — a static `<documents>` doctrine block in the system prompt + aligned tool descriptions, so the tool choice is unambiguous.
- **Explicit index bridge** — a new agent tool `document_index` that indexes a `/workspace` file into the identity's knowledge base on demand, reusing the existing ingest pipeline.

**Explicitly OUT of scope:** unifying the two byte stores (object store vs Neo4j), auto-indexing delivered/created files (would reopen D-03 "no silent searchable memory"), any change to the upload → ingest automatic path, re-architecting retrieval.

## 1. Why this is correct (evidence, not supposition)

Reconnaissance of the live codebase (file:line in §6) established:

- **Two physically distinct stores share the word "document."** (1) The **object store** (Garage/S3 or filesystem, `internal/objectstore`) holds raw uploaded/delivered **bytes** at `identity/<id>/asset/<id>/original`. (2) The **Neo4j knowledge base** (`internal/documents`) holds extracted `:Document`/`:Chunk` nodes + 768d embeddings and is the **only** thing `document_search` reads.
- **Ingestion is automatic on upload, never an agent tool.** Channel attachment handlers (Telegram `bot_dispatch_asset.go`, web `assets_api.go`, CLI `aura docs ingest`) drive `assets.Service.Ingest…File(process:true)` → `assets.DocumentProcessor` → `documents.Service.IngestPath` → markitdown sidecar extract/chunk → `documents.Indexer` Neo4j sparse write (`status="searchable"`) → async embed worker.
- **Three confusion points** (the root causes):
  1. **Contradictory surface framing.** `document_search` and the per-turn knowledge catalog say uploaded docs are "NOT on the filesystem — do NOT use fs_glob/fs_grep/shell"; the `<workspace>`/`<machine>` prompt doctrine says "put deliverables in `/workspace/artifacts`" and "save the files you produce in your workspace." They describe **different** files (indexed uploads vs agent-created artifacts) but nothing tells the agent where the boundary is. An agent that created a docx in `/workspace/artifacts` and is asked "what does the document say?" has two plausible readings and picks wrong.
  2. **`send_file` ingests to a different store than `document_search` reads — invisibly.** A delivered file goes through `assets.Service.IngestAgentFile` with `process:false` (deliberate, D-03: "a produced deliverable must never silently become searchable memory") → object store only, never Neo4j, `DocumentID` empty. `document_search` can never find it. This boundary lives only in code comments; the agent may believe "I delivered it, so I can document_search it" — false.
  3. **No durable `<documents>` doctrine.** The static prompt teaches `<workspace>`/`<memory>`/`<skills>`/`<machine>` but never that uploads are a separate searchable store. All document steering lives in per-tool descriptions + per-turn asset context, not in `messages[0]`.
- **The bridge is cheap and already built.** `documents.Service.IngestPath(ctx, IngestRequest{IdentityID, …}, path)` is the exact pipeline the CLI `aura docs ingest` uses; `IngestRequest` already carries `IdentityID` for the per-identity `(:User)-[:HAS_DOCUMENT]->(:Document)` ownership edge. Exposing it as a scoped agent tool is a thin wrapper, not new machinery.

## 2. Design

### 2.1 `<documents>` doctrine block (Component 1 — the steering fix)
A new **static** block in `internal/agent/prompt.go`, placed immediately after `<workspace>` (the two "where do files live" concepts sit together). Content (byte-stable — respects the KV-cache invariant amendment #16/#29; verified by a needle test like `<workspace>`'s):

```
<documents>
- Two separate document worlds — never confuse them:
  1. The user's UPLOADED/INDEXED documents live in a searchable knowledge base (Neo4j), NOT on the filesystem. For "this document", "the file I uploaded", the PDF/spreadsheet/manual, or what a document says/contains/lists → call document_search FIRST; do NOT fs_glob/fs_grep/shell for them.
  2. The files YOU create or work on live in /workspace (see <workspace>). Read and search them with fs_read/fs_grep, not document_search.
- A file you write under /workspace or deliver with send_file does NOT become searchable on its own. If the user will need to find or recall it later, index it explicitly with document_index — then document_search can find it.
```

### 2.2 Tool description alignment (Component 2)
- `document_search` (`internal/agent/tools/document_search.go`): keep the "NOT on the filesystem" steering; add one clause that files the agent itself creates live in `/workspace` (search with `fs_*`) and become searchable via `document_index`. This removes the apparent contradiction by naming the other world explicitly.
- `send_file` (`internal/agent/tools/send_file.go`): add one clause that a delivered file is display/delivery only and is **not** searchable unless indexed with `document_index`.
- `fs_*`: no change (they already default to the workspace root; the `<documents>` block covers the steering).

### 2.3 `document_index` tool (Component 3 — the explicit bridge)
- **Backend — Approach A (direct `IngestPath`, chosen).** The tool calls `documents.Service.IngestPath(ctx, documents.IngestRequest{IdentityID: ownerFromContext(ctx), SourceKind: "workspace", SourceID: <path/base>}, absPath)`. This reuses the proven CLI pipeline (markitdown extract → chunk → Neo4j sparse index → async embed) and sets the per-identity ownership edge, matching `document_search`'s retrieval scoping (`ownerFromContext`). *Rejected Approach B* (route through the asset layer with `process:true` for full upload parity — object-store byte copy + asset lifecycle): it duplicates bytes that already live durably in `/workspace` (and the per-user Garage bucket via `send_file`), adds a new `process:true` agent-ingest path that reopens the D-03 surface, and buys nothing for search (only Neo4j chunks matter for retrieval).
- **Signature:** `{ "path": string (required, absolute or workspace-relative), "title": string (optional display name) }` → returns `{ document_id, file_name, chunks, status }`.
- **Path confinement:** only files resolving under `AURA_WORKSPACE_DIR` may be indexed; a path that escapes the workspace root (after symlink-resolve) is rejected. Defense-in-depth — the agent indexes its own working files, not arbitrary host paths.
- **Visibility:** `Deferred: true` — an occasional deliberate action, discovered via `tool_search`; the `<documents>` block names it so the agent knows it exists and load-then-calls it. `document_search` stays always-visible (headline retrieval; hiding it caused a past regression).
- **Idempotency:** `IngestPath` keys ingestion by content hash/document identity; re-indexing the same file updates rather than duplicating (confirmed against `documents.Indexer` upsert semantics during planning).
- **Wiring:** registered in `cmd/aura/main.go` next to `DocumentSearch`. Note the backend needs an **ingest-configured** `documents.Service` (Extractor/Indexer/Jobs/Embedder — what the CLI `aura docs ingest` builds via its `docsServiceFactory`), NOT the retrieval-configured Service `docsToolSearcher` builds (Searcher/Reranker). The plan reuses the CLI's ingest builder rather than the retrieval one; if a lazy per-call factory is cleaner than holding an ingest Service on the tool, the plan decides.

### 2.4 Security, cache, tests (Component 4)
- **Security (memory-write surface, `[[l4-recall-injection-security-followup]]` / D-03):** `document_index` writes into searchable memory, so it stays (a) **explicit** — only when the agent calls it, never silent, preserving D-03; (b) **identity-scoped** — the ownership edge is `ownerFromContext(ctx)`, no cross-identity write; (c) **path-confined** to `/workspace`. The tool result carries no untrusted-content escalation beyond the existing ingest path.
- **KV-cache:** the `<documents>` block is static content in `messages[0]` → byte-identical turn-to-turn; a `prompt_test.go` needle test pins the required phrases (as for `<workspace>`). No golden prefix-hash pin exists (verified in Amendment #88).
- **Tests:** daemon-free unit tests for `document_index` — path-confinement fencing (reject outside-workspace + symlink escape), identity scoping (owner from context), arg validation (missing/empty path), nil-backend error. The ingest pipeline itself is already covered in the `documents` package. Prompt needle test. No new integration tier required (the tool is a thin wrapper over an already-integration-tested pipeline), but the real E2E (§4) exercises it live.

## 3. Proposed PRD amendment (ratify before code — plan Task 0)

> **Amendment #89 (2026-07-22): Document surface consolidation + explicit index bridge.** Aura gains a static `<documents>` system-prompt doctrine block distinguishing the two document worlds — the user's uploaded/indexed documents (searchable Neo4j knowledge base, via `document_search`, not on the filesystem) and the agent's own `/workspace` files (filesystem, via `fs_*`/skills) — and a new deferred agent tool **`document_index`** that indexes a `/workspace` file into the calling identity's knowledge base on demand (reusing `documents.Service.IngestPath`, per-identity ownership, path-confined to `AURA_WORKSPACE_DIR`). `document_search` and `send_file` descriptions are aligned to name the boundary. Delivered/created files remain non-searchable until explicitly indexed (D-03 "no silent searchable memory" preserved). Out of scope: store unification, auto-indexing, changes to the automatic upload→ingest path. Design: `docs/superpowers/specs/2026-07-22-document-search-consolidation-design.md`. (Next free amendment number confirmed at plan time.)

## 4. Real E2E (Definition of Done, score >9.8)

Drive an authenticated operator turn on the live stack: ask Aura to *produce* a document in `/workspace`, then in a later turn ask *what it says* / to *find* it. Assert: (a) the created file is NOT auto-searchable (document_search miss before indexing); (b) after the agent calls `document_index`, `document_search` returns cited chunks scoped to the operator; (c) the agent does not fs-hunt for an uploaded document nor document_search a purely-local artifact — it picks the right tool per the `<documents>` block. Contrast with an actually-uploaded document (auto-searchable). `[[e2e-real-not-smoke]]`.

## 5. Risks / carried

- **`/workspace` is currently shared (non-strict), not per-identity** — the Neo4j ownership edge is identity-scoped for *search*, but the filesystem bytes are not isolated (same carried limitation as `shell_exec` in the shared container). Fine for single-operator now; flag for MUSR.
- **markitdown sidecar dependency** — `document_index` fails soft if the sidecar is down (same as the upload path); the tool surfaces the error rather than pretending success.
- **Index-then-poison** — because indexing is explicit and identity-scoped, the memory-poisoning surface is bounded to what the agent deliberately indexes from its own workspace; no new cross-trust boundary vs the existing upload path.

## 6. Sources

Live codebase reconnaissance (2026-07-22): `internal/agent/tools/document_search.go`, `send_file.go`, `send_file_ingest.go`, `fs_*.go`; `internal/documents/{service,retrieve,search,indexer,extract_client,types,worker}.go`; `internal/assets/{service,ingest_agent,document_processor,context}.go`; `internal/agent/prompt.go`; `cmd/aura/docs.go`/`main.go`; `docker/markitdown/app.py`.
