# Aura

## What This Is

Aura è un substrate agentico Go-native, domain-neutral, single-binary, multi-channel. Il prodotto "personal AI" che l'utente percepisce (Telegram bot, conversazioni stateful, memoria persistente, skills installabili) è una **configurazione** sopra il substrate, non l'essenza. Il target di lungo periodo è il bundle commerciale **DGX Spark + Aura** per Italian SMB (business delegato ad Andrea — vedi memory `project_aura_dgx_spark_bundle_vision`); il deliverable tecnico è il substrate riusabile che lo abilita.

## Core Value

**Substrate agentico domain-neutral**: un runtime Go che esegue un agentic loop multi-tool affidabile con identity, channels, skills e memory come overlay configurabili. Se tutto il resto fallisce, ciò che deve funzionare è il loop che riceve un task, sceglie tool, esegue, persiste stato, ritorna risposta — su qualsiasi canale, per qualsiasi identità, con qualsiasi memoria collegata.

## Requirements

### Validated

(None yet — tabula-rasa rewrite. Lo skeleton 633 LOC `af4ca65c` è scaffolding non-funzionante per design; PRD-first principle: senza PRD completo non si scrive una riga, e il PRD è stato lockato il 2026-05-27 con commit `b3faacbf`.)

### Active

**Infrastruttura (Slice 0.5–0.9)**

- [ ] **INFRA-01**: Postgres 17 + sqlc + pgx + golang-migrate operativo con schema `aura.*` e migrazioni versionate (Slice 0.5)
- [ ] **INFRA-02**: Neo4j 5.x Community + APOC + GDS + HNSW vector index 768d + `mcp-neo4j-cypher` MCP server (Slice 0.7)
- [ ] **INFRA-03**: `Agent` interface + workflow agents (Sequential/Loop/Parallel) pattern stolen-not-imported da `google/adk-go` (Slice 0.9)

**Core agent (Slice 1–1.8)**

- [ ] **CORE-01**: LLM client OpenAI-compat (DeepSeek-V4 via OpenRouter default) con ToolResult pattern, prompt caching, SSE streaming (Slice 1)
- [ ] **CORE-02**: `ask_user` tool con pause/resume FIFO multi-pause (Slice 1.5)
- [ ] **CORE-03**: Identity minimal + `capability_grants` scaffolding multi-user (Slice 1.7)
- [ ] **CORE-04**: Conversation persistence multi-thread Claude.ai-style + microcompact (Slice 1.8)

**Capabilities (Slice 2–7)**

- [ ] **CAP-01**: Sandbox runner Python 3.12 stateless (2a) + session-bound con workspace + network allowlist (2b) (Slice 2)
- [x] **CAP-03**: Swarm coordinator riusa ParallelAgent (Slice 3) — *Validated in Phase 9 (2026-06-04); `swarm_spawn` deferred tool + ephemeral runner (waves, per-child isolation, budget tree, depth guard) + first production mail/WhatsApp MCP mounts; live dual-gate E2E PASS (judge 1.00, fan-out 1.30×, mail+WA read-back) — docs/aura-swarm-eval-2026-06-04.md.*
- [x] **CAP-04**: KV cache builder stable-prefix + provider-aware (Slice 4) — *Validated in Phase 6 (2026-06-02); `messages[0]` invariant + provider-aware `cache_control` seam + cross-slice CI gate shipped. Live cache-warming/≥80%-hit-rate criteria deferred to operator UAT (06-HUMAN-UAT.md).*
- [ ] **CAP-05**: Web tools `web_search` (SearXNG) + `web_fetch` (readability + html-to-markdown) (Slice 5)
- [ ] **CAP-06**: Scheduler cron + agent jobs persistente su Postgres (Slice 6)
- [ ] **CAP-07**: Skills system instruction-based (7a/b/c/d) + executable code snippets multi-lang con pattern analysis e TTL archived (7e) (Slice 7)

**Transport + UX (Slice 8–11)**

- [ ] **UX-01**: AG-UI gateway con SSE event protocol transport (Slice 8)
- [ ] **UX-02**: Channels framework + Telegram come canale utente primario + Setup wizard + Gemma 4 multimodale (Slice 9a/b/c)
- [ ] **UX-03**: User onboarding + `Agent.md` profile per identity (Slice 10)
- [ ] **UX-04**: Memory ingestion + taxonomy completa: Documents + Entities + Graph + Agent journal (Slice 11a/b/c/d/e)

### Out of Scope

- **Slice 13 (vLLM + LMCache local LLM fallback)** — gated su disponibilità GPU; riprogrammato in v2 quando il bundle DGX Spark sblocca il path. Cumulative idle stimato +5–7 GB RAM.
- **Native Windows runtime** — Aura gira in container (Docker Compose) o contro Docker sidecar. Su Windows solo via Docker Desktop in dev. Memory `feedback_sqlite_wal_windows_corruption.md` + Slice 2 OQ5: named volumes mandatory, no bind-mount Windows.
- **TTS / voice output** — Aura legge audio (Gemma 4 STT) ma non parla. Pocket-TTS rimosso dal RAM budget (commit `06df9b72`). Voice output è future milestone.
- **Marketplace skills pubblico** — Slice 7 ha skill installer locale (`skill_install`/`skill_create`); niente registry pubblico, niente sharing skills tra utenti, niente versioning distribuito. Local-first per definizione.
- **Multi-user con auth/RBAC reale** — Slice 1.7 fornisce solo "identity minimal + `capability_grants` scaffolding". Niente login, niente sessioni HTTP autenticate, niente OAuth in v1. Multi-tenancy reale arriva in milestone futura.
- **Native Go Neo4j driver** — disciplina PRD: tutto accesso Neo4j passa da `mcp-neo4j-cypher` MCP server (subprocess stdio). Pattern uniforme con altri MCP tools.

## Context

**Stato di partenza**

- Repo Go con skeleton 633 LOC sotto `cmd/` + `internal/` (commit `af4ca65c`). Definisce interfaces thin: `llm.Client`, `agent.Loop`, `tools.Tool` + `Registry`, `sandbox.Runner` stub, `swarm.Coordinator` stub. Zero dipendenze esterne (`go.mod` vuoto di `require`).
- Implementazione precedente preservata al tag `pre-rewrite-2026-05-27`. La sessione web del 2026-05-28 ha lockato il PRD (`prd.md` 4400 LOC, 14 slice) come single source of truth.
- Codebase map completa in `.planning/codebase/` (7 doc, 2721 LOC) distingue Current vs Planned per ogni dimensione (stack, architecture, structure, conventions, testing, integrations, concerns).

**Discipline ereditate**

- **PRD-first principle**: senza PRD completo non si scrive una riga. Deviazioni dal PRD richiedono PRD-amendment commit prima dell'implementazione (vedi PRD §Slice Q&A discipline → Q&A revision protocol).
- **3-gate Slice Q&A discipline** per ogni slice: Gate 1 Definition of Ready (pre), Gate 2 Implementation Q&A (durante con `go vet + build + test + race` verdi), Gate 3 Definition of Done (post pre-merge con coverage ≥75% unit / ≥60% integration, mutation testing ≥70% killed).
- **No god class** (≤600 LOC/file), **refactor on touch**, **3-strike rule**, **deferred-tool pattern** mandatory per tool grandi.

**Ecosistema decisionale già consolidato (28 OQ aperte flagged nel PRD)**

- Embedding = `embeddinggemma-300m` via llama.cpp sidecar + Neo4j HNSW vector store (Mistral/Qdrant deprecati, vedi memory `feedback_embedding_backend_stays_mistral`).
- Knowledge graph = Neo4j via MCP (wiki markdown deprecato, memory `project_graph_memory_core_strategy`).
- OpenRouter provider verificato: DeepSeek-V4 Flash supporta 80% cache savings −63% cost (memory `reference_openrouter_provider_capabilities_2026-05-27`).
- Mini-PC CPU budget condiviso con lavoro utente: embed sidecar ≤4 thread, no busy-loop (memory `feedback_minipc_cpu_budget`).

## Constraints

- **Tech stack**: Go 1.23+ (richiesto da `iter.Seq2` per agent streaming, Slice 0.9). Postgres 17. Neo4j 5.x Community. Python 3.12 slim per sandbox + MCP servers. **Sealed**: scelte derivate da spike validati (Neo4j spike 2026-05-27 in `D:/tmp/aura-neo4j-spike-2026-05-27/` ha provato 1.6-1.8s structured vs 27-75s blob+LLM).
- **Deployment target**: mini-PC 16-core 32 GB RAM (16 GB minimum). Cumulative idle ~5.7–6.2 GB end-of-Slice-7, peak ~7 GB under load. Slice 13 aggiunge +5–7 GB ma è v2.
- **Platform**: Linux preferito (seccomp Slice 2). Docker Desktop su Windows tollerato in dev, **mai** native Windows runtime. Named volumes mandatory.
- **Performance**: KV cache hit rate ≥80% post-Slice 4 (DeepSeek-V4 capability sfruttabile). Web search p95 ≤2s (SearXNG locale). Neo4j vector search p95 ≤30 ms (validato dallo spike).
- **Security**: sandbox `network_mode: none` di default, allowlist hosts esplicita. Secrets in `.env` (gitignored), `.env.example` template committato. Skill execution gated da audit `aura.skill_audit`.
- **Test discipline rigorosa**: realistic fixtures, `goleak.VerifyNone` in `TestMain`, race detector, property-based dove indicato, build tags integration tiers, mutation testing ≥70% killed Gate 3.
- **Commit discipline**: 1 slice = 1 commit (atomico, imperativo, Co-Authored-By trailer). `git push` mai senza richiesta esplicita.

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Tabula-rasa rewrite (drop implementazione `pre-rewrite-2026-05-27`) | Implementazione precedente aveva drift architetturale + dead-code accumulato; PRD-first richiede fondamenta pulite | — Pending |
| PRD `prd.md` 4400 LOC come single source of truth | Validato da 4 sub-agent in parallelo + 3 round di review (commit `b3faacbf`); pattern memory `feedback_validate_plan_with_3_subagents_2026-05-27` | ✓ Good |
| Substrate domain-neutral con overlay personal (non monolite "personal AI") | Memory `feedback_aura_is_platform_shaped`: il "personal" è 4 overlay + skills + MCP wiring; non ottimizzare prematuramente per marketplace | — Pending |
| Bundle commerciale DGX Spark + Aura per Italian SMB (target lungo periodo) | Memory `project_aura_dgx_spark_bundle_vision`: engineering chiude Aura tecnicamente; Andrea gestisce NVIDIA Partner Program + sales + legal | — Pending |
| Telegram come canale utente primario v1 | Memory + PRD Slice 9b: utente reale lo usa quotidianamente; AG-UI gateway (Slice 8) resta API-level | — Pending |
| `mcp-neo4j-cypher` MCP server invece di native Go driver | Disciplina uniforme con altri MCP tools; valida memory `project_neo4j_spike_2026-05-27` (15-45× faster structured vs blob+LLM) | — Pending |
| Embedding `embeddinggemma-300m` 768d native via llama.cpp sidecar, no MRL truncation | Memory `feedback_embedding_backend_stays_mistral`: Neo4j HNSW Lucene validato 22-30ms p95 + recall@5 5/5 | — Pending |
| OpenRouter `deepseek-v4-flash:exacto` default LLM | Memory `reference_openrouter_provider_capabilities_2026-05-27`: 80% cache savings, −63% cost, OpenAI-wire shape | — Pending |
| Slice 13 (vLLM/LMCache) out di v1 → v2 | Gated su GPU; il bundle DGX Spark sblocca il path. Fallback path "13-bis" reusa `aura-llama-multimodal` se serve copertura intermedia | — Pending |
| `Agent` interface pattern stolen, not imported, da `google/adk-go` | adk-go porta 35 GCP/OTel/Gemini deps inaccettabili; Aura reimplementa ~380 LOC stessa shape (Slice 0.9 disclaimer PRD) | — Pending |
| GSD tooling come workflow ufficiale (3-gate per slice) | CLAUDE.md §GSD tooling: 67 commands + 33 agents + 12 hooks + 46 skills installati; pattern testato | — Pending |

## Evolution

This document evolves at phase transitions and milestone boundaries.

**After each phase transition** (via `/gsd-transition`):
1. Requirements invalidated? → Move to Out of Scope with reason
2. Requirements validated? → Move to Validated with phase reference
3. New requirements emerged? → Add to Active
4. Decisions to log? → Add to Key Decisions
5. "What This Is" still accurate? → Update if drifted

**After each milestone** (via `/gsd:complete-milestone`):
1. Full review of all sections
2. Core Value check — still the right priority?
3. Audit Out of Scope — reasons still valid?
4. Update Context with current state

---
*Last updated: 2026-06-04 — Phase 9 (Swarm minimal, CAP-03) complete: `swarm_spawn` + ephemeral runner, mail/WhatsApp MCP production mounts (Deferred flip + allowlist + fail-soft boot), proxied_* pause plumb, live dual-gate E2E PASS (judge 1.00, fan-out 1.30×) + CoT eval re-run green*
