# Spike — Document retrieval at ENTERPRISE SCALE

Follow-up al toy spike (`../document-routing-benchmark`, 21 file). Qui si simula
il regime che l'operatore ha posto: **~460 file + un documento da ~770 "pagine"**.
Quattro esperimenti misurano ciò che il giocattolo non poteva. Alcuni risultati
**contraddicono la narrativa di scala che avevo dato** — riportati comunque, per
intero. Un benchmark serve a questo.

## Come si esegue

```bash
pip install openpyxl                 # unica dipendenza (sqlite3 è stdlib)
python3 generate_scale_corpus.py     # ~460 file + contratto ~770 pagine
python3 experiments.py               # 4 esperimenti (vedi results_scale.txt)
```

Deterministico, offline, BM25 fatto a mano. `corpus/` è gitignored (rigenerabile).

## Corpus

400 fatture (40 clienti) + 20 con nomi "sporchi" (`_v2`, `_FINALE`, `(copia)`) +
registro contabile + 3 report + 40 note che citano fatture + `contratto_quadro.txt`
(140 articoli, ~770 pagine, 8 "needle" piantati in articoli specifici). Ground
truth di archi e needle in `corpus/*.json`.

## Risultati (da `results_scale.txt`)

### EXP 1 — routing precision su 465 file (MRR) — *risultato NULLO, onesto*
```
                 AGG    BIGDOC  EXACT  NOTE   REPORT  OVERALL
flatten          0.501  1.000   1.000  1.000  1.000   0.917
card             0.419  1.000   1.000  1.000  1.000   0.903
card+role        0.419  1.000   1.000  1.000  1.000   0.903
```
**A scala, la rappresentazione (flatten vs card) NON è la leva del routing.**
Anzi flatten è leggermente meglio (0.917 vs 0.903). EXACT/NOTE/BIGDOC/REPORT sono
banali per tutti (token forti). L'unico tipo difficile è **AGG**, e lì vince
flatten. Il mio tag di ruolo appeso alla scheda (`card+role`) **non ha aiutato**
(identico a `card`): appendere prosa di ruolo non muove il ranking BM25. → La leva
vera è (a) la **gamba semantica** (embedding, non testabile offline qui) e (b) i
metadati strutturali usati come **filtro/boost**, non come testo appeso.

### EXP 2 — aggregazione cross-file: open+compute vs ETL+SQL — *conferma FORTE*
```
domanda: 'fatturato totale per cliente' su 400 fatture
risultati identici: True
open+compute (per query):   1131 ms   (ri-legge 400 file OGNI query)
ETL build (una volta, ingest): 1189 ms
SQL query (per query):         0.4 ms   (2550x più veloce)
```
**A scala l'aggregazione DEVE essere ETL→SQL, non aprire-N-file-per-query.** Il
costo di aprire 400 xlsx è ~1.1s *per ogni query*; la stessa risposta in SQL è
0.4ms, col costo di estrazione pagato **una volta** all'ingest. Questo è il
risultato più azionabile: lo strutturato-omogeneo va estratto in tabelle Postgres.

### EXP 3 — passaggio nel doc da ~770 pagine: flat vs gerarchico — *ridimensiona la mia tesi*
```
1820 chunk flat vs 140 sezioni
FLAT (intero doc):          recall@1 7/8   recall@5 7/8
GERARCHICO (sez->chunk):    recall@1 6/8   recall@5 7/8
```
**Avevo sostenuto che il flat degrada e il gerarchico regge. NON si riproduce.**
BM25 flat su 1820 chunk trova bene il needle anche in 770 pagine (i termini del
needle sono distintivi); il gerarchico ne ha *perso uno* instradando la query alla
sezione sbagliata — ha aggiunto un punto di fallimento senza servire. Correzione
onesta: **il problema delle 770 pagine è di context-window** (non le carichi
intere), *non* di qualità del retrieval per query a keyword — lì il flat basta. La
gerarchia/RAPTOR paga per query **semantiche** e di **sintesi cross-sezione**, che
questo harness lessicale non può testare. Ho sovra-venduto la gerarchia.

### EXP 4 — correlazione: blocking prima dell'LLM — *conferma FORTE*
```
400 doc   all-pairs O(N^2): 79800
blocking per CLIENTE -> 1800 coppie (44x riduzione), recall archi stesso-cliente 100%
archi nota->fattura: 0/40 con chiave cliente (chiave sbagliata), 40/40 con chiave numero-fattura
```
**La correlazione a scala NON si fa LLM-ando O(N²) coppie.** Due chiavi di blocking
deterministiche (cliente, numero-fattura) catturano gli archi strutturali a una
frazione delle coppie; l'LLM vede solo il residuo semantico.

## Conclusioni corrette (dopo l'autocorrezione)

| Affermazione (turno scorso) | Verdetto dello spike |
|---|---|
| Aggregazione a scala → ETL+SQL | **CONFERMATA** (2550x) |
| Correlazione → blocking, non O(N²) | **CONFERMATA** (44x, recall 100% strutturale) |
| Routing: la scheda batte il flatten | **FALSIFICATA a scala** — rappresentazione lessicale non è la leva; serve la gamba semantica + filtri strutturali |
| Doc grande: gerarchico batte flat | **NON riprodotta** — il problema è context-window, non retrieval keyword; la gerarchia paga solo per query semantiche/sintesi (non testabili qui) |

## Cosa questo spike NON prova (e serve un modello per farlo)

Entrambe le sorprese negative (routing, gerarchico) puntano allo **stesso buco**:
tutto qui è **lessicale** (BM25), nessun embedding offline. La gamba semantica —
dove card-catalog e gerarchia *dovrebbero* vincere (query concettuali, paraphrase,
sintesi cross-sezione) — resta non misurata. Prossimo esperimento reale: montare
un embedder multilingue e ri-fare EXP1 (routing concettuale) ed EXP3 (recall
semantico nel doc grande) con la gamba vettoriale. Solo allora si può dire se
Tantivy-BM25 basta o se il valore è tutto nel denso.

I due risultati **forti (ETL+SQL, blocking) sono già azionabili** e reggono a ogni
scala: sono aritmetica, non euristica.

## Gamba semantica (chiude il buco) — `semantic_experiments.py`

Montato un embedder multilingue ONNX (`paraphrase-multilingual-MiniLM-L12-v2`,
384d, via fastembed — niente torch) e ri-misurato con query **parafrasate** (basso
overlap lessicale), più un **hybrid RRF** (BM25 + denso). Da `results_semantic.txt`:

```
SEM-A  passage recall nel doc grande, 8 query PARAFRASATE
  BM25-flat     recall@1 2/8   recall@5 3/8    <- il lessicale crolla sulle parafrasi
  DENSE-flat    recall@1 2/8   recall@5 2/8    <- il denso PICCOLO non lo salva
  HYBRID-flat   recall@1 3/8   recall@5 4/8    <- l'hybrid è il migliore
  DENSE-hier    recall@1 2/8   recall@5 2/8

SEM-B  routing su 4 query CONCETTUALI (card catalog)
  BM25-card     MRR 0.750
  DENSE-card    MRR 0.751     <- denso piccolo ~ pari a BM25
  HYBRID-card   MRR 0.762     <- hybrid marginalmente meglio
```

**Conclusioni (oneste):**
1. **L'hybrid (BM25 + denso, RRF) ≥ ogni gamba singola in entrambi i test.** È la
   configurazione robusta: non "lessicale O denso", ma **fusi**. → conferma
   pg_search + pgvector *insieme*, non in alternativa.
2. **Un embedder PICCOLO (384d) NON ribalta le parafrasi**: DENSE-flat era pari o
   peggio di BM25 su SEM-A. Il salto semantico atteso richiede un modello forte —
   il contratto reale di Aura è **e5-large 1024d**, molto più capace, **non
   testato qui** (troppo pesante per l'ambiente). Quindi il risultato denso qui è
   un *lower bound*, non un verdetto sul denso in generale.
3. **Le parafrasi sono dure**: anche l'hybrid fa 4/8@5 vs 7/8 delle query
   lessicali. I veri utenti parafrasano → serve (a) hybrid, (b) embedder forte,
   (c) un **reranker** (Aura ce l'ha; questo spike lo omette) per ripulire i
   candidati fusi.

### Cosa resterebbe da misurare
Ri-fare SEM-A/SEM-B con **e5-large-1024** (il modello reale) + un reranker, per
separare "il denso non aiuta" da "il denso *piccolo* non aiuta". È l'unico modo
per decidere quanto peso dare alla gamba densa vs Tantivy-BM25 nel design finale.

## LLM come reranker — separa retrieval da rerank

"Tu sei un LLM": invece di scaricare e5-large, uso l'LLM in-the-loop (Claude) come
**reranker** sopra i candidati hybrid top-12, per capire se il gap delle parafrasi
è dello *stage di rerank* o dello *stage di retrieval*. Auditable:
`llm_rerank_prep.py` dumpa i top-12 senza marcatore della risposta →
`llm_rerank_candidates.json`; l'LLM scrive le scelte (con motivazione) in
`llm_rerank_picks.json`; `llm_rerank_score.py` confronta col ground truth. Da
`results_llm_rerank.txt`:

```
candidate-recall ceiling (retrieval):  4/8   <- quante needle il retrieval ha messo nel top-12
LLM-rerank recovered:                  4/8
LLM precision on recoverable:          4/4   <- PERFETTO su ciò che era recuperabile
correct abstentions (needle absent):   4/4   <- astensione su tutte le assenti, zero allucinazioni
wrong picks:                           0
```

**Conclusione (la più importante del filo semantico): il reranker LLM è perfetto;
il collo di bottiglia è il RETRIEVAL.** L'LLM ha recuperato ogni needle che il
retrieval aveva fatto emergere (4/4) e si è astenuto correttamente su ogni needle
che il retrieval NON aveva fatto emergere (4/4, nessuna invenzione). Il tetto 4/8
è fissato dal retrieval (embedder piccolo), non dal reranker. Un reranker forte
**non può recuperare ciò che il retrieval non ha mai portato su.**

→ **Dove investire è chiaro: la RECALL del retrieval** (embedder forte e5-large-1024,
hybrid meglio tarato, K più grande), non il reranker — che qui è già perfetto. Questo
risponde alla domanda aperta "quanto peso a pgvector vs Tantivy vs reranker":
l'anello debole misurato è la **gamba densa piccola**, non lo stage di rerank.
