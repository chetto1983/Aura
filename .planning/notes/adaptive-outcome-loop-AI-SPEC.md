# AI-SPEC — Future Adaptive Outcome Loop (Aura impara dai propri errori)

> AI design contract. Consumed by `gsd-planner` and `gsd-eval-auditor`.
> Locks the failure-signal contract, the credit-assignment rule, the reward shape and the
> promotion guard before planning begins.

---

## 0. Perché questa fase esiste

`internal/adaptive` implementa un ciclo a tre eventi — **assignment → delivery → outcome**.
Sull'appliance viva, misurato il 2026-07-27:

| evento | collegato al runtime? | dove |
|---|---|---|
| assignment | **sì** | `main.go:220` passa `adaptiveControls.toolDiscovery` dentro `tool_search` |
| delivery | **sì** | stesso percorso, `DecideAndDeliver` |
| **outcome** | **NO** | `NewOutcomeRecorder` è costruito solo in `cmd/aura/adaptive_benchmark_*` |

In produzione Aura **sceglie**, **serve**, e poi non registra mai com'è andata. Il cerchio è
aperto sull'ultimo terzo. Non impara male: **non ha il posto dove scrivere di avere
sbagliato**, quindi ripete lo stesso errore all'infinito.

Prova, dal turno delle 14:43 del 2026-07-27 (11 passi assistant invece di 2):

```
tool     ## send_file Deliver a file you produced...
tool     no matching tools. If the capability you need is...
tool     error: tool_not_loaded: "fs_glob" is a deferred tool whose schema...
tool     no matching tools. ...
tool     Traceback (most recent call last): ...
```

Cinque cadute in un turno, **zero righe da cui imparare**. Le singole chiamate LLM erano sane
(3-11 s, identiche ai turni veloci): il turno è stato lento solo per il **numero di giri**.

Conseguenza a valle: `adaptive_policy_state` è fermo a `mode=off, rollout_bps=0,
policy_version=bootstrap-off`, con **zero righe** in outbox, coorti, receipt, evidenza e
transizioni. E correttamente — la scala `off → shadow → canary → active`
(`aura.apply_adaptive_policy_transition`, migration 0058) **richiede evidenza** per salire.
Senza outcome non c'è evidenza; senza evidenza il sistema rifiuta di promuovere. Sta facendo
la cosa giusta rifiutandosi di promuovere qualcosa che non ha mai misurato.

**Questa fase chiude il terzo evento.** Non aggiunge un sottosistema: collega due cose che
esistono entrambe.

---

## 1. System Classification

**System Type:** Online decision system con feedback implicito — un bandit contestuale sopra
scelte di *tool discovery*, alimentato da segnali di fallimento che l'agente **già emette**,
senza etichette umane e senza riaddestrare il modello.

**Description:**
Ogni `tool_search` free-text è già un punto di decisione assegnato a un braccio
(`bm25` / `semantic` / `static`). Questa fase osserva **cosa è successo dopo** nella stessa
conversazione, attribuisce l'esito alla decisione che lo ha causato, e lo scrive come outcome
tipizzato sul ledger esistente. Da lì l'evidenza si accumula con l'uso reale, e la scala di
promozione già presente fa il resto.

**"Good"** = dopo N giorni di uso normale, `adaptive_events` contiene outcome attribuiti
correttamente, il rapporto fra cadute e successi per braccio è leggibile, e la promozione a
`shadow` è supportata da evidenza vera invece che da un dataset sintetico.

### Critical Failure Modes

1. **Attribuzione sbagliata (colpa a valanga).** Punire tutti gli 11 passi di un turno quando
   la radice è stata una sola `tool_search` avvelena l'apprendimento: penalizza nove passi che
   erano conseguenze *corrette* di un errore a monte. Mitigazione: §3.2.
2. **Il log conferma sé stesso.** I log non sono un registro neutro: contengono solo ciò che
   la policy viva ha scelto di mostrare. Imparare a specchio sul proprio output è il modo più
   veloce per cementare un bias. Mitigazione: §3.4 + §5.
3. **Promozione su nulla.** Salire di gradino con 4 osservazioni per braccio è automatizzare
   il tirare a indovinare, con l'apparato di garanzia intorno a dargli credibilità.
   Mitigazione: §3.5.
4. **Premiare l'azzardo.** Se mettere davanti il tool sbagliato costa quanto non metterne
   nessuno, la policy impara a indovinare sempre. Mitigazione: §3.3.
5. **Il segnale altera il turno.** La registrazione dell'outcome non deve mai cambiare,
   ritardare o far fallire il turno dell'operatore. È osservazione, non controllo.
6. **Privacy.** Il testo della query non deve attraversare la porta adattiva. `ToolDiscoveryInput`
   oggi passa solo `QueryLength` + `CandidateIDs`; l'outcome deve rispettare la stessa regola.

---

## 2. I segnali: cosa conta come errore

Nessuna etichetta. Tutti già presenti in `aura.tool_invocations` e `aura.conversation_turns`.
Ogni riga è una verità oggettiva, non un'opinione: il tool o è andato in errore o no.

| segnale | dove si legge | lettura |
|---|---|---|
| `tool_not_loaded` | preview del tool result | il ranker non ha messo davanti il tool che poi serviva → **negativo forte** |
| `no matching tools` | preview del tool result | la ricerca non ha prodotto candidati → **astensione** (§3.3), non fallimento |
| secondo `tool_search` sullo stesso intento | due assignment nello stesso turno | il primo non è bastato → **negativo** |
| il turno finisce in errore | `outcome` dello span di turno | **negativo** |
| tool chiamato ∈ insieme restituito, turno `content_stop` | ledger + span | **positivo** |
| giri fino al completamento | `conversation_turns.seq` delta | **costo**, non successo/fallimento (§3.3) |
| l'operatore riformula | turno `user` successivo senza risposta accettata | segnale debole, **fuori scope in questa fase** (§7) |

Fondamento: qualunque metrica deterministica a valle sostituisce il giudizio umano quando
esiste una verità oggettiva (validità dell'output del tool, successo della chiamata).

---

## 3. Design

### 3.1 Dove si aggancia

Un `OutcomeRecorder` costruito nel composition root vivo (`chat_boot.go`, accanto a
`newAdaptiveControlSet`), **non** solo nel benchmark. Alimentato da un osservatore che legge
il ledger già scritto — non da un nuovo percorso di scrittura nel turno caldo.

Vincolo da CFM-5: l'osservatore gira **fuori** dal turno dell'operatore, su ctx staccato
(`context.WithoutCancel`), e un suo fallimento è un WARN, mai un errore di turno.

### 3.2 Attribuzione: earliest critical error

**Regola:** l'outcome si attribuisce alla **prima** decisione fallita del turno, non a tutte.

Si risale la traiettoria fino al primo punto in cui un intervento avrebbe evitato la cascata,
e si emette **un solo** outcome, legato a quell'assignment. I passi successivi sono
conseguenze, non colpe indipendenti.

Operativamente, per `tool_discovery`: dato un turno con assignment `A1..An` e cadute `F1..Fm`
in ordine di `seq`, l'outcome negativo va su `Ai` = l'ultimo assignment **precedente** a `F1`.
Se non esiste, il turno non produce outcome per questo dominio (la caduta non è imputabile
alla discovery).

### 3.3 Forma della ricompensa: astensione di prima classe, penalità asimmetrica

Due scelte deliberate, entrambe assenti oggi:

**L'astensione è un'azione, non un fallimento.** `no matching tools` può essere la mossa
*giusta*: se nessun tool serve davvero, restituire il vuoto è corretto e va premiato, non
punito. Oggi è indistinguibile da una caduta.

**Il falso positivo pesa più del successo.** Con pesi `γ` (tool sbagliato messo davanti),
`α` (successo), `β` (astensione corretta), vincolo:

```
γ > α > β > 0
```

Mettere davanti il tool sbagliato costa **più** di quanto valga azzeccarlo, perché l'agente lo
chiama, brucia un giro e a volte fa danno. Senza questa asimmetria la policy impara a tirare a
indovinare — che è esattamente il comportamento visto alle 14:43.

I **giri spesi** entrano come costo continuo (termine di penalità), non come esito binario: un
turno che arriva in fondo in 6 passi è un successo più caro di uno in 2, non un fallimento.

I valori numerici di `γ/α/β` sono una **open question** (§8), da fissare in fase di plan e
registrare nel `Config` della policy, non hardcodati.

### 3.4 I log sono off-policy per costruzione

Aura ha osservato solo i tool che il ranker vivo ha scelto di mostrare. Valutare un braccio
diverso su quei log è distorto e va corretto con stimatori **doubly-robust**, non con una
media grezza.

Conseguenza pratica: gli outcome vanno scritti con **la propensione con cui l'azione è stata
scelta**, altrimenti la correzione a valle è impossibile — e i log senza propensione non si
recuperano a posteriori.

**Verificato: c'è già, ed è fatta bene.** `ReceiptArmPolicyRef.Weight` è un `ExactRational`
per braccio, e `canonicalRandomizationPlan.CommonDenominator` dà il denominatore comune: la
propensione è `Weight / CommonDenominator`, **esatta e non in virgola mobile**, sigillata
nell'artefatto del receipt con il suo SHA256. È esattamente ciò che serve a uno stimatore
doubly-robust, ed è più di quanto registri la maggior parte dei sistemi in produzione. Nessuna
migration nuova per questo.

**Ma va capito quando viene scritta.** I receipt nascono dai claim di coorte focale, cioè da
`canary`. In `shadow` non c'è randomizzazione: `SelectionShadowStatic` registra
nell'assignment l'azione **campione** (cosa avrebbe scelto la policy appresa) mentre la
delivery serve lo statico. Vedi §6 per cosa questo implica.

### 3.5 Guardia di promozione: penalità di supporto

La scala esistente non deve poter salire su dati insufficienti. Alla richiesta di evidenza si
aggiunge una **penalità di supporto**: una policy candidata che si appoggia su azioni poco
osservate nei log viene penalizzata proporzionalmente.

Espressa come soglia minima verificabile prima della transizione, valutata **dentro**
`apply_adaptive_policy_transition` o nell'evidenza che quella funzione richiede — non in un
controllo applicativo aggirabile (`aura_app` ha già INSERT/UPDATE/DELETE revocati su
`adaptive_policy_state`, e quella proprietà va preservata).

### 3.6 La corona

Nessun percorso di produzione chiama `apply_adaptive_policy_transition`: solo i test.
`aura eval adaptive` offre `{verify|seal-admission|benchmark}`, nessun `promote`.

Serve `aura adaptive promote --to <mode> --reason <text> --actor <uuid>` che invochi la
funzione esistente, così scala legale, evidenza obbligatoria e audit in
`adaptive_policy_transitions` restano tutti in vigore. È il pezzo mancante fra un meccanismo
corretto e un meccanismo **usabile**.

---

## 4. Contratti e seam

| seam | stato | azione |
|---|---|---|
| `adaptive.NewOutcomeRecorder` | esiste, non collegato | costruire nel composition root vivo |
| `adaptive.NewOutcomeEvent` | esiste | riusare invariato |
| `tools.ToolDiscoveryControl` | esiste, collegato | invariato |
| `aura.tool_invocations` | esiste, popolato | sorgente dei segnali §2 |
| `aura.adaptive_randomization_receipts` | esiste, **0 righe** | verificare che porti la propensione |
| `apply_adaptive_policy_transition` | esiste, mai chiamato in prod | esporre via CLI (§3.6) |

**Nessuna nuova tabella prevista** salvo quanto emerga da §3.4. Il modello dati adattivo è già
completo; questa fase lo **usa**.

---

## 5. Evaluation strategy

Come sappiamo che funziona, in ordine di forza.

**E1 — Attribuzione (unit, deterministico).** Traiettorie sintetiche con cadute note in
posizioni note: l'outcome deve atterrare sull'assignment che precede la **prima** caduta, ed
essere **uno solo**. Include il caso "caduta non imputabile alla discovery" → nessun outcome.
Il turno reale delle 14:43 diventa una fixture.

**E2 — Non invasività (integration).** Con il recorder attivo, un turno che fallisce la
registrazione dell'outcome deve completare identico. Asserzione sul turno, non sul log.

**E3 — Asimmetria (property).** Per ogni `(pf, pa)`, la policy preferisce l'astensione quando
il rischio di falso positivo domina. Pinna il vincolo `γ > α > β` come proprietà, non come
valore.

**E4 — Il ciclo si chiude (live, appliance).** Dopo un giorno di uso reale in `shadow`:
`adaptive_events` non vuoto, outcome per dominio leggibili, rapporto cadute/successi per
braccio calcolabile. **Questa è l'accettazione vera**; le altre sono precondizioni.

**E5 — Discriminazione (smoke, già in repo).** `TestToolDiscoveryArms`
(`-tags tool_discovery_eval`) fallisce se i tre bracci scelgono identicamente ovunque: un
catalogo su cui i bracci non si distinguono non deve poter alimentare una promozione.
Misurato il 2026-07-27 sui 61 candidati vivi: bm25 2/10, semantic 6/10, static 6/10, bracci
divergenti su 7/10.

**Anti-metrica.** Il top-1 contro etichette scritte a mano **non** è un criterio di
promozione. Misura se il ranker indovina quello che aveva in mente l'autore, non se l'agente
ha risolto il problema. Il benchmark congelato (`aura/adaptive-aura-benchmark-dataset`) resta
inutilizzabile per `tool_discovery` finché non è rifatto: 10 dei suoi 12 scenari nominano tool
irraggiungibili — `weather_now`/`send_email`/`finance_quote` non esistono,
`memory_add_preference`/`calendar_create_event` perdono il namespace `__`, e `document_search`
è **attivo** mentre `freezeDeferredTools` offre solo i **deferred**. L'evaluator confronta per
uguaglianza esatta di stringa, quindi quei dieci prendono zero sotto **ogni** braccio; i due
superstiti sono banalmente lessicali, che BM25 vince per costruzione. Potere discriminante
non basso: **zero**.

---

## 6. Rollout

Solo `shadow` in questa fase. `shadow` registra cosa *avrebbe* scelto continuando a servire il
campione/statico: rischio zero, e non richiede che nulla di appreso sia ancora affidabile.

**Cosa shadow può e non può dirci — va detto ora, non scoperto dopo.** In shadow non c'è
randomizzazione: l'assignment porta l'azione **campione**, la delivery serve lo **statico**, e
l'outcome misura quindi il percorso *statico*. Si osservano le **scelte** della policy appresa,
non i suoi **esiti**. Shadow risponde a "il cerchio si chiude, l'attribuzione funziona, le
scelte del campione sono sensate"; **non** risponde a "quale braccio è migliore" — per quello
serve il confronto vero, cioè `canary` con randomizzazione e propensione.

`canary` e `active` sono **fuori scope**: richiedono l'evidenza che questa fase produce, e
promuoverli prima significherebbe decidere senza dati.

Precondizione già soddisfatta: gli embedding devono stare sul sidecar locale
(`b0ef36694`) — la strategia `semantic` passa dall'embedder, e con l'endpoint cloud a
1,4-2,1 s per chiamata qualunque dato raccolto sarebbe avvelenato dalla latenza.

---

## 7. Fuori scope

- Riaddestramento del modello. Nessuno dei lavori di riferimento lo fa: si impara **sopra** il
  modello, con feedback strutturato e memoria degli errori — che è già la forma di
  `internal/adaptive`.
- Gli altri quattro domini (`reasoning`, `skill_routing`, `knowledge_retrieval`,
  `memory_recall`). Stesso schema, ma un dominio alla volta.
- La riformulazione dell'operatore come segnale. È il segnale più onesto che esista e va
  aggiunto, ma richiede di distinguere "ha riformulato perché ho sbagliato" da "ha cambiato
  idea". Fase successiva.
- Rifare il dataset sintetico (150-200 scenari stratificati, `catalog_revision` registrata e
  imposta, `expected_any` per l'ambiguità, split held-out). Serve, ma per lo smoke test di
  §E5, non per la promozione.
- Promozione a `canary`/`active`.

---

## 8. Open questions (da chiudere in `/gsd-discuss-phase`)

1. **Valori di `γ/α/β` e del termine di costo per giro.** Vincolo fissato (§3.3), numeri no.
   Vanno nel `Config` della policy, versionati con essa.
2. ~~La propensione è già registrata?~~ **CHIUSA** — sì, `Weight ExactRational` per braccio +
   `CommonDenominator` nel piano, esatta e sigillata (§3.4). Resta la domanda derivata: dato
   che i receipt nascono solo in `canary`, **shadow va strumentato per registrare comunque la
   scelta del campione in forma comparabile**, o si accetta che il confronto fra bracci inizi
   solo a canary? (§6 propende per la seconda: non fingere un confronto che shadow non può
   fare.)
3. **Definizione operativa di "stesso intento"** per riconoscere un `tool_search` ripetuto.
   La query non attraversa la porta (§CFM-6), quindi va deciso su cosa si basa la
   corrispondenza senza violare quel vincolo.
4. **Soglia della penalità di supporto** e se valutarla in SQL dentro la funzione di
   transizione o nell'artefatto di evidenza.
5. **Finestra dell'osservatore**: quanto indietro guarda, e cosa fa con un turno ancora
   aperto.

---

## 9. Riferimenti

- [Where LLM Agents Fail and How They Can Learn From Failures (AgentDebug)](https://arxiv.org/pdf/2509.25370) — earliest critical error, apprendimento senza riaddestramento
- [Risk-Sensitive Contextual Bandits for Abstention-Aware Memory Retrieval](https://arxiv.org/html/2604.27283) — astensione come azione, `γ > α > β`, tasso storico di falsi positivi nello stato
- [Off-Policy Evaluation for Large Action Spaces via Embeddings](https://arxiv.org/pdf/2202.06317) — correzione del bias su log off-policy
- [Safely Exploring Novel Actions via Deployment-Efficient Policy Learning](https://arxiv.org/pdf/2510.07635) — garanzia di non fare peggio della policy che ha generato i log
- [CASP: Support-Aware Offline Policy Selection](https://arxiv.org/html/2604.23022) — penalità di supporto
- [SENTINEL: Failure-Driven RL for Tool-Using Agents](https://arxiv.org/pdf/2606.12908)
- [Don't Let Bandit Feedback Pull Continual LLM-Recommender Updates Off Target](https://arxiv.org/html/2605.18899)

Evidenza interna: `docs/handoff-2026-07-27.md` §6, `internal/agent/tools/tool_discovery_eval_test.go`.
