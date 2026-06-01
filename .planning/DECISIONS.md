# Aura — Registro decisionale (decision registry)

> **Scopo.** Mappa di TUTTE le forcelle architetturali del rewrite, ordinate per
> **irreversibilità × finestra-decisionale** — l'asse "costoso-dopo" emerso dalla
> valutazione Neo4j/Memgraph (2026-06-01). Serve a decidere *deliberatamente* le
> scelte costose-da-cambiare PRIMA di costruirci sopra, non a scoprirle a metà build.
>
> **Come si usa.** Una decisione 🔓 OPEN in §2/§3 va portata a 🔒 con il rigore
> "ricerca aggiornata + mapping sul codice" prima della phase che la cementa.
> Le ⏳ validate-by-building NON si pre-decidono: si misurano costruendo.
>
> Creato 2026-06-01. Fonte: prd.md + ROADMAP.md + sessione di review #24-#29.

## Legenda

**Status:** 🔒 locked-validated · 🔓 open (da valutare) · ⏳ validate-by-building · 📦 deferred-v2
**Reversibilità** (costo di cambiare *dopo* che è atterrata): 🟢 bassa (swap localizzato) · 🟡 media · 🔴 alta (rework a cascata / re-processing corpus) · ⚫ molto alta (riscrittura)

---

## §1 — Substrato fondazionale (LOCKED, validato)

| # | Decisione | Status | Revers. | Cosa la cementa | Validazione |
|---|---|---|---|---|---|
| D01 | **Graph DB = Neo4j 5.26** (vs Memgraph) | 🔒 | 🔴 | tutto il Cypher di Slice 11 | Valutato 2026-06-01: cheap-now/expensive-after-P15; Memgraph scartato per modello in-memory vs corpus che cresce all'infinito. MCP swap sarebbe stato pulito; RAM è il blocker. |
| D02 | **Relational = Postgres 17** + pgx + sqlc + golang-migrate | 🔒 | 🔴 | 6 migration shippate | Phase 1 done |
| D03 | **3-store split** (PG=app-state, Neo4j=knowledge+vector, FS=artifact) | 🔒 | 🔴 | ogni slice con persistenza | non-negoziabile (PRD §Persistence) |
| D04 | **Neo4j data via MCP** (mcp-neo4j-cypher) + native driver SOLO per DDL | 🔒 | 🟡 | client.go chokepoint | Phase 1 done; è ciò che rende D01 reversibile |
| D05 | **Runtime = Go 1.26** | 🔒 | ⚫ | tutto | go.mod 1.26.3 |
| D06 | **Agent runtime custom** (interface "stolen-not-imported" da adk-go) + workflow agents | 🔒 | ⚫ | Phase 2 done | costruito; reopening non vale (revers. ora ⚫) |
| D07 | **KV-cache = prefisso 3-segmenti** `[0]`system+tools `[1]`Agent.md `[2]`Insight `[3..N]`tail | 🔒 | 🟡 | Phase 6 + ogni iniezione memory | pinnato amendment #29 |
| D08 | **LLM client handrolled OpenAI-compat** (no SDK) | 🔒 | 🟢 | Phase 3 done | swappable per design |
| D09 | **Tool design = deferred-tool pattern** + `tool_search` | 🔒 | 🟡 | ogni tool | Phase 3 done |
| D10 | **Memory design** = mem0+Letta+GraphRAG+Cognee blend, valid-time, WRRF, soft-archive, POLE+O | 🔒 | 🔴 | Phase 15 | indurito amendments #24-29 + OQ chiuse |

---

## §2 — TIER-1: forcelle APERTE irreversibili (valutare ADESSO)

> Condividono con D01 la proprietà "costoso-dopo + sotto-esaminato". Sono le uniche
> due che meritano il trattamento completo prima di costruire.

| # | Decisione | Status | Revers. | Finestra (cosa la cementa) | Nota |
|---|---|---|---|---|---|
| **D11** | **Modello embedding + dimensione** — oggi `embeddinggemma-300m @ 768d` | 🔓 | 🔴 | **prima che Phase 15 ingerisca a scala** | **Gemella esatta di D01.** Cambiare embedder dopo ingestione = re-embed dell'intero corpus + reindex HNSW. La dim 768 è contratto hard (HNSW, sidecar, recall bench, `pingEmbed`). Latente: EmbeddingGemma supporta Matryoshka/MRL (potresti troncare). Da valutare: dim (768 vs 256/512/1024), multilingue (utente IT), context window dell'embedder, qualità retrieval vs costo RAM/storage. **Sotto-analizzato quanto D01.** |
| **D12** | **Primitiva isolamento sandbox** (Slice 2) — PRD oggi: container + seccomp allowlist + ulimit + network policy | 🔓 | 🔴 | **Phase 5 (LA PROSSIMA)** | Fondazionale: skills (7), swarm (9), snippet eseguibili (7e) ci costruiscono sopra. Cambiare la primitiva (container+seccomp vs gVisor vs microVM/Firecracker vs WASM/WASI) *dopo* = rework a cascata. Da valutare vs il profilo single-user self-host mini-PC: forza dell'isolamento vs overhead RAM/avvio vs compatibilità Python/multi-lang snippet. |

---

## §3 — TIER-2: bet architetturali (moderatamente reversibili)

| # | Decisione | Status | Revers. | Finestra | Nota |
|---|---|---|---|---|---|
| D13 | **Strategia provider LLM** — single-provider OpenRouter/DeepSeek-V4 via OpenAI-compat | 🔓 | 🟢 | continuo | L'abstraction è swappable (hedge buono). Il *modello* è la scommessa; reasoning-event handling plasma il prompt design. Local fallback = D19 (v2). |
| D14 | **Coordinamento Swarm** (Slice 3/9) — ParallelAgent reuse + bus custom, cap 2-deep | 🔓 | 🟡 | Phase 9 | Full N-deep + DM-by-ID = SWARM-V2-01 (deferred). Modello bus/DM ancora plasmabile dentro il runtime. |
| D15 | **Transport = AG-UI SSE gateway** (Slice 8) | 🔓 | 🟡 | Phase 12 | WebSocket scartato (SSE-only). CLI in-process vs via-agui = D20. Gateway → reversibile-ish. |
| D16 | **Channels framework + Telegram primary** (Slice 9) | 🔒 | 🟡 | Phase 13 | Telegram È la porta d'ingresso del prodotto (README). Locked di fatto. |

---

## §4 — Decisioni pre-merge benchmark (non pre-decidibili, flag-ate)

| # | Decisione | Status | Finestra | Nota |
|---|---|---|---|---|
| D17 | Variante multimodale **Gemma 4** (E2B/E4B/26B/31B) | ⏳ | Slice 9c | benchmark accuracy+latenza+RAM su corpus reale; baseline E4B |
| D18 | **GPU vs CPU** per vLLM | ⏳ | Slice 13 (v2) | CRITICA: vLLM CPU 5-10x più lento → path 13-bis (riusa llama.cpp). Hardware-gated. |
| D19 | Modello **LLM fallback locale** (Gemma 3 12B / Llama 3.1 8B / Qwen 2.5 7B) | 📦 | Slice 13 (v2) | LLM-V2-01 deferred; benchmark IT+EN-code pre-merge |
| D20 | CLI default **in-process vs via-agui** | ⏳ | Slice 8 | misurare latency roundtrip HTTP loopback (~50-150ms attesi) |

---

## §5 — Validate-by-building (TIER-3, si misurano costruendo)

| # | Decisione | Status | Nota |
|---|---|---|---|
| D21 | Task Canvas **deterministico vs LLM-refine** | ⏳ | OQ8 chiusa: skeleton det ship; LLM-refine on solo se offload-recovery-rate >0.15 su carico reale |
| D22 | **Tuning RRF / chunk-size / re-ranker** | ⏳ | OQ1/3 chiuse con default; affinare su benchmark recall corpus reale |
| D23 | Soglie Memify (180gg/<3), insight top-K (3/0.7) | ⏳ | default conservativi configurabili; misurare drift |

---

## §6 — Deferred v2 (fuori milestone, condizioni di rientro note)

| # | Item | Rientra se |
|---|---|---|
| D24 | vLLM + LMCache (LLM-V2-01) | GPU disponibile (DGX Spark path) |
| D25 | Skill cross-conv auto-suggest (Slice 7f, SKILL-V2-01) | dopo 0.7 HNSW + 7e + 11 |
| D26 | Full N-deep swarm (SWARM-V2-01) | use-case reale multi-livello |
| D27 | Bi-temporale memory (OQ7) | caso d'uso audit/retroattivo |
| D28 | Multi-user isolation (OQ6) | oltre single-user; hedge `identity_id` già stampato |

---

## Prossime azioni raccomandate

1. **Valutare D11 (embedding)** — la gemella di D01, stessa logica costoso-dopo, sotto-esaminata. Massima priorità: cementa con Phase 15.
2. **Valutare D12 (sandbox)** — imminente (Phase 5 = next) e fondazionale.
3. Le TIER-2 (D13-D15) si possono portare a 🔒 just-in-time prima della rispettiva phase.
4. Non spendere pre-analisi su §5 (validate-by-building).
