# Handoff — riscrittura della pipeline documenti

**Presupposto:** leggere prima `FINDINGS.md` (stesso folder). Qui c'è solo il piano di
riscrittura; le misure che lo giustificano stanno lì e non vanno ri-derivate.

**Stato al 2026-08-06:** nulla di questo è integrato. `spikes/cocoindex-ingestion/` contiene
830 LOC di codice funzionante e misurato. `compose.yaml` è già a posto sul master
(`a5228a793` fix GLM-OCR, `4ca17b400` Bolt su ArcadeDB).

---

## 1. La decisione in una riga

Sostituire orchestrazione + versioning + client Docling con **MarkItDown + CocoIndex**,
tenendo ArcadeDB come unico store e **senza scrivere driver**: il connector Neo4j stock
parla al plugin Bolt di ArcadeDB.

Non è un refactor: **9.023 LOC spariscono**, e ciò che resta cambia di forma.

---

## 2. Cosa muore, cosa nasce, cosa resta

### Muore (9.023 LOC)

| file / gruppo | LOC | sostituito da |
|---|---|---|
| `pipeline_worker.go` — stage, lease, fingerprint, retry | 513 | riconciliazione CocoIndex |
| `pipeline_store*.go` (7 file) | 1.415 | stato LMDB di CocoIndex |
| `jobs_store.go`, `jobs_worker.go` | 707 | `cocoindex update` |
| `delete_durable_*.go` (4 file) | 1.057 | riconciliazione: ciò che non è dichiarato viene cancellato |
| `events_store.go`, `orphans.go`, `retry_backoff.go`, `job_context.go` | 393 | idem |
| `docling_client*.go`, `docling_passages.go` | 1.041 | MarkItDown (~40 LOC) |
| `internal/arcadedb/document_projection.go` — versioning/tombstone | 567 | non esiste più il concetto |
| **test associati** | **2.947** | — |

**Perché il versioning sparisce davvero:** le query filtrano `active = true` ovunque
(`document_retrieval.go:186`), il tombstone scrive `active = false, tombstoned_at`, e
**nessuna query rilegge mai un passaggio tombstonato**. Si scrive storia che nessuno
consulta. Con la riconciliazione lo stato corrente È lo stato.

### Nasce (~400 LOC stimate)

| componente | cosa fa | stato |
|---|---|---|
| flow CocoIndex (Python, sidecar) | sorgente S3 → MarkItDown → chunk → embed → ArcadeDB | provato |
| `ensure_schema` in SQL nativo | tipi, `ARRAY_OF_FLOATS`, `LSM_VECTOR` — **prima** di ogni scrittura Bolt | provato |
| `documents_mcp` | `document_search` (hybrid + rerank + astensione, con `prefix`), `document_describe`, `document_resolve` | provato, `mcp/` |
| `document_open` (Go, ridotto) | `Router.Route` + copy-in nella sandbox; consuma l'id che l'MCP risolve | da fare |
| `table_query` | SQL sulle tabelle ETL; espone `unit.header_json` + `fingerprint` come schema scoperto | da fare |
| identità stabile del passaggio | `bucket/key` o id catalogo al posto del path del walker (§5b) | **bloccante** |

I due tool NON vanno confusi: `document_search` recupera un passaggio, `table_query`
calcola un aggregato. La domanda "quanti fornitori gestisce Molteni" ha risposta 258 e
quel numero non è scritto in nessuna cella — nessun retrieval può produrlo.

### Resta

`catalog_*` (CRUD, identità), `retrieval*` (scoping, ridotto), `open.go`, `service.go`,
`filecard/`. **`filecard` non si tocca**: fa già l'estrazione strutturata con i test, ed è
la rappresentazione di routing L1. Vedi `FINDINGS.md` §5b — in questo spike è stato
duplicato per errore.

---

## 3. Ordine di esecuzione

Ogni fase è verificabile da sola e non rompe la precedente.

**F0 — identità stabile del passaggio.** *Bloccante, e va per prima* (§5b): finché
`Passage.document` porta il path del walker, la catena trova→apri è spezzata e tutto ciò
che sta sopra è invalidato. Due righe in `process_file`. *Verifica:* `document_search`
restituisce un riferimento che `document_open` risolve.

**F1 — schema-first su ArcadeDB.** Portare le DDL native (`ARRAY_OF_FLOATS` +
`LSM_VECTOR` + FULL_TEXT) in una migration/bootstrap. *Verifica:* `vector.neighbors`
recupera un embedding scritto via Bolt.

**F2 — sidecar CocoIndex con sorgente S3.** Un servizio compose che esegue
`cocoindex update` (o `-L` per il live). Stato LMDB **su volume**, non effimero.
*Verifica:* le 4 run di `FINDINGS.md` §2 (baseline / unchanged / modified / deleted).

**F3 — `documents_mcp` come sidecar.** Registrarlo come gli altri MCP di Aura, fail-soft.
*Verifica:* i tre tool rispondono e `answered:false` arriva strutturato.

**F4 — `document_open` ridotto.** Consuma l'id risolto dall'MCP; resta solo
`Router.Route` + copy-in. *Verifica:* il file compare nella sandbox del chiamante.

**F5 — spegnere il vecchio.** Solo ora cancellare i file di §2. *Verifica:* la suite passa
senza di essi.

**F6 — `table_query` sulle tabelle ETL.** La capacità Excel, distinta dal retrieval.
*Verifica:* "quanti fornitori per acquisitore" torna 258 per Molteni.

**F7 — routing per prefisso end-to-end.** Il filtro strutturale (§4) usato dall'agente,
non solo disponibile come parametro.

**F8 — UI file manager.** Per ultima: senza F7 è solo un browser di file. La UI scrive su
S3 e CocoIndex riconcilia — nessun collante fra UI e ingestion (provato, RUN 4).

---

## 4. Il routing, che è la parte non risolta

`filecard` + `RouteDocumentCards` esistono già. Ma EXP1 aveva misurato che la
rappresentazione testuale **non è la leva** (0,917 flatten vs 0,903 card su 465 file).

La leva non sfruttata è il **path**. Con le cartelle S3, `clienti/rossi/` risponde a "i
documenti di Rossi" in modo deterministico — nessun embedding, nessun errore possibile.
Copre proprio i casi dove il denso fallisce: nomi propri, codici, entità.

Ordine corretto: **filtro strutturale sul prefisso** quando la domanda nomina un'entità
che è una cartella → **poi** semantico sul residuo. Non il contrario.

---

## 5. Trappole che costeranno una giornata se ignorate

1. **Lo stato LMDB deve stare su volume.** In directory effimera ogni run riparte da zero:
   sembra funzionare, costa solo tutto ogni volta.
2. **La proprietà vettoriale va tipizzata PRIMA.** Un `MERGE` Cypher crea una lista non
   tipizzata e `LSM_VECTOR` rifiuta di indicizzarla. Schema-first non è stile, è requisito.
3. **`markitdown-ocr` a priorità −1.0 sostituisce il converter per OGNI PDF** e raddoppia i
   glifi in grassetto (`OGGETTO` → `OOGGGGEETTTTOO`). Plain di default, OCR solo se manca
   il layer di testo.
4. **`pypdfium2` va chiuso esplicitamente** (textpage, page, document) o abortisce con
   `malloc_consolidate` DOPO il commit — sembra successo fino all'exit code.
5. **Il log di ArcadeDB dice `Plugins directory not found`** ed elenca solo l'AutoBackup.
   È fuorviante: Cypher e Bolt ci sono.
6. **Rename su S3 = copy+delete**, non atomico. Cartelle vuote non esistono senza un
   oggetto marcatore a 0 byte.

---

## 5b. DIFETTO: il passaggio non porta un'identità di documento azionabile

`Passage.document` oggi contiene il percorso che il walker aveva **al momento
dell'ingest**, e cambia con la sorgente:

```
localfs → /corpus/GH1_0111_ita_it-IT.pdf   (path dentro il container di ingest)
S3      → mutuo.pdf                        (chiave oggetto, SENZA bucket né prefisso)
```

`document_search` lo restituisce come `document`, ma **non è apribile**: `document_open`
accetta un `document_id` (`doc_…` o uuid catalogo) e risolve via catalogo + object store.
Il valore attuale serve solo a mostrare la provenienza.

**Correzione, prima di integrare:** `process_file` deve scrivere un'identità stabile —
id di catalogo, oppure `bucket/key` completo — e `document_search` deve restituire
QUELLA, non il path del walker. Il contratto verso `document_open` resta invariato:
l'MCP dice *quale* file, il tool Go fa routing + copy-in nella sandbox.

Finché non è fatto, la catena trova → apri **è spezzata**: il modello riceve un
riferimento che non può usare.

## 6. Domande aperte, in ordine di impatto

1. **Identità stabile del passaggio** (§5b) — bloccante, rompe trova→apri.
2. **Routing per prefisso end-to-end** — il parametro c'è nell'MCP, l'agente non lo usa.
   È il pezzo con più valore residuo dopo il punto 1.
3. **`etl_flow.py` va rifatto per chiamare `filecard`** invece di reimplementarlo (§5b
   di FINDINGS).
4. **`table_query` non esiste.** Oggi le tabelle ETL si interrogano solo a mano.
5. **Grafo a 85 s/documento** — solo asincrono. Le 2 chiamate LLM sono sequenziali:
   parallelizzarle dovrebbe dimezzare.
6. **Fail-soft dell'MCP.** Un sidecar giù = documenti non consultabili, mentre un tool
   in-process non può esserlo. Per la memoria il compromesso è già accettato; qui va
   deciso consapevolmente.
7. **Un solo corpus.** Un manuale tecnico e tre documenti amministrativi non sono una
   validazione statistica. Manca soprattutto un vero scansionato in italiano: GLM-OCR è
   provato su pixel, non su una scansione nativa.
8. **`data_json` come `jsonb`** invece di TEXT: toglie il cast e permette un indice GIN.
9. **Poppler** è installato sull'host (`~/bin`) ma Claude Code lo vedrà solo dopo un
   riavvio: la capability è verificata all'avvio.

## 8. Risorse lasciate attive (da pulire quando non servono)

- container `aura-documents-mcp` sulla rete `aura_default`
- bucket Garage `cocoindex-spike` + chiave `cocoindex-spike-key`
- database ArcadeDB `aura_manual_spike`, `aura_ingest_spike`, `aura_s3_spike`
- schema `etl` nel database Postgres `cocoindex`
- volumi `aura-s3-state-v2`, `aura-cocoindex-state`, immagine `aura-pipeline:probe`

Nessuno tocca la produzione: `aura_memory`, i `mem_<uuid>` per identità e il bucket
`aura-assets` non sono stati modificati.

---

## 7. Come riprendere

```bash
cd spikes/cocoindex-ingestion
docker build -t aura-pipeline:probe probes/

# ingest (ARCADE_DB scegli tu; lo stato LMDB su volume, non /tmp)
docker run --rm --network aura_default \
  -e ARCADEDB_PASSWORD=… -e COCOINDEX_DB=/state/coco.db -e CORPUS=/corpus \
  -v aura-s3-state-v2:/state -v $PWD/flows:/probe:ro -v <corpus>:/corpus:ro \
  aura-pipeline:probe sh -c "cd /probe && cocoindex update -f aura_flow.py"

# retrieval: due numeri, non uno
docker run --rm --network aura_default -e ARCADEDB_PASSWORD=… -e OPENROUTER_API_KEY=… \
  -e ARCADE_DB=aura_manual_spike -e CASES_FILE=/probe/cases_manual.json \
  -v $PWD/probes:/probe:ro aura-pipeline:probe python /probe/hybrid.py
```

Bucket Garage dello spike: `cocoindex-spike`. La chiave si ricrea con
`garage key create <nome>` + `garage bucket allow --read --write --owner <bucket> --key <id>`.
