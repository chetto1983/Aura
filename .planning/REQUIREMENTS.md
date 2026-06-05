# Requirements: Aura

**Defined:** 2026-05-29
**Core Value:** Substrate agentico domain-neutral — un runtime Go che esegue un agentic loop multi-tool affidabile con identity, channels, skills e memory come overlay configurabili. Se tutto il resto fallisce, ciò che deve funzionare è il loop che riceve un task, sceglie tool, esegue, persiste stato, ritorna risposta — su qualsiasi canale, per qualsiasi identità, con qualsiasi memoria collegata.

> **Source of truth:** [prd.md](../prd.md) (4400 LOC, locked `b3faacbf` 2026-05-27, 14 slice).
> Research synthesis: [.planning/research/SUMMARY.md](research/SUMMARY.md) (validated 2026-05-29 from 4 parallel research streams).
> REQ-ID mapping = slice number prefix (es. INFRA-01 → Slice 0.5; UX-04 → Slice 11). One requirement per slice or sub-slice atomic unit.

## v1 Requirements

### PRD Pre-Implementation (P0 — no code, doc only)

- [x] **PRD-01**: 20 PRD amendments applied in single commit `prd: pre-implementation drift fixes from independent research convergence` before any Slice 0.5 code commit. Covers: Go 1.25+ bump (AG-UI SDK requirement), Neo4j 5.26 LTS pin, go-readability migration to readeck fork, MarkdownV2 custom escaper as default, AG-UI/telebot SHA pins, conversation FTS sub-slice 1.8.5, /cost commands, OTel hooks, setup wizard token, :AgentInsight retrieval caching spec, Slice 3 swarm scope-reduction (ParallelAgent + 2-deep cap), Slice 7e split into 7e-core (v1) + 7f (v1.x), skill.catalog opt-in, goleak mandate extension, cross-slice cache_invariant_audit.sh CI job, audit role separation (aura_app vs aura_migrate) + TRUNCATE trigger, embedding-dim env contract + boot assertion, agent loop budget contract (3 env vars + child inheritance), docs/aura-quality-snapshot.md living doc + CI gate. [SUMMARY.md PRD Amendments table]

### Infrastructure (Slice 0.5–0.9)

- [x] **INFRA-01**: Postgres 17 + sqlc + pgx + golang-migrate operativo con schema `aura.*` e migrazioni versionate. Include role separation `aura_app` (INSERT/SELECT only on audit tables) vs `aura_migrate` (DDL gated). Boot fail-fast su DB unreachable. Nightly restore drill < 90s. [Slice 0.5 + amendment #17]
- [x] **INFRA-02**: Neo4j 5.26-community + APOC + GDS + HNSW vector index 768d cosine + `mcp-neo4j-cypher` MCP server (subprocess stdio, fail-fast su missing PATH). Embedding dim env contract `AURA_EMBED_DIMENSIONS=768` con boot assertion sidecar. Spike recall@5 5/5 mantained as smoke. [Slice 0.7 + amendments #2, #18]
- [x] **INFRA-03**: Open `Agent` interface + exported workflow agents (Sequential/Loop/Parallel) adapted from `google/adk-go` with Apache-2.0 attribution (~420 LOC). InvocationContext includes `request_id` UUIDv7 for OTel TraceID/run correlation and 8-byte crypto/rand SpanID/ParentSpanID for OTel-compatible span shape. Budget contract includes `AURA_LOOP_MAX_STEPS=25`, `AURA_LOOP_MAX_WALLCLOCK_SEC=300`, `AURA_LOOP_DEDUP_WINDOW=3`, dedup exempt/result-cap vars, passive branch soft-cap, and child budget inheritance from parent's remaining (NON fresh). Zero goroutine leak via `goleak.VerifyNone` mandatory. [Slice 0.9 + amendments #1, #9, #15, #19]

### Core Agent (Slice 1–1.8)

- [x] **CORE-01**: LLM client OpenAI-compat handrolled ~280 LOC (no SDK), DeepSeek-V4 via OpenRouter default (`deepseek/deepseek-v4-flash:exacto`), ToolResult pattern con preview+sidecar, SSE streaming + ctx-cancel end-to-end, OTel span per LLM call. [Slice 1 + amendment #9]
- [x] **CORE-02**: `ask_user` tool con pause/resume FIFO multi-pause, persistent `paused_states` in Postgres, sentinel error pattern (`ErrAwaitingUserInput`). Propaga attraverso swarm nested children con `proxied_from_child_id` mapping. [Slice 1.5]
- [x] **CORE-03**: Identity minimal + `capability_grants` scaffolding multi-user. Single-user default `local` con wildcard `'*'`. `HasCapability()` stub per future tool-dispatch enforcement. 2 tabelle, ~80 LOC. [Slice 1.7]
- [x] **CORE-04**: Conversation persistence multi-thread Claude.ai-style (`aura.conversations` + `aura.conversation_turns` + `aura.conversation_spillover`) + microcompact L1 (in-context) + budget L2 (history trimming). Auto-title, archive, resume. Token + USD aggregati per conversation. [Slice 1.8]
- [x] **CORE-05**: Conversation full-text search via Postgres `pg_trgm` GIN index su `conversation_turns.content` + `aura chat search "<query>"` CLI + Telegram `/search` command. ~80 LOC + 1 migration. [Slice 1.8.5 — amendment #7]

### Capabilities (Slice 2–7)

- [x] **CAP-01**: Sandbox code execution — run untrusted python/shell through the non-deferred `sandbox_exec` tool backed by the local `sandbox-agent` container (`aura-sandbox-agent:py3`) on loopback. The operator starts it with `make sandbox-up`; Aura provisions/downloads nothing at chat boot. (D-15 pivot — supersedes both the bespoke seccomp Python sidecar and the interim MCP-server cut; the generic MCP bridge remains reusable but unmounted.) [Slice 2 / sandbox-agent]
- [x] **CAP-02**: Sandbox workspace persistence — `/workspace` is a sandbox-agent named volume that persists filesystem state across `sandbox_exec` calls, while execution itself is stateless HTTP into the persistent local container. (D-15 pivot — supersedes the bespoke per-conversation SessionManager, `sandbox_sessions`, and host egress proxy path.) [Slice 2 / sandbox-agent]
- [x] **CAP-03**: Swarm coordinator minimale: riusa `ParallelAgent` da Slice 0.9 + cap `MAX_SPAWN_DEPTH=2` per v1. NO DM-by-ID, NO tier-mapped models in v1 (deferred a post-MVP). Child budget inheritance dal parent's remaining. [Slice 3 — amendment #12 / #44]
- [x] **CAP-04**: KV cache builder stable-prefix + provider-aware (DeepSeek/Anthropic/OpenAI/Gemini). Architectural rule: **two system messages** — `messages[0]` cache-stable byte-identical, `messages[1]` mutable. CI job `scripts/cache_invariant_audit.sh` asserts SHA-256(`messages[0]`) constant across 20-turn replay (cross-slice). 80% cache hit target su DeepSeek-V4. [Slice 4 + amendment #16]
- [x] **CAP-05**: Web tools — `web_search` via SearXNG container; `web_fetch` via `codeberg.org/readeck/go-readability/v2` + `JohannesKaufmann/html-to-markdown/v2`. SSRF defense: IPv6 blocklist + DNS rebinding pin. [Slice 5 + amendment #3]
- [x] **CAP-06**: Scheduler cron + `agent_job` persistente su Postgres con `FOR UPDATE SKIP LOCKED` + advisory lock + heartbeat. Backup TaskKind handlers (`backup_postgres`, `backup_neo4j`) cronnati. [Slice 6]
- [ ] **CAP-07**: Skills system instruction-based (7a read + 7b validator + 7c write/edit + 7d install) con SKILL.md format compat Anthropic. `skill.catalog` HTML scrape hidden behind `aura skills enable-catalog` opt-in. Audit-immutable Postgres trigger BEFORE UPDATE/DELETE/TRUNCATE + role separation enforced. Unicode NFKC validator (10K fuzz on skill content). [Slice 7a/b/c/d + amendments #14, #17]
- [ ] **CAP-08**: Skill executable code snippets v1 — save/execute multi-lang con pattern analysis + TTL archived. Reusa sandbox 2b session-bound + skill validator. NO cross-conv cluster auto-suggest in v1 (deferred a Slice 7f / v1.x). [Slice 7e-core — amendment #13]

- [x] **CAP-09 / MCP-V2-01**: Aura MCP Manager control plane for v1. Managed MCP config grows profiles, recipe/catalog metadata, trust classes (`trusted_recipe`, `trusted_local`, `sandboxed_local`, `remote_http`, `blocked`), Streamable HTTP transport, doctor/status/logs, Calendar fixture recipe, sandboxed third-party local runtime, explicit trust approvals, redacted profile export, and mount-time tool risk-policy enforcement. New third-party local commands default to `blocked`; OpenClaw plugin-host runtime remains out-of-scope. [Phase 16 amendment]

### Transport + UX (Slice 8–11)

- [ ] **UX-01**: AG-UI gateway con SSE event protocol transport, thin wrapper over in-process emitter. `internal/agent` MUST NOT import `internal/agui` (boundary enforced via static analysis). [Slice 8]
- [ ] **UX-02**: Channels framework `internal/channels/<name>/` + Telegram come canale utente primario (`gopkg.in/telebot.v4` SHA-pinned) con MarkdownV2 custom ~80 LOC escaper (NON dependency telegramify) + `/cancel`, `/cost`, `/search` commands. [Slice 9b + amendments #4, #5, #8]
- [ ] **UX-03**: Setup wizard `http://127.0.0.1:9081/setup` con one-time token `AURA_SETUP_TOKEN` printato su stdout primo boot, QR per Telegram bot token paste. [Slice 9a + amendment #10]
- [ ] **UX-04**: Multimodal Gemma 4 sidecar (E4B Q4 baseline) per voice (STT) + image input via `ghcr.io/ggml-org/llama.cpp:server`. Markitdown sidecar per document → markdown conversion. [Slice 9c]
- [ ] **UX-05**: User onboarding + `Agent.md` profile per identity (filesystem `~/.aura/agents/<id>/Agent.md`, NON Neo4j). Iniettato come `messages[1]` (NON `messages[0]` per preservare KV cache). [Slice 10]
- [ ] **UX-06**: Memory ingestion documents → chunks → embeddings via Document → Chunk → Entity pipeline. Idempotent via content hash dedup. Embedding boot assertion. [Slice 11a/b + amendment #18]
- [ ] **UX-07**: Entity resolution + knowledge graph community detection via GDS Leiden. UNIQUE constraint su entity name + chaos test 10 goroutines concurrent ingestion. [Slice 11c]
- [ ] **UX-08**: GraphRAG hybrid retrieval (HNSW vector + fulltext BM25 + graph traversal + LLM re-rank). Pre-merge bench recall@5 ≥ 0.8 @ 1K/10K/100K corpus sizes, p95 ≤ 30ms vector search, snapshot in `docs/aura-quality-snapshot.md`. HNSW `M=32` (NON default 16). [Slice 11d + amendment #20]
- [ ] **UX-09**: Agent journal cross-conversation insights (`:AgentEpisode` + `:AgentInsight` subgraphs). `:AgentInsight` retrieval cached N minutes con TTL configurable per preservare KV cache `messages[2]` stability. [Slice 11e + amendment #11]

### Operations (Slice 14)

- [ ] **OPS-01**: End-user packaging & distribution. Single fat Aura container image (`docker/aura/Dockerfile`: Go binary + python/`uvx` + node/`npx` + pinned `mcp-neo4j-cypher==0.6.0`) so the host needs only Docker — MCP subprocesses spawn inside the image, `internal/knowledge/client.go` unchanged. `compose.yaml` gains an `aura` service + one-shot `aura-migrate` + persistent `aura-home` volume. `scripts/install.sh` self-host door (Docker check + secret-gen `openssl rand` chmod-600 idempotent + `OPENROUTER_API_KEY` opt-in + compose up + wizard URL); appliance door = same compose+image pre-seeded. Relaxes the D-22 empty-key fail-fast (`aura serve` boots keyless, agent call fail-closes `llm_not_configured`) so the Phase 13 setup wizard can collect the key later. Image published to `ghcr.io` pinned per release tag; goreleaser host binary retained for dev. [Slice 14 — amendment #47]

## v2 Requirements

Deferred to future release. Acknowledged but NOT in current roadmap.

### Local LLM Fallback

- **LLM-V2-01**: vLLM container `vllm/vllm-openai:latest` + LMCache disk-tier (port 8083, ~7 GB Gemma 3 12B Q5). GPU-gated. Fallback path "13-bis" reusa `aura-llama-multimodal` su CPU se GPU unavailable. [Slice 13a/b — gated su DGX Spark bundle availability]

### Skill Snippet Intelligence

- **SKILL-V2-01**: Slice 7f — cross-conv cluster analyzer per auto-suggest skill da pattern detection. Voyager NeurIPS 2023 + Mem0 procedural memory. Richiede tuning soglie + UX suppression false positive + Neo4j HNSW cross-conv load.

### Advanced Features (post-Telegram-launch evaluation)

- **SWARM-V2-01**: Swarm full N-deep con DM-by-ID + tier-mapped models (Opus per planner, Sonnet per worker, Haiku per fact-check). Re-evaluate dopo che real swarm demand emerge.
- **MCP-V2-01**: Promoted to v1 as **CAP-09** for Phase 16. Remaining v2 work, if any, is public marketplace discovery or OpenClaw-compatible plugin hosting, not the MCP manager itself.
- **PROJ-V2-01**: First-class "Projects" concept (group conversations + shared `Agent.md`).
- **EXPORT-V2-01**: Conversation export/share-link (`aura chat export <id> --format markdown`).
- **RETENTION-V2-01**: `AURA_CONVERSATION_RETENTION_DAYS` policy + auto-delete (GDPR-friendly per Italian SMB).

## Out of Scope

Explicit exclusions. Documented to prevent scope creep. Anti-features locked.

| Feature | Reason |
|---------|--------|
| Native Windows runtime | Aura gira in container (Docker Compose) o contro Docker sidecar. Windows solo via Docker Desktop in dev. Memory `feedback_sqlite_wal_windows_corruption.md` + Slice 2 OQ5: named volumes mandatory, no bind-mount Windows. |
| TTS / voice output | Aura legge audio (Gemma 4 STT) ma non parla. Pocket-TTS rimosso dal RAM budget (commit `06df9b72`). Voice output è future milestone. |
| Marketplace skills pubblico | Slice 7 ha skill installer locale (`skill_install`/`skill_create`); niente registry pubblico, niente sharing skills tra utenti, niente versioning distribuito. Supply-chain risk OWASP LLM Top 10 — local-first per definizione. |
| Multi-user con auth/RBAC reale | Slice 1.7 fornisce solo "identity minimal + `capability_grants` scaffolding". Niente login, niente sessioni HTTP autenticate, niente OAuth in v1. Multi-tenancy reale → future milestone (~+800 LOC stimati). |
| Native Go Neo4j driver | Disciplina PRD: tutto accesso Neo4j passa da `mcp-neo4j-cypher` MCP server (subprocess stdio). Pattern uniforme con altri MCP tools. `neo4j-go-driver` v6 NON adottato anche se esiste. |
| Real-time multi-user collaboration | CRDT/OT, presence, conflict resolution per single-user-first product = scope creep massivo. Telegram threads + Aura single-user è il modello. |
| Auto-rewrite substrate source code | Skills self-extend; il substrate (`internal/agent/loop.go`) NO. Voyager-style skills danno 90% del valore con 10% del rischio. |
| Computer Use / OS-level control | Computer Use richiede VNC/X11 transport, screen capture, vision model UI-aware, click/keystroke playback. Massive surface fuori da Telegram-as-primary-channel UX. v3+ consideration. |
| Embedded LLM in v1 (no API dependency) | DeepSeek-V4 via OpenRouter = quality + 80% cache savings. Slice 13 (vLLM+LMCache) in PRD ma esplicitamente v2 + GPU-gated. |
| Embedded UI library / web SPA | AG-UI è protocollo, non client. Setup wizard `/setup` HTML page = unica HTML/JS surface. Telegram come primary copre il bisogno user-facing senza React. |
| Auto-routing across N LLM providers | OpenRouter già fa multi-provider routing. Building Aura's own router = redundant + foot-gun (cache invalidation, per-provider quirks). PRD `provider-aware KV cache` è il giusto layer di astrazione. |
| Image generation native | Mini-PC RAM budget non ospita SD/FLUX. Skill installabile (`skill.install image-gen` wrapper Replicate/fal.ai). Don't bake in. |
| Real-time websockets transport | SSE-only è sufficient per AG-UI (Slice 8). Agentic event streams sono server→client; client→server è request/response. Websockets aggiungono reconnect/heartbeat/auth complexity. |
| Public skill marketplace abuse protection | No marketplace = no need. |
| Vector DB separato (Qdrant/Pinecone/Weaviate) | Spike `2026-05-27` validato: Neo4j HNSW Lucene = 22-30ms p95 + recall@5 5/5. Aggiungere Qdrant = +1 service operato, +RAM, schema-sync surface, zero gain. |

## Traceability

Populated by gsd-roadmapper during roadmap creation. Phase column references `.planning/ROADMAP.md` Phase Details section by integer phase number.

| Requirement | Phase | Status |
|-------------|-------|--------|
| PRD-01 | Phase 0 — PRD Amendments | Complete |
| INFRA-01 | Phase 1 — Infra DB + Knowledge | Complete |
| INFRA-02 | Phase 1 — Infra DB + Knowledge | Complete |
| INFRA-03 | Phase 2 — Agent Cornerstone | Complete |
| CORE-01 | Phase 3 — LLM Client + ToolResult | Complete |
| CORE-02 | Phase 4 — HITL + Identity + Conversations | Complete |
| CORE-03 | Phase 4 — HITL + Identity + Conversations | Complete |
| CORE-04 | Phase 4 — HITL + Identity + Conversations | Complete |
| CORE-05 | Phase 4 — HITL + Identity + Conversations | Complete |
| CAP-01 | Phase 8 — Sandbox via sandbox-agent (local container) | Complete |
| CAP-02 | Phase 8 — Sandbox via sandbox-agent (local container) | Complete |
| CAP-03 | Phase 9 — Swarm (Minimal) | In Progress (09-01 doc-gate done; code waves 09-02..09-06 pending) |
| CAP-04 | Phase 6 — KV Cache Builder | Complete |
| CAP-05 | Phase 7 — Web Tools | Complete |
| CAP-06 | Phase 10 — Scheduler | Complete |
| CAP-07 | Phase 11 — Skills | Pending |
| CAP-08 | Phase 11 — Skills | Pending |
| CAP-09 / MCP-V2-01 | Phase 16 — MCP Sidecar Manager + Third-Party Trust | Complete |
| UX-01 | Phase 12 — AG-UI Gateway | Pending |
| UX-02 | Phase 13 — Channels + Telegram + Multimodal | Pending |
| UX-03 | Phase 13 — Channels + Telegram + Multimodal | Pending |
| UX-04 | Phase 13 — Channels + Telegram + Multimodal | Pending |
| UX-05 | Phase 14 — Onboarding + Agent.md | Pending |
| UX-06 | Phase 15 — Memory Subsystem | Pending |
| UX-07 | Phase 15 — Memory Subsystem | Pending |
| UX-08 | Phase 15 — Memory Subsystem | Pending |
| UX-09 | Phase 15 — Memory Subsystem | Pending |
| OPS-01 | Phase 17 — Packaging & Distribution | Pending |

**Coverage:**

- v1 requirements: 28 total (1 PRD + 3 INFRA + 5 CORE + 9 CAP + 9 UX + 1 OPS)
- Mapped to phases: 28
- Unmapped: 0 ✓
- Phases used: 18 (P0 through P17)

---
*Requirements defined: 2026-05-29*
*Last updated: 2026-06-05 after Phase 17 OPS-01 packaging & distribution registration (amendment #47)*
