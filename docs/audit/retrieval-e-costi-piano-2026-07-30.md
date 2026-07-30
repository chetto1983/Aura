# Retrieval e costi — cosa ho misurato e cosa resta da fare

Sessione 2026-07-29/30. Tutto quello che segue è misurato sul deployment locale, non dedotto.
I numeri hanno la data perché invecchiano.

## La forma comune dei difetti

Nove difetti su undici hanno la stessa struttura: **un contratto a due lati, verificato su un
lato solo**. Nessuno falliva in modo netto. Ognuno produceva qualcosa di plausibile e
sbagliato, e il segnale arrivava all'operatore travestito da altro.

| difetto | come si presentava |
|---|---|
| pin `mcp` solo lato build | `recv: unexpected EOF` — sembrava un guasto di trasporto |
| volume che maschera la cache uv | nessuna modifica al Dockerfile arrivava a destinazione |
| `-f` cablato nell'unit systemd | avrebbe ignorato `compose.minipc.yaml` in silenzio |
| gate sul manifest invece che sugli argomenti | un round trip sprecato per ogni tool cercato e non usato |
| `activated` senza tetto | ~11k token di manifest a turno nelle sessioni lunghe |
| embedding morto → documento `ready` | l'agente risponde male e nessuno sa perché |
| `-c` per slot | fallimento netto sostituito da lentezza silenziosa su CPU |
| servizio CLI dimezzato | `docs bench` cronometra una pipeline che non è quella vera |
| titolo documento nel grafo | chi cerca nel grafo vede un nome che non esiste |

Se una cosa sola deve sopravvivere a questo documento: **quando aggiungi un vincolo, chiediti
chi altro deve rispettarlo.**

## Chiuso e verificato

### Costi e cache

- **Tariffe a runtime da `/models`** invece della tabella cablata, che copriva 1 modello su
  367 ed era il 30% sotto il prezzo reale. Con `CacheReadPer1M`: senza, il mix a 30 giorni
  veniva prezzato $7,86 contro i $2,889 reali — sovrastima di 2,7×.
- **Usage sommato su tutte le chiamate del turno.** Era una variabile di ciclo: un turno con
  N chiamate registrava solo l'ultima. Misurato prima del fix: 24 turni assistente, 5 con
  usage.
- **Cache misurata a prefisso stabile: 95,9 → 99,4%**, in salita. Non c'è niente da spremere.

### Contesto

- **Il 91% di ogni turno non è la conversazione.** Su un turno reale: 13.266 token di prompt,
  di cui 1.155 di chat e ~12.100 di prefisso fisso.
- **Il manifest era l'82% del prefisso.** Sceso da 9.878 a **3.854 token** portando i tool
  sempre-attivi da 17 a 4. Primo turno a cache fredda: da 11.803 a **5.447**.
- **L1 non rovina il contesto.** Sostituisce l'output con un puntatore ripaginabile, e il
  costo non è per-turno ma una tantum per elemento che attraversa la soglia: turni 3-5
  misurati a 96,7 → 98,6 → 99,4%. La mia tesi iniziale ("invalida a ogni turno") era falsa.
- **Il gate chiedeva la domanda sbagliata.** Ora `everLoaded` risponde "gli argomenti sono
  fondati?" invece di "è nel manifest?", e `activated` tiene il manifest piccolo per conto suo.

### Embedding

- **Batch a byte, non a chunk.** Un foglio si spezza in righe da ~8 KB: un batch da 32
  chiedeva ~67k token a un sidecar che ne aveva 4096.
- **`-c` è per slot.** A 32768 × 4 slot llama.cpp non entrava nei 4 GB e cadeva su CPU:
  ~300 tok/s. Con `-np 1 -c 8192`: **~730 tok/s, GPU al 100%**.
- **Un embedding morto marca il documento.** `DocumentStatusFailed` esisteva nel tipo e nel
  vincolo DB, dichiarato e mai scritto da nessuno.

  > **RITIRATO dal "chiuso e verificato", 2026-07-30.** Il fix `8ea10bc79` **non ha mai
  > funzionato, nemmeno sul percorso di upload.** `EmbeddingWorker.markDocumentDegraded`
  > passa l'id del **grafo** (`doc_<32hex>`) a `PostgresCatalogStore.SetDocumentStatus`, che
  > interpreta l'argomento come UUID e lo rifiuta; l'errore viene inghiottito come `WARN` e
  > la riga di catalogo continua a dire `ready`. Due namespace di id, un contratto a due
  > lati, verificato su un lato solo — la stessa forma di tutti gli altri.
  >
  > Avevo scritto che era chiuso perché il codice c'era e il test passava. Nessuno dei due
  > provava che l'id attraversasse il confine. Il fix vero risolve l'UUID di catalogo via
  > `metadata->>'search_document_id'`, come già fa `backfill.go:141`.
  >
  > Conseguenza su T-9: l'asserzione "se l'embedding muore, `status = failed`" **fallirà — e
  > correttamente.** È il test che scopre questo bug, non che lo assume risolto.

## Aperto, in ordine di valore

### 1. Estrazione entità — il salto vero

Misurato: **`MENTIONS` ha 2 archi su 210 chunk**. Il grafo semantico dei documenti è vuoto.

Il multi-hop oggi può fare solo `chunk → documento → chunk vicino`: navigazione strutturale,
non ragionamento. Se `ZOPPI SRL` fosse un nodo `:Cliente` invece di 10 caratteri dentro un
blob, *"quali clienti ha seguito Berbotto a Cuneo"* diventerebbe una traversata. Oggi non è
né una traversata né una ricerca semantica: non è niente.

È la differenza fra "Aura cerca dentro i tuoi file" e "Aura conosce i tuoi clienti", ed è
l'unica cosa che rende utile il grafo che stiamo già pagando.

### 2. Un router che scelga il percorso di retrieval

Confronto misurato sulla stessa domanda ("codice cliente di ZOPPI SRL"):

| percorso | esito | tempo |
|---|---|---|
| vettoriale, 3 chiamate | **non trovato** | ~30 s |
| fulltext + traversata | **riga esatta** | ~0,1 s |

Una lookup esatta non è un lavoro da coseno. L'indice fulltext `chunk_text` c'è già e
`mcp-neo4j-cypher` è montato: manca solo che qualcosa scelga fra i due.

**Attenzione a due trappole che ho verificato di persona:**

- **Il chunk per riga NON è la soluzione.** Misurato: chunk intero 0,7162, riga di ZOPPI
  0,5225 e solo terza su 41 — **0,7×, peggio**. Il paper sullo Structure-Aware Chunking parla
  di blocchi chiave-valore con i nomi delle colonne; Aura rende `A5882: DIST.CUNEO | B5882:
  F031`, cioè coordinate di cella. Su ~130 caratteri il nome azienda ne occupa 10 e il resto
  è impalcatura. **Il problema è la rappresentazione, non la granularità.**
- Avevo raccomandato il chunk per riga citando il paper senza verificare che Aura ne
  rispettasse la premessa. Non la rispetta.

### 3. Il servizio `docs` della CLI è dimezzato

`cmd/aura/docs.go:218-223` monta `Jobs`, `Extractor`, `Indexer`, `Searcher`. Mancano
`Embedder`, `QueryEmbedder`, `Reranker`, `Knowledge`.

Conseguenze misurate: la Costituzione ingerita via CLI ha **92 chunk e 0 embedding**, nessuna
riga in `aura.documents`, nessun job `document_embed` — quindi il fix sullo stato non può
nemmeno scattare, non c'è un documento da marcare. E `docs search` gira solo sparse, con
`Vector`/`Reranker`/`Expand` tutti falsi.

**`docs bench` non misura la pipeline vera.** Finché resta così, non è utilizzabile come
strumento di test: `retrieval_p95_ms: 0` e `industrial_score: 74` sono di una pipeline
dimezzata.

> **Correzione, 2026-07-30, verificata sul codice e sul deployment vivo.** Due cose qui sopra
> sono sbagliate, e la seconda è peggiore della prima.
>
> **(a) `aura.documents` non c'entra con il servizio dimezzato.** Quella tabella non è
> scrivibile da nessuna composizione di `documents.Service`: la scrive solo
> `catalog_store.go:274 RecordAssetVersion → CreateDocument`, la cui unica catena di chiamata
> in produzione parte da `internal/agui/documents_api.go:42` — il percorso di upload AG-UI.
> Montare `Embedder` nella CLI **non** produrrà una riga lì. Che l'ingest da CLI resti
> invisibile al catalogo è un buco vero, ma è **un altro buco**, e va deciso a parte.
>
> **(b) `retrieval_p95_ms: 0` non è (solo) la pipeline dimezzata: è un cortocircuito prima
> di qualunque I/O.** Il deployment ha `AURA_MUSR_ISOLATION=true` e la CLI costruisce la
> `SearchRequest` **senza `IdentityID`** ([docs.go:113](cmd/aura/docs.go#L113),
> [:189](cmd/aura/docs.go#L189)); [search.go:34-36](internal/documents/search.go#L34-L36)
> allora fa `return nil, nil` e basta. Misurato sullo stack vivo: `aura docs search "ZOPPI
> SRL"` → `{"hits": null, "retrieval_ms": 0}` **e exit code 0**. Con l'isolamento spento, lo
> stesso comando torna `chunk_..._000117` a rango 1, score 2,5876, con dentro ZOPPI SRL e
> BERBOTTO PAOLO. **Quel `0` è il p95 di una funzione che ritorna prima di fare niente**, e
> `industrialScore` non può accorgersene perché penalizza `chunks == 0` e mai `hits == 0`
> ([docs.go:433-448](cmd/aura/docs.go#L433-L448)), mentre gli hit vengono buttati alla riga
> 189. Un T-8 che monta i sei campi e non passa l'identità sarebbe **field-identical e
> ancora un no-op**.
>
> Stessa forma di sempre: contratto a due lati. Qui però il segnale non arrivava travestito
> da altro — non arrivava affatto. Exit 0, JSON valido, zero risultati.

### 4. Cose piccole con conseguenze visibili

- **Titolo documento nel grafo**: `tmpfl5h6p91.xlsx` invece di `Clienti.xlsx`. Postgres ha il
  nome giusto, il grafo il nome del file temporaneo di upload.
- **TTL della cache**: default OpenRouter **5 minuti**. Misurato un turno con `cached=0` dopo
  7 minuti di pausa. Esiste `"ttl": "1h"` in `cache_control`, a fronte di un costo di
  scrittura maggiorato — vale sulle conversazioni intermittenti.
- **`/activity` è in ritardo di un giorno.** L'ultima data disponibile era il 28 mentre
  misuravamo il 29. Per la pagina costi servono tre fonti: `cache_metrics` per il per-turno
  (ed è l'unica che sa la cache hit rate, che OpenRouter non espone), `/key` per i totali
  live, `/activity` per lo storico e il breakdown per modello, con il ritardo dichiarato in
  pagina o sembra rotta.

### 5. Debito lasciato aperto sull'appliance

- **La chiave OpenRouter del collega è ancora sostituita dalla mia.** Backup in
  `/opt/aura/.env.bak.20260729-120156`. Da ripristinare.
- **`compose.cloud.yaml` ripreso da git ha perso 168 byte** di modifiche locali (stesso numero
  di righe, valori cambiati a mano). Non recuperabili. Il file non esiste più nel repo:
  l'appliance va allineata ai due file nuovi.
- Immagini `aura` e `aura-agent-memory-mcp` già caricate lì, mai attivate.

### 6. Non mio, ma va detto

`TestWriteAdaptiveBenchmarkReportCreatesPrivateNonReplacingArtifact` fallisce con
`permissions=0666`. **Preesistente** — verificato con stash su albero pulito. È Windows che
non applica `0600`.

## Le cinque prove sulla cache: meccanica sì, magnitudine no

Fatte, e l'eviction è visibile: al turno in cui la soglia attraversa il tool result, il
prompt **si accorcia di 478 token** (2.215 byte sostituiti da un puntatore di ~90).

Ma il danno alla cache su quel thread è trascurabile — 95,9%, non un crollo — perché conta la
*posizione*: la cache tiene fino al punto riscritto e perde ciò che segue. Lì il punto stava
quasi in fondo a un thread corto. Sull'appliance i 14 tool result spillati stavano da seq 9 a
seq 55 su 65: il punto di rottura era in testa, e tutto il resto andava perso.

**Per misurare il costo vero serve un thread con quella forma.** Il numero che manca è quello,
e non l'ho.

---

# Parte II — pattern industriali e prove

Sessione 2026-07-30. La Parte I dice *cosa* è rotto. Questa dice *quale forma* ha la
soluzione in letteratura e *quale prova* la accetta o la rifiuta. Ogni numero è
etichettato: **[misurato]** oggi sul deployment o sui fixture reali, **[paper]** preso
dalla fonte citata, **[da misurare]** ancora vuoto.

Regola che vale per tutta questa parte: **un pattern preso da un paper è un'ipotesi finché
non passa la premessa del paper sui nostri dati.** La Parte I ha già pagato una volta per
non averlo fatto (il chunk-per-riga raccomandato senza verificare la premessa).

## La rappresentazione tabellare: il paper dice intestazione, il codice scrive coordinate

Il difetto è a una riga sola, ed è leggibile:
[app.py:261](docker/markitdown/app.py#L261) → `cells.append(f"{cell.coordinate}: {text}")`.
La chiave di ogni cella è la **coordinata**, non il nome della colonna. L'intestazione
(riga 1) finisce nel primo chunk e in nessuno degli altri 117.

Lo *Structure-Aware Chunking for Tabular Data* (arXiv 2605.00318) costruisce un Row Tree in
cui *"non-empty cells are encoded as `column_name: value` pairs"*, con l'intestazione
portata nel blocco. **[paper]** MRR 0,3576 → 0,5945 in hybrid; Recall@1 0,366 → 0,754 in
BM25-only; −40% di chunk. La premessa è il nome di colonna. Aura non la rispetta — ed è
questo, non la granularità, che spiega il 0,5225 misurato nella Parte I.

Tre codifiche possibili, misurate oggi su `Clienti.xlsx` (5.889 righe × 7 colonne):

| codifica | riga 5882 | char/riga | impalcatura |
|---|---|---|---|
| **E0 — oggi** | `row 5882: A5882: DIST.CUNEO \| B5882: F031 \| … \| G5882: BERBOTTO PAOLO` | **155** | **30,8%** |
| **E1 — KV pieno (STC)** | `Area Comm Fil Cli: DIST.CUNEO \| … \| Venditore Esterno: BERBOTTO PAOLO` | 212 (+37%) | 0% |
| **E2 — intestazione in testa al chunk, valori in riga** | `DIST.CUNEO \| F031 \| Alba \| 424410 \| ZOPPI SRL \| TREISO \| BERBOTTO PAOLO` | **97 (−37%)** | 0% |

**[misurato]** Su E0 l'impalcatura di coordinate è 280.833 caratteri su 912.252: quasi un
terzo del corpo del foglio non dice niente a nessuno. E2 è **più piccolo di oggi** e non
perde nulla; E1 costa il 37% in più ma mette il nome della colonna accanto a ogni valore,
che è ciò che il paper misura.

Non so quale delle due vinca su Aura, e **non lo deduco**: lo decide **T-2**. Chi
implementa E1 senza T-2 ripete l'errore della Parte I con il segno opposto.

SpreadsheetLLM / SheetCompressor (arXiv 2407.09025) va oltre — structural anchor,
inverted-index translation, aggregazione per formato, **[paper]** fino al 96% di
compressione e +25,6% sul table detection — ma la sua premessa è il foglio *irregolare*
(regioni unite, tabelle multiple per sheet). `Clienti.xlsx` è una tabella singola,
rettangolare, con intestazione: **la parte di SpreadsheetLLM che ci serve è già in E1/E2.**
Il resto è complessità che non paga qui, e va tenuta per quando arriverà il foglio storto.

## Il router: lessicale, non semantico, e non free-form

Tre risultati che si incastrano e dicono la stessa cosa.

**BEIR** (Thakur et al. 2021) e il seguito: i modelli densi crollano fuori dominio sulle
query a **entità rara**. Il termine raro viene mediato via nel vettore; l'indice invertito
lo tratta esattamente come qualunque altro. È letteralmente il caso ZOPPI della Parte I —
vettoriale **non trovato** in ~30 s, fulltext riga esatta in ~0,1 s. Non è una stranezza
del nostro deployment: è il comportamento atteso, documentato, di quella classe di query.

**Adaptive-RAG** (Jeong et al., NAACL 2024): un classificatore *piccolo e dedicato* sceglie
fra non-retrieval / single-step / multi-step. **[paper]** F1 50,91 con GPT-3.5 a **1,03
passi medi** contro le pipeline multi-step fisse. La forma è quella che ci serve: non un
LLM che decide, un classificatore che decide.

**RAGRouter-Bench** (arXiv 2604.03455) dà il numero che conta per un mini-PC: **[paper]**
TF-IDF + SVM → **93,2% di accuratezza**, macro-F1 0,928, **28,1% di token risparmiati**
contro il sempre-il-più-caro (il tetto teorico a etichette perfette è 35,2%). E la riga da
incorniciare: **le feature lessicali battono gli embedding semantici di 3,1 punti di
macro-F1.** Il router giusto è un TF-IDF addestrato, non una chiamata all'embedder.

Questo si salda con due cose che sappiamo già: il mini-LLM su CPU non è praticabile per la
selezione (≥1500 ms), e il classificatore di reasoning embedda 27 anchor a turno. **Un
router lessicale non tocca né l'uno né l'altro: è micro-secondi e zero I/O.**

**Quello che il router NON deve essere: text2cypher libero.** Il dataset Neo4j Text2Cypher
ha 44.387 istanze e **[paper]** anche GPT-4o si ferma intorno al **30% di match in
valutazione per esecuzione**, con il crollo concentrato proprio sugli schemi non visti. Un
generatore di Cypher che sbaglia 7 volte su 10 non è un percorso di retrieval, è una fonte
di risposte plausibili e sbagliate — la forma esatta del difetto che apre questo documento.
**Il bersaglio del router è un insieme piccolo e fisso di traversate parametrizzate**,
scritte e testate a mano, come i cinque piani già congelati in
[retrieval_plan.go:13-19](internal/documents/retrieval_plan.go#L13-L19).

E qui c'è la buona notizia strutturale: **il catalogo dei piani esiste già.**
`FreezeRetrievalPlans` congela `sparse|static|vector|vector_rerank|vector_rerank_expand`
con revisione, scope e digest, e `ValidateResult` rifiuta un risultato che nomini un piano
non congelato. Oggi sceglie `staticRetrievalPlan`, che guarda quali sidecar sono vivi — non
che domanda è. **Il router è un assegnatore dentro un catalogo già congelato, verificato e
testato.** Non è un'architettura nuova.

> **Correzione, 2026-07-30.** Avevo scritto "manca **una sola cosa**: chi sceglie". Ne
> mancano due, e la seconda è più grave. **Il ramo dei piani congelati è codice morto in
> produzione oggi**: `frozenRetrievalRevisions()` non valorizza mai `CorpusEpoch` e
> [main.go:246](cmd/aura/main.go#L246) costruisce `docsToolSearcher{cfg: cfg}` con
> `retrievalRevisions` nil, quindi `validateRetrievalRevisions` fallisce **sempre**
> (`corpus revision is unavailable`) e
> [document_search.go:165-168](internal/agent/tools/document_search.go#L165-L168) ripiega
> **ogni volta** su `Retrieve`. Il catalogo è scritto, testato e mai eseguito.
>
> E il test che avrebbe dovuto sorvegliarlo passa per il motivo sbagliato:
> `TestDocsToolSearcherFailsClosedWithoutCorpusEpoch` usa `cfg: &config.Config{}`, il cui
> `BoltURL` vuoto fa scattare la guardia `!health.Sparse` **prima**, così il controllo
> sull'epoch da cui prende il nome non viene mai raggiunto.
>
> Conseguenza sul piano: il bersaglio del router è **`Retrieve`**, non
> `ExecuteRetrievalPlan`, finché qualcuno non decide da dove viene un `CorpusEpoch` vero —
> e quella è una modifica al comportamento vivo di `document_search`, quindi un lavoro suo,
> non una riga di contorno. Nota a margine: `RerankThreshold` e `RerankBlend` non sono
> valorizzati da nessun costruttore, **produzione compresa** — la guardia sul rerank gira
> sempre a soglia 0 e blend spento.

## L'estrazione: due corpora, due metodi, e non è lo stesso lavoro

Il salto del punto 1 (`MENTIONS` a 2 archi su 210 chunk) si divide in due problemi che la
letteratura tratta insieme e che qui vanno separati, perché **hanno costi diversi di tre
ordini di grandezza.**

### Tabellare: nessun modello, lo schema è già lì

`Clienti.xlsx` ha l'intestazione. Le colonne **sono** l'ontologia. Estrarne entità non è
NER: è una proiezione. **[misurato]** oggi sul file:

| colonna | valori distinti | nodo |
|---|---|---|
| Area Comm Fil Cli | **5** | `:AreaCommerciale` |
| Filiale Ass. | **35** | `:Filiale` (codice) |
| Nome Filiale Assegnazione | 35 | proprietà di `:Filiale` |
| Cliente | **5.889** | `:Cliente` — chiave |
| Ragione sociale | 5.845 | proprietà di `:Cliente` |
| Località | **1.084** | `:Località` |
| Venditore Esterno | **76** | `:Venditore` |

**7.089 nodi e ~23.5k archi, zero chiamate a un modello.** Confronto di scala:
**[paper]** GraphRAG-Bench 2025 misura la costruzione del grafo a **79,9 M token**
(GraphRAG) e **83,9 M** (LightRAG) — e LightRAG esiste proprio per tagliare ~60% del costo
di indicizzazione rinunciando alle community. Su un foglio con intestazione quel costo è
**interamente evitabile**, e pagarlo sarebbe l'anti-pattern.

**[misurato]** Due dipendenze funzionali che il modello deve rispettare: `Filiale → Area` è
una funzione (nessuna filiale in due aree) e `codice filiale → nome` pure. Sono vincoli, non
osservazioni: vanno nel constraint Neo4j, e T-6 li asserisce.

**[misurato] E una trappola di entity resolution, già armata.** Ci sono **42 ragioni sociali
duplicate** (`3D S.R.L.`, `BOSCHI GIULIANO`, …) su clienti *diversi*. Il sidecar memoria fa
MERGE su `{name, type, deduplication_scope}` (Parte I, D-3): applicato qui, **fonderebbe 42
coppie di clienti distinti in un nodo solo**, e nessuno se ne accorgerebbe — la query
tornerebbe un risultato, semplicemente il risultato sbagliato. **La chiave è `Cliente`, il
codice** (5.889 distinti su 5.889 righe). Ancora la stessa forma: contratto a due lati.

### Prosa: qui il modello serve, ed è già stato misurato

Per `Costituzione.pdf`, il G220, l'epub e i 18.147 PDF di Normattiva non c'è nessuno schema
da proiettare. Serve un estrattore.

**D-3 non va ri-litigato, va ri-scopato.** La decisione "non adottare GLiNER" del 2026-07-26
è motivata testualmente dal fatto che *"la pipeline in cui GLiNER vivrebbe non è mai
invocata"* — `store_message` a 0 chiamate, nel sidecar memoria. **Il pipeline documenti è un
percorso di chiamata che esiste e gira.** La decisione era corretta per il suo consumatore e
non dice niente su questo. I numeri, invece, si trasferiscono interi:

| **[misurato] 2026-07-26** | tipi corretti | falsi positivi | latenza | licenza |
|---|---|---|---|---|
| `onnx-community/gliner_multi-v2.1` → `onnx/model_fp16.onnx` | **7/7** | **0** | **119 ms** CPU | apache-2.0 ✓ |
| stesso repo → `model_quantized.onnx` (int8) | **0/7** | 0 | 44 ms | — *silenziosamente rotto* |
| spaCy (4 modelli) | peggiore su tutti | allucina sul rumore | — | — |

119 ms per chunk su CPU, senza toccare la GPU che sta già embeddando. Su 210 chunk sono
25 secondi. Su un LLM sono 210 chiamate.

**Il limite di GLiNER resta quello scritto in D-3: entità sì, relazioni no**
(`gliner_extractor.py:561` restituisce `relations=[]` per costruzione). Per i documenti va
bene ed è il punto: il grafo tabellare prende le relazioni dallo schema, il grafo di prosa
parte da `(:Chunk)-[:MENTIONS]->(:Entity)` — che è esattamente l'arco che oggi non esiste, e
che rende il multi-hop una traversata invece che una navigazione strutturale.

**Cosa viene dopo, quando gli archi esistono.** HippoRAG 2 (ICML 2025) fa retrieval con
Personalized PageRank su un grafo a doppio nodo (passage + phrase): **[paper]** +7 punti F1
sui task associativi contro i retriever a embedding, con **meno** token. Neo4j ha GDS e la
skill `neo4j-gds-skill` è installata. **Non è lavoro per adesso** — PPR su un grafo con 2
archi non fa niente. È la ragione per cui l'ordine è: prima gli archi, poi il router, poi
eventualmente PPR.

## Contextual Retrieval: dove serve e dove no

Anthropic *Contextual Retrieval*: si antepone a ogni chunk un contesto generato dal
documento, prima di embeddare e prima di indicizzare BM25. **[paper]** −35% di fallimenti
top-20 con i soli contextual embeddings, **−49%** aggiungendo il contextual BM25, **−67%**
col reranker.

**Sul tabellare non serve**: il contesto di una riga sono i nomi delle colonne, ed E1/E2 li
mettono lì gratis e in modo deterministico. Generarli con un LLM sarebbe pagare due volte
peggio.

**Sulla prosa il caso è forte** — un chunk di `Costituzione.pdf` che dice *"le condizioni
di cui al comma precedente"* è irrecuperabile senza il suo intorno — **ma il conto va fatto
prima**: è una chiamata LLM per chunk in ingest. Su 210 chunk è una prova; su 18.147 PDF è
un progetto. **[da misurare]** T-7 dà il costo per documento, e solo dopo si decide. La
variante povera e deterministica — anteporre `heading_path` (già estratto e già nello
schema) al testo del chunk prima di embeddare — costa **zero** e va misurata per prima, come
baseline contro cui la versione LLM deve giustificarsi.

## Il corpus di prova esiste già

`D:\turing_AgentMemory_MCP\test\` — e non è un fixture inventato per il test, è materiale
reale, che è la sola ragione per cui i numeri qui sopra valgono qualcosa.

| fixture | forma | a cosa serve |
|---|---|---|
| `Clienti.xlsx` | 5.889 × 7, intestazione, `Foglio1` | **tabellare**: E0/E1/E2, estrazione da schema, lookup esatta |
| `Costituzione.pdf` | 478 KB, prosa strutturata, **92 chunk / 0 embedding** oggi | **prosa**: regressione del §3, riferimenti interni |
| `G220_op_instr_0824_en-US.pdf` | 30 MB, manuale tecnico EN | già in uso da `graphrag_live_test.go` — baseline p95 |
| `Corso Base Robot.docx`, `Robot.pptx` | 18 KB / 21 MB | copertura formati |
| `Diario-ultimo.epub` | 2,1 MB, prosa lunga IT | contextual retrieval su testo continuo |
| `normattiva/pdf_vigente/` | **18.147 PDF, ~920 MB, 13 collezioni** | **la prova di scala** |

Normattiva è la parte importante e va usata a gradini, non tutta: `Codici.zip` sono **40
PDF / 22 MB** (gradino 1), `Testi_Unici.zip` **256 / 26 MB** (gradino 2), `Leggi di
ratifica` **2.334 / 133 MB** (gradino 3). Il manifest per-collezione è in
`pdf_vigente/_manifest.json`.

Su questo corpus si misura la cosa che nessun benchmark pubblico può dirci: **quanto costa
l'ingest per documento e come scala**, con l'estrattore vero, l'embedder vero
(Qwen3-Embedding-0.6B, 1024-d, `--pooling last`, GPU) e il grafo vero.

## I test E2E

Undici prove. Ognuna ha una ground truth verificabile senza giudizio umano e senza LLM
giudice: **[misurato]** oggi, dai fixture, con openpyxl.

> **Gotcha di copertura, da leggere prima di scegliere il tag.** Per CLAUDE.md il gate gira
> su `db_integration neo4j_integration` (e `db_integration` da solo nel job Skills). Un tier
> nuovo sotto un tag nuovo **conta ZERO nel floor 85%** — come già succede a
> `docker_integration`. Quindi: la logica pura di ogni test qui sotto (codifica del chunk,
> classificazione del router, proiezione dello schema) va in test **senza tag**, e sotto il
> tag live resta solo ciò che ha davvero bisogno dello stack.

| id | prova | tag | fallisce se |
|---|---|---|---|
| **T-1** | la codifica xlsx porta i nomi di colonna | *nessuno* (unit) | il chunk contiene `E5882:` |
| **T-2** | **quale codifica vince davvero** | `retrieval_eval` | E1/E2 non battono 0,7162 |
| **T-3** | lookup esatta: sparse batte dense | `neo4j_integration` | ZOPPI non è rank 1 nel piano sparse |
| **T-4** | il router assegna il piano giusto | `retrieval_eval` | accuratezza < 90% o costa un round trip |
| **T-5** | **Berbotto a Cuneo: 0 o 198** | `retrieval_eval` | risponde senza dire quale lettura |
| **T-6** | proiezione deterministica dello schema | `neo4j_integration` | conteggi ≠ attesi o ≥1 chiamata LLM |
| **T-7** | estrazione su prosa, a gradini | `retrieval_eval` | costo/documento fuori budget |
| **T-8** | **il servizio CLI è quello vero** | *nessuno* (unit) | un campo non-nil nel runtime è nil nella CLI |
| **T-9** | ingest CLI → embedding vivi | `document_ingest_live` | 0 embedding o 0 job `document_embed` |
| **T-10** | il titolo nel grafo è quello vero | `neo4j_integration` | `d.file_name` inizia per `tmp` |
| **T-11** | **la forma di thread che manca** | `retrieval_eval` | — è una misura, non un'asserzione |

### T-1 — la codifica porta i nomi di colonna

Unit su `_extract_xlsx`, nessuno stack. Ground truth **[misurato]**: `Clienti.xlsx` produce
**118 chunk** (5.890 righe / 50), e la riga 5882 cade nel chunk **118**.

Asserzioni: ogni chunk `kind="rows"` contiene almeno un nome di colonna dell'intestazione;
**nessun** chunk contiene la regex `\b[A-G]\d{3,4}:` ; il chunk 118 contiene
`ZOPPI SRL` **e** `BERBOTTO PAOLO` **e** l'etichetta della colonna che li governa.

*Se fallisce*: la migrazione della codifica non è arrivata al sidecar — lo stesso difetto
del volume che maschera la cache uv (Parte I).

### T-2 — quale codifica vince davvero

**È il test che decide, e ha già un'ipotesi nulla misurata.** Baseline dalla Parte I: chunk
intero **0,7162**, riga in E0 **0,5225** (terza su 41).

Procedura: le stesse 41 righe, la stessa query, lo stesso embedder; si embedda la riga 5882
in E0, E1 ed E2 e si confrontano coseno e rango.

| | coseno | rango | esito |
|---|---|---|---|
| chunk intero (E0, 50 righe) | 0,7162 **[misurato]** | 1 | baseline |
| riga in E0 | 0,5225 **[misurato]** | 3/41 | **rifiutata** |
| riga in E1 (KV pieno) | **[da misurare]** | | passa se > 0,7162 **e** rango 1 |
| riga in E2 (intestazione hoistata) | **[da misurare]** | | idem, a −37% di caratteri |

*Se falliscono entrambe*: la tesi della rappresentazione è sbagliata quanto quella della
granularità, il chunk-per-riga resta chiuso in via definitiva e il punto 2 si appoggia
interamente al percorso fulltext. **Questo esito va scritto, non nascosto.**

### T-3 — lookup esatta: sparse batte dense

`neo4j_integration`, sullo stack. Query `"ZOPPI SRL"`, corpus `Clienti.xlsx` ingerito.

Asserzioni: il piano `sparse` ritorna il chunk 118 a **rango 1** in **< 200 ms** p95
(coerente con i ~0,1 s della Parte I più il costo del seam MCP, ~40-50 ms per lettura
secondo [graphrag_live_test.go:24-30](internal/documents/graphrag_live_test.go#L24-L30)); il
piano `vector` viene eseguito **e registrato** — rango e latenza a log, senza asserzione,
perché il punto è il confronto, non la condanna.

*Se il vettoriale vince*: BEIR non si applica a questo corpus e il router va ripensato. Va
saputo prima di costruirlo.

### T-4 — il router assegna il piano giusto

Set etichettato di query sui fixture, **tre classi**: `lookup esatta` (ragione sociale,
codice cliente, numero di articolo), `semantica` (*"come si azzera il convertitore"*),
`multi-hop` (*"quali clienti segue X"*). Minimo 60 query, ≥15 per classe, etichettate a mano
— la ground truth è l'etichetta, non l'output di un LLM.

Asserzioni: accuratezza **≥ 90%** (RAGRouter-Bench misura 93,2% con TF-IDF+SVM su un
compito comparabile — sotto 90% il router non sta imparando niente di reale); **latenza di
decisione < 5 ms**, che è il modo di dire *nessuna chiamata di rete e nessun embedding*; e
recall@5 dell'insieme instradato **≥** recall@5 del sempre-vettoriale, a costo minore.

**Il router non deve mai essere l'unico a decidere se il risultato è buono.** Su
misclassificazione, il fallback è il piano `static` di oggi — che è già fail-soft e già
validato da `ValidateResult`.

### T-5 — Berbotto a Cuneo: 0 o 198

**Questo test nasce da un errore trovato oggi nella Parte I.** La domanda d'esempio *"quali
clienti ha seguito Berbotto a Cuneo"* è **ambigua sul corpus vero**, e le due letture non
danno risposte vicine — danno risposte opposte:

| lettura | ground truth **[misurato]** |
|---|---|
| `Località = CUNEO` | **0 clienti.** Berbotto non ha un solo cliente nel comune di Cuneo. |
| `Area Comm = DIST.CUNEO` | **198 clienti** (su 199 totali; 1 è in DIST.TORINO). |

`CUNEO` è simultaneamente un valore di `Località` (98 clienti, 16 venditori, **nessuno dei
quali Berbotto**) e la radice di un valore di `Area Comm Fil Cli`. Berbotto lavora su
**Alba** (186), Bra (10), e una riga ciascuno a Venaria, Fossano e Cuneo-filiale.

Asserzione: il sistema **o** chiede quale lettura, **o** risponde dichiarando quale ha
scelto e con quale campo. **Una lista di clienti senza il nome del campo è un fallimento**,
anche quando i 198 sono giusti — perché la stessa risposta, sotto l'altra lettura, doveva
essere vuota.

*Perché è il test più importante dei undici*: è l'unico che misura se il grafo ha reso il
sistema **più preciso** invece che più sicuro di sé. Ed è la forma di difetto della Parte I
applicata al prodotto invece che al codice.

### T-6 — proiezione deterministica dello schema

Dopo l'ingest di `Clienti.xlsx`, conteggi esatti in Neo4j **[misurato]**:

```
:AreaCommerciale = 5      :Filiale = 35        :Cliente = 5889
:Località = 1084          :Venditore = 76
(:Venditore {name:'BERBOTTO PAOLO'})-[:SEGUE]->(:Cliente) = 199
```

Più i tre invarianti che il caso vero ha già armato: `Filiale → Area` funzionale (constraint,
non asserzione a runtime); `Cliente` chiave di MERGE — **`Ragione sociale` no**, perché 42
sono duplicate e fonderebbero clienti distinti; **contatore di chiamate LLM == 0**.

*Se il contatore non è 0*: qualcuno ha instradato un foglio con intestazione dentro
l'estrattore di prosa, e sta pagando token per una proiezione.

### T-7 — estrazione su prosa, a gradini

Non un'asserzione secca: una misura con budget, su `Codici.zip` (40 PDF) → `Testi_Unici.zip`
(256) → `Leggi_di_ratifica.zip` (2.334), fermandosi appena un gradino esce dal budget.

Per gradino: entità/chunk, tasso di falso positivo su un campione etichettato a mano (≥100
entità), **costo per documento** e **wall-clock per documento**, tenendo separati GLiNER
fp16 CPU (119 ms/chunk **[misurato]**) e l'estrattore LLM. Nello stesso giro si misura anche
la baseline povera del contextual retrieval — `heading_path` anteposto al testo prima
dell'embedding, costo zero — contro cui la variante LLM deve giustificarsi.

Gate: il gradino 3 deve stare in un ingest notturno sull'appliance. Se non ci sta, non si
adotta a 18k PDF, e va detto invece di scoprirlo in produzione.

### T-8 — il servizio CLI è quello vero

Unit, nessuno stack, e **deve essere strutturale**: si confrontano campo per campo la
`documents.Service` costruita da `newDocsService` e quella di `openRetrievalService`
([docs.go:218-224](cmd/aura/docs.go#L218-L224) contro
[docs.go:356-371](cmd/aura/docs.go#L356-L371)), fallendo su ogni campo non-nil nel runtime e
nil nella CLI.

Un test comportamentale qui non basta: `docs bench` "funziona" oggi, stampa
`industrial_score: 74` e misura una pipeline che non esiste. **Il difetto della Parte I è
un contratto a due lati verificato su un lato solo, e solo un'asserzione strutturale lo
tiene chiuso** quando qualcuno aggiungerà il sesto campo.

### T-9 — ingest CLI → embedding vivi

`document_ingest_live`, su `Costituzione.pdf`. Stato oggi **[misurato]**: 92 chunk, **0
embedding**, 0 righe in `aura.documents`, 0 job `document_embed`.

Asserzioni post-T-8: ≥1 job `document_embed` accodato; chunk con `embedding IS NOT NULL`
**uguale al conteggio dei chunk** (uguaglianza fra i due, non il 92 cablato); e — arrivando
dal fix `8ea10bc79` — se l'embedding muore, `status = failed`, mai `ready`. Quest'ultima è
la parte che oggi non è raggiungibile.

Il job non si drena da solo: l'handler `document_embed` è registrato solo in
[asset_processing_worker.go:57](cmd/aura/asset_processing_worker.go#L57), sotto `aura serve`.
Il test deve **guidare il worker in modo sincrono** (il pattern `syncEmbedQueue` di
`graphrag_live_test.go`), non accodare e sperare.

> **Correzione, 2026-07-30.** Avevo messo fra le asserzioni "≥1 riga in `aura.documents`".
> **È irraggiungibile e va tolta**: quella tabella non è scrivibile da nessuna composizione
> di `documents.Service` (vedi la correzione al punto 3 della Parte I). Che l'ingest da CLI
> non compaia nel catalogo resta un buco reale — ma è un buco diverso, e T-9 non è il posto
> dove chiuderlo.

### T-10 — il titolo nel grafo è quello vero

Ingest attraverso il percorso di upload, non da path locale. Asserzione:
`d.file_name = 'Clienti.xlsx'`, e `NOT d.file_name STARTS WITH 'tmp'`. Postgres e Neo4j
devono dire lo stesso nome — di nuovo un contratto a due lati.

### T-11 — la forma di thread che manca

La Parte I si chiude su un numero che non c'è, e questo è il modo di prenderlo. La forma da
ricostruire è quella misurata sull'appliance: **~65 messaggi, 14 tool result spillati fra
seq 9 e seq 55** — punto di rottura in testa, non in coda. Il thread corto già misurato
(95,9%) non serve: la cache tiene fino al punto riscritto e perde ciò che segue, quindi il
danno è funzione della *posizione*, e lì la posizione era quasi in fondo.

Da registrare a ogni turno: `cached_tokens`, `prompt_tokens`, costo, e la seq del primo
elemento riscritto. Il numero cercato è **il costo del turno in cui la soglia attraversa il
tool result più in alto**.

Nello stesso giro si chiude anche il TTL: default OpenRouter 5 minuti, `"ttl": "1h"`
disponibile. **[paper/listino]** la scrittura a 1h costa **2×** l'input contro **1,25×** a 5
minuti, la lettura **0,1×** in entrambi i casi → **il break-even è fra 5 e 7 letture**
sulla stessa cache. Sopra quella soglia conviene; sotto no. Le conversazioni intermittenti
(Telegram) stanno sopra, una sessione continua di cockpit sta sotto: **è una decisione per
canale, non globale.**

## Ordine, e perché è questo

1. **T-8 + T-9** — finché `docs bench` misura una pipeline dimezzata, nessun numero prodotto
   dalla CLI vale niente, T-2 e T-7 compresi. Si sistema lo strumento prima di usarlo.
2. **T-1 + T-2** — la codifica è due righe di Python e decide se il punto 2 della Parte I ha
   una gamba in più o una in meno. Costa poco e chiude un'incertezza grossa.
3. **T-6** — la proiezione tabellare porta 7.089 nodi a costo zero. È il grafo che rende
   sensato tutto il resto, e non aspetta nessuna decisione di modello.
4. **T-3 + T-4 + T-5** — il router sopra un grafo che ora ha qualcosa dentro. T-5 per ultimo
   dei tre, perché è quello che dice se è servito.
5. **T-7** — la prosa a gradini, quando c'è un router che sa quando non chiamarla.
6. **T-10 + T-11** — indipendenti da tutto il resto; T-11 richiede solo pazienza e un thread
   della forma giusta.

PPR/HippoRAG, community detection e contextual retrieval con LLM stanno **dopo** e solo se
T-7 dice che il costo regge. Su un grafo con 2 archi non fanno niente, e su 18.147 PDF non
si sa ancora quanto costino.

## Fonti

- [Structure-Aware Chunking for Tabular Data in RAG (arXiv 2605.00318)](https://arxiv.org/html/2605.00318v1)
- [SpreadsheetLLM: Encoding Spreadsheets for LLMs (arXiv 2407.09025)](https://huggingface.co/papers/2407.09025)
- [Adaptive-RAG (Jeong et al., NAACL 2024)](https://aclanthology.org/2024.naacl-long.389/)
- [Lightweight Query Routing for Adaptive RAG — RAGRouter-Bench (arXiv 2604.03455)](https://arxiv.org/html/2604.03455v1)
- [BEIR: A Heterogeneous Benchmark for Zero-shot IR (Thakur et al. 2021)](https://datasets-benchmarks-proceedings.neurips.cc/paper/2021/file/65b9eea6e1cc6bb9f0cd2a47751a186f-Paper-round2.pdf)
- [Anthropic — Contextual Retrieval](https://www.anthropic.com/engineering/contextual-retrieval)
- [GraphRAG-Bench (arXiv 2506.02404)](https://arxiv.org/pdf/2506.02404)
- [HippoRAG 2 / From RAG to Memory (ICML 2025)](https://icml.cc/virtual/2025/poster/45585)
- [Neo4j Text2Cypher (2024) dataset e benchmark](https://neo4j.com/blog/developer/benchmarking-neo4j-text2cypher-dataset/)
- [Anthropic — prompt caching, TTL e moltiplicatori di scrittura](https://platform.claude.com/docs/en/build-with-claude/prompt-caching)
