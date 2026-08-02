# Handoff — memoria e documenti su ArcadeDB

Sessione del 2026-07-31, seguito di `docs/handoff-2026-07-31-graphrag-vs-turing.md`.
Il prompt da incollare è la sezione [Prompt](#prompt); il resto lo giustifica.

---

## Prompt

> Riprendo il lavoro su ArcadeDB come substrato di memoria e documenti per Aura.
> Leggi `docs/handoff-2026-07-31-arcadedb-memory.md` per il contesto, poi:
>
> 1. **Verifica che i tool `mcp__aura-arcadedb__*` siano nativi** (server registrato,
>    container su `127.0.0.1:8096`). Se ci sono, **usali** — niente script Python per
>    parlarci. Se non ci sono, dimmelo subito.
> 2. **Il test che manca è quello vero e va fatto per primo**: carica
>    `D:/turing_AgentMemory_MCP/test/Costituzione.pdf` e `Clienti.xlsx` — prima quelli
>    due, non tutto il mucchio — convertendoli con il sidecar `aura-markitdown`, e
>    scrivendoli con il **Cypher che Aura ha già** (`internal/documents/indexer.go`),
>    non con codice nuovo. Poi fai domande con risposta verificabile e **mostrami il
>    testo che torna**, non un PASS.
> 3. **Non reintrodurre**: embedding, reranker, fusione, GLiNER. Sono stati misurati e
>    scartati, i numeri sono qui sotto.
> 4. **Prima gira in produzione, poi il test pinna quello che hai imparato.** I test
>    verdi su fixture inventate non valgono niente: oggi cinque bug sono passati sotto
>    una suite verde e li ha trovati tutti il database.

---

## Addendum 2026-07-31 16:40 — verificato; corregge il resto del documento

**Il punto 2 del prompt non è ancora stato eseguito.** Questa sessione è finita sul
punto 1, e ha trovato che tre affermazioni qui sotto sono false.

1. **I tool MCP c'erano: era la chiave di progetto.** `~/.claude.json` aveva **due voci
   per la stessa cartella** — `D:/Aura` (con `aura-arcadedb`) e `d:/Aura` (con
   `turing-memory`). La sessione risolve `d:/Aura`, quindi non vedeva il server. Non era
   «i tool non entrano a sessione avviata».
   **Risolto**: `aura-arcadedb` spostato a **scope user**, dove il maiuscolo non lo può
   più dividere; le due registrazioni `turing-*` (morte, `:8095`) rimosse da ogni scope.
   Backup `~/.claude.json.bak-arcadedb`. Ora `claude mcp list` dà
   `aura-arcadedb: http://127.0.0.1:8096/mcp (HTTP) - ✔ Connected`.

2. **I tool sono SETTE, non quattro.** Oltre ai quattro tabellati, il server espone
   `document_ingest`, `document_search`, `document_delete`. Contratto completo
   (`tools/list` dal vivo) salvato in **`docs/arcadedb-mcp-live-tools.json`**.

3. **Il sorgente di quei tre tool non è nel repo.** `newServer()` in
   `cmd/arcadedb-mcp/main.go` ne registra quattro e non esiste nessun `tool_document*.go`;
   il binario estratto da `aura-arcadedb-mcp:local` (build 16:12) contiene invece tutte e
   sette le stringhe, e `main.go` risulta toccato alle 16:22, **dopo** la build.
   Il codice dei document tool **è stato perso**: va riscritto, e il contratto per farlo è
   il JSON del punto 2.

4. **ArcadeDB adesso è un servizio di Aura**, non più un container orfano. `compose.yaml`
   ha `arcadedb` + `arcadedb-mcp` (volume `aura-arcadedb`, `127.0.0.1:2480` e `:8096`,
   `ARCADEDB_PASSWORD`/`ARCADEDB_DATABASE` in `.env`). Nessuna env di embedding: qui il
   recupero è grafo + full-text e basta.
   **Due trappole nuove, pagate con il container in piedi:**
   - `-Darcadedb.server.defaultDatabases=aura_memory` **non crea niente** e non lo dice.
   - `…=aura_memory[root:$pw]` crea il database **e insieme** scrive
     `"aura_memory":[null]` nella entry di `root` in `server-users.jsonl`, che **oscura**
     il suo stesso `"*":["admin"]`. Risultato: 403 `User 'root' is not allowed to update
     schema` sulla prima DDL — root chiuso fuori dall'unico database che aveva creato.
     La forma giusta è **`aura_memory[]`**, parentesi vuote.
   - Il **server MCP nativo di ArcadeDB 26.7.3 ha 10 tool generici**, ri-verificato oggi
     sul binario con `profile:"all"`: niente `full_text_search`, `vector_search`,
     `hybrid_search`, `sample_records`, `upsert_entity`. La pagina
     `docs.arcadedb.com/…/reference/mcp/mcp` li elenca tutti. Il server Go resta
     giustificato.
   Round-trip verificato sul volume pulito: `memory_upsert_fact` →
   `{"statement":"…","superseded":0}`, `memory_facts_about` restituisce il fatto con
   subject e object risolti.

5. **`D:/turing_AgentMemory_MCP` è stata cancellata** durante la sessione.
   - Le **fixture sopravvivono identiche** in **`d:/tmp/baseline-corpus/`**
     (`Clienti.xlsx` 331 239 B, `Costituzione.pdf` 478 600 B, stesse mtime degli
     originali). Il test del punto 2 non è bloccato: cambia solo il percorso.
   - Il **dato ArcadeDB è salvo** — volume nominato
     `turing-agentmemory-mcp_arcadedb-data`, non un bind mount dentro la cartella.
   - **Le compose sono perse.** `turing-agentmemory-mcp-arcadedb-1` gira ancora; se lo si
     rimuove non resta da cosa ricrearlo. Da conservare prima:
     `arcadedata/arcadedb:26.7.3`, `127.0.0.1:2480`, `-Xms2G -Xmx2G`, il volume dati
     sopra, e la root password che sta nella env `JAVA_OPTS` del container
     (`docker inspect`, non trascritta qui).

---

## La decisione, con i numeri

`turing_AgentMemory_MCP` **non si adotta**. Su ArcadeDB si costruisce direttamente,
**senza embedding né reranker**.

| corpus misto, 3221 chunk, 5 formati | latenza | qualità |
|---|---:|---|
| turing (denso + rerank + fusione) | 1234–2045 ms | rank 1 |
| ArcadeDB `vector.fuse` (denso + lessicale) | 25 ms | rank 1 |
| **ArcadeDB solo full-text** | **28 ms** | 5/5 nei primi 3 |

Il vettore non aggiunge niente di misurabile e costa un embedding per chunk in ingest
più uno per query. Quello che davvero recupererebbe è il **buco di vocabolario**
("which GPU" contro un manuale che scrive *"graphics processor"*), e lì il rimedio
giusto è l'**analizzatore Lucene**, non un modello: `work` matcha 0 righe con
`StandardAnalyzer` e 1 con `EnglishAnalyzer`.

### Sulla memoria, misurato sulla memoria vera di Claude Code

158 note, 344 `[[link]]`, caricate come 488 fatti su 227 entità in 10,6 s.

| | |
|---|---:|
| ricerca full-text, nota giusta a rank 1 | **6/10** |
| **indice iniettato** (137 righe, ~12 KB) | **10/10** |

**Finché l'indice sta in contesto, la ricerca è di troppo.** Il grafo si guadagna il
posto quando non ci sta più: allora non inietti tutto, inietti i fatti delle entità in
gioco — ed è `memory_facts_about`, che costa **8 ms**.

---

## Cosa esiste adesso

### Codice (non committato)

| file | cosa |
|---|---|
| `internal/arcadedb/client.go` | client HTTP: `Query`/`Command` (SQL), **`Read`/`Write` (Cypher)**, `Script` (sqlscript) |
| `internal/arcadedb/memory.go` | modello fatti bitemporale + `UpsertFact`, `FactsAbout`, `SearchFacts` |
| `internal/arcadedb/*_test.go` | unit + **integration dietro `//go:build arcadedb_integration`** |
| `cmd/arcadedb-mcp/` | server MCP (SDK ufficiale `modelcontextprotocol/go-sdk` v1.7.0) |
| `docker/arcadedb-mcp/Dockerfile` | multi-stage, distroless nonroot |

`Read`/`Write` hanno **quella firma di proposito**: è quella che
`internal/documents.KnowledgeClient` e `internal/knowledge.GraphReader` **già
dichiarano**. Un `*arcadedb.Client` le soddisfa, quindi il Cypher esistente gira senza
riscritture.

### I quattro tool

| tool | numeri misurati |
|---|---|
| `memory_upsert_fact` | 18 ms; `supersedes` chiude la finestra, non cancella |
| `memory_facts_about` | **8 ms** — traversata esatta, è il candidato per l'iniezione |
| `memory_search` | 6–8 ms — full-text, **ripiego** quando non conosci l'entità |
| `graph_schema` | introspezione O(1) via `schema:types` |

### Il modello dati

```
(:Entity)-[:FACT {
    statement,                    ← indicizzato FULL_TEXT con EnglishAnalyzer
    predicate,
    valid_from, valid_to,         ← tempo dell'evento  (mai `end`: riservata)
    created_at, expired_at,       ← tempo di transazione
    source_run_id, source_memory_ids
}]->(:Entity)
```

Bitemporale da **Graphiti** (`d:/tmp/graphiti`): il fatto sta sull'arco, una
contraddizione **chiude la finestra** invece di cancellare. Sul temporale di LOCOMO i
sistemi senza questo fanno 21-23%, Zep 79,8%.
Provenienza obbligatoria da **Cognee** (`d:/tmp/cognee`), che dedica un terzo della sua
interfaccia ai source ref — ed è il problema che ha già morso Aura (9 entità di test
scritte sotto l'identità reale dell'operatore, senza discriminatore).

---

## Il porting dei documenti: 6 righe

**Verificato eseguendo il Cypher di Aura, copiato dai suoi file, su ArcadeDB:**

| query da `internal/documents` | esito |
|---|---|
| `chunkUpsertQuery` (`UNWIND $chunks … MERGE … MERGE (d)-[:HAS_CHUNK]->(c)`) | gira, 5 chunk in **uno** statement |
| la stessa due volte | idempotente, nessun duplicato |
| `nextChunkUpsertQuery` + `neighborExpandQuery` | girano, vicini da entrambi i lati |

Incompatibilità **totale**:

| dove | quante | cosa |
|---|---:|---|
| `search.go` — `CALL db.index.fulltext.queryNodes` | 2 | → `SEARCH_INDEX('Chunk[text]', :q)` |
| `retrieve.go` — `db.index.vector.queryNodes`, `vector.similarity.cosine` | 4 | **cancellare** |

**`internal/documents` non si tocca.** Una sola differenza di schema: ArcadeDB non
indicizza una proprietà che non esiste ancora — `CREATE PROPERTY` prima di
`CREATE INDEX`; Neo4j non lo richiede.

---

## Trappole pagate oggi (non ripagarle)

- **`docker restart` NON ricrea il container.** Ho misurato due giri sull'immagine
  vecchia. Sempre `docker rm -f` + `docker run`, e verificare
  `docker inspect --format '{{.Image}}'` contro `docker image inspect --format '{{.Id}}'`.
- **`end` è riservata in ArcadeDB in DUE posizioni**: identificatore nudo e nome di
  bind parameter. `end = :p_end` e `` `end` = :end `` sono entrambi errori; solo
  `` `end` = :p_end `` passa.
- **`out.name` su un arco restituisce NULL, non un errore.** Servono `outV()`/`inV()`.
  Sbagliarlo costa soggetto e oggetto vuoti, in silenzio.
- **`vector.neighbors()` su un indice d'arco perde l'identità dell'arco**: `outV()`
  sopra quella sottoquery muore con `iRecord is null`. (Irrilevante ora che i vettori
  non ci sono, ma è un limite reale di 26.7.3.)
- **`StandardAnalyzer` non fa stemming.** Corpus bilingue → analizzatore per lingua,
  non un modello di embedding.
- **I tool MCP non entrano a sessione avviata.** `claude mcp list` dice `✔ Connected` e
  `ToolSearch` non trova nulla. È il motivo di questo handoff.
- **La documentazione di ArcadeDB corre davanti al prodotto.** Tre volte oggi: i tool
  MCP `hybrid_search`/`vector_search` non esistono nel binario (ce ne sono 10, tutti
  generici, e `query` non accetta bind parameter); le capacità temporali sono
  "documentate" senza una funzione; l'adapter ArcadeDB di Cognee non è nel repo.
  **Verificare sempre sul binario.**

---

## Stato della macchina

- **ArcadeDB `26.7.3`** (era 26.7.1; in mezzo 3 advisory, una è *MCP transport
  permission check bypass*). ~~Pin in `D:/turing_AgentMemory_MCP/compose.yaml`~~ — quella
  compose **non esiste più**, vedi addendum §4.
- **`aura-arcadedb-mcp`** su `127.0.0.1:8096`, database `aura_memory`, **nessuna env di
  embedding**. Registrato in Claude Code come `aura-arcadedb` — **a scope user**, non di
  progetto (addendum §1).
- Database `aura_memory_it` per gli integration test.
- Server MCP nativo di ArcadeDB **acceso in sola lettura** (`allowReads` only, `root`,
  loopback). Si spegne con `POST /api/v1/mcp/config {"enabled": false}`.
- **`aura-tts` e `aura-stt` fermi** (liberati ~1,2 GB). Da riaccendere se servono.
- I container **turing MCP e GLiNER sono morti** durante la sessione (memoria). Non
  servono più.
- Doppia registrazione MCP `turing-agentmemory`/`turing-memory` verso lo stesso `:8095`
  — da ripulire nella user config.
- Repo clonati per riferimento: `d:/tmp/graphiti` (29 MB), `d:/tmp/cognee` (153 MB).

---

## Non committato

**In `d:/Aura`** — tutto nuovo, `go build ./...` verde, integration verdi:
`internal/arcadedb/`, `cmd/arcadedb-mcp/`, `docker/arcadedb-mcp/Dockerfile`, più
`go.mod`/`go.sum` (SDK MCP ufficiale).
Più il lavoro dell'handoff precedente: `internal/agent/mcptools/bridge_memory.go` e
compagnia (soglia di recall) — vale a prescindere.
Più `docs/superpowers/plans/2026-07-31-arcadedb-migration.md`, che ha una stima della
superficie Neo4j (68 file non-test, **78 di test**, 11 con Cypher dentro) ancora valida.

**In `D:/turing_AgentMemory_MCP`** — le 3 fix ai bug trovati stamattina (`ids.py`,
`store_memory_queries.py`, `store_retrieval_queries.py` + test). Valgono se quel repo
resta un banco di prova; se no muoiono con lui.

---

## Aperto

1. **Il test vero sui documenti** — punto 2 del prompt. È l'unica cosa che manca per
   chiudere il percorso documenti, e non l'ho fatta.
2. **L'iniezione del digest nel turno** — l'equivalente del `MEMORY.md` che Claude Code
   riceve nel prompt di sistema. Non è un tool: è cablaggio. È il pezzo che fa la
   differenza fra una memoria e un archivio da interrogare.
3. **LOCOMO** (`d:/tmp/Backboard-Locomo-Benchmark`): 10 conversazioni, 1.540 domande
   valutabili, ognuna con il campo `evidence` che permette di misurare il recall in modo
   **deterministico e gratuito**. Il punteggio con giudice GPT-4.1 è a pagamento: solo
   con via libera esplicita.
4. **Le community Leiden** — ArcadeDB non le fa. Coerente con
   `project_leiden_rerank_external_sidecars`, che le aveva già messe in un sidecar.
5. **Memoria in conflitto**: `project_graph_db_neo4j_stays_alternatives_rejected` dice
   di restare su Neo4j. I dati di oggi la contraddicono. Da riscrivere solo con via
   libera.
