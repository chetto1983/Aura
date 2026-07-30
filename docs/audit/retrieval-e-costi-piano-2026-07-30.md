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
