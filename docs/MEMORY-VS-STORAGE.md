# Memory vs Storage — Two-Layer Model

> Cross-reference: `prd.md §5.7`

Aura's persistence is split into two distinct layers. The distinction is **rebuildability**:

- **Memory** — rebuildable projections and retrieval indexes. If you delete the memory layer you lose speed and freshness, not truth. A rebuild restores it from storage.
- **Storage** — durable artifacts that are the product truth. If you delete the storage layer you lose actual data permanently. No rebuild path exists.

Getting this wrong causes data loss. A new long-term data store must declare which layer it belongs to before its first merge.

---

## Layer Table — `internal/` subdirectory classification

| Subdirectory | Layer | Rationale |
|---|---|---|
| `agentnote` | **Memory** | Agent working-memory scratchpad; scoped to a conversation lifecycle; garbage-collected and rebuildable. |
| `conversation` | **Storage** | Conversation archive written to SQLite per-turn; source of truth for consolidation; not rebuildable from Telegram. |
| `learning` | **Memory** | Operational lessons and experience store; promoted from tool attempts; rebuildable by re-running the promotion pipeline over `conversation`. |
| `storage/freshness` | **Memory** | Freshness tracking (pending_count, last_indexed_at) for memory projections; rebuildable by inspecting source timestamps. |
| `storage/memoryindex` | **Memory** | Compact memory projection (compact_memory_documents); entirely rebuildable from wiki + sources + archive via reindex. |
| `storage/qdrant` | **Memory** | Vector index sidecar; rebuildable by re-embedding all documents. |
| `storage/reindex` | **Memory** | Rebuild orchestration for memory projections; produces no durable state itself. |
| `storage/runs` | **Memory** | Run-event log; ephemeral causal record for a single agent turn; not preserved across restarts. |
| `storage/search` | **Memory** | FTS / BM25 search index; rebuildable from wiki and sources. |
| `storage/sources` | **Storage** | Raw source files (PDF, DOCX, XLSX, PPTX) stored by SHA-256 hash; OCR / markitdown extracts; immutable evidence. Cannot be reconstructed if lost. |
| `wiki` | **Storage** | Curated Markdown knowledge graph; the product truth; not rebuildable from any other layer. Backups mandatory. |
| `backup` | **Infrastructure** | Backup orchestration for the Storage layer. |
| `db` | **Infrastructure** | SQLite connection management and migrations; the persistence substrate used by both layers. |
| `dbrecovery` | **Infrastructure** | Recovery from a corrupt or inaccessible SQLite file. |
| `skills` | **Storage** | Procedural knowledge files on disk; re-installable from remote registries, but locally-authored skills are irreplaceable without a backup. Treat as Storage. |
| `workspace` | **Storage** | User workspace files written by `workspace_write`; not rebuildable. |
| `agent` | **Infrastructure** | Agent loop runtime; no durable state of its own. |
| `api` | **Infrastructure** | HTTP API layer; stateless. |
| `budget` | **Infrastructure** | Per-conversation token budget accounting; ephemeral. |
| `channels` | **Infrastructure** | Channel adapters (Telegram, future WhatsApp); transport only. |
| `chat` | **Infrastructure** | Chat hub routing messages between channels and the agent loop. |
| `concurrency` | **Infrastructure** | Generic concurrency primitives. |
| `config` | **Infrastructure** | Configuration loading; no runtime state. |
| `cron` | **Infrastructure** | Cron scheduler; schedules live in SQLite (Storage), not here. |
| `files` | **Infrastructure** | File generation helpers (xlsx, docx, pdf); stateless. |
| `httputil` | **Infrastructure** | HTTP client utilities; stateless. |
| `identity` | **Infrastructure** | Auth / access control; tokens persisted to SQLite (Storage). |
| `install` | **Infrastructure** | One-shot model asset installer; side-effects are Storage (GGUF files). |
| `llm` | **Infrastructure** | LLM HTTP client; stateless. |
| `logging` | **Infrastructure** | Structured logging; writes to stdout, not a data layer. |
| `mcp` | **Infrastructure** | MCP server lifecycle management; config on disk (Storage). |
| `probe` | **Infrastructure** | E2E test harness; no runtime state. |
| `release` | **Infrastructure** | Version / release utilities; stateless. |
| `sandbox` | **Infrastructure** | Python code-execution sandbox; ephemeral. |
| `secrets` | **Infrastructure** | Secret decryption sidecar bridge; no stored state itself. |
| `stringx` | **Infrastructure** | String utilities; stateless. |
| `swarm` | **Infrastructure** | Multi-agent swarm orchestration; task queue in SQLite (Storage). |
| `telegram` | **Infrastructure** | Telegram transport and session state; sessions ephemeral. |
| `testutil` | **Infrastructure** | Test helpers; no runtime state. |
| `tray` | **Infrastructure** | Windows tray icon; UI only. |

---

## The Cardinal Rule

**Every new long-term data store must answer one question before merging:**

> Is this Memory or Storage?

### If Memory:

- It must have a rebuild path (a command, a migration, or a reindex job that recreates it from Storage without user data).
- Backup is optional; correctness is ensured by the rebuild path.
- It may be wiped and rebuilt on schema changes without data loss.
- It must tolerate NULL / stale data during and after a rebuild.

### If Storage:

- It must be included in the backup strategy (`internal/backup`).
- Schema changes require a migration in `internal/db/migrations` (SQLite) or a file-format version bump (filesystem).
- There is no rebuild. Loss is permanent.
- It must never contain rebuildable projections — keep Storage lean.

### The wrong answer is "it depends"

If you cannot decide, ask: *"If I deleted this today, would Aura lose any information the user intentionally gave her?"*  
Yes → Storage. No → Memory.

---

## Layer movement rules (from `prd.md §5.7`)

```
source_corpus  → knowledge_wiki      via ingest or curated synthesis
conversation_archive → user/project  via validated consolidation
experience_store → operational/skills via repeated-success validation
derived_artifacts → wiki/source       only when intentionally filed
proposal_queue → durable layer        only after approval
```

Movement is always **explicit**. No accidental promotion. No silent copy.

---

## What lives where in SQLite (`aura.db`)

| Table | Layer |
|---|---|
| `api_tokens`, `pending_users`, `allowed_users` | Storage |
| `wiki_issues`, `proposed_updates` | Storage |
| `scheduled_tasks` | Storage |
| `conversations` (archive) | Storage |
| `compact_memory_documents` | Memory |
| `compact_memory_freshness` | Memory |
| `embedding_cache` | Memory |
| `budget_usage` | Memory |

---

*Last updated: 2026-05-17. Owner: add this classification to every new PR that introduces a new table or file-system path.*
