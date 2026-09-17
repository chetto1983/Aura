# Video Studio and the LTX provider — design

Date: 2026-09-17. Status: design approved in conversation, section by section; this document
is the record the plan is written from.

## Goal

Give every signed-in identity a **Studio**: a cockpit surface where a video is generated from a
form, without an agent turn, and kept in a personal gallery. Then add **LTX** (Lightricks,
`api.ltx.io`) as a second video provider, usable from the Studio and from `video_generate`.

The build order is the operator's call: the Studio first, on the OpenRouter video models Aura
already supports; LTX second.

## Decisions taken with the operator

| Question | Decision |
|---|---|
| Where LTX is used | Inside Aura's video pipeline, as a second provider — not an MCP server. |
| Who spends the LTX key | One deployment key, admins only. |
| Provider shape | A `VideoProvider` contract in `mediagen`; OpenRouter and LTX implement it. |
| Where Studio results land | A gallery in the Studio; no agent turn, no conversation. |
| Studio scope | Video only. Images stay in chat. |
| LTX client | Hand-written `net/http`, checked by a contract test against LTX's OpenAPI document. |
| Order | Studio (frontend and its API) before LTX. |

## Inventory that shaped the design (2026-09-17)

- **LTX has no generation MCP.** `docs.ltx.io/_mcp/server` searches the documentation only;
  `hoodtronik/ltx-trainer-mcp` trains LoRAs locally.
- **OpenRouter lists no LTX model.** `GET /api/v1/videos/models` returned 29 models, none from
  Lightricks, so `video_generate` cannot reach LTX today.
- **LTX publishes no SDK in any language.** It publishes an OpenAPI 3.1 document
  (`https://docs.ltx.io/openapi.json`, 46,568 bytes on 2026-09-17).
  - The community clients are unusable here: `cluely/unmodel` is TypeScript;
    `dredozubov/muginn-core/adapters/ltxvideo` is v0.1.0-alpha.1 and its repository returns 404.
  - `oapi-codegen` would generate all 14 operations. The deadcode gate would flag the ones Aura
    never calls, and it adds a dependency and a generation step.
  - `github.com/google/jsonschema-go`, already a direct dependency, can validate request bodies
    against the document's schemas.
- **OpenRouter declares per video model:** `supported_resolutions`, `supported_aspect_ratios`,
  `supported_durations`, `supported_frame_images` (15 of 29 models list both `first_frame` and
  `last_frame`), `generate_audio` and `pricing_skus`. It declares no frame rate.
- **The per-second SKUs match the costs measured on 2026-09-17** for `google/veo-3.1-lite`, 4 s:
  - `duration_seconds_without_audio_720p` 0.03 × 4 ≈ $0.1188;
  - `duration_seconds_with_audio_720p` 0.05 × 4 ≈ $0.198;
  - `duration_seconds_with_audio` 0.08 × 4 ≈ $0.3168.

  This convention was measured on one model only.
- **Parts of the cockpit to reuse:**
  - the shadcn registry, already configured in `web/components.json`, with the `@assistant-ui`
    registry;
  - the owned copies of `model-selector` and `image-generation`;
  - `PreviewByKind`;
  - the asset presign/finalize upload;
  - automatic `Idempotency-Key` headers from `web/src/api/idempotency.ts`.
- **Assets may have no thread.** `aura.assets.thread_id` defaults to `''`.
- **Shares are conversation-scoped only.** `POST /api/shares` takes a `conversation_id`, so a
  standalone asset cannot be shared without a new share kind.

## Part A — the Studio

### A1. Interface

The reference is LTX Studio's playground: operator screenshot
`D:\Immagini\Screenshots\Screenshot 2026-09-17 191937.png`, not committed. The layout is a form
column on the left and a stage on the right.

- **Placement.** A new cockpit mode, `studio`, listed with chat, graph and documents in
  `web/src/shell/modes.ts`. It is not an admin mode.
- **Form column, top to bottom:**
  1. **Start frame | End frame.** Two slots side by side.
     - Each slot takes an image uploaded from the computer (click or drop), or one picked from
       the identity's recent images.
     - A slot the selected model does not declare (`first_frame` / `last_frame`) is disabled,
       and says why.
     - A filled slot shows the thumbnail and a remove control.
  2. **Prompt.** A textarea.
  3. **Model.** The owned `model-selector`, each row with its per-second price range.
     - The first visit selects the deployment's `AURA_VIDEO_MODEL`.
     - After that, the viewer's last choice is kept in `localStorage`, wrapped in try/catch.
  4. **Resolution.** A select with the model's declared values.
  5. **Duration.** A select with the model's declared values.
  6. **Audio.** A switch, shown only when the model declares `generate_audio`. OpenRouter
     declares no frame rate, so the frame-rate control arrives with LTX (Part B).
  7. **Aspect ratio.** A toggle group with the model's declared ratios.
  8. **Footer.** `Reset` and **Generate** (`Ctrl+Enter`, shown with `kbd`).
     - The Generate button carries the estimated cost, "≈ $0.12".
     - When no price is declared for the chosen options, the button says the price is unknown,
       never $0.
- **Defaults are the cheapest declared options:** the lowest resolution, the shortest duration
  and audio off. Changing model keeps a value the new model declares and otherwise falls back
  to that model's cheapest.
- **Stage column:**
  - **While a job runs:** the owned `image-generation` frame, at the chosen aspect ratio, with
    the prompt and an elapsed-time readout.
  - **When done:** the video player (`PreviewByKind`) with **Download**.
  - **Below:** the gallery of the identity's Studio generations, newest first, as cards with
    thumbnail (the video's first frame), status, model, duration, cost and prompt. Clicking a
    card shows it on the stage; **Reuse** loads its options and frames into the form.
  - **With no generation yet:** an empty state that explains the form. There is no
    template/example panel.
- **Phone width.** The form stacks above the stage, with a 16px gutter and no horizontal
  scroll.
- **Components added from the shadcn registry:** `select`, `toggle-group`, `switch`, `field`,
  `kbd`.
- **Styling.** The cockpit's theme tokens, light and dark; strings in `it` and `en`.
- **Errors** appear on the job's card with the tool codes' meaning:
  - `no_key`: connect OpenRouter;
  - `no_credit`: credit exhausted;
  - `unsupported`: the model cannot do what was asked;
  - `content_blocked`, `model_rejected`, `job_failed`, `job_expired`;
  - `outcome_unknown`: the request may already be billed, so do not generate it again.
- **Double submit.** Generate is disabled from click until the server answers. A replayed
  request with the same `Idempotency-Key` returns the stored answer and never submits twice.
- **Deviation from the approved section.** The approved section listed **Share** next to
  Download. Shares are conversation-scoped, so sharing a Studio clip needs a new share kind,
  which is left out of this design. Download only.

### A2. HTTP API

All routes are authenticated and scoped to the calling identity. Every mutating route is added
to `httpMutationRoutes` (`internal/agui/idempotency_http.go`).

| Route | Purpose |
|---|---|
| `GET /api/studio/video-models` | Models for the form, readable by every identity. |
| `POST /api/studio/videos` | Submit one video. Mutation `studio_video_create`. |
| `GET /api/studio/videos?before=<cursor>&limit=<n>` | The gallery, newest first. `limit` ≤ 48. |
| `GET /api/studio/videos/{id}` | One job, polled every 5 s by the stage while it is active. |
| `GET /api/studio/images?limit=<n>` | The identity's recent accepted image assets, for the frame picker. |

**`GET /api/studio/video-models`**

- Each model row carries:
  - `id`;
  - `durations[]`, `resolutions[]`, `aspect_ratios[]`;
  - `first_frame` and `last_frame` (booleans);
  - `audio` (boolean);
  - `prices[]`: `{resolution, audio, usd_per_second}`.
- The price matrix is resolved in Go by a new `mediagen.VideoSecondPrice(skus, resolution,
  audio)`:
  1. The resolution-suffixed audio SKU wins.
  2. Then the unsuffixed audio SKU.
  3. `cents_per_second*` is divided by 100.
  4. An undeclared price is omitted.

  The browser multiplies by the duration, so the price rule has one home.
- The catalog is public and uses no credential.
- The response also names the deployment's default model.

**`POST /api/studio/videos`**

- Body: `{model, prompt, duration, resolution, aspect_ratio, audio, first_frame_asset_id,
  last_frame_asset_id}`.
- It runs the same submission as `video_generate`: credential, catalog entry, clamp, frame
  read, persisted request, one POST.
- It answers `201` with the job, including its `adjustments`, as soon as the provider accepts.
  It never waits inline.
- A refusal answers `4xx` with `{error, message}`, using the tool's codes and messages.

**Job DTO**

`{id, status, model, prompt, used, adjustments, cost_usd, asset_id, error, created_at,
completed_at}`. `used` is the options the provider received. `asset_id` is set once completed,
and the stage streams it from `/api/assets/{id}/stream`.

### A3. Shared submission

- **One submission path.** The tool's submit steps (credential → model → clamp → frames →
  `JobRequest` → `SubmitVideo` → `Insert`) move into `mediagen` as a `VideoSubmitter`.
  `video_generate` and the Studio handler both call it, so the charge-once rules have one
  implementation.
  - `video_generate` keeps its inline wait on top.
  - The Studio tracks the job without an inline waiter.
- **End frame, for both callers.**
  - `VideoInput` gains `LastFrameAssetID`.
  - `ClampVideo` refuses it (`unsupported`, nothing billed) when the model does not declare
    `last_frame`.
  - The request adds `{"frame_type":"last_frame"}`.
  - `video_generate` gains `last_frame_asset_id`, and the media skill's tool rules name it.
- **The Studio model is chosen per request**, not read from `AURA_VIDEO_MODEL`. It must be in
  the catalog, or the request is refused before submit.

### A4. Data

Migration: the next free slot, measured with `ls internal/db/migrations/ | tail -1` when the
task lands (0128 is the head on 2026-09-17). The same commit updates the head pin in
`db_unit_test.go`.

```sql
ALTER TABLE aura.media_job
  ADD COLUMN origin text NOT NULL DEFAULT 'chat' CHECK (origin IN ('chat', 'studio'));
ALTER TABLE aura.media_job ADD CONSTRAINT media_job_origin_scope CHECK (
  (origin = 'chat'   AND conversation_id <> '' AND tool_call_id <> '') OR
  (origin = 'studio' AND conversation_id =  '' AND tool_call_id =  ''));
CREATE INDEX media_job_studio_idx ON aura.media_job (identity_id, created_at DESC, id DESC)
  WHERE origin = 'studio';
```

- **Existing rows.** Every chat row must already have a non-empty conversation and tool call,
  or the `CHECK` fails at migration. Measured on the live database on 2026-09-17: 6 rows, 0
  with an empty conversation or tool call. The count is repeated before the migration lands.
- **Delivery.** A Studio job is delivered when it completes: `CompleteMediaJob` sets
  `delivered_at` for `origin = 'studio'`, so recovery never offers it to a conversation.
- **Wake.** `mediagen.Completion` carries the origin, and the background completion dispatcher
  ignores Studio completions: there is no conversation to wake.
- **Restart.** Active Studio jobs are resumed by the watcher like chat jobs.
- **Asset.** The clip is stored as an accepted agent video with `thread_id = ''`, the same
  asset shape `CompleteMediaJob` already checks against the job's `conversation_id`.

### A5. Tests and acceptance

- **Go.**
  - Unit tests for the submitter (including the refusal paths), `VideoSecondPrice` and the
    handlers.
  - `db_integration` tests for the migration, the Studio history query and the delivered-on-
    completion rule, on the disposable database only.
  - Race and goleak as in the rest of `mediagen`.
  - The coverage and mutation policy entries are updated for the new scopes; mutation runs in
    CI only.
- **Web.**
  - vitest for the capability-driven form (disabled slots, fallbacks on model change, cheapest
    defaults), the cost estimate and the gallery states.
  - The jscpd duplication gate.
- **Live, paid, only with the operator's go.**
  - One Playwright run on the running stack: generate from the Studio with the cheapest
    declared options of `google/veo-3.1-lite` (720p, 4 s, audio off, measured ≈ $0.12).
  - The clip must play on the stage, appear in the gallery after a reload, and leave no
    conversation turn.
  - A second, optional run uses a start and an end frame.

## Part B — LTX as a second provider

Designed and approved before the order changed; built after Part A.

- **Contract.** `mediagen.VideoProvider` has `Submit`, `Status` and `Download`.
  - The OpenRouter client is the first implementation, unchanged in behaviour.
  - `internal/mediagen/ltx` is the second: `POST /v2/text-to-video`, `POST /v2/image-to-video`
    and `GET /v2/{endpoint}/{id}`, with the API key as a Bearer token.
  - The status endpoint is derived from the persisted request: a first frame means
    `image-to-video`.
- **Routing.** LTX model IDs carry the `ltx/` prefix: `ltx/ltx-2-3-fast`, `ltx/ltx-2-3-pro`,
  `ltx/ltx-2-5-fast`, `ltx/ltx-2-5-pro`. A job records `https://api.ltx.io` as its origin, so
  the watcher resolves the right provider after a restart.
- **Credential.**
  - A new secret setting, `LTX_API_KEY` (upstream naming, like `TELEGRAM_BOT_TOKEN`), encrypted
    in `aura.settings`.
  - Submitting requires `governance.write`; everyone else gets `no_key`, and the Studio lists
    LTX models only to admins.
  - Polling and downloading a job already paid use the key without the role check.
- **Catalog.** Declared in code, because LTX has no model list. It cites the Models, LTX-2.3,
  LTX-2.5 and Pricing pages with their date:
  - resolutions 720p, 1080p, 1440p and 4K, landscape or portrait (16:9 / 9:16);
  - the duration matrix per model and frame rate (for example `ltx-2-3-fast` at 24/25 fps:
    6–20 s);
  - frame rates 24, 25, 48 and 50;
  - first and last frame;
  - audio;
  - per-second prices per resolution (cheapest: `ltx-2-3-fast` 720p, $0.03/s, 6 s minimum,
    so $0.18).
- **Frame rate.** The Studio shows the Frame rate select only for a model that declares frame
  rates.
- **Mapping.**
  - `720p` + `16:9` becomes `1280x720`.
  - Frames become `image_uri` / `last_frame_uri` data URIs, refused above LTX's 7 MB encoded
    limit before submit.
  - `processing` becomes `in_progress`.
- **Download.**
  - `result.video_url` is fetched as soon as the job completes, because the link expires.
  - The fetch is HTTPS-only, carries no `Authorization` header (another host), refuses IP
    literals and private addresses, and is byte-bounded like every other clip.
- **Cost.** LTX returns no cost, so it is computed from the declared price × billed seconds.
  The paid acceptance run compares it with the LTX console.
- **Errors.**

  | LTX answer | Aura code |
  |---|---|
  | 401, 403 | `no_key` |
  | 402 | `no_credit` |
  | 422 | `content_blocked` |
  | 400 | `model_rejected` |
  | 429, 5xx | `job_failed`: nothing was accepted, so a later try is safe |
  | no answer, or an unusable 2xx | `outcome_unknown` |

- **Contract test.** It validates every body the client builds against the committed copy of
  `openapi.json` with `google/jsonschema-go`.
- **Chat.** `video_generate` routes by the configured model's prefix. If an admin sets an LTX
  model for chat, a non-admin's chat video answers `no_key`. There is no silent fallback.

## Out of scope

- Sharing a Studio clip (needs a new share kind).
- Image generation in the Studio.
- Templates and examples.
- LTX's retake, extend, reframe, HDR and audio-to-video.
- A per-identity LTX key.
- An LTX MCP server.

## What this design does not prove

- **The SKU naming rule** behind `VideoSecondPrice` was checked against the measured costs of
  one model. Another model's SKUs may be named differently; its estimate then shows as unknown
  rather than guessed.
- **LTX's computed cost** is the documented price, not an invoice, until the paid run compares
  it.
- **The migration's `CHECK`** held for the 6 live rows of 2026-09-17. It says nothing about
  another deployment's rows, which the migration itself checks when it runs.
