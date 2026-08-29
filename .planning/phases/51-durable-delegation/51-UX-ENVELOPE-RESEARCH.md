# Delegation UX — delivery envelope + watching a worker in a parallel pane

Input for the phase 51 gap plan (decided by the operator on 2026-08-29 after the 51-09 live drive:
"il transcript deve arrivare in un canvas a parte, in Telegram solo un messaggio, e sul cockpit la
possibilità di vedere l'agente lavorare in una chat parallela — studia gli altri").

Measured trigger (see `live-check/d03/RESULTS.md`): the absent-operator nudge pushed the raw JSON
report (`[{"goal_index":0,"child_id":"w1","status":"ok","summary":"All 10 documents…`) to Telegram
in chunks; the SC#1 record (`attributedWorkerReport`) writes `[Delegated worker report -- goal: …]\n<JSON>`
as an assistant bubble — invisible today only because the record failed (defect A); once 51-09's fix
lands, the raw bubble appears in the cockpit chat. The 51-07 transcript route exists; the cockpit has
no viewer for it.

## How the references do it (sources read locally under D:\tmp on 2026-08-29)

| Reference | Chat surface | Long content | Mobile / gateway | Watching a worker live |
|---|---|---|---|---|
| **hermes-agent** `tools/delegate_tool.py`, `tools/async_delegation.py`, `gateway/run.py`, `tools/process_registry.py` | the completion is **injected as a turn** (`_inject_watch_notification`) carrying a self-contained `[ASYNC DELEGATION … COMPLETE]` block (goal, context, status, duration, summary); the user sees the **model's reply**, never the block | summary capped (`DEFAULT_MAX_SUMMARY_CHARS=24000`, 75% head / 25% tail, footer) and the full text **spilled to a file** with a `read_file offset=` pointer | gateway relays batched `subagent_progress` into ONE status message the adapter **edits in place** (`edit_message`, `_prepare_gateway_status_message`) — no spam | CLI: tree-view lines above the spinner per subagent tool call (`├─ 🔧 tool "preview"`), per-task completion lines `✓ [1/4] label (12s)`; each relayed event carries `subagent_id/parent_id/depth` so a TUI can rebuild the spawn tree and route kill/pause per branch |
| **LibreChat** `client/src/components/Chat/Messages/Content/{ToolCall,AgentHandoff}.tsx`, `components/Artifacts/Artifacts.tsx`, `packages/api/src/prompts/artifacts` | tool calls collapsed (chevron; auto-expand only with output); handoff to a sub-agent = **one line** "Transferred to *agent*", instructions behind the chevron | right-hand **Artifacts** panel (Source/Preview tabs) via `:::artifact`; the prompt reserves it for "substantial, self-contained, >15 lines, reusable" content — never conversational | — | sub-agent output streams inline as the agent's own message; no parallel view |
| **open-claude-cowork** `renderer/renderer.js` | chat = prose only | right **sidebar "Tool calls"**: one card per call, status running/completed, Input/Output `<pre>` collapsible, output truncated to 2000 chars | — (Clawdbot: separate bot) | multi-chat sessions in the left sidebar (switch, not side-by-side); browser `displayMode` inline/sidebar/hidden |
| **assistant-ui 0.15** `examples/with-artifacts`, `examples/with-custom-thread-list` | the tool call renders as a **chip** in the thread (`defineToolkit … render`) | `ArtifactsView` reads **the last tool-call part from thread state** (`useAuiState`) and shows it in a tabbed pane — the canvas is a view over the thread, not a second channel | — | `ThreadListPrimitive` + `useRemoteThreadListRuntime` for multiple threads; one `AssistantRuntimeProvider` per rendered thread, so a second read-only `Thread` beside the main one is supported by construction |
| **codex TUI** `codex-rs/tui/src/app/{agent_navigation,agent_picker,agent_status_feed,loaded_threads,side}.rs` | `ThreadItem::SubAgentActivity` items summarise child activity in the parent thread | — | — | **the closest model**: every subagent is a real thread with a `SessionSource::SubAgent(ThreadSpawn{parent_thread_id})` edge; `/agent` picker + keyboard next/prev switch the chat widget onto a child thread (footer label names the watched agent); `loaded_threads.rs` rebuilds the descendant set on resume by walking spawn edges; `/agent` status feed gives bounded previews; `/side` = ephemeral side conversations |

Common rule across all five: **the chat gets a chip/row, the canvas gets the content, the mobile
channel gets short prose (or one edited status message), and a worker is a thread you can switch to.**

## assistant-ui already ships the sub-agent surface (operator: "c'è già tutto basta solo riusare", 2026-08-29)

Read on 2026-08-29: https://www.assistant-ui.com/docs/tools/multi-agent and
https://www.assistant-ui.com/docs/cloud/ai-sdk#sub-agent-model-tracking, then verified against the
INSTALLED `@assistant-ui/react` 0.15.14 in `web/node_modules` (not the docs site):

- **`ToolCallMessagePart.messages?: readonly ThreadMessage[]`** (`@assistant-ui/core/dist/types/message.d.ts:220`):
  a sub-agent's conversation travels INSIDE the tool-call part that spawned it. The backend puts a
  `messages` array in the tool result; the frontend sees it on the part.
- **`MessagePartPrimitive.Messages`** renders that nested conversation inside the tool's toolkit
  `render` — read-only by construction ("no editing, branching, or composing"), recursive (a
  sub-agent's own tool calls can carry their own `messages`), and with **scope inheritance** (the
  parent's toolkit renderers — our `ToolFallback`/`toolSummary` — apply to the nested messages for free).
- **`ReadonlyThreadProvider`** (exported from `@assistant-ui/react/dist/index.d.ts`, re-exported from
  `@assistant-ui/core/react`) renders a `ThreadMessage[]` OUTSIDE a tool-call context — i.e. in a side
  panel — with the same scope inheritance. That is the parallel pane, with no second runtime.
- **Sub-agent model tracking** (`createSamplingCollector` / `wrapSamplingHandler`, `samplingCalls`
  metadata keyed by `toolCallId`) is an **Assistant Cloud** dashboard feature; no self-hosted
  equivalent is documented. Not applicable — Aura's per-worker token accounting stays in its own
  transcript/metrics.

Consequence for G2 (revises the proposal below): the worker pane is NOT a second `Thread` bound to a
second runtime. It is (a) the `swarm_spawn` tool-call part carrying `messages` = the child transcript
converted to `ThreadMessage[]` (the same `agent.Event` → part mapping `sseAdapter.ts` already does for
the main thread, applied to the transcript's events), rendered nested via `MessagePartPrimitive.Messages`
inside the `swarm_spawn` chip, and (b) the same array handed to `ReadonlyThreadProvider` in the side
panel for the "watch it work" view. Live tail: `ExternalStoreChat` owns the message array, so the
worker SSE (or the transcript offset poll the 51-07 route already supports — push preferred) appends
to the part's `messages` and both views update. The server-side `Translate()` path stays as the wire
format; the browser needs no new protocol.

Also read on request: https://github.com/carlosduplar/multi-agent-mcp (Python, MIT, 0 stars, hackathon
2025) — a *routing-guidance* MCP server (`get_routing_guidance`, `discover_agents`, `list_agents`) that
tells the calling agent which external CLI (Gemini, Aider, Copilot) to run; it spawns nothing, tracks
nothing and returns no transcript. Not a UX reference for this gap; at most a pointer for a future
"external CLI as a swarm worker" idea, which is out of scope here.

## What Aura already has (inventory — nothing here needs inventing)

- Tool-call rendering with per-tool summaries: `web/src/chat/ExternalStoreChat_messages.tsx:367` `ToolFallback`, `web/src/chat/toolSummary.ts` (special-cases `shell_exec`, `send_file`).
- Canvas: `web/src/chat/artifacts/{ArtifactsPanel,PreviewModal}.tsx` (37B), `artifactMeta.ts` renderer gate (`text` = escaped `<pre>`, D-07), `AppShell.tsx` `ArtifactsResizablePanel` inside `ResizablePanelGroup` (`chat-navigation` / `chat-workspace` / artifacts).
- Artifact frame: `internal/agui/translator.go:19` `aura.artifact`, lifted from `Actions.ArtifactDelta` (`internal/agent/llm_agent_events.go:143`); today only `send_file` produces one; `ExternalStoreChat.onArtifact` auto-opens the panel (D-11).
- Transcript: `dumpTranscript` JSONL of `agent.Event` per child; route `GET /api/conversations/{conv}/swarm/{childID}/transcript?offset=` (51-07, identity-scoped, 404-hiding).
- Server-side AG-UI translation: `internal/agui/translator.go:73` `Translate(threadID, runID, idgen, seq iter.Seq2[*agent.Event, error], showReasoning)` — consumes exactly the event type the transcript stores.
- Reattach rail: `GET /agent/runs/{runID}/events` + `Last-Event-ID` replay window (`server_run_resume.go:63`, `RunRegistry` Start/Get/LiveForThread), and the cockpit's `sseResume.ts` reattach pump. Workers are NOT registered as runs today (`grep RunRegistry internal/swarm` → none).
- Present-operator delivery already has the hermes shape: `aura.steer` frame → no message part (`sseAdapter.ts:134`), the model narrates.
- Worker thread id already exists: `<conv>-swarm-<childID>` (seen as `thread_id` in `agent llm call error` logs).

## Proposal for the gap plan (two deliverables, both mostly reuse)

### G1 — Delivery envelope (daemon tier + one cockpit chip)
1. `swarm.DelegationDelivery`: the durable record and the channel nudge carry a **card**, not the JSON:
   status per worker, goal (one line), duration, ≤300 runes of summary, and a pointer to the
   conversation/transcript. The full consolidated report is persisted as an **asset** (`text/markdown`)
   and announced with an `aura.artifact` frame — the same seam `send_file` uses — so it lands in the
   Artifacts panel and survives reload. (hermes: cap + spill; LibreChat: artifact for substantial content.)
2. Telegram (absent operator): **exactly one short, static message** — status, goal in one line, a
   ≤300-rune summary, "dettagli nel cockpit". Operator decision 2026-08-29 ("telegram deve essere
   semplice"): NO in-place-edited status message, NO model narration, NO report body, NO progress
   relay. The hermes gateway pattern is explicitly not adopted for Telegram.
3. Cockpit chat: `swarm_spawn` chip in `toolSummary.ts`/`ToolFallback` — one row per worker with live
   status (LibreChat's `AgentHandoff` shape, assistant-ui's toolkit chip); click → opens the report
   artifact or the worker pane (G2).

### G2 — Watch a worker in a parallel pane (codex model, assistant-ui mechanics — see §assistant-ui above: `messages` on the tool-call part + `MessagePartPrimitive.Messages` + `ReadonlyThreadProvider`; the points below are the original proposal, superseded where they differ)
1. Server: `GET /api/conversations/{conv}/swarm/{childID}/events` — SSE that replays the transcript
   JSONL through `Translate()` and then **tails** it while the worker is live (the JSONL is already
   append-only and `tail -f`-able; the 51-07 route's offset semantics give the cursor). Identity-scoped
   and 404-hiding exactly like the transcript route. Alternative considered: registering workers in
   `RunRegistry` to reuse `/agent/runs/{runID}/events` — rejected for now: RunRegistry carries operator-run
   capacity/reaper semantics and the JSONL is the durable source anyway.
2. Cockpit: a **read-only `Thread`** bound to `<conv>-swarm-<childID>` mounted as a third
   `ResizablePanel` (or as a tab of the Artifacts panel), fed by the existing `sseAdapter` frame → part
   mapping — no composer, no steer. Switching between workers = a picker over the `swarm_spawn`
   chip's rows (codex `/agent`), with the parent chat staying live beside it.
3. Status in the parent chat: the chip updates from the same SSE (running → ok/failed/stalled, duration),
   so the parent thread shows codex's `SubAgentActivity`-style summary without the transcript leaking in.

### Perimeter / not proposed
- No cockpit-side polling loop for delegation status (51-07 prohibition stands): the pane is push (SSE),
  the chip reads the same stream.
- No second delivery channel, no new messaging schema (D-02).
- Kill/pause per worker from the pane (hermes routes controls by `subagent_id`) is out of this gap; note
  it as a follow-up once the pane exists.

## Consolidation follow-ups (operator remark 2026-08-29: "sta repo sta diventando sempre più complessa")

The four defects the 51-09 live drive found are one class — the same constraint expressed in several
places — not four bugs. Candidates for the gap plan or a post-51-08 `/gsd-audit-fix`, listed so they
are not lost:
1. Identity binding once at the claim-loop boundary (`identityctx.WithIdentityID` before any handler),
   never per call site — closes the RLS class for claim/sweep/nudge alike.
2. One sandbox exec path: fold `DockerBackend.Exec` into `ExecStream` (no writer), so cancel/timeout
   always reaches the process group.
3. `compose.yaml` `environment:` block generated from the knob catalog (`config_knobs.go`) with a gate,
   so an `.env` row can never be dead silently.
4. One place that relates the timers (`AURA_SWARM_CHILD_IDLE_SEC`, `AURA_LLM_TOTAL_TIMEOUT_SEC`,
   `AURA_LOOP_MAX_WALLCLOCK_SEC`, lease) — the boot gate 51-09 added is the seed.

## Open questions for `/gsd-discuss-phase` / the planner
1. ~~Card content on Telegram~~ — DECIDED 2026-08-29 by the operator: one short static message, nothing else.
2. Where the worker pane lives: third resizable panel vs tab in the Artifacts panel (mobile: drawer either way).
3. Does the worker pane need the transcript's reasoning deltas (`showReasoning`) or only tool calls + text?
