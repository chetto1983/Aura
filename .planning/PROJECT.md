# Aura

## What This Is

Aura è un substrate agentico Go-native, domain-neutral, single-binary, multi-channel. Il prodotto "personal AI" che l'utente percepisce (Telegram bot, conversazioni stateful, memoria persistente, skills installabili) è una **configurazione** sopra il substrate, non l'essenza. Il target di lungo periodo è il bundle commerciale **DGX Spark + Aura** per Italian SMB (business delegato ad Andrea — vedi memory `project_aura_dgx_spark_bundle_vision`); il deliverable tecnico è il substrate riusabile che lo abilita.

## Core Value

**Substrate agentico domain-neutral**: un runtime Go che esegue un agentic loop multi-tool affidabile con identity, channels, skills e memory come overlay configurabili. Se tutto il resto fallisce, ciò che deve funzionare è il loop che riceve un task, sceglie tool, esegue, persiste stato, ritorna risposta — su qualsiasi canale, per qualsiasi identità, con qualsiasi memoria collegata.

## Current State

**v1.0.0 Aura Deep Search Web Cockpit — SHIPPED 2026-06-29** (9 phases [22–30], 45 plans, 113 tasks; audit PASSED, 56/56 requirements). The embedded operator cockpit is live over the v0.0.0 substrate — a single-binary Vite + React + assistant-ui web UI served from `aura serve` over the AG-UI/SSE gateway: a hardened agent perimeter, streaming chat with cross-thread HITL approvals, a typed-display evidence protocol + Neo4j graph explorer, read/write governance surfaces (MCP config + skills install lifecycle), web onboarding, and GPU-reranked two-stage retrieval. A post-Phase-25 premium overhaul layered the logo-matched blue design system, Authula embedded auth, an `aura.settings` settings page, and calendar/PIM + WhatsApp connect. Owned-surface coverage 88.1% (≥85% floor), full Go + web CI green, `messages[0]` cache-invariant gate green throughout. Closed as **`override_closeout`** with 6 deferred-by-design verification items (GPU-host live tiers this 4GB-GPU machine cannot run + live-CI-only tiers + Phase-25 carried-forward UAT). Dettaglio in `.planning/MILESTONES.md` + `.planning/milestones/v1.0.0-*`.

**Prior: v0.0.0 Substrate — SHIPPED 2026-06-15** (24 phases, 144 plans, 233 tasks; audit PASSED). Il substrate agentico domain-neutral feature-complete e live-proven: persistence (PG+Neo4j), agent loop + workflow agents, LLM client, HITL, conversazioni, KV cache, sandbox, swarm, web tools, scheduler, skills, AG-UI gateway, Telegram multimodale, onboarding/Agent.md, memory (agent-memory MCP), MCP manager, hooks. Owned-surface coverage 90.3%. Dettaglio in `.planning/milestones/v0.0.0-*`.

**Next milestone:** **v2.0.0 — Industrial Hardening & Multi-User Production** (in planning, scoped 2026-06-29 via `/gsd-new-milestone`). Vedi sezione "## Current Milestone" sotto. Altri candidati noti rinviati (vedi Out of Scope + Key Decisions): il `ui_control` operator-OS shell + scheduler write surfaces, il completamento dei 6 deferred GPU/live-CI tiers su un host GPU adeguato, Slice 13 local-LLM fallback (GPU-gated, DGX Spark path).

## Current Milestone: v2.0.0 — Industrial Hardening & Multi-User Production

**Goal:** Chiudere l'intero audit industriale 2026-06-21 (51 findings: 10 P1, 28 P2, 13 P3) e industrializzare Aura a un onesto **10/10** di production-readiness — via **isolamento sandbox full-capability per-utente** (agent-sandbox-class), **multi-user identity isolation**, una **ToolGateway** centrale con audit/ledger, e hardening observability/security/ops — **senza mai rimuovere il full-host terminal** che è la superficie-core di Aura. La contraddizione F-001 ("full host" vs "10/10") si dissolve: ogni identità guida una sandbox full-capability isolata, l'host reale non è mai esposto.

**Target features:**

- **Per-user full-capability sandbox** — runtime sandbox isolato per identità (agent-sandbox-class): l'agente conserva shell/fs/network completi; l'host reale non è mai esposto. Chiude F-001/R-001. Fork di design (full K8s/k3s vs pattern-over-rivetdev/Docker) risolto in research.
- **Multi-user identity isolation** — store conversazioni/approval owner-scoped + API filtrate per principal autenticato; proof E2E two-identity (F-028/R-022). **NO RBAC/roles/OAuth** (resta out — forma industriale minima).
- **Authula auth cutover** — flip default da passphrase a Authula embedded; `capability_grants` enforced per-route.
- **ToolGateway** — singolo punto di policy + approval + ledger durevole mutating-tool (F-001, F-011, F-020, F-031).
- **Runtime profiles** — `dev` / `local_trusted` / `single_user_hardened` / `server_production` con validation produzione (default secrets, listener health, CORS, run-dir, env-parse fail-fast: F-002, F-007, F-008, F-016, F-017, F-022, F-041).
- **Agent-loop correctness** — terminal `text_response` exclusivity + HITL resume atomicity single+batch + pause-flush durability (F-003, F-004, F-005, F-029, F-030).
- **MCP governance hardening** — transport classifier canonico, trust remoto esplicito, mount timeout, frame cap, process-tree teardown, CLI writes audited (F-013, F-014, F-027, F-031..F-038, F-046).
- **Production observability pack** — OTel spans, Prometheus alert rules, Grafana dashboards, readiness probes, sidecar/trace retention + cleanup (F-023, F-024, F-048).
- **Security & supply-chain** — secret redaction tool-output/trace, network egress policy, encrypted trace option, prompt-injection regression suite, SBOM + pinned actions (F-019, F-021, F-036, F-047, F-050, F-051, F-052).
- **Scale & operations** — load/chaos harness, backup/DR restore drill con RPO/RTO, scheduler drain, systemd stop budgets, object-store topology validation (F-018, F-035, F-042, F-043).
- **Capability evaluation suite + ADRs + release-readiness checklist** (F-025, F-026, F-045, F-049).

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

**v1.0.0 — Aura Deep Search Web Cockpit** (56/56 requirements shipped + audit-passed; full archive in `.planning/milestones/v1.0.0-*`):

- ✓ **HARDEN-01..12**: Agent perimeter production-readiness — panic crash-firewall, secret boundary, MCP resilience, active budget/wallclock caps, Prometheus observability (AG-001..064 ledger) — v1.0.0 (Phase 22)
- ✓ **FND-01..06**: Industrial frontend foundation — research-locked React 19 / Vite 8 / TS 6 / Tailwind 4, dark-operator token theme, zero-warning web CI gate, `//go:embed all:dist`, Node-24 Docker build — v1.0.0 (Phase 23)
- ✓ **WEB-01..04**: Single-binary SPA host on `aura serve` + HMAC signed-session auth boundary (activates `capability_grants`) + non-loopback boot guard + runtime health shell — v1.0.0 (Phase 24)
- ✓ **CHAT-01..05, APRV-01..03**: assistant-ui chat lane over AG-UI/SSE + conversation manager + cost/cache footer + cross-thread HITL approval center + D-09 branch trees — v1.0.0 (Phase 25)
- ✓ **DISP-01..05, SWARM-01**: Typed-display protocol (`aura.display` CUSTOM + Go normalizer) + `switch(type)` display router + source explorer + swarm report — v1.0.0 (Phase 26)
- ✓ **GRAPH-01..04**: Read-only Neo4j WebGL graph explorer — graph-normalizer + read-only Cypher guard + node inspector + path strip — v1.0.0 (Phase 27)
- ✓ **GOV-01..03, ONBD-01..02**: Read-only MCP/skills/scheduler governance boards + web onboarding wizard (2nd loginable identity, `capability_grants`-only authz, prd.md amendment #64) — v1.0.0 (Phase 28)
- ✓ **MCPW-01..03, SKW-01..03**: Governance write — MCP install/env-redaction/lifecycle (`mcp_audit`) + skills install → risk-tiered approval queue → activate (operator-resume-only, append-only audit) — v1.0.0 (Phase 29)
- ✓ **RET-01..05**: Retrieval hardening — fail-soft GPU reranker + two-stage retrieval (vector→rerank→graph-expand) + full-docs ingest (all markitdown formats) + non-monotonic guard + nDCG/Recall/MRR eval harness — v1.0.0 (Phase 30)

### Active

**v2.0.0 — Industrial Hardening & Multi-User Production** (in planning; requisiti formalizzati con REQ-ID in `.planning/REQUIREMENTS.md`, fasi 31+):

- [ ] Per-user full-capability sandbox isolation (agent-sandbox-class) — closes F-001/R-001
- [ ] Multi-user identity isolation + Authula auth cutover (identity-scoped, NO RBAC) — F-028/R-022
- [ ] ToolGateway (policy + approval + durable mutating-tool ledger) — F-001/F-011/F-020/F-031
- [ ] Runtime profiles + production validation — F-002/F-007/F-008/F-016/F-017/F-022/F-041
- [ ] Agent-loop correctness (terminal exclusivity + HITL resume/pause atomicity) — F-003/F-004/F-005/F-029/F-030
- [ ] MCP governance hardening (transport classifier, trust, lifecycle limits, audited writes) — F-013/F-014/F-027/F-031..F-038/F-046
- [ ] Production observability pack (OTel, alerts, dashboards, readiness, retention) — F-023/F-024/F-048
- [ ] Security & supply-chain (redaction, egress, prompt-injection suite, SBOM, pinned actions) — F-019/F-021/F-036/F-047/F-050/F-051/F-052
- [ ] Scale & operations (load/chaos, backup/DR drill, scheduler drain, topology validation) — F-018/F-035/F-042/F-043
- [ ] Capability evaluation suite + ADRs + release-readiness checklist — F-025/F-026/F-045/F-049
- [ ] All 51 audit findings closed → honest 10/10 production readiness (from 4.6/10)

### Out of Scope

- **Slice 13 (vLLM + LMCache local LLM fallback)** — gated su disponibilità GPU; riprogrammato in v2 quando il bundle DGX Spark sblocca il path. Cumulative idle stimato +5–7 GB RAM.
- **Native Windows runtime** — Aura gira in container (Docker Compose) o contro Docker sidecar. Su Windows solo via Docker Desktop in dev. Memory `feedback_sqlite_wal_windows_corruption.md` + Slice 2 OQ5: named volumes mandatory, no bind-mount Windows.
- **Conversational voice mode** — Aura legge audio in ingresso (STT) e produce voice replies TTS su Telegram (Kokoro, Phase 13). Una modalità voce conversazionale full-duplex resta future milestone.
- **Marketplace skills pubblico** — Slice 7 ha skill installer locale (`skill_install`/`skill_create`); niente registry pubblico, niente sharing skills tra utenti, niente versioning distribuito. Local-first per definizione.
- **Multi-user con RBAC/OAuth reale** — v2.0.0 porta IN scope la **multi-user identity isolation** (store/API owner-scoped per principal autenticato + per-identity sandbox isolation + Authula auth cutover + two-identity E2E proof), risolvendo F-028/R-022. **Resta fuori scope**: RBAC reale con ruoli/permessi (admin vs user), OAuth multi-provider, login multi-tenant SaaS-style. L'authz resta `capability_grants`-based enforced per-route. Forma industriale minima: isolamento dati+processo+fs per identità, non un sistema RBAC completo. (RBAC = candidato post-v2.0.0.)
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

**Stato corrente (post-v1.0.0, 2026-06-29)**

- Codebase Go + un greenfield `web/` (React 19 / Vite 8 / TS 6 / Tailwind 4) il cui `//go:embed all:dist` è baked nel single binary; ~1,800 file toccati e +200k LOC dal close v0.0.0 (incluso il cockpit-overhaul layer non-fase). Owned-surface coverage Go 88.1%, frontend Vitest ≥85% + Stryker ≥70%.
- Auth in transizione: HMAC passphrase cookie (default) ↔ **Authula** embedded (flag-gated `AURA_WEB_AUTH_PROVIDER=authula`) — convergono sullo stesso boundary principal/capability.
- Tech debt noto post-close (6 deferred, vedi STATE.md Deferred Items): 4 GPU-host retrieval live tiers (Phase 30) non eseguibili su questo host 4GB-GPU; 2 live-CI-only frontend tiers (Phase 23); Phase-25 UAT carried-forward nel cockpit-overhaul cutover. Tutti NO-SKIP-AS-GREEN + CI-floored.

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
| Cockpit embedded single-binary (Vite+React+assistant-ui `//go:embed`) sopra AG-UI/SSE | Preserva l'invariante single-binary deploy; nessun server Node nel runtime image | ✓ Good — v1.0.0 (Phase 23–25) |
| assistant-ui `useExternalStoreRuntime` come runtime chat | Mappa l'AG-UI event stream su `ThreadMessage[]` senza forkare il transport; deps exact-pinned | ✓ Good — v1.0.0 (Phase 25) |
| Cockpit palette logo-matched **BLUE** (non l'oro speced) | Operator-accepted 2026-06-18; WCAG-AA re-proven; memory `project_cockpit_palette_deviation_blue_vs_graphite` | ✓ Good — v1.0.0 overhaul |
| **Authula** embedded auth flag-gated (supersedes la passphrase cookie Phase-24) | Go embeddable, capability-per-route, 2FA/OAuth/PG; default resta passphrase | — Pending (cutover in corso) |
| Retrieval rerank = GPU Qwen3-Reranker-0.6B Q4_K_M + two-stage (seed→rerank→expand) | Spike 068/069/070: GPU 333ms vs CPU 23s; fail-soft RRF fallback; Neo4j resta | ✓ Good — v1.0.0 (Phase 30) |
| v1.0.0 chiuso come `override_closeout` (6 deferred GPU/live-CI tiers) | Host 4GB-GPU non esegue server-cuda; harness NO-SKIP-AS-GREEN + CI-floored; budget per-stage già live-proven | — Pending (chiudere su host GPU adeguato) |
| v2.0.0 = major bump (trust-model shift) | Da "single trusted operator full-host" a "per-user isolated full-capability sandbox + production industrialization"; semver-major è il segnale onesto | — Pending (in planning) |
| F-001 risolto via per-user full-capability sandbox, NON fencing | "Keep full-host always" + "10/10" si riconciliano: ogni identità guida una sandbox full-capability isolata (agent-sandbox-class); capability mai rimossa, host reale mai esposto, blast radius contenuto. Allinea memory `feedback_aura_full_host_terminal_primary` | — Pending (research valida K8s/k3s vs pattern-over-rivetdev) |
| Multi-user = identity isolation, NON RBAC | Forma industriale minima richiesta dall'audit (F-028): owner-scoped store/API + per-identity sandbox; RBAC/OAuth restano post-v2.0.0 (memory `feedback_no_atomic_bombs_minimal_industrial_shape`) | — Pending |
| Authula default cutover in v2.0.0 | Multi-user production richiede l'auth provider embeddable capability-per-route; flip default da passphrase (memory `project_authula_multiuser_auth_candidate`) | — Pending |

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
*Last updated: 2026-06-29 — started milestone **v2.0.0 Industrial Hardening & Multi-User Production** (close 51-finding 2026-06-21 audit → honest 10/10; per-user full-capability sandbox + multi-user identity isolation + Authula cutover + ToolGateway + observability/security/ops industrialization; phases 31+). Prior: v1.0.0 Aura Deep Search Web Cockpit SHIPPED 2026-06-29 (9 phases [22–30], 45 plans, 113 tasks; audit PASSED 56/56; override_closeout w/ 6 deferred GPU/live-CI tiers; coverage 88.1%). v0.0.0 Substrate SHIPPED 2026-06-15 (24 phases, 144 plans, 233 tasks; coverage 90.3%).*
