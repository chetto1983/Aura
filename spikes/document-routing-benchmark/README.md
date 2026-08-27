# Spike — Document routing benchmark (card-catalog vs chunk-flatten)

**Domanda sotto test** (la "questione fondamentale"): dato un documento in input
che finisce nell'object store, come lo si **trova/instrada** a query time senza
che l'agente apra ogni file per capire qual è quello giusto?

Corpus deliberatamente **interlinkato** (stile contabilità reale): 14 fatture +
registro contabile che le aggrega + anagrafica clienti + scadenzario + 4 file di
prosa che citano fatture/clienti specifici. I dati sono consistenti (i totali del
registro sono la somma esatta delle fatture) — così la risposta a una query di
aggregazione vive in UN file, derivata dagli altri.

## Cosa confronta

Due **rappresentazioni** dello stesso corpus, cercate con lo **stesso** motore
BM25 (così la differenza è la rappresentazione, non l'algoritmo):

| | rappresentazione | granularità |
|---|---|---|
| **A. chunk_flatten** | replica `docker/markitdown/app.py` `_extract_xlsx`: ogni cella → `"{col}{row}: {value}"`, righe unite, chunk ogni 50 righe/12k char, whitespace collassato | 1 doc BM25 per **chunk** |
| **B. card_catalog** | una "scheda" compatta per file: filename + nomi fogli + valori-testo distinti (label/header/categorici/entità, dedupati) + range numerici | 1 doc BM25 per **file** |

Entrambe includono il **filename** (parità) — così il confronto isola l'effetto
della rappresentazione del *contenuto*, non un regalo del nome file.

## Come si esegue

```bash
pip install openpyxl                       # unica dipendenza
python3 generate_corpus.py --out corpus    # ricrea il corpus locale dello spike
python3 benchmark.py          # stampa metriche + demo (vedi results.txt)
```

Senza `--out`, il generatore ricrea la fixture di release in
`scripts/fixtures/document_retrieval_eval/corpus` e il relativo `corpus.sha256`.
Gli XLSX hanno metadati OOXML e timestamp ZIP normalizzati, quindi la generazione
è byte-per-byte riproducibile. Il benchmark dello spike resta deterministico e
offline (BM25 fatto a mano, nessun modello). `results.txt` è l'output committato.

## Risultati

```
                        R@1        R@3        MRR
chunk_flatten        7/20 (35%)  15/20 (75%)  0.557
card_catalog         8/20 (40%)  15/20 (75%)  0.603
```

Per tipo di query (MRR):

| tipo | flat | card | note |
|---|---|---|---|
| EXACT | 0.511 | 0.583 | pari-e-vince: token forti (numero fattura) |
| AGG | 0.258 | **0.473** | card meglio su "IVA Q2", "fattura più alta Verdi" |
| FILTER | 0.333 | 0.333 | pari |
| AGING | 1.000 | 1.000 | entrambi perfetti |
| CROSSREF | 0.292 | 0.208 | **entrambi male, card peggio** — vedi finding 3 |
| PROSE | 0.833 | 0.833 | pari, entrambi buoni |
| TEMPORAL | 0.333 | 0.333 | pari |

## Le 4 conclusioni (la #1 è quella che conta)

**1. La prova che decide tutto — l'answerability.** La query "importo medio delle
fatture non pagate" ha risposta `23851.00 €`, calcolata sul registro. Quel numero
**non compare in NESSUN chunk** (`present? False`): non è una cella, è un
aggregato derivato. Quindi *anche un router perfetto trova solo il FILE* — il
numero esiste solo dopo **apri + calcola** (soffice/openpyxl). Questo dimostra
empiricamente che il RAG-su-chunk **non può strutturalmente** rispondere a query
computazionali: la strada obbligata per il tabellare è **instrada → apri →
calcola**, non "embedda i chunk". È la validazione dell'idea Excel→agente+Python.

**2. La rappresentazione da sola conta poco (con solo lessicale).** card_catalog
batte chunk_flatten solo di ~5pp su R@1. La scheda non è magia: su un corpus
piccolo con forte overlap lessicale, BM25 su entrambe le rappresentazioni è
mediocre per l'instradamento. Il valore della scheda **non** è "rappresento
meglio", è "abilito apri+calcola" (finding 1) e "do una superficie compatta per
la gamba semantica" (finding 3).

**3. Il lessicale da solo fallisce sulle query concettuali → serve la gamba
vettoriale.** CROSSREF ("termini di pagamento di Verdi") instrada male in
entrambi: "Verdi" appare in decine di file, e il concetto "termini di pagamento"
→ colonna `termini_pagamento_giorni` dell'anagrafica è un match **semantico** che
BM25 non vede (vocabulary mismatch). Questo è l'argomento empirico **a favore
dell'hybrid** (pg_search/tsvector + pgvector), non del solo lessicale. NB: questo
benchmark è lessicale-only (nessun modello disponibile offline), quindi
**sotto-stima** la scheda — il campo compatto `termini_pagamento` matcherebbe un
query-embedding molto meglio di una zuppa di 22 chunk.

**4. Molti file simili affollano l'aggregatore.** "Totale ACME" mette 4 fatture
ACME in competizione con il registro (tutte contengono "ACME"). Argomento per
mettere sulla scheda un **ruolo/tipo di file** (registro/aggregato vs singola
fattura) o un routing intent-aware, così le query di aggregazione preferiscono i
file-registro.

## Cosa dice per le decisioni architetturali

- **Excel fuori dal RAG → agente+Python**: validato (finding 1, indiscutibile).
- **Indice = card-catalog che instrada, non contenuto**: la scheda è il proxy
  economico del blob; la verità è il file, aperto on-demand.
- **Serve hybrid (lessicale + vettoriale)**: finding 3 è la prova; il lessicale
  da solo non instrada le query concettuali. → conferma il bisogno di pgvector
  accanto a pg_search/tsvector.
- **La scheda ha bisogno di metadati di ruolo** (finding 4), non solo di testo.

## Limiti onesti (cosa NON prova questo spike)

- **Lessicale-only**: manca la gamba embedding (nessun modello offline). Il caso
  CROSSREF resta una *previsione motivata*, non una misura — è il prossimo
  esperimento (aggiungere un embedder multilingue e ri-misurare card+vettore).
- **N piccolo** (20 query, 21 file): i numeri sono direzionali, non statistici.
- **Estrazione entità dizionario-free** (solo regex fatture): un sistema reale con
  NER/summary-LLM sulla scheda farebbe meglio → i risultati card sono un *lower
  bound*.
- **Nessun reranker**: in produzione c'è un rerank stage sopra il seed.
