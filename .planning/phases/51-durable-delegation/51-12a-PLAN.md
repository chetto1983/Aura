---
phase: 51-durable-delegation
plan: 12a
type: execute
wave: 7
depends_on: ["51-07", "51-09", "51-10", "51-11"]
files_modified:
  - internal/agui/server_swarm_events.go
  - internal/agui/server_swarm_events_status.go
  - internal/agui/server_swarm_events_test.go
  - internal/agui/server_swarm_transcript.go
  - internal/agui/server.go
  - internal/agui/translator.go
  - cmd/aura/serve_agui.go
  - internal/agent/display/payload.go
  - internal/agent/display/preview.go
  - internal/agent/display/preview_test.go
  - internal/agent/display/swarm.go
  - internal/agent/display/swarm_test.go
  - scripts/coverage_package_policy.json
autonomous: true
requirements: [SWARM-12, SWARM-10]

estimate:
  tokens: 80000
  raw_tokens: 80000
  tasks: 2
  confidence: low
  # Split out of the single 51-12 plan at plan-check (blocker B3): that plan carried 27
  # files_modified and a first task touching 15 files including 8 new TypeScript modules —
  # 1.5x the smart-zone budget. This half is Go only, has its own daemon-free tests, and can
  # go green with no browser in the loop. The cockpit half is 51-12b.

must_haves:
  truths:
    - "GET /api/conversations/{conv}/swarm/events serves a worker's transcript as SSE, replayed and then TAILED, translated through the SHIPPED agui.Translate over the agent.Event type dumpTranscript already stores — no second translator, no second SSE pump (SWARM-12 leg 3, PRD Amendment #172 point 3)"
    - "The route is identity-scoped and 404-hiding exactly like the 51-07 transcript route: a bad conversation id, a foreign conversation, an unwired reader and a rejected child id all render the same opaque 404 body, indistinguishable on the wire"
    - "The stream is PUSH: the server tails the file on a fixed package-constant interval and pushes what it reads; there is no route a browser can usefully re-request, which is what makes 51-07's no-polling prohibition enforceable on the client side in 51-12b"
    - "A worker's reasoning deltas never leave the server — the translator is called with showReasoning false, so a worker's raw chain of thought is not in the bytes at all, not merely hidden by the client (UI-SPEC Decision B; ADK's is_final_response precedent)"
    - "The stream ends on its own when the transcript's terminal marker line is observed — the swarm_child_status state-delta key plan 51-11 writes — and unwinds cleanly on client disconnect with no goroutine left behind (goleak, this package's shipped convention)"
    - "A JSONL line that fails to unmarshal is skipped with a WARN and never yielded as an error, so one malformed line cannot kill a stream the operator is watching"
    - "Without a child parameter the SAME route emits one aura.swarm.worker CUSTOM event per child whenever that child's derived state changes, plus one per known child at connect so a client that joins late is never blank"
    - "The multiplexed mode emits only child_id, status, last_event_at, events and duration_sec — never transcript content — so the chip's stream discloses strictly less than the pane's"
    - "Server-side status derivation is one rule in one place: the terminal marker's swarm_child_status when present; otherwise awaiting_input when the last event carries an awaiting-input action; otherwise stalled when the last event is older than the configured idle threshold; otherwise running"
    - "The idle threshold is the SHIPPED AURA_SWARM_CHILD_IDLE_SEC read through chat.cfg — this plan adds no env var, and the operator tunes one knob, not two"
    - "The swarm_spawn display normalizer decodes BOTH shapes through ONE path: a synchronous []ChildReport array exactly as today, and plan 51-11's queued dispatch object by returning its workers array — so the background dispatch and the synchronous result render through the same component and cannot drift"
    - "Any other preview payload still returns not-recognized, so the over-cap and context-unavailable inline errors keep degrading to the escaped raw panel (D-FALLBACK)"
    - "display.ChildReport stays tag-compatible with swarm.ChildReport byte-for-byte — the mirror gains goal and attempts with the same omitempty tags plan 51-11 added, which is the whole reason the mirror exists"
    - statement: "The new agui route and the widened display normalizer land in target-mode coverage packages (internal/agui, internal/agent/display, both >=85% in scripts/coverage_package_policy.json), so the gate must still report both at or above 85% after this plan — a drop is fixed with a test, never with a policy edit"
      verification: explicit
  prohibitions:
    - statement: "MUST NOT write a second SSE pump — the handler ends in the shipped s.streamSSE call, which already owns the bounded buffer, the heartbeat frames, the drop-on-full backpressure and the goleak-clean teardown"
      status: required
      verification: "grep -n 's.streamSSE(' internal/agui/server_swarm_events.go matches and no channel-and-goroutine pump is declared in the new files"
    - statement: "MUST NOT invent a second 404 policy — the new route reuses server_swarm_transcript.go's opaque body and its ladder order (uuid parse, ownership check BEFORE any read, nil-reader guard)"
      status: required
      verification: "grep -n 'swarmTranscriptNotFoundBody' internal/agui/server_swarm_events.go matches, and no second not-found body literal is declared in the package"
    - statement: "MUST NOT declare a second transcript reader interface — swarmTranscriptReader is widened in place, the rule 51-PATTERNS.md states for exactly this situation"
      status: required
      verification: "grep -rn 'interface {' internal/agui/server_swarm_transcript.go internal/agui/server_swarm_events.go shows one reader interface in the package, not two"
    - statement: "MUST NOT stream a worker's reasoning deltas — the translator call site passes false in the showReasoning position"
      status: required
      verification: "read the Translate( call in internal/agui/server_swarm_events.go and confirm the argument in that position is the false literal"
    - statement: "MUST NOT add an env var for the pane's stall threshold — the shipped AURA_SWARM_CHILD_IDLE_SEC is the one knob"
      status: required
      verification: "grep -rn 'AURA_SWARM' internal/agui/ internal/config/config_knobs.go shows no new name introduced by this plan"
    - statement: "MUST NOT add a migration or a delivery channel — this plan adds one read route and translates what already exists on disk"
      status: required
      verification: "git status --porcelain internal/db/migrations/ is empty at plan close"
    - statement: "MUST NOT touch web/ — the cockpit half is plan 51-12b, and a bundle built on this Windows host would destroy the committed internal/webui/dist"
      status: required
      verification: "git diff --name-only for this plan's commits lists no path under web/ and none under internal/webui/dist"
  artifacts:
    - path: "internal/agui/server_swarm_events.go"
      provides: "the identity-scoped, 404-hiding worker-events SSE route in its child (transcript replay + tail) mode"
      min_lines: 150
    - path: "internal/agui/server_swarm_events_test.go"
      provides: "the 404 ladder, replay order, tail pickup, terminal-marker stop, malformed-line skip and goleak coverage — all daemon-free over an in-memory JSONL fixture"
      min_lines: 180
    - path: "internal/agui/server_swarm_events_status.go"
      provides: "the multiplexed per-child status branch and the one status-derivation rule"
      min_lines: 90
  key_links:
    - from: "internal/agui/server_swarm_events.go"
      to: "internal/swarm ReadTranscript + ListChildTranscripts"
      via: "the widened swarmTranscriptReader seam, adapted at cmd/aura/serve_agui.go"
      pattern: "ListChildTranscripts"
    - from: "internal/agui/server_swarm_events.go"
      to: "the cockpit"
      via: "agui.Translate over the transcript's own agent.Event stream, pumped by the shipped streamSSE"
      pattern: "Translate("
    - from: "internal/agui/server_swarm_events_status.go"
      to: "the chip and the picker (plan 51-12b)"
      via: "one CUSTOM event name, declared beside the shipped ArtifactEventName/DisplayEventName constants"
      pattern: "SwarmWorkerEventName"
    - from: "internal/agent/display/preview.go"
      to: "plan 51-11's queued swarm_spawn result object"
      via: "the SAME swarm_spawn normalizer, widened with a second decode attempt, never a second normalizer"
      pattern: "workers"
---

<objective>
Ship the SERVER half of the cockpit worker surface: **one read route that serves a worker's own
transcript as a live SSE stream, and one multiplexed status stream the chip reads — plus the
display normalizer widening that lets a background dispatch render through the shipped swarm
component.**

Measured 2026-08-29 (`live-check/d03/RESULTS.md` §3): the 51-07 transcript route served a live
worker's full event log — 247 800 bytes, 200, 16 ms, mid-run — and **the cockpit has no viewer for
it**. The transcript exists as an HTTP resource, not as a place the operator can look. The
operator's own words the same morning: *"aggiungerei anche sul cockpit la possibilità di vedere
l'agente lavorare su una chat parallela"*.

Nothing here is invented. `agui.Translate` consumes exactly the `agent.Event` type `dumpTranscript`
stores; `streamSSE` is the shipped pump with heartbeat, backpressure and goleak-clean teardown;
`server_swarm_transcript.go` already owns the 404-hiding ladder this route repeats verbatim; and
plan 51-11 already writes the terminal marker this stream stops on and the queued dispatch object
this normalizer decodes.

Decisions carried: **D-15** (a worker is a thread the operator can switch to), **UI-SPEC Decision B**
(tool calls and text, no reasoning deltas), **51-07's prohibition** (no cockpit-side polling — which
this plan makes enforceable by making the server the pusher), **D-02** (no second delivery channel).

Purpose: the bytes the pane and the chip need, provable without a browser. Output: one route in two
modes, one CUSTOM event name, one widened normalizer.
</objective>

## Artifacts this plan produces

**New files (Go)**
- `internal/agui/server_swarm_events.go`, `internal/agui/server_swarm_events_status.go`,
  `internal/agui/server_swarm_events_test.go`

**HTTP route**
- `GET /api/conversations/{conv}/swarm/events` — SSE. With `?child=<childID>` it replays and tails
  that child's transcript translated through `agui.Translate`. Without `child` it emits one
  `aura.swarm.worker` CUSTOM event per child whenever that child's state changes. Optional
  `?offset=` resume cursor on the child mode.

**Go symbols**
- `agui.SwarmWorkerEventName = "aura.swarm.worker"` (the stable CUSTOM event name, namespaced beside `ArtifactEventName`)
- `(*agui.Server).SetSwarmWorkerIdle(d time.Duration)`
- `agui.swarmTranscriptReader` widened in place with `ListChildTranscripts(ctx context.Context, conv string) ([]string, error)`
- `(*agui.Server).registerSwarmWorkerEventRoutes(mux)`, `handleSwarmWorkerEvents`
- `display.ChildReport.Goal string` (`json:"goal,omitempty"`), `display.ChildReport.Attempts int` (`json:"attempts,omitempty"`)
- `display.StatusRunning`, `display.StatusStalled`, `display.StatusDeadLetter`

**New env vars:** none (the stall threshold reuses the shipped `AURA_SWARM_CHILD_IDLE_SEC`).
**New migration:** none. **New dependency:** none. **No file under `web/` is touched.**

<execution_context>
@$HOME/.claude/gsd-core/workflows/execute-plan.md
@$HOME/.claude/gsd-core/templates/summary.md
</execution_context>

<context>
@CLAUDE.md
@.planning/phases/51-durable-delegation/51-UI-SPEC.md
@.planning/phases/51-durable-delegation/51-UX-ENVELOPE-RESEARCH.md
@.planning/research/adk-subagent-visibility-2026-08-29.md
@.planning/phases/51-durable-delegation/51-CONTEXT.md
@.planning/phases/51-durable-delegation/51-07-SUMMARY.md
</context>

<tasks>

<task type="tracer" tdd="true">
  <name>Task 1: TRACER — one live worker's transcript leaves the daemon as a translated, tailing SSE stream</name>
  <files>internal/agui/server_swarm_events.go, internal/agui/server_swarm_events_test.go, internal/agui/server_swarm_transcript.go, internal/agui/server.go, cmd/aura/serve_agui.go</files>
  <read_first>
    - `internal/agui/server_swarm_transcript.go` IN FULL (100 lines) — the 404-hiding ladder to copy verbatim: uuid parse, `s.conv.GetForIdentity` BEFORE any read, nil-reader guard, and one opaque body for every failure. The new route repeats this ladder exactly; do not invent a second policy.
    - `internal/agui/server_sse.go:35-120` (`streamSSE`) — the shipped pump: cap-N buffered channel, producer goroutine, heartbeat comment frames, ctx.Done teardown. The new handler ends in a `s.streamSSE(...)` call and owns no pump of its own.
    - `internal/agui/translator.go:73-120` — `Translate(threadID, runID, idgen, seq iter.Seq2[*agent.Event, error], showReasoning)`: the exact signature and the run-boundary policy. Note it consumes precisely the type `dumpTranscript` writes. Read the constant block at the top of the file too — that is where the new CUSTOM event name lands.
    - `internal/swarm/transcript_api.go` IN FULL — `ReadTranscript`'s offset semantics and its complete-line guarantee (a trailing partial line is withheld until its newline arrives), and `ListChildTranscripts`.
    - `internal/agent/event.go:199-240` — `Event.MarshalJSON`/`UnmarshalJSON`, so the JSONL decode is symmetric with the write.
    - `internal/agent/event.go:69-102` (`Actions`) — `StateDelta map[string]any`, where plan 51-11's terminal marker rides.
    - `cmd/aura/serve_agui.go:25-35` and `:100-115` — `swarmTranscriptAdapter` and the `SetSwarmTranscripts` call site.
    - `.planning/phases/51-durable-delegation/51-11-PLAN.md` §"Wire contracts other plans read" — the terminal marker's three state-delta key names. Key on them verbatim.
    - `.planning/phases/51-durable-delegation/51-UI-SPEC.md` §3 Decision B — tool calls and text, no reasoning deltas.
  </read_first>
  <behavior>
    - `GET /api/conversations/{bad-uuid}/swarm/events` returns 404 with the same opaque body the transcript route uses; so does a conversation the caller does not own, an unwired reader, and a child id `ReadTranscript` rejects. No branch is distinguishable from another on the wire — a test asserts the four responses are byte-identical.
    - `GET /api/conversations/{conv}/swarm/events?child=<childID>` on a conversation the caller owns streams `text/event-stream`, opening with a RUN_STARTED frame and then the translated events of that child's transcript in file order.
    - The stream keeps tailing after the replay: an event appended to the JSONL after the client connected reaches the client without the client asking again.
    - The stream ends with a terminal frame once the transcript's terminal marker line is observed, and unwinds cleanly on client disconnect with no goroutine left behind.
    - A line that fails to unmarshal is skipped with a WARN; the stream continues and the surrounding events still arrive.
    - Reasoning deltas never carry real text — the translator is called with `showReasoning` false.
    - The multiplexed (no `child` parameter) path answers the same opaque 404 until Task 2 implements it, so an early client cannot see a half-built branch.
  </behavior>
  <action>
Create `internal/agui/server_swarm_events.go`. Widen the EXISTING `swarmTranscriptReader` interface
in `server_swarm_transcript.go` in place with `ListChildTranscripts(ctx context.Context, conv
string) ([]string, error)` — the same interface, extended, never a second one (51-PATTERNS.md states
this rule for exactly this situation). Add `ListChildTranscripts` to `cmd/aura/serve_agui.go`'s
`swarmTranscriptAdapter`, closing over the same `runDir`.

Add ONE field to the `Server` struct in `server.go` — `swarmWorkerIdle time.Duration`, the stall
threshold Task 2 reads — plus a `SetSwarmWorkerIdle` setter next to `SetSwarmTranscripts`, and ONE
line `s.registerSwarmWorkerEventRoutes(mux)` beside the existing `s.registerSwarmTranscriptRoutes(mux)`
call. `server.go` is 581 lines against the 600 ceiling: keep the addition to those lines and put
every comment in the new file. If the edit would cross 600, extract per refactor-on-touch rather
than shaving the comment.

`handleSwarmWorkerEvents` repeats `handleSwarmTranscript`'s ladder verbatim — uuid parse,
`s.conv.GetForIdentity(ctx, conv, scopedIdentityID(ctx))`, nil-reader guard, one opaque
`swarmTranscriptNotFoundBody` for every failure — and only then branches on the `child` query
parameter. This task implements the `child` branch; Task 2 implements the multiplexed branch, so
leave a clearly-named unimplemented path that answers the same 404 until then.

The child branch builds an `iter.Seq2[*agent.Event, error]` that: reads with `ReadTranscript(ctx,
conv, child, offset)`, splits the returned bytes on newlines, unmarshals each complete line into an
`agent.Event`, yields it, advances the offset to the value `ReadTranscript` returned, and then
sleeps a package-constant tail interval (one second — a constant in this file, not an env var, and
not a knob) before reading again. It stops when the terminal marker is seen — a line whose
`Actions.StateDelta` carries the `swarm_child_status` key plan 51-11 writes — or when `ctx` is done.
A line that fails to unmarshal is skipped with a WARN, never yielded as an error that would kill the
stream.

Feed that sequence to `Translate(threadID, runID, idgen, seq, false)` with `threadID` composed as
the worker session id `conv + "-swarm-" + child` (the same flat shape `runChild` builds for
`SessionID`) and `runID` the child id, then hand the result to `s.streamSSE(ctx, w, stream)`. Set
the SSE headers the shipped handlers set, including `X-Content-Type-Options: nosniff`.

Say in the file header WHY the server tails rather than exposing a cursor the browser re-requests:
51-07's prohibition forbids a cockpit-side polling loop for delegation status, and a push stream is
the only shape that makes that prohibition enforceable rather than merely asserted.

Test it in `server_swarm_events_test.go` with a fake reader over an in-memory JSONL fixture: the
404 ladder including a foreign identity and a byte-comparison of the four bodies, the replay order,
the tail picking up an event appended AFTER the stream is open, the terminal-marker stop, the
malformed-line skip, and `goleak` cleanliness (the package's existing convention).
  </action>
  <verify>
    <automated>wsl.exe -e bash -lc 'export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"; cd /mnt/d/Repo/Aura &amp;&amp; go build ./... &amp;&amp; go vet ./internal/agui/... ./cmd/aura/... &amp;&amp; go test ./internal/agui/ -run "TestSwarmWorkerEvents" -count=1'</automated>
  </verify>
  <acceptance_criteria>
    - `grep -n 'swarmTranscriptNotFoundBody' internal/agui/server_swarm_events.go` matches — the new route reuses the shipped opaque body, it does not define a second one.
    - Read the `Translate(` call in `internal/agui/server_swarm_events.go` and confirm the argument in the `showReasoning` position is the false literal.
    - `grep -n 's.streamSSE(' internal/agui/server_swarm_events.go` matches — the handler owns no pump of its own.
    - `grep -n 'swarm_child_status' internal/agui/server_swarm_events.go` matches, spelled exactly as plan 51-11 writes it.
    - `go test ./internal/agui/ -run TestSwarmWorkerEventsForeignIdentityIs404 -count=1 -v` passes and the test compares the four 404 bodies for byte equality.
    - `go test ./internal/agui/ -run TestSwarmWorkerEventsTailsAppendedEvent -count=1 -v` passes and the fixture appends AFTER the stream is open.
    - `go test ./internal/agui/ -run TestSwarmWorkerEventsSkipsMalformedLine -count=1 -v` passes with the surrounding events still delivered.
    - `wc -l internal/agui/server.go internal/agui/server_swarm_events.go` — both at or under 600.
    - `git diff --name-only` lists nothing under `web/`.
  </acceptance_criteria>
  <done>A live worker's transcript leaves the daemon as a translated, tailing, identity-scoped SSE stream that stops by itself when the worker finishes.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: The multiplexed status stream the chip reads, and one normalizer for both swarm_spawn shapes</name>
  <files>internal/agui/server_swarm_events_status.go, internal/agui/server_swarm_events.go, internal/agui/server_swarm_events_test.go, internal/agui/translator.go, cmd/aura/serve_agui.go, internal/agent/display/payload.go, internal/agent/display/preview.go, internal/agent/display/preview_test.go, internal/agent/display/swarm.go, internal/agent/display/swarm_test.go, scripts/coverage_package_policy.json</files>
  <read_first>
    - `internal/agui/translator.go`'s constant block — `ArtifactEventName` and `DisplayEventName`, the namespacing convention and the doc-comment style the new constant follows.
    - `internal/agui/server_sse.go` — how a handler that emits its OWN frames (rather than translating an event sequence) feeds `streamSSE`; the artifact event path is the shipped precedent.
    - `internal/agent/display/preview.go:39-78` (`decodeToolPreview`) — the `swarm_spawn` case this task widens, and the D-FALLBACK contract that an unrecognized preview must degrade to the escaped raw panel rather than break.
    - `internal/agent/display/payload.go:1-50` and `internal/agent/display/swarm.go` IN FULL — `ChildReport`'s mirror of `swarm.ChildReport` and the status-constant mirror, both of which gain the fields plan 51-11 added on the swarm side.
    - `.planning/phases/51-durable-delegation/51-11-PLAN.md` §"Wire contracts other plans read" — the EXACT queued-result object shape (`queued`, `note`, `workers[]{goal_index, child_id, status, goal}`). Decode against those names verbatim.
    - `internal/config/config.go:95-97` and the `SwarmChildIdleSec` field — the shipped knob the idle threshold comes from.
    - `scripts/coverage_package_policy.json` — `internal/agui` and `internal/agent/display` are BOTH `"mode": "target"`, so no baseline number can move here; what the gate checks is that each stays at or above 85%. Read `scripts/coverage_gate.sh`'s policy section for what a target-mode failure prints.
  </read_first>
  <behavior>
    - `GET /api/conversations/{conv}/swarm/events` with no `child` parameter emits one `aura.swarm.worker` event per child of the conversation carrying `child_id`, `status`, `last_event_at`, `events` and `duration_sec` — and nothing else. Transcript content never appears in this mode.
    - One event per known child is emitted at connect, so a client that joins late is not blank; afterwards an event is emitted for a child only when its derived state DIFFERS from the state last emitted for that child.
    - Status derivation, in order: the terminal marker's `swarm_child_status` when present; otherwise `awaiting_input` when the last event carries an awaiting-input action; otherwise `stalled` when the last event is older than the configured idle threshold; otherwise `running`.
    - The idle threshold comes from `s.swarmWorkerIdle`, set at `cmd/aura/serve_agui.go` from the shipped config field. A zero or negative value disables the stalled branch rather than marking every child stalled.
    - The multiplexed branch answers the same opaque 404 ladder as the child branch — grouping does not weaken scoping.
    - `decodeToolPreview("swarm_spawn", <a JSON array of ChildReport>)` keeps returning that array unchanged — every synchronous swarm renders exactly as today.
    - `decodeToolPreview("swarm_spawn", <plan 51-11's queued object>)` returns its `workers` array as `[]ChildReport`, so a background dispatch renders one running row per worker through the SAME normalizer.
    - `decodeToolPreview("swarm_spawn", <any other string>)` still returns not-recognized, so the over-cap and context-unavailable inline errors keep degrading to the escaped raw panel.
    - A queued object that decodes but carries no `workers` key returns not-recognized too — an empty array and an absent key are different answers.
    - `display.ChildReport` and `swarm.ChildReport` round-trip through each other's json tags for `goal` and `attempts`.
  </behavior>
  <action>
**Server.** Create `internal/agui/server_swarm_events_status.go` and implement the multiplexed
branch of `handleSwarmWorkerEvents` there (the path Task 1 left answering 404) — a separate file
from the start, because `server_swarm_events.go` already carries the child branch and the 600-line
ceiling is not something to discover later.

It calls `ListChildTranscripts(ctx, conv)`, then for each child reads with `ReadTranscript` from its
own advancing offset on the same package-constant tail interval, derives that child's status by the
ordered rule in `<behavior>`, and yields ONE `events.NewCustomEvent` named by the new exported
constant `SwarmWorkerEventName = "aura.swarm.worker"` whenever a child's derived state differs from
the state last emitted for it — plus one per known child at connect. Namespace the constant beside
`ArtifactEventName` and `DisplayEventName` in `translator.go`'s constant block for discoverability,
and say in its doc comment that it is the chip's and the picker's single source. Feed the sequence
straight to `s.streamSSE`; this branch does not call `Translate` because it emits its own frames.

Wire `SetSwarmWorkerIdle` at `cmd/aura/serve_agui.go` from `chat.cfg.SwarmChildIdleSec`. Do not add
an env var — the operator already tunes that knob, and a second name for the same idea is a knob
that will drift.

**Display normalizer.** In `internal/agent/display/payload.go`, add `Goal string` with tag
`json:"goal,omitempty"` and `Attempts int` with tag `json:"attempts,omitempty"` to `ChildReport`,
mirroring the fields plan 51-11 added to `swarm.ChildReport` byte-for-byte — the two structs must
stay tag-compatible, which is the whole reason the mirror exists. In `internal/agent/display/swarm.go`,
add `StatusRunning`, `StatusStalled` and `StatusDeadLetter` beside the three existing constants, with
the same "mirror of swarm.Status*" comment.

In `internal/agent/display/preview.go`'s `swarm_spawn` case: keep the array attempt first and
unchanged; on its failure, attempt the queued object into a local struct with `Queued`, `Note` and
`Workers []ChildReport`, and return `Workers` when the decode succeeds AND the object actually
carried a workers key. Both failures still return not-recognized. Explain in the comment that the
background dispatch and the synchronous result deliberately share ONE normalizer and ONE cockpit
component, so the chip cannot drift between the two paths.

Tests: extend `server_swarm_events_test.go` with the multiplexed 404 ladder, the connect-time
emission per known child, the change-only re-emission (a child whose state is unchanged produces no
second event), each of the four status-derivation branches, and an assertion that no transcript text
appears in any emitted payload. Extend `preview_test.go` with the four decode cases and
`swarm_test.go` with the mirror-tag assertion.

**Last, re-measure the coverage policy rather than assuming it.** Both packages this plan touches are
target-mode, so the expected outcome is that `scripts/coverage_package_policy.json` does NOT change.
Prove it, do not assume it:

    wsl.exe -e bash -lc 'export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"; cd /mnt/d/Repo/Aura && bash scripts/coverage_docker.sh'

Record the printed lines for `internal/agui` and `internal/agent/display` in the SUMMARY. If either
fell below 85%, the fix is a test in this plan, never an edit to the policy file.
  </action>
  <verify>
    <automated>wsl.exe -e bash -lc 'export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"; cd /mnt/d/Repo/Aura &amp;&amp; go build ./... &amp;&amp; go test ./internal/agent/display/ ./internal/agui/ -count=1'</automated>
  </verify>
  <acceptance_criteria>
    - `go test ./internal/agent/display/ -run TestDecodeSwarmSpawnPreview -count=1 -v` passes with a synchronous-array case, a queued-object case, a queued-object-without-workers case and a plain-string case.
    - `grep -n 'workers' internal/agent/display/preview.go` matches inside the `swarm_spawn` case.
    - `grep -n 'goal,omitempty' internal/agent/display/payload.go internal/swarm/report.go` matches in both — the mirror stays tag-compatible.
    - `grep -n 'SwarmWorkerEventName' internal/agui/translator.go internal/agui/server_swarm_events_status.go` matches in both.
    - `go test ./internal/agui/ -run TestSwarmWorkerStatusEmitsOnlyOnChange -count=1 -v` passes, and a second identical read produces no second event for that child.
    - `go test ./internal/agui/ -run TestSwarmWorkerStatusCarriesNoTranscriptText -count=1 -v` passes against a fixture whose transcript contains a distinctive sentinel string.
    - `grep -rn 'AURA_SWARM' internal/agui/` shows no new env var name.
    - `wc -l internal/agui/server_swarm_events.go internal/agui/server_swarm_events_status.go internal/agui/translator.go` — every file at or under 600.
    - `scripts/coverage_docker.sh` reported `internal/agui` and `internal/agent/display` at or above 85%, and `git diff scripts/coverage_package_policy.json` is empty (both are target-mode; a diff here means something moved that this plan did not expect and must be explained, not committed silently).
    - `git diff --name-only` lists nothing under `web/`.
  </acceptance_criteria>
  <done>The chip's status stream and the pane's transcript stream are both served by one identity-scoped route, and a background dispatch decodes through the same normalizer a synchronous swarm already uses.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| browser → worker-events route | an unauthenticated or foreign-identity caller must not learn whether a conversation or a child exists |
| transcript file → the SSE stream | a malformed or hostile JSONL line reaches the translator |
| model-influenced child id → the filesystem | the `child` query parameter selects a transcript path |
| worker LLM output → the wire | a worker's own text and tool output leaves the daemon toward the operator's browser |

## STRIDE Threat Register (ASVS L1, block on: high)

| Threat ID | Category | Component | Severity | Disposition | Mitigation Plan |
|-----------|----------|-----------|----------|-------------|-----------------|
| T-51-57 | Information Disclosure | `GET /api/conversations/{conv}/swarm/events` | high | mitigate | The 404-hiding ladder from `server_swarm_transcript.go` is repeated verbatim: uuid parse, `s.conv.GetForIdentity` BEFORE any read, nil-reader guard, and one opaque body for every branch. A test compares the four failure bodies for byte equality, so "indistinguishable on the wire" is asserted rather than asserted-about |
| T-51-58 | Tampering | the `child` query parameter | high | mitigate | The parameter reaches the filesystem only through `swarm.ReadTranscript`, whose `validatePathSegment` rejects empty, `.`, `..`, either separator and NUL before any `os.Open`; a rejected id renders the same 404 |
| T-51-59 | Denial of Service | the tailing SSE stream | medium | mitigate | Each read is bounded by `ReadTranscript`'s 1 MiB cap; the tail interval is a fixed one-second constant; the stream terminates on the transcript's terminal marker and on `ctx.Done`; the shipped `streamSSE` pump supplies the bounded buffer, the drop-on-full backpressure and the goleak-clean teardown, asserted by a goleak test |
| T-51-62 | Information Disclosure | the multiplexed status stream | medium | mitigate | It emits only `child_id`, `status`, `last_event_at`, `events` and `duration_sec` — never transcript content — gated by the same identity-scoped ladder as the child mode, and asserted by a test that plants a sentinel string in the transcript and greps the emitted payloads for it |
| T-51-67 | Information Disclosure | a worker's reasoning deltas | medium | mitigate | The translator is called with `showReasoning` false, so the chain of thought is absent from the BYTES, not merely hidden by a client that could be modified. The acceptance criterion reads the argument at the call site rather than counting a token |
| T-51-68 | Denial of Service | a malformed JSONL line | low | mitigate | An unmarshal failure is skipped with a WARN and never yielded as an error, so one bad line written by a crashed writer cannot terminate a stream the operator is watching; a test asserts the surrounding events still arrive |
| T-51-SC | Tampering | npm/pip/cargo installs | high | mitigate | Not applicable: this plan adds zero dependencies and touches no manifest. Any new dependency proposed here would re-trigger the full Package Legitimacy Gate |
</threat_model>

<verification>
Run every gate in WSL, the project's authoritative host — **a Windows result is never a verdict**
(CLAUDE.md, standing operator order):

```
wsl.exe -e bash -lc 'export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"; cd /mnt/d/Repo/Aura && go build ./... && go vet ./...'
wsl.exe -e bash -lc 'export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"; cd /mnt/d/Repo/Aura && go test ./internal/agui/... ./internal/agent/display/... ./cmd/aura/... -count=1'
wsl.exe -e bash -lc 'export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"; cd /mnt/d/Repo/Aura && go test -race ./internal/agui/... ./internal/agent/display/... ./cmd/aura/... -count=1'
wsl.exe -e bash -lc 'export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"; cd /mnt/d/Repo/Aura && bash scripts/coverage_docker.sh'
wsl.exe -e bash -lc 'export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"; cd /mnt/d/Repo/Aura && make quality'
```

There is no live verdict in this plan and that is deliberate: the bytes are provable without a
browser, and the browser verdict belongs to 51-12b's checkpoint, which scores the whole envelope at
once rather than twice.
</verification>

<success_criteria>
- One identity-scoped, 404-hiding route serves a worker's transcript as a translated tailing SSE
  stream that stops on the terminal marker and leaks no goroutine.
- The same route, without a child parameter, serves a per-child status stream carrying no transcript
  content.
- A background `swarm_spawn` dispatch and a synchronous swarm decode through ONE normalizer.
- No new env var, no new migration, no new dependency, no file under `web/`.
- `internal/agui` and `internal/agent/display` are both still at or above 85% and the coverage policy
  file is unchanged.
</success_criteria>

<output>
Create `.planning/phases/51-durable-delegation/51-12a-SUMMARY.md` when done.
</output>
