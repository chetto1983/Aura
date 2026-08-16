# Aura — handoff unico

**Aggiornato: 2026-08-16.** Sostituisce dodici handoff sparsi (`docs/superpowers/*-handoff.md`,
`.planning/handoffs/*`, `docs/handoff-2026-08-13.md`), cancellati il 2026-08-13.

---

## 0. Lavorato il 2026-08-16

**0.2 è CHIUSA** (`67b64b144`), e la ricetta che questo documento prescriveva per chiuderla
**era sbagliata per eccesso**. La correzione vale più della chiusura, perché quella ricetta
era già scritta e qualcuno l'avrebbe eseguita.

**Misura prima → dopo, stessa domanda al vero agente sullo stack acceso** ("cerca nei miei
documenti che cosa dicono a proposito dei footnotes, e dimmi da quale file viene la
risposta"):

```
document_search title    bc4c9304-…-0882a03ea1a5.pdf  →  colm2025_conference.pdf
prima riga della card    tmptq9teunw.pdf — PDF, 119 KB →  colm2025_conference.pdf — PDF, 119 KB
prima frase dell'agente  "il PDF bc4c9304-…"           →  "viene da colm2025_conference.pdf"
```

**Cosa la vecchia §0.2 sbagliava.** Diceva "non è una riga: il fix tocca quattro punti su
due linguaggi" — campo nome sulla riga Passage in `app.py`, DDL in `arcade.py`, struct
`PassageCandidate` + proiezione in Go, fallback in `retrieval_rank.go`. (Il re-ingest
dell'intero corpus non lo diceva, ma un campo nuovo sulla riga Passage lo impone: senza
backfill i passaggi già scritti restano senza nome.) Nessuno dei primi tre serviva. Un Passage rispecchia il **chunk**, non
l'oggetto; il nome sta già in `IndexedDocument`, **nello stesso database**, indicizzato
UNIQUE sulla stessa `search_document_id` che ogni candidato porta con sé — ed è esattamente
la riga che `document_open` leggeva da sempre (`DocumentByID`). Denormalizzare il nome su
ogni chunk avrebbe comprato ciò che una `SELECT … WHERE search_document_id IN :ids` già
risponde. Il fix è `DocumentNames`, chiesto **solo** per i documenti che la gamba card non
ha classificato (quelli classificati il nome ce l'hanno già), e la coda della chiave resta
l'ultima risorsa — dove è ancora giusta, per un oggetto lasciato cadere nel bucket.

**Perché non da Postgres** (era la prima ipotesi ragionevole, ed è misurabilmente falsa):
`aura.assets` ha **0 righe** per la chiave della Perizia e `aura.documents` ne ha **1 in
tutto**, mentre il retrieval vede **7 documenti**. I documenti riconciliati dal bucket
(`test/Costituzione_…pdf`) non hanno **mai** una riga Postgres — è il punto della
riscrittura del 08-08. Sarebbe stato un fix per un sottoinsieme, con una join cross-store
dentro il percorso di ricerca.

**Secondo difetto, stessa famiglia, trovato provando il primo.** La card si apriva col nome
del temporaneo convertito (`tmptq9teunw.pdf`) mentre `file_name` accanto era già giusto.
`app.py` passa a filecard un nome che fa **due lavori**: instrada (`Request.ext()` preferisce
`FileName` a `Path`, quindi un `.ods` convertito e cardato come `.ods` ricade in "file, 12
KB") **e si vede** (quella prima riga è indicizzata e riletta dall'agente). Serviva solo il
primo. Ora `_card_name()` — funzione pura, 6 test — dà lo stem vero sotto l'estensione
convertita.

**Cosa questa misura NON dimostra:**
- **Le righe pre-fix restano con nomi uuid.** `0a0347dc-….md` e i tre `.png` hanno
  `IndexedDocument.file_name` = uuid perché sono stati ingeriti prima che il canale
  metadati esistesse (`a5afccfc3`), e i loro oggetti su Garage non portano il metadato:
  un re-ingest non li ripara. Il nome vero per quei quattro **esiste in
  `aura.assets.file_name`** — è l'unico caso in cui Postgres è la fonte giusta. Riparazione
  non fatta, decisione non presa.
- Un'identità, un corpus di 7 documenti, una query. Non è una misura di recall (§3.3 resta
  UNKNOWN e questa non la tocca).
- Il ramo di fallimento di `DocumentNames` è coperto da unit test, non esercitato vivo.
- Non tocca §6.7 (immagini e audio senza famiglia propria).

**Fixture lasciata nel corpus vivo**: `colm2025_conference.pdf`, chiave
`chat/b4e391e0-6141-4807-b8e5-88ca58f21162.pdf`, identità operatore. Serve a ri-verificare;
una `delete_object` la rimuove e CocoIndex cancella la riga da sé (verificato sostituendo la
chiave precedente).

**Trappola pagata oggi**: l'immagine `aura-ingest:local` era **11 minuti più vecchia** del
commit che stava verificando. Senza ricostruirla il difetto della card sarebbe stato
archiviato come dato stantio invece che come bug vivo. Ricostruire prima di misurare, sempre.

---

## 0-bis. Lavorato il 2026-08-13 (sera)

Su `gsd/phase-45-harness-correctness`, **non pushato, non mergiato** (il branch porta anche
lavoro fase-45 di una sessione parallela, quindi non è mergiabile così com'è).

| Voce | Stato | Prova |
|---|---|---|
| **2.3** test `db_integration` sul DB vivo | **CHIUSA** (`11f49cd9b`) | Tier intero verde con il DB vivo come bersaglio e il suo stato identico prima/dopo (version=95 dirty=f, identities=2 documents=1 jobs=0), zero identità di test in produzione, zero database usa-e-getta rimasti. Erano **8** test, non 4: `digestSearchPool` scriveva in produzione senza migrare. |
| **2.2** gate Content-Type file-manager | **CHIUSA** (`6104cc49b`) | E2E nel browser reale contro il container: create 200 e delete 200 entrambi `application/json`; la stessa create ri-etichettata `text/plain` sul filo → **400**, niente creato. Causa: `RestDataProvider.send` sovrascrive `Rest.sendRequest` e ne perde il default. |
| **4.5** nome dell'allegato | **CHIUSA** (`a5afccfc3` + `146ea83dc`) | Vedi §0.1. |
| **CORS bucket per-identità** (non nel handoff) | **CHIUSA** (`146ea83dc`) | Vedi §0.1. |

**0.4 — Il piano documenti è stato potato: 6.965 → 5.833 LOC non-test.**
Principio applicato (decisione operatore): *Aura mette i byte in Garage e traccia il nome; tutto
il testo lo fa CocoIndex*. Quindi niente chiamanti = si cancella. Via in 16 commit: le 18 query
dello stage machine (incl. `ActivatePipelineCandidate`, **l'unica che scriveva `status='ready'`**,
senza chiamanti — ecco perché nessun documento diventava mai pronto), il sottosistema digest
Postgres, lo store eventi di ingestione, il workflow di delete asincrono con le sue 9 statement,
il CRUD del catalogo, il riconciliatore di orfani. `CatalogStore` da 6 metodi a 1.

Trovati **vivi** e tenuti, ognuno per una ragione misurata: `ReservePipelineCandidateVersion`
(via `RecordAssetVersion`), `CreateDocument` (via l'interfaccia `IngestCatalog`, non via
`CatalogService`), la famiglia `IngestionJobWorker`, `SupportedDocumentExts` (solo un test lo
chiama, ma quel test guarda un'invariante di produzione vera: la deriva fra allowlist assets e
documents faceva 400 silenziosi su pptx/html), `DocumentScopeThread` (nessun riferimento Go, ma
`document_version_recorder.go` produce quel valore), e `Job.SparseChunks` (senza scrittore ma
ancora letto).

**Due colonne restano write-only, decisione non presa:** `aura.document_tags` (il lettore era
`ListDocumentTags`; lo scrittore ha ancora un chiamante) e `aura.storage_objects` (scritta dentro
`ReservePipelineCandidateVersion`, nessuno la rilegge). E `aura.documents.card` idem. **Nessuna
migration di drop**: le tabelle orfane stanno in piedi vuote, di proposito.

**0.2 — CHIUSA il 2026-08-16 (`67b64b144`) — vedi §0. La diagnosi qui sotto è corretta; la
ricetta in fondo alla voce NO, ed è superata: i primi tre dei suoi "quattro punti" non
servivano.** Testo d'origine conservato perché la diagnosi resta il modo giusto di leggere il
difetto.

**0.2 — Il nome è indicizzato ma NON arriva agli occhi dell'agente. Diagnosi completa, fix non fatto.**
Guidando l'agente vero (`document_search`, stack acceso) risponde con la **chiave** e non con il nome:
`05129905-ee6b-45df-b6bd-75b2a7b0bad5.txt (contenuto: "Perizia città di Ghèdi 2026")`, mentre
`IndexedDocument.file_name` per quella `source_key` è già `Perizia città di Ghèdi 2026.txt`.

Causa, letta nel codice: un documento che arriva dalla **gamba passaggi** (nessuna card in match)
passa da `ensureRankedDocumentFromCandidate`, che fa
`doc.document.Title = path.Base(candidate.SourceKey)` (`retrieval_rank.go:129`). La gamba card
invece popola `Title` correttamente (`retrieval_cards.go:57`). `document_search` non formatta
nulla: serializza `RetrievalDocument` così com'è (`document_search.go:85`).

~~**Non è una riga**: `arcadedb.PassageCandidate` (`document_retrieval.go:65-86`) non ha alcun campo
nome — i record Passage non lo portano. Il fix tocca quattro punti su due linguaggi: il campo sulla
riga Passage in `services/ingest/app.py` (il nome ce l'ha già, `file_name`), il DDL in
`services/ingest/arcade.py`, la struct `PassageCandidate` + la proiezione della query in
`internal/arcadedb/document_retrieval.go`, e infine il fallback in `retrieval_rank.go`.~~

**Ricetta SBAGLIATA, non eseguirla.** Vera la premessa (`PassageCandidate` non ha campo nome),
sbagliata la conclusione: da lì non segue che il nome vada messo *sul passaggio*. Sta già in
`IndexedDocument`, stesso database, chiave `search_document_id` che il candidato porta già —
un `SELECT … IN :ids`, zero DDL, zero re-ingest. Vedi §0.
Prova di chiusura (eseguita): la stessa domanda all'agente deve dire il nome, non l'uuid.

**0.3 — 6.6 resta valida.** Avevo scritto che la premessa era falsa perché `document_search`
risponde: non regge. L'agente ha chiamato `document_search` **tre volte** per compilare la lista —
rastrella, non elenca, perché non esiste un `document_list`.

**0.1 — Il nome viaggia, e per farlo viaggiare è emerso che il browser non caricava affatto.**
Il canale metadati è completo e testato da entrambi i lati (Go `PlaceAsset` + percent-encoding,
sidecar `decode_file_name` + `head_object`; CocoIndex non ha superficie metadati — enumerata:
`S3File` espone solo `content_fingerprint/file_path/read/read_text/size`). Misurato su Garage:
i metadati sopravvivono, ma **S3 li ammette solo in ASCII**, quindi gli accenti vanno codificati.

Guidandolo è emerso un difetto **più vecchio e più grave, non in questo documento**: *nessun
upload da browser funziona*. `ConfigureBrowserUploadCORS` gira solo su `cfg.ObjectStoreBucket`
(il bucket condiviso); i bucket per-identità introdotti dallo split "un bucket per identità"
non ricevono mai la regola. Prova: in `aura.assets` **ogni riga `web` è ferma a `presigned`**,
comprese due del **2026-08-08**, mentre ogni riga `agent` (Put server-side, niente browser)
è `accepted`; e un preflight non autenticato sul bucket per-identità dà **403 anche per il
solo `content-type`**, senza alcun header nuovo di mezzo.

Il fix è l'hook `ensureCORS` su `objectStoreProvisionAdapter`, sul mint **e sul resolve** (i
bucket rotti esistono già: riparare solo il mint non avrebbe riparato nulla), una volta per
bucket per processo, fail-soft con WARN.

Garage però gatea `PutBucketCors` dietro il permesso `owner`, che `garageadmin.ReadWrite` nega
all'identità **di proposito** (`types.go`: *"an identity must not be able to re-grant or delete
its bucket via the S3 data plane"*). La scorciatoia da un carattere avrebbe sfondato quel
confine; l'ownership va invece alla chiave **di Aura**, il processo che il bucket lo crea, e
l'identità resta read+write. `TestOnlyAurasOwnKeyIsEverGrantedBucketOwnership` lo asserisce
invece di affidarlo al commento.

**Misurato prima → dopo sul deployment vivo:**
```
preflight (PUT, content-type + x-amz-meta-filename)   403  →  200
ultima riga asset 'web'                         presigned  →  processing
IndexedDocument.file_name    <assetID>.txt (derivato)  →  'Perizia città di Ghèdi 2026.txt'
```
Il primo upload da browser mai completato su questo deployment. La chiave resta
`chat/<assetID>.txt`, senza nome: il confine anti-leak tiene, il nome viaggia accanto.
`web/e2e/attachment-filename.spec.ts` lo fissa e asserisce esplicitamente lo stato `Failed`,
perché è quello che la CORS mancante produceva mentre un'attesa nuda di `Ready` restava muta.

Ogni voce qui sotto è stata **verificata contro il codice di oggi**, non copiata dal documento
d'origine. Due terzi di ciò che quei documenti chiamavano "aperto" era già chiuso — spesso perché
il codice a cui si riferiva è stato cancellato. Quella lista sta in fondo, e serve a impedire che
qualcuno riapra lavoro già fatto.

Dove una voce non è verificabile dal repo (stato di un database vivo, comportamento dell'agente)
è detto esplicitamente. **Un documento che afferma qualcosa non è una prova.**

---

## Stato

Produzione su e sana. Misurato il 2026-08-16, e **tre affermazioni di questa sezione erano
scadute in tre giorni** — conferma che qui il `git branch` vince sul testo:

- `master` non è più la sola branch pubblicata: su origin ci sono anche
  `gsd/phase-45-harness-correctness` (pushata il 2026-08-15, quindi il "**mai pushato**" che
  stava qui è falso), `fix/backup-postgres-arcadedb` e `fix/unify-credential-redaction`.
- La fase 45 è **chiusa su master** (`2585ea4be`, tre waiver registrati). Non è più lavoro in
  corso di una sessione parallela.
- `master` ha **5 commit non pushati**. Restano quattro ref `worktree-agent-*` del 2026-08-13
  senza worktree attaccato.

**Una sessione parallela lavora sullo stesso checkout.** Conseguenze pagate oggi: commit con
pathspec esplicito (l'index conteneva `.planning/phases/45.1-*` non miei) e un
`.git/index.lock` **stantio** di 447 KB rimasto da un processo git morto — rimosso solo dopo
aver verificato che nessun git fosse vivo su Windows né in WSL.

---

## 1. Regole del progetto che il codice non rispetta

Queste vengono prima di tutto perché una regola falsa è peggio di una regola assente: fa fidare.

**1.1 — CLAUDE.md dichiara un floor di coverage per-package che non esiste.**
Il testo dice che il ≥85% per-package è *"enforced by the gate on every run"*.
`scripts/coverage_gate.sh:86-90` somma `covered`/`total` su **tutto** il profilo e `:96-106`
confronta quell'unico aggregato con `MIN`. Non c'è nessun ciclo per package. È così che un
package debole viaggia sulle spalle dei forti — `internal/arcadedb` è stato misurato al 36,6%.
*Confidenza: alta. O si implementa il ciclo, o si corregge CLAUDE.md.*

**1.2 — ADR 0038 è `Accepted` e descrive un datastore che non usiamo.**
`docs/adr/0038-…:19` dice che Neo4j tiene il grafo e che `mcp-neo4j-cypher` è l'interfaccia LLM;
`:69` "Stay on Neo4j Community". Realtà: `compose.yaml:515` monta `arcadedata/arcadedb`, non
esiste un servizio Neo4j, `internal/knowledge/` è cancellato, l'interfaccia viva è
`cmd/arcadedb-mcp`. `grep -rln "Superseded" docs/adr/` → **zero file**. ADR 0042 lo contraddice
in una parentesi (`:19`) invece di superarlo formalmente.
*Confidenza: alta. È l'unica contraddizione dove il codice ha già votato.*

**1.3 — Il budget di token per chunk dice 512 in tre posti e ne vale 2048.**
`compose.yaml:216`, `CLAUDE.md:24` e `.planning/codebase/INTEGRATIONS.md:24` dicono 512;
il valore che gira è `MODEL_MAX_TOKENS = 2048` in `services/ingest/chunk.py:43`, e **nessun
codice Go legge quella env var**. *Confidenza: alta.*

---

## 2. Sicurezza e isolamento

**2.1 — Tre piani condivisi restano non-scoped; il multi-utente è rifiutato da una guardia di boot
invece che risolto.** `internal/config/config_validate.go:110-145` nomina i tre: skills,
`aura.settings`, catalogo MCP. `internal/skills/identity_root.go:61`
`NewSkillToolForIdentity` ha **zero chiamanti di produzione**. `aura.settings` ha PK sulla sola
`key` (`0024_settings.up.sql:15`) e `0087_rls_fail_closed.up.sql:248` la esclude esplicitamente
da RLS — **e contiene `OPENROUTER_API_KEY`**. *Confidenza: alta.*
*(Il piano tool è invece genuinamente chiuso: `SandboxRouter.Route` contiene su ogni profilo.)*

**2.2 — Le scritture del file-manager non hanno il gate sul Content-Type.**
`internal/agui/files_api_write.go:83-98` — il commento stesso dice "Decoded WITHOUT a
Content-Type gate, unlike every other write on this server". Difesa in profondità CSRF
assottigliata rispetto a ogni altra scrittura del server. *Confidenza: alta.*

**2.3 — `migratedDocumentPool(t)` migra qualunque database gli env puntino, ed è usato da quattro
test.** `internal/documents/store_integration_test.go:29` legge `AURA_DB_MIGRATE_URL` e chiama
`db.Migrate`. L'alternativa sicura è **accanto**: `integration_pool_helper_test.go:23`
`pipelineDisposablePool`. **Io oggi ci sono cascato**: un run `db_integration` puntato al DB vivo
gli ha applicato la migration 0095 fuori da un deploy. *Confidenza: alta. Il fix è una riga per
test.*

---

## 3. Misure dovute e gate che non chiudono

**3.1 — Il gate di produzione #115 non è mai stato eseguito e il suo runner è cancellato.**
`scripts/document_pipeline_e2e.sh` rimosso in `7847ecc29`. Il ledger
`docs/superpowers/verification/2026-08-05-…/FINDINGS.md:206-212` è `_pending_` su CP1–CP7.
Ma `prd.md:4619` (emendamento #118) **mantiene** il gate e `:4621` fa di "score sopra 98%" una
condizione di chiusura. `scripts/ingest_reconcile_e2e.sh` non è un sostituto e lo dice nella
propria intestazione. *Confidenza: alta. È l'unica cosa che non è mai stata fatta, contro molte
che sono state fatte e poi cancellate.*

**3.2 — La riga snippet-reuse è ROSSA e non è più cancellabile.**
Ultima misura **FAIL 2026-07-30** (7 e 10 dispatch contro un tetto di 6); le ri-attestazioni di
agosto la preservano e rifiutano di promuoverla — correttamente. Ma l'harness che portava le
asserzioni, `internal/eval/skills_snippet_reuse_cot_eval_test.go`, è stato cancellato in
`acd029d47`. Il comando che lo snapshot stampa a `:467` punta a un package inesistente.
**O si ritira la riga come le sorelle, o si ricostruisce l'harness.** *Confidenza: alta.*

**3.3 — `recall@1` del routing documentale è UNKNOWN e nulla lo misura.**
`docs/aura-quality-snapshot.md:62` non porta nessun numero. I due harness esistono
(`internal/documents/retrieval_fusion_bench_test.go`, `retrieval_abstention_eval_test.go`) e
**vengono compilati** — `scripts/tagged_tier_compile.sh:30` intercetta ogni tag che finisce in
`_eval`, quindi non possono marcire in silenzio. Ma **nessuno li esegue**: né CI, né Makefile, né
script. Il repo lo dice già di suo, alla lettera, in `.planning/codebase/TESTING.md:136`.
Attenzione a un falso indizio: il `retrieval_eval` che compare in
`docs/aura-quality-snapshot.md:21` è un harness RET-05 del 2026-06-28 sotto `internal/eval/`,
albero cancellato il 2026-08-02 — non è prova che il recall documentale sia stato misurato.
Peggio ancora, due delle tre leve citate dalla riga sono morte: `AURA_DOCUMENT_OCR_ENABLED` ha
zero occorrenze nel codice e Docling è stato rimosso.
*Confidenza: alta. L'apparato di misura esiste e compila; il numero non lo produrrà mai nessuno
finché le cose stanno così.*

**3.4 — Il North-Star xlsx delle skills non ha più l'harness Go.**
`internal/eval/skills_cot_eval_test.go::TestSkillsE2E` cancellato in `acd029d47`.
Sopravvive `scripts/chat-e2e-gate.sh`, invocato da nulla. *Confidenza: alta.*

**3.5 — La suite LOCOMO non gira e il dataset non è provisionato.**
`scripts/agent_memory_eval.py:52` la salta con `-skip "^TestLocomo…"`;
`internal/arcadedb/locomo_test.go:86-87` vuole `AURA_LOCOMO_DIR`, che nessun workflow imposta.
*Confidenza: alta.*

**3.6 — Release readiness: 7 report su 10 assenti, i 3 presenti sono stali.**
`artifacts/production-readiness/` ha solo audit-closure, capability-eval e coverage, generati il
2026-08-07 e legati a commit diversi da HEAD. Il gate li vuole tutti e dieci, legati a HEAD e
freschi di 24h (`scripts/release_readiness_gate.py:255-264`). *Confidenza: alta.*

**3.7 — 5 righe di audit restano aperte, tutte a blocco esterno.**
Verificato oggi eseguendo il gate: `5 current unresolved, 5 external, release_ready=false`.
Sono account calendario/email, QR WhatsApp, versione GHCR, ricevuta `send_file`. **Non è lavoro
di codice.** *Confidenza: alta sull'apertura, nulla sulle cause.*

---

## 4. Difetti veri, piccoli

**4.1 — `ReasoningClassifier.generation` è letto e confrontato ma mai assegnato.**
`internal/agent/prompt/reasoning_classifier.go:108` dichiara, `:172` legge, `:181` confronta —
**nessuna assegnazione in tutto l'albero**. La guardia di invalidazione del rebuild è inerte.
*Confidenza: alta. Questo non è codice morto: è una protezione che non protegge.*

**4.2 — Un turno utente senza risposta resta nella storia e viene rimandato per sempre.**
Misurato oggi: conversazione `019ffabe-…`, seq 76 e 77 identici a 3 minuti di distanza, senza
assistant in mezzo. Il primo run è fallito, l'utente ha rimandato. Niente in
`internal/conversations/` fonde o scarta due user consecutivi, quindi il modello riceve la stessa
domanda due volte a ogni turno successivo. *Confidenza: alta.*

**4.3 — `_embed` del sidecar ingest inghiotte il messaggio del server.**
`services/ingest/app.py:196-208` — `urlopen` senza try/except: un HTTP 500 diventa un `HTTPError`
nudo, senza corpo né conteggio token. È la forma esatta che è costata due riproduzioni il
2026-08-09. *Confidenza: alta.*

**4.4 — `ErrDocumentDeleteInFlight` non è instradato al bordo API.**
Prodotto a `internal/documents/catalog_store.go:120` su un percorso vivo
(`service.go:130`), nessun `errors.Is` in `internal/agui/` o `cmd/aura/`. *Confidenza: alta.*

**4.5 — Un allegato di chat arriva come `<assetID>.pdf`.**
`internal/objectstore/types.go:104-106` costruisce la chiave senza il nome reale, e
`services/ingest/app.py:263` ricava il nome dalla chiave. Non esiste un canale metadati:
`Attrs` (`types.go:19-27`) non ha un campo nome e `s3.go:127` non imposta user metadata.
*Confidenza: alta.*

---

## 5. Residuo di cancellazione (codice morto lasciato indietro)

La riscrittura del 2026-08-08 (dodici commit, `14a0d1ae8`→`29bbe0cf9`) ha eliminato la pipeline
in-process, il modello a versioni, Docling, lo store di retrieval Postgres e il delete worker.
Questo è ciò che è rimasto attaccato:

- **13 delle 14 query in `internal/db/queries/document_pipeline.sql` non hanno chiamanti**, inclusa
  `ActivatePipelineCandidate` (`:458`) — l'unica istruzione che scrive `status='ready'`. Più le 5
  legacy in `document_control_plane.sql:421,434,445,456,463`.
- **`SearchDigests`/`SearchDocumentDigests`** (`catalog_store_digest.go:99`): solo test. E il card
  Postgres è ormai **write-only** — `service.go:162` lo scrive, il lettore è ArcadeDB
  (`retrieval_cards.go:41`).
- **4 query storage-object senza chiamanti** (`CreateStorageObject`, `ListDocumentStorageObjects`,
  `MarkStorageObjectDeletePending`, `MarkStorageObjectDeleted`). Nota il punto cieco: i metodi
  sqlc soddisfano il `Querier` usato, quindi `deadcode` li vede raggiungibili.
- **Il vocabolario di stato è aspirazionale**: `catalog_status.go:15-24` dichiara
  `queued/converting/chunking/embedding/projecting`; **nessuno ha usi non-test**, e nemmeno
  `Ready`/`Failed`. L'unico stato che Go scrive è `Stored` (`service.go:133`).
- **8 simboli senza chiamanti non-test**: `cliOperationFromContext`, `recordLLMDuration`,
  `panicobs.Count`, `display.Normalize`, `normalizeSwarmStatus`, `agent.NewTracerProvider`,
  `(*idempotency.Store).RecoverExpired`, più il `generation` di §4.1.
- **`internal/documents` regge 4.373 LOC non-test**, di cui ~1.465 di superficie catalogo, e la
  migration di drop non è mai stata scritta. *Confidenza: alta sul fatto che sia intatto; media
  sul fatto che la rimozione sia ancora voluta.*

---

## 6. Lavoro progettato e non fatto

**6.1 — Slice 1b (compaction durevole).** `docs/context-compaction-1b-plan.md` esiste, ma **tre
punti del piano sono stati smentiti misurando** oggi:
- lo slot di migration nel piano è vecchio — **leggere `ls internal/db/migrations/ | tail -1`
  all'atterraggio**, 0095 è occupato;
- il §6 dice di iniettare il summary *"identical shape to `injectAlwaysBlock`"*: sbagliato,
  sloggerebbe l'always-block da `messages[1]` cambiando il prefisso cachato di ogni conversazione
  che compatta. Va derivato da `splitHeadHistoryActive` (head = system + always-block), **dopo**
  `injectAlwaysBlock`;
- `aura_app` ha già UPDATE su `conversation_turns`: il lavoro di grant del §3 non serve.

Inoltre: RLS è attiva su quella tabella (l'UPDATE va in `WithCallerIdentityTx`), e il trigger
`conversation_turns_snapshot_bump` di 0047 è **BEFORE INSERT OR UPDATE FOR EACH ROW** — un archive
di massa lo spara una volta per riga. Misurato su schema usa-e-getta: `snapshot_version` salta di
N, in **21,6 ms su 400 righe**, innocuo perché nessuno ne legge la grandezza. Lo stesso percorso
solleva `55006` se la conversazione è riservata a export-delete: la compaction deve degradare a
L2.5, non propagare.

La traduzione da scrivere è quella di hermes-agent: flag `active` + archive-and-compact in una
transazione. **Non copiare il suo secondo flag `compacted`**: là serve a una ricerca full-text sui
messaggi che Aura non ha, quindi qui sarebbe una colonna che nessuno legge.

**6.2 — Il piano grafo ha i dump ma nessun restore provato.**
`scripts/restore_drill.sh` dice ancora *"nothing dumps them, so there is no restore to rehearse"*.
La prima metà è falsa da oggi. Il restore ArcadeDB è una chiamata HTTP e funziona a server acceso,
ma il database di destinazione non deve esistere.

**6.3 — Il rilevamento osservabilità non avvisa nessuno.**
`scripts/observability_sidecar_check.sh` (aggiornato oggi, ora verifica anche che Prometheus stia
davvero **raschiando** Aura) non è agganciato a niente: né cron, né CI, né Makefile. Il cron di
Aura ha già `ReschedulesOnRecovery` e manda alert su Telegram — è lì che va.

**6.4 — Confidenza fusa senza consumatore.** `prd.md:6270+` ratifica *"confidenza bassa deve
spingere verso `document_open`, mai verso il silenzio"*. `internal/documents/retrieval_rank.go:62`
decide `RequiresOpen` da famiglia e zero-passaggi, **non** da `FusedScore`. È l'unico punto aperto
con un emendamento PRD già ratificato alle spalle. *Confidenza: alta.*

**6.5 — filecard scrive OOXML a mano** (`xlsx.go` 442 LOC, `ooxml.go` 270) mentre `excelize` non è
nemmeno in `go.mod`. *Confidenza: alta.*

**6.6 — `"che documenti ho caricato?"` non ha niente da chiamare.** Solo `document_search` e
`document_open` sono registrati; `cmd/aura/docs.go:201-217` elenca **job di ingest**, non il
catalogo. *Confidenza: alta.*

**6.7 — Immagini e audio non hanno una famiglia propria.**
`services/ingest/app.py:275-282` lo dichiara apertamente come seam lasciato aperto.

---

## 7. Contraddizioni fra documenti

1. **`.planning/STATE.md` è più recente di ciò che nega.** Committato alle 15:20 del 2026-08-13,
   dice *"Phase 45 has no CONTEXT.md"* e `completed_plans: 0` — ma `45-CONTEXT.md` era stato
   committato alle 11:27 dello stesso giorno, e su disco ci sono 8 PLAN, 3 SUMMARY, CONTEXT,
   DISCUSSION-LOG, PATTERNS, RESEARCH e VALIDATION.
2. **ROADMAP.md sottostima sé stesso**: dice "2/8 plans executed" e lascia 45-05 non spuntato, ma
   `45-05-SUMMARY.md` esiste e sei commit ne portano lo scope.
3. **Il corpus di 77 requisiti non è mai esistito.** ROADMAP.md:439-441 lo dice "already in hand" e
   REQUIREMENTS.md:7 lo cita come provenienza, ma `docs/audit/live-conversations-2026-08-04/` non
   c'è e `git log --all` non lo trova su nessun branch.
4. **REQUIREMENTS.md TOOL-11 è "Pending" ma è già costruito** — `search_files.go` unisce già
   nome e contenuto dietro `target:`; manca solo il rename in `fs_search`.
5. **ADR 0038 vs 0042** — vedi §1.2.
6. **Cinque cicli di riferimento incrociato** fra `prd.md` e i doc di `docs/superpowers/`
   bloccano ancora `/gsd-ingest-docs`.

---

## 8. Trappole pagate, da non ripagare

- **`docker compose restart` su un parent `network_mode: service:` orfana i figli per sempre.**
  Usare `up -d`. Costo misurato: 5h30 di metriche cieche. Risolto strutturalmente oggi, ma la
  regola vale per ogni altro accoppiamento `service:`.
- **`AURA_DB_MIGRATE_URL` puntato al DB vivo in un run `db_integration` lo migra davvero.**
  Creare un database usa-e-getta.
- **Un test che asserisce un letterale si rompe in package che non hai toccato.** Il cambio di
  segnaposto della redazione ha rotto sei asserzioni in quattro package: cercare il letterale su
  tutto l'albero, non solo dove hai modificato.
- **`.env` non è leggibile da queste sessioni** (guardia sui segreti) e
  `OPENROUTER_MANAGEMENT_KEY` sta lì, non passata a nessun container. Per usarla senza vederla:
  ```
  docker run --rm --env-file .env --entrypoint sh curlimages/curl:latest \
    -c 'curl -s -H "Authorization: Bearer $OPENROUTER_MANAGEMENT_KEY" https://openrouter.ai/api/v1/activity'
  ```

---

## 9. Sembrano difetti e non lo sono

- **Tre test falliscono su host Windows** e passano su Linux: `TestStageBoxArtifact_ExtractsRegularFile`
  (modi POSIX) e due verification-evidence che si aspettano `/tmp` dove `os.TempDir()` dà
  `C:\Users\...\Temp\`. Aura gira su container Linux: sono artefatti d'ambiente.
- **Il cache-hit basso è OpenRouter che distribuisce il modello su più provider**, non instabilità
  del prefisso. Tre spiegazioni sono state uccise dai dati prima di quella giusta: il prefisso è
  stabile (`prefix_drift` non scatta mai), il routing sticky è già inviato
  (`session_id == conversation_id`), DeepSeek cacha in automatico. Anche la TTL è caduta: un turno
  a 36 secondi dal precedente leggeva comunque 0%. L'API activity mostra **fino a 5 provider in un
  giorno**, ognuno con la sua cache. Fissare il provider è **deliberatamente NON fatto**: toglie il
  fallback quando quell'host è congestionato. Effetto collaterale della stessa misura, forse più
  importante del costo: **la quantizzazione varia per host** (`fp8` su alcuni, `fp4` su altri) e il
  primo-parte `deepseek/fp8` ha servito 2 richieste su 69 in un giorno.
- **`arcadedb_integration` "non gira da nessuna parte"** era falso **due volte** in due documenti
  diversi: gira nel job CI `arcadedb-integration-test`. Ciò che resta vero è solo che non alimenta
  la coverage.

---

## 10. Chiuso — non riaprire

| Voce | Cosa l'ha chiusa |
|---|---|
| Pipeline in-process, modello a versioni, Docling, retrieval Postgres, delete worker | Riscrittura 2026-08-08 (`14a0d1ae8`→`29bbe0cf9`) |
| Audit F1–F6 (versione durante delete, compensazione proiezione, replay stesso SHA, PII, cleanup, starvation) | `8d2701bd1`, `7cea13da4` |
| Vocabolario di stato rifiutato dal CHECK | `38d78e403` + test di conformità |
| `23505` su sorgente ripetuta | `7248b5880` (indice unico parziale) |
| Follow-through RLS (`IS NULL OR`, RESTRICTIVE) | migrazioni 0087, 0089, 0090, 0093, 0094 |
| `reasoning_live` non girava in CI | `ci.yml:906,978` |
| Nulla eseguiva i test dell'ingest | `d211cf117` + job `ingest-sidecar-test` |
| Asset finalize HTTP 400 | `488fa6714` |
| Web `documents/` con vocabolario stale | cancellato in `38e08c3db`, sostituito da `web/src/files/` |
| CocoIndex "valutato e parcheggiato" | Adottato — emendamento #118, `services/ingest/`, servizio `aura-ingest` |
| TOOL-07 (split skill use / lifecycle) | `internal/agent/tools/skill_manage.go` |
| Riga eval CoT live, GraphRAG recall@5, vector p95 | Genuinamente ritirate: il codice misurato non esiste più |
| Falsi positivi dark-code del 24-07 | `internal/agent/workflow/*`, `NewBudgetFromEnv`, `runServeComponents`, `mcptools.Bridge/Mount`, `recordSpanExportFailure` sono tutti cablati |
| **0.2** — l'uuid al posto del nome nei risultati di `document_search` | `67b64b144` — `DocumentNames` per id sulla sola gamba passaggi; provato prima/dopo sull'agente vero |
| La card che si descriveva col nome del file temporaneo | `67b64b144` — `_card_name()`: stem vero + estensione convertita, 6 unit test |

---

## 11. Non verificabile dal repo

Richiedono una sonda su database/object store vivi o un run reale dell'agente:
oggetti orfani in Garage e asset bloccati in `accepted`/`failed` (08-06); i 24 fatti e 48 entità
da ri-embeddare in `aura_memory`; i comportamenti dell'agente del 08-09 (salta il corpus, rifiuta
invece di aprire, 6 turni su 20 inconcludenti); le licenze dei due corpora — **nessuna pipeline le
scarica oggi**, quindi la decisione è dovuta ma nulla è in violazione.
