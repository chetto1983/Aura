# Media editing (Studio + chat) — design

Date: 2026-09-19. Status: design approved in conversation, section by section; this document
is the record the plan is written from. Evidence: spike 105
(`.planning/spikes/105-studio-media-editing/README.md`).

## Goal

Let the operator edit an image or a video they already have in Aura — a Studio result, a
generated image or clip in chat, an attachment — **in the browser, with no AI model**: crop,
adjust, filter and annotate a photo; trim, crop, rotate and mute a clip.

## Decisions taken with the operator

| Question | Decision |
|---|---|
| AI in the editing path | None. Everything runs client-side. |
| Photo library | Filerobot Image Editor (`react-filerobot-image-editor`, MIT). |
| Video library | Mediabunny (MPL-2.0) plus a timeline Aura writes. |
| Where a result goes | Photo → the Studio library (reusable as reference/first frame) + download. Video → download. No server change. |
| Video operations | Trim, remove audio, rotate, crop. |
| Where "Edit" appears | Studio result, chat generated image, chat generated video, attachment preview. |
| Shape | Shared lazy module `web/src/mediaEdit/` (approach 1 of 3; a separate route and inline editing were rejected). |
| Visual reference | Professional web editors captured on 2026-09-19: Adobe Express *Trim video*, 123apps *Video Cutter* (desktop and phone), Pixlr Express (photo). |

## Inventory that shaped the design (2026-09-19)

- **The Studio library already takes images.** `uploadStudioFrame` (presign → PUT →
  `POST /api/studio/uploads/{id}/finalize`) stores an identity-scoped image that
  `GET /api/studio/library` (`ListRecentImages`) lists. `FinalizeUpload` accepts images only,
  which is why a trimmed video is downloaded rather than stored.
- **The Studio history is `aura.media_job`**, paid generations with provider ids and cost. An
  edit is not a generation and is not written there.
- **Previews are shared.** `PreviewByKind` serves the Studio stage, chat and the share pages;
  `AssetSourceContext` already lets a share tier omit an optional capability (`renderUrl`).
- **The cockpit sends no Content-Security-Policy** (only artifact renders do), so
  styled-components' injected `<style>` and `blob:` object URLs need no header change.
- **Mediabunny 1.58.1** copies on trim by default. With `copy: { boundaryPolicy: 'expand' }`
  an MP4 keeps the preceding key frame behind an edit list, and ffmpeg plus the `<video>`
  element of Chrome, Edge and Firefox start exactly on the requested frame (spike 105). The
  online guide saying a non-zero start forces a transcode is outdated.
- **Filerobot 5.0.0-beta.159** is `latest` on npm, peers React 19; defaults fetch
  translations from Scaleflex (`i18n-fastly.ultrafast.io`) and save at pixel ratio 4.

## Architecture

```
web/src/mediaEdit/
  EditMediaButton.tsx        the "Edit" button; lazy-loads the right dialog
  PhotoEditorDialog.tsx      Filerobot in a full-screen dialog
  VideoEditorDialog.tsx      preview + tools + timeline + save
  VideoTimeline.tsx          filmstrip, two handles, playhead
  videoEdit.ts               pure: Mediabunny options, discarded-track guard, output name
  cropMath.ts                pure: preset → centred, clamped, even-sided rectangle
  timecode.ts                pure: seconds ⇄ "mm:ss.s"
  filerobotTheme.ts          Aura CSS tokens → Filerobot theme
web/src/i18n/resources.mediaEdit.ts   UI strings + Filerobot's 128 keys, en + it
```

`EditMediaButton` takes `{ assetId, kind: 'image' | 'video', fileName, mimeType }`. It renders
nothing when the asset source is not editable, when the image is SVG or GIF, or for any other
kind. Each dialog is a `React.lazy` chunk: Filerobot (~273 KB gzip) and Mediabunny (~143 KB
gzip) load on the first click, never with chat or the Studio.

`AssetSource` gains an optional `editable?: true`. `IDENTITY_SCOPED` sets it; the share tiers
leave it out, so a public page never offers editing.

The button is placed in four existing components: `StudioStage` (`StageActions`, beside
Download and Reuse), `GeneratedImagePreview` (beside Download and Copy), `VideoPreview` (a
small action row under the player) and `PreviewModal` (header, image and video kinds only).

### Data flow

1. Click → the dialog opens full screen and fetches the file through the identity-scoped
   `assetUrl` (same-origin, credentials) as a Blob.
2. Editing happens in the browser.
3. Save:
   - **Photo** → a `File` named `<base>-modificata.<ext>` → `uploadStudioFrame` with progress
     → invalidate `studioKeys.library()` → success state "Saved to the Studio library" with
     **Download** and **Close**.
   - **Video** → a `Blob` from Mediabunny → download as `<base>-modificato.<ext>`.

## Video editor

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

- **Trim.** A filmstrip of about 10 frames decoded with Mediabunny; two handles (start, end)
  dim what is cut; Start/End fields stay in sync. Handles are `role="slider"`: arrows ±0.1 s,
  Shift+arrows ±1 s. ▶ plays the selected range only.
- **Crop.** Presets Original · 1:1 · 9:16 · 16:9 · 4:3 · 3:4. The box can be moved, not
  resized (free resize is out of scope). The resulting size in pixels is shown.
- **Rotate.** 90° left / 90° right; the preview rotates with CSS immediately.
- **Audio.** "Remove audio" switch, disabled when the clip has no audio track.
- **Save.** Always `copy: { boundaryPolicy: 'expand' }` plus the transforms; Mediabunny copies
  or transcodes per track. Trim and mute stay instant and frame-exact on MP4; crop and WebM
  transcode. Whether rotate alone stays on the copy path (rotation metadata) is measured by a
  test, not assumed. A progress bar with **Cancel** (`conversion.cancel()`) shows while saving.
- **Phone.** Tools become a dropdown, controls stack, the filmstrip is full width, handles
  have 44 px targets.

## Photo editor

- Filerobot in a full-screen dialog. Tabs: Adjust (crop presets, rotate, flip), Finetune
  (brightness, contrast, HSV, warmth, blur), Filters, Annotate (text, arrow, shapes, pen),
  Resize. **Watermark and AI tabs are off** (AI calls Scaleflex's cloud).
- Mandatory config: `useBackendTranslations={false}`, `savingPixelRatio={1}`,
  `translations` from `resources.mediaEdit.ts` for the current language, `theme` from
  `filerobotTheme.ts`, and a `StyleSheetManager shouldForwardProp` using
  `@emotion/is-prop-valid` (without it styled-components 6 logs ~35 warnings per mount).
- **Save skips Filerobot's name/format modal**: the name is `<base>-modificata`, the format is
  the source's (PNG, JPEG, WebP; quality 0.92 for lossy), set through
  `defaultSavedImageName` / `defaultSavedImageType`. `onBeforeSave` returns `false`, which
  makes the Save button call `validateInfoThenSave()` and then `onSave` without opening the
  modal (read in `components/buttons/SaveButton/index.jsx`, v5-dev `3087621`).
- Closing with unsaved changes asks for confirmation (`confirm-dialog`).

## Errors

- **A discarded track blocks the export.** If `discardedTracks` holds a video or audio track
  for any reason other than `discarded_by_user`, no file is produced and the dialog says which
  track this browser cannot process (e.g. HEVC on Firefox: "try Chrome or Edge").
  `conversion.isValid` alone is not enough: spike 105 saw Firefox return a valid audio-only
  file from an HEVC clip.
- A failed fetch of the source shows the preview error sentence already used by previews.
- A failed photo upload keeps the dialog and the edits, with **Retry**.
- **Known limit, documented, not handled:** on Firefox a transcoded MP4 with audio carries
  Opus instead of AAC (Firefox cannot encode AAC).

## Testing

- **Vitest, pure logic:** `cropMath` (every preset centred, clamped, even sides), `timecode`
  round trips, `videoEdit` (options per tool combination, the discarded-track guard, output
  names), and a table test that Italian and English cover exactly Filerobot's
  `defaultTranslations` keys.
- **Component tests** with Filerobot and Mediabunny mocked: `EditMediaButton` visibility
  (image/video yes; SVG, GIF, non-editable source no); handles and fields stay in sync,
  keyboard included; Save passes the right options; the HEVC guard stops the export; the
  photo upload path, including Retry after a failure.
- **Playwright E2E with real media:** tiny ffmpeg-generated fixtures (a few KB) under
  `web/e2e/fixtures/`; trim a clip and read the downloaded file's duration with Mediabunny;
  edit a photo and find it in the Studio library. Chrome, plus Firefox for the HEVC guard.
- **Definition of done:** the stack updated to the new image, the real flows exercised on
  `https://localhost` — a Studio image, a chat-generated video, a phone attachment — with
  desktop and phone screenshots.
- **Gates:** lefthook (eslint, tsc, prettier, knip, jscpd, 600-line limit), web coverage
  ≥ 85 %, webui dist rebuilt, and a check that `konva` and `mediabunny` live only in lazy
  chunks.

## Out of scope

- Storing edited videos on the server or a video library.
- Recording edits in the Studio history.
- Free-resize crop, speed, text on video, multi-clip timelines.
- Safari/iOS verification (not measured by the spike; WebCodecs support there is unproven).
