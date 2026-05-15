# Aura Master Plan - Historical Predecessor

> **Status: HISTORICAL — superseded by [prd.md](../prd.md). Preserved as evidence per prd.md §3.2. The 9-phase plan in prd.md is the current authority; the 8-step plan here is the predecessor whose decisions (D1-D13) and wave disposition (§6) are folded into prd.md §16.**

**Do not execute this file as the active plan.** The active route is
`D:/Aura/prd.md` plus `D:/Aura/.planning/deep-refactor/INDEX.md` and the
current handoff files. Use this document only to understand historical decisions
and rejected paths.

**Versione:** 1.1 — Codex article integrated
**Data:** 2026-05-14
**Owner:** solo dev (master-direct)
**Historical inputs integrated at the time:** `docs/aura-restructure-prd.md`
v2, `docs/aura-restructure-prd-REVIEW-1.md`,
`docs/aura-restructure-prd-REVIEW-2.md`,
`D:\tmp\aura-rebuild-strategy.md`, `docs/chat-interface-prd.md` (Wave 3.0),
`.planning/CONTEXT-ENGINEERING-ROADMAP.md`, `.planning/wave1/fix_plan.md`,
`.planning/wave2/fix_plan.md`, `.planning/wave3-agent-swarm/plan.md`.
**References integrated:** `D:\tmp\paper.md` (Kimi K2.5, agent-swarm reference), `D:\tmp\codex.md` (OpenAI — "Unrolling the Codex agent loop", Jan 23, 2026; single-loop default, prompt-cache discipline, MCP list_changed mid-conv trap).
**Status:** HISTORICAL EVIDENCE ONLY. The original executable status is no
longer valid; the 9-phase route in `D:/Aura/prd.md` is authoritative.

**Changelog:**
- v1.0 (2026-05-14) — synthesizer pass; 12 cross-cutting decisions, 8-step path, 22-dir target.
- v1.1 (2026-05-14) — integrated OpenAI Codex agent-loop article (Jan 23, 2026). Decision D9 broadened (Kimi + Codex as paired references); new decision D13 (prompt-cache discipline as policy locked before Step 6); 3 new non-goals (MCP `list_changed` mid-conv, mutate prior messages mid-conv, no Responses API); CONTEXT-ENG Fase 1 promoted to **prereq for Step 6** (kill agent.Runner) rather than INTERLEAVED post-Step 7. No change to Step 1.

---

## 1. Stato attuale verificato (one screen)

**Shipped this session (master, ordine cronologico più recente in alto):**

| SHA | Commit | Effetto |
|-----|--------|---------|
| `c2eb2712` | feat(chathub): silent outbound for heartbeat + cron channels | Slice 0.5 chathub spine, silent channels |
| `d9cd2809` | feat(chathub): web buffered outbound (Router) + ChatService bridge | Web router 192 LOC, bridge 86 LOC — wired ma non in produzione |
| `a0fe64f7` | feat(chathub): Telegram inbound + outbound adapters | **REGRESSIONE** outbound 242 LOC (no CoT, no entity rendering) |
| `39e9da43` | feat(chathub): AgentLoop adapter translates agentruntime events | Translator agentruntime → chathub events (210 LOC) |
| `a48999e3` | feat(agentruntime): event types for streaming + tool lifecycle | Event typing — sano |
| `4ad48390` | chore(config): drop COST_PER_TOKEN legacy env shim | Wave 1.7 cleanup |
| `5d92dc76` | chore(cleanup): drop migrated-out legacy aliases | Wave 1.7 cleanup |
| `a1347a46` | fix(skills): invalidate loader cache on admin install/delete | Wave 1.7 |
| `a4ebe141` | refactor(tools): delete legacy BuildVectorIndex — Reconciler is sole writer | Wave 1 Task 3 + Wave 1.7 |
| `2367f502` | feat(toolindex): Wave 2.10.b — hash-based reconciler | Tool reconciler |
| `eb7e61ad` | feat(install): Wave 2.10 — auto-fetch embedding model | Bootstrap |
| `53a0f6b2` | fix(agentloop): phantom guard — strip code markup + proximity | Memory `feedback_phantom_guard_requires_proximity` |

**Parked (codice committato, traffico zero):**
- `internal/chathub/` (2380 LOC totali: types 159 + hub 251 + agentloop 210 + adapters telegram/web/silent + tests). La spina è sana; **l'outbound Telegram in `adapters/telegram/outbound.go` è una regressione** (perde `🧠 _cot_`, perde `renderForTelegramEntities`, perde fallback entity vs `internal/telegram/streaming.go` 194 LOC).

**Planned-not-started (file `.planning/` ancora `☐`):**
- Wave 1 tasks 1,2,4,5 (RRF fusion, `tool_search` meta-tool, core toolset to 10, `cmd/probe_tools` harness). Task 3 (delete vector router) is **shipped** as `a4ebe141`.
- Wave 2 graph-memory writers (5 tasks, ~1030 LOC, nessuno iniziato).
- Wave 3-agent-swarm (3 Packs A/B/C, ~700-950 LOC, nessuno iniziato).
- Context-Engineering Roadmap Fasi 0-5 (10-17 giorni stimati, nessuna iniziata).

**Stato reale verificato `internal/`:**
- **49 directory** (incluse `orchestration/`, `tracing/`, `release/` **vuote** — nessun `.go`).
- `internal/telegram/` = **8239 LOC** (vero god): setup.go=984, documents_test=626, debug_smoke_test=503, conversation=503, documents=498, atomic_tables=468, scheduler_handlers=430, scheduler_handlers_test=398, entity_markdown_table_test=383, debug_smoke=264, bot=253, bot_test=229, access=226, streaming_test=199, streaming=194 + altri.
- `internal/agent/runner.go` = **520 LOC** (era 555 nel doc PRD; il file è cambiato, ma resta il duplicato di runtime).
- `internal/agentloop/loop.go` = **738 LOC** (canonical primitive).
- `internal/agentruntime/runner.go` = ~266 LOC (canonical envelope).
- `internal/swarm/` 1558 LOC, `internal/swarmtools/` 1089 LOC.

**Frustrazione utente (quote):** _"abbiamo scritto troppo codice in maniera disordinata, serve partire dal core principale"_. Tradotto: tre runtime paralleli, una god class `internal/telegram/` da 8 kLOC, una chathub parcheggiata con outbound regresso, e quattro waves di planning che si contraddicono a vicenda. Il prossimo lavoro deve **non aggiungere superficie** — deve consolidare.

---

## 2. Inputs merged — synthesis matrix

| Doc | Proposta | Status finale nel master plan |
|-----|----------|-------------------------------|
| `docs/aura-restructure-prd.md` v2 | 23 commit / 6 settimane, 3-cut surgery (kill `agent.Runner` → consolida agent → spina chathub) | **PARTIAL** — adottato pattern + ordering di base; effort tagliato, .planning waves esplicitamente riconciliate, swarm-via-Hub rinviato |
| `docs/aura-restructure-prd-REVIEW-1.md` | 4 BLOCKER + 9 MAJOR — picobot LOC budget non equo, `Run.Metadata` keep, F12 fixture harness | **APPLIED** — feature matrix mantenuta, fixture promosso a step esplicito, Run.Metadata keep |
| `docs/aura-restructure-prd-REVIEW-2.md` | 3 BLOCKER + 6 MAJOR — silenzio sui 4 waves `.planning/`, Wave 3 ↔ Commit 2.1 collide | **DECISIVO** — questo è il pezzo mancante che il master plan risolve in §6 |
| `D:\tmp\aura-rebuild-strategy.md` | 3-slice surgery (kill Runner / extract governance / adotta hub) — canonical su "zone di disordine" e file refs | **CANONICAL** sulla diagnosi; sequenza adottata con varianti (governance prima di restructure agent) |
| `docs/chat-interface-prd.md` | Wave 3.0 ChatHub PRD — channel-neutral spine già committata in `internal/chathub/` | **PARTIAL** — la spina è la base; il PRD originale (SSE web, model selector, thread persistence avanzato) resta out-of-scope (§3 PRD v2 conferma) |
| `.planning/CONTEXT-ENGINEERING-ROADMAP.md` | 5 fasi context-eng — Fase 0 baseline, Fase 1 cache, ..., Fase 5 sub-agent `MaxReturnChars` | **DEFERRED** — Fasi 1-3 dopo restructure; Fase 5 INTERLEAVED con Step 4 (kill runner) |
| `.planning/wave1/fix_plan.md` | 5 tasks tool-surface (RRF, tool_search, vector router rm, core 10, probe_tools) | **MIXED** — task 3 SHIPPED; tasks 1,2,4,5 DEFERRED post-restructure |
| `.planning/wave2/fix_plan.md` | Graph memory writers (backlinks, in-mem graph index, entity extraction, multi-page touch, provenance) — 5 tasks 1030 LOC | **DEFERRED** — wiki invariante; `internal/wiki/` non tocca; lavoro INTERLEAVED dopo Step 6 |
| `.planning/wave3-agent-swarm/plan.md` | 3 Pack — Pack A phase/OTel/Telegram, Pack B skill-driven, Pack C propose_patch | **REPLAN** — Pack A si fonde in Step 4 (eventi sui canali post-hub), Pack B+C ripartono dopo Step 7 (swarm-via-Hub) |
| `D:\tmp\paper.md` (Kimi K2.5) | Agent Swarm + PARL + critical-steps | **REFERENCE ONLY** — pattern parallel-decomposition validato; nessuna adozione RL/orchestrator-frozen split |
| `D:\tmp\codex.md` (OpenAI — "Unrolling the Codex agent loop", Jan 2026) | Single flat agent loop pattern; prompt-cache discipline come #1 lever; stateless per-turn; MCP `list_changed` mid-conv trap; mutate-prior-msgs anti-pattern; tool-order stability bug | **REFERENCE** — valida la direzione esistente di Aura (single loop, stateless, reconciler debounce); counterpoint a Kimi K2.5 su parallel-decomposition (default = flat, swarm = opt-in). Strengthens §3 D9 + §6 Context-Eng Fase 1 priority |
| `D:\tmp\picobot` (90 LOC chat + 333 LOC loop) | Gold standard di **shape**, non LOC | **REFERENCE** — usato per validare boundary chat↔agent, non per budget LOC (review F3 valida disclaimer) |
| `D:\tmp\nanobot` | Two-layer memory + autocompact module separate | **PATTERN** — conferma "governance estratta dal loop"; nessun import codice |

---

## 3. Decisioni cross-cutting (la brainstorm)

Ogni decisione: domanda → opzioni con sponsor → vincitore → razionale tagliente → costo se sbagliato. Decisioni accoppiate marcate.

### D1. `agent.Runner` — cancellare o estendere?

| Opzione | Sponsor | Pro | Contro |
|---------|---------|-----|--------|
| (A) **Cancellare ora** (route swarm + chatPipe + scheduler `agent_job` su `agentruntime.Run`) | PRD v2 §6 Commit 2.1, strategy doc Slice 1 | -520 LOC, single runtime, `/api/chat` ottiene streaming/permissive/events, REUSABLE-CODE invariante onorato | Wave 3 Pack A prevede `PhaseSink` proprio in `runner.go`; Context-Eng Fase 5 vuole aggiungere `MaxReturnChars` a `agent.Task` |
| (B) **Estendere e mantenere** (PhaseSink in runner, raise iter cap, propose_patch) | Wave 3 plan Task 3.1/3.3, Context-Eng Fase 5 | Nessun lavoro perso su Wave 3; mapping table evitata | Cementa il duplicato; nuovo lavoro nasce sul runtime sbagliato; Wave 3 Pack A finisce per duplicare phase-events che `agentruntime.Event` già supporta |
| (C) Cancellare ma rimandare swarm | Compromesso | Killa il driver primario (chatPipe + scheduler) | Lascia swarm sul vecchio runtime — non risolve nulla |

**Vincitore: A — cancellare.** Razionale: il runtime canonico per design è `agentruntime`+`agentloop`. `agent.Runner` esiste perché nessuno l'ha mai sostituito. Wave 3 Pack A è il sintomo di una scelta sbagliata: stiamo per investire 350-450 LOC per dare a `runner.go` capacità che `agentruntime` ha già (`Event{ToolStart,ToolEnd,LLMDelta,Final}`). Pack A si **riscrive** come "swarm emette `chat.OutboundEvent{phase:...}` sul Hub" (~80 LOC, non 450). Costo se sbagliato: 1 giorno di revert + riportare swarm + scheduler sul vecchio path. Accettabile.

**Accoppiamento:** D1 forza D8 (Wave 3 Pack A è SUPERSEDED).

### D2. Restructure-first o feature-first?

| Opzione | Sponsor | Pro | Contro |
|---------|---------|-----|--------|
| (A) Restructure prima (Wave 1/2/3 in pausa) | PRD v2, strategy doc | Lavoro nuovo nasce sulla shape giusta; debito interest fermato | 4-6 settimane senza feature visibili; rischio user frustration |
| (B) Feature-first (chiudere Wave 1, poi 2, poi 3, poi restructure) | Implicito in `.planning/` | User-visible progress immediato | Wave 3 Pack A scrive 450 LOC su `runner.go` che poi cancelliamo; rilavoro garantito |
| (C) **Surgical-first** (kill duplicate runtime + chathub merge, poi le waves residue) | Strategy doc Slice 1-3 + sintesi REVIEW-2 | Niente rilavoro: tutte le waves dopo lo step partono dalla shape giusta; Wave 1 residue (tasks 1,2,4,5) ortogonali al restructure | Step 1-3 sono pre-requisito blocking per Wave 2/3 — niente "ship some feature now" |

**Vincitore: C — surgical-first, scope tagliato.** Razionale: PRD v2 propone 23 commit / 6 settimane di restructure prima di qualsiasi feature. Strategy doc propone 3 slice (~2-3 settimane). Il master plan adotta **3 slice surgical** + 1 governance extraction + 1 swarm-via-Hub, per **8 step totali ~3-4 settimane**, dopo le quali Wave 1 residue + Wave 2 + Wave 3 Pack B/C partono già allineate. Wave 3 Pack A SUPERSEDED (vedi D8). Costo se sbagliato: user vuole ship-now e percepisce 4 settimane di restructure come "altro disordine". Mitigazione: ogni step è user-verifiable (G-criterion 1-liner) e atomico (revert-safe).

### D3. Package count target

| Opzione | Sponsor | Pro | Contro |
|---------|---------|-----|--------|
| 9 (picobot) | REVIEW-1 disclaimer F3 | Massima leggibilità | Aura ha features che picobot non ha (phantom guard 380 LOC, microcompact 250, entity rendering 84) — non comprimibile |
| **22** (PRD v2 §4.1) | PRD v2 | Aritmetica verificata 49→22; sub-package mantengono dominio | Stretch — backup top-level vs storage/backup costa 1 dir |
| 25-28 (REVIEW-1 stima conservativa) | REVIEW-1 F5 | Realistico | Lascia troppe directory single-test (orchestration/tracing/release) |

**Vincitore: 22 (stretch 21).** Razionale: 49 → 22 è raggiungibile con merge meccanici verificati (vedi §4 Step 1+2). Empty `orchestration/`, `tracing/`, `release/` sono **delete free**. I 5 merge-pack `agent/*`, `mcp/*`, `storage/search/*`, `storage/sources/*`, `agent/tools/*` riducono di 14. `config/*`, `api/*`, `db/*`, `session/*` di altri 7. Costo se sbagliato: 23-25 invece di 22 — non rompe nulla.

### D4. chathub layer — keep, integrate, delete?

| Opzione | Sponsor | Pro | Contro |
|---------|---------|-----|--------|
| Keep parked | Status quo | Zero rischio | 2380 LOC inutilizzate continuano a divergere |
| **Integrate now** (rinomina `chat/`, adopt come spina) | PRD v2, strategy doc Slice 3, chat-interface PRD | Spina canonica; web e silent funzionano; abilita swarm-via-Hub | Outbound Telegram è regressione — DA RIPORTARE da `streaming.go` |
| Delete | Reazione conservativa | Cancella debt | Butta 4 commit shipped questa session; lascia `/api/chat` su path inferiore |

**Vincitore: Integrate now.** Razionale: la spina (`types.go`+`hub.go`+`agentloop.go` = 620 LOC) è sana — REVIEW-1 F2 ha verificato e ridimensionato il budget LOC. Web router (192) + silent (77) + chat_service (86) sono OK. Solo `adapters/telegram/outbound.go` (242) è da sostituire con port di `streaming.go`. Costo: ~1 giorno di port + fixture harness. Costo se sbagliato: regressione CoT/entity Telegram in produzione → utente la nota in 1 messaggio (mitigato da feature-flag `AURA_USE_HUB`).

### D5. chathub Telegram outbound regression — delete now o dopo?

| Opzione | Sponsor | Pro | Contro |
|---------|---------|-----|--------|
| Delete now (Step 2) | — | Pulizia immediata | Spina perde un adapter; web rimane via hub, telegram resta su path legacy |
| **Replace prima di adoption** (Step 3-4) | PRD v2 Commit 4.3 | Sostituzione atomic; nessuna finestra di regressione | Costa un fixture harness (REVIEW-1 F12) |
| Lasciare e adoptarlo | Mai sponsorizzato | Lazy | Regressione user-visible CoT/entity — NO |

**Vincitore: Replace prima di adoption.** Razionale: il file 242-LOC regresso esiste, ma **nessun caller in produzione lo invoca**. Lo si sostituisce in Step 4 portando `streaming.go` come `channels/telegram/outbound.go`. Il fixture harness (Step 3) garantisce byte-comparison pre/post. Costo se sbagliato: 1 ciclo di edit + revert. Bassissimo perché lo step ha snapshot diff come gate.

### D6. .planning waves — superseded, deferred, interleaved?

Decisione granulare in §6. Sintesi qui:

- **Wave 1 task 3** (delete vector router): SHIPPED.
- **Wave 1 tasks 1,2,4,5** (RRF, `tool_search` tool, core to 10, probe_tools): DEFERRED post-restructure. Ortogonali al restructure: toccano `internal/search/`, `internal/memoryindex/`, `internal/tools/` — tutti packages che il restructure rinomina ma non modifica logicamente. Re-pointare i file refs è meccanico.
- **Wave 2 graph-memory** (5 tasks): DEFERRED post-restructure. `internal/wiki/` invariante (memory: wiki IS graph). `internal/ingest/` si sposta in `storage/sources/ingest/` ma il piano Wave 2 va rifatto su path nuovi — è 1 ora di sed.
- **Wave 3 Pack A** (phase/OTel/Telegram progressive): **SUPERSEDED**. Le sue capacità si fondono in Step 4 + 7 come `chat.OutboundEvent{Type: "phase", Payload: {...}}` su tutti i canali, gratis dal Hub.
- **Wave 3 Pack B** (skill-driven orchestrator): DEFERRED. Indipendente dal restructure — è skill + 80 LOC di plumbing. Si fa dopo Step 8.
- **Wave 3 Pack C** (propose_patch + sanitization + TTL): DEFERRED. Reuse di `proposed_updates` confermato. Path file in `swarmtools/` → post-restructure `agent/tools/swarmtools/`.
- **Context-Eng Roadmap Fasi 0-5**: DEFERRED. Fase 0 (baseline) può essere INTERLEAVED ora (zero codice, solo log scrape). Fase 1.1 (prompt cache reordering) tocca `conversation.go:261` che il restructure preserva — può essere INTERLEAVED se utente vuole sit-back impatto. Fasi 2-5 dopo.

### D7. Hub responsabilità — thick o thin?

| Opzione | Sponsor | Pro | Contro |
|---------|---------|-----|--------|
| Thin (push tutto su agent + channels) | Picobot 90-LOC `chat.go` | Massima testabilità | Aura ha event taps, `/stop` registry, `Run.Metadata`, fan-out — features che il Hub deve incarnare |
| **Thick ≤700 LOC** | PRD v2 §5.6 v2 | Spina porta dispatcher + runIDs + `/stop` + fan-out, niente più | Più grande di picobot ma per buone ragioni |
| Thick + memory + governance | — | Riduce agent | Crea un nuovo god-class; viola single responsibility |

**Vincitore: Thick ≤700 LOC**, già verificato (hub 251 + types 159 + agentloop 210 = 620). Razionale: features REVIEW-1 F2 ha documentato — 7 EventType cases, dispatcher con event-tap, run/event ID minting, `/stop` registry. Non riducibile sotto 600 senza droppare features. Costo: budget rispettato; se cresce sopra 700, refactor.

### D8. Swarm — first-class channel o tool?

| Opzione | Sponsor | Pro | Contro |
|---------|---------|-----|--------|
| First-class channel (`Channel: swarm`, silent outbound) | PRD v2 §4.4 | Sub-task ereditano governance + permissive + event-taps; phase events arrivano gratis (Wave 3 Pack A SUPERSEDED) | Richiede `swarmHubBridge` (~40 LOC) |
| **Tool che chiama `agent.Run`** | Implicito in `agent.Runner` legacy | Status quo | Duplicato runtime; phase events richiedono lavoro custom (Wave 3 Pack A 450 LOC) |

**Vincitore: First-class channel.** Razionale: D1 cancella `agent.Runner`; D8 chiude il cerchio. Swarm sub-task = `InboundMessage{Channel: swarm, Mode: silent, ChannelData: {parent_run_id, assignment_id}}`. Il Manager raccoglie i risultati via `Router.WaitForRun(runID)`. Wave 3 Pack A si fonde nello stream eventi del Hub. Costo se sbagliato: 1 giorno di revert (swarm torna a chiamare `agent.Run` direttamente).

### D9. External references (Kimi K2.5 + OpenAI Codex) — citation depth?

| Opzione | Sponsor | Pro | Contro |
|---------|---------|-----|--------|
| Driver | Nessuno seriamente | Trendy | Aura non fa RL (Kimi); Aura non usa Responses API (Codex); architectural specifics non-portabili |
| Architectural | PRD v1 | Validazione esterna | REVIEW-1 F7 ha smontato la citazione Kimi; Codex usa Responses API stateful-optional, non OpenAI-compat chat completions |
| **Reference only — citare entrambi** | PRD v2 §4.5, REVIEW-2 F12 + Codex Jan 2026 | Onesto + due punti di vista complementari | Niente — è la verità |

**Vincitore: Reference only — cita entrambi.**

- **Kimi K2.5** (`D:\tmp\paper.md`): "Agent Swarm riduce latency 4.5× in wide-search". Pattern di **parallel decomposition** valido per task decomponibili (wide search, multi-source synthesis); NON è il default loop shape.
- **OpenAI Codex** (`D:\tmp\codex.md`): "Single flat agent loop" — user input → inference → tool calls → loop until assistant message. NO nested orchestration di default. È il default per software-agent + personal-agent loops. È **ciò che Aura È e deve restare**.

**Convergenza:** Aura conferma single-loop come default (`internal/agent/loop.go`, post-Step 5 `internal/agent/runtime.go`); swarm resta **tool opt-in** (`swarmtools.delegate`) per task decomponibili, NON sostituto del loop principale. Nessuna citazione architetturale di driver; entrambi i doc citati in §10 con rationale "reference".

Aura implementa orchestrator-worker dal codice esistente (Wave 3 plan line 14), non dal paper Kimi. Aura implementa flat-loop dal codice esistente (`agentloop.Run`), non dal Codex post.

### D13. Prompt-cache discipline — feature wave o config-engineering policy?

| Opzione | Pro | Contro |
|---------|-----|--------|
| Feature wave (Fase 1 dopo Step 8) | "Lo fa dopo restructure" | Step 4-6 toccano prompt assembly senza policy lock → cache-miss regression invisibile |
| **Policy locked BEFORE Step 6** | Lock invariante prima del touch; ogni step fa diff-check | Sposta 4-6h context-eng dentro la finestra restructure |
| Skip | — | Quadratic cost; user paga in latency |

**Vincitore: Policy locked BEFORE Step 6.** Codex (Jan 2026) è esplicito: *"place static content...at the beginning of your prompt, and put variable content...at the end. This also applies to images and tools, which must be identical between requests."* Cache hits → sampling linear, non quadratic. Step 4 + Step 6 toccano prompt assembly; senza lock, regression invisibile.

**Invarianti da lockare prima di Step 6:**
1. **Static-first prefix**: system → AGENT.md/SOUL.md overlay → tool defs → history → runtime context (cwd, datetime).
2. **Tool order stability**: `tools/registry.go` ordering DETERMINISTIC (Codex PR #2611 ha fixato esattamente questo).
3. **Append-not-mutate runtime ctx**: cwd/time/permission change → append `role=user` msg, non mutare il prior.

CONTEXT-ENG Fase 1 promosso a prereq di Step 6 (vedi §6). Costo se sbagliato: cache hit <50% → p95 raddoppia. Gauge: prompt token reuse ratio (Fase 1.2 telemetria).

### D10. Effort horizon — sprint o campagna?

| Opzione | Pro | Contro |
|---------|-----|--------|
| Sprint 1-2 settimane | Visibile, motivante | Non basta per restructure + waves |
| **Campagna corta 3-4 settimane di restructure + waves residue parallele/seguenti** | Realistico, accomodano frustrazione utente | Richiede disciplina sul "non aggiungere features durante restructure" |
| Campagna 6-8 settimane (PRD v2 estimate) | Conservativo | Demotivante; troppo lungo per solo dev |

**Vincitore: Campagna corta.** Step 1-8 in §4 = ~70 ore produttive = ~17 giorni produttivi a 4h/d = ~4 settimane calendar. Wave 1 residue + Wave 2 + Wave 3 Pack B/C dopo. Context-Eng Fasi 0+1 INTERLEAVED. Costo se sbagliato: slip di 1-2 settimane — riassorbito.

### D11. Streaming.go port — verbatim o rewrite?

| Opzione | Pro | Contro |
|---------|-----|--------|
| Verbatim | Zero rischio regressione | Restano helper duplicati con bot.go |
| **Verbatim + thin wrapper** (consumeStream diventa `chat.OutboundAdapter.Deliver`; helpers `tele.Bot` privati restano in channels/telegram) | Zero comportamento cambiato + fixture harness lo dimostra | 30 LOC di wrapper |
| Rewrite | Pulizia | Rischio CoT/entity regression — NO |

**Vincitore: Verbatim + thin wrapper.** Razionale: lo streaming.go è production-tested. Il fixture harness (Step 3) cattura snapshot pre-port; lo step 4 confronta byte-per-byte post-port. Costo se sbagliato: il fixture dà errore — revert.

### D12. Composition root — `cmd/aura/app.go` o `telegram/setup.go`?

| Opzione | Pro | Contro |
|---------|-----|--------|
| Stay in `telegram/setup.go` | Zero refactor | 984-LOC file con 32 import interni continua a essere god — CLAUDE.md GOD CLASS violato |
| **Move to `cmd/aura/app.go`** (split in `wire_*.go`) | Onora "telegram is only an adapter"; composition root visible | 32-48 ore lavoro (lo step più rischioso) |

**Vincitore: Move to `cmd/aura/app.go`**, ma **ultimo step** (Step 8). Razionale: troppi prerequisiti — Hub adopted, channels migrated, swarm via Hub — prima di poter splittare ownership di `*Bot`. PRD v2 Phase 7 ha sviluppato il pattern (7.1 wire funcs, 7.2 goroutine ownership, 7.3 field drop). Lo adottiamo. Costo se sbagliato: race condition su shutdown (alta probabilità senza `-race` test) — gate è `go test -race ./...`. Se Step 8 fallisce 3 volte, congelare. Lo step è facoltativo per chiudere il piano — i passi 1-7 lasciano il sistema funzionante e migliore di oggi anche senza Step 8.

---

## 4. La strada vincente — sequenza ordinata

Ogni step: ≤1 commit logico (può essere split in sub-commit interni), atomico, revert-safe, build verde. Numerazione lineare. Stima 4h/d produttiva.

### Step 1 — `chore(internal): cleanup empty + merge mcp + storage + config + api + db`

- **Cosa:** Delete `internal/orchestration/`, `internal/tracing/`, `internal/release/` (zero `.go`). Merge `mcppolicy + mcpwatch → mcp/`, `search + qdrant + reindex + memoryindex + memoryquality → storage/search/`, `source + ocr + ingest + markitdown → storage/sources/`, `settings + runtimebootstrap → config/`, `auth + setup + health → api/`, `dbrecovery + debugguard → db/`. 6-7 sub-commit atomici (un merge alla volta). Sed `import` paths.
- **Why now:** zero dipendenze su altro; abbassa il count da 49 a ~28; rende i `find` successivi più rapidi.
- **Effort:** 8h (1.5h × 6 merge meccanici + 2h sed/test).
- **Rischio:** Low. Ogni merge è `git mv` + sed import; le test suite continuano verdi se il sed è corretto.
- **Validation gate:** `go build ./...` + `go vet ./...` + `go test ./...` verdi dopo OGNI sub-commit. `(Get-ChildItem internal -Directory).Count` ≤ 30.
- **Rollback:** `git revert` clean.
- **Acceptance:** [ ] count `internal/` ≤ 30; `go list -deps ./...` non emette `import cycle`.
- **Disposition planning:** none affected.

### Step 2 — `chore(chat): rinomina internal/chathub → internal/chat + sposta adapters → internal/channels/`

- **Cosa:** `git mv internal/chathub internal/chat`. `git mv internal/chat/adapters internal/channels`. Sub-cartelle `channels/telegram/`, `channels/web/`, `channels/silent/`. Rename package declarations. `delete internal/chathub/adapters/telegram/outbound.go` — la regressione esce dal tree (verrà ricostruita in Step 4).
- **Why now:** prepara naming finale; consente Step 3-4 di toccare un solo path canonico. Channels diventano una directory dedicata, non più sotto chat/.
- **Effort:** 3h.
- **Rischio:** Low-Med (40+ file import path + tests). `goimports -w` post-sed.
- **Validation gate:** `go build ./... && go test ./...` verdi. `Grep -rn 'chathub' internal/ cmd/` → 0 match.
- **Rollback:** `git revert`.
- **Acceptance:** [ ] `internal/chathub/` non esiste; `internal/chat/` + `internal/channels/` esistono; build verde.
- **Disposition planning:** none affected.

### Step 3 — `test(channels/telegram): record-and-replay fixture per streaming edits`

- **Cosa:** Crea `internal/channels/telegram/fixture/` con `mockBot.go` (impl `tele.API` registrante Send/Edit/SendDocument), helper `Capture(t, scenario, builder)` che esegue `streaming.go::consumeStream` corrente, snapshot in `testdata/<scenario>.json`. 3 scenari: `simple_reply`, `with_cot`, `with_tool_call_and_entity_table`.
- **Why now:** prerequisite per Step 4 byte-comparison. Senza il fixture, l'edit di Step 4 non ha un gate.
- **Effort:** 8h (REVIEW-1 F12 promosso a step esplicito).
- **Rischio:** Med — `tele.Context` ha campi unexported; shim costruisce un context valido senza `tele.Bot` reale.
- **Validation gate:** `go test ./internal/channels/telegram/fixture/...` verde; 3 snapshot JSON committed.
- **Rollback:** `git revert` clean.
- **Acceptance:** [ ] 3 file `testdata/*.json` con sequenza Send/Edit deterministica.
- **Disposition planning:** abilita Step 4.

### Step 4 — `refactor(channels/telegram): porta streaming.go come outbound, delete chathub regressed outbound (era già fatto in Step 2), wire Web Hub adapter + feature flag`

- **Cosa:** (a) porta `internal/telegram/streaming.go::consumeStream` come `internal/channels/telegram/outbound.go` implementando `chat.OutboundAdapter`. Preserva 600ms throttle, CoT (`🧠 _cot_`), `renderForTelegramEntities`, entity fallback. (b) Wire `internal/api/chat.go` via `webch.NewInbound + chat.Hub.ReceiveMessage` dietro env flag `AURA_USE_HUB=false` (default). Telegram resta sul path legacy. (c) Collapse rule documentata: `ChatReply.tokens = Prompt + Completion`.
- **Why now:** abilita `/api/chat` su path canonico in modo non-disruptive. Telegram preserva il path attuale.
- **Effort:** 12h.
- **Rischio:** Med (web hub adopts) + High (telegram outbound port — il gate è il fixture di Step 3).
- **Validation gate:** byte-diff snapshot Step 3 = empty; `cmd/probe_chat` verde con `AURA_USE_HUB=true` (web path); `cmd/probe_chat` verde con `AURA_USE_HUB=false` (legacy path).
- **Rollback:** `git revert`. Web torna al pipeService legacy.
- **Acceptance:** [ ] fixture-byte-diff = 0; probe chat verde su entrambi i flag values; CoT visibile sul Telegram client reale (manual smoke).
- **Disposition planning:** **Wave 3.0 chathub Slice 0 docs → archiviare.** Web path canonical-by-design.

### Step 5 — `refactor(agent): extract governance + merge agentloop+agentruntime → internal/agent`

- **Cosa:** (a) Estrai `agentloop.applyGovernance` + `MicrocompactPolicy` + `ToolResultLimitPolicy` in `internal/agent/governance/`. `conversation.Context.EnforceLimit` e `CompactCompletedToolResults` chiamano `governance.Apply`. Table tests `governance_test.go`. (b) `git mv internal/agentloop/* internal/agent/loop.go`. `git mv internal/agentruntime/* internal/agent/runtime.go`. `agentruntime.Run → agent.Run`. Sed import paths.
- **Why now:** isola la logica del loop dalla `Context` struct; testabile a sé. Prerequisite per Step 6 (kill runner) — dimostriamo che il loop primitive è autosufficiente.
- **Effort:** 12h.
- **Rischio:** Med — molti import path, governance microcompact può driftare. Table-test mitigation.
- **Validation gate:** `go test ./... -race`. `governance_test.go` copre microcompact KeepRecent + ToolResultLimit + edge cases.
- **Rollback:** `git revert`.
- **Acceptance:** [ ] `internal/agentloop/` e `internal/agentruntime/` non esistono; `internal/agent/{loop,runtime,governance/}` esistono; race-test verde.
- **Disposition planning:** **Wave 3 Pack A SUPERSEDED** — i phase events si traducono come `agent.Event` ⇒ `chat.OutboundEvent`. **Context-Eng Fase 3.3 (unificare microcompact)** PARALLELO: la governance estratta è il posto giusto per Fase 3.3 — può essere fatto qui o subito dopo.

### Step 6 — `refactor(agent): kill agent.Runner — route swarm + chatPipe + scheduler agent_job to agent.Run`

- **Cosa:** Crea `internal/agent/no_stream_client.go` (~30 LOC, wrappa `llm.Client.Send` per `agent.ChatClient`). `chatPipeService` (`internal/telegram/chat_service.go` → `internal/channels/web/inbound_bridge.go`) chiama `agent.Run`. `swarm.Manager.runner` chiama `agent.Run`. `internal/scheduler/agent_job.go` re-targetta su `agent.Run`. **DELETE** `internal/agent/runner.go`, `runner_test.go`, README. Field mapping table `agent.Task → agent.Invocation/agentloop.Options` (presa da PRD v2 Commit 2.1).
- **Why now:** D1 cancella il duplicato. È **lo step che killa più LOC** (-520 + tests).
- **Effort:** 10h (mapping verification, swarm test, scheduler agent_job test).
- **Rischio:** Med-High — Task.MaxToolCalls vs MaxIterations semantica diversa (REVIEW-1 F9): `MaxIterations = MaxToolCalls + 1`, `MaxToolCalls=0 → MaxIterations=0` (unlimited).
- **Validation gate:** `go test -race ./internal/swarm/... ./internal/scheduler/... ./internal/api/...`. `cmd/probe_chat` verde. `Grep -rn 'agent\.Runner|NewRunner\(' internal/ cmd/ -g '!*_test.go'` → 0 match.
- **Rollback:** `git revert` clean — Runner torna in vita.
- **Acceptance:** [ ] `internal/agent/runner.go` non esiste; swarm test verde; probe_chat verde; `internal/agent/` totale ≤1700 LOC.
- **Disposition planning:** **Context-Eng Fase 5 INTERLEAVED** — il field mapping è il posto giusto per Fase 5 (Task.MaxReturnChars + ArtifactPath). Aggiungere `Invocation.MaxReturnChars` (default 2000) + `Invocation.ArtifactPath` (optional). Tagliato a 1h dentro questo step.

### Step 7 — `refactor(swarm,cron): swarm via chat.Hub + rename scheduler → cron + agent/tools consolidation`

- **Cosa:** (a) Nuovo `chat.Channel = "swarm"` + `silentch.NewSwarm`. `swarmHubBridge` (~40 LOC) impl `swarm.AgentRunner` traducendo `agent.Task → chat.InboundMessage{Channel: swarm}`. Output via `Router.WaitForRun(runID)` (~20 LOC helper). (b) `git mv internal/scheduler internal/cron`. Sub-cartelle `cron/jobs/`, `cron/maintenance/`, `cron/tick/`. Cron tick produce `InboundMessage{Channel: cron, Mode: silent}`. (c) Merge `tools + toolindex + toolsets + swarmtools → internal/agent/tools/*` (4→1). (d) `git mv internal/agentruntime/sessions internal/session/store.go`. `git mv internal/concurrency internal/session/gate.go`. (e) Wire Telegram dietro flag `AURA_USE_HUB=true` (manteniamo legacy path con flag off finché Step 8 non finisce).
- **Why now:** completa la transizione di tutti i caller produttivi al Hub. Tutti i fan-out di event/phase emergono gratis dal canale.
- **Effort:** 16h (8h swarm via hub + 4h tools merge + 2h cron rename + 2h session).
- **Rischio:** Med (Telegram hub-wire è il pezzo high-risk; mitigato dal flag).
- **Validation gate:** `go test ./... -race`. Nuovo test `internal/swarm/hub_e2e_test.go`: parent run + 3 child run, verifica `parent_run_id`. Probe Telegram con `AURA_USE_HUB=true`: CoT preservato (visual check) + fixture byte-diff = 0.
- **Rollback:** flag off → torna su legacy. Revert per nuovo Channel.
- **Acceptance:** [ ] `internal/scheduler/` non esiste; `internal/cron/` con sub-cartelle esiste; swarm e2e test verde; flag `AURA_USE_HUB=true` non rompe niente in produzione 48h.
- **Disposition planning:** **Wave 3 Pack A definitivamente SUPERSEDED**. **Wave 3 Pack B/C ora possono partire su `internal/agent/tools/swarmtools/`**. **Wave 2 task 2.3-2.4** (ingest pipeline) tocca `internal/storage/sources/ingest/` — path stabile.

### Step 8 (opzionale ma pianificato) — `refactor(app): extract composition root → cmd/aura/app.go + shrink internal/telegram/`

- **Cosa:** (a) Crea `cmd/aura/app.go` (≤300 LOC top-level orchestration) + `wire_storage.go`, `wire_agent.go`, `wire_channels.go`, `wire_api.go`, `wire_mcp.go` (ognuno ≤300 LOC). (b) Sposta corpo di `internal/telegram/setup.go::New` in wire functions. `*Bot` mantenuto temporaneamente. (c) Sposta `bgCtx/bgCancel/bgWg` ownership da `*Bot` a `*App`. Goroutines (reconciler, mcpwatch) ricevono `app.bgCtx`. `go test -race ./...` HARD GATE. (d) Drop 32 dei 38 campi di `*Bot` (lasciare bot, cfg, loc, logger, api, started). Delete `internal/telegram/{conversation,streaming,chat_service,setup,conversation_*}.go`. (e) Drop flag `AURA_USE_HUB` dopo 48h soak.
- **Why now / why last:** prerequisite Step 7 (Hub adopted everywhere). Lavoro ad alto rischio race; si fa con il flag ancora wireable per rollback.
- **Effort:** 32-40h (split in 4 sub-commit con gate indipendenti: 8a wire funcs, 8b goroutine ownership, 8c field drop, 8d flag drop).
- **Rischio:** **High** — race condition shutdown, last-mile cleanup test files.
- **Validation gate:** `go test -race ./...` zero failure su ogni sub-commit. Manual soak 48h. `wc -l cmd/aura/*.go` ciascun file ≤300. Somma `internal/telegram/{bot,commands,handlers}.go` ≤200.
- **Rollback:** sub-commit-wise revert. Se 8b fallisce, revert 8b mantenendo 8a.
- **Acceptance:** [ ] `internal/telegram/setup.go` non esiste; `cmd/aura/app.go` esiste; `(Get-ChildItem internal -Directory).Count` ≤ 22.
- **Disposition planning:** chiude il restructure. Da qui Wave 1 residue / Wave 2 / Wave 3 Pack B+C partono.

### Calendario sommato

| Step | Effort (h) | Calendar (4h/d) | Acceptance gate |
|------|-----------|-----------------|------------------|
| 1 — cleanup + merge meccanici | 8 | 2d | count ≤30 |
| 2 — rename chathub→chat + channels | 3 | 1d | no chathub refs |
| 3 — fixture harness | 8 | 2d | 3 snapshots |
| 4 — Telegram port + web hub flag | 12 | 3d | byte-diff=0 |
| 5 — extract governance + merge agent | 12 | 3d | race test verde |
| 6 — kill agent.Runner | 10 | 2.5d | 0 Runner refs |
| 7 — swarm/cron via Hub + tools merge | 16 | 4d | swarm e2e + 48h soak flag-on |
| 8 — composition root + telegram shrink | 32 | 8d | 22 dirs + ≤200 LOC tg |
| **Totale** | **~101h** | **~26d ≈ 5 settimane calendar** | |

Soak time addizionale: 48-72h al flag-flip di Step 7 + 48h di Step 8d = ~5 giorni passivi.

**Stop-and-reassess:** se Step 6 fallisce 3 volte → STOP, lasciare `agent.Runner` in vita, chiudere il piano a Step 5 (i benefici di Step 1-5 sono comunque grossi). Se Step 8 fallisce → chiudere a Step 7 (composition root extraction è la ciliegina, non il cuore).

---

## 5. Cosa NON facciamo (non-goals con citazioni)

| Non-goal | Origine vincolo |
|----------|------------------|
| No nuovo graph DB (KuzuDB/Neo4j/Zep) | `memory/project_graph_memory_core_strategy.md` — wiki IS the graph |
| No cambio embedding backend (embeddinggemma-300m locked) | `memory/feedback_embedding_backend_stays_mistral.md` body |
| No GPU embedding | `memory/feedback_gpu_not_for_embedding_workload.md` |
| No green-field rewrite | `D:\tmp\aura-rebuild-strategy.md` §7 + REVIEW-1 disclaimer |
| No nuove dipendenze Go | PRD v2 G10 |
| No React/UI rework | PRD v2 §3 non-goal #6 |
| No SSE streaming web | PRD v2 §3 non-goal #8 (separate phase post-restructure) |
| No mem0 ADD-only extractor (Slice 4 of strategy doc) | Out of scope di questo plan — separate phase |
| No multi-tier LLM / mini-LLM per tool retrieval | `memory/feedback_minillm_cpu_not_viable_for_tool_retrieval.md` |
| No regex su NLP / contenuti LLM | `memory/feedback_no_regex_for_nlp.md` |
| No model catalog dinamico / per-thread model selector | chat-interface PRD §3 |
| No RL / PARL / orchestrator-frozen | Paper-driven adoption rejected (D9) |
| No restructure di `internal/wiki/` | `memory/project_graph_memory_core_strategy.md` |
| No feature branches / PR ceremony | `memory/feedback_master_direct_workflow.md` |
| No `git push` non richiesto | CLAUDE.md GIT PUSH DISCIPLINE |
| No modifiche ai test per farli passare | CLAUDE.md NEVER MODIFY TESTS |
| No file >600 LOC (cmd/aura/wire_*.go ≤300 each) | CLAUDE.md GOD CLASS |
| No `propose_patch` o Wave 3 Pack C durante Step 1-7 | D1 + D8 — Wave 3 Pack C parte post-Step 7 |
| No new top-level package senza migration plan | implicit dalla regola ≤22 |
| No `Run.Metadata` removal | PRD v2 §5.6 v2 (REVIEW-1 F4 — costa 1 giorno separato, out of scope) |
| No honoring MCP `notifications/tools/list_changed` mid-conversation | Codex article (Jan 2026) — cache-miss trap esplicito. Aura's reconciler debounce 500ms (Wave 2.10.b) è OK perché NON propaga tool list changes nel `tools` field di una conversazione attiva. Wave 2.10.c (MCP reload) deve mantenere questa invariante: la nuova tool list entra in prompt SOLO al prossimo turn boundary, mai mid-turn |
| No mutating prior system/developer/user messages mid-conversation | Codex article (Jan 2026) — when config changes (cwd, time, permissions, runtime context), append NEW `role=user` o `role=developer` message; do NOT modify prior messages. Cache-prefix discipline. Aura's `RenderRuntimeContext` follow this — relevant to CONTEXT-ENG Fase 1 Task 1.1 (vedi §3 D13) |
| No usare Responses API (`/responses`, `/responses/compact`, `previous_response_id`) | Aura è OpenAI-compat chat completions only. Codex article descrive Responses API; pattern (stateless, compact) sono mappabili al modello Aura, ma le endpoint specifiche NON sono. Compaction analog = `internal/agent/governance/` (Step 5) + conversation summarizer (`internal/conversation/`) |

---

## 6. .planning waves — disposition per wave

| Wave / Fase | Status | Master plan step che la risolve | Note |
|-------------|--------|---------------------------------|------|
| Wave 1 task 1 (RRF fusion in `mergeDocuments`) | DEFERRED post-Step 7 | post-Step 7 | tocca `storage/search/memoryindex/store.go:268-296` (path post-restructure); 80 LOC; ortogonale |
| Wave 1 task 2 (`tool_search` meta-tool) | DEFERRED post-Step 7 | post-Step 7 | post `agent/tools/` consolidation (Step 7c) — 1h sed per path refs |
| Wave 1 task 3 (delete vector router) | **SUPERSEDED — SHIPPED** | commit `a4ebe141` | archive `.planning/wave1/fix_plan.md` task 3 con stub "shipped" |
| Wave 1 task 4 (core toolset to 10) | DEFERRED post-Step 7 | post-Step 7 | file ref: `internal/telegram/tools_provider.go:24-31` → diventa `cmd/aura/wire_agent.go` o equivalente in Step 8 |
| Wave 1 task 5 (cmd/probe_tools harness) | DEFERRED post-Step 7 | post-Step 7 | nuovo file `cmd/probe_tools/main.go`; gate per Recall@5 ≥85% golden set |
| Wave 2 task 2.1 (backlink maintenance in WritePage) | DEFERRED post-Step 7 | post-Step 7 | `internal/wiki/store.go` non si tocca durante restructure; lavoro post-restructure |
| Wave 2 task 2.2 (in-memory graph index) | DEFERRED post-Step 7 | post-Step 7 | nuovo `internal/wiki/graph_index.go` |
| Wave 2 task 2.3 (entity extraction LLM) | DEFERRED post-Step 7 | post-Step 7 | nuovo `internal/storage/sources/ingest/extractor.go` (path post-restructure) |
| Wave 2 task 2.4 (multi-page touch pipeline) | DEFERRED post-Step 7 | post-Step 7 | `storage/sources/ingest/pipeline.go` |
| Wave 2 task 2.5 (provenance markers) | DEFERRED post-Step 7 | post-Step 7 | `internal/wiki/schema.go` + `storage/sources/ingest/multi_page_touch.go` |
| Wave 3 Pack A (phase + OTel + Telegram progressive) | **SUPERSEDED** | Step 5 + Step 7 — phase si traduce come `agent.Event` + `chat.OutboundEvent{Type: phase}`; OTel rimandato a separate phase | archivia `.planning/wave3-agent-swarm/plan.md` Pack A; il phase model dei 5 enum nanobot si re-emergerà come 5 `EventType` su `chat.OutboundEvent` |
| Wave 3 Pack B (skill-driven orchestrator) | DEFERRED post-Step 8 | post-Step 8 | skill statico + 80 LOC plumbing; security validator |
| Wave 3 Pack C (propose_patch + sanitization + TTL) | DEFERRED post-Step 8 | post-Step 8 | reuse `proposed_updates` table; `internal/agent/tools/swarmtools/propose_patch.go` (path post-restructure); namespace isolation invariante |
| Context-Eng Fase 0 (baseline) | INTERLEAVED — può partire ORA | parallel to Step 1-2 | zero codice; solo log scrape + scrittura `.planning/baseline-2026-05.md` |
| Context-Eng Fase 1.1 (prompt cache reordering) | **PROMOTED — prereq for Step 6** (§3 D13) | inline Step 5 + verified Step 6 | **Validato esternamente da OpenAI Codex Jan 2026** (`D:\tmp\codex.md`): static-first prefix + tool-order stability + append-not-mutate runtime context. Step 4 e Step 6 toccano prompt assembly (`internal/conversation/system_prompt.go`, `internal/tools/registry.go` ordering, `internal/telegram/conversation.go:261`). Se non blocchiamo l'ordering ora, regression invisibile. 4-6h inline; output: invariante documentata + ordering test in `internal/conversation/`. Promote priority: lands DOPO Step 1 (cleanup) ma PRIMA di Step 6 (kill runner) |
| Context-Eng Fase 1.2-1.4 (telemetria, AGENT.md, tool desc audit) | DEFERRED post-Step 8 | post-Step 8 | richiede stable telegram path. Fase 1.2 telemetria (prompt token reuse ratio come proxy di cache_hit) priorità alzata — è il gauge del lock Fase 1.1 |
| Context-Eng Fase 2 (system prompt surgery) | DEFERRED post-Step 8 | post-Step 8 | |
| Context-Eng Fase 3.1 (memory tool consolidation `recall_memory`) | DEFERRED post-Step 8 | post-Step 8 | impatta `agent/tools/` post-merge |
| Context-Eng Fase 3.2 (mini retrieval capsule) | DEFERRED post-Step 8 | post-Step 8 | |
| Context-Eng Fase 3.3 (unificare microcompact) | INTERLEAVED Step 5 | parallel/inside Step 5 | governance extraction è il posto giusto |
| Context-Eng Fase 4 (`agent_note` + pinned core block) | DEFERRED post-Step 8 | post-Step 8 | feature nuova; non blocca restructure |
| Context-Eng Fase 5 (`MaxReturnChars` + `ArtifactPath` on agent.Task) | INTERLEAVED Step 6 | inside Step 6 mapping table | come da §3 D1 nota; Invocation.MaxReturnChars + Invocation.ArtifactPath aggiunti durante kill-runner |

**Archiviazione raccomandata post-Step 4:**
- `mkdir .planning/_archived/2026-05-pre-master-plan/`
- `git mv` di:
  - `.planning/wave1/fix_plan.md` (con stub "task 3 shipped commit a4ebe141; tasks 1,2,4,5 deferred — see docs/aura-master-plan.md §6")
  - `.planning/wave3-agent-swarm/plan.md` (con stub "Pack A SUPERSEDED by Steps 5+7 of master plan; Pack B/C deferred to post-Step 8")
  - `docs/wave-3-chathub-slice0.md` (con stub "subsumed by master plan §4 Steps 2-4")
- **NON** archiviare `wave2/fix_plan.md` e `CONTEXT-ENGINEERING-ROADMAP.md` finché Step 7 chiude — stanno tornando vivi subito dopo.

---

## 7. Acceptance — vince quando

Checklist eseguibile. Una linea per gate. Spuntare quando tutto ✅.

- [ ] **A1** `(Get-ChildItem internal -Directory).Count ≤ 22` (stretch 21).
- [ ] **A2** `Grep -rn 'agent\.Runner|NewRunner\(' internal/ cmd/ -g '!*_test.go'` → 0 match.
- [ ] **A3** `cmd/probe_chat` con `AURA_USE_HUB=true` ritorna `ChatReply{reply, elapsed_ms, llm_calls, tool_calls, tokens}` shape stabile. `tokens` = `Prompt + Completion` (collapse rule §3 D11). Probe asserta valore numerico ± 5.
- [ ] **A4** Somma LOC `internal/telegram/{bot,commands,handlers}.go` ≤ 200.
- [ ] **A5** `cmd/aura/app.go` + `wire_*.go` esistono; ciascun file ≤ 300 LOC.
- [ ] **A6** `Grep -rn 'agent\.Run\(' internal/ cmd/ -g '!*_test.go' -g '!internal/chat/*'` → 0 match (Hub è sole entry).
- [ ] **A7** Test E2E `internal/swarm/hub_e2e_test.go` esegue 1 parent + 3 child run, store contiene 3 task records con `parent_run_id`.
- [ ] **A8** Snapshot diff Telegram fixture (Step 3 vs Step 4 post-port vs Step 7 hub-wire) = 0 byte.
- [ ] **A9** Probe Telegram entity rendering: prompt con tabella markdown produce edit con `tele.Entity{Bold,Pre,Code}` sui campi corretti.
- [ ] **A10** `git diff master..HEAD -- go.mod` → zero direct deps nuove.
- [ ] **A11** Soak 48h `AURA_USE_HUB=true` produzione: error rate ≤ baseline + 2/1000 msg, p95 latency ≤ baseline + 100ms, RSS slope ≤ 5 MB/h.
- [ ] **A12** Probe `cmd/probe_chat` prompt "crea xlsx con 3 righe X,Y,Z": unzip artifact, parse `xl/sharedStrings.xml`, assert X/Y/Z presenti; assert `chat_events` o `conversations` row con `EventToolEnd` per `create_xlsx`. (memory `feedback_probe_must_verify_artifact_not_reply`).
- [ ] **A13** Wiki determinism preservato: `Grep -rn 'temperature: 0\|temperature=0' internal/wiki/ internal/storage/sources/` ≥ 5 match (atomic + git + per-page mutex invariati).
- [ ] **A14** Tool-arg privacy preservata: `Grep -rn 'args.*value\|arg.*payload' internal/agent/loop.go` non emette log lines con valori (solo names + keys). Probe: invia tool call con `{secret: "TOPSECRET"}` in args; grep dei log → assenza di "TOPSECRET".
- [ ] **A15** Phantom guard preservato: `internal/agent/phantom_guard.go` esiste; `phantom_guard_test.go` verde post-Step 6.

---

## 8. Rischi top-5

| # | Rischio | Prob | Impact | Mitigazione | Early-warning | Stop-and-reassess trigger |
|---|---------|------|--------|-------------|---------------|---------------------------|
| 1 | Regressione Telegram in produzione (CoT scompare, entity rendering broken) | Med | High | Fixture byte-diff (Step 3) come hard gate; flag `AURA_USE_HUB=false` finché Step 8d; manual soak 48h | Snapshot diff > 0 byte; user mention "Aura non sta più pensando ad alta voce" | 3 failure di Step 4 byte-diff → STOP, chiudere a Step 2 |
| 2 | Swarm test break durante kill `agent.Runner` (Step 6) | Med | Med | Mapping table esplicita (Task.MaxToolCalls → MaxIterations); test esistente preservato; `MaxToolCalls=0 → MaxIterations=0` | `go test ./internal/swarm/...` fail post-Step 6 | Step 6 fail 3× → STOP, chiudere a Step 5 (governance estratta è win comunque) |
| 3 | Race condition shutdown durante goroutine ownership migration (Step 8b) | Med | High | `go test -race ./...` hard gate; stress test shutdown 100 iterazioni | `-race` failure on Start/Stop | 8b fail 2× → revertare 8b, chiudere a Step 8a |
| 4 | `.planning` waves perdono context dopo archiviazione | Low | Med | Stub file con link al master plan §6; archivio non delete | User cerca task vecchio e non lo trova | Restaurare file dall'archivio (zero-cost — `git mv` back) |
| 5 | Wave 3 Pack A "obsoleto"  ma utente ha investito tempo a leggerlo | Med | Low | §6 spiega chiaramente perché Pack A è SUPERSEDED; mostrare l'equivalenza `agent.Event` ⇒ `chat.OutboundEvent{Type:phase}` | User chiede "ma dove finisce il PhaseSink?" | Aggiungere un esempio code-shape nel post-Step-7 nota tecnica |

---

## 9. La domanda dura (≤150 parole)

Questo piano è il piano giusto? Sì, con una cautela. Il restructure ha senso perché tre runtime e una god class 8 kLOC bloccano TUTTE le waves successive — Wave 2 e 3 ripeterebbero il debt se costruite sopra. Ma l'utente è stato bruciato da ottimismo superficiale: ~26 giorni produttivi è una stima 4h/d che richiede disciplina. Lo step 8 (composition root) è il rischio reale; se la finestra di calendar disponibile è 2 settimane, **chiudere a Step 7** è una scelta ragionevole — il sistema sarebbe già drammaticamente migliore (1 runtime, Hub canonico, swarm via Hub, 28→22 dirs, no Telegram regressione). Non c'è "step back further" credibile: shippare solo Wave 1 residue lascia in vita i tre runtime e perpetua il problema che ha fatto scrivere questo doc. La cura è gli Step 1-7. Step 8 è la rifinitura.

---

## 10. Riferimenti

| Path | Cosa sopravvive nel master plan |
|------|--------------------------------|
| `docs/aura-restructure-prd.md` v2 | Ordering Phase 1→7, field-mapping table per kill-Runner, Run.Metadata keep, channels/telegram fixture harness pattern |
| `docs/aura-restructure-prd-REVIEW-1.md` | Picobot LOC disclaimer, feature matrix mantenuta, fixture harness promosso a step esplicito |
| `docs/aura-restructure-prd-REVIEW-2.md` | §6 wave disposition table (la chiave del master plan); G17-G21 domain invariants su `temperature=0` + tool-arg privacy |
| `D:\tmp\aura-rebuild-strategy.md` | Diagnosi 3 zone, classificazione canonical vs duplicate runtime, 3-slice surgery sequence, picobot/nanobot lessons |
| `docs/chat-interface-prd.md` | InboundMessage/OutboundEvent contract (già committato in `internal/chathub/types.go`), channel-neutral spine framing |
| `.planning/wave1/fix_plan.md` | Task 3 SHIPPED-out; tasks 1,2,4,5 deferred con path refs post-Step 7 |
| `.planning/wave2/fix_plan.md` | Tutte 5 task deferred post-Step 7; path refs riscritti a `storage/sources/ingest/`, `internal/wiki/` invariante |
| `.planning/wave3-agent-swarm/plan.md` | Pack A SUPERSEDED (rationale §3 D1 + §6); Pack B/C deferred post-Step 8 |
| `.planning/CONTEXT-ENGINEERING-ROADMAP.md` | Fase 0 INTERLEAVED ora; Fase 3.3 dentro Step 5; Fase 5 dentro Step 6; resto deferred post-Step 8 |
| `D:\tmp\paper.md` (Kimi K2.5) | Solo come reference architetturale di orchestrator-worker che Aura indipendentemente già implementa; nessuna adozione RL |
| `D:\tmp\codex.md` ("Unrolling the Codex agent loop", OpenAI engineering blog, Jan 23, 2026) | REFERENCE — valida single-loop default (counterpoint a Kimi swarm); prompt-cache discipline come #1 lever (§3 D13); stateless-per-turn (Aura già lì); MCP `list_changed` mid-conv trap (§5 non-goal); mutate-prior-msgs anti-pattern (§5 non-goal); tool-order stability bug (Codex PR #2611 → Aura invariante §3 D13). Promotes CONTEXT-ENG Fase 1 a prereq for Step 6 (§6) |
| `D:\tmp\picobot/` | Validazione boundary chat↔agent (90 + 333 LOC); NON usato come budget LOC (review F3) |
| `D:\tmp\nanobot/` | Validazione "autocompact module separate dal loop"; supporta governance extraction (Step 5) |
| `CLAUDE.md` | GOD CLASS ≤600 LOC, REUSABLE CODE, NEVER MODIFY TESTS, master-direct, 3-strike rule |
| `memory/feedback_master_direct_workflow.md` | Tutti i step su master |
| `memory/project_graph_memory_core_strategy.md` | `internal/wiki/` invariante; no graph DB |
| `memory/feedback_embedding_backend_stays_mistral.md` | embedding locked |
| `memory/feedback_phantom_guard_requires_proximity.md` | phantom guard preservato byte-per-byte |
| `memory/feedback_probe_must_verify_artifact_not_reply.md` | A12 unzip + parse xlsx |
| `memory/feedback_inspect_artifact_visually_not_just_pass_status.md` | Manual visual check post-Step 4 (CoT + entity) |
| `memory/feedback_agent_must_know_tools_exist.md` | Single canonical system-prompt via `conversation.RenderSystemPrompt` post-Step 6 |
| `memory/feedback_no_regex_for_nlp.md` | Phantom guard proximity-after-strip preservato; nessun regex su LLM output |
| `memory/feedback_minipc_cpu_budget.md` | Hub I/O bound, no busy-loop; swarm `MaxWorkers ≤ 3` (post-Wave 3 Pack B) |
| `memory/feedback_minillm_cpu_not_viable_for_tool_retrieval.md` | No LLM tool-routing in Step 7 tools merge; embed cosine + manifest + permissive fallback preservati |
| `memory/project_open_gaps_2026-05-13.md` | Wave 2.10.c (MCP reload) e Wave 2.9.5 (GLM-OCR) restano OPEN — out of scope master plan |

— END Master Plan v1.1 —
