# AuraBot Swarm - Design

Data: 2026-05-03

## Obiettivo

Rendere Aura un second brain intelligente che si auto-espande: legge fonti e wiki,
trova lacune, avvia ricerche, sintetizza conoscenza, propone o applica
aggiornamenti, crea task futuri e distilla nuove skill riutilizzabili
dall'esperienza.

La parallelizzazione stile agent teams serve soprattutto a ridurre la latenza
delle fasi di lettura, ricerca, confronto, lint e revisione. Le scritture sulla
wiki e la creazione di skill devono restare serializzate o passare da review,
cosi' Aura cresce senza corrompere la memoria.

## Riferimenti

- Claude Code agent teams: `https://code.claude.com/docs/en/agent-teams`
- Hermes Agent self-improvement: `https://github.com/nousresearch/hermes-agent`
- Picobot spawn stub: `D:\tmp\picobot\internal\agent\tools\spawn.go`
- LLM Wiki pattern: `docs/llm-wiki.md`
- Tool/security decision record: `docs/picobot-tools-audit.md`
- Current implementation state: `docs/implementation-tracker.md`

## Decisione

Non implementare uno swarm con ricorsione libera, tutti i tool abilitati e
scrittura/esecuzione libera. Quella forma richiederebbe un nuovo security model
e toccherebbe troppo il loop Telegram esistente.

Implementare invece un sistema a tre livelli:

1. `AgentRunner`: mini loop LLM/tool bounded, senza dipendenze Telegram.
2. `SwarmManager`: coordina task paralleli, limiti, storage e risultati.
3. `GrowthLoop`: usa lo swarm per far crescere wiki, fonti, task e skill.

## Principi

- Lettura e analisi possono essere parallele.
- Scrittura wiki e installazione skill sono single-writer o review-gated.
- Gli agenti figli ricevono tool allowlist esplicite, non tutti i tool.
- La ricorsione e' bounded: depth, active count, timeout, iteration count.
- I task persistono in SQLite, non in file JSON con locking fragile su Windows.
- Ogni risultato deve essere auditabile: task id, ruolo, input breve, output,
  tool usati, errori e durata.
- L'autonomia deve espandere il second brain, non bypassare l'utente.

## Ruoli AuraBot

### Coordinator

Decide il piano del lavoro, divide i task, sceglie i ruoli, raccoglie risultati
e decide se rispondere, creare proposte o schedulare follow-up.

Tool iniziali:

- `spawn_aurabot`
- `list_swarm_tasks`
- `read_swarm_result`
- tool wiki/source/scheduler gia' esistenti

### Librarian

Naviga wiki, fonti, index, log e conversazioni archiviate. Produce mappe di
contesto e liste di pagine rilevanti.

Allowlist iniziale:

- `list_wiki`
- `read_wiki`
- `search_wiki`
- `list_sources`
- `read_source`
- `lint_wiki`
- `lint_sources`

### Researcher

Cerca nuove fonti o dati esterni quando la wiki mostra buchi. Non scrive in
wiki; restituisce candidati fonte con motivazione.

Allowlist iniziale:

- `web_search`
- `web_fetch`
- MCP read-only esplicitamente allowlistati
- `store_source` solo dopo conferma o policy dedicata

### Synthesizer

Compila sintesi e bozze di aggiornamento wiki. In MVP non scrive direttamente:
produce patch testuali o richieste `propose_wiki_change`.

Allowlist iniziale:

- `read_wiki`
- `search_wiki`
- `read_source`
- `list_wiki`
- futuro `propose_wiki_change`

### Critic

Trova contraddizioni, duplicati, link mancanti, pagine stale, fonti non
processate e skill deboli. Produce issue o task, non correzioni arbitrarie.

Allowlist iniziale:

- `lint_wiki`
- `lint_sources`
- `list_wiki`
- `read_wiki`
- `list_tasks`
- futuro `create_review_issue`

### Skillsmith

Distilla nuove skill dalle esperienze riuscite o ripetute. Non installa skill
direttamente: crea proposte revisionabili.

Allowlist iniziale:

- `list_skills`
- `read_skill`
- `search_skill_catalog`
- futuro `propose_skill`
- futuro `revise_skill_proposal`

### Governor

Applica policy: budget, permessi, toolset, max depth, write gates, skill gates,
MCP gates. Nel primo MVP puo' essere codice deterministico dentro
`SwarmManager`, non un agente LLM separato.

## Architettura

```text
user turn / scheduled growth pass
        |
        v
Coordinator
        |
        v
SwarmManager
        |
        +-- AuraBot: librarian
        +-- AuraBot: researcher
        +-- AuraBot: critic
        +-- AuraBot: synthesizer
        +-- AuraBot: skillsmith
        |
        v
Result aggregation
        |
        +-- user answer
        +-- wiki proposal
        +-- source/task creation
        +-- skill proposal
        +-- scheduled follow-up
```

Ogni AuraBot ha:

- `conversation.Context` isolato.
- Mini tool loop con `max_iterations`.
- Timeout per task.
- Tool definitions filtrate per allowlist.
- System prompt di ruolo.
- Risultato strutturato.

Il loop principale Telegram resta in `internal/telegram/conversation.go`. Non
va estratto subito: e' accoppiato a streaming Telegram, placeholder message,
budget e archiviazione. Il nuovo runner deve essere piccolo e riusabile.

## Storage

Usare SQLite nello stesso DB operativo di Aura.

### `swarm_runs`

```sql
CREATE TABLE IF NOT EXISTS swarm_runs (
  id TEXT PRIMARY KEY,
  goal TEXT NOT NULL,
  status TEXT NOT NULL,
  created_by TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  completed_at TEXT,
  last_error TEXT NOT NULL DEFAULT ''
);
```

### `swarm_tasks`

```sql
CREATE TABLE IF NOT EXISTS swarm_tasks (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL,
  parent_id TEXT NOT NULL DEFAULT '',
  role TEXT NOT NULL,
  subject TEXT NOT NULL,
  prompt TEXT NOT NULL,
  tool_allowlist TEXT NOT NULL,
  status TEXT NOT NULL,
  depth INTEGER NOT NULL DEFAULT 0,
  attempts INTEGER NOT NULL DEFAULT 0,
  blocked_by TEXT NOT NULL DEFAULT '[]',
  result TEXT NOT NULL DEFAULT '',
  tool_calls INTEGER NOT NULL DEFAULT 0,
  llm_calls INTEGER NOT NULL DEFAULT 0,
  elapsed_ms INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  started_at TEXT,
  completed_at TEXT,
  last_error TEXT NOT NULL DEFAULT '',
  FOREIGN KEY(run_id) REFERENCES swarm_runs(id)
);
```

### `skill_proposals`

```sql
CREATE TABLE IF NOT EXISTS skill_proposals (
  id TEXT PRIMARY KEY,
  source_task_id TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL,
  description TEXT NOT NULL,
  body TEXT NOT NULL,
  status TEXT NOT NULL,
  rationale TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  reviewed_at TEXT,
  reviewer_id TEXT NOT NULL DEFAULT '',
  last_error TEXT NOT NULL DEFAULT ''
);
```

Statuses: `proposed`, `approved`, `rejected`, `installed`, `failed`.

## Tool principali

### `spawn_aurabot`

MVP tool, disabilitato di default.

```text
name:          string
role:          librarian|researcher|synthesizer|critic|skillsmith
task:          string
tools:         []string
mode:          async|wait
max_depth:     integer
timeout_sec:   integer
```

Regole:

- Disponibile solo con `AURABOT_ENABLED=true`.
- Rifiuta tool non presenti nell'allowlist del ruolo.
- Rifiuta spawn oltre `AURABOT_MAX_DEPTH`.
- Rifiuta task se `AURABOT_MAX_ACTIVE` e' raggiunto.
- `mode=wait` ritorna risultato breve se completa entro timeout.
- `mode=async` ritorna `task_id`; risultato leggibile dopo.

### `list_swarm_tasks`

Lista task per run, status, role.

### `read_swarm_result`

Legge risultato, errori, tool usati e metriche di un task.

### `propose_skill`

Tool usato da `skillsmith` o coordinator. Non scrive file skill.

```text
name:          string
description:   string
body:          string
rationale:     string
source_task_id string
```

Validation:

- nome `^[a-z0-9][a-z0-9_-]{2,63}$`
- body contiene frontmatter `name` e `description`
- niente segreti o env vars
- niente istruzioni che bypassano allowlist, approval o safety gates
- massimo dimensione configurabile

### `approve_skill`

Admin/review gated. Installa una proposta approvata in `.claude/skills` o
`SKILLS_PATH` usando il loader multi-root gia' esistente.

### `revise_skill_proposal`

Permette a un AuraBot o all'utente di correggere una skill proposta prima
dell'approvazione.

## Growth Loop

Aura deve avere un ciclo autonomo schedulabile, separato dalla risposta utente.

Trigger:

- nightly maintenance gia' esistente
- nuovo task scheduler `second_brain_growth`
- comando utente: "espandi la memoria", "trova buchi", "migliora le skill"
- fine task complesso, se supera soglia di tool calls/LLM calls/durata

Fasi:

1. Scan: leggere index, log, lint, source inbox, task recenti, skill list.
2. Gap detection: trovare buchi, duplicati, stale claims, procedure ripetute.
3. Spawn: creare task paralleli per librarian/researcher/critic/skillsmith.
4. Aggregate: unire risultati e deduplicare.
5. Propose: creare wiki proposals, source tasks, scheduler tasks, skill proposals.
6. Apply safe changes: solo operazioni deterministicamente sicure.
7. Notify: inviare riepilogo all'owner o dashboard.

## Scrittura Wiki

Il collo di bottiglia attuale non si risolve solo con subagent:
`WritePage` aggiorna index, log e git commit per ogni pagina. Per ingest o
update multipagina serve un writer piu' efficiente.

Milestone consigliate:

1. Aggiungere batch write interno:
   - scrive N pagine
   - aggiorna index una volta
   - append log una volta
   - git commit una volta
   - reindex solo pagine toccate
2. Aggiungere `propose_wiki_change` / `apply_wiki_change`.
3. Fare generare agli AuraBot solo proposte, poi un single writer applica.

## Skill Learning Loop

Aura deve imparare procedure, non solo fatti.

Quando una conversazione o uno swarm completa un lavoro complesso, il
`skillsmith` valuta se esiste una procedura riutilizzabile.

Una skill proposta deve includere:

- trigger: quando usarla
- skip: quando non usarla
- workflow passo-passo
- tool necessari e limiti
- input attesi
- output attesi
- failure modes
- esempi minimi
- note di sicurezza

Le skill non si auto-installano in MVP. Passano da review.

## Configurazione

```env
AURABOT_ENABLED=false
AURABOT_MAX_ACTIVE=4
AURABOT_MAX_DEPTH=1
AURABOT_TIMEOUT_SEC=90
AURABOT_MAX_ITERATIONS=5
AURABOT_DEFAULT_MODE=async
AURABOT_GROWTH_ENABLED=false
AURABOT_GROWTH_DAILY_AT=04:00
SKILL_PROPOSALS_ENABLED=true
SKILL_AUTO_INSTALL=false
```

Defaults intenzionalmente conservativi.

## Security

- Nessun `exec` general-purpose nel primo MVP.
- Nessun filesystem broad nel primo MVP.
- Nessuna skill installata senza review/admin.
- MCP solo se gia' configurato e allowlistato per ruolo.
- `create_xlsx`, `create_docx`, `create_pdf` fuori dagli agenti figli di default.
- `request_dashboard_token` mai disponibile agli AuraBot figli.
- Tool con side effect separati da tool read-only.
- Output dei tool troncato e salvato senza segreti.

## Rollout

| Slice | Cosa | Effetto |
| --- | --- | --- |
| 1 | `internal/agent.Runner` bounded | Mini tool loop riusabile, nessun comportamento pubblico nuovo |
| 2 | SQLite `swarm_runs` / `swarm_tasks` + `SwarmManager` | Task paralleli persistenti e limitati |
| 3 | `spawn_aurabot`, `list_swarm_tasks`, `read_swarm_result` | Subagent attivabili dietro `AURABOT_ENABLED` |
| 4 | Ruoli read-only: librarian, critic, researcher | Parallelizzazione sicura di lettura, lint e ricerca |
| 5 | Writer/proposal path: `propose_wiki_change` + single writer | Sintesi parallela senza race sulle pagine |
| 6 | Batch wiki write / deferred index-log-commit | Riduce latenza reale di ingest e update multipagina |
| 7 | `skill_proposals` + `propose_skill` | Aura distilla procedure senza auto-modificarsi |
| 8 | Dashboard/Telegram review per skill proposals | L'utente approva, rifiuta o modifica nuove skill |
| 9 | `second_brain_growth` scheduled task | Aura trova buchi e propone espansioni autonomamente |
| 10 | Bounded child spawn depth 2 | Agent teams piu' ricchi, ancora contenuti |

## Acceptance Criteria

- Un task read-only puo' spawnare 2-4 AuraBot e aggregare risultati.
- Nessun AuraBot figlio vede tool non allowlistati.
- Timeout, max active e max depth sono testati.
- Risultati task sopravvivono a restart perche' persistiti in SQLite.
- La wiki non viene scritta in parallelo da piu' agenti.
- Una skill proposal valida appare in dashboard/review queue.
- Una skill approvata viene installata e caricata dal loader esistente.
- Il growth loop puo' produrre almeno un riepilogo con: buchi trovati,
  fonti suggerite, wiki proposals, skill proposals e follow-up.

## Non-goals MVP

- Nessuna ricorsione libera.
- Nessun terminal backend tipo Hermes.
- Nessun cloud worker.
- Nessun self-install automatico di skill.
- Nessun filesystem/exec broad.
- Nessun protocollo chat real-time tra agenti; risultati via task store.

## Nota finale

La forma utile per Aura non e' "tanti agenti onnipotenti", ma un organismo a
ruoli: legge in parallelo, pensa in parallelo, propone in parallelo, ma scrive
con disciplina. Questo mantiene il second brain vivo senza perdere integrita'.
