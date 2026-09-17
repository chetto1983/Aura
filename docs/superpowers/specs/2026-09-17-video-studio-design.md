# Studio — design

Date: 2026-09-17. Status: design approved in conversation, section by section; this document
is the record the plan is written from.

## Goal

Give every signed-in identity a **Studio**: a cockpit surface where an image or a video is
generated from one bar, without an agent turn, and kept in a personal history. It runs on the
OpenRouter image and video models Aura already supports.

## Decisions taken with the operator

| Question | Decision |
|---|---|
| Where Studio results land | A history owned by the identity; no agent turn, no conversation. |
| Studio scope | Video **and** images: one bar with a mode pill. |
| Provider | OpenRouter, through the existing media pipeline. |
| Shape | BytePlus Lumina's bar, not LTX Studio's two-column form: "ancora più semplice". |
| From Lumina | Tiled option controls, a History side panel, model names with descriptions, an Advanced popover with seed. |
| Colours | Aura's, never Lumina's: the cockpit's theme tokens, light and dark. |
| LTX (Lightricks) | Evaluated and dropped on 2026-09-17: "LTX non si usa più, si fa solo lo studio nel cockpit". |

## Inventory that shaped the design (2026-09-17)

- **OpenRouter declares per video model:** `supported_resolutions`, `supported_aspect_ratios`,
  `supported_durations`, `supported_frame_images` (15 of 29 models list both `first_frame` and
  `last_frame`), `generate_audio`, `seed` and `pricing_skus`. It declares no frame rate, so the
  Studio has no frame-rate control.
- **It declares per image model** a `supported_parameters` map, among them `aspect_ratio` with
  its values and `input_references` with its maximum, plus the endpoint pricing lines Aura
  already reads per image and per output token.
- **Both catalogs carry `name` and `description`**, which Aura drops today.
- **`seed` is a documented top-level parameter** of `POST /videos`: "If specified, the
  generation will sample deterministically, such that repeated requests with the same seed and
  parameters should return the same result. Determinism is not guaranteed for all providers."
  (OpenRouter API reference, read 2026-09-17.)
- **The per-second SKUs match the costs measured on 2026-09-17** for `google/veo-3.1-lite`, 4 s:
  - `duration_seconds_without_audio_720p` 0.03 × 4 ≈ $0.1188;
  - `duration_seconds_with_audio_720p` 0.05 × 4 ≈ $0.198;
  - `duration_seconds_with_audio` 0.08 × 4 ≈ $0.3168.

  This convention was measured on one model only.
- **An image costs about $0.05** on `gpt-image-1-mini` at 3:2, measured 2026-09-16. Image
  models are billed per image or per output token, and a token-billed model has no per-image
  price to show before generating.
- **Image generation is synchronous:** `Client.GenerateImage` returns the bytes, the type and
  the cost. Video generation is a job the watcher supervises.
- **Lumina's two pages share one bar** (read with a browser on 2026-09-17): the image page adds
  a reference-mode pill and a size panel, both provider-specific, and the rest is identical.
- **Parts of the cockpit to reuse:** the shadcn registry with the `@assistant-ui` registry and
  the installed `radix-ui`; the owned `model-selector`, `image-generation` and
  `GenerationFrame`; `PreviewByKind`; the asset presign upload; the automatic
  `Idempotency-Key` header; react-query.
- **Assets may have no thread.** `aura.assets.thread_id` defaults to `''`.
- **Every JSON mutation needs `Idempotency-Key`**, and its body must be valid JSON of at most
  1 MiB (`normalizeHTTPMutation`), so an upload cannot be multipart through a mutating route.
- **`assets.Service.Finalize` always enqueues processing.** An uploaded image gets a paid
  vision summary and a document id for the knowledge index. A Studio input image is an input to
  one generation, not knowledge, so it must not be processed.
- **The background completion dispatcher drops a completion with an empty conversation**, so a
  Studio video job wakes nobody, with no new code.
- **Shares are conversation-scoped only**, so a Studio result cannot be shared without a new
  share kind.

## A1. Interface

The reference is BytePlus Lumina's image and video pages
(`ai.byteplus.com/lumina/en/model/{image,video}`), read with a browser on 2026-09-17, plus the
operator's screenshots. **Only the layout is taken from it. The colours, type and motion are
Aura's**: the cockpit's theme tokens, light and dark, never Lumina's black.

- **Placement.** A new cockpit mode, `studio`, placed after chat in `web/src/shell/modes.ts`.
  It is not an admin mode.
- **Page.**
  - **Centre.** Before anything is generated, a centred title with gradient text ("Dai vita
    alla tua idea" / "Bring your idea to life"), the gradient built from the cockpit's accent
    tokens. Afterwards the selected generation fills the centre: the running frame, or the
    image or clip with **Download** and **Reuse**. The generation bar sits at the bottom,
    centred, at most 56rem wide.
  - **Right.** A **History** panel, collapsible, listing the identity's generations newest
    first as small cards (thumbnail, status, prompt), with a prompt search box. Selecting a
    card shows it in the centre. Its empty state says nothing has been generated yet. The
    toggle is a button in the top right, and the panel's state is remembered per viewer in
    `localStorage`. Below phone width it is a drawer over the page.
- **Generation bar, top row:**
  - **`+ frame` / `+ reference` tile,** slightly rotated like the reference's. It opens a menu
    with **Upload from computer** and **From your images**. A chosen image shows its thumbnail
    and a remove control.
    - **In video mode** it is the start frame, and a second `+ end` tile appears when the model
      declares `last_frame` and a start frame is set.
    - **In image mode** it holds the reference images, up to the model's declared maximum; the
      empty tile disappears at the maximum and returns when one is removed.
    - A tile the model cannot use is disabled, with the reason.
  - **Prompt.** A textarea that grows as it fills: "Describe the video scene you want to
    generate" or "Describe the image you want to generate".
- **Generation bar, bottom row, as pills:**
  - **Mode**: `Image` / `Video`. Switching keeps the prompt and reloads the rest from that
    mode's model.
  - **Model.** The owned `model-selector`, each row showing the catalog's name, its one-line
    description and its price (per second for video; per image, or per million output tokens,
    for image); the pill shows the name, not the id.
    - The first visit selects the deployment's `AURA_VIDEO_MODEL` or `AURA_IMAGE_MODEL`.
    - After that, the viewer's last choice per mode is kept in `localStorage`, wrapped in
      try/catch.
  - **Options.** A popover headed **Video settings** / **Image settings** with **Reset**:
    - **video:** aspect ratio as a grid of tiles, each with a rectangle icon drawn at that
      ratio; resolution as a row of tiles; duration as a slider over the declared durations,
      with the value beside it;
    - **image:** the same aspect-ratio tiles.

    Each control shows only the model's declared values and is hidden when it declares none.
    The pill summarizes them (`16:9 · 720p · 4s`, or `1:1`).
  - **Advanced** (a sliders icon), video only, headed **Advanced** with **Reset**:
    - **Seed**, a number, empty for "let the provider choose", shown only when the model
      declares `seed`;
    - **Sound**, a switch, shown only when the model declares `generate_audio`.
  - **Generate**, on the right: the estimated cost ("≈ $0.12") and `Ctrl+Enter`.
    - An image model billed per token has no per-image price, so the button says the price is
      unknown rather than showing $0. The same holds for any undeclared price.
    - It is disabled from click until the server answers. A replayed request with the same
      `Idempotency-Key` returns the stored answer and never generates twice.
- **Defaults are the cheapest declared options:** the lowest resolution, the shortest duration,
  audio off and no seed. **Reset** restores them. Changing model keeps a value the new model
  declares and otherwise falls back to that model's cheapest.
- **Centre states:**
  - **Running** (video only): `GenerationFrame` at the job's aspect ratio, with the prompt and
    the time elapsed since the job was created.
  - **Completed:** the image or clip (`PreviewByKind`), with model, cost, **Download** and
    **Reuse**, which loads the prompt, options, seed and input images back into the bar.
  - **Failed:** the error, with the tool codes' meaning:
    - `no_key`: connect OpenRouter;
    - `no_credit`: credit exhausted;
    - `unsupported`: the model cannot do what was asked;
    - `content_blocked`, `model_rejected`, `job_failed`, `job_expired`;
    - `outcome_unknown`: the request may already be billed, so do not generate it again.
- **Left out of the reference:** the Explore feed (Aura has no community gallery), the Audio
  mode, the reference-mode and pixel-size panels (provider-specific), "Portrait Gallery", "3D
  Director's Desk", the prompt expand control, discount badges and a frame-rate control (no
  OpenRouter model declares one).
- **Phone width.** The bar stays at the bottom, the centre is one column, History is a drawer,
  a 16px gutter and no horizontal scroll.
- **Components added from the shadcn registry:** `toggle-group`, `switch`, `slider`,
  `dropdown-menu`, `kbd`. The popover, dialog, input and button are already in the cockpit.
- **Strings** in `it` and `en`.
- **Download only.** Sharing a Studio result needs a new share kind, so it is left out.

## A2. HTTP API

All routes are authenticated and scoped to the calling identity. The mutating routes are added
to `httpMutationRoutes` (`internal/agui/idempotency_http.go`) and mounted behind
`agentRunCapability`, like the other cost-bearing routes. The reads inherit the session gate.

| Route | Purpose |
|---|---|
| `GET /api/studio/models?kind=image\|video` | Models for the bar. |
| `POST /api/studio/videos` | Submit one video. Mutation `studio_video_create`. |
| `POST /api/studio/images` | Generate one image. Mutation `studio_image_create`. |
| `GET /api/studio/jobs?kind=&before=<id>&limit=<n>` | The History panel, newest first. `limit` ≤ 48, default 24. The page polls the first page every 5 s while a listed job is active. |
| `GET /api/studio/library?limit=<n>` | The identity's recent usable images, for the frame and reference picker. `limit` ≤ 48. |
| `POST /api/studio/uploads/{id}/finalize` | Accept an image uploaded through `POST /api/assets/presign` as a Studio input, without processing. Mutation `studio_upload_finalize`. |

**`GET /api/studio/models`**

- The response is `{default_model, models[]}`. Each row carries `id`, `name` and `description`
  as the catalog declares them (the id when it declares no name), plus:
  - **video:** `durations[]`, `resolutions[]`, `aspect_ratios[]`, `first_frame`, `last_frame`,
    `audio`, `seed`, and `prices[] = {resolution, audio, usd_per_second}`;
  - **image:** `aspect_ratios[]`, `reference_max`, and either `usd_per_image_min/max` or
    `usd_per_million_tokens_min/max` — whichever the catalog declares, never invented.
- The per-second price is resolved in Go by `mediagen.VideoSecondPrice(skus, resolution,
  audio)`, which tries, in order:
  1. `duration_seconds_{with|without}_audio_{res}`;
  2. `duration_seconds_{with|without}_audio`;
  3. `duration_seconds_{res}`;
  4. `duration_seconds`;
  5. the same four with `cents_per_second`, divided by 100.
- The image price reuses `mediagen.ImagePrice` and `ImageTokenPricePerMillion`.
- The catalog is the tools' catalog on the live route. A non-OpenRouter route answers 409 with
  the settings picker's sentence.

**`POST /api/studio/videos`**

- Body: `{model, prompt, duration, resolution, aspect_ratio, audio, seed,
  first_frame_asset_id, last_frame_asset_id}`.
- The model must be listed by the catalog, or the request is refused (`unsupported`) before
  anything is sent.
- It runs the same submission as `video_generate`: credential, catalog entry, clamp, frame
  read, persisted request, one POST. It answers `201` with the record as soon as the provider
  accepts, and never waits inline.

**`POST /api/studio/images`**

- Body: `{model, prompt, aspect_ratio, reference_asset_ids}`.
- It runs the same generation as `image_generate`: credential, catalog entry, clamp, reference
  read, one paid call. The image is stored as the identity's accepted agent image with no
  thread, and the generation is recorded like a video job, already completed. It answers `201`
  with that record.

**Refusals** answer `{error, message}` with the tool's codes:

| Code | Status |
|---|---|
| `unsupported`, `model_rejected`, `asset_not_found`, `too_large`, `content_blocked` | 422 |
| `no_key` | 409 |
| `no_credit` | 402 |
| `job_failed`, `outcome_unknown` | 502 |

**Record DTO**

`{id, kind, status, model, prompt, used, adjustments, cost_usd, asset_id, error, created_at,
completed_at}`, where `used` is what the provider received: for video the duration,
resolution, aspect ratio, audio, seed and frame asset ids; for image the aspect ratio and the
reference asset ids.

**Uploads**

The browser presigns an image asset (`scope: "thread"`, `thread_id: ""`), PUTs the file, then
calls `POST /api/studio/uploads/{id}/finalize`. That route runs
`assets.Service.FinalizeUnprocessed`: the same checks as `Finalize` (object present, size
limit, hash and sniffed type), then accepted, with no processing enqueued. The asset must be
an image.

## A3. Shared generation

- **One video submission path.** The tool's submit steps (credential → catalog entry → clamp →
  frames → `JobRequest` → `SubmitVideo` → `Insert`) move into `mediagen` as a
  `VideoSubmitter`, used by `video_generate` and the Studio.
  - `video_generate` keeps its inline wait on top; the Studio tracks the job with no inline
    waiter.
- **One image generation path.** The tool's steps (credential → catalog entry → clamp →
  references → `GenerateImage`) move into `mediagen` as an `ImageGenerator`.
  - `image_generate` keeps the staging and the artifact delivery on top.
  - The Studio ingests the bytes as an asset and records the generation.
- **End frame.** `VideoInput`, `JobAudit` and the tool's schema gain `LastFrameAssetID`;
  `ClampVideo` refuses one the model cannot use (`unsupported`, nothing billed).
- **Seed.** `VideoInput` and `VideoRequest` gain `Seed *int`, and `Model` gains the declared
  `Seed bool`. `ClampVideo` drops a seed the model does not declare, with a note, rather than
  refusing: nothing about the clip changes but its reproducibility.
- **Model names.** The catalog keeps each row's `name` and `description`.
- **Catalog lookup in one place.** The lookup that tolerates an unreadable catalog
  (`mediaCatalogEntry`) moves to `mediagen` as `Catalog.Entry`.

## A4. Data

Migration: the next free slot, measured with `ls internal/db/migrations/ | tail -1` when the
task lands (0128 is the head on 2026-09-17). The same commit updates the head pin in
`db_unit_test.go`.

```sql
ALTER TABLE aura.media_job
  ADD COLUMN surface text NOT NULL DEFAULT 'chat' CHECK (surface IN ('chat', 'studio')),
  ADD COLUMN kind    text NOT NULL DEFAULT 'video' CHECK (kind IN ('image', 'video'));
ALTER TABLE aura.media_job ADD CONSTRAINT media_job_surface_scope CHECK (
  (surface = 'chat'   AND conversation_id <> '' AND tool_call_id <> '') OR
  (surface = 'studio' AND conversation_id =  '' AND tool_call_id =  ''));
ALTER TABLE aura.media_job ADD CONSTRAINT media_job_image_is_finished CHECK (
  kind = 'video' OR (status = 'completed' AND delivered_at IS NOT NULL));
CREATE INDEX media_job_studio_idx ON aura.media_job (identity_id, created_at DESC, id DESC)
  WHERE surface = 'studio';
```

- **`surface`, not `origin`:** in `mediagen` a job's origin is already the submission base URL
  recorded in its request (`JobAudit.Origin`).
- **`kind`:** a video row is the durable provider job the watcher supervises; an image row is
  the record of a synchronous generation, inserted already completed and delivered, so the
  watcher — which only ever reads active or completed-undelivered rows — never sees one.
- **`provider_job_id`** is `NOT NULL UNIQUE`. An image has no provider job, so its row carries
  a locally minted `image-<uuid>`: nothing polls it, and the uniqueness still stops a replayed
  insert from recording one generation twice.
- **Existing rows.** Every chat row must have a non-empty conversation and tool call, or the
  `CHECK` fails at migration. Measured on the live database on 2026-09-17: 6 rows, 0 with an
  empty conversation or tool call.
- **Delivery.** A Studio video job is delivered when it completes: `CompleteMediaJob` sets
  `delivered_at` for `surface = 'studio'`, so recovery never offers it to a conversation.
- **Asset.** A result is stored as an accepted agent asset with `thread_id = ''`. The
  completion query's modality check follows the row's kind.
- **Picker.** A new query lists the identity's image assets that are not deleted and are
  usable (`accepted`, `processing`, `searchable`, `embedding` or `complete`), newest first.

## A5. Tests and acceptance

- **Go.**
  - Unit tests for `VideoSecondPrice`, the end-frame and seed clamps, the submitter, the image
    generator (refusal paths included) and the handlers.
  - `db_integration` tests for the migration, the history query, the image listing,
    `FinalizeUnprocessed`, the image record and the delivered-on-completion rule, on the
    disposable database only.
  - Race and goleak as in the rest of `mediagen`.
  - Coverage and mutation policy entries for the new scopes; mutation runs in CI only.
- **Web.** vitest for the form model (declared values, fallbacks on model and mode change,
  cheapest defaults, cost estimate), the option tiles, the frame and reference tiles, the
  History panel and the centre states; lint, typecheck, jscpd and knip.
- **Live, paid, only with the operator's go.** One Playwright run on the running stack:
  - an image with the cheapest declared image model (≈ $0.05);
  - a clip with `google/veo-3.1-lite` at 720p, 4 s, audio off (≈ $0.12).

  Both must appear in History, survive a reload, and leave no conversation turn.

## Out of scope

- Sharing a Studio result (needs a new share kind).
- Audio generation, the Explore feed, templates, portrait galleries and 3D staging.
- A frame-rate control (no OpenRouter model declares one).
- LTX, in any form.

## What this design does not prove

- **The SKU naming rule** behind `VideoSecondPrice` was checked against the measured costs of
  one model. Another model's SKUs may be named differently; its estimate then shows as unknown
  rather than guessed.
- **Seed** is documented as best-effort by OpenRouter itself ("Determinism is not guaranteed
  for all providers"), and this design does not test that two seeded runs match.
- **The migration's `CHECK`** held for the 6 live rows of 2026-09-17. It says nothing about
  another deployment's rows, which the migration itself checks when it runs.
