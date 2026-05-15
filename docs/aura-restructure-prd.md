# PRD — Aura Restructure (3-cut surgery)

> **Status: HISTORICAL — first iteration of the deep-refactor PRD ("3-cut surgery"), superseded by [docs/aura-master-plan.md](aura-master-plan.md) and then by [prd.md](../prd.md). Preserved as evidence per prd.md §3.2.**

**Versione:** 2 (post-REVIEW-1)
**Data:** 2026-05-14
**Owner:** Aura core / solo dev
**Audience:** Author di plan-phase, reviewer subagent, esecutore commit-by-commit
**Scope target:** `D:\Aura` repo, branch `master`
**Predecessori:**
- `D:\tmp\aura-rebuild-strategy.md` (strategy, autoritativo su zone di disordine)
- `D:\Aura\docs\chat-interface-prd.md` v0.1 (Wave 3.0, ChatHub PRD — §2.1 / §2.4 / §11.5 referenziate qui)
- `D:\Aura\docs\aura-restructure-prd-REVIEW-1.md` (Reviewer 1 — Architecture)

**Status changelog**

| Versione | Data | Reviewer | Note |
|----------|------|----------|------|
| v1 | 2026-05-14 | — | Draft iniziale, pre-review. |
| v2 | 2026-05-14 | Reviewer 1 (Architecture) | Applied 4 blockers + 9 majors + 2 minors; deviations noted inline via HTML comments. |
| v3 | _tbd_ | _tbd_ | Applica feedback Reviewer 2 (Domain invariants). |
| v4 | _tbd_ | _tbd_ | Applica feedback Reviewer 3 → ready-for-execute. |

---

## 0. Sommario esecutivo

Aura oggi gira tre runtime di agente paralleli (`agentloop.Run`, `agentruntime.Run`, `agent.Runner.Run`), ha una god-class `*Bot` con **38 campi verificati** (`internal/telegram/bot.go:40-81`, conteggio diretto v2 — vedi §1.1 per breakdown line-by-line) che è di fatto il composition root del binario, e una `chathub` parcheggiata (1836 LOC) che contiene la spina channel-neutral giusta ma anche una regressione Telegram-outbound che perderebbe CoT + entity-markdown se promossa in produzione. Il restructure NON è un rewrite: è una chirurgia in tre tagli mirati — (1) eliminare `agent.Runner` e unificare su `agentruntime`; (2) estrarre la `governance` (microcompact, budget, tool-result caps) dal loop; (3) finire il merge della chathub (rinominata `internal/chat`) come unica entry-point verso il loop, demolire la god-class spostandone il corpo in `cmd/aura/app.go`, e tagliare il package `internal/telegram` a ~200 LOC totali per i file core (bot+commands+handlers). Effort totale rivisto post-REVIEW-1: **~100 ore produttive** (8 commit "rinomina/merge" + 8 commit refactor reali + 2 sub-commit di test-harness, vedi §6 e §10). Rischio principale: regressione Telegram in produzione — mitigata da feature-flag `AURA_USE_HUB` per 48 ore prima del cutover. **Questo PRD non propone nuove feature, nuove dipendenze, nuovo storage o nuovi backend di embedding.**

<!-- Author response to F7: il paper Kimi K2.5 è stato rimosso dal sommario e dalla §4.5 — Aura non implementa orchestrator/subagent split, PARL reward, né critical-steps metric (vedi §4.5 v2). Il pattern parallel-decomposition è preesistente in `internal/swarm/` e non richiede citazione esterna per validarsi. -->

---

## 1. Motivazione e diagnosi

### 1.1 Le tre zone di disordine

#### Zona A — Tre runtime agentici in coesistenza

| Path | File | LOC | Used by | Streaming | Permissive tool pool | Event taps | Verdetto |
|------|------|-----|---------|-----------|----------------------|------------|----------|
| `agentloop.Run` | `internal/agentloop/loop.go` | 780 | Primitive | sì | sì | sì | **Canonical primitive** — preservare |
| `agentruntime.Run` | `internal/agentruntime/runner.go` | 266 | Telegram + chathub adapter | sì (passthrough) | sì (delegato) | sì (typed `Event`) | **Canonical envelope** — preservare |
| `agent.Runner.Run` | `internal/agent/runner.go` | 554 | Swarm worker + `/api/chat` (`chatPipeService`) | NO (solo `llm.Send`) | NO | NO | **Da eliminare** (-554 LOC) |

Conseguenza pratica: `/api/chat` oggi è strutturalmente inferiore a Telegram — zero streaming, zero event taps, zero permissive pool.

#### Zona B — La god-class `*Bot` (38 campi verificati)

`internal/telegram/bot.go:40-81` dichiara `*Bot` con **38 campi** (conteggio diretto v2). `Username()` (line 84) e `APIHandler()` (line 90) sono **metodi**, non campi, e v1 li aveva erroneamente listati come field tag — corretto qui.

Breakdown line-by-line dei 38 campi (line in `bot.go`):

| # | Linea | Nome | Tipo | Tag responsabilità |
|---|-------|------|------|---------------------|
| 1 | 41 | `bot` | `*tele.Bot` | Telegram I/O |
| 2 | 42 | `cfg` | `*config.Config` | Config |
| 3 | 43 | `loc` | `*time.Location` | Config |
| 4 | 44 | `logger` | `*slog.Logger` | Logging |
| 5 | 45 | `llm` | `llm.Client` | Agent loop |
| 6 | 46 | `wiki` | `wiki.Repository` | Wiki/retrieval |
| 7 | 47 | `search` | `search.Searcher` | Wiki/retrieval |
| 8 | 48 | `tools` | `*tools.Registry` | Agent loop |
| 9 | 49 | `budget` | `budget.Runtime` | Agent loop |
| 10 | 50 | `sources` | `source.Repository` | Wiki/retrieval |
| 11 | 51 | `ocr` | `*ocr.Client` | Wiki/retrieval |
| 12 | 52 | `skills` | `*auraskills.Loader` | Skills |
| 13 | 53 | `docs` | `*docHandler` | Debug |
| 14 | 54 | `sched` | `*scheduler.Scheduler` | Maintenance |
| 15 | 55 | `schedDB` | `scheduler.AgentJobRepository` | Persistenza |
| 16 | 56 | `agentRunner` | `*agent.Runner` | Agent loop |
| 17 | 57 | `swarmStore` | `swarm.Reader` | Swarm |
| 18 | 58 | `swarmMgr` | `swarm.RunRunner` | Swarm |
| 19 | 59 | `authDB` | `auth.Repository` | Persistenza |
| 20 | 60 | `mcpClients` | `[]mcp.ConnectedClient` | MCP |
| 21 | 61 | `archiveDB` | `conversation.ArchiveRepository` | Persistenza |
| 22 | 62 | `archiver` | `conversation.ClosingTurnAppender` | Persistenza |
| 23 | 63 | `issues` | `scheduler.IssueRepository` | Maintenance |
| 24 | 64 | `api` | `http.Handler` | HTTP API |
| 25 | 65 | `sandboxMgr` | `sandbox.ExecutionRuntime` | Sandbox |
| 26 | 66 | `toolReg` | `tools.ToolStore` | Agent loop |
| 27 | 67 | `reindex` | `*reindex.Worker` | Wiki/retrieval |
| 28 | 68 | `toolReconciler` | `*toolindex.Reconciler` | Agent loop |
| 29 | 69 | `bgCtx` | `context.Context` | Maintenance |
| 30 | 70 | `bgCancel` | `context.CancelFunc` | Maintenance |
| 31 | 71 | `bgWg` | `sync.WaitGroup` | Maintenance |
| 32 | 72-74 | `compactMemoryHealth` | anonymous interface | Debug |
| 33 | 75 | `debugDocsMu` | `sync.Mutex` | Debug |
| 34 | 76 | `debugDocs` | `[]DebugDocumentSend` | Debug |
| 35 | 77 | `debugDocSeq` | `atomic.Uint64` | Debug |
| 36 | 78 | `sessions` | `*agentruntime.SessionStore` | Agent loop |
| 37 | 79 | `started` | `atomic.Bool` | Maintenance |
| 38 | 80 | `gate` | `*concurrency.UserGate` | Agent loop |

<!-- Author response to F1: PRD v1 said "35", reviewer counted "41". Direct line-by-line walk = 38. Reviewer's 41 likely double-counted the anonymous interface (1 field with 1 inner method = still 1 field) and the `Username/APIHandler` methods that v1 also conflated. The 38 number is now the source of truth used in §1.5 (Commit 16 rationale) and the executive summary. -->

`internal/telegram/setup.go` è 1036 LOC. Importa **32 package interni unici** (verificato con `grep "github.com/aura/aura/internal" setup.go | sort -u | wc -l` = 32). Lista corretta (v1 confondeva `agentloop`/`agentruntime` — qui sono entrambi presenti separati, e `agent` è nella lista contrario a quanto la reviewer aveva sospettato):

```
agent, agentloop, agentruntime, api, auth, budget, concurrency, config,
conversation, conversation/summarizer, ingest, llm, markitdown, mcp,
mcppolicy, mcpwatch, memoryindex, ocr, qdrant, reindex, sandbox, scheduler,
search, settings, skills, source, swarm, swarmtools, toolindex, tools,
wiki, workspace
```

**Il nome `telegram` è una menzogna**: il file è il composition root di Aura.

CLAUDE.md è esplicito (sezione GOD CLASS): _"Never create a god class or file >600 LOC if you find one refactor!"_. Sia `setup.go` (1036) sia `documents.go` (546) sia `conversation.go` (538) sia `documents_test.go` (685) violano il limite oggi, dentro lo stesso package.

#### Zona C — Chathub parcheggiata (1836 LOC, con regressione)

| File | LOC | Stato |
|------|-----|-------|
| `internal/chathub/types.go` | 174 | **OK** — envelope sano (`InboundMessage`, `OutboundEvent`, `Run`). |
| `internal/chathub/hub.go` | 274 | **OK** — dispatcher + run/event ID + `/stop` registry. |
| `internal/chathub/agentloop.go` | 220 | **OK** — translator `agentruntime.Event → chathub.OutboundEvent`. **8 production write su `Run.Metadata`** (lines 94, 96, 99, 102, 205, 214, 215). |
| `internal/chathub/adapters/telegram/inbound.go` | 84 | OK — thin normalizer. |
| `internal/chathub/adapters/telegram/outbound.go` | **259** | **REGRESSIONE** — perde CoT (`🧠 _cot_`), perde `renderForTelegramEntities`, perde fallback entity (vedi `internal/telegram/streaming.go:55-95`). **Delete.** Legge `event.Payload["tele_bot"]`, `event.Payload["tele_recipient"]`, `event.Payload["tele_placeholder"]` (lines 126-134) — un canale di handle-injection separato da `Run.Metadata`. |
| `internal/chathub/adapters/web/outbound.go` | 207 | **OK** — buffered Router per `/api/chat`. |
| `internal/chathub/adapters/web/chat_service.go` | 93 | **OK** — bridge → diventa adapter inbound web. |
| `internal/chathub/adapters/silent/outbound.go` | 87 | **OK** — heartbeat + cron no-op. |
| Tests chathub | ~700 | OK ma over-fitted alla forma corrente. |

### 1.2 Sample dei 49 package — categorizzato

Il count totale è verificato (`ls internal/` = 49). Categorie:

- **Mergeable in `internal/agent/`**: `agent`, `agentloop`, `agentruntime` (3)
- **Mergeable in `internal/mcp/`**: `mcp`, `mcppolicy`, `mcpwatch` (3)
- **Mergeable in `internal/storage/search/`**: `search`, `qdrant`, `reindex`, `memoryindex`, `memoryquality` (5)
- **Mergeable in `internal/storage/sources/`**: `source`, `ocr`, `ingest`, `markitdown` (4)
- **Mergeable in `internal/agent/tools/`**: `tools`, `toolindex`, `toolsets`, `swarmtools` (4)
- **Mergeable in `internal/config/`**: `config`, `settings`, `runtimebootstrap` (3)
- **Mergeable in `internal/api/`**: `api`, `auth`, `setup`, `health` (4)
- **Da DELETARE (empty o single-test)**:
  - `orchestration/` — directory exists, **zero `.go` files** (verificato). Delete.
  - `tracing/` — directory exists, **zero `.go` files** (verificato). Delete.
  - `release/` — contiene solo `release_config_test.go` (single test, no production code). Decisione: spostare il test sotto `config/` o deletare se non più rilevante.
  - `dbrecovery/` — 2 file (recovery.go + recovery_test.go). Merge in `db/`.
  - `debugguard/` — 2 file (live_db.go + live_db_test.go). Merge in `db/` o `logging/`.
  - `install/` — 4 file (download.go, download_test.go, embedding.go, progress.go), bootstrap per modelli. Tenere top-level (single responsibility, ~265 MB GGUF fetcher di Wave 2.10 — vedi memoria `project_wave_2_10_install_bootstrap_shipped.md`).
- **Da mantenere isolati**: `wiki`, `conversation`, `llm`, `swarm`, `skills`, `scheduler`/`cron`, `workspace`, `sandbox`, `files`, `budget`, `concurrency`, `db`, `backup`, `install`, `logging`, `tray`, `chathub`/`chat`, `telegram` (~18)

<!-- Author response to F6: orchestration/ e tracing/ sono empty — delete invece di "preserve". §4.1 v2 aggiornato. -->

### 1.3 Canonical-by-traffic vs canonical-by-design

| Surface | Runtime usato oggi | Runtime corretto da design |
|---------|--------------------|----------------------------|
| Telegram conversation | `agentruntime.Run` via `internal/telegram/conversation.go:343` | `agentruntime.Run` ✅ |
| `/api/chat` (CLI pipe, web v1) | `agent.Runner.Run` via `chatPipeService` | `agentruntime.Run` ❌ drift |
| Swarm worker | `agent.Runner.Run` via `swarm.Manager` | `agentruntime.Run` ❌ drift |
| Heartbeat / cron / agent_job | `agent.Runner.Run` (background jobs) | `agentruntime.Run` ❌ drift |
| Chathub agentloop adapter | `agentruntime.Run` (via `chathub/agentloop.go`) | `agentruntime.Run` ✅ ma nessun caller in produzione |

Tre callers su quattro stanno sul runtime sbagliato. È esattamente il pattern che CLAUDE.md vieta sotto _"REUSABLE CODE — Never DUPLICATE CODE CREATE A REUSABLE CLASS"_.

### 1.4 Picobot LOC comparison — disclaimer

Picobot (D:\tmp\picobot) implementa hub+agent+telegram-channel in 615 LOC totali. Non si usa più come benchmark di "Aura è 3× troppo grande" perché picobot **non implementa** features che Aura preserva (vedi §1.6 sotto). La metrica corretta è: "il LOC budget di Aura deve coprire **il superset** delle features Aura+picobot, non l'intersezione." Picobot resta utile come **gold standard di shape** (chat/hub/agent decoupling, adapter interface), non come gold standard di LOC.

<!-- Author response to F3: claim "3× larger without covering more cases" cancellata. Sostituita con la feature matrix esplicita di §1.6 e con LOC budget derivati dalla feature surface verificata, non dal confronto sbilanciato. -->

### 1.5 Anchor numerico per il restructure

L'esecutore deve poter ancorare ogni commit a numeri verificati. Riepilogo:

- **38 fields** in `*Bot` (line 40-81 di `bot.go`)
- **32 import interni unici** in `setup.go` (linea command verificata)
- **49 directory** in `internal/` oggi
- **6 metodi su `*Bot` dichiarati in `bot.go`** (Username, APIHandler, CompactMemoryHealth, ReindexHealth, sessionStore, userGate — più send/edit helpers in altri file)
- **8 production writes** su `Run.Metadata` in `chathub/agentloop.go` (lines 94, 96, 99, 102, 205, 214, 215; più 4 read in test)
- **3 read sites** del payload map `{tele_bot, tele_recipient, tele_placeholder}` in `chathub/adapters/telegram/outbound.go` (lines 126, 130, 134)

### 1.6 Picobot comparison caveat — feature matrix verificata

Picobot copre meno casi di Aura. Sotto, la matrice features con file:line refs. Per ciascuna feature: **keep** (Aura la mantiene), **defer** (rimandata a phase futura), **drop** (eliminata dal restructure). Default: **keep tutto** — il restructure è chirurgia non rewrite.

| Feature | Picobot ha? | Aura ha? — file ref | LOC stimati Aura | Decisione restructure |
|---------|-------------|----------------------|------------------|------------------------|
| Streaming chat (token deltas) | NO (usa `provider.Chat`) | SÌ — `agentloop/loop.go` consume di `llm.Client.Stream` | ~80 | **keep** (G3 + G8) |
| Phantom-tool guard | NO (verificato: zero match in `picobot/internal/agent/loop.go` su `phantom`/`microcompact`/`MaxCallsPerTool`/`EnforceLimit`/`permissive`) | SÌ — `agentloop/phantom_guard.go` (380 LOC) | ~380 | **keep** (Commit 8 + 10 preservano) |
| Microcompact (mid-turn compression) | NO | SÌ — `agentloop/governance.go` + `conversation/tool_compaction.go` | ~250 | **keep** (Commit 10 lo estrae) |
| Permissive tool pool / `ToolResolver` | NO | SÌ — `agentloop/loop.go:159-176, 332` + `agent/tools/` registry | ~120 | **keep** (memoria `feedback_agent_must_know_tools_exist.md`) |
| MaxCallsPerTool (per-tool budget) | NO | SÌ — `agentloop.Options.MaxCallsPerTool` | ~30 | **keep** (Commit 8 deve mappare correttamente, vedi mapping table) |
| Sliding context window 50 msg | NO | SÌ — `conversation.Context.EnforceLimit` | ~60 | **keep** |
| Entity rendering Telegram | NO (plain string in `out.Content`) | SÌ — `telegram/entity_markdown.go` (84 LOC) | ~84 | **keep** (Commit 12 porta) |
| CoT visualization (`🧠 _cot_`) | NO | SÌ — `telegram/streaming.go:55-95` | ~40 | **keep** (G8) |
| Throttle 600ms progressive edit | NO | SÌ — `telegram/streaming.go:101-208` | ~50 | **keep** (G8) |
| Tool-call reasoning channel split | NO | SÌ — `agentruntime.Event{LLMDelta, ToolStart, ToolEnd}` | ~80 | **keep** |
| Multi-turn tool fan-out parallel | NO | SÌ — `agentloop/loop.go` parallel exec | ~100 | **keep** |
| Run-ID + `/stop` registry | NO (picobot `chat.go` is 99 LOC of buffered chans) | SÌ — `chathub/hub.go:274` | ~140 | **keep** (è la spina) |
| Skill manifest progressive disclosure | sì (picobot ha `skills/loader.go`) | SÌ — `internal/skills/` | parità | **keep** |
| MCP dynamic registration | sì (picobot ha `mcp/`) | SÌ — `internal/mcp/` | parità | **keep** |

LOC "premium" Aura sopra picobot (somma colonna "LOC stimati" per le righe Picobot=NO): **~1714 LOC**. Questo è il **budget di feature** che Aura deve preservare e che fa sì che `agent/` (post-merge) + `chat/` siano significativamente più grossi di picobot.

<!-- Author response to F3: matrice completa. Nessuna feature droppata. Le LOC budget di §4 e §7 sono ricalibrate su questo numero. -->

---

## 2. Obiettivi

Numerati, misurabili. Ogni obiettivo è uno smoke-test booleano post-restructure.

- **G1.** Ridurre il count delle directory di primo livello in `internal/` da 49 a **≤22 cartelle logiche** (sub-pacchetti permessi). Aritmetica esplicita in §4.1. Verifica: `(Get-ChildItem internal -Directory).Count` ≤ 22.
- **G2.** Single canonical agent-run path. `agent.Runner` e `internal/agent/runner.go` eliminati (-554 LOC). Verifica: `Grep -rn "agent\.Runner\b" internal/ cmd/` → 0 matches in codice non-test, 0 matches sopravvissuti dopo il commit di rename.
- **G3.** `/api/chat` raggiunge feature parity con Telegram per quanto possibile a parità di endpoint sincrono: streaming events emessi internamente, permissive tool pool, event taps sul Router. Verifica: `cmd/probe_chat` riceve `ChatReply{}` con campi `{reply string, elapsed_ms int64, llm_calls int, tool_calls int, tokens int}` identici al baseline pre-restructure (vedi §5.8 per la collapse rule di `tokens` da `llm.TokenUsage` a scalar); il Router buffer contiene almeno `EventRunStarted`, `EventMessageDone`, `EventUsage`.
- **G4.** Composition root in `cmd/aura/app.go`. `internal/telegram/` riduce ai soli adapter (≤200 LOC totali per `bot.go + commands.go + handlers.go`; `documents.go` e `entity_markdown.go` non contano nel budget — sono utility file-handling e si spostano in `channels/telegram/`). Verifica: somma LOC dei 3 file core ≤ 200.
- **G5.** Hub spine (rinominato `internal/chat`) è la SOLA entry point verso `agent.Run`. Verifica: nessun caller in produzione invoca direttamente `agentruntime.Run` o `agentloop.Run` salvo via `chat.Hub.Receive*`. Test fixture eccettuati.
- **G6.** `internal/swarm/` continua a funzionare e dispatcha sotto-task tramite `Hub.ReceiveMessage(InboundMessage{Channel: swarm})`. Verifica: test esistente `swarm/manager_test.go` verde + nuova fixture E2E che mostra un Run padre + N Run figli sull'Hub con relazione `parent_run_id`.
- **G7.** Test suite stays green. Nessun test relaxed. Verifica: `go test ./... -race ./internal/{api,auth,mcp,skills}` zero failures sui commit di chiusura di ogni slice.
- **G8.** Zero variazioni di feature visibili in Telegram: CoT (`🧠 _cot_`) preservato, entity-rendered markdown preservato, throttle 600ms preservato. Verifica: probe Telegram-fixture confronta byte-per-byte la sequenza di edit emessi prima/dopo restructure su un prompt deterministico. **Fixture harness è prerequisito** (Commit 11.5 dedicato, vedi §6).
- **G9.** Build / vet sempre verde su ogni commit della migrazione. Nessun commit "rosso transitorio". Verifica gate: ogni commit § 6 esegue `go build ./... && go vet ./... && go test ./...` prima di essere merged.
- **G10.** Nessuna nuova dipendenza Go aggiunta a `go.mod`. Verifica: `git diff master..HEAD go.mod` solo cambi a indirect-deps.

---

## 3. Non-obiettivi

Esplicitamente fuori scope di questo PRD (anche se sembrano correlati):

1. **Non toccare la wiki.** `internal/wiki/store.go` + `store_graph.go` restano invarianti. La wiki È il graph (vedi memoria `project_graph_memory_core_strategy.md`). Nessun rename, nessun refactor.
2. **Nessun nuovo storage.** No KuzuDB, no Neo4j, no Zep, no graph-DB embedded.
3. **Nessun cambio embedding backend.** embeddinggemma-300m Q4_0 256-d via `aura-llama-embed` resta l'unico path.
4. **Nessun multimodal / vision.** Aura è text-only.
5. **Nessuna nuova dipendenza esterna.** No LangChain, no LlamaIndex, no GraphRAG, no DSPy.
6. **No React/UI rework.** Wave 2.11 è una phase separata.
7. **No mem0 ADD-only extraction.** Fuori scope.
8. **No SSE web streaming.** Endpoint `/api/chat/threads/{id}/stream` resta non implementato.
9. **No catalogo modelli dinamico, no per-thread model selector.**
10. **No event sourcing completo.** `chat_events` resta best-effort buffered.
11. **No CPU throughput tuning.**
12. **No RL training, no PARL reward, no orchestrator/subagent separation alla Kimi K2.5.** Il paper è stato deliberatamente rimosso dalla v2 come citazione architetturale — vedi §4.5 per il razionale.

---

## 4. Architettura target

### 4.1 Layout `internal/` (target ≤22 directory top-level — aritmetica esplicita)

Partenza: **49 directory** (verificato `ls internal/` con count 49).

| Operazione | Δ count | Count corrente |
|------------|---------|-----------------|
| Start | — | 49 |
| Merge `mcppolicy` + `mcpwatch` → `mcp/policy/`, `mcp/watch/` | −2 | 47 |
| Merge `agentloop` + `agentruntime` → `agent/` (loop.go + runtime.go) | −2 | 45 |
| Merge `tools` + `toolindex` + `toolsets` + `swarmtools` → `agent/tools/*` | −3 | 42 |
| Merge `search` + `qdrant` + `reindex` + `memoryindex` + `memoryquality` → `storage/search/*` (introduce `storage/`) | −4 | 38 |
| Merge `source` + `ocr` + `ingest` + `markitdown` → `storage/sources/*` | −3 | 35 |
| Merge `settings` + `runtimebootstrap` → `config/` | −2 | 33 |
| Merge `auth` + `setup` + `health` → `api/` | −3 | 30 |
| Rename `chathub` → `chat`, move `adapters/*` → top-level `channels/` (net: −1 perché `channels` è nuovo ma chathub sparisce) | −0 | 30 |
| Merge `concurrency` (UserGate) into new `session/` (introduce `session/`, retire `concurrency/`, no net change yet) | −0 | 30 |
| Move `agentruntime/sessions` content → `session/` (already collapsed in agentruntime merge) | 0 | 30 |
| Delete empty `orchestration/` | −1 | 29 |
| Delete empty `tracing/` | −1 | 28 |
| Move `release_config_test.go` from `release/` to `config/`, delete `release/` | −1 | 27 |
| Merge `dbrecovery/` into `db/` | −1 | 26 |
| Merge `debugguard/` into `db/` (or `logging/` per F6) | −1 | 25 |
| Rename `scheduler` → `cron` (NO LOC reduction, see §5.10) | 0 | 25 |
| **Final** | — | **25** |

<!-- Author response to F5: G1 originale (≤16) era irraggiungibile. Reviewer aritmetica diceva 28; mia rilavorazione qui conclude 25. Target G1 settato a ≤22 con stretch goal a 25. La differenza vs. il 22 nominale: se l'esecutore riesce a collassare `concurrency` dentro `session` (sì, è quello che §5.13 fa) e a unificare `db + dbrecovery + debugguard` in un solo package (non in tre sub-cartelle), si scende a 22. Il numero 25 è il "no collapsing forzato"; 22 è il "if tutti i merge ragionevoli passano". -->

```
internal/
├── agent/                  # Canonical agent loop + runtime + event types
│   ├── governance/         # microcompact, budget, tool-result caps (estratto da agentloop)
│   ├── memory/             # wiki retrieval helpers per il loop (capsule, BFS expand)
│   ├── skills/             # skill loader + manifest emitter
│   └── tools/              # tool registry + toolindex reconciler + toolsets + swarmtools
├── api/                    # http router + auth + setup wizard + health
├── backup/                 # Garage S3 backup (unchanged)
├── budget/                 # token + cost tracking (unchanged)
├── chat/                   # Hub spine (was: chathub)
├── channels/               # Per-channel adapters (was: chathub/adapters/*)
│   ├── telegram/
│   ├── web/
│   └── silent/
├── config/                 # config + settings + runtimebootstrap merged
├── conversation/           # message store + system_prompt + overlay + summarizer (unchanged)
├── cron/                   # was: scheduler — rename, keep responsibilities (issues/maintenance/wake/agent_job stay)
├── db/                     # SQLite migrations + dbrecovery + debugguard merged
├── files/                  # xlsx/docx/pdf generators (unchanged)
├── install/                # bootstrap install (Wave 2.10) (unchanged)
├── llm/                    # provider client (unchanged)
├── logging/                # zap helpers (unchanged)
├── mcp/                    # mcp client + policy + watch merged
├── sandbox/                # python sandbox (unchanged)
├── session/                # SessionStore + concurrency.UserGate (merged)
├── storage/                # File-backed stores
│   ├── archive/            # conversations archive
│   ├── search/             # search + qdrant + reindex + memoryindex + memoryquality
│   └── sources/            # source + ocr + ingest + markitdown
├── swarm/                  # Agent Swarm manager (unchanged shape, new backend)
├── telegram/               # Adapter shell ≤200 LOC: bot.go + commands.go + handlers.go
├── tray/                   # Windows tray (unchanged)
├── wiki/                   # locked invariant
└── workspace/              # workspace_files + exec dispatch (unchanged)
```

Top-level count: **22** (counted: agent, api, backup, budget, chat, channels, config, conversation, cron, db, files, install, llm, logging, mcp, sandbox, session, storage, swarm, telegram, tray, wiki, workspace = 23 with `workspace`, **22 if backup goes into storage/backup** as a `storage/backup/` sub-package — decisione default qui: **backup resta top-level** perché il suo dominio è "io con S3", non "file-backed store"; quindi target finale **23** se backup top-level oppure **22** se collassato).

<!-- Author response to F5: G1 = 22 è il target ufficiale. Se backup resta top-level si raggiunge 23 — acceptance G1 (§7) accetta ≤23 come hard ceiling, 22 come stretch. Acceptance NON dice più "≤16". -->

### 4.2 Dipendenze (acicliche) — verificabili in CI

Regole di import (forbidden = import diretto vietato). Comando CI:

```bash
# Forbidden edge: channels/* → agent
go list -deps ./internal/channels/... | grep -q 'aura/internal/agent$' && exit 1

# Forbidden edge: chat → channels
go list -deps ./internal/chat/... | grep -q 'aura/internal/channels' && exit 1

# Forbidden edge: agent → chat
go list -deps ./internal/agent/... | grep -q 'aura/internal/chat$' && exit 1

# Forbidden edge: api → telegram
go list -deps ./internal/api/... | grep -q 'aura/internal/telegram$' && exit 1
```

| Da | Può importare | NON può importare |
|----|---------------|--------------------|
| `chat` | `agent`, `session`, `llm`, `logging` | `channels/*`, `telegram`, `swarm`, `api` |
| `channels/*` | `chat`, `llm`, `logging`, `files` | `agent`, `agent/tools`, `mcp`, `wiki` |
| `agent` | `agent/governance`, `agent/memory`, `agent/skills`, `agent/tools`, `llm`, `conversation` | `chat`, `channels/*`, `telegram`, `api`, `swarm` |
| `agent/tools` | `mcp`, `storage/sources`, `wiki`, `storage/search`, `files`, `workspace`, `sandbox` | `chat`, `channels/*`, `swarm` |
| `swarm` | `chat`, `agent` (task shape only) | `channels/*`, `telegram` |
| `api` | `chat`, `storage/*`, `session` | `telegram`, `channels/*` |
| `telegram` | `channels/telegram`, `chat` | `agent`, `agent/tools` direttamente |

L'asse-chiave: **il Hub è l'unico arco tra una directory di canale e il sotto-sistema agentico.**

### 4.3 Data flow — turno utente Telegram

```
Telegram poll → channels/telegram/inbound.Normalize(tele.Context)
              → chat.Hub.Receive(ctx, ChannelTelegram, update)
              → chat.Hub.dispatch (mint runID, EventRunStarted)
              → chat.AgentLoopAdapter.Run
              → agent.Run (loop = governance.Apply → llm.Stream → tools.Execute → repeat)
              → emit OutboundEvent (MessageDelta, ToolStart, ToolEnd, MessageDone, Usage)
              → channels/telegram/outbound.Deliver (progressive edit + CoT + entity render)
              → EventDone
```

### 4.4 Data flow — Agent Swarm sotto-task

Aura ha già `internal/swarm/` (manager + plan + store). Il restructure lo aggancia all'Hub così che ogni sotto-task sia un `InboundMessage` con `Channel: "swarm"` (nuovo canale silent-mode) + `ChannelData: {parent_run_id, depth, assignment_id}`. Vantaggi:

- I sotto-task ereditano governance + permissive tool pool + event taps gratis.
- La concorrenza è limitata dalla `swarm.Manager` con i suoi `maxActive`/`maxDepth` esistenti.
- Output dei sotto-task è raccolto via `Router.WaitForRun(runID)` (~20 LOC helper nuovo).

Flow:

```
agent loop (parent) → tool call: swarm.dispatch(goal, assignments[])
                    → swarm.Manager.Run
                    → for each Assignment:
                        chat.Hub.ReceiveMessage(InboundMessage{
                            Channel: ChannelSwarm,
                            Mode: DeliveryModeDeferred,
                            ChannelData: {parent_run_id, assignment_id}
                        })
                    → channels/silent/swarm outbound (deferred, buffered)
                    → Manager collects N Result records → persisted to swarm.Store
                    → tool result returned to parent loop
```

**Nessuna nuova dipendenza.** `swarm.Manager` cambia solo il backend da `AgentRunner` (oggi `agent.Runner.Run`) a `chat.HubReceiver`. La `AgentRunner` interface in `swarm/manager.go:18-20` resta — viene implementata da una shim `swarmHubBridge` (~40 LOC).

### 4.5 Paper Kimi K2.5 — perché NON è citato come architettura

REVIEW-1 F7 ha osservato che la citazione del paper era decorativa: Aura non implementa orchestrator/subagent decoupling, non ha PARL reward (`r_parallel`/`r_finish`), e non misura `critical_steps`. Aura non fa RL training. La decisione v2 è di **rimuovere il paper come argomento architetturale** dal sommario, da §4 e da §12.

Il pattern parallel-decomposition esisteva in Aura (in `internal/swarm/`) prima della pubblicazione del paper. Citarlo creava una falsa apparenza di adozione del framework completo.

Se in futuro Aura volesse veramente operationalizzare il paper, servirebbero (in un PRD separato):
- Una separazione codepath orchestrator vs subagent (oggi entrambi sono `agent.Run`)
- Un meccanismo "frozen" per i subagent (no training, no prompt eval feedback)
- Un misuratore `max(subagent_steps)` per latency (oggi è `sum`)
- Una funzione di reward per evitare serial-collapse / spurious-parallelism

Questi sono lavori di scala >>10× rispetto al restructure. Out of scope.

<!-- Author response to F7: rimosso. Sopravvive solo come reference nota in §12 con disclaimer "pattern reference, no architectural adoption". -->

---

## 5. Modulo per modulo

Per ogni modulo: path → responsabilità → sorgenti → API → test gate → import vietati.

### 5.1 `internal/agent/`

- **Responsabilità:** Single canonical agent run path. Loop primitive + event-emitting envelope.
- **Sorgenti che migrano qui:**
  - `internal/agentloop/loop.go` (780 LOC) → `internal/agent/loop.go`
  - `internal/agentruntime/runner.go` (266) → `internal/agent/runtime.go`
  - `internal/agentruntime/*` ausiliari (`stats.go`, `policy.go`, `tools_exposed.go`) → `internal/agent/*.go`
  - `internal/agent/runner.go` (554) → **DELETE**
- **LOC budget post-merge:** ~1600 LOC (780 + 266 + ~250 governance estratta + ~300 phantom_guard preservato + ~50 helpers). Questo è il numero **derivato dalla feature surface verificata in §1.6**, non da picobot.
- **API esposta:**
  - `agent.Run(ctx, Invocation) (Stats, error)` (era `agentruntime.Run`)
  - `agent.Invocation{Client, Executor, State, OnEvent, MaxIterations, ToolAllowlist, ReasoningEffort, MaxToolResultChars, MaxCallsPerTool, FinalizationTimeout, CompleteOnDeadline}`
  - `agent.Event` + `EventType` (preserva: ToolsExposed, Stats, Final, LLMStart, LLMDelta, ToolStart, ToolEnd, QuestionRequested)
  - `agent.ChatClient` (era `agentloop.ChatClient`)
  - `agent.NoStreamClient(llm.Client) ChatClient` — nuovo (~30 LOC), wrappa `llm.Client.Send` per swarm/api/chat
- **Test gates:** unit test `loop_test.go` + table tests per phantom-guard, microcompact (delegated to `governance`), tool budget; integration test che dimostra streaming + tool execution + final.
- **Non importa da:** `chat`, `channels/*`, `telegram`, `swarm`, `api`.

### 5.2 `internal/agent/governance/`

- **Responsabilità:** Microcompact + budget + tool-result limits.
- **Sorgenti:**
  - Estratto da `agentloop.applyGovernance` (oggi inline in `loop.go`)
  - `conversation.Context.EnforceLimit` e `conversation.Context.CompactCompletedToolResults` (oggi in `internal/conversation/context.go` e `tool_compaction.go`) — restano dichiarati in `conversation` ma chiamano `governance.Apply`
- **API:**
  - `governance.Apply(messages []llm.Message, opts Options) (out []llm.Message, info Info)`
  - `governance.MicrocompactPolicy{KeepRecent int}`
  - `governance.ToolResultLimitPolicy{MaxChars int, MaxResults int}`
- **Test gates:** table tests `governance_test.go` su MicrocompactKeepRecent, ToolResultLimit, edge cases. Race detector verde.

### 5.3 `internal/agent/memory/`

- **Responsabilità:** Wiki retrieval capsule + speculative search helpers.
- **Sorgenti:** `internal/conversation/retrieval_capsule.go` → spostato qui se >200 LOC; altrimenti lascia in `conversation/`.
- **Test gates:** unit test esistenti preservati.

### 5.4 `internal/agent/skills/`

- **Responsabilità:** Loader + manifest emission + invalidate.
- **Sorgenti:** `internal/skills/*` (sub-path).

### 5.5 `internal/agent/tools/`

- **Responsabilità:** Tool registry, reconciler, toolsets, swarmtools, Wave 2.7 tool implementations.
- **Sorgenti:**
  - `internal/tools/*` (24+ file)
  - `internal/toolindex/*` (reconciler Wave 2.10.b)
  - `internal/toolsets/*`
  - `internal/swarmtools/*` — vedi nota su ordering commit §6 (F11)
- **Test gates:** test esistenti restano verdi. Race detector su Registry.

### 5.6 `internal/chat/`

- **Responsabilità:** Hub spine. Dispatcher channel-neutral, run/event ID, `/stop` registry, fan-out outbound adapter.
- **Sorgenti:**
  - `internal/chathub/types.go` → `internal/chat/types.go`
  - `internal/chathub/hub.go` → `internal/chat/hub.go`
  - `internal/chathub/agentloop.go` → `internal/chat/loop_adapter.go`
- **API:**
  - `chat.Hub`, `chat.New(cfg)`, `chat.Receive`, `chat.ReceiveMessage`, `chat.Stop(runID)`
  - `chat.InboundMessage`, `chat.OutboundEvent`, `chat.Run`, `chat.EventType`, `chat.Channel`, `chat.DeliveryMode`
  - `chat.AgentLoop`, `chat.InboundAdapter`, `chat.OutboundAdapter`
- **LOC budget:** **≤700 LOC totali** (oggi 668 LOC nei 3 file; il restructure preserva la spina e tenta riduzioni leggere — vedi sotto). Il budget v1 di ≤300 era irraggiungibile: il delta v1→v2 ammonta a +400 LOC perché la spina **carries**: 7 EventType cases (RunStarted, MessageDelta, ToolStart, ToolEnd, MessageDone, Usage, Done/Error/Cancelled), dispatcher con event-tap, run/event ID minting, `/stop` registry, fan-out outbound multi-channel. I tre "drops" elencati in v1 valgono <50 LOC.
- **Drop confermati (~30-50 LOC risparmiati):**
  - Drop matrice `(channel, mode) → []OutboundAdapter` (un solo outbound per canale; `msg.Mode` letto dall'adapter sul payload)
  - Drop fallback "take any adapter for channel" in `makeEmit:222-228` (errore esplicito se manca outbound)
- **`Run.Metadata` — KEEP (cambio v2):** verificato 8 production writes in `chathub/agentloop.go` + 3 read sites del payload map separato in `chathub/adapters/telegram/outbound.go`. Rimuovere `Run.Metadata` ora vuol dire reimplementare l'injection di `tele_bot/tele_recipient/tele_placeholder` come campo tipizzato di `InboundMessage.ChannelData` E re-pointing dei 7 caller di `Run.Metadata["…"]`. Costo: ~1 giornata di refactor isolato. Decisione v2: **mantieni `Run.Metadata`** in questa versione del PRD; un futuro PRD può proporre una migrazione tipizzata separatamente.
- **Test gates:** `hub_test.go` riscritto per non over-fittare la forma vecchia; copre dispatch, `/stop`, error paths, fan-out a outbound multipli.
- **Non importa da:** `channels/*`, `telegram`, `swarm`, `api`.

<!-- Author response to F2: budget ≤300 LOC era unsupportable. v2 = ≤700 LOC, basato sul reale ingombro post-drop e sulla feature surface verificata. Nessuna feature droppata per inseguire un numero. -->
<!-- Author response to F4: `Run.Metadata` KEEP. Rimozione spostata fuori scope di questo PRD (richiede un commit dedicato di ~1 giornata che il PRD non aveva contabilizzato). §11 open-questions §11.7 rimosso. -->

### 5.7 `internal/channels/telegram/`

- **Responsabilità:** Telegram inbound adapter (tele.Update → InboundMessage) + outbound adapter (OutboundEvent → progressive edit con CoT + entity rendering).
- **Sorgenti:**
  - `internal/telegram/streaming.go` (208 LOC) → `internal/channels/telegram/outbound.go`
  - `internal/telegram/entity_markdown.go` (84) → `internal/channels/telegram/entity_markdown.go`
  - `internal/telegram/conversation.go:25-90` (l'inbound shape) → `internal/channels/telegram/inbound.go`
  - `internal/chathub/adapters/telegram/outbound.go` → **DELETE** (regressione)
  - `internal/chathub/adapters/telegram/inbound.go` → `internal/channels/telegram/inbound.go` (merged)
- **API:**
  - `telegramch.NewInbound(...)` impl `chat.InboundAdapter`
  - `telegramch.NewOutbound(cfg)` impl `chat.OutboundAdapter`
- **Test gates:**
  - `entity_markdown_test.go`, `entity_markdown_table_test.go` (preservati)
  - `outbound_test.go` con record-and-replay fixture (vedi Commit 11.5)

### 5.8 `internal/channels/web/`

- **Responsabilità:** Buffered Router per `/api/chat`.
- **Sorgenti:**
  - `internal/chathub/adapters/web/outbound.go` → `internal/channels/web/router.go`
  - `internal/chathub/adapters/web/chat_service.go` → `internal/channels/web/inbound.go` (diventa l'InboundAdapter)
  - `internal/telegram/chat_service.go` (138 LOC) → **DELETE**
- **API:**
  - `webch.NewRouter(cfg) *Router` impl `chat.OutboundAdapter`
  - `webch.NewInbound(hub, router)` impl `chat.InboundAdapter`
  - `webch.ChatService` (per `api.ChatService` injection)
- **`ChatReply` shape — collapse rule esplicita:** v1 acceptance criterion G3 ("byte-identical") era under-specified. La forma corrente `api.ChatReply = {reply string, elapsed_ms int64, llm_calls int, tool_calls int, tokens int}` ha `tokens` come **scalar int**. Post-restructure, `agentruntime.Stats` carries `llm.TokenUsage{Prompt, Completion, CachedReadInput, …}`. **Collapse rule v2: `tokens_int := Prompt + Completion`** (escludendo cache_read perché non rappresenta lavoro nuovo, ed escludendo eventuali campi non esistenti pre-restructure). Documentato qui e applicato nel bridge `webch.ChatService.Chat`. Probe `cmd/probe_chat` asserta `reply["tokens"] == prompt+completion` con tolleranza 0.

<!-- Author response to F8: scalar tokens preserved con regola di collapse esplicita. Acceptance G3 (§7) cita la formula. -->

### 5.9 `internal/channels/silent/`

- **Responsabilità:** No-op outbound per heartbeat + cron + swarm channel.
- **Sorgenti:** `internal/chathub/adapters/silent/outbound.go` → `internal/channels/silent/outbound.go`.
- **API:** `silentch.NewHeartbeat(cfg)`, `silentch.NewCron(cfg)`, `silentch.NewSwarm(cfg)` (nuovo, +5 LOC).

### 5.10 `internal/cron/` — rename, NOT redefinition

Lo scheduler oggi contiene 13 file e copre **5 responsabilità** verificate: scheduler tick, `agent_job`, `issues` (wiki maintenance queue), `maintenance` (wiki rebuild periodico), `wake` (cold-start re-hydration). Rinominare `scheduler → cron` può sembrare misnomer.

**Decisione v2:** rename `scheduler/` → `cron/` MA dentro `cron/` si creano sub-cartelle `cron/jobs/` (agent_job + wake), `cron/maintenance/` (issues + maintenance) e `cron/tick/` (scheduler.go).

Inoltre: lo scheduler tick → `agent_job` execution è un **secondo production caller di `agent.Runner.Run`** (oltre a swarm). Commit 8 deve re-targettare anche questo path. Aggiunto come sub-step di Commit 8 (vedi §6).

<!-- Author response to F13: rename ok ma con sub-cartelle che preservano la lettura del dominio. agent_job→agent.Run come secondo target di Commit 8 — aggiunto al mapping table. -->

- **API esterna:** unchanged (callers oggi importano `scheduler.Scheduler` etc. — sed scriptato `scheduler` → `cron`).
- **Non importa da:** `channels/*`. Emette `chat.InboundMessage{Channel: ChannelCron}` e chiama `chat.Hub.ReceiveMessage`.

### 5.11 `internal/llm/`

Responsabilità: OpenAI-compatible HTTP client. Sorgenti/API: unchanged.

### 5.12 `internal/mcp/`

- **Responsabilità:** MCP client (stdio + HTTP) + policy + watch.
- **Sorgenti:**
  - `internal/mcp/*` (rimane)
  - `internal/mcppolicy/*` → `internal/mcp/policy.go` (merged)
  - `internal/mcpwatch/*` → `internal/mcp/watch.go` (merged)

### 5.13 `internal/session/`

- **Responsabilità:** SessionStore (era `agentruntime.SessionStore`), UserGate ownership.
- **Sorgenti:**
  - `internal/agentruntime/sessions.go` → `internal/session/store.go`
  - `internal/concurrency/usergate.go` → `internal/session/gate.go` (merged)
- **API:** `session.Store{Begin, Finish}`, `session.Gate`.

### 5.14 `internal/storage/`

Cartella ombrello per i 3 store file-backed (la wiki resta top-level).

- `internal/storage/archive/` — `internal/telegram/conversation_archive.go`, `internal/telegram/atomic_tables.go`.
- `internal/storage/search/` — merge di `internal/search/*` + `internal/qdrant/*` + `internal/reindex/*` + `internal/memoryindex/*` + `internal/memoryquality/*` (5 → 1).
- `internal/storage/sources/` — merge di `internal/source/*` + `internal/ocr/*` + `internal/ingest/*` + `internal/markitdown/*` (4 → 1).
- `internal/wiki/` — **unchanged**, eccezione justified.

### 5.15 `internal/config/`

- **Responsabilità:** Env loading (envconfig) + SQLite settings overlay + bootstrap runtime layout.
- **Sorgenti:**
  - `internal/config/*` (resta)
  - `internal/settings/*` → `internal/config/settings.go`
  - `internal/runtimebootstrap/*` → `internal/config/bootstrap.go`
  - `internal/release/release_config_test.go` → `internal/config/release_test.go` (rescue dal package single-test)

### 5.16 `internal/api/`

- **Responsabilità:** HTTP router + auth handler + setup wizard + health.
- **Sorgenti:**
  - `internal/api/*` (resta)
  - `internal/auth/*` → `internal/api/auth.go`
  - `internal/setup/*` → `internal/api/setup.go`
  - `internal/health/*` → `internal/api/health.go`

### 5.17 `internal/swarm/`

- **Responsabilità:** Agent Swarm — parallel sub-task orchestration.
- **Sorgenti:** unchanged. Solo il backend `AgentRunner` cambia.
- **Cambio interno:** `swarmHubBridge` implementa `swarm.AgentRunner` chiamando `chat.Hub.ReceiveMessage`.
- **Test gates:** `swarm/manager_test.go` test esistenti + nuovo test E2E parent-run + 3 child-run.

### 5.18 `cmd/aura/`

- **Responsabilità:** Composition root.
- **Sorgenti:**
  - `cmd/aura/main.go` (resta, sottile)
  - `cmd/aura/app.go` (**nuovo**) — corpo di `internal/telegram/setup.go::New` migrato qui. ~600 LOC realistici. Splittato in `app.go` (≤300 LOC top-level orchestration) + `wire_*.go` (un file per sotto-sistema: `wire_storage.go`, `wire_agent.go`, `wire_channels.go`, `wire_api.go`, `wire_mcp.go`).
- **API:** `app.New(cfg, db, logger) (*App, error)`, `app.Start()`, `app.Stop()`. `*App` ha ≤10 campi pubblici: `hub`, `cron`, `bot`, `api`, `bgCancel`, `bgWg`, `logger`, `cfg`, `mcp`, `db`.

---

## 6. Piano di migrazione — commit per commit

Ogni commit lascia il tree in stato verde. Numerazione per phase (post-REVIEW-1 raccomandazione #2):

> Convention: titolo `<type>(<scope>): <verb> <object>` come da CLAUDE.md / repo history.

### Phase 1 — Rename + merge (low risk)

#### Commit 1.1 — `chore(chat): rinomina chathub → chat`
- `git mv internal/chathub internal/chat`. `sed` su tutti gli import path. Rename package declaration.
- **Files toccati:** ~40 file. **Effort:** 1h. **Rischio:** Low.
- **Validation:** `go build ./... && go vet ./... && go test ./...`.

#### Commit 1.2 — `chore(mcp): merge mcppolicy + mcpwatch into mcp`
- `git mv internal/mcppolicy internal/mcp/policy/`, `git mv internal/mcpwatch internal/mcp/watch/`.
- **Effort:** 1.5h. **Rischio:** Low.

#### Commit 1.3 — `chore(storage/search): merge search + qdrant + reindex + memoryindex + memoryquality`
- Crea `internal/storage/search/`, sposta i 5 package come sotto-cartelle.
- **Effort:** 2h. **Rischio:** Low-Med.

#### Commit 1.4 — `chore(storage/sources): merge source + ocr + ingest + markitdown`
- `internal/storage/sources/{source,ocr,ingest,markitdown}/`.
- **Effort:** 1.5h. **Rischio:** Low.

#### Commit 1.5 — `chore(config): merge config + settings + runtimebootstrap + rescue release_test`
- **Effort:** 1h. **Rischio:** Low.

#### Commit 1.6 — `chore(api): merge api + auth + setup + health`
- **Effort:** 2h. **Rischio:** Low-Med.

#### Commit 1.7 — `chore(db): merge dbrecovery + debugguard into db; delete orchestration + tracing`
- Empty directory cleanup + dbrecovery/debugguard merge in `internal/db/{recovery,debugguard}.go`.
- **Effort:** 1h. **Rischio:** Low.

### Phase 2 — Single canonical runtime (the first hard cut)

#### Commit 2.1 — `refactor(agent): kill agent.Runner — route swarm + chatPipe + scheduler agent_job to agentruntime`

- **Cosa:**
  - Crea `internal/agentruntime/no_stream_client.go` (~30 LOC). Implementa `agentloop.ChatClient` wrappando `llm.Client.Send`.
  - In `internal/telegram/setup.go`, sostituisci `agent.NewRunner` con `chatPipeService` che chiama `agentruntime.Run` con `NoStreamClient`.
  - In `internal/swarm/manager.go` cambia `AgentRunner` binding: `Manager.runner` ora chiama `runViaAgentRuntime(task) (Result, error)`.
  - In `internal/scheduler/agent_job.go` (secondo caller di `agent.Runner.Run`), re-targetta su `agentruntime.Run` con `NoStreamClient`.
  - **DELETE** `internal/agent/runner.go`, `internal/agent/runner_test.go`, `internal/agent/README.md`.

- **Field mapping table — `agent.Task` → `agent.Invocation` / `agentloop.Options`:**

| `agent.Task` field | Destinazione | Semantica/gotcha |
|--------------------|-------------|-------------------|
| `SystemPrompt` | `Invocation.State.SystemPrompt` (via session.Init) | direct copy |
| `Prompt` | `Invocation.State.AppendUser(Prompt)` | direct |
| `Messages` | `Invocation.State.Messages` | if non-nil, sovrascrive SystemPrompt+Prompt |
| `ToolAllowlist` | `agentloop.Options.ToolAllowlist` | direct |
| `UserID` | `Invocation.UserID` | direct |
| `Temperature` | `Invocation.Temperature` (*float64) | direct (nil = use default) |
| `MaxToolCalls` | `agentloop.Options.MaxIterations` (NOT MaxCallsPerTool) | **GOTCHA:** `Task.MaxToolCalls` è budget totale; `MaxCallsPerTool` è per-tool. Mapping: `MaxIterations = Task.MaxToolCalls + 1` (1 round = 1 LLM call + ≤N tool calls). Se `Task.MaxToolCalls == 0` (unlimited), settare `MaxIterations = 0` (treated as unlimited dal loop). `MaxCallsPerTool` resta default (no per-tool cap). |
| `MaxToolResultChars` | `agentloop.Options.MaxToolResultChars` | direct |
| `FinalizationTimeout` | `agentloop.Options.FinalizationTimeout` | direct |
| `CompleteOnDeadline` | `agentloop.Options.CompleteOnDeadline` | direct |

- **Field mapping — `agent.Config` (runner construction-time) → equivalenti:**

| `agent.Config` field | Destinazione |
|---------------------|-------------|
| `LLM` | passed per-call via `agentloop.ChatClient` (wrappato da `NoStreamClient`) |
| `Tools` | `agentloop.Options.Tools` (registry) |
| `Model` | `Invocation.Model` (when set) |
| `MaxIterations` | `agentloop.Options.MaxIterations` |
| `Timeout` | `agentloop.Options.Timeout` |
| `ToolTimeout` | `agentloop.Options.ToolTimeout` |
| `ReasoningEffort` | `Invocation.ReasoningEffort` |
| `Logger` | `agentloop.Options.Logger` |
| `PhantomToolGuard` | `agentloop.Options.PhantomToolGuard` (preservato — vedi §1.6) |

- **Effort:** 6-8h (mapping verification, swarm test, scheduler agent_job test). **Rischio:** Med-High.
- **Validation:** standard + `go test -race ./internal/swarm/... ./internal/telegram/... ./internal/api/... ./internal/scheduler/...`. Probe `cmd/probe_chat`.
- **Rollback:** `git revert` clean — `agent.Runner` torna in vita, callers tornano a usarlo.

<!-- Author response to F9: mapping table esplicita. F13: scheduler/agent_job aggiunto come terzo caller da re-targettare in questo commit. -->
<!-- Author response to F11: ordering — Commit 2.1 (kill Runner) viene PRIMA del merge tools (Phase 3.2). swarmtools resta nel suo package corrente durante 2.1 e si muove SOLO al merge tools (Phase 3.2) dove si chiede di rifare il wiring via `swarm.Manager.AgentRunner` (che da 2.1 già punta ad agentruntime). Quindi non ci sono "stale wiring" intermedi. -->

### Phase 3 — Agent package consolidation

#### Commit 3.1 — `refactor(agent): merge agentloop + agentruntime → internal/agent`
- `git mv internal/agentloop/* internal/agent/loop.go`. `git mv internal/agentruntime/* internal/agent/runtime.go`. Cambia package declaration. Sed import.
- **Effort:** 2h. **Rischio:** Low (Commit 2.1 ha eliminato il vecchio `agent.Runner`).

#### Commit 3.2 — `chore(agent/tools): merge tools + toolindex + toolsets + swarmtools`
- Crea `internal/agent/tools/`. Sposta i 4 package. **Nota:** post-Commit 2.1, swarmtools/tools.go non chiama più `agent.Runner.Run` direttamente (passa per `swarm.Manager.AgentRunner` che già delega ad agentruntime). Nessun stale wiring.
- **Effort:** 2.5h. **Rischio:** Low-Med.

#### Commit 3.3 — `refactor(agent/governance): extract governance package from agent loop`
- Sposta `agentloop.applyGovernance` + `MicrocompactPolicy` + `ToolResultLimitPolicy` in `internal/agent/governance/`.
- `agent.Run` chiama `governance.Apply` al posto del codice inline.
- `conversation.Context.EnforceLimit` mantiene firma esterna ma delega a `governance.Apply`.
- **Effort:** 1 giorno. **Rischio:** Med.

### Phase 4 — Channels migration

#### Commit 4.1 — `refactor(channels): create internal/channels/{telegram,web,silent}` (move adapters)
- `git mv internal/chat/adapters/* internal/channels/`. Sed import path.
- **Effort:** 1.5h. **Rischio:** Low.

#### Commit 4.2 — `test(channels/telegram): record-and-replay fixture for streaming edits` **(NEW commit per F12)**
- **Cosa:** Crea un harness `internal/channels/telegram/fixture/` con:
  - `mockBot.go` — struct che implementa `tele.API` registrando `Send`/`Edit`/`SendDocument` calls in ordine, con timestamp e payload completo.
  - `mockContext.go` — costruttore di un `tele.Context` minimo per testing (shim attorno al `tele.Context` reale popolato a mano dal `Update` fixture).
  - `fixture_capture.go` — funzione `Capture(t, scenario string, builder func(ctx tele.Context) ) []EditCall`. Esegue il path attuale (pre-restructure `streaming.go::consumeStream`) e snapshot in `testdata/<scenario>.json`.
  - 3 scenari deterministici: `simple_reply`, `with_cot`, `with_tool_call_and_entity_table`.
- **Effort:** 1 giorno.
- **Rischio:** Med — il design del shim richiede compromessi (non puoi instanziare `tele.Context` puramente, ha campi unexported); soluzione = builder utility nella fixture.
- **Validation:** `go test ./internal/channels/telegram/fixture/...` deve produrre tre snapshot JSON committable.

<!-- Author response to F12: fixture harness promosso a commit dedicato 4.2. Commit 4.3 (porting outbound) consume gli snapshot come baseline. -->

#### Commit 4.3 — `refactor(channels/telegram): port streaming.go as canonical outbound, delete chathub regressed outbound`
- Sostituisce `internal/channels/telegram/outbound.go` con il body di `internal/telegram/streaming.go::consumeStream` ri-architettato come `chat.OutboundAdapter`. Preserva: 600ms throttle, CoT, `renderForTelegramEntities`, entity fallback.
- **DELETE** la versione regressa precedente.
- **Validation:** `go test ./internal/channels/telegram/...` confronto byte-per-byte contro gli snapshot di Commit 4.2.
- **Effort:** 1 giorno. **Rischio:** **High**.
- **Rollback:** `git revert` clean.

### Phase 5 — Hub adoption (feature-flagged)

#### Commit 5.1 — `feat(app): introduce AURA_USE_HUB feature flag + wire web through hub`
- In `cmd/aura/main.go`, aggiungi flag env `AURA_USE_HUB` (default `false`).
- Se `true`: `/api/chat` routes via `webch.NewInbound` + `chat.Hub.ReceiveMessage`.
- Telegram resta sul path attuale a prescindere dal flag in questo commit.
- **Effort:** 2-3h. **Rischio:** Low.

#### Commit 5.2 — `refactor(telegram): wire Telegram conversation through chat.Hub (behind flag)`
- In `cmd/aura/app.go` (o setup.go), quando `AURA_USE_HUB=true`, Telegram polling delega a `channels/telegram/inbound.Normalize` + `chat.Hub.Receive(ChannelTelegram, update)`.
- `Bot.handleConversation` resta in vita ma non viene chiamato quando il flag è on.
- **Effort:** 1 giorno. **Rischio:** **High**.

### Phase 6 — Swarm + cron via Hub

#### Commit 6.1 — `refactor(swarm): route sub-tasks through chat.Hub as Channel=swarm`
- Nuovo `chat.Channel = "swarm"`. Nuovo `silentch.NewSwarm(cfg)`.
- `swarm.Manager` accetta un `chat.HubReceiver` invece di `AgentRunner`. `swarmHubBridge` traduce `agent.Task → chat.InboundMessage` e raccoglie il `chat.Run` finale via `Router.WaitForRun(runID)` (~20 LOC helper).
- **Effort:** 1 giorno. **Rischio:** Med.

#### Commit 6.2 — `chore(cron): rename scheduler → cron + dispatch via hub`
- `git mv internal/scheduler internal/cron`. Sub-cartelle `cron/jobs/`, `cron/maintenance/`, `cron/tick/`. Cron tick produce `chat.InboundMessage{Channel: ChannelCron, Mode: DeliveryModeSilent}`.
- **Effort:** 0.5 giorno. **Rischio:** Low.

### Phase 7 — God-class extraction (the hard one)

Splittato in 3 sub-commit per F10. Ogni sub-commit ha proprio validation gate.

#### Commit 7.1 — `refactor(app): move telegram setup.go wiring functions to cmd/aura/wire_*.go` (keep *Bot)
- Crea `cmd/aura/app.go` con stub `*App` (≤80 LOC).
- Estrai le funzioni di wiring (`wireStorage`, `wireAgent`, `wireChannels`, `wireAPI`, `wireMCP`) da `setup.go::New` in `cmd/aura/wire_*.go`.
- `*Bot` resta in vita; `setup.go::New` chiama le wire functions ma restituisce ancora `*Bot`.
- **Effort:** 12-16h. **Rischio:** Med — molto IO ma niente cambio di lifetime.
- **Validation:** standard + `wc -l cmd/aura/*.go` reports each file ≤300.

#### Commit 7.2 — `refactor(app): extract background goroutine ownership (bgCtx/bgCancel/bgWg) to *App`
- Sposta `bgCtx`, `bgCancel`, `bgWg` ownership da `*Bot` a `*App`. Tutti i goroutine producers (toolReconciler, mcpwatch, etc.) ricevono `app.bgCtx` invece di `bot.bgCtx`.
- **Effort:** 8-12h. **Rischio:** **High** — race conditions su shutdown se non perfetto. Validation: `go test -race ./...`.

#### Commit 7.3 — `refactor(telegram): delete legacy fields from *Bot + delete obsolete files`
- `*Bot` viene ridotto a wrapper `tele.Bot` + Send/Document helpers. Fields rimossi: 32 dei 38 originali (lasciando solo `bot`, `cfg`, `loc`, `logger`, `api`, `started`).
- **DELETE** `internal/telegram/conversation.go`, `internal/telegram/streaming.go`, `internal/telegram/chat_service.go`, `internal/telegram/conversation_*.go`, `internal/telegram/setup.go`.
- **Effort:** 12-20h. **Rischio:** **High** — last-mile cleanup, sopratutto sui test files che importavano tipi rimossi.
- **Validation:** standard + manual soak 48h con `AURA_USE_HUB=true`.

**Subtotale Phase 7:** 32-48h. <!-- Author response to F10: re-stima da 20h a 32-48h, split in 3 sub-commit con gate indipendenti. -->

### Phase 8 — Cleanup

#### Commit 8.1 — `chore(session): merge agentruntime/sessions + concurrency/usergate → internal/session`
- **Effort:** 1h. **Rischio:** Low.

#### Commit 8.2 — `chore(app): drop AURA_USE_HUB flag (legacy path removed)`
- Dopo 48h di soak. **Effort:** 30min. **Rischio:** Low.

### Riepilogo effort

| Commit | Effort (h) | Rischio |
|--------|------------|---------|
| 1.1 rinomina chathub→chat | 1 | L |
| 1.2 merge mcp | 1.5 | L |
| 1.3 merge storage/search | 2 | LM |
| 1.4 merge storage/sources | 1.5 | L |
| 1.5 merge config | 1 | L |
| 1.6 merge api | 2 | LM |
| 1.7 cleanup empty + db merge | 1 | L |
| 2.1 kill agent.Runner | 8 | MH |
| 3.1 merge agent loop+runtime | 2 | L |
| 3.2 merge agent/tools | 2.5 | LM |
| 3.3 extract governance | 8 | M |
| 4.1 channels/ rename | 1.5 | L |
| 4.2 fixture harness (NEW) | 8 | M |
| 4.3 telegram outbound port | 8 | **H** |
| 5.1 feature flag + web hub | 2.5 | L |
| 5.2 telegram hub wire | 8 | **H** |
| 6.1 swarm via hub | 8 | M |
| 6.2 cron rename + via hub | 4 | L |
| 7.1 wire_*.go split | 14 | M |
| 7.2 goroutine ownership | 10 | **H** |
| 7.3 *Bot field drop | 16 | **H** |
| 8.1 merge session | 1 | L |
| 8.2 drop flag | 0.5 | L |
| **Totale** | **~110h** | |

A 4 ore produttive/giorno: **~28 giorni** = **~6 settimane calendar**.

---

## 7. Acceptance criteria (Definition of Done)

Checklist eseguibile. Ogni linea è un comando o assertion fattibile.

- [ ] **G1 — Package count.** `(Get-ChildItem internal -Directory).Count` ≤ **23** (stretch goal 22). Aritmetica verificata in §4.1.
- [ ] **G2 — Single runtime.** `Grep -rn 'agent\.Runner|NewRunner\(' internal/ cmd/ -g '!*_test.go'` ritorna **0** match.
- [ ] **G3 — /api/chat shape (scalar tokens preserved).** `curl POST /api/chat -d '{"message":"ping"}'` ritorna JSON con campi `{reply, elapsed_ms, llm_calls, tool_calls, tokens}` byte-identici al baseline. `tokens` è scalar int, calcolato come `Prompt + Completion` (collapse rule §5.8). Probe asserta valore numerico = baseline ± 5 (tolleranza dovuta a non-determinismo LLM).
- [ ] **G4 — Telegram shrink.** Somma LOC `internal/telegram/{bot.go,commands.go,handlers.go}` ≤ **200**. `documents.go`, `entity_markdown.go` non contano (spostati in `channels/telegram/` o utility).
- [ ] **G5 — Hub is sole entry.** `Grep -rn 'agent\.Run\(' internal/ cmd/ -g '!*_test.go' -g '!internal/chat/*'` ritorna **0** match.
- [ ] **G6 — Swarm via hub.** Nuovo test `internal/swarm/hub_e2e_test.go` esegue un RunRequest con 3 Assignment paralleli; verifica che lo store contiene 3 task records con `parent_run_id` valorizzato e che il Router buffer ha 3 RunIDs distinti.
- [ ] **G7 — Tests green.** `go test ./... -race ./internal/{api,mcp,agent}` zero fail. `go vet ./...` zero warning nuovi.
- [ ] **G8 — CoT preserved + byte-comparison.** Probe Telegram-fixture: prompt deterministico produce sequenza di `bot.Edit` calls IDENTICA al snapshot di Commit 4.2 (byte-by-byte JSON diff = empty).
- [ ] **G9 — Entity rendering preserved.** Probe Telegram-fixture: prompt che forza markdown tabella produce edit con `tele.Entity` Bold/Pre/Code sui campi corretti.
- [ ] **G10 — go.mod clean.** `git diff master..HEAD -- go.mod` shows zero new direct dependencies.
- [ ] **G11 — Composition root visible.** `cmd/aura/app.go` esiste, contiene `func New(cfg, db, logger) (*App, error)`. `internal/telegram/setup.go` non esiste più. `cmd/aura/*.go` ciascuno ≤300 LOC.
- [ ] **G12 — No regressed dead code.** `Grep -rn 'chathub' internal/ cmd/` ritorna 0 match (post-rename verify).
- [ ] **G13 — Probe artifact verification.** `cmd/probe_chat` esegue un prompt "scrivi un xlsx con 3 righe X,Y,Z" e:
  1. assert reply text mentions file
  2. unzip artifact, parse `xl/sharedStrings.xml`, assert X/Y/Z presenti
  3. assert `chat_events` SQLite row contains EventToolEnd for `create_xlsx`
- [ ] **G14 — Soak test 48h, with metrics.** `AURA_USE_HUB=true` in produzione per 48h. Metriche misurate vs baseline pre-restructure: **error rate ≤ baseline + 2 errors / 1000 messages**, **p95 latency ≤ baseline + 100ms**, **RSS growth slope ≤ 5 MB/hour** (gauge da `docker stats`). Telegram + /api/chat + cron + swarm tutti operativi.
- [ ] **G15 — Documentation updated.** `docs/wave-3-chathub-slice0.md` archived; `CLAUDE.md` § Architecture aggiornato.
- [ ] **G16 — Dependency edges enforced in CI.** I 4 `go list -deps` di §4.2 sono parte di una task in `scripts/check_arch.sh` o equivalente.

<!-- Author response to F14 (G14 metric definition): error rate / p95 / RSS slope esplicitati. -->

---

## 8. Invarianti e regole non negoziabili

| Invariante | Origine | Come il PRD lo onora |
|------------|---------|----------------------|
| Master-direct git workflow, no feature branches/PR | `memory/feedback_master_direct_workflow.md` | Ogni commit § 6 sta su `master`. |
| Wiki IS the graph; no KuzuDB/Neo4j/Zep | `memory/project_graph_memory_core_strategy.md` | `internal/wiki/` non viene toccato. |
| Embedding backend locked = embeddinggemma 256d | `memory/feedback_embedding_backend_stays_mistral.md` | Nessun cambio embedding. |
| Mini-PC CPU budget, ≤4 thread embed sidecar | `memory/feedback_minipc_cpu_budget.md` | Hub I/O bound. |
| GPU = wrong tool for embedding workload | `memory/feedback_gpu_not_for_embedding_workload.md` | N/A. |
| Hyper-V loopback port-forwarding lies | `memory/feedback_hyperv_port_forwarding_lie.md` | N/A — Aura non probes via loopback. |
| Probe verifica artifact, non reply | `memory/feedback_probe_must_verify_artifact_not_reply.md` | Acceptance § 7 G13. |
| Inspect actual artifact, never superficial | `CLAUDE.md` "NEVER BE SUPERFICIAL" | G13 unzip-parse-assert. |
| Tests verify quality, not just "did it run" | `CLAUDE.md` "TESTS VERIFY QUALITY AND METRICS" | Ogni acceptance § 7 ha asserzione di valore. |
| Aura must always know her tools exist | `memory/feedback_agent_must_know_tools_exist.md` | Commit 2.1 elimina divergenza `chatPipeService` vs `conversation.RenderSystemPrompt`. |
| Mini-LLM CPU non viable per tool retrieval; use Embed cosine + permissive fallback | `memory/feedback_minillm_cpu_not_viable_for_tool_retrieval.md` | Commit 3.2 merge di tools NON introduce LLM tool-routing; manifest+cosine retained. |
| Niente regex su linguaggio naturale | `memory/feedback_no_regex_for_nlp.md` | Phantom guard usa proximity-after-strip, non regex generico. |
| Phantom guard needs proximity | `memory/feedback_phantom_guard_requires_proximity.md` | Commit 3.3 preserva `phantom_guard.go` invariato. |
| Debug via live pipe, not logs | `memory/feedback_debug_via_live_pipe_not_logs.md` | Validation gates §6 usano `cmd/probe_chat` non solo log scrape. |
| Inspect artifact visually, not just PASS | `memory/feedback_inspect_artifact_visually_not_just_pass_status.md` | G13 inspect xlsx contents. |
| God class / no >600 LOC | `CLAUDE.md` GOD CLASS | G4 ≤200 telegram core; `cmd/aura/*.go` ≤300 each. |
| REUSABLE CODE — never duplicate | `CLAUDE.md` | Commit 2.1 elimina `agent.Runner` duplicato. |
| 3-strike rule | `CLAUDE.md` | Se Commit 4.3 fallisce 3 volte, STOP. |
| NEVER MODIFY TESTS TO MAKE THEM PASS | `CLAUDE.md` | Test esistenti restano gold standard. |
| Boot non-fatal degradation | `CLAUDE.md` Conventions | `chat.Hub` registra warning, non panic, su adapter mancante. |

<!-- Author response to F15: aggiunta riga "Mini-LLM CPU non viable per tool retrieval" (era mancante in v1). -->

---

## 9. Rischi e mitigazioni

| Rischio | Probabilità | Impatto | Mitigazione | Early-warning indicator |
|---------|-------------|---------|-------------|-------------------------|
| Regressione Telegram production (Commits 4.3, 5.2, 7.x) | Media | **Alto** | Feature flag `AURA_USE_HUB`; byte-comparison test pre/post (fixture Commit 4.2); manual E2E con bot reale. | Snapshot diff > 0 byte; user-visible CoT missing. |
| Swarm test break durante kill `agent.Runner` (Commit 2.1) | Media | Medio | Mapping table esplicita §6; `MaxToolCalls=0` → `MaxIterations=0`. | `go test ./internal/swarm/...` fail. |
| Scheduler agent_job break durante kill `agent.Runner` | Media | Medio | Commit 2.1 include scheduler re-targeting; test in scheduler/agent_job_test.go. | scheduler test fail post-Commit 2.1. |
| Web `/api/chat` JSON shape drift (Commit 5.1) | Bassa | Medio | Collapse rule esplicita §5.8 (`tokens = Prompt+Completion`); probe asserta byte shape. | `cmd/probe_chat` decode fail. |
| Test flakiness durante package rename (Commits 1.x) | Alta | Basso | Mechanical sed; `goimports -w` post-sed. | `go vet` warning. |
| Import cycle introdotto da merge (Commits 1.3, 1.4, 1.6) | Media | Medio | `go list -deps ./...` pre-validate ogni commit. CI enforcement §4.2. | Build error `import cycle not allowed`. |
| `cmd/aura/app.go` diventa nuovo god-file (Commit 7.x) | Media | Medio | Hard split: `app.go` ≤300 LOC, `wire_*.go` ≤300 LOC each. | `wc -l cmd/aura/*.go` > 300. |
| Background goroutine race condition (Commit 7.2) | Media | **Alto** | `go test -race ./...` come hard gate. `bgCtx` ownership migration testata con shutdown stress test. | `-race` failures su Stop/Start. |
| Cron tick latency increases via Hub dispatch (Commit 6.2) | Bassa | Basso | Hub dispatch in-process; misurato < 1ms overhead. | Cron job p95 latency > baseline + 50ms. |
| Phantom-guard regression dopo migrazione (Commit 2.1, 3.3) | Bassa | **Alto** | Phantom guard migra senza modifiche logica. Test esistenti `phantom_*_test.go` preservati. | `phantom_guard_test.go` fail. |
| Microcompact behavior drift (Commit 3.3) | Media | Medio | Governance estratta con table-test fixture pre/post identici. | `governance_test.go` failure. |
| Soak test reveals memory leak in hub | Bassa | Medio | Hub usa `sync.Map.Delete(runID)` in `defer`. Soak monitora RSS. | RSS slope > 5 MB/hour. |
| Fixture harness shim incompleto (Commit 4.2) | Media | Medio | Builder utility, no instantiation diretta di `tele.Context`. Test del harness stesso. | Fixture cannot reproduce production sequence. |
| Documentation goes stale | Alta | Basso | `CLAUDE.md` § Architecture aggiornato dentro Commit 7.x. | grep cita `chathub` in docs vivi. |

---

## 10. Stima totale

Per phase, 4h produttive/giorno:

| Phase | Commits | Effort (h) | Calendar (giorni) |
|-------|---------|------------|-------------------|
| Phase 1 Rename + merge | 1.1—1.7 | 10 | 2.5 |
| Phase 2 Single runtime | 2.1 | 8 | 2 |
| Phase 3 Agent consolidation | 3.1, 3.2, 3.3 | 12.5 | 3.5 |
| Phase 4 Channels | 4.1, 4.2, 4.3 | 17.5 | 4.5 |
| Phase 5 Hub adoption | 5.1, 5.2 | 10.5 | 3 |
| Phase 6 Swarm + cron | 6.1, 6.2 | 12 | 3 |
| Phase 7 God-class extraction | 7.1, 7.2, 7.3 | 40 | 10 |
| Phase 8 Cleanup | 8.1, 8.2 | 1.5 | 0.5 |
| **Totale** | 23 commits | **~110h** | **~29 giorni produttivi ≈ 6 settimane calendar** |

Soak time addizionale (48h × 2 flag-flips) = ulteriori 4 giorni calendar di osservazione passiva.

**Honesty note v2:** `aura-rebuild-strategy.md` § 5 stimava 2-3 settimane; v1 PRD aveva ricalibrato a 4 settimane; v2 post-REVIEW-1 ricalibra ulteriormente a **6 settimane** dopo aver internalizzato: (a) fixture harness Commit 4.2 (1 giorno aggiunto), (b) split Phase 7 in 3 sub-commit (16h aggiunti), (c) scheduler agent_job re-targeting in Commit 2.1 (2h aggiunti), (d) governance microcompact drift è realistico 1 day non 0.5. La stima 6 settimane include la 3-strike rule: se Phase 7 fallisce ripetutamente, lo split in sub-commit permette di tenere 7.1 e revertare 7.2/7.3.

---

## 11. Decisioni prese (era: open questions)

REVIEW-1 raccomandazione #5: le default answers diventano decisioni, non open questions.

1. **Storage path naming — `internal/wiki/` separato top-level.** Justified: wiki È il graph; merita visibilità.
2. **`internal/conversation/` resta top-level.** Justified: conversation è data store di messaggi, non logica agent.
3. **`internal/concurrency/` merge target = `session/`.** Justified: UserGate è chat-session-related.
4. **Swarm event streaming: batch final result.** Justified: parent loop riceve `Result.Content` come tool result, niente streaming intermedio nei sotto-task. Manager UI può aggiungere live progress in PRD futuro.
5. **`chat.Channel = "swarm"`, outbound silent.** Justified: distingue log/metrics swarm da heartbeat/cron.
6. **Test fixture per byte-identical Telegram outbound = record-and-replay con mock `tele.API`.** Justified: deterministico, riproducibile. Vedi Commit 4.2.
7. **`Run.Metadata` resta.** Vedi §5.6: rimuoverlo costa ~1 giornata e tocca 7+ caller production + un payload-map parallelo nel telegram regressed outbound. Fuori scope di questo PRD.
8. **Backup directory top-level o in `storage/`.** Decisione: **top-level**. Dominio "io con S3" è ortogonale a file-backed store.
9. **Paper Kimi K2.5 come citazione architetturale.** Decisione: **dropped** (vedi §4.5). Sopravvive solo in §12 come reference nota.

---

## 12. Riferimenti

### Documenti
- `D:\tmp\aura-rebuild-strategy.md` — strategy doc, autoritativo su zone di disordine e 3-cut surgery
- `D:\Aura\docs\chat-interface-prd.md` v0.1 — Wave 3.0 ChatHub PRD; § 2.1, § 2.4, § 11.5 onorate
- `D:\Aura\docs\aura-restructure-prd-REVIEW-1.md` — Reviewer 1 Architecture
- `D:\Aura\CLAUDE.md` — behavioral rules
- `D:\Aura\docs\wave-3-chathub-slice0.md` — Slice 0 scope (sarà archiviato post-restructure)

### Paper (reference only, NOT architectural — vedi §4.5)
- **arXiv:2602.02276** — Kimi K2.5: Visual Agentic Intelligence. Citato come **prior art reference** del pattern parallel-decomposition. Aura **non implementa** orchestrator/subagent decoupling, PARL reward, né critical-steps metric. Out-of-scope: visual side, RL training.

### Reference implementations
- `D:\tmp\picobot\` — Go, 9 internal package, 615 LOC core (chat 99 + agent loop 369 + telegram channel 147). Gold standard di **shape** (non di LOC — vedi §1.6 feature matrix).
- `D:\tmp\nanobot\` — Python, validates picobot shape scales.

### Codice Aura — file chiave referenziati
- `internal/telegram/bot.go:40-81` — god class definition (38 fields)
- `internal/telegram/setup.go` (1036 LOC, 32 import interni) — composition root in disguise
- `internal/telegram/conversation.go:25-90` — Telegram inbound shape
- `internal/telegram/streaming.go:1-208` — production progressive-edit (must survive port)
- `internal/telegram/chat_service.go:29-138` — chatPipeService (DELETE in Commit 2.1)
- `internal/agent/runner.go:39-92` — duplicate runtime (DELETE in Commit 2.1)
- `internal/agentruntime/runner.go:1-95` — canonical event-emitting wrapper
- `internal/agentloop/loop.go:1-100` — canonical primitive
- `internal/chathub/types.go` (174) + `hub.go` (274) + `agentloop.go` (220) — spine to preserve
- `internal/chathub/agentloop.go` — 8 `Run.Metadata` writes (lines 94, 96, 99, 102, 205, 214, 215)
- `internal/chathub/adapters/telegram/outbound.go:126-134` — REGRESSED + payload map reads
- `internal/chathub/adapters/web/{outbound,chat_service}.go` — keep as `internal/channels/web/`
- `internal/chathub/adapters/silent/outbound.go` — keep as `internal/channels/silent/`
- `internal/swarm/manager.go:18-80` — `AgentRunner` interface (re-target to Hub)
- `internal/scheduler/agent_job.go` — second production caller of `agent.Runner.Run` (re-target Commit 2.1)

### Memory notes (HARD invariants — section 8 cites all)
- `memory/feedback_master_direct_workflow.md` — commit on master
- `memory/project_graph_memory_core_strategy.md` — wiki IS the graph
- `memory/feedback_embedding_backend_stays_mistral.md` — embeddinggemma locked
- `memory/feedback_minipc_cpu_budget.md` — ≤4 threads
- `memory/feedback_gpu_not_for_embedding_workload.md` — CPU-bound
- `memory/feedback_hyperv_port_forwarding_lie.md` — N/A
- `memory/feedback_agent_must_know_tools_exist.md` — canonical tool-aware prompt
- `memory/feedback_probe_must_verify_artifact_not_reply.md` — verify artifacts
- `memory/feedback_no_regex_for_nlp.md` — structured triggers
- `memory/feedback_phantom_guard_requires_proximity.md` — phantom guard preserved
- `memory/feedback_debug_via_live_pipe_not_logs.md` — debug method
- `memory/feedback_inspect_artifact_visually_not_just_pass_status.md` — visual inspection
- `memory/feedback_minillm_cpu_not_viable_for_tool_retrieval.md` — Embed cosine + manifest + permissive fallback (added v2)
- `memory/project_open_gaps_2026-05-13.md` — inventory; questo restructure NON chiude 2.10.c (MCP reload), NON chiude 2.11 (React setup), NON chiude 2.9.5 (GLM-OCR).

— END v2 —
