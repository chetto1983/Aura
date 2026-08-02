# Retrieval = solo Postgres + sandbox — sintesi dello studio

**Stato:** PROPOSTO, evidence-backed — *non* una decisione approvata. Richiede un
PRD-amendment prima di qualsiasi codice di produzione (PRD-first principle), e se
accettato **supererebbe l'ADR 0038** ("keep Neo4j / reject all-Postgres"), che va
riaperto con l'evidenza qui sotto.

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

Obiettivo: **un solo datastore (Postgres) + il sandbox**, eliminando Neo4j (grafo +
vettori) e Garage (object store), e risolvere la pessima resa su Excel.

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
| D5 | Grafo (AGE) vs edge-table | **Edge-table** per 1-2 hop; AGE solo per traversal profondi o il cockpit | ragionato; correlazione è 1-2 hop |
| D6 | Garage | **Tiered**: `filesystem-dev` per locale/single-op, **Garage per Hetzner/scala** (non rimosso, tierato) | blast-radius + scale reasoning |
| D9 | Sandbox runtime | DockerBackend attuale per locale; **agent-sandbox (k8s) candidato per Hetzner/MUSR** — con due verifiche bloccanti | vedi §7 |
| D7 | Reranker vs retrieval — dove sta il collo di bottiglia? | **Il rerank è già perfetto; investi nella RECALL del retrieval** | LLM-reranker 4/4 su recuperabili; ceiling fissato dall'embedder |
| D8 | Embedder | Un modello **piccolo ma forte** basta (EmbeddingGemma-300M) | ceiling **4/8 → 8/8**, dense@5 **2/8 → 8/8** |

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

STORE  Postgres unico: pgvector (denso) + pg_search|tsvector (BM25) + tabelle ETL
       + doc_edges. Neo4j: rimosso.
       Object store (astrazione objectstore mantenuta) + Sandbox — TIERED:
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

- **pg_search (Tantivy) vs tsvector**: meno critico ora — il denso forte porta il
  grosso del recall. tsvector come baseline, pg_search se il lessicale esatto
  (nomi/ID/codici) soffre in produzione. Da misurare A/B sul corpus reale.
- **AGE vs edge-table**: edge-table basta per 1-2 hop. AGE solo se il cockpit graph
  explorer resta una feature difesa o servono traversal profondi. AGE è Apache-2.0
  (scioglie il vincolo licenza dell'ADR 0038) ma **pg_search + AGE nello stesso
  Postgres non è un combo provato** — potresti dover scegliere due estensioni su tre.
- **Memoria agente POLE+O**: mem0 mostra che il grafo memoria vale ~2pp; per
  single-operator probabilmente non vale — memoria flat pgvector (stile mem0). È la
  parte più graph-shaped: decisione separata.
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
