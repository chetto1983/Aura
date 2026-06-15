# Aura

## What This Is

Aura è un substrate agentico Go-native, domain-neutral, single-binary, multi-channel. Il prodotto "personal AI" che l'utente percepisce (Telegram bot, conversazioni stateful, memoria persistente, skills installabili) è una **configurazione** sopra il substrate, non l'essenza. Il target di lungo periodo è il bundle commerciale **DGX Spark + Aura** per Italian SMB (business delegato ad Andrea — vedi memory `project_aura_dgx_spark_bundle_vision`); il deliverable tecnico è il substrate riusabile che lo abilita.

## Core Value

**Substrate agentico domain-neutral**: un runtime Go che esegue un agentic loop multi-tool affidabile con identity, channels, skills e memory come overlay configurabili. Se tutto il resto fallisce, ciò che deve funzionare è il loop che riceve un task, sceglie tool, esegue, persiste stato, ritorna risposta — su qualsiasi canale, per qualsiasi identità, con qualsiasi memoria collegata.

## Current State

**v0.0.0 Substrate — SHIPPED 2026-06-15** (24 phases, 144 plans, 233 tasks; audit PASSED). Il substrate agentico è feature-complete e live-proven sullo stack reale: persistence (PG+Neo4j), agent loop + workflow agents, LLM client, HITL, conversazioni, KV cache, sandbox, swarm, web tools, scheduler, skills, AG-UI gateway, Telegram multimodale, onboarding/Agent.md, memory (agent-memory MCP), MCP manager, hooks. Owned-surface coverage 90.3% (ogni package owned ≥85%), CI verde. Dettaglio storico in `.planning/MILESTONES.md` + `.planning/milestones/v0.0.0-*`.

**Next: v1.0.0 — Aura Deep Search Web Cockpit** (definito via `/gsd-new-milestone`): operator cockpit web embedded (Vite + React + assistant-ui) sopra l'AG-UI/SSE gateway, secondo `docs/design/aura-deep-search-figma/ux-spec.md`.

## Current Milestone: v1.0.0 — Aura Deep Search Web Cockpit

**Goal:** Build the embedded "Aura Deep Search" operator cockpit (Vite + React + `@assistant-ui/react-ag-ui`) served from `aura serve` over the existing AG-UI/SSE gateway, per `docs/design/aura-deep-search-figma/ux-spec.md` — preserving the single-binary deploy invariant — and harden the agent perimeter first so the web exposure lands on a production-ready base.

**Target features:**
- Agent perimeter production-readiness hardening (Phase 22 — `internal/agent` audit remediation: panic firewall, `shell_exec` secret-leak, MCP reconnect resilience, hook fail-soft, observability)
- Embedded operator cockpit SPA served from `aura serve` via `//go:embed` (single binary, one port)
- Minimal web-auth boundary (GAP-2): reverse-proxy default + in-binary signed session cookie (activates `capability_grants`) + fail-fast non-loopback guard
- Chat lane (assistant-ui over `POST /agent/run` SSE) + conversation list/search/rename/archive + approval center (HITL) + runtime health + cost/cache footer
- Typed-display protocol (GAP-1): `aura.display` CUSTOM event + Go normalizer + display router (web_result / document / code / table / chart / system_event / swarm_report / graph_*)
- Neo4j Graph Explorer (WebGL canvas, path strip, node inspector, read-only Cypher guard)
- Read-only governance boards (MCP server list/status, skills active/pending/archived/audit, scheduler board)
- Web onboarding / setup wizard

**Deferred to a follow-up milestone:** governance write surfaces (MCP install/remove, skill approve/delete via HTTP) and the `ui_control` operator-OS shell (dock windows, command palette) — highest abuse surface, needs hardened auth.

## Requirements

### Validated

Tutti i requisiti del substrate v0.0.0 sono shipped + audit-passed (vedi `.planning/milestones/v0.0.0-*` e `MILESTONES.md`).

**Infrastruttura**

- ✓ **INFRA-01**: Postgres 17 + sqlc + pgx + golang-migrate, schema `aura.*`, 14 migrazioni, role separation `aura_app`/`aura_migrate` — v0.0.0 (Phase 1)
- ✓ **INFRA-02**: Neo4j 5.26 Community + APOC + GDS + HNSW 768d + `mcp-neo4j-cypher` MCP server — v0.0.0 (Phase 1)
- ✓ **INFRA-03**: `Agent` interface + workflow agents (Sequential/Loop/Parallel), budget contract — v0.0.0 (Phase 2)

**Core agent**

- ✓ **CORE-01**: LLM client OpenAI-compat (DeepSeek-V4 via OpenRouter) + ToolResult + SSE streaming + reasoning data-plane — v0.0.0 (Phase 3)
- ✓ **CORE-02**: `ask_user` pause/resume FIFO multi-pause, crash-recoverable — v0.0.0 (Phase 4)
- ✓ **CORE-03**: Identity minimal + `capability_grants` scaffolding — v0.0.0 (Phase 4)
- ✓ **CORE-04**: Conversation persistence multi-thread + microcompact ladder + pg_trgm FTS — v0.0.0 (Phase 4)

**Capabilities**

- ✓ **CAP-01**: Sandbox runner — *adopted rivetdev/sandbox-agent (D-15 pivot, supersedes bespoke 2a/2b); host-primary `shell_exec` + deliberate container escalation* — v0.0.0 (Phase 8)
- ✓ **CAP-03**: Swarm coordinator (ParallelAgent reuse, budget tree, depth guard); live dual-gate E2E PASS — v0.0.0 (Phase 9)
- ✓ **CAP-04**: KV cache builder stable-prefix + provider-aware `cache_control` + cross-slice `messages[0]` invariant CI gate — v0.0.0 (Phase 6)
- ✓ **CAP-05**: Web tools `web_search` (SearXNG) + `web_fetch` (readability) con IPv6/DNS-pin SSRF defense — v0.0.0 (Phase 7)
- ✓ **CAP-06**: Scheduler cron + `agent_job` persistente (SKIP LOCKED + advisory lock + heartbeat) + origin-channel routing — v0.0.0 (Phase 10 + 20)
- ✓ **CAP-07**: Skills instruction-based + executable snippets, scoring-gated self-extension, append-only audit + snippet-reuse steady state — v0.0.0 (Phase 11 + 18)
- ✓ **CAP-09 / MCP-V2-01**: Aura MCP Manager control plane — managed config v2, profiles, recipes, trust classes, Streamable HTTP, doctor/status/logs, redacted export, mount-time risk-policy — v0.0.0 (Phase 16)

**Transport + UX**

- ✓ **UX-01**: AG-UI gateway SSE event protocol transport (pure translator + REASONING lifecycle + fanout) — v0.0.0 (Phase 12)
- ✓ **UX-02**: Channels framework + Telegram primary channel + setup wizard + multimodal STT/OCR/TTS sidecars — v0.0.0 (Phase 13)
- ✓ **UX-03**: User onboarding LoopAgent + identity-aware `Agent.md` profile at `messages[1]` — v0.0.0 (Phase 14)
- ✓ **UX-04**: Memory subsystem — *adopted forked neo4j-labs/agent-memory MCP sidecar (POLE+O), Go wiring + `aura memory` CLI* — v0.0.0 (Phase 15)
- ✓ **EXT-01**: Plugins — in-process `HookManager` (5 LlmAgent insertion points + trust-gated command hooks) — v0.0.0 (Phase 21)

### Active

v1.0.0 — Aura Deep Search Web Cockpit (REQ-IDs detailed in `REQUIREMENTS.md`; mapped to phases by the roadmapper):

- [ ] **HARDEN-\***: Agent perimeter production-readiness remediation (Phase 22 bug-fix; `internal/agent` audit)
- [ ] **WEB-\***: Embedded cockpit SPA host (`aura serve` + `//go:embed`) + minimal web-auth boundary (GAP-2)
- [ ] **CHAT-\***: assistant-ui chat lane over AG-UI/SSE + conversations + approval center + runtime health + cost/cache
- [ ] **DISPLAY-\***: Typed-display protocol (GAP-1) + display router
- [ ] **GRAPH-\***: Neo4j Graph Explorer (read-only)
- [ ] **GOV-\***: Read-only governance boards (MCP / skills / scheduler)
- [ ] **ONBOARD-\***: Web onboarding / setup wizard

### Out of Scope

- **Slice 13 (vLLM + LMCache local LLM fallback)** — gated su disponibilità GPU; riprogrammato in v2 quando il bundle DGX Spark sblocca il path. Cumulative idle stimato +5–7 GB RAM.
- **Native Windows runtime** — Aura gira in container (Docker Compose) o contro Docker sidecar. Su Windows solo via Docker Desktop in dev. Memory `feedback_sqlite_wal_windows_corruption.md` + Slice 2 OQ5: named volumes mandatory, no bind-mount Windows.
- **Conversational voice mode** — Aura legge audio in ingresso (STT) e produce voice replies TTS su Telegram (Kokoro, Phase 13). Una modalità voce conversazionale full-duplex resta future milestone.
- **Marketplace skills pubblico** — Slice 7 ha skill installer locale (`skill_install`/`skill_create`); niente registry pubblico, niente sharing skills tra utenti, niente versioning distribuito. Local-first per definizione.
- **Multi-user con auth/RBAC reale** — Slice 1.7 fornisce solo "identity minimal + `capability_grants` scaffolding". v1.0.0 aggiunge una boundary web minima (reverse-proxy + session-cookie firmato che attiva lo scaffolding `capability_grants`); ma niente login multi-tenant, RBAC, o OAuth reale in questa milestone.
- **Native Go Neo4j driver** — disciplina PRD: tutto accesso Neo4j passa da `mcp-neo4j-cypher` MCP server (subprocess stdio). Pattern uniforme con altri MCP tools.

## Context

**Stato di partenza**

- Repo Go con skeleton 633 LOC sotto `cmd/` + `internal/` (commit `af4ca65c`). Definisce interfaces thin: `llm.Client`, `agent.Loop`, `tools.Tool` + `Registry`, `sandbox.Runner` stub, `swarm.Coordinator` stub. Zero dipendenze esterne (`go.mod` vuoto di `require`).
- Implementazione precedente preservata al tag `pre-rewrite-2026-05-27`. La sessione web del 2026-05-28 ha lockato il PRD (`prd.md` 4400 LOC, 14 slice) come single source of truth.
- Codebase map completa in `.planning/codebase/` (7 doc, 2721 LOC) distingue Current vs Planned per ogni dimensione (stack, architecture, structure, conventions, testing, integrations, concerns).

**Discipline ereditate**

- **PRD-first principle**: senza PRD completo non si scrive una riga. Deviazioni dal PRD richiedono PRD-amendment commit prima dell'implementazione (vedi PRD §Slice Q&A discipline → Q&A revision protocol).
- **3-gate Slice Q&A discipline** per ogni slice: Gate 1 Definition of Ready (pre), Gate 2 Implementation Q&A (durante con `go vet + build + test + race` verdi), Gate 3 Definition of Done (post pre-merge con coverage ≥85% owned-surface, mutation testing ≥70% killed).
- **No god class** (≤600 LOC/file), **refactor on touch**, **3-strike rule**, **deferred-tool pattern** mandatory per tool grandi.

**Ecosistema decisionale già consolidato**

- Embedding = `granite-embedding-97m` (IBM Granite, 384d) via llama.cpp sidecar + Neo4j HNSW vector store (vedi memory `feedback_embedding_backend_stays_mistral`).
- Knowledge graph = Neo4j via MCP (wiki markdown deprecato, memory `project_graph_memory_core_strategy`).
- OpenRouter provider verificato: DeepSeek-V4 Flash supporta 80% cache savings −63% cost (memory `reference_openrouter_provider_capabilities_2026-05-27`).
- Mini-PC CPU budget condiviso con lavoro utente: embed sidecar ≤4 thread, no busy-loop (memory `feedback_minipc_cpu_budget`).

## Constraints

- **Tech stack**: Go 1.26. Postgres 17. Neo4j 5.26 Community. Python 3.12 slim per sandbox + MCP servers. **Sealed**: scelte derivate da spike validati (Neo4j spike 2026-05-27 in `D:/tmp/aura-neo4j-spike-2026-05-27/` ha provato 1.6-1.8s structured vs 27-75s blob+LLM).
- **Deployment target**: mini-PC 16-core 32 GB RAM (16 GB minimum). Cumulative idle ~5.7–6.2 GB, peak ~7 GB under load. Slice 13 aggiunge +5–7 GB ma è v2.
- **Platform**: Linux preferito (seccomp Slice 2). Docker Desktop su Windows tollerato in dev, **mai** native Windows runtime. Named volumes mandatory.
- **Performance**: KV cache hit rate ≥80% (DeepSeek-V4 capability sfruttabile). Web search p95 ≤2s (SearXNG locale). Neo4j vector search p95 ≤30 ms (validato dallo spike).
- **Security**: sandbox `network_mode: none` di default, allowlist hosts esplicita. Secrets in `.env` (gitignored), `.env.example` template committato. Skill execution gated da audit `aura.skill_audit`. AG-UI gateway loopback-only di default (amendment #35).
- **Test discipline rigorosa**: realistic fixtures, `goleak.VerifyNone` in `TestMain`, race detector, property-based dove indicato, build tags integration tiers, mutation testing ≥70% killed Gate 3.
- **Commit discipline**: 1 slice = 1 commit (atomico, imperativo, Co-Authored-By trailer). `git push` mai senza richiesta esplicita.

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Tabula-rasa rewrite (drop implementazione `pre-rewrite-2026-05-27`) | Implementazione precedente aveva drift architetturale + dead-code accumulato; PRD-first richiede fondamenta pulite | ✓ Good — v0.0.0 shipped pulito (24 fasi, 90.3% coverage) |
| PRD `prd.md` 4400 LOC come single source of truth | Validato da 4 sub-agent in parallelo + 3 round di review (commit `b3faacbf`); pattern memory `feedback_validate_plan_with_3_subagents_2026-05-27` | ✓ Good |
| Substrate domain-neutral con overlay personal (non monolite "personal AI") | Memory `feedback_aura_is_platform_shaped`: il "personal" è 4 overlay + skills + MCP wiring; non ottimizzare prematuramente per marketplace | ✓ Good |
| Bundle commerciale DGX Spark + Aura per Italian SMB (target lungo periodo) | Memory `project_aura_dgx_spark_bundle_vision`: engineering chiude Aura tecnicamente; Andrea gestisce NVIDIA Partner Program + sales + legal | — Pending |
| Telegram come canale utente primario v1 | Memory + PRD Slice 9b: utente reale lo usa quotidianamente; AG-UI gateway (Slice 8) resta API-level | ✓ Good — Telegram live in Phase 13 |
| `mcp-neo4j-cypher` MCP server invece di native Go driver | Disciplina uniforme con altri MCP tools; valida memory `project_neo4j_spike_2026-05-27` (15-45× faster structured vs blob+LLM) | ✓ Good |
| Embedding `granite-embedding-97m` (IBM Granite, 384d) via llama.cpp sidecar | Memory `feedback_embedding_backend_stays_mistral`: Neo4j HNSW Lucene validato; backbone per reasoning-classifier locale | ✓ Good |
| OpenRouter `deepseek-v4-flash` default LLM | Memory `reference_openrouter_provider_capabilities_2026-05-27`: 80% cache savings, −63% cost, OpenAI-wire shape | ✓ Good |
| Sandbox: pivot da bespoke a rivetdev/sandbox-agent (D-15) | Bespoke sandbox = "nuclear bomb" (memory); off-the-shelf HTTP runner + host-primary shell | ✓ Good — Phase 8 |
| Memory: adopt forked neo4j-labs/agent-memory MCP off-the-shelf (amendment #61) | Evita ~1850 LOC custom; POLE+O + Protocol-pluggable embedder/LLM/backend; spikes 031-035 validati live | ✓ Good — Phase 15 |
| Slice 13 (vLLM/LMCache) out di v1 → v2 | Gated su GPU; il bundle DGX Spark sblocca il path | — Pending |
| `Agent` interface pattern stolen, not imported, da `google/adk-go` | adk-go porta 35 GCP/OTel/Gemini deps inaccettabili; Aura reimplementa ~380 LOC stessa shape | ✓ Good |
| GSD tooling come workflow ufficiale (3-gate per slice) | CLAUDE.md §GSD tooling: commands + agents + hooks + skills installati; pattern testato | ✓ Good |

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
*Last updated: 2026-06-15 — v0.0.0 Substrate milestone SHIPPED (24 phases, 144 plans, 233 tasks; audit PASSED, owned-surface coverage 90.3%). All substrate requirements moved to Validated. TTS-out removed from Out of Scope (Kokoro shipped in Phase 13). Next: v1.0.0 Aura Deep Search Web Cockpit, scoped via /gsd-new-milestone.*
