# Superficie di memoria di Aura — design

Data: 2026-07-28
Stato: **parzialmente attuato** — vedi §0. Il resto resta proposta, pre-PRD-amendment.
Input: [`HANDOFF-2026-07-28-memory-surface.md`](../../audit/HANDOFF-2026-07-28-memory-surface.md),
ADR [`compaction-spike-2026-07-20.md`](../../audit/compaction-spike-2026-07-20.md) §6 Layer C
Sostrato: fork `neo4j-agent-memory` 2.14.7 vendored in `docker/agent-memory/`, installato `-e` da `src/`

Ogni numero in questo documento è **misurato** sullo stack vivo il 2026-07-28, non stimato.
Il metodo di misura è in §9 perché sia riproducibile.

---

## 0. Stato di attuazione (aggiornato a fine giornata 2026-07-28)

La sessione è passata dal design all'implementazione su richiesta dell'operatore. Cosa è
**in produzione**, cosa è **nel codice non rilasciato**, e cosa è rimasto proposta.

### Attuato e verificato sullo stack

| | esito |
|---|---|
| **Embedder → Qwen3-Embedding-0.6B** | ampiezza della banda da 0.16 a 0.49; separazione in recupero da 0.01 a 0.14 (§2.6) |
| **Troncamento MRL a 768** | `truncateMRL` in Go + `_fit` nell'adapter del fork, con test |
| **Self-test di boot MRL-aware** | accetta un sidecar più largo, rifiuta uno più stretto (§0.2) |
| **Superficie: 6 tool al modello** | i 13 restano sul server per Aura (§0.1) |
| **`memory_forget` esteso a `relationship`** | l'unico modo di rompere un `SAME_AS` sbagliato; 5 casi provati, iniezione Cypher rifiutata |
| **`memory_search` con bucket `facts`** | soggetto esatto poi semantico; un bucket sconosciuto ora è un errore, non `{}` |
| **Tool differiti: l'uso promuove** | §0.3 |
| **Skill `memory-aura`** | builtin embedded, il playbook dei tre verbi |

### Rifiutato dopo averlo costruito

Il **pipeline di estrazione** (spaCy italiano + GLiNER + GLiREL) è stato costruito,
misurato e **rimosso**. Le ragioni, tutte misurate:

- l'estrazione gira **solo** dentro `short_term.add_message`; con la superficie
  long-term-only quel percorso non viene mai raggiunto — costo 20 GB di immagine per
  codice mai eseguito;
- sullo stesso testo il pipeline completo era **3,5× più lento** dell'LLM da solo
  (45,9 s contro 13,0 s) e produceva **un'entità in più: un duplicato** (`Aura` come
  `OBJECT` e come `ORGANIZATION`), che la dedup type-strict non riconcilierà mai;
- `glirel` 1.2.1 è abbandonato e costringe tutta l'immagine a `huggingface_hub<1.0` +
  `transformers<5.0`.

Restano nel fork la rimappatura WikiNER (`PER`→`PERSON`, senza cui ogni persona non
inglese entra come `OBJECT`) e `rapidfuzz`, che costa nulla e resuscita la gamba fuzzy.
Il `Dockerfile` documenta cosa rimettere se l'alimentazione automatica venisse riaperta.

### 0.1 «Hide, never remove»

La prima attuazione **cancellò** i tool dal server. Ha rotto la produzione: Aura chiama
`memory_get_facts`, `memory_add_entity` e `memory_get_context` **direttamente**, via
`mcp.Transport.CallTool`, per l'onboarding e il recall — e quelle chiamate non passano
dal registry. Risultato: `Unknown tool: memory_get_facts`, onboarding in errore, 502.

La superficie MCP ha **due consumatori**: il modello e il codice Go di Aura. Il filtro
appartiene quindi al bridge (`internal/agent/mcptools/bridge.go`, `hiddenFromModel`),
non al server. Due test lo bloccano, incluso che il nascondere sia scoped al namespace.

### 0.2 Un vettore della larghezza sbagliata sparisce in silenzio

Provato di proposito: un `:Fact` scritto con embedding 1024 in un indice 768 viene
**accettato** da Neo4j, il nodo esiste, e resta **fuori dall'indice** — invisibile per
sempre, senza errore. In lettura invece l'errore c'è
(`Index query vector has 1024 dimensions, but indexed vectors have 768`).

Asimmetria da ricordare: la corruzione si accumula muta e si manifesta solo dopo. Per
questo il self-test di boot resta fatale sul lato stretto. Otto nodi reali (l'onboarding
dell'operatore) sono stati scritti in quella finestra e ri-embeddati a 768.

### 0.3 Tool differiti: l'uso promuove, la ricerca no

`deriveActivated` ricostruiva il grant da **ogni** `tool_search` mai visto, quindi
l'insieme promosso cresceva in modo monotòno: 33 tool, poi 56, ciascuno con lo schema
completo nel manifest a ogni turno.

Misurato su questo deployment: un tool MCP costa in media **368 token**; 56 tool sono
**~20.600 token di manifest per turno**, oltre la soglia in cui l'accuratezza di scelta
collassa ([TSI](https://arxiv.org/pdf/2605.24660): a 100+ tool scende al 13%). È questo
«l'agente peggiora più si va avanti».

Politica adottata, la stessa di [`pi`](https://github.com/earendil-works/pi)
(`splitDeferredTools`): **un tool effettivamente invocato resta promosso, uno soltanto
cercato no**, con esenzione per l'ultima ricerca — senza la quale il modello va in
livelock (cerca, il turno finisce, il grant sparisce, ricerca).

Non adottato, e perché: [`ProviderToolSearchMiddleware`](https://reference.langchain.com/python/langchain/agents/middleware/provider_tool_search/ProviderToolSearchMiddleware)
di LangChain delega al tool search **nativo del provider**, disponibile solo su
Anthropic/OpenAI — Aura è su OpenRouter + llama.cpp per vincolo dell'ADR. Il
`tool_reference` di `pi` ha lo stesso limite. La *politica* invece è provider-agnostica.

Restano aperti: il tetto 5-10 con sfratto per rilevanza
([MemTool](https://arxiv.org/pdf/2507.21428) misura che la rilevanza batte recency e
frequenza) e la riduzione del nucleo sempre-caricato da 11 a 3-5. Il minimo reale non è
1: `text_response` è come il loop termina, differirlo significa un agente che non può
rispondere senza prima cercare il permesso di rispondere.

---

## 1. Problema

Il grafo di memoria di Aura si sporca da solo e non ha modo di ripulirsi.

Tre sintomi osservati in produzione:

1. **Entità distinte collassano l'una nell'altra.** `Andrea Piemarino` (PERSON) porta
   `canonical_name: "Davide"`; `Aura GitHub repository` (OBJECT:REPOSITORY) porta
   `canonical_name: "Test Entity"`; `ZTestEntityDel9` idem. Il `display_name` preferisce
   `canonical_name`, quindi il grafo *mostra* il nome sbagliato.
2. **Regole contraddittorie convivono a `confidence: 1.0`.** `:Preference bad4437d`
   («i deferred non persistono tra turni») è falsa dal commit `5523ad01`, ma
   `:Fact ddb3efe2` afferma il contrario, e nessuna delle due è correggibile
   ri-aggiungendo: `add_*` deduplica e mergia sul match più vicino *senza cambiarne il testo*.
3. **La superficie non riflette l'uso.** 12 tool `memory__*` montati non-deferred,
   25 chiamate storiche su tutto il DB, 5 tool mai chiamati.

Il primo sintomo ha una causa meccanica che questo design chiude. Gli altri due sono
questioni di superficie e di percorso di correzione.

---

## 2. Diagnosi misurata — perché i duplicati nascono

Il pipeline documentato è: **spaCy → GLiNER2 → LLM fallback → Merge Strategy →
Deduplication (fuzzy + semantic) → Enrichment → Extracted Entities**.
La deduplication non è nel pipeline di estrazione — `extraction/pipeline.py:188`
deduplica solo per chiave esatta `normalized_name::type` fra gli stadi. Il box
«fuzzy + semantic» del diagramma è la dedup **allo storage**, in
`memory/long_term.py:add_entity` → `CompositeResolver.resolve` +
`_check_for_duplicates`.

### 2.1 Nel container vivo mancano tre quarti del pipeline

L'immagine installa `pip install -e ".[mcp,google,openai]"` (`Dockerfile:57`).
Verificato dentro `aura-agent-memory-mcp`:

| pacchetto | stato | conseguenza |
|---|---|---|
| `spacy` | **MISSING** | Stage 1 non gira |
| `gliner` | **MISSING** | Stage 2 non gira |
| `glirel` | **MISSING** | estrazione relazioni assente |
| `rapidfuzz` | **MISSING** | gamba **fuzzy** di resolver e dedup morta |
| `sentence_transformers` | MISSING | non usato (embedder è remoto) |

**Non è un problema di configurazione: è di pacchettizzazione.** Verificato costruendo
`MemorySettings()` dentro il container vivo:

```
extraction.extractor_type = ExtractorType.PIPELINE     <- gia' il default
extraction.merge_strategy = MergeStrategy.CONFIDENCE
schema_config.model       = SchemaModel.POLEO
```

`extractor_type` è **già** `PIPELINE` (`config/settings.py:188-190`), `enable_spacy` ed
`enable_gliner` sono **già** `true` (`:193-194`), lo schema è **già** POLE+O. `NAM_LLM`
configura il *provider* LLM, non il tipo di estrattore. Aura non ha mai scelto
"solo LLM": ha chiesto il pipeline completo e ne ha ottenuto una corsia, perché le
altre due non sono installate.

Provato eseguendo il pipeline nel container vivo:

```
WARNING pipeline: Stage 'SpacyEntityExtractor' failed: spaCy is required...
WARNING pipeline: Stage 'GLiNEREntityExtractor' failed: GLiNER is required...
fallback_on_error = True        entita finali: 0
```

Gli stadi **non vengono saltati**: vengono costruiti, falliscono a `extract()`, e
`fallback_on_error=True` li degrada a `warning`. L'estrazione prosegue con ciò che resta.

Il fallimento della risoluzione è **silenzioso** per costruzione:
`CompositeResolver.__init__` fa `try: FuzzyMatchResolver(...) except Exception: pass`
(`resolution/composite.py:60-64`) e lascia `_fuzzy_resolver = None`;
`_check_for_duplicates` fa `try: from rapidfuzz import fuzz ... except ImportError: pass`
(`memory/long_term.py:1717-1738`). La catena documentata *Exact → Fuzzy → Semantic*
gira come **Exact → Semantic**.

### 2.2 L'unico discriminante rimasto è sotto il rumore dell'embedder

`CompositeResolver` ha `semantic_threshold = 0.80` (`config/settings.py:262-264`).
Misurato sul sidecar `aura-llama-embed`
(`granite-embedding-311m-multilingual-r2`, 768d), coseno fra **nomi di entità** —
la stringa esatta che quel confronto vede — su 28 coppie non correlate:

```
banda misurata:   0.7312 ──────────────────────── 0.8904
soglia resolver:                    0.80 ▲
                                         └── dentro il rumore
6 coppie non correlate su 28 superano la soglia
```

I tre collassi osservati sono **predetti esattamente**:

| coppia | coseno | `canonical_name` osservato |
|---|---|---|
| `Test Entity` ↔ `ZTestEntityDel9` | **0.8904** | → `Test Entity` |
| `Davide` ↔ `Andrea Piemarino` | **0.8645** | → `Davide` |
| `Aura GitHub repository` ↔ `Test Entity` | **0.8002** | → `Test Entity` |

L'ultimo passa per 2 decimillesimi. La catena causale è chiusa.

`type_strict` è l'unica cosa che impedisce un disastro maggiore: confronta solo
entità dello stesso tipo. Questo rende §2.3 particolarmente grave.

**La corruzione viene dal resolver, non dal deduplicatore.** Sono due stadi distinti
e vanno attribuiti con precisione:

- `DeduplicationConfig` (`memory/long_term.py:44-49`) ha `auto_merge_threshold = 0.95`
  e `flag_threshold = 0.85`. Tutte e tre le coppie misurate stanno **sotto 0.95**:
  nessuna è stata auto-fusa. Le due sopra 0.85 sono state *flaggate* — ed è da lì che
  vengono i `SAME_AS {status: "pending"}` (`long_term.py:714`).
- Il `canonical_name` sbagliato lo scrive il **resolver**, a monte e
  indipendentemente: `canonical_name = resolved.canonical_name`
  (`long_term.py:565-572`), con `semantic_threshold = 0.80`.

Quindi: i `SAME_AS` pending sono il deduplicatore che fa il suo lavoro (segnala e
aspetta una revisione che nessuno ha mai potuto fare); i `canonical_name` corrotti
sono il resolver che decide da solo sotto la soglia sbagliata.

### 2.3 Puntare spaCy all'italiano, da solo, peggiorerebbe le cose

`it_core_news_sm` 3.8.0 emette il set **WikiNER**: `['LOC', 'MISC', 'ORG', 'PER']`
(letto da `spacy-models/meta/it_core_news_sm-3.8.0.json`).

`SpacyEntityExtractor.DEFAULT_TYPE_MAPPING` (`extraction/spacy_extractor.py:49-75`)
è tarato su **OntoNotes**, il set dei modelli inglesi: `PERSON`, `ORG`, `GPE`,
`NORP`, `FAC`, `WORK_OF_ART`, `LAW`…

| label italiana | mappata? | esito |
|---|---|---|
| `LOC` | sì | → `LOCATION` |
| `ORG` | sì | → `ORGANIZATION` |
| **`PER`** | **no** | → **`OBJECT`** |
| **`MISC`** | **no** | → **`OBJECT`** |

Il default per un'etichetta non mappata non è «scarta», è `OBJECT`:
`self.type_mapping.get(ent.label_, "OBJECT")` (`spacy_extractor.py:154`).
Ogni **persona** italiana entrerebbe nel grafo come oggetto — e finirebbe nel pool
type-strict dove vive `Test Entity`, alimentando altri collassi §2.2.

Cambiare `spacy_model` senza rimappare è quindi una regressione, non una migliorìa.

### 2.4 Gli stadi non si scelgono: girano tutti

Il nome «LLM **Fallback**» del diagramma descrive il *ruolo* dello stadio (ultimo della
catena, massima accuratezza), non un'invocazione condizionale. Nel codice:

```python
merge_strategy: MergeStrategy = MergeStrategy.CONFIDENCE   # pipeline.py:378
stop_on_success: bool = False                              # pipeline.py:379
```

`extract_with_details` itera **tutti** gli stadi e interrompe presto solo
`if self.stop_on_success or self.merge_strategy == MergeStrategy.FIRST_SUCCESS`
(`pipeline.py:487-492`). Con i default, **ogni estrazione esegue spaCy, GLiNER e
l'LLM**, e la merge `CONFIDENCE` tiene, per chiave `normalized_name::type`, l'entità
con confidence più alta.

Questo cancella una conclusione che avevo tratto per prima: accendere il pipeline
**non toglie OpenRouter dal percorso di scrittura della memoria**. Lo Stage 3 continua
a girare a ogni turno. Il beneficio del pipeline è la **qualità** — tre estrattori che
votano invece di uno — non il costo né l'indipendenza dal provider.

### 2.5 GLiREL è esportato ma non è cablato da nessuno

`GLiRELExtractor` e `GLiRELConfig` esistono (`extraction/gliner_extractor.py:853-870`,
modello di default `jackboyla/glirel-large-v0`) e sono esportati in
`extraction/__init__.py:92-93`. Ma:

- **nessun chiamante interno**: né la factory né il pipeline costruiscono mai
  `GLiRELExtractor`; `is_glirel_available()` è definito e non lo invoca nessuno;
- **nessun extra `glirel`** in `pyproject.toml` — a differenza di `spacy` e `gliner`,
  che hanno il proprio (`pyproject.toml:71-75`).

Sono classi per l'utente della libreria, non uno stadio del pipeline. Abilitare GLiREL
è quindi **pacchettizzazione + cablaggio**, non un flag.

Oggi le relazioni le estrae solo lo Stage 3: spaCy dichiara di non farlo
(«spaCy does not extract relations or preferences», `spacy_extractor.py:44-45`) e
GLiNER da solo fa NER, non relazioni.

### 2.6 La causa vera è l'embedder, e si cambia

Le soglie sono un sintomo. Il problema è che `granite-embedding-311m-multilingual-r2`
non ha dinamica sui nomi brevi: comprime 28 coppie non correlate in una banda larga
0.16, e qualunque soglia scelta dentro quella banda è arbitraria.

Due candidati misurati sullo **stesso identico corpus** di nomi reali del grafo:

| | granite-311m | EmbeddingGemma-300m | **Qwen3-Embedding-0.6B** |
|---|---|---|---|
| banda su 28 coppie non correlate | 0.7312 – 0.8904 | 0.5043 – 0.7496 | 0.3467 – 0.8355 |
| **ampiezza (potere discriminante)** | 0.1592 | 0.2453 | **0.4857** |
| coppie ≥ 0.80 | 6 / 28 | 0 / 28 | 1 / 28 |
| `Test Entity` ↔ `ZTestEntityDel9` | 0.8904 | 0.7496 | 0.6503 |
| `Davide` ↔ `Andrea Piemarino` | 0.8645 | 0.7320 | 0.8355 |
| `Aura GitHub repository` ↔ `Test Entity` | 0.8002 | 0.5570 | **0.4050** |

**Scelto Qwen3**, e non per la colonna «≥ 0.80» — dove Gemma vince — ma per l'ampiezza.
Gemma abbassa tutto; Qwen3 *separa*. La differenza si vede dove conta davvero, cioè in
**recupero**, sullo stesso caso che con granite dava 0.90 al pertinente e 0.89 al non
pertinente:

| | separazione pertinente / non pertinente |
|---|---|
| granite | **0.0100** |
| Qwen3 embedding (troncato a 768) | **0.1363** |
| Qwen3 + rerank cross-encoder | 0.96593 contro 0.00008 |

L'unica coppia sopra 0.80 (`Davide`/`Andrea Piemarino`, 0.8355) sono due nomi di persona
italiani: l'embedder ha ragione a dirli *simili*, ed è coperta dalla soglia documentata
0.9 di §2.6b. Ed è della **stessa famiglia** del reranker `Qwen3-Reranker-0.6B` già in
stack, quindi embedding e giudizio condividono tokenizer e training.

Tre dettagli che viaggiano con il modello e che sbagliati lo degradano in silenzio:

- **pooling**: è una proprietà del modello, non una scelta di casa. Era cablato `cls` in
  `compose.yaml`; ora è `${AURA_EMBED_POOLING:-...}` e sta in `.env` accanto al modello.
- **larghezza**: Qwen3 emette 1024 nativi contro i 768 dell'indice, e **llama.cpp ignora
  il parametro `dimensions`** — verificato: chiedendo 256 restituisce la larghezza nativa
  invariata. Il troncamento Matryoshka è quindi client-side, in `truncateMRL`
  (`internal/documents/embedder.go`) e nel `_fit` dell'adapter del fork. Misurato che la
  banda tiene troncando: 1024 → 0.4891, 768 → 0.4857, 512 → 0.4828.
- **prefissi di task**: Qwen3 è asimmetrico e documenta prefissi distinti per query e
  documenti; il fork embedda la stringa nuda. I numeri qui sopra sono **senza** prefissi,
  quindi sono un limite inferiore → OQ8.

### 2.6b La configurazione documentata da Neo4j Labs, come rinforzo

Questa è la scoperta che riduce il design. `configuration.adoc` upstream raccomanda:

```bash
NAM_RESOLUTION__SEMANTIC_THRESHOLD=0.9      # il codice ha default 0.8
NAM_DEDUPLICATION__EMBEDDING_THRESHOLD=0.92
NAM_EXTRACTION__EXTRACTOR_TYPE=pipeline
NAM_SCHEMA_CONFIG__MODEL=poleo
```

**Aura non ne imposta nessuna**: il servizio `aura-agent-memory-mcp` in `compose.yaml`
passa solo `NEO4J_*`, `NAM_BACKEND`, `NAM_LLM`, `NAM_EMBEDDING`, `OPENAI_*`,
`AURA_RERANK_*`. Gira quindi sui default del codice, che **divergono** dai valori
raccomandati proprio sul parametro che rompe.

Il tetto di rumore misurato è **0.8904**. La soglia documentata è **0.9**.

| coppia | coseno | con 0.80 (attuale) | con 0.90 (documentata) |
|---|---|---|---|
| `Test Entity` ↔ `ZTestEntityDel9` | 0.8904 | collassa | **rifiutata** |
| `Davide` ↔ `Andrea Piemarino` | 0.8645 | collassa | **rifiutata** |
| `Aura GitHub repository` ↔ `Test Entity` | 0.8002 | collassa | **rifiutata** |

Verificato che la variabile sia davvero onorata, non solo documentata:
`NAM_RESOLUTION__SEMANTIC_THRESHOLD=0.9` → `s.resolution.semantic_threshold = 0.9`.

**Correzione a una mia conclusione precedente.** Avevo proposto di spostare il
giudizio di identità sul cross-encoder `aura-rerank` (misurato: separa 245×–2700× sullo
stesso corpus, e su `Andrea Piemarino` dice correttamente *nessun match*). È
un'architettura inventata sopra un problema che il laboratorio ha già risolto con un
numero. **Non entra nel design.** Resta annotata qui come materiale per OQ3 se, con un
corpus molto più grande, 0.9 dovesse smettere di bastare.

### 2.7 Le env var di deduplication documentate sono morte in questo fork

`configuration.adoc` documenta `NAM_DEDUPLICATION__ENABLED`, `__EMBEDDING_THRESHOLD`,
`__SAME_AS_THRESHOLD` ecc. Ma in questo fork:

- `MemorySettings` **non ha un campo `deduplication`** (verificato:
  `hasattr(s, 'deduplication') == False`); i campi annidati sono `schema_config`,
  `extraction`, `resolution`, `search` (`config/settings.py:608-612`);
- `DeduplicationConfig` è una `@dataclass` in `memory/long_term.py:24`, e
  **nessun chiamante le passa mai `deduplication=`** — è sempre il default.

Quelle variabili non hanno effetto. Divergenza doc/implementazione: da segnalare
upstream, e soprattutto **da non usare come leva** in questo design. Le soglie di
dedup (0.95 / 0.85) si toccano solo dal codice — e §2.2 mostra che non serve toccarle.

---

### 2.8 `get_context` perde il filtro di sessione, e il recupero non distingue

Esperimento controllato sul server vivo, `user_identifier` dedicato, ripulito dopo
(verificato: 18 `Message` e 28 `Entity`, come prima, zero residui). Due sessioni con
contenuto disgiunto: `convA` due messaggi su Kubernetes, `convB` uno su backup su nastro.

Passando un `session_id` esplicito, con `--session-strategy persistent` invariato:

| chiamata | isolamento |
|---|---|
| `memory_get_conversation(convA)` | **corretto** — 2 messaggi, solo ALPHA |
| `memory_search(session_id=convA)` | **corretto** — solo ALPHA |
| `memory_get_context(session_id=convA)` | **perde** |

Il testo restituito localizza la falla con precisione:

```
### Recent Conversation          <- corretto, solo ALPHA
**user**: ...ALPHA... Kubernetes
**user**: Sempre in ALPHA...

### Relevant Past Messages       <- PERDE
- [user] ...ALPHA...                          (relevance: 0.90)
- [user] Sempre in ALPHA...                   (relevance: 0.90)
- [user] ...BETA... backup su nastro          (relevance: 0.89)   <- altra sessione
```

Due difetti distinti in una risposta:

1. **Il filtro non si propaga.** `session_id` scopa la metà "Recent Conversation" ma
   non la metà semantica, benché `memory_search` lo onori sulla stessa ricerca.
2. **Il recupero non discrimina.** I due messaggi *letteralmente della conversazione
   richiesta* prendono 0.90; un messaggio di un'altra conversazione, su un argomento
   senza alcun rapporto, prende **0.89**. Un centesimo. Non è imprecisione
   occasionale: su questo embedder la funzione di punteggio non separa.

Il secondo punto è il gate di qualunque architettura «finestra corta + recupero»:
finché il pertinente e il non pertinente distano 0.01, il recupero non può reggere il
peso del contesto. Il cross-encoder `aura-rerank` — già in stack, e scartato in §2.6
per la dedup perché lì bastava un numero — è il candidato naturale **qui**.

### 2.9a Due fallimenti silenziosi trovati montando il pipeline

Nessuno dei due dà errore, ed è il motivo per cui vanno scritti qui.

**Un vettore di larghezza sbagliata non viene rifiutato da Neo4j.** Provato di
proposito: un `:Fact` scritto con embedding da 1024 in un indice da 768 viene
accettato, il nodo esiste, sembra sano — e resta **fuori dall'indice vettoriale**,
quindi invisibile alla ricerca per sempre. Non c'è nessun segnale a runtime. L'unico
punto dove si può intercettare è il boot, e per questo il self-test
(`internal/knowledge/ping.go`) resta fatale sul lato *stretto* anche ora che accetta
il lato largo (§4.A).

**Il tokenizer veloce perde alcune emoji.** `transformers` avvisa che la conversione
sentencepiece→fast non implementa il byte-fallback: un carattere fuori vocabolario
diventa `[UNK]` invece di essere spezzato nei suoi byte. Misurato sul tokenizer di
GLiNER (`DebertaV2TokenizerFast`): accenti italiani, nomi propri, identificatori
tecnici e simboli (`≈ ∞ € «»`) passano **intatti**; delle emoji provate, 📅 si perde
e 👍 no. Il danno è confinato all'**estrazione** — il testo del messaggio resta
memorizzato verbatim, è il NER che vede un buco. Non vale un intervento; vale saperlo
se un giorno mancano entità in messaggi pieni di emoji dai canali chat.

### 2.9 I duplicati con lo stesso nome e tipo diverso sono invisibili

Emerso dallo stesso esperimento: da tre messaggi, l'estrazione ha creato `ALPHA` due
volte — una `ORGANIZATION` e una `EVENT`. `DeduplicationConfig.match_same_type_only`
è `True` (`memory/long_term.py:49`), quindi quelle due non vengono mai confrontate.

È una classe di duplicati che né la soglia di §2.6 né lo sweep di §4.C intercettano,
perché entrambi lavorano dentro un tipo. Va nominata nel design anche se non la si
chiude subito: «i duplicati devono sparire» non è vero finché questa resta aperta.

## 3. Decisioni prese con l'operatore

| # | decisione | stato |
|---|---|---|
| D1 | **Opzione C** — alimentazione automatica del grafo; i tool restano per il write deliberato e le correzioni | presa |
| D2 | **Tripartizione** `memory_modify` / `memory_supersede` / `memory_forget` | presa |
| D3 | Superficie potata a **7 tool**, `get_facts` assorbito da `memory_search` | presa 2026-07-28 |
| D4 | `get_entity` **si tiene** (era in dubbio) | presa 2026-07-28 |
| D5 | `get_context` esce dai tool → **iniezione long-term-only** | presa |
| D6 | **GLiNER abilitato** come da ADR | presa 2026-07-28 |
| D7 | **I duplicati devono sparire** — prevenzione + riparazione dell'esistente | presa 2026-07-28 |
| D8 | Serve una **skill memoria** per Aura | presa |

---

## 4. Architettura

Quattro componenti indipendenti, ciascuno testabile da solo.

```
                    ┌─────────────────────────────────────────┐
   turni Aura ─────▶│ A. Pipeline di estrazione (immagine)     │
   (mirror host)    │    spaCy·it + GLiNER + LLM fallback      │
                    └────────────────┬────────────────────────┘
                                     ▼
                    ┌─────────────────────────────────────────┐
                    │ B. Giudizio di identità                  │
                    │    embedder = candidati                  │
                    │    cross-encoder = verdetto              │
                    └────────────────┬────────────────────────┘
                                     ▼
                              ┌──────────────┐
                              │  grafo Neo4j │
                              └──────┬───────┘
                        ┌────────────┴────────────┐
                        ▼                         ▼
        ┌───────────────────────────┐  ┌──────────────────────────┐
        │ C. Riparazione            │  │ D. Superficie modello     │
        │    sweep consolidation    │  │    7 tool + iniezione     │
        │    (dry_run di default)   │  │    + skill                │
        └───────────────────────────┘  └──────────────────────────┘
```

### A. Pipeline di estrazione — è una modifica all'immagine

`Dockerfile:57` diventa `pip install -e ".[mcp,google,openai,extraction,fuzzy]"`.
Gli extra esistono già in `pyproject.toml:71-78`:
`extraction = ["spacy>=3.7.0", "gliner>=0.2.0"]`, `fuzzy = ["rapidfuzz>=3.0.0"]`.

Tre conseguenze, tutte volute:

1. **Stage 1 e 2 si accendono** senza toccare un solo flag — `extractor_type` è già
   `PIPELINE` e i due `enable_*` sono già `true` (§2.1). Non c'è configurazione da
   cambiare: c'è software da installare.
2. **La gamba fuzzy resuscita** in `CompositeResolver` *e* in `_check_for_duplicates`.
3. **L'LLM continua a girare a ogni estrazione** (§2.4): con `stop_on_success=False` e
   merge `CONFIDENCE` gli stadi si sommano, non si escludono. Il guadagno è la
   **qualità** — tre estrattori che votano, `CONFIDENCE` tiene il più confidente —
   **non** il costo né l'indipendenza dal provider. Chi volesse quest'ultima dovrebbe
   passare a `stop_on_success=True`, che però spegne la ridondanza: fuori scope qui.

**Il modello italiano richiede la rimappatura di §2.3, nello stesso commit.**
`spacy_model = it_core_news_sm` più un `type_mapping` che copre WikiNER:
`PER→PERSON`, `LOC→LOCATION`, `ORG→ORGANIZATION`, `MISC→OBJECT` (esplicito,
non per default silenzioso). Senza questa riga lo Stage 1 è una regressione.

I pesi GLiNER (`gliner-community/gliner_medium-v2.5`, `config/settings.py:213`) e il
modello spaCy vanno **cotti nell'immagine**: il servizio non ha volumi né cache HF
(`compose.yaml`, servizio `aura-agent-memory-mcp`), quindi altrimenti verrebbero
riscaricati a ogni start — o fallirebbero in un deploy air-gapped.

**GLiREL non è uno stadio del pipeline.** La documentazione lo cabla con una classe
dedicata — `GLiNERWithRelationsExtractor.for_poleo()` — che sostituisce
`GLiNEREntityExtractor` allo Stage 2 («Relations needed → GLiNER + GLiREL →
`[GLiNERWithRelations]`»). La classe esiste nel fork (`gliner_extractor.py:1108`,
`for_poleo` a `:1227`); la factory però costruisce sempre `GLiNEREntityExtractor`
(`factory.py:118-143`) e non ha un ramo che la produca. Abilitare GLiREL è quindi
**pacchetto + cablaggio della factory**, ed è l'unica modifica di codice del componente A.

Con GLiREL le relazioni smettono di dipendere dallo Stage 3: oggi le estrae solo
l'LLM, perché spaCy dichiara di non farlo (`spacy_extractor.py:44-45`) e GLiNER da
solo fa NER. Le relazioni estratte vengono già persistite da sole come `RELATED_TO`
(documentato in `explanation/extraction-pipeline.adoc` §Automatic Relationship Storage),
quindi non serve superficie nuova per scriverle — coerente con D1.

### B. Giudizio di identità — usare la configurazione documentata

Una riga, non un'architettura:

```
NAM_RESOLUTION__SEMANTIC_THRESHOLD=0.9
```

È il valore raccomandato da `configuration.adoc`; il codice ne spedisce 0.8. Il tetto
di rumore misurato sui nomi di entità è 0.8904, quindi 0.9 rifiuta tutte e tre le
coppie che oggi collassano (§2.6), e la variabile è verificata come onorata.

Perché non di più: alzarla oltre non ha basi misurate, e ogni punto sopra il tetto è
richiamo perso su duplicati veri. Perché non un cross-encoder: §2.6 — era una mia
invenzione sopra un problema già risolto a monte.

Le soglie del **deduplicatore** non si toccano: nessuna delle tre coppie le ha
superate (§2.2), e comunque le env var documentate per farlo non sono cablate in
questo fork (§2.7).

### C. Riparazione — le primitive esistono, manca la porta

Già in libreria, senza superficie MCP:

| primitiva | file |
|---|---|
| `dedupe_entities` (dry_run=True di default, idempotente, audit-noded) | `memory/consolidation.py:46` |
| `detect_superseded_preferences` | `memory/consolidation.py:219` |
| `merge_duplicate_entities` | `memory/long_term.py:1882` |
| `supersede_preference` | `memory/long_term.py:969` |
| `delete_message` | `memory/short_term.py:1071` |

Lo sweep è un **job operatore**, non un tool del modello: gira su richiesta o a cadenza,
`dry_run` di default, e il diff va rivisto prima di applicare. Non aggiunge superficie
al manifest dell'LLM.

Deve riparare, sull'esistente: i 3 `canonical_name` corrotti e i 6 `SAME_AS` in stato
`pending` (creati da `add_entity` quando la dedup *flagga* invece di fondere —
`long_term.py:714` — e mai rivisti perché nessun percorso di revisione era esposto).

### D. Superficie del modello — solo la famiglia long-term

> **Decisione D9 (2026-07-28).** Questo MCP serve **solo come memoria a lungo termine**,
> e in quel ruolo deve funzionare al 100%. Sparisce la famiglia **short-term**; la
> famiglia long-term resta intera, più i verbi di correzione.

Chi scrive la memoria — due sorgenti, entrambe necessarie:

1. **L'estrazione automatica** (D1): i turni specchiati host-side alimentano il
   pipeline, che produce `Entity` / `Fact` / `Preference`. Nessuna superficie per il
   modello: è il componente A.
2. **Il modello, deliberatamente**, per ciò che il testo non contiene — una decisione
   presa, una preferenza espressa, una lezione da una correzione: `memory_add_fact`,
   `memory_add_preference`.

| tool | ruolo |
|---|---|
| `memory_add_fact` | write deliberato |
| `memory_add_preference` | write deliberato |
| `memory_search` (+ bucket `facts`) | lettura |
| `memory_get_entity` | lettura strutturale, e vista sui duplicati |
| `memory_modify` | «è sbagliato» |
| `memory_supersede` | «era vero, ora non più» |
| `memory_forget` | «non deve esistere» |

Spariscono — tutti short-term, ciascuno con un motivo misurato qui dentro:

| tool | perché sparisce |
|---|---|
| `store_message` | non è superficie del modello: il mirror è host-side (D1) |
| `get_conversation` | Postgres è la verità: 552 turni su 19 conversazioni contro 18 `Message` |
| `list_sessions` | sessione globale unica: elenca sempre una riga |
| `get_context` | short-term fuori scope, **e** perde il filtro di sessione (§2.8) |
| `add_entity` | entità e relazioni nascono dall'estrazione; ed è il percorso che *crea* i collassi §2.2 |
| `create_relationship` | idem, e con GLiREL le relazioni arrivano dal pipeline |
| `get_facts` | assorbito da `memory_search` (§5.2) |

#### I tre verbi, formulati per come si usano davvero

Una prima stesura glossava `forget` come «non deve esistere» e lo lasciava a coprire
anche il vecchio e lo sbagliato. È una partizione errata: il **vecchio** lo chiude
`supersede`, lo **sbagliato** lo corregge `modify`. A `forget` non resta nessuno dei due.

| verbo | quando | cosa succede al nodo |
|---|---|---|
| `modify` | il contenuto è sbagliato, il nodo ha diritto di esistere | resta, con id e relazioni; testo ed embedding aggiornati |
| `supersede` | era vero, non lo è più | resta, `valid_until` chiuso, `SUPERSEDED_BY` verso il successore |
| `forget` | **non doveva mai entrare nel grafo** | sparisce |

Il terzo caso non è teorico. Nel grafo vivo oggi:

- `Test Entity` e `ZTestEntityDel9` — scarti di una sessione di prova del 20/07/2026.
  Non sono «sbagliate» né «vecchie»: non devono esserci. E sono esattamente quelle su
  cui è collassato `Aura GitHub repository` (§2.2).
- Il `SAME_AS` fra `Davide` e `Andrea Piemarino` — un arco che non doveva nascere.
- La richiesta esplicita dell'operatore di cancellare qualcosa: diritto all'oblio,
  non correzione.

#### Non c'è via di fuga, quindi ogni classe di corruzione ha bisogno del suo verbo

Il modello **non ha Cypher**. `graph_query` è uno dei cinque tool che
`mcp/_instructions.py` insegna e che non esistono (§7): al modello viene detto di avere
un accesso al grafo che non ha. Non esiste nessun altro strumento generico.

La conseguenza è severa: **ciò che nessun verbo raggiunge è permanente.** Oggi le
relazioni non sono raggiunte da niente — `create_relationship` non ha inverso — quindi
i `SAME_AS` sbagliati sono definitivi.

E non restano fermi: **propagano**. Il docstring di `update_entity`
(`memory/long_term.py:2810-2816`) dice che un `canonical_name` stantìo «lascia che il
resolver rimergi le correzioni future sul vecchio valore». Un nodo spazzatura con un
`canonical_name` sbagliato non è un nodo morto, è un **attrattore**: ogni scrittura
futura di quel nome ci ricade sopra. È il meccanismo per cui `Test Entity` si è preso
un repository, e per cui il grafo peggiora da solo invece di stabilizzarsi.

Da qui due requisiti non negoziabili:

1. **`memory_forget` esteso a `relationship`** — è l'unico modo di rompere un `SAME_AS`.
   (Cade invece l'estensione a `message`: in questo scope non serve più.)
2. **`mcp/_instructions.py` riscritto sui tool reali** — insegnare `graph_query` non è
   solo inutile, fa credere al modello di avere una via di riparazione che non esiste,
   e gli impedisce di cercare quella vera.

#### Perché diventano deferred

Oggi `memory` è l'**unico namespace MCP non-deferred**
(`internal/agent/mcptools/bridge.go:259-261`, `return namespace != "memory"`).
L'eccezione nasceva dal costo: un tool deferred costava una `tool_search` **per turno**.
Dal commit `5523ad01` il grant è **conversation-scoped** (`deriveActivated` in
`internal/agent/llm_agent_promote.go`): costa una `tool_search` **per conversazione**.
Verificato live: 5 turni consecutivi, 28 chiamate, nessuna `tool_search` ripetuta.

**L'eccezione cade.** Tutti e 7 i tool diventano deferred, come `calculator` (23) e
`calendar` (14).

| tool | intenzione | stato |
|---|---|---|
| `memory_add_fact` | write deliberato | esiste |
| `memory_add_preference` | write deliberato | esiste |
| `memory_search` | lettura | esiste, **+ bucket `facts`** |
| `memory_get_entity` | lettura strutturale | esiste |
| `memory_modify` | «è **sbagliato**» — corregge in place per id | **esiste già come `memory_update`** |
| `memory_supersede` | «**era** vero, ora non più» | primitiva esiste, manca la porta |
| `memory_forget` | «non deve **esistere**» | esiste, **da estendere** |

Tagliati: `add_entity`, `create_relationship`, `store_message`, `get_conversation`,
`list_sessions`, `get_facts`, `get_context`. Razionale per ciascuno in §5.

---

## 5. Superficie — dettaglio delle decisioni

### 5.1 `memory_modify` è già spedito

`memory_update` (registrato, 17 funzioni nel sidecar ricostruito) fa **esattamente**
ciò che D2 chiede: edita per id bypassando resolver e dedup, e —

- su **entity**: tiene `canonical_name` in sync con `name`, «altrimenti un
  `canonical_name` stantìo lascia che il resolver rimergi le correzioni future sul
  vecchio valore — il bug esatto che questo metodo esiste per risolvere»
  (`long_term.py:2810-2816`), e **rigenera l'embedding** su cambio di `name`/`description`;
- su **preference**: rigenera l'embedding dal testo post-update
  (`long_term.py:2511+`), così `preference_embedding_idx` non resta sul vecchio testo;
- su **fact**: `long_term.py:2635`.

**La prima gamba della tripartizione è zero lavoro di libreria.** Resta una sola
domanda aperta, di naming: rinominare `memory_update` → `memory_modify` per allineare
i tre verbi, oppure tenere il nome spedito. Vedi §11 OQ1.

### 5.2 `memory_search` assorbe `get_facts`

Oggi i bucket sono `messages` / `entities` / `preferences` / `traces`.
`memory_types: ["facts"]` restituisce **`{"results": {}}`** — silenzioso, nessun errore.
I 37 nodi `:Fact` non sono raggiungibili da `search` in nessun modo.

Il ramo `facts` di `get_entity` copre solo il caso owned-entity: `Davide` → 16 fatti,
ma `Aura` → 0 mentre `get_facts(subject="Aura")` → 3, perché un fatto compare lì solo
se il suo soggetto nomina un'entità posseduta.

Due modifiche al fork:

1. **Aggiungere il bucket `facts`** a `memory_search`, che deve fare *entrambe* le cose
   che `get_facts` faceva: **match esatto per subject** e, in mancanza, semantico.
   Perdere il lookup esatto sarebbe una regressione — la soglia semantica è la stessa
   che assegna 0.91 a «si proprio quello».
2. **Errore su `memory_types` sconosciuto.** Un bucket inesistente che risponde `{}`
   è un errore che passa in silenzio; va rifiutato, non ignorato.

### 5.3 `get_entity` si tiene — è il read più economico e l'unico strutturale

Misurato:

| tool | payload completo |
|---|---|
| `memory_get_context` (default) | **14.610** char |
| `memory_search` (limit=5) | 10.176 char |
| `memory_get_context` (long-term, max_items=5) | 7.980 char |
| **`memory_get_entity`** | **1.280** char |

Restituisce entità + fatti + vicini in **una** chiamata, ed è l'unico che espone
`other_matches` — la vista sui duplicati. È l'input necessario a `modify`/`supersede`/
`forget`: devi vedere *quale* dei nodi omonimi stai per correggere, e il suo id.
Ed è come i tre collassi di §2.2 sono stati trovati: `search`, nella stessa passata,
ha restituito 5 entità su 8 e ha nascosto `Andrea Piemarino`.

### 5.4 `memory_forget` esteso a message e relationship

Oggi copre `preference` / `fact` / `entity`, con ownership enforced e cancellazione
non-cascading sulle entità (un'entità condivisa viene scollegata, non rimossa).

Va esteso a **`relationship`**: `create_relationship` non ha inverso, e i `SAME_AS`
sbagliati di §2.2 non sono rompibili da nessun tool. Senza questo verbo lo sweep C può
fondere ma non può disfare, e §4.D mostra che non esiste nessuna via di fuga alternativa.

Non va esteso a `message`: fuori scope per D9. `delete_message` resta in libreria
(`short_term.py:1071`) senza porta, e va bene così.

### 5.5 `get_context` cade — e non torna come iniezione verbatim

Cade per D9 (è la metà short-term), ma anche se lo scope fosse più largo il payload
**non sarebbe iniettabile**:

- `content` e `structuredContent` sono **lo stesso blob duplicato** — metà del payload
  è spreco puro sul wire (14.610 char in totale, default);
- **Agent.md compare 4 volte** nella stessa risposta (2 in *Recent Conversation*,
  2 in *Relevant Past Messages*), ed è già nel system prompt di Aura: quinta copia;
- la rilevanza non filtra — «si proprio quello» prende **0.91**, sopra i dump di
  Agent.md a 0.90 (§2.8 lo riproduce in laboratorio: 0.90 contro 0.89);
- trasporta **entrambe** le regole contraddittorie di §1 nello stesso payload:
  iniettarlo verbatim inietta la contraddizione a ogni turno;
- **perde il filtro di sessione** (§2.8).

Resta aperto **come** il long-term arriva in contesto: a domanda via `memory_search`
(che il filtro lo onora), o iniettato da un assemblaggio Aura-side. Vedi OQ5.

Vincolo di sicurezza in entrambi i casi: il long-term che entra in contesto è una
superficie di memory-poisoning / prompt-injection — con l'iniezione atterra in
`messages[1]`. Il gate `/gsd-secure-phase` sul confine ingestion→memoria resta
**pre-requisito per qualunque default in produzione**.

### 5.6 Tagli restanti

| tool | perché cade |
|---|---|
| `add_entity` | D1: le entità nascono dall'estrazione automatica. Ed è il percorso che *crea* i collassi §2.2 — toglierlo dal modello riduce la superficie del bug mentre B atterra |
| `create_relationship` | D1: `MENTIONS` 20, `ABOUT_SUBJECT` 16, `RELATED_TO` 18 nascono dall'estrazione, e con GLiREL arriveranno dal pipeline. Mai chiamato dal modello |
| `store_message` | non è superficie del modello: resta il **mirror host-side** (D1). Oggi chiamato 1 volta in totale, motivo per cui il `MemoryObserver` a tre livelli è attivo di default **senza nulla da comprimere** (soglia 30.000) |
| `get_conversation` | D9. Postgres è la verità: 552 turni, 19 conversazioni contro 18 `Message`. Mai chiamato |
| `list_sessions` | D9. Sessione globale unica: elenca sempre una riga. Mai chiamato |
| `get_context` | D9 + §5.5 |

---

## 6. Skill memoria per Aura

I tool dicono *come* scrivere. La skill dice **quando**, e soprattutto quando *non*.

Contenuto, derivato dai difetti misurati:

1. **Non scrivere ciò che l'estrazione già scrive.** Entità, relazioni e menzioni
   nascono da sole (D1). Il write deliberato è per ciò che il testo non contiene:
   una decisione presa, una preferenza espressa, una lezione da una correzione.
2. **Correggere non è ri-aggiungere.** `add_*` mergia sul match più vicino *senza
   cambiarne il testo*: ri-aggiungere per correggere re-mergia sempre sulla vecchia
   formulazione. È il meccanismo che ha prodotto le due regole contraddittorie di §1.2.
   Per correggere si usa `memory_modify` con l'id.
3. **I tre verbi, e come sceglierli.** *È sbagliato* → `modify`. *Era vero, ora non più*
   → `supersede` (conserva la storia; `valid_until` + `SUPERSEDED_BY`, e
   `get_preferences_for(as_of=…)` fa già il time-travel). *Non deve esistere* → `forget`.
4. **Da dove viene un id.** Dal `add_*` che ha scritto (campo `id`, o
   `deduplication.matched_entity_id` quando ha mergiato), da `get_entity`, o da `search`.
5. **Prima di correggere, guarda i duplicati.** `get_entity` espone `other_matches`:
   correggere il nodo sbagliato fra due omonimi è peggio che non correggere.

La skill va nel repo skill di Aura (`$AURA_SKILLS_DIR`), non fra le 69 di `.claude/skills/`:
è una skill **dell'agente**, non dell'ambiente di sviluppo.

---

## 7. Gestione degli errori

Il tema ricorrente della diagnosi è che **gli errori passano in silenzio**. Il design
li rende rumorosi.

| situazione | oggi | dopo |
|---|---|---|
| `rapidfuzz`/`gliner` assenti | `except: pass`, stadio saltato senza traccia | l'immagine li contiene; se uno stadio non si costruisce, **log a livello error all'avvio** |
| `memory_types` sconosciuto | `{"results": {}}` | errore esplicito |
| cross-encoder irraggiungibile | n/a | **non merge**, e lo dice (§4.B) |
| `forget` su nodo non posseduto | già corretto: rifiuta e lo riporta | invariato — è il modello giusto, va esteso a message/relationship |
| `_instructions.py` insegna 5 tool inesistenti (`memory_start_trace`, `memory_record_step`, `memory_complete_trace`, `memory_export_graph`, `graph_query`) | il modello li chiama e fallisce | **riscritte sui 7 tool reali** |
| commento Dockerfile dice `--embedding-dimensions 384`, il comando usa `768` | documentazione falsa | allineato |

---

## 8. Testing

Regole di casa che vincolano: coverage floor 85% sul tag matrix, `t.Skip` che
`t.Fatal` sotto `$CI`, no skip-as-green, mutation ≥70% sui file critici.

**Il grafo Neo4j è VIVO e non disposable.** Ogni test che scrive usa un
`user_identifier` dedicato — i tool lo accettano e lo scope isola dall'operatore
(`b130c94d-a213-463a-a797-ec124104363a`). Il coverage gate non va mai puntato su
questo Neo4j: il tier `neo4j_integration` di `scripts/coverage_docker.sh` lo azzera.

| livello | cosa prova | dove |
|---|---|---|
| unit (Go) | il bridge non ha più l'eccezione `namespace != "memory"`; i 7 tool sono deferred; la promozione resta conversation-scoped | `internal/agent/mcptools/` |
| unit (Python, fork) | la rimappatura WikiNER: `PER→PERSON`, non `OBJECT`. **Test di regressione diretto su §2.3** | `tests/` del fork |
| unit (Python, fork) | `memory_types` sconosciuto → errore, non `{}` | idem |
| **property-based** | per ogni coppia di nomi distinti, il giudizio di identità non fonde. È la forma giusta: §2.2 è un invariante violato, non un caso singolo | idem |
| integration | il bucket `facts` restituisce sia il match esatto per subject sia il semantico | stack vivo, user scope dedicato |
| integration | `forget` su message e su relationship; **e in particolare che rompa un `SAME_AS`** | idem |
| regressione dati | i 3 `canonical_name` corrotti e i 6 `SAME_AS` pending sono un **fixture reale**: lo sweep C deve ripararli in dry-run prima di applicare | idem |
| E2E | la DoD del progetto: scenario reale sullo stack vivo, score >9.8. Non smoke |

Il caso E2E naturale è la contraddizione di §1.2: Aura deve accorgersi che
`bad4437d` è falsa, correggerla con il verbo giusto, e non ri-crearla al turno dopo.

---

## 9. Riproduzione delle misure

Perché i numeri di questo documento siano verificabili e non da fidarsi:

- **superficie e payload**: sessione MCP JSON-RPC diretta su `http://127.0.0.1:8091/mcp`
  (`initialize` → `notifications/initialized` → `tools/call`), misurando
  `len(json.dumps(result))`. Nota operativa: il mount MCP configurato in
  `~/.claude.json` punta a `127.0.0.1:8095/mcp/`, **che è morto** — il sidecar vivo è
  su `8091`. Va corretto.
- **banda dell'embedder**: coseno fra gli embedding degli 8 nomi di entità reali del
  grafo, via `POST /v1/embeddings` su `aura-llama-embed:8081`.
- **separazione del cross-encoder**: `POST /rerank` su `aura-rerank:8085`, stesso corpus.
- **dipendenze mancanti**: `docker exec aura-agent-memory-mcp python -c "import gliner"` etc.
- **label italiane**: `spacy-models/meta/it_core_news_sm-3.8.0.json`, campo `labels.ner`.

---

## 10. PRD-amendment — pre-requisito al codice

CLAUDE.md §PRD-first: nessuna riga di codice prima dell'amendment. L'ADR della spike
lo richiedeva già per la strategia 4-layer; questo design ne è il Layer C concreto.

L'amendment deve coprire:

1. **Superficie di memoria**: da 12 tool non-deferred a **7 deferred**; caduta
   dell'eccezione `namespace != "memory"` e sua motivazione (`5523ad01`).
2. **`get_context` non è più un tool**: diventa iniezione long-term-only, con il
   vincolo di sicurezza `messages[1]` esplicitato.
3. **Pipeline di estrazione**: il sidecar passa da una corsia a tre — spaCy italiano
   rimappato + GLiNER (+GLiREL per le relazioni) + LLM. È una modifica **all'immagine**,
   non alla configurazione: `extractor_type` era già `PIPELINE`. Dichiarare
   esplicitamente che **l'LLM continua a girare a ogni estrazione** (§2.4): il PRD non
   deve poter essere letto come "la memoria non chiama più il provider".
4. **Soglia di risoluzione**: `NAM_RESOLUTION__SEMANTIC_THRESHOLD=0.9`, il valore
   raccomandato upstream. Documentare il *perché* con la misura (tetto di rumore
   0.8904), non come numero magico.
5. **Env catalog**. Le variabili del sidecar sono `NAM_*` — naming upstream, quindi
   ricadono nell'eccezione già prevista dal PRD per librerie/sidecar di terze parti,
   come `TELEGRAM_BOT_TOKEN` e `LLAMA_*`. Vanno catalogate con quel nome, **non**
   rinominate in `AURA_*`. Voci Aura-side nuove: cadenza/abilitazione dello sweep di
   consolidation, toggle dell'iniezione long-term e suo `max_items`
   (`AURA_CONTEXT_MEMORY_RECALL` esiste già e va riconciliato, non duplicato).
6. **Divergenza doc/implementazione da registrare**: `NAM_DEDUPLICATION__*` è
   documentato upstream ma non cablato in questo fork (§2.7). Il PRD non deve
   catalogare env var che non fanno nulla.
6. **Migration**: nessuna Postgres. Se lo sweep introduce nodi di audit, la
   sequenza Cypher è `internal/knowledge/migrations/` — e il numero **non si deduce**:
   `ls internal/knowledge/migrations/ | tail -1`.

---

## 11. Domande aperte

1. **OQ1 — naming.** `memory_update` è spedito, documentato bene e già usato dal
   modello. Rinominarlo `memory_modify` allinea i tre verbi ma rompe un nome vivo.
   Tenere `memory_update` conserva la compatibilità ma lascia la tripartizione
   asimmetrica a livello di nome. *Raccomandazione: tenere `memory_update` e
   descrivere la tripartizione nella skill, dove conta.*
2. **OQ2 — `supersede` sui fatti.** `supersede_preference` è preference-only e usa
   `valid_until` + `SUPERSEDED_BY`. I `:Fact` hanno già `valid_from`/`valid_until`
   nello schema di `add_fact`. Estendere la primitiva ai fatti è lavoro nuovo di
   libreria: va dimensionato nel plan.
3. **OQ3 — cadenza dello sweep.** On-demand, o schedulato? Il grafo è piccolo
   (37 fatti, 28 entità): on-demand è sufficiente oggi, ma la cadenza è la differenza
   fra igiene e accumulo. Va decisa con un numero, non per default.
4. **OQ4 — dimensione del modello spaCy.** `sm` non ha vettori, `md`/`lg` sì. Il
   pipeline non usa i vettori di spaCy (l'embedding viene dal sidecar), quindi `sm`
   dovrebbe bastare: da confermare misurando, non assumendo.
5. **OQ5 — come il long-term arriva in contesto.** A domanda, con `memory_search`
   (che il filtro di sessione lo onora, §2.8), oppure iniettato da un assemblaggio
   Aura-side a ogni turno. La misura pesa contro l'iniezione automatica finché il
   punteggio non separa (0.90 contro 0.89): iniettare a ogni turno ciò che il
   retriever non sa scegliere significa iniettare rumore in `messages[1]`, con il
   costo di sicurezza di §5.5. *Raccomandazione: a domanda, e si rivaluta quando il
   gate di §2.8 passa.*
6. **OQ6 — duplicati con lo stesso nome e tipo diverso** (§2.9). `match_same_type_only`
   è `True` e nessuna delle leve di questo design li vede. Chiuderli richiede o un
   confronto cross-tipo (che riapre il problema del rumore) o una regola di
   precedenza fra tipi. Non risolto qui, ma **nominato**: senza, «i duplicati devono
   sparire» resta parzialmente falso.
7. **OQ7 — glirel è abbandonato upstream.** La 1.2.1 è l'ultima release e ha due
   difetti di pacchettizzazione (§4.A) che costringono l'intera immagine a
   `huggingface_hub<1.0` + `transformers<5.0`. È un debito che cresce: va deciso se
   accettarlo, forkare glirel, o rinunciare alle relazioni da GLiREL e lasciarle
   allo Stage 3.

---

## 12. Fuori scope

- Riscrivere `MemoryObserver`: si alimenta da `store_message`, che con il mirror di D1
  inizia finalmente a ricevere dati. Va **osservato** prima di essere toccato.
- Il rilevatore automatico di preferenze (`--no-auto-preferences`, regole su keyword
  inglesi in `_preference_detector.py`): resta spento. Su input italiano mancherebbe
  comunque il bersaglio, e D1 affida il write deliberato al modello.
- I Layer A / B / D dell'ADR: questo design è il **Layer C**.
- La rimozione del motore di compaction dark: fase separata, già decisa dall'ADR.
