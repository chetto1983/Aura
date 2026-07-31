# Findings register — retrieval, ingest, misura (2026-07-30)

> **Registro sorgente storico.** Gli stati correnti `R-01..R-42`, incluse le
> decisioni superseded/retired e la postcondizione live di R-08, sono
> riconciliati in
> [definitive-closure-ledger-2026-07-31.md](definitive-closure-ledger-2026-07-31.md).

Registro dei findings prodotti dalla fase di ricognizione **in sola lettura** che precede
l'implementazione dei test T-1/T-8/T-9 di
[retrieval-e-costi-piano-2026-07-30.md](retrieval-e-costi-piano-2026-07-30.md).

**Come è stato prodotto.** Quattro agent read-only sul deployment vivo e sul codice, 71
claim grezzi, consolidati qui in 34. Nessuna riga di codice scritta durante la ricognizione.
Le voci marcate **[ri-verificato]** le ho ricontrollate personalmente, non prese sulla parola.

**Come leggere la severità.**

| | significato |
|---|---|
| **P0** | rompe qualcosa che un utente usa, adesso |
| **P1** | rompe uno strumento nostro, o arma un difetto per il prossimo che tocca il file |
| **P2** | falso verde: un test o una metrica che passa mentre il difetto è vivo |
| **P3** | rumore, debito, documentazione invecchiata |

**Nota di metodo.** Il gruppo **B** è il più importante di questo registro. Sono errori nella
specifica che ho scritto io, trovati prima di implementarla. Un registro che elenca solo i
difetti degli altri non sta facendo il suo lavoro.

---

## A. Difetti nel sistema vivo

### R-01 · P0 · Il fix sullo stato "embedding morto" non è mai andato a segno, su nessun percorso

`EmbeddingWorker.markDocumentDegraded` passa l'id del **grafo** (`doc_<32hex>`) a
`PostgresCatalogStore.SetDocumentStatus`, che interpreta l'argomento come **UUID** e lo
rifiuta. L'errore viene inghiottito come `WARN` e la riga di catalogo continua a dire
`ready`. Due namespace di id, un confine, mai attraversato — commit `8ea10bc79` compreso.

**Fix**: risolvere l'UUID di catalogo via `metadata->>'search_document_id'`, come già fa
[backfill.go:141](internal/documents/backfill.go#L141).
**Conseguenza sul piano**: l'asserzione di T-9 "se l'embedding muore → `failed`" **fallirà, e
correttamente**. È il test che scopre questo, non che lo assume risolto.

### R-02 · P0 · `aura docs search` restituisce zero risultati con exit 0 nel deployment vivo

`.env:196` ha `AURA_MUSR_ISOLATION=true`; la CLI costruisce la `SearchRequest` **senza
`IdentityID`** ([docs.go:113](cmd/aura/docs.go#L113), [:189](cmd/aura/docs.go#L189));
[search.go:34-36](internal/documents/search.go#L34-L36) fa `return nil, nil` **prima di
qualunque I/O**.

Misurato: `aura docs search "ZOPPI SRL"` → `{"hits": null, "retrieval_ms": 0}`, **exit 0**.
Con isolamento spento: `chunk_index=117`, score **2,5876**, rank 1.

**Blast radius**: solo la CLI operatore. Il percorso del prodotto è sano —
[document_search.go:139](internal/agent/tools/document_search.go#L139) passa
`IdentityID: ownerFromContext(ctx)`, che mappa una ctx vuota sull'UUID `local`.

### R-03 · P1 · Il sidecar rerank va in HTTP 500 sopra ~512 token, e il fallimento è silenzioso

Il batch fisico resta a 512 (nessun flag `-ub`; `-c 2048` è irrilevante). La troncatura a
**480 rune** in [rerank/client.go:32](internal/rerank/client.go#L32) è **l'unica cosa** che
lo tiene in piedi. E [client.go:110-112](internal/rerank/client.go#L110-L112) trasforma un
non-2xx in ordine identità **con errore nil**.

Misurato a 8 candidati: 480 char → OK 1033 ms; **800/1200/1600/2400 char → HTTP 500**.
Log: `input (949 tokens) is too large to process. increase the physical batch size (current batch size: 512)`.

**Perché è armato adesso**: E1 costa **+43%** di caratteri. Chiunque alzi
`maxRerankDocChars` per far entrare chunk più ricchi **spegne il rerank senza un errore, un
log dopo il primo, o un test rosso**.

### R-04 · P1 · L'ingest da CLI non produce embedding, e non è in grado di dirlo

[service.go:144-148](internal/documents/service.go#L144-L148): `Embedder` nil → l'enqueue
viene saltato **senza errore, senza log, senza cambio di stato**. Il job resta `searchable`,
che su quel percorso è terminale.

E [docs.go:90](cmd/aura/docs.go#L90) stampa `"embedding_status": "not_started"` come
**letterale cablato**: l'output è byte-identico che l'embedding sia stato accodato o mai
tentato.

Misurato: `costituzione.pdf` → 92 chunk, **0 embedding**, 0 job `document_embed`.

### R-05 · P1 · `aura.documents` non è scrivibile da nessuna composizione di `documents.Service`

L'unico scrittore è `PostgresCatalogStore.RecordAssetVersion → CreateDocument`, raggiungibile
solo da [documents_api.go:42](internal/agui/documents_api.go#L42) (upload AG-UI), che richiede
`AssetID` + bucket/key + sha256 che un ingest da file locale non ha.

**Conseguenza**: un documento ingerito da CLI è **invisibile al catalogo del cockpit**. È un
buco di prodotto reale, ed è **un buco diverso** da quello del servizio dimezzato.

### R-06 · P1 · Il catalogo dei piani di retrieval è codice morto in produzione

`frozenRetrievalRevisions()` non valorizza mai `CorpusEpoch` e
[main.go:246](cmd/aura/main.go#L246) costruisce `docsToolSearcher{cfg: cfg}` con
`retrievalRevisions` nil → `validateRetrievalRevisions` fallisce **sempre**
(`corpus revision is unavailable`) →
[document_search.go:165-168](internal/agent/tools/document_search.go#L165-L168) ripiega
**ogni volta** su `Retrieve`. Scritto, testato, mai eseguito.

**Conseguenza sul piano**: il bersaglio del router è **`Retrieve`**, non
`ExecuteRetrievalPlan`.

### R-07 · P2 · `RerankThreshold` e `RerankBlend` non sono valorizzati da nessun costruttore

Produzione compresa. La guardia sul rerank gira **sempre** a soglia 0 e blend spento.
Dichiarati a [service.go:54-55](internal/documents/service.go#L54-L55), assegnati da nessuno.

### R-08 · P3 · Cinque chunk orfani e un nodo con doppia label nel grafo vivo

5 `:Chunk` senza `HAS_CHUNK` entrante; il nodo `59c67520-…` porta **sia `:Document` sia
`:Entity`** (è un'entità della memoria, non un documento). Un `MATCH (c:Chunk) RETURN
count(c)` sbaglia di 5, un `MATCH (d:Document)` di 1.

### R-09 · P3 · `document_ingest_jobs` conserva l'errore su una riga poi riuscita

La riga `Clienti.xlsx` è `status=complete`, 118/118 embedded, e il campo `error` contiene
ancora `dial tcp 172.20.0.5:8081: connect: connection refused`. Il campo non viene ripulito
al successo. **Nessun test asserisca `error=''` su una riga riuscita.**

---

## B. Errori nella specifica che ho scritto — trovati prima di implementarla

### R-10 · P2 · **T-8 come specificato non può chiudere il bug che T-9 misura**

Avevo detto di diffare `newDocsService` contro `openRetrievalService`. Sono un costruttore
di **ingest** contro uno di **ricerca**. `Service.Embedder` — il campo la cui assenza causa
R-04 — **non è valorizzato da nessuno dei due**. L'asserzione "non-nil nel runtime, nil nella
CLI" su quella coppia **passa mentre il bug è vivo**.

Il contraltare corretto è un **terzo** costruttore che non avevo nominato:
`runtimeDocumentIngestor.IngestPath` a [docs.go:254-261](cmd/aura/docs.go#L254-L261), che
`Embedder` lo setta. **Tre costruttori, non due.** *(Corretto nel brief prima che B1
partisse.)*

### R-11 · P2 · La regex di T-1 manca il chunk 0 — proprio quello con l'intestazione

`\b[A-G]\d{3,4}:` non matcha le righe 1-50, le cui coordinate hanno 1-2 cifre (`A1:`, `B2:`).
Misurato sul grafo vivo: **117/118** chunk matchano come scritta, **118/118** con
`\b[A-G]\d+:`.

Il chunk 0 è **l'unico che contiene i nomi di colonna**. Un fix che lasciasse le coordinate
lì dentro passerebbe T-1 come specificata.

### R-12 · P2 · T-10 asserisce sulla proprietà sbagliata e **passa oggi con il bug vivo**

**[ri-verificato]** `MATCH (d:Document) RETURN d.file_name, d.title`:

```
"Clienti.xlsx",     "tmpfl5h6p91.xlsx"        <- il nome temporaneo è in TITLE
"costituzione.pdf", "COSTITUZIONE ITALIANA…"
```

`d.file_name` è **già corretto**. Il nome temporaneo perde in `d.title`. La mia asserzione
(`d.file_name = 'Clienti.xlsx' AND NOT d.file_name STARTS WITH 'tmp'`) è verde da subito.

### R-13 · P2 · L'asserzione T-9 su `aura.documents` è strutturalmente irraggiungibile

Vedi R-05. Non è un problema di cablaggio: `RecordAssetVersion` richiede input che un ingest
da file locale non possiede. **Rimossa** dal piano e promossa a decisione separata.

### R-14 · P2 · La codifica E2 come l'ho specificata distrugge i confini di riga

`_chunk_text` **collassa tutti i whitespace, newline compresi**, prima di spezzare. Il
`"\n".join(lines)` viene distrutto e dentro un chunk da 50 righe il letterale `row <n>: ` è
**l'unico delimitatore superstite**. E2 senza prefisso produce un muro indistinto, e perde
anche la citazione: il locator dà solo `row_start`/`row_end`, mai la riga esatta.

**E2 tiene il prefisso → 107 char/riga, non 97.** E il mio confronto E1 escludeva il prefisso
che E0 ha: **E1 è +43%, non +37%** (222 char/riga, non 212).

### R-15 · P3 · I riferimenti di riga nel piano sono stale di ~13 righe

`newDocsService` è a [docs.go:205-229](cmd/aura/docs.go#L205-L229) (il piano dice 218-224);
`openRetrievalService` a [:343-372](cmd/aura/docs.go#L343-L372) (il piano dice 356-371).
Editare le righe citate colpirebbe il codice sbagliato.

### R-16 · P3 · La proprietà ordinale si chiama `chunk_index`, non `ordinal`

Chiavi reali di `:Chunk`: `chunk_index`, `chunk_count`, `chunk_hash`, `content_hash`,
`locator_json`, `heading_path`, `kind`, `text`, `file_name`, `document_id`, `source_id`,
`active`, `embedding`, `embedding_model`, `embedding_version`, `embedded_at`,
`created_at`, `updated_at`, `id`.

---

## C. Trappole di misura — chiunque misuri velocità le incontra

### R-17 · P1 · **Ripetere lo stesso input al sidecar embed misura la prompt cache, non il modello**

| run | tok/s |
|---|---|
| batch da 8, **input identici** | **346.060** |
| batch da 8, **input distinti**, stessa forma | **3.485** |

**Sovrastima di 99×.** Il log lo dice: `selected slot by LCP similarity, sim_best = 0.125`.
Ogni benchmark di embedding **deve** usare payload distinti.

### R-18 · P1 · `n_slots=1`: un batch da 8 sono 8 task seriali, non un passaggio parallelo

Un `launch_slot_`/`release` per input, in sequenza. ~1,5 s per elemento su chunk da 50
righe. Qualunque estrapolazione su Normattiva che assuma parallelismo è sbagliata.

### R-19 · P2 · La latenza del rerank documentata è stale di ~3×

[rerank/client.go:41](internal/rerank/client.go#L41) dice *"~300ms for 10 short docs (spike
070)"*. Misurato al contratto di produzione (8 × 480 rune): **948-1061 ms**, p50 1008.
Scaling ~96-134 ms per candidato: 4→384 ms, 8→948, 16→1808, 32→4271.

### R-20 · P1 · Il costo di una query fulltext è ~7-9 ms, non 2,4-2,9 s

La cifra ingenua a colpo singolo è **avvio JVM + `docker exec`**, non la query. Amortizzata su
100 query nella stessa sessione: **(3640−2941)/99 = 7,1 ms** e **(3180−2289)/99 = 9,0 ms**.
Chi misura il percorso sparse con un `cypher-shell` per query misura Java che parte.

### R-21 · P3 · Latenza embed misurata, per forma di input

| input | latenza |
|---|---|
| query corta (hot path retrieval) | min 12,7 · **p50 17,7-22,7** · p95 44-46 ms |
| chunk ~7,5 KB / ~5300 token | **1229-1417 ms** singolo |

---

## D. Rischi d'ambiente — cose che fanno fallire il lavoro per motivi non tecnici

### R-22 · P1 · Due worktree git **attivi dentro il repo**, con copie dei file che tocchiamo

`D:/Aura/.claude/worktrees/agent-a218f7c0a1287596f` e `.../agent-a825a40ca6592da00` (più due
stale). Contengono duplicati di `internal/eval/retrieval_eval.go` e
`internal/documents/document_ingest_live_test.go`. Un `grep -r` dalla radice ritorna **10
hit per il tag `retrieval_eval`, 8 dei quali esche.**

**Regola operativa**: ogni ricerca e ogni edit vanno scopati a `./internal` e `./cmd`.

### R-23 · P1 · L'albero di lavoro è più sporco di quanto credessi, e qualcosa ci sta ancora scrivendo

**9 file modificati, non 6.** I file che avevo indicato (`internal/agent/mcptools/bridge*.go`,
`memory_integration_test.go`) sono **puliti** — sono atterrati in `ec7e7f7ed`. Al loro posto
ci sono **quattro file non previsti** sotto `web/src/chat/` (`ExternalStoreChat.tsx`,
`composer/useReasoningCapabilities.ts`, `composer/useReasoningEffort.ts`,
`composer/__tests__/reasoningEffort.test.ts`), con mtime **10:57-10:58 di oggi**, assenti dal
primo `git status` della sessione.

**Qualunque commit deve nominare i path esplicitamente e verificare con `git show --stat`.**

### R-24 · P2 · Un tier nuovo non conta nel floor di copertura 85%

`document_ingest_live` e `retrieval_eval` contribuiscono **zero**. Ogni pezzo di logica pura
(codifica, diff strutturale dei campi) deve stare in test **senza tag**, o l'aggregato scende
sotto 85% e la CI fallisce ~20 minuti dopo il push.

### R-25 · P2 · Un `.py` nuovo accanto ad `app.py` non arriva nel container

[Dockerfile:28](docker/markitdown/Dockerfile#L28) è `COPY app.py .` e il servizio **non ha
bind mount** (`docker inspect … .Mounts` → `[]`). Un modulo nuovo passerebbe il test locale
mentre il sidecar continua a servire E0 per sempre. **Il `COPY` cambia nello stesso commit**,
e si verifica confrontando l'image id, non con un test verde.

### R-26 · P3 · Il container markitdown **non** sta mascherando la modifica

Buona notizia, verificata: `md5sum` di `/app/app.py` nel container e di
`docker/markitdown/app.py` nel repo **coincidono** (`fc96992a…`). Un rebuild atterra.

### R-27 · P2 · `openpyxl.EmptyCell` non ha `.coordinate` né `.column` in read_only

Il codice attuale sopravvive **solo per l'ordine delle istruzioni** (`if value is None:
continue` a [app.py:256-257](docker/markitdown/app.py#L256-L257) gira **prima** di
`cell.coordinate` a [:261](docker/markitdown/app.py#L261)). Una E1 che raggiunga
`cell.column` va in `AttributeError` sul primo foglio con una cella vuota interna — e
`Clienti.xlsx` non ne ha, **quindi il fixture non lo prende**.

### R-28 · P3 · Build pulito al baseline

`go build ./...` e `go vet ./...` **exit 0**, e i tier taggati (`db_integration
neo4j_integration`, `document_ingest_live`, `retrieval_eval`) vettano puliti. Qualunque
rumore nuovo appartiene a chi implementa.

---

## E. Documentazione invecchiata

### R-29 · P1 · **[ri-verificato]** I conteggi delle migration in CLAUDE.md sono materialmente sbagliati

| | CLAUDE.md dice | reale su disco |
|---|---|---|
| Postgres | 40 (`0001-0040`, floor `0040_shared_links`) | **79** (`0079_idempotency_rejected_state`), DB a `version 79` |
| Cypher | 2 (`0001-0002`) | **7** (`0007_chunk_embedding_1024`) |

CLAUDE.md contiene già la regola giusta — *"il numero non si deduce: `ls … | tail -1`"* — e
**il proprio esempio la viola**. Chi si fida del numero scritto crea una migration che
collide.

### R-30 · P3 · `aura.document_chunks` e `aura.document_embeddings` sono vuote e inutilizzate

0 righe entrambe. Il ledger vero per file è **`aura.document_ingest_jobs`**. La colonna di
raggruppamento dei job è `job_type`, **non** `kind`.

---

## G. Memoria a lungo termine — trovati e chiusi il 2026-07-30 pomeriggio

Questi vengono da un E2E con l'agente vero, non da lettura di codice. Il metodo che ha
funzionato è quello che avevo già in memoria e non stavo applicando: **chiedere ad Aura per
prima**. Ha risposto in un turno a una domanda su cui stavo facendo grep da venti minuti.

### R-39 · P0 · **CHIUSO** — il bridge nascondeva i due tool che costruiscono il grafo

[bridge_memory.go](internal/agent/mcptools/bridge_memory.go) teneva `memory_add_entity` e
`memory_create_relationship` fuori dal modello. Sono **gli unici due** che creano un nodo e
un arco; senza, la memoria a lungo termine non può diventare un grafo, perché lo scrittore
dei fatti non conia entità di proposito.

Nascondere non risparmiava niente: sono `Deferred`, quindi non pesano sul manifest per turno
— costano solo quando `tool_search` li restituisce.

**Misurato, stesso prompt prima e dopo:**

| | entità create | fatti | `ABOUT_SUBJECT` | archi |
|---|---|---|---|---|
| prima (`ACME PROBE SRL`) | **0** | 3 | **0/3** | 0 |
| dopo (`BOREALIS PROBE SPA`) | **2** | 3 | **3/3** | 1 |

Totali sul grafo: entità 19→21, `ABOUT_SUBJECT` 4→7. Chiuso in `6f001bf9`.

### R-40 · P1 · L'agente ha riportato all'operatore una proprietà del grafo che era falsa

Convinta che `add_entity` non esistesse, ha usato `add_fact` e ha scritto:

> *"crea/aggancia entità automaticamente… i tre fatti sono collegati al nodo ACME PROBE SRL
> nel grafo… `memory_get_entity("ACME PROBE SRL")` restituirà l'entità"*

Zero entità, zero archi. Consegnato in tabella con tre spunte verdi. **Non è un tool che
fallisce: è un'asserzione sul grafo, sbagliata, con la faccia della certezza** — la forma di
difetto di tutto questo documento, arrivata fino all'operatore.

La causa immediata è R-39 ed è chiusa. Resta aperto che nulla le impedisca di affermare una
proprietà strutturale senza verificarla.

### ~~R-41~~ · **RITIRATA** — il tipo di relazione È conservato e traversabile

> **Sbagliata, e la sbagliavo io.** Avevo scritto che il tipo collassa perché la mia query
> proiettava `type(r)`, che in Neo4j è sempre l'etichetta dell'arco. Il tipo semantico vive
> in `r.type`, **dentro il pattern del MERGE**:
> `MERGE (e1)-[r:RELATED_TO {type: $relation_type}]->(e2)`
> ([queries.py:548](docker/agent-memory/src/neo4j_agent_memory/graph/queries.py#L548)) —
> quindi `FOLLOWS` e `WORKS_AT` sono **archi distinti**, non lo stesso arco sovrascritto.
>
> Verificato in due modi. Sonda Cypher diretta: due tipi fra la stessa coppia coesistono, e
> `MATCH ()-[r:RELATED_TO {type:'FOLLOWS'}]->()` ne restituisce esattamente 1. E poi
> end-to-end con l'agente, che è la prova che conta:
>
> ```
> da           etichetta     tipo_semantico   verso
> CARLO NERI   RELATED_TO    FOLLOWS          DELTA PROBE SRL
> ```
>
> *"Quali clienti segue X"* **è** una traversata: si filtra sulla proprietà. Il modello dati
> è corretto; era la mia proiezione a essere incompleta.

### R-42 · **P0** · Ogni nuova persona viene proposta come fusione con l'operatore

**[misurato due volte su due, grafo vivo 2026-07-30]**

```
da            verso     metodo      confidenza  stato
MARIA VERDI   Davide    embedding   0.8749      pending
CARLO NERI    Davide    embedding   0.8915      pending
```

Non è un caso isolato: **due entità `:Person` create, due archi `SAME_AS` verso `Davide`**,
entrambi per similarità di embedding sopra 0,87. Nomi di persona italiani non correlati.
Alzato a P0 dalla prima stesura, che lo trattava come una proposta singola.

**Perché è grave, e non lo dico io.** La doc di `memory_forget` nel sidecar lo scrive:

> *"the deduplicator emits SAME_AS between entities it thinks are alike — and a wrong SAME_AS
> is how the graph degrades on its own: the entity it points at becomes an **attractor** that
> later writes of the same name merge onto. Nothing else can break one."*

Quindi il meccanismo di degrado è **documentato dall'autore**, lo strumento per romperlo
esiste ed è esposto al modello (`memory_forget` con `node_type='relationship'`) — e **niente
si accorge** che vada usato. Uno `status: "pending"` che nessuno legge non è un gate.

Si salda con le **42 ragioni sociali duplicate** di `Clienti.xlsx` (R-36): se la chiave di
identità è il nome e l'arbitro è il coseno, entità distinte si fondono. Su un grafo clienti è
l'errore che non si vede finché non risponde male. **La chiave dev'essere il codice.**

Il numero che manca prima di scegliere la soglia: la distribuzione delle confidenze su coppie
note-distinte contro coppie note-uguali, sul corpus vero. 0,87 su due nomi non correlati dice
che l'embedder non separa i nomi propri — non che la soglia va alzata di un po'.

---

## F. Evidenza esterna — cosa dicono i vendor e i paper, e cosa ne facciamo

Questi non sono difetti, quindi non hanno una P. Hanno un verdetto: **conferma** (sapevamo,
ora è sostenuto da qualcuno che paga il conto), **opzione nuova** (non era nel piano),
**scarto** (valutato e rifiutato, con la ragione), **non applicabile**.

> **Nota operativa, vale per la prossima volta.** `neo4j.com` risponde **403** a WebFetch e
> Medium **429**. Le quattro fonti sono state lette con **i sidecar di Aura**: `curl` con
> User-Agent browser → `POST :8083/extract` (markitdown) per il testo strutturato, e
> `:18080` (SearXNG) per trovare le pagine di dettaglio. Il sidecar ha anche riprodotto dal
> vivo **R-12**: `title: tmp6l2xzfmy.html`.

### R-31 · conferma · Neo4j dice del **proprio** strumento di non puntarlo sui fogli di calcolo

[LLM Graph Builder](https://neo4j.com/labs/genai-ecosystem/llm-graph-builder/), riquadro di
avvertenza: *"best results for files with long-form text in English"* · *"**less well suited
for tabular data like Excel or CSV** or images/diagrams/slides"*.

La divisione **tabellare deterministico / prosa con modello** del piano non è più una nostra
opinione contro un paper: è la posizione del produttore sul suo prodotto. Non cambia il
piano, lo sostiene.

### R-32 · **opzione nuova** · Il grafo kNN `SIMILAR` — espansione senza entità

LGB collega i chunk molto simili con una relazione **`SIMILAR`**, formando un grafo kNN
*prima* e *indipendentemente* da qualunque estrazione di entità.

**Perché è la scoperta che conta.** Il punto 1 del piano (`MENTIONS` a 3 archi) è bloccato
dietro l'estrazione, che costa modelli, tempo e manutenzione (R-35). `SIMILAR` no: si
costruisce **con l'indice vettoriale che abbiamo già** — `chunk_embedding`, 1024d, COSINE,
ONLINE al 100% (vedi baseline) — un passaggio batch di `queryNodes` top-k per chunk e un
`MERGE` dell'arco. **Zero chiamate a un modello.**

È il gradino mancante fra dove siamo (**1 hop su archi lessicali**) e dove vogliamo andare
(**grafo di entità**), e va misurato **prima** di spendere sull'estrazione — perché se
l'espansione via `SIMILAR` recupera gran parte del guadagno, il preventivo dell'estrazione
cambia di segno.

### R-33 · conferma · La forma-obiettivo dell'espansione è 2 hop su entità, non 1 su lessico

| | LLM Graph Builder | Aura oggi |
|---|---|---|
| seed | vector index **+ fulltext ibrido** | vector, con fallback sparse |
| espansione | relazioni **fra entità, fino a 2 hop** | **1 hop** su `NEXT_CHUNK\|HAS_CHUNK` |
| layer del grafo | lessicale **ed** entity, separati e ispezionabili | solo lessicale |

Il commento in [graphrag.go:78-81](internal/documents/graphrag.go#L78-L81) già annota che
l'arco `:HAS_CHUNK` ancorato al chunk non matcha nessun chunk fratello, quindi in pratica
l'espansione vive **tutta** su `:NEXT_CHUNK` — l'ordine di lettura. Navigazione strutturale,
non ragionamento, esattamente come dice il piano.

### R-34 · conferma · L'estrazione va vincolata a uno schema, non lasciata libera

LGB: *"higher quality data extraction if you configure the graph schema for nodes and
relationship types"*, con schema predefinito, schema proprio, schema pescato dal DB esistente,
o dedotto da un'ontologia RDF / uno schema Cypher.

Stessa cosa dal lato LangChain nel post Milvus: `LLMGraphTransformer` può dichiarare label e
proprietà esplicite, *"reducing noise that can be generated by similar meanings or labels with
different spellings or tenses"*.

**Conseguenza su T-7**: il set di label ammesse si decide **prima** di misurare, non dopo. È
la stessa lezione della chiave di MERGE (le 42 ragioni sociali duplicate): un'entità mal
tipizzata non è un'etichetta sbagliata, è **un nodo in più**.

### R-35 · conferma · Il costo di manutenzione dell'estrazione LLM è dichiarato dal vendor

LGB ha una funzione dedicata, **"Delete Disconnected Nodes"**, per le entità che dopo
l'estrazione restano attaccate **solo ai chunk** e a nient'altro. Chi vende lo strumento ha
dovuto costruire una UI per ripulire la sua stessa uscita.

Va nel preventivo di T-7 come voce esplicita, non come sorpresa.

### R-36 · **applicabile a T-6** · Lo scheletro dell'ingest tabellare, in tre passaggi

Dal [pezzo su CSV/JSON](https://medium.com/aarth-software/data-ingestion-with-neo4j-leveraging-csv-and-json-8d25b9705b3),
e coincide con quello che serve alla proiezione di `Clienti.xlsx`:

1. **constraint prima del carico** — `CREATE CONSTRAINT FOR (c:Cliente) REQUIRE c.codice IS UNIQUE`
2. **`MERGE` dei nodi**, un passaggio per label
3. **relazioni in un passaggio separato** — `MATCH` + `MATCH` + `MERGE`

**Due adattamenti obbligatori per noi.** `LOAD CSV` vuole il file dentro la import dir di
Neo4j e noi passiamo per `mcp-neo4j-cypher`: la forma è **`UNWIND $rows AS row`**
parametrizzato, con `CALL {} IN TRANSACTIONS` per il batching. E il constraint **non è
cosmetico**: con 42 ragioni sociali duplicate su clienti diversi, la chiave dev'essere il
codice `Cliente`, altrimenti il `MERGE` fonde clienti distinti e la query torna un risultato
— semplicemente quello sbagliato.

L'articolo è del maggio 2023 e ammette da solo che APOC *"above 5 aren't supporting it at
times"*: buon tutorial, non fonte autorevole. Il pattern regge, i comandi vanno riverificati.

### R-37 · **non applicabile** · Sharded property databases è Enterprise

[La pagina](https://neo4j.com/docs/operations-manual/current/scalability/sharded-property-databases/data-ingestion/)
si apre con **"Infinigraph · Not available on Aura · Introduced in 2025.12"** e parla di
`neo4j-admin database import full --property-shard-count`. Noi giriamo **Neo4j Community**.
Zero applicabilità — meglio dirlo che ricavarne un paragrafo di contorno.

### R-38 · **scarto** · Vettori in Milvus + grafo in Neo4j

Il [post Neo4j+Milvus](https://neo4j.com/blog/developer/ingest-documents-neo4j-milvus/) mette
i vettori in Milvus e il grafo in Neo4j, con `LLMGraphTransformer` e Llama 3.1 8B via Ollama.

**Non compra niente qui**: i vettori sono già in Neo4j a 1024d con HNSW quantizzato e
l'indice è ONLINE; un secondo store raddoppierebbe la superficie operativa su un mini-PC con
**3699 MiB su 4096 già occupati** dalla GPU. È anche un post in co-marketing con Milvus, e la
ragione dello split è quella. L'unica cosa che porto via è R-34, che sta anche altrove.

---

## Baseline vivo — i numeri "prima", misurati 2026-07-30

**Neo4j**

| | |
|---|---|
| `:Chunk` totali | **215** — 123 con embedding, **92 senza** |
| i 92 senza | esattamente `costituzione.pdf`, ingerito da CLI |
| `Clienti.xlsx` | **118 chunk / 118 embedded**; ZOPPI in `chunk_index=117` |
| chunk con un nome di colonna | **1 su 118** |
| `:Document` | 3 nodi etichettati, **2 reali** (il terzo è `:Document:Entity`) |
| `MENTIONS` / `:Entity` | **3** / 19 — nessuna dall'ingest documenti |
| `HAS_CHUNK` / `NEXT_CHUNK` | 210 / 208 |
| `chunk_embedding` | VECTOR, **1024d**, COSINE, `hnsw.m=32`, `ef_construction=200`, quantization ON, `vector-2.0`, ONLINE 100% |
| `chunk_text` | FULLTEXT, analyzer standard, `eventually_consistent=false`, ONLINE 100% |

**Postgres**

| | |
|---|---|
| `aura.documents` | **1** (Clienti.xlsx, `ready`, dall'upload) |
| `aura.ingestion_jobs` | 2 — `asset_process/succeeded`, `document_embed/succeeded` |
| `aura.document_ingest_jobs` | Clienti.xlsx `complete` 118/118 · costituzione.pdf **`searchable` 92/0**, `completed_at` NULL |

**Sidecar**

| | |
|---|---|
| embed | Qwen3-Embedding-0.6B Q8_0, `n_embd=1024`, `n_ctx=8192`, GPU (3699/4096 MiB), **`n_slots=1`** |
| rerank | Qwen3-Reranker-0.6B Q4_K_M, `n_ctx=2048`, `--pooling rank`, `-ngl 99` |

**T-3 ha già la sua risposta.** `db.index.fulltext.queryNodes('chunk_text','ZOPPI SRL')` →
`chunk_index=117`, score **2,5876**, contro 0,5518 del secondo: **margine 4,7×**, costo
marginale **~7-9 ms**. La premessa di T-3 è vera sui dati vivi e non richiede lavoro di
indicizzazione.

---

## Cosa cambia nel piano

| # | prima | dopo |
|---|---|---|
| T-8 | diff fra 2 costruttori | **3 costruttori**, e il diff include `Embedder`/`MaxBytes` (R-10) |
| T-8 | montare i 6 campi | montare i campi **e passare l'identità**, o resta un no-op (R-02) |
| T-9 | `≥1 riga in aura.documents` | **rimossa** — irraggiungibile (R-13); il buco diventa decisione a sé (R-05) |
| T-9 | asserisce `failed` su embedding morto | **si aspetta che fallisca**, e chiude R-01 sulla superficie toccata |
| T-1 | regex `\d{3,4}` | `\d+` (R-11) |
| T-2 | E1 +37%, E2 97 char | **E1 +43% (222), E2 107 con prefisso** (R-14) |
| T-10 | asserisce `d.file_name` | **`d.title`** (R-12) |
| speed | batch tok/s | **payload distinti**, e niente estrapolazioni parallele (R-17, R-18) |
| router | bersaglio `ExecuteRetrievalPlan` | **`Retrieve`** finché non c'è un `CorpusEpoch` vero (R-06) |
| punto 1 | estrazione entità è l'unico salto | **prima `SIMILAR`**, che non richiede modelli (R-32) |
| T-6 | proiezione da schema | constraint → `UNWIND $rows` + `MERGE` → relazioni a parte (R-36) |
| T-7 | misurare poi decidere lo schema | **label ammesse decise prima**, e cleanup nel preventivo (R-34, R-35) |

**Un nuovo test si aggiunge alla lista.** L'espansione via `SIMILAR` (R-32) è misurabile
**oggi**, con l'indice che c'è e senza estrarre niente: costruire il grafo kNN sui 215 chunk
vivi e rimisurare recall@5 contro l'espansione a 1 hop attuale. Se recupera gran parte del
guadagno atteso dalle entità, il preventivo dell'estrazione cambia di segno — e questo va
saputo **prima** di spendere, non dopo.

**Fuori scope, da promuovere a task propri:** R-03 (rerank fail-soft armato), R-05 (ingest CLI
invisibile al catalogo), R-06 (rianimare i piani congelati), R-07, R-29 (correggere CLAUDE.md),
R-22/R-23 (worktree dentro il repo, albero sporco che qualcuno sta ancora scrivendo).
