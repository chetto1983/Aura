# Aura Daily Questions Gap Audit

Date: 2026-05-03

Goal: build a realistic set of daily user questions and check what Aura can do today, what is fragile, and what is truly missing to make it useful as a proactive second brain.

## 20 Daily Questions

| # | User question | Expected useful behavior | Current coverage | What is missing |
| - | ------------- | ------------------------ | ---------------- | --------------- |
| 1 | Cosa sai di me e cosa dovrei aggiornare? | Search memory, summarize stable facts, flag stale or missing facts. | Partial: `search_wiki`, speculative wiki context, `read_wiki`. | Staleness scoring and "missing profile fields" checklist. |
| 2 | Riassumimi cosa e' successo oggi nelle mie conversazioni. | Read today's archive, extract decisions/tasks/facts, propose wiki updates. | Partial: conversation archive + summarizer exists. | User-facing daily digest command/tool and source-linked summary. |
| 3 | Quali cose importanti non ho ancora salvato nella wiki? | Inspect recent turns and propose reviewable wiki updates. | Partial: `propose_wiki_change` exists after latest code, review queue exists. | Runtime must be restarted; better proposal origin metadata and dedupe UI. |
| 4 | Ricordami cosa devo fare oggi. | List active tasks due today with local time and status. | Partial: `list_tasks`. | Natural filtering by date/recipient and concise "today" view. |
| 5 | Ricordami domani alle 9 di chiamare X. | Schedule one-shot reminder in local timezone. | Good: `schedule_task` supports `at_local` and `in`. | Confirmation should show local time and timezone consistently. |
| 6 | Ogni giorno feriale alle 10 cerca segnali di trading e salvali. | Run an autonomous weekday job: web search, synthesize, review/save. | Weak: scheduler only gives LLM a reminder/daily path. | Weekday schedules plus agentic scheduled job kind with tool allowlist. |
| 7 | Cerca online questa cosa e ricordala se e' utile. | Web search, source attribution, then write or propose durable memory. | Partial: `web_search`, `web_fetch`, `write_wiki`, `propose_wiki_change`. | Policy/routing: prefer review proposal for uncertain external info unless user explicitly says save. |
| 8 | Ho mandato un PDF: cosa contiene e cosa devo farci? | OCR, ingest, summary page, action items, links. | Good: PDF upload, OCR, ingest, sources, wiki page. | Action-item extraction and follow-up task proposals. |
| 9 | Trova nei miei documenti la clausola/prezzo/scadenza X. | Search sources + wiki, read exact evidence, cite source/page. | Partial: source read/list + wiki search. | Unified source search over OCR text with page-level citations. |
| 10 | Crea un file Excel/Word/PDF con questi dati. | Generate file, store source, send over Telegram. | Good: `create_xlsx`, `create_docx`, `create_pdf` shipped. | Templates/styles and "reuse previous document format". |
| 11 | Dammi un briefing mattutino. | Calendar/tasks/wiki/recent changes/news, concise agenda. | Weak: pieces exist, no briefing orchestrator. | Briefing job/tool with sections, preferences, and reviewable outputs. |
| 12 | Che decisioni abbiamo preso su Aura questa settimana? | Search archive/wiki, group decisions, link evidence. | Partial: archive + wiki. | Dedicated decision extraction/index and date-range filters. |
| 13 | Quali pagine wiki sono rotte o obsolete? | Lint wiki, graph health, propose fixes. | Good for lint/maintenance. | Obsolescence detection based on age, contradictions, and low confidence. |
| 14 | Collega queste note tra loro. | Find related pages, suggest links, update review queue. | Partial: wiki related fields + proposals. | Link-suggestion engine and approval flow that patches related slugs. |
| 15 | Installa una skill per fare X. | Search catalog, explain fit, install if admin, then use it. | Good: skills catalog/admin + loader. | Safer skill evaluation sandbox and post-install smoke test. |
| 16 | Usa agenti paralleli per capire cosa manca nel mio second brain. | Run read-only AuraBot team, synthesize gaps with metrics. | Good in code, but runtime currently disabled. | Restart/apply settings; then ensure LLM routes broad audits to swarm. |
| 17 | Monitora ogni giorno questa cosa online e avvisami solo se cambia. | Scheduled watch with web fetch/search, diff, threshold, notify/save. | Missing. | Watch task kind, last-result storage, diffing, alert thresholds. |
| 18 | Fammi vedere il perche' di una risposta. | Show compact evidence trail: wiki pages, sources, web URLs, tool calls. | Partial: logs/dashboard and tool outputs. | User-facing citation/evidence envelope per answer. |
| 19 | Pulisci la memoria duplicata o contraddittoria. | Detect duplicates/conflicts, propose merge/patches. | Partial: summarizer deduper and wiki lint. | Conflict detection, merge proposals, approval UI. |
| 20 | Se noti una cosa importante, proponimela senza aspettare che te lo chieda. | Proactive proposal after useful discoveries, never silent mutation. | Partial: `propose_wiki_change` prompt/tool. | Runtime activation, notification strategy, proposal quality scoring. |

## Additional Daily Questions

### Morning, Planning, And Tasks

| # | User question | Expected useful behavior | Current coverage | What is missing |
| - | ------------- | ------------------------ | ---------------- | --------------- |
| 21 | Cosa ho in agenda oggi e cosa rischia di saltare? | Combine due tasks, recent commitments, unresolved proposals, and risk flags. | Partial: tasks + archive exist. | A "today agenda" read model and commitment extraction. |
| 22 | Ho 30 minuti liberi: cosa mi conviene fare? | Rank small tasks by urgency, value, and context. | Weak. | Task metadata: effort, priority, deadline, context, dependencies. |
| 23 | Quali promemoria sono inutili o duplicati? | Audit tasks, find duplicates/stale rows, propose cleanup. | Partial: `list_tasks`, dashboard delete/cancel. | Task dedupe and natural-language cleanup proposal. |
| 24 | Trasforma questa conversazione in una lista di azioni. | Extract action items, owners, due dates, and create reminders/proposals. | Partial: LLM can reason, scheduler can create reminders. | Structured action-item tool and review before scheduling many tasks. |
| 25 | Ogni lunedi fammi il piano della settimana. | Weekly recurring agent job that reads memory and tasks. | Weak: API has `every_minutes`, LLM tool lacks weekly/weekday semantics. | Calendar recurrence rules plus agent job kind. |
| 26 | Cosa sto rimandando da troppo tempo? | Analyze stale tasks, repeated reminders, recurring topics. | Missing. | Task history, snooze/count metadata, "stuck" detector. |
| 27 | Ricordami questa cosa solo se non l'ho gia' fatta. | Conditional reminder based on state/evidence. | Missing. | Task predicates, completion signals, and check-before-notify behavior. |
| 28 | Quando mi conviene fare X? | Suggest a time based on existing tasks and user preference. | Weak. | Availability model, preferences, and calendar integration or local schedule store. |

### Knowledge, Wiki, And Memory

| # | User question | Expected useful behavior | Current coverage | What is missing |
| - | ------------- | ------------------------ | ---------------- | --------------- |
| 29 | Cosa e' cambiato nella mia wiki questa settimana? | List changed pages, new pages, proposals, maintenance issues. | Partial: git/wiki files and proposals exist. | Wiki change feed exposed as tool/API. |
| 30 | Quali pagine dovrei leggere o aggiornare oggi? | Rank stale/high-value pages. | Weak. | Page freshness, importance, and review cadence metadata. |
| 31 | Questa informazione contraddice qualcosa che sai gia'? | Search related memory, detect conflict, propose patch. | Partial: search/read wiki. | Conflict-detection prompt/tool with review-gated merge. |
| 32 | Fammi una mappa del mio progetto Aura. | Build graph summary from wiki/docs/sources and show clusters. | Partial: dashboard graph + swarm read audit. | Graph query/summarization tool and exportable map. |
| 33 | Trova tutti i punti dove parliamo di X. | Search wiki, sources, archive, and return evidence. | Partial: wiki search and archive storage. | Unified search across wiki/source/archive with filters. |
| 34 | Salva questa preferenza e usala da ora in poi. | Write durable user preference and inject it in future context. | Partial: `write_wiki`, prompt overlays. | Preference-specific schema and automatic context selection. |
| 35 | Aggiorna questa pagina senza duplicare contenuto. | Patch an existing page semantically. | Partial: `write_wiki` overwrites by title; proposals can patch by append. | True `update_wiki` diff/merge tool. |
| 36 | Che cosa non sai ancora di me ma sarebbe utile sapere? | Ask focused profile-gap questions. | Missing. | User profile schema and progressive onboarding questions. |

### Sources, Documents, And Evidence

| # | User question | Expected useful behavior | Current coverage | What is missing |
| - | ------------- | ------------------------ | ---------------- | --------------- |
| 37 | Che documenti ho caricato ultimamente? | List recent sources, status, errors, and ingest pages. | Partial: `list_sources`, dashboard source inbox. | Natural recent filter and Telegram-friendly summary. |
| 38 | Confronta questi due PDF. | OCR/read both, compare differences, cite pages. | Partial: source read after OCR. | Multi-source compare tool with page-level chunks. |
| 39 | Estrai le scadenze da questo documento e ricordamele. | OCR, extract dates, create reviewable tasks. | Partial: OCR + scheduler. | Date/entity extraction pipeline with review before task creation. |
| 40 | Trova la fonte originale di questa nota. | Resolve wiki claim to source URL/source ID/turn ID. | Weak. | Evidence fields on wiki pages and source-turn linking. |
| 41 | Crea un report con le fonti usate. | Generate DOCX/PDF with citations. | Partial: create_docx/create_pdf. | Citation model and source bibliography generation. |
| 42 | Questo PDF e' gia' stato caricato? | Dedup by sha256 and show existing source. | Good: source sha256 dedup. | Better user-facing duplicate explanation. |
| 43 | Leggi questa pagina web e salvala come fonte. | Fetch URL, store source, optionally ingest/wiki proposal. | Partial: `web_fetch`, `store_source`. | Pipeline from URL -> source -> summary -> proposal. |
| 44 | Dammi solo le prove, senza opinioni. | Return evidence snippets with citations. | Partial. | Evidence-only response mode. |

### Web Monitoring And External Context

| # | User question | Expected useful behavior | Current coverage | What is missing |
| - | ------------- | ------------------------ | ---------------- | --------------- |
| 45 | Monitora questa pagina e dimmi se cambia. | Store URL, scheduled fetch, diff, notify. | Missing. | Web watch task kind and last snapshot storage. |
| 46 | Ogni mattina dammi 3 notizie rilevanti per i miei progetti. | Search web guided by wiki/project context. | Weak. | Context-aware briefing agent job and relevance scoring. |
| 47 | Verifica se questa informazione e' ancora vera. | Web search/fetch, compare with memory, propose correction. | Partial: web + wiki. | Fact verification workflow with dated evidence. |
| 48 | Se una fonte e' poco affidabile, segnalamelo. | Source quality checks and uncertainty labels. | Weak. | Source reputation/recency heuristics and confidence labels. |
| 49 | Cerca alternative migliori a questo tool/servizio. | Web search, compare options, save/propose outcome. | Partial: web search + wiki. | Decision matrix generator and durable comparison schema. |
| 50 | Tieni traccia dei prezzi o disponibilita'. | Scheduled search/fetch, numeric extraction, threshold alert. | Missing. | Watcher with extraction rules and threshold triggers. |
| 51 | Dimmi cosa e' cambiato in una repo/prodotto. | Fetch release notes/changelog, summarize delta. | Partial via web if URL known. | Changelog watcher and GitHub/MCP integration path. |
| 52 | Ricordati solo le cose confermate da fonti affidabili. | Save only sourced stable claims. | Partial. | Save policy based on evidence strength and review status. |

### Proactivity, Skills, And Agent Teams

| # | User question | Expected useful behavior | Current coverage | What is missing |
| - | ------------- | ------------------------ | ---------------- | --------------- |
| 53 | Cosa potresti imparare a fare meglio per aiutarmi? | Inspect installed skills, catalog, common tasks, suggest additions. | Partial: skills tools + swarm skillsmith. | Skill gap analyzer and post-install verification. |
| 54 | Crea una skill per questa procedura ricorrente. | Turn repeated workflow into a local skill with instructions/tests. | Weak: skill system exists, creation not productized. | Skill authoring workflow, review, and safe install path. |
| 55 | Dividi questo problema tra piu' agenti e dammi una sintesi. | Run AuraBot swarm with roles and metrics. | Good in code; runtime disabled in current log. | Activation verification and prompt routing hardening. |
| 56 | Quando rispondi lento, dimmi dove hai perso tempo. | Show LLM/tool timing and bottlenecks. | Partial: logs have `elapsed_ms`, tool timings, swarm metrics. | User-facing latency breakdown on demand. |
| 57 | Se sto facendo confusione, fammi domande mirate. | Detect ambiguity and ask one or two clarifying questions. | Partial prompt behavior. | Intent/ambiguity classifier and domain-specific question templates. |
| 58 | Suggeriscimi un miglioramento alla wiki dopo ogni ricerca importante. | Propose durable update in review queue. | Partial: `propose_wiki_change`. | Proposal quality scoring, dedupe, notification threshold. |
| 59 | Non salvare automaticamente cose rischiose. | Prefer review queue for sensitive/uncertain facts. | Partial prompt policy. | Hard write policy for categories: finance, health, secrets, personal data. |
| 60 | Fai un audit settimanale del second brain e proponi 5 miglioramenti. | Scheduled swarm read audit -> proposals -> dashboard review. | Missing as automated routine. | Agentic scheduled job plus swarm-to-proposal pipeline. |

## Capability Heatmap

| Capability | Today | Why it matters | Next improvement |
| ---------- | ----- | -------------- | ---------------- |
| Wiki memory lookup | Strong | The assistant can answer from durable memory. | Unified search with sources and archive. |
| Wiki mutation | Medium | Direct writes work, but semantic patching is limited. | Add `update_wiki` diff/merge and stronger proposal approval. |
| Proactive memory growth | Medium in code, weak at runtime | `propose_wiki_change` is implemented but depends on restart and routing quality. | Activate runtime, add proposal quality/dedupe metadata. |
| Document ingestion | Strong | PDF OCR and source ingest are already practical. | Extract actions/deadlines and page-level citations. |
| Source search | Medium | Sources exist, but retrieval is not as unified as wiki search. | Index OCR/source chunks with source/page references. |
| Scheduling | Medium | Reminders, daily, every-minutes, maintenance exist. | Weekdays and agentic scheduled jobs. |
| Autonomous routines | Weak | Daily usefulness needs recurring procedures, not just reminders. | `agent_job` task kind with tool allowlist and metrics. |
| Swarm/parallel agents | Strong in code, weak live | The implementation exists with metrics, but live process logged disabled. | Restart/settings sanity check and route broad audits to swarm. |
| Evidence/citations | Medium | Tool outputs have references, but answers do not consistently preserve them. | Internal evidence envelope and "show sources" response mode. |
| Skills | Medium-strong | Catalog/install/read exist. | Skill creation workflow and smoke tests. |
| Dashboard review | Medium | Pending proposals, tasks, sources, swarm panels exist. | Better proposal provenance, batch approve/reject, relation patching. |
| Daily command center | Weak | The stores exist but no single daily view/tool. | `daily_briefing` tool and scheduled briefing job. |

## Workable Product Epics

### Epic A: Agentic Scheduled Jobs

User-visible promise: "Aura can run useful routines for me, not only remind me to run them."

Minimum useful slice:

- Add scheduler kind `agent_job`.
- Store `goal`, `tool_allowlist`, `write_policy`, `recipient_id`, `last_result`, `last_error`, `last_metrics`.
- Dispatcher runs a bounded agent or AuraBot swarm when the task fires.
- Default write policy: propose-only, no direct wiki mutation.
- Telegram notification includes summary, proposals created, sources checked, and elapsed time.

First acceptance prompts:

- "Ogni giorno feriale alle 10 cerca segnali di trading e proponimi un aggiornamento wiki."
- "Ogni mattina controlla le fonti su AuraBot e dimmi cosa e' cambiato."
- "Ogni lunedi fammi un audit del second brain."

### Epic B: Weekday And Calendar-Like Recurrence

User-visible promise: "Quando dico giorni feriali, Aura capisce davvero giorni feriali."

Minimum useful slice:

- Add recurrence field for weekdays, e.g. `weekdays=mon,tue,wed,thu,fri`.
- Support `daily` plus weekdays at first; avoid full cron until needed.
- Expose it in `schedule_task`, API, and dashboard.
- Make confirmations say local time, weekdays, and next run.

First acceptance prompts:

- "Ogni giorno feriale alle 10 ricordami X."
- "Ogni lunedi alle 9 fai manutenzione wiki."
- "Solo sabato e domenica alle 11 fammi il riepilogo."

### Epic C: Daily Briefing

User-visible promise: "Aura knows what deserves attention today."

Minimum useful slice:

- Add read-only `daily_briefing` tool.
- Sections: due tasks, stale tasks, recent wiki changes, pending proposals, source ingest failures, wiki issues, recent important conversation facts.
- Keep output short enough for Telegram.
- Later schedule it as an `agent_job`.

First acceptance prompts:

- "Dammi il briefing di oggi."
- "Cosa devo fare oggi?"
- "Cosa e' cambiato da ieri?"

### Epic D: Unified Evidence Search

User-visible promise: "Aura can answer from memory and show why."

Minimum useful slice:

- Index source OCR/text chunks alongside wiki pages.
- Include `source_id`, filename, page number when available, wiki slug, and score.
- Add a `search_memory` tool that queries wiki + sources + optionally archive snippets.
- Preserve compact evidence in final answers.

First acceptance prompts:

- "Trova nei miei documenti la scadenza del contratto."
- "Fammi vedere il perche' della risposta."
- "Quali fonti supportano questa nota?"

### Epic E: Wiki Curation And Semantic Updates

User-visible promise: "La wiki cresce senza diventare spazzatura."

Minimum useful slice:

- Add `update_wiki` for semantic patch/diff, not whole-page rewrite.
- Add link suggestions and duplicate/conflict proposals.
- Surface proposal origin: tool, source IDs, turn IDs, swarm run/task IDs.
- Add batch approve/reject in dashboard.

First acceptance prompts:

- "Aggiorna questa pagina senza duplicare."
- "Trova contraddizioni nella wiki."
- "Collega queste note dove ha senso."

### Epic F: Skill Creation Workflow

User-visible promise: "Aura puo' trasformare procedure ripetute in capacita' riusabili."

Minimum useful slice:

- Add a guided "create skill draft" flow that writes a reviewable SKILL.md proposal.
- Include trigger rules, safe tool boundaries, and a smoke prompt.
- Install only after explicit approval.
- Record skill provenance in wiki/proposal queue.

First acceptance prompts:

- "Crea una skill per fare il mio briefing mattutino."
- "Trasforma questa procedura in una skill riutilizzabile."
- "Che skill mi manca per lavorare meglio?"

## Prioritized Backlog From This Audit

Status note: slice 17h closed the recurrence parity part of this backlog. `schedule_task` now exposes `every_minutes`, daily `weekdays`, and dashboard/API/backend parity for business-day schedules. The remaining P0 is live-runtime activation sanity for AuraBot/proposals.

| Priority | Slice | Why first | Risk |
| -------- | ----- | --------- | ---- |
| P0 | Restart/settings sanity for AuraBot + `propose_wiki_change` | New code is useless if the live bot does not load it. | Low |
| Done | Expose `every_minutes` in `schedule_task` or align tool/API recurrence | Shipped in slice 17h. | Low |
| Done | Add weekday recurrence | Shipped in slice 17h. | Medium |
| P1 | Add `agent_job` scheduler kind, propose-only | Converts reminders into autonomous bounded routines. | Medium-high |
| P1 | Add `daily_briefing` read-only tool | High daily value, low mutation risk. | Medium |
| P1 | Add source/wiki unified search | Unlocks document Q&A with evidence. | Medium |
| P2 | Evidence envelope | Improves trust across all answers. | Medium |
| P2 | Proposal provenance + batch review | Makes proactivity manageable. | Medium |
| P2 | `update_wiki` semantic patch | Prevents duplicate/overwrite memory damage. | Medium-high |
| P3 | Skill creation workflow | Powerful, but safer after review/proposal path is strong. | Medium |

## Test Prompts For Future E2E

Use these as repeatable natural-language smoke tests:

1. "Cosa sai di me e cosa dovrei aggiornare?"
2. "Dammi il briefing di oggi in 5 punti."
3. "Ogni giorno feriale alle 10 cerca segnali di trading e proponi un aggiornamento wiki."
4. "Monitora questa pagina e avvisami solo se cambia: https://example.com"
5. "Ho 30 minuti liberi, cosa mi conviene fare?"
6. "Trova nei miei documenti la prossima scadenza."
7. "Fammi vedere il perche' della risposta precedente."
8. "Trova contraddizioni nella wiki e proponi correzioni."
9. "Crea una skill per questa procedura ricorrente."
10. "Usa agenti paralleli per auditare tutto il second brain e dimmi cosa manca."

## What Works Today

- Basic durable memory: read, search, and write wiki pages.
- External lookup: web search and fetch when Ollama web credentials are available.
- Document ingestion: PDF OCR, source inbox, auto-ingest into wiki.
- File generation: XLSX, DOCX, PDF creation and Telegram delivery.
- Reminders and maintenance: one-shot/daily reminders and nightly wiki maintenance.
- Review-gated growth: pending wiki proposals exist in code.
- Read-only parallel investigation: AuraBot swarm exists in code with metrics and dashboard.

## What Is Still Not Useful Enough

1. Scheduled tasks are not agentic enough.

   The daily trading example exposed the real gap. The scheduler can fire `reminder` and `wiki_maintenance`, but it cannot yet run a saved agent procedure like "search web -> synthesize -> propose wiki update -> notify". The LLM therefore downgraded the user's request into a reminder.

2. Recurrence is too coarse for daily life.

   The LLM tool currently exposes `daily`, `at`, `at_local`, and `in`; it does not expose weekdays, weekends, or calendar-like rules. Backend recurrence has `every`, but the LLM-facing tool is behind the product need.

3. Runtime and settings need a hard sanity check.

   The DB says `AURABOT_ENABLED=true`, but the live log says `AuraBot swarm disabled`. Until restart/settings application is verified, the most useful new behavior is present in code but absent in the running bot.

4. Evidence is not first-class enough.

   Aura can read wiki/source/web, but the answer does not always carry a compact evidence trail. For a second brain, trust grows when the user can see "this came from page X, source Y, turn Z".

5. The wiki can grow, but not yet curate itself.

   `propose_wiki_change` is the right direction, but usefulness depends on quality: dedupe, confidence, target page, source turn IDs, and easy approval/reject from dashboard.

6. There is no daily command center.

   Users will ask "today", "this week", "what changed", "what should I do". Aura has the raw stores, but lacks a daily briefing/digest layer that composes tasks, wiki changes, conversations, sources, and watch results.

## Highest-Value Next Slices

1. **Agentic scheduled jobs**

   Add a scheduler kind like `agent_job` with a saved goal, read/write policy, tool allowlist, last result, and metrics. First target: weekday web-monitoring job that proposes wiki updates instead of directly mutating.

2. **Weekday recurrence**

   Extend scheduler store/API/tool with weekdays (`mon,tue,wed,thu,fri`) and update `schedule_task` prompt/params. This fixes the exact "giorni feriali alle 10" failure.

3. **Runtime activation check**

   Restart Aura, verify the boot log says `AuraBot swarm enabled`, and add a settings/debug endpoint or boot self-test that reports effective config source for swarm/summarizer.

4. **Daily briefing tool**

   Add a read-only tool that returns today's tasks, recent conversation summaries, pending proposals, wiki issues, and source ingest status. Later it can be scheduled as an `agent_job`.

5. **Evidence envelope**

   Standardize a compact answer metadata block internally: wiki slugs, source IDs, web URLs, conversation turn IDs, swarm run IDs. The user sees it only when useful or when asking "perche'?".

## Bottom Line

Aura is already useful for memory lookup, document ingestion, file creation, reminders, and broad read-only audits. The missing step from "assistant with tools" to "daily second brain" is autonomous but bounded procedure execution: scheduled agent jobs, recurrence rules, evidence trails, and review-gated memory growth.
