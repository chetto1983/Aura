# Browser takeover: one hand on the box browser at a time

Proposed on 2026-10-09 at `1c872a1`, after reading OpenDots' Dot computers (CopilotKit, MIT,
released 2026-10-01) against Aura's live view (prd.md §12, "The operator takes the browser
over"). Not yet agreed with the operator: every decision below is a proposal for review, and
the PRD paragraph records the reading, not a live run.

OpenDots states the behaviour in one sentence: taking over pauses the agent's input while the
person clicks and types, and after release the agent must take a fresh snapshot before element
actions resume. Aura's live view lets the operator sign in through the cockpit, which is the
whole point of it, but has no notion of who holds the browser.

## The problem, measured in the tree

- `docker/aura-sandbox/browser-relay.mjs` forwards every allowed viewer event (`input_mouse`,
  `input_keyboard`, `input_touch`, `config`) to agent-browser's stream socket as it arrives.
- `handleBrowserInput` (`internal/agui/browser_live.go`) checks that the session is the caller's
  and that a viewer stream is open, then writes the event. There is no state beyond "a viewer
  exists".
- `browserViewers` is named in `internal/agui/server.go` and `browser_live.go` only. Nothing on
  the agent's call path reads it. The one browser-specific hook on that path,
  `withBrowserProfile` (`internal/agent/mcptools/bridge_call.go`), rewrites the profile argument.
- The agent holds `@eN` references from its last snapshot. The operator's navigation renumbers
  them in the page and nothing invalidates them in the agent's context. The browser skill
  (`internal/skills/embed/browser-aura/SKILL.md`) advises a new snapshot after anything that
  navigates; prd.md §12 (2026-09-27) records a model skipping that skill.

Consequence: a viewer's click and the agent's `agent_browser_click` reach the same Chromium with
no order between them, and the first agent action after a handoff may click whatever now sits
at a stale reference.

Not measured: the race on a live box, and which read tools renumber references. `snapshot`
does (the skill says so and the 2026-09-26 E2E relied on it); `read` and `screenshot` are not
measured. Both are acceptance items below.

## Decisions (proposed)

- **Watching is the default; control is explicit.** A viewer sees frames and may not type until
  it takes the session over. A click on a watched view sends nothing.
- **One holder per (identity, session).** Control belongs to the viewer stream that took it. The
  existing `claim` already lets a newer viewer replace the older one; the newer one starts
  watching. The end of the holding stream releases control.
- **While held, the agent's writes are refused, its reads pass.** A `browser__agent_browser_*`
  call on the held session that `browserRecipeActions` grades `MCPActionMutate` returns a tool
  error at once. Reads (`snapshot`, `screenshot`, `get_*`, `wait_*`, `tab_list`) run, so the
  agent can confirm the page after the handoff. The skill keeps telling it not to poll.
- **Refusal, not a pause and not an approval.** The call fails with an error the model reads.
  Rejected: blocking the call until release under `pausable` (the operator's time excluded).
  The login handoff already works by the agent ending its turn and the operator saying when
  they are done; a blocked call could not end the turn, and a held session can last minutes.
  Rejected: an approval row, which is a question to the operator, and the operator is the one
  holding the browser.
- **After release, references are stale until a snapshot.** The first call on that session
  that carries a `selector` argument is refused until an `agent_browser_snapshot` on the
  session succeeds. Calls without a selector (`open`, `press`, `scroll`, `back`, waits) pass:
  they do not address an element by reference.
- **The inline chat view and `/browser/<session>` share the control**, because both render
  `BrowserLiveView` and both reach the same server registry.
- **No new capability.** The routes stay own-identity only (`scopedIdentityID` plus session
  name), as today.
- **Taking over does not cancel an in-flight agent call.** Browser calls are short (click 55 ms,
  snapshot 33 ms, prd.md §12); a running `wait_for_*` is a read and finishes on its own.
- **No idle timeout on a hold.** The stream's end releases; a tab left open is a deliberate
  hold. Revisit only if measured as a problem.

## Shape

### Control registry, `internal/browsercontrol`

A new leaf package with no Aura imports, so both `internal/agui` (writer) and
`internal/agent/mcptools` (reader) can use it without a cycle. One file, well under 600 lines.

```go
// Registry remembers who holds each (identity, session) and which sessions the operator drove
// since the agent last took a snapshot.
type Registry struct { mu sync.Mutex; held map[key]holder; stale map[key]struct{} }

type Holder string // an opaque token the viewer stream owns

func (r *Registry) Hold(identityID, session string, h Holder)              // replaces a previous holder
func (r *Registry) Release(identityID, session string, h Holder) bool      // only the holder releases; marks stale
func (r *Registry) Held(identityID, session string) bool
func (r *Registry) Stale(identityID, session string) bool
func (r *Registry) Refresh(identityID, session string)                     // a snapshot succeeded
```

`Release` marks the session stale whenever it was held, even for a second: the operator may
have navigated with one click.

### Server, `internal/agui`

`browser_live.go` is at 295 lines; the control route goes in a new `browser_control.go`.

- `POST /api/browser/sessions/{session}/control` with `{"held": true|false}`. `204` on success.
  `409 {"error":"no_live_view"}` when the caller has no open stream for the session, since
  control belongs to a stream. The holder token is the viewer the stream claimed.
- `POST .../input` while the caller's stream is not the holder answers
  `409 {"error":"not_in_control"}`. The existing `409 no_live_view` stays for a missing stream.
- The stream handler's deferred `release` also releases control when its viewer held it.
- A hold marks the box used, as input does today (prd.md §12, the idle reaper).

### Bridge, `internal/agent/mcptools`

A consumer-declared seam, wired at the composition root beside the relay
(`cmd/aura/serve_agui.go`, `serve_browser_live.go`):

```go
type browserControl interface {
    Held(identityID, session string) bool
    Stale(identityID, session string) bool
    Refresh(identityID, session string)
}
```

`bridge_call.go` (125 lines) gains, after `withBrowserProfile`:

```go
if err := browserRefusal(b.name, args, held, stale); err != nil { return tools.ToolResult{}, err }
```

and after a successful `agent_browser_snapshot`, `Refresh`. `browserRefusal` is a pure function
in a new `bridge_browser_control.go`: it reads the session from the profiled args, grades the
tool through `browserRecipeActions`, and returns one of two errors. The identity comes from
`identityctx.IdentityID(ctx)`, as `routeApprove` reads it.

A nil `browserControl` (unit tests, the CLI's `aura toolpipe`) refuses nothing.

### Web, `web/src/browserLive`

- `useBrowserLive` (88 lines) returns `control: 'watching' | 'driving'`, `takeOver()`,
  `release()`. The server is the truth: a `409 not_in_control` on input sets `watching`; the
  existing `409 taken_over` mapping moves to the body's `error` field, since both are 409.
- `BrowserLiveView` (215 lines) sends mouse, wheel and key events only while driving. The
  header gains one button, "Take over" / "Release", and the status dot's label names the state.
  Keys in `resources.browserLive.ts`, both locales.
- `ExternalStoreChat_messages.tsx` renders the same component under the turn and changes
  nothing.

### Skill text

`internal/skills/embed/browser-aura/SKILL.md`, "Logins": the operator takes the session over in
the live view, signs in, releases and says so; the agent then takes a snapshot, because its
old references are refused until it does. "Before you act on a site" names the two refusals so
the model recognises them.

## Errors

| Where | Condition | Answer |
|---|---|---|
| input route | caller's stream is not the holder | `409 not_in_control` |
| control route | caller has no stream for the session | `409 no_live_view` |
| bridge | write tool on a held session | `browser session "x" is held by the operator in the live view: wait for them to release it and tell you, then take a snapshot` |
| bridge | `selector` on a stale session | `references of browser session "x" are stale, the operator drove it: take a snapshot first` |

Both bridge errors are plain tool errors (`isError`), never a pause, never a reservation.

## Observability

- `slog.Info` on hold and release with identity, session and the hold's duration.
- One counter in the obs catalog, `browser_control_refusals`, attribute `reason` in
  `held | stale`, beside `mcp_calls`.
- The reservation ledger is untouched: a refused call never reaches `Decide`'s reserve.

## Testing

Per prd.md §18 and CLAUDE.md: realistic fixtures, goleak on stream tests, race detector,
coverage floor 85% across the tag matrix, mutation ≥70% on the critical files.

- `internal/browsercontrol`: hold, replace, release by a non-holder (no-op), release marks
  stale, refresh clears; property test that `Held` and `Stale` are never both true for a
  session a snapshot just refreshed. Mutation on this file.
- `internal/agui`: control route and the two 409s; stream end releases a hold (goleak, the
  existing `main_test.go` pattern); the inline view and the page share one registry entry.
- `internal/agent/mcptools`: `browserRefusal` table over all 29 recipe tools × held × stale,
  with and without `selector`; `Execute` with a fake control; `snapshot` calls `Refresh` only
  on success. Mutation on `bridge_browser_control.go`.
- `web`: vitest for the hook's state machine (watching → driving → watching on release, on
  `not_in_control`, on stream end) and for the view sending nothing while watching;
  `liveInput.ts` stays untouched and mutation-tested as today.
- Lab-VM acceptance, the Definition of Done (score > 9.8 on the real scenario):
  1. the agent opens the fixture login page in session `portal` and ends its turn;
  2. the operator takes over in the inline view, signs in, releases, writes "done";
  3. the agent's first `click` with a stale `@eN` is refused with the stale error, it takes a
     snapshot, the next click succeeds, three runs out of three;
  4. during a hold, an agent `fill` is refused with the held error and a `get_url` passes;
  5. measure which of `read`, `screenshot`, `snapshot` without `interactive` renumber
     references, and narrow or widen `Refresh`'s trigger to the measurement.

## Open questions for the operator

1. Should `read` count as a snapshot for `Refresh`? Decided after item 5 above, not before.
2. Should a hold end an in-flight `wait_for_*`? Proposed no.
3. On Telegram, should the agent be told when the operator releases, or does the operator say
   so, as the skill has it today? Proposed the latter, nothing new.

## Out of scope

OpenDots' per-Dot activity log, its per-Dot browser/files/shell toggles (Aura has
`RequireCapability` and the box), and its supervisor as the only Docker-socket holder, which
is a separate decision for prd.md §17.
