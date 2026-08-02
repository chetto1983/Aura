# Retrieval one-store + sandbox — sintesi dello studio

**Stato:** PROPOSTO, evidence-backed — *non* una decisione approvata. Richiede un
PRD-amendment prima di qualsiasi codice di produzione (PRD-first principle), e
**supera l'ADR 0038** ("keep Neo4j / reject all-Postgres, reject ArcadeDB"), che è
già di fatto riaperto: **il deploy locale è ora su ArcadeDB** (l'alternativa Apache
che l'ADR aveva scartato). Due architetture candidate — "solo Postgres" (Opzione A)
e "Postgres + ArcadeDB" (Opzione B, raccomandata) — vedi §3b.

**Branch:** `claude/document-retrieval-postgres-4cadcd` · **Spike:**
`spikes/document-routing-benchmark/` (toy, 21 file) e
`spikes/document-routing-scale/` (~460 file + doc ~770 pagine).

Questo documento è il punto unico da leggere: raccoglie la mappatura del codice
attuale, le decisioni con la loro evidenza, i risultati dei due spike, le
autocorrezioni fatte strada facendo, e ciò che resta da decidere.

---

## 1. Contesto — la proposta dell'operatore

> "Il retrieve dei documenti, soprattutto Excel, fa schifo. Togliere completamente
> Garage e Neo4j, usare solo Postgres e sandbox." (+ Tantivy, Apache AGE come idee)

Obiettivo: **un solo datastore + il sandbox**, eliminando Neo4j (grafo + vettori) e
Garage (object store), e risolvere la pessima resa su Excel. *Aggiornamento:*
l'operatore ha successivamente spostato il **deploy locale su ArcadeDB** (multi-model
Apache) → il "solo datastore" può essere Postgres (Opzione A) oppure Postgres per il
control-plane + ArcadeDB per grafo/vettori/FTS/memoria (Opzione B). Vedi §3b.

## 2. Stato attuale mappato (codice)

- **Retrieval documenti** (`internal/documents/`): oggi vive **interamente su Neo4j**
  — vettoriale HNSW (`chunk_embedding`, 1024d), BM25 fulltext
  (`db.index.fulltext.queryNodes('chunk_text')`), espansione grafo 1-hop
  (`:NEXT_CHUNK`/`:HAS_CHUNK`). Postgres tiene solo lo stato dei job. **Nessun
  pgvector nel progetto.**
- **Excel "fa schifo"** — causa radice in `docker/markitdown/app.py` `_extract_xlsx`
  + `_normalize`: struttura tabellare appiattita a prosa (newline→spazi), header
  perso dopo il 1° chunk (taglio ogni 50 righe), `data_only=False` (salva le
  *formule* non i valori), 50 righe eterogenee → 1 solo vettore.
- **Neo4j blast radius**: 4 dataset — retrieval documenti (Neo4j-only), memoria
  agente POLE+O (sidecar Python, genuinamente graph-shaped), adaptive projection
  (Postgres è authoritative → banale), cockpit graph explorer (UI). Accesso via
  `mcp-neo4j-cypher` (dati) + driver nativo (solo DDL).
- **Garage blast radius**: astrazione `objectstore.Store` con backend
  `filesystem-dev` **già production-grade** → passare a filesystem è **zero-code**
  per single-operator; solo l'isolamento per-identità (Garage Admin API) è
  accoppiato, ed è **già deferred a MUSR** (Amendment #88.2).

## 3. Decisioni, con evidenza

| # | Decisione | Verdetto | Evidenza |
|---|---|---|---|
| D1 | Excel/tabellare nel RAG? | **No** — instrada → apri → calcola (soffice/pandas) | answerability: gli aggregati non sono in nessun chunk (toy) |
| D2 | Aggregazione cross-file a scala? | **ETL righe → tabelle Postgres → SQL** | **2550×** più veloce di open-per-query (scale EXP2) |
| D3 | Correlazione documenti? | **Blocking deterministico (cliente, n°fattura) → LLM solo sul residuo** | **44×** riduzione, recall strutturale **100%** (scale EXP4) |
| D4 | Lessicale vs denso | **Fusi, ma la fusione va PESATA / delegata al rerank** | vedi D7 (RRF ingenuo degrada) |
| D5 | Correlazione: grafo vs edge-table | Opzione A: **edge-table** (no AGE). Opzione B: **grafo nativo ArcadeDB** (vedi §3b) | ragionato; correlazione è 1-2 hop |
| D6 | Garage | **Tiered**: `filesystem-dev` per locale/single-op, **Garage per Hetzner/scala** (non rimosso, tierato) | blast-radius + scale reasoning |
| D9 | Sandbox runtime | DockerBackend attuale per locale; **agent-sandbox (k8s) candidato per Hetzner/MUSR** — con due verifiche bloccanti | vedi §7 |
| D7 | Reranker vs retrieval — dove sta il collo di bottiglia? | **Il rerank è già perfetto; investi nella RECALL del retrieval** | LLM-reranker 4/4 su recuperabili; ceiling fissato dall'embedder |
| D8 | Embedder | Un modello **piccolo ma forte** basta (EmbeddingGemma-300M) | ceiling **4/8 → 8/8**, dense@5 **2/8 → 8/8** |
| D10 | Motore per grafo/vettori/FTS | **Opzione B — ArcadeDB** (multi-model, Apache, Bolt+MCP, near-drop-in Neo4j); Postgres resta il control-plane | §3b; deploy locale già su ArcadeDB |
| D11 | Isolamento multi-tenant | **ArcadeDB db-per-identità** (fisico) — collassa RLS + `*Scoped` + bucket in uno | §3b; caveat GHSA-fxc7-fm93-6q77 (fixed) |

## 3b. ArcadeDB cambia il quadro — architetture + isolamento (agg. 2026-08)

ArcadeDB (verificato: multi-model — grafo + documento + kv + **Lucene FTS** +
**vettori HNSW/DiskANN** — openCypher 25, SQL, Gremlin; **Bolt** certificato coi 5
driver Neo4j incl. Go; **Postgres wire**; **MCP server integrato**; Apache-2.0)
apre una terza via e riscrive alcune decisioni.

**Le tre architetture:**

| | A — Solo Postgres | **B — Postgres + ArcadeDB** (racc.) | C — Solo ArcadeDB |
|---|---|---|---|
| Neo4j | assorbito in PG (pgvector+pg_search+edge-table) | **sostituito da ArcadeDB** (near-drop-in Bolt+Cypher+MCP) | sostituito |
| Vettori+BM25+grafo | 3 estensioni PG impilate | **un motore nativo solo** | un motore solo |
| Control-plane (auth/RLS/sqlc/`aura.*`) | Postgres | **Postgres (resta)** | riscritto su ArcadeDB ⚠️ |
| Rischio | stacking estensioni + ParadeDB ops | **basso** (PG intatto, ArcadeDB near-drop-in) | **alto** (rewrite auth) |

**Reframe delle decisioni sotto Opzione B:**
- **D5 (correlazione)** → risolta a favore del **grafo nativo** ArcadeDB (edge Cypher,
  traversal 1-2 hop banale). **Niente AGE, niente join-table.**
- **pg_search/Tantivy e AGE** → **moot**: ArcadeDB ha Lucene FTS (≈ Tantivy) + grafo +
  vettori nativi.
- **Memoria** → **non sei più costretto a flattenare a pgvector**: il grafo costa zero
  extra (stesso motore), quindi puoi tenere POLE+O se rende, e i **fatti temporali
  stile Zep** (validità nel tempo, +22pp su LongMemEval) sono naturali. La pressione
  mem0 "togli il grafo" nasceva dal costo del secondo server (Neo4j) — che con
  ArcadeDB sparisce. Opzioni memoria non-esclusive: grafo/vettori in ArcadeDB +
  **memoria a file Markdown** (stile Anthropic memory tool, trasparente, no vector DB)
  su `/workspace`.
- **D1/D2/D7/D8** invariati (store-agnostici): Excel→open+compute; aggregazione→ETL+SQL
  (in Postgres); rerank già perfetto → investi nella recall; embedder forte → ceiling 8/8.

### Isolamento — db-per-identità (il vantaggio chiave di ArcadeDB)

Un server ArcadeDB ospita **molti database, completamente isolati**: niente catalogo
condiviso, **niente query/join/transazioni cross-database**. Quindi
**database-per-identità = isolamento fisico imposto dal motore**, che **collassa i tre
meccanismi attuali** (Postgres RLS + Neo4j query `*Scoped` con filtro ownership D-13 +
Garage bucket-per-identità) **in uno solo**, ed **elimina la classe di leak** "mi son
scordato il `WHERE owner_id`" (sparisce tutto il codice `*Scoped`). Provisioning = crea
db; **deprovisioning = droppa il db** (cancellazione completa, GDPR-friendly).

Split pulito: **Postgres centrale** (auth, registry identità — inerentemente
cross-identità) + **ArcadeDB db-per-identità** (documenti/chunk/vettori/FTS/grafo/memoria
del singolo utente) + **object store** per i blob.

**Due caveat onesti:**
1. **Niente cross-database** è a doppio taglio: i **dati condivisi** (operatore/`local`,
   catalogo globale) vanno duplicati per-db o mergeati lato Go. OK per il retrieval
   per-utente; da pianificare per le feature globali.
2. **Flag sicurezza**: advisory **GHSA-fxc7-fm93-6q77** (cross-database auth bypass —
   token scoped a un db potevano mutare altri db) — **corretta** (commit `04110c0` +
   test), ma è un segnale sulla maturità di sicurezza: **pinna una versione ≥ fix**,
   tieni defense-in-depth, pesa contro le decadi di Postgres RLS.

**Scala**: ideale per decine/centinaia di identità (single-op → MUSR piccolo);
verificare per decine di migliaia (N db su una JVM).

## 4. Evidenza — i due spike

### Toy (21 file, `document-routing-benchmark/`)
- Routing card-catalog 40% vs chunk-flatten 35% R@1 — **la rappresentazione da
  sola non è la leva**.
- **Answerability** (il risultato che decide D1): "importo medio delle fatture non
  pagate" = 23851.00 € — **in nessun chunk**. Un aggregato derivato esiste solo
  dopo apri+calcola.

### Scale (~460 file + doc ~770 pagine, `document-routing-scale/`)
- **EXP1 routing** (465 file, lessicale): flatten 0.917 vs card 0.903 MRR — la
  rappresentazione lessicale **non è la leva**; il tag di ruolo appeso non aiuta.
- **EXP2 ETL vs open**: `SELECT ... GROUP BY` **0.4 ms/query** vs aprire 400 xlsx
  **1131 ms/query** → **2550×**, costo ETL pagato una volta all'ingest (D2).
- **EXP3 flat vs gerarchico** nel doc grande: flat 7/8 vs gerarchico 6/8 @1 — per
  query **keyword** il flat basta; il problema delle 770 pagine è **context-window**,
  non retrieval quality (autocorrezione, vedi §5).
- **EXP4 blocking**: 79.800 all-pairs → 1.800 per chiave cliente (**44×**), recall
  100%; gli archi nota→fattura richiedono la chiave n°fattura (D3).
- **Semantico (MiniLM-384d)**: dense @5 2/8, **hybrid @5 4/8** (hybrid > singole
  gambe quando entrambe deboli); routing 0.750/0.751/0.762.
- **LLM-reranker** (io, Claude, auditable): ceiling retrieval 4/8; recuperati 4/8
  = **4/4 su ciò che era recuperabile**, **4/4 astensioni corrette**, 0
  allucinazioni → **il rerank è perfetto, il collo di bottiglia è il retrieval** (D7).
- **EmbeddingGemma-300M (GGUF)**: ceiling **4/8 → 8/8**, dense@5 **2/8 → 8/8** →
  un embedder piccolo-ma-forte porta il retrieval a saturazione (D8). Sanity:
  `cos(parafrasi, needle-giusto)=0.681` vs 0.466 (MiniLM lo invertiva).

## 5. Autocorrezioni (honesty ledger)

Diverse conclusioni "small-scale" si sono ribaltate misurando — riportate perché
lo studio vale solo se è onesto:

- ~~"La scheda card-catalog batte il flatten"~~ → **falsificata a scala**: routing
  lessicale ~pari (0.917 vs 0.903). La leva è il **denso forte + filtri
  strutturali**, non la rappresentazione testuale.
- ~~"Nel doc grande il gerarchico batte il flat"~~ → **non riprodotta** per query
  keyword (flat 7/8 ≥ gerarchico 6/8). Il valore della gerarchia è per query
  semantiche/di sintesi, non keyword.
- ~~"L'hybrid vince sempre"~~ → **raffinata**: l'RRF **equal-weight** *peggiora*
  quando una gamba domina (Gemma-dense 8/8 → hybrid 5/8; routing → 0.532). La
  fusione va **pesata**, o il candidate-set (unione, ceiling 8/8) va passato al
  **rerank** invece di usare l'ordine RRF come finale.
- ~~"tsvector basta / brute-force le card / apri l'originale sempre"~~ → premesse
  single-operator che **saltano** con 1000 file + PDF da 700 pagine (context-window,
  ETL, object-store tornano rilevanti).

## 6. Architettura target proposta

```
INGEST   (async, con backpressure)
  prosa            → chunk+overlap → embed (EmbeddingGemma-class) → pgvector + BM25
                   → + summary/scheda per-file (routing L1)
  tabellare omog.  → ETL righe → TABELLE Postgres reali            → SQL
  tabellare eterog.→ scheda schema-fingerprint + puntatore blob    → soffice/pandas open+compute
  correlazione     → blocking (cliente, n°fattura, entità) → regole + LLM sul residuo → doc_edges (tabella)

QUERY
  L1 routing (quale file): card catalog, dense + lessicale, + metadati di ruolo
  L2:
    prosa      → retrieve(UNIONE dense+BM25, ceiling alto) → RERANK(LLM/cross-encoder) → risposta
    tabellare  → SQL (omogeneo) | open+compute soffice/pandas (eterogeneo)
    correlati  → edge-expand 1-2 hop su doc_edges (WITH RECURSIVE)

STORE  Opzione A — Postgres unico: pgvector + pg_search|tsvector + tabelle ETL + doc_edges.
       Opzione B (racc.) — Postgres (control-plane, auth/RLS/sqlc, tabelle ETL)
                         + ArcadeDB (grafo + vettori + Lucene FTS + memoria),
                           un database PER IDENTITÀ (isolamento fisico, §3b).
       Neo4j: rimosso in entrambe. Garage: tierato (D6).
       Object store + Sandbox — TIERED:
         locale/single-op → objectstore=filesystem-dev · sandbox=DockerBackend attuale
         Hetzner/scala    → objectstore=Garage (S3)     · sandbox=agent-sandbox (k8s)
```

**Nota tiering (D6/D9).** Garage NON è rimosso: è il backend object-store del tier
Hetzner (`filesystem-dev` resta per il locale). Il sandbox segue lo stesso schema:
il DockerBackend attuale per il locale, **agent-sandbox** (Kubernetes, Go,
Apache-2.0, E2B-compatibile, storage S3 → si sposa con Garage) come candidato per
Hetzner/MUSR. Garage + agent-sandbox si compongono (lo storage S3 di agent-sandbox
può puntare a Garage).

## 7. Decisioni ancora aperte (di prodotto, non di ricerca)

- **A vs B (il bivio principale)**: solo-Postgres (estensioni impilate) vs
  Postgres+ArcadeDB (multi-model, near-drop-in Neo4j, isolamento db-per-identità).
  Dato che il locale è già su ArcadeDB, B è la traiettoria attuale. Sotto B le due
  voci seguenti diventano **moot**.
- ~~**pg_search vs tsvector**~~ / ~~**AGE vs edge-table**~~: **moot sotto Opzione B**
  (ArcadeDB ha Lucene FTS + grafo nativi). Rilevanti solo se si sceglie l'Opzione A.
- **Memoria agente POLE+O**: sotto A → flat pgvector (mem0-style, il grafo vale ~2pp e
  non giustifica un secondo motore). Sotto B → **il grafo costa zero** (stesso motore),
  quindi tienilo se rende; i fatti temporali (Zep, +22pp) sono naturali in ArcadeDB.
  Ortogonale: **memoria a file Markdown** (Anthropic memory tool) su `/workspace`,
  trasparente e senza vector DB. Vedi §3b.
- **Garage → filesystem vs S3**: risolto come tiering (D6) — filesystem locale,
  Garage su Hetzner.
- **Sandbox (D9) — VERIFICATO (2026-08), esito: NON adottare il repo linkato as-is.**
  Esistono DUE progetti distinti:
  - **`agent-sandbox/agent-sandbox`** (quello linkato): ha l'MCP (`/mcp`, SSE) +
    E2B-compat + 3 endpoint REST (create/get/delete). **Ma il suo `install.yaml`
    NON contiene alcuna NetworkPolicy/egress/iptables/sidecar** → pod con outbound
    ILLIMITATO, incluso il metadata server 169.254.169.254 (furto credenziali/SSRF
    su cloud). Runtime di isolamento **non dichiarato**. → **regressione di
    sicurezza** vs il DockerBackend attuale (che ha egress control load-bearing,
    storia WR-01/CAP_NET_ADMIN). "Ha un MCP → niente da inventare" è mezza verità:
    il plumbing MCP c'è, ma il **confine di sicurezza (egress) è proprio ciò che
    manca** — la parte difficile la scrivi comunque tu.
  - **`kubernetes-sigs/agent-sandbox`** (Kubernetes SIG, 3.4k star, Apache-2.0):
    Sandbox CRD che delega a **runtime sicuri gVisor/Kata via RuntimeClass**
    (isolamento kernel+network), template a isolamento strict di default. **Ma
    nessun MCP** — SDK Go/Python.
  - **Due verifiche restano comunque bloccanti**: (1) richiede Kubernetes (k3s ok)
    → aggiungerlo ha senso solo se Hetzner è già k8s o per il multi-tenant; per il
    single-op il DockerBackend attuale basta e non va rippato. (2) qualunque
    sostituto deve **almeno pareggiare** l'egress control attuale, non regredire.
  - **Strade pulite**: SIG project (sicurezza vera) + facade MCP/E2B sottile;
    oppure il repo linkato **hardened** (RuntimeClass gVisor/Kata + NetworkPolicy
    deny-egress + blocco metadata). Rif. egress-fatto-bene: `mattolson/agent-sandbox`
    (sidecar proxy + iptables).

## 8. Non ancora provato / prossimi passi

- Ri-misurare **routing L1 concettuale** con EmbeddingGemma (fatto per il passage
  recall; il routing card qui era ancora parziale) e con **fusione pesata**.
- **pg_search vs tsvector** e **pgvector HNSW vs pgvectorscale** su volume reale
  (100k–qualche milione di chunk).
- Validare l'**estrazione archi** (regole + LLM) con precision/recall sugli archi
  ground-truth a scala.
- **Coesistenza estensioni** (pgvector + pg_search + eventuale AGE) in un'immagine.

## 9. Riproducibilità

```bash
pip install openpyxl fastembed llama-cpp-python
cd spikes/document-routing-benchmark && python3 generate_corpus.py && python3 benchmark.py
cd ../document-routing-scale && python3 generate_scale_corpus.py && python3 experiments.py
python3 semantic_experiments.py            # gamba densa MiniLM (fastembed)
python3 llm_rerank_prep.py                 # candidati per il rerank LLM (auditable)
python3 llm_rerank_score.py                # score dei pick registrati
# EmbeddingGemma: scarica il GGUF in models/ poi:
python3 embeddinggemma_rerun.py
```
Corpus e modelli non tracciati (rigenerabili/scaricabili) — vedi `spikes/.gitignore`.

## 10. Riferimenti

- **Interni**: ADR `docs/adr/0038-graph-store-license-neo4j-gplv3-vs-arcadedb-apache.md`
  (da riaprire), PRD Amendment #88.2 (Garage/MUSR), #89 (`document_index`),
  #106.2 (XLSX extraction).
- **Esterni**: RAPTOR (recursive summary tree), mem0 (graph ~+2pp su LoCoMo),
  ParadeDB `pg_search` (Tantivy-in-Postgres), Apache AGE (Cypher-in-Postgres,
  Apache-2.0), EmbeddingGemma-300M (Google, 768d multilingue).
- **ArcadeDB**: multi-model Apache-2.0 (grafo+doc+kv+FTS+vettori), HNSW/DiskANN,
  Lucene FTS, openCypher 25, Bolt (v26.2.1, certificato coi driver Neo4j), Postgres
  wire, MCP integrato; **isolamento db-per-server** (no cross-db query); advisory
  **GHSA-fxc7-fm93-6q77** (cross-db auth bypass, fixed `04110c0`).
- **Memoria (comparativa)**: Letta/MemGPT (core/recall/archival, self-edit), LangGraph
  (checkpointer + store namespaced + semantic), Anthropic memory tool (file Markdown),
  Zep/Graphiti (temporal KG, +22pp su LongMemEval vs mem0).
