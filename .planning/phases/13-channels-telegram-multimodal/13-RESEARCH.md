# Phase 13: Channels + Telegram + Multimodal — Research

**Researched:** 2026-06-08
**Domain:** Go daemon channels framework + Telegram bot (telebot.v4) + AG-UI fanout consumption + setup HTTP/SSE API + multimodal sidecar HTTP clients
**Confidence:** HIGH (integration seams read at file:line in-repo; external deps registry-verified; engine/transport/table decisions all spike-locked)

> **Scope note:** Both pre-planning gates are CLOSED (PRD amendment #58/#59/#60 committed; multimodal spike session-6 complete). This research is **implementation wiring**, NOT technology selection. Every "what library / what model / what engine" question is already answered in the binding sources — do not re-open them. The open work is the Go glue: interfaces, per-turn fanout wiring, escaper, sidecar clients, migration, tests.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions (D-01 … D-15, all gates CLOSED)
- **D-01** — PRD amendment committed (Amendment #58, commit `49ea7ba1`); #59/#60 add the 9c verdict. telebot pin = tag, tables = PNG, artifact = sendDocument, migration slot 0008→**0012**, wizard = API-only, CLI NOT channel-ified, 9c = 3 CPU sidecars.
- **D-02** — Tables→PNG + sendDocument land INSIDE 9b (no dedicated sub-slice). 9b grows ~920→~1150 LOC src.
- **D-03** — Setup wizard = **API backend only** in Phase 13. NO `page.html` (deferred to next milestone). Endpoints: `POST /setup/token` (getMe validate), `POST /setup/onboard-link` (`{deep_link, qr_svg}`), `GET /setup/status`, `GET /setup/events` (SSE, poll DB 2s). All token-gated (`AURA_SETUP_TOKEN` → stdout first boot).
- **D-04** — Bot token via `curl POST /setup/token` OR env `TELEGRAM_BOT_TOKEN`; user onboarding via **terminal ASCII QR** (`qrterminal`) + deep-link `t.me/<bot>?start=<token>`; `qr_svg` JSON stays for the future frontend.
- **D-05** — Tool `send_file {path, caption?}` explicit; the agent decides delivery. NO renderer auto-detect. Deferred-tool pattern.
- **D-06** — Channel-agnostic via a generic artifact event in the AG-UI stream; each channel renders it (Telegram → sendDocument; CLI → path; AG-UI → custom). Substrate never knows Telegram.
- **D-07** — Path policy: **any readable path** (consistent with #50 full-host posture; future gating = capability_grants Slice 1.7, not ceremonies).
- **D-08** — CLI **stays in `cmd/aura`** (chat.go/chat_render.go/chat_repl.go do NOT move). The PRD "chat.go → internal/channels/cli/" refactor is **ANNULLATO**. CLI = debug-only, NOT a daemon channel.
- **D-09** — Channel interface + registry from PRD even with one real channel (UX-02 deliverable, ~170 LOC).
- **D-13/D-14/D-15** — 9c engine FINAL: `aura-ocr-vl` (GLM-OCR Q8/llama.cpp CPU, vision), `aura-stt` (faster-whisper large-v3-turbo int8, OGG/Opus direct), `aura-tts` (Kokoro-82M `if_sara`, opus). vLLM + Gemma OUT. Vision switch `AURA_VISION_CLOUD` (false=GLM-OCR local, true=OpenRouter/minimax-m3). markitdown for documents.

### Claude's Discretion (research options, recommend — see body)
- Exact shape of the AG-UI artifact event (custom event vs tool_call_result extension) — planner decides with the real stream in hand. **Body §5 gives the tradeoff + a recommendation grounded in the actual `Actions.ArtifactDelta` field that already exists.**
- Per-run fanout wiring (Subscribe-before-Run; the channel builds translator+fanout per turn) — implementation detail guided by `internal/agui/fanout.go`. **Body §2 gives the exact wiring.**
- 50 MB Telegram cap on send_file (overflow behavior), caption handling, the tool's `Deferred` flag. **Body §5 recommends.**
- Registry lifecycle / error-aggregation details in serve.go. **Body §1 gives the pattern.**
- TTS trigger shape (explicit `send_voice` tool like send_file, vs auto-detect on voice input) — planner choice (#59 point 3). **Body §7 gives the tradeoff.**

### Deferred Ideas (OUT OF SCOPE)
- Setup wizard **frontend** (page.html, dark theme, browser flow) — next milestone.
- `OPENROUTER_API_KEY` field in wizard (Phase 17 D-22 keyless-boot door) — arrives with the frontend.
- CLI channel-ification — **annulled, not deferred**.
- vLLM as unified serving engine (Slice 13 / DGX Spark) — out of Phase 13.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| **UX-02** | Channels framework `internal/channels/<name>/` + Telegram primary channel (telebot.v4 tag-pinned) with custom MarkdownV2 escaper + `/cancel`,`/cost`,`/search` (10 commands), tables→PNG, send_file artifacts | §1 (interface+registry), §2 (fanout consumption), §3 (escaper), §4 (tables), §5 (send_file), §8 (migration), §9 (tests). Integration seam `internal/agui/fanout.go` read live. |
| **UX-03** | Setup wizard `:9081/setup/*` token-gated API, `AURA_SETUP_TOKEN` on stdout first boot, QR/deep-link onboarding (API-only this phase) | §6 (endpoint contracts, SSE pump, getMe, qrterminal+qr_svg), §8 (migration 0012 telegram_setup_pending). |
| **UX-04** | Multimodal: voice STT-in + TTS-out + image vision + documents (markitdown) via 3 CPU sidecars | §7 (voice.go/tts.go/photo.go/documents.go HTTP clients, AURA_VISION_CLOUD branch, OGG/Opus both ways, env catalog). |
</phase_requirements>

## Summary

Phase 13 is **glue, not invention.** Every hard technology decision is locked by spike + PRD amendment: telebot.v4 `v4.0.0-beta.9` (transport), x/image+gofont PNG (tables), 3 CPU sidecars GLM-OCR/faster-whisper/Kokoro (9c), sendDocument (artifacts), poll-DB-2s SSE (setup). The implementation work is (a) a tiny `Channel` interface + `Registry` mounted in `bootServe`, (b) a Telegram channel that does **Subscribe-before-Run per turn** against the already-shipped `internal/agui/fanout.go` and maps the AG-UI event set to a 2-message status-pane-B render, (c) an entity-aware MarkdownV2 escaper proven by a 10K fuzz, (d) a token-gated setup HTTP+SSE API, and (e) four sidecar HTTP clients. The integration seam (`fanout.go` / `translator.go` / `types.go`) was built in Phase 12 expressly for this and is read here at file:line.

The most important discovery for the planner: the AG-UI **`Actions.ArtifactDelta map[string]any`** field already exists on `internal/agent/event.go:71` as forward-compat, and the translator (`translator.go`) does **not** yet map it. That makes the "artifact event shape" discretion a clean, low-risk addition (one new translator branch + one SDK custom event), not a protocol redesign. The second key finding: the migration is **0012** (not the PRD's stale "0008_telegram" — 0008 is `proxied_child_id_text`; the floor is 0011 `tool_invocations`).

**Primary recommendation:** Build the `Channel` interface to mirror the established `serveEnv`/`bootServe` lifecycle pattern; drive each Telegram turn with `NewFanout(Translate(...))` → `Subscribe()` (status + content consumers) → `Run(ctx)` → range each subscriber channel into the status-pane-B renderer; reuse the runner's `Turn` iterator as the source; add migration 0012 with `telegram_accounts` + `telegram_setup_pending`; wire 4 sidecar clients as plain OpenAI-compat HTTP POSTs; gate the whole thing behind `AURA_CHANNEL_TELEGRAM_ENABLED`.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Channel lifecycle (Start/Stop) | Daemon (`cmd/aura/serve.go` bootServe) | `internal/channels` registry | Mirrors the existing scheduler+AG-UI mount in `serveEnv`; channels are long-lived daemon subsystems. |
| Per-turn agent drive | `internal/runner` (`Turn` iterator) | Telegram channel | The Runner is the sole loop-driver (D-A1-06); the channel is a consumer, never re-implements turn logic. |
| Event → message render | Telegram channel (`renderer.go`/`status_pane.go`) | `internal/agui` (translator produces events) | The channel owns Telegram-specific rendering; `agui` owns the protocol mapping. Boundary: channel imports agui, not vice-versa. |
| HITL pause/resume | `internal/askuser` (persistence) + Runner (orchestration) | Telegram `hitl.go` (UX surface) | askuser.Store is the sole pause backend; the channel only renders inline keyboard / ForceReply and calls SubmitAnswer-equivalent. |
| Setup token + onboarding | `internal/setup` (HTTP API) | `internal/db` (telegram_setup_pending) | Setup is an isolated loopback HTTP server, separate port `:9081`, separate from the AG-UI gateway `:9080`. |
| Multimodal transcode/OCR/TTS | 3 CPU sidecars (compose services) | Telegram `voice.go`/`tts.go`/`photo.go`/`documents.go` (HTTP clients) | The sidecars own the ML; Aura is a thin OpenAI-compat HTTP client. Zero Go ML code. |
| Vision routing | `photo.go` (`AURA_VISION_CLOUD` branch) | `internal/llm` (`SupportsVision` flag) | One runtime env branch: local sidecar vs OpenRouter. |

## Standard Stack

### Core (all registry-verified 2026-06-08; greenfield — none currently in go.mod)
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `gopkg.in/telebot.v4` | `v4.0.0-beta.9` | Telegram bot transport (polling, Send, inline keyboards, getFile, sendDocument/Photo/Voice) | **[VERIFIED: Go proxy]** latest tag is exactly beta.9 (2026-06-02); spike 017 validated live send + Pitfall #18. CI gate = literal `gopkg.in/telebot.v4 v4.0.0-beta.9` grep in go.mod. **[ASSUMED]** package legitimacy — name from spike/PRD, not Context7. |
| `golang.org/x/image` | `v0.41.0` | Table→PNG: opentype face + `font/gofont/gomono` + `gomonobold`, draw grid, `png.Encode` | **[VERIFIED: Go proxy]** v0.41.0 = 2026-05-21, matches spike 018b requirement (≥v0.41.0). Already an indirect dep — promote to direct. Zero CGO, embedded fonts. |
| `github.com/mdp/qrterminal/v3` | `v3.2.1` | Terminal ASCII QR for the onboarding deep-link (D-04) | **[VERIFIED: Go proxy]** v3.2.1 = 2025-03-19. PRD-named dep ("dep già prevista"). **[ASSUMED]** legitimacy. |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/skip2/go-qrcode` | `v0.0.0-20200617195104` (pseudo-version, **no semver tag**) | `qr_svg` field generation for the future frontend | PRD-named (qr.go). **[WARNING: 2020 pseudo-version, unmaintained]** — `qr_svg` is dead weight this phase (frontend deferred). **Planner decision:** either keep skip2 minimal (PRD parity) or defer `qr_svg` entirely and return an empty/omitted field until the frontend lands. `boombuler/barcode v1.1.0` (2025) is a maintained alternative if SVG is actually needed now. |
| Go stdlib `net/http` | (1.26.4) | Setup server, SSE pump, sidecar HTTP clients | Reuse the `serve.go` pattern (`http.Server` + `ReadHeaderTimeout`). No framework. |
| Go stdlib `mime/multipart` | (1.26.4) | STT `/v1/audio/transcriptions` upload (file field) | voice.go posts the OGG/Opus bytes as multipart; greenfield in repo (no existing multipart usage — verified). |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| x/image PNG tables | fogleman/gg, headless browser | **REJECTED by spike** (telegram-channel.md "What to Avoid"): new dep surface / CGO / chromium. x/image+gofont is zero-new-CGO. |
| Custom MarkdownV2 escaper | `eekstunt/telegramify-markdown-go` | **REJECTED amendment #4**: 4-star supply-chain risk; in-tree ~80 LOC escaper is the locked decision. |
| telebot.v4 SHA-pin | telebot SHA | **REJECTED amendment #58(1)**: repo is tagged now; tag pin is the pin. |

**Installation:**
```bash
go get gopkg.in/telebot.v4@v4.0.0-beta.9
go get golang.org/x/image@v0.41.0      # promote indirect → direct
go get github.com/mdp/qrterminal/v3@v3.2.1
# skip2/go-qrcode ONLY if qr_svg is implemented this phase (see Supporting note)
```

## Package Legitimacy Audit

> slopcheck was **not available** in this research session (no pip/network install attempted under sandbox). Per protocol, externally-sourced package names are tagged `[ASSUMED]` and the planner MUST gate each `go get` behind a `checkpoint:human-verify` task (or confirm against the official repo) before install. Registry existence (verified below via Go proxy) does NOT confer VERIFIED legitimacy.

| Package | Registry | Version | Source Repo | Registry check | Disposition |
|---------|----------|---------|-------------|----------------|-------------|
| `gopkg.in/telebot.v4` | Go proxy | v4.0.0-beta.9 (2026-06-02) | github.com/tucnak/telebot | ✓ tag exists, latest | Approved — spike-017 live-validated; planner verify repo identity before `go get` |
| `golang.org/x/image` | Go proxy | v0.41.0 (2026-05-21) | go.googlesource.com/image | ✓ official golang.org/x | Approved — first-party Go sub-repo, low risk |
| `github.com/mdp/qrterminal/v3` | Go proxy | v3.2.1 (2025-03-19) | github.com/mdp/qrterminal | ✓ tag exists | Approved — planner verify before `go get` |
| `github.com/skip2/go-qrcode` | Go proxy | v0.0.0-2020… (no tag) | github.com/skip2/go-qrcode | ✓ pseudo-version | **Flagged** — unmaintained 2020; only if qr_svg built this phase; prefer deferring or `boombuler/barcode v1.1.0` |

**Packages removed due to slopcheck [SLOP]:** none (slopcheck unavailable; nothing hallucinated — all four are PRD/spike-named and registry-confirmed).
**Packages flagged:** `skip2/go-qrcode` (stale, optional). Planner: insert `checkpoint:human-verify` before each `go get` per the graceful-degradation rule.

## Architecture Patterns

### System Architecture Diagram

```
                    Telegram user (phone)
                          │  text / voice OGG-Opus / photo / document
                          ▼
            ┌──────────────────────────────────────┐
            │  telebot.v4 Bot (polling goroutine)    │  ← bot.go, Channel.Start
            │  bot-intercept commands (/cancel ...)  │  ← commands.go (NO LLM)
            └───────┬───────────────────┬────────────┘
                    │ text turn         │ multimodal pre-process
                    │                   ▼
                    │        voice.go → aura-stt /v1/audio/transcriptions  (OGG/Opus direct)
                    │        photo.go → aura-ocr-vl /v1/chat/completions  (AURA_VISION_CLOUD=false)
                    │                 → OpenRouter image_url               (AURA_VISION_CLOUD=true)
                    │        documents.go → markitdown /convert (tiered ≤5MB sync / 5-50MB async)
                    │                   │ (all produce a TEXT user message)
                    ▼                   ▼
            ┌──────────────────────────────────────┐
            │  runner.Turn(ctx, convID, &userMsg)    │  iter.Seq2[*agent.Event, error]
            └───────────────────┬────────────────────┘
                                │ per turn:
                                ▼
            ┌──────────────────────────────────────┐
            │  agui.Translate(thread,run,idgen,seq)  │  → iter.Seq2[events.Event, error]
            │  agui.NewFanout(translated)            │
            │   .Subscribe()  ×2  (status + content) │  ← Subscribe BEFORE Run (panic otherwise)
            │   .Run(ctx)                            │  single producer, drop-on-full cap-64
            └───────┬───────────────────┬────────────┘
              status events        content events
                    ▼                   ▼
            status_pane.go         renderer.go
            (msg #1 edited:        (msg #2: streamed text,
             tool list 🟡→✅/❌,    MarkdownV2 escaped via mdv2.go;
             reasoning, cost)       tables → tables.go PNG sendPhoto;
                    │               artifact event → artifact.go sendDocument)
                    │ throttle 1500ms       │ throttle 500ms ; chat queue 1000ms/chat_id
                    ▼                       ▼
            ┌──────────────────────────────────────┐
            │  Telegram Bot API (sendMessage/edit,   │
            │  sendPhoto, sendDocument, sendVoice)   │
            └────────────────────────────────────────┘

  HITL pause path:  Actions.AwaitingInput Event → RUN_FINISHED(interrupt)
     → hitl.go renders InlineKeyboard (options) or ForceReply (clarification)
     → callback → askuser SubmitAnswer → runner.Turn(ctx, convID, nil) resumes

  Setup (separate :9081 loopback HTTP, isolated from :9080 AG-UI):
     POST /setup/token (getMe) → POST /setup/onboard-link → {deep_link, qr_svg}
     GET /setup/events (SSE, poll telegram_setup_pending every 2s) → onboarding_completed
```

### Recommended Project Structure
```
internal/channels/
├── channel.go            # Channel interface (~70 LOC) — Name/Start/Stop/IsHealthy
├── registry.go           # StartAll/StopAll, env AURA_CHANNEL_<NAME>_ENABLED, error aggregation (~100 LOC)
└── telegram/
    ├── bot.go            # telebot.Bot wrapper, polling, Start/Stop goroutine; implements Channel
    ├── agui_subscriber.go# per-turn NewFanout/Subscribe/Run wiring (the seam consumer)
    ├── renderer.go       # AG-UI events → Telegram messages, status-pane-B + throttle
    ├── status_pane.go    # status pane manager (msg #1: tool list + reasoning + cost)
    ├── mdv2.go           # entity-aware MarkdownV2 escaper (~80 LOC) + 10K fuzz
    ├── tables.go         # markdown table → gridded PNG (x/image) → sendPhoto (~150 LOC)
    ├── artifact.go       # artifact event consumer → sendDocument (~60 LOC)
    ├── hitl.go           # ask_user pending → InlineKeyboard / ForceReply (~150 LOC)
    ├── commands.go       # 10 bot-intercept commands (~210 LOC)
    ├── onboarding.go     # /start <token> → consume pending → INSERT telegram_accounts (~80 LOC)
    ├── config.go         # BotToken (env) + bind addr (~60 LOC)
    ├── store.go          # sqlc adapter for telegram_accounts + telegram_setup_pending (~80 LOC)
    ├── voice.go          # POST aura-stt /v1/audio/transcriptions (faster-whisper, OGG direct) (~130 LOC)
    ├── tts.go            # POST aura-tts /v1/audio/speech (Kokoro if_sara) → sendVoice (~90 LOC)
    ├── photo.go          # vision routing on AURA_VISION_CLOUD (~95 LOC)
    └── documents.go      # tiered markitdown /convert (~160 LOC)
internal/setup/
    ├── server.go         # isolated :9081 HTTP server (~150 LOC)
    ├── handlers.go       # /setup/token, /setup/onboard-link, /setup/status, /setup/events SSE (~200 LOC)
    ├── qr.go             # qr_svg generation (optional this phase) (~50 LOC)
    └── types.go          # request/response shapes (~40 LOC)
internal/agent/tools/send_file.go   # deferred-tool: emit artifact event (~80 LOC)
internal/db/migrations/0012_telegram.{up,down}.sql
internal/db/queries/telegram_accounts.sql + telegram_setup_pending.sql
```

### Pattern 1: Channel interface (mirror the daemon-subsystem shape)
**What:** A minimal interface a daemon channel implements; the registry aggregates lifecycle.
**When to use:** All channels (Telegram now; WhatsApp/Discord future).
```go
// internal/channels/channel.go — shape per PRD §Architettura componenti
type Channel interface {
    Name() string                          // "telegram" — keys AURA_CHANNEL_<NAME>_ENABLED
    Start(ctx context.Context) error       // launch polling goroutine; returns once started, not on each turn
    Stop(ctx context.Context) error        // graceful drain (telebot Bot.Stop + goleak-clean)
    IsHealthy() bool                       // health probe for /setup/status
}
```
**Critical wiring detail:** the PRD's older sketch shows `Start(ctx, sub)` taking a fanout subscriber. **That is wrong for this codebase** — fanout is built *per turn* (Subscribe-before-Run, one Fanout per `runner.Turn`), not once at channel start. The channel holds the `*runner.Runner` and builds a fresh `Fanout` inside each turn handler. Keep `Start(ctx)` clean; the per-turn wiring is internal to the channel (see §2).

### Pattern 2: Registry mount in bootServe (mirror serveEnv)
**What:** `serveEnv` already composes the scheduler + AG-UI HTTP server (`cmd/aura/serve.go:50-145`). The channels registry mounts the same way.
```go
// In bootServe (serve.go), after the AG-UI server wiring (~line 144):
reg := channels.NewRegistry()
if tg, err := telegram.New(telegram.Deps{
    Runner: chat.run, Conv: chat.conv, Pause: chat.pause /* askuser.Store */,
    Token: cfg.TelegramBotToken, Store: telegramStore,
    StatusThrottle: cfg.TelegramStatusThrottleMS, /* ... */
}); err == nil {
    reg.Register(tg)
}
// serveEnv gains a `channels *channels.Registry` field; runServe calls reg.StartAll(ctx)
// alongside scheduler.Start, and reg.StopAll on shutdown (before env.close()).
```
Registry `StartAll` reads `AURA_CHANNEL_<NAME>_ENABLED` (default true) per registered channel, calls `Start`, and **aggregates errors** with `errors.Join` so one failed channel does not abort the daemon (fail-soft, mirrors the AG-UI server's "log but never exit" posture at `serve.go:84-88`). `--no-telegram` / `--only=cli` flags override the env (PRD Punto 1, acceptance 9a).

### Anti-Patterns to Avoid
- **Subscribe-after-Run:** `Fanout.Subscribe` **panics** if called after `Run` (`fanout.go:51`). Both consumers (status + content) MUST be subscribed before `Run(ctx)`.
- **Double Run:** `Fanout.Run` panics on second call (`fanout.go:67`). One Fanout = one turn = one Run.
- **Whole-string MarkdownV2 escape:** neutralizes the bot's own formatting (telegram-channel.md). Escape outside entities only.
- **Reading getUpdates for send verification:** bot-sent messages never appear there. Assert on the `Send` response (`msg.Text`/`msg.Photo`/`msg.Document`/`msg.Voice`).
- **CLI channel-ification:** D-08 annuls it. Do NOT move `cmd/aura/chat*.go`.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| AG-UI event fan-out to status+content consumers | A second translator / bespoke channel split | `agui.NewFanout` + `Subscribe()`×2 + `Run()` (`internal/agui/fanout.go`) | Already shipped Phase 12, drop-on-full cap-64, goleak-clean, lifecycle-frame guaranteed delivery. |
| Token streaming / reasoning / tool lifecycle event coalescing | Re-parsing agent.Event | `agui.Translate` (`translator.go`) | Pure function; coalesces deltas into TEXT_MESSAGE_*/REASONING_*/TOOL_CALL_* with correct interleave + interrupt outcomes. |
| Markdown table rendering | ASCII alignment from scratch | x/image PNG (spike 018b, ~150 LOC proven in `sources/018b`) | Bot API has no table entity; PNG won the on-device head-to-head; pre-block fallback ≤56 char/row. |
| File MIME detection for sendDocument | mime sniffing | `tele.Document{File: FromDisk(path), FileName: name}` | Telegram detects MIME itself (spike 019, 4/4 exact). path+filename suffice. |
| OGG/Opus transcode | ffmpeg pre-step | faster-whisper (decodes Opus inline via PyAV) IN; Kokoro `response_format=opus` OUT | spike 027/028: zero transcode both ways. whisper.cpp CAN'T decode Opus — that's why faster-whisper won. |
| Cost / search backends | New queries | `llm.CostUSD`/`CostUSDValue` (`prices.go:32-57`) + `conversations.SearchConversationTurns` (`store.go:418`) | `/cost` and `/search` reuse the EXISTING locked surfaces byte-for-byte (PRD cross-slice invariant test). |
| Pause/resume persistence | New state machine | `askuser.Store` + `runner.SubmitAnswer`/`Turn(convID,nil)` | The Runner is the sole paused_states writer; HITL is a render-only surface over it. |

**Key insight:** Phase 12 built `fanout.go` *expressly* for this phase ("the in-process seam the Phase-13 Telegram channel consumes", `fanout.go:28`). The single biggest mistake would be re-implementing event distribution instead of using it.

## Code Examples

### Per-turn fanout consumption (the §2 discretion, made concrete)
```go
// Source: internal/agui/fanout.go:41-67 + translator.go:23 + runner/runner.go:177 (read live)
func (t *Telegram) handleTurn(ctx context.Context, convID, userMsg string) {
    idgen := agui.NewIDGenerator()
    runID := uuid.NewString()
    // runner.Turn yields *agent.Event; Translate maps to AG-UI events.Event.
    translated := agui.Translate(convID, runID, idgen, t.runner.Turn(ctx, convID, &userMsg))
    fo := agui.NewFanout(translated)
    statusCh := fo.Subscribe()   // → status_pane.go
    contentCh := fo.Subscribe()  // → renderer.go    (BOTH before Run)
    fo.Run(ctx)                  // single producer goroutine; closes both chans on end/cancel
    go t.statusPane.consume(statusCh)   // tool list, reasoning, cost footer (throttle 1500ms)
    t.renderer.consume(contentCh)       // TEXT_MESSAGE_* → escaped send (throttle 500ms)
}
```
Map per `translator.go` real output: `RUN_STARTED`→open pane; `TOOL_CALL_START/ARGS/END/RESULT`→pane tool list 🟡→✅/❌; `REASONING_*`→pane "💭" line; `TEXT_MESSAGE_*`→content; `STATE_DELTA`→running cost on pane footer; `RUN_FINISHED(success)`→finalize; `RUN_FINISHED(interrupt)`→HITL; `RUN_ERROR`→❌.

### MarkdownV2 entity-aware escaper (the §3 surface)
```go
// Source: telegram-channel.md "MarkdownV2 discipline" (Pitfall #18 strict)
// Reserved OUTSIDE entities: _ * [ ] ( ) ~ ` > # + - = | { } . ! \
// Inside ```pre```/`code` fences: only ` and \ are reserved (pipes/dashes/dots flow through)
// Escape reserved chars OUTSIDE intended bold/code/pre spans; NEVER whole-string.
// Always keep the plain-text fallback: if escaped send 400s, resend WITHOUT ParseMode.
```

### STT client (voice.go, multipart upload)
```go
// Source: PRD §Slice 9c voice.go + spike 027 (faster-whisper /v1/audio/transcriptions)
// 1. telebot getFile + download the .ogg bytes
// 2. multipart POST to STT_BASE_URL/audio/transcriptions, file field = the OGG/Opus bytes
//    (faster-whisper decodes Opus inline — NO ffmpeg pre-step)
// 3. 2 retries exp 1s/2s; hard fail → "❌ Trascrizione non disponibile." + reaction 😵
// 4. transcript text → user message → runner.Turn (process as text)
```

## Runtime State Inventory

> This is a greenfield feature phase (new channel + new sidecars + new migration), NOT a rename/refactor. No existing runtime state is being renamed or migrated. The one *new* persistent state surface is migration 0012 (telegram_accounts, telegram_setup_pending) — a forward addition, documented in §8, not a migration of existing data.
>
> **Verified absent:** no Telegram/channel/setup/multimodal code, env vars, or go.mod deps currently exist (grep-confirmed: `internal/channels/` and `internal/setup/` are empty/absent; no `AURA_CHANNEL_*`/`AURA_SETUP_*`/`MULTIMODAL_*`/`telebot`/`x/image` in config or go.mod). Nothing to inventory.

## Common Pitfalls

### Pitfall 1: Subscribe-after-Run panic
**What goes wrong:** Building the fanout, calling `Run`, then trying to add the content subscriber → `panic: Fanout.Subscribe called after Run`.
**Why:** `fanout.go:51` guards against the data race of registering after the producer snapshotted `f.subs`.
**How to avoid:** Subscribe BOTH status and content consumers, THEN `Run(ctx)`. One Fanout per turn.
**Warning signs:** A panic in tests the moment a second consumer is wired.

### Pitfall 2: MarkdownV2 all-or-nothing 400
**What goes wrong:** One naked `-`/`.`/`(` anywhere → the WHOLE `sendMessage` 400s (`can't parse entities`).
**Why:** Telegram rejects per-message, not per-char (telegram-channel.md, spike 017).
**How to avoid:** Entity-aware escaper + **always** the plain-text fallback (resend without ParseMode on 400). The 10K-fuzz acceptance is the proof.
**Warning signs:** Intermittent 400s on LLM replies containing prose punctuation.

### Pitfall 3: Migration number drift (PRD says 0008, reality is 0012)
**What goes wrong:** Following the PRD's literal "`0008_telegram.up.sql`" collides with the shipped `0008_proxied_child_id_text`.
**Why:** The PRD was written before later slices landed; CONTEXT.md D-01(d) and the migration floor both correct it.
**How to avoid:** Migration is **0012** (`0011_tool_invocations` is the shipped floor). Mirror the 0009/0004 file pattern (role grants, COMMENT, idempotent seeds where applicable).
**Warning signs:** `golang-migrate` dirty-version error on apply.

### Pitfall 4: sendVoice/caption 400 on non-ASCII
**What goes wrong:** A `sendVoice` caption with emoji + parens + em-dash → HTTP 400 (#59 point 3 live caveat).
**Why:** Telegram caption parsing is stricter for voice.
**How to avoid:** ASCII-clean captions on sendVoice (consistent with the mdv2 escaper discipline).
**Warning signs:** TTS reply send fails while the text reply succeeded.

### Pitfall 5: goroutine leaks (bot polling, SSE pump, async document convert)
**What goes wrong:** A leaked polling goroutine / SSE poll loop / async 5-50MB convert goroutine fails `goleak.VerifyNone`.
**Why:** PRD amendment #15 mandates goleak for 9a (HTTP+SSE pump), 9b (bot polling), 9c (sidecar HTTP) — these are on the explicit list (`prd.md:1759`).
**How to avoid:** `Stop(ctx)` joins the polling goroutine; the SSE pump exits on ctx-cancel / client disconnect; the async convert goroutine is tracked by a WaitGroup the channel `Stop` drains. `goleak.VerifyNone(t)` in TestMain for every package that spawns.

### Pitfall 6: AURA_VISION_CLOUD must be a single runtime branch, not two code paths
**What goes wrong:** Building separate photo handlers per mode duplicates logic and breaks the "switch = .env only, zero code" requirement (#60).
**Why:** D-15/#60 explicitly require config-only switchability with DeepSeek+GLM-OCR as the unchanged default.
**How to avoid:** One `photo.go` function, one `if cfg.VisionCloud { ...OpenRouter... } else { ...aura-ocr-vl... }` branch. Default `AURA_VISION_CLOUD=false`.

## Setup API contract (§6, UX-03)

| Endpoint | Method | Body / Query | Returns | Notes |
|----------|--------|--------------|---------|-------|
| `/setup/token` | POST | `{token}` + `?token=<AURA_SETUP_TOKEN>` | `{ok, bot_username}` | Validates the bot token via telebot `getMe`; persists in memory; restarts bot goroutine if active. |
| `/setup/onboard-link` | POST | `?token=` | `{deep_link, qr_svg}` | Mints onboarding UUID, INSERT `telegram_setup_pending` TTL 1h, deep_link `t.me/<bot>?start=<token>`. Terminal ASCII QR also printed via qrterminal (D-04). |
| `/setup/status` | GET | `?token=` | `{bot_configured, account_count, last_activity}` | |
| `/setup/events` | GET (SSE) | `?token=` | `{type:"onboarding_completed", telegram_user_id, username}` | **Poll `telegram_setup_pending.consumed_at` every 2s** (D-03, open-question-3 default). LISTEN/NOTIFY deferred. |

- **Token gate (`requireSetupToken` middleware):** 401 if `?token=` query OR `X-Aura-Setup-Token` header ≠ `AURA_SETUP_TOKEN`. Token unset at boot → generate random UUIDv4, print single parseable line `AURA_SETUP_TOKEN=<value>` to stdout. In-memory only (no disk). Invalidated after onboarding completes; second navigation → 401. **Acceptance: 401 without / 200 with / 401 after onboard-complete.**
- **Bind:** `127.0.0.1:9081` default (`AURA_SETUP_BIND` override for remote QR scan). **Separate port from the AG-UI gateway `:9080`** (`config.go:230`, `AURA_AGUI_BIND`).
- **SSE pump goleak:** the 2s poll loop is a goroutine per connection — must exit on `r.Context().Done()` (client disconnect) AND on server shutdown ctx. Mirror the AG-UI server's bounded-drain shutdown (`serve.go:98-102`).

## Migration 0012 (§8, the corrected number)

```sql
-- internal/db/migrations/0012_telegram.up.sql  (NOT 0008 — floor is 0011)
-- Mirror the 0009/0004 pattern: aura_app DML grants, aura_migrate DDL, COMMENT, FK to aura.identities.
CREATE TABLE aura.telegram_accounts (
  telegram_user_id bigint PRIMARY KEY,
  identity_id      uuid NOT NULL REFERENCES aura.identities(id),  -- Slice 1.7 FK (0004)
  username         text,
  first_name       text,
  added_at         timestamptz NOT NULL DEFAULT now(),
  last_seen_at     timestamptz
);
CREATE TABLE aura.telegram_setup_pending (
  onboarding_token text PRIMARY KEY,
  identity_id      uuid NOT NULL REFERENCES aura.identities(id),
  generated_by     text,
  created_at       timestamptz NOT NULL DEFAULT now(),
  expires_at       timestamptz NOT NULL,         -- created_at + 1 hour
  consumed_at      timestamptz NULL
);
CREATE INDEX telegram_setup_pending_active
  ON aura.telegram_setup_pending (expires_at) WHERE consumed_at IS NULL;
-- GRANT SELECT,INSERT,UPDATE,DELETE ... TO aura_app;  GRANT ALL ... TO aura_migrate;  (per 0009 precedent)
```
sqlc query files: `telegram_accounts.sql` (~6 queries: insert/get-by-tg-id/get-by-identity/touch-last-seen/count/list), `telegram_setup_pending.sql` (~3: insert/consume-and-return/delete-expired). Follow the `askuser`/`identity` Store pattern: `Store{pool,q}`, SQLSTATE classification via `errors.As`+`pgErr.Code`, pgtype at the boundary, `db.WithTx` for the consume-and-INSERT-account atomic step.

## Multimodal sidecar wiring (§7, UX-04)

| Sidecar | Endpoint | Client file | Env (upstream naming) | Audio/format contract |
|---------|----------|-------------|------------------------|------------------------|
| `aura-ocr-vl` (GLM-OCR Q8 / llama.cpp CPU) | `POST /v1/chat/completions` with `image_url` (base64) | photo.go | `MULTIMODAL_BASE_URL=http://aura-ocr-vl:8082/v1`, `MULTIMODAL_MODEL=glm-ocr`, `MULTIMODAL_API_KEY=no-key` | image in → text/HTML-table out |
| `aura-stt` (faster-whisper large-v3-turbo int8) | `POST /v1/audio/transcriptions` (multipart) | voice.go | `STT_BASE_URL=http://aura-stt:9000/v1`, `STT_MODEL=large-v3-turbo` | **OGG/Opus direct in** (no ffmpeg) |
| `aura-tts` (Kokoro-82M `if_sara`) | `POST /v1/audio/speech` `response_format=opus` | tts.go | `TTS_BASE_URL=http://aura-tts:8880/v1`, `TTS_VOICE=if_sara`, `TTS_FORMAT=opus` | text in → **opus voice note out** → sendVoice |
| markitdown | `POST /convert` | documents.go | (markitdown sidecar URL) | doc → markdown; tiered ≤5MB sync / 5-50MB async / >50MB refuse |

- **Vision branch (`AURA_VISION_CLOUD`, default false):** `false` → POST aura-ocr-vl; `true` → attach `image_url` to the primary LLM turn if `Model.SupportsVision` (e.g. minimax-m3), else `MULTIMODAL_FALLBACK_MODEL` (default `minimax/minimax-m3`) — both OpenRouter, no sidecar. Add `SupportsVision`/`SupportsAudio` lookups to `internal/llm/models.go` (NEW — see 13-PATTERNS.md §"No Analog Found"; `Model` is a bare string at `internal/llm/config.go:77`, there is NO `Model` struct and NO `openai_compat/` package). Seed: `deepseek/deepseek-v4-flash`=false, `minimax/minimax-m3`=true.
- **Env var convention:** third-party sidecars keep upstream naming (`MULTIMODAL_*`, `STT_*`, `TTS_*`) per CLAUDE.md exception; Aura-native knobs use `AURA_<DOMAIN>_<UNIT>` (`AURA_VISION_CLOUD`, `AURA_CHANNEL_TELEGRAM_ENABLED`, `AURA_SETUP_TOKEN`, `AURA_SETUP_BIND`, `AURA_TELEGRAM_STATUS_THROTTLE_MS`/`_CONTENT_THROTTLE_MS`/`_CHAT_RATE_LIMIT_MS`). `TELEGRAM_BOT_TOKEN` keeps upstream naming (third-party).
- **Compose:** the 3 sidecars + markitdown are compose services (config, not Go LOC). With `AURA_VISION_CLOUD=true` the operator simply doesn't start `aura-ocr-vl` (tier B no-GPU). STT/TTS stay local in both tiers.

## State of the Art

| Old Approach (PRD pre-amendment) | Current Approach (locked) | When Changed | Impact |
|--------------|------------------|--------------|--------|
| telebot SHA-pin (amendment #5) | Tag `v4.0.0-beta.9` | #58 (2026-06-07) | CI gate = version grep, not SHA. |
| Telegram tables as text/HTML | x/image PNG primary, pre-block fallback | #58(2) | New `tables.go`, x/image direct dep. |
| Text-only replies | + send_file artifacts (sendDocument) | #58(3) | New `send_file` tool + artifact event. |
| Setup wizard with page.html | API backend only | #58(5) | No HTML; `qr_svg` returned for future frontend. |
| 9c = Gemma-4 unified sidecar (vLLM/GPU) | 3 CPU sidecars (GLM-OCR + faster-whisper + Kokoro) | #59 (spike 020 INVALIDATED vLLM on 4GB) | New tts.go (TTS voice-out leg); photo.go→OCR-VL; voice.go→faster-whisper OGG-direct. |
| Vision fallback = markitdown OCR | minimax-m3 cloud via `AURA_VISION_CLOUD` | #59/#60 | One env branch in photo.go; markitdown stays for documents only. |
| Migration 0008_telegram | Migration **0012** | floor moved (0011 tool_invocations) | Avoids collision with 0008_proxied_child_id_text. |

**Deprecated/outdated:**
- vLLM for 9c on this hardware — DEAD (spike 020). Do not plan any vLLM path.
- whisper.cpp for STT — loses to faster-whisper (can't decode Opus without ffmpeg). Do not plan whisper.cpp.
- CLI-as-channel refactor — annulled (D-08).

## Validation Architecture

> Nyquist validation is ENABLED for this phase (config absent ⇒ enabled). The orchestrator extracts this section to seed VALIDATION.md.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `goleak` (mandatory, amendment #15) + `testing/fstest`/`net/http/httptest` for HTTP; property/fuzz via `testing.F` |
| Config file | none (go test); build tags select tiers |
| Quick run command | `go test ./internal/channels/... ./internal/setup/... ./internal/agent/tools/` |
| Full suite command | `go test -race -tags 'db_integration multimodal_integration telegram_integration' ./...` then `make coverage` (owned-surface floor ≥85%, CLAUDE.md) |

### Test tiers (build tags)
- **unit** (no tag, always in CI): escaper fuzz, table PNG golden, renderer golden fixtures, command dispatch (no LLM), registry lifecycle, photo.go env-branch with mock sidecar, setup token-gate, fanout consumption with synthetic event stream.
- **`db_integration`**: onboarding token round-trip (INSERT telegram_accounts, consume pending, cleanup expired), Store CRUD against real PG.
- **`telegram_integration`** (NEW, live bot, operator chat only): sendPhoto/sendDocument/sendVoice round-trip asserting on the Bot-API response (`msg.Photo`/`msg.Document.{FileName,MIME,FileSize}`/`msg.Voice` non-nil) — **ground truth = response, never getUpdates** (spike 017/019). `t.Fatal` under `$CI` when token env unset (no-skip-as-green).
- **`multimodal_integration`** (NEW, requires the 3 sidecars up): voice note IT round-trip (STT), photo IT OCR recall, sendVoice (TTS). Skipped without containers; `t.Fatal` under `$CI` if env set but sidecars unreachable.

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| UX-02 | Channel interface + registry StartAll/StopAll + AURA_CHANNEL_*_ENABLED + error aggregation | unit + goleak | `go test ./internal/channels/` | ❌ Wave 0 |
| UX-02 | Per-turn fanout consumption maps AG-UI events → status-pane-B render | unit (synthetic event stream, golden fixture per event type incl. microcompact pointer) | `go test ./internal/channels/telegram/` | ❌ Wave 0 |
| UX-02 | MarkdownV2 escaper: 10K random-Unicode fuzz, no `400 can't parse entities`; pre-fence pipes/dashes unescaped | fuzz (`testing.F`) | `go test -run Fuzz -fuzz FuzzMdv2 -fuzztime 30s ./internal/channels/telegram/` + 10K seeded table | ❌ Wave 0 |
| UX-02 | tables.go: 4-col/6-col → deterministic PNG (dims+grid); pre-block fallback | golden + live | `go test ./internal/channels/telegram/` ; `-tags telegram_integration` for sendPhoto | ❌ Wave 0 |
| UX-02 | send_file → artifact event → sendDocument (xlsx/pdf, MIME exact) | unit (event emit) + `telegram_integration` (round-trip) | `go test -tags telegram_integration ./...` | ❌ Wave 0 |
| UX-02 | 10 commands bot-intercept, no LLM call; /cost==CLI, /search==CLI (cross-slice invariant) | unit | `go test ./internal/channels/telegram/` | ❌ Wave 0 |
| UX-02 | HITL: options→InlineKeyboard, none→ForceReply, callback→resume; throttle via synctest | unit (`testing/synctest`) | `go test ./internal/channels/telegram/` | ❌ Wave 0 |
| UX-02 | bot polling goroutine goleak-clean | unit goleak | `go test ./internal/channels/telegram/` (TestMain goleak) | ❌ Wave 0 |
| UX-03 | Setup token gate: 401 without / 200 with / 401 after onboard-complete | unit (httptest) | `go test ./internal/setup/` | ❌ Wave 0 |
| UX-03 | /setup/token getMe validate, /setup/onboard-link {deep_link,qr_svg}, /setup/status | unit (httptest + telebot Offline/mock) | `go test ./internal/setup/` | ❌ Wave 0 |
| UX-03 | SSE /setup/events poll-2s emits onboarding_completed; pump goleak-clean | unit (httptest SSE) + goleak | `go test ./internal/setup/` | ❌ Wave 0 |
| UX-03 | onboarding round-trip INSERT telegram_accounts + cleanup expired | `db_integration` | `go test -tags db_integration ./internal/channels/telegram/ ./internal/setup/` | ❌ Wave 0 |
| UX-04 | voice.go STT OGG/Opus direct, 2-retry, hard-fail UX | unit (mock sidecar) + `multimodal_integration` | `go test -tags multimodal_integration ./internal/channels/telegram/` | ❌ Wave 0 |
| UX-04 | tts.go Kokoro opus→sendVoice, ASCII caption | unit (mock) + `multimodal_integration` (msg.Voice non-nil) | `-tags multimodal_integration` | ❌ Wave 0 |
| UX-04 | photo.go AURA_VISION_CLOUD: false→sidecar mock called, true→cloud called (no sidecar) | unit (both branches, mock) | `go test ./internal/channels/telegram/` | ❌ Wave 0 |
| UX-04 | documents.go tiered ≤5MB sync / 5-50MB async / >50MB refuse; async goleak-clean | unit (mock markitdown) + goleak | `go test ./internal/channels/telegram/` | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go vet ./... && go build ./... && go test -race ./internal/<touched-package>/` (CLAUDE.md post-edit gate).
- **Per wave merge:** `go test ./internal/channels/... ./internal/setup/... ./internal/agent/tools/` + `golangci-lint run`.
- **Phase gate:** full tag matrix (`db_integration multimodal_integration telegram_integration`) live + `make coverage` ≥85% owned-surface + mutation spot-check ≥70% on the escaper + renderer critical files, before `/gsd-verify-work`.

### Wave 0 Gaps
- [ ] `internal/channels/main_test.go` + `internal/channels/telegram/main_test.go` + `internal/setup/main_test.go` — TestMain with `goleak.VerifyNone` (amendment #15).
- [ ] Golden fixtures dir: one AG-UI-event→Telegram-message fixture per event type (incl. microcompact pointer case, PRD acceptance).
- [ ] Escaper 10K-Unicode seed corpus + `FuzzMdv2`.
- [ ] Table PNG golden bytes (4-col, 6-col) — deterministic dims+grid.
- [ ] Mock sidecar HTTP servers (httptest) for STT/TTS/OCR-VL/markitdown unit tiers.
- [ ] CI job additions: `telegram_integration` (operator-token-gated, optional) + `multimodal_integration` (sidecar-gated). Both `t.Fatal`-under-`$CI`-when-env-set per no-skip-as-green.
- [ ] No new framework install (Go stdlib + goleak already in tree).

## Security Domain

> `security_enforcement` is not explicitly false in config — included.

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | yes | Setup token gate (`AURA_SETUP_TOKEN`, one-time, in-memory, 401 middleware). Telegram onboarding via short-TTL (1h) single-use `telegram_setup_pending` token. |
| V3 Session Management | partial | No HTTP sessions (loopback API); the onboarding token is the single-use credential. |
| V4 Access Control | yes | `telegram_accounts.identity_id` FK to `aura.identities` (single-user `local`); future per-identity gating = capability_grants (Slice 1.7), explicitly NOT this phase (D-07). |
| V5 Input Validation | yes | MarkdownV2 escaper (entity-aware) + plain-text fallback prevents 400-injection; sidecar responses bounded by `AURA_CONVERSATION_TURN_CAP_BYTES` → sidecar spill. send_file path is any-readable by D-07 (host posture #50). |
| V6 Cryptography | no | No new crypto; bot token + setup token are secrets in env/memory, never logged. |

### Known Threat Patterns
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Setup endpoint exposed on 0.0.0.0 without token | Spoofing/Elevation | `AURA_SETUP_BIND` defaults loopback; token gate is mandatory on all `/setup/*` (amendment #10). |
| Onboarding token replay | Spoofing | Single-use (`consumed_at`), 1h TTL, indexed partial cleanup. |
| Bot token leakage in logs | Info Disclosure | Never log the token; getMe response only exposes bot username. |
| MarkdownV2 injection / send-failure | Tampering/DoS | Entity-aware escaper + plain-text fallback (10K fuzz acceptance). |
| Unbounded sidecar response | DoS | Turn-cap → sidecar spill; document tier refuse >50MB; HTTP timeouts (STT 30s, doc 30s sync). |

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | build/test | ✓ | 1.26.4 | — |
| `aura-stt` sidecar | voice.go (UX-04) | ✗ (compose, operator starts) | faster-whisper large-v3-turbo | unit tier uses httptest mock; `multimodal_integration` skipped without it |
| `aura-tts` sidecar | tts.go (UX-04) | ✗ (compose) | Kokoro-82M if_sara | mock / skip |
| `aura-ocr-vl` sidecar | photo.go local mode (UX-04) | ✗ (compose) | GLM-OCR Q8 | `AURA_VISION_CLOUD=true` → OpenRouter (no sidecar) |
| markitdown sidecar | documents.go (UX-04) | ✗ (compose) | — | mock / skip |
| Postgres | migration 0012, db_integration | ✓ (compose, dev stack) | 17 | — |
| Real Telegram bot token | telegram_integration tier | ✓ (`.env` `TELEGRAM_BOT_TOKEN`, operator) | — | unit tier uses `tele.Settings{Offline:true}` |

**Missing with no fallback (blocking only the integration tiers, not unit/build):** the 4 sidecars + a live bot. **Plan must:** keep all unit tests sidecar-free via httptest mocks; gate live tiers behind build tags + env; never let a missing sidecar fail the unit gate or CI default job. `AURA_VISION_CLOUD=true` is the documented no-GPU fallback for the vision half.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | telebot.v4 / qrterminal / skip2 package *identities* (not just registry existence) are legitimate | Standard Stack, Audit | slopcheck unavailable; planner must verify repos before `go get`. Low risk (PRD/spike-named, telebot live-validated spike 017). |
| A2 | markitdown sidecar exposes `POST /convert` (not OpenAI-compat) | §7 documents.go | If the endpoint differs, documents.go client shape changes — verify the markitdown image's API at implementation time. The OCR/STT/TTS endpoints ARE spike-verified OpenAI-compat. |
| A3 | No `Model` capability struct exists; `internal/llm/models.go` is net-new (the research's earlier `openai_compat/models.go` path was stale) | §7 photo.go | Confirmed: `Model` is a bare string at `internal/llm/config.go:77`, no `openai_compat/` package. Planner creates `internal/llm/models.go` with `SupportsVision`/`SupportsAudio` lookups (~30 LOC). |
| A4 | The Runner exposes `SubmitAnswer`/`PendingFor`/`Turn(convID,nil)` for HITL resume (seen in chat_repl.go usage) | §1, hitl.go | If signatures differ, hitl.go wiring adjusts; the REPL proves the surface exists. |
| A5 | `qr_svg` may be safely deferred/stubbed this phase (frontend deferred) | Supporting | If the operator wants real SVG now, add boombuler/barcode; low risk (D-03 defers the frontend). |
| A6 | No outbound `ToolResult`→`Actions.ArtifactDelta` path exists yet; the seam must be ADDED in `internal/agent/llm_agent_events.go` | §5, OQ1 | Confirmed by live read: `toolResultEvent` (`llm_agent_events.go:69-105`) already lifts `ToolResult.Meta` (`tools/spec.go:58`) onto the Event but never stamps `Actions.ArtifactDelta`. send_file writes the descriptor into `ToolResult.Meta`; one new line in `toolResultEvent` lifts it to `Actions.ArtifactDelta` (analog: shell_exec exit_code via Meta, `tools/shell_exec.go:156`). Low risk, additive. |

## Open Questions (RESOLVED)

1. **Artifact event shape (D-06 discretion — RECOMMENDATION provided).**
   **RESOLVED:** custom AG-UI event off `Actions.ArtifactDelta` (Plan 13-02). The substrate emits a dedicated AG-UI custom event (not an overloaded `TOOL_CALL_RESULT`), keeping the artifact path channel-agnostic.
   - What we know: `Actions.ArtifactDelta map[string]any` already exists on `internal/agent/event.go:71` (forward-compat, currently unmapped by `translator.go`). The translator already has a clean STATE_DELTA branch pattern (`translator.go:87-95`) to copy.
   - **Emit seam (named, read live):** there is NO existing outbound path from a `ToolResult` to `Actions.ArtifactDelta`. The concrete plumbing is: `runTool` (`llm_agent.go:357`) returns the tool's `ToolResult` as `run.Result`; `toolResultEvent` (`llm_agent_events.go:69-105`) projects that onto the Event and ALREADY reads `run.Result.Meta` (`tools/spec.go:58`, `map[string]any`) via `toolResultMetaMap`/`exitCodeFromMeta` (lines 101-102). send_file writes its `{path,filename,caption}` artifact descriptor into `ToolResult.Meta` (exactly as `shell_exec` writes `exit_code` at `tools/shell_exec.go:156` `res.Meta = &meta`); `toolResultEvent` gains ONE new line stamping `ev.Actions.ArtifactDelta` from that meta key. This is the minimal addition and `internal/agent/llm_agent_events.go` owns it. NOTE: this is NOT the `ask_user` sentinel mechanism — that path is name-gated to `ask_user` only (amendment #51/D-40) and a non-ask_user sentinel is dropped as a RoleTool error.
   - What's unclear (now decided): emit a NEW AG-UI custom event for artifacts, OR extend the existing `TOOL_CALL_RESULT` path with an artifact marker (like the `tool_call_id` StateDelta marker pattern at `translator.go:71`). → custom event chosen.
   - **Rationale:** Use `Actions.ArtifactDelta` + a dedicated translator branch emitting an AG-UI **custom event** (the SDK has custom-event support per spike 015's 28-event surface). It's channel-agnostic (D-06), keeps tool results clean, and reuses the field already designed for it. Tradeoff: a custom event needs each channel to handle-or-ignore it (Telegram→sendDocument, CLI→print path, AG-UI HTTP→pass through); the tool_call_result-marker approach avoids a new event type but conflates "artifact produced" with "tool returned text," which is exactly the conflation `translator.go:71` already had to special-case. Prefer the clean custom event.

2. **TTS trigger shape (#59 point 3 discretion).**
   **RESOLVED:** `voice_mode` pref + auto-echo on voice input (Plan 13-08). No explicit `send_voice` tool this phase; the modality echoes the user's input modality plus the `voice_mode` preference.
   - Explicit `send_voice` tool (symmetric with `send_file`, deferred-tool pattern) vs auto-reply-vocale when the user sent a voice note vs `voice_mode` preference (Slice 10 `preferences.json` already has the field).
   - **Rationale:** Support BOTH the `voice_mode` preference AND auto-on-voice-input (cheap, no new tool). Defer an explicit `send_voice` tool unless the operator wants the agent to *choose* per-message — `voice_mode` + echo-modality covers the smoke. Note Slice 10 isn't shipped yet, so `voice_mode` reading may need a stub/default until Phase 14.

3. **send_file `Deferred` flag + overflow (CONTEXT discretion).**
   **RESOLVED:** `Deferred:true`, `Mutating:false`, error on >50MB (Plan 13-02). The tool returns an error ToolResult the agent surfaces, never a silent truncation.
   - **Rationale:** `Deferred: true` (it has a path/caption schema + examples, fits the deferred rule) and `Mutating: false` (it reads a file, sends it — no host mutation). On >50MB: return an error ToolResult the agent surfaces as a user-facing message (don't silently truncate). Caption ASCII-safe (Pitfall 4 applies to document captions too).

4. **qr_svg this phase or deferred?**
   **RESOLVED:** deferred SVG stub, terminal ASCII QR is the real path this phase (Plan 13-07). The `qr_svg` JSON field stays for forward-compat (empty/omitted body); `qrterminal` ASCII QR is the live onboarding path.
   - **Rationale:** See A5 — recommend deferring the SVG body (frontend deferred) and returning an empty/omitted `qr_svg`, keeping the JSON field for forward-compat. Terminal ASCII QR (qrterminal) is the real onboarding path this phase.

## Sources

### Primary (HIGH confidence — read live in this session)
- `internal/agui/fanout.go` (Subscribe-before-Run, Run-once, drop-on-full cap-64, lifecycle-frame guarantee) — file:line cited throughout §2.
- `internal/agui/translator.go` + `types.go` (real AG-UI event set, ID generators, interrupt mapping) — §2.
- `internal/agent/event.go:37-129` (`Actions.ArtifactDelta` field, ToolInvocation, AwaitingInput) — §5 artifact decision.
- `internal/agent/llm_agent.go:357` (`runTool` → `ToolResult` projection) + `internal/agent/llm_agent_events.go:69-105` (`toolResultEvent` lifts `ToolResult.Meta` onto the Event; the ArtifactDelta stamp lands here) — §5 emit seam.
- `internal/agent/tools/spec.go:53-63` (`ToolResult.Meta *ToolResultMeta` outbound channel) + `internal/agent/tools/shell_exec.go:156` (`res.Meta = &meta` analog) — §5 emit seam.
- `internal/agent/tools/result.go` (`WithToolCallContext`/`toolCallCtx` — INBOUND-only ctx; NOT an outbound Event stamp) + `internal/agent/tools/web_fetch.go` (reads inbound convID, returns a normal ToolResult) + `internal/agent/llm_agent_pause.go` (the ask_user sentinel→Event stamp, name-gated to ask_user) — §5 (why send_file uses Meta, not the sentinel).
- `internal/runner/runner.go:177` (`Turn` iterator), `interfaces.go` (narrow stores) — §1/§2.
- `cmd/aura/serve.go:50-145` (`serveEnv`/`bootServe` mount pattern, fail-soft HTTP) — §1.
- `cmd/aura/chat_repl.go` (Runner-driven turn loop, pause render, /search excerpt) — §1, HITL.
- `internal/agent/tools/spec.go` + `current_time.go` + `ask_user.go` (deferred-tool convention, ToolResult) — §5, send_file.
- `internal/askuser/store.go` + `internal/llm/prices.go` + `internal/conversations/store.go:418` (HITL/cost/search backends) — Don't Hand-Roll.
- `internal/db/migrations/0004,0009` (migration + grant + COMMENT pattern) — §8.
- `internal/llm/config.go:77` (`Model` is a bare string field; no `Model` struct, no `openai_compat/` package — the basis for the net-new `internal/llm/models.go`) — §7.
- `internal/config/config.go` (env helpers, loopback bind, `:9080` AG-UI) — env catalog.
- `prd.md` §Slice 9 (2856-3208) + amendments #58/#59/#60 — binding decisions (status pane B, throttle 1500/500/1000, 10 commands, doc tiers, migration schema, sidecar compose, AURA_VISION_CLOUD).
- `.claude/skills/spike-findings-Aura/references/telegram-channel.md` — binding 9b blueprint (telebot pin, MarkdownV2 Pitfall #18, tables PNG, sendDocument).
- `.planning/spikes/MANIFEST.md` §Session-5/6 (binding operator requirements, spikes 017-029 verdicts).
- `.planning/phases/13-channels-telegram-multimodal/13-CONTEXT.md` + `.planning/REQUIREMENTS.md` (UX-02/03/04).

### Secondary (MEDIUM — verified against Go proxy)
- Go module proxy: telebot.v4 v4.0.0-beta.9, x/image v0.41.0, qrterminal v3.2.1, skip2/go-qrcode (2020 pseudo), boombuler/barcode v1.1.0 — version + date confirmed 2026-06-08.

### Tertiary (LOW — flagged)
- Package legitimacy (slopcheck): UNVERIFIED this session (tool unavailable) — A1, planner gates before install.
- markitdown `/convert` endpoint shape — A2, verify at implementation.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — every lib spike-validated or PRD-locked + registry-confirmed; legitimacy gate deferred to planner (A1).
- Architecture (interface, fanout wiring, registry mount): HIGH — integration seams read at file:line; the fanout was built for exactly this.
- Multimodal wiring: HIGH — engine/endpoints spike-locked; A2 (markitdown endpoint) is the one MEDIUM spot.
- Migration: HIGH — number corrected to 0012, schema from PRD, pattern from 0009/0004.
- Pitfalls: HIGH — all from live spikes (017/019/027) + the fanout panic guards + amendment #15 goleak mandate.

**Research date:** 2026-06-08
**Valid until:** 2026-07-08 (stable; telebot is a beta tag — re-check if a new tag lands before planning, but the spike-pinned beta.9 is the locked decision regardless).

## RESEARCH COMPLETE

Phase 13 is implementation wiring over already-locked decisions. The Telegram channel consumes the Phase-12 `internal/agui/fanout.go` seam via **Subscribe-before-Run per turn** (status + content consumers), driving `runner.Turn` through `agui.Translate` → `NewFanout` → `Run`; the `Channel` interface + `Registry` mount in `bootServe` exactly like the existing scheduler/AG-UI subsystems; the setup wizard is a token-gated loopback HTTP+SSE API on `:9081` (poll-DB-2s); migration is **0012** (not the PRD's stale 0008); and 9c is four thin OpenAI-compat HTTP clients to CPU sidecars with a single `AURA_VISION_CLOUD` env branch. All external deps are registry-verified at the exact spike-pinned versions. The biggest enabling discovery is that `Actions.ArtifactDelta` already exists on the Event for the channel-agnostic artifact path, making D-06 a low-risk additive translator branch (the emit seam itself must be added in `toolResultEvent` — see OQ1).

**Open decisions (all RESOLVED — see §Open Questions (RESOLVED)):**
1. Artifact event shape — RESOLVED: custom AG-UI event off `Actions.ArtifactDelta`; emit seam added in `toolResultEvent` lifting `ToolResult.Meta` (Plan 13-02).
2. TTS trigger — RESOLVED: `voice_mode` pref + auto-on-voice-input; explicit `send_voice` tool deferred (Plan 13-08).
3. send_file flags — RESOLVED: `Deferred:true`, `Mutating:false`, error (not truncate) on >50MB, ASCII caption (Plan 13-02).
4. `qr_svg` — RESOLVED: defer the SVG body, keep the JSON field; terminal ASCII QR (qrterminal) is the real path (Plan 13-07).
5. Package legitimacy (slopcheck unavailable) — planner gates each `go get` behind a verify checkpoint (A1).
