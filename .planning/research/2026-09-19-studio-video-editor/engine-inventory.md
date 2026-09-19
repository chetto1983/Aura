# Browser video editor — inventory before invention (2026-09-19)

Scope: what exists for growing Aura's single-clip mediabunny editor
(`web/src/mediaEdit/videoMedia.ts`, `VideoEditor.tsx`) toward split / speed / text / image overlay /
keyframes / transitions / multi-track / undo-redo / audio mixing, with **everything in the browser and no
request leaving the origin**. Aura's `LICENSE` is MIT and `web/` pins `mediabunny@1.58.1`, `konva@9.3.18`
(via filerobot) and `react-konva@19.3.0`.

Method:
- Repos cloned with `git clone --depth 1` into `D:/tmp/vid-*`. npm tarballs were unpacked into `D:/tmp/npmtar/`.
- Versions and dates come from `npm view <pkg> --json` (`dist-tags.latest` and `time[latest]`).
- Commit counts are `gh api repos/<r>/commits?since=2026-06-21` on the default branch.
- Bundle size is **measured**, not taken from bundlephobia (its API returned errors for 20 of 22 packages).
  Each package was bundled with esbuild 0.x as `import * as m from '<pkg>'` (whole public surface, minified, ESM, browser).
  react/react-dom are external, wasm/css/fonts are stubbed, and the result is gzipped with zlib.
  The "excl. mb" figures also treat `mediabunny` and `konva` as external, which gives the size added on top of what Aura already ships.
  Setup: `D:/tmp/bundle-measure/measure.mjs`.
- Network checks grep the shipped dist or source for `fetch(`, URLs, posthog and CDN imports, then read the call site.

Feature legend: **S** split · **Sp** speed · **T** text · **I** image overlay · **K** keyframe/enter-exit animation ·
**Tr** transitions · **M** multi-track · **U** undo/redo · **A** audio mixing.
✓ = public API present · ~ = only primitives to build it from · ✗ = absent · ? = not verified.

## Table

| Candidate | License (exact) | Latest + date | Commits 90d | React 19 | Export path | S | Sp | T | I | K | Tr | M | U | A | Embed / fork | Gzip (measured) | Own network requests | vs mediabunny |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| **mediabunny** | MPL-2.0 | 1.58.1 · 2026-09-19 | 163 | n/a (no framework) | WebCodecs, own muxers | ~ | ~ | ~ | ~ | ✗ | ✗ | ~ | ✗ | ~ | lib (in tree) | 175 KB (whole surface) | none (only `UrlSource` with a caller URL) | — |
| **WebAV** `@webav/av-cliper` + `av-canvas` | MIT (tarball LICENSE; package.json `license` empty). "WebAV Pro" is a separate paid product | 1.2.8 · 2026-01-10 | 0 | framework-agnostic, no peers | WebCodecs + own `@webav/mp4box.js`, MP4 out only; OPFS | ✓ | ✓ | ~ | ✓ | ✓ | ✗ | ✓ | ✗ | ✓ | lib | 60 + 49 KB | only `fetch(font.url)` when caller passes a font URL | **duplicates** demux/mux (mp4box.js), MP4-only input |
| **VideoFlow** `@videoflow/core` + `renderer-browser` + `renderer-dom` | Apache-2.0 (repo LICENSE; tarballs ship no LICENSE file) | 1.3.4 · 2026-09-16 | 29 | framework-agnostic | WebCodecs via **mediabunny ^1.40 (imported)**; `renderer-server` = Playwright, optional | ✗ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✗ | ✓ | lib | core 8 KB; browser 226 KB; dom 187 KB (excl. mb) | **yes**: Google Fonts CSS for default "Noto Sans" on every `initLayers` + any registry font | **reuses** (dedupes to our 1.58.1) |
| ↳ `@videoflow/react-video-editor` | "VideoFlow React Video Editor License": free ≤3 employees / non-profit, **company license otherwise** (not OSI) | 1.3.5 · 2026-09-18 | 4 | peer `^18 \|\| ^19` | via renderer-browser | ✗ (trim only) | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | React component | ? | inherits fonts fetch | reuses |
| **Omnitool** `@omnimedia/omnitool` | MIT | 1.1.0-135 (pre-release, "WIP, breaking changes") · 2026-09-18 | 75 | framework-agnostic (lit) | WebCodecs via **mediabunny ^1.27.3 (imported)** + pixi.js 8, File System Access | ~ | ✗ | ✓ | ✓ | ✓ | ✓ | ✓ | ✗ | ✓ | lib | 314 KB (excl. mb) | only if bg-remover/transcribe used (`@huggingface/transformers` model download) | **reuses** |
| **Diffusion Studio core** `@diffusionstudio/core` | npm says MPL-2.0, but since v4 the GitHub repo holds only playground+docs (no `src`); **watermark unless a signed paid key** | 4.0.3 · 2025-11-30 | 0 | framework-agnostic | WebCodecs, **inlined private copy of mediabunny**; needs SharedArrayBuffer (COOP/COEP) | ✓ | ✗ | ✓ | ✓ | ✓ | ✓ | ✓ | ~ (checkpoints) | ✓ | lib | 123 KB | license check offline (ECDSA); font presets pull fonts.gstatic / S3 only if used | **duplicates** |
| **Twick** `@twick/*` | "Sustainable Use License v1.0" (not OSI: no SaaS without commercial agreement, no rebrand); only `@twick/ffmpeg-web` is MIT | 0.15.31 · 2026-05-07 | 0 | peer `^18 \|\| ^19` | `browser-render`: WebCodecs + mediabunny + ffmpeg.wasm/mp4-wasm; also `render-server` | ✓ | ? | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | React lib (whole editor) | video-editor 2019 KB; timeline 133 KB | **yes**: ffmpeg core + mp4-wasm from cdn.jsdelivr.net by default; posthog-js bundled (opt-in) | reuses + adds ffmpeg |
| **OpenVideo** (ex designcombo react-video-editor) `openvideo`, `@openvideo/core\|engine-pixi\|timeline` | package.json says MIT, **tarball LICENSE = "OpenVideo License"** (company license >3 employees); engine repo 404 (not public) | 1.4.0 · 2026-08-24 | 1 (app repo) | app: React 19.2 | WebCodecs + mediabunny + pixi.js | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | lib + Next.js app | engine-pixi 662 KB; timeline 127 KB | **yes**: unconditional POST `telemetry.combo.sh/ping` with hostname on every render | reuses |
| **@designcombo/timeline, /state** | **DESIGNCOMBO COMMERCIAL LICENSE** (Layerhub LLC) | 5.5.8 · 2025-12-05 | — | peers none | n/a (state + fabric timeline) | | | | | | | ✓ | ✓ | | lib | 118 + 30 KB | none seen | n/a |
| **Remotion** `remotion`, `@remotion/player`, `@remotion/web-renderer` | "Remotion License": free for individuals, ≤3-employee for-profits and non-profits; **company license otherwise** (not OSI). `@remotion/timeline-utils` is MIT | 4.0.526 · 2026-09-17 | 2920 | peer `>=16.8` / `>=18` | web-renderer: WebCodecs via **pinned mediabunny 1.56.1**; or Node/Lambda | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✗ (paid "Editor Starter") | ✓ | lib (React compositions) | player 95 KB; web-renderer 570 KB (excl. mb) | **yes**: POST `www.remotion.pro/api/track/register-usage-point` on every render, success and failure, even with `"free-license"` | **duplicates** (own pinned copy) |
| **Revideo** `@revideo/*` | MIT | 0.11.0 · 2026-07-10 | 19 | player-react (no peer) | **needs Node**: WasmExporter uploads to `/uploadVideoFile`, `/audio-processing` (vite-plugin + ffmpeg) | ~ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✗ | ✓ (server) | code-first engine | core 49 KB; 2d 977 KB | CLI telemetry via posthog-node (Node side) | separate (mp4-wasm) |
| **Motion Canvas** | MIT | 3.17.2 · 2024-12-14 | 1 | no React | **needs Vite dev server** (HMR socket / ffmpeg plugin) | ✗ | ~ | ✓ | ✓ | ✓ | ✓ | ~ | ✗ | ~ | code-first engine | core 46 KB; 2d 743 KB | none in core | separate |
| **etro** | **GPL-3.0** (copyleft over Aura's MIT bundle) | 0.14.1 · 2026-08-12 | 12 | framework-agnostic | **MediaRecorder** real-time capture (`movie.record`), not WebCodecs | ✗ | ~ | ✓ | ✓ | ✓ | ✗ | ✓ | ✗ | ✓ | lib | 13 KB | none | separate |
| **OpenCut** (main) | MIT | no release; Rust/gpui rewrite | 22 | web shell only | n/a yet | | | | | | | | | | app (rewrite, "not taking contributions") | — | — | — |
| **OpenCut classic** | MIT (`opencut-wasm` crate MIT) | last commit 2026-05-17 | 0 | React 19 (Next.js) | WebCodecs via **mediabunny `CanvasSource`** | ✓ | ✓ (soundtouchjs, LGPL-2.1) | ✓ | ✓ | ✓ | ✗ | ✓ | ✓ | ✓ | **app to fork** (auth, Postgres, Upstash, Cloudflare) | n/m | **yes**: Databuddy analytics script, backend APIs | reuses |
| **OpenReel Video** | MIT | v1.0.0-alpha.17 · 2026-08-29 | 10 | React 19 app; `@openreel/core` private, not on npm | WebCodecs via mediabunny + ffmpeg.wasm | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | **app to fork** | n/m | **yes**: unpkg ffmpeg core, mediapipe from jsdelivr/unpkg/storage.googleapis, esm.sh mediabunny, AI SaaS (kie.ai, ElevenLabs, Freepik); posthog when env set | reuses |
| **Omniclip** (classic app) | MIT repo / ISC package.json | 1.1.3 · 2025-08-07 | 0 | lit, not React | ffmpeg.wasm + WebCodecs | ✓ | ? | ✓ | ✓ | ? | ✓ | ✓ | ✓ | ✓ | app (superseded by Omnitool) | n/m | **yes**: hard-coded PostHog key, CDN imports (jsdelivr, unpkg) | separate |
| **Timeline Studio** (MartinDelophy/ai-video-editor) | MIT | no npm; commit 2026-09-17 | 201 | React app | mediabunny + ffmpeg.wasm + libav.js | ✓ | ✓ | ✓ | ✓ | ✓ | ? | ✓ | ✓ | ✓ | app to fork | n/m | **yes**: `@heyputer/puter.js`, HF models | reuses |
| **@xzdarcy/react-timeline-editor** (+ `timeline-engine`) | MIT (engine ISC) | 1.0.0 · 2026-01-25 | 0 | peer `>=18`; react-virtualized 9.22.6 peer allows ^19 | none (UI only) | UI | | | | | | ✓ rows | ✗ | | React UI lib | 66 KB | none | orthogonal |
| **dnd-timeline** | MIT | 3.1.1 · 2026-05-16 | 1 | headless hooks on @dnd-kit | none (UI only) | UI | | | | | | ✓ | ✗ | | React headless lib | 18 KB | none | orthogonal |
| **animation-timeline-js** | MIT | 2.3.5 · 2024-07-21 | 0 | framework-agnostic canvas | none (keyframe lane UI) | | | | | UI | | ✓ | ✗ | | UI lib | 18 KB | none | orthogonal |
| **@theatre/core** / **@theatre/studio** | core Apache-2.0 / **studio AGPL-3.0-only** | 0.7.2 · 2024-05-19 | 0 | framework-agnostic | none (animation sequencer) | | | | | ✓ | | | studio only | | lib | core 35 KB | none seen | orthogonal |
| **konva / react-konva** (in tree) | MIT | konva 10.6.0 (tree 9.3.18) · react-konva 19.3.0 · 2026-09 | active | react-konva peer `react ^19.3.0` | stage → canvas → `CanvasSource` | | | ✓ | ✓ | ~ (Tween) | | | ✗ | | lib (already shipped) | 56 KB + 45 KB (0 incremental) | none | complementary |
| **MediaFox** `@mediafox/core` | MIT (npm; GitHub shows no license) | 1.2.14 · 2026-02-07 | 0 | framework-agnostic | none (player) | | | | | | | playlist | | | lib | 28 KB (excl. mb) | none | **reuses** (peer `mediabunny ^1.25.3`) |
| **ffmpeg.wasm** `@ffmpeg/ffmpeg` / `@ffmpeg/core` | MIT wrapper / **GPL-2.0-or-later core** | 0.12.15 / 0.12.10 · 2025-01-07 | — | n/a | wasm ffmpeg | ~ | ~ | ~ | ~ | ✗ | ~ | ~ | ✗ | ✓ | lib | n/m (`@ffmpeg/core` npm unpackedSize 64.7 MB) | loads core from unpkg unless self-hosted | duplicates |
| **IMG.LY CE.SDK / VEED / Shotstack / Rendley** | **excluded**: proprietary, needs a license key (CE.SDK, Rendley) or is SaaS/cloud-render (VEED, Shotstack) | not inspected | — | — | vendor | | | | | | | | | | — | — | license server / cloud by design | — |

"n/m" = not measured (app, not a library).

## Evidence per candidate

**mediabunny 1.58.1** (`web/node_modules/mediabunny/dist/modules/src/*.d.ts`, `D:/tmp/vid-mediabunny/docs/guide/converting-media-files.md`)
- `conversion.d.ts:93-102` `composable: true`: a conversion only adds tracks, so *several conversions can target one Output*. `execute({until})` (`:366-379`) advances them in lockstep (docs l.676-690 show the loop). That is the only multi-input primitive. There is no built-in concatenation or timeline.
- `conversion.d.ts:196-218` `video.process(sample)` returns a CanvasImageSource/VideoSample/array/null, plus `processedWidth/Height`. This is the overlay hook (draw text/images or a Konva stage per frame).
  `sample.d.ts:283-286` `setTimestamp`/`setDuration` let `process` retime video, i.e. speed, video only.
- Audio: `process(AudioSample)` (`:268`), `AudioBufferSink.buffers()` (`media-sink.d.ts:347-377`) and `AudioBufferSource` (`media-source.d.ts:231-250`).
  **No mixer and no time-stretch**: mixing means OfflineAudioContext, done by us. The internal resampler is not exported (`index.d.ts`).
- Composition by hand: `CanvasSink.canvases()/getCanvas(t)` per input (`media-sink.d.ts:252-285`) → draw on one canvas → `CanvasSource.add(timestamp,duration)` (`media-source.d.ts:91-112`).
  OpenCut classic's exporter does exactly this (`apps/web/src/services/renderer/scene-exporter.ts:3-16`).
- `grep -rniE "concat|speed|mix"` over the `.d.ts` found only NAL-unit concat helpers and channel up/down-mixing.

**WebAV** (`D:/tmp/npmtar/webav-av-cliper-1.2.8/package/dist/av-cliper.d.ts`, `D:/tmp/vid-WebAV`)
- `split(time)` on MP4Clip/ImgClip/AudioClip/EmbedSubtitlesClip (`:66,473,614`). `BaseSprite.time.playbackRate` (`:88-96`). `setAnimation(TKeyFrameOpts)` with `'from'|'to'|'NN%'` keyframes (`:144,806`).
- `Combinator.addSprite/output()` produces an MP4 ReadableStream (`:177-222`). Also `concatAudioClip`, `fastConcatMP4`, `mixinMP4AndAudio`, `renderTxt2ImgBitmap` (text is rendered to a bitmap).
- Dependencies: `@webav/mp4box.js`, `opfs-tools`, `wave-resampler`. README: "Compatible with Chrome 102+", advanced features in paid "WebAV Pro". No transitions in the OSS API.
- The only `fetch(` in the dist is the caller-supplied `font.url`. Its own editor demo uses `@xzdarcy/react-timeline-editor` (`packages/av-canvas/demo/video-editor.tsx`).
- Repo moved to `WebAV-Tech/WebAV` (the hms-dbmz URL 404s). No commits since 2026-01-10.

**VideoFlow** (`D:/tmp/vid-VideoFlow/src`, `D:/tmp/vid-videoflow-react-video-editor`)
- `core/layers/BaseLayer.ts:6-53` (`speed` → `timelineDuration = sourceDuration / speed`). Layer types: Video/Audio/Image/Text/Captions/Shape/Group.
  `transitionIn/Out` presets. `renderer-browser/audio/mixer.ts` mixes via OfflineAudioContext.
- `renderer-browser/package.json:47` `"mediabunny": "^1.40.0"`. It is imported (`BrowserRenderer.ts:69`, `video/VideoFrameSource.ts:65`), so it dedupes to Aura's copy.
- **Network**: `BrowserRenderer.ts:514-516` always loads `'Noto Sans'`, and `:1528-1541` does `fetch(buildFontUrl(...))` against fonts.googleapis.com. The same happens in `renderer-dom/DomRenderer.ts:1260,269`. A patch-package fix or self-hosted fonts would be needed.
- The editor's exported commands (`dist/commands/index.d.ts`) have trim/move/resize/keyframe/transition/effect but **no split**. Undo is Immer patches.
  The editor's LICENSE.md is the tiered company license; it says core and renderers are Apache-2.0.

**Omnitool** (`D:/tmp/vid-omnitool/s`)
- `timeline/types.ts` keyframes (`[time,value]`, 6 interpolations) for opacity/transform. `timeline/parts/animations/presets.ts` has slideIn/slideOut enter-exit presets.
  `o.sequence/o.stack/o.transition.fade`, text/image/video/audio items (README quick start).
- `renderers/export/parts/audio-mix.ts` mixes audio. `export/produce.ts` defaults to vp9/opus. mediabunny is imported in `driver/driver.ts` and `timeline/parts/media.ts`.
- There is no per-clip speed (the only `playbackRate` is in `renderers/player/parts/playback.ts`) and no undo/history module.
- Network: `features/parts/load-pipe.ts:8` `pipeline(task, model)` downloads HF models (only for bg-remover/speech). Google Fonts appear only in the demo `index.html.ts`.

**Diffusion Studio core 4.0.3** (`D:/tmp/npmtar/diffusionstudio-core-4.0.3/package/dist`)
- `core.es.js`: the Composition constructor logs "No Diffusion Studio Core key provided. Rendering with watermark.". The render loop draws the overlay when `!t?.features?.length`.
  `license/verify.d.ts` holds an ECDSA public key and `verifyPayload` (offline).
- `clips/clip/clip.d.ts:58,161-183` (transition, `split`, checkpoints). `animation/interpolate-*.d.ts` hold the keyframes. There is no playbackRate anywhere in the `.d.ts`.
- The mediabunny error string "unsupported or unrecognizable format" is present in `core.es.js` with no `import 'mediabunny'`, so it carries its own inlined copy. The render path uses `SharedArrayBuffer` and `AudioWorklet`.
- `D:/tmp/vid-core` HEAD `4e784a7` ("v4-release") contains only `playground/`, `docs/`.

**Twick** (`D:/tmp/vid-twick`, `D:/tmp/npmtar/twick-*`)
- `LICENSE.md` is the Sustainable Use License v1.0: §3 forbids offering it "as a hosted service, platform, or SaaS without a commercial agreement" and forbids rebranding.
- `packages/timeline/src/utils/analytics.ts:15-75` bundles posthog-js. It is opt-in (`enabled:true` or env) and defaults to `us.i.posthog.com`. `timeline-context.tsx:357-395` has `UndoRedoProvider`.
- `packages/browser-render/src/audio/audio-video-muxer.ts:15` sets `CDN_BASE = cdn.jsdelivr.net/npm/@ffmpeg/core@…`. `browser-renderer.ts:131` loads mp4-wasm from jsdelivr.

**OpenVideo / designcombo** (`D:/tmp/npmtar/openvideo-*`, `D:/tmp/vid-react-video-editor`)
- `designcombo/react-video-editor` now redirects to `openvideodev/react-video-editor`. Its `package.json` uses `@openvideo/core|engine-pixi|timeline 1.3.1`, `@aws-sdk/client-s3`, `@deepgram/sdk` and Pexels API routes.
- Every `@openvideo/*` tarball ships the "OpenVideo License" (company license required above 3 employees), despite `"license":"MIT"` in package.json. `github.com/openvideodev/openvideo` returns 404.
- `engine-pixi/dist/index.umd.js`: `flush()` → `fetch("https://telemetry.combo.sh/ping",{method:"POST",mode:"no-cors",body:{d:window.location.hostname,e:"render_complete"}})`. It is unconditional, and the same string is in `openvideo@0.2.18`.
- The `@designcombo/timeline|state@5.5.8` tarball LICENSE is "DESIGNCOMBO COMMERCIAL LICENSE, Copyright (c) 2024 Layerhub LLC".

**Remotion** (`D:/tmp/npmtar/remotion-*`, `D:/tmp/vid-remotion/packages`)
- `LICENSE.md`: Free License for individuals, for-profits with ≤3 employees, and non-profits. Otherwise a paid Company License is required ("In Remotion 5.0, the license will slightly change").
- `@remotion/licensing/dist/register-usage-event.js`: `HOST='https://www.remotion.pro'`, POST `/api/track/register-usage-point` with host, 3 retries.
  `web-renderer/dist/esm/index.mjs:1939-1960` sends it even for `licenseKey:"free-license"` (it just sends `null`). It is called at `:6517-6580` on success and failure.
- The web-renderer depends on `mediabunny` **pinned 1.56.1** (it installed a nested `node_modules/mediabunny` next to ours), so it would ship a second copy. `@remotion/player` and core have no usage call.

**Revideo / Motion Canvas** (`D:/tmp/vid-revideo`, `D:/tmp/vid-motion-canvas`)
- Revideo `packages/core/src/exporter/WasmExporter.ts:21-73` fetches `/@mp4-wasm`, then POSTs to `/uploadVideoFile`, `/revideo-ffmpeg-decoder/finished`, `/audio-processing/generate-audio` (Node side). `packages/telemetry` uses posthog-node → eu.posthog.com.
- Motion Canvas `packages/core/src/app/ImageExporter.ts:63-111` uses `import.meta.hot.send('motion-canvas:export')`. `packages/ffmpeg/client/FFmpegExporterClient.ts:183` calls `fetch('/ffmpeg/…')`. Both need a dev server.
- Both are code-first scene engines (generator functions), not NLE data models. Revideo's repo moved to `midrender/revideo`.

**etro** (`D:/tmp/vid-etro/src`)
- `LICENSE` is GPL-3.0. Export is `movie/movie.ts:227,299-326`, which captures `canvas.captureStream()` into a `MediaRecorder` in real time.
- Layers `layer/{video,audio,image,text}.ts`, effects `effect/*` (GLSL). `util.ts:110` has `KeyFrame`. `layer/audio-source.ts:13-192` has `playbackRate`.

**OpenCut / OpenCut classic** (`D:/tmp/vid-OpenCut`, `D:/tmp/vid-opencut-classic`)
- The main README says "OpenCut is being rewritten from the ground up" (Rust core via gpui, `Cargo.toml`) and "not set up to take outside contributions". The usable code is `opencut-classic`.
- Classic `apps/web/src`: `commands/` (undo), `animation/` (keyframes), `retime/`+`speed/`, `text/`, `stickers/`, `timeline/`, `masks/`.
  There is no transitions module. Speed audio uses `soundtouchjs` (LGPL-2.1).
- `app/layout.tsx:51` loads `cdn.databuddy.cc/databuddy.js`. Its dependencies include `better-auth`, `drizzle-orm`, `pg`, `@upstash/*` and `@opennextjs/cloudflare`: a hosted app, not a component.

**OpenReel Video** (`D:/tmp/vid-openreel-video`)
- The README feature list covers all nine asked features (speed "0.25x to 4x with audio pitch preservation", keyframes, transitions, unlimited undo). `packages/core/package.json` is `"private": true` (not on npm).
- Network hits in source: `unpkg.com/@ffmpeg/core@0.12.6`, `cdn.jsdelivr.net/npm/@mediapipe/tasks-vision`, `storage.googleapis.com/mediapipe-models/*`, `esm.sh/mediabunny@1.25.3`, `kieai.redpandaai.co`, ElevenLabs, Freepik.
  `apps/web/src/hooks/useAnalytics.ts` loads PostHog only when `VITE_PUBLIC_POSTHOG_KEY/HOST` are set.

**Omniclip / Timeline Studio** (`D:/tmp/vid-omniclip/s/main.ts:1-35`, `D:/tmp/vid-ai-video-editor/package.json`)
- Omniclip calls `posthog.init('phc_CMbH…', {api_host:'https://eu.i.posthog.com'})` and imports `opfs-tools`, `mediainfo.js`, `@ffmpeg/core` and shoelace from jsdelivr/unpkg at runtime. The README says "Omniclip 2.0 is in development" (that is Omnitool).
- Timeline Studio (MIT, 201 commits in 90 days) depends on `@heyputer/puter.js`, `@huggingface/transformers`, `kokoro-js`, `@ffmpeg/*`, `@libav.js/variant-webcodecs` and `mediabunny`. It is an AI-first app, fork only.

**Timeline UI components** (`D:/tmp/npmtar/xzdarcy-react-timeline-editor-1.0.0`, npm metadata)
- `@xzdarcy/react-timeline-editor` `dist/interface/timeline.d.ts`: `editorData: TimelineRow[]`, `effects`, `scale`, `gridSnap`, `dragLine`, `enableRowDrag`, `getActionRender`, `onActionMoving/ResizeEnd/...`, `engine`. It has no history and no media awareness.
- `dnd-timeline` 3.1.1 is headless hooks over `@dnd-kit/core` (18 KB gz). `animation-timeline-js` is a canvas keyframe-lane control, last release 2024-07.
- `@theatre/core` (Apache-2.0) is a keyframe sequencer. Its editor UI `@theatre/studio` is **AGPL-3.0-only**. Last release 2024-05.
- `react-konva@19.3.0` has peer `react ^19.3.0` and `konva ^8||^9||^10`. Konva's Transformer, Tween and `stage.toCanvas()` are already in Aura's bundle via filerobot.

**MediaFox** (`D:/tmp/bundle-measure/node_modules/@mediafox/core/README.md`)
- A mediabunny-powered player (peer `mediabunny ^1.25.3`): canvas render (WebGPU/WebGL/2D), Web Audio, playlists. It is 28 KB gz on top of mediabunny and has no URLs in its dist. It is a preview-playback option, not an editor.

## Gaps — nothing under an acceptable license covers these

"Acceptable" means OSI license compatible with Aura's MIT bundle, embeddable as a library, no server, no request of its own.
That leaves: mediabunny, WebAV, VideoFlow core/renderers (after patching the Google Fonts fetch), Omnitool (pre-release), react-timeline-editor, dnd-timeline, animation-timeline-js, @theatre/core, konva/react-konva, MediaFox.

1. **Undo/redo for a video timeline model.** None of the acceptable libraries has one.
   It exists only in non-OSI packages (VideoFlow editor, Twick, OpenVideo, designcombo) or as app code inside MIT apps (OpenCut classic `commands/`, OpenReel) that would have to be extracted.
2. **An embeddable editor shell** (React 19 timeline + inspector + preview, wired to an engine) under an OSI license. Every ready-made shell is either non-OSI (VideoFlow editor, Twick, OpenVideo, Remotion Editor Starter) or a whole hosted app (OpenCut classic, OpenReel, Omniclip, Timeline Studio).
   The OSI timeline widgets (react-timeline-editor, dnd-timeline) know nothing about media, trims or keyframes.
3. **No single OSI engine covers all nine features.**
   - VideoFlow lacks split in both core and editor, and its undo is in the non-OSI editor.
   - WebAV lacks transitions and undo, and has had no commits in 90 days.
   - Omnitool lacks per-clip speed and undo, and is a pre-release.
   - mediabunny gives primitives only: composable conversions, `process`, sinks/sources. It has no keyframes, transitions, mixer or time-stretch.
4. **Speed change with pitch preservation.** No acceptable library was verified to time-stretch audio.
   mediabunny exports no resampler or stretcher. WebAV's `playbackRate` was not verified for pitch. The only implementation seen is `soundtouchjs` (LGPL-2.1) inside OpenCut classic.
5. **Zero-network out of the box.** Even the best-fitting engine (VideoFlow renderer-browser/dom) fetches Google Fonts on init and would need a patch. Every non-OSI option also phones home or pulls from a CDN: Remotion web-renderer, OpenVideo, Twick's ffmpeg/mp4-wasm.
