# Video Studio audio — spike findings

Spec: `docs/superpowers/specs/2026-09-27-video-studio-audio-design.md`. Probes run in headless
Chromium (Playwright 1.62.1, chromium-headless-shell 1234, WSL) against the probe's own Vite server.

## Fixtures

The VM's TTS (`/api/voice/capabilities` → `{"tts":true,"stt":true}`) answered each phrase as a
24 kHz mono MP3 (48–50 kB). `verify-fixtures.mjs` on the committed bytes:

```
music RMS -18.24 dBFS (want -18.24)
speech.wav: speech windows -18.02, -18.18, -18.35 dBFS; gaps -200.00, -200.00, -200.00 dBFS
speech-noisy.wav: speech windows -17.63, -17.74, -17.92 dBFS; gaps -27.63, -27.95, -29.00 dBFS
```

Ground truth: `{"sampleRate":16000,"snrDb":10,"speechRmsDb":-18.18,"windows":[[1,3.662],[5.162,7.857],[9.357,11.905]]}`.

## S1 — how VideoFlow applies volume, and in which time domain

`node s1.mjs` (BrowserRenderer 1.3.4 `renderAudio()`, 100 ms RMS windows, `music.wav`). The brief's
five cases put `volume` in the layer's properties, as a number or as a keyframe array:

```
static-1: level -21.24 dBFS, drop at timeline none s
static-0.5: level -21.24 dBFS, drop at timeline none s
step-untrimmed: level -21.24 dBFS, drop at timeline none s
step-trimmed-2s: level -21.07 dBFS, drop at timeline none s
step-trimmed-2s-speed-2: level -21.27 dBFS, drop at timeline none s
```

That output matches none of the brief's readings, because none of the five volumes reached the mix.
`compile()` copies both forms verbatim into `properties` (`out/s1.json`, `compiled`). Without
transitions, the mixer reads only `animations` (`renderer-browser/dist/audio/mixer.js:197`,
`applyAudioKeyframes`), and the gain node starts at 1. The second round writes
`animations: [{property: 'volume', keyframes}]` straight into the compiled VideoJSON, using the
brief's exact STEP keyframes:

```
json-static-0.5: level -27.26 dBFS, drop at timeline none s
json-step-untrimmed: level -21.24 dBFS, drop at timeline 3 s
json-step-trimmed-2s: level -21.07 dBFS, drop at timeline 1 s
json-step-trimmed-2s-speed-2: level -33.31 dBFS, drop at timeline none s
json-two-point-ramp-0-to-1-over-2s: level -21.24 dBFS, drop at timeline 1 s
video-static-property-0.5: level -24.08 dBFS, drop at timeline none s
video-static-property-1: level -24.08 dBFS, drop at timeline none s
video-json-0.5: level -30.1 dBFS, drop at timeline none s
```

The speed-2 case prints "none" because it has already dropped by its reference window. Its levels
are −21.2 dBFS for 0–0.4 s and −33.3 dBFS from 0.5 s: a drop at **0.5 s**. A gain of 0.5 measures
−6.02 dB and a gain of 0.25 measures −12.06 dB, both exact.

**Verdict**
- Keyframe times are **absolute source seconds**. The timeline position is
  `startTime + (kf.time − sourceStart) / |speed|`: trimmed 2 s → 1.0 s, trimmed with speed 2 → 0.5 s.
- Between keyframes the gain **steps**. The mixer calls `setValueAtTime` once per keyframe, with no
  ramp. In the two-point case (0 at 1 s, 1 at 3 s), the level stays at −200 dBFS until 3.0 s and
  then jumps to full.
- Before the first keyframe the gain is **1**, not the first keyframe's value. The ramp case plays
  at full volume for its first second. Keyframes that map before timeline 0 are skipped
  (`mixer.js:214`).
- A static `volume` is ignored for video layers too: 0.5 and 1 both measure −24.08 dBFS.

**Consequences for Plan B**
- The spec's Compile section ("either a static `volume` or the keyframes from `volumeCurve`",
  spec §Compile) cannot work as written. Every audible layer's volume must go into `animations`, in
  source seconds, with:
  - a keyframe at `sourceStart` carrying the initial value;
  - fades and envelope ramps sampled densely, because the mixer will not interpolate them.

  `toVideoJSON` already ends in `flow.compile()` (`web/src/videoStudio/videoflow.ts:329`), so the
  measured way is to write `animations` into the returned JSON.
- **Live bug**: the Studio's clip Volume slider does nothing on a clip without transitions.
  `addVideo({volume: clip.volume ?? 1})` (`videoflow.ts:220`) is the ignored static form measured
  above. Clips WITH transitions take the other mixer branch, which samples
  `getPropertiesAtFrame` + `applyTransitions` per frame and does merge static properties
  (`mixer.js:177-195`); this was read in the code, not measured. T2 fixes it on touch, with its own
  test.
- **Suspected bug, not measured**: `clipOpacity` (`videoflow.ts:75-94`) hands its fade to VideoFlow
  as a keyframe array in `properties`, with times from 0 to `clip.duration` (segment time). S1
  shows that `compile()` leaves such an array in `properties` untouched. Nothing here shows what
  the visual runtime does with it (`RuntimeBaseLayer.getPropertiesAtFrame` → `ensureUnit`). Even if
  it animates, the times are in the wrong domain for every trimmed clip. T2 measures it before
  touching it.

**What S1 does not show**
- Only the offline mix (`renderAudio`) was measured. Neither the DOM preview's live playback nor
  the MP4 mux is covered, and `@videoflow/renderer-server` (sub-project 2) is not measured at all.
- Only forward speed: the reverse branch (`speed < 0`) was not measured.
- Only one layer at a time, and no audio layer with transitions.
- Keyframes created through the API (`layer.set` / `animate`) were not measured. `serializeLayer`
  folds a single keyframe back into the static `properties`, which the mixer ignores.
