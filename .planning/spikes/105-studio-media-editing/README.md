---
spike: 105
idea: studio-media-editing
name: studio-media-editing
type: standard
validates: "Given a Studio image or video (or a phone upload), when the operator edits it in the browser with Filerobot (photos) or Mediabunny (video trim), then the saved file is valid, correctly oriented and cut where asked, with no AI model and no request leaving the origin"
verdict: VALIDATED
related: []
tags: [studio, web, image-editing, video-trim, webcodecs, filerobot, mediabunny]
---

# Spike 105: Studio media editing without AI models

## What This Validates

Given the Studio's outputs (1024 px PNG/WebP, H.264+AAC MP4, silent MP4) and phone uploads
(12 MP JPEG with EXIF orientation, HEVC, rotated MP4, VP9 WebM), when the operator crops/filters a
photo or trims a clip entirely in the browser, then the result is a valid file of the expected size,
orientation and duration, on React 19 + Vite 8 (the cockpit's stack), fully offline.

## Research

| Approach | Tool | Pros | Cons | Status |
|---|---|---|---|---|
| Photo editor | `react-filerobot-image-editor` 5.0.0-beta.159 (MIT) | crop/rotate/flip, finetune, 20+ filters, annotate, watermark, resize; React 19 peers; theme + `translations` props | `latest` dist-tag is a beta; pulls konva + styled-components + @scaleflex/ui | **chosen** |
| Photo editor | Pintura / IMG.LY CE.SDK | polished | commercial | rejected |
| Video trim | `mediabunny` 1.58.1 (MPL-2.0, zero deps) | WebCodecs, copy-trim with edit lists, crop/rotate/flip/resize/mute, MP4+WebM | the trim UI is ours to write | **chosen** |
| Video editor | Twick 0.15 | full multitrack timeline | Sustainable Use License, 0.x | rejected |
| Video editor | Remotion | mature | company licence | rejected |

The online guide says a non-zero `trim.start` "currently forces a transcode": **outdated**. The
installed 1.58.1 d.ts documents `copy: { boundaryPolicy: 'expand' | 'shrink', shiftTolerance,
boundaryTolerance }` and copies by default. Always read the installed d.ts, not the guide.

## How to Run

```bash
bash make-media.sh            # needs Docker (jrottenberg/ffmpeg:7.1-alpine) + internet for 3 photos
cd app && npm install && npm run dev    # http://localhost:5199  (?tab=photo | ?tab=video)
node run-video.mjs chrome msedge firefox   # trims → out/<browser>/, report out/video-report.json
node run-playback.mjs         # <video> first frame of trimmed files, per browser
node run-photo.mjs chrome     # saves via Filerobot, UI filter+Save, console/off-origin audit
node run-it.mjs               # our Italian table, offline
bash probe.sh out/chrome/*.mp4    # independent ffprobe of the outputs
```

## Investigation Trail

1. **Build.** Filerobot 5.0.0-beta.159 + react-konva 19.3 + styled-components 6.5 + Mediabunny
   build clean under React 19.3 / Vite 8.3. Lazy chunks, gzip: **Filerobot 272.6 KB**,
   **Mediabunny 142.6 KB** (all demuxers/muxers; not tree-shaken further here).
2. **Video, 13 cases × 3 browsers** (Chrome 153, Edge 153, Firefox 155) — all produced files.
   ffprobe agrees with Mediabunny's own reading on every Chrome output.
3. **Cut precision.** Burned-in `hh:mm:ss.mmm fN` stamps; requested cut 2.5 s, keyframes every 2 s.
   `exact` (transcode) → first frame 2.500 f75. `expand` (copy) → **also 2.500 f75**: the MP4
   carries the 2.0 s keyframe with a negative timestamp and an edit list hides it; ffmpeg and the
   `<video>` element of Chrome, Edge and Firefox all start at f75. `shrink` → video starts at the
   next keyframe (4.000 f120) while audio starts at 2.5: 1.5 s without picture. **Never use shrink.**
4. **Speed.** copy (`expand`): 10–60 ms for 3.5–30 s clips (Chrome/Edge), 35–360 ms (Firefox).
   exact: ~1 s for 3.5 s at 720p; 30 s at 1080p = 10.9 s Chrome, 8.3 s Edge, 11.1 s Firefox.
5. **WebM.** `expand` on VP9 transcodes anyway (WebM has no edit lists; default shiftTolerance 0):
   ~0.8 s in Chrome, **6.8 s in Firefox** (software VP9) for 3.5 s.
6. **Firefox traps.** (a) cannot encode AAC → `exact` on an MP4 writes **Opus-in-MP4** audio;
   (b) cannot decode HEVC → `exact` on the HEVC clip **silently dropped the video track**
   (`undecodable_source_codec`) and still returned `isValid: true` with audio only; the copy
   (`expand`) path works but Firefox cannot play the HEVC result either.
7. **Rotation.** `clip-rot90` exact keeps the 90° display matrix; `rotate: 90` + `width: 640`
   bakes it (640×1138). Encoders: Chrome avc/hevc/vp9/av1/vp8; Edge and Firefox no hevc.
8. **Photos** (Chrome and Firefox, identical outcomes): 1024 PNG save 79–89 ms, WebP 128 ms,
   12 MP JPEG 294–351 ms. **EXIF Orientation=6 is honoured** and baked into pixels (3024×4032,
   visually rotated 90° CW). `savingPixelRatio: 4` (the library default) does not upscale past
   the natural size (still 4032×3024) but costs +70% time and +70% bytes → set 1.
9. **UI path.** Filters tab → Clarendon → Save → modal → Save: `onSave` receives PNG 1024×1024
   with `designState.filter = 'Clarendon'`. Default file name doubles the extension
   (`studio-1024.png.png`) → pass `defaultSavedImageName`.
10. **styled-components 6** forwards Filerobot's props to the DOM: 35 React/styled warnings per
    mount. A `StyleSheetManager shouldForwardProp={isPropValid}` wrapper → 0.
11. **Network.** Default `useBackendTranslations: true` calls
    `i18n-fastly.ultrafast.io/api/export?grid=undefined` (Scaleflex) on every mount and gets
    nothing back: `language: 'it'` alone stays English. With `useBackendTranslations: false` and our
    own `translations` object the UI is Italian and **zero** off-origin requests were seen. The table
    has 128 keys (`lib/context/defaultTranslations.js`); some labels (Annotate, filter names) use
    keys outside the partial table written here.
12. **Phone** (390×844, touch): Filerobot switches to its phone layout (hamburger tabs, scrolling
    tool bar); usable.

## Results

**VALIDATED**, with rules the real build must follow:

- Photos: Filerobot with `useBackendTranslations={false}`, `savingPixelRatio={1}`, a full Italian
  `translations` table (128 keys, in `web/src/i18n` style), `defaultSavedImageName`, and the
  `StyleSheetManager` prop filter. Adds konva, react-konva, styled-components, @emotion/is-prop-valid.
- Video: Mediabunny with `copy: { boundaryPolicy: 'expand' }` as the default cut (instant and
  frame-exact on MP4); `copy: false` only when re-encoding is needed (crop/rotate/resize, WebM).
- Refuse, don't ship, a conversion that discarded the **video** track (`discardedTracks` with
  `type === 'video'` and a reason other than `discarded_by_user`): `isValid` alone is not enough.
- On Firefox, an `exact` MP4 export changes audio to Opus: either keep `expand` there or warn.
- Not measured: Safari/iOS (no WebKit WebCodecs run here), memory on 4K/long clips, a Garage
  upload of the result, and a real OpenRouter video (the fixtures mimic its container and codecs).
