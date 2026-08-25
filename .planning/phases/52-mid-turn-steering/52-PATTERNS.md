# Phase 52: Mid-turn steering - Pattern Map

**Mapped:** 2026-08-25
**Re-mapped:** 2026-08-25 (revision iteration 2) — the original map was cut against a six-plan
outline. The plan set is now **eight** plans whose `files_modified` union is **66 paths**, and the
map's "19 / 19" line was asserting a completeness it did not have: the entire web layer (52-07) plus
nine Go/SQL paths were absent from the File Classification table.

**Files analyzed:** 44 production paths (net-new + modified) across the eight plans.
**Analogs found:** 44 / 44 — every production path now carries a row in the File Classification
table below, re-diffed against the plans' `files_modified` union rather than asserted.

**The 66-path union, accounted for exactly** (44 + 5 + 16 + 1 = 66):

- **44 production paths** — classified below, each with a same-repo analog.
- **5 planning/documentation artifacts** — `prd.md`, `.planning/REQUIREMENTS.md`,
  `.planning/ROADMAP.md`, `.planning/phases/52-mid-turn-steering/52-VALIDATION.md`,
  `docs/aura-quality-snapshot.md`. Prose, not code; their conventions come from the documents
  themselves and from CLAUDE.md, not from a structural twin.
- **16 test siblings** — every `*_test.go`, `*.test.ts`, `*.test.tsx`. Each follows the analog assigned
  to the production file it covers. `internal/agent/agent_fuzz_test.go` is the one exception and it
  gets its own row below, because 52-02 does not merely extend it: it must RESTATE a property that
  file currently asserts.
- **1 generated/built output** — `internal/webui/dist`, produced by `docker webbuild` on Linux and
  guarded by the `web-dist-freshness` gate; never hand-edited. It is also the phase's single
  "no analog found" entry (see that section).

**LOC headroom note (CLAUDE.md 600-LOC ceiling), measured 2026-08-25 — read before assigning
work to a plan:**

| File | Current LOC | Headroom | Verdict |
|---|---|---|---|
| `internal/agent/llm_agent.go` | 561 | 39 | **No new logic.** Two 1-line drain-point calls only. |
| `internal/agui/server.go` | 534 | 66 | One route registration + one setter call only. |
| `internal/runner/runner.go` | 577 | 23 | **Almost none.** One struct field + one config passthrough line MAX; the wiring function itself goes in a new sibling file. |
| `internal/askuser/store.go` | 527 | 73 | New TTL logic → new sibling file, not appended here. (Re-measured 2026-08-25 rev-2: 527, not the 513 first recorded.) |
| `internal/channels/telegram/commands.go` | 436 | 164 | Room for one busy-peek helper. |
| `internal/agui/idempotency_http.go` | 406 | 194 | Room for one map entry. |
| `internal/agui/translator.go` | 454 | 146 | Room for one CUSTOM-frame branch + const. |
| `internal/agent/event.go` | 298 | 302 | Room for one additive `Actions` field. |
| `internal/agent/llm_agent_finalize.go` | 270 | 330 | Reference only (pattern donor, not touched). |
| `internal/runner/runner_persist.go` | 532 | 68 | If a durable-steer-turn persistence branch is needed, it is tight — prefer a sibling file (`runner_persist_steer.go`) over appending here. |
| `internal/channels/telegram/bot_dispatch_turn.go` | 151 | 449 | Plenty of room. |
| `internal/config/config_agui_run.go` | 42 | — | Pattern donor for new `config_agui_steer.go` (net-new file, no ceiling pressure). |
| `internal/config/config_knobs.go` | 220 | 380 | Room for 3 catalog rows + 1 corrected default. |
| `internal/config/config_agui_test.go` | 88 | 512 | Room, or clone as a new sibling test file. |
| `internal/agui/server_project.go` | 106 | 494 | Room for the folded-todo validation additions. (Re-measured 2026-08-25 rev-2: 106, not the 93 first recorded.) |
| `internal/agui/runregistry.go` / `runsession.go` | 253 / 213 | large | Reference only (pattern donor for `internal/steer`, not touched). |
| `cmd/aura/serve_agui.go` | 245 | 355 | Room for the SteerInbox wiring block. |
| `cmd/aura/serve_channels.go` | 384 | 216 | Room for one `Deps{}` field. |
| `internal/agent/llm_agent_construct.go` | 89 | 511 | Room for one config→field thread. |

The two tightest files (`llm_agent.go` at 39 lines of headroom, `runner.go` at 23) are exactly
the two files every drain point and every wiring call must touch. Treat every line added there
as scarce; push all real logic into new sibling files.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `internal/steer/inbox.go` (new) | service (in-memory registry) | pub-sub / event-driven | `internal/agui/runregistry.go` + `runsession.go` | role-match (composite) |
| `internal/agent/llm_agent_steer.go` (new) | core loop helper | event-driven (drain + inject) | `internal/agent/llm_agent_finalize.go` | exact (sibling-file-of-llm_agent.go convention) |
| `internal/agent/llm_agent.go` (modified) | core loop (controller) | request-response / streaming | itself (2 call sites only) | exact |
| `internal/agent/llm_agent_construct.go` (modified) | constructor | — | itself | exact |
| `internal/agent/event.go` (modified) | model/DTO | — | itself (`ArtifactDelta`/`ViewDelta`/`Display` precedent) | exact |
| `internal/agui/translator.go` (modified) | transform (pure fn) | streaming | itself (`aura.artifact`/`aura.display` CUSTOM-frame branches) | exact |
| `internal/agui/server_run_steer.go` (new) | controller (HTTP route) | request-response | `internal/agui/server_run_resume.go` (`handleRunCancel`) | exact |
| `internal/agui/idempotency_http.go` (modified) | config/registry | — | itself (`agent_run_cancel` row) | exact |
| `internal/agui/server.go` (modified) | route registration | — | itself (line 344) | exact |
| `internal/channels/telegram/bot_dispatch_turn.go` (modified) | controller (channel dispatch) | event-driven | itself (`startTurn`/`sendBusy`) | exact |
| `internal/channels/telegram/commands.go` (modified) | service (busy registry) | — | itself (`registerTurn`) | exact |
| `internal/config/config_agui_steer.go` (new) | config | — | `internal/config/config_agui_run.go` | exact |
| `internal/config/config_knobs.go` (modified) | config catalog | — | itself (lines 114-120) | exact |
| `internal/config/config_agui_test.go` or new `config_agui_steer_test.go` | test | — | `internal/config/config_agui_test.go` | exact |
| `internal/agui/server_project.go` (modified) | pure mapping helper | transform | itself (`resumeAnswers`/`payloadString`) | exact |
| `internal/askuser/*` (TTL expiry, new sibling file) | service (reaper) | batch/event-driven | `internal/agui/runregistry.go` (`evictExpired`/reaper goroutine) | role-match |
| `internal/runner/runner.go` (modified) | composition | — | itself (`buildAgent`, 1-2 lines only) | exact |
| `internal/runner/runner_steer.go` (new, mirrors `runner_reasoning.go`) | wiring helper | — | `internal/runner/runner_reasoning.go` (`WithReasoningOverride`) | exact |
| `cmd/aura/serve_agui.go` (modified) | composition root | — | itself (lines 79-83, `RunRegistry` construction) | exact |
| `cmd/aura/serve_channels.go` (modified) | composition root | — | itself (`buildTelegramDeps`, line 78-98) | exact |
| `internal/agent/trust.go` (modified) | pure fn (render + trust decision) | transform | itself (`renderToolResultForPrompt` / `wrapUntrustedToolOutput`) | exact |
| `internal/agent/prompt.go` (modified) | static prompt constant | — | itself (`SystemPrompt`'s existing named static sections) | exact |
| `internal/agent/agent_fuzz_test.go` (modified) | property test | — | itself (`FuzzRenderToolResultForPrompt`) | exact — **but its current property contradicts 52-02's scrub and must be restated, not extended** |
| `internal/runner/runner_persist.go` (modified) | persistence dispatch | event-driven | itself (`persistEvent`'s existing per-`Actions`-field branches) | exact |
| `internal/runner/runner_resume.go` (modified) | resume controller | request-response | itself | exact |
| `internal/runner/runner_mint_approval.go` (modified) | pause minting | — | itself (the mint path that records pause metadata) | exact |
| `internal/askuser/store.go` (modified) | store (sqlc wrapper) | request-response | itself (the `WHERE resumed_at IS NULL` conditional update) | exact |
| `internal/askuser/expire.go` (new) | service (reaper) | batch/event-driven | `internal/agui/runregistry.go` (`evictExpired` + lazy-start ticker) | role-match |
| `internal/db/queries/paused_states.sql` (modified) | sqlc query source | — | itself (the existing `paused_states` queries; regenerate with sqlc, never hand-edit the generated Go) | exact |
| `internal/agui/server_run.go` / `server_run_detach.go` (modified) | controller | request-response / streaming | itself | exact — **read-only in 52-03's stated non-goals; a git-scoped criterion proves 52-04 leaves them alone** |
| `internal/channels/telegram/bot_dispatch_steer.go` (new) | controller (channel branch) | event-driven | `internal/channels/telegram/bot_dispatch_hitl.go` (the sibling-file dispatch-branch convention) | exact |
| `internal/channels/telegram/bot_dispatch_queue.go` (new) | service (per-chat pending slot) | event-driven | `internal/agui/runregistry.go` (mutex-guarded map keyed registry) | role-match |
| `internal/channels/telegram/bot_dispatch.go` (modified) | dispatch router | event-driven | itself (its existing branch-to-sibling-file dispatch) | exact |
| `internal/channels/telegram/bot.go` (modified) | channel struct + deps | — | itself (existing `Deps` field threading) | exact |
| `cmd/aura/serve.go` (modified) | composition root | — | itself (the existing background-worker start block the askuser TTL sweep joins) | exact |
| `cmd/aura/chat_boot.go` (modified) | composition root | — | itself (existing `chatEnv` shared-singleton construction) | exact |
| `internal/config/config.go` + `config_askuser.go` (new) | config | — | `internal/config/config_agui_run.go` (sub-struct + non-fatal envutil fallback) | exact |
| **— web layer (52-07); every analog below verified present in the tree 2026-08-25 —** | | | | |
| `web/src/chat/steerRun.ts` (new) | API client fn | request-response | `web/src/chat/sseResume.ts`'s `cancelRun` (same mutation-with-`Idempotency-Key` shape) | exact |
| `web/src/chat/sseAdapter.ts` (modified) | transform (frame → part) | streaming | itself, plus `web/src/chat/sseAdapter.onArtifact.test.ts` as the test shape for a new CUSTOM-frame handler | exact |
| `web/src/chat/sseResume.ts` (modified) | streaming client | streaming | itself (`cancelRun`, `streamRunResilient`) | exact |
| `web/src/chat/ExternalStoreChat_steer.ts` (new) | state slice split out of the container | — | `web/src/chat/ExternalStoreChat_liveRun.ts` (116 LOC) and its five `ExternalStoreChat_*.ts` siblings — the established split-to-stay-under-600 convention | exact |
| `web/src/chat/SteerNotice.tsx` (new) | presentational in-thread marker | — | `web/src/chat/compaction/CompactionMarker.tsx` (an in-thread, non-message marker) | exact |
| `web/src/chat/ExternalStoreChat.tsx` (modified) | container | — | itself — **545/600 LOC; wiring lines only, capped at +20 by 52-07** | exact |
| `web/src/chat/Composer.tsx` (modified) | container | — | itself (`sendDisabled` / `sendBlocked` prop) — **540/600 LOC; capped at +30** | exact |
| `web/src/i18n/resources.ts` (modified) | i18n bundle | — | itself — **566/600 LOC**; if two locales of steer copy do not fit, split rather than force them in | exact |
| `web/src/i18n/resources.steer.ts` (new, conditional) | i18n bundle split | — | `web/src/i18n/resources.composer.ts`, whose header states the split rationale verbatim (`resources.governance.ts` / `resources.graph.ts` set the precedent) | exact |

## Pattern Assignments

### `internal/steer` (new package) — the conversation-id-keyed inbox

**No direct analog exists** (confirmed absent by CONTEXT.md and by `find . -iname "*steer*"`
returning nothing). It is a composite of two proven in-memory patterns already in this
codebase, both in `internal/agui`:

**Analog 1 — keyed registry with mutex + map:** `internal/agui/runregistry.go:55-67`
```go
type RunRegistry struct {
	mu       sync.Mutex
	byRun    map[string]*RunSession
	byThread map[threadKey]*RunSession
	cfg      runRegistryConfig
	now      func() time.Time
	stop       chan struct{}
	reaperDone chan struct{}
	closed     bool
}
```
`internal/steer`'s inbox is the SAME shape keyed by conversation id alone (D-01: run-id-keyed
was rejected because Telegram has no run id — `runner.Turn` is called directly with no
RunRegistry in the loop at all, per `internal/channels/telegram/bot_dispatch_turn.go`). One
map (`map[string][]pendingSteer` or a bounded ring per conversation id), one mutex, no
run-scoped `byRun` twin needed — there is exactly one key space this time.

**Analog 2 — bounded queue with a drain contract:** `internal/agui/runsession.go:52-80`
(the `ring []seqEvent` fixed-cap buffer + `subscribeFrom`/`append` mutex discipline). The
steer inbox needs the SAME two invariants runsession.go already proves work: (a) append and
drain serialize on the same mutex so there is no lost-update window, (b) a cap bounds memory
(`AURA_AGUI_RUN_STEER_MAX` / `_MAX_BYTES`, Claude's Discretion in CONTEXT.md, mirroring
`AURA_AGUI_RUN_BUFFER_EVENTS`'s `defaultRunBufferEvents = 2048` convention at
`internal/agui/runsession.go:26`).

**What differs from both analogs:** no SSE fan-out, no subscriber set, no replay-from-seq —
the inbox is drained (consumed, not replayed) by the agent loop, which is a `Pop`/`DrainAll`
call, not a channel subscription. The closest shape for THAT half is a plain mutex-guarded
slice, e.g.:
```go
type Inbox struct {
	mu   sync.Mutex
	byConv map[string][]Message // FIFO per conversation, capped
}
func (i *Inbox) Push(convID string, msg Message) error { /* cap check, D-08 scrub happens in the agent, not here */ }
func (i *Inbox) Drain(convID string) []Message { /* pop-all, empties the slot */ }
```

### `internal/agent/llm_agent_steer.go` (new) — the two drain points

**Analog:** `internal/agent/llm_agent_finalize.go` (whole file) — this is the established
precedent for "logic that belongs conceptually inside the `Run` loop but is concern-split
into a sibling file because `llm_agent.go` is at its LOC ceiling" (see the file's own header
comment, lines 1-10): *"Concern-split out of llm_agent.go (D-07): that file is at its
no-god-class headroom, and the recovery counter + Italian-stub fallback land here."*
`llm_agent_steer.go` is the SAME move for the steer drain.

**Drain point A — before every API call.** `llm_agent.go:316-318` is where the per-round
`budget := a.roundBudget(ic)` is assembled, immediately before `buildRequest`/`prepareReasoningRequest`.
This is where a `a.drainSteer(ic)` call belongs (one line added to `llm_agent.go`; the real
logic — pop from inbox, wrap in the nonce marker, append `llm.Message{Role: llm.RoleUser, ...}`
to `a.history`, emit the `aura.steer` Event — lives in the new file).

**Drain point B — end of a tool-call batch.** `llm_agent.go:538-539`, the loop's own comment
marks the spot: `// loop: the next LLM call sees the appended RoleTool results.` — right after
`a.dispatch(...)` returns `done=false` and the `for` loop is about to re-iterate. Same
`a.drainSteer(ic)` call.

**Nonce-envelope pattern to reuse verbatim (D-07):** `internal/agent/trust.go:59-73`
```go
func wrapUntrustedToolOutput(source, content string) string {
	nonce := toolOutputNonce()
	escapedSource := html.EscapeString(source)
	escapedContent := html.EscapeString(norm.NFKC.String(content))
	return `<tool_output source="` + escapedSource + `" trust="untrusted" nonce="` + nonce + `">` +
		"\n" + escapedContent + "\n</tool_output>"
}

func toolOutputNonce() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("agent: crypto/rand failed minting tool output nonce: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}
```
D-07 reuses `toolOutputNonce()` directly (do not write a second minter — CLAUDE.md
"inventory before invention"); the steer marker's tag name and trust framing are new
(`<user_steer ... nonce="...">` or similar — Claude's Discretion), but the nonce call is the
SAME function, exported or package-visible as needed.

**Recovery-nudge injection precedent (the SHAPE of appending a user-role message mid-loop,
NOT the position — CONTEXT.md's Existing Code Insights is explicit that this does NOT license
the steer position):** `internal/agent/llm_agent_finalize.go:55-66`
```go
func (a *LlmAgent) maybeRecover(toolName string) (recovered bool) {
	if a.recoveryAttempts >= 1 {
		return false
	}
	a.recoveryAttempts++
	nudge := recoveryNudgeGeneric
	if toolName != "" {
		nudge = recoveryNudgeToolPrefix + toolName + recoveryNudgeToolSuffix
	}
	a.history = append(a.history, llm.Message{Role: llm.RoleUser, Content: nudge})
	return true
}
```
Copy the MECHANICS (append to `a.history` tail, never `messages[0]`) but not the counter
semantics — a steer is not a one-shot-per-run gate, it drains whatever is queued.

**Config field to add to `LlmAgentConfig`/`LlmAgent`, mirroring the `Gateway`/`Ledger` fields:**
`internal/agent/llm_agent.go:179-185` (config struct) and
`internal/agent/llm_agent_construct.go:32-56` (constructor threading):
```go
// llm_agent.go LlmAgentConfig
Gateway *gateway.Gateway
Ledger  VerificationLedger
```
```go
// llm_agent_construct.go NewLlmAgent
agent := &LlmAgent{
	...
	gateway: cfg.Gateway,
	ledger:  cfg.Ledger,
	...
}
```
A `Steer *steer.Inbox` (or a narrower interface) field follows the identical two-line pattern
in both files — nil-safe exactly like `gateway`/`ledger` are (nil gateway = Allow no-op, nil
ledger = gate disabled), so a steer-off deployment (`AURA_AGUI_RUN_STEER=false`, the explicit
rollback per D-12) degrades to "drain is a no-op" rather than a nil-pointer panic.

### `internal/agent/event.go` — the `aura.steer` echo payload field

**Analog:** `internal/agent/event.go:69-97`, the `Actions` struct's additive-field
convention:
```go
type Actions struct {
	Escalate      bool           `json:"escalate,omitempty"`
	StateDelta    map[string]any `json:"state_delta,omitempty"`
	ArtifactDelta map[string]any `json:"artifact_delta,omitempty"`
	ViewDelta      map[string]any  `json:"view_delta,omitempty"`
	AwaitingInput  *AwaitingInput  `json:"awaiting_input,omitempty"`
	ToolInvocation *ToolInvocation `json:"tool_invocation,omitempty"`
	Display *display.Payload `json:"display,omitempty"`
	DiscardStreamed bool `json:"discard_streamed,omitempty"`
}
```
Add `SteerDelta map[string]any \`json:"steer_delta,omitempty"\`` (untyped map, same rationale
as `ArtifactDelta`/`ViewDelta`: the shape is a wire payload for a channel-agnostic consumer,
not a persisted DB type — CONTEXT.md's Claude's Discretion leaves the exact shape open,
constrained only by "ring-buffered like every other frame"). Every existing event stays
byte-identical (additive `omitempty`).

### `internal/agui/translator.go` — the `aura.steer` CUSTOM frame

**Analog:** `internal/agui/translator.go:19-35` (the three existing CUSTOM-event-name
constants) and `:142-166` (the `ArtifactDelta`/`Display` emission branches):
```go
const ArtifactEventName = "aura.artifact"
const DisplayEventName = "aura.display"
const ViewEventName = "aura.mcp_view"
```
```go
if len(ev.Actions.ArtifactDelta) > 0 {
	if !closeRuns() {
		return
	}
	if !yield(events.NewCustomEvent(artifactEventName, events.WithValue(ev.Actions.ArtifactDelta)), nil) {
		return
	}
	continue
}
if ev.Actions.Display != nil {
	if !closeRuns() {
		return
	}
	if !yield(events.NewCustomEvent(DisplayEventName, events.WithValue(ev.Actions.Display)), nil) {
		return
	}
	continue
}
```
Add `SteerEventName = "aura.steer"` beside the other three, and one more branch in the same
shape keyed on `ev.Actions.SteerDelta`, slotted next to the artifact/display branches (before
the generic `StateDelta` branch, same precedence rule the existing comment documents at
`:139-150`). This is also where D-09's "visible line" (auto-delivery notice) and D-04's
Telegram redirect echo both eventually surface on the AG-UI/cockpit side — the SAME event
also needs to reach the Telegram channel path (which does NOT go through `Translate` for its
own reply text — see the Telegram section below).

### `internal/agui/server_run_steer.go` (new) — `POST /agent/runs/{runID}/steer`

**Analog:** `internal/agui/server_run_resume.go:87-103`, `handleRunCancel` — structurally
identical: resolve → side-effect → 202. This is the closest possible twin because D-02
explicitly ratifies keeping the route runID-shaped for parity with this exact handler's
`httpMutationRoutes` registration.
```go
func (s *Server) handleRunCancel(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.resolveRunSession(w, r)
	if !ok {
		return
	}
	if sess.cancel != nil {
		sess.cancel()
	}
	writeJSONStatus(w, http.StatusAccepted, map[string]string{"status": "cancelling"})
}
```
`handleRunSteer` differs only in the side effect: instead of `sess.cancel()`, decode the
steer text from the request body (size-capped, mirroring `normalizeHTTPMutation`'s
`maxRunBodyBytes` pattern at `internal/agui/idempotency_http.go:150-156`), then
`s.steer.Push(sess.ThreadID, text)` — **`sess.ThreadID` IS the conversation id** (confirmed:
`internal/agui/server_run.go:120` calls `s.run.Turn(ctx, in.ThreadID, ...)` and
`internal/runner/runner.go:556` sets `SessionID: convID` — `ThreadID` and `convID` are the
same value on this path), which is exactly D-01/D-02's contract: a runID-shaped route that
resolves to a conversation-id-keyed push.

**404-hiding ladder to reuse verbatim, not reinvent:** `internal/agui/server_run_resume.go:18-47`,
`resolveRunSession` — nil registry / malformed runID / registry miss / owner mismatch all
answer the identical 404 (MUSR-01/D-06 "hide existence, never 403"). `handleRunSteer` calls
this SAME method; it needs zero new resolution logic.

### `internal/agui/idempotency_http.go` — route registration

**Analog:** the existing `agent_run_cancel` row, `internal/agui/idempotency_http.go:59`:
```go
"POST /agent/runs/{runID}/cancel": httpMutationMeta("agent_run_cancel"),
```
Add directly beside it:
```go
"POST /agent/runs/{runID}/steer": httpMutationMeta("agent_run_steer"),
```
This is the ONE-LINE change that gives the steer route Idempotency-Key enforcement
identical to cancel (D-02's "Idempotency-Key required" contract) — no new code path, the
existing `idempotencyMutation` middleware wraps it exactly like every other row in the map.

### `internal/agui/server.go` — mux registration + setter

**Analog:** `internal/agui/server.go:344` (route) and `:247-252` (`SetRunRegistry`):
```go
mux.HandleFunc("POST /agent/runs/{runID}/cancel", s.handleRunCancel)
...
func (s *Server) SetRunRegistry(registry *RunRegistry) { s.runs = registry }
```
Add `mux.HandleFunc("POST /agent/runs/{runID}/steer", s.handleRunSteer)` beside the cancel
line, and a `SetSteerInbox(inbox *steer.Inbox) { s.steer = inbox }` setter beside
`SetRunRegistry`. Both are 1-2 line additions — this file has only 66 lines of headroom, so
resist the urge to inline any steer logic here.

### `internal/channels/telegram/bot_dispatch_turn.go` — the D-03/D-04/D-05 channel gate

**This file IS its own closest analog** — D-03 explicitly replaces its existing busy-copy
seam rather than introducing a parallel one. Current busy path,
`internal/channels/telegram/bot_dispatch_turn.go:94-101`:
```go
turnCtx, cancel := context.WithCancel(daemonCtx)
if !t.cmds.registerTurn(chatID, cancel) {
	cancel()
	if onBusy != nil {
		onBusy()
	}
	return
}
```
and the `onBusy` callback wired from `runTurnWithAssets` (line 37-38):
```go
t.startTurn(daemonCtx, sender, notifier, to, chatID, &text, inboundWasVoice,
	func() { t.reply(c, turnBusyMessage) })
```
and `sendBusy` (lines 144-151, also called from `bot_dispatch_hitl.go:189`):
```go
func (t *Telegram) sendBusy(sender botSender, chatID int64) {
	if sender == nil {
		return
	}
	if _, err := sender.Send(tele.ChatID(chatID), turnBusyMessage); err != nil {
		slog.Warn("telegram: busy reply send failed", "chat", chatID, "err", err)
	}
}
```
**The change:** `registerTurn` returning `false` means "a turn IS live for this chat" — that
is exactly the signal D-03 needs to route a PLAIN-TEXT message to `t.deps.Steer.Push(convID(chatID),
text)` instead of `onBusy()`, then reply with D-04's fixed line ("Redirected the active turn
with your correction.") instead of `turnBusyMessage`. D-05's non-text gate (photo/voice/document
still queues for next turn, i.e. keeps today's `onBusy` behavior) means the branch must run
BEFORE the `registerTurn` call reads `text`'s type/content, so the two `onBusy` call sites
(`bot_dispatch_turn.go:38` for the plain-text handler, `bot_dispatch_hitl.go:189` for the HITL
continuation path) need different treatment: `bot_dispatch_hitl.go`'s resume path is NOT a
plain-text-during-live-turn case (#132 item 7: `ask_user`-paused runs are terminal and NOT
steerable — CONTEXT.md's Interaction note), so `sendBusy` stays unchanged there; only the
`runTurnWithAssets` call site's `onBusy` changes.

### `internal/channels/telegram/commands.go` — busy-peek helper (if needed)

**Analog:** `internal/channels/telegram/commands.go:112-120`, `registerTurn`:
```go
func (c *commands) registerTurn(chatID int64, cancel context.CancelFunc) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, busy := c.cancels[chatID]; busy {
		return false
	}
	c.cancels[chatID] = cancel
	return true
}
```
`registerTurn`'s own `false` return already IS the "chat is busy" signal — no new peek method
is strictly required; the steer routing can live entirely in the `if !t.cmds.registerTurn(...)`
branch already shown above. Listed here only in case the planner decides the text/non-text
split (D-05) needs to happen BEFORE attempting registration (to avoid a wasted
register-then-unregister cycle for a message that will queue rather than steer).

### `internal/config/config_agui_steer.go` (new) — the three knobs

**Analog:** `internal/config/config_agui_run.go`, the WHOLE file (42 lines) — structurally
this is the closest possible twin, including the exact "default true is not dark code"
framing D-12 reapplies from D-11:
```go
type AGUIRunConfig struct {
	Detach          bool // AURA_AGUI_RUN_DETACH — decouple run lifetime from the HTTP fetch (default true, #90.1)
	BufferEvents    int  // AURA_AGUI_RUN_BUFFER_EVENTS — per-run replay ring capacity in events (default 2048)
	LingerSec       int  // AURA_AGUI_RUN_LINGER_SEC — terminal-session replay window before reaper eviction (default 180)
	MaxWallclockSec int  // AURA_AGUI_RUN_MAX_WALLCLOCK_SEC — outer detached-ctx bound over the agent Budget (default 3600)
	MaxLive         int  // AURA_AGUI_RUN_MAX_LIVE — live-run registry cap; Start past it answers 503 (default 16)
}

func loadAGUIRunConfig() AGUIRunConfig {
	return AGUIRunConfig{
		Detach:          envutil.BoolDefault("AURA_AGUI_RUN_DETACH", true),
		BufferEvents:    envutil.IntDefault("AURA_AGUI_RUN_BUFFER_EVENTS", 2048),
		LingerSec:       envutil.IntDefault("AURA_AGUI_RUN_LINGER_SEC", 180),
		MaxWallclockSec: envutil.IntDefault("AURA_AGUI_RUN_MAX_WALLCLOCK_SEC", 3600),
		MaxLive:         envutil.IntDefault("AURA_AGUI_RUN_MAX_LIVE", 16),
	}
}
```
New file:
```go
type AGUISteerConfig struct {
	Enabled  bool // AURA_AGUI_RUN_STEER — default true (D-12: off is dark code)
	Max      int  // AURA_AGUI_RUN_STEER_MAX — queued-steer cap per conversation
	MaxBytes int  // AURA_AGUI_RUN_STEER_MAX_BYTES — per-steer byte cap
}

func loadAGUISteerConfig() AGUISteerConfig {
	return AGUISteerConfig{
		Enabled:  envutil.BoolDefault("AURA_AGUI_RUN_STEER", true),
		Max:      envutil.IntDefault("AURA_AGUI_RUN_STEER_MAX", <value>),
		MaxBytes: envutil.IntDefault("AURA_AGUI_RUN_STEER_MAX_BYTES", <value>),
	}
}
```
(Exact defaults for Max/MaxBytes are Claude's Discretion per CONTEXT.md — not specified.)

### `internal/config/config_knobs.go` — catalog + the D-11 fix-on-touch

**Analog and the bug to fix in the SAME pass**, `internal/config/config_knobs.go:114-120`:
```go
{Name: "AURA_AGUI_BUFFER_CAP", Kind: KindInt, Default: "64"},
{Name: "AURA_AGUI_SSE_HEARTBEAT_SEC", Kind: KindInt, Default: "15"},
{Name: "AURA_AGUI_RUN_DETACH", Kind: KindBool, Default: "false"},   // ← WRONG, code default is true (D-11)
{Name: "AURA_AGUI_RUN_BUFFER_EVENTS", Kind: KindInt, Default: "2048"},
{Name: "AURA_AGUI_RUN_LINGER_SEC", Kind: KindInt, Default: "180"},
{Name: "AURA_AGUI_RUN_MAX_WALLCLOCK_SEC", Kind: KindInt, Default: "3600"},
{Name: "AURA_AGUI_RUN_MAX_LIVE", Kind: KindInt, Default: "16"},
```
Two changes in this one map literal: (1) correct line 116's `Default: "false"` to
`Default: "true"` (D-11, confirmed by `config_agui_run.go:36`'s actual `BoolDefault(...,
true)` and by `config_agui_test.go:28-30`'s own passing assertion that the loaded value IS
true — the catalog row is the one place still lying); (2) add three new rows for
`AURA_AGUI_RUN_STEER` (`KindBool`, `Default: "true"`), `AURA_AGUI_RUN_STEER_MAX` (`KindInt`),
`AURA_AGUI_RUN_STEER_MAX_BYTES` (`KindInt`) directly after line 120.

### `internal/config/config_agui_test.go` — test pattern to clone

**Analog:** the whole file (88 lines) — `TestAGUIConfigDefaultsAndOverrides` already proves
the exact three-phase pattern (default → override → malformed-fallback) this phase's new
knobs need:
```go
if !cfg.AGUIRun.Detach {
	t.Error("AGUIRun.Detach default = false, want true (amendment #90.1 flip)")
}
...
t.Setenv("AURA_AGUI_RUN_DETACH", "false")
...
if cfg.AGUIRun.Detach {
	t.Error("AGUIRun.Detach override = true, want false (explicit rollback wins)")
}
...
t.Setenv("AURA_AGUI_RUN_DETACH", "not-a-bool")
...
if !cfg.AGUIRun.Detach {
	t.Error("malformed AURA_AGUI_RUN_DETACH must fall back to the default (true, #90.1)")
}
```
Clone this triple for `AGUISteer.Enabled`/`Max`/`MaxBytes`, either appended to this file (88/600,
plenty of room) or as a new `config_agui_steer_test.go` mirroring the whole-file-clone
convention `config_agui_test.go` itself was split out under (per its own header comment,
lines 5-8: "moved out of config_test.go ... so that file stays under the 600-LOC cap").

### `internal/agui/server_project.go` — the folded approval-resume defects

**Analog:** the file's own `resumeAnswers`/`payloadString`, `internal/agui/server_project.go:21-52`:
```go
func resumeAnswers(entries []types.ResumeEntry) map[string]runner.ResponseInput {
	out := make(map[string]runner.ResponseInput, len(entries))
	for _, e := range entries {
		action := askuser.ActionAccept
		if e.Status == types.ResumeStatusCancelled {
			action = askuser.ActionCancel
		}
		out[e.InterruptID] = runner.ResponseInput{Action: action, Content: payloadString(e.Payload)}
	}
	return out
}

func payloadString(payload any) string {
	switch v := payload.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}
```
Folded defect #1 (no per-tool decision policy — accept on a decline-only pause) and #2 (empty
answer resumes silently) both live in this exact function pair. LibreChat's reference
behavior is 403 / 400 respectively (CONTEXT.md); the Aura shape is NOT a second validation
layer bolted on afterward — the constraint carried from the todo is explicit: *"New
validation goes inside that transaction's front door, never as a second path around it."*
The transaction's front door is `MarkResumedTx`/`MarkResumedBatchTx`
(`internal/askuser/store.go:325-342`, `:382-...`), and the cross-store claim is
`CommitResumeBatch` (`internal/runner/resume_committer.go:68`,
`internal/runner/runner_resume.go:97,176-177`) — the idempotency-key invariant to preserve
verbatim:
```go
func (s *Store) MarkResumedTx(ctx context.Context, q *sqlc.Queries, token string, ans ResumeAnswer) error {
	...
	n, err := q.MarkPausedStateResumed(ctx, sqlc.MarkPausedStateResumedParams{Token: id, ResumedAnswer: answer})
	if err != nil {
		return fmt.Errorf("mark resumed %s: %w", token, err)
	}
	if n == 0 {
		return fmt.Errorf("mark resumed %s: %w", token, ErrPauseNotFound)
	}
	return nil
}
```
The `n == 0` (`RowsAffected==0`) check IS the idempotency key (WHERE `resumed_at IS NULL`) —
any new per-tool-policy or empty-answer check must reject BEFORE this call is reached (so an
invalid resume never consumes the conditional-update slot a legitimate later retry would
need), not as a second check after.

### `internal/askuser` — pending-approval TTL (folded defect #3)

**No explicit target file named in CONTEXT.md.** Given `internal/askuser/store.go` is at
513/600 (87 lines of headroom — tight for a ticker+reaper addition plus its own tests), the
closest STRUCTURAL analog for the reaper itself is `internal/agui/runregistry.go:186-226`,
the linger-reaper:
```go
func (r *RunRegistry) startReaperLocked() {
	if r.reaperDone != nil {
		return
	}
	interval := r.cfg.linger / 2
	if interval <= 0 {
		interval = time.Second
	}
	r.stop = make(chan struct{})
	r.reaperDone = make(chan struct{})
	go func(stop <-chan struct{}, done chan<- struct{}) {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				r.evictExpired()
			}
		}
	}(r.stop, r.reaperDone)
}

func (r *RunRegistry) evictExpired() {
	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, sess := range r.byRun {
		if terminal, at := sess.terminalState(); terminal && now.Sub(at) > r.cfg.linger {
			delete(r.byRun, id)
		}
	}
}
```
This is the proven "TTL by ticker, goleak-clean, joined on Close" shape already in this
codebase — a `paused_states` TTL reaper (LibreChat's `APPROVAL_EXPIRED_ERROR`/
`expireApproval()` reference) should copy this SAME lazy-start-on-first-use +
`stop`/`reaperDone` channel pair rather than inventing a new goroutine-lifecycle idiom.
Land it as a new sibling file (`internal/askuser/expire.go`), not appended to `store.go`.

### `internal/runner/runner.go` + `internal/runner/runner_steer.go` (new) — wiring

**Analog for the "context-key threading, not a struct field" alternative:**
`internal/runner/runner_reasoning.go:27-33`:
```go
func WithReasoningOverride(ctx context.Context, effort llm.ReasoningEffort) context.Context { ... }
func reasoningOverride(ctx context.Context) (llm.ReasoningEffort, bool) { ... }
```
**Analog for the struct-field + `buildAgent` passthrough alternative:**
`internal/runner/runner.go:162-177` (the `Runner` struct's `breaker *llm.Breaker` /
`gateway`-shaped fields — search the struct for the shared-singleton convention) and
`:550-568` (`buildAgent`'s `LlmAgentConfig{...}` literal):
```go
la := agent.NewLlmAgent(agent.LlmAgentConfig{
	Client:     r.client,
	...
	Breaker:    r.breaker,    // shared process-lifetime breaker (B-05)
	Gateway:           r.gateway,
	...
})
```
Given `runner.go` has only 23 lines of headroom, the RIGHT split is: one field added to the
`Runner` struct literal block (1 line), one line added inside the existing `LlmAgentConfig{}`
literal in `buildAgent` (1 line: `Steer: r.steerInbox,` mirroring `Gateway: r.gateway,`), and
the actual inbox-lookup-by-convID logic (if any beyond a direct field pass) goes in a NEW
`runner_steer.go` sibling file mirroring `runner_reasoning.go`'s file-per-concern precedent.
`convID` is already in scope at `buildAgent`'s call site (`internal/runner/runner.go:535`,
the function's own second parameter) — it is the SAME string as `SessionID: convID` two lines
below (`:556`), which is exactly the key `internal/steer.Inbox` needs (D-01).

### `cmd/aura/serve_agui.go` — composition-root wiring

**Analog:** `internal/agui`'s own `RunRegistry` construction,
`cmd/aura/serve_agui.go:74-83`:
```go
var runRegistry *agui.RunRegistry
if chat.cfg.AGUIRun.Detach {
	runRegistry = agui.NewRunRegistry(serverCfg)
	aguiServer.SetRunRegistry(runRegistry)
}
```
The steer inbox needs a SHARED singleton (not agui-owned) because Telegram pushes into it
too — construct it once, higher in the composition root (wherever `chat.pool`/`chat.assets`
are built, likely `cmd/aura/serve.go` or `chatEnv` construction), then wire it into agui with
the identical `if chat.cfg.AGUISteer.Enabled { aguiServer.SetSteerInbox(chat.steer) }` shape,
and into Telegram via `buildTelegramDeps`.

### `cmd/aura/serve_channels.go` — Telegram-side wiring

**Analog:** `cmd/aura/serve_channels.go:78-98`, `buildTelegramDeps` — the exact convention
for injecting a shared backend into the channel:
```go
func buildTelegramDeps(chat *chatEnv, tgCfg telegram.Config) telegram.Deps {
	return telegram.Deps{
		Turn:               ensuringTurn(chat.run),
		...
		Assets:             chat.assets,
		Search:             chat.conv,
		...
	}
}
```
and the `Deps` struct's own field-by-field doc convention,
`internal/channels/telegram/bot.go:66-96` (each field documents what a nil value degrades
to — e.g. `Assets` at `:91-94`: *"nil means attachments cannot be accepted at all"*). Add a
`Steer *steer.Inbox` field to `telegram.Deps` with the SAME nil-degrades-gracefully doc
comment (nil ⇒ D-03's redirect branch never fires, `sendBusy` stays the fallback — the
`AURA_AGUI_RUN_STEER=false` rollback path), and pass `Steer: chat.steer` in
`buildTelegramDeps`.

## Shared Patterns

### Additive `Actions` field (never breaks the wire)
**Source:** `internal/agent/event.go:69-97`
**Apply to:** `event.go` (new `SteerDelta` field), any Event constructor that needs to carry
the D-09 visible line or the `aura.steer` echo payload.
```go
ArtifactDelta map[string]any `json:"artifact_delta,omitempty"`
```
Every additive field in this struct is `omitempty` + untyped `map[string]any` when the shape
is a forward-compat wire payload rather than a persisted DB row — `SteerDelta` should follow
the SAME rule, not `Display`'s typed-pointer convention (that one has a stable Go type,
`display.Payload`, because it feeds a persisted-shape normalizer; a steer echo does not).

### CUSTOM AG-UI frame for anything the SDK's built-in events cannot carry
**Source:** `internal/agui/translator.go:19-35, 142-166`
**Apply to:** `translator.go` (`aura.steer` branch), any cockpit/channel consumer that needs
to render the redirect notice.
```go
const ArtifactEventName = "aura.artifact"
...
if !yield(events.NewCustomEvent(artifactEventName, events.WithValue(ev.Actions.ArtifactDelta)), nil) {
	return false
}
```

### Owner-scoped 404-hiding resolution ladder
**Source:** `internal/agui/server_run_resume.go:18-47`
**Apply to:** `server_run_steer.go` (reuses `resolveRunSession` verbatim — no new resolution
logic needed at all).

### `httpMutationRoutes` one-line registration
**Source:** `internal/agui/idempotency_http.go:53-122`
**Apply to:** the new steer route — copy the `agent_run_cancel` row exactly, changing only
the path suffix and the normalizer name.

### Config sub-struct + non-fatal envutil fallback + malformed-value-falls-back test triple
**Source:** `internal/config/config_agui_run.go` (whole file) +
`internal/config/config_agui_test.go` (whole file)
**Apply to:** `config_agui_steer.go` (new) + its test clone.

### Mutex-guarded map keyed registry with lazy-start ticker reaper
**Source:** `internal/agui/runregistry.go` (whole file)
**Apply to:** `internal/steer`'s inbox (the map half) and `internal/askuser`'s new TTL reaper
(the ticker half) — two DIFFERENT phase deliverables reusing the SAME proven shape from the
SAME donor file.

### Shared-singleton composition-root injection (`chat.X` → both agui and Telegram)
**Source:** `cmd/aura/serve_agui.go:109-112` (`aguiServer.SetApprovalStore(chat.pause)`,
`SetApprovalGrantStore(...)`) + `cmd/aura/serve_channels.go:78-98` (`buildTelegramDeps`)
**Apply to:** wiring the ONE process-wide `*steer.Inbox` into both `agui.Server` and
`telegram.Deps` — this is the established pattern for "one backend, two consumers," already
proven for `chat.pause` (askuser.Store), `chat.assets`, `chat.conv`.

## No Analog Found

**One, and it is a build artifact rather than a shape:** `internal/webui/dist`. It is generated by
`docker webbuild` on Linux node-24 and guarded by the `web-dist-freshness` CI gate; a Windows-built
bundle re-hashes chunks and fails that gate. There is nothing to pattern-match — the instruction is
"do not hand-edit it, rebuild it the sanctioned way".

Every other production path in the phase's scope has at least a role-match analog in the existing
codebase. `internal/steer` is net-new but is explicitly a composite of two files already read above
(`runregistry.go` + `runsession.go`), not an invented shape; the same holds for
`internal/channels/telegram/bot_dispatch_queue.go`, whose registry shape is `runregistry.go`'s and
whose announce-out-loud behaviour mirrors 52-05's leftover auto-delivery.

**The previous revision of this document said "None" while omitting the entire web layer and nine
Go/SQL paths from its table.** That is recorded here rather than silently corrected: a completeness
claim nobody measured is worse than an admitted gap, because the next reader trusts it.

## Metadata

**Analog search scope:** `internal/agui/`, `internal/agent/` (incl. all `llm_agent_*.go`
siblings), `internal/runner/`, `internal/askuser/`, `internal/conversations/`,
`internal/channels/telegram/`, `internal/config/`, `cmd/aura/`.
**Files scanned/read in full or targeted ranges:** `runregistry.go`, `runsession.go`,
`server_run_detach.go`, `server_run_resume.go`, `server_run.go`, `idempotency_http.go`,
`translator.go`, `server_project.go`, `server.go` (partial), `llm_agent.go`,
`llm_agent_finalize.go`, `llm_agent_dispatch.go`, `llm_agent_round.go`, `llm_agent_events.go`,
`llm_agent_construct.go`, `model_round.go`, `trust.go`, `event.go` (partial),
`bot_dispatch_turn.go`, `commands.go` (partial), `bot.go` (partial), `serve_agui.go`,
`serve_channels.go` (partial), `runner.go` (partial), `runner_persist.go`,
`store_append.go` (`internal/conversations`), `store.go` (`internal/askuser`, partial),
`config_agui_run.go`, `config_agui_test.go`, `config_knobs.go` (partial).
**Pattern extraction date:** 2026-08-25
