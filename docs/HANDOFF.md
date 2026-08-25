# Aura — handoff unico

**Aggiornato: 2026-08-23.** Sostituisce dodici handoff sparsi (`docs/superpowers/*-handoff.md`,
`.planning/handoffs/*`, `docs/handoff-2026-08-13.md`), cancellati il 2026-08-13.

**Questo documento tiene solo ciò che è APERTO.** Il lavoro chiuso viene cancellato appena è
chiuso: un handoff che accumula un diario di cose fatte fa solo confusione, e la prova di ciò che
è stato fatto sta nei commit, non qui. Le sezioni 8, 9 e 10 sono le uniche eccezioni, e non per
raccontare: servono a impedire che qualcuno ripaghi una trappola, insegua un falso difetto o
riapra una decisione già presa.

---

## Stato

Produzione su e sana. Git ri-misurato il 2026-08-23; il resto il 2026-08-17.

- `master` è a `5a061bfde`, pushato il 2026-08-23 (**CI in corso al momento della scrittura** —
  verificare prima di trattarla come verde). Porta l'emendamento PRD #127 e gli scope di
  approvazione (vedi §10). Il push precedente, `ef698eea2`, era verde su tutti e tre i run:
  **CI 25 job su 25**, CodeQL, Skills. Un solo branch locale (`master`): i quattro ref
  `worktree-agent-*`, `tmp/62-reserves` e `gsd/phase-45-harness-correctness` sono stati
  cancellati dopo aver verificato **per contenuto** — non per SHA — che il loro lavoro è in
  master.
- Su origin restano tre branch **interamente fusi** (`ahead=0` su `origin/master`) che nessuno ha
  ancora cancellato: `gsd/phase-45-harness-correctness`, `fix/unify-credential-redaction`,
  `fix/backup-postgres-arcadedb`. Più due branch Dependabot con PR aperte (**#46** pin
  `codeql-action`, **#47** nove patch AWS SDK), entrambe verdi su tutta la matrice.
- **Una sessione parallela lavora sullo stesso checkout.** Conseguenze già pagate: commit con
  pathspec esplicito (l'index conteneva file non miei), un `.git/index.lock` stantio da un
  processo git morto, e un `git commit` che riesce con **zero** file. Verifica sempre con
  `git show --stat`.

**Regola imparata il 2026-08-17, che vale più di una voce aperta:** i test `db_integration` si
guidano con `AURA_DB_URL` su **`aura_app`**, mai su `aura`. Su questo deployment `aura` è
`rolsuper=true rolbypassrls=true`, quindi Postgres salta del tutto la row security e **un'intera
classe di bug di scoping diventa invisibile** — tre test che chiamavano il loro store senza
principal passavano in locale e fallivano nel gate. Copia le DSN di `scripts/coverage_docker.sh`
(`aura_app` per l'app, `aura_migrate` per le migration, `aura` solo come bootstrap).

---

## Come si chiude

Le sezioni sotto **diagnosticano**; questa **sequenzia** — meno **1.1** e **6.4**, chiuse il
2026-08-23. L'ordine non è la loro numerazione: prima ciò che sblocca altro, poi ciò che è pericoloso, poi ciò che è solo
lavoro. Ogni riga dice **cosa fare** e **cosa lo prova** — un punto senza prova non si chiude,
si sposta.

### Onda 0 — decisioni che solo l'operatore può prendere (nessun codice le muove)

*Due delle tre voci sono chiuse (2026-08-23): **1.1** corretta in `CLAUDE.md`, **6.4** decisa con
**ADR 0045**. Vedi §10.*

| # | Cosa | Cosa lo chiude |
|---|---|---|
| **3.7** | **Ri-misurata il 2026-08-22: quattro righe su cinque non descrivevano più la realtà**, e il register è stato corretto. Gli account calendario ed email SONO autorizzati, il device WhatsApp È appaiato. Restano aperte per ragioni nuove, non per le vecchie. | **Il difetto interno di EXT-001/002 è risolto il 2026-08-23** (vedi §10): il daemon monta `server=calendar tools=1` e un turno reale chiude `RUN_FINISHED` con tre `calendar__calendar` riuscite. Restano aperte solo sul **loro criterio**, che non è il rimonto: EXT-001 vuole un CRUD reversibile completo, EXT-002 una ricevuta d'invio — entrambe scritture reali sull'account dell'operatore, che nessuna sessione deve fare di propria iniziativa. EXT-003/005 sono ferme su un'**approvazione che il gateway trattiene per progetto** (`gateway_approval_required`). Solo **EXT-004** è ancora esterna. |

**Nota strutturale che l'analisi originale non aveva:** chiudere una riga **è** lavoro di codice.
`release_ready` è letteralmente `not rows` (`scripts/audit_closure_gate.py:132`) e
`REQUIRED_CURRENT_IDS` (`:25`) pinna EXT-001..005, quindi togliere una riga dal register senza
togliere il suo ID dal gate lo fa fallire con `missing current issues`. E `external_blocked` conta
in `open_total` esattamente come `open`: **non esiste uno stato di waiver**, quindi «registrare
perché restano aperte» non può in nessun caso far diventare verde quel gate.

### Onda 1 — sicurezza, dalla più grave

| # | Cosa | Cosa lo chiude |
|---|---|---|
| **2.3** | ~~Se skills o pyscripts raggiungono `../mcp/servers.json` per traversal, D-106 è falsa.~~ **CHIUSO 2026-08-23.** Il traversal è NO (`bc8291fb8`), ma la premessa di D-106 cade lo stesso per la via dell'install: npx gira da root nel processo aura e il registry è scrivibile da lì. | **Fatto.** `stdio_shape.go` rifiuta allo spawn e al salvataggio le forme che nessun server MCP ha — nessuna allowlist, nessuna capacità tolta all'agente (PRD amendment #128). Dettaglio in §6.x qui sotto. |
| **2.1** | `aura.settings` ha PK sulla sola `key`, è esclusa da RLS (`0087:248`) e **contiene `OPENROUTER_API_KEY`**. | O si scopa per identità, o si registra perché la guardia di boot che rifiuta il multi-utente lo rende irrilevante — con il commit che lo dice, non a voce. |
| **2.2** | Le scritture del file-manager decodificano senza gate sul Content-Type, sole fra tutte le scritture del server. | Lo stesso 400 che danno le altre, più il test che lo prova sul filo. |

### Onda 2 — i documenti che mentono (costano poco, e una regola falsa fa fidare)

| # | Cosa | Cosa lo chiude |
|---|---|---|
| **1.2** | ADR 0038 è `Accepted` e descrive Neo4j, che non esiste più qui. Zero file `Superseded` in tutta `docs/adr/`. | 0038 marcato `Superseded by 0042`. È l'unica contraddizione dove il codice ha già votato. |
| **1.3** | Il budget token per chunk dice 512 in tre posti e vale 2048. | I tre posti (`compose.yaml:216`, `CLAUDE.md:24`, `.planning/codebase/INTEGRATIONS.md:24`) allineati a `services/ingest/chunk.py:43`. |
| **§7.1-7.2** | `.planning/STATE.md` nega file committati quattro ore prima; `ROADMAP.md` sottostima sé stesso. | Rigenerati, o cancellati se nessuno li legge. |
| **§7.3** | Il corpus di 77 requisiti «already in hand» non è mai esistito su nessun branch. | L'affermazione rimossa da `ROADMAP.md:439-441` e `REQUIREMENTS.md:7`. |
| **§7.4** | TOOL-11 è «Pending» ma è costruito: manca solo il rename in `fs_search`. | Rename fatto, o riga marcata done. |
| **§7.6** | Cinque cicli di riferimento incrociato bloccano `/gsd-ingest-docs`. | Il comando gira. |

### Onda 3 — gate che non chiudono

| # | Cosa | Cosa lo chiude |
|---|---|---|
| **3.4bis** | **Il più economico di tutti**: la sonda MUSR spostata è misurata a mano ma non è mai stata eseguita. | Il job `musr-e2e` verde. Nessun lavoro, solo una run. |
| **3.2** | La riga snippet-reuse è ROSSA e il suo harness è cancellato: lo snapshot stampa un comando che punta a un package inesistente. | O la riga si ritira come le sorelle, o l'harness si ricostruisce. Non c'è terza via. |
| **3.4** | Stessa forma: il North-Star xlsx delle skills non ha più harness Go, e `scripts/chat-e2e-gate.sh` non è invocato da nulla. | Ritiro o ricostruzione. |
| **3.5** | LOCOMO è saltata da `agent_memory_eval.py:52` e nessun workflow imposta `AURA_LOCOMO_DIR`. | **ADR 0045 declina il corpus**, quindi «dataset provisionato» non è più un'opzione: la suite si ritira come specificata, o si rifà su un corpus permissibile. |
| **3.3** | `recall@1` del routing documentale è UNKNOWN: i due harness compilano e **nessuno li esegue**. | Non più bloccata da una decisione mancante: **ADR 0045** declina `open_ragbench`, quindi la riga deve prima **nominare il corpus che intende** e poi portare un numero in `docs/aura-quality-snapshot.md:62`. Attenzione, due delle tre leve citate dalla riga sono morte (`AURA_DOCUMENT_OCR_ENABLED`, Docling): la riga va riscritta, non solo riempita. |
| **3.1** | Il gate di produzione #115 non è mai stato eseguito e il suo runner è cancellato, ma `prd.md:4619` lo mantiene come condizione di chiusura. | O si riscrive il runner, o si emenda il PRD. È l'unica cosa che non è **mai** stata fatta. |
| **3.6** | Sette report readiness su dieci assenti, i tre presenti legati a commit diversi da HEAD. | Dieci report freschi di 24h su HEAD. Dipende da 3.7. |

### Onda 4 — lavoro di codice

| # | Cosa | Cosa lo chiude |
|---|---|---|
| **6.1a** | Una soglia di compaction sotto la quota di overhead la **spegne in silenzio**, e la manopola è esposta in Settings. | Clamp, o rifiuto che dice qual è il minimo utile su questa finestra. Prova: soglia impossibile → errore, non silenzio. |
| **6.1b** | La soglia conta i token della **storia**, non quelli della richiesta: a 30% non è scattata con 38.941 token di input riportati dal provider. | Decidere quale numero si intende comprimere, e confrontare quello. |
| **6.1c** | `compactionTimeout` è 3 minuti **per scelta, non per misura**, e un timeout lì è silenzioso. | Misurare quanto dura davvero un riassunto su una storia lunga, poi tenere il numero o renderlo configurabile come fa hermes. |
| **6.1d** | Al reload la gauge ricade su `LastInputTokens`, che è la somma dei prompt del round: un limite superiore, non la finestra. | Una colonna, o l'accettazione esplicita del limite superiore scritta nella UI. |
| **6.1e** | `llm.Config.Validate` pretende una finestra > 33.000, quindi **un modello a 32k non fa partire il daemon**. Le riserve sono costanti assolute. | Riserve proporzionali alla finestra. Prova: Qwen a 32.768 parte. |
| **6.2** | I dump del grafo ci sono, il restore non è mai stato provato. | Un restore eseguito (HTTP, a server acceso, su database che non deve esistere) e `scripts/restore_drill.sh` che non mente più. |
| **6.3** | `observability_sidecar_check.sh` non è agganciato a niente: né cron, né CI, né Makefile. | Agganciato al cron di Aura, che ha già `ReschedulesOnRecovery` e manda alert su Telegram. |
| **6.5** | `filecard` scrive OOXML a mano (712 LOC) e `excelize` non è nemmeno in `go.mod`. | Sostituzione, o la ragione misurata per non farla. |
| **6.6** | «che documenti ho caricato?» non ha niente da chiamare: `cmd/aura/docs.go:201-217` elenca job di ingest, non il catalogo. | Un verbo che risponde alla domanda. |
| **6.7** | Immagini e audio non hanno una famiglia propria — `services/ingest/app.py:275-282` lo dichiara come seam lasciato aperto. | Famiglia, o seam chiuso dichiarando che non serve. |
| **§5** | Resta da **misurare**, non da dedurre, se dei 2.012 LOC di `internal/documents` serva rimuovere ancora. | Un conteggio di chiamanti, non una stima. |
---

## 1. Regole del progetto che il codice non rispetta

Queste vengono prima di tutto perché una regola falsa è peggio di una regola assente: fa fidare.

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
`aura.settings`, catalogo MCP. Le primitive per-identità scritte in Phase 36 per skills e
catalogo MCP (`skills.NewSkillToolForIdentity`, `mcp.MountForIdentity` e famiglia) avevano
**zero chiamanti di produzione** e sono state **cancellate**: facevano sembrare semi-risolto
un piano che non lo era. `aura.settings` ha PK sulla sola
`key` (`0024_settings.up.sql:15`) e `0087_rls_fail_closed.up.sql:248` la esclude esplicitamente
da RLS — **e contiene `OPENROUTER_API_KEY`**. *Confidenza: alta.*
*(Il piano tool è invece genuinamente chiuso: `SandboxRouter.Route` contiene su ogni profilo.)*

**2.3 — Il critical di code-scanning è una decisione registrata, ma la sua premessa ha un lato
non verificato.** `go/command-injection` su `internal/mcp/sdkclient.go:169` è **D-106**, scritto
nel codice sopra la riga stessa e tracciato come T-45.1-06: il validatore del comando di lancio
non è stato portato di proposito, perché montare un MCP non deve costare cerimonia. La premessa
(«lo monta solo l'operatore») **regge oggi**, verificata il 2026-08-17 e non assunta: il registry
è `~/.aura/mcp/servers.json` dentro il container aura, la box del modello monta solo le cache
pip/uv/npm e `/workspace`, e nessun tool agente scrive quella config — l'unica scrittura è
`POST /api/governance/mcp`, autenticata.
**Il traversal è VERIFICATO il 2026-08-23, e la risposta è NO** (`bc8291fb8`,
`internal/skills/writer_traversal_security_test.go`). Non per lettura della regexp: tutti e nove
i verbi raggiungibili dall'agente (`create`, `update`, `save_snippet`, `install`, `archive`,
`restore`, `delete`, `set_always`, `use_snippet`) sono stati guidati contro `../mcp/servers.json`
in 14 grafie — POSIX, Windows, percent-encoded, assoluta, con separatore finale, solo-punti — e
tutte tornano `ErrInvalidName`. Ogni caso verifica anche che il registry sia **byte-identico**
dopo, perché `archive`/`restore`/`delete` uniscono il nome in un path che finisce in
`os.RemoveAll`: un rifiuto arrivato dopo la join avrebbe comunque distrutto il file. Il layout del
test è quello misurato in produzione, non uno comodo: `/var/lib/aura/mcp/servers.json` accanto a
`/var/lib/aura/skills`, un solo salto relativo, con il processo aura a `uid=0` che può scriverlo.
Il materializzatore dell'install salta i symlink invece di seguirli (provato con un albero che
punta al registry). E `~/.aura/pyscripts` **non ha nessuno scrittore**: la premessa dell'analisi
originale era stale — gli snippet Slice 7e sono skill `type:snippet` sotto la root skills, e
`pyscripts` è solo una directory provisionata e montata nella box.

**Ma la premessa di D-106 cade lo stesso, per una strada che questa voce non guardava.** Cinque
anelli, ognuno misurato il 2026-08-23, nessuno dedotto:

1. `skill_manage action=install` è **Risky** (`classify.go:92`) e `gated()` ferma **solo**
   `Destructive` → nessuna approvazione. Che il modello ci arrivi da solo non è ipotesi:
   l'emendamento #126 lo ha misurato dal vivo.
2. `Installer.Install` esegue `npx skills add <source> --copy -y` via `exec.CommandContext`
   (`installer.go:176`, `execCommandRunner`) — **nel processo aura**, non nella box.
3. Il container aura gira `uid=0(root)` e `npm config get ignore-scripts` risponde **`false`**:
   il `postinstall` di un pacchetto npm gira da root.
4. `/var/lib/aura/mcp/servers.json` (0600 root) è **scrivibile** da quel processo.
5. `sdkclient.go:169` costruisce la command line da quel file **senza validarla** — il critical
   accettato.

Il traversal non serve. E l'ordine è invertito rispetto a quello che la difesa presume:
`SanitizeName` e la blocklist girano sul SKILL.md **dopo** che npx ha già eseguito codice di
terzi. L'ultimo anello non è stato eseguito di proposito — sarebbe scrivere un exploit contro il
deployment vivo; i cinque anelli sono misurati singolarmente.

**Direttiva operatore, 2026-08-23:** non si chiude togliendo capacità all'agente («un problema che
potresti avere anche te»). `--ignore-scripts`, `install` portato a `Destructive`, npx non-root e
la validazione della command line sono tutte **respinte**: contraddicono D-06/D-07 e la postura
no-cerimonia. La direzione ammessa è **integrità, non permessi** — l'agente quel file non lo
scrive mai, l'unico scrittore legittimo è `POST /api/governance/mcp`, autenticato; quindi il
daemon può tenere l'impronta del registry e segnalare (audit + allarme, non rifiuto) una
riscrittura fuori banda, lasciando intatti install, script, root e la scelta della sorgente.
*Confidenza: alta su tutti e cinque gli anelli. Non implementato.*

**2.2 — Le scritture del file-manager non hanno il gate sul Content-Type.**
`internal/agui/files_api_write.go:83-98` — il commento stesso dice "Decoded WITHOUT a
Content-Type gate, unlike every other write on this server". Difesa in profondità CSRF
assottigliata rispetto a ogni altra scrittura del server. *Confidenza: alta.*

---

## 3. Misure dovute e gate che non chiudono

**3.0 — Il registry MCP su Postgres non ha ancora una misura di coverage.**
`internal/mcpregistry` e `cmd/aura/mcp_registry.go` sono nati il 2026-08-24 e il tier che li
esercita davvero — un vero `Upsert`/`List`/`Remove` contro Postgres — non esiste: i test
`cmd/aura` girano su un registry in memoria (`mcp_registry_fake_test.go`), che è giusto per
provare il parsing degli argomenti e **non prova nulla** sul sealing dell'env, sul round-trip
del jsonb o sulla transazione unica che tiene insieme la riga di audit e la scrittura. Il floor
dell'85% è aggregato, quindi un package scoperto viaggia sulle spalle degli altri e il gate può
restare verde mentendo. *Serve `bash scripts/coverage_docker.sh` con lo stack acceso, e dei test
`db_integration` che guidino lo store come `aura_app` — mai come `aura`, per la ragione nella
sezione Stato.*

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

**3.4bis — La sonda MUSR sul piano documenti non è mai stata eseguita.**
`TestTwoIdentityCrossDeny/documents_cross_deny` è stata spostata da `aura.documents` (ritirata da
`0098`) a `aura.ingestion_jobs` il 2026-08-17. Le sue quattro asserzioni sono state **misurate a
mano** come `aura_app` su schema fresco (A vede 1, B vede 0, senza identità 0, B che forgia una
riga di A → `new row violates row-level security policy`), ma **il test Go in sé non è mai
girato**: il suo tag set pretende Garage e Authula vivi. Gira per la prima volta nel job
`musr-e2e`. *Confidenza: alta sulla misura, nulla sull'esecuzione.*

**3.5 — La suite LOCOMO non gira e il dataset non è provisionato.**
`scripts/agent_memory_eval.py:52` la salta con `-skip "^TestLocomo…"`;
`internal/arcadedb/locomo_test.go:86-87` vuole `AURA_LOCOMO_DIR`, che nessun workflow imposta.
*Confidenza: alta.*

**3.6 — Release readiness: 7 report su 10 assenti, i 3 presenti sono stali.**
`artifacts/production-readiness/` ha solo audit-closure, capability-eval e coverage, generati il
2026-08-07 e legati a commit diversi da HEAD. Il gate li vuole tutti e dieci, legati a HEAD e
freschi di 24h (`scripts/release_readiness_gate.py:255-264`). *Confidenza: alta.*

**3.7 — Le 5 righe di audit sono chiuse.**
Le misure del 2026-08-22 avevano corretto il registro a cinque righe, una sola esterna. EXT-005 è
stata chiusa il 2026-08-25 con cleanup osservato; nello stesso giorno l'operatore ha attestato come
già completate e risolte EXT-001…EXT-004. Il gate corrente stampa `0 current unresolved`, zero
esterne e `release_ready=true` per il registro audit.

- **EXT-001…EXT-004 — CHIUSE 2026-08-25 PER ATTESTAZIONE OPERATORE.** L'operatore ha confermato
  che le azioni di chiusura erano già state completate e risolte: CRUD calendario reversibile,
  ricevuta email autorizzata, round-trip WhatsApp approvato e rimozione/verifica della versione
  GHCR protetta. Questa sessione registra l'attestazione senza ripetere alcuna scrittura esterna e
  senza inventare dettagli di ricevuta non forniti.
- **EXT-005 — CHIUSA 2026-08-25.** La consegna sul canale cockpit autenticato e la ricevuta sul
  filo erano già provate il 2026-08-22 (`aura.artifact`, asset 29 byte, bucket per-identità e
  content hash). La terza gamba è ora osservata sullo stesso volume persistente: il run root è
  vuoto, quindi non restano né lo staging TTL sotto `$AURA_RUN_DIR/tmp/` né le due directory legacy
  `aura-sendfile-*`. La riga è stata rimossa dal registro corrente e da `REQUIRED_CURRENT_IDS`.

*Residuo legacy ricontrollato 2026-08-25:* le directory esatte
`/var/lib/aura/runs/aura-sendfile-3383401088/` e `aura-sendfile-830219956/` sono assenti. Il run
root persistente è vuoto. L'osservazione prova la rimozione, non attribuisce senza evidenza quale
boot, sweep o intervento abbia eliminato le directory.

*Stato corrente: registro vuoto, `open_total=0`, `release_ready=true`.*

---

*(§4 non esiste più: conteneva sei difetti piccoli, tutti chiusi. I numeri di sezione non
vengono ricompattati — sono citati altrove e un rinumero li renderebbe bugiardi.)*

---

## 5. Residuo di cancellazione (codice morto lasciato indietro)

La riscrittura del 2026-08-08 e la ritirata del catalogo (`0098`) hanno eliminato la pipeline
in-process, il modello a versioni, Docling, lo store di retrieval Postgres, il delete worker e
`aura.documents` con le sue nove tabelle sorelle. Questo è ciò che resta attaccato:

- **`internal/documents` misura oggi 2.012 LOC non-test su 16 file**, di cui **un solo**
  `catalog*.go`. Le cifre precedenti (5.989 / 24 / 824) sono superate: la migration di drop, che
  questa voce dava per non scritta, è `0098`. *La domanda «serve ancora rimuovere?» è quindi
  quasi chiusa — quel che resta va misurato, non dedotto da questa riga.*
- **Lasciato apposta**: `(*idempotency.Store).RecoverExpired` non ha chiamanti di produzione ma
  ha test d'integrazione vivi e **nessun percorso gemello** che ne copra il fatto. È cablaggio
  non finito, non residuo: cancellarlo chiuderebbe d'ufficio una domanda che nessuno ha posto —
  cosa recupera un'operazione idempotente rimasta in-progress.
- **Ogni ritirata lascia test che puntano al vuoto, e il compilatore non li vede tutti.** `0098`
  ne ha lasciati due, trovati solo perché erano rossi su una strada altrui: uno seminava
  `aura.documents` nel tier `db_integration`, l'altro chiamava
  `documents.NewPostgresCatalogStore` sotto un **tag**, quindi il build non taggato restava
  verde e solo `tagged-tier-compile` lo vedeva. *Chi cancella una tabella deve girare
  `scripts/tagged_tier_compile.sh`, non solo `go build ./...`.*
---

## 6. Lavoro progettato e non fatto

**6.8 — MCP per identità: metà fatta, e le due metà mancanti non sono lo stesso lavoro.**
Il 2026-08-24 il registry MCP è passato su Postgres (`aura.mcp_server`, migration `0101`, il
file `servers.json` cancellato) e un server dietro OAuth ora monta **quando lo si autorizza** e
ritorna dopo un riavvio dal grant salvato. Misurato sullo stack acceso: calendar 1 tool, linear
53, memory 4, notion 28, whatsapp 15, tutti `ready` sul board. Resta aperto questo:

- *Un registry per processo.* `tools.Registry` è uno solo e condiviso. I grant invece sono per
  identità (`aura.identity_mcp_oauth`, `0100`), quindi un server che **due** identità hanno
  autorizzato viene rifiutato al boot (`mcpoauth.ErrAmbiguousOwner`) invece di dare a uno il
  token dell'altro. È la scelta giusta per un'appliance a operatore singolo e **sbagliata** per
  una multi-utente: LibreChat tiene un pool di connessioni per utente
  (`MCPManager.getUserConnection`), non un registry solo. Va deciso, non scoperto il giorno che
  esiste la seconda persona.
- *`calendar` e `whatsapp` sono single-account per costruzione.* `sendingMiddleware`
  (`internal/agent/mcptools/mount.go:120`) aggiunge `IdentityMetaMiddleware` **solo** se
  `policy.memory`, quindi solo `memory` porta `_meta.aura.user_identifier` e instrada al
  database ArcadeDB della persona. Gli altri due condividono un account PIM e un pairing
  WhatsApp fra tutte le identità. **Il lavoro non è simmetrico**: `aura-pim-mcp` è già
  multi-account (`list_accounts` restituisce `accountId`, ogni tool ne prende uno), quindi
  calendar diventa per-identità stampando l'identità sul mount (una riga in
  `bridge_risk.go:149`) e mappando identità → `accountId` **nel nostro fork** del sidecar —
  stessa forma di Linear. Il bridge WhatsApp invece non ha alcun concetto di account: una
  sessione pairata, un volume, una porta. Lì per-identità significa **un container per
  identità** (lo schema che Aura già usa per ArcadeDB), provisionato dalla saga che crea già le
  directory per identità; forkare il bridge per tenere N sessioni whatsmeow combatte contro un
  design upstream che assume un dispositivo solo.
- *Non spiegato, e ormai non riproducibile.* `/var/lib/aura/mcp/servers.json` è stato trovato
  troncato a mappa vuota **due volte** nello stesso pomeriggio, con dentro i nomi `hung`,
  `e2e-probe`, `ssrf-probe` — che sono fixture di test di `cmd/aura`. Solo `aura` e
  `aura-migrate` montavano quel volume, quindi un test sull'host non poteva averlo scritto. La
  causa non è stata trovata; il file non esiste più, quindi il sintomo non può tornare. Se
  qualcosa di simile ricapita su `aura.mcp_server`, questa riga è il precedente.

**6.1 — Compaction: quel che resta aperto.** Slice 1b è consegnata (riga durevole `0096` con
watermark, trigger anticipato, aggiornamento iterativo, ramo per branch, `/compact` esplicito con
il marker in chat). Aperti solo questi:

- *Una soglia sotto la quota di overhead disattiva la compaction in silenzio.*
  `earlyCompactionTokens` = `finestra × pct/100 − overhead manifest`, e restituisce 0 (= spenta)
  quando è ≤ 0. Su questo deployment l'overhead è ~19k, quindi qualunque percentuale sotto ~24%
  spegne tutto senza un warning — e la manopola è esposta all'operatore in Impostazioni, sul
  pannello «Budget token» del rail (`web/src/settings/`). Va clampata,
  o rifiutata dicendo qual è il minimo utile su questa finestra.
- *La soglia conta i token della STORIA, non quelli della richiesta.* System prompt e manifest
  dei tool (~19k qui) non entrano nel conteggio: con finestra 81.920 e soglia 30% la compaction
  non è scattata benché il provider riportasse 38.941 token di input. Se l'intento è «comprimi
  quando la richiesta è grande», il numero da confrontare è un altro.
- *`compactionTimeout` è 3 minuti per scelta, non per misura.* Alzato da 45s quando il tetto di
  output del summarizer è stato tolto (un riassunto senza tetto è ~10x più lungo). Un timeout lì
  è **silenzioso** — si ripiega sul drop deterministico, quindi la storia sparisce invece di
  condensarsi — ma sul percorso automatico quei minuti sono latenza che l'operatore subisce.
  hermes-agent lo rende configurabile (`auxiliary.compression.timeout`); qui è una costante.
- *Al reload la gauge del cockpit ricade su `LastInputTokens`*, che è la somma dei prompt di ogni
  chiamata del round — un limite superiore, non la misura della finestra. In-stream è corretta
  (`context_tokens`); serve una colonna, o si accetta il limite superiore e lo si dice.
- *Limite non di questa slice*: `llm.Config.Validate` pretende
  `window > max(output, 20000) + 13000 = 33.000`, quindi **un modello con finestra ≤ 33k non fa
  partire il daemon** (Qwen a 32.768 falliva per 232 token). Le riserve sono costanti assolute:
  su 1M sono briciole, su 32k sono più della finestra. Vanno rese proporzionali.

**6.2 — Il piano grafo ha i dump ma nessun restore provato.**
`scripts/restore_drill.sh` dice ancora *"nothing dumps them, so there is no restore to rehearse"*.
La prima metà è falsa da oggi. Il restore ArcadeDB è una chiamata HTTP e funziona a server acceso,
ma il database di destinazione non deve esistere.

**6.3 — Il rilevamento osservabilità non avvisa nessuno.**
`scripts/observability_sidecar_check.sh` (aggiornato oggi, ora verifica anche che Prometheus stia
davvero **raschiando** Aura) non è agganciato a niente: né cron, né CI, né Makefile. Il cron di
Aura ha già `ReschedulesOnRecovery` e manda alert su Telegram — è lì che va.

**6.4 — Confidenza fusa senza consumatore. La licenza è DECISA (ADR 0045); la misura resta da
ridefinire.** Il blocco esterno che teneva ferma questa voce — `open_ragbench` CC-BY-NC-4.0 e
`snap-research/locomo` NOASSERTION, registrati in `prd.md` come decisione dovuta e non presa — è
stato sciolto il 2026-08-23: **entrambi i corpora sono declinati**. Nessuna pipeline può scaricarli.
Gli harness restano su disco e restano compilati: quel che è stato declinato è il corpus, non
l'apparato.

Resta vero tutto il resto della diagnosi del 2026-08-16, e va riletto perché è ciò che ora determina
*cosa* si misura al posto loro:

1. **La logica "da famiglia" non esiste.** È `forceOpen || len(doc.passages) == 0`, e basta.
2. **Il PRD ratifica una direzione, non una soglia.** L'emendamento #119 promuove il punteggio fuso a
   *«segnale di affidabilità del ranking»*, riporta ROC AUC 0.880 e **nessun punto di lavoro**. Con i
   due corpora declinati, scrivere un `if score < …` resta inventare il numero — ora però per una
   ragione registrata, invece che per una decisione rimandata.

**Due sonde sullo stack acceso, e il fallimento che #119 cita non si riproduce:** un aggregato
incrociato (somma con filtro su città **e** anno) ha prodotto **€ 2.325.167 su 11 clienti, esatto**,
con `requires_open` **false** per tutto il giro — apre per via della prosa del tool. E un'ambiguità a
quattro file ha prodotto **Q3 = 862.523 €, esatto**, aprendoli tutti e quattro in parallelo.
**Cosa questo NON dimostra:** non refuta #119, il cui caso era un corpus di 130 documenti con 75 CSV.
Dice solo che a questa scala il consumatore non serve.

**6.5 — filecard scrive OOXML a mano** (`xlsx.go` 442 LOC, `ooxml.go` 270) mentre `excelize` non è
nemmeno in `go.mod`. *Confidenza: alta.*

**6.6 — `"che documenti ho caricato?"` non ha niente da chiamare.** Solo `document_search` e
`document_open` sono registrati; `cmd/aura/docs.go:201-217` elenca **job di ingest**, non il
catalogo. *Confidenza: alta.*

**6.7 — Immagini e audio non hanno una famiglia propria.**
`services/ingest/app.py:275-282` lo dichiara apertamente come seam lasciato aperto.

**6.8 — La DOMANDA di ogni pausa generata dal server è solo in inglese.** Gli scope di
approvazione (#127) hanno risolto le *etichette* — codice semantico sul filo, parole scritte da
ogni superficie — ma la riga sopra i bottoni resta `"Approve skill_manage (risk=destructive)? …"`
anche in una card italiana. Non è una svista di quella slice: la domanda è generata in Go
(`gatewayApprovalQuestion`, e gli analoghi di `shell_exec` e install skill), salvata in
`paused_states.question`, ed è **il vincolo CR-01** — il resume la confronta byte per byte con
quella emessa. Tradurla lato client significherebbe parsare prosa, cioè la cosa che il codice
di questo repo evita apposta. Farla bene vuol dire dare a *tutte* le domande generate dal server
lo stesso trattamento codice+render: una colonna nuova su `paused_states` (parametri strutturati
accanto al testo, che resta l'oggetto del confronto) più un campo su `ask_user`. Lavoro suo,
deliberatamente non iniziato qui — decisione dell'operatore il 2026-08-23.

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

- **Un `mux.Handle` senza slash finale è un path ESATTO: niente sotto di esso eredita.**
  `/api/approvals` è montato così sul parent mux, quindi `/api/approvals/grants` ha risposto
  **404** benché la rotta esistesse e fosse registrata sul mux AG-UI. Non si scopre in Go: si
  scopre aprendo la pagina. Ogni rotta nuova sotto un prefisso già montato vuole la sua voce.
- **Le `options` di una pausa non sono mai arrivate a nessun canale, per due bug diversi.**
  `paused_states.options` è serializzato da `agent.PauseOption` come `[{label,value}]`, e il
  cockpit accettava solo `string[]` → ogni pausa con opzioni cadeva in silenzio sul campo di
  testo libero. Telegram, in parallelo, ignorava `p.Options` su `kind=approval` e mostrava il
  Sì/No fisso, che risponde con contenuto vuoto. Una funzionalità può essere shippata,
  testata e **irraggiungibile** su ogni superficie: il test che manca è quello che guarda la
  pagina.
- **Prima di localizzare, guarda dove nasce la stringa.** Le etichette di scope erano generate
  in Go, e il gateway non conosce la lingua dell'operatore: tre bottoni inglesi dentro una card
  italiana. La soluzione non è tradurre lato server ma spedire un **codice semantico** e lasciare
  che ogni superficie scriva le sue parole — è ciò che `internal/runner` fa già con
  `ResolveOutcome`, ed era scritto nel commento di quel file.
- **`docker compose restart` su un parent `network_mode: service:` orfana i figli per sempre.**
  Usare `up -d`. Costo misurato: 5h30 di metriche cieche. Risolto strutturalmente oggi, ma la
  regola vale per ogni altro accoppiamento `service:`.
- **`AURA_DB_MIGRATE_URL` puntato al DB vivo in un run `db_integration` lo migra davvero.**
  Creare un database usa-e-getta.
- **`AURA_DB_URL` puntato al ruolo `aura` in un run `db_integration` da un VERDE FALSO.** Quel
  ruolo e' `rolsuper=true rolbypassrls=true`, quindi Postgres salta la row security e ogni bug di
  scoping diventa invisibile: tre test che chiamavano il loro store senza principal passavano in
  locale e fallivano nel gate. Il gate usa `aura_app`. Copiare le DSN di
  `scripts/coverage_docker.sh`, non improvvisarle.
- **Git Bash traduce i CRLF IN LETTURA, quindi un file CRLF li' sembra LF.** Uno script con CRLF
  gira sotto Git Bash e muore in WSL con un errore che non c'entra niente
  (`set: pipefail: invalid option name`: il nome opzione letto era `pipefail` seguito da CR). Si
  diagnostica dal lato WSL (`wsl -e bash -lc "head -1 file | od -c"`) e si ripara dal blob
  committato, perche' `.gitattributes` dice gia' `*.sh text eol=lf` e `git checkout --` non
  aiuta se a sporcarlo e' stato un editor.
- **Il pre-push legge l'ALBERO DI LAVORO, la CI legge il COMMIT.** Costo misurato il
  2026-08-23: **cinque job rossi** su un push i cui hook erano tutti verdi. La correzione prettier
  di due file stava sul disco ma non nel commit, quindi il gate `web` la vedeva e
  `npm run format:check` in CI no. Su un checkout condiviso (§Stato) questo scarto è la norma:
  prima di pushare, confronta ciò che hai committato con ciò che gira, non con ciò che vedi.
  Corollario dello stesso giorno: un'assertion può restare indietro rispetto a due file che
  concordano — `container_artifacts_test.go` pinnava `whatsapp-mcp:sha-e0b8345` mentre
  `compose.yaml` e `ci.yml` erano già su `sha-9911eb8`, e da sola faceva fallire tre job.
- **Il dist embeddato va costruito da un worktree PULITO, non dall'albero di lavoro.**
  `web-dist-freshness` ricostruisce dai sorgenti **committati** e pretende byte identici: un dist
  costruito sopra le modifiche non committate di un'altra sessione non può corrispondere.
  Committa i sorgenti, `git worktree add --detach <tmp> <sha>`, costruisci lì, copia indietro.
  E per non Linux-corrompere `web/node_modules` dell'host, monta un volume anonimo sopra:
  `docker run -v "$PWD:/repo" -v /repo/web/node_modules -w /repo/web node:24 …`.
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
  `C:\Users\...\Temp\`. Aura gira su container Linux: sono artefatti d'ambiente. Ri-osservati il
  2026-08-17: `bash scripts/coverage_docker.sh` da Git Bash containerizza **solo Postgres** e
  lascia i test Go sull'host, quindi il gate va girato in WSL o questi tre si leggono come rossi.
- **I 16 `go/log-injection` di code-scanning sono falsi positivi.** Sono tutti
  `slog.Warn(..., "err", err)` e slog qui monta `NewJSONHandler` (`internal/obs/init.go:98`): i
  valori sono escapati in JSON, quindi un a-capo non forgia una riga. Verificato il 2026-08-17;
  non ri-triagiarli uno per uno.
- **Il cache-hit basso è OpenRouter che distribuisce il modello su più provider**, non instabilità
  del prefisso. Tre spiegazioni sono state uccise dai dati prima di quella giusta: il prefisso è
  stabile (`prefix_drift` non scatta mai), il routing sticky è già inviato
  (`session_id == conversation_id`), DeepSeek cacha in automatico. Anche la TTL è caduta: un turno
  a 36 secondi dal precedente leggeva comunque 0%. L'API activity mostra **fino a 5 provider in un
  giorno**, ognuno con la sua cache. Fissare il provider è **deliberatamente NON fatto**: toglie il
  fallback quando quell'host è congestionato. Effetto collaterale della stessa misura, forse più
  importante del costo: **la quantizzazione varia per host** (`fp8` su alcuni, `fp4` su altri) e il
  primo-parte `deepseek/fp8` ha servito 2 richieste su 69 in un giorno.
- **Le pause di Aura non scadono, e non è una svista.** `internal/askuser/store.go:14` lo
  specifica (`SPEC Req#4`): *"There is no internal timeout / expiry state… the only resolution paths
  are Resume, ResumeBatch, and AutoResolveForConversation."* Confrontando con hermes viene la
  tentazione di portarne il timeout di approvazione (300s, poi esito `timeout` distinto da `deny`,
  in `tools/approval.py:3670-3730`). **Non va portato:** hermes *blocca un thread dell'agente* su un
  `threading.Event` mentre aspetta, quindi DEVE limitare l'attesa o la sessione resta incastrata.
  Aura non blocca: persiste la pausa e il turno finisce, quindi una pending aperta costa una riga e
  nient'altro. Stesso nome, problema diverso — portare il timeout toglierebbe una proprietà
  specificata per curare un male che qui non esiste.

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
| L'approvazione senza scope (ogni distruttiva trattenuta ogni volta) | `72aef55a2` + `64fc478d8` — emendamento #127, `once`/`session`/`always`; provato dall'agente vero: tre delete in un turno con zero approvazioni sotto il grant di sessione, e un `always` sopravvissuto al riavvio del container |
| Il cockpit che non ha mai renderizzato le `options` come bottoni | `72aef55a2` — `parseOptions` accetta `{label,value}`, la forma che il DB scrive davvero |
| Telegram che ignorava le `options` su `kind=approval` | `72aef55a2` — `approvalScopeMarkup`, un bottone per scope più Rifiuta |
| `/new` e `/clear` nel composer | Cancellati: non facevano niente che la sidebar non faccia |
| Il tetto di output del summarizer (`summaryMaxTokens = 1024`) | Tolto dal filo, mai da rimettere: `agent/context_compressor.py` di hermes lo legifera («NO max_tokens: the output cap must never truncate a summary»). La lunghezza si chiede nel PROMPT, derivata da quanto si comprime |
| Copiare l'`archive_and_compact` di hermes | Rifiutato con misura: là la compaction fa UPDATE sulle righe dei messaggi, che qui spara `conversation_turns_snapshot_bump` (0047) → `55006` → niente compaction proprio durante un export. E il flag `active` è per-riga, mentre l'albero branch di 0017 condivide le righe fra rami: non può esprimere «superata su A, viva su B». Il watermark per (conversazione, ramo) sì |
| Iniettare il summary «identical shape to `injectAlwaysBlock`» (piano 1b §6) | Sbagliato: sloggerebbe l'always-block da `messages[1]` cambiando il prefisso cachato. Derivato invece da `splitHeadHistoryActive`, **dopo** `injectAlwaysBlock` |
| `aura.documents` e le nove tabelle sorelle | Migration `0098` |
| **1.1** — il floor di coverage per-package che il gate non applica | `CLAUDE.md` corretto: il gate confronta **un solo aggregato** (`scripts/coverage_gate.sh:86-107`), e ora la regola lo dice. Il ciclo per package NON è stato implementato perché sotto il solo tag `db_integration` boccerebbe package la cui coverage vera vive in un altro tier (`internal/arcadedb`: 92,81% sotto `arcadedb_integration`) |
| **3.7 / EXT-001-002** — riavviare il daemon per rimontare la superficie calendario | **FATTO il 2026-08-23**, alla condizione che era stata posta: il lavoro fase 46 è costruito e in master (`2edbc3910`, `b01413620`, `8cc2aeed2`). Rimonto con `up -d` come prescritto: `mcp mounted server=calendar tools=1`. Provato da un turno reale — `RUN_FINISHED`, tre `calendar__calendar` riuscite, zero `Unknown tool` — non dedotto dal log di mount. Le due righe restano aperte sui loro criteri di scrittura, non più su questo difetto |
| **3.7 / EXT-003** — approvare io la pending al posto dell'operatore | **Deciso di NO.** È esattamente il controllo che ha funzionato: `approve.go:148-150` lega `ApproveChallenge` alla challenge **e** a una domanda visibile all'operatore, *"the informed-consent binding a benign relayed question cannot satisfy (CR-01)"*. Hermes fa lo stesso in modo ancora più netto — l'utente risolve fuori banda con `/approve`/`/deny <reason>` e scope `once\|session\|always`. Auto-approvare svuoterebbe la sola cosa che in tutta EXT-003 ha funzionato al primo colpo |
| **6.4** — le licenze dei due corpora di eval | **ADR 0045**: `open_ragbench` (CC-BY-NC-4.0) e `snap-research/locomo` (NOASSERTION) **declinati entrambi**. Nessuna pipeline può scaricarli; le misure che li aspettavano sono ridefinite, non cancellate |

---

## 11. Non verificabile dal repo

Richiedono una sonda su database/object store vivi o un run reale dell'agente:
oggetti orfani in Garage e asset bloccati in `accepted`/`failed` (08-06); i 24 fatti e 48 entità
da ri-embeddare in `aura_memory`; i comportamenti dell'agente del 08-09 (salta il corpus, rifiuta
invece di aprire, 6 turni su 20 inconcludenti); *(le licenze dei due corpora non sono più qui:
decise il 2026-08-23 con ADR 0045.)*
