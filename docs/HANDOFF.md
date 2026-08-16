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

**La TERZA superficie, segnalata dall'operatore e chiusa** (`eeb35015e`): il file manager del
cockpit etichettava ogni riga con un uuid. Stessa causa — leggeva la chiave invece del nome —
e stessa cura: `ObjectNames` deriva la `search_document_id` dalla chiave del bucket
(`SearchDocumentID` in Go è la stessa funzione di `identity.search_document_id` nel sidecar) e
riusa `DocumentNames`. **Una query per pagina, mai una HEAD per riga**: la stessa scelta che fa
il browser di `garage-webui`, che riserva la HEAD per l'apertura del singolo oggetto.

La metà widget è **misurata contro il pacchetto installato**, non assunta: `parseId` assegna
`name` dall'id in modo INCONDIZIONATO — un nome inviato accanto viene scartato — tranne che
esce prima quando `parent` è 0, e la classe base legge poi `parent || cartellaRichiesta`,
ripristinando il posizionamento. `parent: 0` non è un genitore: è l'unico modo di dire «questi
campi sono miei, non derivarli», e siccome salta la derivazione va inviato anche `ext` o si
perde l'icona. Comportamento non documentato che su un upgrade regredirebbe **in silenzio**,
quindi `web/src/files/filesApi.store.test.ts` lo fissa: l'upgrade rompe un test invece di far
tornare gli uuid in produzione, cosa che nessun test Go può vedere.

**Quattro righe restano uuid e questo non le ripara**: caricate prima del canale metadati, il
nome che l'indice conosce per loro *è* l'uuid. I nomi veri sono in `aura.assets.file_name`
(verificati: `report_di_test.md`, `btc_last_week.png`, `phase26-browser-upload.png` ×2). Un
ripiego su Postgres nel solo file manager lo farebbe **divergere da `document_search`**: la
riparazione giusta è sull'indice, e resta una decisione non presa.

**Sei fixture lasciate nel corpus vivo** dell'identità operatore: `colm2025_conference.pdf`,
`clienti_2026.csv` e i quattro `fatturato_q*_2025.csv`. Servono a ri-verificare; una
`delete_object` le rimuove e CocoIndex cancella la riga da sé (verificato sostituendo una
chiave).

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

**2.3 — CHIUSA il 2026-08-16 (`b04b7dd08`), e il perimetro della voce era sbagliato per
difetto.** `migratedDocumentPool` non esisteva più: `11f49cd9b` aveva già portato
`internal/documents` su database usa-e-getta. Ma il pericolo non era mai stato lì soltanto —
**30 file di test in 19 package** prendono `AURA_DB_MIGRATE_URL` dall'ambiente e lo passano a
`db.Migrate`, che applica ogni migration pendente a qualunque cosa quell'URL indichi. `.env`
distribuisce il DSN del deployment e ogni operatore ce l'ha esportato per ragioni che con i
test non c'entrano: fra un normale `go test -tags db_integration` e una migration sul DB vivo
c'era **una variabile d'ambiente**.

Convertire tutti e 30 a database usa-e-getta sarebbe stato il fix di `documents` trenta volte:
un `CREATE DATABASE` e una migration completa **per test**, pagati a ogni run di CI, per
difendersi da un nome. Ciò che separa il sicuro dal pericoloso è la guardia, quindi c'è solo
quella — una riga per call-site, costo di runtime zero.

La regola è quella di `scripts/coverage_docker.sh`, apposta perché le due non possano
divergere: un database che si chiama `aura` è rifiutato **se `GITHUB_ACTIONS` non è settato**,
perché in CI quel nome appartiene a un container che il job ha creato e butterà via. Fallisce e
non salta: un skip leggerebbe come «questo tier non si applica», il falso verde che la regola
no-skip-as-green esiste per impedire.

Provata su un test `db_integration` vero, in tre direzioni: puntata a `…/aura` rifiuta **prima
di aprire la connessione**; puntata a `…/aura_dev` lascia passare e il test fallisce più avanti
per conto suo; con `GITHUB_ACTIONS=true` il nome vivo è ammesso, quindi nessun job di CI cambia
comportamento.

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

**4.1 — CHIUSA il 2026-08-16 (`7b051204e`), ma non come questa voce prescriveva.** Il fatto era
esatto: `generation` dichiarato, fotografato, confrontato, **mai assegnato**. La conclusione no.

Chiamarla *«una protezione che non protegge»* implica che manchi l'assegnazione. Misurato:
manca l'intero percorso d'invalidazione. Nulla marca stale la banca, `built` viene solo messo
a `true`, gli anchor sono statici per costruzione, e `singleflight` già serializza le build
sulla stessa chiave — quindi la corsa da cui la guardia proteggeva non può avvenire. Renderla
viva avrebbe voluto dire **inventare un trigger d'invalidazione** per una banca che non ha
niente da invalidare. Rimossa, e la pubblicazione è incondizionata.

Ciò che rende sicura la rimozione era già fissato da due test esistenti (fallimento non
cachato, singleflight a freddo). L'unica proprietà senza test proprio — la banca costruita una
volta e riusata, quella che *sembrava* protetta — è stata aggiunta, asserita come delta per
fissare il "non ricostruisce" invece del numero di tier.

**4.2 — CHIUSA il 2026-08-16 (`02fba5490`).** `dropRepeatedUserTurns` collassa due turni utente
adiacenti **byte-identici**, tenendo il più recente, prima di `injectAlwaysBlock` — così la testa
sintetica non può mai essere candidata e il conteggio token, la compaction e il drop L2.5
lavorano sulla storia com'è avvenuta. Due messaggi *diversi* di fila restano intatti: sono una
persona che scrive due volte prima che l'agente risponda (`019fa501` seq 72-73, «via non capisci
un cazzo» / «percè????»), e fonderli le metterebbe parole in bocca.

Provato guidando l'agente sulla conversazione vera, stesso prompt prima e dopo:
*«la frase compare DUE volte, entrambe dentro un unico messaggio»* → *«1 — compare una sola volta
come messaggio utente completo»*.

Una correzione alla voce d'origine: diceva che il modello riceve **due messaggi**. Ne riceve
**uno**, col testo ripetuto dentro — il provider coalesce i messaggi consecutivi dello stesso
ruolo (Aura non ha alcuna fusione propria: cercata, non c'è). L'effetto è quello descritto, il
meccanismo no.

**4.2b — CHIUSA il 2026-08-16 (`5befdd184`) — era la causa di 4.2.** Un run che muore non
lasciava **nessuna** traccia in conversazione: né un turno d'errore, né uno vuoto. Il silenzio
era indistinguibile da una risposta lenta, quindi ripetere era l'unica mossa che la persona
avesse — ed è precisamente come nasceva il doppione di 4.2.

**Causa trovata riproducendola, non leggendola.** Riavviare il daemon a metà run lascia un
turno utente, nessun turno assistant e **zero righe in `cache_metrics`** — l'impronta identica
a quella del 2026-08-13. La catena: lo shutdown drena gli stream SSE in volo con un tetto di
10 secondi (`aguiShutdownTimeout`, `serve.go:42-46`); un round più lungo viene cancellato, il
suo handler ritorna, e l'operazione di idempotenza viene marcata `indeterminate` — che è **lo
stesso stato di un `agent_run` riuscito**. Nulla, da nessuna parte, distingueva i due.

Ora `recordInterruptedRound` scrive il marcatore, con `context.WithoutCancel` perché gira
*proprio perché* il contesto è morto. Provato ripetendo la stessa riproduzione: prima `seq 1
user` e basta, dopo `seq 2 assistant "[run interrupted before it produced an answer…]"`.

**L'altra metà — il SIGKILL — è chiusa senza sweep** (`61e5d92bf`). Lo sweep sembrava
obbligato, e non lo era: avrebbe dovuto indovinare una soglia oltre la quale un turno
sospeso è "davvero" morto, e sbagliandola avrebbe dichiarato morto un run lungo ma vivo.
Guardando hermes-agent (`repair_empty_non_final_messages`) è emersa la strada giusta —
**riparare la copia di filo in lettura, mai la storia salvata** — e applicata alla forma di
Aura non serve alcuna soglia: quando una storia viene riletta, il round che l'ha prodotta è
finito per definizione, quindi un turno utente seguito da un altro turno utente non ha
ricevuto risposta *per registro*, non per tempistica.

Il passo specifico di hermes **non è stato portato**: sostituisce un segnaposto nei messaggi
vuoti e non finali, che alcuni provider rifiutano con un 400 che poi avvelena ogni turno
successivo. Misurato prima di scrivere: Aura ha **zero** righe di quella forma, quindi
portarlo sarebbe stato curare una malattia che questo corpus non ha.

Provato con un SIGKILL vero a metà run — che lascia solo il turno utente. Al turno dopo
l'agente ha risposto «No.» citando testualmente `[no answer was produced for the message
above]`, e si è offerto di scrivere il saggio che non aveva scritto. Effetto collaterale
utile: due turni utente consecutivi diventano alternati sul filo, quindi un provider che
pretende l'alternanza stretta è soddisfatto per costruzione invece che perché li fonde in
silenzio.

**Correzione a quanto avevo scritto qui stamattina.** Avevo detto che il difetto «è successo
di nuovo mentre misuravo il fix». **Falso**, e l'ho verificato: quel run aveva risposto (seq 86,
20.141 token registrati in `cache_metrics`); era caduto solo il client del mio driver, che è un
fallimento diverso — il lavoro c'era, la risposta pure, ed era leggibile ricaricando. Avevo
scambiato un client caduto per un run morto.

**4.3 — CHIUSA il 2026-08-16 (`f0a2941d2`).** L'`HTTPError` nudo ora porta il **corpo** della
risposta — dove llama.cpp nomina il guasto vero, *«input is too large to process, increase the
physical batch size»*, cioè il fix in una riga — e il **conteggio token misurato** accanto al
tetto del modello, perché la causa stragrande è un chunk che ha sforato. Il file portava già la
cicatrice: il commento di `process_file` spiega che un chunk dimensionato al tetto nudo sfora
per via del prefisso e *«the request 500s»*.

Misurare dentro il percorso d'errore è sicuro per costruzione: `count_tokens` degrada a stima
per carattere quando il server è irraggiungibile, quindi un server giù non può trasformare un
embed fallito in un **messaggio d'errore** fallito. Idem per un corpo illeggibile (`fp` a None):
leggerlo avrebbe sostituito il fallimento originale con uno nostro. La catena `raise … from exc`
resta, quindi il traceback arriva ancora alla causa di trasporto.

**E l'errore ora si autocorregge** (`a7a0fe61d`), perché leggerlo era metà del lavoro: perdere
il vettore di UN chunk perde **l'intero file**. CocoIndex cattura il fallimento del componente,
stampa "component build failed" e prosegue, quindi il documento non arriva mai all'indice — e
in modalità **live**, che è come gira questo deployment, `audit_pass()` non viene mai eseguito
per accorgersene. È esattamente così che il 2026-08-09 sparirono due documenti.

Su sforamento ora embedda **la testa che ci sta** invece di niente: la Passage conserva il testo
intero, quindi `document_open` e la card non cambiano, e solo il vettore è calcolato su meno del
tutto — degradazione piccola e dichiarata, contro la perdita del file.

Due scelte da non disfare: il trigger è **la misura**, mai la stringa d'errore del provider
(«input is too large to process» è il fraseggio di llama.cpp, un conteggio oltre il tetto
significa la stessa cosa ovunque), e un fallimento che **non** è uno sforamento viene sollevato
e non ritentato, perché mandare meno non ripara un server che sta caricando e il retry lo
nasconderebbe. Il taglio riusa `chunk.chunk`, lo stesso splitter verificato che dimensiona tutto
il resto della pipeline: una seconda regola di sizing, in disaccordo con la prima, si
manifesterebbe proprio come questo errore.

Validato contro il sidecar vivo: 33009 token → HTTP 500 (documento perso), stessa chiamata dopo
→ vettore da 768 dimensioni. Il corpo del server dice *«input (33009 tokens)»*, cioè **lo stesso
numero** misurato qui.

**Resta aperto, ed è la vera fragilità**: in live `audit_pass()` non gira mai, quindi qualunque
altra perdita resta invisibile. Lo sforamento è coperto adesso; ogni altro modo di perdere un
documento no.

**4.4 — CHIUSA il 2026-08-16 (`34fdf1049`, poi `a6e81e56e`), e la chiusura di ieri conteneva
due affermazioni mie che oggi ho misurato false.**

Il fatto originale era esatto (nessun `errors.Is` da nessuna parte) e la conseguenza era più
concreta di «non instradato»: `processAsset` appiattiva **qualunque** errore in `StatusFailed`
con codice `processor_failed`, quindi un blocco che **non è colpa del file** risultava
indistinguibile da un formato non supportato o da un upload corrotto. Ora porta il suo codice
(`delete_in_flight`), classificato con `errors.Is` e non per sottostringa: il sentinel è
avvolto **due volte** salendo (`"catalog document: %w"` in `documents.Service`, poi il ritorno
del processor) e solo il sentinel sopravvive intatto.

**Prima falsità — «una corsa che si risolve da sola in secondi».** Nessuna istruzione scrive
più `aura.documents.status='deleting'`: `6519956a2` (ieri) ha cancellato il workflow di delete
asincrono che lo faceva. Una riga trovata in quello stato è quindi **incagliata**, e non esiste
niente che la finisca. Aspettare non serve e ricaricare serve meno di tutto — il blocco è sulla
sorgente, non sui byte. Catalogo vivo al momento della misura: **0 righe** in quello stato (1
riga totale, `deleted`). Il messaggio del sentinel diceva *«document with this source is being
deleted»*, presente progressivo, cioè «aspetta» detto a chi non ha niente da aspettare; ora
dice *«stranded mid-delete and will not clear on its own»* e quella frase arriva intatta fino
alla riga asset.

**Seconda falsità — «non fa il retry: serve backoff e un limite».** Sbagliata due volte. Il
backoff e il limite **esistono già**, al posto giusto: `IngestionJobWorker.recordHandlerFailure`
fa backoff esponenziale con full jitter e manda in `dead_letter` a `MaxAttempts` (5, da
`assetProcessingIngestionJobRequest`), con due test che lo coprivano da prima che scrivessi la
riga. E per **questo** errore il retry non ripara niente, perché lo stato non si schiarisce mai.

Il retry resta lì deliberatamente: è il comportamento giusto se un workflow di delete torna, e
costa quattro tentativi sprecati se non torna. Sopprimerlo significherebbe aggiungere al worker
un canale «fallimento permanente» il cui unico utente è uno stato che nulla sa creare — codice
al buio per definizione.

La misura non può marcire in un commento: `db.TestNoQueryRevivesTheStrandedDocumentStatus`
fallisce il giorno in cui una query torna a scrivere quello stato (verificato facendolo fallire,
non solo passare).

**Validazione live.** Immagine ricostruita e ridistribuita — `aura version` → `d817a9f54+44`;
quella che girava non conteneva nemmeno `delete_in_flight`, quindi §4.4 non era mai stata
provata sullo stack. Due `aura docs ingest --source-id strand-probe-44` sullo stesso file, con
la riga incagliata a mano fra i due:

```
status | error_code       | error_message
failed | delete_in_flight | catalog document: document with this source is stranded mid-delete and will not clear on its own
```

e il job durevole ha percorso tutta la vita davanti alla misura: `queued`, attempt 1/5, next
attempt a **+25 s** (full jitter su base 1 minuto), poi **`dead_letter` a 5 tentativi**. Righe e
oggetti della prova rimossi; catalogo tornato a 1 documento / 5 asset.

**Ricaduta da registrare** (non toccata): il Task 7 di
`docs/superpowers/plans/2026-08-05-document-pipeline-operator-e2e.md` chiede a un operatore di
riprodurre la finestra di delete-in-flight e di **ripetere il checkpoint** se la query non trova
righe. Quella finestra non esiste più per costruzione: l'istruzione è diventata un ciclo
infinito, e CP4 nel ledger di `verification/…/FINDINGS.md` è ancora `_pending_`. Il piano
è del 2026-08-05, cioè precedente sia alla riscrittura dell'08-08 sia alla rimozione del
workflow: aggiornarne una riga sola lo farebbe sembrare corrente.

**4.5 — CHIUSA il 2026-08-16 (`a5afccfc3`), e la voce era già scaduta quando l'ho riletta.**
Restava scritto che «non esiste un canale metadati». Esiste da stamattina, ed è la stessa
misura che ha chiuso §0.2: `Attrs.Metadata` porta la user metadata, `PlaceAsset` scrive
`x-amz-meta-filename` percent-encoded (`asset_placement.go:43`) e il sidecar la rilegge con
una HEAD (`source.object_file_name`), degradando al nome della chiave — che per un file
lasciato nel bucket a mano **è** il suo nome.

La chiave resta `chat/<assetID>.<ext>` di proposito: viaggia dentro URL presignate e log di
accesso, e il nome di un file è spesso il suo contenuto («Perizia città di Ghèdi 2026.txt»).

Verificato oggi sul bucket vivo, non sul codice: **8 oggetti `chat/` su 8** portano il nome
reale in metadata, accenti compresi, e ArcadeDB li mostra identici lato indice —
`Perizia città di Ghèdi 2026.txt`, `colm2025_conference.pdf`, i quattro `fatturato_q*_2025.csv`.
Il nome non è solo esposto: entra in `file_name_words`, quindi prima di questo cambio cercare
un documento con il nome che gli aveva dato l'operatore non trovava niente.

**Coda: il download aveva ancora il uuid** (`9a1515f8b`, trovata dall'operatore nel browser).
Avevo sistemato la lista e lasciato `Content-Disposition` costruito su `path.Base(key)`, quindi
le due superfici dicevano nomi diversi dello stesso oggetto. Il nome era già nella risposta:
`S3Store.Get` buttava via `out.Metadata` mentre `Head` la teneva, quindi l'handler aveva
`attrs` in mano con l'unico campo che gli serviva mancante. Ora `Get` la porta, `ReadObject` la
decodifica con `DecodeFileName` (che rifiuta già qualunque cosa somigli a un path) e l'handler
la preferisce alla chiave — metadata dell'oggetto e non indice, perché non costa un giro in più
su un percorso che l'oggetto l'ha già letto e perché nomina anche un oggetto che il sidecar non
ha ancora raggiunto. La lista continua a usare l'indice: nominare una pagina intera così
vorrebbe dire una HEAD per riga.

Misurato sul filo, prima e dopo, con una sessione autenticata dentro la rete:

```
prima:  attachment; filename="deedee78-b835-4aab-9d30-44e60f60d4cc.md"; filename*=UTF-8''deedee78-…
dopo:   attachment; filename="saggio_crittografia.md"; filename*=UTF-8''saggio_crittografia.md
dopo:   attachment; filename="Perizia citta di Ghedi 2026.txt"; filename*=UTF-8''Perizia%20citt%C3%A0%20di%20Gh%C3%A8di%202026.txt
```

L'accentato è la prova che l'header non è stato toccato: il parametro esteso conserva l'unicode,
il fallback ASCII piega i diacritici invece di sfigurarli, e la guardia contro l'iniezione di
header è la stessa di prima.

**Residuo, ed è una decisione aperta, non un baco**: i 7 oggetti storici
`identity/<id>/asset/<uuid>/original` non hanno metadata (precedono il canale) e non sono
indicizzati — `identity/` è escluso dal walker — quindi il file manager li mostra per chiave.
I loro nomi veri stanno in `aura.assets.file_name`; darglieli significa una seconda lookup nel
file manager, ed è la decisione che resta da prendere.

---

## 5. Residuo di cancellazione (codice morto lasciato indietro)

La riscrittura del 2026-08-08 (dodici commit, `14a0d1ae8`→`29bbe0cf9`) ha eliminato la pipeline
in-process, il modello a versioni, Docling, lo store di retrieval Postgres e il delete worker.
Questo è ciò che è rimasto attaccato:

**Rimisurata il 2026-08-16: quasi tutta già chiusa, e non da me.**

- ~~13 delle 14 query in `document_pipeline.sql`~~ — **chiuse da `98d7a136a`** («Delete the
  in-process pipeline's bookkeeping; CocoIndex does that work»). Il file oggi contiene **una**
  query, `ReservePipelineCandidateVersion`, e ha un chiamante; `document_control_plane.sql` ne
  ha dieci e **tutte** hanno chiamanti non-test. Contato eseguendo il grep su ogni `-- name:`,
  non a occhio.
- ~~`SearchDigests`/`SearchDocumentDigests`, 4 query storage-object~~ — **sparite** con lo stesso
  commit: ne restano solo due commenti che ne raccontano la storia.
- ~~Vocabolario di stato aspirazionale~~ — **già potato**: `catalog_status.go` dichiara oggi i
  **due** valori che Go scrive davvero (`accepted`, `stored`), spiega perché il CHECK ne ammette
  dodici, e `TestDocumentVocabulariesMatchTheDatabase` impedisce la deriva nella direzione che
  fa male (una costante che il vincolo non ammette = 23514 in produzione).
- **5 simboli rimossi il 2026-08-16 (`b6520a16c`)**, ciascuno dopo aver distinto residuo da
  *cablaggio mai finito*: `agent.NewTracerProvider` **più tutto il bootstrap dietro** —
  `newTracerProvider`, `countingSpanExporter` e la metrica
  `aura_agent_span_export_failures_total`, che non era a zero, era **senza scrittore**, perché il
  provider vero lo installa `obs.Init`; `recordLLMDuration`, doppione irraggiungibile di
  `llmCallBoundary` (l'istogramma continua a fluire, i suoi due test ora guidano il boundary
  reale); `display.Normalize`, wrapper di una riga; `normalizeSwarmStatus`, il cui fatto arriva
  all'operatore dentro lo swarm report. `cliOperationFromContext` non era superseduto: **non era
  mai stato codice di produzione**, i suoi unici tre lettori erano test, quindi è sceso nel file
  di test. −262 righe, +51.
- **Lasciato apposta**: `(*idempotency.Store).RecoverExpired` non ha chiamanti di produzione ma
  ha test d'integrazione vivi e **nessun percorso gemello** che ne copra il fatto. È cablaggio
  non finito, non residuo: cancellarlo chiuderebbe d'ufficio una domanda che nessuno ha posto —
  cosa recupera un'operazione idempotente rimasta in-progress.
- **`internal/documents` misura oggi 5.989 LOC non-test su 24 file**, di cui 824 di superficie
  catalogo (`catalog*.go`) — le cifre precedenti (4.373 / ~1.465) sono superate, e il numero è
  **cresciuto** nonostante le potature. La migration di drop resta non scritta. *Confidenza:
  alta sulla misura; la domanda «serve ancora rimuovere?» resta aperta.*

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

**6.4 — Confidenza fusa senza consumatore. APERTA, ma non per la ragione scritta qui prima, e
bloccata su una misura che nessuno ha fatto** (indagata il 2026-08-16).

Due correzioni alla voce d'origine, che diceva: *«`retrieval_rank.go:62` decide `RequiresOpen`
da famiglia e zero-passaggi»*.

1. **La logica "da famiglia" non esiste.** È `forceOpen || len(doc.passages) == 0`, e basta.
   La famiglia non compare in tutto il retrieval.
2. **Il PRD ratifica una direzione, non una soglia.** L'emendamento #119 promuove il punteggio
   fuso a *«segnale di affidabilità del ranking»* e dice *«confidenza bassa deve spingere verso
   `document_open`»* — ma al punto 5 di «cosa NON dimostra» scrive *«nessun numero qui riguarda
   la latenza o il costo di un eventuale consumatore»*. Riporta ROC AUC 0.880 e **nessun punto
   di lavoro**. Scrivere un `if score < …` oggi è inventare il numero.

**Due sonde sullo stack acceso, e il fallimento che #119 cita non si riproduce:**

- *Aggregato incrociato* (somma con filtro su città **e** anno, che nessuna card può
  rispondere): l'agente ha detto «è una domanda di somma con filtro, quindi apro il file»,
  ha chiamato `document_open` e ha risposto **€ 2.325.167 su 11 clienti — esatto**. Con
  `requires_open` **false** per tutto il giro: apre per via della prosa del tool, che già dice
  *«or the question needs the whole file (how many, sum, average, maximum, grouping)»*.
- *Ambiguità a quattro file* (quattro tabelle trimestrali dello stesso schema, risposta
  ricomponibile solo aprendoli tutti): ha aperto **tutti e quattro** in parallelo, si è corretto
  da solo su un bug di delimitatore, e ha risposto **Q3 = 862.523 €** — esatto, incluso un
  margine del 2,8% sul secondo.

**Cosa questo NON dimostra.** Non refuta #119: il suo caso era un corpus di 130 documenti con
75 CSV, dove il file giusto stava al rank 2 fra molti simili e l'agente ha speso 24 chiamate
prima di rifiutare. Le mie sonde hanno 4 file tutti pertinenti — è ambiguità piccola, non
grande. Dice solo che a questa scala il consumatore non serve.

**Per sbloccarla servirebbe** l'harness che già esiste e compila
(`retrieval_abstention_eval_test.go`, tag `retrieval_eval`) su `open_ragbench` — 1.914 query
con qrels veri. Ma quel corpus è **CC-BY-NC-4.0** e §11 registra la licenza come decisione
**dovuta e non presa**: scaricarlo in pipeline è prendere quella decisione. Blocco esterno,
non blocco di codice.

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
| **4.2** — la domanda ripetuta rimandata al modello per sempre | `02fba5490` + `d2afda7a5` (il call-site, dimenticato dal primo) |
| **4.2b** — il run che muore senza lasciare traccia | `5befdd184` — causa riprodotta col restart a metà run, marcatore provato dal vivo |

---

## 11. Non verificabile dal repo

Richiedono una sonda su database/object store vivi o un run reale dell'agente:
oggetti orfani in Garage e asset bloccati in `accepted`/`failed` (08-06); i 24 fatti e 48 entità
da ri-embeddare in `aura_memory`; i comportamenti dell'agente del 08-09 (salta il corpus, rifiuta
invece di aprire, 6 turni su 20 inconcludenti); le licenze dei due corpora — **nessuna pipeline le
scarica oggi**, quindi la decisione è dovuta ma nulla è in violazione.
