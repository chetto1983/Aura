# Ingestion su CocoIndex — sintesi dello spike

**Data:** 2026-08-06 · **Stato:** misurato, non integrato · **Codice:** `flows/`, `probes/`

Sostituire la pipeline documenti (Docling + versioning + worker di cancellazione fatti a
mano) con **un estrattore + CocoIndex + ArcadeDB**, senza scrivere un driver e senza
aggiungere un datastore. Tutte le cifre qui sotto sono misurate su questo host, non stimate.

> **MarkItDown non è più il candidato estrattore.** Questo documento lo misura solo contro
> Docling, su PDF, dove vince. Allargato a tutti i formati Office la sera del 2026-08-06
> è caduto: fallisce del tutto su `.doc`, `.ppt`, `.odt`, `.ods`, `.odp`, restituisce
> markup RTF grezzo come se fosse testo, e **rompe la ricerca per frase esatta sui PDF
> giustificati** emettendo doppi spazi (invisibile: produce più caratteri di un estrattore
> pulito). Il candidato è **iscc-tika 0.6.0** — 15 formati su 16, 18× più veloce sul PDF.
> Il verdetto corrente e i gap residui sono consolidati in
> `.planning/codebase/CONCERNS.md`. Le cifre qui sotto restano valide come confronto con
> Docling, non come scelta.

---

## 1. La decisione, in una tabella

Il confine non è per tipo di file, è per **forma del contenuto e dimensione**:

| input | strategia | perché, con il numero |
|---|---|---|
| Excel / tabellare | l'agente **apre** e aggrega; ETL→SQL quando i file sono migliaia | D1/D2 dello studio precedente |
| PDF che entra in **metà** finestra | **aprilo**, niente RAG | nessun retrieval da sbagliare |
| PDF che non entra | **hybrid + rerank** | **10/10 recall@1, 6/6 astensioni** |

La soglia è aritmetica, non un giudizio: `caratteri / 3,6 ≈ token`. Misurato sul corpus —
i 3 PDF piccoli stanno a 1,4k / 26k / 63k token (tutti apribili), il manuale SINAMICS a
**114k** (non apribile), un PDF da 700 pagine a ~428k.

**Corollario scomodo:** le prime misure di retrieval (6/12) erano fatte su documenti che
entrano tutti in contesto, cioè su casi che non sono casi RAG. Misuravano la cosa sbagliata.

---

## 2. Le misure

### Conversione — MarkItDown contro Docling (solo PDF; per la scelta vedi HANDOFF §3.4)

| documento | pagine | Docling | MarkItDown |
|---|---|---|---|
| `documenti da stampare` | 3 | ~9 s¹ | **0,26 s** |
| `gas_richiesta_voltura` | 34 | ~100 s¹ | **14,4 s** |
| `TESEBRO000050EN` | 122 | ~370 s¹ | **47,9 s** |
| `ocr_test` (scansione) | 1 | 4,2 s | **3,3 s** (fallback GLM-OCR) |

¹ estrapolato da 5,9 s / 2 pagine misurate.

`do_ocr=true` su un PDF digitale ha aggiunto **+2 caratteri per +10,7 s**: il default
`do_ocr=false` era già corretto. Su una scansione vera invece `do_ocr=false` rende
**0 caratteri** — lì l'OCR non è un'ottimizzazione, è l'unica strada.

### Retrieval — sul caso RAG vero (manuale SINAMICS, 328 pagine, 696 passaggi)

```
HYBRID (denso k=10 ∪ token esatto k=10) → rerank LLM con astensione
recall@1            = 10/10
astensioni corrette =  6/6
ingest              = 88,9 s (0,27 s/pagina)
query               = 33 ms (embed 22 ms + ricerca vettoriale 11 ms)
```

**Perché serve l'hybrid.** Il denso da solo fa 4/12 su un indice misto. I fallimenti sono
tutti identificatori isolati (`00881404688150`, `LDLEI66H09D205G`, `008837`): `LIKE`
dimostra che sono **presenti nel testo indicizzato**, quindi è un fallimento di recupero,
non di estrazione. `CONTAINSTEXT` li trova tutti e quattro. I due segnali sono
complementari **per tipo di domanda** — ed è esattamente il caso in cui l'RRF a pesi
uguali peggiora (D4).

**Perché serve l'astensione.** Su 6 domande senza risposta nel corpus, la distanza non
separa: peggiore risposta vera 0,5886 contro migliore risposta falsa 0,5764. Nessuna
soglia funziona. Il rerank sì: 6/6.

### S3 incrementale (Garage)

```
RUN 1  baseline, 3 oggetti     3 added                     26,4 s
RUN 2  nessuna modifica        3 unchanged                  0,1 s
RUN 3  1 file modificato       1 reprocessed, 2 unchanged   0,8 s
RUN 4  1 file cancellato       1 deleted,     2 unchanged   0,3 s
```

RUN 4 ha rimosso **tutti i 249 passaggi** di quel documento da ArcadeDB (347 → 98) senza
una riga di codice nostro. Il rilevamento usa l'**ETag** come fingerprint: non scarica
nulla per accorgersi che un file è invariato.

### ETL generalista (per estensione, mai per contenuto)

7 file → 131 unità → **21.797 righe in 3,9 s**. Tre tabelle sole (`source → unit →
datarow`): per `.xlsx` unità = foglio, per `.pdf` = pagina, per `.pptx` = slide.

### Excel: SQL risponde dove il RAG non può, per costruzione

Domanda reale sul foglio `Fornitori - Acquisitori` (1.385 righe, colonne scoperte a
runtime, nessuno schema scritto a mano):

```
 acquisitore        | fornitori
--------------------+-----------
 Luca Molteni       |       258
 M. Grazia Colombo  |       151
 Claudio Sannà      |       110
```

**Il numero 258 non esiste in nessuna cella.** Esiste solo dopo l'aggregazione, quindi
nessun retrieval può restituirlo: non c'è niente da recuperare. È il risultato di
answerability del D1 riprodotto sui dati dell'operatore, ed è la prova che **Excel non
entra nel RAG** — entra in tabelle, e l'agente fa SQL.

Senza l'euristica header quelle 1.385 righe erano tutte sotto la chiave
`"Elenco Fornitori Acquisitori"` (un titolo). Nota: `data_json` è TEXT e richiede
`::jsonb`; come colonna `jsonb` nativa si risparmia il cast e si indicizza con GIN.

### MCP documenti — l'astensione sopravvive al confine

Server FastMCP su streamable-http, interrogato da un client come farebbe un host:

```
TOOLS: document_search, document_describe, document_resolve
resolve → 696 passaggi indicizzati
search  → answered:true,  passaggio con 6SL3055-0AA00-5EA0
search  → answered:false, "Nessun passaggio riporta il prezzo di listino della CU320-2"
```

Il punto non è che i tool funzionano: è che **`answered:false` attraversa il confine MCP**
come dato strutturato con motivazione, invece di una lista top-k che un agente potrebbe
scambiare per una risposta. La distanza non può prendere quella decisione (peggiore vera
0,5886 > migliore falsa 0,5764); il reranker sì, e il contratto lo rende esplicito.

`document_search` guadagna il parametro che il tool Go non ha mai avuto: `prefix`.

**Difetto storico, ora chiuso:** `Passage.document` portava il path del walker al momento dell'ingest
(`/corpus/…` da localfs, `mutuo.pdf` senza bucket da S3), che `document_open` non sa
risolvere. La catena trova→apri è stata chiusa con l'identità stabile cross-language `doc_`;
il confine corrente e le prove sono riassunti in `.planning/codebase/CONCERNS.md`.

---

## 3. ArcadeDB parla Neo4j — nessun driver da scrivere

Il plugin Bolt è **già nell'immagine** (`lib/arcadedb-bolt-*.jar`), va solo abilitato:

```
-Darcadedb.server.plugins=Bolt:com.arcadedb.bolt.BoltProtocolPlugin   # porta 7687
```

Il connector Neo4j **stock** di CocoIndex funziona senza modifiche (driver Python `neo4j`
6.2.0, certificato da ArcadeDB). L'unica istruzione che non attraversa è
`CREATE VECTOR INDEX` (proprietaria Neo4j 5.18) — i vettori in ArcadeDB sono nativi
(JVector/`LSM_VECTOR`) e si dichiarano **prima** in SQL:

```sql
CREATE PROPERTY Passage.embedding IF NOT EXISTS ARRAY_OF_FLOATS;
CREATE INDEX IF NOT EXISTS ON Passage (embedding) LSM_VECTOR
  METADATA { "dimensions": 768, "similarity": "COSINE", "quantization": "NONE" };
```

Verificato end-to-end: `vector.neighbors` recupera embedding scritti via Bolt. La
proprietà **deve** essere tipizzata prima — un `MERGE` crea una lista non tipizzata e
`LSM_VECTOR` rifiuta di indicizzarla.

Trappola: il log di boot dice `Plugins directory not found: ./lib/plugins` ed elenca solo
l'AutoBackupScheduler. È fuorviante — Cypher è compilato dentro.

---

## 4. Cosa sparisce dalla codebase

`internal/documents` è 11.664 LOC non-test (9.072 nella root + 2.592 in `filecard/`)
+ 8.561 di test. Classificazione verificata leggendo le funzioni, non i nomi dei file:

| gruppo | non-test | test | destino |
|---|---|---|---|
| orchestrazione: `pipeline_worker` (stage, lease, fingerprint, retry), `pipeline_store_*`, `jobs_*`, `delete_durable_*`, `events_store`, `orphans`, `retry_backoff`, `job_context` | 4.468 | 2.047 | **CocoIndex** |
| client Docling: `docling_client{,_transport,_types}`, `docling_passages` | 1.041 | 565 | **iscc-tika** (~40 LOC) |
| `internal/arcadedb/document_projection.go` — versioning/tombstone | 567 | 335 | **sparisce** |
| **cancellabile** | **6.076** | **2.947** | **9.023 LOC** |
| catalogo, retrieval, API, identity | 3.563 | — | resta |
| `filecard/` (scheda per-file, routing L1) | 2.592 | — | resta |

Cioè **circa metà** di `internal/documents` sparisce, non "quasi tutto": ciò che resta è
dominio di Aura (CRUD documenti, scoping per identità, routing L1), non impalcatura.

Una nota però: `retrieval.go` + `retrieval_postgres.go` + `retrieval_rank.go` sono 1.053
LOC per fare ciò che `probes/hybrid.py` fa in 132. Non spariscono — portano lo scoping per
identità — ma si riducono parecchio. Con quelli il conto realistico supera il 50%.

**Il versioning sparisce davvero.** Le query sono `WHERE active = true` ovunque
(`document_retrieval.go:186`) e il tombstone scrive `active = false, tombstoned_at`, ma
**nessuna query legge mai un passaggio tombstonato**: la storia si scrive e non si rilegge.
Serve solo a sapere quali passaggi sono correnti — che è ciò che la riconciliazione di
CocoIndex garantisce per costruzione.

NON sono sostituiti: catalogo, scoping per identità, superficie API.

---

## 5. Difetti trovati e corretti

| difetto | effetto | correzione |
|---|---|---|
| `markitdown-ocr` registrato a priorità −1.0 | sostituisce il converter PDF per **ogni** file; il suo pdfplumber raddoppia i glifi in grassetto (`OGGETTO` → `OOGGGGEETTTTOO`, 319 duplicati contro 123) e la parola non è più cercabile | estrazione plain di default, OCR solo se manca il layer di testo — **più veloce e più corretto** (2,2 s → 1,4 s) |
| reranker con candidati troncati a 700 caratteri su chunk da 1200 | si asteneva su risposte mai mostrate | 1400 |
| chunk di sole righe di tabella (`--- \| --- \|`) | vincevano il top-1 sulle domande senza risposta | scartati sotto 24 caratteri alfanumerici |
| `pypdfium2` senza `close()` esplicito | `malloc_consolidate(): unaligned fastbin chunk`, abort **dopo** il commit: sembrava successo fino all'exit code | chiusura esplicita di textpage/page/document |
| riga 1 ≠ intestazione in 4 fogli su 9 | 1.386 record indicizzati sotto un titolo | euristica di forma (riempimento, testualità, unicità, divergenza di tipo); rifiuta sotto 0,55 e conserva sempre le celle posizionali |
| `HF_HOME` mancante su `aura-ocr-vl` | 1,4 GB riscaricati a ogni recreate | `HF_HOME=/root/.cache/llama.cpp` |
| `-np`/`-c` non impostati | 4 slot × 10240 di KV, GPU a 181 MiB liberi | `-np 1 -c 16384` (markitdown-ocr rende a 300 DPI → 8.083-8.126 token) |

---

## 5b. Errore di metodo commesso in questo spike

`probes/`/`flows/etl_flow.py` contiene un'euristica di rilevamento header (~50 righe,
senza test) scritta dopo aver visto il problema "la riga 1 è un titolo" su 4 fogli su 9.

**Esisteva già.** `internal/documents/filecard/` (2.592 LOC) estrae schede strutturate da
xlsx/csv/pdf/ooxml/zip/text e ha i test di quei casi esatti:

```go
TestHeaderIsFoundBelowABannerRow    // "Listino fornitori 2026" + riga vuota + header vero
TestHeaderlessTableKeepsItsFirstRow // senza header la prima riga resta dato
TestCSVCardSniffsSemicolonsAndItalianDecimals
```

`filecard/` era comparso nel conteggio LOC e non è stato aperto. **`etl_flow.py` va rifatto
per CHIAMARE `filecard`, non per rifarlo**, e `filecard` NON va sostituito: è anche la
rappresentazione di routing L1.

## 5c. Cartelle S3 e routing per path

S3 non ha cartelle: ha prefissi. `list_objects_v2(Delimiter='/')` restituisce
`CommonPrefixes`, cioè il livello corrente — il caricamento pigro che serve a una UI.
Verificato su Garage:

```
livello '/'        -> [DIR] clienti/  [DIR] fornitori/  [FILE] documento.pdf
livello 'clienti/' -> [DIR] clienti/bianchi/  [DIR] clienti/rossi/
```

**Il path è una leva di routing che oggi non usiamo.** EXP1 aveva già misurato che la
rappresentazione testuale non sposta il routing (0,917 flatten vs 0,903 card); il prefisso
invece risponde a "i documenti di Rossi" in modo **deterministico**, senza embedding e
senza errore. Il routing corretto è: prima filtro strutturale sul prefisso, poi semantico
sul residuo.

Gotcha per una UI tipo `svar-widgets/react-filemanager`: **rename/move = copy+delete**
(S3 non ha rename, non è atomico) e **le cartelle vuote non esistono** (serve un oggetto
marcatore a 0 byte). Il resto mappa 1:1. La UI non deve conoscere la pipeline: scrive su
S3 e CocoIndex riconcilia — provato, RUN 4.

## 6. Aperto

- **Routing automatico** apri-vs-RAG: la soglia è calcolata ma non ancora cablata.
- **Grafo**: 2 chiamate LLM per documento, memoizzate, ma **85 s/documento** — solo
  asincrono. Le due chiamate sono sequenziali: parallelizzarle dovrebbe dimezzarle.
- **`db gruppi merce`**: falso positivo dell'euristica header (score 0,675). Dati
  comunque recuperabili da `cells_json`.
- **Un solo corpus**. Un manuale tecnico e tre documenti amministrativi non sono una
  validazione statistica.

## 7. Riproducibilità

```bash
docker build -t aura-pipeline:probe probes/
# ingest + retrieval
docker run --rm --network aura_default -e ARCADEDB_PASSWORD=… -e COCOINDEX_DB=/state/coco.db \
  -v aura-s3-state-v2:/state -v $PWD/flows:/probe:ro \
  aura-pipeline:probe sh -c "cd /probe && cocoindex update -f aura_flow.py"
```

Lo stato LMDB **deve** stare su un volume persistente: in una directory effimera ogni
esecuzione riparte da zero e l'incrementale non esiste — sembra funzionare, costa solo
tutto ogni volta.
