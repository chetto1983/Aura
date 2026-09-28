# Tool rendering with Tool UI and assistant-ui Elements

Agreed design, 2026-09-28. This is Spec 2 of the cockpit tool UI work. [Spec 1](2026-09-25-mcp-elicitation-question-card-design.md) owns `ask_user`, MCP elicitation, and the shared question card. This spec reviews the remaining tool surface, selects useful components from both [Tool UI](https://www.tool-ui.com/) and [assistant-ui Elements](https://www.assistant-ui.com/elements), and orders their adoption. It is a design contract; no renderer or send-flow implementation is included here.

## Intent and decisions

The operator wants every native, memory, calendar, and WhatsApp tool reviewed, with a prioritized rollout. Use Tool UI wherever its component fits; evaluate Elements alongside it and choose the closer fit for Aura's behavior. Keep the compact cockpit row as the entry point. For email and WhatsApp text messages, present an editable draft before each send. The operator approves the exact effective arguments once per message.

Success means a completed tool call opens a useful, readable view when the data supports one, while unknown or malformed output remains visible as escaped text. The send review must show the actual pending message and destination, allow edits, and bind the approved version to a single external send. Existing source navigation, artifact access, MCP Apps, generation progress, and media playback behavior must survive the change.

## Measured baseline and corrected research

- The [2026-09-25 mapping](../2026-09-25-tool-ui-tool-mapping.md) inventories 26 native tools, 13 memory MCP tools, one calendar tool with 29 actions, and 15 WhatsApp tools. Its candidate map is research, not an implementation decision. This spec supersedes its recommendations where they differ.
- `ToolActivityCard` hosts the collapsed row and `DisplayRouter` renders typed expanded content. `ToolResultPanel` is the escaped, capped fallback. `system_event` and `local_artifact` may render inline; generation has its own progress frame. Pending approvals remain visible above the composer under the [compact chat spec](2026-07-23-cockpit-compact-chat-ui-spec.md).
- `internal/agent/display/normalize.go` currently recognizes `web_search`, `web_fetch`, `swarm_spawn`, and `shell_exec` / `sandbox_exec`, plus web errors. Most native and MCP results reach the raw fallback. `aura.artifact` and `aura.mcp_view` are existing trusted event paths separate from `aura.display`.
- `web/src/chat/artifacts/renderers/previewDispatch.tsx` has no audio preview. Video uses a Range stream, keeps its natural proportions, and never autoplays. `AssetSourceContext` chooses authenticated or public-share URLs.
- The mapping's approval caveat is stale: current `internal/agent/mcptools/bridge_risk.go` grades calendar `send_email` and WhatsApp `send_message` as Destructive, and the gateway withholds them for approval. However, `internal/gateway/approve.go` currently shows only argument keys and lets the model re-emit the call after approval. Standing grants can skip that question. The editable, exact-message review below requires a dedicated path.
- `web/components.json` already contains both `@tool-ui` and `@assistant-ui` registries. The project uses `@assistant-ui/react` but owns its existing AG-UI thread wiring. Choose standalone, props-driven Elements; do not initialize a second chat runtime.

## Component selection

Selection is per result shape, not per brand. Port only selected component source, pin its upstream revision or registry artifact, record the license, adapt to Aura's shipped blue tokens, English and Italian strings, keyboard use, and narrow screens. Tool UI's archived repository is pinned for Spec 1 at `49a870286facdbf28160cd647f0d337ebdc9b275`; Elements is a changing registry, so record each installed entry's revision during implementation. Do not install a catalog wholesale.

| Result / interaction | Tool UI candidate | Elements candidate | Selected treatment and reason |
|---|---|---|---|
| Collapsed tool activity | Tool UI activity/progress | Tool call, tool timeline | Keep Aura `ToolActivityCard` and `ToolGroup`: they already encode running, nested, and completed states and the compact chat contract. Reuse visual details only. |
| Working checklist | Plan | Todo list, agent plan | **Elements Todo list**, adapted for `todo_write`'s individual `pending`, `in_progress`, `completed` states and updates. Tool UI Plan is the fallback if its source adapts more cleanly. Avoid an active-index-only representation. |
| Shell output | Terminal | Terminal block | **Elements Terminal block** standalone: command, combined output, cwd, exit code, duration, truncation. Aura's shell preview combines stdout/stderr and may append a stderr tail; do not claim separate streams or imply line streaming before Aura emits it. |
| Applied patch | Code diff | Code diff, reviewable diff | **Elements Code diff** standalone: parse the trusted applied unified diff into file/hunk lines. It avoids Tool UI's `@pierre/diffs` dependency. Read-only; no hunk acceptance or re-apply action. |
| Read/written code | Code block | Syntax highlighter | **Tool UI Code block** for text with filename/language and line numbers where available. Keep a raw-text escape hatch for parsing failures and large output. |
| Tabular results | Data table, stats display | Data table, spec sheet | **Elements Data table** for row-shaped results, with Aura's existing filtering, CSV export, pagination and copy retained. **Tool UI Stats display** for compact diagnostic counts. No table for a single acknowledgement. |
| Web and document evidence | Citation, link preview | Web search, inline citation, link preview, retrieval chunks | Keep `WebResultDisplay`, `DocumentDisplay`, and Source Explorer as the data/interaction owners. Adapt Tool UI citation/link-preview presentation where `ref_id`, URL safety, and source click-through remain intact. Elements Web search's simple result shape cannot replace this contract. |
| Audio and video | Audio, video | Media player | **Elements Media player** as the control surface over Aura's source URLs and error handling; explicit controls, no autoplay, Range seeking, natural video ratio. Audio is the first media gain. |
| Single image / gallery | Image, image-gallery | Image generation, image gallery | **Tool UI Image and Image gallery** frames adapted to Aura's asset loading, natural image dimensions, error state, and small widths. Keep the generation state machine. A gallery requires real dimensions (from decoded image or ingested metadata), never guessed placeholders. |
| Email / WhatsApp review | Message draft | Draft email, approval card | **Tool UI Message draft** visual base, extended for editable fields and WhatsApp. Elements Draft email is a separate generative UI example; it does not own Aura's gateway approval. |
| Worker results | Progress tracker | Subagent list, agent plan | Keep `WorkerPane` and `SwarmReportTable` for live/final detail. An Elements Subagent list may summarize worker status if it preserves links and errors. |
| Server UI | No equivalent | Elicitation form, web preview | Keep sandboxed `McpViewFrame`; Spec 1 owns the one-field-at-a-time elicitation card. Do not insert a generic browser frame or Elements elicitation flow. |

### Selection constraints

- A copied component is presentation only. It does not decide whether untrusted tool text is safe to render, fetch asset bytes, approve a call, or execute an action.
- Adapters must preserve the exact semantic status and data available. A completed result is never animated as a running stream. A failed tool is not displayed as successful because its payload parses.
- Use accessible names and focus order for disclosures, tables, media controls, and draft actions; honor reduced motion. Keep the current blue tokens from `web/tokens/tokens.json`, not old graphite-gold mockups.
- Tool UI media schemas require absolute URLs. Build them through the existing source context (including public shares), validate allowed protocols/origins, and retain `PreviewError`. Never expose a container path as a browser URL. `blob:` object URLs must be revoked.

## Rendering architecture

```text
tool call + concrete result
  -> trusted Go projection by exact native tool or trusted MCP server/action identity
  -> typed aura.display event + replay snapshot (or existing aura.artifact / aura.mcp_view)
  -> DisplayRouter within ToolActivityCard
  -> local component adapter validation -> selected Tool UI / Elements view
                                   \-> escaped, capped ToolResultPanel on failure
```

The Go normalizer alone upgrades tool results to rich `aura.display` data. Add explicit payload variants in `internal/agent/display/payload.go` and mirrored `web/src/chat/displays/types.ts`; keep `tool_call_id` correlation. Do not parse an arbitrary result in React and infer a rich renderer from JSON keys, filenames, Markdown, or an untrusted server's claimed name. For MCP projection, check managed-server identity and the exact tool/action and validate bounded result shapes before issuing a typed payload. Preserve the original raw result for fallback. Client `safeParse` or equivalent is a second check, not the trust decision.

The same decode and projection must run for live events and `MESSAGES_SNAPSHOT` replay. A running call may show the row's existing progress, while the rich result arrives when completed. If a payload is absent, malformed, oversized, or from an unknown version, the expanded row renders `ToolResultPanel` with escaped, capped, copyable text. Errors remain errors. The router must not drop unknown content or render server HTML/Markdown as trusted UI.

For each selected component, the implementation records a narrow adapter contract: accepted source type, maximum rows/lines/media items, field limits, URL policy, parser failure fallback, and live/replay fixture. Large tables and diffs remain bounded with a clear truncation or pagination indicator. No client adapter performs tool execution.

## Complete tool-family disposition

The table names every native and memory tool and all 15 WhatsApp tools. Calendar's one multiplexed tool is resolved by its action; its 29 action names are grouped below. “Row” means the existing collapsed row plus raw expanded details; it is an intentional decision where a specialized card adds little.

### Native tools (26)

| Tool(s) | Result in cockpit | Rollout |
|---|---|---|
| `ask_user` | Spec 1 QuestionCard; no second approval/elicitation UI | Existing |
| `shell_exec`, `shell_poll` | Elements Terminal block for completed output; widen trusted code payload for command, combined output, cwd, exit code, duration, truncation; a running/background poll remains a status row associated with its job | 1 |
| `shell_kill` | Row with termination status | 1 |
| `patch` | Elements Code diff from applied unified diff; multi-file patches keep each filename/hunk; show failure as error | 1 |
| `read_file`, `write_file` | Tool UI Code block for supported text; line numbers on read; binary/image uses existing attachment/artifact path; write receipt shows target and bounded written preview | 1 |
| `search_files` | Elements Data table for file/line/match or file list; preserve paths and search term | 1 |
| `read_tool_output` | Row and raw page; avoid presenting a partial page as a whole result | 1 |
| `tool_search` | Row; internal manifest retrieval | 1 |
| `text_response` | Assistant message, no duplicate tool card | Existing |
| `current_time` | Row or compact value | 1 |
| `todo_write` | Elements Todo list with localized heading; snapshot list order is stable within that revision, no invented persistent task IDs | 1 |
| `task` | Elements Data table for `list`; create/run/cancel receipts stay rows; Scheduler board stays separate | 3 |
| `swarm_spawn`, `swarm_status` | Existing worker report and pane; optional Elements Subagent list for status, with errors and navigation retained | 3 |
| `skill`, `skill_manage`, `plugin_pack` | Table for list results where structured; mutating/admin receipts remain rows | 3 |
| `document_search` | Evidence cards with citation/ref identity and Source Explorer; no fabricated external URL | 3 |
| `document_open` | Row and resulting artifact/download link when available | 3 |
| `web_search`, `web_fetch` | Existing evidence/document data and Source Explorer with Tool UI citation/link-preview styling where safe | 3 |
| `image_generate`, `video_generate` | Existing queued/progress/result frames; selected media/image view only on completed asset | 2 |
| `send_file` | Existing `LocalArtifactDisplay` delivery/download controls; selected image/video/audio preview for supported MIME, otherwise file card | 2 |

`sandbox_exec`, although absent from the 26-tool inventory, shares `shell_exec`'s existing normalized code path and follows the terminal treatment if surfaced. This is not a new tool commitment.

### Memory MCP (13)

| Tool(s) | Result in cockpit | Rollout |
|---|---|---|
| `memory_search`, `memory_recall`, `memory_facts_about`, `memory_entities` | Validated facts/entities in Elements Data table with source and validity fields retained | 3 |
| `graph_diagnostics` | Tool UI Stats display for counts, bounded raw details available | 3 |
| `graph_schema` | Tool UI Code block for validated JSON | 3 |
| `graph_path` | Link/open the existing graph workspace when a trusted graph reference exists; otherwise row/raw path data | 3 |
| `memory_digest` | Row with bounded summary; no invented table | 3 |
| `memory_upsert_fact`, `memory_batch`, `memory_forget`, `memory_merge_entities`, `memory_reembed` | Row with actual success/error receipt; keep current risk classification | 3 |

### Calendar MCP: one tool, 29 actions

The calendar server's MCP Apps view remains the primary interactive surface. A trusted action projection may show a compact result in the tool row; it must not supplant the sandboxed view or create another calendar workflow.

| Actions | Result in cockpit | Rollout |
|---|---|---|
| `list_accounts`, `get_emails`, `get_email_details`, `search_emails`, `list_calendars`, `get_calendar_events`, `get_calendar_event_details`, `get_contacts`, `search_contacts`, `get_contact_details`, `get_guide`, `get_unsubscribe_info`, `get_email_attachment`, `get_contextual_email_summary` | MCP Apps view first; validated row/table/document reference where it improves a completed call; attachment remains an asset/download, not a container path | 3 |
| `create_event`, `update_event`, `mark_email_read`, `create_contact`, `update_contact`, `bulk_mark_emails_read` | MCP Apps view or concise success/error receipt; no draft message card | 3 |
| `respond_to_event`, `delete_email`, `move_email`, `delete_event`, `delete_contact`, `bulk_delete_emails`, `bulk_move_emails`, `unsubscribe_from_email` | Existing gateway approval and result receipt; never disguise the action as a message draft | 3 |
| `send_email` | Editable message draft, then one gateway-owned send of approved effective arguments and sent/declined/uncertain receipt | 3 |

### WhatsApp MCP (15)

| Tool(s) | Result in cockpit | Rollout |
|---|---|---|
| `list_messages`, `list_chats` | Existing MCP Apps chat view; row can summarize validated count/context | 3 |
| `search_contacts`, `get_contact`, `get_chat`, `get_contact_chats`, `get_direct_chat_by_contact`, `get_last_interaction`, `get_message_context` | Validated table/spec sheet for compact results where useful; preserve chat/identity fields; view remains primary for browsing | 3 |
| `get_media_data`, `download_media` | Selected media preview only after bytes are available through an authorized Aura asset URL; otherwise receipt/download | 2/3 |
| `send_file`, `send_audio_message` | Existing destructive approval and delivery receipt; preview authorized attachment/audio when available. Do not treat a file send as the text-message draft | 2/3 |
| `send_message` | Editable WhatsApp draft, then one gateway-owned send and receipt | 3 |
| `send_reaction` | Existing approval and concise receipt | 3 |

## Editable message draft and send authority

This applies specifically to calendar `send_email` and WhatsApp `send_message`, including when a standing session or always grant exists. Each distinct outbound message receives its own review. Other destructive actions retain their current approval path. Noninteractive/headless or production paths continue to fail closed under gateway policy; the draft UI never authorizes a send by itself.

1. **Withhold and present.** The gateway catches the tool call before MCP execution and records a server-owned pending draft scoped to authenticated operator, conversation, tool-call ID, trusted server/action, original argument fingerprint, and expiry. Park the run at this invocation boundary without opening the MCP send call or spending its timeout while the operator reviews. Emit an owner-only review projection for the cockpit. The pending card stays above the composer and never collapses into a tool row; the final receipt links back to that row. Show the complete human-material destination and content: email sender account, To/Cc/Bcc, subject and body; WhatsApp destination/chat identity and body. Also show any delivery-relevant fields from the actual tool schema. Secret transport or credential values are never displayed. Sender/server/action identity stays fixed for this draft.
2. **Edit.** The card allows changing the schema-supported recipient/destination and message fields, with clear labels, validation feedback, and an explicit Send and Decline. Client edits are local until submission. The backend validates field types, limits, destination format/identity, allowed editable keys, and all effective arguments against the live tool schema and policy. It builds the effective argument object itself from the stored original plus validated overrides; client-submitted hidden fields, tool identity, or fingerprints have no authority. A changed destination is visibly reflected in the final Send action. Reject stale, foreign, expired, or already resolved draft IDs.
3. **Approve and execute.** One owner-authenticated Send request atomically consumes that draft's one-shot authorization and records dispatch started. A dedicated gateway continuation accepts only this draft ID and its validated effective-argument fingerprint, then executes the **effective** arguments through the normal audit and reservation funnel without opening another approval challenge. Return the execution result to the parked run so the model sees the actual send outcome. The model does not reconstruct or retry the edited call. Standing grants do not bypass this message-specific draft seam. A double click, replayed request, competing tab, reconnect, or model retry cannot create a second authorization. Show a pending/sent/failed/declined receipt in live and replay; resolve or expire the review card. If delivery becomes ambiguous after dispatch, show an *uncertain* receipt and require a new explicit review for any retry; do not claim exactly-once delivery without server-side idempotency.
4. **Privacy and escape.** Draft content is visible only to its owner in the authenticated conversation. Keep body, recipients and attachments out of log messages, trace attributes, fingerprints displayed to users, and public shares. The persisted record is bounded and expires. Escape may close an editing affordance but must not silently approve, decline, or cancel the run; explicit Decline is the rejection path. Do not offer Tool UI's client-only “undo” countdown because it cannot recall a delivered external message.

The present gateway relay (`gateway_approval_required` -> model `ask_user` -> re-emitted call) cannot satisfy edited-argument authority. The implementation plan must replace that relay for these two actions with the server-owned flow above while retaining audit facts and deny behavior. Any backend gap discovered during implementation is resolved before enabling the card, not papered over with a client-side confirmation.

## Rollout and acceptance

| Wave | Scope | Release evidence |
|---|---|---|
| 1. Structured native work | Todo list, terminal, code diff/block, search table; row dispositions | Trusted projection and mirrored types; live/replay equality; error/malformed fallback; mobile and keyboard review. No backend change to shell or patch execution. |
| 2. Media | Audio first, then image/video frames and dimensions-backed gallery; generated and sent artifacts | Authenticated/public-share asset routes, Range seeking, natural aspect, never autoplay, media failure fallback, object URL cleanup; preserve download/edit controls and generation progress. |
| 3. MCP and communication | Memory, calendar/WhatsApp read results, evidence styling, worker summaries, editable drafts | Trusted server/action checks, MCP Apps preservation, Source Explorer navigation, full owner-scoped one-shot send scenarios including grants, edits, reload, expiry and ambiguous delivery. |

Each wave is independently useful and can ship once its listed behavior works; “review all” does not require installing a card for every row-only tool. Update the PRD only after the relevant behavior has been measured on a running stack, following `CLAUDE.md`'s PRD-first rule. No claim of improved task time or reliability is made until measured.

### Verification contract

- **Go projection:** concrete native/MCP shape tests; wrong server/tool/action, malformed and oversized data fall back; exact `tool_call_id`; live event and replay snapshot carry the same typed payload; no untrusted HTML promotion.
- **Frontend:** selected component adapter tests for valid/malformed/boundary values and accessible interaction; old rows, source IDs, artifact links, MCP Apps, CSV/filter/copy, generation states and public-share behavior remain usable. Verify English/Italian, blue theme, reduced motion and narrow width.
- **Media:** real audio/video/image asset fixtures; controls and seeking; no autoplay on first mount or thread reopen; portrait/square ratios; revoked object URLs; authorized URL and failure behavior.
- **Sending:** gateway and cockpit integration tests prove edited arguments are the executed arguments, one review per message even under grants, declined/expired/foreign/stale requests never execute, duplicate submit and model retry cannot send twice, and an ambiguous external outcome is not auto-retried. Exercise both email and WhatsApp with a fake MCP sidecar before a real-stack smoke.
- **Cockpit smoke:** a representative completed call from each family, a malformed fallback, replay/reconnect, one edited email, and one edited WhatsApp message. Record actual UX/runtime observations before any PRD amendment.

## Boundaries

This spec does not redesign Spec 1's QuestionCard, the Scheduler board, the graph workspace, MCP Apps documents, the assistant composer, or the model/runtime integration. It does not add actions to read-only diff or media views. Tool UI and Elements components are copyable presentation sources, not an alternative trust boundary. The current `table`/`chart` display kinds should be wired to genuine trusted producers or removed when touched; no dead renderer is kept solely to make the catalog seem covered.

## Source references

- Local: [Tool UI mapping](../2026-09-25-tool-ui-tool-mapping.md), [Spec 1](2026-09-25-mcp-elicitation-question-card-design.md), [compact chat](2026-07-23-cockpit-compact-chat-ui-spec.md), `internal/agent/display`, `internal/gateway`, `internal/agent/mcptools/bridge_risk.go`, `web/src/chat/displays`, `web/src/chat/artifacts`, `web/components.json`.
- Official: [Tool UI repository](https://github.com/assistant-ui/tool-ui), [Tool UI guide](https://www.assistant-ui.com/docs/tools/tool-ui), [Message Draft](https://www.tool-ui.com/docs/message-draft), [Elements catalog](https://www.assistant-ui.com/elements), [Todo list](https://www.assistant-ui.com/elements/todo-list), [Terminal block](https://www.assistant-ui.com/elements/terminal-block), [Code diff](https://www.assistant-ui.com/elements/code-diff), [Data table](https://www.assistant-ui.com/elements/data-table), [Media player](https://www.assistant-ui.com/elements/media-player), [Web search](https://www.assistant-ui.com/elements/web-search).
