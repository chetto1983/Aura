# Media editing and video to the model — design

Date: 2026-09-19. Status: design approved in conversation, section by section, then revised
after an adversarial review (13 findings, the five serious ones re-checked against the code)
and two operator additions: video attachments, and video reaching the model when the selected
LLM accepts it. This document is the record the plan is written from. Evidence: spike 105
(`.planning/spikes/105-studio-media-editing/README.md`).

## Goal

1. Let the operator edit an image or a video they already have in Aura — a Studio result, a
   generated image or clip in chat, a file the agent delivered, a chat attachment — **in the
   browser, with no AI model**: crop, adjust, filter and annotate a photo; trim, crop, rotate
   and mute a clip.
2. Send a video attachment to the model **natively when, and only when, the selected LLM
   declares video input**, on every backend Aura drives through the OpenAI SDK (OpenRouter,
   llama.cpp, Ollama); otherwise the model keeps receiving the stored text reference.

## Decisions taken with the operator

| Question | Decision |
|---|---|
| AI in the editing path | None. Everything runs client-side. |
| Photo library | Filerobot Image Editor (`react-filerobot-image-editor`, MIT). |
| Video library | Mediabunny (MPL-2.0) plus a timeline Aura writes. |
| Where a result goes | Photo → the Studio library + download. Video → download. |
| Video operations | Trim, remove audio, rotate, crop. |
| Where "Edit" appears | Studio result; chat generated image and video; agent-delivered files (`PreviewModal`); chat attachments, **image and video**. |
| Shape | Shared lazy module `web/src/mediaEdit/` (approach 1 of 3; a separate route and inline editing were rejected). |
| Video to the model | Native only if the active model's capability source reports `video`; one rule for the web and Telegram. Speech stays text-only (unchanged). |
| Visual reference | Adobe Express *Trim video*, 123apps *Video Cutter* (desktop and phone), Pixlr Express, captured 2026-09-19. |

## Inventory that shaped the design (2026-09-19)

**Cockpit**
- `uploadStudioFrame` = presign → PUT → `POST /api/studio/uploads/{id}/finalize` (image-only,
  `serve_studio.go:169`); `GET /api/studio/library` lists it; `studioKeys.library()` is the
  query key (`useStudio.ts:37`). The finalize route requires an `Idempotency-Key`, which
  `installMutationIdempotency` attaches globally.
- **The Studio can be unwired**: every Studio route answers 503 while `s.studio == nil`
  (`internal/agui/studio_api.go:45`), which happens whenever a media dependency is missing.
- **A Studio record carries no file name or MIME type** (`StudioRecord`, `studioApi.ts`;
  `StudioStage` passes `mimeType: ''` and the prompt as the name), and
  `/api/assets/{id}/download` always answers `application/octet-stream`
  (`assets_render_api.go:21`). `getAsset(id)` (`chat/attachments/api.ts:27`) returns both.
- `PreviewModal` is opened only from `ArtifactsPanel` (agent-delivered files). Operator
  attachments render through `AttachmentCard` / `AttachmentImage`.
- **Video attachments already exist server-side**: `InferModality` classifies `.mp4`/`.webm`
  as `video` (`internal/assets/limits.go:50-70`) even when the browser sends the hint
  `unknown`, but the client type `AssetModality` has no `'video'` (`types.ts:22`) and
  `AttachmentCard` draws such an asset as a bare file card. `.mov` is refused by that
  allowlist.
- Image upload ceiling: `AURA_ASSET_MAX_IMAGE_BYTES`, default 25 MiB (`config.go:501`).
- No Content-Security-Policy on the cockpit (only artifact renders and MCP views).
- Reusable pieces: `useAssetContent(id, 'blob')` / `useBlobPreview` (credentialed fetch +
  object-URL lifetime), `chat/durationFormat.ts`, `components/ui/confirm-dialog.tsx`.

**Libraries (installed sources read)**
- Mediabunny 1.58.1: `copy` defaults to `{}` with `boundaryPolicy: 'expand'`; `cancel()`,
  `isValid`, `discardedTracks` (reasons include `undecodable_source_codec`);
  `CanvasSink.canvasesAtTimestamps` for thumbnails; `rotate` is applied **before** `crop`
  and in addition to the file's rotation metadata; `displayWidth` is post-rotation.
- Filerobot 5.0.0-beta.159: peers React 19; defaults `useBackendTranslations: true`
  (Scaleflex fetch) and `savingPixelRatio: 4`; `savingPixelRatio` **and**
  `previewPixelRatio` are required props in its typings; `onBeforeSave` returning `false`
  saves without the name/format modal (`SaveButton`, v5-dev `3087621`); its close button
  already shows a discard confirmation; `@scaleflex/ui` portals menus, pickers and modals to
  `document.body`; filter names are raw labels, never translated.

**Model side**
- `ContentCapabilitySource` already resolves the active model's input modalities per backend:
  OpenRouter `architecture.input_modalities` (`video` is an allowed token), llama.cpp
  `GET /props` `modalities`, Ollama `/api/show` `capabilities` (only `vision` is mapped).
  `ProviderContentCapabilities.SupportsMIME` maps `video/*` to the `video` modality.
- `TurnMediaLoader` (`internal/assets/turn_media_loader.go:56`), the AG-UI attachment loop
  (`internal/agui/server_context.go:72`) and Telegram (`bot_dispatch_media.go:29`) admit
  **images only**; `openai_compat.nativeContentPart` writes `image_url` and `input_audio`,
  nothing for video.
- **The three backends disagree on the wire.** OpenRouter: `{"type":"video_url",
  "video_url":{"url":"data:video/mp4;base64,…"}}` (formats mp4, mpeg, mov, webm). llama.cpp,
  since PR #24269 (merged 2026-06-08): `{"type":"input_video","input_video":{"data":"<base64>"}}`,
  decoded by an `ffmpeg` the server image must provide; issue #17660 was closed as stale, not
  by that PR. Ollama: no video content part and no `video` capability.
- openai-go v3.61.0's `ChatCompletionContentPartUnionParam` has no video variant, but
  `param.Override` serialises a caller-supplied part (`packages/param/encoder.go:89`).

## Part 1 — Editing (cockpit)

### Architecture

```
web/src/mediaEdit/
  EditMediaButton.tsx        the "Edit" button; hands its target to the provider
  MediaEditorProvider.tsx    mounted in AppShell; owns the open editor, lazy-loads it
  MediaEditorLayer.tsx       full-screen layer (not a Radix Dialog — see below)
  PhotoEditor.tsx            Filerobot + save to library / download
  VideoEditor.tsx            preview + tools + timeline + save
  VideoTimeline.tsx          filmstrip, two handles, playhead
  videoEdit.ts               pure: Mediabunny options, discarded-track guard, output names
  cropMath.ts                pure: preset → rectangle in post-rotation display space
  timecode.ts                pure: format and parse "mm:ss.s" (a round-tripping pair)
  filerobotTheme.ts          Aura CSS tokens → Filerobot theme
web/src/i18n/resources.mediaEdit.ts   UI strings + Filerobot's 128 keys, en + it
```

- The open editor lives in a `MediaEditorProvider` mounted in `AppShell`, not inside each
  button. `EditMediaButton` hands its target to the provider; a modal closes itself first
  (`PreviewModal` is a Radix Dialog whose focus trap would swallow an editor opened inside
  it). The share page is not under `AppShell`, a second guard beside `AssetSource.editable`.
- `EditMediaButton` takes `{ assetId, kind }` and, when the surface knows them, the MIME type
  and file name. The editor resolves the file name and MIME type with `getAsset(id)` — the one
  source that has them on every surface — and fetches the bytes through
  `useAssetContent(id, 'blob')`. The button renders nothing when the asset source is not
  editable, for any other kind, and for SVG and GIF when the surface knows the MIME type; on
  the Studio stage (which does not) the editor resolves it and says "This format cannot be
  edited here" if needed.
- `AssetSource` gains an optional `editable?: true`; `IDENTITY_SCOPED` sets it and the share
  tiers leave it out, following the `renderUrl` precedent.
- **Placements:** `StudioStage` (`StageActions`), `GeneratedImagePreview` (`ImageActions`
  gains an optional extra-action prop), `VideoPreview` (a small action row under the player),
  `PreviewModal` header (image and video kinds), `AttachmentCard` action row (ready image and
  video assets). `AssetModality` gains `'video'`; `AttachmentCard` keeps the file-card look
  for video (no inline player — out of scope).
- **Surface:** `MediaEditorLayer` is a full-screen layer portalled into `document.body` with
  `role="dialog"`, `aria-modal="true"` and a label; while it is open the app root is marked
  `inert`, Escape closes it and focus returns to the button. It is **not** a Radix Dialog:
  Filerobot's menus, colour picker and modals portal to `document.body` and a Radix focus
  trap / outside-dismiss would treat them as outside. A component test opens a Filerobot
  menu inside the layer and asserts the layer stays open.
- Both editors are `React.lazy` chunks (Filerobot ~273 KB gzip, Mediabunny ~143 KB gzip).

### Video editor

Layout after 123apps and Adobe Express, in Aura's tokens:

```
┌───────────────────────────────────────────────────────────────┐
│ clip.mp4                                     Reset        ✕   │
│ [ Trim ] [ Crop ] [ Rotate ] [ Audio ]                        │
│               ┌─────────────────────────┐                     │
│               │  preview (live rotate)  │  ← crop box         │
│               └─────────────────────────┘                     │
│ ▐░░░▌[▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓]▐░░░░░░▌   filmstrip            │
│  ▶   Start [00:02.5]  End [00:06.0]      1280×720      Save   │
└───────────────────────────────────────────────────────────────┘
```

- **Trim.** About 10 thumbnails from `CanvasSink`; two handles (start, end) dim what is cut;
  Start/End fields stay in sync. Handles are `role="slider"`: arrows ±0.1 s, Shift+arrows
  ±1 s. ▶ plays the selected range only.
- **Crop.** Presets Original · 1:1 · 9:16 · 16:9 · 4:3 · 3:4. The box moves, it does not
  resize. `cropMath` works in **display space after the total rotation** (file metadata +
  the user's turns), because Mediabunny rotates before cropping; sides are even for H.264.
- **Rotate.** 90° left / right; the preview rotates with CSS immediately.
- **Audio.** "Remove audio" switch, disabled when the clip has no audio track.
- **Save.** Always `copy: { boundaryPolicy: 'expand' }` plus the transforms; Mediabunny
  copies or transcodes per track. Trim and mute stay instant and frame-exact on MP4; crop and
  WebM transcode; whether rotate alone stays on the copy path is measured by a test. The output
  container follows the source MIME from `getAsset` (MP4 or WebM), never `blob.type`. A
  progress bar with **Cancel** shows while saving; the file downloads as
  `<base>-modificato.<ext>` and every object URL is revoked after use.
- **Memory.** The source and the output live in memory (`BlobSource` + `BufferTarget`). The
  source is an existing asset, so the server's video limit (`assets.Limits.MaxVideoBytes`,
  derived by `assetMaxVideoBytesFor`) already bounds it; the editor shows the size before
  loading and asks for confirmation above 500 MB.
- **Phone.** Tools become a dropdown, controls stack, the filmstrip is full width, handles
  have 44 px targets.

### Photo editor

- Filerobot inside `MediaEditorLayer`. Tabs: Adjust (crop presets, rotate, flip), Finetune,
  Filters, Annotate, Resize. **Watermark and AI tabs are off** (AI calls Scaleflex's cloud).
- Config: `useBackendTranslations={false}`, `savingPixelRatio={1}`,
  `previewPixelRatio={window.devicePixelRatio}`, `translations` from `resources.mediaEdit.ts`,
  `theme` from `filerobotTheme.ts`, `StyleSheetManager shouldForwardProp` with
  `@emotion/is-prop-valid`, `removeSaveButton`. Filerobot's own Save button is hidden: the
  photo is exported through `getCurrentImgDataFnRef` from Aura's header (Download, Save to
  library, Close), named `<base>-modificata` in the source's format (PNG, JPEG, WebP;
  quality 0.92). `onBeforeSave` is not used.
- **Two actions, both available from the start:** **Save to library** and **Download**.
  - Save to library uploads with progress, invalidates `studioKeys.library()`, then says
    "Saved to the Studio library".
  - Studio availability is read before uploading from the library query
    (`useStudioLibrary`); a 503 disables Save to library with the sentence "The Studio is not
    active: download the photo instead", so no orphan upload is created. Retry exists only for
    transient failures (network, 5xx other than 503).
  - A refused upload shows the server's own sentence (the presign route answers 400 with it);
    Download stays available.
- Unsaved changes on close: Aura's `ConfirmDialog` only. Filerobot gets no `onClose`, so its
  own close button and discard confirmation never render (`components/buttons/CloseButton.js`).
- Known limit: filter names (Original, Clarendon, Sepia…) stay English — Filerobot does not
  translate them.

## Part 2 — Video to the model (daemon)

**Measure first** (CLAUDE.md "PRD-first"): before the llama.cpp branch is written, a spike on
the live stack records, with a llama.cpp build containing PR #24269 and a video model
(Qwen3-VL or Gemma 4): whether the official server image ships `ffmpeg`, the exact `/props`
key that reports video, and one real `input_video` request. The OpenRouter branch is measured
the same way with one video-capable model. The code follows the measurement; if llama.cpp
cannot take video in the image Aura ships, its branch is dropped, not guessed.

- **One admission rule.** `TurnMediaLoader`, the AG-UI attachment loop and Telegram admit
  `image` **and** `video` (audio stays text-only). The shared loader decides; channels only
  pass references.
- **Capability decides, per backend.** `projectNativeMedia` already asks the active model's
  `ContentCapabilitySource`; a video part is projected only when `SupportsMIME("video/…")` is
  true, otherwise the turn keeps the stored text reference. No model-name guessing.
- **Wire shape per backend,** in `nativeContentPart`, through `param.Override`:
  OpenRouter → `video_url` with a data URL; llama.cpp → `input_video` with raw base64; any
  other target (Ollama included) → no part, because none advertises video.
- **Probes.** llama.cpp: map the measured `/props` video key to `video`. Ollama: unchanged
  (it reports no video and its OpenAI endpoint has no video part).
- **Size.** Only current-turn references are projected (unchanged). A video larger than the
  provider accepts fails the request like any provider refusal today; a per-request video
  ceiling is added only if the measurement shows one is needed.

## Errors (both parts)

- **A discarded track blocks the export.** If `discardedTracks` holds a video or audio track
  for any reason other than `discarded_by_user`, no file is produced and the editor says which
  track this browser cannot process (e.g. HEVC on Firefox: "try Chrome or Edge").
- A failed fetch of the source shows the preview error sentence already used by previews.
- **Known limit, documented:** on Firefox a transcoded MP4 with audio carries Opus, not AAC.
- A video the model cannot take is never an error: it falls back to the text reference.

## Testing

- **Vitest, pure logic:** `cropMath` (every preset, with 0/90/180/270 total rotation, centred,
  clamped, even sides), `timecode` (format/parse round trip), `videoEdit` (options per tool
  combination, the discarded-track guard, output names and containers), and a table test that Italian and
  English cover Filerobot's `defaultTranslations` keys.
- **Component tests** with Filerobot and Mediabunny mocked: `EditMediaButton` visibility
  (image/video yes; SVG, GIF, non-editable source, unresolved asset no); the layer stays open
  when a Filerobot portal menu is clicked; handles and fields in sync, keyboard included; Save
  options; the HEVC guard; photo Save to library, 503 → disabled with the sentence, transient
  failure → Retry, too-large → JPEG.
- **Go:** loader admits image and video, refuses audio, per channel; `nativeContentPart` emits
  the exact OpenRouter and llama.cpp JSON (golden bytes) and nothing for Ollama; projection
  skips video when the capability source lacks it; the llama.cpp probe maps the measured key.
- **Playwright E2E (Chrome, CI project unchanged)** with tiny ffmpeg-generated fixtures under
  `web/e2e/fixtures/`: trim a clip and read the downloaded file's duration with Mediabunny;
  edit a photo and find it in the Studio library; edit a video attachment. Firefox is checked
  by hand in the live verification.
- **Definition of done on the live stack** (`https://localhost`): a Studio image, a
  chat-generated video, an image and an MP4 attachment edited, desktop and phone screenshots;
  a video attachment sent to a video-capable OpenRouter model (answer describes the clip) and
  to a text-only model (falls back); the llama.cpp leg if the measurement kept it.
- **Gates:** lefthook pre-commit and pre-push (oxlint type-aware, tsc, prettier, knip — with
  `konva` checked as a declared peer —, jscpd, 600-line limit, Go vet/lint/deadcode), web
  coverage ≥ 85 %, Go package coverage policy, Stryker ≥ 70 % in CI only, webui dist rebuilt,
  and a check that `konva` and `mediabunny` live only in lazy chunks.

## Out of scope

- Storing edited videos on the server or a video library; edits in the Studio history.
- `.mov` attachments (the server allowlist is MP4/WebM); an inline player in attachment cards.
- Free-resize crop, speed, text or captions on video, multi-clip timelines.
- Native audio to the model (speech stays text).
- Safari/iOS verification (WebCodecs support there is unproven by the spike).
