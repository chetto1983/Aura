# HANDOFF 2026-07-28 — superficie memoria (agent-memory MCP)

Sessione precedente chiusa a metà brainstorming. Questo file è la verità di partenza:
ogni numero qui sotto è **misurato**, non stimato.

## 1. Stato consegnato

- **Commit `5523ad01`** — `fix(agent): a loaded tool schema outlives its turn, so its grant must too`.
  Il grant di un tool deferred è ora **conversation-scoped** (`deriveActivated` in
  `internal/agent/llm_agent_promote.go`, seeding in `llm_agent_construct.go:38`).
  Verificato live: prima ogni uso deferred era preceduto da `tool_search` nello stesso turno
  (seq 64→66, 79→81, 95→97), dopo 5 turni consecutivi e 28 chiamate senza nessuna.
  Vale anche per i tool MCP namespaced (22 `calculator__*`).
  Conseguenza per questo lavoro: **deferrare un tool ora costa una `tool_search` per
  conversazione, non per turno.**

- **Sidecar ricostruito** dal sorgente vendored su HEAD:
  `AURA_AGENT_MEMORY_VENDOR_REV=$(git rev-parse HEAD) docker compose build aura-agent-memory-mcp`
  → `memory_update` ora è registrato (17 funzioni). L'immagine precedente era indietro
  rispetto al fork: il tool era già scritto e mai buildato.

- **MCP montato sulla sessione Claude Code**: `http://127.0.0.1:8091/mcp` (HTTP, connesso).
  `/mcp/` con slash finale risponde 307. I tool compaiono solo **dopo restart della sessione**.

## 2. Misure

**Superficie tool**
- 12 `memory__*` montati, **unico namespace MCP non-deferred**:
  `internal/agent/mcptools/bridge.go:259-261` → `return namespace != "memory"`.
  `calculator` (23) e `calendar` (14) sono tutti deferred.
- Uso storico su tutto il DB: **25 chiamate totali**.
  `add_fact` 6, `add_preference` 5, `get_context` 4, `search` 4, `forget` 4,
  `get_entity` 1, `store_message` 1.
  **Mai chiamati**: `add_entity`, `create_relationship`, `get_conversation`,
  `get_facts`, `list_sessions`.

**Popolamento del grafo**
- Neo4j: `Fact` 37, `Entity` 28, `Message` 18, `Preference` 14, `Conversation` 1.
  **`Observation` 0, `Reflection` 0, `ReasoningTrace` 0.**
- Postgres: **552 turni** su **19 conversazioni**.
- Entità e relazioni nascono dall'**estrazione automatica**, non dal modello:
  `MENTIONS` 20, `ABOUT_SUBJECT` 16, `RELATED_TO` 18, `SAME_AS` 6 (dedup attiva).

**Cosa Aura non consuma**
- Grep repo-wide (`*.go`): **0 match** per `resources/list`, `prompts/list`,
  `resources/read`, `prompts/get`, server instructions. Aura parla solo
  `tools/list` + `tools/call`.
- Il server espone anche: resources (`memory://context/{session_id}`, `memory://entities`,
  `memory://preferences`, `memory://graph/stats`) e prompts (`memory-conversation`,
  `memory-reasoning`, **`memory-review`** = flag contradictions + suggest updates).
- `MemoryObserver` (`_observer.py`) = compressione a tre livelli
  Reflections → Observations → recent messages, soglia `observation_threshold: 30000`,
  **attiva di default e senza nulla da comprimere** (si alimenta da `store_message`,
  chiamato 1 volta in totale).

**Difetti trovati**
- `_instructions.py` insegna 5 tool **inesistenti**: `memory_start_trace`,
  `memory_record_step`, `memory_complete_trace`, `memory_export_graph`, `graph_query`.
- Container avviato con `--no-auto-preferences`; il detector è rule-based su **keyword
  inglesi** (`_preference_detector.py`) — su input italiano mancherebbe comunque il bersaglio.
- Commento del Dockerfile dice `--embedding-dimensions 384`, il comando reale usa `768`.
- **Nessun percorso di correzione** → il grafo tiene due regole opposte sui tool deferred,
  entrambe a `confidence: 1.0`:
  - `:Preference bad4437d` (categoria `communication_style`, errata) — «cerca SEMPRE con
    tool_search… i deferred non persistono tra turni». Ora **falsa**.
  - `:Fact ddb3efe2` `tool_loading_strategy → unified_tool_surface_fixed` — «tutti i built-in
    sempre caricati, tool_search non serve». Design **mai implementato**.
  Causa meccanica, dal docstring del fork: `add_*` deduplica e fa merge sul match più vicino
  **senza cambiarne il testo** → ri-aggiungere per correggere re-merga sempre sulla vecchia
  formulazione.

## 3. Primitive già scritte nella libreria e senza porta MCP

| primitiva | file |
|---|---|
| `supersede_preference` | `memory/long_term.py:969` |
| `dedupe_entities` | `memory/consolidation.py:46` |
| `merge_duplicate_entities` | `memory/long_term.py:1882` |
| `delete_message` | `memory/short_term.py:1071` |
| `update_preference` / `update_fact` / `update_entity` | `integration.py:351/412/483` |

I primi due sono esattamente ciò che l'ADR
[`compaction-spike-2026-07-20.md`](compaction-spike-2026-07-20.md) indica come
**«l'unico lavoro genuinamente nuovo»** del Layer C (consolidation hygiene).

## 4. Decisioni già prese con l'operatore

1. **Opzione C** — alimentazione del grafo **automatica** (mirror dei turni in short-term,
   Observer, estrazione in background); i tool restano solo per il write **deliberato**
   e per le **correzioni**.
2. **Tripartizione approvata** — tre intenzioni diverse, tre verbi:
   - `memory_modify` — «è **sbagliato**»: corregge in place per id, mantiene id e relazioni,
     **rinfresca l'embedding**.
   - `memory_supersede` — «**era** vero, ora non più»: chiude `valid_to`, collega il
     successore, conserva la storia. (I nodi `:Preference` hanno già `valid_from`.)
   - `memory_forget` — «non deve **esistere**»: cancellazione dura, **da estendere a
     message e relationship** (`create_relationship` oggi non ha inverso).
3. **Serve una skill memoria per Aura** + i tool con cui l'agente aggiorna se stesso.
4. **Potatura proposta, DA CONFERMARE** (era la domanda aperta a fine sessione):
   tenere `add_fact`, `add_preference`, `search`, `forget` + i due nuovi = **6 tool, tutti
   deferred**; togliere `add_entity`, `create_relationship`, `get_conversation`,
   `list_sessions`, `get_facts`, `get_entity`, `store_message`; `get_context` diventa
   **iniezione** (è già una resource). Cade anche l'eccezione `namespace != "memory"`.
   I due tagli su cui l'operatore poteva volerla pensare diversamente: `get_entity` e
   `get_context`.

## 5. Vincoli

- **PRD-first**: PRD-amendment prima del codice. L'ADR della spike lo richiede già per la
  strategia 4-layer.
- **Gate brainstorming**: design presentato e approvato prima di implementare.
- **Neo4j è VIVO e non disposable.** Mai puntarci il coverage gate. Per i test di scrittura
  usare un `user_identifier` dedicato: i tool lo accettano e lo scope isola dall'operatore
  (`b130c94d-a213-463a-a797-ec124104363a`, `--user-id aura-local`).
- Aura vede un tool MCP **nuovo** solo dopo restart del container `aura` (il mount avviene
  al boot; il reconnect-on-use recupera solo un server già montato).

## 6. Prossimo passo

Confermare la potatura (§4.4) → design doc + PRD-amendment → `writing-plans`.
