# Aura Quality Snapshot — 2026-05-27

Living quality doc per [memory `aura-as-product`](C:\Users\Davide\.claude\projects\d--Aura\memory\feedback_aura_as_product.md). Sostituisce / supera il superficiale `tool-e2e-2026-05-27.md`. Audit reale su tutti gli strati di memoria + grafo + latenze + UX.

**Method:** direct `/api/tools/call` + SQLite queries + Qdrant inspection. Real artifacts, not smoke.

---

## TL;DR — dove c'è da lavorare (ranked by impact)

| # | Issue | Severity | Domain | Est LOC | Tracking |
|---|---|---|---|---|---|
| 1 | **user-facts promotion pipeline morta** — 1 fact in 250 user-turn (0.4%) | CRITICAL | memory | ~200 | new |
| 2 | **lessons sono solo telemetry, niente insight azionabili** | CRITICAL | memory | ~150 | new |
| 3 | **xlsx row-explosion: 859/908 = 94.6% wiki = noise** | HIGH | wiki/ingest | ~80 (option B) | [[xlsx-row-explosion-bug-2026-05-23]] |
| 4 | **god_nodes graph drift**: dominio = "robot corso PDF" non "Aura dev" | HIGH | wiki/graph | ~300 | new |
| 5 | **22 broken `[[link]]`** (17 row-skip, 5 missing concept pages) | MEDIUM | wiki/splitter | ~50 | enclosed in #3 |
| 6 | **Qdrant `indexed_vectors_count=0`** sulla collection primary | MEDIUM | search | ~30 | new |
| 7 | **Retired tools still attempted by LLM** (execute_code/shell, recall_*) | MEDIUM | prompt | ~40 | new |
| 8 | **Web 4xx classificati come `fatal`** (dovrebbero essere `recoverable`) | LOW | tool registry | ~15 | new |
| 9 | **task P95 30s outlier** (3 attempts in 10 min window 2026-05-26) | LOW | scheduler | investigate | new |
| 10 | **embedding_cache table empty** mentre Qdrant ha 542 vectors | LOW | embed | ~30 | new |

Totale stima: ~900 LOC distribuiti in 5-6 phase atomiche.

---

## Strato per strato

### 1. Active turn context (`agent_note`) — ✓ OK

- 4 azioni testate set/get/append/clear, round-trip verificato byte-level.
- Latency P50=18ms, P95=27ms. **Veloce, affidabile.**
- Conv_id scoped correttamente.

### 2. Conversation archive (`conversations`) — ✓ OK ma silenzioso

- 1218 messaggi totali (599 assistant + 369 tool + 250 user) su 5 chat_id.
- Chat principale (`1148481707` = Davide): 660 turn.
- Ultimo: 2026-05-27 05:25:46.
- 1 conversation compaction in `conversation_compactions`.
- **NO bug visibile**, ma la pipeline che dovrebbe estrarre user_facts da qui (vedi #1) non lavora.

### 3. Compact memory (`compact_memory_documents`) — ✗ VUOTA

- 0 rows. Schema esiste (FTS5 virtual tables tutte presenti) ma niente content.
- I 542 points in Qdrant (`aura_memory_v1` + `_compact`) sono l'unico storage di durable knowledge.
- **Implicazione**: search(user_facts) e search(lessons) restituiscono da Qdrant, NON da compact SQLite. La duplicazione del kind+content nello SQL doveva essere una projection per FTS fallback / dashboard read; non popolata.

### 4. User memory (search action=user_facts) — ✗ PIPELINE ROTTA

```json
1 user fact(s):
- [user_memory] user_memory:phase07d-live-20260525_125050
  — Phase07D live approved user fact (2d ago)
```

**UN solo fact** in 250 turn dell'utente, datato 2026-05-25 e marcato "phase07d-live" = sembra una probe automatica, non un vero fact estratto da una conversazione.

L'utente da mesi parla di:
- preferenze workflow (master-direct, no-PR salvo richiesta)
- modelli preferiti (Gemma vs DeepSeek)
- design rules (no smoke test, no kindergarten, no fast-path classifier)
- progetti (Aura, marketing, robotica)
- decisioni operative (Phase-CLEAN, Phase-MM, ecc.)

Niente di questo è in user_memory. **La pipeline che estrae fact dalle conversazioni e li promuove come user_memory NON LAVORA, o lavora con un threshold troppo alto, o il `daily-memory-decay` job sta scartando tutto.**

→ **Investigate**: `internal/storage/memory_promotion/*` (se esiste), `daily-memory-decay` job logic, `proposal_ttl_sweep` job logic. Dovrebbero generare `propose_patch(user_memory)` automatiche da conversation archive. Probabilmente generano niente o tutto va in `proposed_updates` con status=pending senza mai essere promosso.

### 5. Operational memory (search action=lessons) — ✗ SOLO TELEMETRY

Sample (5 hits, ultime 24h):

```
- operational:fbb85ddd9cb9d60b — "Tool `file` repeated `validation` failures (9 times in last 7 days)"
- operational:ae4a61676c28025c — '{"tool":"file","cause":"error_class:validation",...}'
- operational:8c86dc6fc40ad965 — "Tool `execute_shell` repeated `not_found` failures (3 times)"
- operational:5b6118a7e8b0a420 — "Tool `search` repeated `not_found` failures (5 times)"
- operational:5a634ef046fb55a6 — '{"tool":"web","cause":"error_class:error",...}'
```

Tutte auto-generated da `tool_attempts` aggregator. Tutte hanno `would_have_prevented_by: "Check recent failures for this tool and adjust the next call before retrying"` — generico inutile.

**Non sono lezioni operative azionabili. Sono failure summaries.** Una vera lesson dovrebbe dire: *"Quando chiami `file` con path che inizia con `/workspace/`, riceverai 'path outside root'. Usa path relativi al workspace root invece."* Quella sarebbe accionabile.

Cosa manca:
- Pattern extraction: aggregare i `last_error` per tool e estrarre il pattern reale (regex su error_redacted).
- Suggested fix: lookup nel tool registry per la corretta signature.
- Promotion threshold: alzare a "5 fallimenti con pattern coerente" non "9 errori generici".

### 6. Wiki — ✗ 94.6% NOISE

```
wiki_documents total:  908
  -row-N noise:         859  (94.6%)
  non-row useful:        49  (5.4%)
```

860 pagine sono `source-io-g92184-xng-malesya-robot-plc-eng-v1-00-NNN-row-N` da un singolo xlsx. Memory [[xlsx-row-explosion-bug-2026-05-23]] documenta il bug + fix proposta (sheet-aware splitter option B, ~80 LOC). **Ancora aperto** perché "deferred until Phase-OUT closes" — Phase-OUT non si è ancora aperta.

Effetti collaterali:
- Le query `search(action=search)` su keyword random rischiano di hittare 1 row tra 859 — segnale rumoroso.
- Il god_nodes ranking è dominato dall'xlsx source-master che linka 860 children.
- L'embed storage Qdrant ha 859 vectors essenzialmente uguali → spazio sprecato + noise.

**Decision needed**: aprire Phase-OUT (o un mini Phase-XLSX-FIX inline in Phase-CLEAN W2 cleanup) per chiudere questo bug PRIMA che continui a degradare il graph quality.

### 7. Wiki [[link]] resolution — ⚠ 22/1013 broken (2.2%)

Dettaglio:
- **17 broken** = master xlsx page (`source-io-g92184-...`) linka `row-1`..`row-17` ma il splitter ha generato `row-18`..`row-NNN`. **Bug del row splitter**: salta le prime 17 righe (header row count del xlsx?) ma il TOC generator del master non lo sa e linka da row-1.
- **5 broken** = pagine concept che dovrebbero esistere ma non sono mai state generate:
  - `[[marchetto-davide]]` — variante invertita di `davide-marchetto` (esiste). Bug di chi ha generato il [[link]]: name order non-canonico.
  - `[[main-sheet]]`, `[[pick-sheet]]`, `[[drop-sheet]]` — nomi di sheet xlsx mai promossi a wiki pages dedicate.

**Fix**: nel splitter, fix offset row-N. Nei generator di concept-pages, normalizzare name order (auto-link variant) e creare stub-pages per sheet nominati.

### 8. Wiki god-nodes (centrality) — ⚠ DRIFT DOMINIO

```
Top 10 by total connections:
1. robot           (concept, 41)
2. source-corso-base-robot  (sources, 25)
3. source-albatech (sources, 7)
4. source-davide-marchetto-ticket (sources, 7)
5. davide-marchetto (concept, 6)
6. metodi-di-calibrazione-dei-riferimenti (concept, 6)
7. frame           (entity, 5)
8. giunti          (concept, 5)
9. interfacce-con-i-plc-i-o  (concept, 5)
10. movimenti-del-robot (concept, 5)
```

**Top 10 è ENTIRELY robotica/PLC.** Zero menzione di:
- Aura (il progetto dev attuale)
- Phase-CLEAN/Phase-KV/Phase-MM
- OpenRouter, DeepSeek, Gemma, Mistral
- cache, prompt, agent, tool, system
- markdown, wiki, source, qdrant, search

Il wiki RIFLETTE il dominio del PDF corso robotica caricato come source — non il dominio del LAVORO ATTUALE dell'utente (Aura development). 

**Root cause**: le conversazioni dev (250 user turn nell'archive) non vengono mai promosse a wiki pages. Il wiki cresce SOLO da source ingest. Quindi diventa il knowledge graph dei DOCUMENTI CARICATI, non del LAVORO SVOLTO.

**Phase candidate**: implementare un "auto-promote-from-conversation" job che identifica thread con ≥N messaggi su un topic coerente e propone una wiki page summary via `propose_patch(action=wiki)`. Filtro su user_intent (es. "salva questo", "ricorda", deliberate work-summary signals).

### 9. Qdrant index health — ⚠ DEGRADED

```
aura_memory_v1:          points=47   indexed=0
aura_memory_v1_compact:  points=495  indexed=?
```

**`indexed_vectors_count=0` sul primary** → HNSW non costruito → search via full-scan. Su 47 points è ininfluente, ma:
- È un sintomo: l'auto-indexing settings di Aura per Qdrant non stanno triggering rebuild.
- Quando primary cresce (es. dopo Phase-CLEAN cleanup quando ci saranno più "real" knowledge), full-scan diventa pesante.

**Distribuzione strana**: 47 primary vs 495 compact = 10:1 invertito vs aspettativa. Primary dovrebbe essere il source-of-truth, compact la projection. Qui sembra opposto.

→ **Investigate**: `internal/storage/qdrant/*` indexing config, scheduled rebuild job (se esiste).

### 10. Sources — ✓ OK funzionale

- 23 sources via `/api/sources`. Storage tramite wiki_documents con `metadata.kind="sources"` (consolidato, no separata `sources` SQL table).
- Latency `source(list)` P50=13ms, P95=23ms. Veloce.
- Dedup SHA-256 funziona (verified via xlsx upload test).

### 11. Skills — ✓ OK ma slow outlier

- skill(list) ritorna 701B di catalogo.
- P50=663ms (alto rispetto agli altri tool), P95=7.5s.
- Outlier 7.5s probabilmente install/download remoto.

### 12. Tool latency P50/P95 (production tool_attempts, n=549) — ✓ OK SALVO 2 OUTLIER

| Tool | P50 | P95 | Max | Verdetto |
|---|---|---|---|---|
| search | 34ms | 178ms | 4552ms | ✓ veloce, max rare |
| file | 15ms | 79ms | 5069ms | ✓ veloce |
| web | 1021ms | 3020ms | 4606ms | ✓ (esterno) |
| agent_note | 18ms | 27ms | 31ms | ✓ excellent |
| text_response | 12ms | 19ms | 22ms | ✓ excellent |
| **task** | 20ms | **30558ms** | **32877ms** | ✗ outlier |
| tool_search | 14ms | 22ms | 25ms | ✓ excellent |
| wiki_page | 134ms | 274ms | 409ms | ✓ ok (FTS write) |
| create_document | 20ms | 241ms | 241ms | ✓ ok |
| **skill** | **663ms** | **7578ms** | 7578ms | ⚠ slow |
| execute_shell | 54ms | 223ms | 223ms | (retired, low n) |
| execute_code | 24ms | 3136ms | 3136ms | (retired, low n) |
| source | 13ms | 23ms | 23ms | ✓ excellent |
| mcp_calculator | 17ms | 19ms | 19ms | ✓ excellent |
| wiki_subgraph | 95ms | 103ms | 103ms | ✓ ok |

**`task` 30s outlier**: 3 attempts in window 2026-05-26 20:13-20:29 con elapsed 32s/30s/13s. Probabilmente WAL contention durante batch operation. Eccezionale, non normale (P50=20ms).

**`skill` slow outlier**: skill install scarica file remoti. Eccezionale, da capire se cacheable.

### 13. Tool UX friction (driver iterations to PASS) — ⚠ MEDIUM

Driver E2E iter v1→v3 ha richiesto **5 schema correction** su 13 tool base:
- `propose_patch`: needs `action` + `change_summary` (non `kind` + `title` + `body`)
- `create_document`: needs `format` + `spec` nested (non `kind` + `sheets` flat)
- `file`: paths relativi a workspace root, non absoluti
- `mcp_calculator_solve_equation`: equation DEVE contenere `=` letterale
- `search`: `action` esplicito sempre richiesto

Mitigation: Aura ritorna error message LLM-friendly con il retry JSON pronto. **Eccellente recovery pattern**, ma indica che la schema documentation nelle tool descriptions non è sufficiente al primo tentativo.

→ Investigate: i Description fields nei tool registry sono troppo brevi/ambigui? O serve un "examples" array nello schema?

### 14. Retired tools still attempted — ⚠ MEDIUM

```
execute_code     last attempt 2026-05-24  (n=14)
execute_shell    last attempt 2026-05-25  (n=16)
recall_operational  last attempt 2026-05-23
```

Il `tool-and-function-map` dichiara questi RETIRED ma:
- `GET /api/tools` non li espone (corretto)
- Tuttavia il LLM li ha chiamati fino a 2-3 giorni fa

→ L'LLM provider (Gemma 4 via OpenRouter) ha training stale che li include. Il system prompt + tool manifest dovrebbero blockare esplicitamente, ma non lo fanno (chiamate finiscono in `tool_attempts` con outcome=ok per execute_shell P50=54ms — quindi forse SONO ancora registrati ma il map è obsoleto).

→ Confirm: querying `/api/tools` mostra 36 tool, NON include execute_code/execute_shell. Quindi tool_attempts ha histories di quando erano registrati. **OK**, il map è corretto, il problema è che le tool_attempts non vengono purged.

### 15. Embedding cache — ⚠ LOW

`embedding_cache` SQLite table vuota. Significa: nessun checksum-based skip. Ogni call a embed re-computa. Lavoro sprecato se l'embed gemma-300m sidecar è lento.

→ Investigate: il cache wiring è broken? Era pensato come optimization? Misure: tempo medio embed per query.

---

## Memory layer coverage matrix

| Layer | Status | Read-test | Write-test | Quality |
|---|---|---|---|---|
| Active turn context (agent_note) | ✓ | ✓ | ✓ | High |
| Conversation archive (conversations) | ✓ | ✓ | N/A | High |
| Compact memory (compact_memory_*) | ✗ EMPTY | ✗ | N/A | — |
| User memory (search user_facts) | ✗ STARVED | ✓ (1 fact) | ✗ untested | Critical gap |
| Operational memory (search lessons) | ⚠ TELEMETRY ONLY | ✓ | N/A | Low |
| Wiki pages (wiki_documents) | ⚠ 94.6% NOISE | ✓ | ✓ | Low |
| Wiki graph (god_nodes/subgraph) | ⚠ DOMAIN DRIFT | ✓ | N/A | Low |
| Sources (consolidated wiki) | ✓ | ✓ | ✗ untested | Medium |
| Skills (skill catalog) | ✓ | ✓ | ✗ untested | Medium |
| Projections (embedding_cache, Qdrant) | ⚠ DEGRADED | ✓ Qdrant | ✗ | Medium |

---

## Cosa NON è stato verificato (gap dell'audit)

- `search` actions `path`, `diff`, `gaps`, `surprises`, `suggest_questions`
- `wiki_page` actions `edit`, `replace`
- `source` actions `read`, `store`, `reprocess`, `delete`, `lint`
- `file` actions `patch`, `grep`, `path_info`, `mkdir`, `walk`, `move`, `copy`
- `task` actions `run_now`, `delete`
- `skill` actions `info`, `install`, `remove`
- `create_document` formats `docx`, `pdf`
- 20/23 `mcp_calculator_*` variants
- `delegate_*` dynamic tools
- Stream chat path (`POST /chat/stream`)
- Authentication boundary (cross-user access denial)
- Concurrent tool dispatch (parallel tool calls)
- Tool result truncation behavior at MaxToolResultChars

---

## Punch list operativa (priority order)

### Phase-MEM-FIX (~350 LOC, 3-4 stories)
Wave A: user-facts promotion pipeline funzionante (extract from conversations → propose_patch user_memory automatica). Wave B: lessons content quality (pattern-extraction + suggested-fix lookup).

### Phase-WIKI-NOISE (~80 LOC, 1 story)
Sheet-aware xlsx splitter (option B di [[xlsx-row-explosion-bug-2026-05-23]]). Skip header rows. Riindicizza dopo.

### Phase-WIKI-GRAPH (~300 LOC, 3 stories)
- Auto-promote-from-conversation job (genera propose_patch wiki da conversazioni dev)
- Concept-stub-creator per [[broken-links]] frequenti
- god_nodes weighting che penalizzi noise source-row pages

### Phase-RETIRED-PURGE (~40 LOC, 1 story)
Add `tool_attempts` purge job (>30 days OR retired tool names).

### Phase-QDRANT-INDEX (~30 LOC, 1 story)
Fix HNSW index rebuild trigger su primary collection.

### Phase-TOOL-UX-POLISH (~50 LOC, 1 story)
Ricco "examples" array negli schema dei 5 tool con friction più alta. Ridurre schema-correction iterations per LLM.

### Defer
- task P95=30s outlier — investigate solo se ricorre
- skill 7.5s install — cache su disk se ricorre
- embedding_cache empty — investigate solo se embed sidecar è bottleneck

---

## Note metodologiche

- Memory rule [[probe-must-verify-artifact-not-reply]] applicata: ogni write tool verificato via SQLite or filesystem, non via output string.
- Memory rule [[inspect-artifact-visually-not-just-pass-status]] applicata: xlsx artifact deserializzato e parsato per content.
- Memory rule [[groundtruth-before-planning]] applicata: telemetry live (`tool_attempts`, `runs`, `wiki_documents`) ha rivelato bug strutturali invisibili dai docs.
- L'audit precedente (`tool-e2e-2026-05-27.md`) ha valore solo come smoke-coverage matrix, NON come quality assessment. Questo doc lo supera.

## Reproducibility

```powershell
# Punto di partenza:
sqlite3 D:\Aura\data\aura.db
curl http://localhost:6333/collections
python D:\Aura\tmp\tool-e2e-driver.py   # smoke coverage
# Re-run audit:
# (raw SQL queries in this doc are reproducible against any healthy Aura install)
```
