---
spike: 107
idea: studio-media-editing
name: studio-timeline-editor
type: standard
validates: "Given the Studio clip, a text and a photo, when an operator edits them on an OSI timeline over a VideoFlow project (drag, trim, split, keyframes, speed, undo) and exports, then the browser alone produces a valid MP4 with every layer where the model puts it, the audio keeps its pitch at 0.5× and 2×, and no request leaves the origin"
verdict: VALIDATED
related: [105]
tags: [studio, web, video-editor, videoflow, timeline, dnd-timeline, undo-redo, immer, time-stretch, signalsmith-stretch, mediabunny]
---

# Spike 107: A Clideo-like Studio video editor in the browser

## What This Validates

Can the Studio grow from spike 105's single-clip trim into a multi-layer editor with no server and no
off-origin request? The parts are VideoFlow (Apache-2.0) as the model and renderer, an OSI timeline
widget as the shell, an undo/redo history of our own, and pitch-preserving speed. The stack is React
19.3 + Vite 8.3 (the cockpit's). The test media are `web/e2e/fixtures/media-edit/clip.mp4` (4 s,
320×180 H.264 at 24 fps with B-frames, 440 Hz sine AAC, burned-in clock) and `photo.png`.

**Overall: VALIDATED, with the rules in Results.** Two defects and three weights showed up. Each has a
fix that needs no fork, and each fix was measured.

| Q | Question | Verdict |
|---|---|---|
| 1 | VideoFlow as composition + render engine, offline | **VALIDATED** with 4 rules (fonts, frame bias, instance API, registry stub) |
| 2 | OSI timeline shell under React 19, desktop + touch | **VALIDATED**: `dnd-timeline` passes 9/9 checks on both devices as shipped |
| 3 | Our own undo/redo over the project | **VALIDATED**: immer patches (zundo also passes) |
| 4 | Pitch-preserving speed + mux | **VALIDATED**: `signalsmith-stretch` (MIT) + mediabunny; VideoFlow's own `pitch` fails |

## Research

The candidate inventory comes from the earlier survey
(`scratchpad/timeline-inventory.md`, 2026-09-19). This spike measures the four chosen parts.

| Part | Package (installed) | License | Status |
|---|---|---|---|
| Model + export | `@videoflow/core` / `renderer-browser` / `renderer-dom` 1.3.4 | Apache-2.0 | **chosen**, 4 rules |
| Timeline shell | `dnd-timeline` 3.1.1 (+ `@dnd-kit/core` 6.3.1) | MIT | **chosen** |
| Timeline shell | `@xzdarcy/react-timeline-editor` 1.0.0 | MIT (engine ISC) | works; needs a touch fix; 3× the bytes |
| History | `immer` 11.1.18 `produceWithPatches`/`applyPatches` | MIT | **chosen** |
| History | `zundo` 2.3.0 over `zustand` 5.0.15 | MIT | works; snapshot-based |
| History | VideoFlow OSI packages | — | none (`undo`/`redo` grep over `@videoflow/*/dist`: 0 hits; the undo in `react-video-editor` is non-OSI) |
| Time-stretch | `signalsmith-stretch` 1.3.2 | MIT (npm `license` **and** GitHub `LICENSE.txt`, Signalsmith Audio 2022) | **chosen** |
| Time-stretch | `soundtouchjs` 0.3.0 | LGPL-2.1 | pitch OK, output 10 % short |
| Time-stretch | VideoFlow layer `speed` + `pitch = 1/speed` (its granular shifter) | Apache-2.0 | **fails** pitch |
| Time-stretch | `HTMLMediaElement.preservesPitch` | browser | preview only, passes |
| Time-stretch | `rubberband-web` / `rubberband-wasm` / `@echogarden/rubberband-wasm` | GPL-2.0 | excluded (copyleft over the MIT bundle) |
| Time-stretch | `@soundtouchjs/audio-worklet` 2.1.1 (MPL-2.0 since 2026-08), `phaze` 0.0.71 (MIT, last release 2022) | — | found, **not measured** |

## How to Run

```bash
bash make-media.sh                      # copies clip.mp4, photo.png, Aura's Atkinson woff2 into media/
cd app && npm install && npm run dev    # http://localhost:5207  (?tab=compose|dnd|xz|history|speed)
node run-compose.mjs 3                  # Q1: renders → out/compose/*.mp4, network log, stock-font control
node run-frames.mjs                     # Q1: ffmpeg frame grabs: text/photo regions + frame-by-frame timing
node run-timeline.mjs                   # Q2: both shells, desktop + 390×844 touch → out/timeline-report.json
node run-history.mjs                    # Q3: scripted undo/redo session, immer vs zundo
node run-speed.mjs                      # Q4: stretch 0.5×/2×, FFT, preservesPitch, retimed MP4 + ffprobe
cd app && npx vite build && cd .. && node run-size.mjs   # gzip per chunk and per package
cd app && STUB_GOOGLE_FONTS=1 npx vite build --outDir ../out/dist-stub   # registry-stub build
bash probe.sh out/compose/*.mp4         # independent ffprobe
```

The browser is Chrome 153 (`channel: 'chrome'`, headless). Every run keeps its JSON under `out/`.

## Investigation Trail

1. **Install.** `npm install` under React 19.3 printed no peer warnings. `npm ls` gives
   `@videoflow/renderer-browser → mediabunny@1.58.1 deduped`. `react-virtualized` 9.22.6 (xzdarcy)
   declares `react ^19`. `dnd-timeline` declares no peers; its `@dnd-kit/*` peers allow React ≥16.8.
2. **Compose + render (Q1).** Setup: `new VideoFlow({320×180, 24 fps})`. Layers: `addVideo(clip)`;
   `addText('AURA 107')` in Atkinson Hyperlegible Next, white on black, at [0.3, 0.82]; and
   `addImage(photo, scale 0.35)` with `animate({position:[0.2,0.3], opacity:0} → {position:[0.8,0.4],
   opacity:1}, 3 s, linear)`. Then `compile()`, and the instance API `new BrowserRenderer(json)` →
   `exportVideo()`. ffprobe: **H.264 High 320×180, 24 fps, 96 frames, 4.000 s + AAC-LC 4.011 s**,
   344,346 B through the worker and 460,949 B on the main thread (`worker: false`).
3. **Frame grab (independent of VideoFlow).** ffmpeg frames of the export were diffed against the source
   frame at the same time; connected changed regions were then located. At 1.02 s and 3.52 s one
   region contains the text anchor (bbox [28,126]–[169,163], 1,177 near-white glyph pixels). A second
   region contains the photo's *keyframed* centre, which moves from (129,60) to (256,72) between the two
   grabs. Text, image and the keyframed animation are present.
4. **Defect: frame sampling on the boundary.** Every output frame was matched to its source frame
   (argmin of the clock-region difference over all 96 source frames). **10 of 96 frames show the
   previous source frame** (n = 8, 11, 26, 29, …, 47): source frame 8 is dropped and 7 is shown twice
   (visually confirmed on the burned-in clock). This is identical with and without the worker.
   Cause: `VideoFrameSource.holds(t)` tests `t >= sample.timestamp` with `t = frame / fps` exactly on
   the frame's start boundary, so float rounding lands a hair before the sample. **Workaround without a
   fork:** add 0.1 ms to every video layer's `sourceStart` (`1e-4`), which gives **0 / 96 mismatches**.
5. **Split through the model.** `splitLayer()` replaces one video layer with two over the same
   `source`: the left keeps `sourceDuration = offset`, the right gets `startTime = cut`,
   `sourceStart += offset` and the rest. Keyframe times are *source-media* time, so both halves keep
   their animation. Test: cut at 2 s, then trim 0.5 s off the right half's head. The export is
   3.500 s (84 frames), and frames ≥ 48 show source n + 12 with no mismatch.
6. **Fonts.** There is no config option. Core's `defaults.fontFamily` only renames the default family.
   `BrowserRenderer.initLayers()` and `DomRenderer` always call `loadFont('Noto Sans')`, and `loadFont`
   `fetch`es `fonts.googleapis.com` for any family in its bundled registry. The two renderers share
   one choke point: every text layer calls `this.renderer.loadFont(family)`. `fonts.js` replaces that
   **public method on the instance**. It injects a same-origin stylesheet and records it in
   `loadedFonts`, a public field on BrowserRenderer and a TS-private one on DomRenderer, which
   FontEmbedder reads to inline `@font-face` into the SVG raster. `'Noto Sans'` and
   `'Atkinson Hyperlegible Next'` both map to Aura's own `web/public/fonts/atkinson-…woff2`.
   **Network log: 81 requests, 0 off-origin.** That covers 3 worker renders, 3 main-thread renders,
   split, bias and the DomRenderer preview. **Negative control** (stock `loadFont`, fresh page):
   **26 requests to `fonts.googleapis.com` / `fonts.gstatic.com`**.
7. **Render time**, 4 s at 320×180, Chrome 153. Dev build: worker 446–848 ms (first run of a page is
   the slowest), main thread 444–566 ms, split 396–510 ms. Production build: 402–658 ms.
8. **Bundle** (`run-size.mjs`, gzip per package from the build's module report):
   `@videoflow/core` 10.1 KB, `renderer-dom` 5.8 KB and **`renderer-browser` 234.5 KB**, which
   together add **250.4 KB** on top of mediabunny (172.9 KB, already in Aura). Two items dominate
   `renderer-browser`:
   - `googlefonts.json`, 147.3 KB: the Google Fonts registry, dead once fonts are local.
   - `workerBundle.js`, 39.1 KB: the encoder worker as an esbuild string. It carries a **private copy
     of mediabunny's muxer path** (23 modules), so the dedupe in `npm ls` covers the main thread only.
     VideoFlow's lockfile pins mediabunny 1.40.1, and the minified string has no version marker, so
     the copy's version is inferred, not measured. The string is imported statically and ships even
     with `worker: false`.

   A 10-line Vite `load` hook (`STUB_GOOGLE_FONTS=1`) that serves `{"items":[]}` for the registry
   brings the renderer chunk from **351.4 → 206.6 KB gzip**. The stubbed production build renders a
   byte-identical file (344,346 B) with 0 off-origin requests. With the stub alone (no loadFont
   override) nothing is fetched either, but the text falls back to a system face (343,163 B), so the
   override stays.
9. **API traps.** `$.renderVideo()` does `import(/* @vite-ignore */ pkg)` and cannot be bundled, so
   use `BrowserRenderer` directly. The static `BrowserRenderer.render()` builds `new BrowserRenderer`
   inside itself, so an instance override never reaches it: use `new` + `exportVideo()` + `destroy()`.
   `DomRenderer.play()` resolves only when playback ends, so don't `await` it from a handler.
10. **Timeline shells (Q2).** Both shells render one row per layer over the same immer history and a
    DomRenderer preview. The preview is kept in step with `add/remove/updateLayer` + `updateVideo`,
    and the renderer gets clones because it mutates what it receives. Nine Playwright checks per
    device: ruler → preview seek, preview → playhead, playhead follows playback, drag, trim end, trim
    start (source window follows), preview state equals model, one undo entry per gesture with
    undo/redo to exact snapshots, and zoom ×2. The phone is 390×844 with `isMobile`, and touch goes
    through CDP `Input.dispatchTouchEvent`.
    - **dnd-timeline: 9/9 desktop, 9/9 phone, 0 console warnings, no horizontal overflow**. It fits
      its range to the width (196 px/s desktop, 47 px/s phone), snaps to a 250 ms grid and syncs the
      preview in 1.5 ms. It is headless, so the UI is ours (ruler, playhead, zoom buttons for touch,
      since zoom is `ctrl`+wheel). dnd-kit moves the item with a CSS transform during the drag, so
      the shell commits on drag/resize end.
    - **react-timeline-editor: 9/9 desktop; phone 6/9 → 9/9 with one CSS line.** Its actions keep
      `touch-action: auto`, so Chrome claims the swipe as a pan and fires `pointercancel`; drag and
      trim do nothing (the report without the fix is kept). `.timeline-editor-action { touch-action:
      none }` fixes it. Its scale is a fixed 160 px/s, so the 4 s clip overflows a phone until zoomed
      out once. Preview sync takes 7.7–8.9 ms because rows are rebuilt. Snap is `gridSnap` + `dragLine`.
    - Size: **dnd-timeline + dnd-kit + resize-observer-polyfill 25.9 KB** gzip; **react-timeline-editor
      75.7 KB** JS + 1.1 KB CSS (it inlines react-virtualized and interactjs).
11. **Undo/redo (Q3).** `editor/history.js` is about 70 lines. Each edit is an immer recipe, and
    `produceWithPatches` yields `[state, patches, inverse]`, which *is* the history entry.
    `begin()`/`end()` open a transaction so a gesture folds into one entry. Scripted session: add
    layer, drag (30 events), trim (20 events), property, split. It gives **5 entries**; each undo
    restores the exact prior snapshot (JSON equality), each redo the next one, undo at the bottom is
    a no-op, and a new edit after undo clears redo. **immer: all checks pass, 7.1 KB gzip.** zundo
    passes the same session (0.8 + 0.4 KB for zustand, already in web's tree via
    `@assistant-ui/react`), but coalescing a gesture needs `pause()` → restore the pre-gesture state →
    `resume()` → one `setState`. It also keeps whole snapshots, where immer keeps deltas that can be
    serialized and labelled. immer's drag entry holds 60 raw patches (11 KB of history against zundo's
    4.8 KB here); compact per path before persisting.
12. **Speed (Q4).** The clip's audio (mono 44.1 kHz) was stretched offline. FFT: 32,768-point Hann
    window over the middle, parabolic peak, "purity" = energy within ±10 Hz of the peak. Times are 3
    runs.

    | Method | 0.5× duration | 0.5× peak | 2× duration | 2× peak | Time 0.5× / 2× |
    |---|---|---|---|---|---|
    | control: plain resample | 8.000 s ✓ | 220.01 Hz ✗ | 2.000 s ✓ | 879.98 Hz ✗ | 10–12 / 3 ms |
    | **signalsmith-stretch** | **8.000 s ✓** | **440.64 Hz ✓** | **2.000 s ✓** | **440.32 Hz ✓** | 140–157 / 43–45 ms |
    | soundtouchjs | 7.169 s ✗ (−10.4 %) | 439.97 Hz ✓ | 1.804 s ✗ (−9.8 %) | 440.03 Hz ✓ | 19–28 / 10–11 ms |
    | VideoFlow `speed` + `pitch: 1/speed` | 8.000 s ✓ | 454.40 Hz ✗ | 2.000 s ✓ | 504.97 Hz ✗ | 37–62 / 24–27 ms |
    | `<video>` `preservesPitch` (live) | — | 440.08 Hz ✓ | — | 440.02 Hz ✓ (control `false`: 879.99) | real time |

    The control failing shows the FFT catches a wrong pitch. signalsmith purity is ≥ 0.9997. Its
    WASM is **64,494 B (27.5 KB gzip)**, inlined as base64; the whole module is **45.1 KB gzip**. It
    runs in an `OfflineAudioContext`: `addBuffers` + one `schedule({rate})` + `startRendering`.
    soundtouchjs is 4.2 KB, but its `SimpleFilter` never flushes the stretch tail.
13. **Retimed MP4.** The video packets are copied with `timestamp/speed` and `duration/speed`
    (`EncodedPacketSink` → `EncodedVideoPacketSource`, no re-encode), and the signalsmith audio is
    encoded to AAC via mediabunny's `AudioBufferSource`. Mux time: 58 ms (0.5×) and 19 ms (2×).
    ffprobe: **8.011 s** (video 8.000 s, 96 frames at 12 fps) and **2.020 s** (video 2.000 s, 96 frames
    at 48 fps). ffmpeg decoded the audio independently, and its FFT gives **440.64 Hz** and **440.32 Hz**.
    Both pass.

## Results

**VALIDATED.** Rules the real build must follow:

- **VideoFlow**
  - Render with `new BrowserRenderer(json)` + `exportVideo()` + `destroy()`, and preview with
    `DomRenderer`, each with `loadFont` replaced by the same-origin loader (`fonts.js`).
  - Stub `googlefonts.json` at build time (−145 KB gzip).
  - Nudge video `sourceStart` by 1e-4 s until VideoFlow samples mid-frame (worth an upstream issue).
  - Budget ≈103 KB gzip for VideoFlow once stubbed (250.4 − 147.3), plus a second, private mediabunny muxer in its
    worker string.
- **Shell: `dnd-timeline`.** 26 KB, touch works as shipped, and a headless shell lets the UI be
  Aura's own. Commit on gesture end. Replay edits to the preview through the renderer's incremental
  API, with clones. If react-timeline-editor is ever used, add `touch-action: none` and fit its
  scale to the width.
- **History: immer patches**, one transaction per gesture, compacted per path before persisting.
  Don't reach for zundo just because zustand is already in the tree.
- **Speed: `signalsmith-stretch` offline for export, `preservesPitch` for live preview.** Don't use
  VideoFlow's `pitch` to cancel `speed`: it is off by 14–65 Hz. Don't bundle soundtouchjs. LGPL-2.1
  §6 would oblige the Studio to ship its source and to let users re-link a replacement, which a
  minified, hashed Vite chunk does not allow without extra packaging. It also returns 10 % too little
  audio. Mux with mediabunny, copying packets.

## What This Does NOT Prove

- **Browsers and devices.** Only Chrome 153 on Windows. No Edge, Firefox or Safari/iOS (WebKit
  WebCodecs and AudioWorklet in an `OfflineAudioContext` are unmeasured). Touch was emulated through
  CDP in desktop Chrome, not tested on a real phone.
- **Scale.** Only a 4 s 320×180 clip. Render time, memory and preview smoothness at 1080p, on long
  clips or with many layers are not measured, nor is audio/video sync during `DomRenderer` playback
  beyond playhead position.
- **The frame-bias workaround** is proven on one 24 fps B-frame H.264 source; 25/29.97/30/60 fps and
  VFR sources are untested.
- **Audio quality.** Pitch was judged on a pure sine. Speech and music (transients, formants,
  stereo) were not listened to.
- **Speed on video.** The retimed video keeps its frames and changes the frame rate (12 / 48 fps).
  Conforming to a project frame rate needs a re-encode, which was not measured. Speed inside a
  VideoFlow export (layer `speed` + pre-stretched audio layer) was not wired.
- **Scope.** No transitions, effects, captions or multi-track audio mixing were exercised. There is no
  integration into `web/` (its Vite config, i18n, lazy routes, Garage upload). The history ran
  against scripted ops and three UI gestures, not a real editing session.
- **Unmeasured candidates.** The mediabunny version inside VideoFlow's worker string is inferred.
  `@soundtouchjs/audio-worklet` (MPL-2.0) and `phaze` were not measured.
