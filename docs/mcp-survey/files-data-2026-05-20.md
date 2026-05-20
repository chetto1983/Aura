# MCP Survey — Files, Data, Storage (2026-05-20)

Scope: MCP servers that add a **storage/data dimension** to Aura — local filesystem, local SQL databases (SQLite/Postgres/MySQL/DuckDB), and self-hosted S3-compat object storage (Garage/MinIO/Nextcloud-WebDAV). Aura already has `workspace_*` filesystem tools, `create_xlsx/docx/pdf`, native SQLite, native Qdrant, and a Garage S3 sidecar — candidates are evaluated against that baseline.

Hard exclusions in this scope:
- SaaS-only S3/Drive/Dropbox/OneDrive MCPs (AWS S3 Tables, S3 Tables on Iceberg, etc.)
- `MarimerLLC/calendar-mcp` and `huhabla/calculator-mcp` (already in Aura memory, different domain)

Self-hosted purity (`SH=1`) = zero mandatory external API/key/cloud account. Configurability (`Cfg=0..3`) = how readily the knobs map to a dashboard JSON form (3 = env vars + JSON schema, 0 = code edits).

---

## modelcontextprotocol/servers — Filesystem (reference)
- **URL**: https://github.com/modelcontextprotocol/servers/tree/main/src/filesystem
- **Language / runtime**: TypeScript / Node.js
- **License**: MIT
- **Last commit**: active in main (Jan-2026 release line)
- **Stars**: ~70k (umbrella repo)
- **Transport**: stdio
- **Tool surface**: 13 tools — `read_text_file`, `read_media_file`, `read_multiple_files`, `write_file`, `edit_file`, `create_directory`, `list_directory`, `list_directory_with_sizes`, `move_file`, `directory_tree`, `search_files`, `get_file_info`, `list_allowed_directories`
- **Self-hosted purity**: 1 (pure local fs)
- **Configurability**: 2 (CLI args for allowed dirs + MCP Roots dynamic protocol)
- **Footprint**: green — npx package, ~30 MB node
- **Write capability**: read + write (write tools annotated destructive)
- **Overlap with Aura native**: HIGH — Aura's `workspace_write/read/list/delete` already cover this surface. The only deltas are `read_media_file` (binary) and `edit_file` (in-place patching), plus MCP Roots dynamic root negotiation.
- **Verdict**: SKIP for general use — overlap is ~80%. Optionally ADOPT scoped to a separate "external" path (e.g. `/host-mount/`) for sandboxed read access outside Aura's workspace if that use-case appears.

---

## Digital-Defiance/mcp-filesystem
- **URL**: https://github.com/Digital-Defiance/mcp-filesystem
- **Language / runtime**: TypeScript / Node.js
- **License**: MIT
- **Last commit**: 2025-12-12 (v0.1.9)
- **Stars**: ~0 (brand new)
- **Transport**: stdio
- **Tool surface**: 12 advanced ops — `fs_batch_operations` (with rollback), `fs_watch_directory` + `fs_get_watch_events`, `fs_search_files` + `fs_build_index`, `fs_compute_checksum`/`fs_verify_checksum`, `fs_analyze_disk_usage`, `fs_copy_directory`, `fs_sync_directory`, `fs_create_symlink`
- **Self-hosted purity**: 1
- **Configurability**: 3 (`mcp-filesystem-config.json` with workspaceRoot, blockedPaths, blockedPatterns, resource limits — clean dashboard mapping)
- **Footprint**: green — node binary
- **Write capability**: read + write + watch
- **Overlap with Aura native**: MEDIUM — Aura has no watcher, no batch-with-rollback, no full-text fs index, no checksum verification. These are net-new.
- **Verdict**: CONDITIONAL — interesting capabilities (batch rollback + watch) but **0 stars / brand new** = unproven. Revisit once it has ≥50 stars and a real-world adopter. Could be rebuilt as native Aura tool in <1 day instead.

---

## motherduckdb/mcp-server-motherduck (DuckDB)
- **URL**: https://github.com/motherduckdb/mcp-server-motherduck
- **Language / runtime**: Python (uvx-installable)
- **License**: MIT
- **Last commit**: 2026-04-29 (v1.0.6)
- **Stars**: ~484
- **Transport**: stdio + HTTP (configurable via `--transport`)
- **Tool surface**: 5 tools — `execute_query`, `list_databases`, `list_tables`, `list_columns`, `switch_database_connection`
- **Self-hosted purity**: 1 when used with `--db-path /local/file.duckdb` (no MotherDuck token needed for local files)
- **Configurability**: 3 (CLI flags + env vars; `--db-path`, `--read-only`, `--motherduck-token` optional)
- **Footprint**: yellow — Python + DuckDB engine ~250 MB
- **Write capability**: read + write (gated by `--read-only`)
- **Overlap with Aura native**: NONE — Aura has SQLite + Qdrant but no analytical OLAP engine. DuckDB unlocks columnar SQL over CSV/parquet/JSON files in `/workspace/` directly.
- **Verdict**: ADOPT — high-leverage net-new capability (LLM-driven analytics over wiki sources, scheduled-task history, conversation archive joins). Maintained by MotherDuck team.

---

## crystaldba/postgres-mcp (Postgres MCP Pro)
- **URL**: https://github.com/crystaldba/postgres-mcp
- **Language / runtime**: Python
- **License**: MIT
- **Last commit**: 2025-05-16 (v0.3.0)
- **Stars**: ~2,800
- **Transport**: stdio + SSE/HTTP
- **Tool surface**: 9 tools — `list_schemas`, `list_objects`, `get_object_details`, `execute_sql`, `explain_query`, `get_top_queries`, `analyze_workload_indexes`, `analyze_query_indexes`, `analyze_db_health`
- **Self-hosted purity**: 1 (works with any reachable Postgres incl. self-hosted)
- **Configurability**: 3 (`DATABASE_URI` env + `--access-mode {unrestricted, restricted}` flag — trivial dashboard form)
- **Footprint**: yellow — Python ~200 MB
- **Write capability**: gated — restricted mode = read-only with execution-time cap; unrestricted = full DDL/DML
- **Overlap with Aura native**: NONE today — Aura uses SQLite only. Becomes valuable the moment a Postgres workload appears (analytics warehouse, vector pgvector store, app DB introspection).
- **Verdict**: CONDITIONAL — ADOPT immediately if/when Postgres enters Aura's stack; otherwise queue as "pluggable when needed". Index-tuning + health-check tools are unique value above raw SQL.

---

## wenb1n-dev/mysql_mcp_server_pro
- **URL**: https://github.com/wenb1n-dev/mysql_mcp_server_pro
- **Language / runtime**: Python
- **License**: MIT
- **Last commit**: 2025-08-05 (v1.7.0)
- **Stars**: ~243
- **Transport**: stdio + SSE + Streamable HTTP
- **Tool surface**: 10 tools — `execute_sql`, `get_db_health_running`, `get_db_health_index_usage`, `get_table_desc`, `get_table_index`, `get_table_lock`, `get_table_name`, `optimize_sql`, `use_prompt_queryTableData`, `get_chinese_initials`
- **Self-hosted purity**: 1
- **Configurability**: 3 (`.env` with role assignment: readonly/writer/admin + optional OAuth 2.0 flag)
- **Footprint**: yellow — Python ~200 MB
- **Write capability**: gated via role (`readonly`/`writer`/`admin`)
- **Overlap with Aura native**: NONE — Aura has no MySQL surface.
- **Verdict**: SKIP for now — Aura has no MySQL workload on the roadmap. Better choice than `designcomputer/mysql_mcp_server` if MySQL ever lands (richer health/index/lock tools, three-tier RBAC).

---

## txn2/mcp-s3 (S3-compatible, works with Garage)
- **URL**: https://github.com/txn2/mcp-s3
- **Language / runtime**: Go (single static binary)
- **License**: Apache-2.0
- **Last commit**: active (142 commits, recent)
- **Stars**: ~3
- **Transport**: stdio
- **Tool surface**: 9 tools — `s3_list_buckets`, `s3_list_objects`, `s3_get_object`, `s3_get_object_metadata`, `s3_put_object`, `s3_delete_object`, `s3_copy_object`, `s3_presign_url`, `s3_list_connections`
- **Self-hosted purity**: 1 — explicitly supports custom endpoint via `S3_ENDPOINT` + `S3_USE_PATH_STYLE` ("Works with AWS S3, SeaweedFS, LocalStack, and any S3-compatible storage"); Garage qualifies
- **Configurability**: 3 (env vars + `S3_ADDITIONAL_CONNECTIONS` JSON for multi-connection — clean form mapping; presets per bucket trivial)
- **Footprint**: green — Go static binary ~20 MB, no runtime deps
- **Write capability**: read + write — **read-only by default** (`MCP_S3_EXT_READONLY`); PUT/DELETE/COPY gated. GET 10 MB / PUT 100 MB caps.
- **Overlap with Aura native**: PARTIAL — Aura has Garage but no native tool surface exposed to the agent for S3 ops. This fills the gap with a Go binary that matches Aura's runtime (no Python sidecar).
- **Verdict**: ADOPT — best-fit for Aura's existing Garage. Tiny footprint, same language, read-only default + presigned URL gen unlocks new flows (sharable artifact links, multi-bucket browsing). Low star-count is the only caveat — small surface area, easy to audit/fork.

---

## cbcoutinho/nextcloud-mcp-server (WebDAV bonus)
- **URL**: https://github.com/cbcoutinho/nextcloud-mcp-server
- **Language / runtime**: Python
- **License**: AGPL-3.0
- **Last commit**: 2026-04-07
- **Stars**: ~232
- **Transport**: Streamable HTTP (default) + stdio
- **Tool surface**: 110+ tools across 10 Nextcloud apps; **WebDAV/Files = 12 tools** (filesystem access, OCR, document processing) — also Notes, Calendar, Contacts, Tables, Talk, Deck, Cookbook, Sharing
- **Self-hosted purity**: 1 — runs against any self-hosted Nextcloud
- **Configurability**: 3 (env vars `NEXTCLOUD_HOST` / `NEXTCLOUD_USERNAME` / `NEXTCLOUD_PASSWORD` + Login Flow v2 multi-user)
- **Footprint**: yellow — Python ~250 MB
- **Write capability**: read + write
- **Overlap with Aura native**: NONE — Aura has no Nextcloud surface today. Useful only if the user runs Nextcloud.
- **Verdict**: CONDITIONAL — ADOPT only if user adopts a Nextcloud instance. AGPL-3.0 must be flagged: dynamic-linked as a separate process via MCP, so Aura itself is not relicense-tainted, but worth documenting.

---

## Top 3 picks (ranked for Aura's actual stack today)

1. **txn2/mcp-s3** — Go binary speaking pure S3 against Aura's existing Garage sidecar. Tiny footprint, runtime-language match, read-only default with capability gate for writes, presigned-URL generation = clean "share an artifact" flow. Zero overlap with current agent tool surface.
2. **motherduckdb/mcp-server-motherduck** — DuckDB unlocks an analytical SQL dimension Aura is missing entirely. Use cases: query the conversation archive, run aggregations across wiki sources, join scheduled-task history with budget usage. MIT, maintained by the upstream DuckDB cloud vendor, fully offline with `--db-path`.
3. **crystaldba/postgres-mcp** — banked as a "ready-to-plug" option the moment Postgres lands (pgvector workload, app introspection). 2.8k stars + index-tuning + health checks is best-in-class. No adoption cost today; document as "available on demand".

## What's missing

- **No mature MinIO/Garage-native MCP exists** other than `minio/mcp-server-aistor` (Python, MinIO-leaning, no license stated, 40 stars). `txn2/mcp-s3` was the only credible pure-S3-via-endpoint option found — and at 3 stars it's effectively a "fork-and-own" candidate.
- **No SQLite MCP worth adopting** — every candidate (`jparkerweb/mcp-sqlite`, `StacklokLabs/sqlite-mcp`, the archived official) duplicates what Aura already does directly in-process. SQLite-over-MCP would add a serialization tax with zero capability gain.
- **No "Parquet/CSV-as-table" MCP** independent of DuckDB — which is fine, DuckDB covers that case better than any standalone tool would.
- **No filesystem MCP adds enough above Aura's `workspace_*`** to justify adoption. The closest (`Digital-Defiance/mcp-filesystem`) has interesting batch-rollback + watcher semantics but at 0 stars is not battle-tested; cheaper to add `workspace_watch` + `workspace_batch` natively if those features are needed.
- **WebDAV-only MCPs (non-Nextcloud)** appear to not exist as standalone — every WebDAV tool is bundled inside a Nextcloud server. If a generic WebDAV target ever matters, it'll need to be written from scratch.
