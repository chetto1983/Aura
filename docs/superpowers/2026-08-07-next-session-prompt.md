# Prompt per la prossima sessione

Riprendi la riscrittura della pipeline documenti di Aura. La pipeline **funziona già end to
end**: un file su Garage viene ingerito da solo, l'agente lo trova e il modello locale
risponde. Quel che resta è **togliere la complessità che non serve a nessuno** — non
aggiungere niente.

Leggi PRIMA questi tre file, in quest'ordine:

1. `docs/superpowers/2026-08-07-document-pipeline-rewrite-handoff.md` — stato, decisioni
   già prese, e i fatti che NON vanno riderivati
2. `docs/superpowers/plans/2026-08-06-document-pipeline-rewrite.md` — il piano
3. `.superpowers/sdd/2026-08-06-document-pipeline-rewrite/progress.md` — il ledger: leggi
   la sezione finale `=== SESSION 2026-08-07 (second) ===`, che contiene l'inventario di
   cancellazione già verificato e l'ordine corretto

**Sei su `master`, HEAD `bdb3406c3`, già pushato, CI verde** (CI + Skills + CodeQL), albero
pulito. Il branch `feat/document-pipeline-rewrite` è stato mergiato: non ripartire da lì.

---

## Quattro regole che questa sessione ha pagato care

- **Non inventare. Solo documentazione ufficiale e il pacchetto INSTALLATO.** La
  documentazione pubblicata di cocoindex descrive un'API diversa da quella installata.
  Prima di scrivere una chiamata: `dir()`, `inspect.signature()`, `__all__` dentro il
  container. Tre volte in due sessioni un componente bespoke è stato autorizzato con la
  motivazione "la libreria non lo fa" quando la libreria lo faceva.
- **Il riferimento è hermes-agent**, in `D:\tmp\hermes-agent`. Per un tool, si copia il suo
  e si traduce in Go. Non si progetta un'alternativa.
- **Cerca il pacchetto prima di scrivere il codice.** Il matcher fuzzy a 9 strategie scritto
  a mano è stato cancellato e sostituito da `github.com/sergi/go-diff/diffmatchpatch`. Nota
  però che i pacchetti vanno scelti per **categoria**, non per nome: `lithammer/fuzzysearch`
  e `sahilm/fuzzy` fanno subsequence-ranking per autocomplete e NON localizzano un blocco
  multi-riga.
- **Niente è finito finché un documento vero non attraversa la catena sullo stack acceso**, e
  ogni misura dichiara **cosa NON dimostra**.

---

## Cosa è già fatto — non rifarlo

- **Task 5** (`c8e9df49b`) — `services/ingest/arcade.py`, solo `ensure_schema()`. Le righe le
  scrive il target stock di CocoIndex, non noi.
- **Task 6** (`4bb5049a3`) — l'app CocoIndex che riconcilia Garage→ArcadeDB.
- **Task 7** (`43999e5af`) — `aura-llm` serve Qwen3.5-9B-UD-Q4_K_XL **locale** da path
  pre-fetchato, `-c 32768`, verificato su GPU (VRAM 1088→7211 MiB, 45 tok/s).
- **Il cerchio** (`8ab4dbf85`) — file su Garage → pickup automatico in ~19s senza trigger →
  Passage con `source_key` → domanda → risposta corretta dal modello locale.
- **I tool file** (`ce5927c07`) — i 5 `fs_*` cancellati, i 4 di hermes portati
  (`read_file`, `write_file`, `patch`, `search_files`, con `target` che assorbe glob+grep).
  `document_index` e `document_describe` cancellati con loro. Il fuzzy di `patch` usa
  `sergi/go-diff v1.4.0`, non un matcher scritto a mano.
- **Il set attivo è 12**, allineato a quello dell'agente di riferimento:
  `ask_user document_open document_search patch read_file read_tool_output search_files
  send_file shell_exec text_response tool_search write_file`. `todo_write` e `web_search`
  restano deferred **perché lo sono anche nel riferimento**. `skill` e `swarm_spawn` sono
  nel set del riferimento ma restano deferred **per peso**: 1.638 e ~1.100 token di
  descrizione contro poche righe. Tre guardie lo impongono (`always_active_test.go`,
  `send_file_test.go`, e il gate cache-invariant che verifica `skill` deferred su 23
  richieste). Copiare il set è metà del lavoro; i byte sono l'altra metà.
- **La copertura** (`bdb3406c3`) — 85,2% (26576/31207) misurato con
  `scripts/coverage_docker.sh` sullo stack vivo. `internal/agent/tools` da 74,7% a 86,2%.

---

## Difetti trovati spingendo. Non sono chiusi tutti, e si ripresentano

Quattro gate dichiaravano di girare **senza girare**. Se un gate è sospettosamente veloce,
non è cache: guarda quanto ci mette davvero.

- **`tagged-tier-compile` non partiva** dal pre-push. Segnava 0,4 s contro 61 reali, e
  lefthook stampa il tempo nel sommario, quindi si leggeva come cache calda. Causa: MSYS.
  `git grep -lz '^//go:build '` non matcha **niente** su Git-for-Windows, perché il pattern
  inizia con `//` e viene riscritto come path UNC prima che git veda l'argomento — lo
  stesso mangling che impone `MSYS_NO_PATHCONV` su ogni `docker run` qui. Misurato: forma
  ancorata 0 file, `^\/\/go:build ` e `-F '//go:build'` tutti. Quale gamba girasse dipendeva
  da **come il chiamante scriveva la root**. Corretto (`426fca836`), ma la lezione resta.
- **`.gitignore` ignorava l'intero frontend** (`web/` invece di `web/node_modules/`).
  eslint, tsc e prettier passavano — leggono il filesystem, non l'indice — mentre knip,
  che rispetta `.gitignore`, vedeva un progetto vuoto e dichiarava inutilizzate tutte e 32
  le dipendenze, `cytoscape` compresa mentre un file la importa alla riga 2. Corretto.
- **`scripts/quality_snapshot_gate.sh` chiama `python`**, che WSL non ha (solo `python3`).
  Oggi risolto con uno shim in `~/.local/bin`: **lo script va sistemato**, non è chiuso.
- **`coverage_docker.sh` su WSL richiede `poppler-utils`**. Senza `pdftotext` tre test di
  `filecard` falliscono e il gate si ferma **prima** di calcolare la copertura — un rosso
  che non c'entra nulla con la copertura. La CI lo installa; l'ambiente locale no.

Errore di diagnosi da non ripetere: sul primo avevo "normalizzato" il confronto dei path,
che sembrava la correzione ovvia e invece spostava solo la discovery sulla gamba rotta.

## Il lavoro: cancellare, non aggiungere

### 1. Il piano documenti in Go — ~10.000 LOC

Inventario **già verificato**, non rimisurarlo (fonte: `spikes/cocoindex-ingestion/FINDINGS.md` §4):

| gruppo | LOC | destino |
|---|---|---|
| `pipeline_worker`, `pipeline_store*`, `jobs_*`, `delete_durable_*`, `events_store`, `orphans`, `retry_backoff`, `job_context`, `docling_*`, `pipeline_types`, `pipeline_artifact_cache` | 9.024 | via |
| `internal/arcadedb/document_projection*.go` | 978 | via |
| catalogo, retrieval, API, scoping per identità | 3.563 | **resta** |
| `internal/documents/filecard/` | 2.592 | **resta** |

**L'ordine è vincolante e non è quello dei nomi dei file.** Docling NON è isolato: 10 file
fuori dal suo gruppo lo referenziano, fra cui `internal/assets/document_processor.go` e
`cmd/aura/asset_processing_worker.go`, che stanno fuori da `internal/documents`. Si
cancellano **prima i consumatori** (`pipeline_worker`), **poi** il client. Build + vet +
test fra un gruppo e l'altro; mai un gruppo cancellato a metà attraverso un commit.

Il versioning va via davvero: ogni query è `WHERE active = true`
(`document_retrieval.go:186`) e **nessuna query legge mai un passaggio tombstonato** — la
storia si scrive e non si rilegge. La riconciliazione di CocoIndex garantisce per
costruzione quel che il tombstone provava a tracciare.

### 2. `skill` — 10 azioni dietro un enum

`skill` costa **1.638 token, il 12% di OGNI prompt**, perché multiplexa
`list/info/use/install/create/update/delete/save_snippet/restore/archive` dietro un solo
campo `action`, con un paragrafo di prosa per ognuna. Solo lo schema dei parametri è 747
token.

Il riferimento ha due parametri: nome e argomenti. Lo split naturale è `skill` per
leggere/usare e `skill_write` per l'authoring — e `internal/agent/tools/skill_write.go`
(293 LOC) **esiste già**, semplicemente non è esposto come tool separato.

Stessa cosa per `swarm_spawn`: `swarmSpawnDescription` è 4.364 caratteri (~1.100 token).

Entrambi sono nel set attivo del riferimento e qui restano deferred **solo per peso**.
Quando le descrizioni scendono alla dimensione del riferimento, si promuovono e si estende
`always_active_test.go`, che pretende una motivazione scritta.

### 3. Residui

`AURA_DOCLING_*` (5 voci incl. il digest pinnato), `AURA_RERANK_*`, e in `prd.md`
`AURA_DOCUMENT_CHUNK_MAX_TOKENS=512` che è dell'era Docling mentre il tetto vero è **2048**,
dichiarato dal GGUF di EmbeddingGemma. `aura-rerank` **non esiste già più** nell'albero: non
cercarlo.

---

## Come si chiude

Il gate dell'emendamento #115, sopra il 98%, guidato dall'agente vero. Una suite unitaria
verde non chiude niente.

**Vincolo misurato che decide come si esegue il gate:** in catch-up le eccezioni propagano
(exit 3, fail-closed); in live `auto_refresh` le inghiotte e il ciclo dopo riparte
(fail-open, è nel suo docstring). **Il gate DEVE girare il sidecar in catch-up**, o
un'estrazione fallita passa verde — esattamente lo skip-as-green che il gate esiste per
impedire.

**Prerequisito ancora aperto:** `.env` punta ancora a OpenRouter
(`AURA_LLM_BASE_URL=https://openrouter.ai/api/v1`). Va ribaltato sul locale per il gate. È
il file vivo dell'operatore: chiedi prima di toccarlo.

**Il test che conta**, e che non è ancora stato fatto: ≥6 documenti veri con **distrattori**
che condividono vocabolario, domande naturali che **non nominano** né il file né il suo
contenuto, più una domanda **senza risposta nel corpus** per verificare l'astensione. Il
test già fatto nominava il documento nella domanda, quindi il retrieval non poteva
sbagliare: prova molto meno di quanto sembri.

---

## Ambiente

- Lavora **dentro il container `aura`** (`docker exec aura sh -lc '…'`): path Linux, tutto
  raggiungibile — garage 403, arcadedb 204, aura-llm 200, embed 200 — e nessun problema di
  path MSYS.
- I gate `.sh` girano in **WSL**, mai in Git Bash.
- `MSYS_NO_PATHCONV=1` su ogni `docker run` con un path assoluto; `-i` per un heredoc o lo
  script viene scartato in silenzio e sembra un blocco.
- **Mai `find /`**: due subagent hanno lasciato ricerche orfane al 25% di CPU per ore.
- Un modulo Python nuovo è invisibile finché non ricostruisci
  `docker build -t aura-ingest:local -f docker/aura-ingest/Dockerfile .`
- Un fallimento su Windows con permessi `0600` vs `0666` è un artefatto dell'ambiente:
  verifica in WSL prima di chiamarlo regressione.
- `docker/agent-memory` (77 file del sidecar Neo4j ritirato) è sul disco ma **gitignorato**:
  fa fallire `TestRetiredGraphPlaneStaysOutOfTheComposeDeclarations` in locale e passa in
  CI, dove un checkout pulito non ce l'ha. È spazzatura cancellabile.
- Prima di dire "l'ha rotto il branch", riproduci su un worktree di `origin/master` — e
  controlla che la copia abbia davvero `node_modules`, o il verde è vacuo (mi è successo).
