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

## S2 — wavesurfer's envelope inside a dnd-timeline item

`node s2.mjs`: dnd-timeline 3.1.1, wavesurfer 8.0.1 with its Envelope plugin, 4,000 cached peaks,
a 300 s item on a 0–400 range, Chromium at desktop 1280×800 and Pixel 5. Each row drags the
second envelope point down 16 px, then drags the item body 90 px sideways, then zooms to 0–200 and
100–120. Zoom cells read `width px, redraw ms, handle boxes`:

| layout | stroke | guard | mobile | render ms | point drag → point | point drag → item | body drag → item | zoom 0–200 | zoom 100–120 |
|---|---|---|---|---|---|---|---|---|---|
| span | fixed | none | no | 11.3 | ✗ | ✓ | ✓ | 1650 · 113 · 16×16 | 16500 · 120 · 16×16 |
| span | fixed | svg | no | 10.6 | ✓ | ✗ | ✗ | 1650 · 105 · 16×16 | 16500 · 119 · 16×16 |
| span | fixed | **point** | no | 12.5 | ✓ | ✗ | ✓ | 1650 · 116 · 16×16 | 16500 · 110 · 16×16 |
| span | fixed | none | yes | 11.7 | ✗ | ✓ | ✓ | 1650 · 106 · 16×16 | 16500 · 120 · 16×16 |
| span | fixed | svg | yes | 15.1 | ✓ | ✗ | ✗ | 1650 · 107 · 16×16 | 16500 · 133 · 16×16 |
| span | fixed | **point** | yes | 12.3 | ✓ | ✗ | ✓ | 1650 · 109 · 16×16 | 16500 · 131 · 16×16 |
| span | raw | point | no | 9.9 | ✓ | ✗ | ✓ | 1650 · 117 · 18×16 | 16500 · 123 · 54×16 |
| content | raw | point | no | 10.5 | ✓ | ✗ | ✓ | **810** · 112 · 16×16 | **1100** · 104 · 17×16 |

**Verdict: pass, in the configuration the bold rows name.** A guard is needed: without one, every
point drag moves the item instead. The brief's guard (any press on the envelope's SVG) blocks every
body drag too, because the SVG covers the whole waveform (`width/height 100%`, absolute,
`z-index 4`). The guard that passes stops a press only when its composed path contains an
`<ellipse>`, the only draggable element while `dragLine` is off (its default). Renders from peaks
take 10–15 ms; a zoom's re-render, measured from the range change, takes 104–136 ms. Both are under
the 200 ms bar.

The brief's code did not work as written, for five reasons, each read in the installed package:
- The envelope draws its points as `<ellipse>`, not `<circle>`.
- wavesurfer renders into an **open shadow root**. Outside it, `event.target` is retargeted to the
  host, so `target.closest('svg')` never matches; the guard reads `event.nativeEvent.composedPath()`.
- The envelope **freezes its viewBox at creation** (`0 0 ${wrapper.clientWidth} ${clientHeight}`),
  and a dnd-timeline item has no width until the timeline has measured itself. Created on mount,
  the viewBox is `0 0 1 48` for good. The first `redrawcomplete` fires at that 1 px width, with the
  handles 13,200 px wide and covering the lane, which is why the unguarded body drag first looked
  like a point drag. The waveform must be created only once its host has a width (a
  `ResizeObserver`); `renderMs` above is the first draw at full width.
- `itemContentStyle` pads the content box down to the part of the span inside the range
  (`paddingLeft/Right = max(0, −deltaX)`, dnd-timeline `index.mjs:745-754`). A waveform in it
  squeezes the whole item into the visible part (the `content` row: 810 px and then 1,100 px
  instead of 1,650 and 16,500), so it no longer lines up with time. The waveform goes in its own
  layer, `position:absolute; inset:0`, inside the item box (`itemStyle` is absolute with the full
  span width), and the timeline's `overflow:hidden` clips it.
- With `preserveAspectRatio="none"` and a frozen viewBox, a zoom stretches the strokes sideways:
  handles grow to 18 px, then 54 px (`raw`). The plugin exposes `part="polyline"` and
  `part="envelope-circle"`, so
  `::part(polyline), ::part(envelope-circle) { vector-effect: non-scaling-stroke }` on the shadow
  host keeps them at 16×16 at every zoom (`fixed`). `rx` itself is recomputed on every `redraw`.

**Consequences for Plan B's waveform component**
- Create it only after its host has width.
- Mount it in an absolute layer over the item box, not in `itemContentStyle`.
- Guard point presses by composed path, `<ellipse>` only.
- Style strokes through `::part`.
- The envelope's polyline starts and ends at `y = height`, volume 0 (`points: 0,${h} ${w},${h}`).
  The component always passes the two edge points (time 0 and the item's duration) at the item's
  edge gain, or an untouched envelope draws as a fade-in and a fade-out that the mix does not do.

**Package check — `react-audio-visualize` 1.2.0** (MIT, last push 2024-09-27, operator's
suggestion)
- `AudioVisualizer` decodes a whole `Blob` to draw bars. It takes no cached peaks, has no envelope,
  and knows nothing of zoom.
- `LiveAudioVisualizer` only draws a live FFT from a `MediaRecorder` that the caller still manages.

wavesurfer, already a dependency, covers both. The waveform side is measured above. For the voice
recording side, its Record plugin (`wavesurfer.js/plugins/record`) wraps `MediaRecorder` with
pause/resume and device-loss handling, delivers the Blob on `record-end`, and draws the live
waveform (`scrollingWaveform` / `continuousWaveform`). Not adopted: a second package for a subset
of what the first already does.

**What S2 does not show**
- A pointer only, never a two-finger touch: the mobile rows are Pixel 5 emulation driven by mouse
  events.
- Not the Studio's own item markup (`ItemButton`, `setActivatorNodeRef`), and not keyboard drags.
- Not peaks computed from a real file: the peaks are synthetic.
- Not an envelope with more than three points.

## S3 — which noise suppressor, and how much it cleans

`node s3.mjs`: `@sapphi-red/web-noise-suppressor` 0.4.1, run offline (`OfflineAudioContext`,
48 kHz, mono) over `speech-noisy.wav` (10 dB SNR) and `speech.wav`. Speech and gaps are scored
against the fixture's ground-truth windows.

**First run: silence from all three.** Every suppressor gave a 171.7 dB "floor drop" and a
182.3 dB "speech loss", at over 1,200× real time. Nothing had been processed. Each processor
instantiates its WASM in an async block inside its constructor. Until that finishes, `process()`
skips (`!this.processor`) and leaves the output buffer at zero, and the processor posts no ready
message (`dist/rnnoise/workletProcessor.js`, class `m`). An offline render of 12.9 s outruns that
init. The package has no offline example (issue #7, "Example with OfflineAudioContext source",
closed unanswered). `s3-wait.mjs` measured two ways to let the init finish:

```
prestart wait=0ms: floor drop 171.7 dB, speech loss 182.3 dB
prestart wait=50ms: floor drop 37.7 dB, speech loss 0.5 dB
prestart wait=500ms: floor drop 37.7 dB, speech loss 0.5 dB
suspend wait=0ms: floor drop 171.7 dB, speech loss 182.3 dB
suspend wait=50ms: floor drop 37.0 dB, speech loss 0.5 dB
suspend wait=500ms: floor drop 37.0 dB, speech loss 0.5 dB
```

`prestart` waits after creating the node and before `startRendering()`. `suspend` suspends at the
first render quantum and resumes after the wait. The probe now uses `prestart` with 500 ms:

| suppressor | noise floor drop (dB) | speech loss, noisy (dB) | speech loss, clean (dB) | × real time | latency (ms) |
|---|---|---|---|---|---|
| none | 0.0 | 0.0 | 0.0 | 25.6 | 0 |
| **rnnoise** | **37.7** | 0.5 | −0.1 | 18.9 | 20.67 |
| speex | 4.5 | 0.4 | 0.1 | 22.9 | 2.67 |
| gtcrn | 14.9 | 0.5 | −0.1 | 11.8 | 31.88 |

Raw levels on the noisy file: gaps go from −28.27 dBFS to −65.96 (RNNoise), −43.22 (GTCRN) and
−32.81 (Speex), while speech stays within 0.5 dB of −17.70. The `× real time` column includes the
500 ms wait, so it is a lower bound: every suppressor is well above 1.

Assets fetched, and the bytes the committed dist would gain for the chosen suppressor:
- `rnnoise_simd.wasm` 157,234 B, fetched on a SIMD browser;
- `rnnoise.wasm` 152,656 B, the non-SIMD fallback (`loadRnnoise` picks one at runtime; both ship);
- the worklet 64,483 B.

For reference, `speex.wasm` is 56,268 B and `gtcrn.wasm` 196,884 B.

**Verdict: RNNoise.** It has the largest floor drop by 22.8 dB, costs no more than 0.5 dB of
speech on either file, and runs faster than real time.

**T6's E2E threshold:** a noise-floor drop of at least **34.7 dB** (37.7 − 3) on `speech-noisy.wav`.

**Consequences for Plan B**
- The denoise step waits for the worklet before rendering (50 ms was enough here, 0 ms never).
  Because nothing signals readiness, it must also check its own output: an all-silent render of
  non-silent input is an error, never a saved asset.
- RNNoise delays its output by 20.67 ms (992 samples at 48 kHz). The denoised asset drops those
  samples from its head, so that swapping it under a video clip does not shift the voice against
  the lips.

**What S3 does not show**
- Only this fixture's noise was tested: synthetic noise mixed at 10 dB SNR. Babble, music and
  reverberation were not, and GTCRN may rank differently on them.
- Only headless Chromium on the WSL host: not Safari, and not a phone CPU.
- The latency was measured once, on a single half-second window of the clean file; that it stays
  constant across a file is assumed, not measured.
- The ×RT figure includes a fixed wait and so does not isolate the render cost.
