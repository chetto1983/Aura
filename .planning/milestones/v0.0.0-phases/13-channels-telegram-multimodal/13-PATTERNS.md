# Phase 13: Channels + Telegram + Multimodal - Pattern Map

**Mapped:** 2026-06-08
**Files analyzed:** 33 (new/modified)
**Analogs found (verified at file:line):** 28 with a clean replication target / 33 total

> Every analog below was **read live this session** at the cited path:line and the excerpt is copied
> from the real file. Where the research (`13-RESEARCH.md`) cited a path that does **not** exist
> (`internal/llm/openai_compat/models.go`, `internal/channels/`), this map corrects it and flags the
> file as partially-novel. The phase is glue, not invention: most files mirror an established shape.

---

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/channels/channel.go` | interface decl | lifecycle contract | `internal/agent/tools/spec.go` (Tool iface) | role-match (greenfield iface) |
| `internal/channels/registry.go` | registry / lifecycle aggregator | daemon-subsystem | `internal/agent/tools/spec.go` Registry + `cmd/aura/serve.go` fail-soft | role-match |
| `cmd/aura/serve.go` (MODIFY) | daemon composition root | subsystem mount | `cmd/aura/serve.go:135-144` (AG-UI mount in bootServe) | exact (same file) |
| `internal/channels/telegram/bot.go` | daemon channel | event-driven poll loop | `internal/agui/server.go` + `cmd/aura/serve.go:84-88` fail-soft goroutine | role-match |
| `internal/channels/telegram/agui_subscriber.go` | seam consumer | per-turn fanout drive | `internal/agui/fanout.go:41-67` (Subscribe-before-Run) | exact |
| `internal/channels/telegram/renderer.go` | event→message render | request-response stream | `internal/agui/translator.go` event switch | role-match |
| `internal/channels/telegram/status_pane.go` | render manager | streaming/throttle | `internal/agui/translator.go` (event family handling) | partial |
| `internal/channels/telegram/mdv2.go` | pure transform | string transform | NONE clean (escaper is in-tree-novel; spec from spike) | **partial-novel** |
| `internal/channels/telegram/tables.go` | pure transform | render (PNG) | NONE clean (x/image PNG; spike 018b source) | **partial-novel** |
| `internal/channels/telegram/artifact.go` | event consumer | event-driven | `internal/agui/translator.go:71` (tool_call_id marker branch) | role-match |
| `internal/channels/telegram/hitl.go` | UX surface over pause | request-response | `internal/runner/runner_resume.go` (SubmitAnswer/PendingFor) + `internal/askuser/store.go` | role-match |
| `internal/channels/telegram/commands.go` | command dispatch | request-response | `cmd/aura/chat_repl.go` (slash-command intercept) | role-match |
| `internal/channels/telegram/onboarding.go` | store writer | CRUD (atomic) | `internal/askuser/store.go` (db.WithTx consume) | role-match |
| `internal/channels/telegram/config.go` | config | env read | `internal/config/config.go:343-369` (envIntDefault/envBoolDefault) | exact |
| `internal/channels/telegram/store.go` | sqlc adapter | CRUD | `internal/askuser/store.go` (Store{pool,q}) | exact |
| `internal/channels/telegram/voice.go` | HTTP client (STT) | request-response (multipart) | `internal/llm/openai_compat/client.go:36-48` (HTTP client ctor) | role-match |
| `internal/channels/telegram/tts.go` | HTTP client (TTS) | request-response | `internal/llm/openai_compat/client.go` | role-match |
| `internal/channels/telegram/photo.go` | HTTP client + env branch | request-response | `internal/agent/tools/web_fetch.go` (env-branch + result) + openai_compat client | role-match |
| `internal/channels/telegram/documents.go` | HTTP client (tiered) | file-I/O / async | `internal/llm/openai_compat/client.go` + `cmd/aura/serve.go` WaitGroup drain | partial |
| `internal/setup/server.go` | HTTP server | request-response | `cmd/aura/serve.go:139-143` (`http.Server`+ReadHeaderTimeout) | exact |
| `internal/setup/handlers.go` | HTTP handlers + SSE | request-response / SSE | `internal/agui/server.go:75-151` (Mux + handlers + SSE) | exact |
| `internal/setup/qr.go` | pure transform | encode (optional) | NONE (skip2/go-qrcode; defer per A5) | **partial-novel / deferrable** |
| `internal/setup/types.go` | DTO | shape decl | `internal/agui` request/response shapes | role-match |
| `internal/agent/tools/send_file.go` | deferred tool | event emit | `internal/agent/tools/web_fetch.go` (Deferred:true) + `ask_user.go` (sentinel emit) | exact |
| `internal/db/migrations/0012_telegram.up.sql` | migration | DDL | `internal/db/migrations/0009_scheduler.up.sql` + `0004_identity.up.sql` | exact |
| `internal/db/migrations/0012_telegram.down.sql` | migration | DDL | existing `*.down.sql` pair | exact |
| `internal/db/queries/telegram_accounts.sql` | sqlc queries | CRUD | `internal/db/queries/identity.sql` | exact |
| `internal/db/queries/telegram_setup_pending.sql` | sqlc queries | CRUD | `internal/db/queries/identity.sql` + `paused_states.sql` | exact |
| `internal/agui/translator.go` (MODIFY) | translator branch | event map | `internal/agui/translator.go:85-95` (STATE_DELTA branch) | exact (same file) |
| `internal/llm/config.go` or new `models.go` (MODIFY/NEW) | model capability flags | data decl | **NONE** — `Model` is a bare `string` today (`config.go:77`); no Model struct/`models.go` exists | **novel** |
| `internal/channels/main_test.go` + `telegram/main_test.go` + `internal/setup/main_test.go` | test harness | goleak | `internal/agent/tools/main_test.go` (TestMain + goleak) | exact |

---

## Pattern Assignments

### `internal/channels/channel.go` (interface, lifecycle contract)

**Analog:** `internal/agent/tools/spec.go:68-71` (the `Tool` interface — the codebase's idiom for a narrow capability interface).

The Channel interface is **greenfield** (Telegram is the first real channel), but it must be a *minimal* Go interface in the codebase idiom: a tiny method set, doc-commented, no fat params. Mirror the `Tool` shape:

```go
// internal/agent/tools/spec.go:68
type Tool interface {
	Spec() Spec
	Execute(ctx context.Context, args json.RawMessage) (ToolResult, error)
}
```

**What to copy:** the narrowness (4 methods, all on the interface), `context.Context` first param on lifecycle methods, the package-level doc-comment that states the design rule.
**What differs:** the methods are `Name() string` / `Start(ctx) error` / `Stop(ctx) error` / `IsHealthy() bool` (per `13-RESEARCH.md` Pattern 1). **Critical (research §1):** `Start(ctx)` takes NO fanout subscriber — fanout is built per-turn inside the channel, not at start. The PRD's older `Start(ctx, sub)` sketch is wrong for this codebase.

---

### `internal/channels/registry.go` (registry / lifecycle aggregator, daemon-subsystem)

**Analog A — Registry container:** `internal/agent/tools/spec.go:76-99` (`Registry{tools map}` + `NewRegistry`/`Register`/`All`).
**Analog B — fail-soft error posture:** `cmd/aura/serve.go:84-88` (one subsystem failing must NOT abort the daemon).

```go
// internal/agent/tools/spec.go:80-86
func NewRegistry() *Registry { return &Registry{tools: make(map[string]Tool)} }
func (r *Registry) Register(t Tool) { r.tools[t.Spec().Name] = t }
```

```go
// cmd/aura/serve.go:84-88 — the fail-soft posture StartAll must replicate
go func() {
	if err := env.httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("aura serve: agui http server stopped", "err", err)
	}
}()
```

**What to copy:** the map-backed registry + `Register`; the "log but never exit" fail-soft posture.
**What differs:** `StartAll`/`StopAll` read `AURA_CHANNEL_<NAME>_ENABLED` (default true, via `config.envBoolDefault` shape) per channel before calling `Start`, and **aggregate errors with `errors.Join`** so one failed channel does not abort the daemon (research §1 / Pattern 2). `--no-telegram`/`--only=cli` flags override the env.

---

### `cmd/aura/serve.go` (MODIFY — channels registry mount in bootServe)

**Analog:** the **same file**, `serve.go:135-144` — the AG-UI gateway is already mounted in `bootServe` exactly this way. The channels registry is a new sibling subsystem mounted identically.

```go
// cmd/aura/serve.go:135-144 — copy this mount shape for the channels registry
aguiServer := agui.NewServer(chat.run, chat.conv, agui.ServerConfig{
	CORSPermissive: chat.cfg.AGUICORSPermissive,
	BufferCap:      chat.cfg.AGUIBufferCap,
})
httpSrv := &http.Server{
	Addr:              chat.cfg.AGUIBind,
	Handler:           aguiServer.Mux(),
	ReadHeaderTimeout: aguiReadHeaderTimeout,
}
return &serveEnv{chatEnv: chat, store: store, scheduler: scheduler, httpSrv: httpSrv}, nil
```

Lifecycle (`runServe`, `serve.go:83-102`) shows the start-goroutine + graceful-`Shutdown`-on-ctx-cancel pattern the registry's `StartAll`/`StopAll` must slot into:

```go
// serve.go:93 — Start blocks until signal; serve.go:98-102 — bounded drain on shutdown
schedErr := env.scheduler.Start(ctx)
shutCtx, cancel := context.WithTimeout(context.Background(), aguiShutdownTimeout)
defer cancel()
if err := env.httpSrv.Shutdown(shutCtx); err != nil { ... }
```

**What to copy:** add a `channels *channels.Registry` field to `serveEnv`; in `bootServe` build `telegram.New(telegram.Deps{Runner: chat.run, Conv: chat.conv, Pause: chat.pause, Token: cfg.TelegramBotToken, Store: telegramStore, ...})` and `reg.Register(tg)`; in `runServe` call `reg.StartAll(ctx)` alongside `scheduler.Start` and `reg.StopAll` on shutdown **before `env.close()`**. The setup HTTP server (`:9081`) mounts as a third `http.Server` sibling to the AG-UI one.
**What differs:** two new subsystems (channels registry + setup server) instead of one; both fail-soft.

---

### `internal/channels/telegram/agui_subscriber.go` (seam consumer, per-turn fanout drive)

**Analog:** `internal/agui/fanout.go:41-67` (verified: `NewFanout`/`Subscribe`/`Run`) + `internal/agui/translator.go:23` (`Translate` signature) + `internal/runner/runner.go:177` (`Turn` iterator).

```go
// internal/runner/runner.go:177 — the source iterator (verified signature)
func (r *Runner) Turn(ctx context.Context, convID string, userMsg *string) iter.Seq2[*agent.Event, error]

// internal/agui/translator.go:23 — the pure mapper
func Translate(threadID, runID string, idgen IDGenerator, seq iter.Seq2[*agent.Event, error]) iter.Seq2[events.Event, error]

// internal/agui/fanout.go:50-67 — Subscribe-before-Run (PANICS otherwise)
func (f *Fanout) Subscribe() <-chan events.Event {
	if f.started.Load() { panic("agui: Fanout.Subscribe called after Run ...") }   // :51
	...
}
func (f *Fanout) Run(ctx context.Context) {
	if !f.started.CompareAndSwap(false, true) { panic("agui: Fanout.Run called twice ...") } // :67
	...
}
```

**What to copy (exact wiring, research §2 Code Example):**
```go
translated := agui.Translate(convID, runID, idgen, t.runner.Turn(ctx, convID, &userMsg))
fo := agui.NewFanout(translated)
statusCh := fo.Subscribe()   // status_pane.go
contentCh := fo.Subscribe()  // renderer.go — BOTH before Run
fo.Run(ctx)
```
**What differs / anti-patterns to obey:** Subscribe BOTH consumers **before** `Run` (`fanout.go:51` panics on Subscribe-after-Run); one Fanout = one turn = one `Run` (`fanout.go:67` panics on double-Run). The channel holds the `*runner.Runner` and builds a fresh `Fanout` inside each turn handler — never one at channel start.

---

### `internal/channels/telegram/renderer.go` + `status_pane.go` (event→message render)

**Analog:** `internal/agui/translator.go:34-141` — the canonical event-family switch. The renderer consumes the SAME `events.Event` set the translator produces, so the switch arms map 1:1.

Event→render map (research §2, derived from the real `translator.go` output):
`RUN_STARTED`→open pane; `TOOL_CALL_START/ARGS/END/RESULT`→pane tool list 🟡→✅/❌; `REASONING_*`→pane "💭"; `TEXT_MESSAGE_*`→content (renderer); `STATE_DELTA`→running cost on pane footer; `RUN_FINISHED(success)`→finalize; `RUN_FINISHED(interrupt)`→HITL; `RUN_ERROR`→❌.

```go
// translator.go:220-238 — the tool-lifecycle event shape the renderer must recognize
case agent.ToolInvocationStart:
	yield(events.NewToolCallStartEvent(ti.ToolCallID, ti.ToolName), nil)
	if ti.Arguments != "" { yield(events.NewToolCallArgsEvent(ti.ToolCallID, ti.Arguments), nil) }
case agent.ToolInvocationEnd:
	yield(events.NewToolCallEndEvent(ti.ToolCallID), nil)
	yield(events.NewToolCallResultEvent(idgen.NewToolResultID(ti.ToolCallID), ti.ToolCallID, ti.ResultPreview), nil)
```

**What to copy:** the exhaustive event-type switch idiom; deterministic handling.
**What differs:** the renderer is stateful (status pane = msg #1 edited in place; content = msg #2 streamed) with throttles (status 1500ms / content 500ms / chat queue 1000ms per chat_id, from PRD Punto). Text goes through `mdv2.go` escape + plain-text fallback; tables→`tables.go` PNG sendPhoto; artifact→`artifact.go` sendDocument. Use `testing/synctest` for throttle tests (research test map).

---

### `internal/channels/telegram/mdv2.go` (pure transform) — PARTIALLY NOVEL

**Analog:** **NONE clean in-tree.** This is an in-tree ~80 LOC escaper, locked by amendment #4 (the supply-chain alternative `telegramify-markdown-go` was rejected). Spec is binding from the spike, not from a code analog.

**Source of truth:** `.claude/skills/spike-findings-Aura/references/telegram-channel.md` "MarkdownV2 discipline" (Pitfall #18 strict). Reserved OUTSIDE entities: `_ * [ ] ( ) ~ ` `> # + - = | { } . ! \`. Inside `` ``` ``/`` ` `` fences only `` ` `` and `\` are reserved. Escape OUTSIDE intended spans; NEVER whole-string. Always keep the plain-text fallback (resend without ParseMode on a 400).

**What differs:** no replication target — the planner is *inventing* here against a spike spec. Proof obligation: a 10K-Unicode fuzz (`FuzzMdv2`) producing zero `400 can't parse entities`. Closest *discipline* analog for "pure transform + golden/fuzz test" is the escaper test pattern, not a code body.

---

### `internal/channels/telegram/tables.go` (pure transform, render PNG) — PARTIALLY NOVEL

**Analog:** **NONE in-tree.** x/image + `font/gofont/gomono` PNG pipeline. The proven body is the spike source `.claude/skills/spike-findings-Aura/sources/018b` (~150 LOC, 5-21ms), NOT an in-repo file. `golang.org/x/image` is currently an indirect dep — promote to direct `v0.41.0`.

**What differs:** no in-tree replication target; the planner copies from the spike source. Test = golden PNG bytes (deterministic dims+grid) for 4-col and 6-col; pre-block fallback ≤56 char/row.

---

### `internal/channels/telegram/artifact.go` (event consumer → sendDocument)

**Analog:** `internal/agui/translator.go:71-83` — the `tool_call_id`-marker branch shows the exact idiom for "special-case an Event by a marker in `Actions.StateDelta` and emit a distinct AG-UI event." The artifact path adds the symmetric branch off `Actions.ArtifactDelta`.

```go
// translator.go:71 — the marker-disambiguation pattern to mirror for artifacts
if callID, ok := toolResultCallID(ev); ok {
	if !closeRuns() { return }
	yield(events.NewToolCallResultEvent(idgen.NewToolResultID(callID), callID, preview), nil)
	continue
}
```

**Field already exists (key finding):** `internal/agent/event.go:71` — `ArtifactDelta map[string]any` is present, forward-compat, currently **unmapped** by the translator. The artifact branch in `translator.go` (see MODIFY entry below) emits a custom AG-UI event off it; `artifact.go` in the channel is the *consumer* that renders that event via `tele.Document{File: FromDisk(path), FileName: name}` (Telegram auto-detects MIME — spike 019).
**What differs:** the consumer side is Telegram-specific (sendDocument); the substrate stays channel-agnostic (D-06).

---

### `internal/channels/telegram/hitl.go` (UX surface over pause)

**Analog:** `internal/runner/runner_resume.go:68/89/172` (`SubmitAnswer`/`SubmitAnswers`/`PendingFor` — verified) + `internal/askuser/store.go` (the pause backend). The Runner is the SOLE `paused_states` writer; HITL is render-only over it.

The pause arrives as an AG-UI interrupt — `translator.go:48-55` is the exact event the channel keys on:
```go
// translator.go:48 — AwaitingInput → RUN_FINISHED(interrupt); the HITL trigger
if ai := ev.Actions.AwaitingInput; ai != nil {
	yield(events.NewRunFinishedEventWithOptions(threadID, runID,
		events.WithInterruptOutcome([]types.Interrupt{interruptFrom(ai)})), nil)
	return
}
```
**What to copy:** read pending via `runner.PendingFor(ctx, convID)`; resume via `runner.SubmitAnswer(ctx, token, ResponseInput{...})`; the three-action accept/decline/cancel model (`askuser` `ActionAccept`/`ActionDecline`/`ActionCancel`).
**What differs:** render options→`InlineKeyboard`, clarification (no options)→`ForceReply`; callback → resume → `runner.Turn(ctx, convID, nil)` continues. (Research A4: REPL proves these signatures exist.)

---

### `internal/channels/telegram/commands.go` (command dispatch, no LLM)

**Analog:** `cmd/aura/chat_repl.go` (slash-command intercept loop). `/cost` and `/search` MUST reuse the EXISTING locked surfaces byte-for-byte (cross-slice invariant test):

```go
// internal/conversations/store.go:418 — /search backend (verified)
func (s *Store) SearchConversationTurns(ctx context.Context, query string, limit int) ([]SearchResult, error)

// internal/llm/prices.go:32-57 — /cost backend (verified): CostUSDValue / CostUSD
func CostUSDValue(prices map[string]Price, model string, promptTokens, completionTokens int, providerCost *float64) (usd float64, ok bool)
```

**What to copy:** the bot-intercept-before-LLM dispatch; reuse of `SearchConversationTurns`/`CostUSD` (no new queries).
**What differs:** 10 commands, Telegram-native rendering. Unit test: `/cost`==CLI, `/search`==CLI assertion.

---

### `internal/channels/telegram/config.go` (config, env read)

**Analog:** `internal/config/config.go:343-369` (`envIntDefault`/`envBoolDefault` — verified) + `:230` (`envDefault("AURA_AGUI_BIND", "127.0.0.1:9080")`).

```go
// config.go:359-369 — the env-bool helper idiom (malformed → fallback, never fatal)
func envBoolDefault(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" { return fallback }
	b, err := strconv.ParseBool(v)
	if err != nil { return fallback }
	return b
}
```

**What to copy:** the silent-fallback env-helper pattern; loopback bind default.
**What differs (env catalog, research §7):** Aura-native knobs use `AURA_<DOMAIN>_<UNIT>` — `AURA_CHANNEL_TELEGRAM_ENABLED`, `AURA_SETUP_TOKEN`, `AURA_SETUP_BIND` (default `127.0.0.1:9081`), `AURA_VISION_CLOUD`, `AURA_TELEGRAM_STATUS_THROTTLE_MS`/`_CONTENT_THROTTLE_MS`/`_CHAT_RATE_LIMIT_MS`. Third-party/sidecar keep upstream naming: `TELEGRAM_BOT_TOKEN`, `MULTIMODAL_*`, `STT_*`, `TTS_*` (CLAUDE.md exception).

---

### `internal/channels/telegram/store.go` + `onboarding.go` (sqlc adapter + atomic writer)

**Analog:** `internal/askuser/store.go:1-64` (verified — the canonical `Store{pool,q}` pattern, explicitly "copies the canonical Store pattern proved in internal/identity").

```go
// internal/askuser/store.go:56-64 — the Store shape to replicate
type Store struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
}
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool, q: sqlc.New(pool)} }
```

**What to copy:** `Store{pool,q}`; SQLSTATE classification via `errors.As`+`pgErr.Code` (never message match); sentinel errors; pgtype conversion at the boundary; `db.WithTx` for the atomic consume-pending-and-INSERT-account step (`onboarding.go`).
**What differs:** new domain projections (`telegram_accounts` / `telegram_setup_pending`); queries live in `internal/db/queries/telegram_*.sql`.

---

### `internal/channels/telegram/voice.go` / `tts.go` / `photo.go` / `documents.go` (sidecar HTTP clients)

**Analog:** `internal/llm/openai_compat/client.go:36-48` (verified — the handrolled HTTP-client ctor idiom: ctx-driven timeout on the dialer, no `http.Client.Timeout`, `DisableKeepAlives` for goleak-clean) + `internal/agent/tools/web_fetch.go` (the env-branch + result-marshal idiom, for `photo.go`).

```go
// openai_compat/client.go:36-48 — the sidecar HTTP-client ctor to mirror
func New(cfg llm.Config) *Client {
	return &Client{
		cfg: cfg,
		httpClient: &http.Client{
			Transport: &http.Transport{
				DialContext: (&net.Dialer{Timeout: ...}).DialContext,
				DisableKeepAlives: true,
			},
		},
	}
}
```

**What to copy:** ctx-bound timeout, `DisableKeepAlives` (goleak), the secret-only-on-header discipline; for `photo.go` the single-runtime-branch idiom.
**What differs (research §7):** these are plain OpenAI-compat POSTs to CPU sidecars, not the streaming SSE client — far simpler. `voice.go` = `mime/multipart` POST to `STT_BASE_URL/audio/transcriptions` (OGG/Opus direct, no ffmpeg; 2 retries 1s/2s; hard-fail UX). `tts.go` = POST `TTS_BASE_URL/audio/speech` `response_format=opus` → `sendVoice` (ASCII-clean caption, Pitfall 4). `photo.go` = **ONE function, ONE `if cfg.VisionCloud { ...OpenRouter image_url... } else { ...aura-ocr-vl... }` branch** (Pitfall 6 / #60: default false, switch = `.env` only, zero code dup). `documents.go` = tiered markitdown `/convert` (≤5MB sync / 5-50MB async / >50MB refuse; async goroutine tracked by a WaitGroup the channel `Stop` drains — goleak). **A2 caveat:** markitdown `/convert` endpoint shape is `[ASSUMED]` — verify at impl.

---

### `internal/setup/server.go` + `handlers.go` (HTTP server + SSE pump)

**Analog:** `cmd/aura/serve.go:139-143` (`http.Server` + `ReadHeaderTimeout`) + `internal/agui/server.go:67-151` (verified — `NewServer`/`Mux` no-router method-pattern routing + handler + SSE).

```go
// agui/server.go:75-80 — the Mux idiom (Go 1.22 method-pattern, no chi/gorilla)
func (s *Server) Mux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /agent/run", s.handleRun)
	mux.HandleFunc("GET /threads/{id}/messages", s.handleMessages)
	return s.withCORS(mux)
}

// agui/server.go:145-146 — SSE response headers to copy for /setup/events
w.Header().Set("Content-Type", "text/event-stream")
w.Header().Set("Cache-Control", "no-cache")
```

**What to copy:** `http.NewServeMux` + method-pattern routes; `http.Server{ReadHeaderTimeout: ...}`; the SSE header set; graceful bounded `Shutdown` on ctx-cancel (`serve.go:98-102`).
**What differs:** separate loopback `:9081` (not `:9080`); a `requireSetupToken` middleware (401 if `?token=`/`X-Aura-Setup-Token` ≠ `AURA_SETUP_TOKEN`, generated UUIDv4 to stdout first boot, in-memory only, invalidated after onboarding); `/setup/token` (telebot `getMe` validate), `/setup/onboard-link` (`{deep_link, qr_svg}`), `/setup/status`, `/setup/events` SSE that **polls `telegram_setup_pending.consumed_at` every 2s** and MUST exit on `r.Context().Done()` AND server shutdown (goleak, Pitfall 5).

---

### `internal/agent/tools/send_file.go` (deferred tool, event emit)

**Analog:** `internal/agent/tools/web_fetch.go:39-59` (verified — `Deferred:true` ToolSpec literal with full Description + example) + `internal/agent/tools/ask_user.go:97-119` (the sentinel-emit-instead-of-result idiom) + `spec.go:31-45` (Spec struct: `Mutating` flag).

```go
// web_fetch.go:47-58 — the Deferred:true ToolSpec literal to mirror
return Spec{
	Name:    "web_fetch",
	Summary: "Fetch a public web page and return it as readable markdown.",
	Description: "Fetch a single public web page ... Example: {\"url\":\"https://...\"}.",
	Parameters: params,
	Deferred:   true,
}
```

**What to copy:** the `Spec{}` literal with `Deferred:true`, `Summary` (one line, manifest-visible) + full `Description` + JSON-schema `Parameters` + an inline example; the `internal/agent/tools/<name>.go` file convention.
**What differs (research §5 recommendation):** schema `{path, caption?}`; `Deferred:true`, `Mutating:false` (reads+sends a file, no host mutation); Execute emits a generic artifact event into the AG-UI stream via `Actions.ArtifactDelta` (D-06 channel-agnostic) rather than returning prose; on >50MB return an error ToolResult (don't truncate); ASCII-safe caption.

---

### `internal/db/migrations/0012_telegram.{up,down}.sql` + `queries/telegram_*.sql`

**Analog:** `internal/db/migrations/0009_scheduler.up.sql` + `0004_identity.up.sql` (both verified) for the migration; `internal/db/queries/identity.sql` for the sqlc queries.

```sql
-- 0009_scheduler.up.sql:60-68 — the grant + COMMENT footer pattern to copy
GRANT SELECT, INSERT, UPDATE, DELETE ON aura.scheduler_tasks TO aura_app;
GRANT ALL                            ON aura.scheduler_tasks TO aura_migrate;
COMMENT ON TABLE aura.scheduler_tasks IS '...';

-- 0004_identity.up.sql:14-18 — the FK-to-aura.identities pattern telegram_accounts copies
CREATE TABLE aura.capability_grants (
    identity_id uuid NOT NULL REFERENCES aura.identities (id) ON DELETE CASCADE,
    ...
);
```

```sql
-- queries/identity.sql:1-4 — the sqlc named-query idiom
-- name: CreateIdentity :one
INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, $3)
RETURNING id, name, kind, created_at;
```

**What to copy (verbatim idiom):** the header comment citing PRD §/amendment; `aura_app` = DML grants only, `aura_migrate` = `GRANT ALL`; `COMMENT ON TABLE`; FK `REFERENCES aura.identities(id)`; partial index (`0009:51` precedent for `WHERE consumed_at IS NULL`); `-- name: X :one/:many/:exec` sqlc query headers.
**What differs:** **number is 0012** (Pitfall 3 — `0011_tool_invocations` is the floor; `0008` is `proxied_child_id_text`, NOT telegram). Schema = `telegram_accounts` (PK `telegram_user_id bigint`, FK `identity_id`) + `telegram_setup_pending` (PK `onboarding_token`, single-use `consumed_at`, 1h `expires_at`, partial active index). Query files: `telegram_accounts.sql` (~6 queries), `telegram_setup_pending.sql` (~3, incl. atomic consume-and-return).

---

### `internal/agui/translator.go` (MODIFY — artifact translator branch)

**Analog:** the **same file**, `translator.go:85-95` — the `STATE_DELTA` branch is the clean template for an additive event branch.

```go
// translator.go:85-95 — the additive branch pattern to copy for ArtifactDelta
if len(ev.Actions.StateDelta) > 0 {
	if !closeRuns() { return }
	if !yield(events.NewStateDeltaEvent(stateDeltaOps(ev.Actions.StateDelta)), nil) { return }
	continue
}
```

**What to copy:** a new `if len(ev.Actions.ArtifactDelta) > 0 { closeRuns(); yield(<custom artifact event>); continue }` branch, slotted next to STATE_DELTA, keying off the already-present `event.go:71` field.
**What differs (research OQ1 recommendation):** emit a dedicated AG-UI **custom event** (SDK custom-event support per spike 015) rather than overloading `TOOL_CALL_RESULT` — keeps tool results clean and stays channel-agnostic (each channel handles-or-ignores it). Low-risk additive change (one branch + one custom event), not a protocol redesign.

---

## Shared Patterns

### Fail-soft daemon-subsystem lifecycle
**Source:** `cmd/aura/serve.go:84-102` (start goroutine that logs-but-never-exits; graceful bounded `Shutdown` on ctx-cancel).
**Apply to:** `channels/registry.go` `StartAll`/`StopAll`, `telegram/bot.go` polling goroutine, `setup/server.go`. One failed subsystem must never abort the daemon; `errors.Join` for aggregation.

### sqlc Store pattern
**Source:** `internal/askuser/store.go:1-64` (`Store{pool,q}`, SQLSTATE via `errors.As`+`pgErr.Code`, sentinel errors, pgtype at boundary, `db.WithTx` for atomic multi-row).
**Apply to:** `telegram/store.go`, `telegram/onboarding.go`.

### Deferred-tool convention
**Source:** `internal/agent/tools/spec.go:31-45` (Spec + `Deferred`/`Mutating`) + `web_fetch.go:47-58` (Deferred literal) + CLAUDE.md tool-design rule.
**Apply to:** `send_file.go`.

### Migration grant + COMMENT + FK
**Source:** `0009_scheduler.up.sql:60-68` + `0004_identity.up.sql:14-32`.
**Apply to:** `0012_telegram.{up,down}.sql`.

### Sidecar HTTP-client ctor (ctx-timeout, goleak-clean)
**Source:** `internal/llm/openai_compat/client.go:36-48`.
**Apply to:** `voice.go`, `tts.go`, `photo.go`, `documents.go`.

### env-helper (silent fallback, AURA_ convention)
**Source:** `internal/config/config.go:343-369` + `:230`.
**Apply to:** `telegram/config.go`, `setup` config, the new `AURA_*` env vars.

### goleak TestMain
**Source:** `internal/agent/tools/main_test.go` (TestMain with `goleak.VerifyNone`).
**Apply to:** `internal/channels/main_test.go`, `telegram/main_test.go`, `internal/setup/main_test.go` (amendment #15 — bot polling, SSE pump, async convert all on the explicit goleak list).

---

## No Analog Found (genuinely novel — planner is inventing, not replicating)

| File | Role | Data Flow | Reason / Source of truth (not a code analog) |
|------|------|-----------|----------------------------------------------|
| `internal/channels/telegram/mdv2.go` | pure transform | string escape | In-tree escaper locked by amendment #4 (supply-chain alt rejected). Spec = spike `telegram-channel.md` Pitfall #18; proof = 10K fuzz. No in-repo body to copy. |
| `internal/channels/telegram/tables.go` | pure transform | PNG render | x/image+gofont pipeline. Proven body is the **spike source** `sources/018b` (~150 LOC), not an in-repo file. Promote `x/image` indirect→direct. |
| `internal/setup/qr.go` | pure transform | SVG encode | `skip2/go-qrcode` (2020 pseudo-version, unmaintained). Research A5/OQ4: **recommend deferring the SVG body** (frontend deferred), keep an empty/omitted `qr_svg` field; terminal ASCII QR via `qrterminal` is the real path. No analog needed if deferred. |
| `internal/llm` model-capability flags (`SupportsVision`/`SupportsAudio`) | data decl | flag lookup | **Research cited `internal/llm/openai_compat/models.go` — that file does NOT exist; `Model` is a bare `string` at `config.go:77`, no Model struct exists.** This is net-new: the planner must decide where the capability map lives (new `internal/llm/models.go` with a `model→{vision,audio}` table, or a method on config). `deepseek/deepseek-v4-flash`=false, `minimax/minimax-m3`=true. Pure addition, ~30 LOC, but **no existing struct to extend** — flag as novel + correct the research's stale path. |

---

## Channel interface / registry note (greenfield-but-shaped)

The `Channel` interface + `Registry` are **greenfield** (Telegram is the first real channel, `internal/channels/` is absent on disk — verified). They are **not** unanchored, however: they mirror two existing shapes — the narrow-interface idiom of `tools.Tool`/`tools.Registry` (`spec.go:68-99`) and the fail-soft daemon-subsystem lifecycle of the scheduler + AG-UI server in `bootServe`/`runServe` (`serve.go:83-145`). The planner should treat these as "role-match, replicate the shape" rather than pure invention: the method set is new but every wiring decision (mount point, start goroutine, graceful shutdown, error aggregation) has a verified in-repo precedent cited above.

## Metadata

**Analog search scope:** `internal/agui`, `internal/agent/tools`, `internal/agent`, `internal/runner`, `internal/askuser`, `internal/conversations`, `internal/llm` (+ `openai_compat`), `internal/config`, `internal/db/migrations`, `internal/db/queries`, `cmd/aura`.
**Files scanned (read live at file:line):** `serve.go`, `fanout.go`, `translator.go`, `spec.go`, `ask_user.go`, `current_time.go`, `web_fetch.go`, `askuser/store.go`, `event.go`, `prices.go`, `runner.go`, `server.go`, `client.go`, `config.go`, `0009_scheduler.up.sql`, `0004_identity.up.sql`, `identity.sql` (+ grep verification of `SearchConversationTurns`, `Turn`, `SubmitAnswer`/`PendingFor`, `SupportsVision` absence).
**Pattern extraction date:** 2026-06-08

## PATTERN MAPPING COMPLETE

33 files mapped — **28 pure/strong replication** (verified in-repo analog at file:line) and **5 partially-novel** (`mdv2.go`, `tables.go`, `qr.go`, the `SupportsVision`/`SupportsAudio` model-flags surface, and the greenfield `channel.go`/`registry.go` pair which is shaped by — but has no exact body in — the tools-registry + daemon-subsystem precedents). The single research correction the planner must heed: `internal/llm/openai_compat/models.go` does **not** exist (`Model` is a bare string at `config.go:77`), so the vision/audio capability flags are a net-new decl with no struct to extend.
