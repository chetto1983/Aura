# Phase 13: Channels + Telegram + Multimodal - Context

**Gathered:** 2026-06-07
**Status:** Ready for planning — **GATED on two pre-planning actions** (see Decisions D-01 and D-13)

<domain>
## Phase Boundary

Il primo canale production user-facing di Aura. La fase consegna:

1. **Channels framework** — `internal/channels/channel.go` (Channel interface ~70 LOC) + `registry.go` (StartAll/StopAll ~100 LOC, env `AURA_CHANNEL_<NAME>_ENABLED`), per daemon channel (Telegram ora, WhatsApp/Discord futuri).
2. **Telegram channel** (`internal/channels/telegram/`) — bot telebot.v4 polling, subscriber in-process del fanout AG-UI (`internal/agui/fanout.go`), status pane pattern B (2 msg/turn), escaper MarkdownV2 entity-aware, HITL inline keyboard/ForceReply, 10 commands bot-intercept, **tabelle→PNG** via x/image, **artifact delivery** via `send_file` tool + sendDocument.
3. **Setup wizard — SOLO API backend** (`internal/setup/`) — endpoint `/setup/*` token-gated su `:9081`; QR ASCII in terminale + deep-link via API; **nessuna pagina HTML** (frontend = prossima milestone).
4. **Multimodale 9c** — voice STT + image + documents (markitdown). **Engine + modello decisi da spike dedicato pre-plan** (survey modelli multimodali 2026 ≤4 GB VRAM + vLLM, misurato sulla GPU 4 GB di questo PC).

Requirements: UX-02 (9b), UX-03 (9a), UX-04 (9c). Slices PRD: 9a, 9b, 9c.

</domain>

<decisions>
## Implementation Decisions

### PRD amendments + scope (pre-planning gate)
- **D-01 — Amendment PRD COMMIT ORA, pre-planning.** L'amendment al `prd.md` viene scritto e committato PRIMA di `/gsd-plan-phase 13`. Contenuto: (a) telebot.v4 pin = **tag `v4.0.0-beta.9`** con CI gate = grep letterale in go.mod (amendment #5 stale: il repo è taggato dal 2026-06-02); (b) tabelle Telegram = **PNG primario** (renderer.go + tables.go, pre-block fallback, key-value card solo per 2-col key|value); (c) **artifact file delivery** via sendDocument (directive operatore 2026-06-07); (d) migration slot **0008→0012** (0008 è occupato da proxied_child_id_text); (e) descope wizard → solo API backend; (f) CLI non channel-ificata (vedi D-08); (g) 9c rimandata a verdetto spike (vedi D-13). ROADMAP success criterion 1 va emendato di conseguenza (niente pagina web).
- **D-02 — Tabelle-PNG + sendDocument DENTRO 9b** (no sub-slice dedicata). 9b passa ~920→~1150 LOC src: è tutto "come il bot parla".

### Setup wizard (9a)
- **D-03 — Solo API backend in Phase 13.** Niente `page.html` (PRD prevedeva ~250 LOC HTML embedded). Il frontend arriva nella **prossima milestone**. Endpoints: `POST /setup/token` (valida getMe), `POST /setup/onboard-link` (ritorna `{deep_link, qr_svg}`), `GET /setup/status`, `GET /setup/events` (SSE, poll DB 2s) — tutti token-gated (`AURA_SETUP_TOKEN` su stdout primo boot, amendment #10 invariato).
- **D-04 — Percorso operatore confermato:** bot token via `curl POST /setup/token` **oppure** env `TELEGRAM_BOT_TOKEN`; onboarding utente via **QR ASCII in terminale** (`qrterminal`, dep già PRD) + deep-link `t.me/<bot>?start=<token>`; il `qr_svg` JSON resta per il frontend futuro.

### Artifact delivery (9b)
- **D-05 — Tool `send_file` esplicito** `{path, caption?}`: l'agente decide quando consegnare. NIENTE auto-detect del renderer. Coerente con deferred-tool pattern + "Aura must always know her tools exist".
- **D-06 — Channel-agnostic via evento:** `send_file` emette un evento artifact generico nello stream AG-UI; ogni canale lo rende a modo suo (Telegram → sendDocument; CLI → stampa path; AG-UI → evento custom). Il substrate non conosce Telegram.
- **D-07 — Path policy: qualsiasi path leggibile.** Coerente con postura amendment #50 (full host terminal — shell_exec può già leggere tutto, allowlist = teatro di sicurezza). Gating futuro = capability_grants (Slice 1.7), non ceremonies.

### Channel framework + CLI (9a)
- **D-08 — CLI RESTA in `cmd/aura`** (chat.go/chat_render.go/chat_repl.go non si muovono). Il refactor PRD "chat.go → internal/channels/cli/" è ANNULLATO via amendment: superficie provata in Phase 12, la CLI REPL è on-demand, non un daemon channel. CLI = debug-only.
- **D-09 — Channel interface + registry da PRD** anche con un solo canale reale: deliverable UX-02, ~170 LOC totali, già dimensionato.

### Multimodale 9c
- **D-13 — SPIKE DEDICATO PRE-PLAN, 9c-blocking.** Prima di `/gsd-plan-phase 13` va fatta una sessione `/gsd-spike` accurata: **survey dei modelli multimodali 2026 (STT + vision) che girano in ≤4 GB VRAM**, serviti con **vLLM**, misurati sulla **GPU da 4 GB di questo PC (32 GB RAM)** — numeri di produzione reali. Metriche: WER su voice IT/EN, qualità vision, latenza p50/p95, VRAM/RAM steady. Il default PRD (llama.cpp + Gemma 4 E4B Q4) è il baseline di confronto / fallback CPU.
- **D-14 — Le open question PRD 9c (variante Gemma finale, vision fallback markitdown OCR) sono ASSORBITE dal verdetto dello spike.** Il design voice.go/photo.go non si pianifica prima del verdetto.

### Claude's Discretion
- Forma esatta dell'evento artifact AG-UI (custom event vs estensione tool_call_result) — il planner decide con lo stream reale in mano.
- Wiring per-run del fanout (Subscribe-before-Run: il canale Telegram costruisce translator+fanout per turno) — implementation detail guidato da `internal/agui/fanout.go`.
- Cap 50 MB Telegram su send_file (comportamento overflow), caption handling, flag Deferred del tool.
- Dettagli registry lifecycle/error-aggregation in serve.go.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Truth-source di fase
- `prd.md` §Slice 9 (righe ~2843-3172) — slicing 9a/9b/9c, decisioni Punti 1-8 (status pane B, throttle 1500/500/1000ms, HITL, 10 commands, documenti tiered, migration schema), acceptance per sub-slice. **DA LEGGERE DOPO l'amendment D-01** — alcune sezioni (telebot pin, wizard HTML, CLI refactor, 9c engine) saranno emendate.
- `.planning/ROADMAP.md` §Phase 13 — success criteria (criterion 1 da emendare per wizard API-only).
- `.planning/REQUIREMENTS.md` UX-02/UX-03/UX-04 — requirement mapping.

### Spike findings (ground truth validato live, BINDING per il planning)
- `.claude/skills/spike-findings-Aura/references/telegram-channel.md` — blueprint completo 9b: telebot tag pin, MarkdownV2 entity-aware (Pitfall #18 strict), tabelle PNG (pipeline x/image + gofont ~150 LOC, 5-21ms), sendDocument (path+filename bastano, MIME auto), send-response = read-back ground truth, cap 4096/1024 char, "What to Avoid".
- `.planning/spikes/MANIFEST.md` §Session-5 requirements — i 3 requirement operatore binding per `/gsd-plan-phase 13`.
- `.claude/skills/spike-findings-Aura/sources/017..019-*/` — harness spike riusabili come base test.

### Seam di integrazione
- `internal/agui/fanout.go` — il seam in-process che il canale Telegram consuma (Subscribe-before-Run obbligatorio, drop-on-full cap-64, goleak-clean). Costruito apposta in Phase 12.
- `internal/agui/translator.go` + `types.go` — stream eventi AG-UI che il renderer Telegram mappa.

### Pre-requisiti shippati (verificati nel codebase)
- `internal/conversations/store.go:418` `SearchConversationTurns` (pg_trgm) — backend di `/search`.
- CostUSD in `internal/runner`/`internal/conversations`/`internal/llm/prices` — backend di `/cost`.
- `internal/askuser` (pause/resume FIFO) — backend HITL inline keyboard.
- `cmd/aura/serve.go` — daemon dove la registry channels si monta.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/agui/fanout.go` — fanout in-process pronto, commentato esplicitamente come "the in-process seam the Phase-13 Telegram channel consumes".
- Spike sources 017-019 — codice Go provato live per send MarkdownV2, render tabelle PNG, sendDocument: base diretta per `mdv2.go`, `tables.go`, artifact path.
- `golang.org/x/image` è già dep indiretta — promuovere a diretta v0.41.0+ (gofont/gomono + gomonobold embedded, zero CGO).
- `cmd/aura/serve.go` `bootServe` — punto di mount per la channels registry.

### Established Patterns
- Deferred-tool pattern (CLAUDE.md) — `send_file` segue la convenzione `internal/agent/tools/<name>.go`.
- Migration numbering: slot successivo = **0012** (0011 = tool_invocations).
- Build tag tiers: `db_integration` per onboarding round-trip; multimodal tier arriverà con 9c post-spike.
- Goleak mandatory: bot polling goroutine (9b), setup HTTP + SSE pump (9a) — amendment #15.

### Integration Points
- `internal/channels/` — directory esistente ma VUOTA: il framework atterra lì.
- Fanout Subscribe-before-Run: il canale costruisce translator+fanout per ogni turno che avvia.
- `aura.identities` (Slice 1.7) — FK di `telegram_accounts`; identity `local` singola.
- Telegram channel consuma in-process (amendment #35), NON via HTTP server 8b.

</code_context>

<specifics>
## Specific Ideas

- Tabelle: verdetto operatore on-device head-to-head (spike 018) — PNG vince sia sul caso comune 4-col sia sullo stress 6-col; pre-block leggibile solo ≤56 char di riga.
- "look spike" — l'operatore vuole che planning e implementazione partano dai findings spike, non dal PRD nudo: i requirement session-5 del MANIFEST sono binding.
- Lo spike multimodale deve guardare il mercato 2026 ("valutiamo altri modelli multimodali del 2026"), non solo Gemma 4 — il PRD su questo è invecchiato.
- Hardware reale: questo PC = 32 GB RAM + GPU 4 GB VRAM. Lo spike misura qui.

</specifics>

<deferred>
## Deferred Ideas

- **Setup wizard frontend** (page.html, dark theme, flusso browser completo) — prossima milestone.
- **Campo OPENROUTER_API_KEY nel wizard** (Phase 17 D-22 keyless-boot door) — arriva con il frontend del wizard; Phase 13 espone solo le API.
- **CLI channel-ification** — annullata, non rimandata: la CLI resta debug-only in cmd/aura salvo ripensamenti futuri.
- **vLLM come serving engine unificato** (Slice 13 / DGX Spark) — lo spike 9c può produrre segnali utili ma la decisione resta fuori da Phase 13.

</deferred>

---

*Phase: 13-channels-telegram-multimodal*
*Context gathered: 2026-06-07*
