---
phase: 51-durable-delegation
plan: 12b
type: execute
wave: 8
depends_on: ["51-07", "51-09", "51-10", "51-11", "51-12a"]
files_modified:
  - web/src/chat/workers/workerStream.ts
  - web/src/chat/workers/workerStream.test.ts
  - web/src/chat/workers/useWorkerStatuses.ts
  - web/src/chat/workers/WorkerPane.tsx
  - web/src/chat/workers/WorkerPicker.tsx
  - web/src/chat/workers/workerWatchControls.ts
  - web/src/chat/workers/WorkerWatchProvider.tsx
  - web/src/shell/WorkerPaneShell.tsx
  - web/src/shell/useWorkerPane.ts
  - web/src/AppShell.tsx
  - web/src/chat/displays/types.ts
  - web/src/chat/displays/swarmRow.ts
  - web/src/chat/displays/SwarmReportTable.tsx
  - web/src/i18n/resources.display.ts
  - .planning/phases/51-durable-delegation/live-check/cockpit/RESULTS.md
  - prd.md
  - docs/aura-quality-snapshot.md
autonomous: false
requirements: [SWARM-12, SWARM-10]

estimate:
  tokens: 115000
  raw_tokens: 115000
  tasks: 4
  confidence: low
  # Split out of the single 51-12 plan at plan-check (blocker B3). This half is the cockpit,
  # the live drive and the PRD record; the Go server half is 51-12a and must be merged first,
  # because every stream this plan consumes is served by it.

must_haves:
  truths:
    - "The cockpit shows a worker as a parallel read-only thread: a pane fed by 51-12a's child-mode SSE stream, folded into thread messages through the SHIPPED reduceFrame the main thread already uses, and tailed while the worker is live (SWARM-12 leg 3, PRD Amendment #172 point 3)"
    - "The pane's live updates are PUSH ONLY — one SSE connection per watched worker and one status connection for the whole chip; the cockpit runs no polling loop for delegation status (51-07's prohibition, restated by the UX research perimeter)"
    - "The pane renders tool calls and text, not reasoning deltas — and it does so because 51-12a's server never sends them, so the guarantee holds even against a modified client (UI-SPEC Decision B)"
    - "The pane is a third resizable panel sharing ONE right-rail slot with the Artifacts panel, mutually exclusive by default, reusing the dynamic panelIds mechanism useArtifactsPanel already owns; on mobile it collapses into the SAME Drawer overlay slot ArtifactsDrawer uses (UI-SPEC Decision A + checker Placement note)"
    - "Before the first transcript event the pane shows the connecting copy, never empty chrome (UI-SPEC E2 empty)"
    - "On stream or fetch failure the pane shows the error copy and points at the report artifact as the fallback path (UI-SPEC E2 error)"
    - "The pane's messages are read-only by construction through ReadonlyThreadProvider — no composer, no steer, no branching — and its tool cards inherit the parent toolkit's renderers by scope inheritance, so they look and behave exactly like the main thread's (UI-SPEC E2 populated)"
    - "The pane scrolls vertically inside its panel on desktop and inside a full-height Drawer on mobile (UI-SPEC E2 overflow)"
    - "With exactly one worker the picker still renders with that worker selected, so the pane always names what it shows (UI-SPEC E2 zero-one-many)"
    - "The picker lists one entry per worker with a status dot and a truncated goal, highlights the watched worker, moves focus with the arrow keys and Home/End, activates on Enter or Space, and gives every entry a 44px minimum in literal px (UI-SPEC E3 populated, §3 Mechanics)"
    - "At most AURA_SWARM_MAX_GOALS picker entries wrap onto a second line rather than clip — no entry is ever hidden (UI-SPEC E3 overflow)"
    - "A picker entry's goal truncates to one line and its title attribute carries the full goal (UI-SPEC E3 long-text)"
    - "With one worker and with N workers the picker renders the same entry layout — one selected entry, or N entries (UI-SPEC E3 zero-one-many)"
    - "The chip shows one row per worker from the swarm_spawn call onward with status running, an info dot and the running label, before any worker event arrives — no separate spinner (UI-SPEC E1 loading)"
    - "With zero workers the chip shows the SHIPPED swarm.emptyHeading / swarm.emptyBody copy, never a blank card (UI-SPEC E1 empty)"
    - "A failed row shows the danger dot, the failed label and the error text on one line; a stalled row shows the warning dot, a clock icon and the stalled label — colour is never the only signal (UI-SPEC E1 error)"
    - "A populated row carries status dot and label, the goal on one line, a live duration, a bounded summary, a Watch worker control always, and a View report control only on terminal rows — both controls at least 44px tall in literal px (UI-SPEC E1 populated)"
    - "Rows update independently from the same stream: a finished row carries View report while sibling rows still read running (UI-SPEC E1 partial)"
    - "Rows scroll inside the card and are capped at the goals cap (UI-SPEC E1 overflow)"
    - "Zero rows show the empty copy; one row and N rows share one identical row layout with no single-row special case (UI-SPEC E1 zero-one-many)"
    - "Goal and summary truncate to one line with an ellipsis and the full text stays reachable in the row's existing in-place expand (UI-SPEC E1 long-text)"
    - "The row height minimum comes from the density variable --row-h, not a hardcoded value, so the chip and the picker respect the operator's chosen density like every other cockpit list row; every touch target minimum is a literal px value, never a rem (UI-SPEC Spacing exceptions)"
    - "Accent is used for exactly four things — the Watch worker control, the active picker indicator, focus rings, and the report-artifact pointer — and never for a status dot, a background fill or a generic highlight (UI-SPEC Color)"
    - "No new npm dependency, no new shadcn block, no new font, no new colour hex is introduced — every element composes primitives already installed (UI-SPEC Registry Safety)"
    - "This plan changes no Go package, so no entry of scripts/coverage_package_policy.json can move and the file is deliberately NOT in files_modified — the gate that applies here is the quality snapshot, whose globs cover web/src/**"
    - statement: "While a large transcript is still replaying, the pane keeps the connecting copy until the first message renders — verified visually against a worker with a large transcript (UI-SPEC E2 loading)"
      verification: backstop
    - statement: "A worker mid-tool-call shows that tool's chip in its running state inside the pane — verified visually against a live shell_exec (UI-SPEC E2 partial)"
      verification: backstop
    - statement: "Long tool outputs inside the pane wrap or truncate exactly as in the main thread by scope inheritance — verified visually with a tool result over 2000 characters (UI-SPEC E2 long-text)"
      verification: backstop
  prohibitions:
    - statement: "MUST NOT add a cockpit-side polling loop for delegation status or worker transcripts — the pane and the chip both read the push stream; the server may tail the file, the browser may not poll the route"
      status: required
      verification: "grep -rn 'setInterval\\|refetchInterval' web/src/chat/workers/ returns nothing"
    - statement: "MUST NOT write a second frame-to-part mapping or a second SSE parser — the shipped reduceFrame is the whole client-side mapping"
      status: required
      verification: "grep -rn 'TEXT_MESSAGE_CONTENT' web/src/chat/workers/ matches only inside a test fixture"
    - statement: "MUST NOT put a composer, a steer control or a branch control in the pane — read-only is by construction, not by convention"
      status: required
      verification: "grep -rn 'ComposerPrimitive' web/src/chat/workers/ returns nothing, and ReadonlyThreadProvider wraps the pane's messages"
    - statement: "MUST NOT add an npm dependency, a shadcn block, a font or a colour hex — the UI-SPEC's Registry Safety table declares zero of each"
      status: required
      verification: "git diff --stat -- web/package.json web/package-lock.json web/components.json is empty"
    - statement: "MUST NOT build the web dist on the Windows host — vite build there destroys the committed internal/webui/dist; the bundle is produced by docker compose build aura and extracted from the webbuild stage"
      status: required
      verification: "git status --porcelain internal/webui/dist shows no stray host-built output"
    - statement: "MUST NOT let the worker pane and the Artifacts panel occupy the right rail at the same time — a four-column desktop layout is out of scope and would breach chat-workspace's minimum width"
      status: required
      verification: "the AppShell test asserts that opening one closes the other, and the rule is enforced in the two hooks rather than in JSX"
    - statement: "MUST NOT use a rem-based minimum for any interactive target — the root font is density-driven, so a rem target silently shrinks below 44px at compact density"
      status: required
      verification: "grep -rn 'min-h-\\[44px\\]' web/src/chat/workers/ web/src/chat/displays/SwarmReportTable.tsx matches on every new control"
    - statement: "MUST NOT change a Go file — 51-12a owns the server half and merged first; a Go edit here means the split was violated"
      status: required
      verification: "git diff --name-only for this plan's commits lists no path ending in .go"
    - statement: "MUST NOT derive a pass/fail verdict from a grep on the raw SSE stream — the live verdicts come from the browser, the DB and the container filesystem"
      status: required
      verification: "the Task 3 checkpoint names a non-stream source for each verdict"
    - statement: "MUST NOT run the live verification against a stale container image"
      status: required
      verification: "docker image inspect aura:local --format '{{.Created}}' is newer than the last commit of plans 51-11, 51-12a and this plan's Tasks 1-2"
    - statement: "MUST NOT write the PRD amendment before the live drive — an amendment written on an intention is the exact failure CLAUDE.md's PRD-first principle names"
      status: required
      verification: "the amendment task is ordered AFTER the checkpoint in this plan, and its commit timestamp is later than the RESULTS.md commit"
  artifacts:
    - path: "web/src/chat/workers/workerStream.ts"
      provides: "the EventSource clients folding AG-UI frames into ThreadMessageLike through the shipped reduceFrame, plus the status-stream opener"
      min_lines: 90
    - path: "web/src/chat/workers/WorkerPane.tsx"
      provides: "the read-only parallel thread with its header, empty and error states"
      min_lines: 90
    - path: "web/src/chat/workers/WorkerPicker.tsx"
      provides: "the keyboard-navigable worker switcher"
      min_lines: 80
    - path: "web/src/shell/useWorkerPane.ts"
      provides: "the right-rail slot state and the mutual exclusion against the Artifacts panel"
      min_lines: 60
    - path: ".planning/phases/51-durable-delegation/live-check/cockpit/RESULTS.md"
      provides: "the live drive evidence for the full delivery envelope"
      min_lines: 60
  key_links:
    - from: "web/src/chat/workers/workerStream.ts"
      to: "web/src/chat/sseAdapter.ts reduceFrame"
      via: "the SAME frame to part mapping the main thread uses, applied to the worker's frames"
      pattern: "reduceFrame"
    - from: "web/src/chat/workers/workerStream.ts"
      to: "51-12a's GET /api/conversations/{conv}/swarm/events"
      via: "one EventSource per watched worker, cookies riding same-origin"
      pattern: "swarm/events"
    - from: "web/src/chat/displays/SwarmReportTable.tsx"
      to: "web/src/shell/useWorkerPane.ts"
      via: "the WorkerWatch context, the SourceExplorerProvider seam idiom, so a deeply nested row opens the shell's pane without prop drilling"
      pattern: "useWatchWorker"
    - from: "web/src/chat/workers/useWorkerStatuses.ts"
      to: "51-12a's aura.swarm.worker CUSTOM event"
      via: "one status subscription for the whole chip, reduced into a child-id keyed map"
      pattern: "aura.swarm.worker"
---

<objective>
Give the operator the surface Amendment #172 point 3 promised: **the cockpit shows a worker as a
parallel read-only thread, with the parent chat staying live beside it, and the `swarm_spawn` chip
showing one row per worker with live status.**

Plan 51-12a already serves the bytes. Nothing here is invented either: `reduceFrame` is the shipped
frame-to-part mapping; assistant-ui 0.15.16 exports `ReadonlyThreadProvider` for precisely this
side-panel case, with scope inheritance so the pane's tool cards are the main thread's tool cards;
`useArtifactsPanel` already generalizes the right rail into a dynamic panel-id set; `ArtifactsShell`
is the resizable-panel-plus-drawer trio to mirror; and `SourceExplorerProvider` is the shipped
answer to "a deeply nested render-prop consumer must open one shell-level surface".

Decisions carried: **D-15** (a worker is a thread the operator can switch to), **UI-SPEC Decision A**
(a third resizable panel sharing one right-rail slot with Artifacts, mutually exclusive; a Drawer on
mobile), **UI-SPEC Decision B** (tool calls and text, no reasoning deltas), **51-07's prohibition**
(no cockpit-side polling — the pane is push), **D-02** (no second delivery channel).

Purpose: the operator can watch the work instead of waiting for the report — and then the whole
envelope gets driven in a real browser and written down. Output: one read-only pane, one picker, the
chip the parent chat needed, one evidence file, one PRD amendment recording what was measured.
</objective>

## Artifacts this plan produces

**New files (TypeScript)**
- `web/src/chat/workers/workerStream.ts` (+ `.test.ts`)
- `web/src/chat/workers/useWorkerStatuses.ts`
- `web/src/chat/workers/WorkerPane.tsx`
- `web/src/chat/workers/WorkerPicker.tsx`
- `web/src/chat/workers/workerWatchControls.ts`
- `web/src/chat/workers/WorkerWatchProvider.tsx`
- `web/src/shell/WorkerPaneShell.tsx`
- `web/src/shell/useWorkerPane.ts`

**TS symbols**
- `WorkerStatus` type, `openWorkerStream()`, `openWorkerStatusStream()`, `useWorkerStatuses()`
- `WorkerPane`, `WorkerPicker`, `WorkerWatchProvider`, `useWatchWorker()`
- `WorkerResizablePanel`, `WorkerDrawer`, `useWorkerPane()`, `CHAT_WORKER_PANEL_ID = 'chat-worker'`
- `DisplayChildReport.goal?`, `DisplayChildReport.attempts?`; `SwarmStatus` widened with `'running' | 'stalled' | 'dead_letter'`; `statusIconName(status)`

**i18n keys (en + it, in the existing `swarm.*` namespace)**
`swarm.watch`, `swarm.viewReport`, `swarm.status.running`, `swarm.status.stalled`,
`swarm.status.dead_letter`, `swarm.pane.title`, `swarm.pane.close`, `swarm.pane.connecting`,
`swarm.pane.error`, `swarm.picker.label`, `swarm.columns.goal`, `swarm.columns.duration`,
`shell.resizeWorker`

**New env vars:** none. **New migration:** none. **New npm dependency:** none. **No Go file is touched.**

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
@.planning/phases/51-durable-delegation/51-12a-SUMMARY.md
</context>

<tasks>

<task type="tracer" tdd="true">
  <name>Task 1: TRACER — clicking Watch on a chip row opens the live worker in a read-only pane beside the parent chat</name>
  <files>web/src/chat/workers/workerStream.ts, web/src/chat/workers/workerStream.test.ts, web/src/chat/workers/WorkerPane.tsx, web/src/chat/workers/workerWatchControls.ts, web/src/chat/workers/WorkerWatchProvider.tsx, web/src/shell/WorkerPaneShell.tsx, web/src/shell/useWorkerPane.ts, web/src/AppShell.tsx, web/src/chat/displays/SwarmReportTable.tsx, web/src/i18n/resources.display.ts</files>
  <read_first>
    - `.planning/phases/51-durable-delegation/51-12a-SUMMARY.md` — the route as BUILT: its two modes, its query parameters, the CUSTOM event name, and anything the executor recorded as differing from the plan. This plan consumes that route; read what shipped, not what was planned.
    - `web/src/chat/sseAdapter.ts:151-315` (`reduceFrame`) and `web/src/chat/sseAdapter_parts.ts:1-55` (`AssistantTurnState`, `newAssistantTurn`) — `state.content` is already a `ChatPart[]`, which is exactly a `ThreadMessageLike`'s content. This is the whole client-side mapping; write no second one.
    - `web/src/chat/artifacts/ArtifactsPanel.tsx:1-70` — the panel header treatment (`<h2>` + close button) the pane reuses byte-for-byte, and its accent focus-ring class.
    - `web/src/shell/useArtifactsPanel.ts` IN FULL — the desktop-vs-drawer decision, the persisted open intent, and the dynamic `panelIds` mechanism `useWorkerPane` mirrors.
    - `web/src/shell/ArtifactsShell.tsx` IN FULL — the `ResizableHandle` + `ResizablePanel` + lazy panel + `Drawer` trio `WorkerPaneShell` mirrors, including the comment explaining why registration order determines column order (no `order` prop in v4).
    - `web/src/AppShell.tsx:110-130` and `:436-485` — where `useArtifactsPanel` is called and where `ArtifactsResizablePanel` is mounted. **This file is 560 lines against a 600 ceiling; plan the edit to be a handful of lines** and put every new comment in the new files.
    - `web/src/chat/displays/SourceExplorerContext.tsx` and `web/src/chat/displays/sourceExplorerControls.ts` — the context seam idiom for "a deeply nested render-prop consumer opens one shared surface", which `WorkerWatchProvider`/`useWatchWorker` copies, including why the controls live in a `.ts` leaf.
    - `web/src/chat/displays/SwarmReportTable.tsx` IN FULL (174 lines) — the row structure the Watch control joins and the header comment's rule that colour is never the only signal.
    - `@assistant-ui/react` exports: confirm `ReadonlyThreadProvider` and `fromThreadMessageLike` in `web/node_modules/@assistant-ui/react/dist/index.d.ts` before writing the pane.
    - `.planning/phases/51-durable-delegation/51-UI-SPEC.md` §3 (Decisions A and B, and Mechanics), §Typography, §Color, §Spacing and the Copywriting Contract.
  </read_first>
  <behavior>
    - `openWorkerStream(conv, child)` folds the received frames through `reduceFrame` into a single assistant `ThreadMessageLike` whose content is the accumulated parts, and re-emits it on every frame.
    - Its `close()` removes its listeners and closes the source; a test asserts that after `close()` a further pushed frame produces no emission.
    - The pane renders the connecting copy until the first message part exists, the messages once they do, and the error copy on stream failure — and it never renders a composer.
    - The error copy names the report artifact as the fallback path, so a broken stream still tells the operator where the content is.
    - Opening the pane closes the Artifacts panel and vice versa; the mutual exclusion is decided in the two hooks, so a test can assert it without rendering the shell.
    - The last-open intent survives a reload the same way the Artifacts panel's does, under its own storage key.
    - A Watch control on a chip row opens the pane focused on that row's child id, from anywhere in the message tree, without a prop being threaded through the render-prop boundary.
    - The Watch control and the pane close button are each at least 44px in both dimensions at every density, asserted in literal px.
  </behavior>
  <action>
Create `web/src/chat/workers/workerStream.ts`. `openWorkerStream(conversationId, childId, handlers)`
opens an `EventSource` on 51-12a's route with the child query parameter; cookies ride same-origin, so
no credential option is needed and no auth header is invented. For each received message it
`JSON.parse`s the payload into an `AguiFrame` and calls the SHIPPED `reduceFrame` against a
`newAssistantTurn(childId)` accumulator, then emits
`[{ id: childId, role: 'assistant', content: state.content, status: state.status }]` as
`ThreadMessageLike[]`. It exposes a `close()` that removes listeners and closes the source. Write no
second frame-to-part mapping and no second stream parser — the shipped one is the whole contract, and
a second one is how the pane and the main thread start disagreeing about the same bytes.

Create `web/src/chat/workers/WorkerPane.tsx`. It converts the `ThreadMessageLike[]` with
`fromThreadMessageLike` and renders them inside `ReadonlyThreadProvider`, so tool-call rendering
inherits the parent toolkit's renderers by scope inheritance and the pane gets no input affordance by
construction. Header: reuse `ArtifactsPanel`'s `<h2>` treatment and its close button position and size
byte-for-byte. States: `swarm.pane.connecting` before the first part, `swarm.pane.error` on failure
with a sentence pointing at the report artifact, messages otherwise. The pane body scrolls
vertically; nothing here sets a fixed height.

Create `web/src/chat/workers/workerWatchControls.ts` (the context object plus a `useWatchWorker()`
hook) and `web/src/chat/workers/WorkerWatchProvider.tsx` (the provider), copying
`sourceExplorerControls.ts` / `SourceExplorerContext.tsx`'s split exactly — controls in a `.ts` leaf
so a nested consumer never imports the provider component. The controller exposes
`watchWorker(childId: string)`.

Create `web/src/shell/useWorkerPane.ts` mirroring `useArtifactsPanel.ts`: the same
`matchMedia('(min-width: 64rem)')` desktop gate, the same localStorage-persisted open intent under
its own key, `CHAT_WORKER_PANEL_ID = 'chat-worker'`, and the same dynamic `panelIds` contribution.
Add the watched child id to its state, since the pane must always name what it shows.

Create `web/src/shell/WorkerPaneShell.tsx` mirroring `ArtifactsShell.tsx`: a `WorkerResizablePanel`
(its own `ResizableHandle` + `ResizablePanel` + lazily-imported `WorkerPane`) and a `WorkerDrawer`
reusing the same right `Drawer` the Artifacts panel uses.

In `web/src/AppShell.tsx`: call `useWorkerPane`, compose its panel id into the same `panelIds` array
`useArtifactsPanel` feeds, wrap `{workspace}` in `WorkerWatchProvider` wired to `useWorkerPane`'s open
action, and mount `WorkerResizablePanel` in the SAME right-rail position where
`ArtifactsResizablePanel` mounts — as an either/or, never both. Enforce the mutual exclusion in the
two hooks (opening one calls the other's close), not with a conditional buried in the JSX, so the
rule is testable without a render. Keep this edit to a handful of lines: the file is at 560 of 600.

In `web/src/chat/displays/SwarmReportTable.tsx`: add the Watch control to each row as its own button,
separate from the existing row-expand button so a click target is never ambiguous. Give it
`min-h-[44px] min-w-[44px]` in literal px — a rem minimum silently shrinks below 44px at compact
density — and the accent focus ring class `ArtifactsPanel`'s buttons already use. It calls
`useWatchWorker().watchWorker(report.child_id)`. The row's minimum height comes from the density
variable `--row-h`, not a hardcoded value.

Add the i18n keys `swarm.watch`, `swarm.pane.title`, `swarm.pane.close`, `swarm.pane.connecting`,
`swarm.pane.error` and `shell.resizeWorker` to BOTH the `en` and `it` blocks of
`web/src/i18n/resources.display.ts`, taking the copy verbatim from the UI-SPEC's Copywriting
Contract. The remaining keys land in Task 2.

Do NOT run `vite build` on this host: it writes into `internal/webui/dist` with `emptyOutDir` and
would destroy the committed bundle. Verify with the web test and lint scripts here; the bundle is
produced by `docker compose build aura` in Task 3.
  </action>
  <verify>
    <automated>cd web &amp;&amp; npm run test -- --run src/chat/workers src/shell &amp;&amp; npm run lint</automated>
  </verify>
  <acceptance_criteria>
    - `grep -n 'reduceFrame' web/src/chat/workers/workerStream.ts` matches; `grep -rn 'TEXT_MESSAGE_CONTENT' web/src/chat/workers/` matches only inside the test fixture.
    - `grep -n 'ReadonlyThreadProvider' web/src/chat/workers/WorkerPane.tsx` matches, and `grep -rn 'ComposerPrimitive' web/src/chat/workers/` returns nothing.
    - `grep -rn 'setInterval\|refetchInterval' web/src/chat/workers/` returns nothing.
    - `grep -rn 'min-h-\[44px\]' web/src/chat/workers/ web/src/chat/displays/SwarmReportTable.tsx` matches on the pane close button and the Watch control.
    - A hook-level test asserts that opening the worker pane closes the Artifacts panel and vice versa, without rendering `AppShell`.
    - A `workerStream` test asserts that after `close()` a further pushed frame produces no emission.
    - Both the `en` and the `it` block carry all six new keys — a test or a lint rule that compares the two key sets passes.
    - `git diff --stat -- web/package.json web/package-lock.json web/components.json` is empty.
    - `git status --porcelain internal/webui/dist` is empty — no host-built bundle.
    - `wc -l web/src/AppShell.tsx web/src/chat/displays/SwarmReportTable.tsx` — both at or under 600.
    - `git diff --name-only` lists no path ending in `.go`.
  </acceptance_criteria>
  <done>Clicking Watch on a delegation's chip row shows that worker's live transcript in a read-only pane beside the parent chat, fed by one SSE connection, with the Artifacts panel yielding the rail.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: The chip the parent chat needed — one row per worker with live status, goal and duration, and a picker to switch between them</name>
  <files>web/src/chat/displays/types.ts, web/src/chat/displays/swarmRow.ts, web/src/chat/displays/SwarmReportTable.tsx, web/src/chat/workers/workerStream.ts, web/src/chat/workers/useWorkerStatuses.ts, web/src/chat/workers/WorkerPicker.tsx, web/src/chat/workers/WorkerPane.tsx, web/src/i18n/resources.display.ts</files>
  <read_first>
    - `.planning/phases/51-durable-delegation/51-12a-SUMMARY.md` — the CUSTOM event name and the exact field set the status mode emits (`child_id`, `status`, `last_event_at`, `events`, `duration_sec`). Decode against what shipped.
    - `web/src/chat/displays/swarmRow.ts` IN FULL (40 lines) — `SwarmStatus`, `SWARM_DOT_CLASS`, `SWARM_STATUS_KEY`, `isSwarmStatus`, and the rule that an out-of-enum status degrades to the danger dot.
    - `web/src/chat/displays/SwarmReportTable.tsx` IN FULL — the collapsed-row grid template `grid-cols-[3rem_1fr_8rem_2fr]` this task extends, and the header comment's rule that colour is never the only signal.
    - `web/src/chat/durationFormat.ts` IN FULL (73 lines) — `useElapsed`/`formatElapsed`, to be reused verbatim for the duration cell; do not write a second formatter.
    - `web/src/chat/toolSummary.ts` — the `cap()`/truncate helpers the row already uses for one-line text.
    - `web/src/chat/artifacts/ArtifactsPanel.tsx` — the tab strip the picker is styled after, per the UI-SPEC's Placement note (a plain row of buttons, not a new tablist treatment).
    - `.planning/phases/51-durable-delegation/51-UI-SPEC.md` §1 (the chip, including the exact status/icon/colour mapping and the two separate 44px controls), §3 Mechanics (the picker's roles and keyboard contract), and §"Checker recommendations".
    - `internal/config/config.go:95-97` — the live `AURA_SWARM_MAX_GOALS` cap (default 8) the picker's entry ceiling refers to.
  </read_first>
  <behavior>
    - `isSwarmStatus` accepts `running`, `stalled` and `dead_letter` in addition to the three shipped values; `running` maps to the info dot, `stalled` and `needs_user_input` both map to the warning dot but carry different icons and different labels; an unknown value still degrades to the danger dot with the unknown label.
    - `statusIconName(status)` is a pure function returning the lucide icon name that differentiates the two warning states, unit-testable without a render.
    - A row renders index, child id, status dot and label, goal on one line, a duration, and a bounded summary; the duration ticks while the status is running and freezes on a terminal status.
    - The Watch control is present on every row; the View report control is present only on a terminal row.
    - `useWorkerStatuses(conversationId)` opens exactly ONE connection for the whole chip and exposes a map from child id to status; it opens no connection when the conversation id is empty and closes on unmount.
    - The picker renders one entry per known worker with a status dot and a truncated goal carrying the full goal in its title, highlights the watched one, moves focus with the arrow keys, jumps with Home and End, activates on Enter or Space, and never hides an entry — entries wrap.
    - With exactly one worker the picker still renders that one entry, selected.
    - Zero rows render the shipped empty copy; one row and N rows share one identical layout.
  </behavior>
  <action>
In `web/src/chat/displays/types.ts` add the optional `goal` and `attempts` fields to
`DisplayChildReport`, mirroring what 51-12a added to the Go side.

In `swarmRow.ts` widen `SwarmStatus` with `'running' | 'stalled' | 'dead_letter'` and add their
entries to `SWARM_DOT_CLASS` (`bg-info` for running, `bg-warning` for stalled, `bg-danger` for
dead_letter) and `SWARM_STATUS_KEY`; extend `isSwarmStatus` with the same three literals. Add the
pure `statusIconName(status)` helper HERE — not in the `.tsx` — so the mapping is unit-testable.

In `SwarmReportTable.tsx`: extend the collapsed grid template with a goal cell and a duration cell,
render the goal truncated to one line with the existing `cap`/`truncate` helper, render the duration
through `useElapsed`/`formatElapsed` with `tabular-nums` in the mono face, and render the status cell
as dot plus a 16px lucide icon plus the label so colour is never the only signal. Add the View report
control beside the Watch control, present only on a terminal status, calling the watch context's
report action, and give it the same literal-px 44px minimum. Take the row minimum height from
`--row-h`. If the file crosses 600 lines, extract the row into `SwarmReportRow.tsx` rather than
trimming.

Add `openWorkerStatusStream(conversationId)` to `workerStream.ts` beside `openWorkerStream`, and
create `web/src/chat/workers/useWorkerStatuses.ts` — a hook opening exactly one such stream and
reducing 51-12a's worker events into a `ReadonlyMap<string, WorkerStatus>`. This is a push
subscription: no timer, no re-fetch, no repeated request. It opens nothing for an empty conversation
id and closes on unmount.

Create `web/src/chat/workers/WorkerPicker.tsx` — a plain row of buttons styled like the shipped
Artifacts tab strip per the UI-SPEC's Placement note, with `role="tablist"` semantics, roving
tabindex, the arrow/Home/End/Enter/Space contract, an accent underline plus `aria-selected` on the
active entry, a status dot mirroring the chip's own dot, a truncated goal with the full goal in
`title`, wrapping rather than clipping, and a literal-px 44px minimum per entry. Mount it under the
pane header in `WorkerPane.tsx`, fed by `useWorkerStatuses`, so the pane always names what it shows
even with a single worker.

Add the remaining i18n keys to BOTH `en` and `it`: `swarm.viewReport`, `swarm.status.running`,
`swarm.status.stalled`, `swarm.status.dead_letter`, `swarm.picker.label`, `swarm.columns.goal`,
`swarm.columns.duration` — copy verbatim from the UI-SPEC Copywriting Contract.

Tests: a `swarmRow` unit test for the widened status map and the icon helper; a `SwarmReportTable`
render test covering zero rows, one row, N rows, a terminal row with both controls, a running row
with only the Watch control, and a stalled row's icon; a `useWorkerStatuses` test asserting exactly
one connection for N workers and a closed connection on unmount; a `WorkerPicker` keyboard test.

The bundle is still NOT built on this host — `docker compose build aura` in Task 3 produces it.
  </action>
  <verify>
    <automated>cd web &amp;&amp; npm run test -- --run src/chat/displays src/chat/workers &amp;&amp; npm run lint</automated>
  </verify>
  <acceptance_criteria>
    - `grep -rn 'setInterval\|refetchInterval' web/src/chat/workers/` returns nothing.
    - `grep -n 'useElapsed\|formatElapsed' web/src/chat/displays/SwarmReportTable.tsx` matches — no second duration formatter was written.
    - `grep -n 'aria-selected' web/src/chat/workers/WorkerPicker.tsx` matches, and the keyboard test asserts ArrowLeft, ArrowRight, Home, End, Enter and Space.
    - The `useWorkerStatuses` test asserts exactly one opened connection for a six-worker conversation, and that it is closed on unmount.
    - `git diff --stat -- web/package.json web/package-lock.json` is empty.
    - `wc -l web/src/chat/displays/SwarmReportTable.tsx` is at or under 600 (or the row was extracted).
    - `git diff --name-only` lists no path ending in `.go`.
    - `cd web && npm run lint` reports no new error.
  </acceptance_criteria>
  <done>A background delegation shows one row per worker from the dispatch onward, with live status, goal, duration and two unambiguous controls; the picker switches the pane between workers while the parent chat stays live.</done>
</task>

<task type="checkpoint:human-verify" gate="blocking">
  <name>Task 3: LIVE — drive the whole delivery envelope on the running stack, in the browser</name>
  <precondition>Plans 51-11 and 51-12a are merged AND the running `aura:local` image was built from a HEAD containing them and this plan's Tasks 1-2. Confirm BEFORE driving: `git log --oneline -1 -- internal/swarm/delegation_card.go` and `git log --oneline -1 -- internal/agui/server_swarm_events.go` both return a commit, and `docker image inspect aura:local --format '{{.Created}}'` is newer than both. If any check fails, run `docker compose build aura &amp;&amp; docker compose --profile sandbox up -d aura` and re-check. The cockpit bundle is built inside that image — a host `vite build` would destroy the committed dist and must not be used. Recreating `aura` strands its netns sidecars: recreate `arcadedb-mcp`, `pim-mcp` and `whatsapp` too, and capture `docker logs aura` to a file BEFORE any restart, because a recreate discards them.</precondition>
  <files>.planning/phases/51-durable-delegation/live-check/cockpit/RESULTS.md</files>
  <read_first>
    - `.planning/phases/51-durable-delegation/live-check/d03/RESULTS.md` IN FULL — the evidence format, the container-knob dump that opens it, and the discipline that a verdict comes from the daemon log, Postgres or the filesystem, never from the SSE stream.
    - `.planning/phases/51-durable-delegation/live-check/envelope/RESULTS.md` — plan 51-11's own drive, so this one does not repeat what it already proved and instead scores the whole envelope together.
    - `CLAUDE.md` §DEFINITION OF DONE and §"Real E2E, not smoke".
  </read_first>
  <what-built>
Plans 51-11 and 51-12a plus this plan's Tasks 1-2 are the whole delivery envelope. 51-11: a finished
or dead-lettered background delegation records a CARD per worker (not raw report JSON) into
`aura.conversation_turns`, persists the full report as an owned `text/markdown` asset on the origin
thread, gives every worker a unique stable child id that closes its transcript with a terminal
marker, exposes a Deferred `swarm_status` tool to the parent model, and sends **ONE aggregated
Telegram message per fan-out** when the LAST worker of a `swarm_spawn` call reaches a terminal state.
51-12a: a `GET /api/conversations/{conv}/swarm/events` SSE route in two modes — one child's
transcript replayed and tailed through the shipped `agui.Translate`, and a multiplexed per-child
status stream. Tasks 1-2: a read-only worker pane in a right-rail slot shared exclusively with the
Artifacts panel, a keyboard-navigable picker, and a chip that renders one row per worker with live
status, goal, duration and two separate 44px controls. **None of the cockpit half has been seen in a
browser.**
  </what-built>
  <how-to-verify>
Bring the stack up (`docker compose --profile sandbox up -d`), confirm the precondition, and read the
live routed model out of `aura.settings` rather than assuming it.

Drive ONE real two-worker background delegation from the cockpit in a browser, with two goals that
each take long enough to watch and that finish at clearly different times. Then collect, per verdict,
from the source named:

1. **A card per worker in the chat, not JSON** — the browser: screenshot the thread showing the
   dispatch chip with two rows and, below it, the terminal record for EACH worker. Cross-check with
   `psql`: the `aura.conversation_turns` rows for that conversation. A row starting with a bracket
   and containing a goal-index key is a FAIL. Two cards is the expected count, not one.
2. **The full report as an artifact in the Artifacts panel** — the browser: open Artefatti on that
   thread, confirm the report rows are listed and preview. Cross-check with `psql`: each asset row's
   `file_name`, `mime_type`, `source_kind` and `thread_id`.
3. **ONE Telegram message for the fan-out, and not before** — the phone, plus `psql` on
   `aura.steer_queue` (`kind = 'delegation_result'`, `fanout_key`, `nudged_at`) and
   `pending_notifications`. Two things must both hold. **(a) The negative half:** while worker 1 is
   finished and worker 2 still `running` in `aura.ingestion_jobs`, worker 1's steer row exists,
   undrained, with `nudged_at IS NULL`, and NOTHING has arrived on the phone. A message at that
   moment is a FAIL. **(b) The positive half:** after worker 2 finishes, ONE message arrives carrying
   TWO status lines and the closing line — not two messages, not chunked, not the report body — and
   both steer rows carry the same `fanout_key` and the same `nudged_at`. Paste both rows and the
   message. State plainly whether device arrival was confirmed by you or only that the send fired.
4. **The pane tails a live worker** — the browser: while worker 1 is mid-`shell_exec`, click Watch on
   its row. Record that the pane opens in the right rail, that the Artifacts panel closed, that the
   connecting copy showed before the first message, that the tool card appears in its running state,
   and that new events arrive without a reload. Then use the picker to switch to worker 2 and confirm
   the parent chat stayed live throughout. Screenshot each. Cross-check the pane's content against
   `docker exec aura tail -n 5 $AURA_RUN_DIR/<conv>/swarm/<child>.jsonl`. Also open the browser
   devtools Network tab and confirm exactly ONE EventSource per watched worker plus ONE status stream
   — any repeating request to the swarm routes is a polling regression and a FAIL.
5. **`swarm_status` answers from facts** — in the same conversation, while a worker is still running,
   ask Aura *"puoi vedere l'avanzamento?"*. Her answer must name the child id, its status, its elapsed
   time and something the worker actually did. Cross-check against the transcript file and the job
   row. The 2026-08-29 answer to beat is verbatim *"non c'è un endpoint per pollare lo stato
   intermedio di un worker già accodato"*.
6. **The three backstop considerations** — while you are in the pane, deliberately check the three
   `verification: backstop` truths in this plan's front matter: the connecting copy holding while a
   large transcript replays, a mid-tool-call tool chip in its running state, and a tool result over
   2000 characters wrapping or truncating as it does in the main thread. Record each as observed or
   as still open; do not mark one closed you did not look at.

Score the run against CLAUDE.md's DoD bar and record everything in
`.planning/phases/51-durable-delegation/live-check/cockpit/RESULTS.md` in the d03 format, including
the image digest, the routed model, the container knob dump, and a "what this does NOT prove"
section. At minimum that section names: a fan-out with a worker parked in `awaiting_input` (whose
named cost is that the phone stays silent about the finished siblings), the pane's behaviour across a
daemon restart mid-worker, and the card's rendering on a phone.
  </how-to-verify>
  <resume-signal>Reply "approved" once RESULTS.md is written, all six verdicts pass and the DoD score is recorded, or describe exactly which verdict failed and paste the evidence you read.</resume-signal>
  <acceptance_criteria>
    - `.planning/phases/51-durable-delegation/live-check/cockpit/RESULTS.md` exists and names, for each of the six verdicts, the exact source it was read from — browser screenshot, table and column, container path, or device.
    - Verdict 3 records BOTH halves explicitly: the mid-flight silence with the unnudged row, and the single two-line message with the two rows sharing one `fanout_key` and one `nudged_at`.
    - The file records the image digest and build time and the routed model read from `aura.settings`.
    - The file records the devtools observation for verdict 4 explicitly, including the connection count.
    - Each of the three backstop considerations is marked observed or still open, individually.
    - The file carries a "what this does NOT prove" section and a DoD score.
    - No verdict in the file is sourced from a grep over the SSE stream.
  </acceptance_criteria>
  <done>The operator has watched a real worker work in the cockpit, switched between two of them, and confirmed the per-worker cards, the artifacts, the ONE fan-out Telegram message that waited for the slower worker, and a fact-based progress answer — with the evidence written down.</done>
</task>

<task type="auto">
  <name>Task 4: Record the measurement in the PRD and re-attest the quality snapshot — written LAST, from Task 3's numbers</name>
  <files>prd.md, docs/aura-quality-snapshot.md</files>
  <read_first>
    - `.planning/phases/51-durable-delegation/live-check/cockpit/RESULTS.md` — the file Task 3 just wrote. This amendment records THOSE numbers; if a verdict is missing from that file it is missing from the amendment too, stated as such.
    - `prd.md` §Amendment #172 in full (`grep -n "Amendment #172" prd.md`, then read the section) — this task appends the follow-up amendment that records what shipped and what it measured, and it must not restate what #172 already decided.
    - `CLAUDE.md` §"PRD-first principle" — an amendment records a measurement and states plainly what the measurement does NOT prove.
    - `docs/aura-quality-snapshot.md` — read the rows whose CI-gate-path globs cover `internal/swarm/**`, `internal/agui/**`, `internal/agent/display/**`, `internal/documents/**`, `internal/steer/**` and `web/src/**`.
    - `scripts/quality_snapshot_gate.sh` — the exact gate this task must satisfy before the phase-close push.
  </read_first>
  <action>
**This task runs AFTER Task 3's checkpoint is approved, and it is the last task of the plan.** That
ordering is the point: an amendment written before the drive records an intention, which is the exact
failure CLAUDE.md's PRD-first principle names. Do not draft it early.

Append a new PRD amendment recording what this gap SHIPPED and what Task 3 MEASURED. Take the next
amendment number from the highest one currently in `prd.md`, never from this plan. It states:

  - the delivery envelope's four legs as built — a card per worker, the report artifact, ONE Telegram
    message per fan-out, and the cockpit pane;
  - the fan-out decision itself, with the operator's words, the substrate it needed (a deterministic
    key in the job payload plus one nullable column on `aura.steer_queue`, whose migration slot was
    read from the directory at creation), and the negative verdict that proved the gate holds;
  - the three wire contracts other code now depends on: the queued `swarm_spawn` result object, the
    transcript's terminal marker, and the `aura.swarm.worker` CUSTOM event name;
  - the `swarm_status` tool as SWARM-10's parent leg;
  - the numbers Task 3 produced.

It states plainly what it does NOT prove — at minimum: a fan-out holding a worker parked in
`awaiting_input` (the named cost of "uno per fan-out": the phone stays silent about the finished
siblings until the question is answered or the row's TTL expires it), the card's rendering on a phone,
the pane's behaviour across a daemon restart mid-worker, the artifact's size at the step and
wall-clock budget caps, and the fact that Telegram remains the only shipped Deliverer so the channel
fan-out's choice has still never been exercised between two candidates.

Update `docs/aura-quality-snapshot.md`: for EVERY row whose CI-gate-path glob matches a file this
phase changed, set `Last measured` to today and PREPEND a re-attestation note — a fresh measurement
where the metric moved, otherwise a metric-neutral justification naming exactly what changed and why
the number cannot move. Keep the prior notes, prefixed as prior.

Verify locally BEFORE the push; the command must print an `ok` line naming the checked row count.
  </action>
  <verify>
    <automated>wsl.exe -e bash -lc 'export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"; cd /mnt/d/Repo/Aura &amp;&amp; AURA_QUALITY_CHANGED_FILES="$(git diff --name-only origin/master..HEAD)" AURA_QUALITY_BASE_DATE="$(git log -1 --format=%cs origin/master)" bash scripts/quality_snapshot_gate.sh'</automated>
  </verify>
  <acceptance_criteria>
    - The quality-snapshot gate prints its `ok` line with a non-zero checked row count.
    - `grep -n "$(date +%Y-%m-%d)" docs/aura-quality-snapshot.md` matches on every row whose glob covers a file this phase changed.
    - The new PRD amendment cites the evidence path `.planning/phases/51-durable-delegation/live-check/cockpit/RESULTS.md` and carries a section stating what the measurement does not prove, including the parked-worker fan-out case.
    - The amendment number is strictly greater than every other amendment number already in `prd.md`.
    - `git log --format='%h %cs %s' -- prd.md .planning/phases/51-durable-delegation/live-check/cockpit/RESULTS.md` shows the RESULTS.md commit strictly earlier than the amendment commit.
  </acceptance_criteria>
  <done>The PRD records what was measured, with its date, its evidence and its perimeter; the quality snapshot gate is green.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| worker LLM output → the operator's browser | the pane renders a worker's own text and tool output inside the cockpit origin |
| the pane's read-only posture | a second input surface onto a running worker would be unaudited |
| the right-rail slot | two panels competing for one slot is a layout failure, not only a visual one |

## STRIDE Threat Register (ASVS L1, block on: high)

| Threat ID | Category | Component | Severity | Disposition | Mitigation Plan |
|-----------|----------|-----------|----------|-------------|-----------------|
| T-51-60 | Elevation of Privilege | worker text rendered in the cockpit origin | high | mitigate | The pane renders through the SAME assistant-ui part renderers as the main thread by scope inheritance — no `dangerouslySetInnerHTML`, no new markdown host, no new renderer. The docx-preview in-origin XSS lesson applies: a new render path is a new escape surface, so this plan opens none, and an acceptance criterion asserts no second frame-to-part mapping exists |
| T-51-61 | Spoofing | the pane's read-only posture | medium | mitigate | `ReadonlyThreadProvider` forbids composing and branching by construction; no composer primitive is imported into the workers directory, and an acceptance criterion greps for its absence — so the pane cannot become an unaudited second input to a worker |
| T-51-63 | Denial of Service | one connection per watched worker | low | accept | The chip opens one status stream and the pane one child stream, so at most two concurrent EventSources per tab regardless of the worker count — well inside the browser's per-origin limit. The alternative, one stream per row, is explicitly not built, and a test asserts one connection for a six-worker conversation |
| T-51-69 | Tampering | the committed `internal/webui/dist` bundle | medium | mitigate | A host `vite build` writes into that directory with `emptyOutDir` and destroys the committed bundle. The bundle is produced only by `docker compose build aura`; a prohibition, an acceptance criterion on `git status --porcelain internal/webui/dist`, and the checkpoint's precondition all enforce it |
| T-51-70 | Repudiation | a PRD amendment written before the drive | medium | mitigate | The amendment task is ordered after the blocking checkpoint and an acceptance criterion compares commit timestamps, so the PRD cannot record a measurement that has not happened — the failure CLAUDE.md's PRD-first principle names |
| T-51-SC | Tampering | npm/pip/cargo installs | high | mitigate | Not applicable: this plan adds zero dependencies, and the UI-SPEC's Registry Safety table declares zero new shadcn blocks and zero third-party registries. An acceptance criterion asserts `web/package.json` and its lockfile are untouched |
</threat_model>

<verification>
Web gates (these run on the host; they are not Go and WSL buys nothing here):

```
cd web && npm run lint
cd web && npm run test
```

The bundle is built ONLY by `docker compose build aura` (~30 s warm). **Never run `vite build` on
this host** — it writes into `internal/webui/dist` with `emptyOutDir` and destroys the committed
bundle.

The Go suite is not this plan's to run — no Go file is touched — but the phase-level gates still
apply at close and run in WSL, the project's authoritative host:

```
wsl.exe -e bash -lc 'export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"; cd /mnt/d/Repo/Aura && make quality'
wsl.exe -e bash -lc 'export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"; cd /mnt/d/Repo/Aura && bash scripts/coverage_docker.sh'
```

Task 3's live drive in a real browser is the only verdict that closes this plan.
</verification>

<success_criteria>
- The operator can watch a live worker work in a read-only pane beside the parent chat, switch
  between workers with a picker, and never sees an input affordance in that pane.
- The `swarm_spawn` chip shows one row per worker from the dispatch onward with live status, goal,
  duration, a Watch control on every row and a View report control on terminal rows.
- The pane and the chip are push-only — devtools shows no repeating request to the swarm routes.
- One `swarm_spawn` call produced exactly ONE Telegram message, and produced none while a worker of
  that fan-out was still running.
- No new npm dependency, no new shadcn block, no new env var, no new migration, no Go file touched.
- The PRD records the measurement with its date, its evidence and its perimeter — written after the
  drive, never before — and the quality snapshot gate is green.
</success_criteria>

<output>
Create `.planning/phases/51-durable-delegation/51-12b-SUMMARY.md` when done.
</output>
