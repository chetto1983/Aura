# Image and video generation as two native tools

Date: 2026-09-14. Status: approved section by section in brainstorming. Awaiting review of this
text.

## Why

Aura cannot produce an image or a video. No tool, sidecar or client generates media
(`internal/agent/tools/`), `internal/llm` knows only input modalities, and the assets store has
no video modality at all.

Measured on 2026-09-14 against OpenRouter with a throwaway probe built on
`github.com/openai/openai-go/v3 v3.61.0`, the SDK Aura already depends on
(`internal/llm/openai_compat/client.go:48-57`). Operator-chosen models:

| What | Result |
|---|---|
| `client.Images.Generate` (typed) → `POST images/generations` | Works. OpenRouter serves the OpenAI alias of its documented `POST /images`: a bogus model gets the same model-level 404 body on both routes, an unknown route gets a bare 404. |
| `microsoft/mai-image-2.6`, prompt only | 38 s, 1024² PNG, 1.08 MB, `usage.cost` $0.038982. Response decodes into `openai.ImagesResponse`; `data[].media_type` and `usage.cost` are extra fields, read from the raw body. Image inspected visually: matches the prompt. |
| `client.Videos.New` (typed) | Unusable: the SDK sends multipart, OpenRouter answers `400 Content-Type must be application/json`. |
| `client.Post(ctx, "videos", json)` + typed `Videos.Get` + typed `Videos.DownloadContent` | Works end to end. The submit body arrives padded with blank lines before the JSON; `json.Unmarshal` tolerates it. `Video.Status` decodes, `Progress` stays 0. |
| `minimax/hailuo-3-max`, 5 s, 480p | Completed by the first poll, 17 s total, `usage.cost` $0.2475. MP4 832×480, 24 fps H.264, 1.59 MB, frames show real motion. An AAC track is present although `generate_audio:false` was sent; the catalog declares `generate_audio:false` for this model, so the flag is not honoured and the track is the model's own (−56 dB mean). |
| `GET images/models`, `GET videos/models` | Free. 52 image models with `supported_parameters` descriptors, 29 video models with `supported_durations`, `supported_resolutions`, `pricing_skus`. |

Every `VideoService` method in v3.61.0 carries `Deprecated: The Sora API is scheduled to
permanently shut down on September 24, 2026.`

What the codebase already provides, read on 2026-09-14:

- `send_file` ingests a file into the identity's object store and emits `aura.artifact`
  `{path, filename, caption, tool_call_id, size_bytes, asset_id, mime_type}`
  (`internal/agent/tools/send_file.go:148-185`). It reads only box paths under `/workspace`
  (`send_file.go:124-130`).
- The cockpit reducer turns `aura.artifact` into a `local_artifact` display carrying the asset id,
  never a raw path (`web/src/chat/sseAdapter.ts:253-277`). `LocalArtifactDisplay` already has one
  inline branch by MIME, for HTML (`web/src/chat/displays/LocalArtifactDisplay.tsx:24-35`).
- Every tool call renders through the single `tools.Fallback` seam
  (`web/src/chat/ExternalStoreChat_messages.tsx:350-441`); rich rendering is reserved for payloads
  the trusted normalizer `internal/agent/display` produced or the reducer synthesized.
- Telegram delivers every artifact with `sendDocument` (`internal/channels/telegram/artifact.go:50-69`).
- A turn spends on the identity's own OpenRouter key: `IdentityLLMResolver.SnapshotFor` runs
  `identitykey.Decide` and refuses on no key or no credit, never substituting the services key
  (`internal/runner/runner_identity_llm.go:116-170`, wired at `cmd/aura/serve.go:232`). The
  snapshot's context key is private to `internal/runner` (`runner_llm_runtime.go:66-70`).
- The loop budget caps a turn at `AURA_LOOP_MAX_WALLCLOCK_SEC`, default 300 s
  (`internal/agent/budget.go:32-43`); each tool runs under `budget.NodeTimeout()`
  (`internal/agent/llm_agent_tool.go:195-198`).
- A background shell's completion wakes its conversation through
  `shellCompletionDispatcher` → `Runner.WakeWithSteer`, one goroutine per conversation, coalescing
  completions (`cmd/aura/shell_completion.go:33-137`). The wake message tells the agent to call
  `shell_poll` exactly once.
- Assets know document, image and audio; any other modality is `ErrAssetUnsupported`
  (`internal/assets/limits.go:49-90`). Attachments reach the model with their `asset_id`
  (`internal/assets/context.go:24-43`).
- The LLM model is picked with `ModelPicker`, assistant-ui's model-selector element over
  `GET /api/settings/llm-models` (`web/src/settings/ModelPicker.tsx`,
  `internal/agui/settings_llm_models.go`). The web project is a shadcn project with the
  `@assistant-ui` registry configured (`web/components.json`).

Inventory before invention. Existing MCP servers were evaluated
(`stabgan/openrouter-mcp-multimodal`, `thesimj/rust-openrouter-mcp`) and not adopted: Aura's MCP
bridge keeps only `TextContent` and `structuredContent` and skips images and resource links
(`internal/mcp/result.go:31-56`), so their media never reaches the conversation, and their files
land on the server's filesystem where `send_file` cannot read them. Hermes Agent
(`NousResearch/hermes-agent`, `tools/video_generation_tool.py`, `plugins/video_gen/openrouter`)
contributed the catalog clamp and the rule that content is downloaded only from the configured
origin; its blocking `video_generate` (900 s) does not fit Aura's 300 s turn.

### What the measurement does not show

- Any model other than the two above, and any latency distribution: one run each.
- `aspect_ratio` and `input_references` sent through `Images.Generate` via `option.WithJSONSet`
  (the typed params have neither field). Prompt and model only were measured.
- `frame_images` (image-to-video), `input_references` on video, `expired` jobs, content-policy
  refusals, a 402 from a key over its limit.
- The shape of per-endpoint image pricing (`GET images/models/{id}/endpoints`): the probe's parse
  came back empty. Read the endpoint's documentation before writing the formatter.
- Whether `SpendOverview` splits spend per identity key.

## Decisions

1. Generation is an Aura capability, served by OpenRouter only. No local sidecar.
2. Two native Go tools, `image_generate` and `video_generate`, on the installed openai-go client.
   No MCP server, no hand-written HTTP client.
3. The operator picks the image and video model the way they pick the LLM; the agent never picks
   a model.
4. Every generation spends on the identity's own key through the same credit decision as its LLM
   turns.
5. A video is produced by one detached job watcher with two outcomes: inline in the same turn when
   it finishes within a short wait, otherwise announced later by waking the conversation. Once its
   job row is stored, a paid clip is never lost, including across a restart. The submission
   interval before that row exists is excluded: its outcome is unknown and it is never re-sent
   (§3, Recovery boundary).
6. v1 covers text-to-image, image editing with references, text-to-video and image-to-video.
7. The cockpit renders generation with assistant-ui's `image` and `image-generation` elements,
   installed from the registry and restyled; video gets its own player in the same card: the
   browser's native controls, click to play, streamed from the asset with HTTP Range (§5).
8. Telegram shows images as photos and videos as videos. WhatsApp is out of v1.

## Design

### 1. Models, key and spend

**Settings.** Two rows, `AURA_IMAGE_MODEL` (default `microsoft/mai-image-2.6`) and
`AURA_VIDEO_MODEL` (default `minimax/hailuo-3-max`), added where `AURA_TTS_MODEL` lives:
`internal/settings/settings.go` allowed keys, `internal/config/config_knobs.go` catalog,
`web/src/settings/modelSettingsDefs.ts` (cloud-only). Both join `adminOnlySettingKeys`, next to
`AURA_LLM_MODEL`, and `callTimeSettingKeys` in `internal/agui/settings_api_authz.go`, so only an
admin writes them and a saved value is live at once, with no restart.

**Catalogs.** `GET /api/settings/image-models` and `GET /api/settings/video-models`, behind the
same capability as `llm-models` (`cmd/aura/serve_webui.go:209-211`). The daemon fetches
OpenRouter's `/images/models` and `/videos/models` and returns rows shaped for the picker. The same
fetch, cached for 5 minutes in the daemon, feeds the tools' clamp (§2).

**Picker.** `ModelPicker` stays one component and takes the row formatter as a prop instead of
calling `modelMeta` directly. Image rows show the price per image and the maximum number of
reference images. Video rows show the $/s range (from `duration_seconds*` and
`cents_per_second*` SKUs; a token- or megapixel-priced model shows no price rather than a wrong
one), the duration range, the resolutions and whether image-to-video is supported. Vendor groups,
search, free text, count and Refresh behave exactly as for the LLM.

**Key.** A port `MediaCredentials.For(ctx, identityID) (baseURL, apiKey string, err error)`,
implemented in `cmd/aura` over `IdentityLLMResolver.SnapshotFor`, is injected into both tools and
the watcher at serve boot, the way `SendFile.Router` and `SendFile.Assets` are. No key and no
credit map to the tool errors `no_key` and `no_credit`; the services key is never used. The
resolver's local-backend exemption carries no OpenRouter credential, so on a llama.cpp or Ollama
route the port answers `no_key` too: generation needs the OpenRouter route. The client is
`openai.NewClient(option.WithBaseURL(baseURL), option.WithAPIKey(apiKey))`, configured like
`openai_compat`.

**Spend.** `usage.cost` is returned in the tool result and stored on the video job. The key's
OpenRouter limit is the governor.

### 2. The tools

Both live in `internal/agent/tools/` (`image_generate.go`, `video_generate.go`), with the
OpenRouter calls, catalog clamp and job watcher in a new package `internal/mediagen`. Every file
stays under 600 lines. Both specs are `Deferred: true`; their summaries name "image", "video",
"picture", "animate" so `tool_search` surfaces them.

**`image_generate`**

| Param | Type | Notes |
|---|---|---|
| `prompt` | string, required | |
| `aspect_ratio` | enum `1:1 16:9 9:16 4:3 3:4 3:2 2:3` | clamped to the model's declared set; omitted when the model declares none |
| `reference_asset_ids` | string[] | images to edit or follow; refused with `unsupported` before any call when the model takes fewer than requested (none when it declares no `input_references`) |

Flow: credential → clamp against `supported_parameters` → load each reference through the
identity-scoped assets service (image modality only, each within `AURA_ASSET_MAX_IMAGE_BYTES`) and
encode it as `data:<mime>;base64,…` → `client.Images.Generate` with `aspect_ratio` and
`input_references` set through `option.WithJSONSet`, raw body captured with
`option.WithResponseBodyInto` → decode `b64_json`, extension from `media_type` → stage the bytes in
the run directory → ingest through the helper `send_file` already uses
(`ingestForDelivery`) → `aura.artifact` meta with `caption` = prompt.

Result preview: `{asset_id, mime_type, model, cost_usd, used: {...}, adjustments: [...], delivered}`,
where `adjustments` lists every clamped or dropped parameter in plain words.

Amended 2026-09-16 after review: references are never dropped. The first design truncated them
to the model's maximum with a note, which let an edit on a model without `input_references` go
out as a billed text-to-image that ignored the photo. A request the model cannot take whole is
now refused before any provider call, with a message that says nothing was generated and what
to do; which references to keep is the agent's choice, not the first N. The first-frame refusal
carries the same guidance. Aspect ratio, duration, resolution and audio still clamp with notes:
the nearest value still produces the requested content.

**`video_generate`**

| Param | Type | Notes |
|---|---|---|
| `prompt` | string | required unless `job_id` is given |
| `job_id` | string | collect mode: deliver a finished job, or report its status |
| `duration` | integer seconds | nearest value in `supported_durations` |
| `resolution` | enum `480p 720p 768p 1080p 1K 2K 4K` | nearest supported by pixel height |
| `aspect_ratio` | enum `16:9 9:16 1:1 4:3 3:4 3:2 2:3 21:9 9:21` | nearest supported by ratio |
| `first_frame_asset_id` | string | animate an image; sent as `frame_images[{frame_type:"first_frame"}]`; rejected with `unsupported` when the model declares no frame images |
| `reference_asset_ids` | string[] | `input_references`; refused with `unsupported` when more than a declared maximum |
| `audio` | boolean | sent only when the model declares `generate_audio` |

Submit flow: credential → clamp → encode images as for `image_generate` →
`client.Post(ctx, "videos", body, &raw)` → insert the job row (§3) → start its watcher → wait on
the watcher up to `AURA_VIDEO_INLINE_WAIT_SEC` (default 45, always inside the tool's context) →
finished: emit the artifact exactly as `image_generate` does, and mark the job delivered; not
finished: return `{status: "in_progress", job_id, message}` telling the model the video will be
announced in this conversation.

Collect flow (`job_id` given): load the job for this identity → `completed` and not delivered:
re-stage the bytes from the asset, emit the artifact, set `delivered_at` → already delivered:
`already_delivered` → still running: `{status}` → `failed` or `expired`: the matching error.

Content is downloaded only from the configured base URL (`videos/{id}/content`), never from
`unsigned_urls`, so the key goes only to the host the operator chose. If the lint gate rejects the
deprecated `Videos.Get` / `Videos.DownloadContent`, the same paths are called with `client.Get`.

**Clamp.** Pure functions in `internal/mediagen/clamp.go` over the cached catalog rows: nearest
supported value, drop unsupported toggles (the API answers 400 for `seed` or `generate_audio` on
models that lack them), and record each change. A model missing from the catalog is sent
unclamped; OpenRouter validates.

### 3. Video jobs

**Table** `aura.media_job`, identity-scoped with fail-closed row-level security, the pattern
`0090_rls_fail_closed_assets` applies to `aura.assets`: `id`, `identity_id`, `conversation_id`, `tool_call_id`, `provider_job_id` (unique),
`model`, `request` (jsonb, the clamped body without image data, plus a reserved `_aura` object
with the reference asset IDs and the submission origin, never sent to the provider), `status`
(`pending | in_progress | completed | failed | expired | cancelled`, CHECK), `error`, `asset_id`,
`cost_usd`, `created_at`, `updated_at`, `completed_at`, `delivered_at`. Queries through sqlc. The
migration number is the next free slot when the phase lands (`ls internal/db/migrations/ | tail -1`),
never taken from this document.

**Watcher** (`internal/mediagen/watcher.go`, lifecycle owned by the daemon): polls
`videos/{id}` every 5 s with the job owner's credential, writes each status change, and stops on a
terminal status or after a 30-minute ceiling (`expired`). On `completed` it downloads the MP4,
ingests it as a video asset, stores `asset_id` and `cost_usd`, and closes the job's done channel.
Download retries never resubmit a generation.

**Wake.** Whether a finished job is delivered by a waiting tool call or by a wake is decided under
one lock: the tool registers as the job's waiter before it waits and deregisters when the wait runs
out, and the watcher reads the waiter under the same lock when the job turns terminal. With a
waiter, the tool delivers; without one, the watcher hands a completion to the daemon's
background-completion dispatcher. `delivered_at` makes a second delivery impossible either way.
`shellCompletionDispatcher` becomes that dispatcher (`cmd/aura/background_completion.go`): it keeps
its per-conversation goroutine and coalescing, and accepts shell and media completions, each
formatting its own line and steer source (`steer.SourceShell`, new `steer.SourceMedia`). The media
line reads: video job `<id>` finished with status `<s>`; call `video_generate` with
`job_id=<id>` exactly once to deliver it, then continue.

**Boot.** The daemon resumes watchers for `pending` and `in_progress` rows, and re-wakes the
conversation for `completed` rows with no `delivered_at`.

**Recovery boundary** (decided 2026-09-14). The recovery guarantee starts after a successful
Insert of the job row. The POST to OpenRouter and the Postgres transaction cannot be made atomic.
The Video API reference read on 2026-09-14 (submit, poll, content, models) documents no
submission idempotency key and no endpoint that lists jobs. The only other place a job ID
surfaces is the completion webhook (`callback_url` or a workspace default): it needs a public
HTTPS receiver, which Aura does not run, and its `X-OpenRouter-Idempotency-Key`
(`<job_id>-<status>`) deduplicates webhook deliveries, not submissions. That reading does not show
how the provider behaves on a duplicate body. So the submission interval is excluded: when the
provider has accepted a job but the response or the Insert is lost, the outcome is unknown, the
provider ID is known only to the interrupted process, and nothing ever POSTs again automatically.
After the Insert, the job is resumed from its durable provider ID. `delivered_at` gives one winner
per completed job. It does not prove that an external channel received the clip: a crash between
the claim and the tool result leaves the asset in identity storage and the job delivered, and a
later collect answers `already_delivered`. Telegram receipt stays best-effort.

| Fault | Expected evidence | Test |
|---|---|---|
| before acceptance | no remote job, no charge claim | `TestSubmitVideoNeverResendsAfterFailureOrLostResponse` (added by Task 6 to Task 4's `client_video_test.go`), 5xx case: one POST, an error, no job to persist |
| after acceptance / response lost | outcome unknown; never POST again automatically | same test, lost-response case: the connection closes after the body is read; one POST, an error (SDK retries are off) |
| response received / before Insert | remote ID known only to the interrupted process | excluded by the guarantee; Task 9 pins that the row is persisted before the inline wait starts |
| after Insert | same job resumed; one content download and one asset identity | Task 6 `TestMediaJobInsertRefusesDuplicateProviderID`, `TestMediaJobRecoverableOrdersByCreationAndRetainsIt`; Task 7 resume tests |
| after ingest / before Complete | the stable SourceRef `media-job:<id>` reuses the same asset | Task 7 ingestion-recovery test; Task 6 `TestMediaJobCompleteRequiresOwnedAcceptedVideoAsset` |
| after claim / before event | asset survives; external receipt is not proven | Task 6 `TestMediaJobClaimsOnceAndIsolatesOwners`, `TestMediaJobClaimRollsBackWhenTheAssetCannotBeBound`; Task 9 collect answers `already_delivered` |

### 4. Assets

`ModalityVideo` joins `internal/assets/limits.go` (`video/mp4`, `video/webm`, extensions `.mp4`,
`.webm`) with `AURA_ASSET_MAX_VIDEO_BYTES`, default 52428800 (50 MiB, the `send_file` and Telegram
upload ceiling). Images use the existing `ModalityImage`. A clip above the ceiling fails the job
with `too_large`; its cost has already been charged and is recorded.

### 5. Cockpit

**Elements.** `npx shadcn@latest add @assistant-ui/image @assistant-ui/image-generation`, restyled
to the cockpit tokens. New strings in English and Italian.

**While running.** `ToolFallback` gains a branch for `image_generate` and `video_generate` whose
part is running: a `GenerationFrame` built on the ImageGeneration element (dot grid over the
blurred gradient) sized to the requested `aspect_ratio`, with the prompt from `argsText` rendered as
plain text. It renders inline, not inside the collapsed tool row. The frame shows the elapsed time
as `m:ss`, counted from when the running part mounted and ticking once a second. There is no
percentage: the video client reads only the job's status, so Aura has no progress value to show.

**After the tool returns.**

- Artifact attached → the reducer's `local_artifact` display, unchanged in shape.
  `LocalArtifactDisplay` gains two branches next to HTML, keyed by `previewKind`, which gains
  `video`:
  - `image/*` → the Image element with fullscreen zoom and actions (download through the existing
    `/api/assets/{id}/download`, copy), fed by `useAssetContent`. SVG stays download-only.
  - `video/*` → `VideoPreview`, `<video controls playsInline preload="metadata">` with the
    browser's native controls. It never autoplays, including when the thread is reopened. Its
    `src` is a same-origin asset URL, not a blob, so a clip starts playing and seeks without being
    downloaded whole. That URL serves the stored bytes inline with the accepted video
    `Content-Type` and `Accept-Ranges: bytes`: `206 Partial Content` for a satisfiable `Range`,
    `416` for an unsatisfiable one. It applies the same identity ownership checks as
    `/api/assets/{id}/download`, and the public share page gets the equivalent token-scoped URL.
    None of the asset routes serve `Range` today (`internal/agui/assets_api.go`, read on
    2026-09-14). Range handling reuses `net/http.ServeContent` or the object store's ranged read;
    it is not a hand-written parser.
  Media delivered by `send_file` gains the same previews.
- `{status: "in_progress"}` → the frame stays, without animation or elapsed time, with a static
  "arriving in this chat" label: the part is replayed when the thread is reopened, and a job that
  finished hours earlier must not look like it is still running. The video appears in the wake
  turn.

The player and loading choices (click to play, native controls, Range streaming, elapsed time)
were made with the operator on 2026-09-14. No measurement proves them yet: acceptance 2 and 4 are
where they get checked in a browser.
- `content_blocked` → the Image element's content-filter card. Any other error → the existing
  `ToolActivityCard` error rendering.

Regeneration uses the message action bar's existing Reload.

### 6. Telegram

`artifact.go` chooses by the descriptor's `mime_type` (Bot API limits read on 2026-09-14):

- `image/*` up to 10 MB → `sendPhoto`; larger → `sendDocument`.
- `video/*` up to 50 MB → `sendVideo` with `supports_streaming`; larger → a text message saying the
  video is available in the cockpit.
- Anything else → `sendDocument`, unchanged.

While either generation tool runs, the status consumer sends the chat action `upload_photo` or
`upload_video` every 4 s (an action lasts at most 5 s).

### Error contract

Tool errors are `{error, message}` results the model adapts to, never Go errors: `no_key`,
`no_credit`, `unsupported` (for example a first frame on a text-only model), `asset_not_found`,
`content_blocked`, `model_rejected` (OpenRouter's message attached), `job_failed`, `job_expired`,
`too_large`, `already_delivered`.

### Configuration

| Name | Default | Where |
|---|---|---|
| `AURA_IMAGE_MODEL` | `microsoft/mai-image-2.6` | settings row, cockpit Models pane |
| `AURA_VIDEO_MODEL` | `minimax/hailuo-3-max` | settings row, cockpit Models pane |
| `AURA_VIDEO_INLINE_WAIT_SEC` | `45` | knob catalog |
| `AURA_ASSET_MAX_VIDEO_BYTES` | `52428800` | knob catalog, next to `AURA_ASSET_MAX_IMAGE_BYTES` |

The PRD env catalog gains these four rows.

## Out of scope for v1

- WhatsApp delivery: the bridge sends only a `media_path` under its own allow-listed roots
  (`whatsapp-bridge/media_path.go`), which needs a media root shared with Aura.
- An "Animate" button on an image card, multi-image galleries, partial-image streaming.
- Video edit, extend and remix.
- Letting the agent choose a model per call.
- Lifting images and resource links from MCP tool results into assets.

## Acceptance

1. `GET /api/settings/image-models` lists `microsoft/mai-image-2.6`; `GET /api/settings/video-models`
   lists `minimax/hailuo-3-max` with a $/s price. A non-admin write of either model row is refused.
2. In a fresh cockpit conversation, "generate an image of …" makes the agent call `image_generate`
   (turn trace), shows the generation frame while it runs, then the image inline with zoom. The
   asset row is `image/png`, the result carries `cost_usd`, and the image is inspected visually.
3. After uploading a photo, "make it night" calls `image_generate` with that upload's asset id in
   `reference_asset_ids`.
4. "A 5-second video of …" delivers the video inline in the same turn.
5. With `AURA_VIDEO_INLINE_WAIT_SEC=1` the tool returns `in_progress`, the conversation is woken,
   the agent calls `video_generate` with the `job_id`, and the video appears once. A second collect
   returns `already_delivered`.
6. Aura restarted right after a submit resumes the job on boot and delivers the video exactly once.
7. "Animate this" on a generated image calls `video_generate` with `first_frame_asset_id`.
8. On Telegram, a generated image arrives as a photo message and a video as a video message.
9. An identity with no credit gets `no_credit` and no request reaches OpenRouter.
10. Across 5 fresh conversations the agent finds both tools through `tool_search` without being told
    their names.
11. Gates: coverage at or above 85% across the tag matrix, mutation at or above 70% on
    `internal/mediagen/clamp.go` and the watcher state machine (CI), race detector and goleak clean,
    vitest at or above 85% and Stryker at or above 70% on the new web components.

## Test plan

- **Unit.** Clamp tables plus a property test (the chosen value is always in the supported set,
  and unchanged when already supported); price label; request body building; job state
  transitions; dispatcher coalescing of shell and media completions; `ModalityVideo` validation;
  artifact descriptor for both tools.
- **httptest OpenRouter.** Images with `b64_json` and `media_type`; video submit with the
  whitespace-padded body; poll sequences to `completed`, `failed` and `expired`; content download;
  400 on an unsupported parameter; 402; content-policy failure.
- **db_integration.** `aura.media_job` store with row-level security (identity A cannot read B's
  job), the resume query, `delivered_at` exactly once.
- **Asset streaming (Go).** A single range, an open-ended range, a suffix range, an unsatisfiable
  range (416), a foreign identity's asset (refused like download), and the share-token URL.
- **Web.** `ToolFallback` generation branches with the elapsed counter (fake timers),
  `LocalArtifactDisplay` image and video branches (the video `src` is the asset URL and no blob is
  fetched), picker formatters.
- **Live E2E.** Acceptance 2-9 on the running stack, driven by the real agent. Paid runs are
  batched and started only after a go, with the estimated cost stated first.

## Order of work

1. PRD amendment recording the 2026-09-14 measurement and its limits.
2. `ModalityVideo` in assets.
3. `internal/mediagen`: client, catalog, clamp.
4. `image_generate`.
5. Migration, job store, watcher, dispatcher generalization.
6. `video_generate`.
7. Settings rows, catalog endpoints, picker formatter.
8. Cockpit elements and displays.
9. Telegram delivery.
10. Live E2E.
