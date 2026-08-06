# Handoff — riscrittura della pipeline documenti

**Presupposto:** leggere prima `FINDINGS.md` (stesso folder) per le misure dello spike di
ingestione. Le misure di **retrieval, estrattori e stack di embedding** stanno invece qui,
§3, perché sono state fatte dopo e in parte contraddicono quel documento.

**Stato al 2026-08-06, sera.** Nulla è integrato. `spikes/cocoindex-ingestion/` contiene
830 LOC funzionanti e misurate. Sul master sono atterrati stasera `a0d236105` (pooling e
tetto di contesto dell'embedder), e il deployment locale è stato portato a HEAD: immagine
ricostruita, database dalla migration **78 alla 93**.

> **Questo documento è stato riscritto integralmente.** La versione precedente indicava
> **MarkItDown** come convertitore e dava il reranker per acquisito. Entrambe le cose sono
> state misurate e **entrambe sono cadute**. Le sezioni §1, §2 e §3 sono nuove; §4 e §5
> corrette; i numeri di LOC di §2 erano sbagliati per un errore di somma, non per drift.

---

## 1. La decisione in una riga

Sostituire orchestrazione, versioning e conversione con **iscc-tika + CocoIndex +
ArcadeDB**, senza scrivere driver: il connector Neo4j stock parla al plugin Bolt di
ArcadeDB.

Tre motori con tre confini netti, e Aura che li tiene insieme:

| motore | fa | NON fa |
|---|---|---|
| **iscc-tika** | estrae testo da 15 formati su 16 | non chunka, non indicizza |
| **CocoIndex** | riconcilia lo stato incrementale | **non recupera** — zero simboli query/search/rank in `coco.__all__` |
| **ArcadeDB** | recupera: denso, full-text, prefiltro, diversità, cutoff | non produce embedding: nessuna funzione text→vector esiste |

Ciò che resta di Aura è ciò che nessun motore può sapere: **di chi** sono i documenti
(bucket e database per identità, RLS fail-closed), **quando** si aggiornano, **cosa** può
vedere il modello, e **come** un file arriva dentro la sandbox.

Non è un refactor: **9.023 righe spariscono** (10.108 contando i test di integrazione), e
due sidecar con loro.

---

## 2. Cosa muore, cosa nasce, cosa resta

### Muore

Ricontato riga per riga su HEAD `a0d236105` — nessun drift dallo spike, ma la tabella
dell'edizione precedente era incompleta.

| file / gruppo | LOC grezze | non vuote | sostituito da |
|---|---|---|---|
| `pipeline_worker.go` — stage, lease, fingerprint, retry | 513 | 489 | riconciliazione CocoIndex |
| `pipeline_store*.go` (7 file) | 1.415 | 1.334 | stato LMDB di CocoIndex |
| `jobs_store.go`, `jobs_worker.go` | 707 | 652 | `cocoindex update` |
| `delete_durable_*.go` (4 file) | 1.057 | 984 | riconciliazione: ciò che non è dichiarato viene cancellato |
| `events_store.go`, `orphans.go`, `retry_backoff.go`, `job_context.go` | 393 | 358 | idem |
| `pipeline_types.go`, `pipeline_artifact_cache.go`, `delete.go` | 383 | 352 | idem |
| `docling_client*.go`, `docling_passages.go` (4 file) | 1.041 | 972 | iscc-tika (~40 LOC) |
| `internal/arcadedb/document_projection.go` — versioning/tombstone | 567 | 544 | il concetto non esiste più |
| **sottototale non-test** | **6.076** | 5.685 | |
| **test unitari associati** (13 file) | **2.947** | 2.719 | — |
| **totale** | **9.023** | **8.404** | |
| *+ test di integrazione* (5 file) | *1.085* | | |
| **totale reale** | **10.108** | | |

> **La riga `pipeline_types` / `pipeline_artifact_cache` / `delete.go` mancava**
> nell'edizione precedente: la tabella sommava 8.640 mentre la prosa diceva 9.023, e la
> prosa aveva ragione — quei tre file sono esattamente le 383 righe di differenza.
> Ricontato su HEAD `a0d236105`: nessun drift dallo spike, tutti i 39 file esistono ancora.
> I test di integrazione (`pipeline_store_integration*`, `delete_durable_integration`,
> `document_projection_integration`) non erano contati e valgono altre 1.085 righe.

**Muore anche il registro stage della migration `0093`.** `document_pipeline_stages` porta
le colonne `input_fingerprint`, `producer_version`, `attempt_count`, `lease_generation`,
`next_attempt_at` — una per una, le chiavi di memoizzazione di CocoIndex, reimplementate a
mano in 610 righe di SQL il 5 agosto. Ed è **keyed sul digest dell'immagine Docling**
(`cfg.Document.DoclingImage` → `ProducerVersion`): tolto Docling, quel registro non ha
nemmeno più una chiave.

**Muoiono due sidecar:**

- **`aura-docling`** — sostituito da un estrattore 18× più veloce che copre 15 formati su
  16 (§3.3). Con lui vanno la rete interna `aura-docling`, il volume
  `aura-docling-models`, i tre `AURA_DOCLING_*`, il segreto generato da `install.sh`,
  `scripts/seed_docling_tokenizer.sh` e l'aggancio dell'OCR-VL a quella rete.
  **L'OCR-VL resta**: serve anche alla visione di Aura (`MULTIMODAL_BASE_URL`).
- **`aura-rerank`** — sul corpus vero non guadagna nulla (§3.2). Libera la GPU.

**Perché il versioning sparisce davvero:** le query filtrano `active = true` ovunque
(`document_retrieval.go:186`), il tombstone scrive `active = false, tombstoned_at`, e
**nessuna query rilegge mai un passaggio tombstonato**. Si scrive storia che nessuno
consulta. Con la riconciliazione lo stato corrente È lo stato.

### Nasce (~400 LOC stimate)

| componente | cosa fa | stato |
|---|---|---|
| flow CocoIndex (Python, sidecar) | sorgente → iscc-tika → chunk → embed → ArcadeDB | provato con MarkItDown, **da riportare su Tika** |
| normalizzazione legacy | `soffice --headless --convert-to` davanti a `.doc`/`.xls`/`.ppt` | provato live (§3.4) |
| `ensure_schema` in SQL nativo | tipi, `ARRAY_OF_FLOATS`, `LSM_VECTOR` — **prima** di ogni scrittura Bolt | provato |
| `documents_mcp` | `document_search`, `document_describe`, `document_resolve` | provato, `mcp/` — **da togliere il rerank** |
| `document_open` (Go, ridotto) | `Router.Route` + copy-in nella sandbox | da fare |
| `table_query` | SQL sulle tabelle ETL; espone `unit.header_json` + `fingerprint` come schema scoperto | da fare |
| identità stabile del passaggio | id catalogo o `bucket/key` al posto del path del walker (§6) | **bloccante** |

I due tool NON vanno confusi: `document_search` recupera un passaggio, `table_query`
calcola un aggregato. "Quanti fornitori gestisce Molteni" ha risposta 258 e quel numero non
è scritto in nessuna cella — nessun retrieval può produrlo.

### Resta

`catalog_*` (CRUD, identità), `retrieval*` (scoping, ridotto), `open.go`, `service.go`,
`filecard/`. **`filecard` non si tocca**: fa già l'estrazione strutturata con i test, ed è
la rappresentazione di routing L1 (`FINDINGS.md` §5b — nello spike era stato duplicato per
errore).

---

## 3. Le misure di stasera

Tutte locali. Nessuna chiamata a OpenRouter.

### 3.1 Il retrieval funziona — su un banco ben posto

Il pessimismo precedente veniva da **TAT-QA, che è inadatto**: il 93% delle sue domande non
nomina niente, perché presuppone che tu stia già guardando la tabella. Rimisurato su
**OTT-QA**, costruito apposta per la ricerca aperta:

```
corpus 8.891 tabelle, n=200, card = titolo pagina + titolo sezione + schema
  recall@1    0,84       recall@10   0,94
  recall@5    0,94       recall@50   0,97
  accuratezza documento  0,85   (denso da solo: 0,84)
  0,45 s a domanda, interamente in locale
```

### 3.2 Il reranker non si guadagna il posto

Su OTT-QA aggiungeva **un punto** (0,84 → 0,85). Non bastava per decidere: quelle card sono
pulite, in inglese, scritte dal benchmark. La misura che mancava è stata fatta sul corpus
vero — ISTAT `Elenco-comuni-italiani.xlsx`, un ordine Schneider `.xls`, la Costituzione —
con le stesse 13 domande e le stesse card:

| | reranker | **denso da solo** |
|---|---|---|
| documento giusto | 8/10 | **8/10** |
| meccanismo giusto (tabella vs passaggio) | 9/10 | **9/10** |

Identico. E sull'astensione il denso è persino migliore delle card: peggior domanda con
risposta 0,2446 contro miglior domanda senza 0,2334, dove il reranker dava rumore puro
(0,0003 contro 0,0006). **Con l'onestà d'obbligo: tre sole domande senza risposta e un
margine di 0,011** — è un indizio, non una soglia di produzione.

### 3.3 L'astensione sono due domande diverse, non una

| domanda | banco | accuratezza massima raggiungibile |
|---|---|---|
| "c'è qualcosa NEL corpus su questo?" | SQuAD-it, negativo = altro paragrafo | **0,90** |
| "la RISPOSTA è dentro questo passaggio?" | SQuAD 2.0, impossibili per costruzione | **0,65** |

Su SQuAD 2.0 le mediane sono 0,9993 contro 0,9974: **nessuna soglia esiste**, perché un
cross-encoder misura la RILEVANZA e una domanda impossibile di SQuAD 2.0 è massimamente
rilevante al suo paragrafo. Quindi la seconda domanda **la decide il modello che legge il
passaggio** — è un contratto di prompt, non una soglia di retrieval. Il che è anche il
motivo per cui il reranker può sparire senza lasciare un buco.

### 3.4 L'estrattore: MarkItDown è fuori, iscc-tika è il candidato

Corpus vero (`.xls` BIFF del 2010, Costituzione in PDF giustificato a 47 pagine, ISTAT
`.xlsx` da 1,2 MB) più una ventaglio LibreOffice in csv/doc/docx/html/odp/ods/odt/pdf/ppt/
pptx/rtf/xls/xlsx. Il punteggio è su **tempo** e su **sopravvivenza verbatim di una frase
nota** — un estrattore che riflusce il testo sembra a posto finché la ricerca per frase
esatta non torna vuota.

| estrattore | trovata | mancante | **fallito** |
|---|---|---|---|
| markitdown 0.1.7 | 6 | 2 | **5** |
| pymupdf4llm | 4 | 4 | 5 |
| extractous 0.3.0 / **iscc-tika 0.6.0** | **15** | 1 | **0** |

- MarkItDown solleva `UnsupportedFormatException` su `.doc`, `.ppt`, `.odt`, `.ods`,
  `.odp`: ogni formato Office legacy e tutta la famiglia OpenDocument. Su `.rtf` fa di
  peggio che fallire — restituisce 22.480 caratteri di markup RTF grezzo come se fosse
  testo.
- **Rompe la ricerca per frase esatta sui PDF giustificati**: sulla Costituzione emette
  `La  sovranità  appartiene  al  popolo`, 49,8 doppi spazi per 1k caratteri. Invisibile,
  perché produce **più** caratteri di un estrattore pulito (96.063 contro 92.891). E non è
  configurabile: quel documento prende il path pdfminer (`form_page_count == 0`), e
  `convert_stream` non espone opzioni di layout.
- **Tika-native vince: 0,26 s contro 6,63 s sul PDF (18×), 1,44 s contro 14,70 s
  sull'xlsx da 1,2 MB (10×).**
- `extractous` e `iscc-tika` sono lo stesso motore con output byte-identico. Si sceglie
  **iscc-tika** perché è vivo: extractous è fermo al 2024-12-21, iscc-tika è al 2026-07-23,
  Apache-2.0. Costo ~117 MB di payload, ~359 MB di immagine.
- **Non sono co-installabili**: due immagini native GraalVM nello stesso processo danno
  `java.lang.NoSuchMethodError ... TesseractOCRConfig.setSkipOcr`. Un banco che le importa
  entrambe condanna quella caricata per seconda — testare ognuna nella propria immagine.

**Il buco `.xls`.** `filecard/build.go:105` instrada solo `.xlsx` e `.xlsm`: un `.xls`
legacy — quello che Excel 2010 e ogni ufficio più vecchio ancora emette — **non ha alcuna
porta d'ingresso in Aura**. Non serve un parser BIFF: LibreOffice è già dentro
`docker/aura/Dockerfile`, e `soffice --headless --convert-to xlsx` preserva tutto ciò che
serve a filecard (85 righe, banner in riga 2, header vero in riga 4, numeri che restano
numeri). Verificato live sul file dell'operatore.

### 3.5 Lo stack di embedding era configurato male in tre modi, tutti silenziosi

Nessuno dei tre dà errore. Danno solo risultati peggiori che sembrano normali.

| difetto | costo misurato |
|---|---|
| `AURA_EMBED_POOLING=last` (residuo Qwen3) su un modello mean | recall@1 **0,90 → 0,70** |
| prefissi asimmetrici assenti | recall@1 **0,25 → 0,05** (5×) |
| immagine CUDA senza `--gpus`: `-ngl 99` ignorato | 128 ms → **7 ms** per testo (18×) |

Corretti in `a0d236105`, e il pooling **cancellando la manopola** invece di validarla: il
GGUF dichiara `gemma-embedding.pooling_type = 1` e llama.cpp lo usa quando la bandiera è
assente. Verificato: senza la bandiera il vettore è identico bit per bit (differenza massima
0,0). Una manopola che può essere giusta solo concordando col file è un footgun — e un
commento che avvertiva di quell'accoppiata c'era già, e non è bastato.

**Il tetto di contesto è 2048 token, non 8192.** Il GGUF dichiara `context_length = 2048` e
Google documenta lo stesso; llama.cpp toglieva l'8192 a ogni avvio ("*the slot context
(8192) exceeds the training context of the model (2048) - capping*"). Un input più lungo
prende HTTP 500, almeno rumorosamente. **Il chunker deve stare sotto i 2048 token**, non
sotto un budget in byte.

---

## 4. Ordine di esecuzione

Ogni fase è verificabile da sola e non rompe la precedente.

**F0 — identità stabile del passaggio.** *Bloccante, va per prima* (§6): finché
`Passage.document` porta il path del walker, la catena trova→apri è spezzata e tutto ciò che
sta sopra è invalidato. Due righe in `process_file`. *Verifica:* `document_search`
restituisce un riferimento che `document_open` risolve.

**F1 — schema-first su ArcadeDB.** Portare le DDL native (`ARRAY_OF_FLOATS` + `LSM_VECTOR` +
FULL_TEXT) in una migration/bootstrap. *Verifica:* `vector.neighbors` recupera un embedding
scritto via Bolt.

**F2 — sidecar CocoIndex.** Un servizio compose che esegue il flow. Stato LMDB **su volume**,
non effimero. *Verifica:* le 4 run di `FINDINGS.md` §2 (baseline / unchanged / modified /
deleted).

> **Il live NON si ottiene con `-L`** (misurato su 1.0.19, §5 trappola 7): la sorgente S3
> non lo implementa. Si ottiene avvolgendo la funzione di sync in `coco.auto_refresh(fn,
> interval=…)` — API pubblica, nessun LiveComponent da scrivere. Provato contro Garage a
> processo residente: add, modify e delete riconciliati senza riavvio.

**F2b — sostituire MarkItDown con iscc-tika nel flow**, con LibreOffice davanti per i
formati legacy. *Verifica:* i 16 formati del banco §3.4 entrano, e la frase nota sopravvive
verbatim sul PDF giustificato.

**F3 — `documents_mcp` come sidecar**, registrato come gli altri MCP di Aura, fail-soft, e
**senza la gamba di rerank**. *Verifica:* i tre tool rispondono e `answered:false` arriva
strutturato.

**F4 — `document_open` ridotto.** Consuma l'id risolto dall'MCP; resta `Router.Route` +
copy-in. *Verifica:* il file compare nella sandbox del chiamante.

**F5 — spegnere il vecchio.** Solo ora cancellare i file di §2, il servizio `aura-docling` e
il servizio `aura-rerank`. *Verifica:* la suite passa senza di essi.

**F6 — `table_query` sulle tabelle ETL.** La capacità Excel, distinta dal retrieval.
*Verifica:* "quanti fornitori per acquisitore" torna 258 per Molteni.

**F7 — routing per prefisso end-to-end.** Il filtro strutturale (§5) usato dall'agente, non
solo disponibile come parametro.

**F8 — UI file manager.** Per ultima: senza F7 è solo un browser di file. La UI scrive sulla
sorgente e CocoIndex riconcilia — nessun collante fra UI e ingestion (provato, RUN 4).

---

## 5. Il routing, e la sorgente

`filecard` + `RouteDocumentCards` esistono già, ed EXP1 aveva misurato che la
rappresentazione testuale **non è la leva** (0,917 flatten contro 0,903 card su 465 file).

La leva non sfruttata è il **path**. Con le cartelle, `clienti/rossi/` risponde a "i
documenti di Rossi" in modo deterministico — nessun embedding, nessun errore possibile.
Copre proprio i casi dove il denso fallisce: nomi propri, codici, entità. Ordine corretto:
**filtro strutturale sul prefisso** quando la domanda nomina un'entità che è una cartella →
**poi** semantico sul residuo.

**Il routing per valore è la cosa che non funziona, e non va risolta ingenuamente.** "Quanti
comuni ha Torino" prende 0,0003 e finisce sul file sbagliato, perché la card porta lo SCHEMA
(`Codice Regione`, `Progressivo del Comune`) e "Torino" è una cella. Un indice dei valori lo
risolve — `Torino → comuni.xlsx` con 322 riscontri — **e rompe qualcosa di peggio**: Caorso
e Verdi sono anch'essi comuni italiani, quindi entrambe le domande senza risposta finiscono
con sicurezza sulla tabella ISTAT.

**La lezione vera: l'astensione è una POST-condizione.** Una card non può rispondere, quindi
chiederle "questa risponde?" è la domanda sbagliata. Prima si risponde, poi si verifica.

### La sorgente: Postgres o S3

Fatto nuovo di stasera, che capovolge il problema. Il database vivo era alla migration 78 e
il piano documenti **non aveva RLS**; portato alla 93, adesso ce l'ha ed è **fail-closed**:
policy `RESTRICTIVE FOR ALL TO aura_app` che pretende `app.current_identity`, senza ramo
permissivo-su-non-impostato.

Quindi una sorgente Postgres che non imposta la GUC **non vede tutte le righe: non ne vede
nessuna**. Il rischio si è invertito, e il pezzo che serviva non è bespoke:
`PgTableSource` accetta un `asyncpg.Pool` che costruiamo noi, e `asyncpg.create_pool`
espone gli hook `setup=` e `init=` per connessione — la GUC si imposta lì, e l'isolamento
resta imposto dal server invece che da una `WHERE` che ci si può dimenticare. `PgSourceSpec`
non ha alcuna clausola di filtro, e con la RLS non serve.

Da decidere ancora: se la sorgente sia il catalogo o il bucket. Entrambe hanno isolamento
imposto dal server (bucket per identità con le credenziali del proprietario, contro RLS per
identità). **Non è una decisione da prendere senza discuterla.**

---

## 6. DIFETTO: il passaggio non porta un'identità azionabile

`Passage.document` contiene il percorso che il walker aveva **al momento dell'ingest**, e
cambia con la sorgente:

```
localfs → /corpus/GH1_0111_ita_it-IT.pdf   (path dentro il container di ingest)
S3      → mutuo.pdf                        (chiave oggetto, SENZA bucket né prefisso)
```

`document_search` lo restituisce come `document`, ma **non è apribile**: `document_open`
accetta un `document_id` (`doc_…` o uuid catalogo) e risolve via catalogo + object store.

**Correzione, prima di integrare:** `process_file` deve scrivere un'identità stabile — id di
catalogo, oppure `bucket/key` completo — e `document_search` deve restituire QUELLA. Il
contratto verso `document_open` resta invariato: l'MCP dice *quale* file, il tool Go fa
routing + copy-in.

L'identità esiste già lato Go: `SearchDocumentID(identityID, sourceKind, sourceKey)` in
`internal/documents/ids.go:85` produce `doc_<32hex>` da uno SHA-256, e `open.go:114` la
risolve. Va prodotta la stessa dal lato Python.

---

## 7. Trappole che costeranno una giornata se ignorate

1. **Lo stato LMDB deve stare su volume.** In directory effimera ogni run riparte da zero:
   sembra funzionare, costa solo tutto ogni volta.
2. **La proprietà vettoriale va tipizzata PRIMA.** Un `MERGE` Cypher crea una lista non
   tipizzata e `LSM_VECTOR` rifiuta di indicizzarla. Schema-first non è stile, è requisito.
3. **`pypdfium2` va chiuso esplicitamente** (textpage, page, document) o abortisce con
   `malloc_consolidate` DOPO il commit — sembra successo fino all'exit code.
4. **Il log di ArcadeDB dice `Plugins directory not found`** ed elenca solo l'AutoBackup.
   È fuorviante: Cypher e Bolt ci sono.
5. **Rename su S3 = copy+delete**, non atomico. Cartelle vuote non esistono senza un
   oggetto marcatore a 0 byte.
6. **`cocoindex update -L` su una sorgente non-live non fallisce: stampa
   `⏳ Ready | Watching for changes...` ed esce con 0.** Sotto `restart: unless-stopped`
   gira in tondo facendo una LIST completa a ogni giro, con tutti i sintomi del successo.
   Solo `localfs(live=True)` restituisce un `_LiveDirItems` con `watch()`;
   `amazon_s3.list_objects` rifiuta `live=` con `TypeError`. Usare `coco.auto_refresh`.
7. **Su Docker Desktop inotify non attraversa un bind mount da Windows**, quindi
   `localfs(live=True)` su una cartella dell'host non vede nulla e sembra che il live sia
   rotto. Provare su un volume Linux, scrivendo dall'interno del container.
8. **L'esempio ufficiale di `filter` di ArcadeDB NON funziona.** La documentazione mostra
   `filter: (SELECT @rid FROM Doc WHERE …)`, ma `parseRidFilter` accetta solo `RID`,
   `Identifiable` o `String`: una subquery dà `ResultInternal` e solleva. Materializzare i
   RID e passare una lista di stringhe. E `vector.boost` vuole `boosts` come **lista** di
   `{field, weight}`, non una mappa.
9. **`vector.neighbors` ha tre opzioni non documentate** — `groupBy`, `groupSize`,
   `maxDistance` — e sono proprio le tre che risolvono il problema dei molti file. La
   pagina ufficiale documenta solo `efSearch` e `filter`.
10. **Il `.env` sopravvive ai cambi di modello.** `AURA_EMBED_MODEL=qwen3-embedding-0.6b`
    era rimasto dopo il passaggio a EmbeddingGemma, e un `AURA_EMBED_MODEL` non vuoto
    **instrada gli embedding su OpenRouter** (`config.EmbedRoute`, D-28). Era armato per
    `aura-arcadedb-mcp`.
11. **`/tmp` nel container `aura` è tmpfs**: `docker cp` dentro restituisce 0 e il file non
    appare mai. LibreOffice dice "source file could not be loaded", che sembra un problema
    di formato e non lo è. Copiare in `/root`.
12. **Una misura che non lascia un file non è ripetibile.** Il banco degli estrattori di
    stasera stampava a video: i numeri di §3.4 sono stati registrati al momento, ma non
    esiste artefatto da rieseguire. Persistere sempre l'output di una valutazione.

---

## 8. Domande aperte, in ordine di impatto

1. **Identità stabile del passaggio** (§6) — bloccante, rompe trova→apri.
2. **La sorgente: catalogo Postgres o bucket S3** (§5) — da discutere, non da decidere di
   corsa. La RLS fail-closed toglie il rischio principale ma non sceglie al posto nostro.
3. **Routing per prefisso end-to-end** — il parametro c'è nell'MCP, l'agente non lo usa.
4. **`etl_flow.py` va rifatto per chiamare `filecard`** invece di reimplementarlo.
5. **`table_query` non esiste.** Oggi le tabelle ETL si interrogano solo a mano.
6. **Il grafo resta ingiustificato**: 85 s a documento, 2 chiamate LLM sequenziali, e zero
   domande misurate a cui risponde e che hybrid+rerank non copriva. Fondere ETL, grafo e RAG
   non risolve nessuno dei quattro fallimenti misurati — erano tutti problemi di segnale di
   routing o di collocazione dell'astensione, non di superficie mancante.
7. **Fail-soft dell'MCP.** Un sidecar giù = documenti non consultabili, mentre un tool
   in-process non può esserlo. Per la memoria il compromesso è già accettato; qui va deciso.
8. **Nessuno scansionato vero in italiano.** GLM-OCR è provato su pixel, non su una
   scansione nativa. E `seed.pdf` (un PDF fatto da slide) sconfigge **tutti** gli
   estrattori, perché la frase si spezza fra caselle di testo.
9. **`data_json` come `jsonb`** invece di TEXT: toglie il cast e permette un indice GIN.

---

## 9. Risorse dello spike lasciate attive

- container `aura-documents-mcp` sulla rete `aura_default`
- bucket Garage `cocoindex-spike` + chiave `cocoindex-spike-key`
- database ArcadeDB `aura_manual_spike`, `aura_ingest_spike`, `aura_s3_spike`
- schema `etl` nel database Postgres `cocoindex`
- volumi `aura-s3-state-v2`, `aura-cocoindex-state`
- immagini `aura-pipeline:probe`, `aura-tika:solo`, `aura-extractous:solo`,
  `aura-extractors:bench`

Nessuno tocca la produzione: `aura_memory`, i `mem_<uuid>` per identità e il bucket
`aura-assets` non sono stati modificati. Backup del database prima della migration 78→93:
`backups/aura-pre-0093-20260806-230918.dump`.

---

## 10. Come riprendere

```bash
cd spikes/cocoindex-ingestion
docker build -t aura-pipeline:probe probes/

# ingest (lo stato LMDB su volume, non /tmp)
docker run --rm --network aura_default \
  -e ARCADEDB_PASSWORD=… -e COCOINDEX_DB=/state/coco.db -e CORPUS=/corpus \
  -v aura-s3-state-v2:/state -v $PWD/flows:/probe:ro -v <corpus>:/corpus:ro \
  aura-pipeline:probe sh -c "cd /probe && cocoindex update -f aura_flow.py"

# retrieval: due numeri, non uno
docker run --rm --network aura_default -e ARCADEDB_PASSWORD=… \
  -e ARCADE_DB=aura_manual_spike -e CASES_FILE=/probe/cases_manual.json \
  -v $PWD/probes:/probe:ro aura-pipeline:probe python /probe/hybrid.py
```

Bucket Garage dello spike: `cocoindex-spike`. La chiave si ricrea con
`garage key create <nome>` + `garage bucket allow --read --write --owner <bucket> --key <id>`.
