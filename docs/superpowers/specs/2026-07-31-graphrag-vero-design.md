# GraphRAG vero su Neo4j — design

Data: 2026-07-31
Stato: approvato per la pianificazione (sezioni 1–3 approvate dall'operatore)
Segue e non rimette in discussione: `2026-07-28-memory-surface-design.md`

## Perché

Neo4j in Aura fa due mestieri e li fa bene: indice vettoriale (`chunk_embedding`) e
indice full-text (`chunk_text`). Il terzo — essere un grafo — non lo fa.

Censimento del deployment vivo, 2026-07-31:

| | |
|---|---:|
| `Chunk` / `NEXT_CHUNK` | 302 / 299 |
| `Entity` (di cui fixture di test) | 27 (~17) |
| `Fact` | 26 |
| `Fact` con arco `ABOUT_SUBJECT` | **4** |
| archi semantici totali (`RELATED_TO` + `MENTIONS` + `SAME_AS`) | **10** |
| procedure GDS installate e usate | tutte / **nessuna** |

Il sottografo dell'entità meglio popolata:

```
Davide --ABOUT_SUBJECT-- 4 Fact
Davide --HAS_ENTITY-- User(e343c45d…)
```

Nessun arco verso `Caraglio`. Nessuno verso `PmSync`. Eppure i fatti dicono
`located_in → Caraglio` e `works_for → PmSync`, e quelle entità **esistono come nodi**.

Conseguenza misurabile, non teorica: `memory_get_entity(Davide, include_neighbors)`
restituisce quattro fatti e l'utente proprietario. Una domanda a due salti — «dove
lavora la persona che vive a Caraglio?» — non è risolvibile per traversata e ricade
sul vettoriale sui fatti singoli.

## Cosa c'è già, e va usato invece che riprogettato

Lo schema che Neo4j dichiara è **già** quello di un GraphRAG:

- etichette tipizzate `Person` / `Organization` / `Location` / `Concept`;
- `RELATED_TO` con `relation_type`, `confidence`, `created_at`;
- `SAME_AS` con `match_type`, `status`, `confidence` — entity resolution;
- `MENTIONS` da `Message` a `Entity` con `confidence` — provenienza;
- `Entity.embedding`, `Fact.embedding`, `Preference.embedding` — già popolati;
- `MemoryCorpusRevision` / `DocumentCorpusRevision` — epoche per invalidare cache.

Non manca il modello. Manca il contenuto, e manca **un arco che nello schema non
esiste**: `Fact` punta al soggetto, verso l'oggetto non c'è nulla.

## Il vincolo che ha già ucciso un tentativo

Il pipeline classico (spaCy IT + GLiNER + GLiREL su torch) è stato **costruito,
misurato e rimosso** il 2026-07-28: 3,5× più lento dell'LLM da solo (45,9 s contro
13,0 s sulla stessa frase), un'entità in più che era un duplicato, `glirel` 1.2.1
abbandonato, ~20 GB di immagine per un percorso di codice mai raggiunto. Questo design
**non lo riapre**.

### Misura nuova: GLiNER ONNX, 2026-07-31, 4 thread CPU

Stesso modello, runtime diverso, testo italiano vero preso dai dati dell'operatore:

| variante | peso | latenza media | entità |
|---|---:|---:|---|
| `model.onnx` (fp32) | 1104 MB | 233 ms | complete |
| **`model_fp16.onnx`** | **553 MB** | **174 ms** | **identiche a fp32, score allo 0.01** |
| `model_quantized` (int8) | 333 MB | 87 ms | **nessuna** |
| `model_int8` | 333 MB | 37 ms | **nessuna** |

Caricamento da cache calda: 5,9 s. Le due varianti int8 sono **inservibili** — GLiNER
senza quantization-aware training perde tutto in int8, come la letteratura segnala.

Esempi, `model_fp16`:

```
"Sono Davide, faccio il programmatore a Caraglio e lavoro per PmSync"      158 ms
   Davide/persona 0.98 · Caraglio/luogo 0.94 · PmSync/organizzazione 0.69

"Il cliente ZOPPI SRL ha codice 424410 e ha sede a Volvera (Torino)"       159 ms
   ZOPPI SRL/organizzazione 0.89 · 424410/CODICE 0.80 · Volvera/luogo 0.97

riga grezza di spreadsheet                                                 182 ms
   F038/luogo 0.56 · VOLVERA/organizzazione 0.66            ← rumore, sotto soglia

"il servomotore B&R 8LSA35.DB030S300-3 entro venerdì da Cuneo"             198 ms
   B&R/organizzazione 0.60 · venerdì/data 0.92 · Cuneo/luogo 0.95
   il part number NON viene visto
```

Tre letture, tutte vincolanti per il design:

1. su prosa italiana è buono, e **distingue il letterale dall'entità** (`424410` →
   `codice`, non un nodo da coniare). È il discriminatore che oggi manca del tutto;
2. su righe tabellari è rumoroso: la confidence separa, ≥0.85 si usa, sotto no;
3. **non sostituisce l'LLM** — perde il part number, che in quella frase è il dato
   più prezioso.

Il suo posto è quindi tipizzare ciò che l'LLM ha già isolato, e fare da rete nel job
di consolidamento. Non estrarre al posto suo.

**Budget immagine.** ~20 GB era lo stack rifiutato; lo spike con `gliner`+torch è
1,75 GB; il target è **onnxruntime + tokenizer + pesi fp16 ≈ 700 MB**. Vincolo
esplicito dell'operatore: si sta in ONNX. Il pacchetto `gliner` importa torch comunque,
quindi arrivarci richiede pre/post-processing proprio (~100 righe: tokenizzazione e
decodifica degli span) oppure un sidecar Node con `transformers.js`, che è il runtime
per cui quel repo ONNX è pubblicato. È lavoro vero e va pianificato come tale.

## Design

### 1. Gli archi nascono alla scrittura

`memory_add_fact` accetta oggi tre stringhe e scrive un `Fact` piatto. Accetta anche il
**tipo** dei due estremi (`PERSON` / `ORGANIZATION` / `LOCATION` / `CONCEPT` /
`LITERAL`). Con un estremo tipizzato entità il write path fa `MERGE` del nodo e
materializza l'arco:

```
memory_add_fact(subject: "Davide", subject_type: PERSON,
                predicate: "works_for",
                object: "PmSync", object_type: ORGANIZATION)

  (:Person {name:"Davide"})-[:RELATED_TO {relation_type:"works_for", confidence}]->
  (:Organization {name:"PmSync"})
```

Con `LITERAL` (`Europe/Rome`, `424410`, `"ok"`, un timestamp) non conia nulla: resta
proprietà del fatto. Chi non dichiara il tipo ottiene il comportamento di oggi, quindi
nessun chiamante esistente si rompe.

Il tipo lo dichiara l'LLM — lo sa già, ha appena scritto la tripla — e quando non lo fa
lo mette GLiNER fp16 con soglia 0.85. Il `Fact` **resta** come record con provenienza e
confidence: l'arco è la vista navigabile, il fatto è l'evidenza.

### 2. Il job di consolidamento

Fuori dal turno, quindi i 174 ms per frase non li vede nessuno.

**Passaggio 0 — pulizia, non negoziabile.** Via le fixture di test: ~17 entità su 27
(`ALICE-ONLY`, `Atomic Profile Entity …`, `Codex Live Place …`, `AuditSource…`,
`Quasar Walrus Almanac`, `Tungsten Turbine Apparatus`, `aura-e2e-…`), i fatti
`onboarding_completed`, i `Codex Live Subject`. Finché sono lì ogni misura di qualità è
falsata e quelle entità compaiono nelle risposte all'operatore — misurato: `Quasar
Walrus Almanac` e `Tungsten Turbine Apparatus` occupavano 2 slot su 4 nella risposta a
«dove vive Davide».

**1. Tipizzazione** degli estremi dei fatti rimasti piatti. Soglia 0.85; sotto soglia il
fatto resta piatto e viene **marcato**, mai indovinato.

**2. Risoluzione.** `MERGE` per nome canonico, poi `SAME_AS` sui quasi-duplicati usando
gli embedding già presenti sulle entità. `status` distingue proposto da confermato; una
fusione sbagliata si rompe con `memory_forget` su relazione, che esiste già ed è stato
esteso apposta il 2026-07-28.

**3. Materializzazione.** Da fatto tipizzato a `RELATED_TO {relation_type, confidence}`.
È qui che i 22 fatti orfani diventano grafo.

**4. Comunità.** `gds.leiden` sul grafo entità, un riassunto per comunità, con
`MemoryCorpusRevision.epoch` a invalidare i riassunti quando il grafo cambia.

### 3. Il recupero che cammina

1. **Àncora** — la query si risolve prima a entità per **nome esatto**, poi vettoriale
   con floor. Lezione di `tool_search`, misurata: il denso non trova i letterali.
2. **Traversata** — 1–2 hop da ogni àncora su `RELATED_TO` / `ABOUT_SUBJECT`, con cap
   sul fan-out, raccogliendo fatti ed entità vicine.
3. **Fusione** — sottografo percorso e ricerca vettoriale sono due gambe, fuse per
   reciprocal rank con `rrfK`: **la stessa funzione e la stessa costante** già in
   produzione per i documenti (`internal/documents/seed_fusion.go`, commit `76ef4205f`).
4. **Domande globali** — senza àncora ma con tema, si risponde dai riassunti di
   comunità invece che da k chunk.

### 4. La soglia, subito e separatamente

`memory_search` espone `threshold` e nessuno lo passa. Misurato oggi sul grafo vivo:

| query | default | `threshold: 0.8` |
|---|---|---|
| «dove vive Davide» | 5 entità (2 fixture di test), 1267 ms | **Davide + Caraglio**, 282 ms |
| «codice cliente ZOPPI» | 5 entità + 5 fatti | **1 fatto: ZOPPI SRL**, 52 ms |
| «xilofono quantistico marmellata» | 5 entità + 5 fatti + 1 preferenza | ancora 3 risultati |

Il bridge inietta già `user_identifier` (`withMemoryUserIdentifier`): lo stesso seam
inietta `threshold` quando manca. Vale da solo, non aspetta il resto.

Nota onesta: 0.8 compra precisione e 5–20× di latenza, **non** compra il «non lo so» —
la query senza senso restituisce ancora dati veri. Con 27 entità e nessun arco tutto è
vicino a tutto: è la topologia mancante, non il cutoff. Lo risolvono le sezioni 1–2.

## Cosa NON si fa

- Non si riapre il pipeline spaCy+GLiNER+GLiREL su torch (misurato e rifiutato).
- Non si usa int8 (misurato oggi: zero entità).
- Non si sostituisce l'LLM come estrattore: perde i part number.
- Non si tocca il recupero documentale, appena sistemato e verificato (`76ef4205f`,
  `e14f88f8f`).

## Limiti dichiarati

- Il corpus di misura è minuscolo: 27 entità, 26 fatti, un documento. Ogni numero qui è
  vero e nessuno è generalizzabile. Le soglie vanno ri-misurate a corpus cresciuto.
- GLiNER fp16 è misurato su **quattro** frasi. Serve un set di valutazione vero prima di
  farne dipendere la tipizzazione automatica.
- Il costo ONNX-puro (~100 righe di pre/post-processing, o un sidecar Node) non è
  stimato in ore, solo identificato.
