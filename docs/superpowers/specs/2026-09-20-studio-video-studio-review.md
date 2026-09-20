# Adversarial review — `docs/superpowers/specs/2026-09-20-studio-video-studio-design.md`

Reviewer: Fable 5.1, 2026-09-20. Method: every claim traced to a file; the installed packages read
from `.planning/spikes/107-studio-timeline-editor/app/node_modules/` (VideoFlow 1.3.4,
dnd-timeline 3.1.1, immer 11.1.18) and VideoFlow's source at `D:/tmp/vid-VideoFlow` (tag 1.3.4,
`d6d01b8`); the code that must live beside the design read in `web/src/{mediaEdit,studio,chat/attachments}`,
`internal/assets`, `cmd/aura/serve_studio.go`, `internal/agui/studio_api.go`.

Legend: **[V]** verified by reading the file cited. **[S]** suspected: follows from the code but was not
run. Paths are repo-relative unless they start with `D:/tmp` or `node_modules`.

Ordered by damage.

---

## 1. "Export lands in the Studio library" has no backend. The library and finalize are image-only. [V]

- Spec line 52: "export to MP4, landing in the Studio library as an ordinary asset"; line 140: "uploaded
  as an ordinary asset and appears in the Studio library".
- `cmd/aura/serve_studio.go:163-171`: `Library` calls `b.assets.ListRecentImages`; `FinalizeUpload`
  calls `FinalizeUnprocessed(…, assets.ModalityImage)` with the comment "accepts an uploaded
  reference as an image and nothing else".
- The only upload helper the cockpit has, `web/src/studio/frameUpload.ts:18-36`, is that image
  path. Today's video editor **downloads** (`web/src/mediaEdit/VideoEditor.tsx:139-143`,
  `downloadBlob`), and the previous spec put "storing edited videos on the server or a video
  library" explicitly out of scope (`docs/superpowers/specs/2026-09-19-studio-media-editing-design.md:257`).
- The spec names no Go change, no route, no modality, and Gate 1 demands migration + env catalog
  for anything that touches persistence (CLAUDE.md §Slice Q&A).

**Damage:** the headline promise ("the source of the next project or an attachment in a chat") is
unbuildable as written. **Smallest fix:** add one sentence to §Export naming the backend work —
`FinalizeUpload` takes a modality argument (image *or* video) and `Library` becomes
`ListRecentMedia` — or downgrade v1 to "download, like the quick editor" and keep the library for
the next cycle.

## 2. "The project saved, reopened, and edited again" has nowhere to live. [V]

- Spec line 53 promises it; line 96-97 says the project "can be stored"; §Files (lines 119-135) has
  no store, no API, no migration.
- `ls internal/db/migrations | tail -1` → `0130_cloudflare_remote_access`; a grep of every
  `CREATE TABLE` finds `aura.assets`, `aura.asset_events`, `aura.media_job` — nothing for a project.
- There is no cockpit KV either: the Studio's state is `aura.media_job` rows (spec 2026-09-17,
  plan head) and the library is assets.

**Damage:** without it, "two tabs" (item 12) and "an agent writes the project" (item 9) have no
substrate, and the E2E "reopen and edit again" cannot be written. **Smallest fix:** store the
project as an asset — `application/json`, modality document, through the presign/PUT/finalize
that already exists (`frameUpload.ts`) — and list projects by MIME. Zero migrations. If a table is
wanted instead, the spec must name it and its migration slot (Gate 1).

## 3. Several clips with audio were never rendered; the spike had one video layer. [V]

- Spec line 47 ("several clips and images in sequence") rests on line 25-29 (spike 107).
- Spike 107 composition: **one** `addVideo` (`app/src/project.js:11`), one text, one image. The
  split test cut that one layer in two (README line 93-96). README "What this does NOT prove"
  line 221: "No transitions, effects, captions or **multi-track audio mixing** were exercised."
- VideoFlow *can* do it (`D:/tmp/vid-VideoFlow/src/renderer-browser/audio/mixer.ts:320-330`
  schedules each layer with `start(when, sourceStart, sourceDuration)`; `mute` honoured at :293),
  but the cost is real: **every video layer decodes its whole source to PCM**
  (`mixer.ts:89-104`, `decodeAudioData(arrayBuffer)` on the entire blob; only `RuntimeAudioLayer`
  caches a `decodedBuffer`, `RuntimeAudioLayer.ts:18,52`; `RuntimeVideoLayer.ts:34` reports
  `hasAudio` unconditionally). A clip split into four = four full decodes of the same file.
- The DOM preview does the same on **every `play()`**: `DomRenderer.ts:969-982` mixes the whole
  project offline, encodes it to a WAV blob and plays that. A 3-minute 48 kHz stereo project is
  ~69 MB float + ~35 MB WAV per press of Play. [S — arithmetic, not measured]

**Damage:** the first real project (two 8 s 720p Studio clips + a phone clip) is the first
measurement, in production. **Smallest fix:** one more spike run before the plan —
`run-compose.mjs` with two `addVideo` layers, one split, one muted, ffprobe the audio — and a
design line: "sources are decoded once per source, not per item" (a `decodedBuffer` per
`ProjectSource`, handed to the layers).

## 4. The lane model's cost is not in "The phone"; it is in the timeline, and the spec does not name it. [V]

The Adobe tour (`.planning/research/2026-09-19-studio-video-editor/adobe-express-tour.md:298-312`)
says scenes win because overlays follow their shot and nearly every operation is per-shot. The
spec (line 38-41) accepts lanes and says the consequence "is written into 'The phone' below". It is
not:

- **Ripple is undefined.** With clips abutting on one lane, `trimClip`, `splitAt`, `removeRange`
  and `reorderTrack` (line 103-104) all move every later clip. Text and image items carry absolute
  `start`/`end` (line 89-90), so after a trim of clip 1 the title sits over the wrong shot. The
  spec never says whether overlays ripple, anchor to a clip, or stay put. Clideo, the model
  chosen, leaves gaps and lets the operator close them (`clideo-tour.md:130-133`) — also unstated.
- **Reorder contradicts collision.** Line 116: "an item dropped on top of another on the same lane"
  is refused. Line 49: "reorder by dragging". Dragging clip B before clip A on an abutting lane
  is, at the moment of release, a drop on top of A. `dnd-timeline` gives the shell a `span` on
  `onDragEnd` and nothing else (`node_modules/dnd-timeline/dist/index.d.ts`, `ItemDefinition`,
  `DragEndEvent`); the ripple/insert semantics are entirely ours and unspecified.
- The phone section (lines 152-163) instead promises what a phone cannot give (item 8).

**Damage:** the commands cannot be written from the spec; two implementers would write two
editors. **Smallest fix:** one paragraph, "Lane semantics": video lane = sequence (no gaps, every
edit ripples later clips); overlay items are anchored `{ clipId, offset }` and follow their clip;
`moveClip` on the video lane means *insert before/after*, never free position. Collision then only
applies to overlay lanes.

## 5. A source the browser cannot decode exports **black frames with no error**. [V on the code path, S on the timing]

- Previous cycle's HEVC case: `docs/superpowers/verification/2026-09-19-media-editing.md:221-231` —
  Firefox trims HEVC by copy, refuses a crop with the app's own sentence. The guard is
  `web/src/mediaEdit/editRules.ts:113-126` (`blockingDiscards`) + `videoMedia.ts:99`
  (`canDecode()`).
- The spec has no equivalent. A composition is always a re-encode (line 145), so the copy escape
  hatch is gone.
- VideoFlow: `VideoFrameSource.create` returns `null` when `track.canDecode()` is false
  (`D:/tmp/vid-VideoFlow/src/renderer-browser/video/VideoFrameSource.ts:133-148`); the layer then
  falls back to two `<video>` elements (`RuntimeVideoLayer.ts:83-86, 102`). If the browser cannot
  play the codec either, `seekVideo` resolves after a **2 s timeout per frame**
  (`RuntimeVideoLayer.ts:185-195`) and `drawInto` "leaves whatever it had on screen rather than
  clearing" (`VideoFrameSource.ts:152-157`). A 4 s 30 fps HEVC clip → 120 × 2 s ≈ 4 minutes of
  render, output black or frozen, export "succeeds". [S: the 2 s/frame figure is read, not run]
- Worse for a missing source (see item 11): `BrowserRenderer.initLayers` catches a failing
  layer, `console.warn`s and **disables it for this render** (`BrowserRenderer.ts:525-540`).

**Damage:** silent bad output, the one thing the previous cycle explicitly refused. **Smallest
fix:** `addClip` probes the source (`probeVideo` + `canDecode()` from `videoMedia.ts`, already
written) and refuses undecodable sources with the existing `mediaEdit.video.blocked` sentence;
`ProjectSource` gains `decodable`, `fps`, `hasAudio`.

## 6. The project has no frame rate; the +0.1 ms rule is proven at 24 fps only. [V]

- `VideoJSON` requires `fps` (`node_modules/@videoflow/core/dist/types.d.ts:243-251`); the spec's
  `VideoProject` (lines 78-84) has none, and `ProjectSource` has no `fps` either.
- Spike README line 214-215: "The frame-bias workaround is proven on one 24 fps B-frame H.264
  source; 25/29.97/30/60 fps and VFR sources are untested." The verified real sources are 30 fps
  (`phone.mp4`, `hevc.mp4`, verification doc lines 52-54).
- Mixed-fps sources on one timeline are the normal case (a 24 fps Studio clip next to a 30 fps
  phone clip); VideoFlow conforms every layer to the project fps, so the sampling boundary the
  nudge patches over moves with the ratio.

**Damage:** duplicated/dropped frames on the first phone clip, invisible in unit tests. **Smallest
fix:** `VideoProject.fps` (default: the first video source's), `ProjectSource.fps`, and one
`run-frames.mjs` pass on `phone.mp4` (30 fps) before the plan. Ten minutes.

## 7. "Every change is a pure command" breaks in three places the spec does not address. [V]

1. **Live feedback.** The spike measured the opposite of what the spec says. Spec line 108: "a drag
   is one command, applied on release". Spike `app/src/editor/history.js:17-30`: every move event
   commits a patch inside an open transaction (README line 157-158: "immer's drag entry holds 60
   raw patches"). Either is fine; the spec must pick, because they differ in what the preview
   draws mid-gesture. With "applied on release", the preview during a drag is *not* the project
   (dnd-kit moves the item with a CSS transform, README line 140), the trim-follow behaviour the
   Clideo tour wants ("the playhead follows the edge and the preview shows that exact frame",
   `clideo-tour.md:128-129`, measured in the spike as check "trim start (source window follows)")
   needs `DomRenderer.seek()`/`updateLayer()` **outside** any command, and Konva handles on the
   stage need the same. Line 114's "the editor redraws from the project" is false for the whole
   duration of every gesture.
2. **A render in flight.** `BrowserRenderer` takes the JSON at construction
   (`BrowserRenderer.d.ts:45`) and mutates what it gets (README line 132). Edits during a 30 s
   export produce a history whose "receipts" (line 112) describe a project the file does not
   match. No `version` on the project, no "rendered from version N" on the asset.
3. **Agent preview.** Line 113-114: "a proposed change is previewed before it lands". One
   `DomRenderer` hosts one `videoJSON` (`DomRenderer.d.ts:30`). Preview-before-apply needs a
   second renderer or a swap-and-restore; neither is in §Files.

**Smallest fix:** add to §Commands: "gesture state lives in the component, not the project; a
command is issued on release; export snapshots the project and records its version on the
asset; a proposed change is previewed by applying it to a copy in a second preview". Six lines.

## 8. The phone section promises pinch zoom and touch that were not measured. [V]

- Spec line 158: "the lanes scroll horizontally with **pinch zoom**".
  `node_modules/dnd-timeline/dist/index.mjs`: zoom is `ctrlKey && wheel` only; no `touches`,
  no `gesturestart`, no pinch anywhere. README line 139: "zoom buttons for touch, since zoom is
  ctrl+wheel". The spike lab zooms with two buttons (`app/src/DndTimelineLab.jsx:74-87`).
- Spec line 158-159: "touch drag works because dnd-timeline was measured on a touch viewport".
  README line 208-210: "Touch was emulated through CDP in desktop Chrome, not tested on a real
  phone" (`run-timeline.mjs:3,49-54`).
- Phone scale measured at **47 px/s** (README line 138): a 60 s project is 2 800 px wide on a
  390 px screen, with three lanes at the 44 px floor plus ruler, stage and inspector in 844 px.
  The spec's answer (line 162) is "anything that cannot be made to work with a thumb is
  desktop-only" — a deferral, not the cost the operator was told was written here.
- The Clideo tour's own "do not copy" list has "thin touch handles (18 px hit area)"
  (`clideo-tour.md:361`); dnd-timeline's `resizeHandleWidth` default is not stated in the spec.

**Smallest fix:** replace "pinch zoom" with the measured zoom buttons; say "touch measured under
CDP emulation; a real phone is in the Definition of Done"; and list, now, the two things that are
desktop-only (reorder by drag; overlay drag on the stage) so the phone gets the inspector fields
for them.

## 9. "Each command is already a tool for the agent" is unfounded in this repo. [V]

- Spec line 111-113. Agent tools are Go (`internal/agent/tools/<name>.go`, CLAUDE.md §Tool design);
  the commands are TypeScript in a browser tab. "The same validation the UI gets" exists only if
  the validation is ported to Go or the daemon drives a browser. Neither is mentioned.
- `TrackItem.props` is `Record<string, unknown>` (line 92), so the "same validation" cannot exist
  even in TypeScript: nothing typed can be refused.

**Smallest fix:** either drop the agent sentence from v1 (keep "the project is JSON, so an agent
can write it later") or make `props` a discriminated union per item kind and say the Go tool
validates the project JSON against a schema generated from the TS types.

## 10. Memory has no ceiling and the accounting is worse than the spec implies. [V on the mechanisms, S on the numbers]

- Spec line 182-183 admits scale is unmeasured, then promises "several clips" and "the project
  saved" with no limit. The quick editor at least asks above 500 MB
  (`web/src/mediaEdit/MediaEditorHost.tsx:19-21`), one source at a time.
- VideoFlow's `MediaCache` fetches **every** source as a whole `Blob` and keeps it while any layer
  references it (`D:/tmp/vid-VideoFlow/src/core/MediaCache.ts:154-168`, 5 s eviction after the
  last release). Each video *item* opens its own mediabunny `Input` + `VideoSampleSink` decoder
  (`VideoFrameSource.ts:137-143`): a clip split in four = four decoders on one file. Plus item 3's
  per-layer PCM and per-play WAV. The server's cap is per asset (`internal/assets/limits.go:106`,
  `AURA_ASSET_MAX_VIDEO_BYTES`), not per project.
- Spike sources: 163 832 B, 320×180 (`web/e2e/fixtures/media-edit/clip.mp4`). Real Studio output
  measured in spike 106: 1280×720, 8 s, 2.8 MB — 16× the pixels. Render at 1080p: "not measured"
  (README line 211-212).

**Smallest fix:** a project ceiling in the spec — sum of source bytes and item count — reusing the
500 MB constant, with the confirm sentence the quick editor already has; and "one decoder per
source" as a design rule (item 3).

## 11. An asset deleted under a project makes the export **succeed with a hole**. [V]

- `internal/assets/service.go:328-345`: `Delete` soft-deletes the row and best-effort deletes the
  object. No reference check; `ReachabilityKind` (`content_parts.go:25`) tracks content parts,
  not projects. The project holds only `assetId` (spec line 82, 96).
- Reopen: `MediaCache.fetchAndStore` throws on a non-OK response (`MediaCache.ts:154-158`);
  `initLayers` catches it, warns, **disables the layer and renders anyway**
  (`BrowserRenderer.ts:525-540`). A clip vanishes from the MP4; nothing in the UI knows.

**Smallest fix:** the editor probes every source on open (item 5's probe), marks missing ones in
the project (`ProjectSource.status: 'missing'`), shows a placeholder, and refuses to export while
one is missing. Server-side reachability can wait.

## 12. Two tabs, one project: last write wins, silently. [S — no store exists yet, item 2]

Whatever store item 2 picks, immer patches without a version are last-write-wins. **Smallest fix:**
`VideoProject.version`, the save carries the version it read, the server (or the asset finalize)
refuses a stale one with 409 and the UI says "changed elsewhere, reload".

## 13. A render interrupted after the file exists leaves an orphan; the spec has no export lifecycle. [V]

- `BrowserRenderer.exportVideo` honours `signal` (`BrowserRenderer.ts:1299-1364, 1438-1447`) and
  `onProgress`; the spec's UI (§Files) has no progress or cancel. The quick editor aborts on
  unmount (`VideoEditor.tsx:98-101`).
- Once uploading: presign → PUT → finalize is three requests (`frameUpload.ts:22-35`). A tab
  closed between PUT and finalize leaves a presigned, never-finalized asset. The previous spec
  guarded only the 503 case.

**Smallest fix:** copy the quick editor's progress bar + Cancel + abort-on-unmount into §Export,
and say the finalize is idempotent by asset id (the route already requires an `Idempotency-Key`,
`internal/agui/idempotency_http.go:91`).

## 14. "One decoder and one muxer" is half true; the worker carries its own mediabunny. [V]

- Spec line 21-22 and 139. README line 114-119: the worker string carries "a private copy of
  mediabunny's muxer path (23 modules)… VideoFlow's lockfile pins mediabunny 1.40.1… inferred,
  not measured", and it ships "even with `worker: false`". The worker path (the fast one, the one
  the 0.4–0.8 s came from) and the main-thread path wrote **different files** (344 346 B vs
  460 949 B, README line 78-79).

**Smallest fix:** say "worker export, with VideoFlow's bundled muxer (mediabunny ≈1.40); the
main-thread path is not used" — and put the version pin on the list of things to re-check on a
VideoFlow bump.

## 15. Konva is not a dependency of `web/`. [V]

- `web/package.json` dependencies: `mediabunny` only among the media libraries; `konva` and
  `react-konva` are transitive through `react-filerobot-image-editor` (engine inventory line 6:
  "konva@9.3.18 (via filerobot)"). No file in `web/src` imports either. `knip.json:12` ignores
  only `assistant-stream`, so an import of an undeclared package fails the pre-push lint.
- Declaring `konva` at a different major than filerobot's 9.3.18 ships two copies; `react-konva`
  19.3.0 pins `react ^19.3.0` and `konva ^8||^9||^10`.

**Smallest fix:** declare both at the versions filerobot resolves, or cut the Konva stage
(item 17). Also note the coordinate mapping the stage must do: VideoFlow `position`, `anchor` are
normalised `[0..1]` and `scale` is unitless (`D:/tmp/vid-VideoFlow/src/core/layers/VisualLayer.ts:117,122,148`);
`fontSize` is `em`/`px` (`TextualLayer.ts:94`).

## 16. The same-origin font loader depends on two stylesheets that do not exist. [V]

- Spike `app/src/fonts.js:8-11` maps `'Noto Sans'` → `/fonts/noto-sans-alias.css` and
  `'Atkinson Hyperlegible Next'` → `/fonts/atkinson.css`. `web/public/fonts/` holds the `.woff2`
  files and no `.css`. Small, but the "zero off-origin requests" promise (line 58, 174) fails on
  the first text layer if it is forgotten, because the fallback is the stock Google Fonts fetch.

**Smallest fix:** list the two stylesheets under §Files.

## 17. Scope: three cycles drawn as one. [V by comparison]

The previous cycle shipped 2 058 LOC for one clip, one lane, four operations, download only. This
spec adds: a second model + translation layer, eleven commands with refusal rules, history,
a headless timeline with lanes/ruler/playhead/zoom/drag/trim/snap/touch, a Konva stage, an
inspector, animation presets, a renderer adapter with four workarounds, export + upload, a
backend for the library (item 1), a store (item 2), i18n in two languages, a phone layout and
three tiers of tests. The spike's four throwaway lab tabs are already 706 LOC
(`app/src/*.jsx`, `editor/*.js`); `Timeline.tsx` alone will not stay under 600 with the ruler,
playhead, zoom, drag, trim handles and collision feedback the spec assigns to it (line 129).

- **Cycle A (engine):** `project.ts`, `commands.ts`, `history.ts`, `videoflow.ts`, export to
  **download**, Playwright "two clips and a title" read back with mediabunny. This is where the
  unmeasured things (items 3, 5, 6, 10) get measured.
- **Cycle B (surface):** timeline, inspector, stage, phone, i18n.
- **Cycle C (persistence and the door):** project store, library video, versioning, agent tool.

**Cut one item:** the Konva stage handles. Position and size through the inspector (fields plus
nine anchor presets) covers the v1 layers; it removes an undeclared dependency (item 15), a second
gesture system with its own live-feedback problem (item 7) and the px↔normalised mapping. The
spec's own phone section already says the fields are the phone's way (line 157).

**Cheapest missing thing that makes the rest usable:** a source probe at `addClip` — `fps`,
`hasAudio`, `decodable`, `duration`, size — written once in `project.ts` on top of
`videoMedia.ts`'s `probeVideo`/`canDecode`. It closes items 5, 6 and 11 at the model boundary
and costs one function.

---

## What survives

- **The engine choice.** The inventory (`engine-inventory.md`) is thorough and the API is shaped
  as the adapter assumes: `LayerSettingsJSON.startTime / sourceStart / sourceDuration`
  (`core/dist/types.d.ts:105-125`) map one-to-one onto `TrackItem.start / sourceStart / end`;
  `mute` is a real layer property (`AuditoryLayer.d.ts`); z-order follows `LayerJSON.track` then
  array order (`sortByTrack.ts`); `transitionIn/Out` with `fade`, `slideUp/Down/Left/Right`,
  `zoom`, `typewriter` presets (`transitions/presets.ts`) *is* the "enter/exit animation from a
  short list" — no custom animation code needed; `signal` and `onProgress` exist on export.
- **The four rules** are real and their fix points exist in the public typings: `loadFont` is a
  public method and `loadedFonts` a public field on `BrowserRenderer` (`BrowserRenderer.d.ts:31,76`);
  the instance API (`new` + `exportVideo` + `destroy`) is the documented one.
- **immer patches for history** — measured, small, and the transaction pattern in `history.js`
  is sixty lines that already work.
- **dnd-timeline as a headless shell** — `useItem({ span, onResizeEnd })`, `useRow`, `TimelineContext`
  with `rangeGridSizeDefinition` fit lanes exactly; the missing pieces (pinch, ripple, sensors)
  are ours by design, not surprises.
- **Commands as pure functions over data.** Right idea. One honest question: the spike's model
  *was* VideoFlow's `VideoJSON` (`ops.js`, 59 lines over it directly). The spec's separate
  `VideoProject` buys isolation from VideoFlow's 1.x churn at the price of a translation the spike
  never measured (split was tested on `VideoJSON`, not on a `TrackItem`). Defensible, but say so.
- **The "never" list** (line 58-59) and the off-origin E2E check are the right guards; the spike's
  81-request / 0-off-origin log is real.

## Claims traced, for the record

| Spec line | Claim | Source | Status |
|---|---|---|---|
| 21-22 | one decoder, one muxer | README 114-119 | half: worker has private mediabunny |
| 26 | 26 off-origin → 0 | README 105-107 | V |
| 27 | 10 of 96 frames | README 86-91 | V, 24 fps only (README 214) |
| 29 | 0.4–0.8 s render | README 108-109 | V, 4 s 320×180 only |
| 30 | 9/9 desktop and 390×844 | README 137, run-timeline.mjs | V, CDP-emulated touch |
| 33 | 30-event drag = one step | README 149-154 | V, spike commits per event in a transaction |
| 35-37 | signalsmith pitch | README 164-170 | V, out of scope |
| 39-41 | Adobe: scenes survive a phone | adobe-express-tour.md 304-308 | V, quoted correctly |
| 67-68 | 52–65 ms copy path | verification 2026-09-19 line 21, 239 | V |
| 140 | appears in the Studio library | serve_studio.go 163-171 | **false**: images only |
| 158 | pinch zoom | dnd-timeline dist | **false**: ctrl+wheel only |
| 182 | 4 s 320×180 only | README 211 | V, honest |
