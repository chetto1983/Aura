# PRD: Aura — Personal AI Agent with Compounding Memory

**Version:** 4.0
**Date:** 2026-05-02
**Status:** reflects shipped state through slice 11u (Phase 11 complete; cross-platform binaries via GoReleaser).

## Introduction

Aura è un agente AI personale local-first accessibile via Telegram. Accumula conoscenza in una **wiki markdown maintained-by-the-LLM** (frontmatter + `[[wiki-links]]`) e si estende con tool agentici discreti: source/PDF ingestion via Mistral OCR, web search/fetch, scheduler persistito, skills (Anthropic format + skills.sh catalog), MCP servers (stdio + HTTP). Una **dashboard web embedded** offre osservabilità e write actions, gateway dietro bearer-token emessi via Telegram.

Rispetto alla v3.0 questa PRD documenta lo stato realmente in produzione: SQLite invece di PostgreSQL, OpenAI-compat HTTP come client primario, dashboard React embedded nel binario, streaming Telegram con markdown→HTML, /start approval queue, embedding cache.

## Goals

- Agente AI personale affidabile via Telegram con tool calling agentico.
- Memoria compounding via wiki markdown maintained-by-LLM, versionata in Git.
- Ingestione automatica di PDF in source store + wiki via Mistral OCR.
- Determinismo per scritture wiki (temperature=0, prompt/schema versioning).
- Cost control con token tracking, budget soft/hard.
- Dashboard web per osservabilità e azioni di amministrazione.
- Estensione tramite skills (Anthropic format) e MCP servers, sempre opt-in.
- Latenza percepita bassa: streaming + progressive Telegram edit + cache + parallelism.

## User Stories

### US-001: Conversazione via Telegram con tool calling
**Description:** Come utente, mando messaggi al bot Telegram e ricevo risposte usando tool che il modello sceglie automaticamente.

**Acceptance Criteria:**
- [x] Bot riceve solo da utenti allowlistati (env + `allowed_users` SQLite).
- [x] /start sconosciuto → `pending_users` queue con notifica fan-out agli owner.
- [x] Una goroutine per conversazione.
- [x] Tool calls indipendenti nello stesso turn eseguite in parallelo.
- [x] Streaming LLM con progressive Telegram edit (placeholder a 30 char, edit ogni 800 ms).
- [x] Markdown LLM convertito al subset HTML supportato da Telegram.

### US-002: Memoria wiki compounding
**Description:** Come utente, voglio che Aura ricordi e colleghi conoscenza tra conversazioni in una wiki ispezionabile.

**Acceptance Criteria:**
- [x] Wiki in markdown con frontmatter YAML (`schema_version`, `prompt_version`, `category`, `related`, `sources`).
- [x] Link `[[slug]]` parsati in `ExtractWikiLinks`.
- [x] `index.md` auto-generato per categoria, `log.md` append-only audit.
- [x] Schema validation prima di scrivere; retry con feedback.
- [x] Atomic write (temp + rename) + Git versioning.
- [x] File-level mutex contro corruzione.

### US-003: Ingestione PDF via OCR
**Description:** Come utente, carico un PDF su Telegram e Aura lo OCR-izza, lo conserva come source, e lo ingerisce nella wiki.

**Acceptance Criteria:**
- [x] Validazione MIME/size contro `OCR_MAX_FILE_MB`.
- [x] Bounded concurrency (2 OCR simultanee).
- [x] Single-message progress UX (initial → edit a ogni step → finale ✅/❌).
- [x] Sha256 dedup; layout `wiki/raw/src_<id>/{original.pdf, source.json, ocr.md, ocr.json}`.
- [x] Mistral OCR (`/v1/ocr`) con `table_format`, `extract_header`, `extract_footer`.
- [x] Auto-ingest via `AfterOCRHook` — produce source summary page con `[[wiki-link]]`.
- [x] Catch-up tool `ingest_source` per source pre-hook.
- [x] Browser upload via dashboard `POST /api/sources/upload` (stesso pipeline).

### US-004: Search wiki ibrido
**Description:** Come sistema, voglio combinare vector search e FTS sulla wiki.

**Acceptance Criteria:**
- [x] Primary: chromem-go (vector) con embedding Mistral.
- [x] Mirror: SQLite FTS per fallback testuale.
- [x] **Embedding cache** SHA-keyed in SQLite — warm restart skippa Mistral round-trip.
- [x] **Concurrent indexing** con `coll.AddDocuments` (`indexConcurrency=4`).
- [x] **Speculative retrieval** (slice 11p): `search_wiki` chiamato prima del primo LLM call.
- [x] Cache stats (`hits`/`misses`) esposti su `/api/health`.

### US-005: Scheduler persistito
**Description:** Come utente, voglio che Aura mi ricordi cose e mantenga job di manutenzione anche dopo restart.

**Acceptance Criteria:**
- [x] SQLite `scheduled_tasks` table.
- [x] Kinds: `reminder`, `wiki_maintenance`.
- [x] Schedule: `at` (one-shot), `daily HH:MM`, `in <duration>`, `at_local HH:MM`.
- [x] Goroutine autonoma con bootstrap nightly 03:00.
- [x] Tools: `schedule_task`, `list_tasks`, `cancel_task`.
- [x] Runtime time context iniettato nel system prompt.
- [x] Dashboard `/tasks` con New / Cancel.

### US-006: Cost control
**Description:** Come utente voglio limiti di budget per non sforare.

**Acceptance Criteria:**
- [x] Token tracking per conversazione + globale.
- [x] Cost prediction prima di ogni LLM call.
- [x] Soft budget → warning. Hard budget → halt.
- [x] Per-turn telemetry: `elapsed_ms`, `llm_calls`, `tool_calls`.
- [x] Embedding cache hit-rate visibile su dashboard.

### US-007: Skills system
**Description:** Come utente, voglio attivare skills (Anthropic format) con progressive disclosure.

**Acceptance Criteria:**
- [x] Loader multi-root: `SKILLS_PATH` + `.claude/skills`.
- [x] Manifest `- **name** — description` nel system prompt; body fetched on-demand via `read_skill`.
- [x] Loader memoizzato 1s.
- [x] Catalog browse via skills.sh; install/delete dietro `SKILLS_ADMIN=true`.
- [x] Install: `npx skills add` con env sanitizzato (drop secrets), 90s timeout.
- [x] Delete: containment + symlink refusal.
- [x] Dashboard `/skills` con Local + Catalog tabs.

### US-008: MCP integration
**Description:** Come utente, voglio collegare MCP server e usare i loro tool.

**Acceptance Criteria:**
- [x] Stdio + Streamable-HTTP transports.
- [x] JSON-RPC 2.0; init flow: `initialize` → `tools/list` → `tools/call`.
- [x] Config via `mcp.json` (gitignored runtime, `mcp.example.json` tracked).
- [x] Boot non-fatale; failure di server → warning.
- [x] Tools registrati come `mcp_<server>_<tool>` nel registry standard.
- [x] Dashboard `/mcp` con per-tool Run + JSON form auto-seedato dallo schema.
- [x] `POST /api/mcp/{server}/tools/{tool}` con 60s timeout, 64 KiB body/output cap.

### US-009: Dashboard con bearer auth
**Description:** Come utente, voglio una dashboard web sicura per ispezionare e amministrare Aura.

**Acceptance Criteria:**
- [x] React 19 + Vite, embedded nel binario via `//go:embed all:dist`.
- [x] Bearer token in header; storage hashed (SHA-256) in `api_tokens`.
- [x] Token emessi via tool `request_dashboard_token` → consegna out-of-band via Telegram.
- [x] Sign-out + 401 redirect con `?expired=1`.
- [x] Tema light/dark/contrast con palette derivata dal logo.
- [x] Mobile drawer, keyboard chord shortcuts (`g h/w/g/s/t/k/m`), help (`?`).
- [x] Routes: health / wiki / wiki graph / sources / tasks / skills / mcp / pending.

### US-010: Pending /start approval
**Description:** Come owner, voglio approvare manualmente nuovi utenti che fanno /start dopo bootstrap.

**Acceptance Criteria:**
- [x] Unknown /start → `pending_users` + Telegram fan-out a tutti gli owner.
- [x] Dashboard `/pending` polled ogni 8s con Approve/Deny.
- [x] Approve mint un fresh token e lo invia via Telegram out-of-band.
- [x] Spam `/start` non re-pinga gli owner finché la richiesta è pending.
- [x] TOFU bootstrap conservato per il primo /start su install vergine.

### US-011: Cross-platform deployment
**Description:** Come utente, voglio binari pronti per Linux/macOS/Windows.

**Acceptance Criteria:**
- [x] GoReleaser config per linux/darwin/windows × amd64/arm64.
- [x] Tray icon Windows con "Open Dashboard" + Quit.
- [x] Tray no-op su altre piattaforme (`tray_other.go`).
- [x] Single binary embed dashboard.
- [x] `cmd/aura` carica `.env` esplicitamente al boot.

### US-012: Prompt overlay files
**Description:** Come utente, voglio tunare personality / norme / fatti utente / tool guidance senza recompile.

**Acceptance Criteria:**
- [x] `PROMPT_OVERLAY_PATH` (default `.`) scansionato ogni turn.
- [x] File opzionali: `SOUL.md`, `AGENTS.md`, `USER.md`, `TOOLS.md`.
- [x] Cambi raccolti al turn successivo, no restart.

### US-013: Compounding Memory (Phase 12)
**Description:** Come utente, voglio che ogni conversazione contribuisca conoscenza durevole alla wiki, che la wiki si manutenga da sola, e che la dashboard mostri lo stato del compounding.

**Acceptance Criteria:**
- [x] Ogni turno Telegram archiviato in SQLite (`conversations` table) con tool_calls JSON e telemetry per-turn.
- [x] `BufferedAppender` non blocca l'hot path; drop-on-full warn invece di stallare.
- [x] `turn_index` allocato monotonicamente da `MAX(turn_index)` per chat — niente data loss anche se `EnforceLimit` trim.
- [x] `GET /api/conversations[?chat_id]&limit=` lista (chat_id opzionale → ultimi turni globali); `GET /api/conversations/{id}` detail con tool_calls.
- [x] Dashboard `/conversations` con drawer per turno, filtro chat_id/date/has_tools, JSON export.
- [x] Post-turn `Runner.MaybeExtract` ogni `SUMMARIZER_INTERVAL` turni, scoring temperature=0, dedup similarity (>0.85 skip / ≥0.5 patch / <0.5 new).
- [x] Apply paths gated by `SUMMARIZER_MODE`: `auto` writes wiki direttamente; `review` insert in `proposed_updates`; `off` no-op (early-return prima del LLM call).
- [x] Wiki pages auto-create con `prompt_version=summarizer_v1`, `tags: [auto-added]`, `sources: [turn:N]`.
- [x] Dashboard `/summaries` con cards approvabili/rejectabili (sonner toasts).
- [x] `MaintenanceJob` notturno: lint + Levenshtein auto-fix single-match; ambigui → `wiki_issues` queue con severity policy.
- [x] High-severity → owner DM via `Bot.SendToOwner` (single-fire per batch).
- [x] Dashboard `/maintenance` raggruppato per severity, resolve action.
- [x] `/api/health` espone `compounding_rate { auto_added_7d, total_pages, rate_pct }`.
- [x] Dashboard 5° HealthDashboard card "Compounding rate".
- [x] Sidebar nav + chord shortcuts: `g v`/`g u`/`g x`.
- [x] `dropLegacyConversations` migration idempotente per DB esistenti.
- [x] Test suite: 289 tests green; staticcheck U1000 zero findings; coverage core data-layer 100% per function.
- [x] Live E2E checklist completed during Phase 12; historical checklist content now lives in git history and the closure status is summarized in `docs/implementation-tracker.md`.

## Functional Requirements

- FR-1: Telegram bot con allowlist (env + `allowed_users` SQLite) e /start approval queue.
- FR-2: Orchestrator sequenziale (1 goroutine per conversazione), no DAG.
- FR-3: LLM client interface con `Send` + `Stream`; OpenAI-compatible HTTP primary, Ollama fallback.
- FR-4: Tool registry con execution parallela per tool indipendenti nello stesso turn.
- FR-5: Wiki MD + frontmatter, `[[links]]`, `index.md`, `log.md`, schema validation, atomic write, Git versioning.
- FR-6: Source store con sha256 dedup e per-id mutex.
- FR-7: Mistral OCR client con base64 PDF, render in `ocr.md` PDR §4.
- FR-8: Ingestion pipeline LLM-driven con auto-trigger via `AfterOCR`.
- FR-9: Embedding via Mistral con SHA-keyed cache in SQLite.
- FR-10: Scheduler SQLite-backed con kinds `reminder` / `wiki_maintenance`.
- FR-11: Skills loader multi-root con progressive disclosure manifest e cache 1s.
- FR-12: MCP client stdio + HTTP, tools come `mcp_<server>_<tool>`.
- FR-13: Web dashboard React 19 embedded, bearer auth, write actions gated.
- FR-14: Streaming Telegram con progressive edit (30 char threshold, 800 ms rate).
- FR-15: Markdown → HTML renderer per Telegram (subset b/i/s/u/code/pre/a/blockquote).
- FR-16: Token tracking + budget enforcement (soft warning, hard halt).
- FR-17: Per-turn telemetry: `elapsed_ms`, `llm_calls`, `tool_calls`.
- FR-18: Conversation history cap a `MAX_HISTORY_MESSAGES` (Picobot pattern).
- FR-19: Speculative `search_wiki` prima del primo LLM call.
- FR-20: Prompt overlay files letti ogni turn da `PROMPT_OVERLAY_PATH`.
- FR-21: Tray icon Windows con "Open Dashboard"; no-op altrove.
- FR-22: GoReleaser cross-platform binaries.

## Non-Goals

- No DAG engine o orchestrazione distribuita.
- No multi-agent coordination.
- No fine-tuning o training custom.
- No collaborazione real-time multi-utente.
- No cloud-hosted wiki (local-first only).
- No Postgres (rimosso a favore di SQLite per persistence runtime).
- No mobile app nativa — dashboard web responsive copre il caso.

## Design Considerations

- **Telegram-first ma non-only**: dashboard web è first-class, non secondaria.
- **Local-first**: file system + SQLite + Git; nessuna dipendenza cloud per il core.
- **Determinismo wiki**: temperature=0 + prompt/schema versioning su scritture.
- **Progressive disclosure**: skills/MCP/sources caricano in contesto solo quando il modello li chiede; manifest in system prompt resta tight.
- **Streaming-first UX**: progressive edit Telegram + spinner/skeleton dashboard.
- **Picobot patterns**: history cap, tool registry, prompt overlay files, parallel tool calls.
- **Sicurezza by default**: bearer auth dashboard, MCP/skills opt-in, admin gates su install/delete, no secrets nei log o nei tool args visibili.

## Technical Considerations

- **Language**: Go (single binary, goroutine-per-conversation).
- **HTTP server**: chi router.
- **Telegram**: telebot.v4.
- **DB**: SQLite via `database/sql` (no CGO requirement per chromem-go).
- **Vector search**: chromem-go con embedding Mistral + SHA cache.
- **OCR**: Mistral Document AI (`/v1/ocr`).
- **Wiki Git**: `go-git/go-git/v5`.
- **MCP**: implementazione custom Picobot-port.
- **Frontend**: React 19 + Vite + react-router-dom v7 + shadcn/ui + Tailwind, build embedded via `//go:embed`.
- **Logging**: zap.
- **Tracing**: OpenTelemetry opt-in.
- **Config**: envconfig + `.env` loader esplicito al boot.
- **Distribuzione**: GoReleaser multi-arch.

## Success Metrics

- Wiki writes con schema valida >99% (validation + retry).
- Tool argument logging zero-leak: nessun secret/contenuto raw nei log.
- Embedding cache hit-rate >80% dopo warm-up su wiki stabile.
- Per-turn latency p50 (no tool call) < 5s con streaming attivo.
- Cost dentro hard budget 100% del tempo.
- Tutti i test green (`go test ./...`) su ogni commit di slice.
- Single binary deploy starts in <10s.

## Open Questions

- Quando passare a `sqlite-vector` (CGO) per evitare round-trip embedding remoto in tutti i casi?
- File creation milestone (xlsx/docx/pdf): persistenza in source store o ephemeral tmp?
- Trust level di MCP servers — necessario un secondo gate oltre a `mcp.json` opt-in?
- Backup automatico SQLite + wiki/raw/ — strategia (Git LFS? rsync schedulato?).

## Roadmap

### Phase 1 — Core bot + wiki MD migration ✅
- Bot Telegram, orchestrator, LLM client.
- Migrazione wiki da YAML a MD + frontmatter.

### Phase 2 — PDF/OCR + source store + ingestion ✅
- Slices 1–6: config, source store, OCR client, Telegram PDF handler, source tools, ingestion pipeline.

### Phase 3 — Wiki maintenance + scheduler + natural-prompt tests ✅
- Slices 7–9: wiki tools, SQLite scheduler, cmd/debug_ingest.

### Phase 4 — Web dashboard ✅
- Slices 10a–10e + browser upload: read API, frontend scaffold, write actions, bearer auth, polish + theme.

### Phase 5 — MCP + skills + dashboard panels + admin install ✅
- Slices 11a–11e: MCP client, skills/MCP panels, install/delete admin gate, multi-root loader.

### Phase 6 — Skill format hardening + embed cache + concurrent indexing ✅
- Slices 11f–11j: progressive-disclosure manifest, install cwd fix, SHA-keyed embed cache, parallel index, health stats.

### Phase 7 — Smart-and-fast ✅
- Slices 11k–11n, 11r: history cap, parallel tools, skills cache, benchmarks, latency telemetry.

### Phase 8 — Pending-user gate + speculative wiki + prompt overlay ✅
- Slices 11o–11q.

### Phase 9 — Streaming end-to-end + Markdown→HTML ✅
- Slices 11s–11u.

### Phase 10 — File creation milestone (next)
- `create_xlsx` tool con `xuri/excelize/v2`.
- `send_document` wrapper Telegram (`tele.Document{File: tele.FromReader(...)}`).
- Persistenza in sources store.
- docx / pdf in slice successive.

---

**End of PRD v4.0**
