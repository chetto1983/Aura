# Piano — migrazione del substrato grafo da Neo4j ad ArcadeDB

Sessione del 2026-07-31. Sostituisce la valutazione "fork neo4j-agent-memory vs
turing_AgentMemory_MCP" di `docs/handoff-2026-07-31-graphrag-vs-turing.md`: la
domanda era quale dei due adottare, la risposta misurata è **nessuno dei due**.

---

## Esito della valutazione

`turing_AgentMemory_MCP` **non si adotta**. Non per la qualità dei risultati — su
quella pareggia — ma per il costo e la fragilità dello strato Python che lo separa
dal database.

Testa a testa sul corpus misto (3.221 chunk, 6 documenti, 5 formati: pdf, docx,
pptx, xlsx, epub):

| | turing (Python) | ArcadeDB nativo |
|---|---:|---:|
| risposta corretta in posizione 1 | sì | sì |
| latenza p50 | 1234–2045 ms | **25 ms** |
| di cui reranker | ~860 ms (70%) | — |
| di cui GLiNER per query | 339 ms | — |
| traversata grafo effettiva | ~2 ms | ~2 ms |
| bug latenti trovati | **3** | 0 |

Domanda di controllo su entrambi («Su quale principio è fondata la Repubblica
italiana secondo l'articolo 1?»): stesso chunk, stessa posizione. La differenza è
tutta nel percorso, non nel risultato.

### I tre bug, tutti nello strato che sparirebbe

1. **Immagine stale.** `server.py` nel container leggeva `GLINER_MODEL` invece di
   `GLINER_MEMORY_MODEL`; la fix era già nel working tree, l'immagine non era mai
   stata ricostruita. Effetto: *ogni* scrittura di messaggio falliva.
2. **`end` è parola riservata in ArcadeDB**, ed è riservata in **due** posizioni:
   come identificatore nudo e come nome di bind parameter. `end = :p_end` e
   `` `end` = :end `` sono entrambi errori di sintassi; solo `` `end` = :p_end ``
   passa. Effetto: gli archi `MENTIONS` non venivano **mai** scritti, quindi
   niente entità, fatti, community — e ogni ricerca riportava
   `degraded_channels: ["graph"]` senza altro sintomo.
3. **`}}` in un literal non-f-string** di una concatenazione implicita (solo il
   primo frammento era f-string). Il `MATCH` a due salti era sintatticamente
   invalido. Effetto: canale grafo morto in ogni deployment, da sempre.

Corretti in `D:/turing_AgentMemory_MCP` (`ids.py`, `store_memory_queries.py`,
`store_retrieval_queries.py`, più `tests/test_store_memory_queries_reserved_words.py`
e `tests/test_entity_traversal_statement_shape.py` nuovi). **Non committati** —
vedi §Decisioni aperte.

Dopo le fix: 5 canali attivi, `degraded_channels: []`, `max_hop: 2`, 40 entità /
4 fatti / 5 community dove prima erano zero.

---

## Cosa dà ArcadeDB, verificato sul binario 26.7.3

Verificato eseguendo, non leggendo la documentazione — che su questo punto **è
avanti al prodotto**.

**C'è:**

- `vector.neighbors` / `vector.sparseNeighbors` — indici `LSM_VECTOR` e
  `LSM_SPARSE_VECTOR`, JVector (`jvector-4.0.0-rc.8-hf1.jar` nell'immagine)
- `SEARCH_INDEX` — full-text Lucene nativo
- **`vector.fuse(<s1>, …, <sN>[, {fusion:'RRF'|'DBSF'|'LINEAR', k, weights,
  groupBy, groupSize, limit}])`** — fusione ibrida **dentro il database**, con
  pesi per canale e deduplica per documento
- `MATCH` / Cypher nativo (OpenCypher 25, 97,8% TCK), Bolt su 7687 se si abilita
  il plugin `Bolt:com.arcadedb.bolt.BoltProtocolPlugin`
- server MCP integrato (`POST /api/v1/mcp`, JSON-RPC 2.0, auth + permessi
  granulari `allowReads`/`allowInsert`/…)

**Non c'è, malgrado la documentazione:**

- i tool MCP `vector_search`, `full_text_search`, `hybrid_search`,
  `upsert_entity`, `upsert_relationship` — il binario ne espone **10** e sono
  tutti generici (`list_databases`, `get_schema`, `query`, `execute_command`,
  `server_status`, 3 di profiling, 2 di settings)
- bind parameters nel tool MCP `query`: accetta solo
  `(database, language, query, limit)`. Per un vettore a 1024 dimensioni è
  inutilizzabile, e per testo utente è un'iniezione. **Il percorso programmatico
  è l'API HTTP `/api/v1/query/{db}`**, che i `params` li accetta; il server MCP
  serve a un LLM che esplora a mano.
- chunking e generazione di embedding: restano fuori dal database, sempre.

`DBSF` va usato sapendo cosa fa: nella prova i punteggi erano esattamente `2.0` e
`1.0`, cioè **il conteggio dei canali che hanno trovato la riga**, non una
distribuzione normalizzata. Come segnale di groundedness funziona ("l'hanno
trovata entrambi i canali" vs "uno solo"); come confidenza calibrata no.

---

## La forma del wrapper

```
markitdown (sidecar Aura, esiste)  →  chunker (Go)  →  embed (sidecar Aura, 8 ms)
                                            ↓
      ArcadeDB — store + vector.fuse + MATCH  →  UNA query SQL, p50 25 ms
```

Sparisce: fusione RRF a mano, orchestrazione dei canali, reranker, GLiNER.
Resta, e Aura ce l'ha già: markitdown, embed, un client HTTP.

---

## Superficie della migrazione in Aura

| superficie | quantità |
|---|---:|
| file Go **non-test** che citano Neo4j | 68, su 24 package |
| file di **test** che lo citano | **78** ← la coda che domina il calendario |
| file con **Cypher scritto dentro** | **11** |
| migrazioni Cypher (`internal/knowledge/migrations/`) | 8 |
| funzioni APOC distinte | 4 |
| frontend grafo | ~1.600 LOC |

**In produzione non esiste un driver Neo4j nativo.** Gli unici import di
`neo4j-go-driver` stanno in `.planning/spikes/`. Tutto passa da un solo collo:
`knowledge.Client` → `cypherTransport` → `mcp-neo4j-cypher`.

I due seam su cui innestare:

```go
// internal/knowledge/client.go:27
type cypherTransport interface {
    CallTool(context.Context, string, map[string]any) (string, error)
    Close() error
}

// internal/knowledge/graphview.go:38
type GraphReader interface {
    Read(ctx context.Context, query string, params map[string]any) ([]map[string]any, error)
}
```

`GraphReader` combacia 1:1 con l'API ArcadeDB: `POST /api/v1/query/{db}` prende
`{language, command, params}` e restituisce `{result: [...]}`, cioè
`[]map[string]any` con `params`.

### Gli 11 file con Cypher dentro

- `internal/documents/` — **6**: `graphrag.go`, `indexer.go`, `retrieve.go`,
  `search.go`, `backfill.go`, `corpus_revision.go` ← il peso è qui
- `internal/knowledge/` — 2: `graphview_intent.go`, `reset.go`
- `internal/adaptive/graph.go`
- `internal/cron/handlers/backup.go`
- `internal/eval/retrieval_eval.go`

### APOC — 34 chiamate, 4 funzioni

| funzione | usi | sostituzione |
|---|---:|---|
| `apoc.convert.toJson` | 25 | marshal in Go |
| `apoc.map.removeKey` | 6 | `delete()` in Go |
| `apoc.meta` | 4 | introspezione nativa (`SELECT FROM schema:types`) |
| `apoc.export.cypher.all` | 2 | export nativo ArcadeDB |

### Indici vettoriali

`VECTOR INDEX` (Neo4j HNSW 1024d cosine) → `LSM_VECTOR`. Tocca
`internal/knowledge/migrations/0001_init.cypher`,
`0007_chunk_embedding_1024.cypher`, `internal/documents/retrieve.go`.

### Frontend — deciso: modello dati **e** qualità

| file | LOC | destino |
|---|---:|---|
| `web/src/graph/graphIntent.ts` | 343 | riscritto sul modello ArcadeDB (gemello di `graphview_intent.go`) |
| `web/src/graph/GraphExplorer.tsx` | 511 | **ridisegnato** — interazione + estetica, non solo wiring |
| `web/src/graph/graphApi.ts` | 31 | nuovo contratto |
| test unitari + e2e | 602 + 2 spec | riscritti |
| `web/src/i18n/resources.graph.ts` | 135 | chiavi nuove, en+it |

Vincoli: §Frontend_aesthetics di `CLAUDE.md`, palette blu già accettata, barra
"premium non minimale", gate coverage ≥85% + Stryker ≥70%.

---

## Ordine di esecuzione

Dal più informativo al più costoso. **Neo4j resta in piedi** finché il punto 3
non è verde.

1. **Client ArcadeDB dietro `GraphReader`** — file nuovo, non tocca nulla.
2. **Verifica sui 3.221 chunk già caricati** in ArcadeDB: stesse domande, stesse
   risposte, p50 atteso ~25 ms. Se non coincidono, il piano si ferma qui.
3. Percorso di **lettura** di `internal/documents` (`search.go`, `retrieve.go`).
4. Scritture, 8 migrazioni, DDL indici vettoriali.
5. CLI e cablaggio: `cmd/aura/neo4j.go`, sottocomandi `neo4j_reset` /
   `neo4j_migrate` / `neo4j_cypher`, env `NEO4J_*` → `ARCADEDB_*`.
6. Frontend: intent, poi ridisegno dell'esploratore.
7. I 78 file di test.

Il punto aperto che nessuno dei due sistemi risolve: **le community Leiden**.
ArcadeDB non fa community detection; turing le calcola con graspologic in Python.
Coerente con `project_leiden_rerank_external_sidecars`, che le aveva già collocate
in un sidecar esterno.

---

## Stato lasciato sulla macchina

- ArcadeDB portato da `26.7.1` a **`26.7.3`** (3 advisory di sicurezza in mezzo,
  una è *MCP transport permission check bypass*; più un fix di perdita di archi su
  grafi ad alto grado). Pin in `D:/turing_AgentMemory_MCP/compose.yaml`.
- Server MCP nativo di ArcadeDB **acceso in sola lettura**
  (`allowReads:true`, tutto il resto `false`, solo `root`, bound su `127.0.0.1`).
  Si spegne con `POST /api/v1/mcp/config {"enabled": false}`.
- `aura-tts` e `aura-stt` **fermi**, per liberare ~1,2 GB prima dell'ingest.
- `aura-rerank` è salito a 3,13 GiB sotto i benchmark (partiva da 1,14).
- Corpus di prova caricato e riutilizzabile: 3.221 chunk in ArcadeDB, database
  `agentmem_t_v1_bc533360…`, file in `/bertoni/data/corpus/` dentro il container.
- Doppia registrazione MCP `turing-agentmemory` / `turing-memory` verso lo stesso
  `:8095` — da ripulire nella user config.
- `D:/turing_AgentMemory_MCP/tests/test_gliner_provider.py` è a **613 LOC**, oltre
  il cap 600 del loro stesso gate. Preesistente, non toccato.

---

## Decisioni aperte

1. **Le fix a turing si committano?** Valgono a prescindere (il repo resta un
   banco di prova), ma se il wrapper vince, quel codice non entra mai in Aura.
2. **Il lavoro Aura non committato** dell'handoff precedente
   (`bridge_memory.go` con la soglia di recall, `bridge.go`,
   `bridge_memory_threshold_test.go`, `bridge_user_identifier_test.go`) — vale
   comunque: `memory_search` documenta `threshold` e nessuno lo passava.
3. **Memoria in conflitto**: `project_graph_db_neo4j_stays_alternatives_rejected`
   dice di restare su Neo4j. Le misure di oggi la contraddicono su ArcadeDB. Da
   riscrivere solo con via libera esplicita.
4. **Mai testato**: i 18.147 PDF Normattiva (13 ZIP, 920 MB compressi in
   `test/normattiva/pdf_vigente/`). A 25 ms per query la lettura regge; il costo
   è tutto nell'ingest.
