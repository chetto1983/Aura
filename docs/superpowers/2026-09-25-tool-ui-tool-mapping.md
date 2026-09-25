# Aura's tools mapped to the Tool UI gallery

2026-09-25. This is the input for Spec 2: reviewing how every tool renders in the cockpit against Tool UI (assistant-ui/tool-ui, MIT). It is research, not a decision. Nothing here is approved for building.

## Sources

Every claim below was read from source on 2026-09-25.

- **Tool UI**
  - 27 component directories in `assistant-ui/tool-ui` `apps/www/components/tool-ui/`.
  - The prop fields come from each component's `schema.ts` on `main`.
- **Aura's native tools:** the 26 `Spec().Name` values in `internal/agent/tools/`.
- **Memory MCP:** 13 tools in `cmd/arcadedb-mcp/`, matching `docs/arcadedb-mcp-live-tools.json`.
- **Calendar MCP** (`chetto1983/aura-pim-mcp` `main` @ 6734385):
  - ONE tool, with 29 actions multiplexed (`CalendarActionTool.cs:295-323`).
  - It ships an MCP Apps view, `ui://calendar/view.html`.
- **WhatsApp MCP** (`chetto1983/whatsapp-mcp` `main` @ 1ec0233):
  - 15 tools (`whatsapp-mcp-server/main.py`).
  - `list_messages` and `list_chats` carry the MCP Apps view `ui://whatsapp/client.html`.
- **The cockpit today:**
  - `web/src/chat/displays/DisplayRouter.tsx`, `ToolActivityCard.tsx`, `toolSummary.ts`;
  - `generation/GenerationToolDisplay.tsx`, `mcpapps/McpViewFrame.tsx`;
  - `approvals/`.
- **The backend normalizer:** `internal/agent/display/normalize.go:28-47`.

## How the cockpit renders a tool today

Every tool call is a one-line `ToolActivityCard` row, collapsed by default. What the row expands to depends on the tool:

| Path | Tools | Renderer |
|---|---|---|
| Typed display (Go normalizer → `aura.display`) | `web_search` → `web_result`, `web_fetch` → `document`, `shell_exec` → `code` or `local_artifact`, `swarm_spawn` → `swarm_report` | `WebResultDisplay`, `DocumentDisplay`, `CodeDisplay`, `SwarmReportTable` |
| Artifact frame (`aura.artifact`) | `send_file` | `LocalArtifactDisplay` (download button) |
| Generation frame | `image_generate`, `video_generate` | `GenerationFrame` (queued → progress → result) |
| MCP Apps view (`aura.mcp_view`) | calendar (the whole tool), WhatsApp `list_messages` / `list_chats` | `McpViewFrame`, the server's own document in a sandboxed frame |
| Approval card | `ask_user`, and shell approval challenges (`approval.shell.command`) | `InlineApprovalCard` |
| Everything else | the other ~50 tools | `ToolResultPanel`: escaped, capped raw text |

**The integration rule every mapping inherits (HARDEN-08).**
- The Go normalizer is the ONLY producer of a rich payload. The cockpit never upgrades raw tool output to a rich render.
- So each Tool UI adoption below costs three pieces:
  1. a `normalize.go` case, plus a payload field if needed;
  2. a `DisplayKind` in `displays/types.ts`;
  3. the Tool UI renderer behind a `DisplayRouter` case.
- The component's `safeParse*` stays as a second gate. A payload that fails it falls back to `ToolResultPanel`, the way unknown kinds do today.

**Fit legend**

- **Direct:** the tool already returns every field the component requires.
- **Widen:** the tool has the data, but the display payload drops it, or it lacks one cheap field.
- **Aura owns:** an Aura renderer or the server's own MCP Apps view already does more. Tool UI would at most restyle it.
- **Row only:** the one-line row is the right amount of UI.

## Native tools (26)

| Tool | Today | Tool UI | Fit | Notes |
|---|---|---|---|---|
| `ask_user` | `InlineApprovalCard` | `question-flow`, `option-list`, `approval-card` | — | **Spec 1**, in progress. |
| `shell_exec` | `code` display | `terminal` | Widen | `terminal` requires `command` + `exitCode`, and takes `stdout`, `stderr`, `durationMs`, `cwd`, `truncated`. `shell_exec` already returns `exit_code`, `cwd` and `duration_ms` (`shell_exec.go:64-66`), but `display.Code` keeps only `Body`, `Lang` and `Cancelled` (`display/code.go`). The approval challenge before a command is already in Spec 1's card family. |
| `shell_poll` | raw | `terminal` | Widen | New output of a background job; there is no normalizer case today. |
| `shell_kill` | raw | — | Row only | |
| `patch` | raw | `code-diff` | Widen | `patch` already returns a unified diff (`patch.go:63`, `patch_diff.go`). `code-diff` takes it as `patch`, with `filename` and `language`. It needs the `@pierre/diffs` dependency. |
| `write_file` | raw | `code-block` | Widen | The written content is in the args. `filename` + `language` come from the path. A diff would need the previous content, which the result does not carry. |
| `read_file` | raw | `code-block`; `image` for box images | Widen | The text result is line-numbered and must be stripped into `code-block`, with `lineNumbers: visible`. Box images are already attached to the model natively. |
| `search_files` | raw | `data-table` | Widen | Content mode is rows of file, line and match; names mode is a file list. |
| `read_tool_output` | raw | — | Row only | Pages a truncated result. |
| `tool_search` | raw | — | Row only | Internal to the model. |
| `text_response` | the assistant message | — | — | It IS the reply. |
| `current_time` | raw | — | Row only | |
| `todo_write` | raw | `plan` | Direct | The operator's first pick (2026-09-25). The statuses are identical: `pending`, `in_progress`, `completed`; Plan adds `cancelled`. `content` → `label`, and `activeForm` shows while `in_progress`. `todo_write` has no id or title, so the id comes from the list position and the title is a fixed i18n string. |
| `task` (the scheduler) | raw | `data-table` for `list` | Widen | Create, cancel and run are row only. Tool UI has no schedule component. The cockpit's Scheduler board is a separate open gap. |
| `swarm_spawn` | `SwarmReportTable`, `WorkerPane`, `ToolGroup` nesting | `progress-tracker` while running | Aura owns | The `progress-tracker` step statuses (`pending`, `in-progress`, `completed`, `failed`) fit the live fan-out. The final reports stay in `SwarmReportTable`. |
| `swarm_status` | raw | `progress-tracker` | Widen | One step per worker. |
| `skill` | raw | `data-table` for `list` | Widen | Low value. |
| `skill_manage` | raw | — | Row only | Admin writes. |
| `plugin_pack` | raw | `data-table` | Widen | Skills, connectors and commands. Low value. |
| `document_search` | raw (citations via `rehypeCitations` / Source Explorer) | `citation` | Widen | `citation.href` must be an absolute URL (`z.string().url()`). A document passage has none; the same-origin asset route would have to be made absolute. Aura's Source Explorer stays. |
| `document_open` | raw | — | Row only | Downloads to `/workspace`. |
| `web_search` | `WebResultDisplay` | `citation` / `link-preview` | Aura owns | The Aura card carries `ref_id` into the Source Explorer. Tool UI would only restyle it. |
| `web_fetch` | `DocumentDisplay` | `link-preview` header | Aura owns | The markdown body stays Aura's. |
| `image_generate` | `GenerationFrame` | `image`, `image-gallery` | Aura owns | The queued → progress state machine is Aura's. The final result could use `image` (`alt` is required, and `src` must be an absolute URL). |
| `video_generate` | `GenerationFrame` | `video` | Aura owns | Same as `image_generate`. |
| `send_file` | `LocalArtifactDisplay` | `image`, `video`, `audio` by `mime_type` | Widen | `asset_id` + `mime_type` are already on the payload (`displays/types.ts` DisplayArtifact). Media gets a player, and any other file keeps the download card. `src` needs the absolute asset URL. |

## Memory MCP (13, `cmd/arcadedb-mcp`)

| Tool | Tool UI | Fit | Notes |
|---|---|---|---|
| `memory_search`, `memory_recall`, `memory_facts_about` | `data-table` | Widen | Facts are rows: subject, predicate, object, validity. MCP results reach the cockpit raw today; there is no MCP normalizer. |
| `memory_entities` | `data-table` | Widen | |
| `graph_diagnostics` | `stats-display` | Widen | Counts as stat tiles. |
| `graph_schema` | `code-block` (JSON) | Widen | |
| `graph_path` | — | Aura owns | The cockpit has a graph workspace (`web/src/graph/`). Tool UI has no graph component. |
| `memory_upsert_fact`, `memory_batch`, `memory_forget`, `memory_merge_entities`, `memory_digest`, `memory_reembed` | — | Row only | If the gateway gate asks before a destructive call, that is Spec 1's approval card. |

## Calendar MCP (1 tool, 29 actions)

The server ships its own MCP Apps view for the whole tool, and the cockpit already frames it (`McpViewFrame`). That view owns the rendering: **Aura owns**. Spec 2 has to check one moment against the view.

- **`send_email`** → `message-draft`, the email variant. Its fields `to`, `cc`, `bcc`, `subject`, `body` and `from` are the action's args. But `message-draft` is a review-before-send card. Aura's gateway gate asks only before Destructive tools, and `send_email` is Mutating, not Destructive. So adopting it is a behaviour decision (ask before sending mail), not a restyle.

## WhatsApp MCP (15)

| Tool | Tool UI | Fit | Notes |
|---|---|---|---|
| `list_messages`, `list_chats` | — | Aura owns | The server's own `ui://whatsapp/client.html` view. |
| `search_contacts`, `get_contact`, `get_chat`, `get_contact_chats`, `get_direct_chat_by_contact`, `get_last_interaction`, `get_message_context` | `data-table` | Widen | Contacts and chats as rows. Low value, since the Apps view covers browsing. |
| `send_message` | `message-draft` | Widen | `message-draft` has only `email` and `slack` variants. A WhatsApp variant means editing our copy of the component, which Tool UI allows because it is copy-paste source. The same gate caveat as `send_email` applies. |
| `send_file`, `download_media`, `get_media_data` | `image`, `video`, `audio` by mimetype | Widen | |
| `send_audio_message` | `audio` | Widen | |
| `send_reaction` | — | Row only | |

## Media in depth: `image`, `image-gallery`, `video`, `audio`

Read from each component's `schema.ts` and `.tsx` on `main`, and from `shared/media/aspect-ratio.ts` and `shared/media/sanitize-href.ts`. It was compared with Aura's own renderers in `web/src/chat/artifacts/renderers/`.

**How Aura shows media today**

- **Images:** a blob object URL from `useBlobPreview`, relabelled to the asset's mimetype. SVG is gated to download.
- **Video:** `VideoPreview.tsx` streams the tier's Range route (`assetSource.streamUrl`, served by `internal/agui/assets_stream_api.go`), so a clip seeks without a full download.
  - It **never autoplays, including when a thread is reopened**.
  - It keeps the clip's own proportions. Letterboxing was fixed there on 2026-09-17.
- **Errors:** every renderer falls back to `PreviewError`.
- **Audio:** `previewDispatch.tsx` has NO audio kind. An audio file falls through to `download`.
- **Public share page:** the `AssetSourceContext` seam swaps in token-scoped URLs, so the same renderers work there.

**What each Tool UI media component brings, and what it breaks**

| Component | Brings | Breaks or lacks, against Aura's rules | Aura sources it would serve |
|---|---|---|---|
| `audio` | A real player: play/pause, a seek slider, time readout, optional artwork, and `full` / `compact` variants. `preload="metadata"`. | Needs the Range `/stream` URL, made absolute, for seeking. No `onError`, so a failed load needs our fallback wrapper. | **A new capability.** WhatsApp voice notes (`download_media`, `get_media_data`, `send_audio_message`), uploaded audio, and `send_file` of audio. Today all of them are a download button. |
| `image` | Title and source attribution on a hover overlay, `sanitizeHref` on links, and `onNavigate` to intercept clicks (we would open Aura's artifact panel). | 1. `ratio: "auto"` does NOT size to the image. `RATIO_CLASS_MAP.auto` is `""`, so the box is `min-h-[160px]` with the `<img>` `absolute inset-0` and `object-cover`: a whole generated image would be cropped. Confirm in a render before relying on this reading. 2. `AspectRatioSchema` has only `auto`, `1:1`, `4:3`, `16:9`, `9:16`, while `image_generate` offers `3:4`, `3:2`, `2:3` too, so our copy's enum must grow. 3. No `onError`. 4. `min-w-80` (320 px), which is tight at phone width. | `image_generate` results, image uploads, WhatsApp and calendar images, and `read_file` box images. |
| `image-gallery` | A masonry grid plus a native `<dialog>` lightbox, and a per-tile `onError` fallback (`ImageOff`). | Every image REQUIRES `width` and `height` (positive). Aura stores no image dimensions: no asset column, no migration. They must be read client-side (`naturalWidth` after the blob loads) or recorded at ingest. | Several images in one turn: multiple generations, or a WhatsApp chat's media. |
| `video` | Poster, native controls, a title/source overlay, and `onMediaEvent`. | 1. **`autoPlay` defaults to `true`** (with `defaultMuted` `true`), which contradicts Aura's never-autoplay rule, so every mount must pass `autoPlay={false}`. 2. `ratio: "auto"` becomes `aspect-video` on a black background: portrait or square clips letterbox again, the exact defect `VideoPreview` fixed on 2026-09-17. 3. A hover `scale-[1.01]`. 4. No `onError`. | `video_generate` results, `send_file` video, WhatsApp video. |

**The shared constraint: URLs.** Every media schema declares `src: z.url()`. Zod 4 (`node_modules/zod/v4/core/schemas.js` `validateURL`) accepts any string the WHATWG parser takes WITHOUT a base.
- So a `blob:` URL passes.
- A relative `/api/assets/…` path fails `safeParse` and drops to the fallback.
- The adapter must pass `new URL(streamUrl(id), location.origin).href`, or a blob URL.
- The `AssetSourceContext` seam stays the one place that decides which route, so the public share page keeps working.

**Dependency.** Aura's `web/package.json` has no direct `zod`: 4.6.5 is only transitive today. Tool UI pins 4.3.6 in its own app. `shadcn add` would add `zod` as a direct dependency.

**Recommendation for Spec 2's media slice**

1. **Adopt `audio` first.** It fills a real gap: there is no audio rendering at all today.
2. **Adopt `image-gallery` only with dimensions solved.** Deciding client-side `naturalWidth` versus ingest-time metadata is a Spec 2 question.
3. **For `image` and `video`, keep Aura's loading layer and take Tool UI's frame.** Aura's layer is the blob and stream URLs, `PreviewError`, never-autoplay and natural proportions. Tool UI's frame is the overlay, attribution and metadata. The installed copies are ours to edit, so the fixes above (the ratio enum, `auto` sizing, `autoPlay={false}`, `onError`) go into our copy.

## Tool UI components with no Aura tool today

- `weather-widget`, `geo-map`, `order-summary`: no producing tool.
- `instagram-post`, `linkedin-post`, `x-post`: no producing tool.
- `parameter-slider`, `preferences-panel`: inputs, not tool results. The settings workspace is its own surface.
- `item-carousel`, `chart`: nothing produces them today (see side finding 1).

## Side findings

1. **Dark renderers.** `display.KindTable` and `display.KindChart` are defined (`internal/agent/display/payload.go:19,21`), and the cockpit has `TableDisplay` and `ChartDisplay` for them. But no Go code produces either kind: a grep for a producer finds only the type definitions. The `filecard.KindTable` in `internal/documents/filecard` is a different type. Spec 2 either feeds them (`data-table` would replace `TableDisplay`) or deletes them. CLAUDE.md forbids dark code.
2. **Spec 1 already covers every approval.** Shell approval challenges render through `InlineApprovalCard` (`approvals/approvalQuestion.ts` `approval.shell.command`), so Spec 1's card redesign reaches them too.
3. **Both of Aura's own sidecars already ship MCP Apps views**, so they are the servers that need Tool UI least.

## Suggested order for Spec 2

Biggest visible gain for the least backend work first:

1. `todo_write` → `plan` (Direct).
2. `shell_exec` / `shell_poll` → `terminal` (widen `display.Code` with `exit_code`, `cwd` and `duration_ms`).
3. `patch` → `code-diff`, and `write_file` / `read_file` → `code-block`.
4. Media, per "Media in depth":
   - `audio` first, because there is no audio rendering today;
   - then `image` / `video` frames over Aura's loading layer;
   - `image-gallery` once image dimensions are solved.
5. Resolve the dark `table` / `chart` kinds with `data-table` for `search_files`, `task list` and the memory facts.
6. Separately, as a behaviour decision: `message-draft` before `send_email` / `send_message`.
