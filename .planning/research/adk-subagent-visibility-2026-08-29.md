# ADK as a reference for swarm-worker visibility (Aura) — 2026-08-29

Sources, read 2026-08-29. Docs: `https://adk.dev/llms.txt` pages fetched raw (curl) plus `google/adk-docs@1203686`.
Code (shallow clones in `D:\tmp`): `google/adk-python@c3d3730`, `google/adk-go@0da17d5`, `google/adk-web@9a154af`
(all 2026-08-28). Quotes are verbatim; code claims cite `repo:path:line`. Aura is not ADK — section 3 names only the
ideas worth borrowing; section 4 lists what neither the pages nor the code establish.

## 1. ADK's model in five sentences

1. Every occurrence — user input, LLM text, tool call, tool result, state change, control signal, error — is one `Event`
   with `author` ("'user' or agent name"), `invocation_id`, `id`, `timestamp`, `branch` ("Hierarchy path"), `partial`/
   `turn_complete`, `long_running_tool_ids`, and `actions` (`state_delta`, `artifact_delta`, `transfer_to_agent`,
   `escalate`, `skip_summarization`) [events].
2. One `Runner` drives a cooperative loop: the agent yields an event, the Runner commits its actions through
   `SessionService`/`ArtifactService`, forwards the event upstream to the UI, then lets the agent resume; `partial`
   events are forwarded but never committed (adk-go `runner/runner.go:349` "only commit non-partial event") [event-loop].
3. A sub-agent has no channel of its own: a workflow/custom agent calls `sub.run_async(ctx)` and re-yields the child's
   events into the same loop; `ParallelAgent` merges children through a queue and each child **blocks until the runner
   has processed its event** (adk-python `agents/parallel_agent.py:63-66`, adk-go `parallelagent/agent.go` ack channel).
4. Delegation has three shapes: **transfer** (`transfer_to_agent`, the child takes the conversation), **`AgentTool`**
   (a *separate* `Runner` + `InMemorySessionService`; "The nested run's events are not forwarded to the caller; only the
   last event's content becomes the response", adk-python `tools/agent_tool.py:308-311`), and ADK 2.0 **modes**
   `chat`/`task`/`single_turn` where the sub-agent runs inline as a node and "Direct usage of `AgentTool` is
   discouraged" (`agent_tool.py:118-127`).
5. Long work is not done inside a tool: `LongRunningFunctionTool` returns an id and the run pauses while the *client*
   polls and feeds `FunctionResponse`s back; artifacts are versioned blobs announced only by `artifact_delta:
   {filename: version}`; cancel is an `AbortSignal` (TS), resume replays committed events by `invocation_id` (Py/Kotlin);
   the dev UI renders every non-user author as `'bot'` (adk-web `chat.component.ts:2727`) and offers an Events tab and
   an OTel span-tree Trace tab, not a per-sub-agent view.

## 2. Concern → what ADK does → URL → verbatim quote (code refs where the source adds to the page)

| Concern | What ADK does | URL / code | Verbatim (short) |
|---|---|---|---|
| Parent observing a sub-agent (custom agent) | Parent generator re-yields child events; no observation API | https://adk.dev/agents/custom-agents/index.md | "invoke sub-agents … using their `run_async` method and yield their events" / "`yield` events produced by sub-agents or its own logic back to the runner." |
| Parent observing (ParallelAgent) | Children on isolated branches; events interleaved; parent gets results at the end; a direct child's `escalate` cancels siblings | https://adk.dev/agents/workflow-agents/parallel-agents/index.md; adk-python `agents/parallel_agent.py:43-49,63-66,87-90` | "no automatic sharing of conversation history or state between these branches" / "The order of results may not be deterministic." / code: `await queue.put((event, resume_signal)); await resume_signal.wait()` |
| Parent observing (2.0 modes) | `task`/`single_turn` see only their branch; parent proceeds after all finish | https://adk.dev/workflows/collaboration/index.md | "each agent only sees events from its own branch … and cannot see what its peer agents are doing. Once all parallel branches complete, the parent agent receives the collected results and can proceed." |
| `author` | Every event names its producer; tool results authored by the requesting agent; UI collapses to user/bot | https://adk.dev/events/index.md; adk-web `chat.component.ts:2727`, `UiEvent.ts:100-101` | "the event `author` is typically the agent that requested the tool call" / code: `const role = event.author === 'user' ? 'user' : 'bot';` / `return this.event?.author ?? 'root_agent';` (shown only in the Events tab) |
| `branch` | Dot path `parent.child`; ParallelAgent sets `agent.sub_agent`; 2.0 node tools add `name@run_id` (function-call id) | https://adk.dev/events/index.md; adk-python `agents/invocation_context.py:141-148`, `events/_branch_path.py`, `tools/agent_tool.py:415-420`; adk-go `parallelagent/agent.go` | "`branch: Optional[str] # Hierarchy path`" / code: "The format is like agent_1.agent_2.agent_3, where agent_1 is the parent of agent_2" / "`'parent_agent@1.collect_user_info_tool@2.sub_workflow'`" / `_BranchPath.create_sub_branch(base_branch, name=self.agent.name, run_id=fc_id)` |
| Correlation | One `invocation_id` spans all agent runs of a user turn; `is_final_response` may be true once per agent | https://adk.dev/runtime/event-loop/index.md; adk-python `events/event.py:288-296` | "all tied together by a single `invocation_id`" / code: "when multiple agents participate in one invocation, there could be one event has `is_final_response()` as True for each participating agent." |
| Result → parent: `AgentTool` | Own Runner, in-memory session, forwarding artifact service; only `state_delta` and last content come back; streaming forced off | https://adk.dev/tools-custom/function-tools/index.md; adk-python `tools/agent_tool.py:264-268,308-336`; adk-go `tool/agenttool/agent_tool.go:143-195` | "Agent B's answer is **passed back** to Agent A" / code: "The nested run's events are not forwarded to the caller; only the last event's content becomes the response … so always run unary." |
| Result → parent: transfer | Child owns the conversation; Go runner re-picks the agent from the last non-user author | https://adk.dev/tools-custom/function-tools/index.md; adk-go `runner/runner.go:709-741` | "Agent A is effectively out of the loop." / code: `findAgentToRun` walks `session.Events()` backwards and returns the last transferable author |
| Result → parent: 2.0 modes | `single_turn` returns "with result"; `task` via `finish_task`; sub-agents auto-wrapped as `_SingleTurnAgentTool`/`_TaskAgentTool`; task tool must not run in parallel | https://adk.dev/workflows/collaboration/index.md; adk-python `agents/llm_agent.py:1278-1300`, `tools/agent_tool.py:466-470` | "Automatic (via `finish_task`)" / "Automatic (with result)" / code: "IMPORTANT: This tool delegates execution to a specialized agent. Do NOT call this tool in parallel with any other tools." |
| What becomes a bubble | `is_final_response()` filters tool calls/results/partials | https://adk.dev/events/index.md | "Filters out intermediate steps, such as tool calls and partial streaming text, from the final user-facing message(s)." / "Rely on this helper method in your application/UI layer" |
| Artifact save/version | Int version per filename; session or `user:` scope; REST list/versions/metadata | https://adk.dev/artifacts/index.md; adk-python `cli/api_server.py:1591-1770` | "Each time you save an artifact with the same filename, a new version is created." / routes: `/apps/{app}/users/{u}/sessions/{s}/artifacts/{name}/versions/{v}` |
| Artifact → UI | `artifact_delta` on the next event; the dev UI fetches the version and renders it inline under that event | https://adk.dev/events/index.md; adk-web `chat.component.ts:1625-1631,2013-2045`, `artifact-tab.component.ts:127` | "`{filename: version}`" / "# UI might refresh an artifact list" / code: `this.renderArtifact(key, apiEvent.actions.artifactDelta[key], uiEvent)` → placeholder then `artifactService.getArtifactVersion(...)`; artifact tab has `downloadArtifact` |
| Streaming vs commit | Partial forwarded, not committed; SSE yields partial chunks AND a final aggregated event | https://adk.dev/runtime/event-loop/index.md; adk-python `agents/_streaming_mode.py:52-75` | "**forwards it immediately** upstream (for UI display) but **skips processing its `actions`**" / code: "you will receive both partial text chunks AND a final aggregated text event. To avoid displaying text twice: …" |
| Dev web UI | Chat, sessions, state, event history; trace = OTel spans per session; dev-only | https://adk.dev/runtime/web-interface/index.md; adk-python `cli/dev_server.py:796-822`; adk-web `trace-tab/trace-tree.component.ts:142` | "**Event history**: Inspect all events generated during agent execution" / "***not meant for use in production deployments***" / code: `/dev/apps/{app_name}/debug/trace/session/{session_id}` returns `{name, span_id, parent_span_id, attributes…}`; UI `buildSpanTree` |
| Background / ambient | Triggered runs, no human; results to logs/Pub/Sub; per-process semaphore | https://adk.dev/runtime/ambient-agents/index.md | "you need to route their outputs to a notification channel" / "Trigger endpoints use a semaphore to limit the number of concurrent agent invocations." |
| Cancel | `AbortSignal` through Runner/agents/AgentTool/MCP/model; committed events stay | https://adk.dev/runtime/cancel/index.md | "Cancellation in ADK is non-destructive: events already committed to the session remain persisted." / "**No partial events:** Events that were in progress but not yet yielded are discarded." |
| Resume | Replays committed events per agent by `invocation_id`; tools at-least-once; not from web UI/CLI | https://adk.dev/runtime/resume/index.md | "**Parallel Agent**: Determines which sub-agents have already completed and only runs those that have not finished." / "run ***at least once***, and may run more than once" |
| Long-running tools | Returns id, run pauses, client polls and sends `FunctionResponse`; declaration gets a no-repeat note | https://adk.dev/tools-custom/function-tools/index.md; adk-python `tools/long_running_tool.py:31-35`; adk-web `long-running-response.ts` | "designed to help you start and *manage* long running tasks … but ***not perform*** the actual, long task" / code: "NOTE: This is a long-running operation. Do not call this tool again if it has already returned some intermediate or pending status." |
| Parallel tool execution | Async tools run concurrently in one turn (Python) | https://adk.dev/tools-custom/performance/index.md | "the framework attempts to run any agent-requested function tools in parallel" |
| HITL confirmation | Tool pauses; UI dialog or REST `FunctionResponse` named `adk_request_confirmation` with matching id; Go events carry `RequestedInput{InterruptID, Message, ResponseSchema, Payload}` | https://adk.dev/tools-custom/confirmation/index.md; adk-go `session/session.go` (`RequestInput`), adk-web `long-running-response.ts:64` | "The `id` in the `function_response` should match the `function_call_id` from the `adk_request_confirmation` `FunctionCall` event." / code: "UI surfaces read the same field to render the prompt." |
| HITL inside a subagent | `task` asks clarifications and auto-returns; `single_turn` disallows; task agents are leaves | https://adk.dev/workflows/collaboration/index.md | "**Human in the Loop** \| Full interaction \| For clarification only \| Disallowed" / "***Task* mode agents** must be leaf agents and cannot have subagents." |
| Callbacks as seam | before/after agent + tool hooks; after-agent can only append; not inherited by sub-agents | https://adk.dev/callbacks/types-of-callbacks/index.md; https://adk.dev/tutorials/agent-team/index.md | "logging the entry point of the agent's activity" / "the content it returns is emitted as an *additional* event" / "The `before_model_callback` defined on the root agent does NOT automatically apply to sub-agents." |
| A2A | Landing page only; streaming note in the extension page | https://adk.dev/a2a/index.md; https://adk.dev/a2a/a2a-extension/index.md | "**Sub-agent data loss:** Ensures ADK outputs from remote agents are reliably preserved … when multiple agents are nested within the remote agent's sub-agent tree." |

## 3. What maps onto Aura's plan — and what does not

Aura today (read, not assumed): `internal/agent/event.go` `Event` already has `Author`, `Branch` ("hierarchical label
only (D-15)"), `Actions.StateDelta/ArtifactDelta/Escalate/AwaitingInput/Display/ToolInvocation`. Worker events go to
`<runDir>/<conv>/swarm/<childID>.jsonl` (`internal/swarm/report.go` `dumpTranscript`) and are read by
`GET /api/conversations/{conv}/swarm/{childID}/transcript` (`internal/agui/server_swarm_transcript.go`); the parent's
tool result is `[]ChildReport{goal_index, child_id, status ok|failed|needs_user_input, summary, error, question,
options, tool_call_id}`; `swarm_spawn` tells the model "Each worker returns a single final report";
`internal/agent/display/swarm.go` already normalizes that into a `swarm_report` display payload.

Worth borrowing:
- **One loop, `author` + `branch` on every event, one subscription.** ADK's inline path (ParallelAgent, 2.0 nodes) pushes
  child events into the parent's stream tagged `author=child`, `branch=parent.child[@run_id]`; the UI filters, it does
  not connect twice. Aura has the fields — the gap is fan-in from the worker goroutine into the parent run's AG-UI stream.
  The "read-only parallel thread" is then a `branch` filter; the `.jsonl` route stays as replay/back-fill.
- **Branch = `parent.child@tool_call_id`.** ADK's 2.0 `_SingleTurnAgentTool` keys the sub-branch on the function-call id
  (`agent_tool.py:415-420`). Aura's `swarm_spawn` tool-call id + `w1..wN` gives the same stable, replayable key.
- **Partial vs committed; aggregated final.** Forward `partial` chunks for display, persist only non-partial, and emit one
  aggregated final per worker (`_streaming_mode.py:52-75`). Aura's `DiscardStreamed` is the same idea on retry.
- **`artifact_delta: {filename: version}` + fetch-on-delta.** adk-web renders an artifact by reacting to `artifactDelta`
  on the event and fetching `/artifacts/{name}/versions/{v}`; the canvas panel should be driven the same way (worker's
  final event carries `{"swarm/<childID>/report.md": 1}`), never by pushing the body through chat.
- **`is_final_response()` decides the bubble.** The swarm JSON is a `function_response`, which ADK never renders as chat
  unless `skip_summarization`; the card is the tool-result display (`display.Payload`), the parent's text is the bubble.
  This is the direct fix for (b), and Telegram gets the same rule.
- **Long-running-tool conventions for `swarm_status`.** Return an id and a `"status"` key ("pending"); put ADK's
  no-repeat note in the tool description; tell the model to read "progress vs. completion". The `[]ChildReport` slot
  shape already fits; `ToolCallID` is ADK's `function_call_id` matching rule.
- **Worker questions = `task` mode + `RequestInput`.** ADK's Go event field `RequestedInput{InterruptID, Message,
  ResponseSchema, Payload}` is Aura's `AwaitingInput` one-to-one; ChildReport `needs_user_input` + `tool_call_id` is the
  D-05 proxy; ADK's "must be leaf agents" matches the depth cap.
- **Cancel is non-destructive.** Committed events persist, in-flight ones are dropped, the signal reaches the sub-runner:
  a cancelled worker keeps its transcript and reports `failed`, never a truncated file.
- **Trace = span tree, not chat.** ADK's only "watch the tree" view is OTel spans (`agent_run`/`invoke_agent` per agent,
  `parent_span_id` links). Aura's `SpanID`/`ParentSpanID` are already on `Event`; a worker view is a span-tree filter.

Does not map:
- ADK's parent **blocks** while yielding child events; `ParallelAgent` returns only "once all parallel branches
  complete", and each child waits for the runner's ack per event. There is no parent LLM reasoning while children run
  and no parent-side status query — `swarm_status` has no ADK analogue; ADK's polling lives in the *client*.
- `AgentTool` isolates the child (own Runner, in-memory session, events not forwarded) and the project now discourages
  it; Aura's current "wait for the final report" is exactly this shape and is the thing being replaced.
- ADK has no separate transcript resource: the session event history **is** the transcript. Aura's `.jsonl` + HTTP
  route is an addition, not a port.
- The dev UI shows one `bot` role for every agent; nothing about a per-worker card or a parallel thread exists to copy.
- Nothing on channel projection (Telegram); ambient agents route results to logs/Pub/Sub, not to a UI.
- Go-specific: "The state is immutable within a single `Run` invocation" — worker state must ride on events.

## 4. NOT found / not documented (do not assume more than pages and code say)

- No page describes how the dev UI renders sub-agent events. Code confirms the chat pane maps every non-user author to
  `'bot'`; the author is visible only in the Events tab detail row and the Trace tab span tree.
- `branch` format is documented only as "Hierarchy path"; the `agent_1.agent_2` and `name@run_id` forms come from code
  docstrings (`invocation_context.py:141-148`, `_branch_path.py`), not from any doc page. Whether LLM transfer sets
  `branch` is not stated; code shows transfer keeps the caller's `invocation_context` (`base_llm_flow.py:1680-1688`).
- Whether `AgentTool` forwards child events: the doc says only "captures its final response"; the code says explicitly
  it does not forward them (`agent_tool.py:308-311`). `include_plugins=True` "preserving trace spans and event
  streaming" is not defined anywhere.
- Any API for a parent agent to inspect a running child (no `swarm_status`, no child handle, no progress event type).
- The exact `ParallelAgent` result API ("e.g., through a list of results or events"); code returns only merged events.
- Language parity: cancel is TypeScript-only; resume Python 1.16+/Kotlin; collaboration modes Python/Go 2.0; parallel
  tool execution Python 1.10+; confirmation TypeScript is manual; resume "not currently supported" from web UI/CLI.
- A2A progress/task-status surfacing: `a2a/index.md` is a link page; only the extension note on streaming data loss.
- Confirmation known limits: `DatabaseSessionService` and `VertexAiSessionService` "not supported by this feature".
- Artifact push to a viewer: none; the UI pulls on `artifactDelta` and the placeholder assumes `image/png`
  (`chat.component.ts:2025-2035`) — no document/canvas rendering path.
- `_run_node_internal` event forwarding was read only as far as `NodeRunner` "enqueues events to ic.event_queue" and
  `Runner._consume_event_queue` (`runners.py:727`); the scheduler's WAITING/interrupt path was not traced further.
