# Video Studio audio editing (sub-project 1 of 3)

Date: 2026-09-27. Status: design approved in brainstorming (model, scope, decomposition). Awaiting
review of this text.

## Why

The operator, 2026-09-27, over a screenshot of the Studio's Audio tab: *"allo studio dei video manca
la parte di editing audio e i tool da esporre a aura"*. Clideo was named as the reference.

What the Studio can do with sound today, read on 2026-09-27: two fields per clip, `volume` (0–2)
and `muted` (`web/src/videoStudio/project.ts:48-49`), edited by the Audio tab
(`web/src/videoStudio/Inspector_clip.tsx:243-275`). There is no audio lane, no audio source kind
(`ProjectSource.kind` is `'video' | 'image'`, `project.ts:16`), no fade, no noise control, and the
picker refuses audio files (`SOURCE_ACCEPT`, `VideoStudio_sources.ts:31`). Aura has no editing tool
at all.

## The whole job, and this document's part of it

The work is split into three sub-projects, each with its own spec, plan and execution, in this
order:

1. **Audio in the Studio (this document).** The model, the commands and the UI for audio lanes,
   Extract audio, waveform + volume envelope, ducking, noise reduction, TTS and voice recording.
2. **`aura-video-mcp` sidecar.** A Node container that imports the Studio's pure TypeScript core,
   exposes MCP tools to Aura (deferred by the existing bridge, `internal/agent/mcptools/bridge.go:88`),
   renders with `@videoflow/renderer-server`, and delivers the result to chat and Telegram.
3. **Aura panel inside the Studio.** A chat bound to the open project that edits it through the
   sub-project 2 tools; the Studio reloads the new version as one undoable step.

One constraint of sub-project 2 binds this one: **the core stays pure.** `project.ts`,
`commands*.ts`, the compile step and the volume-curve function must not touch the DOM, Web Audio or
`fetch`, so the sidecar can import them unchanged. Everything that needs a browser (decoding, VAD,
denoising, peaks, recording) lives in separate modules that only WRITE their results into the
project through commands.

Out of scope, decided: a stock-music library (Clideo's is Pixabay's; Pixabay has no public music
API), camera/screen recording, and the video-side timeline tools Clideo has and we do not
(duplicate, bring forward/backward, freeze frame).

## Clideo, measured 2026-09-27

Driven with Playwright (WSL, headless Chromium) through five sessions on an anonymous project
holding only synthetic fixtures; its GraphQL traffic was recorded.

| Area | What Clideo does |
|---|---|
| Audio item inspector | Tabs **Audio** (Volume 0–100 %, 100 % is the end of the slider; Fade In 0–5.0 s; Fade Out 0–5.0 s; Noise reduction on/off), **Speed** (presets 0.5, 0.75, 1, 1.25, 1.5, 2), **Time** (duration presets 1–30 s, timing). |
| Video clip, Audio tab | Volume, Noise reduction, an **Extract audio** button. |
| Timeline | Audio on its own lane under the video, waveform drawn inside the item. Toolbar: Split, Freeze frame (video only), Duplicate, Bring Forward, Send Backward, Delete, Timeline Settings, Zoom, Fit, Full Screen. |
| Sources | Upload (an audio file lands on an audio lane by itself), stock music, Record (Audio / Camera / Screen / Screen & Camera), TTS (language, voice, text), Generate. |
| Model (`ProjectSynchronize`) | Absolute positions in ms (`playbackStartTime`, `trimFromStartTime`, `trimFromEndTime`), no ripple. `audioOptions {volume, volumeFadeInTime, volumeFadeOutTime, isNoiseReduced, basicSpeed}`; the same audio fields on a video clip. TTS is its own element type (`ttsTrackElements`). |
| AI Agent | The browser sends the chat plus the whole project (`projectStructure`); the model answers with actions (`adjust_audio_volume {ids, volume}`, `adjust_audio_fade {ids, fade_out}`, `timeline_clip_trim {ids, mode, trim_from_start}`); **the browser executes them** and returns a TOOL message per action ("volume: 50 => 30"). |

Measured limits worth beating: asked for ducking, the agent only set the music to 30 %; asked to
detach a clip's audio it answered "feature not available" although the button exists; its trim
from the start left a 1 s gap at the head (no ripple); a 40 s music file over 8 s of video made the
project 40 s long.

What we take: the Audio / Speed / Time set on an audio item, fades bounded at 5 s, waveform in the
item, Extract audio in the clip's Audio tab, and — for sub-projects 2 and 3 — tool results written
as "before => after". What we keep because it is better: the anchored, rippling model; music never
lengthening the project past the video; a volume range to 200 %.

## Inventory — what already exists

Read from the installed packages and the npm registry on 2026-09-27. Nothing below is written by us.

| Need | Package | Licence | Evidence |
|---|---|---|---|
| Audio lane in the render | `AudioLayer` of `@videoflow/core` 1.3.4 (installed) | Apache-2.0 | `node_modules/@videoflow/core/dist/layers/AudioLayer.d.ts` |
| Volume envelope in the render | `volume` is `animatable: true` | Apache-2.0 | `AuditoryLayer.js:34`; the mixer applies it on the gain node, `renderer-browser/dist/audio/mixer.js:262` (`applyAudioKeyframes(layer, 'volume', gainNode.gain, …)`) |
| Same audio in preview and export | the DOM preview plays `renderMixedAudio`, the export's own mixer | Apache-2.0 | `renderer-dom/dist/DomRenderer.js:43,792` |
| Waveform + draggable volume points + mic recording | `wavesurfer.js` 8.0.1, plugins `envelope`, `record`, `regions` | BSD-3-Clause | published 2026-09-24; `src/plugins/` lists `envelope.ts`, `record.ts`, `regions.ts` |
| Speech detection for ducking | `@ricky0123/vad-web` 0.0.31 (Silero) with offline `NonRealTimeVAD` | ISC | published 2026-09-12; exported from `packages/web/src/index.ts` |
| Noise reduction | `@sapphi-red/web-noise-suppressor` 0.4.1 (Web Audio nodes); fallback `@shiguredo/noise-suppression` 2025.1.0 | MIT / Apache-2.0 | both published 2026-09-19 |
| Decode and encode audio files | `mediabunny` 1.58.1 (installed) | MPL-2.0 | already used by the export and by `e2e/video-studio.spec.ts` |
| Text to speech | Aura's own `POST /api/tts` (Kokoro local or OpenRouter) and `GET /api/voice/capabilities` | ours | `internal/agui/voice_api.go:72-74`, `internal/multimodal/tts.go` |

Rejected: `video-edit-mcp` (MoviePy 1.0.3, file-path I/O on its own disk, last commit 2025-07-30);
`@videoflow/react-video-editor` (source-available, a company licence above three employees);
`peaks.js` and `waveform-data` (LGPL-3.0); an ffmpeg filter-graph render (a translator we would own,
and output that drifts from the preview).

**The one piece we write** is the function that turns speech windows into volume keyframes (attack,
hold, release) and multiplies them with fades and the envelope. No package does it for VideoFlow;
it is a pure function of a few dozen lines.

The SPA is served without a Content-Security-Policy (only artifact renders and MCP views set one,
`internal/agui/assets_render_api.go:85`, `internal/agui/mcp_views_api.go:105`), so WebAssembly and
AudioWorklet are not blocked. The VAD model, the ONNX runtime WASM and the noise-suppressor worklets
are **served from the Aura origin**, never a CDN: the existing E2E already fails any request to
another origin (`e2e/video-studio.spec.ts`, header).

## Model

```ts
interface AudioItem {
  readonly id: string;
  readonly sourceId: string;           // an 'audio' source, or a 'video' source (extracted audio)
  readonly anchor: OverlayAnchor;      // rides its clip through ripple, like an overlay
  readonly sourceStart: number;
  readonly duration: number;           // source seconds; may run past the anchor clip
  readonly volume: number;             // 0–2
  readonly muted: boolean;
  readonly fadeIn?: number;            // 0–5 s
  readonly fadeOut?: number;           // 0–5 s
  readonly speed?: number;             // same range as a clip; inherited on extraction
  readonly envelope?: readonly EnvelopePoint[];   // item-local seconds, gain 0–2, sorted
  readonly ducking?: { readonly amountDb: number; readonly ramp: number }; // −24..−3 dB, 0.1–2 s
  readonly denoise?: boolean;
  readonly extractedFrom?: string;     // clip id, when the item came from Extract audio
  readonly label?: string;             // TTS text or "Recording hh:mm", shown on the lane
}
interface AudioTrack { readonly id: string; readonly items: readonly AudioItem[] }

VideoProject.audio: readonly AudioTrack[]           // lanes under the video; [] when absent
VideoItem.denoise?: boolean
ProjectSource.kind: 'video' | 'image' | 'audio'
ProjectSource.speech?: readonly (readonly [number, number])[]  // source seconds, from the VAD
ProjectSource.denoisedAssetId?: string              // cleaned audio, computed once per source
```

Rules:

- **An audio item never outlives the video.** Its window starts where its anchor puts it and is
  clipped at the project's end; it never lengthens the project (`projectDuration` stays the video
  lane's).
- **An audio item never disappears because of a video edit.** When a trim or a removal takes the
  range its anchor sits in, it re-anchors to whatever clip now covers the same project time, or to
  the last clip. The overlay rule (drop the overlay, `commands.ts:173-190`) does not apply to audio.
  The one exception is `extractedFrom`: removing that clip removes the audio extracted from it.
- **Extract audio** = the clip becomes `muted: true`, and an `AudioItem` appears over the same
  source, window and speed, anchored to the clip at offset 0, with `extractedFrom` set. After that
  the two are independent; undo restores both.
- **Final volume** = fade × envelope × ducking, merged into one `volume` keyframe list by the pure
  curve function. Nothing of it is baked into the project: ducking is recomputed at every compile
  from `ProjectSource.speech`, so it stays right after any trim or move.
- **Ducking** lowers the item under the speech found in every OTHER audible source: clips that are
  not muted, and audio items that do not duck themselves.
- **Noise reduction is not applied live.** Its first activation on a source produces a cleaned
  audio asset (`denoisedAssetId`); the compile then reads those bytes. For a video clip this means
  muting the clip's own audio and laying an `AudioLayer` of the cleaned asset over the same window.
  Browser and sidecar read the same file, so they produce the same sound.
- **Compatibility**: a project saved before this change has no `audio` and loads as `audio: []`;
  the loader's validation (`isSource`, `isClip`, `isProject`, `projectStore.ts:139,154,268`)
  accepts every new field as optional.

## Commands (pure core)

New commands go in `commands_audio.ts`; `commands.ts` is at 538 lines and does not grow past its
reanchor change. Every command validates and throws `CommandRefusal` like the existing ones.

| Command | Does |
|---|---|
| `addAudio` | Puts an audio source on the first free audio lane at a project time; the anchor is the clip under that time. |
| `extractAudio` | As defined above. Refused for a clip whose source has no audio (`hasAudio === false`). |
| `setAudioProperties` | volume, muted, fadeIn, fadeOut (their sum ≤ the item's timeline length), speed, denoise, ducking. |
| `setEnvelope` | Replaces the envelope points; sorts them, clamps time to the item, gain to 0–2. |
| `moveAudio` / `trimAudio` | A drag on the audio lane: new start (re-anchored to the clip under it), new `sourceStart`/`duration` within the source. |
| `splitAudio` | Splits the selected audio item at the playhead; envelope points go to the half they fall in. |
| `recordAnalysis` | Writes `speech` and/or `denoisedAssetId` onto a source. The only door by which browser-side analysis enters the project. |
| `setClipPresentation` (extended) | Carries `denoise` for a clip; it is part of `ClipEditProperties` once `VideoItem` has it. |
| `removeItem` (extended) | Removes an audio item. |
| `resliceLane` (extended) | Re-anchors audio items by the rule above. |

## Compile

`videoflow_audio.ts` (new; `videoflow.ts` is at 438 lines) adds, for each audio item, one VideoFlow
`AudioLayer` with `startTime`, `sourceStart`, `sourceDuration` and `speed`, `mute`, and either a
static `volume` or the keyframes from `volumeCurve`. Speech windows are mapped from source time into
project time through every clip and item that plays that source. The denoise swap for clips happens
here. `toVideoJSON` stays the single entry point for preview, export and — in sub-project 2 — the
server render.

## Browser-side analysis

`audioAnalysis.ts`, lazy-loaded so the editor's first paint does not pay for ONNX:

- `decodeSource(url)` → `AudioBuffer`, via mediabunny.
- `detectSpeech(buffer)` → speech windows, via `NonRealTimeVAD`.
- `denoise(buffer)` → an Opus file encoded by mediabunny, uploaded through the existing presign
  path, answered with its asset id.
- `peaks(buffer)` → the waveform, cached in memory per source for the session. Peaks are not saved:
  they are cheap to recompute and would bloat the project file.

Each runs with an `AbortSignal`, reports progress on the control that started it, and on failure
leaves the project untouched and says so. A cancelled analysis writes nothing.

## UI

- **Rail**: a new **Audio** button opens the audio panel: *Upload audio*, *Record voice* (wavesurfer
  `record` plugin, microphone), *Text to speech* (shown only when `/api/voice/capabilities` reports
  TTS; text → `/api/tts` → asset → item at the playhead). The clip picker also accepts audio files
  and sends them to an audio lane, as Clideo does. `VideoStudio.tsx` is at 598 lines: the rail moves
  out to `VideoStudio_rail.tsx` first.
- **Timeline**: audio lanes under the overlay lanes, one row per `AudioTrack`. The item draws its
  waveform with wavesurfer from the cached peaks and its label; when selected it shows the envelope
  points (wavesurfer `envelope` plugin), draggable, double-click to add, drag off to remove. Drag
  moves the item, the handles trim it, the existing zoom and scrubber apply.
- **Audio item inspector** (`Inspector_audio.tsx`, new): **Audio** — Volume 0–200 %, Fade in 0–5 s,
  Fade out 0–5 s, Noise reduction, Ducking (on/off, amount in dB, softness in s); **Speed** — the
  clip speed component, reused; **Time** — start and duration, the existing `TimeField`.
- **Clip Audio tab**: the existing volume and mute, plus Noise reduction and an **Extract audio**
  button. The shared audio controls move out of `Inspector_clip.tsx` (531 lines) into
  `Inspector_audio.tsx`.
- **Mobile**: the existing mobile rail already has an audio entry (`VideoStudio_mobile.tsx:40`); it
  gains the same panel.
- Every string in English and Italian (react-i18next).

Server side, one change: `folderFor` files `ModalityAudio` under `media/`, next to video and images
(`internal/assets/service.go:179-184`); today audio lands in `chat/`.

## Spikes that gate the plan

Measure first, then amend the PRD, then build (CLAUDE.md, PRD-first). Each spike is throwaway code
whose result is written into this spec and the PRD amendment before Task 1 starts.

| Spike | Question | Pass condition |
|---|---|---|
| S1 | Does a VideoFlow `AudioLayer` with animated `volume` and a `speed` export as expected, and in which time domain are its keyframe times (source or layer)? | The exported MP4's audio RMS follows the keyframes within 1 dB. |
| S2 | Does wavesurfer 8 render from peaks only (no media element) inside a `dnd-timeline` item, with envelope points draggable without starting the item's drag? | Both gestures work in Chromium desktop and mobile emulation; a 5-minute track renders under 200 ms from cached peaks. |
| S3 | Which noise suppressor works in an `OfflineAudioContext`, and how well? | Noise floor in the gaps of the noisy-speech fixture drops by a measured margin; processing is faster than real time on this workstation. The margin becomes Task 6's E2E threshold. |
| S4 | Does `NonRealTimeVAD` find the speech windows of the fixture with everything served from the Aura origin? | Windows match the fixture's ground truth within 150 ms at each edge; zero requests to another origin; the lazy chunk's size is recorded. |

If S2 or S3 fails, the fallback is a question to the operator, not a component written in its place
(CLAUDE.md, STOP BEFORE BESPOKE).

## Tasks and their E2E

Every task ends with a real E2E. The protocol, from the operator's rules for this repository:

1. Unit tests, `tsc`, eslint, prettier, knip and `npm run build` green in WSL; the committed dist
   rebuilt (the `web-dist-freshness` gate).
2. Commit on `master`, push, CI green including "Publish Aura edge image".
3. The lab VM's updater applies the image (it waits for about 15 minutes without chat activity).
   Nothing is installed on the VM by hand.
4. The task's Playwright spec runs from WSL against `https://192.168.101.158`, signed in with the
   operator's own cockpit account from `.env.google` (never printed; no identity is created).
5. The exported MP4 is read back and **measured**: `e2e/support/audioMeasure.ts` (new) decodes its
   audio with mediabunny and computes RMS in dBFS per 100 ms window. Screenshots are inspected, not
   only the PASS line. The projects and assets the run created on the operator's account are
   deleted afterwards.

| Task | Delivers | E2E measures |
|---|---|---|
| T0 | Spikes S1–S4, the PRD amendment, the audio fixtures with their provenance | — (measurements are the output) |
| T1 | Model, loader compatibility, `folderFor` audio → `media/`, rail split | An old saved project opens unchanged; an uploaded audio file is listed under media. |
| T2 | Audio lane, upload, `addAudio`/`moveAudio`/`trimAudio`/`splitAudio`, inspector Audio/Speed/Time with volume and fades, compile | Music at 50 % exports about 6 dB below 100 % (±1 dB); a 2 s fade-in starts at least 15 dB below the steady level; the music stops at the video's end. |
| T3 | Extract audio | The clip is muted, its tone is still in the export, carried by the item; trimming the item's first second leaves that second silent. |
| T4 | Waveform + envelope | An envelope point dragged to 0 produces the matching drop in the export; the waveform is visible (screenshot). |
| T5 | TTS + voice recording | A TTS item's window carries speech energy; a recording made through Chromium's fake microphone (`--use-file-for-fake-audio-capture`) lands on the lane and in the export. |
| T6 | Noise reduction (clips and items) | On the noisy-speech fixture, the gaps' noise floor drops by at least the S3 margin. |
| T7 | Ducking | With −12 dB, the music inside the speech windows sits 12 dB below the music outside them (±2 dB), with ramps no shorter than the chosen softness. |

Fixtures, in `web/e2e/fixtures/video-studio/audio/`, small and committed with a README recording how
each was made: a music bed (synthetic chords), a speech clip made by Aura's own TTS, and the same
speech mixed with noise at a stated SNR. The existing clips `clip-a.mp4`/`clip-b.mp4` carry
different tones and are reused.

Unit gates, per the cockpit's standing rules: vitest coverage ≥ 85 % on every touched file, Stryker
≥ 70 % killed on the pure core (`commands_audio.ts`, the volume curve, the audio compile).

## What this does not prove

The E2E proves short synthetic fixtures on Chromium desktop and mobile emulation, analysed on this
workstation's CPU and rendered in the browser. It does not establish long-project memory use, VAD
accuracy on real noisy speech in other languages, noise reduction on music, behaviour on Safari, or
anything about the server render (sub-project 2).

## For review

- The per-task E2E runs on the VM, so **every task is pushed** and waits for an image publish and
  an update window: roughly 30–40 minutes of wall clock per task. CLAUDE.md otherwise pushes at the
  end of a phase; this spec follows the per-task E2E instruction and pushes per task.
