# Aura — handoff unico

**Aggiornato: 2026-08-17.** Sostituisce dodici handoff sparsi (`docs/superpowers/*-handoff.md`,
`.planning/handoffs/*`, `docs/handoff-2026-08-13.md`), cancellati il 2026-08-13.

**Questo documento tiene solo ciò che è APERTO.** Il lavoro chiuso viene cancellato appena è
chiuso: un handoff che accumula un diario di cose fatte fa solo confusione, e la prova di ciò che
è stato fatto sta nei commit, non qui. Le sezioni 8, 9 e 10 sono le uniche eccezioni, e non per
raccontare: servono a impedire che qualcuno ripaghi una trappola, insegua un falso difetto o
riapra una decisione già presa.

---

## Stato

Produzione su e sana. Misurato il 2026-08-17.

- `master` è a `4f6af1093`, allineato a `origin/master`, **niente di non pushato**. Un solo
  branch locale (`master`): i quattro ref `worktree-agent-*`, `tmp/62-reserves` e
  `gsd/phase-45-harness-correctness` sono stati cancellati dopo aver verificato **per contenuto**
  — non per SHA — che il loro lavoro è in master.
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

**2.3 — Il critical di code-scanning è una decisione registrata, ma la sua premessa ha un lato
non verificato.** `go/command-injection` su `internal/mcp/sdkclient.go:169` è **D-106**, scritto
nel codice sopra la riga stessa e tracciato come T-45.1-06: il validatore del comando di lancio
non è stato portato di proposito, perché montare un MCP non deve costare cerimonia. La premessa
(«lo monta solo l'operatore») **regge oggi**, verificata il 2026-08-17 e non assunta: il registry
è `~/.aura/mcp/servers.json` dentro il container aura, la box del modello monta solo le cache
pip/uv/npm e `/workspace`, e nessun tool agente scrive quella config — l'unica scrittura è
`POST /api/governance/mcp`, autenticata.
**Quello che NON è stato verificato, ed è la domanda intera:** che gli scrittori di file
dell'agente — skills in `~/.aura/skills`, pyscripts in `~/.aura/pyscripts` — non possano fare
traversal fino a `../mcp/servers.json`. Se possono, «trust class dell'operatore» è falso e la
riga 169 diventa esecuzione di comando arbitrario. *Confidenza: alta su ciò che è misurato, nulla
sul traversal. È il primo controllo di /gsd-secure-phase 45.1.*

**2.2 — Le scritture del file-manager non hanno il gate sul Content-Type.**
`internal/agui/files_api_write.go:83-98` — il commento stesso dice "Decoded WITHOUT a
Content-Type gate, unlike every other write on this server". Difesa in profondità CSRF
assottigliata rispetto a ogni altra scrittura del server. *Confidenza: alta.*

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

**3.7 — 5 righe di audit restano aperte, tutte a blocco esterno.**
Verificato oggi eseguendo il gate: `5 current unresolved, 5 external, release_ready=false`.
Sono account calendario/email, QR WhatsApp, versione GHCR, ricevuta `send_file`. **Non è lavoro
di codice.** *Confidenza: alta sull'apertura, nulla sulle cause.*

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

**6.1 — Compaction: quel che resta aperto.** Slice 1b è consegnata (riga durevole `0096` con
watermark, trigger anticipato, aggiornamento iterativo, ramo per branch, `/compact` esplicito con
il marker in chat). Aperti solo questi:

- *Una soglia sotto la quota di overhead disattiva la compaction in silenzio.*
  `earlyCompactionTokens` = `finestra × pct/100 − overhead manifest`, e restituisce 0 (= spenta)
  quando è ≤ 0. Su questo deployment l'overhead è ~19k, quindi qualunque percentuale sotto ~24%
  spegne tutto senza un warning — e la manopola è esposta all'operatore in Settings. Va clampata,
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
| `/new` e `/clear` nel composer | Cancellati: non facevano niente che la sidebar non faccia |
| Il tetto di output del summarizer (`summaryMaxTokens = 1024`) | Tolto dal filo, mai da rimettere: `agent/context_compressor.py` di hermes lo legifera («NO max_tokens: the output cap must never truncate a summary»). La lunghezza si chiede nel PROMPT, derivata da quanto si comprime |
| Copiare l'`archive_and_compact` di hermes | Rifiutato con misura: là la compaction fa UPDATE sulle righe dei messaggi, che qui spara `conversation_turns_snapshot_bump` (0047) → `55006` → niente compaction proprio durante un export. E il flag `active` è per-riga, mentre l'albero branch di 0017 condivide le righe fra rami: non può esprimere «superata su A, viva su B». Il watermark per (conversazione, ramo) sì |
| Iniettare il summary «identical shape to `injectAlwaysBlock`» (piano 1b §6) | Sbagliato: sloggerebbe l'always-block da `messages[1]` cambiando il prefisso cachato. Derivato invece da `splitHeadHistoryActive`, **dopo** `injectAlwaysBlock` |
| `aura.documents` e le nove tabelle sorelle | Migration `0098` |

---

## 11. Non verificabile dal repo

Richiedono una sonda su database/object store vivi o un run reale dell'agente:
oggetti orfani in Garage e asset bloccati in `accepted`/`failed` (08-06); i 24 fatti e 48 entità
da ri-embeddare in `aura_memory`; i comportamenti dell'agente del 08-09 (salta il corpus, rifiuta
invece di aprire, 6 turni su 20 inconcludenti); le licenze dei due corpora — **nessuna pipeline le
scarica oggi**, quindi la decisione è dovuta ma nulla è in violazione.
