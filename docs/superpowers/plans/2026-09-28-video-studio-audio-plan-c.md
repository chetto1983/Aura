# Video Studio audio — Plan C (waveform, voice, noise reduction, ducking) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Finish sub-project 1 of the Video Studio audio work: a sound draws its waveform and a draggable volume envelope, the operator can record a voice or have a text read aloud onto a lane, noise reduction cleans a clip's or a sound's audio, and ducking lowers a sound under the speech the other sources carry — each proven by a measured export on the lab VM.

**Architecture:** The pure core (`project.ts`, `commands*.ts`, `audioLane.ts`, `volumeCurve.ts`, `videoflow*.ts`) gains only the compile side: the denoised-asset swap and a ducking factor in the one volume curve. Everything that needs a browser — decoding, peaks, RNNoise, the WebRTC VAD, the microphone — lives in separate modules (`audioDecode.ts`, `audioClean.ts`, `audioSpeech.ts`, `AudioWaveform.tsx`, `AudioRecorder.tsx`), lazy-loaded where they carry WASM, and enters the project only through `setEnvelope`, `recordAnalysis`, `setAudioProperties` and `setClipPresentation`.

**Tech Stack:** React 19, TypeScript, dnd-timeline 3.1.1, `wavesurfer.js` 8.0.1 (Envelope and Record plugins), `@sapphi-red/web-noise-suppressor` 0.4.1 (RNNoise), `@echogarden/fvad-wasm` 0.2.0 (WebRTC VAD), mediabunny 1.58.1 (Opus encode), VideoFlow 1.3.4, vitest, Playwright.

**Spec:** `docs/superpowers/specs/2026-09-27-video-studio-audio-design.md` (§Model, §Compile, §Browser-side analysis, §UI, §Tasks T4–T7), with the measurements in `spikes/video-studio-audio/FINDINGS.md` (S2 waveform/envelope, S3 noise suppressor, S4 VAD). Plans A and B shipped T0–T3.

## Global Constraints

- **The core stays pure** (spec §The whole job): `project.ts`, `commands*.ts`, `audioLane.ts`, `volumeCurve.ts`, `videoflow.ts`, `videoflow_audio.ts` never import a browser analysis module, the DOM, Web Audio or `fetch`.
- **Everything loaded is served from the Aura origin** (spec §Inventory): WASM and worklets are Vite `?url` imports, emitted into the dist. No CDN; the E2E fails any request to another origin.
- **The WASM is lazy**: `audioClean.ts` and `audioSpeech.ts` are reached only through `import()` from the control that needs them, so the editor's first paint does not pay for RNNoise or fvad.
- **Dependencies pinned to the measured versions**: `wavesurfer.js` `8.0.1`, `@sapphi-red/web-noise-suppressor` `0.4.1`, `@echogarden/fvad-wasm` `0.2.0` — exact, no caret, as `mediabunny` is.
- **Measured constants, not tuned ones**: RNNoise runs at 48 000 Hz, waits 500 ms for its worklet's WASM before an offline render, and drops its first 992 samples (S3); speech detection runs RNNoise → 16 000 Hz → fvad mode 3 on 30 ms frames → pauses under 300 ms merged, windows under 250 ms dropped (S4); `points-change` arrives 200 ms after the last envelope change (`envelope.esm.js`, `timeout(…, 200)`).
- **Ducking defaults**: −12 dB, 0.5 s softness; ranges −24…−3 dB and 0.1…2 s (`checkRanges`, `commands_audio.ts`).
- Every string in English and Italian (`resources.videoStudioAudio.ts`); every file ≤ 600 lines; a Radix slider is found through the element its label names (`getByLabel(name).getByRole('slider')`).
- Unit gates: vitest ≥ 85 % lines on every touched file; Stryker ≥ 70 % killed on `commands_audio.ts`, `audioLane.ts`, `volumeCurve.ts`, `videoflow_audio.ts` (read from the `critical-mutation` artifact). The compile's new pure logic — the denoise swap, the speech mapping, the ducking factor — goes in those files, so `stryker.config.json` does not change; the view and analysis helpers (`audioView.ts`, `speechWindows.ts`) are held by vitest's 85 % alone.
- **The decoder is the browser's**, not mediabunny's (a deviation from spec §Browser-side analysis, which names `decodeSource` via mediabunny): `decodeAudioData` on an `OfflineAudioContext` resamples to the context's rate as it decodes, so the waveform (8 kHz), RNNoise (48 kHz) and fvad (16 kHz) each get their rate without a hand-written resampler. mediabunny stays the encoder of the cleaned file.
- **A lockfile change forces a full Stryker run**: the incremental cache is keyed on `web/package-lock.json`. The last full run took 101 min 41 s of the job's 120 (f913427a3). If a task's run hits the wall, raise `timeout-minutes` with the measured figure in its comment, never concurrency (ci.yml's own rule).
- `<scratchpad>` in a command is this session's helper-script directory as WSL sees it (`/mnt/c/Users/Davide/AppData/Local/Temp/claude/d--Aura/<session>/scratchpad`): `vtf.sh` (vitest on named files), `webcheck.sh` (tsc, oxlint, lint-contract, knip, prettier), `cov.sh` (per-file line coverage), `build.sh` (the embedded dist), `waitfix.sh` (waits for the VM's image to carry a SHA), `e2e-vm.sh` (Playwright against the VM as the operator's account). Every Node-side command runs in WSL through a script file, never as a Windows `.exe`.
- E2E protocol per task (spec §Tasks): unit + `webcheck.sh` green, commit, dist rebuilt and committed, push, CI green, the VM's updater applies the image, the task's spec runs RED on the previous image first and GREEN on the new one, the exported file is measured, screenshots inspected, created assets deleted by `assetCleanup`.

## Review Focus

1. **An envelope drag over a preview that is still loading.** Each drag commits once (the plugin's 200 ms debounce), the waveform does not decode its source again (the per-session cache), and the plugin is never reset under the pointer by its own echo (`sameView`). (Task 1, `AudioWaveform.test.tsx`.)
2. **A source this browser cannot decode.** The lane still shows the sound's block with no waveform and no error; noise reduction and ducking say so on their control and leave the project untouched. (Tasks 1, 3, 4.)
3. **Noise reduction turned on, off and on again.** The cleaned asset is computed and uploaded once; the second time reuses `denoisedAssetId`. (Task 3, `Inspector_audioClean.test.tsx`.)
4. **Ducking with nothing to duck under**: every other source muted, or silent. The analysis still records `speech: []` so it is not run again, and the curve stays flat. (Task 4.)
5. **The panel closed or the selection changed while an analysis runs.** It is aborted and writes nothing; a refused microphone is a sentence, not a crash. (Tasks 2, 3, 4.)

---
### Task 1: Waveform and envelope (spec T4)

**Files:**
- Modify: `web/package.json`, `web/package-lock.json` (`wavesurfer.js` `8.0.1`)
- Modify: `web/src/videoStudio/volumeCurve.ts` (export `envelopeAt`)
- Create: `web/src/videoStudio/audioView.ts`
- Create: `web/src/videoStudio/audioDecode.ts`
- Create: `web/src/videoStudio/AudioWaveform.tsx`
- Modify: `web/src/videoStudio/Timeline_audio.tsx`, `web/src/videoStudio/Timeline.tsx`
- Modify: `web/src/styles/video-studio-audio.css`
- Modify: `web/e2e/support/videoStudio.ts` (`silentFilm` moves here), `web/e2e/video-studio-lane.spec.ts`
- Create: `web/e2e/video-studio-envelope.spec.ts`
- Test: `web/src/videoStudio/__tests__/audioView.test.ts`, `audioDecode.test.ts`, `AudioWaveform.test.tsx`, `Timeline_audio.test.tsx`

**Interfaces:**
- Consumes: `setEnvelope(project, {itemId, points})` (Plan B), `audioWindow`, `useAssetSource().assetUrl`.
- Produces: `decodeMono(url, rate, signal?) → Promise<Float32Array>`, `mixDown(buffer) → Float32Array`, `waveformSamples(url)`, `WAVEFORM_RATE` (audioDecode.ts — Tasks 3 and 4 decode through `decodeMono`); `envelopeAt(points, time)` exported from volumeCurve.ts; `silentFilm(clip, name, clips?)` in the E2E support (Tasks 2–4).

- [ ] **Step 1: Add the dependency**

Run in WSL: `cd /mnt/d/Aura/web && npm install --save-exact wavesurfer.js@8.0.1`
Expected: `package.json` gains `"wavesurfer.js": "8.0.1"`; the lockfile resolves it with its integrity hash.

- [ ] **Step 2: Write the failing pure tests**

`web/src/videoStudio/__tests__/audioView.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import { envelopeFromView, envelopeView, peaksOf, sameView } from '../audioView';

// What the waveform shows, asked directly: the peaks of the stretch the lane displays, and the
// envelope in the plugin's terms and back.

describe('peaksOf', () => {
  it('takes the loudest sample of each bucket over the asked stretch only', () => {
    // 10 samples per second: second 1 is quiet, second 2 loud, and the bucket boundary splits them.
    const samples = Float32Array.from([
      0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0.1, -0.2, 0.1, 0, 0, 0, 0, 0, 0, 0, 0.9, -0.8, 0.5, 0, 0, 0, 0,
      0, 0, 0,
    ]);
    expect(Array.from(peaksOf(samples, 10, 1, 3, 2))).toEqual([
      expect.closeTo(0.2, 6),
      expect.closeTo(0.9, 6),
    ]);
  });

  it('answers zeros past the end of the samples, and nothing for no bucket', () => {
    expect(Array.from(peaksOf(Float32Array.from([0.5]), 10, 5, 6, 3))).toEqual([0, 0, 0]);
    expect(peaksOf(Float32Array.from([0.5]), 10, 0, 1, 0)).toHaveLength(0);
  });
});

describe('envelopeView', () => {
  it('always draws both edges, at the gain the envelope has there', () => {
    expect(envelopeView(undefined, 8)).toEqual([
      { time: 0, volume: 1 },
      { time: 8, volume: 1 },
    ]);
    expect(
      envelopeView(
        [
          { time: 2, gain: 0.5 },
          { time: 6, gain: 0.5 },
        ],
        8,
      ),
    ).toEqual([
      { time: 0, volume: 0.5 },
      { time: 2, volume: 0.5 },
      { time: 6, volume: 0.5 },
      { time: 8, volume: 0.5 },
    ]);
  });

  it('shows only the points inside the stretch the lane displays', () => {
    expect(
      envelopeView(
        [
          { time: 1, gain: 0 },
          { time: 9, gain: 0 },
        ],
        4,
      ).map((point) => point.time),
    ).toEqual([0, 1, 4]);
  });
});

describe('envelopeFromView', () => {
  it('replaces the shown points and keeps the ones past the stretch', () => {
    const next = envelopeFromView(
      [
        { time: 0, volume: 1 },
        { time: 3, volume: 0.2 },
        { time: 4, volume: 1 },
      ],
      [{ time: 9, gain: 0.4 }],
      4,
    );
    expect(next).toEqual([
      { time: 0, gain: 1 },
      { time: 3, gain: 0.2 },
      { time: 4, gain: 1 },
      { time: 9, gain: 0.4 },
    ]);
  });

  it('answers no envelope at all when every point is back at full gain', () => {
    expect(
      envelopeFromView(
        [
          { time: 0, volume: 1 },
          { time: 8, volume: 1 },
        ],
        undefined,
        8,
      ),
    ).toEqual([]);
  });

  it('sorts points the plugin reports out of order', () => {
    const next = envelopeFromView(
      [
        { time: 8, volume: 1 },
        { time: 0, volume: 1 },
        { time: 4, volume: 0 },
      ],
      undefined,
      8,
    );
    expect(next.map((point) => point.time)).toEqual([0, 4, 8]);
  });
});

describe('sameView', () => {
  it('compares time and volume whatever the order and whatever ids the plugin added', () => {
    const shown = [
      { id: 'b', time: 8, volume: 1 },
      { id: 'a', time: 0, volume: 0.5 },
    ];
    expect(
      sameView(shown, [
        { time: 0, volume: 0.5 },
        { time: 8, volume: 1 },
      ]),
    ).toBe(true);
    expect(sameView(shown, [{ time: 0, volume: 0.5 }])).toBe(false);
    expect(
      sameView(shown, [
        { time: 0, volume: 0.4 },
        { time: 8, volume: 1 },
      ]),
    ).toBe(false);
  });
});
```

`web/src/videoStudio/__tests__/audioDecode.test.ts`:

```ts
import { afterEach, describe, expect, it, vi } from 'vitest';
import { decodeMono, mixDown, waveformSamples, WAVEFORM_RATE } from '../audioDecode';

// The browser's decoder, stood in for: jsdom has no Web Audio, so an OfflineAudioContext that
// answers a two-channel buffer is enough to judge the mixing, the rate asked and the cache.

function buffer(channels: number[][]): AudioBuffer {
  return {
    length: channels[0]?.length ?? 0,
    numberOfChannels: channels.length,
    getChannelData: (index: number) => Float32Array.from(channels[index] ?? []),
  } as unknown as AudioBuffer;
}

const decoded = vi.hoisted(() => ({ rates: [] as number[] }));

function stubDecoder(answer: AudioBuffer) {
  vi.stubGlobal(
    'OfflineAudioContext',
    class {
      constructor(_channels: number, _length: number, rate: number) {
        decoded.rates.push(rate);
      }
      decodeAudioData(): Promise<AudioBuffer> {
        return Promise.resolve(answer);
      }
    },
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
  decoded.rates.length = 0;
});

describe('mixDown', () => {
  it('averages the channels into one', () => {
    expect(Array.from(mixDown(buffer([[1, 0], [0, 1]])))).toEqual([0.5, 0.5]);
    expect(Array.from(mixDown(buffer([[0.25, -0.25]])))).toEqual([0.25, -0.25]);
  });
});

describe('decodeMono', () => {
  it('decodes at the rate asked and answers one channel', async () => {
    stubDecoder(buffer([[0.2, 0.4], [0.2, 0.4]]));
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response(new Uint8Array([1, 2])))));
    const samples = await decodeMono('/a.wav', 16000);
    expect(decoded.rates).toEqual([16000]);
    expect(Array.from(samples)).toEqual([expect.closeTo(0.2, 6), expect.closeTo(0.4, 6)]);
  });

  it('is loud about a sound the server would not give', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response('', { status: 404 }))));
    await expect(decodeMono('/gone.wav', 8000)).rejects.toThrow(/404/);
  });
});

describe('waveformSamples', () => {
  it('decodes a source once per session at the waveform rate', async () => {
    stubDecoder(buffer([[0.5]]));
    const fetcher = vi.fn(() => Promise.resolve(new Response(new Uint8Array([1]))));
    vi.stubGlobal('fetch', fetcher);
    await waveformSamples('/once.wav');
    await waveformSamples('/once.wav');
    expect(fetcher).toHaveBeenCalledOnce();
    expect(decoded.rates).toEqual([WAVEFORM_RATE]);
  });

  it('forgets a failed decode, so the next look tries again', async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(new Response('', { status: 500 }))
      .mockResolvedValueOnce(new Response(new Uint8Array([1])));
    vi.stubGlobal('fetch', fetcher);
    stubDecoder(buffer([[0.5]]));
    await expect(waveformSamples('/retry.wav')).rejects.toThrow(/500/);
    await expect(waveformSamples('/retry.wav')).resolves.toHaveLength(1);
  });
});
```

- [ ] **Step 3: Run them to verify they fail**

Run: `MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/vtf.sh src/videoStudio/__tests__/audioView.test.ts src/videoStudio/__tests__/audioDecode.test.ts`
Expected: FAIL — `Failed to resolve import "../audioView"` and `"../audioDecode"`.

- [ ] **Step 4: Write the pure view and the decoder**

In `volumeCurve.ts`, `envelopeAt` becomes exported (the waveform draws the edges at the gain the mix plays there): change `function envelopeAt(` to `export function envelopeAt(`.

`web/src/videoStudio/audioView.ts`:

```ts
// audioView.ts — what a sound's waveform shows: the peaks of the stretch of its source the lane
// displays, and its envelope in the Envelope plugin's terms and back. Pure, so the lane and its
// tests share one definition.

import type { EnvelopePoint } from './project';
import { envelopeAt } from './volumeCurve';

/** A point as wavesurfer's Envelope plugin holds it: seconds into the waveform, gain 0–1. */
interface ViewPoint {
  readonly time: number;
  readonly volume: number;
}

/** Closer than this, two times or two gains are the same one. */
const SAME = 1e-6;

/** The loudest sample in each of `count` equal buckets of `samples` between `from` and `to`
 *  seconds; a bucket past the end of the samples is silent. */
export function peaksOf(
  samples: Float32Array,
  rate: number,
  from: number,
  to: number,
  count: number,
): Float32Array {
  const peaks = new Float32Array(Math.max(0, count));
  const first = from * rate;
  const size = ((to - from) * rate) / Math.max(1, peaks.length);
  for (let bucket = 0; bucket < peaks.length; bucket += 1) {
    const end = Math.min(samples.length, Math.ceil(first + (bucket + 1) * size));
    let peak = 0;
    for (let index = Math.max(0, Math.floor(first + bucket * size)); index < end; index += 1) {
      peak = Math.max(peak, Math.abs(samples[index] ?? 0));
    }
    peaks[bucket] = peak;
  }
  return peaks;
}

/**
 * The envelope as the plugin draws it over `visible` source seconds. Both edges are always there:
 * without them the plugin's line starts and ends at volume 0 and draws a fade-in and a fade-out the
 * mix does not do (S2). Points past the stretch — a sound the film's end cuts short — stay hidden.
 */
export function envelopeView(
  envelope: readonly EnvelopePoint[] | undefined,
  visible: number,
): ViewPoint[] {
  const inside = (envelope ?? [])
    .filter((point) => point.time > SAME && point.time < visible - SAME)
    .map((point) => ({ time: point.time, volume: point.gain }));
  return [
    { time: 0, volume: envelopeAt(envelope, 0) },
    ...inside,
    { time: visible, volume: envelopeAt(envelope, visible) },
  ];
}

/** The item's envelope after the plugin moved its points: the shown stretch replaced, the points
 *  past it kept, and an envelope back at full gain everywhere dropped altogether. */
export function envelopeFromView(
  points: readonly ViewPoint[],
  envelope: readonly EnvelopePoint[] | undefined,
  visible: number,
): EnvelopePoint[] {
  const hidden = (envelope ?? []).filter((point) => point.time > visible + SAME);
  const next = [...points.map((point) => ({ time: point.time, gain: point.volume })), ...hidden];
  next.sort((a, b) => a.time - b.time);
  return next.every((point) => Math.abs(point.gain - 1) < SAME) ? [] : next;
}

/** Whether the plugin already shows these points, whatever their order and whatever ids it added:
 *  its own edit echoing back must not reset a drag under the pointer. */
export function sameView(shown: readonly ViewPoint[], wanted: readonly ViewPoint[]): boolean {
  const byTime = (points: readonly ViewPoint[]) => [...points].sort((a, b) => a.time - b.time);
  const a = byTime(shown);
  const b = byTime(wanted);
  return (
    a.length === b.length &&
    a.every((point, index) => {
      const other = b[index];
      return (
        other !== undefined &&
        Math.abs(point.time - other.time) < SAME &&
        Math.abs(point.volume - other.volume) < SAME
      );
    })
  );
}
```

`web/src/videoStudio/audioDecode.ts`:

```ts
// audioDecode.ts — a sound's samples in the browser. The decoding is the browser's own
// (`decodeAudioData`), which resamples to the context's rate as it decodes: the waveform wants few
// samples, the noise reduction 48 kHz and the speech detector 16 kHz, and none of them has to
// resample by hand. Browser-only: the pure core never imports this (spec §The whole job).

/** Samples per second kept for a waveform: a hundred peaks a second need no more. */
export const WAVEFORM_RATE = 8000;

/** A decoded sound's channels averaged into one. */
export function mixDown(buffer: AudioBuffer): Float32Array {
  const mono = new Float32Array(buffer.length);
  for (let channel = 0; channel < buffer.numberOfChannels; channel += 1) {
    const data = buffer.getChannelData(channel);
    for (let index = 0; index < mono.length; index += 1) {
      mono[index] = (mono[index] ?? 0) + (data[index] ?? 0) / buffer.numberOfChannels;
    }
  }
  return mono;
}

/** A sound's samples, mono, at `rate`. */
export async function decodeMono(
  url: string,
  rate: number,
  signal?: AbortSignal,
): Promise<Float32Array> {
  const response = await fetch(url, { credentials: 'same-origin', signal: signal ?? null });
  if (!response.ok) {
    throw new Error(`videoStudio: the sound could not be read (${String(response.status)})`);
  }
  const context = new OfflineAudioContext(1, 1, rate);
  return mixDown(await context.decodeAudioData(await response.arrayBuffer()));
}

const waveforms = new Map<string, Promise<Float32Array>>();

/** A source's waveform samples, decoded once per session: peaks are cheap to cut from them and
 *  would bloat the project file if saved (spec §Browser-side analysis). A failed decode is
 *  forgotten, so the next look tries again. */
export function waveformSamples(url: string): Promise<Float32Array> {
  const cached = waveforms.get(url);
  if (cached !== undefined) return cached;
  const decoding = decodeMono(url, WAVEFORM_RATE);
  waveforms.set(url, decoding);
  decoding.catch(() => {
    waveforms.delete(url);
  });
  return decoding;
}
```

- [ ] **Step 5: Run them to verify they pass**

Run the Step 3 command. Expected: PASS, 12 tests.

- [ ] **Step 6: Write the failing component test**

`web/src/videoStudio/__tests__/AudioWaveform.test.tsx`:

```tsx
import { fireEvent, render, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { AudioWaveform } from '../AudioWaveform';
import type { EnvelopePoint } from '../project';

// The waveform with wavesurfer stood in for: a fake that records what it was created with, and a
// fake Envelope plugin whose `points-change` the test fires. The rules under test are S2's.

interface FakePoint {
  readonly time: number;
  readonly volume: number;
}

const fakes = vi.hoisted(() => {
  class FakeEnvelope {
    points: FakePoint[];
    setCalls = 0;
    private listener: ((points: FakePoint[]) => void) | undefined;
    constructor(points: FakePoint[]) {
      this.points = points;
    }
    on(_event: string, listener: (points: FakePoint[]) => void) {
      this.listener = listener;
      return () => undefined;
    }
    getPoints() {
      return this.points;
    }
    setPoints(points: FakePoint[]) {
      this.points = points;
      this.setCalls += 1;
    }
    emit(points: FakePoint[]) {
      this.listener?.(points);
    }
  }
  return {
    FakeEnvelope,
    created: [] as { options: Record<string, unknown>; destroyed: boolean }[],
    envelopes: [] as InstanceType<typeof FakeEnvelope>[],
    width: 300,
  };
});

vi.mock('wavesurfer.js', () => ({
  default: {
    create: (options: Record<string, unknown>) => {
      const instance = { options, destroyed: false, destroy: () => (instance.destroyed = true) };
      fakes.created.push(instance);
      return instance;
    },
  },
}));

vi.mock('wavesurfer.js/plugins/envelope', () => ({
  default: {
    create: (options: { points: FakePoint[] }) => {
      const plugin = new fakes.FakeEnvelope(options.points);
      fakes.envelopes.push(plugin);
      return plugin;
    },
  },
}));

const decode = vi.hoisted(() => ({ fail: false }));
vi.mock('../audioDecode', () => ({
  WAVEFORM_RATE: 100,
  waveformSamples: () =>
    decode.fail
      ? Promise.reject(new Error('undecodable'))
      : Promise.resolve(new Float32Array(1000).fill(0.5)),
}));

beforeEach(() => {
  fakes.created.length = 0;
  fakes.envelopes.length = 0;
  fakes.width = 300;
  decode.fail = false;
  vi.stubGlobal(
    'ResizeObserver',
    class {
      private readonly callback: ResizeObserverCallback;
      constructor(callback: ResizeObserverCallback) {
        this.callback = callback;
      }
      observe() {
        this.callback(
          [{ contentRect: { width: fakes.width } } as ResizeObserverEntry],
          this as unknown as ResizeObserver,
        );
      }
      disconnect() {
        // Nothing to release.
      }
    },
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
});

function mount(
  props: Partial<Parameters<typeof AudioWaveform>[0]> = {},
): ReturnType<typeof render> & { onEnvelope: ReturnType<typeof vi.fn> } {
  const onEnvelope = vi.fn();
  const view = render(
    <AudioWaveform
      assetId="m"
      sourceStart={1}
      visible={4}
      envelope={undefined}
      selected={false}
      onEnvelope={onEnvelope}
      {...props}
    />,
  );
  return Object.assign(view, { onEnvelope });
}

describe('AudioWaveform', () => {
  it('draws nothing until its host has a width', async () => {
    fakes.width = 0;
    mount();
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(fakes.created).toHaveLength(0);
  });

  it('draws the peaks of the stretch the lane shows, and no envelope when not selected', async () => {
    mount();
    await waitFor(() => {
      expect(fakes.created).toHaveLength(1);
    });
    const options = fakes.created[0]?.options;
    expect(options?.duration).toBe(4);
    expect((options?.peaks as Float32Array[])[0]).toHaveLength(400);
    expect(options?.plugins).toEqual([]);
  });

  it('shows the envelope with both edges while selected, and commits what the plugin reports', async () => {
    const envelope: EnvelopePoint[] = [{ time: 2, gain: 0.5 }];
    const view = mount({ selected: true, envelope });
    await waitFor(() => {
      expect(fakes.envelopes).toHaveLength(1);
    });
    const plugin = fakes.envelopes[0];
    expect(plugin?.points.map((point) => point.time)).toEqual([0, 2, 4]);
    plugin?.emit([
      { time: 0, volume: 1 },
      { time: 2, volume: 0 },
      { time: 4, volume: 1 },
    ]);
    expect(view.onEnvelope).toHaveBeenCalledWith([
      { time: 0, gain: 1 },
      { time: 2, gain: 0 },
      { time: 4, gain: 1 },
    ]);
  });

  it('moves the drawn points for an envelope changed elsewhere, but not for its own echo', async () => {
    const view = mount({ selected: true, envelope: [{ time: 2, gain: 0.5 }] });
    await waitFor(() => {
      expect(fakes.envelopes).toHaveLength(1);
    });
    const plugin = fakes.envelopes[0];
    // Its own edit comes back as the new prop: already drawn, so left alone.
    plugin?.setPoints([
      { time: 0, volume: 0.5 },
      { time: 2, volume: 0.5 },
      { time: 4, volume: 0.5 },
    ]);
    const before = plugin?.setCalls ?? 0;
    view.rerender(
      <AudioWaveform
        assetId="m"
        sourceStart={1}
        visible={4}
        envelope={[{ time: 2, gain: 0.5 }]}
        selected
        onEnvelope={view.onEnvelope}
      />,
    );
    expect(plugin?.setCalls).toBe(before);
    // An undo brings back a different envelope: drawn anew.
    view.rerender(
      <AudioWaveform
        assetId="m"
        sourceStart={1}
        visible={4}
        envelope={undefined}
        selected
        onEnvelope={view.onEnvelope}
      />,
    );
    expect(plugin?.setCalls).toBe(before + 1);
    expect(plugin?.points).toEqual([
      { time: 0, volume: 1 },
      { time: 4, volume: 1 },
    ]);
  });

  it('keeps a press on an envelope point from starting the item drag, and lets any other through', () => {
    const outer = vi.fn();
    const { getByTestId } = render(
      // eslint-disable-next-line jsx-a11y/no-static-element-interactions -- the test's stand-in for dnd-kit
      <div onPointerDown={outer}>
        <AudioWaveform
          assetId="m"
          sourceStart={0}
          visible={4}
          envelope={undefined}
          selected
          onEnvelope={vi.fn()}
        />
      </div>,
    );
    const host = getByTestId('sound-waveform');
    const point = document.createElementNS('http://www.w3.org/2000/svg', 'ellipse');
    host.appendChild(point);
    fireEvent.pointerDown(point);
    expect(outer).not.toHaveBeenCalled();
    fireEvent.pointerDown(host);
    expect(outer).toHaveBeenCalledOnce();
  });

  it('draws no waveform for a source the browser cannot decode, and says nothing', async () => {
    decode.fail = true;
    mount();
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(fakes.created).toHaveLength(0);
  });

  it('tears the waveform down when it goes', async () => {
    const view = mount();
    await waitFor(() => {
      expect(fakes.created).toHaveLength(1);
    });
    view.unmount();
    expect(fakes.created[0]?.destroyed).toBe(true);
  });
});
```

(If oxlint has no `jsx-a11y/no-static-element-interactions` rule enabled, drop the disable comment; `webcheck.sh` reports an unused directive.)

- [ ] **Step 7: Run it to verify it fails**

Run: `MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/vtf.sh src/videoStudio/__tests__/AudioWaveform.test.tsx`
Expected: FAIL — `Failed to resolve import "../AudioWaveform"`.

- [ ] **Step 8: Write the component**

`web/src/videoStudio/AudioWaveform.tsx`:

```tsx
import { useEffect, useRef, useState, type PointerEvent, type RefObject } from 'react';
import WaveSurfer from 'wavesurfer.js';
import EnvelopePlugin from 'wavesurfer.js/plugins/envelope';
import { useAssetSource } from '../chat/artifacts/renderers/assetSourceContext';
import { waveformSamples, WAVEFORM_RATE } from './audioDecode';
import { envelopeFromView, envelopeView, peaksOf, sameView } from './audioView';
import type { EnvelopePoint } from './project';

// AudioWaveform.tsx — a sound's waveform inside its lane item and, while it is selected, its volume
// envelope: points to drag, a double-click to add one, a drag off the item to remove one. S2 set the
// rules this follows (spikes/video-studio-audio/FINDINGS.md): it is created only once its host has
// a width, because the envelope freezes its viewBox at creation; it fills the item box, not
// dnd-timeline's content box; a press on a point is kept from dnd-kit, found by the composed path
// because wavesurfer draws in a shadow root; and its strokes are pinned through ::part (CSS).

/** Peaks drawn per second of sound: finer than any zoom the lane offers shows. */
const PEAKS_PER_SECOND = 100;

/** Whether the host has been laid out yet. Under jsdom it never is — its ResizeObserver is the
 *  no-op polyfill in src/test/setup.ts, which never reports — so nothing is decoded or drawn
 *  in a test that does not stand in an observer of its own. */
function useHasWidth(host: RefObject<HTMLDivElement | null>): boolean {
  const [hasWidth, setHasWidth] = useState(false);
  useEffect(() => {
    const element = host.current;
    if (element === null || typeof ResizeObserver === 'undefined') return undefined;
    const observer = new ResizeObserver(([entry]) => {
      setHasWidth((entry?.contentRect.width ?? 0) > 0);
    });
    observer.observe(element);
    return () => {
      observer.disconnect();
    };
  }, [host]);
  return hasWidth;
}

interface AudioWaveformProps {
  readonly assetId: string;
  readonly sourceStart: number;
  /** Source seconds the lane shows: the window, through the speed, cut at the film's end. */
  readonly visible: number;
  readonly envelope: readonly EnvelopePoint[] | undefined;
  readonly selected: boolean;
  readonly onEnvelope: (points: readonly EnvelopePoint[]) => void;
}

type Envelope = ReturnType<typeof EnvelopePlugin.create>;

export function AudioWaveform({
  assetId,
  sourceStart,
  visible,
  envelope,
  selected,
  onEnvelope,
}: AudioWaveformProps) {
  const { assetUrl } = useAssetSource();
  const host = useRef<HTMLDivElement | null>(null);
  const hasWidth = useHasWidth(host);
  const [samples, setSamples] = useState<Float32Array>();
  const plugin = useRef<Envelope>(undefined);
  const view = envelopeView(envelope, visible);
  // The newest props for the plugin's listener, which lives as long as the wavesurfer it belongs to.
  const latest = useRef({ envelope, visible, onEnvelope, view });
  latest.current = { envelope, visible, onEnvelope, view };

  useEffect(() => {
    if (!hasWidth) return undefined;
    let live = true;
    waveformSamples(assetUrl(assetId)).then(
      (decoded) => {
        if (live) setSamples(decoded);
      },
      () => {
        // Undecodable here: the lane still shows the sound's block, which is all it can show.
      },
    );
    return () => {
      live = false;
    };
  }, [hasWidth, assetUrl, assetId]);

  useEffect(() => {
    const container = host.current;
    if (container === null || samples === undefined || visible <= 0) return undefined;
    const envelopePlugin = selected
      ? EnvelopePlugin.create({
          points: latest.current.view.map((point) => ({ ...point })),
          dragPointSize: 14,
        })
      : undefined;
    const surfer = WaveSurfer.create({
      container,
      height: Math.max(1, container.clientHeight),
      peaks: [
        peaksOf(
          samples,
          WAVEFORM_RATE,
          sourceStart,
          sourceStart + visible,
          Math.max(1, Math.round(visible * PEAKS_PER_SECOND)),
        ),
      ],
      duration: visible,
      interact: false,
      cursorWidth: 0,
      normalize: true,
      waveColor: getComputedStyle(container).color,
      plugins: envelopePlugin === undefined ? [] : [envelopePlugin],
    });
    envelopePlugin?.on('points-change', (points) => {
      const now = latest.current;
      now.onEnvelope(envelopeFromView(points, now.envelope, now.visible));
    });
    plugin.current = envelopePlugin;
    return () => {
      plugin.current = undefined;
      surfer.destroy();
    };
  }, [samples, sourceStart, visible, selected]);

  // An envelope changed elsewhere — undo, a split — reaches the points already drawn. The one this
  // plugin just sent back is what it already shows, so a drag is never reset under the pointer.
  const viewKey = JSON.stringify(view);
  useEffect(() => {
    const shown = plugin.current;
    const wanted = latest.current.view;
    if (shown !== undefined && !sameView(shown.getPoints(), wanted)) {
      shown.setPoints(wanted.map((point) => ({ ...point })));
    }
  }, [viewKey]);

  const keepPointsFromDrag = (event: PointerEvent<HTMLDivElement>) => {
    const onPoint = event.nativeEvent
      .composedPath()
      .some((node) => node instanceof Element && node.localName === 'ellipse');
    if (onPoint) event.stopPropagation();
  };
  return (
    // A pointer guard, not a control: the points inside are the controls, and dnd-kit must not see
    // their presses (S2).
    // eslint-disable-next-line jsx-a11y/no-static-element-interactions
    <div
      ref={host}
      data-testid="sound-waveform"
      className="video-studio-waveform"
      onPointerDown={keepPointsFromDrag}
    />
  );
}
```

(`useEffect(…, [viewKey])` reads `latest.current.view` rather than listing `view`; if oxlint's exhaustive-deps rule objects, keep the rule and depend on `viewKey` only — the key IS the value.)

- [ ] **Step 9: Run it to verify it passes**

Run the Step 7 command. Expected: PASS, 7 tests.

- [ ] **Step 10: The lane draws it — failing test**

In `Timeline_audio.test.tsx`, mock the waveform so the lane test judges only the wiring, and add a test. Add under the existing mocks:

```tsx
const waves = vi.hoisted(() => ({ props: [] as Record<string, unknown>[] }));
vi.mock('../AudioWaveform', () => ({
  AudioWaveform: (props: Record<string, unknown>) => {
    waves.props.push(props);
    return <div data-testid="sound-waveform" />;
  },
}));
```

and the test, in `describe('Timeline, on the audio lanes')`, through the file's own `mount(selectedId)` (it renders `<Timeline>` on `project()` and its `applied()` runs the one edit `onCommand` received):

```tsx
  it('draws each sound’s waveform over what the lane shows, and commits its envelope', () => {
    waves.props.length = 0;
    const { applied } = mount('bed');
    const bed = waves.props.findLast((props) => props.selected === true);
    // bed: 2 source seconds from second 1 at speed 1, all of it inside the 8 s film.
    expect(bed).toMatchObject({ assetId: 'm', sourceStart: 1, visible: 2 });
    expect(waves.props.filter((props) => props.selected === false)).not.toHaveLength(0);
    (bed?.onEnvelope as (points: { time: number; gain: number }[]) => void)([
      { time: 1, gain: 0.25 },
    ]);
    expect(soundIn(applied(), 'bed')?.envelope).toEqual([{ time: 1, gain: 0.25 }]);
  });
```

- [ ] **Step 11: Run it to verify it fails**

Run: `MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/vtf.sh src/videoStudio/__tests__/Timeline_audio.test.tsx`
Expected: FAIL — no waveform props recorded.

- [ ] **Step 12: Wire the lane**

In `Timeline_audio.tsx`:
- `AudioItemViewProps` gains `readonly assetId: string | undefined; readonly visible: number; readonly onEnvelope: (itemId: string, points: readonly EnvelopePoint[]) => void;` (import `EnvelopePoint` from `./project`, `AudioWaveform` from `./AudioWaveform`).
- Inside the item root, after the content `div` and before the two `Handle`s:

```tsx
      {assetId === undefined ? null : (
        // The item box, not dnd-timeline's content box: the waveform has to line up with time (S2).
        // It takes presses only while selected — then its points are the controls — and lets the
        // unselected sound's button take the click that selects it.
        <div
          className="video-studio-waveform-layer"
          style={{ pointerEvents: selected ? 'auto' : 'none' }}
        >
          <AudioWaveform
            assetId={assetId}
            sourceStart={item.sourceStart}
            visible={visible}
            envelope={item.envelope}
            selected={selected}
            onEnvelope={(points) => {
              onEnvelope(item.id, points);
            }}
          />
        </div>
      )}
```

- `AudioLaneItemsProps` gains `readonly onEnvelope: (itemId: string, points: readonly EnvelopePoint[]) => void;`, and each `AudioItemView` gets `assetId={sourceOf(project, item.sourceId)?.assetId}`, `visible={(span.end - span.start) * Math.abs(item.speed ?? 1)}`, `onEnvelope={onEnvelope}`.
- The file header gains one sentence: "Each sound draws its waveform and, selected, its envelope (AudioWaveform.tsx)."

In `Timeline.tsx`: `LanesProps` gains `readonly onEnvelope: (itemId: string, points: readonly EnvelopePoint[]) => void;`; `Lanes` passes it to `AudioLaneItems`; `Timeline` defines

```tsx
  function onEnvelope(itemId: string, points: readonly EnvelopePoint[]) {
    onCommand((current) => setEnvelope(current, { itemId, points }));
  }
```

and hands it to `<Lanes … onEnvelope={onEnvelope} />` (import `setEnvelope` from `./commands_audio`, `EnvelopePoint` from `./project`).

In `web/src/styles/video-studio-audio.css`, append:

```css
/* A sound's waveform fills its item box — not dnd-timeline's content box, which is padded down to
   the part inside the range and would squeeze the waveform off its time (S2). The trim handles stay
   above it. */
.video-studio-waveform-layer {
  position: absolute;
  inset: 0;
  z-index: 1;
  overflow: hidden;
  border-radius: 5px;
}

.video-studio-waveform {
  height: 100%;
  color: color-mix(in oklab, var(--video-studio-text) 45%, transparent);
}

/* The envelope freezes its viewBox at creation and stretches with the item on a zoom; pinned
   strokes keep its line and points at their size at every zoom (S2). */
.video-studio-waveform > div::part(polyline),
.video-studio-waveform > div::part(envelope-circle) {
  vector-effect: non-scaling-stroke;
}

/* The plugin paints its line and points through presentation attributes, which any CSS rule
   outranks: the envelope takes the selection's colour. */
.video-studio-waveform > div::part(polyline) {
  stroke: var(--video-studio-selection);
}

.video-studio-waveform > div::part(envelope-circle) {
  fill: var(--video-studio-selection);
  stroke: var(--video-studio-panel-strong);
}
```

The trim handles already sit above the layer: `Handle` (Timeline_items.tsx) renders with Tailwind's `z-10`.

- [ ] **Step 13: Run the lane and the whole suite**

Run: `MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/vtf.sh src/videoStudio`
Expected: PASS except the known WSL-only `videoflow_fonts.test.ts` import-attribute failure (it passes in CI).

- [ ] **Step 14: Move `silentFilm` into the shared support and write the envelope E2E**

In `web/e2e/support/videoStudio.ts`, add (moved from `video-studio-lane.spec.ts`, which then imports it):

```ts
const FRAME = { width: 320, height: 180 };

/** Muted 4 s clips of one source: the only sound in the export is the one the test puts there. */
export function silentFilm(clip: string, name: string, clips = 2) {
  return {
    id: randomUUID(),
    name,
    size: FRAME,
    fps: 30,
    sources: [
      { id: 'src-a', assetId: clip, kind: 'video', duration: 4, size: FRAME, hasAudio: true },
    ],
    video: Array.from({ length: clips }, (_, index) => ({
      id: `clip-${String(index + 1)}`,
      sourceId: 'src-a',
      duration: 4,
      sourceStart: 0,
      muted: true,
    })),
    overlays: [],
  };
}
```

(import `randomUUID` from `node:crypto` there; delete the local `silentFilm` and `FRAME` from the lane spec if nothing else there uses `FRAME`.)

`web/e2e/video-studio-envelope.spec.ts`:

```ts
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { expect, test } from './support/assetCleanup';
import { uploadAsset } from './support/assetUpload';
import { levelBetween, windowPowers } from './support/audioMeasure';
import { AUDIO_FIXTURES, exportTo, reopen, silentFilm, uploadClip } from './support/videoStudio';

// video-studio-envelope.spec.ts — a sound's waveform on its lane, and its envelope proven by the
// file: a point added in the last quarter of the music and pulled to the floor makes the export
// drop there, while its first second plays as it did.

test('a waveform on the lane, and an envelope point pulled to the floor, heard in the export', async ({
  page,
}, info) => {
  test.skip(
    info.project.name.startsWith('mobile'),
    'the envelope is shaped with a mouse; the phone has the same lane',
  );
  test.setTimeout(10 * 60_000);
  const clip = await uploadClip(page);
  const music = await uploadAsset(
    page,
    resolve(AUDIO_FIXTURES, 'music.wav'),
    'music.wav',
    'audio/wav',
    { use: 'media' },
  );
  const film = silentFilm(clip, 'envelope check');
  const editor = await reopen(
    page,
    {
      ...film,
      sources: [
        ...film.sources,
        { id: 'src-m', assetId: music, kind: 'audio', duration: 8, size: { width: 0, height: 0 } },
      ],
      audio: [
        {
          id: 'lane-a',
          items: [
            {
              id: 'bed',
              sourceId: 'src-m',
              anchor: { clipId: 'clip-1', offset: 0 },
              sourceStart: 0,
              duration: 8,
              volume: 1,
              muted: false,
            },
          ],
        },
      ],
    },
    clip,
  );
  await editor.getByRole('button', { name: 'Sound 1' }).click();
  const waveform = editor.getByTestId('sound-waveform');
  // Both edge points are drawn once the waveform is: the envelope is up.
  await expect(waveform.locator('ellipse')).toHaveCount(2, { timeout: 30_000 });
  await info.attach('envelope-waveform', { contentType: 'image/png', body: await page.screenshot() });

  // A point three quarters in, away from the trim handles over the edges, pulled to the floor.
  const box = await waveform.boundingBox();
  if (box === null) throw new Error('the waveform has no box');
  const x = box.x + box.width * 0.75;
  await page.mouse.dblclick(x, box.y + box.height / 2);
  const points = waveform.locator('ellipse');
  await expect(points).toHaveCount(3);
  // The new point is the one under the double-click, whatever order the plugin draws them in.
  const centre = (b: { x: number; width: number }) => b.x + b.width / 2;
  const boxes = (await Promise.all((await points.all()).map((one) => one.boundingBox()))).filter(
    (one) => one !== null,
  );
  const point = boxes.sort((a, b) => Math.abs(centre(a) - x) - Math.abs(centre(b) - x))[0];
  if (point === undefined) throw new Error('the new point has no box');
  await page.mouse.move(centre(point), point.y + point.height / 2);
  await page.mouse.down();
  await page.mouse.move(centre(point), box.y + box.height - 1, { steps: 8 });
  await page.mouse.up();
  // The plugin reports 200 ms after the last move (envelope.esm.js); the commit follows it.
  await page.waitForTimeout(600);
  await info.attach('envelope-shaped', { contentType: 'image/png', body: await page.screenshot() });

  const powers = await windowPowers(
    page,
    readFileSync(await exportTo(page, editor, info, 'envelope.mp4')),
  );
  const levels = { start: levelBetween(powers, 0.5, 1.5), dip: levelBetween(powers, 5.7, 6.3) };
  await info.attach('envelope-levels', {
    contentType: 'application/json',
    body: JSON.stringify(levels, null, 2),
  });
  // The music is −18.24 dBFS; the first second loses about 1 dB to the ramp, the dip far more.
  expect(levels.start).toBeGreaterThan(-21);
  expect(levels.start - levels.dip).toBeGreaterThan(10);
});
```


- [ ] **Step 15: Run the envelope E2E RED on the VM's current image**

`MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/e2e-vm.sh video-studio-envelope.spec.ts --project=chrome --reporter=line`
Expected: FAIL — `sound-waveform` has no ellipse (the running image draws no waveform).

- [ ] **Step 16: Static checks, coverage, commit, dist, push, GREEN**

`webcheck.sh` on the touched files (tsc, oxlint 0/0, lint-contract, knip, prettier); coverage ≥ 85 % lines on `audioView.ts`, `audioDecode.ts`, `AudioWaveform.tsx`, `Timeline_audio.tsx`, `Timeline.tsx` (`cov.sh`).

```bash
git add web/src/videoStudio/audioView.ts web/src/videoStudio/audioDecode.ts web/src/videoStudio/AudioWaveform.tsx web/src/videoStudio/__tests__/audioView.test.ts web/src/videoStudio/__tests__/audioDecode.test.ts web/src/videoStudio/__tests__/AudioWaveform.test.tsx web/e2e/video-studio-envelope.spec.ts
git commit -m "feat(video-studio): a sound's waveform on its lane and its volume envelope" -- web/package.json web/package-lock.json web/src/videoStudio/volumeCurve.ts web/src/videoStudio/audioView.ts web/src/videoStudio/audioDecode.ts web/src/videoStudio/AudioWaveform.tsx web/src/videoStudio/Timeline_audio.tsx web/src/videoStudio/Timeline.tsx web/src/styles/video-studio-audio.css web/src/videoStudio/__tests__/audioView.test.ts web/src/videoStudio/__tests__/audioDecode.test.ts web/src/videoStudio/__tests__/AudioWaveform.test.tsx web/src/videoStudio/__tests__/Timeline_audio.test.tsx web/e2e/support/videoStudio.ts web/e2e/video-studio-lane.spec.ts web/e2e/video-studio-envelope.spec.ts
```

`build.sh`, commit `internal/webui/dist` ("build(web): rebuild the embedded dist for the waveform"), push, CI green (a full Stryker run: the lockfile changed), `waitfix.sh <dist sha>`, then `e2e-vm.sh video-studio-envelope.spec.ts video-studio-lane.spec.ts --reporter=line,json` GREEN; read the levels and look at both screenshots (the waveform drawn inside Sound 1; the dip in the envelope line).

---
### Task 2: Text to speech and voice recording (spec T5)

**Files:**
- Modify: `web/src/chat/voice/voiceApi.ts` (`synthesizeSpeechAudio`), `web/src/chat/voice/voiceApi.test.ts`
- Create: `web/src/videoStudio/AudioRecorder.tsx`
- Create: `web/src/videoStudio/VideoStudio_audioPanel.tsx`
- Modify: `web/src/videoStudio/VideoStudio.tsx` (the panel, `addFile(file, label)`)
- Modify: `web/src/i18n/resources.videoStudioAudio.ts` (`panel.*`, en + it)
- Modify: `web/src/styles/video-studio-audio.css` (the recorder's level strip)
- Create: `web/e2e/fixtures/video-studio/audio/speech-phrase.mp3`; modify the fixtures' `README.md`
- Modify: `web/e2e/video-studio-lane.spec.ts` (`addMusic` goes through the panel)
- Create: `web/e2e/video-studio-voice.spec.ts`
- Test: `web/src/videoStudio/__tests__/AudioRecorder.test.tsx`, `VideoStudio_audioPanel.test.tsx`, `VideoStudio_audio.test.tsx`

**Interfaces:**
- Consumes: `silentFilm(clip, name, clips?)` (Task 1, e2e support); `useVoiceCapabilities()`; `addFile` in `VideoStudio.tsx`.
- Produces: `synthesizeSpeechAudio(text, signal?) → Promise<{ blob: Blob; truncated: boolean }>`; `AudioPanel`; `AudioRecorder`. Nothing later tasks import.

- [ ] **Step 1: The speech as bytes — failing test**

In `voiceApi.test.ts`, import `synthesizeSpeechAudio` beside `synthesizeSpeech` and add to `describe('synthesizeSpeech')`:

```ts
  it('answers the audio itself for a caller that stores it, with the truncation flag', async () => {
    const fetchMock = stubFetch(
      new Response(new Uint8Array([1, 2, 3]), {
        headers: { 'Content-Type': 'audio/mpeg', 'X-Aura-TTS-Truncated': 'true' },
      }),
    );
    const controller = new AbortController();
    const spoken = await synthesizeSpeechAudio('buongiorno', controller.signal);
    expect(spoken.truncated).toBe(true);
    expect(spoken.blob.type).toBe('audio/mpeg');
    expect(spoken.blob.size).toBe(3);
    expect(fetchMock.mock.calls[0]?.[1]?.signal).toBe(controller.signal);
  });
```

Run: `MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/vtf.sh src/chat/voice/voiceApi.test.ts`
Expected: FAIL — `synthesizeSpeechAudio is not a function`.

- [ ] **Step 2: Split the call**

In `voiceApi.ts`, replace `synthesizeSpeech` with:

```ts
interface SpokenAudio {
  /** The audio/mpeg body. */
  readonly blob: Blob;
  /** True when the backend capped the input text (the D-05 "too long" hint). */
  readonly truncated: boolean;
}

/** POST text to /api/tts and answer the audio/mpeg body: the Video Studio stores it as a sound. */
export async function synthesizeSpeechAudio(
  text: string,
  signal?: AbortSignal,
): Promise<SpokenAudio> {
  const res = await fetch(TTS_ROUTE, {
    method: 'POST',
    headers: { Accept: 'audio/mpeg', 'Content-Type': 'application/json' },
    credentials: 'same-origin',
    body: JSON.stringify({ text }),
    signal: signal ?? null,
  });
  if (!res.ok) throw new Error(`tts failed: ${String(res.status)}`);
  return { blob: await res.blob(), truncated: res.headers.get(TRUNCATED_HEADER) === 'true' };
}

/** POST text to /api/tts and wrap the audio/mpeg body in an object URL. */
export async function synthesizeSpeech(text: string): Promise<SynthesizedSpeech> {
  const { blob, truncated } = await synthesizeSpeechAudio(text);
  return { url: URL.createObjectURL(blob), truncated };
}
```

and add one line to the file header: "The Video Studio's Text to speech takes the bytes themselves (`synthesizeSpeechAudio`) and stores them as a sound." Run the Step 1 command. Expected: PASS, every `voiceApi` test (the existing `synthesizeSpeech` tests now run through the split and still pass).

- [ ] **Step 3: The strings**

In `resources.videoStudioAudio.ts`, add to `videoStudioAudioEn`:

```ts
  panel: {
    title: 'Add a sound',
    description: 'Upload a sound, record your voice, or have a text read aloud.',
    upload: 'Upload audio',
    record: 'Record voice',
    stop: 'Stop and add',
    recording: 'Recording…',
    recordingLabel: 'Recording {{time}}',
    noMicrophone: 'No microphone: allow this page to use it, then try again.',
    speech: 'Text to read aloud',
    speak: 'Read it aloud',
    speaking: 'Reading it aloud…',
    speechFailed: 'The voice could not read that text: {{reason}}',
    truncated:
      'Added — but the text is longer than the voice reads at once, so the sound holds only its first part.',
  },
```

and to `videoStudioAudioIt`:

```ts
  panel: {
    title: 'Aggiungi un suono',
    description: 'Carica un suono, registra la tua voce o fai leggere un testo ad alta voce.',
    upload: 'Carica audio',
    record: 'Registra voce',
    stop: 'Ferma e aggiungi',
    recording: 'Registrazione…',
    recordingLabel: 'Registrazione {{time}}',
    noMicrophone: 'Nessun microfono: consenti a questa pagina di usarlo, poi riprova.',
    speech: 'Testo da leggere ad alta voce',
    speak: 'Leggilo ad alta voce',
    speaking: 'Lettura in corso…',
    speechFailed: 'La voce non è riuscita a leggere quel testo: {{reason}}',
    truncated:
      'Aggiunto, ma il testo è più lungo di quanto la voce legga in una volta: il suono ne contiene solo la prima parte.',
  },
```

- [ ] **Step 4: The recorder — failing test**

`web/src/videoStudio/__tests__/AudioRecorder.test.tsx`:

```tsx
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { AudioRecorder } from '../AudioRecorder';

// The recorder with wavesurfer's Record plugin stood in for: the fake answers startRecording as the
// microphone would (or refuses it), and hands its blob to `record-end` as the plugin does — on
// stopRecording, and also on destroy, which is the plugin's own teardown (record.esm.js `destroy`).

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

const fake = vi.hoisted(() => ({
  refuse: false,
  blobType: 'audio/webm;codecs=opus',
  plugin: undefined as
    | {
        started: number;
        stopped: number;
        emit: (blob: Blob) => void;
      }
    | undefined,
}));

vi.mock('wavesurfer.js/plugins/record', () => ({
  default: {
    create: () => {
      let listener: ((blob: Blob) => void) | undefined;
      const plugin = {
        started: 0,
        stopped: 0,
        on: (_event: string, callback: (blob: Blob) => void) => {
          listener = callback;
          return () => undefined;
        },
        startRecording: () => {
          plugin.started += 1;
          return fake.refuse
            ? Promise.reject(new Error('Error accessing the microphone: denied'))
            : Promise.resolve();
        },
        stopRecording: () => {
          plugin.stopped += 1;
          plugin.emit(new Blob(['voice'], { type: fake.blobType }));
        },
        emit: (blob: Blob) => listener?.(blob),
      };
      fake.plugin = plugin;
      return plugin;
    },
  },
}));

vi.mock('wavesurfer.js', () => ({
  default: {
    create: () => ({
      // The plugin's teardown hands over what it had, as the real one does.
      destroy: () => fake.plugin?.emit(new Blob(['late'], { type: fake.blobType })),
    }),
  },
}));

beforeEach(() => {
  fake.refuse = false;
  fake.blobType = 'audio/webm;codecs=opus';
  fake.plugin = undefined;
});

describe('AudioRecorder', () => {
  it('records, then hands over a file typed by its container alone', async () => {
    const onRecorded = vi.fn();
    render(<AudioRecorder onRecorded={onRecorded} />);
    fireEvent.click(screen.getByRole('button', { name: 'videoStudio.audio.panel.record' }));
    const stop = await screen.findByRole('button', { name: 'videoStudio.audio.panel.stop' });
    expect(screen.getByRole('status').textContent).toBe('videoStudio.audio.panel.recording');
    fireEvent.click(stop);
    expect(fake.plugin?.stopped).toBe(1);
    const file = onRecorded.mock.calls[0]?.[0] as File;
    expect(file.name).toBe('recording.webm');
    expect(file.type).toBe('audio/webm');
    expect(screen.getByRole('button', { name: 'videoStudio.audio.panel.record' })).toBeTruthy();
  });

  it('says a refused microphone in a sentence and records nothing', async () => {
    fake.refuse = true;
    const onRecorded = vi.fn();
    render(<AudioRecorder onRecorded={onRecorded} />);
    fireEvent.click(screen.getByRole('button', { name: 'videoStudio.audio.panel.record' }));
    expect((await screen.findByRole('alert')).textContent).toBe(
      'videoStudio.audio.panel.noMicrophone',
    );
    expect(onRecorded).not.toHaveBeenCalled();
  });

  it('drops the recording the plugin hands over when the panel closes mid-way', async () => {
    const onRecorded = vi.fn();
    const view = render(<AudioRecorder onRecorded={onRecorded} />);
    fireEvent.click(screen.getByRole('button', { name: 'videoStudio.audio.panel.record' }));
    await screen.findByRole('button', { name: 'videoStudio.audio.panel.stop' });
    view.unmount();
    expect(onRecorded).not.toHaveBeenCalled();
  });

  it('names the file after an ogg container too', async () => {
    fake.blobType = 'audio/ogg';
    const onRecorded = vi.fn();
    render(<AudioRecorder onRecorded={onRecorded} />);
    fireEvent.click(screen.getByRole('button', { name: 'videoStudio.audio.panel.record' }));
    fireEvent.click(await screen.findByRole('button', { name: 'videoStudio.audio.panel.stop' }));
    await waitFor(() => {
      expect((onRecorded.mock.calls[0]?.[0] as File | undefined)?.name).toBe('recording.ogg');
    });
    await act(() => Promise.resolve());
  });
});
```

Run: `MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/vtf.sh src/videoStudio/__tests__/AudioRecorder.test.tsx`
Expected: FAIL — `Failed to resolve import "../AudioRecorder"`.

- [ ] **Step 5: Write the recorder**

`web/src/videoStudio/AudioRecorder.tsx`:

```tsx
import { Mic, Square } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import WaveSurfer from 'wavesurfer.js';
import RecordPlugin from 'wavesurfer.js/plugins/record';
import { Button } from '@/components/ui/button';

// AudioRecorder.tsx — a voice recorded from the microphone, with wavesurfer's Record plugin drawing
// the live level while it runs. The plugin picks the container MediaRecorder offers (webm/Opus in
// Chromium); the file is typed by the container alone, because the audio picker's list and the
// server's name containers, not codecs.

type Phase = 'idle' | 'starting' | 'recording' | 'refused';

type Recorder = ReturnType<typeof RecordPlugin.create>;

export function AudioRecorder({ onRecorded }: { readonly onRecorded: (file: File) => void }) {
  const { t } = useTranslation();
  const host = useRef<HTMLDivElement>(null);
  const recorder = useRef<Recorder>(undefined);
  const [phase, setPhase] = useState<Phase>('idle');
  const latest = useRef(onRecorded);
  latest.current = onRecorded;

  useEffect(() => {
    const container = host.current;
    if (container === null) return undefined;
    let live = true;
    const record = RecordPlugin.create({ scrollingWaveform: true, renderRecordedAudio: false });
    const surfer = WaveSurfer.create({
      container,
      height: 40,
      waveColor: getComputedStyle(container).color,
      plugins: [record],
    });
    record.on('record-end', (blob) => {
      // Closing the panel mid-recording destroys the plugin, and its teardown still hands over what
      // it had: a recording nobody finished is not a sound anybody asked for.
      if (!live) return;
      const type = blob.type.split(';')[0] || 'audio/webm';
      latest.current(new File([blob], `recording.${type.split('/')[1] ?? 'webm'}`, { type }));
    });
    recorder.current = record;
    return () => {
      live = false;
      recorder.current = undefined;
      surfer.destroy();
    };
  }, []);

  async function start() {
    const record = recorder.current;
    if (record === undefined) return;
    setPhase('starting');
    try {
      await record.startRecording();
      setPhase('recording');
    } catch {
      setPhase('refused');
    }
  }

  function stop() {
    setPhase('idle');
    recorder.current?.stopRecording();
  }

  return (
    <div className="video-studio-recorder">
      <div ref={host} className="video-studio-recorder-level" aria-hidden="true" />
      {phase === 'recording' ? (
        <>
          <p role="status" className="text-xs text-text-muted">
            {t('videoStudio.audio.panel.recording')}
          </p>
          <Button type="button" size="sm" onClick={stop}>
            <Square aria-hidden="true" />
            {t('videoStudio.audio.panel.stop')}
          </Button>
        </>
      ) : (
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={phase === 'starting'}
          onClick={() => void start()}
        >
          <Mic aria-hidden="true" />
          {t('videoStudio.audio.panel.record')}
        </Button>
      )}
      {phase === 'refused' ? (
        <p role="alert" className="text-xs text-danger">
          {t('videoStudio.audio.panel.noMicrophone')}
        </p>
      ) : null}
    </div>
  );
}
```

Run the Step 4 command. Expected: PASS, 4 tests.

- [ ] **Step 6: The panel — failing test**

`web/src/videoStudio/__tests__/VideoStudio_audioPanel.test.tsx`:

```tsx
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { AudioPanel } from '../VideoStudio_audioPanel';

// The Add audio panel with its three doors. The recorder and the voice are stood in for; what is
// judged is what reaches the workspace's `onSound`, and when the panel closes.

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, unknown>) =>
      values === undefined ? key : `${key} ${Object.values(values).join(' ')}`,
    i18n: { language: 'en' },
  }),
}));

const voice = vi.hoisted(() => ({
  tts: true,
  speak: vi.fn<(text: string, signal?: AbortSignal) => Promise<{ blob: Blob; truncated: boolean }>>(),
}));
vi.mock('../../chat/voice/useVoiceCapabilities', () => ({
  useVoiceCapabilities: () => ({ tts: voice.tts, stt: false }),
}));
vi.mock('../../chat/voice/voiceApi', () => ({
  synthesizeSpeechAudio: (text: string, signal?: AbortSignal) => voice.speak(text, signal),
}));
vi.mock('../AudioRecorder', () => ({
  AudioRecorder: ({ onRecorded }: { onRecorded: (file: File) => void }) => (
    <button
      type="button"
      onClick={() => {
        onRecorded(new File(['v'], 'recording.webm', { type: 'audio/webm' }));
      }}
    >
      fake recorder
    </button>
  ),
}));

function mount() {
  const onOpenChange = vi.fn();
  const onUpload = vi.fn();
  const onSound = vi.fn(() => Promise.resolve());
  const view = render(
    <AudioPanel open onOpenChange={onOpenChange} onUpload={onUpload} onSound={onSound} />,
  );
  return { ...view, onOpenChange, onUpload, onSound };
}

function speak(text: string) {
  fireEvent.change(screen.getByLabelText('videoStudio.audio.panel.speech'), {
    target: { value: text },
  });
  fireEvent.click(screen.getByRole('button', { name: 'videoStudio.audio.panel.speak' }));
}

beforeEach(() => {
  voice.tts = true;
  voice.speak.mockReset();
  voice.speak.mockResolvedValue({
    blob: new Blob(['mp3'], { type: 'audio/mpeg' }),
    truncated: false,
  });
});

describe('AudioPanel', () => {
  it('closes and opens the file picker for Upload audio', () => {
    const { onOpenChange, onUpload } = mount();
    fireEvent.click(screen.getByRole('button', { name: 'videoStudio.audio.panel.upload' }));
    expect(onOpenChange).toHaveBeenCalledWith(false);
    expect(onUpload).toHaveBeenCalledOnce();
  });

  it('puts a text read aloud on a lane, named after the text, and closes', async () => {
    const { onSound, onOpenChange } = mount();
    speak('  The river runs past the old mill.  ');
    await waitFor(() => {
      expect(onSound).toHaveBeenCalledOnce();
    });
    expect(voice.speak.mock.calls[0]?.[0]).toBe('The river runs past the old mill.');
    const [file, label] = onSound.mock.calls[0] as unknown as [File, string];
    expect(file.name).toBe('speech.mp3');
    expect(file.type).toBe('audio/mpeg');
    expect(label).toBe('The river runs past the old mill.');
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it('names a long text by its first forty characters', async () => {
    const { onSound } = mount();
    speak('Nobody expected the concert to start so early, not even the band.');
    await waitFor(() => {
      expect(onSound).toHaveBeenCalledOnce();
    });
    expect(onSound.mock.calls[0]?.[1]).toBe('Nobody expected the concert to start so…');
  });

  it('adds a cut text and stays open to say it was cut', async () => {
    voice.speak.mockResolvedValue({ blob: new Blob(['mp3'], { type: 'audio/mpeg' }), truncated: true });
    const { onSound, onOpenChange } = mount();
    speak('A very long text.');
    expect((await screen.findByRole('status')).textContent).toBe('videoStudio.audio.panel.truncated');
    expect(onSound).toHaveBeenCalledOnce();
    expect(onOpenChange).not.toHaveBeenCalled();
  });

  it('says why the voice failed and adds nothing', async () => {
    voice.speak.mockRejectedValue(new Error('tts failed: 503'));
    const { onSound } = mount();
    speak('Hello.');
    expect((await screen.findByRole('alert')).textContent).toBe(
      'videoStudio.audio.panel.speechFailed tts failed: 503',
    );
    expect(onSound).not.toHaveBeenCalled();
  });

  it('offers no text field when this Aura has no voice', () => {
    voice.tts = false;
    mount();
    expect(screen.queryByLabelText('videoStudio.audio.panel.speech')).toBeNull();
    expect(screen.getByRole('button', { name: 'videoStudio.audio.panel.upload' })).toBeTruthy();
  });

  it('does not read an empty text', () => {
    mount();
    expect(
      screen.getByRole('button', { name: 'videoStudio.audio.panel.speak' }).hasAttribute('disabled'),
    ).toBe(true);
  });

  it('aborts the reading and adds nothing when the panel closes under it', async () => {
    let signal: AbortSignal | undefined;
    voice.speak.mockImplementation((_text, given) => {
      signal = given;
      return new Promise(() => undefined);
    });
    const { onSound, rerender, onOpenChange, onUpload } = mount();
    speak('Hello.');
    rerender(
      <AudioPanel open={false} onOpenChange={onOpenChange} onUpload={onUpload} onSound={onSound} />,
    );
    await waitFor(() => {
      expect(signal?.aborted).toBe(true);
    });
    expect(onSound).not.toHaveBeenCalled();
  });

  it('puts a recording on a lane, named after the hour it was made', () => {
    const { onSound, onOpenChange } = mount();
    fireEvent.click(screen.getByRole('button', { name: 'fake recorder' }));
    const [file, label] = onSound.mock.calls[0] as unknown as [File, string];
    expect(file.name).toBe('recording.webm');
    expect(label).toMatch(/^videoStudio\.audio\.panel\.recordingLabel \d{2}:\d{2}/);
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });
});
```

Run: `MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/vtf.sh src/videoStudio/__tests__/VideoStudio_audioPanel.test.tsx`
Expected: FAIL — `Failed to resolve import "../VideoStudio_audioPanel"`.

- [ ] **Step 7: Write the panel**

`web/src/videoStudio/VideoStudio_audioPanel.tsx`:

```tsx
import { Upload } from 'lucide-react';
import { useEffect, useId, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useVoiceCapabilities } from '../chat/voice/useVoiceCapabilities';
import { synthesizeSpeechAudio } from '../chat/voice/voiceApi';
import { AudioRecorder } from './AudioRecorder';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Textarea } from '@/components/ui/textarea';

// VideoStudio_audioPanel.tsx — the rail's Add audio: a sound from a file, from the microphone, or
// read aloud from a text by Aura's own voice (spec §UI). Whatever its origin, a sound enters the
// project through the workspace's one door, `addFile`, so it is probed, uploaded and placed at the
// playhead exactly as a picked file is. Text to speech is offered only where /api/voice/capabilities
// says a voice is configured; the probe runs when the panel opens, not with the editor.

/** How much of a text names the sound it became. */
const LABEL_CHARS = 40;

type Reading =
  | { readonly state: 'idle' }
  | { readonly state: 'reading' }
  | { readonly state: 'cut' }
  | { readonly state: 'failed'; readonly reason: string };

interface SoundDoorProps {
  readonly onSound: (file: File, label: string) => void;
  readonly onAdded: () => void;
}

function SpeechDoor({ onSound, onAdded }: SoundDoorProps) {
  const { t } = useTranslation();
  const { tts } = useVoiceCapabilities();
  const id = useId();
  const [text, setText] = useState('');
  const [reading, setReading] = useState<Reading>({ state: 'idle' });
  // Aborted when the panel closes: DialogContent unmounts, and a reading nobody waits for adds
  // nothing.
  const running = useRef<AbortController>(undefined);
  useEffect(
    () => () => {
      running.current?.abort();
    },
    [],
  );
  if (!tts) return null;

  async function speak() {
    const words = text.trim();
    const controller = new AbortController();
    running.current = controller;
    setReading({ state: 'reading' });
    try {
      const { blob, truncated } = await synthesizeSpeechAudio(words, controller.signal);
      if (controller.signal.aborted) return;
      const file = new File([blob], 'speech.mp3', { type: blob.type || 'audio/mpeg' });
      onSound(file, words.length > LABEL_CHARS ? `${words.slice(0, LABEL_CHARS)}…` : words);
      if (truncated) {
        setReading({ state: 'cut' });
        return;
      }
      setReading({ state: 'idle' });
      onAdded();
    } catch (error) {
      if (controller.signal.aborted) return;
      setReading({
        state: 'failed',
        reason: error instanceof Error ? error.message : String(error),
      });
    }
  }

  return (
    <div className="grid gap-2">
      <label htmlFor={id} className="text-xs font-medium">
        {t('videoStudio.audio.panel.speech')}
      </label>
      <Textarea
        id={id}
        rows={3}
        value={text}
        onChange={(event) => {
          setText(event.target.value);
        }}
      />
      <Button
        type="button"
        variant="outline"
        size="sm"
        disabled={text.trim() === '' || reading.state === 'reading'}
        onClick={() => void speak()}
      >
        {t(
          reading.state === 'reading'
            ? 'videoStudio.audio.panel.speaking'
            : 'videoStudio.audio.panel.speak',
        )}
      </Button>
      {reading.state === 'cut' ? (
        <p role="status" className="text-xs text-text-muted">
          {t('videoStudio.audio.panel.truncated')}
        </p>
      ) : null}
      {reading.state === 'failed' ? (
        <p role="alert" className="text-xs text-danger">
          {t('videoStudio.audio.panel.speechFailed', { reason: reading.reason })}
        </p>
      ) : null}
    </div>
  );
}

interface AudioPanelProps {
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly onUpload: () => void;
  readonly onSound: (file: File, label: string) => Promise<void>;
}

export function AudioPanel({ open, onOpenChange, onUpload, onSound }: AudioPanelProps) {
  const { t, i18n } = useTranslation();
  const close = () => {
    onOpenChange(false);
  };
  const add = (file: File, label: string) => {
    void onSound(file, label);
  };
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="video-studio-audio-panel">
        <DialogHeader>
          <DialogTitle>{t('videoStudio.audio.panel.title')}</DialogTitle>
          <DialogDescription>{t('videoStudio.audio.panel.description')}</DialogDescription>
        </DialogHeader>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() => {
            close();
            onUpload();
          }}
        >
          <Upload aria-hidden="true" />
          {t('videoStudio.audio.panel.upload')}
        </Button>
        <AudioRecorder
          onRecorded={(file) => {
            const time = new Date().toLocaleTimeString(i18n.language, {
              hour: '2-digit',
              minute: '2-digit',
            });
            add(file, t('videoStudio.audio.panel.recordingLabel', { time }));
            close();
          }}
        />
        <SpeechDoor onSound={add} onAdded={close} />
      </DialogContent>
    </Dialog>
  );
}
```

(`toLocaleTimeString(…, { hour: '2-digit', minute: '2-digit' })` gives `14:03` in en and it alike, which is what the test's `\d{2}:\d{2}` reads; if the WSL ICU answers `2:03 PM` for `en`, the test pins `i18n.language` to `'it'` instead and the ruling goes in the ledger.)

Run the Step 6 command. Expected: PASS, 9 tests.

- [ ] **Step 8: The workspace opens it — failing test**

In `VideoStudio_audio.test.tsx`, mock the voice (add beside the other mocks):

```tsx
const speech = vi.hoisted(() => ({
  audio: vi.fn(() =>
    Promise.resolve({ blob: new Blob(['mp3'], { type: 'audio/mpeg' }), truncated: false }),
  ),
}));
vi.mock('../../chat/voice/voiceApi', () => ({ synthesizeSpeechAudio: speech.audio }));
vi.mock('../../chat/voice/useVoiceCapabilities', () => ({
  useVoiceCapabilities: () => ({ tts: true, stt: false }),
}));
// jsdom has no canvas and no microphone: the recorder is AudioRecorder.test.tsx's to judge.
vi.mock('../AudioRecorder', () => ({ AudioRecorder: () => null }));
```

replace the test `offers Add audio on the rail, which opens the audio picker` with:

```tsx
  it('opens the audio panel from the rail, whose Upload audio opens the picker', async () => {
    mount(film());
    const picker = await screen.findByLabelText(i18n.t('videoStudio.audio.pick'));
    const click = vi.spyOn(picker, 'click');
    // The first clip is selected on open, so the phone bar shows its tools, not its add actions:
    // the one Add audio on screen is the rail's.
    fireEvent.click(screen.getByRole('button', { name: i18n.t('videoStudio.audio.add') }));
    const panel = await screen.findByRole('dialog', { name: i18n.t('videoStudio.audio.panel.title') });
    fireEvent.click(within(panel).getByRole('button', { name: i18n.t('videoStudio.audio.panel.upload') }));
    expect(click).toHaveBeenCalledOnce();
    expect(screen.queryByRole('dialog', { name: i18n.t('videoStudio.audio.panel.title') })).toBeNull();
  });

  it('puts a text read aloud at the playhead, named after the text', async () => {
    mount(film());
    fireEvent.click(await screen.findByRole('button', { name: i18n.t('videoStudio.audio.add') }));
    const panel = await screen.findByRole('dialog', { name: i18n.t('videoStudio.audio.panel.title') });
    fireEvent.change(within(panel).getByLabelText(i18n.t('videoStudio.audio.panel.speech')), {
      target: { value: 'The river runs.' },
    });
    fireEvent.click(within(panel).getByRole('button', { name: i18n.t('videoStudio.audio.panel.speak') }));
    expect((await sound(1)).textContent).toContain('The river runs.');
    expect(assets.presignAsset).toHaveBeenCalledWith(
      expect.objectContaining({ file_name: 'speech.mp3', mime_type: 'audio/mpeg' }),
    );
  });
```

(import `within` from `@testing-library/react`.) Run: `MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/vtf.sh src/videoStudio/__tests__/VideoStudio_audio.test.tsx`
Expected: FAIL — no dialog named "Add a sound".

- [ ] **Step 9: Wire the workspace**

In `VideoStudio.tsx`:
- import `AudioPanel` from `./VideoStudio_audioPanel`;
- state `const [audioPanel, setAudioPanel] = useState(false);` beside `mobileInspectorOpen`;
- `async function addFile(file: File, label = file.name) {` with `const placement = { time: playhead, label };` — the rest unchanged;
- both `onAddAudio={() => audioInput.current?.click()}` (the rail's and the phone bar's) become `onAddAudio={() => { setAudioPanel(true); }}`;
- beside `<ConfirmDialog …/>`:

```tsx
        <AudioPanel
          open={audioPanel}
          onOpenChange={setAudioPanel}
          onUpload={() => audioInput.current?.click()}
          onSound={addFile}
        />
```

In `video-studio-audio.css`, append:

```css
/* The recorder's live level: a strip the Record plugin scrolls while the microphone is open. */
.video-studio-recorder {
  display: grid;
  gap: 8px;
}

.video-studio-recorder-level {
  min-height: 40px;
  border-radius: 6px;
  background: color-mix(in oklab, var(--video-studio-accent) 10%, transparent);
  color: var(--video-studio-accent);
}
```

Run the Step 8 command, then the whole folder: `MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/vtf.sh src/videoStudio src/chat/voice`
Expected: PASS (except the known WSL-only `videoflow_fonts.test.ts`).

- [ ] **Step 10: The fixture the CI stack speaks with**

The CI stack runs no voice (voice.spec.ts header), so the E2E answers `/api/tts` there with the bytes the lab VM's own TTS gave for the first fixture phrase — `spikes/video-studio-audio/out/phrase-1.bin`, an MP3 (`ID3` header) that `tts-phrases.sh` saved verbatim, git-ignored in the spike:

```bash
cp spikes/video-studio-audio/out/phrase-1.bin web/e2e/fixtures/video-studio/audio/speech-phrase.mp3
```

and add the row to the fixtures' README table:

```markdown
| `speech-phrase.mp3` | "The river runs past the old mill every morning.", as `/api/tts` answered it | The lab VM's own `POST /api/tts` (`tts-phrases.sh`, phrase 1), saved byte for byte: the E2E serves it where no voice is configured |
```

- [ ] **Step 11: The lane spec's door moves, and the voice E2E**

In `video-studio-lane.spec.ts`, `addMusic` opens the panel first:

```ts
async function addMusic(page: Page, editor: Locator): Promise<void> {
  const chooser = page.waitForEvent('filechooser');
  await pressAddAction(editor, 'Add audio');
  await page.getByRole('dialog', { name: 'Add a sound' }).getByRole('button', { name: 'Upload audio' }).click();
  await (await chooser).setFiles(resolve(AUDIO_FIXTURES, 'music.wav'));
  const sound = editor.getByRole('button', { name: 'Sound 1' });
  await expect(sound).toBeVisible({ timeout: 60_000 });
  await expect(sound).toContainText('music.wav');
}
```

`web/e2e/video-studio-voice.spec.ts`:

```ts
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import type { Page } from '@playwright/test';
import { expect, test } from './support/assetCleanup';
import { levelBetween, windowPowers } from './support/audioMeasure';
import {
  AUDIO_FIXTURES,
  exportTo,
  pressAddAction,
  reopen,
  silentFilm,
  uploadClip,
} from './support/videoStudio';

// video-studio-voice.spec.ts — the two sounds the operator makes instead of picking: a text read
// aloud by Aura's voice, and a voice recorded through the microphone. Chromium's fake microphone
// plays speech.wav (1 s of silence, then a phrase until 3.66 s), so the recording is proven by the
// file: speech where the phrase is, and the silence before it still silent — the fake device's own
// beep, which it plays when it cannot read the file, would fill that second.

test.use({
  launchOptions: {
    args: [
      '--use-fake-device-for-media-stream',
      '--use-fake-ui-for-media-stream',
      `--use-file-for-fake-audio-capture=${resolve(AUDIO_FIXTURES, 'speech.wav')}`,
    ],
  },
});

/** Where this Aura has no voice (CI), /api/tts answers with what the lab VM's voice said. */
async function voiceOrStandIn(page: Page, info: { annotations: { type: string; description: string }[] }) {
  const caps = await page.evaluate(async () => {
    const answer = await fetch('/api/voice/capabilities', { credentials: 'same-origin' });
    return (await answer.json()) as { tts?: boolean; stt?: boolean };
  });
  if (caps.tts === true) return;
  info.annotations.push({
    type: 'voice',
    description: 'no TTS configured here: /api/tts answered with speech-phrase.mp3',
  });
  await page.route('**/api/voice/capabilities', (route) =>
    route.fulfill({ json: { tts: true, stt: caps.stt === true } }),
  );
  await page.route('**/api/tts', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'audio/mpeg',
      body: readFileSync(resolve(AUDIO_FIXTURES, 'speech-phrase.mp3')),
    }),
  );
}

test('a text read aloud lands at the playhead and speaks in the export', async ({ page }, info) => {
  test.skip(info.project.name.startsWith('mobile'), 'the phone bar opens the same panel');
  test.setTimeout(10 * 60_000);
  const clip = await uploadClip(page);
  await voiceOrStandIn(page, info);
  const editor = await reopen(page, silentFilm(clip, 'speech check'), clip);
  await pressAddAction(editor, 'Add audio');
  const panel = page.getByRole('dialog', { name: 'Add a sound' });
  await panel.getByLabel('Text to read aloud').fill('The river runs past the old mill every morning.');
  await panel.getByRole('button', { name: 'Read it aloud' }).click();
  const sound = editor.getByRole('button', { name: 'Sound 1' });
  await expect(sound).toBeVisible({ timeout: 120_000 });
  await expect(sound).toContainText('The river runs past the old mill');
  await expect(panel).toHaveCount(0);
  await info.attach('speech-lane', { contentType: 'image/png', body: await page.screenshot() });

  const powers = await windowPowers(page, readFileSync(await exportTo(page, editor, info, 'speech.mp4')));
  const levels = { speech: levelBetween(powers, 0.3, 2.3) };
  await info.attach('speech-levels', { contentType: 'application/json', body: JSON.stringify(levels, null, 2) });
  // The film is two muted clips: any sound in its first seconds is the voice's.
  expect(levels.speech).toBeGreaterThan(-40);
});

test('a voice recorded through the microphone lands on a lane and in the export', async ({ page }, info) => {
  test.skip(info.project.name.startsWith('mobile'), 'the phone bar opens the same panel');
  test.setTimeout(10 * 60_000);
  const clip = await uploadClip(page);
  const editor = await reopen(page, silentFilm(clip, 'recording check'), clip);
  await pressAddAction(editor, 'Add audio');
  const panel = page.getByRole('dialog', { name: 'Add a sound' });
  await panel.getByRole('button', { name: 'Record voice' }).click();
  const stop = panel.getByRole('button', { name: 'Stop and add' });
  await expect(stop).toBeVisible();
  // Four seconds of the fake microphone: speech.wav's lead silence and its first phrase.
  await page.waitForTimeout(4_000);
  await info.attach('recording-live', { contentType: 'image/png', body: await page.screenshot() });
  await stop.click();
  const sound = editor.getByRole('button', { name: 'Sound 1' });
  await expect(sound).toBeVisible({ timeout: 60_000 });
  await expect(sound).toContainText('Recording');
  await info.attach('recording-lane', { contentType: 'image/png', body: await page.screenshot() });

  const powers = await windowPowers(page, readFileSync(await exportTo(page, editor, info, 'recording.mp4')));
  const levels = { lead: levelBetween(powers, 0.1, 0.8), phrase: levelBetween(powers, 1.2, 3.4) };
  await info.attach('recording-levels', { contentType: 'application/json', body: JSON.stringify(levels, null, 2) });
  expect(levels.phrase).toBeGreaterThan(-40);
  expect(levels.phrase - levels.lead).toBeGreaterThan(20);
});
```

- [ ] **Step 12: RED on the VM's current image**

`MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/e2e-vm.sh video-studio-voice.spec.ts --project=chrome --reporter=line`
Expected: FAIL on both — no dialog named "Add a sound" (the running image's Add audio opens the picker).

- [ ] **Step 13: Static checks, coverage, commit, dist, push, GREEN**

`webcheck.sh` on the touched files; coverage ≥ 85 % lines on `voiceApi.ts`, `AudioRecorder.tsx`, `VideoStudio_audioPanel.tsx`, `VideoStudio.tsx`.

```bash
git add web/src/videoStudio/AudioRecorder.tsx web/src/videoStudio/VideoStudio_audioPanel.tsx web/src/videoStudio/__tests__/AudioRecorder.test.tsx web/src/videoStudio/__tests__/VideoStudio_audioPanel.test.tsx web/e2e/fixtures/video-studio/audio/speech-phrase.mp3 web/e2e/video-studio-voice.spec.ts
git commit -m "feat(video-studio): record a voice or have a text read aloud onto a lane" -- web/src/chat/voice/voiceApi.ts web/src/chat/voice/voiceApi.test.ts web/src/videoStudio/AudioRecorder.tsx web/src/videoStudio/VideoStudio_audioPanel.tsx web/src/videoStudio/VideoStudio.tsx web/src/i18n/resources.videoStudioAudio.ts web/src/styles/video-studio-audio.css web/src/videoStudio/__tests__/AudioRecorder.test.tsx web/src/videoStudio/__tests__/VideoStudio_audioPanel.test.tsx web/src/videoStudio/__tests__/VideoStudio_audio.test.tsx web/e2e/fixtures/video-studio/audio/speech-phrase.mp3 web/e2e/fixtures/video-studio/audio/README.md web/e2e/video-studio-lane.spec.ts web/e2e/video-studio-voice.spec.ts
```

`build.sh`, commit the dist, push, CI green (the Web E2E job runs the voice spec against the route stand-in), `waitfix.sh <dist sha>`, then `e2e-vm.sh video-studio-voice.spec.ts video-studio-lane.spec.ts --reporter=line,json` GREEN — on the VM the TTS test runs against its real voice (no `voice` annotation expected). Read the levels, look at the three screenshots (the panel's live level while recording; each sound on its lane with its label).

---
### Task 3: Noise reduction on clips and sounds (spec T6)

**Files:**
- Modify: `web/package.json`, `web/package-lock.json` (`@sapphi-red/web-noise-suppressor` `0.4.1`)
- Create: `web/src/videoStudio/audioClean.ts`
- Create: `web/src/videoStudio/Inspector_audioClean.tsx`
- Modify: `web/src/videoStudio/Inspector_audio.tsx` (the switch in both Audio tabs; `ClipAudioControls` takes `project`)
- Modify: `web/src/videoStudio/Inspector_clip.tsx:440` (passes `project`)
- Modify: `web/src/videoStudio/videoflow_audio.ts` (`playsCleaned`, the cleaned source for sounds, a cleaned layer for clips)
- Modify: `web/src/videoStudio/videoflow.ts` (a cleaned clip's picture is muted)
- Modify: `web/src/i18n/resources.videoStudioAudio.ts` (`denoise`, `cleaning`, `cleanFailed`)
- Create: `web/e2e/video-studio-denoise.spec.ts`
- Test: `web/src/videoStudio/__tests__/audioClean.test.ts`, `Inspector_audioClean.test.tsx`, `videoflow_audio.test.ts`, `videoflow.test.ts`, `Inspector_audio.test.tsx`

**Interfaces:**
- Consumes: `decodeMono(url, rate, signal?)` (Task 1, `audioDecode.ts`); `recordAnalysis`, `setAudioProperties` (`commands_audio.ts`), `setClipPresentation` (`commands.ts`); `uploadSource(file)` (`VideoStudio_sources.ts`); `deleteAsset(id)` (`chat/attachments/api.ts`).
- Produces: `denoiseSamples(samples, signal?) → Promise<Float32Array>` and `DENOISE_RATE` (`audioClean.ts` — Task 4's speech detection runs on RNNoise's output); `NoiseReductionSwitch` and the file-private `useAnalysis()` (`Inspector_audioClean.tsx` — Task 4 adds `DuckingControls` in the same file, on the same hook); `playsCleaned(project, clip)` (`videoflow_audio.ts`).

- [ ] **Step 1: Add the dependency**

Run in WSL: `cd /mnt/d/Aura/web && npm install --save-exact @sapphi-red/web-noise-suppressor@0.4.1`
Expected: `"@sapphi-red/web-noise-suppressor": "0.4.1"` in `package.json`; `ls node_modules/@sapphi-red/web-noise-suppressor/dist` lists `rnnoise.wasm`, `rnnoise_simd.wasm`, `rnnoiseWorklet.js` (the paths S3 imported with `?url`).

- [ ] **Step 2: The compile — failing tests**

In `videoflow_audio.test.ts`, import `playsCleaned` beside `addAudioItems`, and add a helper plus tests:

```ts
/** The project with its sources cleaned once: both have a denoised copy on record. */
function cleaned(items: AudioItem[] = [], clip: Partial<VideoItem> = {}): VideoProject {
  const base = project(items, clip);
  return {
    ...base,
    sources: base.sources.map((source) =>
      source.id === 'src-a' || source.id === 'src-m'
        ? { ...source, denoisedAssetId: `${source.assetId}-clean` }
        : source,
    ),
  };
}

describe('noise reduction in the compile', () => {
  it('plays a denoised sound from its cleaned copy, and an undenoised one from its source', () => {
    const { addAudio, flow } = flowSpy();
    addAudioItems(flow, cleaned([sound({ denoise: true })]), urls);
    expect(addAudio.mock.calls[0]?.[1]).toMatchObject({ name: 'bed', source: '/api/assets/m-clean/content' });
    addAudio.mockClear();
    addAudioItems(flow, cleaned([sound()]), urls);
    expect(addAudio.mock.calls[0]?.[1]).toMatchObject({ source: '/api/assets/m/content' });
  });

  it('plays a sound whose cleaning never finished from its source', () => {
    const { addAudio, flow } = flowSpy();
    addAudioItems(flow, project([sound({ denoise: true })]), urls);
    expect(addAudio.mock.calls[0]?.[1]).toMatchObject({ source: '/api/assets/m/content' });
  });

  it('gives a cleaned clip a sound layer of its own, on the clip’s window', () => {
    const { addAudio, flow } = flowSpy();
    addAudioItems(flow, cleaned([], { denoise: true, sourceStart: 2, speed: 2 }), urls);
    expect(addAudio).toHaveBeenCalledWith(
      { mute: false },
      {
        name: 'clip-1#clean',
        source: '/api/assets/a-clean/content',
        startTime: 0,
        sourceStart: 2,
        sourceDuration: 4,
        speed: 2,
      },
    );
  });

  it('knows which clips play their cleaned copy', () => {
    const base = cleaned([], { denoise: true });
    const clip = base.video[0];
    if (clip === undefined) throw new Error('fixture');
    expect(playsCleaned(base, clip)).toBe(true);
    expect(playsCleaned(base, { ...clip, muted: true })).toBe(false);
    expect(playsCleaned(base, { ...clip, denoise: false })).toBe(false);
    expect(playsCleaned(project([], { denoise: true }), clip)).toBe(false);
    const still = base.video[1];
    if (still === undefined) throw new Error('fixture');
    expect(playsCleaned(base, { ...still, denoise: true })).toBe(false);
  });

  it('moves a cleaned clip’s volume onto its sound layer, and leaves the muted picture alone', () => {
    const out = withVolumes(
      cleaned([], { denoise: true, volume: 0.5 }),
      json(layer('clip-1', { sourceStart: 0.0001 }), layer('clip-1#clean', { sourceStart: 0 })),
    );
    expect(out.layers[0]?.animations).toEqual([]);
    expect(out.layers[1]?.animations).toEqual([
      { property: 'volume', keyframes: [{ time: 0, value: 0.5 }] },
    ]);
  });
});
```

In `videoflow.test.ts`, give the fake `VideoFlow` class an `addAudio` that records into a new `calls.audios` array (declared and reset like `calls.videos`):

```ts
    addAudio(props: Record<string, unknown>, settings: Record<string, unknown>) {
      calls.audios.push({ props, settings });
      return { animate: vi.fn() };
    }
```

and add to `describe('toVideoJSON')`:

```ts
  it('mutes a cleaned clip’s picture, whose sound now plays from its cleaned copy', async () => {
    const base = project();
    await toVideoJSON(
      {
        ...base,
        sources: base.sources.map((source) =>
          source.id === 'src-a' ? { ...source, denoisedAssetId: 'asset-a-clean' } : source,
        ),
        video: base.video.map((clip) => (clip.id === 'clip-1' ? { ...clip, denoise: true } : clip)),
      },
      urls,
    );
    expect(calls.videos[0]?.props.mute).toBe(true);
    expect(calls.audios.map((audio) => audio.settings.name)).toEqual(['clip-1#clean']);
  });
```

Run: `MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/vtf.sh src/videoStudio/__tests__/videoflow_audio.test.ts src/videoStudio/__tests__/videoflow.test.ts`
Expected: FAIL — `playsCleaned` is not exported; the cleaned sound still plays `m`; no `clip-1#clean` layer.

- [ ] **Step 3: Write the compile**

In `videoflow_audio.ts`:
- import `clipStarts` and `type VideoItem` from `./project` beside the existing imports;
- add, above `addAudioItems`:

```ts
/** The name of the sound layer a cleaned clip plays through; the clip's own layer keeps its id. */
const CLEANED = '#clean';

/** The cleaned copy a clip or a sound plays instead of its source: only once noise reduction is on
 *  AND its cleaning has finished — until then it plays what it always played. */
function cleanedAsset(
  project: VideoProject,
  sourceId: string,
  denoise: boolean | undefined,
): string | undefined {
  return denoise === true ? sourceOf(project, sourceId)?.denoisedAssetId : undefined;
}

/** Whether a clip's sound comes from its cleaned copy. VideoFlow's video layer can only play its
 *  own file's audio, so a cleaned clip is its muted picture plus a sound layer of its own. */
export function playsCleaned(project: VideoProject, clip: VideoItem): boolean {
  const source = sourceOf(project, clip.sourceId);
  return (
    !clip.muted &&
    source?.kind === 'video' &&
    source.hasAudio !== false &&
    cleanedAsset(project, clip.sourceId, clip.denoise) !== undefined
  );
}
```

- in `addAudioItems`, the sound's source becomes `source: urls.assetUrl(cleanedAsset(project, item.sourceId, item.denoise) ?? source.assetId),` and, after the loop over the sounds:

```ts
  const starts = clipStarts(project);
  project.video.forEach((clip, index) => {
    const cleaned = playsCleaned(project, clip)
      ? cleanedAsset(project, clip.sourceId, clip.denoise)
      : undefined;
    if (cleaned === undefined) return;
    // No cut nudge: it keeps the PICTURE off a frame boundary, and this layer has none. The
    // cleaned copy is already trimmed of RNNoise's delay (audioClean.ts), so it sits on the picture.
    flow.addAudio(
      { mute: false },
      {
        name: `${clip.id}${CLEANED}`,
        source: urls.assetUrl(cleaned),
        startTime: starts[index] ?? 0,
        sourceStart: clip.sourceStart,
        sourceDuration: clip.duration,
        speed: clip.speed ?? 1,
      },
    );
  });
```

- in `loudnessByLayer`, the clip loop keys a cleaned clip's loudness by its sound layer:

```ts
  for (const clip of project.video) {
    const source = sourceOf(project, clip.sourceId);
    if (!clip.muted && source?.kind === 'video' && source.hasAudio !== false) {
      byName.set(playsCleaned(project, clip) ? `${clip.id}${CLEANED}` : clip.id, {
        volume: clip.volume ?? 1,
      });
    }
  }
```

- the file header gains: "A clip or sound with noise reduction on plays its cleaned copy (`denoisedAssetId`) once the copy exists; a clip does so through a sound layer of its own under its muted picture."

In `videoflow.ts`: import `playsCleaned` beside `addAudioItems`, and in `addClip` the property becomes `mute: clip.muted || playsCleaned(project, clip),` with the comment above it gaining: "A cleaned clip is muted too: its sound plays from the cleaned copy's own layer (videoflow_audio.ts)."

Run the Step 2 command. Expected: PASS.

- [ ] **Step 4: RNNoise in the browser — failing tests**

`web/src/videoStudio/__tests__/audioClean.test.ts`:

```ts
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanedFile, denoiseSamples, DENOISE_RATE } from '../audioClean';

// RNNoise offline, with the browser stood in for: jsdom has no Web Audio and no WebCodecs. The fake
// renderer answers what RNNoise does to its input — the same samples 992 later (its delay, S3) — or
// silence, and records when rendering started, so the trim, the silence refusal and the wait for
// the worklet's WASM are each judged against a known answer.

const DELAY = 992;

const audio = vi.hoisted(() => ({
  silence: false,
  renderedAt: [] as number[],
  lengths: [] as number[],
}));

vi.mock('@sapphi-red/web-noise-suppressor', () => ({
  loadRnnoise: () => Promise.resolve(new ArrayBuffer(8)),
  RnnoiseWorkletNode: class {
    connect() {
      return undefined;
    }
  },
}));

const decode = vi.hoisted(() => ({ samples: new Float32Array(0), rates: [] as number[] }));
vi.mock('../audioDecode', () => ({
  decodeMono: (_url: string, rate: number) => {
    decode.rates.push(rate);
    return Promise.resolve(decode.samples);
  },
}));

const encoded = vi.hoisted(() => ({ buffers: [] as { length: number; sampleRate: number }[] }));
vi.mock('mediabunny', () => ({
  QUALITY_HIGH: 'high',
  OggOutputFormat: class {},
  BufferTarget: class {
    buffer: ArrayBuffer | null = null;
  },
  AudioBufferSource: class {
    add(buffer: { length: number; sampleRate: number }) {
      encoded.buffers.push(buffer);
      return Promise.resolve();
    }
  },
  Output: class {
    private readonly target: { buffer: ArrayBuffer | null };
    constructor(options: { target: { buffer: ArrayBuffer | null } }) {
      this.target = options.target;
    }
    addAudioTrack() {
      return undefined;
    }
    start() {
      return Promise.resolve();
    }
    finalize() {
      this.target.buffer = new Uint8Array([0x4f, 0x67, 0x67, 0x53]).buffer;
      return Promise.resolve();
    }
  },
}));

class FakeContext {
  private input = new Float32Array(0);
  readonly audioWorklet = { addModule: () => Promise.resolve() };
  readonly destination = {};
  constructor(_channels: number, length: number, _rate: number) {
    audio.lengths.push(length);
  }
  createBuffer(_channels: number, length: number) {
    const data = new Float32Array(length);
    return {
      copyToChannel: (samples: Float32Array) => {
        data.set(samples);
        this.input = data;
      },
    };
  }
  createBufferSource() {
    return { buffer: null, connect: () => undefined, start: () => undefined };
  }
  startRendering() {
    audio.renderedAt.push(Date.now());
    const out = new Float32Array(this.input.length + DELAY);
    if (!audio.silence) out.set(this.input, DELAY);
    return Promise.resolve({ getChannelData: () => out });
  }
}

class FakeAudioBuffer {
  readonly length: number;
  readonly sampleRate: number;
  constructor(options: { length: number; sampleRate: number }) {
    this.length = options.length;
    this.sampleRate = options.sampleRate;
  }
  copyToChannel() {
    return undefined;
  }
}

function ramp(length: number): Float32Array {
  return Float32Array.from({ length }, (_, index) => ((index % 100) - 50) / 100);
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.stubGlobal('OfflineAudioContext', FakeContext);
  vi.stubGlobal('AudioBuffer', FakeAudioBuffer);
  audio.silence = false;
  audio.renderedAt.length = 0;
  audio.lengths.length = 0;
  decode.rates.length = 0;
  encoded.buffers.length = 0;
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

async function settle<T>(work: Promise<T>): Promise<T> {
  await vi.advanceTimersByTimeAsync(1000);
  return work;
}

describe('denoiseSamples', () => {
  it('waits half a second for the worklet’s WASM before it renders', async () => {
    const started = Date.now();
    const work = denoiseSamples(ramp(4800));
    await vi.advanceTimersByTimeAsync(499);
    expect(audio.renderedAt).toHaveLength(0);
    await vi.advanceTimersByTimeAsync(1);
    await work;
    expect((audio.renderedAt[0] ?? 0) - started).toBeGreaterThanOrEqual(500);
  });

  it('trims RNNoise’s delay, so the clean sound starts where the source does and keeps its length', async () => {
    const input = ramp(4800);
    const out = await settle(denoiseSamples(input));
    expect(out).toHaveLength(input.length);
    expect(Array.from(out.subarray(0, 200))).toEqual(Array.from(input.subarray(0, 200)));
    // The render runs a delay longer, or the trim would cut the source's last 20 ms.
    expect(audio.lengths).toEqual([input.length + DELAY]);
  });

  it('refuses silence answered to sound', async () => {
    audio.silence = true;
    const work = denoiseSamples(ramp(4800));
    const refused = expect(work).rejects.toThrow(/silence/);
    await vi.advanceTimersByTimeAsync(1000);
    await refused;
  });

  it('accepts silence answered to silence', async () => {
    audio.silence = true;
    const out = await settle(denoiseSamples(new Float32Array(4800)));
    expect(out).toHaveLength(4800);
  });

  it('stops before rendering when its signal is aborted during the wait', async () => {
    const controller = new AbortController();
    const work = denoiseSamples(ramp(4800), controller.signal);
    const refused = expect(work).rejects.toThrow();
    controller.abort();
    await vi.advanceTimersByTimeAsync(1000);
    await refused;
    expect(audio.renderedAt).toHaveLength(0);
  });
});

describe('cleanedFile', () => {
  it('decodes at RNNoise’s rate and answers an Ogg Opus file named after its source', async () => {
    decode.samples = ramp(9600);
    const file = await settle(cleanedFile('/api/assets/a/download', 'src-a'));
    expect(decode.rates).toEqual([DENOISE_RATE]);
    expect(file.name).toBe('src-a.clean.ogg');
    expect(file.type).toBe('audio/ogg');
    expect(file.size).toBe(4);
    expect(encoded.buffers).toEqual([expect.objectContaining({ length: 9600, sampleRate: 48000 })]);
  });
});
```

Run: `MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/vtf.sh src/videoStudio/__tests__/audioClean.test.ts`
Expected: FAIL — `Failed to resolve import "../audioClean"`.

- [ ] **Step 5: Write it**

`web/src/videoStudio/audioClean.ts`:

```ts
import { loadRnnoise, RnnoiseWorkletNode } from '@sapphi-red/web-noise-suppressor';
import rnnoiseWasm from '@sapphi-red/web-noise-suppressor/rnnoise.wasm?url';
import rnnoiseSimdWasm from '@sapphi-red/web-noise-suppressor/rnnoise_simd.wasm?url';
import rnnoiseWorklet from '@sapphi-red/web-noise-suppressor/rnnoiseWorklet.js?url';
import { AudioBufferSource, BufferTarget, OggOutputFormat, Output, QUALITY_HIGH } from 'mediabunny';
import { decodeMono } from './audioDecode';

// audioClean.ts — noise reduction in the browser: RNNoise run offline over a source (S3,
// spikes/video-studio-audio/FINDINGS.md), and the result encoded as Ogg Opus by mediabunny so it is
// stored beside the original and played in its place. Reached only through import() from the
// control that asks for it: the RNNoise WASM is not the editor's first paint. The WASM and the
// worklet are this origin's own files (Vite `?url`), never a CDN's.
//
// RNNoise is a mono model, so a cleaned copy is mono: noise reduction is for speech.

/** RNNoise's model rate; the context runs there and the decoder resamples to it. */
export const DENOISE_RATE = 48000;
/** The worklet instantiates its WASM in its constructor and answers silence until it has, with no
 *  message when it is ready; an offline render outruns that. S3: 0 ms gives silence, 500 ms works. */
const WORKLET_WARMUP_MS = 500;
/** RNNoise's delay at 48 kHz, 20.67 ms (S3): trimmed, so a cleaned clip stays on its picture. */
const RNNOISE_DELAY = 992;
/** A peak under this is silence. */
const SILENT = 1e-4;

function peak(samples: Float32Array): number {
  let loudest = 0;
  for (const sample of samples) loudest = Math.max(loudest, Math.abs(sample));
  return loudest;
}

/** `samples` — mono, at DENOISE_RATE — with RNNoise run over them, as long as they were. */
export async function denoiseSamples(
  samples: Float32Array,
  signal?: AbortSignal,
): Promise<Float32Array> {
  // A delay longer than the input, or the trim below would cut its last 20 ms.
  const context = new OfflineAudioContext(1, samples.length + RNNOISE_DELAY, DENOISE_RATE);
  const buffer = context.createBuffer(1, samples.length, DENOISE_RATE);
  buffer.copyToChannel(samples, 0);
  const source = context.createBufferSource();
  source.buffer = buffer;
  const wasmBinary = await loadRnnoise({ url: rnnoiseWasm, simdUrl: rnnoiseSimdWasm });
  await context.audioWorklet.addModule(rnnoiseWorklet);
  // Typed for AudioContext; an AudioWorkletNode takes any BaseAudioContext (S3).
  const node = new RnnoiseWorkletNode(context as unknown as AudioContext, {
    wasmBinary,
    maxChannels: 1,
  });
  source.connect(node);
  node.connect(context.destination);
  source.start();
  await new Promise((resolve) => setTimeout(resolve, WORKLET_WARMUP_MS));
  signal?.throwIfAborted();
  const rendered = await context.startRendering();
  const clean = rendered.getChannelData(0).slice(RNNOISE_DELAY, RNNOISE_DELAY + samples.length);
  if (peak(samples) > SILENT && peak(clean) <= SILENT) {
    throw new Error('videoStudio: noise reduction answered silence to a sound');
  }
  return clean;
}

/** A source's cleaned copy as a file to store: decoded, RNNoise, encoded as Ogg Opus. */
export async function cleanedFile(url: string, name: string, signal?: AbortSignal): Promise<File> {
  const clean = await denoiseSamples(await decodeMono(url, DENOISE_RATE, signal), signal);
  const buffer = new AudioBuffer({
    length: clean.length,
    numberOfChannels: 1,
    sampleRate: DENOISE_RATE,
  });
  buffer.copyToChannel(clean, 0);
  const target = new BufferTarget();
  const output = new Output({ format: new OggOutputFormat(), target });
  const track = new AudioBufferSource({ codec: 'opus', bitrate: QUALITY_HIGH });
  output.addAudioTrack(track);
  await output.start();
  await track.add(buffer);
  await output.finalize();
  if (target.buffer === null) throw new Error('videoStudio: the cleaned sound was not written');
  return new File([target.buffer], `${name}.clean.ogg`, { type: 'audio/ogg' });
}
```

(If `tsc` does not know `*.wasm?url` / `*.js?url`, the web app's `vite/client` types already declare `*?url`; `webcheck.sh` says which.)

Run the Step 4 command. Expected: PASS, 6 tests.

- [ ] **Step 6: The strings**

In `resources.videoStudioAudio.ts`, `videoStudioAudioEn` gains `denoise: 'Noise reduction'`, `cleaning: 'Cleaning the sound…'`, `cleanFailed: 'Noise reduction failed: {{reason}}'`; `videoStudioAudioIt` gains `denoise: 'Riduzione del rumore'`, `cleaning: 'Pulizia del suono…'`, `cleanFailed: 'Riduzione del rumore non riuscita: {{reason}}'`.

- [ ] **Step 7: The switch — failing tests**

`web/src/videoStudio/__tests__/Inspector_audioClean.test.tsx`:

```tsx
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { NoiseReductionSwitch } from '../Inspector_audioClean';
import type { AudioItem, VideoProject } from '../project';

// The Noise reduction switch, with the cleaning and the upload stood in for. What is judged is the
// ONE edit it makes — the cleaned copy recorded and the switch turned on together, one undo step —
// and that a copy already made is never made again (Review Focus 3).

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, unknown>) =>
      values === undefined ? key : `${key} ${Object.values(values).join(' ')}`,
  }),
}));

const cleaning = vi.hoisted(() => ({
  cleanedFile: vi.fn<(url: string, name: string, signal?: AbortSignal) => Promise<File>>(),
  upload: vi.fn<(file: File) => Promise<string>>(),
  remove: vi.fn<(id: string) => Promise<unknown>>(),
}));
vi.mock('../audioClean', () => ({ cleanedFile: cleaning.cleanedFile }));
vi.mock('../VideoStudio_sources', () => ({ uploadSource: cleaning.upload }));
vi.mock('../../chat/attachments/api', () => ({ deleteAsset: cleaning.remove }));

function film(over: Partial<AudioItem> = {}, denoisedAssetId?: string): VideoProject {
  return {
    id: 'p',
    name: 'demo',
    size: { width: 320, height: 180 },
    fps: 25,
    sources: [
      { id: 'src-a', assetId: 'a', kind: 'video', duration: 8, size: { width: 320, height: 180 }, hasAudio: true },
      {
        id: 'src-v',
        assetId: 'v',
        kind: 'audio',
        duration: 6,
        size: { width: 0, height: 0 },
        ...(denoisedAssetId === undefined ? {} : { denoisedAssetId }),
      },
    ],
    video: [{ id: 'clip-1', sourceId: 'src-a', duration: 8, sourceStart: 0, muted: false }],
    overlays: [],
    audio: [
      {
        id: 'lane',
        items: [
          {
            id: 'voice',
            sourceId: 'src-v',
            anchor: { clipId: 'clip-1', offset: 0 },
            sourceStart: 0,
            duration: 6,
            volume: 1,
            muted: false,
            ...over,
          },
        ],
      },
    ],
  };
}

type Edit = (current: VideoProject) => VideoProject;

function mount(project: VideoProject, on = false) {
  const edits: Edit[] = [];
  const onCommand = vi.fn((edit: Edit) => {
    edits.push(edit);
  });
  const view = render(
    <NoiseReductionSwitch
      project={project}
      target={{ kind: 'sound', id: 'voice', sourceId: 'src-v', on }}
      onCommand={onCommand}
    />,
  );
  const apply = (index = 0) => edits[index]?.(project);
  return { ...view, onCommand, apply };
}

function flip() {
  fireEvent.click(screen.getByRole('switch', { name: 'videoStudio.audio.denoise' }));
}

beforeEach(() => {
  cleaning.cleanedFile.mockReset();
  cleaning.cleanedFile.mockResolvedValue(new File(['ogg'], 'src-v.clean.ogg', { type: 'audio/ogg' }));
  cleaning.upload.mockReset();
  cleaning.upload.mockResolvedValue('v-clean');
  cleaning.remove.mockReset();
  cleaning.remove.mockResolvedValue({});
});

describe('NoiseReductionSwitch', () => {
  it('cleans the source once, then records the copy and turns the switch on in one edit', async () => {
    const { onCommand, apply } = mount(film());
    flip();
    expect(await screen.findByRole('status')).toBeTruthy();
    await waitFor(() => {
      expect(onCommand).toHaveBeenCalledOnce();
    });
    expect(cleaning.cleanedFile).toHaveBeenCalledWith('/api/assets/v/download', 'src-v', expect.any(AbortSignal));
    const next = apply();
    expect(next?.sources.find((source) => source.id === 'src-v')?.denoisedAssetId).toBe('v-clean');
    expect(next?.audio?.[0]?.items[0]?.denoise).toBe(true);
    expect(screen.queryByRole('status')).toBeNull();
  });

  it('turns back on from the copy already made, without cleaning again', () => {
    const { onCommand, apply } = mount(film({}, 'v-clean'));
    flip();
    expect(onCommand).toHaveBeenCalledOnce();
    expect(cleaning.cleanedFile).not.toHaveBeenCalled();
    expect(apply()?.audio?.[0]?.items[0]?.denoise).toBe(true);
  });

  it('turns off without cleaning anything', () => {
    const { apply } = mount(film({ denoise: true }, 'v-clean'), true);
    flip();
    expect(apply()?.audio?.[0]?.items[0]?.denoise).toBe(false);
    expect(cleaning.cleanedFile).not.toHaveBeenCalled();
  });

  it('says why a cleaning failed and leaves the project untouched', async () => {
    cleaning.cleanedFile.mockRejectedValue(new Error('Unable to decode audio data'));
    const { onCommand } = mount(film());
    flip();
    expect((await screen.findByRole('alert')).textContent).toBe(
      'videoStudio.audio.cleanFailed Unable to decode audio data',
    );
    expect(onCommand).not.toHaveBeenCalled();
    expect(cleaning.upload).not.toHaveBeenCalled();
  });

  it('writes nothing, and uploads nothing, when it goes away while cleaning', async () => {
    let signal: AbortSignal | undefined;
    let finish: (file: File) => void = () => undefined;
    cleaning.cleanedFile.mockImplementation((_url, _name, given) => {
      signal = given;
      return new Promise((resolve) => {
        finish = resolve;
      });
    });
    const { onCommand, unmount } = mount(film());
    flip();
    await waitFor(() => {
      expect(signal).toBeDefined();
    });
    unmount();
    expect(signal?.aborted).toBe(true);
    await act(async () => {
      finish(new File(['ogg'], 'x.ogg', { type: 'audio/ogg' }));
      await Promise.resolve();
    });
    expect(cleaning.upload).not.toHaveBeenCalled();
    expect(onCommand).not.toHaveBeenCalled();
  });

  it('deletes the copy it uploaded when it went away during the upload', async () => {
    let finish: (id: string) => void = () => undefined;
    cleaning.upload.mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const { onCommand, unmount } = mount(film());
    flip();
    await waitFor(() => {
      expect(cleaning.upload).toHaveBeenCalledOnce();
    });
    unmount();
    await act(async () => {
      finish('v-clean');
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(cleaning.remove).toHaveBeenCalledWith('v-clean');
    });
    expect(onCommand).not.toHaveBeenCalled();
  });

  it('offers nothing for a clip whose source has no sound', () => {
    const base = film();
    const silent: VideoProject = {
      ...base,
      sources: base.sources.map((source) => (source.id === 'src-a' ? { ...source, hasAudio: false } : source)),
    };
    render(
      <NoiseReductionSwitch
        project={silent}
        target={{ kind: 'clip', id: 'clip-1', sourceId: 'src-a', on: false }}
        onCommand={vi.fn()}
      />,
    );
    expect(screen.queryByRole('switch')).toBeNull();
  });

  it('turns a clip’s noise reduction on through the clip’s own edit', async () => {
    const edits: Edit[] = [];
    const base = film();
    render(
      <NoiseReductionSwitch
        project={base}
        target={{ kind: 'clip', id: 'clip-1', sourceId: 'src-a', on: false }}
        onCommand={(edit) => edits.push(edit)}
      />,
    );
    flip();
    await waitFor(() => {
      expect(edits).toHaveLength(1);
    });
    const next = edits[0]?.(base);
    expect(next?.video[0]?.denoise).toBe(true);
    expect(next?.sources[0]?.denoisedAssetId).toBe('v-clean');
  });
});
```

Run: `MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/vtf.sh src/videoStudio/__tests__/Inspector_audioClean.test.tsx`
Expected: FAIL — `Failed to resolve import "../Inspector_audioClean"`.

- [ ] **Step 8: Write the switch**

`web/src/videoStudio/Inspector_audioClean.tsx`:

```tsx
import { useEffect, useId, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAssetSource } from '../chat/artifacts/renderers/assetSourceContext';
import { deleteAsset } from '../chat/attachments/api';
import { setClipPresentation } from './commands';
import { recordAnalysis, setAudioProperties } from './commands_audio';
import { sourceOf, type VideoProject } from './project';
import { uploadSource } from './VideoStudio_sources';
import { Switch } from '@/components/ui/switch';

// Inspector_audioClean.tsx — the audio controls that need an analysis in the browser before they
// can edit: noise reduction, and (Task 4) ducking. Each runs its analysis under an AbortSignal, says
// on the control that it is working, and enters the project in ONE edit — the analysis recorded
// and the setting changed together, so one undo takes back both. A failure is said on the control
// and leaves the project untouched; a control that goes away mid-way writes nothing (spec
// §Browser-side analysis). The WASM behind each analysis is imported when it is first asked for.

type Commit = (edit: (current: VideoProject) => VideoProject) => void;

type AnalysisState =
  | { readonly state: 'idle' }
  | { readonly state: 'working' }
  | { readonly state: 'failed'; readonly reason: string };

/** One analysis at a time for a control, aborted when the control goes: mounted with the item's id
 *  as its key, a control that goes is also a control whose selection changed. */
function useAnalysis() {
  const [state, setState] = useState<AnalysisState>({ state: 'idle' });
  const running = useRef<AbortController>(undefined);
  useEffect(
    () => () => {
      running.current?.abort();
    },
    [],
  );
  async function run(task: (signal: AbortSignal) => Promise<void>) {
    running.current?.abort();
    const controller = new AbortController();
    running.current = controller;
    setState({ state: 'working' });
    try {
      await task(controller.signal);
      if (!controller.signal.aborted) setState({ state: 'idle' });
    } catch (error) {
      if (controller.signal.aborted) return;
      setState({ state: 'failed', reason: error instanceof Error ? error.message : String(error) });
    }
  }
  return { state, run };
}

/** What a switch turns: a clip's noise reduction or a sound's, over the source it plays. */
interface CleanTarget {
  readonly kind: 'clip' | 'sound';
  readonly id: string;
  readonly sourceId: string;
  readonly on: boolean;
}

interface NoiseReductionSwitchProps {
  readonly project: VideoProject;
  readonly target: CleanTarget;
  readonly onCommand: Commit;
}

/** Noise reduction for one clip or sound. The first time it is turned on for a source, the source
 *  is cleaned and the copy stored (`denoisedAssetId`); every later time reuses that copy. */
export function NoiseReductionSwitch({ project, target, onCommand }: NoiseReductionSwitchProps) {
  const { t } = useTranslation();
  const { assetUrl } = useAssetSource();
  const id = useId();
  const { state, run } = useAnalysis();
  const source = sourceOf(project, target.sourceId);
  if (source === undefined || source.kind === 'image' || source.hasAudio === false) return null;
  const known = source;

  const turned = (current: VideoProject, denoise: boolean) =>
    target.kind === 'clip'
      ? setClipPresentation(current, { clipId: target.id, denoise })
      : setAudioProperties(current, { itemId: target.id, denoise });

  function turn(on: boolean) {
    if (!on || known.denoisedAssetId !== undefined) {
      onCommand((current) => turned(current, on));
      return;
    }
    void run(async (signal) => {
      const { cleanedFile } = await import('./audioClean');
      const file = await cleanedFile(assetUrl(known.assetId), known.id, signal);
      signal.throwIfAborted();
      const assetId = await uploadSource(file);
      if (signal.aborted) {
        // The upload could not be stopped once started: the copy it made is deleted rather than
        // left in the library with nothing pointing at it. The control is gone, so a failed delete
        // has nobody to tell.
        await deleteAsset(assetId).catch(() => undefined);
        return;
      }
      onCommand((current) =>
        turned(recordAnalysis(current, { sourceId: known.id, denoisedAssetId: assetId }), true),
      );
    });
  }

  return (
    <div className="grid gap-1">
      <div className="flex items-center gap-2 text-xs text-text-muted">
        <Switch
          id={id}
          checked={target.on}
          disabled={state.state === 'working'}
          aria-label={t('videoStudio.audio.denoise')}
          onCheckedChange={turn}
        />
        <label htmlFor={id}>{t('videoStudio.audio.denoise')}</label>
      </div>
      {state.state === 'working' ? (
        <p role="status" className="text-xs text-text-muted">
          {t('videoStudio.audio.cleaning')}
        </p>
      ) : null}
      {state.state === 'failed' ? (
        <p role="alert" className="text-xs text-danger">
          {t('videoStudio.audio.cleanFailed', { reason: state.reason })}
        </p>
      ) : null}
    </div>
  );
}
```

(`signal.throwIfAborted()` after the cleaning is what makes "goes away while cleaning" upload nothing: the abort lands in `useAnalysis`'s `catch`, which returns silently because the signal says it was asked for.)

Run the Step 7 command. Expected: PASS, 8 tests.

- [ ] **Step 9: Both Audio tabs show it — failing test**

`Inspector_audio.test.tsx` renders the real `Inspector` through its `mount(selectedId)` and names controls by key (its `react-i18next` mock answers the key). Add to `describe('Inspector, on a sound')`:

```tsx
  it('offers noise reduction beside the mute', () => {
    mount('bed');
    expect(screen.getByRole('switch', { name: 'videoStudio.audio.denoise' })).toBeTruthy();
  });
```

and to `describe('Inspector, on a clip Audio tab')`:

```tsx
  it('offers noise reduction on the clip Audio tab too', () => {
    mount('clip-1');
    openTab('videoStudio.inspector.tabs.audio');
    expect(screen.getByRole('switch', { name: 'videoStudio.audio.denoise' })).toBeTruthy();
  });
```

Run: `MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/vtf.sh src/videoStudio/__tests__/Inspector_audio.test.tsx`
Expected: FAIL — no switch named `videoStudio.audio.denoise`.

- [ ] **Step 10: Mount it**

In `Inspector_audio.tsx`:
- import `NoiseReductionSwitch` from `./Inspector_audioClean`;
- in `AudioItemInspector`'s Audio tab, after the `MuteSwitch`:

```tsx
          <NoiseReductionSwitch
            key={item.id}
            project={project}
            target={{ kind: 'sound', id: item.id, sourceId: item.sourceId, on: item.denoise === true }}
            onCommand={onCommand}
          />
```

- `ClipAudioControls` takes `project` (`readonly project: VideoProject;`) and renders, after its `MuteSwitch`:

```tsx
      <NoiseReductionSwitch
        key={clip.id}
        project={project}
        target={{ kind: 'clip', id: clip.id, sourceId: clip.sourceId, on: clip.denoise === true }}
        onCommand={onCommand}
      />
```

- the header comment gains: "Noise reduction lives in Inspector_audioClean.tsx: it analyses before it edits."

In `Inspector_clip.tsx:440`: `<ClipAudioControls project={props.project} clip={props.clip} onCommand={props.onCommand} />`.

Run: `MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/vtf.sh src/videoStudio`
Expected: PASS (except the known WSL-only `videoflow_fonts.test.ts`).

- [ ] **Step 11: The denoise E2E**

`web/e2e/video-studio-denoise.spec.ts`:

```ts
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { expect, test } from './support/assetCleanup';
import { uploadAsset } from './support/assetUpload';
import { levelBetween, windowPowers } from './support/audioMeasure';
import { AUDIO_FIXTURES, exportTo, reopen, silentFilm, uploadClip } from './support/videoStudio';

// video-studio-denoise.spec.ts — Noise reduction proven by the file: the noisy-speech fixture
// (speech.wav plus pink noise at 10 dB SNR) is exported before and after the switch. S3 measured
// RNNoise dropping the gaps' floor by 37.7 dB on this file; the threshold is that minus 3 dB. The
// speech itself must stay where it was.

/** Inside the gaps between phrases, past RNNoise's release; inside the phrases, past their edges. */
function levels(powers: readonly number[]) {
  const mean = (a: number, b: number) => (a + b) / 2;
  return {
    gap: mean(levelBetween(powers, 3.9, 4.9), levelBetween(powers, 8.1, 9.1)),
    speech: mean(levelBetween(powers, 1.3, 3.4), levelBetween(powers, 5.5, 7.6)),
  };
}

test('noise reduction drops the noise between phrases and keeps the phrases', async ({ page }, info) => {
  test.skip(info.project.name.startsWith('mobile'), 'the phone shows the same inspector switch');
  test.setTimeout(15 * 60_000);
  const clip = await uploadClip(page);
  const noisy = await uploadAsset(
    page,
    resolve(AUDIO_FIXTURES, 'speech-noisy.wav'),
    'speech-noisy.wav',
    'audio/wav',
    { use: 'media' },
  );
  const film = silentFilm(clip, 'denoise check', 4);
  const editor = await reopen(
    page,
    {
      ...film,
      sources: [
        ...film.sources,
        { id: 'src-n', assetId: noisy, kind: 'audio', duration: 12.905, size: { width: 0, height: 0 } },
      ],
      audio: [
        {
          id: 'lane-a',
          items: [
            {
              id: 'voice',
              sourceId: 'src-n',
              anchor: { clipId: 'clip-1', offset: 0 },
              sourceStart: 0,
              duration: 12.905,
              volume: 1,
              muted: false,
            },
          ],
        },
      ],
    },
    clip,
  );
  const before = levels(
    await windowPowers(page, readFileSync(await exportTo(page, editor, info, 'noisy.mp4'))),
  );

  await editor.getByRole('button', { name: 'Sound 1' }).click();
  const inspector = editor.getByRole('region', { name: 'Properties' });
  await inspector.getByRole('tab', { name: 'Audio' }).click();
  await inspector.getByRole('switch', { name: 'Noise reduction' }).click();
  await expect(inspector.getByRole('status')).toHaveText('Cleaning the sound…');
  await expect(inspector.getByRole('switch', { name: 'Noise reduction' })).toBeChecked({ timeout: 5 * 60_000 });
  await expect(editor.getByRole('alert')).toHaveCount(0);
  await info.attach('denoise-inspector', { contentType: 'image/png', body: await page.screenshot() });

  const after = levels(
    await windowPowers(page, readFileSync(await exportTo(page, editor, info, 'clean.mp4'))),
  );
  await info.attach('denoise-levels', {
    contentType: 'application/json',
    body: JSON.stringify({ before, after }, null, 2),
  });
  expect(before.gap - after.gap).toBeGreaterThanOrEqual(34.7);
  expect(Math.abs(before.speech - after.speech)).toBeLessThan(2);
});
```

- [ ] **Step 12: RED on the VM's current image**

`MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/e2e-vm.sh video-studio-denoise.spec.ts --project=chrome --reporter=line`
Expected: FAIL — no switch named "Noise reduction" (after the first export, which the running image already does).

- [ ] **Step 13: Static checks, coverage, commit, dist, push, GREEN**

`webcheck.sh` on the touched files (knip must see `denoiseSamples` and `DENOISE_RATE` used — `DENOISE_RATE` by the test and by `cleanedFile`; `denoiseSamples` by `cleanedFile` until Task 4 imports it); coverage ≥ 85 % lines on `audioClean.ts`, `Inspector_audioClean.tsx`, `Inspector_audio.tsx`, `Inspector_clip.tsx`, `videoflow_audio.ts`, `videoflow.ts`. Check that the dist carries the WASM as files of its own origin: after `build.sh`, `ls internal/webui/dist/assets | grep -E 'rnnoise|Worklet'` lists `rnnoise-*.wasm`, `rnnoise_simd-*.wasm` and `rnnoiseWorklet-*.js`, and none of them is referenced by the entry chunk (`grep -l rnnoise internal/webui/dist/assets/index-*.js` finds nothing — only the lazy `audioClean` chunk names them).

```bash
git add web/src/videoStudio/audioClean.ts web/src/videoStudio/Inspector_audioClean.tsx web/src/videoStudio/__tests__/audioClean.test.ts web/src/videoStudio/__tests__/Inspector_audioClean.test.tsx web/e2e/video-studio-denoise.spec.ts
git commit -m "feat(video-studio): noise reduction on clips and sounds, cleaned in the browser" -- web/package.json web/package-lock.json web/src/videoStudio/audioClean.ts web/src/videoStudio/Inspector_audioClean.tsx web/src/videoStudio/Inspector_audio.tsx web/src/videoStudio/Inspector_clip.tsx web/src/videoStudio/videoflow_audio.ts web/src/videoStudio/videoflow.ts web/src/i18n/resources.videoStudioAudio.ts web/src/videoStudio/__tests__/audioClean.test.ts web/src/videoStudio/__tests__/Inspector_audioClean.test.tsx web/src/videoStudio/__tests__/Inspector_audio.test.tsx web/src/videoStudio/__tests__/videoflow_audio.test.ts web/src/videoStudio/__tests__/videoflow.test.ts web/e2e/video-studio-denoise.spec.ts
```

`build.sh`, commit the dist, push, CI green (a full Stryker run: the lockfile changed; `videoflow_audio.ts` ≥ 70 % killed), `waitfix.sh <dist sha>`, then `e2e-vm.sh video-studio-denoise.spec.ts --reporter=line,json` GREEN. Read `before`/`after`; look at the inspector screenshot (switch on, no alert).

---
### Task 4: Ducking under speech (spec T7)

**Files:**
- Modify: `web/package.json`, `web/package-lock.json` (`@echogarden/fvad-wasm` `0.2.0`)
- Create: `web/src/videoStudio/fvad-wasm.d.ts`
- Create: `web/src/videoStudio/speechWindows.ts`
- Create: `web/src/videoStudio/audioSpeech.ts`
- Modify: `web/src/videoStudio/volumeCurve.ts` (`Ducking`, `duckAt`, `gainAt` ducks)
- Modify: `web/src/videoStudio/videoflow_audio.ts` (the speech besides a sound, mapped to its layer; `unheardSources`)
- Modify: `web/src/videoStudio/Inspector_audioClean.tsx` (`DuckingControls`)
- Modify: `web/src/videoStudio/Inspector_audio.tsx` (mounts it in a sound's Audio tab)
- Modify: `web/src/i18n/resources.videoStudioAudio.ts` (the ducking strings)
- Create: `web/e2e/video-studio-ducking.spec.ts`
- Test: `web/src/videoStudio/__tests__/speechWindows.test.ts`, `audioSpeech.test.ts`, `volumeCurve.test.ts`, `videoflow_audio.test.ts`, `Inspector_audioClean.test.tsx`

**Interfaces:**
- Consumes: `decodeMono` (Task 1); `denoiseSamples`, `DENOISE_RATE` (Task 3, `audioClean.ts`); `useAnalysis` (Task 3, private to `Inspector_audioClean.tsx`, where `DuckingControls` goes); `recordAnalysis`, `setAudioProperties` (`commands_audio.ts`, whose `checkRanges` already bounds ducking to −24…−3 dB and 0.1…2 s); `silentFilm` (Task 1, e2e support).
- Produces: `detectSpeech(url, signal?) → Promise<SpeechWindow[]>`; `speechFromFrames(voiced, frameSeconds)`; `duckAt(duck, t)`; `unheardSources(project, itemId) → ProjectSource[]`; `DuckingControls`. The last task of the plan: nothing consumes them after it.

- [ ] **Step 1: Add the dependency and declare it**

Run in WSL: `cd /mnt/d/Aura/web && npm install --save-exact @echogarden/fvad-wasm@0.2.0`
Expected: `"@echogarden/fvad-wasm": "0.2.0"`; `node_modules/@echogarden/fvad-wasm/` holds `fvad.js` and `fvad.wasm` (and no declarations).

`web/src/videoStudio/fvad-wasm.d.ts`:

```ts
// @echogarden/fvad-wasm ships libfvad as an Emscripten module with no declarations. Declared is
// exactly the surface audioSpeech.ts calls — libfvad's C API and the heap view Emscripten exports
// (`Module["HEAP16"]`, fvad.js `updateMemoryViews`) — and nothing more.
declare module '@echogarden/fvad-wasm' {
  interface FvadModule {
    _fvad_new(): number;
    _fvad_free(handle: number): void;
    _fvad_set_sample_rate(handle: number, rate: number): number;
    _fvad_set_mode(handle: number, mode: number): number;
    _fvad_process(handle: number, frame: number, length: number): number;
    _malloc(bytes: number): number;
    _free(pointer: number): void;
    readonly HEAP16: Int16Array;
  }
  export default function fvadInit(options: {
    readonly locateFile: (path: string) => string;
  }): Promise<FvadModule>;
}
```

- [ ] **Step 2: The frames as windows — failing test**

`web/src/videoStudio/__tests__/speechWindows.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import { speechFromFrames } from '../speechWindows';

// S4's smoothing of the VAD's 30 ms verdicts: a pause under 300 ms joins its neighbours, a window
// under 250 ms is dropped.

const FRAME = 0.03;

function frames(pattern: string): boolean[] {
  return [...pattern].map((mark) => mark === '#');
}

describe('speechFromFrames', () => {
  it('joins consecutive voiced frames into one window, in seconds', () => {
    const found = speechFromFrames(frames('..##########..'), FRAME);
    expect(found).toHaveLength(1);
    expect(found[0]?.[0]).toBeCloseTo(0.06, 9);
    expect(found[0]?.[1]).toBeCloseTo(0.36, 9);
  });

  it('bridges a pause shorter than 300 ms, and keeps one of 300 ms apart', () => {
    // Nine silent frames are 270 ms; ten are 300 ms.
    expect(speechFromFrames(frames(`${'#'.repeat(10)}${'.'.repeat(9)}${'#'.repeat(10)}`), FRAME)).toHaveLength(1);
    expect(speechFromFrames(frames(`${'#'.repeat(10)}${'.'.repeat(10)}${'#'.repeat(10)}`), FRAME)).toHaveLength(2);
  });

  it('drops a window shorter than 250 ms, and keeps one of 270 ms', () => {
    expect(speechFromFrames(frames('..########..'), FRAME)).toEqual([]);
    expect(speechFromFrames(frames('..#########..'), FRAME)).toHaveLength(1);
  });

  it('answers no speech for no frames and for silence', () => {
    expect(speechFromFrames([], FRAME)).toEqual([]);
    expect(speechFromFrames(frames('..........'), FRAME)).toEqual([]);
  });
});
```

Run: `MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/vtf.sh src/videoStudio/__tests__/speechWindows.test.ts`
Expected: FAIL — `Failed to resolve import "../speechWindows"`.

- [ ] **Step 3: Write it**

`web/src/videoStudio/speechWindows.ts`:

```ts
// speechWindows.ts — the VAD's 30 ms verdicts as speech windows, smoothed as S4 measured them
// (spikes/video-studio-audio/FINDINGS.md): a pause shorter than 300 ms joins its neighbours, a
// window shorter than 250 ms is dropped. Pure, so the smoothing is tested without the WASM that
// produces the verdicts.

export type SpeechWindow = readonly [number, number];

/** A pause shorter than this is a breath inside one phrase. */
const MERGE_GAP = 0.3;
/** A window shorter than this is a click, not speech. */
const MIN_SPEECH = 0.25;

/** The voiced frames as smoothed windows, in seconds from the first frame. */
export function speechFromFrames(voiced: readonly boolean[], frameSeconds: number): SpeechWindow[] {
  const merged: [number, number][] = [];
  voiced.forEach((isVoiced, index) => {
    if (!isVoiced) return;
    const start = index * frameSeconds;
    const last = merged.at(-1);
    if (last !== undefined && start - last[1] < MERGE_GAP - 1e-9) last[1] = start + frameSeconds;
    else merged.push([start, start + frameSeconds]);
  });
  return merged.filter(([start, end]) => end - start >= MIN_SPEECH - 1e-9);
}
```

(The `1e-9` keeps a pause of exactly ten frames — 0.3 s in floating point, which may land a hair under — on the side the spike's own integer-millisecond comparison put it.)

Run the Step 2 command. Expected: PASS, 4 tests.

- [ ] **Step 4: Speech detection in the browser — failing test**

`web/src/videoStudio/__tests__/audioSpeech.test.ts`:

```ts
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { detectSpeech } from '../audioSpeech';

// S4's pipeline with its WASM stood in for: RNNoise answers its input, the resampler answers the
// samples it was asked to render at 16 kHz, and the fake libfvad calls a frame voiced when its
// samples are loud — so what is judged is the framing, the heap copy and the order of the steps.

const RATE = 16000;

const steps = vi.hoisted(() => ({ order: [] as string[], refuseMode: false, freed: [] as string[] }));
const input = vi.hoisted(() => ({ samples: new Float32Array(0) }));

vi.mock('../audioDecode', () => ({
  decodeMono: (_url: string, rate: number) => {
    steps.order.push(`decode@${String(rate)}`);
    return Promise.resolve(input.samples);
  },
}));
vi.mock('../audioClean', () => ({
  DENOISE_RATE: 48000,
  denoiseSamples: (samples: Float32Array) => {
    steps.order.push('rnnoise');
    return Promise.resolve(samples);
  },
}));
vi.mock('@echogarden/fvad-wasm', () => ({
  default: () => {
    const heap = new Int16Array(4096);
    return Promise.resolve({
      HEAP16: heap,
      _fvad_new: () => 7,
      _fvad_free: () => steps.freed.push('handle'),
      _fvad_set_sample_rate: (_handle: number, rate: number) => (rate === RATE ? 0 : -1),
      _fvad_set_mode: () => (steps.refuseMode ? -1 : 0),
      _malloc: () => 64,
      _free: () => steps.freed.push('frame'),
      _fvad_process: (_handle: number, pointer: number, length: number) => {
        let loud = 0;
        for (let index = 0; index < length; index += 1) loud = Math.max(loud, Math.abs(heap[pointer / 2 + index] ?? 0));
        return loud > 1000 ? 1 : 0;
      },
    });
  },
}));

class Resampler {
  private samples = new Float32Array(0);
  readonly destination = {};
  constructor(_channels: number, _length: number, rate: number) {
    steps.order.push(`resample@${String(rate)}`);
  }
  createBuffer() {
    return {
      copyToChannel: (samples: Float32Array) => {
        // Every third sample: 48 kHz down to 16 kHz, as the browser's resampler answers it.
        this.samples = samples.filter((_, index) => index % 3 === 0);
      },
    };
  }
  createBufferSource() {
    return { buffer: null, connect: () => undefined, start: () => undefined };
  }
  startRendering() {
    return Promise.resolve({ getChannelData: () => this.samples });
  }
}

/** `seconds` of 48 kHz silence with a loud stretch from `from` to `to`. */
function withSpeech(seconds: number, from: number, to: number): Float32Array {
  return Float32Array.from({ length: seconds * 48000 }, (_, index) =>
    index >= from * 48000 && index < to * 48000 ? 0.5 * Math.sin(index / 7) : 0,
  );
}

beforeEach(() => {
  vi.stubGlobal('OfflineAudioContext', Resampler);
  steps.order.length = 0;
  steps.freed.length = 0;
  steps.refuseMode = false;
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('detectSpeech', () => {
  it('denoises at 48 kHz, resamples to 16 kHz, and finds the loud stretch within a frame', async () => {
    input.samples = withSpeech(3, 1, 2);
    const found = await detectSpeech('/api/assets/s/download');
    expect(steps.order).toEqual(['decode@48000', 'rnnoise', 'resample@16000']);
    expect(found).toHaveLength(1);
    expect(found[0]?.[0]).toBeCloseTo(0.99, 1);
    expect(found[0]?.[1]).toBeCloseTo(2.01, 1);
    expect(steps.freed.sort()).toEqual(['frame', 'handle']);
  });

  it('answers no speech for silence', async () => {
    input.samples = new Float32Array(48000);
    await expect(detectSpeech('/api/assets/s/download')).resolves.toEqual([]);
  });

  it('is loud about a detector that refuses its settings, and frees what it took', async () => {
    steps.refuseMode = true;
    input.samples = withSpeech(1, 0, 0.5);
    await expect(detectSpeech('/api/assets/s/download')).rejects.toThrow(/refused/);
    expect(steps.freed.sort()).toEqual(['frame', 'handle']);
  });

  it('stops after the noise reduction when its signal is aborted', async () => {
    input.samples = withSpeech(1, 0, 0.5);
    const controller = new AbortController();
    controller.abort();
    await expect(detectSpeech('/api/assets/s/download', controller.signal)).rejects.toThrow();
    expect(steps.order).not.toContain('resample@16000');
  });
});
```

Run: `MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/vtf.sh src/videoStudio/__tests__/audioSpeech.test.ts`
Expected: FAIL — `Failed to resolve import "../audioSpeech"`.

- [ ] **Step 5: Write it**

`web/src/videoStudio/audioSpeech.ts`:

```ts
import fvadInit from '@echogarden/fvad-wasm';
import fvadWasm from '@echogarden/fvad-wasm/fvad.wasm?url';
import { denoiseSamples, DENOISE_RATE } from './audioClean';
import { decodeMono } from './audioDecode';
import { speechFromFrames, type SpeechWindow } from './speechWindows';

// audioSpeech.ts — where a source has speech, by S4's pipeline (spikes/video-studio-audio/
// FINDINGS.md): RNNoise first, because WebRTC's VAD alone calls 10 dB-SNR noise speech from end to
// end; then the browser's resampler down to the VAD's 16 kHz; then libfvad mode 3 over 30 ms frames,
// smoothed. Edges within 95 ms on both fixtures. Reached only through import(), like audioClean.ts;
// the WASM is this origin's own file.

const VAD_RATE = 16000;
const FRAME_SECONDS = 0.03;
/** libfvad's most aggressive mode: the fewest false windows on the denoised copy (S4). */
const VAD_MODE = 3;

async function resampled(samples: Float32Array, from: number, to: number): Promise<Float32Array> {
  const context = new OfflineAudioContext(1, Math.ceil((samples.length * to) / from), to);
  const buffer = context.createBuffer(1, samples.length, from);
  buffer.copyToChannel(samples, 0);
  const source = context.createBufferSource();
  source.buffer = buffer;
  source.connect(context.destination);
  source.start();
  return (await context.startRendering()).getChannelData(0);
}

async function voicedFrames(samples: Float32Array): Promise<boolean[]> {
  const vad = await fvadInit({ locateFile: () => fvadWasm });
  const handle = vad._fvad_new();
  const frame = Math.round(VAD_RATE * FRAME_SECONDS);
  const pointer = vad._malloc(frame * 2);
  try {
    if (
      vad._fvad_set_sample_rate(handle, VAD_RATE) !== 0 ||
      vad._fvad_set_mode(handle, VAD_MODE) !== 0
    ) {
      throw new Error('videoStudio: the speech detector refused its settings');
    }
    // Read after the allocation: Emscripten replaces the view only when memory grows, and
    // processing a frame allocates nothing.
    const heap = vad.HEAP16;
    const voiced: boolean[] = [];
    for (let at = 0; at + frame <= samples.length; at += frame) {
      for (let index = 0; index < frame; index += 1) {
        const sample = Math.max(-1, Math.min(1, samples[at + index] ?? 0));
        heap[pointer / 2 + index] = Math.round(sample * 32767);
      }
      voiced.push(vad._fvad_process(handle, pointer, frame) === 1);
    }
    return voiced;
  } finally {
    vad._free(pointer);
    vad._fvad_free(handle);
  }
}

/** Where the sound at `url` has speech, in seconds of its source. */
export async function detectSpeech(url: string, signal?: AbortSignal): Promise<SpeechWindow[]> {
  const clean = await denoiseSamples(await decodeMono(url, DENOISE_RATE, signal), signal);
  signal?.throwIfAborted();
  const voiced = await voicedFrames(await resampled(clean, DENOISE_RATE, VAD_RATE));
  return speechFromFrames(voiced, FRAME_SECONDS);
}
```

Run the Step 4 command. Expected: PASS, 4 tests.

- [ ] **Step 6: The ducking factor — failing tests**

In `volumeCurve.test.ts`, import `duckAt` beside the existing imports and add:

```ts
describe('duckAt', () => {
  const duck = { windows: [[2, 4]] as const, amountDb: -12, ramp: 0.5 };
  const floor = 10 ** (-12 / 20);

  it('lowers by the amount inside a speech window, and not at all far from one', () => {
    expect(duckAt(duck, 3)).toBeCloseTo(floor, 9);
    expect(duckAt(duck, 2)).toBeCloseTo(floor, 9);
    expect(duckAt(duck, 1)).toBe(1);
    expect(duckAt(duck, 5)).toBe(1);
  });

  it('ramps down before the window and back up after it, over the softness', () => {
    expect(duckAt(duck, 1.75)).toBeCloseTo(1 - 0.5 * (1 - floor), 9);
    expect(duckAt(duck, 4.25)).toBeCloseTo(1 - 0.5 * (1 - floor), 9);
    expect(duckAt(duck, 1.5)).toBeCloseTo(1, 9);
    expect(duckAt(duck, 4.5)).toBeCloseTo(1, 9);
  });

  it('takes the deeper duck where two windows’ ramps meet', () => {
    const two = { windows: [[1, 2], [2.6, 3]] as const, amountDb: -12, ramp: 0.5 };
    // 0.3 s after the first and 0.3 s before the second: each alone would be 40 % down.
    expect(duckAt(two, 2.3)).toBeCloseTo(1 - 0.4 * (1 - floor), 9);
  });

  it('does not duck without ducking, or with nothing to duck under', () => {
    expect(duckAt(undefined, 3)).toBe(1);
    expect(duckAt({ windows: [], amountDb: -24, ramp: 1 }, 3)).toBe(1);
  });

  it('multiplies into the gain the mixer plays', () => {
    const input = { sourceStart: 0, speed: 1, length: 6, volume: 0.5, duck };
    expect(gainAt(input, 3)).toBeCloseTo(0.5 * floor, 9);
    expect(gainAt(input, 0.5)).toBeCloseTo(0.5, 9);
  });
});
```

Run: `MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/vtf.sh src/videoStudio/__tests__/volumeCurve.test.ts`
Expected: FAIL — `duckAt` is not exported.

- [ ] **Step 7: Write it**

In `volumeCurve.ts`, add after the `CurveInput` interface's `envelope` field:

```ts
  /** Where to lower the layer under other sounds' speech. */
  readonly duck?: Ducking | undefined;
```

and above `CurveInput`:

```ts
export interface Ducking {
  /** Speech in the other sounds, in timeline seconds from the layer's start. */
  readonly windows: readonly (readonly [number, number])[];
  readonly amountDb: number;
  /** Seconds the gain takes to go down before a window and to come back after it. */
  readonly ramp: number;
}
```

then, after `envelopeAt`:

```ts
/** The ducking gain `t` timeline seconds into the layer: the full `amountDb` inside a speech
 *  window, a linear ramp of `ramp` seconds down into it before and up out of it after, 1 elsewhere.
 *  Where two windows' ramps meet, the deeper one wins. */
export function duckAt(duck: Ducking | undefined, t: number): number {
  if (duck === undefined) return 1;
  let depth = 0;
  for (const [start, end] of duck.windows) {
    const outside = t < start ? start - t : t > end ? t - end : 0;
    depth = Math.max(depth, 1 - outside / duck.ramp);
  }
  return 1 - depth * (1 - 10 ** (duck.amountDb / 20));
}
```

and `gainAt` returns `input.volume * Math.max(0, fade) * envelopeAt(input.envelope, t * input.speed) * duckAt(input.duck, t);`. The file header gains: "Ducking multiplies in: the layer goes down under the other sounds' speech (`duckAt`)."

Run the Step 6 command. Expected: PASS.

- [ ] **Step 8: Speech besides a sound, in the compile — failing tests**

In `videoflow_audio.test.ts`, import `unheardSources` beside the others and add:

```ts
/** The fixture with speech heard in the clip's source and ducking on the bed. */
function ducked(over: Partial<AudioItem> = {}, clip: Partial<VideoItem> = {}): VideoProject {
  const base = project([sound({ ducking: { amountDb: -12, ramp: 0.5 }, ...over })], clip);
  return {
    ...base,
    sources: base.sources.map((source) =>
      source.id === 'src-a' ? { ...source, speech: [[1, 2]] } : source,
    ),
  };
}

function frames(out: VideoJSON) {
  return out.layers[0]?.animations.find((animation) => animation.property === 'volume')?.keyframes ?? [];
}

describe('ducking in the compile', () => {
  const floor = 10 ** (-12 / 20);

  it('lowers a sound under the speech the clips carry, on the sound’s own clock', () => {
    // The clip's speech is 1–2 s of the film; the bed starts at 1 s, so it is ducked from its first
    // instant for one second, then comes back over half a second.
    const out = withVolumes(ducked(), json(layer('bed', { sourceStart: 2, sourceDuration: 4 })));
    const keyframes = frames(out);
    expect(keyframes[0]?.time).toBe(2);
    expect(keyframes[0]?.value).toBeCloseTo(floor, 6);
    const halfway = keyframes.find((keyframe) => Math.abs(keyframe.time - 3.25) < 1e-6);
    expect(halfway?.value).toBeCloseTo(1 - 0.5 * (1 - floor), 6);
    expect(keyframes.at(-1)?.value).toBeCloseTo(1, 6);
  });

  it('maps speech through the clip’s trim and speed', () => {
    // Trimmed by 1 s and at double speed, source 1–2 s plays at film 0–0.5 s and its ramp is over
    // at 1 s, which is where the bed starts (offset 2 source seconds at 2×): flat. Ignoring the
    // trim (0.5–1 s) or the speed (0–1 s) would put speech under the bed's first instant.
    const out = withVolumes(
      ducked({ anchor: { clipId: 'clip-1', offset: 2 } }, { sourceStart: 1, speed: 2 }),
      json(layer('bed', { sourceStart: 2, sourceDuration: 3 })),
    );
    expect(frames(out)).toEqual([]);
  });

  it('does not duck under a muted clip, under itself, or under another ducking sound', () => {
    const compiled = json(layer('bed', { sourceStart: 2, sourceDuration: 4 }));
    expect(frames(withVolumes(ducked({}, { muted: true }), compiled))).toEqual([]);
    // The bed's own source has speech at 2–4 s, and a second sound plays that source from the
    // film's start: the bed ducks under that sound, never under itself — and not at all once that
    // sound ducks too.
    const other = sound({ id: 'other', anchor: { clipId: 'clip-1', offset: 0 }, sourceStart: 0 });
    const withOther = (over: Partial<AudioItem>): VideoProject => {
      const base = ducked({}, { muted: true });
      return {
        ...base,
        sources: base.sources.map((source) =>
          source.id === 'src-m' ? { ...source, speech: [[2, 4]] } : source,
        ),
        audio: [...(base.audio ?? []), { id: 'lane-2', items: [{ ...other, ...over }] }],
      };
    };
    expect(frames(withVolumes(withOther({ ducking: { amountDb: -6, ramp: 1 } }), compiled))).toEqual([]);
    expect(frames(withVolumes(withOther({}), compiled))).not.toEqual([]);
  });

  it('keeps a sound flat when nothing it ducks under has speech', () => {
    const quiet = project([sound({ ducking: { amountDb: -12, ramp: 0.5 } })]);
    const heard: VideoProject = {
      ...quiet,
      sources: quiet.sources.map((source) => (source.id === 'src-a' ? { ...source, speech: [] } : source)),
    };
    expect(frames(withVolumes(heard, json(layer('bed', { sourceStart: 2 }))))).toEqual([]);
  });

  it('names the sources a ducking sound has not heard yet, once each', () => {
    const base = project([sound({ ducking: { amountDb: -12, ramp: 0.5 } }), sound({ id: 'voice', anchor: { clipId: 'clip-1', offset: 3 }, sourceStart: 0, duration: 1 })]);
    expect(unheardSources(base, 'bed').map((source) => source.id)).toEqual(['src-a', 'src-m']);
    expect(unheardSources(ducked(), 'bed').map((source) => source.id)).toEqual([]);
    const muted = project([sound()], { muted: true });
    expect(unheardSources(muted, 'bed')).toEqual([]);
  });
});
```

(`project(items)` builds the data without validating it, so the voice sharing the bed's lane is fine for `unheardSources`, which reads sources, not placement.)

Run: `MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/vtf.sh src/videoStudio/__tests__/videoflow_audio.test.ts`
Expected: FAIL — `unheardSources` is not exported; the bed has no ducking keyframes.

- [ ] **Step 9: Write it**

In `videoflow_audio.ts`:
- import `clipTimelineDuration` and `type ProjectSource` from `./project` (beside `clipStarts`, `sourceOf`, …), and `type Ducking` beside `volumeKeyframes, type CurveInput`;
- `type Loudness = Pick<CurveInput, 'volume' | 'fadeIn' | 'fadeOut' | 'envelope' | 'duck'>;`
- add, above `loudnessByLayer`:

```ts
/** A source playing somewhere in the film: the timeline window it plays over, from which point of
 *  the source, how fast. */
interface Player {
  readonly source: ProjectSource;
  readonly start: number;
  readonly end: number;
  readonly sourceStart: number;
  readonly speed: number;
}

/** What a ducking sound goes down under: every unmuted clip with a sound, and every unmuted sound
 *  that does not duck itself — a bed does not duck under another bed — but never the sound itself. */
function playersBesides(project: VideoProject, itemId: string): Player[] {
  const starts = clipStarts(project);
  const clips = project.video.flatMap((clip, index): Player[] => {
    const source = sourceOf(project, clip.sourceId);
    if (clip.muted || source?.kind !== 'video' || source.hasAudio === false) return [];
    const start = starts[index] ?? 0;
    const end = start + clipTimelineDuration(clip);
    return [{ source, start, end, sourceStart: clip.sourceStart, speed: clip.speed ?? 1 }];
  });
  const sounds = audioTracks(project)
    .flatMap((track) => track.items)
    .flatMap((item): Player[] => {
      const source = sourceOf(project, item.sourceId);
      if (item.id === itemId || item.muted || item.ducking !== undefined || source === undefined) {
        return [];
      }
      const window = audioWindow(project, item);
      return [{ source, ...window, sourceStart: item.sourceStart, speed: item.speed ?? 1 }];
    });
  return [...clips, ...sounds];
}

/** The sources a ducking sound goes down under whose speech nobody has listened for yet. */
export function unheardSources(project: VideoProject, itemId: string): ProjectSource[] {
  const unheard = new Map<string, ProjectSource>();
  for (const { source } of playersBesides(project, itemId)) {
    if (source.speech === undefined) unheard.set(source.id, source);
  }
  return [...unheard.values()];
}

/** The speech the film carries besides this sound, on the film's clock, merged: each source's
 *  speech mapped through every window that plays it (spec §Compile). */
function speechBesides(project: VideoProject, itemId: string): [number, number][] {
  const windows = playersBesides(project, itemId)
    .flatMap((player) =>
      (player.source.speech ?? []).map(([from, to]): [number, number] => [
        Math.max(player.start, player.start + (from - player.sourceStart) / player.speed),
        Math.min(player.end, player.start + (to - player.sourceStart) / player.speed),
      ]),
    )
    .filter(([from, to]) => to > from)
    .sort((a, b) => a[0] - b[0]);
  const merged: [number, number][] = [];
  for (const window of windows) {
    const last = merged.at(-1);
    if (last !== undefined && window[0] <= last[1]) last[1] = Math.max(last[1], window[1]);
    else merged.push(window);
  }
  return merged;
}

/** A sound's ducking on its own clock: the speech besides it, measured from where it starts. */
function duckingOf(project: VideoProject, item: AudioItem): Ducking | undefined {
  if (item.ducking === undefined) return undefined;
  const start = audioWindow(project, item).start;
  return {
    ...item.ducking,
    windows: speechBesides(project, item.id).map(([from, to]) => [from - start, to - start] as const),
  };
}
```

- in `loudnessByLayer`, the sound's entry gains `duck: duckingOf(project, item),` (import `type AudioItem` from `./project`).
- the header gains: "A ducking sound's curve goes down under the speech every other audible source carries (`speechBesides`), mapped from source seconds through each clip and sound that plays it."

`videoflow_audio.ts` is ~200 lines after this; `volumeCurve.ts` ~110.

Run the Step 8 command. Expected: PASS.

- [ ] **Step 10: The strings**

`videoStudioAudioEn` gains:

```ts
  ducking: 'Lower under speech',
  duckAmount: 'Lower by',
  duckSoftness: 'Softness',
  listening: 'Listening for speech…',
  listenFailed: 'Speech detection failed: {{reason}}',
  listenAgain: 'Listen to the sounds added since',
```

`videoStudioAudioIt` gains:

```ts
  ducking: 'Abbassa sotto il parlato',
  duckAmount: 'Abbassa di',
  duckSoftness: 'Morbidezza',
  listening: 'Ricerca del parlato…',
  listenFailed: 'Rilevamento del parlato non riuscito: {{reason}}',
  listenAgain: 'Ascolta i suoni aggiunti dopo',
```

- [ ] **Step 11: The controls — failing tests**

In `Inspector_audioClean.test.tsx`, import `DuckingControls` beside `NoiseReductionSwitch`, mock the detector beside the other mocks:

```tsx
const hearing = vi.hoisted(() => ({
  detect: vi.fn<(url: string, signal?: AbortSignal) => Promise<(readonly [number, number])[]>>(),
}));
vi.mock('../audioSpeech', () => ({ detectSpeech: hearing.detect }));
```

reset it in `beforeEach` (`hearing.detect.mockReset(); hearing.detect.mockResolvedValue([[1, 2]]);`), and add:

```tsx
function mountDucking(project: VideoProject) {
  const edits: Edit[] = [];
  const item = project.audio?.[0]?.items[0];
  if (item === undefined) throw new Error('fixture');
  const view = render(
    <DuckingControls project={project} item={item} onCommand={(edit) => edits.push(edit)} />,
  );
  return { ...view, edits, apply: (index = 0) => edits[index]?.(project) };
}

function heard(project: VideoProject, sourceId: string, speech: (readonly [number, number])[]): VideoProject {
  return {
    ...project,
    sources: project.sources.map((source) => (source.id === sourceId ? { ...source, speech } : source)),
  };
}

describe('DuckingControls', () => {
  it('listens to the clip’s sound, then records it and turns ducking on in one edit', async () => {
    const { edits, apply } = mountDucking(film());
    fireEvent.click(screen.getByRole('switch', { name: 'videoStudio.audio.ducking' }));
    expect((await screen.findByRole('status')).textContent).toBe('videoStudio.audio.listening');
    await waitFor(() => {
      expect(edits).toHaveLength(1);
    });
    expect(hearing.detect).toHaveBeenCalledWith('/api/assets/a/download', expect.any(AbortSignal));
    const next = apply();
    expect(next?.sources.find((source) => source.id === 'src-a')?.speech).toEqual([[1, 2]]);
    expect(next?.audio?.[0]?.items[0]?.ducking).toEqual({ amountDb: -12, ramp: 0.5 });
  });

  it('records no speech as heard, so a silent film is not listened to again', async () => {
    hearing.detect.mockResolvedValue([]);
    const { apply, edits } = mountDucking(film());
    fireEvent.click(screen.getByRole('switch', { name: 'videoStudio.audio.ducking' }));
    await waitFor(() => {
      expect(edits).toHaveLength(1);
    });
    expect(apply()?.sources.find((source) => source.id === 'src-a')?.speech).toEqual([]);
  });

  it('turns on at once when everything has been heard, and with nothing to listen to', () => {
    const muted: VideoProject = { ...film(), video: film().video.map((clip) => ({ ...clip, muted: true })) };
    const { apply } = mountDucking(muted);
    fireEvent.click(screen.getByRole('switch', { name: 'videoStudio.audio.ducking' }));
    expect(hearing.detect).not.toHaveBeenCalled();
    expect(apply()?.audio?.[0]?.items[0]?.ducking).toEqual({ amountDb: -12, ramp: 0.5 });
  });

  it('sets the amount and the softness, and turns off', () => {
    const on = heard(film({ ducking: { amountDb: -12, ramp: 0.5 } }), 'src-a', [[1, 2]]);
    const { apply } = mountDucking(on);
    fireEvent.keyDown(within(screen.getByLabelText('videoStudio.audio.duckAmount')).getByRole('slider'), {
      key: 'ArrowLeft',
    });
    expect(apply(0)?.audio?.[0]?.items[0]?.ducking).toEqual({ amountDb: -13, ramp: 0.5 });
    fireEvent.keyDown(within(screen.getByLabelText('videoStudio.audio.duckSoftness')).getByRole('slider'), {
      key: 'ArrowRight',
    });
    expect(apply(1)?.audio?.[0]?.items[0]?.ducking?.ramp).toBeCloseTo(0.6, 9);
    fireEvent.click(screen.getByRole('switch', { name: 'videoStudio.audio.ducking' }));
    expect(apply(2)?.audio?.[0]?.items[0]?.ducking).toBeUndefined();
  });

  it('offers to listen to a sound added after ducking was turned on, and records only what it hears', async () => {
    const on = film({ ducking: { amountDb: -12, ramp: 0.5 } });
    const { apply, edits } = mountDucking(on);
    fireEvent.click(screen.getByRole('button', { name: 'videoStudio.audio.listenAgain' }));
    await waitFor(() => {
      expect(edits).toHaveLength(1);
    });
    const next = apply();
    expect(next?.sources.find((source) => source.id === 'src-a')?.speech).toEqual([[1, 2]]);
    expect(next?.audio?.[0]?.items[0]?.ducking).toEqual({ amountDb: -12, ramp: 0.5 });
  });

  it('says why listening failed and leaves ducking off', async () => {
    hearing.detect.mockRejectedValue(new Error('Unable to decode audio data'));
    const { edits } = mountDucking(film());
    fireEvent.click(screen.getByRole('switch', { name: 'videoStudio.audio.ducking' }));
    expect((await screen.findByRole('alert')).textContent).toBe(
      'videoStudio.audio.listenFailed Unable to decode audio data',
    );
    expect(edits).toHaveLength(0);
  });

  it('writes nothing when it goes away while listening', async () => {
    let signal: AbortSignal | undefined;
    hearing.detect.mockImplementation((_url, given) => {
      signal = given;
      return new Promise(() => undefined);
    });
    const { edits, unmount } = mountDucking(film());
    fireEvent.click(screen.getByRole('switch', { name: 'videoStudio.audio.ducking' }));
    await waitFor(() => {
      expect(signal).toBeDefined();
    });
    unmount();
    expect(signal?.aborted).toBe(true);
    expect(edits).toHaveLength(0);
  });
});
```

(import `within` from `@testing-library/react`.) Run: `MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/vtf.sh src/videoStudio/__tests__/Inspector_audioClean.test.tsx`
Expected: FAIL — `DuckingControls` is not exported.

- [ ] **Step 12: Write the controls, and mount them**

In `Inspector_audioClean.tsx`, add the imports (`AudioDucking`, `AudioItem` types from `./project`; `unheardSources` from `./videoflow_audio`; `Slider` from `@/components/ui/slider`) and:

```tsx
/** Ducking as it is first turned on: the spec's defaults (spec §T7). */
const DEFAULT_DUCKING: AudioDucking = { amountDb: -12, ramp: 0.5 };

interface DuckingControlsProps {
  readonly project: VideoProject;
  readonly item: AudioItem;
  readonly onCommand: Commit;
}

/** Lower this sound under the speech the rest of the film carries. The speech of every source it
 *  goes down under is listened for once and recorded on the source; a source added later is offered
 *  to be listened to rather than silently left out of the curve. */
export function DuckingControls({ project, item, onCommand }: DuckingControlsProps) {
  const { t } = useTranslation();
  const { assetUrl } = useAssetSource();
  const id = useId();
  const { state, run } = useAnalysis();
  const ducking = item.ducking;
  const unheard = unheardSources(project, item.id);

  const duck = (current: VideoProject, next: AudioDucking | null) =>
    setAudioProperties(current, { itemId: item.id, ducking: next });

  /** Listens to every unheard source, then makes ONE edit: what it heard, then `then`. */
  function listen(then: (current: VideoProject) => VideoProject) {
    void run(async (signal) => {
      const { detectSpeech } = await import('./audioSpeech');
      const heard: { sourceId: string; speech: (readonly [number, number])[] }[] = [];
      for (const source of unheard) {
        heard.push({ sourceId: source.id, speech: await detectSpeech(assetUrl(source.assetId), signal) });
      }
      signal.throwIfAborted();
      onCommand((current) =>
        then(heard.reduce((next, analysis) => recordAnalysis(next, analysis), current)),
      );
    });
  }

  function turn(on: boolean) {
    if (!on) {
      onCommand((current) => duck(current, null));
      return;
    }
    if (unheard.length === 0) {
      onCommand((current) => duck(current, DEFAULT_DUCKING));
      return;
    }
    listen((current) => duck(current, DEFAULT_DUCKING));
  }

  return (
    <div className="grid gap-2">
      <div className="flex items-center gap-2 text-xs text-text-muted">
        <Switch
          id={id}
          checked={ducking !== undefined}
          disabled={state.state === 'working'}
          aria-label={t('videoStudio.audio.ducking')}
          onCheckedChange={turn}
        />
        <label htmlFor={id}>{t('videoStudio.audio.ducking')}</label>
      </div>
      {ducking === undefined ? null : (
        <>
          <label className="video-studio-adjustment">
            <span>{t('videoStudio.audio.duckAmount')}</span>
            <span className="font-mono tabular-nums">{ducking.amountDb} dB</span>
            <Slider
              key={ducking.amountDb}
              aria-label={t('videoStudio.audio.duckAmount')}
              min={-24}
              max={-3}
              step={1}
              defaultValue={[ducking.amountDb]}
              onValueCommit={([amountDb = ducking.amountDb]) => {
                onCommand((current) => duck(current, { ...ducking, amountDb }));
              }}
            />
          </label>
          <label className="video-studio-adjustment">
            <span>{t('videoStudio.audio.duckSoftness')}</span>
            <span className="font-mono tabular-nums">{ducking.ramp.toFixed(1)}s</span>
            <Slider
              key={ducking.ramp}
              aria-label={t('videoStudio.audio.duckSoftness')}
              min={0.1}
              max={2}
              step={0.1}
              defaultValue={[ducking.ramp]}
              onValueCommit={([ramp = ducking.ramp]) => {
                onCommand((current) => duck(current, { ...ducking, ramp: Math.round(ramp * 10) / 10 }));
              }}
            />
          </label>
          {unheard.length === 0 || state.state === 'working' ? null : (
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => {
                listen((current) => current);
              }}
            >
              {t('videoStudio.audio.listenAgain')}
            </Button>
          )}
        </>
      )}
      {state.state === 'working' ? (
        <p role="status" className="text-xs text-text-muted">
          {t('videoStudio.audio.listening')}
        </p>
      ) : null}
      {state.state === 'failed' ? (
        <p role="alert" className="text-xs text-danger">
          {t('videoStudio.audio.listenFailed', { reason: state.reason })}
        </p>
      ) : null}
    </div>
  );
}
```

(import `Button` from `@/components/ui/button`. `Math.round(ramp * 10) / 10` keeps a slider's float step — 0.6000000000000001 — from reaching `checkRanges` and the saved file as noise. The file stays under 300 lines.)

In `Inspector_audio.tsx`, the sound's Audio tab renders, after the `NoiseReductionSwitch`:

```tsx
          <DuckingControls key={`duck-${item.id}`} project={project} item={item} onCommand={onCommand} />
```

(import `DuckingControls` beside `NoiseReductionSwitch`).

Run the Step 11 command, then `MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/vtf.sh src/videoStudio`
Expected: PASS (except the known WSL-only `videoflow_fonts.test.ts`).

- [ ] **Step 13: The ducking E2E**

`web/e2e/video-studio-ducking.spec.ts`:

```ts
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { expect, test } from './support/assetCleanup';
import { uploadAsset } from './support/assetUpload';
import { levelBetween, windowPowers, WINDOW } from './support/audioMeasure';
import { AUDIO_FIXTURES, exportTo, reopen, silentFilm, uploadClip } from './support/videoStudio';

// video-studio-ducking.spec.ts — the music ducked under a voice, proven by the file. The voice is
// speech.wav at 1 % volume on a lane of its own: loud enough to be a source the music ducks under,
// quiet enough (−58 dBFS) that every level measured is the music's. S4's detector finds the first
// two phrases at 0.99–3.72 s and 5.16–7.92 s; with a 0.5 s softness the music is fully up before
// 0.49 s and between 4.22 and 4.66 s, and fully down inside 1.3–3.4 s and 5.5–7.6 s.

const SOFTNESS = 0.5;

test('the music goes 12 dB down under speech, over ramps as long as the softness', async ({ page }, info) => {
  test.skip(info.project.name.startsWith('mobile'), 'the phone shows the same inspector controls');
  test.setTimeout(15 * 60_000);
  const clip = await uploadClip(page);
  const music = await uploadAsset(page, resolve(AUDIO_FIXTURES, 'music.wav'), 'music.wav', 'audio/wav', { use: 'media' });
  const speech = await uploadAsset(page, resolve(AUDIO_FIXTURES, 'speech.wav'), 'speech.wav', 'audio/wav', { use: 'media' });
  const film = silentFilm(clip, 'ducking check');
  const item = (id: string, sourceId: string, volume: number) => ({
    id,
    sourceId,
    anchor: { clipId: 'clip-1', offset: 0 },
    sourceStart: 0,
    duration: 8,
    volume,
    muted: false,
  });
  const editor = await reopen(
    page,
    {
      ...film,
      sources: [
        ...film.sources,
        { id: 'src-m', assetId: music, kind: 'audio', duration: 8, size: { width: 0, height: 0 } },
        { id: 'src-s', assetId: speech, kind: 'audio', duration: 12.905, size: { width: 0, height: 0 } },
      ],
      audio: [
        { id: 'lane-a', items: [item('bed', 'src-m', 1)] },
        { id: 'lane-b', items: [item('voice', 'src-s', 0.01)] },
      ],
    },
    clip,
  );
  await editor.getByRole('button', { name: 'Sound 1' }).click();
  const inspector = editor.getByRole('region', { name: 'Properties' });
  await inspector.getByRole('tab', { name: 'Audio' }).click();
  await inspector.getByRole('switch', { name: 'Lower under speech' }).click();
  await expect(inspector.getByRole('status')).toHaveText('Listening for speech…');
  await expect(inspector.getByRole('switch', { name: 'Lower under speech' })).toBeChecked({ timeout: 5 * 60_000 });
  await expect(editor.getByRole('alert')).toHaveCount(0);
  await info.attach('ducking-inspector', { contentType: 'image/png', body: await page.screenshot() });

  const powers = await windowPowers(page, readFileSync(await exportTo(page, editor, info, 'ducked.mp4')));
  const mean = (a: number, b: number) => (a + b) / 2;
  const up = mean(levelBetween(powers, 0.1, 0.4), levelBetween(powers, 4.3, 4.6));
  const down = mean(levelBetween(powers, 1.3, 3.4), levelBetween(powers, 5.5, 7.6));
  // The ramp into the first phrase: from the first window 1 dB under "up" to the first within 1 dB
  // of "down". A step crosses in one window (≤ 0.1 s). A 0.5 s ramp, linear in gain from 1 to
  // −12 dB, spends 81 % of its length between those two levels — 0.41 s — and the 100 ms windows
  // blur that by one: at least 0.3 s is a ramp as long as the softness, measured.
  const level = (index: number) => 10 * Math.log10(Math.max(powers[index] ?? 0, 1e-20));
  const first = (from: number, below: number) => {
    for (let index = Math.round(from / WINDOW); index < powers.length; index += 1) {
      if (level(index) < below) return index * WINDOW;
    }
    throw new Error(`the level never went under ${String(below)} dB`);
  };
  const leaving = first(0.4, up - 1);
  const arrived = first(0.4, down + 1);
  const levels = { up, down, depth: up - down, ramp: arrived - leaving };
  await info.attach('ducking-levels', { contentType: 'application/json', body: JSON.stringify(levels, null, 2) });
  expect(levels.depth).toBeGreaterThanOrEqual(10);
  expect(levels.depth).toBeLessThanOrEqual(14);
  expect(levels.ramp).toBeGreaterThanOrEqual(SOFTNESS - 0.2);
});
```

- [ ] **Step 14: RED on the VM's current image**

`MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/e2e-vm.sh video-studio-ducking.spec.ts --project=chrome --reporter=line`
Expected: FAIL — no switch named "Lower under speech".

- [ ] **Step 15: Static checks, coverage, commit, dist, push, GREEN**

`webcheck.sh` on the touched files; coverage ≥ 85 % lines on `speechWindows.ts`, `audioSpeech.ts`, `volumeCurve.ts`, `videoflow_audio.ts`, `Inspector_audioClean.tsx`, `Inspector_audio.tsx`. After `build.sh`, `ls internal/webui/dist/assets | grep fvad` lists the WASM as a file of its own, reached only from the lazy `audioSpeech` chunk.

```bash
git add web/src/videoStudio/fvad-wasm.d.ts web/src/videoStudio/speechWindows.ts web/src/videoStudio/audioSpeech.ts web/src/videoStudio/__tests__/speechWindows.test.ts web/src/videoStudio/__tests__/audioSpeech.test.ts web/e2e/video-studio-ducking.spec.ts
git commit -m "feat(video-studio): duck a sound under the speech the rest of the film carries" -- web/package.json web/package-lock.json web/src/videoStudio/fvad-wasm.d.ts web/src/videoStudio/speechWindows.ts web/src/videoStudio/audioSpeech.ts web/src/videoStudio/volumeCurve.ts web/src/videoStudio/videoflow_audio.ts web/src/videoStudio/Inspector_audioClean.tsx web/src/videoStudio/Inspector_audio.tsx web/src/i18n/resources.videoStudioAudio.ts web/src/videoStudio/__tests__/speechWindows.test.ts web/src/videoStudio/__tests__/audioSpeech.test.ts web/src/videoStudio/__tests__/volumeCurve.test.ts web/src/videoStudio/__tests__/videoflow_audio.test.ts web/src/videoStudio/__tests__/Inspector_audioClean.test.tsx web/e2e/video-studio-ducking.spec.ts
```

`build.sh`, commit the dist, push, CI green (a full Stryker run: the lockfile changed; `volumeCurve.ts` and `videoflow_audio.ts` ≥ 70 % killed), `waitfix.sh <dist sha>`, then the whole plan's E2E on the VM in one run: `e2e-vm.sh video-studio-ducking.spec.ts video-studio-denoise.spec.ts video-studio-voice.spec.ts video-studio-envelope.spec.ts video-studio-lane.spec.ts video-studio-extract.spec.ts --reporter=line,json` GREEN. Read every levels attachment; look at every screenshot.
