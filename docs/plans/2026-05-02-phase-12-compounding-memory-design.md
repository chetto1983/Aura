# Phase 12 — Compounding Memory: Design

**Date:** 2026-05-02
**Status:** approved (user, in brainstorming session)
**Execution model:** Claude Code Agent Teams (3 teammate, functional split: backend / frontend / Q&A)

---

## 1. Thesis

Aura oggi ricorda solo quello che **l'utente decide** di scrivere a mano nella wiki o di caricare come source. La conversazione, dove avviene la maggior parte del segnale (decisioni, fatti nuovi su persone/progetti, conclusioni di ricerca), è effimera: dopo `MAX_HISTORY_MESSAGES` la storia evapora.

Phase 12 chiude il loop: **ogni conversazione contribuisce alla memoria durabile**. Aura archivia i turn, estrae deltas di conoscenza, propone (o applica) aggiornamenti wiki, e mantiene la base pulita con maintenance schedulata.

Tre componenti accoppiati:

1. **Conversation Archive** — persistenza durabile dei turn (fonte di verità storica + base per estrazione).
2. **Auto-summarization / delta extractor** — post-turn callback che identifica fatti nuovi, dedup contro wiki esistente, propone update.
3. **Proactive maintenance** — scheduler job notturno che gira `lint_wiki`, ripara link rotti, segnala stale.

Risultato osservabile: dopo 1 settimana di uso, la wiki contiene osservazioni che l'utente non ha mai scritto a mano, e nessuna è duplicata o orfana.

---

## 2. Architecture

### 2.1 Conversation Archive (T1, backend)

Nuova tabella SQLite `conversations`:

```sql
CREATE TABLE conversations (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  chat_id INTEGER NOT NULL,
  user_id INTEGER NOT NULL,
  turn_index INTEGER NOT NULL,
  role TEXT NOT NULL,             -- 'user' | 'assistant' | 'tool'
  content TEXT NOT NULL,
  tool_calls TEXT,                -- JSON, null per role!=assistant
  tool_call_id TEXT,              -- per role=tool
  llm_calls INTEGER DEFAULT 0,    -- da turnStats slice 11r
  tool_calls_count INTEGER DEFAULT 0,
  elapsed_ms INTEGER DEFAULT 0,
  tokens_in INTEGER DEFAULT 0,
  tokens_out INTEGER DEFAULT 0,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_conv_chat ON conversations(chat_id, turn_index);
CREATE INDEX idx_conv_user ON conversations(user_id, created_at);
```

Hook nell'orchestrator: dopo ogni `runToolCallingLoop` con `Done=true`, scrive batch di message del turn. Niente blocking: usa `go store.Append(...)` con buffered channel size 100.

API endpoints:
* `GET /api/conversations` — lista per `chat_id` o `user_id`, paginata.
* `GET /api/conversations/{id}` — turn detail con tool_calls espansi.

### 2.2 Auto-summarization (T2, backend)

Nuovo pacchetto `internal/conversation/summarizer/`. Ogni N turn (default `SUMMARIZER_TURN_INTERVAL=5`, configurabile, 0=disabled) o quando il modello emette `mark_for_extraction` tool, gira un'estrazione:

1. **Salience scoring** (LLM, temperature=0): legge ultimi K turn, restituisce candidate facts con score 0-1, source span. Schema:
   ```json
   {
     "candidates": [
       {"fact": "...", "score": 0.85, "category": "person|project|preference|fact|todo",
        "related_slugs": ["...","..."], "source_turn_ids": [123, 124]}
     ]
   }
   ```
2. **Threshold gate**: solo `score >= SUMMARIZER_MIN_SALIENCE` (default 0.7) procede.
3. **Dedup**: per ogni candidate, `search_wiki(fact, 3)`. Se top result similarity > 0.85 → **skip** (già coperto). Se 0.5–0.85 → **append-as-update** alla pagina esistente (patch mode). Se < 0.5 → **new-page candidate**.
4. **Apply mode** (config `SUMMARIZER_MODE`):
   * `auto` (default): scrive direttamente via `wiki.Store.WritePage`/append, logga in `wiki/log.md` con prefix `[auto-sum]`. Undoable rivedendo `log.md`.
   * `review`: scrive in `proposed_updates` SQLite table; dashboard `/summaries` mostra diff per approve/reject.
   * `off`: solo log, niente write.
5. **Audit trail**: ogni write include `source_turn_ids` in frontmatter `evidence:` field, così può essere rintracciata.

Vincoli di sicurezza:
* Cap a 5 candidate per estrazione (anti-explosion).
* Cap a 1 estrazione ogni 60s per chat (anti-loop).
* Skip durante un tool-loop attivo (l'estrattore vede solo turn `Done=true`).

### 2.3 Proactive maintenance (T3, backend)

Nuovo task kind `wiki_maintenance` esiste già (slice 8). Lo riempiamo:

* **Lint pass**: chiama `wiki.Store.Lint(ctx)`, ottiene `[]LintIssue` (broken links, orphans, missing categories, stale).
* **Auto-fix sicuri**: link broken con un'unica candidate slug match (Levenshtein distance ≤ 2) → riparato automaticamente, log entry in `log.md`.
* **Issue queue**: il resto va in nuova SQLite table `wiki_issues` (severity, kind, slug, detected_at).
* **Notifica**: se ≥ 1 issue severity=high, `Bot.SendToOwner` con summary "Aura ha trovato N issue nella wiki, dashboard /maintenance".

Schedulato ogni notte 03:15 (offset di 15 min dal nightly bootstrap esistente per evitare lock contention).

### 2.4 Frontend (T2, frontend)

Tre nuove route nella dashboard:

* **`/conversations`** — table view. Filtri: chat_id, range data, `has_tools`. Click su row → drawer con turn-by-turn timeline (user / assistant / tool color-coded). Esporta JSON.
* **`/summaries`** — solo se `SUMMARIZER_MODE=review`. Cards con fact proposto, candidate page (esistente vs nuova), source turn link, score. Approve → POST applica + log; Reject → soft-delete in `proposed_updates`.
* **`/maintenance`** — issue queue da `wiki_issues`. Filtri severity. Per ogni issue: snippet contesto, suggested fix (se auto-fixable: button "Apply", altrimenti "Mark resolved" o "Open page").

Dashboard `/` (health) gains a fourth metric card: "Compounding rate" = `(auto_added_pages_7d / total_pages) * 100`. Rough proxy della velocità di crescita organica.

### 2.5 Q&A (T3, QA)

* Unit tests per ogni modulo: archive store, summarizer scoring + dedup branch coverage, maintenance auto-fix + queue.
* Integration: nuovo `cmd/debug_summarizer` che inietta una mock conversation (5 turn con fatto nuovo + fatto ridondante + fatto a basso score), gira summarizer end-to-end, asserisce che SOLO il fatto nuovo entra in wiki.
* Race testing: `go test -race ./internal/conversation/...`.
* Smoke E2E: live test via Telegram ("Ti dico che ho deciso X" → verificare entry `[auto-sum]` in log.md entro 30s).
* Code review pass finale (Opus 4.7).

---

## 3. Team Composition (Agent Teams)

| Teammate | Model | Owns | Slice ownership |
|----------|-------|------|-----------------|
| **Backend** | Sonnet 4.6 | `internal/conversation/`, `internal/scheduler/`, new SQLite migrations, `internal/api/conversations.go`, `internal/api/summaries.go`, `internal/api/maintenance.go`, `internal/telegram/bot.go` (hook wiring) | T1 + T2 backend + T3 backend |
| **Frontend** | Sonnet 4.6 | `web/src/components/ConversationsPanel.tsx`, `SummariesPanel.tsx`, `MaintenancePanel.tsx`, `web/src/api.ts` extensions, `web/src/types/api.ts`, sidebar nav, keyboard shortcuts. Rebuild `internal/api/dist/` | T1+T2+T3 UI surfaces |
| **Q&A** | Sonnet 4.6 | `*_test.go` files in modificati packages, `cmd/debug_summarizer/`, e2e harness, lint fixture, race/coverage runs | All test artifacts |

**Lead** (this session): Sonnet 4.6 (after manual `/model` switch). Orchestrates spawn, dispatches via task list, sintetizza milestone status. Escala a Opus solo se hard arch question.

**Code review pass**: Opus 4.7 single shot a milestone done.

**Disjoint files invariant**:
* Backend touches `internal/`, `cmd/`, `*.sql` migrations.
* Frontend touches `web/`, regenerates `internal/api/dist/`.
* Q&A touches `*_test.go` and `cmd/debug_*/`.
* Conflict zone: `internal/api/dist/` (frontend regenerates, backend may need to bump after wiring). Resolution: frontend always last in dependency chain for any slice that touches both.

---

## 4. Data Flow

```
User Telegram message
        ↓
runToolCallingLoop completes (Done=true)
        ↓
hook → ConversationArchive.Append(turn)  ──→ SQLite conversations
        ↓
turn_count_for_chat % SUMMARIZER_TURN_INTERVAL == 0?
        ├── no → end
        └── yes → Summarizer.Extract(last K turns)
                      ↓
                LLM scoring (temperature=0)
                      ↓
                filter score >= MIN_SALIENCE
                      ↓
                for each candidate:
                  search_wiki(fact, 3)
                      ↓
                    similarity > 0.85 → SKIP (log)
                    0.5 < sim ≤ 0.85 → APPEND patch
                    sim ≤ 0.5         → NEW page candidate
                      ↓
                mode=auto: WritePage + log.md [auto-sum]
                mode=review: insert proposed_updates
                mode=off: log only

Nightly 03:15 (scheduler)
        ↓
WikiMaintenance.Run()
        ↓
wiki.Store.Lint() → []LintIssue
        ↓
auto-fixable (Levenshtein ≤ 2 single match) → repair + log
rest → wiki_issues table
        ↓
high-severity count > 0 → Bot.SendToOwner notification
```

---

## 5. Configuration (additions to .env.example)

```bash
# Conversation archive
CONV_ARCHIVE_ENABLED=true

# Auto-summarization
SUMMARIZER_ENABLED=true
SUMMARIZER_MODE=auto             # auto | review | off
SUMMARIZER_TURN_INTERVAL=5       # extract every N turns (0=disabled)
SUMMARIZER_MIN_SALIENCE=0.7      # threshold 0-1
SUMMARIZER_LOOKBACK_TURNS=10     # how many recent turns to scan
SUMMARIZER_MAX_CANDIDATES=5      # cap per extraction
SUMMARIZER_COOLDOWN_SEC=60       # min seconds between extractions per chat

# Maintenance
WIKI_MAINTENANCE_ENABLED=true
WIKI_MAINTENANCE_AUTOFIX=true    # auto-repair Levenshtein-1 broken links
WIKI_MAINTENANCE_NOTIFY_HIGH=true
```

---

## 6. Slice Plan (rough — refined by writing-plans)

Grouped by teammate ownership. Numerated 12.x to align with PDR convention.

### Backend (Sonnet)
* **12a** — SQLite `conversations` table + `archive.Store` + tests.
* **12b** — Hook nell'orchestrator: post-turn append (non-blocking, buffered).
* **12c** — `GET /api/conversations` + `GET /api/conversations/{id}` endpoints.
* **12d** — `internal/conversation/summarizer/` package: scoring LLM client, schema, dedup helper.
* **12e** — Summarizer post-turn callback wiring + cooldown.
* **12f** — Apply paths (auto, review SQLite table, off).
* **12g** — `wiki_maintenance` task implementation + auto-fix Levenshtein.
* **12h** — `wiki_issues` SQLite + notify-on-high.
* **12i** — Compounding-rate metric on `/api/health`.

### Frontend (Sonnet)
* **12j** — `/conversations` route + drawer + filters.
* **12k** — `/summaries` route (gated on `SUMMARIZER_MODE=review`).
* **12l** — `/maintenance` route + issue cards + auto-fix button.
* **12m** — Compounding-rate card on dashboard health.
* **12n** — Sidebar nav + chord shortcuts (`g v` conversations, `g u` summaries, `g x` maintenance).

### Q&A (Sonnet, with Opus pass at end)
* **12o** — Unit tests: archive store branch coverage.
* **12p** — Unit tests: summarizer scoring + dedup branches.
* **12q** — Unit tests: maintenance auto-fix + queue.
* **12r** — `cmd/debug_summarizer` integration harness.
* **12s** — Live E2E checklist (Telegram script).
* **12t** — Race + coverage report.
* **12u** — Final Opus 4.7 code review pass → REVIEW.md → optional fix-up commits.

Total: 21 slice (≈ 7 per teammate). Realistico per 1-2 settimane di lavoro continuo con team in parallelo.

---

## 7. Risk & Mitigation

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Wiki explosion (auto-sum genera troppe pagine) | medium | Threshold 0.7, cap 5 candidate/estrazione, cooldown 60s, dedup obbligatoria pre-write |
| LLM costo gonfiato dal summarizer | medium | TURN_INTERVAL=5 default + temperature=0 (cacheable). Stima: 1 estrazione ogni ~5 turn × ~2k tokens = trascurabile vs costi conversation principale |
| Race tra teammate su `internal/api/dist/` | low | Frontend rebuilda solo dopo che il backend ha mergiato i suoi commit del giorno (task list dependency esplicita) |
| Auto-fix link sbagliato per Levenshtein collision | low | Solo single-match candidate (no ambiguity). Log `[auto-fix]` permette undo. |
| User vuole vedere proposte prima di apply | controllabile | `SUMMARIZER_MODE=review` esiste day-1; switchabile via env senza recompile |
| Conversation archive SQLite cresce all'infinito | medium-long-term | Aggiungere `archive_retention_days` (default 365) come follow-up — fuori scope Phase 12 |

---

## 8. Out of Scope (deferred to Phase 13+)

* **File creation milestone** (xlsx/docx/pdf) — Phase 13.
* **Structured wiki pages** (frontmatter tipato per budget tracking) — Phase 14.
* **Cross-conversation semantic search** sopra l'archive — può essere un follow-up se utile (richiede embedding sui turn, non gratuito).
* **Multi-utente isolation** del summarizer — oggi `chat_id` based, fine per single-owner deployment. Multi-tenant sharding è Phase 15+.
* **Summarizer LLM model selection** — usa lo stesso `LLM_*` config. Modello dedicato (es. cheaper Haiku via separate config) è ottimizzazione successiva.

---

## 9. Definition of Done

* Tutti i 21 slice committati su master, ognuno atomico.
* `go test ./...` + `go test -race ./internal/conversation/... ./internal/scheduler/... ./internal/api/...` green.
* Frontend lint + tsc + build clean.
* Live E2E: 5 conversazioni di test producono almeno 1 entry `[auto-sum]` in `log.md`.
* Notturno gira una volta in shadow (con log) prima di abilitare auto-fix.
* Opus code review pass produce `docs/REVIEW.md` con findings classificati; high-severity fissati prima di chiudere.
* `prd.md` v4.1: aggiunta sezione "5.3 Compounding Memory" + roadmap Phase 12 = done.
* `docs/implementation-tracker.md` aggiornato con tutte le 12.x.

---

## 10. Manual prerequisite (user)

Aggiungere a `D:\Aura\.claude\settings.json`:

```json
{
  "enabledPlugins": { "ralph-skills@ralph-marketplace": true },
  "env": {
    "CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS": "1"
  }
}
```

(Il flag è auto-modification protetta — Claude non può flipparlo da sé.)

Poi nuova sessione Claude Code per attivare il flag.

---

**End of design.**
