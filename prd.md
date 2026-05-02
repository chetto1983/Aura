# Aura — Personal AI Agent with Compounding Memory

**Product Requirements Document — Version 4.1**
**Date:** 2026-05-02
**Status:** reflects shipped state through slice 12u (Phase 12 complete; compounding memory: conversation archive + auto-summarization + wiki maintenance).

---

# 1. Executive Summary

Aura è un agente AI personale, local-first, accessibile via Telegram, che accumula conoscenza in una **wiki markdown maintained-by-the-LLM** e si estende con **tool agentici** (source ingestion, web, scheduler, skills, MCP). Una **dashboard web embedded** offre osservabilità e controllo (sources, wiki/graph, tasks, skills, MCP, pending users) protetta da bearer-token emessi via Telegram.

Rispetto alla v3.0 (planning-only) la v4.0 documenta lo stato realmente in produzione: SQLite invece di PostgreSQL, OpenAI-compat HTTP come client primario, pipeline OCR Mistral integrata, dashboard React embedded nel binario, skills.sh + MCP come superfici di estensione, scheduler autonomo persistito, streaming Telegram con markdown→HTML.

Principi invariati: **determinismo, semplicità, file-system + SQLite, controllo esplicito, fallback sempre disponibili**.

---

# 2. Design Principles

1. **Determinismo > Creatività** — temperature=0 per scritture wiki, prompt/schema versioning.
2. **Semplicità > Astrazione prematura** — nessun DAG, 1 goroutine = 1 conversazione.
3. **File system + SQLite > sistemi distribuiti** — wiki su disco + Git, stato runtime in SQLite locale.
4. **Controllo esplicito > automazione opaca** — feature flag, allowlist, admin gates per operazioni privilegiate.
5. **Fallback sempre disponibili** — Ollama offline, embedding cache, single-message progress.
6. **Progressive disclosure** — skills/MCP/sources caricano in contesto solo quando il modello li richiede.

---

# 3. System Architecture

## 3.1 Core Loop

```
User → Telegram → Orchestrator → LLM (streaming + tool calls)
                                    ↓
                         ┌──────────┴──────────┐
                         │  Tool Registry       │
                         │  - source/wiki/tasks │
                         │  - web search/fetch  │
                         │  - skills/MCP        │
                         └──────────┬──────────┘
                                    ↓
                Wiki (MD + frontmatter + [[links]])
                Sources (raw PDF + ocr.json + ocr.md)
                SQLite (auth, scheduler, embed cache)
```

In parallelo gira la **Web Dashboard** (`internal/api`) embedded nel binario via `//go:embed`, e il **Tray icon** (Windows) per aprire la dashboard.

## 3.2 Execution Model

* 1 goroutine = 1 conversazione (Telegram).
* Tool-calling loop con `MAX_TOOL_ITERATIONS` (default 10).
* Tool calls indipendenti nello stesso turn vengono **eseguite in parallelo** (slice 11l).
* Streaming LLM con **progressive edit Telegram** (slice 11t): placeholder dopo 30 char, edit ogni 800 ms.
* Cap di history a `MAX_HISTORY_MESSAGES` (default 50) — Picobot pattern, slice 11k. Summarization solo come fallback.
* Speculative wiki retrieval (slice 11p): `search_wiki` viene già fatto prima del primo round LLM e iniettato nel system prompt.

---

# 4. Core Components

## 4.1 Orchestrator (`internal/orchestrator`)

* Gestione conversazioni (1 per chat).
* Loop tool-calling deterministico.
* Retry con exponential backoff (max `LLM_MAX_RETRIES`).
* Budget enforcement (soft warning + hard halt).
* Per-turn telemetry: `elapsed_ms`, `llm_calls`, `tool_calls` (slice 11r).

## 4.2 LLM Client Layer (`internal/llm`)

### Interface

```go
type Client interface {
    Send(ctx, Request) (Response, error)
    Stream(ctx, Request) (<-chan Token, error)
}
```

### Implementations

* **OpenAI-compatible HTTP client** (primary) — `LLM_BASE_URL` + `LLM_API_KEY`.
* **Ollama client** (fallback / offline) — `OLLAMA_BASE_URL` + `OLLAMA_MODEL`.

`Stream()` supporta tool-calls via `stream_options.include_usage` e accumula i frammenti `function.arguments` per indice (slice 11s) — i consumer non vedono mai JSON parziale.

### Embedding (separato dal chat model)

* `EMBEDDING_BASE_URL=https://api.mistral.ai/v1`, `EMBEDDING_MODEL=mistral-embed`, `EMBEDDING_API_KEY` dedicata. Nessun fallback automatico verso `LLM_API_KEY`.

---

## 4.3 Wiki System (`internal/wiki`)

### Storage

* File system + Git (`go-git/go-git/v5`).
* Path: `WIKI_PATH` (default `./wiki`).

### Format

Markdown con YAML frontmatter (migrazione completata da `.yaml` → `.md`):

```markdown
---
title: ...
slug: ...
schema_version: 2
prompt_version: ingest_v1
category: ...
tags: [...]
related: [other-slug, another-slug]
sources: [src_<id>]
---
Body markdown con [[wiki-links]] in stile Obsidian.
```

### Special files

* `index.md` — auto-generato per categoria.
* `log.md` — append-only audit trail (azione, slug, timestamp).
* `SCHEMA.md` — documentazione formato.

### Write safety

* File-level mutex.
* Atomic write (temp + rename).
* `MigrateYAMLToMD` one-shot al boot.

---

## 4.4 Source Store + OCR (`internal/source`, `internal/ocr`, `internal/ingest`)

### Source

* Layout: `wiki/raw/src_<sha256-16hex>/{original.pdf, source.json, ocr.json, ocr.md}`.
* Sha256-based dedup, atomic `source.json` write, per-id mutex.
* Stati: `stored | ocr_complete | ingested | failed`.

### OCR

* Mistral Document AI (`/v1/ocr`), bearer auth, base64 PDF.
* Wire flags: `table_format`, `extract_header`, `extract_footer`, `include_image_base64`.
* Render in `ocr.md` con layout PDR §4 (`# Source OCR: <filename>`, `## Page N`).
* Cap: `OCR_MAX_PAGES`, `OCR_MAX_FILE_MB`.

### Ingestion

* Pipeline `internal/ingest.Pipeline.Compile` — LLM-driven, produce summary page con `[[wiki-link]]`.
* Auto-trigger via `docHandler.AfterOCR` (Telegram upload) o `POST /api/sources/upload` (browser).
* Catch-up via tool `ingest_source` per source pre-hook.

---

## 4.5 Tools (`internal/tools`)

Registry condiviso con il modello. Ogni tool implementa `Name/Description/Parameters/Execute`. Le tool-call concorrenti vengono parallelizzate (slice 11l).

### Built-in tools

| Categoria | Tool |
| --------- | ---- |
| Web | `web_search`, `web_fetch` (Ollama API) |
| Wiki | `write_wiki`, `read_wiki`, `search_wiki`, `list_wiki`, `lint_wiki`, `rebuild_index`, `append_log` |
| Source | `store_source`, `ocr_source`, `read_source`, `list_sources`, `lint_sources`, `ingest_source` |
| Scheduler | `schedule_task`, `list_tasks`, `cancel_task` |
| Skills | `read_skill` (progressive disclosure body fetch) |
| Auth | `request_dashboard_token` (out-of-band token via Telegram) |
| MCP | `mcp_<server>_<tool>` (registrato dinamicamente) |

### Argument logging policy

Solo nomi tool e chiavi degli argomenti vengono loggati (slice 5/registry). Mai contenuto raw, URL con token, base64, o testo source.

---

## 4.6 Scheduler (`internal/scheduler`)

* SQLite-backed (`scheduled_tasks` table).
* Kinds: `reminder`, `wiki_maintenance`.
* Schedule fields: `at` (one-shot), `daily HH:MM`, `in <duration>`, `at_local HH:MM`.
* Goroutine autonoma con bootstrap di un job nightly 03:00.
* Runtime time context iniettato nel system prompt.

---

## 4.7 Skills (`internal/skills`)

* Anthropic skill format: `skills/<name>/SKILL.md` con frontmatter (`name`, `description`).
* Multi-root loader: `SKILLS_PATH` (default `./skills`) + `.claude/skills` (priorità a primario).
* **Progressive disclosure** (slice 11f): system prompt include solo manifest `- **name** — description`. Body caricato on-demand via `read_skill`.
* Loader memoizzato per 1s (slice 11m).
* Catalogo: `SKILLS_CATALOG_URL=https://skills.sh/`.
* Admin install/delete dietro `SKILLS_ADMIN=true` (default off): `npx skills add` con env sanitizzato (drop secrets), 90s timeout, containment + symlink refusal.

---

## 4.8 MCP (`internal/mcp`)

* Picobot-port: stdio + Streamable-HTTP transports, JSON-RPC 2.0.
* Init flow: `initialize` → `tools/list` → `tools/call`.
* Config: `MCP_SERVERS_PATH=./mcp.json` (`mcp.example.json` tracked).
* Boot non-fatale: failure di un server è warning, mai abort.
* Tool wrapper espone come `mcp_<server>_<tool>` nel registry standard.
* Dashboard `/mcp` + `POST /api/mcp/{server}/tools/{tool}` per invocazione manuale (60s timeout, 64 KiB body/output cap).

---

## 4.9 Web Dashboard (`internal/api` + `web/`)

* React 19 + Vite + react-router-dom v7.
* Build → `internal/api/dist/`, embedded via `//go:embed all:dist`.
* Listener: `HTTP_PORT` (default `127.0.0.1:8080`).
* Routes: `/` health, `/wiki`, `/wiki/:slug`, `/wiki/graph`, `/sources`, `/tasks`, `/skills`, `/mcp`, `/pending`, `/login`.
* Theme: palette derivata dal logo (deep navy + electric cyan), light/dark/contrast in oklch, ambient aurora background, brand glow su nav attivo.
* UX: skeleton placeholders, mobile drawer, keyboard chord shortcuts (`g h/w/g/s/t/k/m`), help dialog (`?`).

### Auth

* Bearer token in header `Authorization: Bearer <token>`.
* Tokens hashed (SHA-256) in `api_tokens` (SQLite).
* Emessi via tool `request_dashboard_token` → consegnati out-of-band via Telegram (`Bot.SendToUser`). Plaintext mai nei log.
* Endpoints: `GET /api/auth/whoami`, `POST /api/auth/logout`.
* Sign-out in sidebar; 401 → redirect `/login?expired=1`.

### Write actions

* Sources: ingest, re-OCR, upload (PDF drop-zone).
* Wiki: rebuild index, append log.
* Tasks: schedule one-time / daily, cancel.
* Skills (admin-gated): install, delete.
* MCP: invoke tool con form auto-seedato dallo schema.

---

## 4.10 Tray Icon (`internal/tray`, Windows)

* Multi-resolution `.ico` con weighted centroid + circular mask.
* Voce "Open Dashboard" lancia browser su `HTTP_PORT`.
* Quit pulisce shutdown.

---

## 4.11 Telegram Interface (`internal/telegram`)

* `telebot.v4`.
* PDF handler: validate + bounded concurrency (2), single-message progress edit.
* /start approval queue (slice 11o): unknown user → `pending_users` + fan-out notifica agli owner; approve/deny dalla dashboard. TOFU bootstrap conservato per il primo /start su install vergine.
* Markdown → HTML renderer (slice 11u): converte `**`/`##`/`-`/`[link]` nel sottoinsieme Telegram (`b/i/s/u/code/pre/a/blockquote`). Headings → `<b>`, bullets → `•`. Schema URL ristretto a http(s)/tg.
* Streaming: progressive edit con rate-limit 800 ms.

---

## 4.12 Conversation Context (`internal/conversation`)

* `active_context` (sliding window) + `rolling_summary` + `transcript`.
* Cap principale: `MAX_HISTORY_MESSAGES` (Picobot pattern). Summarization solo per messaggi singoli patologicamente grandi.
* Tool-result messages mantenuti accoppiati ai relativi assistant-tool-call durante trim.
* Speculative search: `SetSearchContext` chiamato prima del primo LLM call.

### Prompt overlay files (slice 11q)

Letti ogni turn da `PROMPT_OVERLAY_PATH` (default `.`):
* `SOUL.md` — personality.
* `AGENTS.md` — collaboration norms.
* `USER.md` — durable user facts.
* `TOOLS.md` — tool guidance.

Tutti opzionali; modificabili a runtime senza recompile.

---

## 4.13 Search (`internal/search`)

* Primary: chromem-go (vector search) sulla wiki indicizzata.
* Mirror: SQLite FTS per fallback testuale.
* **Embedding cache** SHA-keyed (slice 11h): `embedding_cache(content_sha, model)` in SQLite. Cold start invariato; warm restart skippa Mistral round-trip per pagine immutate.
* **Concurrent indexing** (slice 11i): `coll.AddDocuments` parallelo (`indexConcurrency=4`).
* Stats esposte su `/api/health` (`hits`/`misses`/`hit-rate`).

---

## 4.14 Health & Observability (`internal/health`, `internal/tracing`, `internal/logging`)

* `GET /api/health`: process block (version, git_revision, started_at, uptime_seconds), embed cache stats, scheduler status.
* Logging strutturato via `zap`. Nessun secret nei log.
* OpenTelemetry opt-in (`OTEL_ENABLED`).
* Per-turn structured log "conversation complete" (slice 11r).

---

## 4.15 Config (`internal/config`)

* `envconfig`.
* `.env.example` tracked, `.env` gitignored runtime.
* Caricato esplicitamente all'avvio di `cmd/aura` (no shell env required).

---

# 5. Memory Model

## 5.1 Layers

* **Raw** — `wiki/raw/<source_id>/` (PDF originale + OCR durabile).
* **Wiki** — `wiki/<slug>.md` (markdown maintained dal modello).
* **Schema** — `wiki/SCHEMA.md` + frontmatter validation.
* **Conversation (in-memory)** — cap a 50 messaggi, summary durabile via wiki.
* **Conversation archive (SQLite, slice 12a–12c)** — ogni turno persistito su `conversations` table; tool_calls JSON + per-turn telemetry sul ruolo assistant.
* **Embedding cache** — SQLite, riusa embed tra restart.

## 5.2 Deterministic Mode (MANDATORY per wiki writes)

* `temperature = 0`.
* `prompt_version`, `schema_version` su ogni pagina (regex: `v{n}` | `ingest_v{n}` | `summarizer_v{n}`).
* Atomic write + Git commit per ogni cambio.

## 5.3 Compounding Memory (Phase 12)

La memoria si compone automaticamente: ogni conversazione contribuisce conoscenza durevole, la wiki si manutiene da sola, la dashboard espone le superfici nuove.

### Pipeline

1. **Archive** (slice 12a–12c). `BufferedAppender` (chan 100, drain goroutine, drop-on-full warn) archivia ogni messaggio nel `conversations` table dietro `CONV_ARCHIVE_ENABLED=true`. `turn_index` allocato monotonicamente da `MAX(turn_index) WHERE chat_id = ?` per resistere ai trim di `EnforceLimit`. API: `GET /api/conversations[?chat_id]&limit=`, `GET /api/conversations/{id}`. Dashboard route: `/conversations` con drawer per turno + tool_calls expanded.
2. **Summarize** (slice 12d–12f, 12k.1). Dopo ogni `SUMMARIZER_INTERVAL=5` turni un `Runner` chiama lo `LLMScorer` (temperature=0) sui `SUMMARIZER_LOOKBACK=10` turni più recenti, filtra per `SUMMARIZER_MIN_SALIENCE`, dedup contro la wiki via similarity (>0.85 skip, ≥0.5 patch, <0.5 new). Apply paths via `SUMMARIZER_MODE`: `auto` scrive direttamente la wiki con `prompt_version=summarizer_v1`; `review` insert in `proposed_updates` per approvazione dashboard (`/summaries`); `off` no-op (early-return prima del LLM call per evitare cost leak). Cooldown per-chat in-process map.
3. **Maintain** (slice 12g–12h, 12l.1). `MaintenanceJob` notturno chiama `wiki.Lint`, computa Levenshtein vs slug esistenti; un solo candidato ≤2 → auto-fix via `RepairLink`; ambigui → enqueue in `wiki_issues` (severity policy: `broken_link_unfixable=high, orphan=med, missing_category=low`). High-severity → `notifyOwner` via `Bot.SendToOwner`. API: `GET /api/maintenance/issues[?status,severity]`, `POST /api/maintenance/issues/{id}/resolve`. Dashboard route: `/maintenance` raggruppato per severity.
4. **Compounding rate** (slice 12i, 12m). `/api/health` espone `compounding_rate { auto_added_7d, total_pages, rate_pct }` calcolato da `[auto-sum]` lines in `wiki/log.md` ultimi 7d / `wiki.Store.ListPages`. Dashboard: 5° card `HealthDashboard` con `TrendingUp` icon.
5. **Nav** (slice 12n). Sidebar items + chord shortcuts: `g v` /conversations, `g u` /summaries, `g x` /maintenance.

### Configurazione

```env
CONV_ARCHIVE_ENABLED=true              # write turns to SQLite archive
SUMMARIZER_ENABLED=true                # post-turn extraction
SUMMARIZER_MODE=off                    # off | review | auto
SUMMARIZER_INTERVAL=5                  # extract every N turns
SUMMARIZER_LOOKBACK=10                 # turns passed to scorer
SUMMARIZER_MIN_SALIENCE=0.5            # filter threshold
SUMMARIZER_COOLDOWN_SECONDS=60         # min seconds between extractions per chat
```

### Migrazioni / dati legacy

`scheduler.Store.migrate` esegue `dropLegacyConversations` come passo idempotente: rileva un eventuale `conversations` table preesistente (privo di colonna `chat_id`, residuo di `internal/search/sqlite.go` rimosso in 12.cleanup) e la rimuove prima di applicare lo schema Phase 12. Una sola corsa, silente.

---

# 6. Persistence

* **SQLite** (`DB_PATH`, default `./aura.db`):
  * `api_tokens` (auth dashboard)
  * `pending_users` (queue /start)
  * `allowed_users` (allowlist runtime)
  * `scheduled_tasks` (scheduler)
  * `embedding_cache` (search)
* **File system** (wiki/raw + wiki/<slug>.md + index/log) versionato in Git.
* **Conversations**: in-memory (history cap), summary durabile via wiki tools quando rilevante.

---

# 7. Cost Control

* Token tracking per conversazione + globale.
* Cost prediction prima di ogni LLM call.
* Soft budget → warning Telegram. Hard budget → halt.
* `/api/health` espone cache hit-rate per monitorare risparmio embedding.

---

# 8. Security

* Telegram allowlist (env + `allowed_users` SQLite).
* /start approval queue post-bootstrap.
* Dashboard bearer-only, token mai nei response body.
* MCP servers opt-in; skills install dietro `SKILLS_ADMIN`.
* Tool argument logging: solo chiavi.
* No secrets nei log.
* Path containment + symlink refusal per file-system mutations.
* Markdown→HTML renderer rifiuta schemi `javascript:` ecc.

---

# 9. Deployment

* **Cross-platform binari** via GoReleaser (linux/darwin/windows × amd64/arm64).
* Docker multi-stage Alpine (server-only path).
* Tray icon su Windows; headless su altre piattaforme (`tray_other.go` no-op).
* Web dashboard embedded nel binario — single artifact.

---

# 10. Testing Strategy

* **Unit**: `go test ./...` deve restare verde su ogni slice.
* **Integration**: `cmd/debug_ingest`, `cmd/debug_tools`, `cmd/debug_llm` per smoke test naturali.
* **Live OCR**: build tag `live_ocr` per round-trip Mistral reali (`RERENDER_DIRS` per re-render hermetico).
* **Race-clean**: `go test -race ./internal/{api,auth,mcp,skills,...}` su PR critiche.
* **Benchmarks** (slice 11n): skills loader cached/uncached, registry sequential/parallel.

---

# 11. Observability

* `/api/health` — rollup health.
* Process info via `runtime/debug.ReadBuildInfo` (version, git revision).
* Structured per-turn telemetry.
* Embed-cache stats card su dashboard.

---

# 12. Roadmap (storica)

| Phase | Stato | Note |
| ----- | ----- | ---- |
| 1 — Core bot + wiki MD migration | done | Slices preliminari, migrazione YAML→MD. |
| 2 — PDF/OCR + source store + ingestion | done | Slices 1–6. |
| 3 — Wiki maintenance + scheduler + natural-prompt tests | done | Slices 7–9. |
| 4 — Web dashboard (read+write+auth+polish) | done | Slices 10a–10e + browser upload. |
| 5 — MCP + skills + dashboard panels + admin install | done | Slices 11a–11e. |
| 6 — Skill format hardening + embed cache + concurrent indexing | done | Slices 11f–11j. |
| 7 — Smart-and-fast (history cap, parallel tools, skills cache, benchmarks, latency telemetry) | done | Slices 11k–11n, 11r. |
| 8 — Pending-user gate + speculative wiki + prompt overlay | done | Slices 11o–11q. |
| 9 — Streaming end-to-end + progressive Telegram edit + Markdown→HTML | done | Slices 11s–11u. |
| 12 — Compounding memory (archive + summarizer + maintenance) | done | Slices 12a–12u + 12u.1–12u.7 follow-ups. v0.12.0. |
| **Next — Phase 13** — `internal/telegram/bot.go` god-class refactor | not started | bot.go a 1394 LOC / 33 funzioni; estrai handler/wiring submodules. |
| **Next** — File creation milestone | not started | xlsx/docx/pdf generation tools, Telegram delivery. |
| **Backlog** — REVIEW.md HR-01, HR-02 (RepairLink partial-commit, Category/RelatedSlugs lost on review-approve) | not started | v0.12.1. |

---

# 13. Final Notes

Aura non è un sistema complesso: è una pipeline deterministica con tool agentici discreti e una memoria che si compone nel tempo. La v4.0 documenta lo stato vivo. Ogni nuovo slice deve poter essere descritto in una riga in tabella e in un commit atomico.

---

**End of PRD v4.1**
