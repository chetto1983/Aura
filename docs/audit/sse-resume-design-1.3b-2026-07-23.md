# SSE stream resilience — fix-plan 1.3 Tier B design (AG-UI cockpit gateway)

**Status:** DESIGN ONLY — deferred from implementation because it amends the AG-UI gateway
contract (PRD-first discipline). A future session writes the PRD amendment and the
implementation plan directly from this document.
**Date:** 2026-07-23
**Scope:** `internal/agui` (gateway), `internal/config` (knobs), `web/src/chat` (client).
Runner (`internal/runner`) is touched only at one call-convention seam; the agent core,
translator, Fanout, and Telegram paths are unchanged.
**Baseline commits referenced:** heartbeat `a93648c5` (Tier A), current `master` @ `b0edba5d`.

---

## Executive summary

Today a cockpit agent run lives and dies with a single HTTP fetch: `handleRun`
(internal/agui/server.go:423) drives `Runner.Turn` with the **request context**, so a client
disconnect (tab close, Wi-Fi blip, proxy timeout) cancels the run mid-turn, loses all streamed
frames, and releases the thread lock via the handler `defer unlock()`. There is no way to
reattach.

The chosen design introduces a small in-memory **RunRegistry** of **RunSession** objects inside
`internal/agui`. `handleRun` keeps ALL of its synchronous pre-work unchanged (validation,
owner-scoped 404s, effort governance, pinned-skill injection, `SubmitAnswers`), then — behind a
default-off env flag `AURA_AGUI_RUN_DETACH` — hands the translated event stream to a
**detached producer goroutine** running on a `context.WithoutCancel`-derived, wallclock-bounded
context. The producer appends every translated (and pre-redacted) event into a **per-run ring
buffer with monotonically increasing sequence numbers**, fans out to live subscribers with the
existing drop-on-full / lifecycle-never-dropped discipline, and releases the thread lock when
the turn completes (not when the HTTP handler returns). The original HTTP response becomes
subscriber #0. Every SSE frame now carries an `id: <seq>` line.

A new **read-only, authenticated, owner-scoped** endpoint `GET /agent/runs/{runID}/events`
replays the buffer from `Last-Event-ID + 1` and continues live; a replay gap (ring overflow /
evicted session) returns `410 Gone` and the client falls back to the existing
`MESSAGES_SNAPSHOT`. Because disconnect no longer cancels, a new mutation route
`POST /agent/runs/{runID}/cancel` restores the explicit Stop affordance. The web client gains a
bounded-retry reconnect loop keyed on the `runId` it already receives in `RUN_STARTED`, and
live-run discovery after a page reload via an additive `live_run_id` field on the conversation
DTO. Everything is in-memory: **no migration**, no durable event log; a daemon restart loses
resumability, and the client degrades to the snapshot — exactly today's behavior.

---

## 1. Run-lifetime decoupling

### 1.1 Context derivation

The detached run context is built in `handleRun` **after** all synchronous gates have passed
(so every 4xx path is unchanged) and **from the fully-decorated request context** — the order
matters because the ctx at that point already carries, as values:

- the authenticated principal (`identityctx` / `principalKey`, set by `RequireAuth` →
  `withPrincipal`, auth.go:307),
- the idempotency operation (`idempotency.WithOperation`, idempotency_http.go:206),
- the validated fixed reasoning-effort override (`runner.WithReasoningOverride`, threaded by
  `applyReasoningEffort`, server_reasoning_effort.go:58),
- `runner.WithThreadLockHeld` (server.go:499).

```go
// server_run_detach.go (new file)
func detachedRunContext(reqCtx context.Context, maxWallclock time.Duration) (context.Context, context.CancelFunc) {
    // Values survive, cancellation does not: the client's disconnect must never
    // reach the agent turn (golang-context: WithoutCancel for background work
    // outliving requests). All four ctx values above are value-keyed, so they ride.
    ctx := context.WithoutCancel(reqCtx)
    // Belt-and-suspenders wallclock bound. The agent Budget (runner.buildAgent →
    // bud.WithDeadline) already bounds every turn from AURA_LOOP_*; this outer cap
    // only exists so a mis-configured budget can never leak a goroutine forever.
    return context.WithTimeout(ctx, maxWallclock)
}
```

**What bounds the run:** three nested bounds, tightest wins —
1. the agent Budget deadline (existing, from `AURA_LOOP_*`, applied inside
   `runner.buildAgent`),
2. the new outer wallclock cap `AURA_AGUI_RUN_MAX_WALLCLOCK_SEC` (default **3600**),
3. explicit cancellation: the session's `cancel` func, fired by (a) the new cancel endpoint,
   (b) the conversation-delete lifecycle (unchanged — `Runner.cancelSession` cancels the
   *turn* ctx registered by `trackSession`, which descends from ours), (c) daemon shutdown
   (registry `Close()` walks all sessions).

**Changed invariant:** "client disconnect cancels the run mid-turn" is deliberately removed
when the flag is on. This is safe because every run remains bounded by the Budget deadline and
the wallclock cap, and the delete-lifecycle / shutdown cancels still work — they never depended
on the HTTP request ctx (they go through `trackSession`'s registered cancel keyed by
(identity, session), runner_session.go:86).

### 1.2 Thread-lock lifetime

Today: `TryLockThread` → `defer unlock()` in `handleRun` (server.go:490-499). The lock is held
for exactly the HTTP request lifetime, which (pre-detach) equals the turn lifetime because the
handler blocks in `streamSSE` until the turn drains.

Design: the handler still calls `TryLockThread` (the 409 `ErrThreadBusy` fast-path is
unchanged), but on the detached path **ownership of `unlock` transfers to the producer
goroutine**, which calls it when the translated stream is fully drained into the ring:

```go
unlock, locked := locker.TryLockThread(ctx, in.ThreadID)
if !locked { http.Error(w, runner.ErrThreadBusy.Error(), http.StatusConflict); return }
// NO defer here on the detached path — the producer owns the release.
sess := s.runs.Start(runParams{ ..., unlock: unlock })   // producer: defer unlock()
```

The lock-release point is **semantically identical** to today (turn end = stream drained = lock
free); only the *carrier* changes from handler-defer to producer-defer. What DOES change
observably: a detached run whose client vanished keeps the lock until the turn finishes, so a
second `POST /agent/run` on that thread gets 409 for the run's true duration instead of being
able to sneak in after the disconnect-cancel. That is the correct new contract — the cockpit
shows/attaches the live run instead of starting a competing one (§4).

**HITL pause does NOT hold the lock.** A pause is not a suspended goroutine: the translator
yields `RUN_FINISHED(interrupt)` and returns (translator.go:79-86), `runner.turnLocked` flushes
the pause turn and completes (runner.go:439-465), the producer drains, and unlocks. A paused
run's RunSession transitions to terminal exactly like a success. Resume remains what it is
today: a NEW `POST /agent/run` with `Resume[]` entries → a new runID, new session, new lock
acquisition. No change to the pause store (`askuser`), `SubmitAnswers`, or the approvals
center.

### 1.3 Abandoned runs — reaper + linger TTL

A run nobody is watching simply completes on its own (that is the feature). The RunSession then
**lingers** in the registry for `AURA_AGUI_RUN_LINGER_SEC` (default **180**) so a late resume
can still replay the tail + `RUN_FINISHED`, after which a **reaper** evicts it:

- One registry-owned reaper goroutine, `time.Ticker` at `linger/2` granularity, started
  lazily on first `Start()` and joined by `Close()` (goleak-clean).
- Eviction rule: `terminal && now-finishedAt > linger`. Non-terminal sessions are never
  reaped — they are bounded by the wallclock ctx, whose expiry makes them terminal
  (`RUN_ERROR` from the ctx-cancelled turn) and starts their linger clock.
- After eviction a resume gets 404 → client falls back to `MESSAGES_SNAPSHOT`
  (`/api/conversations/{id}/messages` via existing `fetchThreadMessages`), which by then holds
  the persisted turn (turn persistence at turn end is untouched).

### 1.4 Effort-override and other ctx values across the detach

Nothing extra to do — this is exactly why `context.WithoutCancel` (not
`context.Background()`) is the derivation base: `runner.buildAgent` reads the override via
`reasoningOverride(ctx)` (runner_reasoning.go:33) from the turn ctx, which descends from the
detached ctx, which retains all values of the request ctx. Same for the principal (needed by
`scopeContextToConversation` and the (identity, session) lock keying) and the idempotency
operation (needed by mutating-tool child operations inside the turn).

---

## 2. Event buffering + IDs

### 2.1 RunSession + ring buffer (new file `internal/agui/runsession.go`)

```go
// seqEvent is one translated, ALREADY-REDACTED AG-UI event with its wire id.
type seqEvent struct {
    Seq int64        // 1-based, strictly monotonic per run — the SSE `id:` value
    Ev  events.Event // post-Translate, post-redactEvent (see §6.3)
}

type RunSession struct {
    RunID      string
    ThreadID   string
    IdentityID string    // owner captured at start (scopedIdentityID) — resume gate, §3.2
    mu         sync.Mutex
    ring       []seqEvent // fixed-cap ring, cap = AURA_AGUI_RUN_BUFFER_EVENTS
    head       int        // ring write index
    nextSeq    int64      // next seq to assign
    firstSeq   int64      // oldest seq still in the ring (gap detection)
    subs       map[int]chan seqEvent // live subscribers (cap = ServerConfig.BufferCap)
    terminal   bool
    finishedAt time.Time
    cancel     context.CancelFunc // wallclock/explicit-cancel handle
}
```

- `append(ev)` (producer-only): assign `Seq`, write into the ring (overwriting the oldest and
  advancing `firstSeq` when full), then fan to `subs` under the session mutex with the
  **existing** pump discipline — non-lifecycle frames drop-on-full with a WARN +
  `recordSSEDropped()`, lifecycle frames (per the existing `isLifecycleFrame`,
  server_sse.go:128) block-until-delivered-or-ctx-done. `pumpSend`'s policy is extracted into
  a shared helper both paths call; no second copy of the classification table.
- `subscribeFrom(fromSeq)` (any goroutine): under the session mutex, snapshot every ring entry
  with `Seq > fromSeq` into the new subscriber channel first, then register the channel.
  Because `append` takes the same mutex, **replay-then-live is gapless and duplicate-free by
  construction** — there is no window between the snapshot and registration.
  Returns `(ch, ok)`; `ok=false` when `fromSeq+1 < firstSeq` (replay gap → 410, §3.3).
- Terminal: the producer marks `terminal`, stamps `finishedAt`, closes all subscriber
  channels (sole sender closes — same principle as `Fanout.closeAll`), and calls `unlock()`.

### 2.2 Why extend, not reuse, Fanout

`Fanout` (fanout.go) is deliberately **subscribe-before-Run** — `Subscribe` after `Run`
panics (WR-06), because its producer snapshots `f.subs` once and has no history. Resume is
definitionally subscribe-AFTER-run-started + replay-from-history, i.e. the exact contract
Fanout forbids. Retrofitting a ring into Fanout would (a) change a load-bearing invariant the
Telegram channel and `client.go Subscribe` rely on, (b) add buffer memory to every Telegram
turn that never needs it. So: **RunSession is a sibling, not a replacement**; it reuses
`isLifecycleFrame`, the drop policy (extracted helper), `redactEvent`, and `recordSSEDropped`.
Fanout, `client.go`, and `agui_subscriber.go` are byte-untouched. The existing
`streamSSE`/`pumpSend` stay as-is for the flag-off path.

### 2.3 The `id:` field on the wire

The SDK's `sse.SSEWriter.WriteEventWithType` emits only `event:` + `data:` and cannot emit an
`id:` field (verified: `pkg/encoding/sse/writer.go`). Per the SSE spec, any field line written
before the terminating blank line belongs to the same event block — so the drain loop writes
the id line itself, immediately before delegating the rest of the frame to the SDK writer:

```go
// drainSession (server_run_detach.go) — the resume-capable twin of streamSSE's drain loop.
fmt.Fprintf(w, "id: %d\n", sev.Seq)                     // same block as the SDK's event/data
err := writer.WriteEventWithType(ctx, w, sev.Ev, string(sev.Ev.Type()))
```

The heartbeat comment (`:hb\n\n`, Tier A) is written **without** an id line — comments must
not advance the client's `Last-Event-ID` (and the cockpit parser already skips `:` lines).
The single-writer invariant of the drain loop (heartbeat can only land BETWEEN frames, never
inside one — server_sse.go:63-92) is preserved: the id write and the SDK write happen in the
same `case` arm of the same select, so they are one atomic unit relative to the heartbeat arm.
The flag-off path emits no `id:` lines (byte-identical wire to today); the flag-on path's id
line is additive and invisible to any parser that ignores unknown fields (the cockpit's
`parseSSEBlock` today reads only `data:` lines — it gains an `id` reader in §4.1).

### 2.4 Memory bounds

- Ring cap: `AURA_AGUI_RUN_BUFFER_EVENTS` (default **2048** events). Translated frames are
  small deltas (tens to hundreds of bytes; a `TOOL_CALL_RESULT` preview is already capped by
  the runner's `PreviewCap`). Budget ≈ 2048 × ~512 B ≈ **1 MiB per run** worst-case.
- Live-run cap: `AURA_AGUI_RUN_MAX_LIVE` (default **16**). `RunRegistry.Start` refuses past
  the cap with `503 + Retry-After` (before the thread lock is taken). Concurrency is already
  naturally throttled — one live run per (identity, thread) via the thread lock — so 16 is
  generous for the single-operator cockpit. Worst-case registry memory ≈ 16 MiB + linger tail.
- Eviction: on terminal + linger expiry (§1.3), the reaper drops the session (ring included)
  and deletes the thread-index entry.

### 2.5 RunRegistry (new file `internal/agui/runregistry.go`)

```go
type RunRegistry struct {
    mu       sync.Mutex
    byRun    map[string]*RunSession               // runID → session
    byThread map[threadKey]*RunSession            // (identityID, threadID) → LIVE session (live-run discovery, §4.2)
    cfg      runRegistryConfig                    // caps + linger + wallclock
    // reaper plumbing: lazily-started goroutine + done chan, Close() joins it.
}
type threadKey struct{ identity, thread string }
```

`byThread` holds only non-terminal sessions (the producer removes its entry on terminal —
the linger applies to `byRun` only). Wired into `Server` via `SetRunRegistry` (same
narrow-seam convention as `SetApprovalStore`), constructed in the daemon composition root
(`cmd/aura/serve_webui.go` neighborhood) only when `AURA_AGUI_RUN_DETACH=true`; a nil registry
= flag off = today's code path, so all existing `NewServer` callers/tests are untouched
(D-A2-02 discipline).

---

## 3. Resume contract

### 3.1 Routes (registered in `Server.Mux`, colocated per package convention)

```go
mux.HandleFunc("GET /agent/runs/{runID}/events", s.handleRunEvents)   // read-only SSE resume
mux.HandleFunc("POST /agent/runs/{runID}/cancel", s.handleRunCancel)  // explicit Stop (mutation)
// httpMutationRoutes (idempotency_http.go) gains:
//   "POST /agent/runs/{runID}/cancel": httpMutationMeta("agent_run_cancel"),
// GET /agent/runs/{runID}/events is deliberately NOT in the inventory: it is read-only
// (mutation_coverage_test.go asserts the inventory; the review note documents the exclusion
// the same way graph query / TTS / STT are excluded).
```

Both live inside the agui mux, so they inherit the parent-mount `RequireAuth` whole-origin
gate and `withCORS` exactly like `/agent/run` — zero new wiring in `cmd/aura/serve_webui.go`
beyond the `SetRunRegistry` call.

### 3.2 Identity-scoped 404 semantics (parity with handleRun)

`handleRunEvents` resolution ladder, mirroring server.go:440-453:

1. `runID` fails the `"run-" + uuid` shape check → **404 "run not found"** (parity with the
   malformed-thread-id 404-before-lookup chokepoint, T-12-11).
2. Registry miss (never existed, or reaped) → **404** — same body, hide existence.
3. Registry hit but `sess.IdentityID != scopedIdentityID(ctx)` → **404** — a foreign
   principal must not learn the run exists (MUSR-01/D-06 discipline; same reason foreign
   threads 404, never 403).
4. `Last-Event-ID` present but unparsable (non-integer / negative) → **400**.
5. Replay gap (`ok=false` from `subscribeFrom`) → **410 Gone**, JSON body
   `{"error":"replay window exceeded"}` — a *distinct* signal from 404 so the client knows
   the run may still be live but partial-frame recovery is impossible (§4.3).

### 3.3 Last-Event-ID semantics

- Header **`Last-Event-ID: <n>`** → replay every buffered event with `Seq > n`, then continue
  live. Header only — the client is fetch-based (it can set headers; no `EventSource`
  limitation applies), and a query-param alias would just be a second spelling to test.
- **Absent header** → `fromSeq = 0`: replay the whole buffer from the beginning. This is the
  page-reload attach path (§4.2) — the client has nothing and wants the full in-flight
  partial turn. If seq 1 has already rotated out of the ring, that too is a replay gap → 410.
- **Run already terminal, still lingering** → replay the requested tail (which necessarily
  ends in `RUN_FINISHED`/`RUN_ERROR`), then close the response cleanly. No hanging, no
  heartbeat wait: the subscriber channel is already closed, the drain loop exits after the
  replayed frames.
- **Duplicate-delivery contract:** replay starts strictly at `n+1`; the server never re-sends
  an id the client acknowledged. Client-side reducer idempotence is therefore a safety net,
  not the mechanism (§4.1).
- The resumed stream uses the same drain loop (`drainSession`), so it carries the Tier A
  heartbeat and the same `id:` discipline as the original stream.

### 3.4 CORS / auth parity

Nothing bespoke: both new routes sit inside the same mux behind the same `RequireAuth` mount
and the same `withCORS` wrapper as every other agui route. `ServerConfig` gains nothing for
this; the registry knobs arrive via new fields (`RunDetach bool`, `RunBufferEvents`,
`RunLingerSec`, `RunMaxWallclockSec`, `RunMaxLive int`) resolved from config like
`SSEHeartbeatSec` is today.

---

## 4. Client changes (`web/src/chat`)

### 4.1 sseAdapter: id-aware parsing + reconnect loop

- `parseSSEBlock` gains `id:` extraction and returns `{ frame, id? }`; `readSSEFrames` yields
  the pair. The reducer (`reduceFrame`) is untouched — it stays a pure fold; idempotence on
  *lifecycle* frames already holds (`ensureText`/`ensureTool` are get-or-create;
  `TEXT_MESSAGE_END`, `RUN_FINISHED` are assignments), and content deltas are never replayed
  because the server's `n+1` contract (§3.3) is exact. A vitest property test pins this:
  folding `frames` equals folding `frames[0..k] + resume(frames[k+1..])` for every split k.
- New `web/src/chat/sseResume.ts` (keeps sseAdapter.ts under its 600-LOC cap):

```ts
export interface ResilientRunOptions extends StreamRunOptions {
  readonly maxRetries?: number;      // default 5
  readonly backoffBaseMs?: number;   // default 500, exponential, cap 10_000, ±25% jitter
}
// streamRunResilient:
// 1. POST /agent/run (streamRun's request builder, unchanged headers/body).
// 2. Track lastEventId from `id:` lines and runId from the RUN_STARTED frame.
// 3. On a mid-stream network error (fetch TypeError / reader error) — NOT on
//    AbortSignal (user Stop) and NOT on a clean RUN_FINISHED/RUN_ERROR frame —
//    reconnect: GET /agent/runs/{runId}/events with Last-Event-ID: lastEventId,
//    same-origin credentials, bounded retries + backoff, folding into the SAME
//    AssistantTurnState (state survives across attempts — it lives in the wrapper).
// 4. Terminal conditions: RUN_FINISHED/RUN_ERROR frame (done), retries exhausted /
//    404 / 410 (fallback, §4.3), abort (user stop → POST cancel, §4.4).
```

- The pre-first-byte POST failure path is idempotency territory, not reconnect territory —
  see §5.2.

### 4.2 Page-reload attach — threading the live run through conversation state

Additive DTO field: `GET /api/conversations/{id}` (and the list rows) gains
`live_run_id?: string`, served by the conversation handler consulting
`RunRegistry.byThread[{scopedIdentityID(ctx), id}]` — in-memory read, nil registry (flag off)
→ field simply absent, so the DTO is byte-identical when disabled. On thread open,
`ExternalStoreChat` (which already fetches the conversation + `fetchThreadMessages`):

1. Renders the snapshot as today.
2. If `live_run_id` is set → append a fresh running assistant message and attach via
   `GET /agent/runs/{id}/events` with **no** `Last-Event-ID` (full-buffer replay rebuilds the
   in-flight partial turn — text so far, open tool cards, reasoning drawer), then continue
   live through the same reducer. The runtime learns the turn is live purely from the
   folded `status: running` — no assistant-ui runtime API change needed; this is the same
   ExternalStore `setMessages` path the live stream already drives.
3. The composer's send affordance respects the existing 409 `ErrThreadBusy` contract — while
   `live_run_id` is set, sending is disabled with a "run in corso" hint (today the user would
   get a raw 409; this is strictly better UX for the widened lock window of §1.2).

### 4.3 Failure UX when resume is impossible

Retries exhausted, or resume returned 404/410 → the wrapper: (a) marks the assistant message
`{ type: 'incomplete', reason: 'error' }` with a short "connessione persa" note, (b) refetches
`MESSAGES_SNAPSHOT` via the existing `fetchThreadMessages` — if the run finished server-side
while we were disconnected, the persisted turn replaces the partial; if it is still running,
the snapshot shows the pre-run state and the user can reopen the thread later (live_run_id
re-discovery). No data is lost that the persistence layer had; the only loss is the live
partial view — the same loss profile as today's total-loss, strictly narrowed.

### 4.4 Stop affordance

Today Stop = `AbortController.abort()` → request ctx cancel → run dies. With detach, abort
only detaches the viewer. The wrapper therefore maps the user's Stop to:
`abort()` (stop rendering) **then** `POST /agent/runs/{runId}/cancel` (the fetch wrapper
`installMutationIdempotency` auto-attaches the Idempotency-Key). Server side,
`handleRunCancel` resolves the session with the §3.2 ladder (404 semantics), then fires
`sess.cancel()` — which cancels the detached ctx → the turn ctx → the agent unwinds exactly
as a disconnect-cancel does today (the translator maps the error to `RUN_ERROR`, buffered as
the terminal frame). Response: `202 {"status":"cancelling"}` (the turn's unwind is async);
cancelling an already-terminal run is a 202 no-op (idempotent by nature).

---

## 5. Idempotency + replay

### 5.1 The resume endpoint is read-only by construction

`GET /agent/runs/{runID}/events` performs zero writes: no `SubmitAnswers`, no effort
persistence, no lock acquisition, no turn start — it only subscribes to an existing session.
`SubmitAnswers` remains reachable exclusively through `POST /agent/run` (Resume[] entries) and
`POST /api/approvals/{token}/resolve`, both in the mutation inventory. A reconnect can
therefore never re-trigger a side effect, no matter how many times it fires. This MUST be
pinned by the mutation-coverage test note (the route is GET, so the inventory test's
method-based sweep already ignores it; the design adds an explicit assertion that the handler
has no store-write seam).

### 5.2 Retrying the original POST after a pre-byte network error

Scenario: the client POSTs `/agent/run`, the network dies before any response byte. Did the
server start the run? Unknowable client-side. The contract:

1. The browser-level fetch wrapper (web/src/api/idempotency.ts) already attaches ONE
   `Idempotency-Key` per concrete Request, so a client-initiated retry of the *same logical
   send* MUST reuse the key (the wrapper preserves an explicitly-set header — the resilient
   wrapper sets it once and re-sends the same `Request`).
2. Server outcomes on the retried key (existing registry semantics, idempotency_http.go):
   - Run never started → `DecisionAcquired` → the run starts now. Clean.
   - Run started and is live → `DecisionInProgress` → **409 + Retry-After**. The client
     treats this as "the run exists": it fetches the conversation, reads `live_run_id`, and
     attaches via the resume endpoint with no Last-Event-ID (full replay). No duplicate turn.
   - Run started and finished → `DecisionIndeterminate` → **422**. Same recovery: snapshot
     refetch shows the persisted turn.
3. `MarkIndeterminate` timing stays where it is (after `next.ServeHTTP` returns,
   idempotency_http.go:214-226). Under detach the handler returns when the *viewer* leaves,
   possibly while the run is live — marking indeterminate at that point is still correct
   because indeterminate never re-executes (422), and the InProgress window it shortens is
   covered by the thread lock (a duplicate POST on the same thread gets 409 ErrThreadBusy
   from `TryLockThread` for the run's whole lifetime, §1.2). Moving the mark to run-terminal
   would couple the idempotency middleware to the registry for no additional safety — rejected.

---

## 6. Security

1. **Whole-origin auth:** both new routes live inside the agui mux behind the existing
   `RequireAuth` parent mount; the loopback-dev pass-through (`SecretConfigured=false`)
   behaves exactly as for `/agent/run`. No new unauthenticated surface (the only
   unauthenticated handlers remain the `/s/{token}` share routes).
2. **Per-identity scoping:** the session snapshots `IdentityID` at start from
   `scopedIdentityID(ctx)` — the same resolution `handleRun` used for the owner-scoped
   `GetForIdentity` gate — and both resume and cancel compare against the caller's
   `scopedIdentityID`, answering 404 on mismatch (§3.2). The (identity, thread) key of
   `byThread` reuses the runner's composite-key rationale (D-23): two identities never share
   a discovery slot.
3. **Redaction parity:** today `redactEvent` runs at the drain loop (server_sse.go:84), i.e.
   per-viewer at write time. Under detach, redaction moves to **buffer-insert time**
   (`RunSession.append` stores post-`redactEvent` frames), so the original stream and every
   resumed stream serve byte-identical frames — a resume can never widen what the live
   stream showed. `showReasoning` stays the cockpit-wide `true` handleRun passes to
   `Translate` (D-01: single-operator, whole-origin-private); it is recorded on the session
   for auditability but there is no per-viewer divergence to reconcile because the registry
   is fed ONLY by handleRun — Telegram's own `Translate(…, ShowReasoning)` call sites and
   Fanout never touch it.
4. **Cancel is a governed mutation:** inventory entry + Idempotency-Key + owner-scoped 404,
   so a foreign principal can neither cancel nor confirm existence of another's run — parity
   with `DeleteConversationLifecycle`'s 0-rows→404 discipline.
5. **DoS posture:** `RunMaxLive` caps registry memory; the ring cap bounds per-run memory;
   resume subscribers use the same `BufferCap` drop-on-full channels, so a slow resumer can
   never stall the producer (T-12-09 preserved); a resume flood is read-only work bounded by
   `RequireAuth`.

---

## 7. Test strategy + rollout

### 7.1 Gating and defaults

- `AURA_AGUI_RUN_DETACH` (KindBool, default **false**). Flag off ⇒ `runs == nil` ⇒
  `handleRun` takes today's exact code path (byte-identical wire, handler-defer unlock,
  request-ctx run) and the two new routes answer 404 (registry nil-check first — hide the
  surface entirely). Dev/live `.env` flips it on for the E2E campaign; the default flips to
  true in a follow-up commit only after the real-scenario E2E scores >9.8 (DoD rule) — the
  flip is its own PRD-amendment line.
- New knobs (all `internal/config/config_knobs.go` registered, env-catalog'd):
  `AURA_AGUI_RUN_DETACH=false`, `AURA_AGUI_RUN_BUFFER_EVENTS=2048`,
  `AURA_AGUI_RUN_LINGER_SEC=180`, `AURA_AGUI_RUN_MAX_WALLCLOCK_SEC=3600`,
  `AURA_AGUI_RUN_MAX_LIVE=16`.

### 7.2 Test plan sketch (daemon-free first — coverage-gate rule)

| Layer | Tests |
|---|---|
| `runsession.go` (pure) | ring wrap + `firstSeq` advance; gapless subscribe-under-append (race test with concurrent append/subscribe); property test (rapid): for random append/subscribe interleavings, every subscriber sees a strictly-increasing, gap-free suffix; lifecycle-never-dropped under full channel; terminal closes all channels (goleak). |
| `runregistry.go` | max-live refusal; reaper evicts only terminal+expired (use short linger; `synctest` if adopted); byThread removed at terminal; Close joins reaper (goleak). |
| `server_run_detach.go` handlers | scripted fake Runner (existing `agenttest`/fake-runner pattern in server tests): detached run completes after ResponseRecorder closes; unlock released at turn end not handler end (assert via `TryLockThread` probe); resume replays from `Last-Event-ID+1` (golden: reuse `internal/agui/testdata/golden-events.json` frames + assert `id:` lines); no-header full replay; gap → 410; foreign identity → 404; terminal replay ends after RUN_FINISHED; cancel → turn ctx cancelled → buffered RUN_ERROR terminal; malformed runID → 404. |
| Idempotency interplay | retry same key while live → 409 InProgress; after terminal → 422; duplicate POST same thread → 409 ErrThreadBusy. |
| Flag-off regression | full existing server test suite must pass with nil registry (no wire diff — golden compare of a run with detach off vs current master output). |
| Web (vitest) | parseSSEBlock id extraction; reducer split-replay idempotence property (§4.1); reconnect loop: mid-stream cut → resume fetch carries Last-Event-ID → identical final ThreadMessage; retries-exhausted → incomplete + snapshot refetch; reload-attach from live_run_id; Stop → cancel POST fired with Idempotency-Key. |
| Live E2E (DoD) | cockpit driver (memory: cockpit-e2e-idempotency-key recipe): start a long tool-calling run, kill the TCP connection mid-stream, reconnect, assert the final rendered message equals an uninterrupted control run; page-reload attach; Stop actually halts the agent (verify via /metrics + thread lock free). Score >9.8 required before the default flip. |
| Race/goleak | `go test -race` on internal/agui; goleak on every session/registry test. |

### 7.3 Migration-free rollout

All state is in-memory (`RunRegistry`), no Postgres/Neo4j migration, no persisted schema
change. The only DTO change is additive-optional (`live_run_id`). Rollback = flip the flag
off (or don't set it) — the wire reverts byte-identically. Daemon restart mid-run: sessions
are lost, clients get 404 on resume → snapshot fallback (§4.3); acceptable and documented as
a non-goal (§8).

---

## 8. Explicit non-goals + risks

**Non-goals**
- **Multi-replica fan-in / horizontal scale:** the registry is process-local; Aura is a
  single-process daemon by architecture. Out of scope, forever severable (a Redis-backed
  registry could later satisfy the same interface).
- **Durable event log / resume across daemon restart:** no persistence of AG-UI frames; the
  `MESSAGES_SNAPSHOT` remains the durable truth. A restart degrades to today's behavior.
- **Telegram / programmatic-Fanout resilience:** their paths (`agui_subscriber.go`,
  `client.go`) are untouched; Telegram has its own delivery semantics.
- **Multi-viewer live co-watching:** the session *supports* N subscribers structurally, but
  the design only specifies one browser viewer; concurrent-viewer UX is not designed here.
- **Branch/edit SSE routes (`streamPost`: /edit, /select):** they stream the same translated
  shape and can adopt the same wrapper later, but Tier B scopes only `/agent/run`. The
  design keeps `drainSession` route-agnostic so this is additive.

**Risks & mitigations**
1. **Widened thread-lock window** (§1.2): a crashed *client* used to free the thread via
   cancel; now the lock persists until turn end. Mitigated by: turn is Budget-bounded, the
   cancel endpoint exists, and the composer disables send while `live_run_id` is live.
2. **Memory creep** from lingering rings: bounded by `RunMaxLive × RunBufferEvents` + linger;
   reaper is tested for leak-freedom.
3. **Changed invariant — disconnect ≠ cancel** could surprise automation that relied on
   closing the connection to stop a run: called out in the PRD amendment; the cancel route is
   the sanctioned replacement.
4. **Fanout confusion risk:** two fan-out mechanisms now exist in one package. Mitigated by
   naming (`RunSession` vs `Fanout`), a package-doc paragraph stating Fanout = in-process
   subscribe-before-run, RunSession = HTTP resume, and by sharing the policy helpers so
   drift is structurally impossible.
5. **Proxy `id:`/comment handling:** some intermediaries reorder nothing but may buffer;
   already mitigated by Tier A heartbeat + `Cache-Control: no-cache`; `X-Accel-Buffering:
   no` can be added to the resume handler if the live E2E shows buffering (decide at E2E).
6. **Clock of seq vs SDK validation:** `events.ValidateSequence` consumers (if any resume a
   mid-run suffix) can see a suffix starting mid-lifecycle; the cockpit reducer tolerates
   this (ensure-on-demand parts), and full-replay (no header) is the default attach path —
   documented, not "fixed".

---

## PRD amendment checklist

The amendment (one commit, before implementation) must ratify, verbatim:

1. **Run lifetime is decoupled from the HTTP fetch** when `AURA_AGUI_RUN_DETACH=true`:
   client disconnect no longer cancels an in-flight `/agent/run` turn; a run is bounded by
   the agent Budget, `AURA_AGUI_RUN_MAX_WALLCLOCK_SEC`, explicit cancel, delete-lifecycle,
   and daemon shutdown.
2. **SSE frames on `/agent/run` (and the resume route) carry `id: <seq>`** — a per-run,
   1-based, strictly monotonic sequence; heartbeat comments carry no id. Additive when the
   flag is on; wire byte-identical when off.
3. **New route `GET /agent/runs/{runID}/events`** — authenticated, owner-scoped (foreign or
   unknown ⇒ 404), read-only SSE resume honoring `Last-Event-ID` (replay from n+1; absent ⇒
   full-buffer replay; replay gap ⇒ 410 Gone; terminal run ⇒ tail + close). Explicitly NOT
   in the mutation inventory.
4. **New route `POST /agent/runs/{runID}/cancel`** — the explicit Stop, added to
   `httpMutationRoutes` (`agent_run_cancel`, Idempotency-Key required), owner-scoped 404,
   202 semantics. Supersedes disconnect-as-stop.
5. **Thread-lock lifetime = run lifetime** (release moves from handler-defer to
   turn-completion); 409 `ErrThreadBusy` now spans the run's true duration. HITL pause still
   releases the lock (a paused run holds nothing).
6. **Conversation DTO gains optional `live_run_id`** (in-memory, absent when flag off /
   no live run) for reload-attach discovery.
7. **Redaction point moves to buffer-insert** for the detached path: resumed frames are
   byte-identical to originally-streamed frames; `showReasoning=true` stays cockpit-scoped
   (D-01 unchanged).
8. **Idempotency recipe for pre-byte POST retry:** reuse the key; 409-InProgress/422 ⇒
   discover via `live_run_id` and attach read-only. `agent_run` remains terminally
   indeterminate (unchanged).
9. **Env catalog additions:** `AURA_AGUI_RUN_DETACH` (default false),
   `AURA_AGUI_RUN_BUFFER_EVENTS` (2048), `AURA_AGUI_RUN_LINGER_SEC` (180),
   `AURA_AGUI_RUN_MAX_WALLCLOCK_SEC` (3600), `AURA_AGUI_RUN_MAX_LIVE` (16).
10. **Non-goals ratified:** single-process only (no multi-replica fan-in), no durable frame
    log (restart ⇒ snapshot fallback), Telegram/Fanout paths unchanged, default-flip to
    `true` only after live E2E >9.8 in a separate amendment line.

## Implementation task breakdown (SDD-sized)

1. **RS-01 — RunSession core** (`internal/agui/runsession.go` + tests): seqEvent, ring,
   gapless subscribeFrom, shared pump-policy helper extracted from `pumpSend`/`send`,
   terminal close. Pure, daemon-free, property + race + goleak. (~250 LOC + tests)
2. **RS-02 — RunRegistry + reaper** (`internal/agui/runregistry.go` + tests): Start/lookup/
   byThread, max-live refusal, linger reaper, Close. Config struct + `SetRunRegistry` seam.
3. **RS-03 — config knobs** (`internal/config`): five knobs + `ServerConfig` fields +
   catalog/validate/knob-tests (follow `AURA_AGUI_SSE_HEARTBEAT_SEC`'s pattern).
4. **RS-04 — detached handleRun path + drainSession** (`internal/agui/server_run_detach.go`,
   edit `server.go` ≤600 LOC): flag branch after the existing sync pre-work, producer
   goroutine owning Translate→redact→append + unlock, subscriber-#0 drain with `id:` lines +
   heartbeat. Flag-off golden-parity test.
5. **RS-05 — resume + cancel handlers** (same file or `server_run_resume.go`): §3 ladder,
   Last-Event-ID, 410, terminal tail, cancel 202; `httpMutationRoutes` entry + coverage-test
   update; conversation DTO `live_run_id` (edit `conversations_api.go`).
6. **RS-06 — daemon wiring** (`cmd/aura/serve_webui*.go`): construct registry when flag on,
   `SetRunRegistry`, shutdown `Close()` ordering (before Runner.Stop).
7. **RS-07 — web client** (`web/src/chat/sseResume.ts`, edits to `sseAdapter.ts` id parsing,
   `ExternalStoreChat.tsx` wrapper adoption + live_run_id attach + Stop→cancel + busy
   composer): vitest suite incl. split-replay property. Built via docker webbuild (dist rule).
8. **RS-08 — live E2E + flip** : cockpit driver scenario (mid-stream kill, reload-attach,
   Stop), score >9.8, quality-snapshot re-attestation, then the default-flip PRD amendment +
   commit.

Dependencies: RS-01→RS-02→RS-04→RS-05; RS-03 parallel; RS-07 after RS-05 contract freeze;
RS-06 anytime after RS-02; RS-08 last.
