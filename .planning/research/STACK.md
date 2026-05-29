# Stack Research — Aura Go-Native Agentic Substrate

**Domain:** Go single-binary agentic AI runtime + Docker Compose sidecars, mini-PC deployment (16-core / 32 GB / Linux preferred)
**Researched:** 2026-05-29
**Mode:** Project Research — Stack dimension (independent validation of PRD-locked choices)
**Overall confidence:** HIGH (most PRD choices verified current and idiomatic) with 3 specific drift flags requiring PRD-amendment consideration (Neo4j versioning, go-readability deprecation, Telegram-markdown supply-chain risk).

---

## Executive Summary

The PRD's stack is **fundamentally sound for 2026**. The PRD-first principle and validated spike-driven choices (Neo4j 2026-05-27 spike, OpenRouter provider audit, mini-PC CPU budget memory) have produced selections that match what current production Go agent systems pick when they want minimal-dep, mini-PC-friendly substrates. **Eight of the thirteen validated items pass without amendment.**

Three drift flags surface from independent verification, each with a clear PRD-amendment proposal below:

1. **Neo4j versioning drift (P1):** Neo4j moved to CalVer (`2025.01.0` through `2026.05.0`) in January 2025. "Neo4j 5.x" is now LTS-only (5.26.x) — patches but no features. The PRD's `neo4j:5-community` ships **5.26.x LTS** (frozen on Cypher 5, no Cypher 25, no 2025+ features). Recommend: **lock to `neo4j:5.26-community` explicitly** if LTS is desired (3-year support through Nov 2028), or **upgrade to `neo4j:2026.05-community`** to get current features (vector index improvements, Cypher 25). The PRD says "Neo4j 5.x" which is ambiguous — pin a tag.
2. **`go-shiori/go-readability` deprecated 2025-12-05 (P1):** README now redirects to **`codeberg.org/readeck/go-readability/v2`** (Readability.js v0.6 compatible). The PRD Slice 5 file targets cite the deprecated path. Migrate at Slice 5 implementation time.
3. **Telegram markdown library is a 4-star supply-chain risk (P2):** `eekstunt/telegramify-markdown-go` has 4 stars, 1 committer, all commits May 2026. Alternative `barbashov/telegramify-markdown-go` is even younger (0 stars, May 2026). The PRD already calls for "VERIFICARE pre-merge: licenza + active maintenance ... fallback a port custom ~80 LOC" — that contingency must actually fire. Recommend planning the ~80 LOC custom port as **default**, not fallback; vendor the ~5 escape rules from Telegram's MarkdownV2 spec.

Beyond these flags, the PRD is **opinionated, current, and minimal-dep** in exactly the way an agentic substrate needs. The anti-features section (no LangChain, no GORM, no off-the-shelf agent frameworks) matches 2026 community wisdom around Go agentic systems built directly on `net/http` + `database/sql` + first-party libraries.

## Recommended Stack

### Core Technologies

| Technology | Version | Purpose | Why Recommended |
|---|---|---|---|
| **Go** | **1.25.x stable (toolchain 1.25 mandatory minimum, 1.26 current)** | Language runtime | PRD says "Go 1.23+" for `iter.Seq2`. **Bump minimum to 1.25**: (1) AG-UI Go SDK declares `go 1.24.4` in `go.mod` — cannot use the PRD's stated SDK on Go 1.23; (2) 1.25 adds production-ready `synctest` for deterministic concurrency tests; (3) 1.25 adds GC arena pooling improvements relevant for long-lived agent processes. Latest Go stable is 1.26.3 (May 2026), but 1.25 is the safe-pick LTS-like floor. HIGH confidence (verified via golang/go tags + ag-ui/go.mod). |
| **PostgreSQL** | **17.x-alpine** (latest: 17.x stream; 18.4 GA but new) | Primary OLTP store | PRD pick correct. 17 is current GA stream (17.0 released Sept 2024, latest 17.x). 18.0 released Sept 2025 (REL_18_4 May 2026) — too new for production agent substrate; 17 has another ~3-4 years of community support. `postgres:17-alpine` ~80 MB image, ~250 MB RAM idle confirmed. HIGH confidence. |
| **jackc/pgx** | **v5.9.2** (latest May 2026) | Pure-Go PostgreSQL driver + pool | PRD pick correct and current. `v5.9.2` is the head of v5 line, actively maintained (Marek Tonneslan commits May 2026). **No v6 in alpha or beta** — confirmed via tag inspection of `jackc/pgx`. pgx remains the Go Postgres performance leader; `database/sql` + `lib/pq` is the *only* serious alternative and ~30% slower with worse type fidelity. HIGH confidence. |
| **sqlc** | **v1.31.1** (April 2026) + `version: "2"` config | SQL-first type-safe codegen | PRD pick correct. v2 config is current and stable; engine `postgresql` with `sql_package: "pgx/v5"`. Generates per-query Go funcs that map 1:1 to SQL files — natural fit for the PRD's "anti-god-class by design" intent. Released roughly quarterly; v1.31.1 includes pgx/v5 type-mapping fixes. HIGH confidence. |
| **golang-migrate/migrate** | **v4.19.1** (Nov 2025) | File-based versioned schema migrations | PRD pick correct. Battle-tested by Uber, Hashicorp, many. `embed.FS` source supported natively (no separate file-watcher). 4.19.1 includes pgx/v5 driver. Slow release cadence (~6-9 months) but stable and no rewrites pending. HIGH confidence. |
| **Neo4j** | **`neo4j:5.26-community` LTS** (recommended) OR `neo4j:2026.05-community` (current) | Knowledge graph + HNSW vector index | **PRD says "Neo4j 5.x" — ambiguous**. As of January 2025 Neo4j moved to CalVer (2025.01.0 → 2026.05.0 latest). "5.x" now means 5.26 LTS only (patches, no new features). Spike validated 2026-05-27 was on Aura's specific corpus and likely worked on 5.x; the **5.26 LTS** path keeps it stable through Nov 2028 with HNSW vector index + Cypher 5. The **2026.05 path** adds Cypher 25 + vector index quality-of-life improvements but rolling release model (each minor EOL when next ships). Community Edition support is informal in both lines. **Recommend pinning `neo4j:5.26-community`** for the agent substrate (stability > features); revisit if a 2026.x feature becomes load-bearing. MEDIUM confidence on choice between 5.26-LTS vs 2026.x; HIGH confidence that PRD's bare "5.x" string needs pinning. |
| **APOC + GDS plugins** | Match Neo4j minor | Cypher procedures + graph algorithms (Leiden, PageRank) | PRD pick correct. Use `NEO4J_PLUGINS='["apoc","graph-data-science"]'` (auto-install matching version). Mandatory for Slice 11c community-detection. HIGH confidence. |
| **mcp-neo4j-cypher** | **v0.6.0** (April 2026, Apache 2.0) | MCP server for Neo4j subprocess access | PRD pick correct and current. Actively maintained (commits April 2026, last release `cypher - prep v0.6.0` PR #283). Apache 2.0 license confirmed. 950 stars on the umbrella `neo4j-contrib/mcp-neo4j` monorepo. **Operational gotchas verified**: requires `pip install mcp-neo4j-cypher` on host PATH; subprocess stdio framing is FastMCP-based (locked `<3.x` per `cypher - lock fastmcp to <3.x #269`, Feb 2026); EXPLAIN-based read-only validation added April 2026. PRD's "fail-fast at boot if missing" is the right behavior. HIGH confidence on the choice; **MEDIUM confidence on long-term discipline** of using MCP-only with no Go-native fallback — see What NOT to Use below. |
| **OpenAI-compat wire layer (handrolled ~280 LOC)** | N/A — own code | LLM HTTP+SSE client targeting OpenRouter | PRD pick **strongly correct**. The official `openai/openai-go v3.37.0` (May 2026) is excellent but is 696 doc snippets, generated-from-OpenAPI, pulls cyclic dep deps, and ships the entire OpenAI surface (Realtime, Batch, Files, Vector Stores, Assistants, Threads, Runs, etc.) — wildly over-spec for an agent substrate that only needs `POST /v1/chat/completions` with streaming. Reference implementations confirm the pattern (`codex-rs` ~no-retry SSE, `picobot/internal/providers/openai.go` ~60s timeout, `nanobot/agent/runner.py` 300s default). PRD's `~280 LOC` estimate is realistic. HIGH confidence. |
| **EmbeddingGemma 300m** | Latest GGUF Q4_0 from `ggml-org`, served by `llama.cpp` server | 768d embeddings, OpenAI-compat `/v1/embeddings` | PRD pick correct. 768d native, MRL-trained (truncatable to 512/256/128 but PRD correctly skips MRL since Neo4j HNSW is configured to 768d). Memory `feedback_embedding_backend_stays_mistral` documents the spike — 22-30 ms p95, IT recall@5 5/5. EmbeddingGemma is Google's 300M open embedding from Gemma 3 family, works in llama.cpp via `--embedding` flag, exposes OpenAI `/v1/embeddings` endpoint. ~600 MB RAM, 4-thread CPU fits the mini-PC budget. HIGH confidence. |
| **llama.cpp server** | `ghcr.io/ggml-org/llama.cpp:server` rolling (e.g., `b9401` May 2026) | Embedding + multimodal LLM serving | PRD pick correct. Multi-purpose: embedding sidecar (Slice 0.7) + Gemma 4 multimodal (Slice 9c). OpenAI-compat APIs for chat (`/v1/chat/completions`), embeddings (`/v1/embeddings`), audio transcription (`/v1/audio/transcriptions`), with vision via `mmproj` projector files. Rolling release; pin specific `b<N>` tag in compose. HIGH confidence. |
| **AG-UI Go SDK community** | Latest from `sdks/community/go` (no semver release tags; pull at commit SHA) | Event protocol transport (SSE) | PRD pick correct. Repository is **very active** — daily/weekly releases through May 2026 (`release/2026-05-28`). Go SDK at `sdks/community/go` last touched 2026-05-14 (`feat(go-sdk): add interrupt, resume, and multimodal source types`). **Caveat**: no semver tags on the Go SDK specifically; consumers use pseudo-versions via `go get github.com/ag-ui-protocol/ag-ui/sdks/community/go@<sha>`. PRD's `latest` placeholder is acceptable but **commit-pin in `go.mod`** for reproducibility. Requires `go 1.24.4` per its own `go.mod`. HIGH confidence on choice; MEDIUM confidence on long-term API stability given pre-1.0 release scheme. |
| **`gopkg.in/telebot.v4`** | branch `v4` (no tagged release; commits May 2026 implement Bot API 9.4/9.5) | Telegram bot framework | PRD pick correct in choice, but **caveat**: v4 branch has **no semver release tag** (latest tag is v3.3.6 from June 2024). v4 is on `master` of `tucnak/telebot` and is actively developed (4618 stars, 62 open issues, commits 2026-05-17 fixing multipart filename, 2026-05-08 implementing Bot API 9.4/9.5). Use `gopkg.in/telebot.v4` as PRD specifies — Go modules handles the `gopkg.in` versioning. Alternative: `go-telegram/bot` (1722 stars, MIT, semver-tagged, also active May 2026) — more modern but smaller community. PRD pick is defensible; recommend a Slice 9b pre-merge gate that fetches a specific commit SHA. MEDIUM confidence (active but unreleased-as-tag). |
| **SearXNG container** | `searxng/searxng` rolling | Self-hosted meta-search for `web_search` | PRD pick correct. AGPL-3.0 license — **safe as a container** (network IPC, not linked code; no copyleft obligation on Aura). 30 828 stars, daily commits (updated 2026-05-29). Active community. HIGH confidence. |
| **html-to-markdown** | **`github.com/JohannesKaufmann/html-to-markdown/v2 v2.5.1`** (May 2026) | HTML → Markdown for `web_fetch` | PRD pick correct and current. v2 is the actively-developed line (185 doc snippets in Context7). v2.5.1 May 2026. Maintained, plugin-extensible. HIGH confidence. |
| **go-readability** | ⚠ **MIGRATE** to `codeberg.org/readeck/go-readability/v2` | Article extraction from HTML | **PRD pick is deprecated**: `github.com/go-shiori/go-readability` README (Dec 2025) reads: "*This package is deprecated in favor of codeberg.org/readeck/go-readability/v2*". The successor tracks Readability.js v0.6 (vs original at v0.5). Migration is straightforward API-wise (same `FromURL` / `FromReader` shape). PRD Slice 5 file targets must update the import path; the rationale (continuity + pre-rewrite usage) is invalidated by the upstream deprecation. **HIGH confidence — drift flag P1**. |
| **`github.com/skip2/go-qrcode`** | latest (no recent semver) | QR SVG generation for setup wizard | PRD pick acceptable. 2996 stars, MIT, last updated May 2026 (commit activity, not release) — repository is in maintenance mode (last tag `v0.0.0-20200617195104-da1b6568686e` from 2020). API stable; PNG/SVG generation is feature-complete. Alternative: `boombuler/barcode` covers QR + more. PRD pick is fine for the narrow QR-only need. MEDIUM confidence (stable but unreleased). |
| **`github.com/mdp/qrterminal/v3`** | latest | ASCII QR for console fallback | PRD pick correct. MIT, 535 stars, last updated May 2026. Tiny, single-purpose, widely used. HIGH confidence. |
| **Telegram Markdown lib** | ⚠ **Plan for custom port (~80 LOC), not third-party** | Markdown → Telegram MarkdownV2 escaping | **PRD's `eekstunt/telegramify-markdown-go` is a 4-star, 1-committer project with all commits May 2026** — supply-chain risk. The alternative `barbashov/telegramify-markdown-go` is 0 stars, also May 2026. Both are too new for production reliance. PRD already calls out the "fallback to custom port ~80 LOC" — recommend **promoting the custom port to default**. The MarkdownV2 escape rule set is small and stable (`_*[]()~\`>#+-=|{}.!`), goldmark already in the dependency tree for parsing if needed. **HIGH confidence on risk; MEDIUM confidence on the recommendation** (a 4-star lib may turn out fine, but the asymmetric risk favors the port). |
| **`go.uber.org/goleak`** | **v1.3.0** (Oct 2023) | Goroutine leak detection in tests | PRD pick correct. Stable for years; v1.3.0 is the latest; very low churn (mature API). Mandatory in `TestMain` per PRD Slice 1 acceptance row. HIGH confidence. |
| **`github.com/joho/godotenv`** | **v1.6.0-pre.4** (May 2026) | `.env` file loader | PRD pick correct. The 1.6.0 series is in pre-release but production-stable; v1.5.1 (Feb 2023) is the last fully-tagged release and still works for the load-order (`.env` → JSON file → env vars) PRD describes. Pure-Go, zero-dep. Alternatives (`spf13/viper`) are massively over-spec for a `.env` parser. HIGH confidence. |

### Supporting Libraries

| Library | Version | Purpose | When to Use |
|---|---|---|---|
| `github.com/google/uuid` | v1.6.0 | UUID v4/v7 generation | Conversation IDs, tool call IDs, run IDs (Slice 1.8, 8). Transitive via AG-UI SDK; PRD doesn't list but will need. |
| `github.com/stretchr/testify` | v1.10+ | Assertion DSL | Per CLAUDE.md skills, `golang-stretchr-testify` skill installed. Adopt if test brevity > stdlib `testing` orthogonality; otherwise skip. PRD-neutral. |
| `golang.org/x/sync/errgroup` | latest | Concurrent task synchronization | Slice 0.9 `ParallelAgent` workflow agent (per PRD pattern). Critical for swarm coordinator. |
| `github.com/google/go-cmp` | v0.6+ | Deep-equal test diffs | Golden fixture comparisons (SSE response fixtures Slice 1, AG-UI event sequences Slice 8). Drop-in for `reflect.DeepEqual`. |
| `github.com/spf13/cobra` | v1.10+ | CLI subcommand framework | CLAUDE.md skill `golang-spf13-cobra` installed. Defer until subcommand surface exceeds ~6 commands. Slice 0.5 starts with stdlib `flag` (per repo skeleton); migrate to Cobra around Slice 6 when `aura {db,neo4j,llm-router,task,telegram,...} {sub}` becomes unwieldy. |
| `github.com/coreos/go-systemd/v22/journal` | optional | systemd journal logging on Linux | Only if running as systemd service; otherwise stdlib `slog` to stdout + Docker log driver. |
| `golang.org/x/time/rate` | latest | Token-bucket rate limiting | Slice 9b Telegram chat queue (1/sec per chat_id), Slice 5 web fetch politeness. Stdlib equivalent doesn't exist; `x/time` is semi-official. |

### Development Tools

| Tool | Purpose | Notes |
|---|---|---|
| `sqlc` v1.31.1 binary | SQL → Go codegen | Install via `go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1`. CI golden-test: `make sqlc && git diff --exit-code` (PRD Slice 0.5 acceptance row already requires this). |
| `golangci-lint` v1.62+ | Aggregate linter (errcheck, staticcheck, govet, gosimple, ineffassign, ...) | Memory `feedback_golangci_lint_catches_what_audits_miss`: 2 rounds of audit agents = 0 dead-code, linter found 20. Run pre-merge mandatory. |
| `go-mod-outdated` | Dependency staleness reporter | `go list -u -m -json all \| go-mod-outdated -update -direct` weekly. |
| `mockery` v2.x or stdlib mocks | Interface mocking | Defer until needed; CLAUDE.md prefers manual `Stub` impls in `_test.go` for small interfaces. |
| `go-fuzz` (deprecated) → native `go test -fuzz` | Property-based fuzzing | Native fuzzing built into `go test` since 1.18. PRD §Test discipline rigorosa requires property-based dove indicato. |
| `mewt` or `muton` | Mutation testing | PRD Gate 3 DoD: ≥70% killed. CLAUDE.md `mutation-testing` skill installed. Pick at Gate 3 implementation time. |
| `delve` (`dlv`) v1.23+ | Go debugger | CLAUDE.md `golang-troubleshooting` skill. Mandatory for race conditions / agent loop debugging. |
| `pprof` (stdlib `net/http/pprof`) | Profiling | Gate behind `AURA_PPROF_ENABLED=1` env (default off). |
| Docker Compose v2.x | Sidecar orchestration | PRD `compose.yaml` (formerly `sandbox/compose.yaml`). |
| `mcp-neo4j-cypher` v0.6.0 | Neo4j MCP server | `pip install mcp-neo4j-cypher==0.6.0` mandatory on host PATH. Subprocess stdio. FastMCP `<3.x` per upstream lock. |
| `mcp-inspector` (Anthropic) | MCP server testing | One-shot validation tool; not a runtime dep. |

## Installation

```bash
# === Go runtime ===
# Install Go 1.25.x (or 1.26.x) — 1.23 MINIMUM is INSUFFICIENT for AG-UI SDK (requires 1.24.4)

# === Go modules (initial bootstrap commits at Slice 0.5, accrete by slice) ===
# Slice 0.5 (Postgres infra)
go get github.com/jackc/pgx/v5@v5.9.2
go get github.com/jackc/pgx/v5/pgxpool@v5.9.2
go get github.com/golang-migrate/migrate/v4@v4.19.1
go get github.com/golang-migrate/migrate/v4/database/pgx/v5@v4.19.1
go get github.com/joho/godotenv@v1.6.0-pre.4

# Slice 0.7 (Neo4j infra) — pure subprocess stdio MCP; no Go driver
# (only ensure mcp-neo4j-cypher on PATH)

# Slice 1 (LLM client) — no external SDK; uses net/http stdlib
go get github.com/google/uuid@v1.6.0

# Slice 5 (Web tools)
go get github.com/JohannesKaufmann/html-to-markdown/v2@v2.5.1
# WARNING: PRD's go-shiori/go-readability is DEPRECATED.
# Use the successor:
# Module path is on codeberg.org — install via:
#   go get codeberg.org/readeck/go-readability/v2@latest
# OR keep deprecated path with explicit acknowledgment in commit message + tracking issue.

# Slice 8 (AG-UI gateway) — REQUIRES Go 1.24.4+
go get github.com/ag-ui-protocol/ag-ui/sdks/community/go@<commit-sha-from-2026-05-14-or-later>

# Slice 9a (Setup wizard)
go get github.com/skip2/go-qrcode@latest
go get github.com/mdp/qrterminal/v3@latest

# Slice 9b (Telegram) — telebot.v4 has no tagged release; pin SHA
go get gopkg.in/telebot.v4@<commit-sha-post-2026-05-08>
# Telegram MarkdownV2: recommend custom port (~80 LOC) instead of:
#   eekstunt/telegramify-markdown-go (4 stars, 1 committer, all May 2026)
#   barbashov/telegramify-markdown-go (0 stars, all May 2026)

# Slice 9c (Multimodal) — pure HTTP to llama.cpp server; no Go client

# === Testing & tooling (Go install, not module deps) ===
go install go.uber.org/goleak@v1.3.0  # NOT a tool, module dep — included transitively in test files
go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
go install github.com/golang-migrate/migrate/v4/cmd/migrate@v4.19.1

# === Python sidecar deps (host) ===
pip install mcp-neo4j-cypher==0.6.0  # required on PATH for Slice 0.7

# === Docker images (pinned in compose.yaml) ===
# postgres:17-alpine
# neo4j:5.26-community  (RECOMMENDED) or neo4j:2026.05-community  (alternative)
# searxng/searxng:latest  (rolling)
# ghcr.io/ggml-org/llama.cpp:server-b9401  (pin specific build, not :server tag)
# Custom: sandbox/Dockerfile (python:3.12-slim base)
# Slice 13 (deferred to v2): vllm/vllm-openai:latest  (GPU required for sanity)
```

## Alternatives Considered

| Recommended | Alternative | When to Use Alternative |
|---|---|---|
| Hand-rolled OpenAI-compat ~280 LOC | `openai/openai-go v3.37.0` (official SDK) | **When** you need Assistants API, Realtime API, Vector Stores, Batch API, or detailed type-fidelity on edge OpenAI features. Aura needs none of these (single endpoint, single shape). The SDK pulls ~70 internal packages and a generated-from-OpenAPI surface — wrong shape for a substrate that wants ~280 LOC of total wire code. **Reject for Aura.** |
| Hand-rolled OpenAI-compat ~280 LOC | `sashabaranov/go-openai` v1.40+ | **When** you want a lightweight community SDK with maintained tool-call types but acceptable extra deps. Defensible alternative to hand-rolling if the SSE parser becomes a maintenance burden — but at 87 doc snippets it brings its own opinion on streaming shape that may not match Aura's `ToolResult` pattern. **Marginal; keep PRD choice.** |
| `jackc/pgx/v5` | `database/sql` + `lib/pq` | **Never for new code.** lib/pq is in maintenance-only mode (no v2 planned), ~30% slower for large result sets, no native pgx-style `COPY` protocol, weak type fidelity for `jsonb`/`tstzrange`/arrays. pgx supersedes it. |
| `jackc/pgx/v5` | `entgo.io/ent` ORM, `gorm.io/gorm` | **When** you want active-record-style ORM. Aura's "SQL-first via sqlc, no ORM" is the **correct** posture for a substrate: ORMs hide query cost, encourage N+1, and add a layer between schema and Go that fights migrations. sqlc gives you compile-time-checked queries without an abstraction layer. **Reject for Aura.** |
| `golang-migrate/migrate/v4` | `pressly/goose`, `rubenv/sql-migrate`, sqlc's `migrate` (experimental) | **When** you need Go-function migrations (not pure SQL files). Aura's all-SQL migrations make golang-migrate the right pick — embed.FS source + idempotent CLI. Goose is a defensible alternative (slightly more flexible front-matter) but no clear win. sqlc's migrate is not production-grade per PRD Slice 0.5 OQ 2. |
| `neo4j:5.26-community` LTS | Apache Jena Fuseki, JanusGraph, ArangoDB Community, **TigerGraph Community** | **When** you have an open-source-only or Java-stack requirement. Neo4j Community is open-source (GPL v3), the spike validated structured Cypher retrieval at 15-45× over blob+LLM (memory `project_neo4j_spike_2026-05-27`). Cypher tooling + APOC + GDS + native vector index is unmatched in the open-source graph space. Migration would invalidate the spike. **Reject.** |
| `mcp-neo4j-cypher` (subprocess) | `github.com/neo4j/neo4j-go-driver/v5` (native Go bolt driver) | **When** you need <5ms call latency or fine-grained transaction control. The PRD discipline ("tutto accesso Neo4j passa da MCP") is correct for the substrate goal — uniform LLM interface — but the **MCP subprocess adds ~2-10ms per Cypher call** vs a native bolt connection pool (process startup amortized once, then stdin/stdout JSON-RPC overhead per request). At Aura's read volume (LLM-paced, ~1 Hz max), this is invisible; at Slice 11d GraphRAG retrieval volume (multiple Cypher per user turn), revisit. **Keep PRD pick; document fallback escape hatch.** |
| `telebot.v4` (untagged) | `go-telegram/bot` (1722 stars, MIT, semver-tagged, May 2026 active) | **When** you want semver releases for reproducibility. `go-telegram/bot` is a defensible alternative; smaller community (1.7k vs 4.6k stars) but actively maintained, cleaner API surface for handlers. Switch cost is ~1 day at Slice 9b implementation; bring up as Slice 9b OQ if Telebot's untagged-master-branch model becomes a blocker (e.g., `go.sum` reproducibility across team). |
| `eekstunt/telegramify-markdown-go` (4 stars) | **Custom ~80 LOC port** of MarkdownV2 escape rules | **Recommend make this DEFAULT, not fallback.** The escape set is ~17 chars (`_*[]()~\`>#+-=|{}.!`) and entity rules are stable. Vendor with full test coverage; no supply-chain risk; no dependency. PRD already plans this as fallback — promote it. |
| llama.cpp CPU multimodal | `Ollama` server | **When** you want a single-binary install + model registry over llama.cpp. Ollama wraps llama.cpp + adds a model library + simpler API. Aura already runs containers, so llama.cpp's container is fine; Ollama would add a layer for no substrate benefit. PRD-aligned. |
| vLLM + LMCache (Slice 13) | llama.cpp CPU-only chat fallback (PRD's "13-bis" path) | **If mini-PC has no GPU**, vLLM in CPU mode is 5-10× slower than llama.cpp CPU (per PRD §Slice 13 ⚠ Open question + corroborated by 2026 benchmarks: vLLM is GPU-first design, CPU mode "is your only choice [llama.cpp] for CPU-only single-user"). **Strongly favor 13-bis** unless DGX Spark bundle is materialized first. LMCache is GPU/disk-tiered KV cache and offers ~3-10× TTFT reduction *on GPU* — value on CPU is marginal because llama.cpp already has internal KV reuse. |
| SearXNG self-hosted | Brave Search API, Tavily, Exa, DuckDuckGo Lite | **When** you want zero-container cost, accept paid query budget, accept third-party privacy implications. Aura's mini-PC + privacy posture make SearXNG the right pick. PRD Slice 5 OQ 1 already settled this. |
| Telegram (Slice 9b primary) | WhatsApp Business API, Discord, Signal | **When** target user uses a different platform. PRD lists these as future per-slice add-ons under `internal/channels/<name>/`. Telegram chosen because (a) target user uses daily, (b) bot API is open & free, (c) HITL via inline keyboards is well-supported. |

## What NOT to Use

| Avoid | Why | Use Instead |
|---|---|---|
| **LangChain Go** (`tmc/langchaingo`) | Pulls ~30 transitive deps, opaque "chain" control flow hides where prompts and retries happen, prompts-as-data instead of code (breaks the deferred-tool pattern), versioning lags upstream Python by months. Substrate goal is the *opposite* — explicit `Agent` interface, every tool call traceable, every dep auditable. | First-party stdlib + ~280 LOC OpenAI-compat client + Slice 0.9 `Agent` interface. |
| **GORM**, **Ent**, any Go ORM | Hides query cost (N+1 traps), fights schema migrations (struct tags vs `.sql` truth), runtime reflection overhead, makes `sqlc`'s anti-god-class promise impossible. | sqlc + golang-migrate + pgxpool. |
| Generic agent frameworks (`google/adk-go` package imported, `langchain-go`, `eino`, `swarmgo`) | adk-go specifically: 35 transitive deps (GCP, OTel, Gemini SDKs) — PRD Slice 0.9 already correctly rejects with "stolen-not-imported" pattern. Other agent frameworks bring more deps + their own opinions on `Agent` shape. | Re-implement the ~380 LOC of the `Agent` interface + workflow agents inline (PRD Slice 0.9). |
| `github.com/go-shiori/go-readability` | **Deprecated 2025-12-05**. Upstream README points to successor. Continuing the old path = security fixes don't backport, breaking-change `html` package upgrades won't land. | `codeberg.org/readeck/go-readability/v2`. |
| `lib/pq` for new Postgres code | Maintenance-only since 2017, slower, weaker type fidelity (no native jsonb/tstzrange/arrays), no native COPY protocol. | `jackc/pgx/v5` (already PRD). |
| `bytedance/sonic` or non-stdlib JSON | The agent loop is not JSON-bound (LLM API parsing is one call/sec scale). Adds CGO + arch-specific code paths for zero realistic gain in this codebase. | `encoding/json` stdlib. |
| `spf13/viper` for config | 50+ deps, file format auto-detection, "remote config" features Aura doesn't need. `godotenv` + JSON file + `os.Getenv` (PRD load order) is ~40 LOC vs Viper's giant surface. | `joho/godotenv` + custom JSON file unmarshal (PRD plan). |
| `logrus`, `zap`, `zerolog` for new logging | Stdlib `log/slog` (Go 1.21+) is now the standard; structured, hierarchical, no extra dep, supports all attribute types Aura needs. Logrus is in maintenance mode (last release 2023). | `log/slog` stdlib with `slog.HandlerOptions`. |
| WhisperX / Whisper.cpp container | PRD Slice 9c **explicitly removed** Whisper sidecar in favor of Gemma 4 unified audio (-300 MB RAM, 1 fewer sidecar). | Gemma 4 E4B mmproj via llama.cpp server. |
| Qdrant / Milvus / Weaviate as standalone | Memory `feedback_embedding_backend_stays_mistral` deprecated Qdrant after spike — Neo4j HNSW Lucene index validated at 22-30 ms p95 + recall@5 5/5 on Aura corpus. Adding another store = backup pain, sync drift, RAM cost. | Neo4j 5.26 vector index (PRD pick). |
| Python wiki/markdown filesystem store | Memory `project_graph_memory_core_strategy` deprecated wiki .md + `[[wiki-links]]` after Neo4j spike validated structured graph 1.6-1.8s vs blob+LLM 27-75s (15-45×). | Neo4j knowledge graph via mcp-neo4j-cypher. |
| `mholt/archiver`, `klauspost/compress` for tarball ops | Stdlib `archive/tar` + `compress/gzip` cover the Postgres `pg_dump` + Neo4j `neo4j-admin database dump` backup needs. | Stdlib. |

## Stack Patterns by Variant

**If mini-PC has no GPU (current default, until DGX Spark bundle):**
- Skip Slice 13 entirely; use PRD's "13-bis" path (reuse `aura-llama-multimodal` Gemma 4 for chat fallback).
- vLLM is **wrong tool**: GPU-first design, CPU mode 5-10× slower than llama.cpp CPU per 2026 community benchmarks.
- Embedding stays on CPU (memory `feedback_gpu_not_for_embedding_workload`: CUDA = 2000ms/query vs CPU = 32ms because Aura is latency-bound single-query, not throughput-bound).

**If mini-PC gains GPU (DGX Spark or RTX 4070+):**
- Slice 13 unlocks: vLLM serving Gemma 3 12B Q5 + LMCache disk-tier KV cache (3-10× TTFT reduction on long context per LMCache project measurements).
- Keep llama.cpp multimodal sidecar in parallel (vision + STT still better on llama.cpp's CUDA path for one-shot calls; doppio-sidecar PRD pattern).
- Add `aura-vllm-chat` service in compose with `--gpu-memory-utilization 0.85`.

**If deploying to ARM mini-PC (Apple Silicon or NVIDIA Grace):**
- llama.cpp has mature ARM NEON paths; embedding sidecar performance unaffected.
- pgx, sqlc, Neo4j Community ARM64 images all available.
- AG-UI SDK is pure Go, ARM-clean.
- Docker Desktop Apple Silicon: confirmed working; Linux/ARM64 native preferred.

**If multi-user (post-v1):**
- Slice 1.7 identities + capability_grants already scaffolds.
- Add bearer token auth at AG-UI endpoint (PRD Slice 8 "out of scope explicit — future slice").
- Postgres `aura.*` schema is multi-tenant-ready (1 db, N schema per tenant if scale demands).

**If running entirely offline (privacy mode):**
- Local LLM (Slice 13 once GPU available, or 13-bis CPU fallback).
- SearXNG hits Google/DuckDuckGo upstream — replace with local-only `aura-llm-router` flag to disable `web_search`.
- mcp-neo4j-cypher subprocess fully local.
- Postgres + Neo4j fully local.
- Telegram channel breaks (Telegram is cloud-only). CLI channel + future local web SPA over AG-UI remain.

## Version Compatibility

| Package A | Compatible With | Notes |
|---|---|---|
| `jackc/pgx/v5` v5.9.2 | `golang-migrate/migrate/v4` v4.19.1 driver `pgx/v5` | golang-migrate ships `database/pgx/v5` driver since v4.17. **Earlier migrate versions used `pgx/v4` driver** — verify import path is `github.com/golang-migrate/migrate/v4/database/pgx/v5` not `/pgx`. |
| `sqlc` v1.31.1 | `pgx/v5` codegen | `sqlc.yaml` v2 `sql_package: "pgx/v5"` (NOT `pgx` which targets v4, NOT `database/sql` which generates `*sql.DB`-shaped code). Mismatched setting produces compiling-but-broken code. |
| `neo4j:5.26-community` | `mcp-neo4j-cypher` v0.6.0 | v0.6.0 supports Neo4j 5.x bolt protocol. Verified via FastMCP `<3.x` constraint + neo4j-python-driver compatibility. Untested against `neo4j:2026.x` images — if upgrading Neo4j past 5.26, run MCP smoke first. |
| `neo4j:5.26-community` | APOC + GDS plugins | Plugin minor must match Neo4j minor. `NEO4J_PLUGINS=["apoc","graph-data-science"]` auto-resolves matching version from Neo4j plugin registry. |
| AG-UI Go SDK | Go ≥ 1.24.4 | Declared in `sdks/community/go/go.mod`. **PRD's "Go 1.23+" is insufficient.** |
| `telebot.v4` (untagged master) | Telegram Bot API 9.4/9.5 | Commits 2026-05-08 implement Bot API 9.4/9.5. Earlier `v3.3.6` tag (June 2024) only supports up to Bot API 7. Use v4 branch for current features. |
| `embeddinggemma-300m` GGUF | llama.cpp build `b9400+` | EmbeddingGemma support landed in llama.cpp around late 2025; pin a recent build (`ghcr.io/ggml-org/llama.cpp:server-b9401` or later). Earlier builds may have incorrect embedding output (see ggml-org/llama.cpp #19040 accuracy issue). |
| Gemma 4 mmproj | llama.cpp `server` build with vision | Vision requires `--mmproj` projector file matching Gemma 4 variant. Pin build that ships vision support. |
| `golang-migrate/migrate/v4` | `embed.FS` source | Use `iofs.New(migrationsFS, "migrations")` for `embed.FS` migration source. Requires `migrate/v4/source/iofs`. |
| LMCache (Slice 13 only) | vLLM v1+ | LMCache `--kv-transfer-config '{"kv_connector":"LMCacheConnectorV1","kv_role":"kv_both"}'` requires vLLM v0.6.0+ (LMCache v0.4.5 in May 2026 targets vLLM v0.7-0.10). Pin compatible matrix at Slice 13 time. |

## Drift Flags Summary (for roadmap consumer)

| # | Drift | Severity | PRD location | Recommendation |
|---|---|---|---|---|
| 1 | Neo4j 5.x is ambiguous post-CalVer | P1 | Slice 0.7 stack table; compose.yaml `neo4j:5-community` | Pin `neo4j:5.26-community` (LTS, supported through Nov 2028) explicitly; or commit to `neo4j:2026.05-community` (current, rolling EOL). PRD-amendment recommended. |
| 2 | `go-shiori/go-readability` deprecated 2025-12-05 | P1 | Slice 5 file targets + commit template | Migrate to `codeberg.org/readeck/go-readability/v2`. PRD-amendment at Slice 5 DoR. |
| 3 | `eekstunt/telegramify-markdown-go` 4-star supply-chain risk | P2 | Slice 9b dipendenze Go nuove | Promote PRD's "fallback custom port" to default; vendor ~80 LOC MarkdownV2 escaper. |
| 4 | Go 1.23 minimum is insufficient for AG-UI SDK (1.24.4) | P1 | Slice 0.9 Pre-requisiti + CLAUDE.md Tech stack | Bump minimum to Go 1.25.x (or 1.26.x current). PRD-amendment in CLAUDE.md + Slice 0.9. |
| 5 | `telebot.v4` has no semver tag — pseudo-version dep | P3 | Slice 9b dipendenze Go nuove | Pin specific commit SHA in `go.mod`; document in Slice 9b commit message. |
| 6 | AG-UI Go SDK has no semver tag — pseudo-version dep | P3 | Slice 8 dipendenza go.mod | Pin specific commit SHA in `go.mod`; document in Slice 8 commit message. |

## Confidence Assessment

| Area | Confidence | Verification source |
|---|---|---|
| Core data stack (Postgres 17 / pgx v5 / sqlc / migrate) | HIGH | GitHub release inspection May 2026 + Context7 + PRD spike alignment. |
| Neo4j choice | HIGH on engine, MEDIUM on version pin | endoflife.date + Neo4j official changelog (CalVer). |
| MCP discipline | HIGH on choice, MEDIUM on long-term ops | mcp-neo4j repo commits + FastMCP version lock + PRD spike. |
| OpenAI-compat handrolled | HIGH | PRD has 5/5 reference impls in `D:/tmp/` already cited. |
| AG-UI SDK | HIGH on protocol, MEDIUM on Go SDK API stability (pre-1.0) | go.mod inspection + commit activity. |
| Telegram lib | MEDIUM (active but untagged) | Repo commit log + Telegram Bot API version coverage. |
| Web tools | HIGH except go-readability (deprecated → HIGH-flagged) | README inspection of upstream. |
| Embedding stack | HIGH | Google EmbeddingGemma docs + memory `feedback_embedding_backend_stays_mistral` spike. |
| llama.cpp multimodal | HIGH | Active daily builds + Gemma 4 mmproj support. |
| vLLM + LMCache (Slice 13) | HIGH on GPU path, HIGH on "CPU is wrong tool" finding | Red Hat 2025-09-30 article + multiple 2026 benchmarks. |
| Test discipline | HIGH | Stdlib `testing` + native fuzz (1.18+) + goleak v1.3.0 mature. |
| Config / env | HIGH | godotenv API stable since v1.5.1 (2023). |

## Sources

### Context7 lookups
- `/jackc/pgx` + `/websites/pkg_go_dev_github_com_jackc_pgx_v5` — confirmed v5.9.2 current, no v6.
- `/websites/sqlc_dev_en` + `/websites/sqlc_dev` — sqlc v1.31.1 + v2 config current.
- `/tucnak/telebot` + `/go-telebot/telebot` — telebot v4 branch active, no semver.
- `/ag-ui-protocol/ag-ui` — Go SDK alive, `go 1.24.4` required.
- `/websites/neo4j` + `/websites/neo4j_apoc_current` + `/websites/neo4j_cypher-manual_current` — Neo4j CalVer + Cypher 25 era confirmed.
- `/websites/vllm_ai_en_stable` + `/vllm-project/vllm` — vLLM v0.14.0rc2 current; GPU-first.
- `/openai/openai-go` — v3.37.0 official Go SDK current; rejected as too heavy.
- `/johanneskaufmann/html-to-markdown` — v2 line current.

### GitHub repository inspection (gh CLI, 2026-05-29)
- `jackc/pgx` tags v5.9.2 latest; commits 2026-05-16 active; no v6 tags exist.
- `sqlc-dev/sqlc` release v1.31.1 (2026-04-22) latest.
- `ag-ui-protocol/ag-ui` daily releases through 2026-05-28; Go SDK go.mod requires `go 1.24.4`; Go-specific commits 2026-05-14, 2026-03-06, 2026-02-26.
- `tucnak/telebot` no v4 tag (latest tag v3.3.6 from 2024-06-10); v4 branch exists with commits 2026-05-17 implementing Bot API 9.4/9.5; 4618 stars; LICENSE NOASSERTION.
- `go-shiori/go-readability` README contains deprecation notice (2025-12-05): "*This package is deprecated in favor of codeberg.org/readeck/go-readability/v2*".
- `neo4j-contrib/mcp-neo4j` 950 stars, MIT, last updated 2026-05-29; mcp-neo4j-cypher v0.6.0 (PR #283 April 2026); fastmcp pinned `<3.x`.
- `neo4j/neo4j` tag inspection: `2026.05.0`, `2026.04.0`, ..., `2026.01.4`, `2025.12.1`, ..., `5.26.26` (LTS line current patch). Confirms CalVer shift.
- `vllm-project/vllm` v0.21.0 latest (2026-05-15); nightly builds CUDA 12.9 / 13.0.
- `LMCache/LMCache` v0.4.5 latest (2026-05-15); GPU-targeted; KV cache extension for vLLM.
- `LMCache/LMCache` README: 3-10x TTFT reduction in long-context vLLM scenarios.
- `JohannesKaufmann/html-to-markdown` v2.5.1 (2026-05-07) latest.
- `searxng/searxng` 30828 stars, AGPL-3.0, active 2026-05-29.
- `eekstunt/telegramify-markdown-go` 4 stars, MIT, all commits 2026-03 to 2026-05.
- `barbashov/telegramify-markdown-go` 0 stars, no license, commits 2026-05-23.
- `skip2/go-qrcode` 2996 stars, MIT, last release 2020 (maintenance mode), API stable.
- `mdp/qrterminal` 535 stars, MIT, updated 2026-05-22.
- `joho/godotenv` v1.6.0-pre.4 (2026-05-25) current; v1.5.1 last full release Feb 2023.
- `golang-migrate/migrate` v4.19.1 (2025-11-29).
- `golang/go` tags: latest stable go1.26.3, go1.25.x maintenance, go1.23/go1.24 older.
- `google/adk-go` v1.3.0 (2026-05-19) confirmed; PRD's "stolen not imported" rationale (35 GCP deps) valid.
- `uber-go/goleak` v1.3.0 (2023-10-24), API mature.

### Web research with publication dates
- [Neo4j CalVer announcement & Cypher 25](https://feedback.neo4j.com/changelog/important-update-calendar-versioning-cypher-25) — 2025 changelog.
- [Neo4j supported versions](https://neo4j.com/developer/kb/neo4j-supported-versions/) — confirms 5.26 LTS through Nov 2028, CalVer starting 2025.01.
- [endoflife.date Neo4j](https://endoflife.date/neo4j) — version EOL matrix.
- [vLLM vs llama.cpp Red Hat Developer 2025-09-30](https://developers.redhat.com/articles/2025/09/30/vllm-or-llamacpp-choosing-right-llm-inference-engine-your-use-case) — vLLM GPU-first; llama.cpp wins CPU + single-user.
- [vLLM vs llama.cpp Markaicode 2026](https://markaicode.com/vs/vllm-vs-llamacpp/) — confirms CPU benchmark gap.
- [Ollama vs vLLM vs llama.cpp benchmarks 2026 TechPlained](https://www.techplained.com/ollama-vs-vllm-vs-llamacpp) — single-user throughput numbers.
- [Best LLM Inference Engines 2026 DeployBase](https://deploybase.ai/articles/best-llm-inference-engine) — engine comparison matrix.
- [Google EmbeddingGemma announcement](https://developers.googleblog.com/en/introducing-embeddinggemma/) — 768d native, MRL training.
- [EmbeddingGemma Hugging Face card](https://huggingface.co/google/embeddinggemma-300m) — 300M params, multilingual.

### PRD-internal evidence already cited
- `prd.md` Slice 0.5 stack table — Postgres 17/pgx/sqlc/migrate validated by search 2026.
- `prd.md` Slice 0.7 stack table — Neo4j spike `D:/tmp/aura-neo4j-spike-2026-05-27/` 22-30 ms p95 + recall@5 5/5.
- `prd.md` Slice 1 OQ closures — OpenRouter+DeepSeek-V4 verified, timeout pattern verified across 5 production impls.
- Memory `feedback_embedding_backend_stays_mistral` — Neo4j HNSW Lucene validated, Qdrant deprecated.
- Memory `feedback_gpu_not_for_embedding_workload` — CPU=32ms vs CUDA=2000ms for single-query embedding.
- Memory `feedback_minipc_cpu_budget` — embed sidecar ≤4 threads, no busy-loop.
- Memory `reference_openrouter_provider_capabilities_2026-05-27` — DeepSeek V4 Flash 80% cache, −63% cost.

---
*Stack research for: Go-native agentic AI substrate (Aura)*
*Researched: 2026-05-29*
*Compatibility verified against PRD `b3faacbf` (2026-05-27 lock) + codebase `af4ca65c` (633-LOC skeleton).*
