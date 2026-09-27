# Video Studio audio — Plan B (audio lanes, the mix, Extract audio) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the Studio audio lanes that export exactly what the operator hears: sounds hung on clips that ride the ripple, volume and fades that the mixer actually applies, a clip Volume slider that finally works, and Extract audio.

**Architecture:** The pure core grows three modules: `audioLane.ts` (a sound's window in project time and how it re-anchors when the video lane is resliced), `commands_audio.ts` (the audio commands, same contract as `commands.ts`) and `volumeCurve.ts` (gain over time as VideoFlow keyframes). `videoflow_audio.ts` turns audio items into `AudioLayer`s and writes every audible layer's volume into the compiled JSON's `animations`, the only place the mixer reads it (S1). The UI adds an audio lane under the video lane, an upload door for sounds, an inspector for audio items and the Extract audio button. Plan C (T4–T7: waveform + envelope, TTS + record, denoise, ducking) is written after this plan runs, against the code it produces.

**Tech Stack:** TypeScript/React 19 + Vite 8, VideoFlow 1.3.4 (`@videoflow/core`, `renderer-browser`, `renderer-dom`), dnd-timeline 3.1.1, mediabunny 1.58.1, Radix Slider, react-i18next, vitest, Playwright 1.62.1.

**Spec:** `docs/superpowers/specs/2026-09-27-video-studio-audio-design.md` (with the S1–S4 measurements in `spikes/video-studio-audio/FINDINGS.md`).

## Global Constraints

- **Never run a Windows executable.** `node`, `npm`, `npx`, `go` in Git Bash are Windows exes: every such command runs **in WSL** from a script file (`MSYS_NO_PATHCONV=1 wsl bash /mnt/c/.../script.sh`), with `export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"`.
- **Shared tree:** stage and commit explicit paths only (`git commit -- <paths>`, `git add <new file>` first); never `git add -A` outside `internal/webui/dist`, never `git reset` the shared HEAD, **never `--no-verify`**. Push from WSL: `LEFTHOOK_BIN=$HOME/go/bin/lefthook git -c core.hooksPath=.git/hooks push origin master`.
- **Files ≤ 600 lines**; refactor on touch. `commands.ts` (538) grows only by the re-anchor wiring; `VideoStudio.tsx` (547) sheds its pure helpers before it grows; `Inspector_clip.tsx` (531) sheds its audio controls.
- **The core stays pure** (spec §"The whole job"): `project.ts`, `audioLane.ts`, `commands*.ts`, `volumeCurve.ts` and `videoflow_audio.ts` import no DOM, Web Audio or `fetch`.
- Ranges (spec §Model): item volume 0–2, envelope gain 0–1, fades 0–5 s each and their sum ≤ the item's timeline length, speed 0.25–4, ducking −24..−3 dB over 0.1–2 s.
- Volume reaches the mix **only** through `animations: [{property: 'volume', keyframes}]` in the compiled VideoJSON; keyframe times are absolute source seconds starting at the layer's own `sourceStart`; gains step between keyframes, so ramps are sampled every `CURVE_STEP` (S1).
- Every user-facing string in English and Italian, in `web/src/i18n/resources.videoStudioAudio.ts` (merged under `videoStudio.audio`). Refusal keys live there too.
- Unit gates: vitest coverage ≥ 85 % on every touched file; oxlint (`npm run lint`, read "Found N errors") + `node scripts/lint-contract.mjs` + `npx prettier --check .` + `npm run typecheck` + `npm run deadcode` green; `npm run build` refreshes `internal/webui/dist`, committed separately.
- **Per-task E2E protocol (spec §Tasks):** commit on `master`, push, CI green including *Publish Aura edge image*, wait until the VM runs an image containing the commit (`wait-contains.sh <full sha>`), run the task's spec from WSL against `https://192.168.101.158` (`e2e-vm.sh`), read the measured numbers and the screenshots, and confirm the run left no asset behind (the `assetCleanup` auto fixture fails the run otherwise). Task 1 is pure core with no UI surface: it rides Task 2's push and E2E (ruling in the ledger).
- The VM account is the operator's own; never print its credentials, never create an identity.
- A new E2E is run **RED against the VM image before the task lands** whenever the old image can load its fixtures, so the E2E is seen to fail for the right reason.

## Review Focus

1. **A trim, split or removal on the video lane under a sound.** The sound never disappears (except one extracted from a clip removed whole) and never leaves the saved file unloadable: its anchor must name a clip that exists, or `projectStore` refuses the file. (Task 1, `audioLane.test.ts`.)
2. **A sound longer than the film, or starting near its end.** It is cut at the project's end, never lengthens the project, and a drop at or past the end is refused rather than parked on an invisible zero-length window. (Task 1 unit; Task 2 E2E measures the export's length.)
3. **Fades that no longer fit after a trim or a speed change.** The command clamps or refuses; a curve is never built with `fadeIn + fadeOut > length`, which would make the gain dip twice. (Task 1.)
4. **A clip whose Volume is 50 %.** It exports 6 dB lower than at 100 %: the static `volume` the Studio sent before was ignored by the mixer (S1's live bug). (Task 2 E2E, run RED on the old image first.)
5. **An audio item selected when the operator presses Split or Delete.** Split cuts the sound at the playhead, Delete removes the sound; neither touches the clip underneath. (Task 3.)

---

### Task 1: The audio core — lane geometry, re-anchoring and commands

**Files:**
- Create: `web/src/videoStudio/audioLane.ts`
- Create: `web/src/videoStudio/commands_audio.ts`
- Create: `web/src/i18n/resources.videoStudioAudio.ts`
- Modify: `web/src/videoStudio/commands.ts` (export `EPSILON`; `Placement` from `audioLane`; re-anchor audio in `resliceLane` and `removeItem`; `removeItem` removes an audio item)
- Modify: `web/src/videoStudio/project.ts` (the `EnvelopePoint.time` comment names its domain)
- Modify: `web/src/i18n/resources.videoStudio.ts` (merge `audio`)
- Test: `web/src/videoStudio/__tests__/audioLane.test.ts`, `web/src/videoStudio/__tests__/commands_audio.test.ts`

**Interfaces:**
- Produces (`audioLane.ts`): `interface Placement {id; from; to}`, `audioLength(item): number`, `audioWindow(project, item): {start; end}`, `anchorAt(project, time): OverlayAnchor | undefined`, `audioLaneIsBusy(project, track, span, exceptId?)`, `freeAudioTrack(project, span, exceptId?): string | undefined`, `reanchorAudio(before, after, placements): readonly AudioTrack[] | undefined`, `withoutAudioItem(project, itemId): VideoProject | undefined`, `findAudioItem(project, itemId): AudioItem | undefined`.
- Produces (`commands_audio.ts`): `addAudio(project, {sourceId, time, label?})`, `moveAudio(project, {itemId, start, trackId?})`, `trimAudio(project, {itemId, start, end})` (source seconds from the item's current `sourceStart`, like `trimClip`), `splitAudio(project, {itemId, time})`, `setAudioProperties(project, {itemId, volume?, muted?, fadeIn?, fadeOut?, speed?, denoise?, ducking?: AudioDucking | null})`, `setEnvelope(project, {itemId, points})`, `recordAnalysis(project, {sourceId, speech?, denoisedAssetId?})`, `AUDIO_REFUSAL` (the i18n keys).
- `EnvelopePoint.time` is **source seconds from the item's `sourceStart`**: a point stays on the sound it was set on when the speed changes, and it is the time domain of wavesurfer's envelope over the item's waveform (Plan C).

- [ ] **Step 1: Write the failing lane tests**

`web/src/videoStudio/__tests__/audioLane.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import { anchorAt, audioLength, audioWindow, freeAudioTrack, reanchorAudio } from '../audioLane';
import { removeItem, removeRange, splitAt, trimClip } from '../commands';
import type { AudioItem, VideoProject } from '../project';

// Two 4 s clips of one 10 s source, and a music bed hung on the second one second into it.
function project(item: Partial<AudioItem> = {}): VideoProject {
  return {
    id: 'p',
    name: 'demo',
    size: { width: 1920, height: 1080 },
    fps: 30,
    sources: [
      { id: 'src-a', assetId: 'a', kind: 'video', duration: 10, size: { width: 1920, height: 1080 } },
      { id: 'src-m', assetId: 'm', kind: 'audio', duration: 8, size: { width: 0, height: 0 } },
    ],
    video: [
      { id: 'clip-1', sourceId: 'src-a', duration: 4, sourceStart: 0, muted: false },
      { id: 'clip-2', sourceId: 'src-a', duration: 4, sourceStart: 4, muted: false },
    ],
    overlays: [],
    audio: [
      {
        id: 'lane-a',
        items: [
          {
            id: 'bed',
            sourceId: 'src-m',
            anchor: { clipId: 'clip-2', offset: 1 },
            sourceStart: 0,
            duration: 2,
            volume: 1,
            muted: false,
            ...item,
          },
        ],
      },
    ],
  };
}

function bed(next: VideoProject): AudioItem | undefined {
  return next.audio?.[0]?.items.find((item) => item.id === 'bed');
}

describe('audioWindow', () => {
  it('starts where the anchor puts it and lasts its source length over its speed', () => {
    expect(audioWindow(project(), bed(project())!)).toEqual({ start: 5, end: 7 });
    expect(audioLength({ ...bed(project())!, speed: 2 })).toBe(1);
  });

  it('is cut at the project end and never lengthens the project', () => {
    const long = project({ duration: 8 });
    expect(audioWindow(long, bed(long)!)).toEqual({ start: 5, end: 8 });
  });

  it('reads the anchor offset through the clip speed, like an overlay', () => {
    const fast = { ...project(), video: project().video.map((c) => ({ ...c, speed: 2 })) };
    // clip-2 starts at 2 s at double speed; offset 1 source second is half a timeline second.
    expect(audioWindow(fast, bed(fast)!).start).toBe(2.5);
  });
});

describe('anchorAt', () => {
  it('hangs a time on the clip under it', () => {
    expect(anchorAt(project(), 6)).toEqual({ clipId: 'clip-2', offset: 2 });
  });

  it('falls back to the last clip at its start past the last frame, and to nothing with no clip', () => {
    expect(anchorAt(project(), 99)).toEqual({ clipId: 'clip-2', offset: 0 });
    expect(anchorAt({ ...project(), video: [] }, 1)).toBeUndefined();
  });
});

describe('freeAudioTrack', () => {
  it('names a lane only where nothing covers the span, the item itself excepted', () => {
    expect(freeAudioTrack(project(), { start: 5.5, end: 6 })).toBeUndefined();
    expect(freeAudioTrack(project(), { start: 0, end: 5 })).toBe('lane-a');
    expect(freeAudioTrack(project(), { start: 5.5, end: 6 }, 'bed')).toBe('lane-a');
  });
});

describe('a sound follows the video lane', () => {
  it('rides the ripple when an earlier clip is trimmed', () => {
    const next = trimClip(project(), { clipId: 'clip-1', start: 1, end: 4 });
    expect(bed(next)?.anchor).toEqual({ clipId: 'clip-2', offset: 1 });
    expect(audioWindow(next, bed(next)!).start).toBe(4);
  });

  it('follows the half of a split clip that holds its offset', () => {
    const next = splitAt(project(), { time: 4.5 });
    const right = next.video[2];
    expect(bed(next)?.anchor).toEqual({ clipId: right?.id, offset: 0.5 });
    expect(audioWindow(next, bed(next)!).start).toBe(5);
  });

  it('keeps its project time when the content under its anchor is removed', () => {
    const next = removeRange(project(), { from: 4, to: 6 });
    // clip-2 lost its first two seconds; the sound started at 5 s and still does, on clip-2
    // (spec §Model: "re-anchors to whatever clip now covers the same project time").
    expect(bed(next)?.anchor).toEqual({ clipId: 'clip-2', offset: 1 });
    expect(audioWindow(next, bed(next)!).start).toBeCloseTo(5, 6);
  });

  it('re-anchors instead of disappearing when its clip is removed', () => {
    const next = removeItem(project(), { itemId: 'clip-2' });
    expect(bed(next)?.anchor).toEqual({ clipId: 'clip-1', offset: 0 });
  });

  it('goes with the clip it was extracted from, when that clip is removed whole', () => {
    const next = removeItem(project({ extractedFrom: 'clip-2' }), { itemId: 'clip-2' });
    expect(next.audio?.[0]?.items).toEqual([]);
  });

  it('empties the lanes when no clip is left to hang on', () => {
    const next = removeRange(project(), { from: 0, to: 8 });
    expect(next.audio?.[0]?.items).toEqual([]);
  });

  it('leaves a project saved before audio existed without an audio key', () => {
    const { audio: _audio, ...legacy } = project();
    expect(trimClip(legacy, { clipId: 'clip-1', start: 1, end: 4 }).audio).toBeUndefined();
    expect(reanchorAudio(legacy, legacy, new Map())).toBeUndefined();
  });

  it('removes an audio item by id and nothing else', () => {
    const next = removeItem(project(), { itemId: 'bed' });
    expect(next.audio?.[0]?.items).toEqual([]);
    expect(next.video).toHaveLength(2);
  });
});
```

- [ ] **Step 2: Write the failing command tests**

`web/src/videoStudio/__tests__/commands_audio.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import { audioWindow } from '../audioLane';
import { CommandRefusal } from '../commands';
import {
  addAudio,
  AUDIO_REFUSAL,
  moveAudio,
  recordAnalysis,
  setAudioProperties,
  setEnvelope,
  splitAudio,
  trimAudio,
} from '../commands_audio';
import { projectDuration, type AudioItem, type VideoProject } from '../project';

function project(items: AudioItem[] = []): VideoProject {
  return {
    id: 'p',
    name: 'demo',
    size: { width: 1920, height: 1080 },
    fps: 30,
    sources: [
      { id: 'src-a', assetId: 'a', kind: 'video', duration: 10, size: { width: 1920, height: 1080 }, hasAudio: true },
      { id: 'src-m', assetId: 'm', kind: 'audio', duration: 8, size: { width: 0, height: 0 } },
      { id: 'src-img', assetId: 'i', kind: 'image', duration: 0, size: { width: 8, height: 6 } },
      { id: 'src-mute', assetId: 's', kind: 'video', duration: 5, size: { width: 8, height: 6 }, hasAudio: false },
    ],
    video: [
      { id: 'clip-1', sourceId: 'src-a', duration: 4, sourceStart: 0, muted: false },
      { id: 'clip-2', sourceId: 'src-a', duration: 4, sourceStart: 4, muted: false },
    ],
    overlays: [],
    ...(items.length === 0 ? {} : { audio: [{ id: 'lane-a', items }] }),
  };
}

function item(over: Partial<AudioItem> = {}): AudioItem {
  return {
    id: 'bed',
    sourceId: 'src-m',
    anchor: { clipId: 'clip-1', offset: 0 },
    sourceStart: 0,
    duration: 4,
    volume: 1,
    muted: false,
    ...over,
  };
}

function refusalKey(run: () => unknown): string {
  try {
    run();
  } catch (error) {
    if (error instanceof CommandRefusal) return error.reasonKey;
    throw error;
  }
  throw new Error('the command did not refuse');
}

const only = (next: VideoProject) => next.audio?.flatMap((lane) => lane.items) ?? [];

describe('addAudio', () => {
  it('opens the first lane with the whole sound hung on the clip under the time', () => {
    const next = addAudio(project(), { sourceId: 'src-m', time: 5, label: 'Bed' });
    const [added] = only(next);
    expect(added).toMatchObject({ anchor: { clipId: 'clip-2', offset: 1 }, duration: 8, volume: 1, label: 'Bed' });
    expect(projectDuration(next)).toBe(8);
  });

  it('joins a lane free over its window and opens another where it is not', () => {
    const base = project([item({ duration: 2 })]);
    expect(addAudio(base, { sourceId: 'src-m', time: 3 }).audio).toHaveLength(1);
    expect(addAudio(base, { sourceId: 'src-m', time: 1 }).audio).toHaveLength(2);
  });

  it('plays the sound of a video source that has one', () => {
    expect(only(addAudio(project(), { sourceId: 'src-a', time: 0 }))[0]?.duration).toBe(10);
  });

  it('refuses a still, a silent clip, a missing source, a lane with no clip and a time past the end', () => {
    expect(refusalKey(() => addAudio(project(), { sourceId: 'src-img', time: 0 }))).toBe(AUDIO_REFUSAL.notSound);
    expect(refusalKey(() => addAudio(project(), { sourceId: 'src-mute', time: 0 }))).toBe(AUDIO_REFUSAL.notSound);
    expect(refusalKey(() => addAudio(project(), { sourceId: 'nope', time: 0 }))).toBe(AUDIO_REFUSAL.notSound);
    expect(refusalKey(() => addAudio({ ...project(), video: [] }, { sourceId: 'src-m', time: 0 }))).toBe(AUDIO_REFUSAL.noClip);
    expect(refusalKey(() => addAudio(project(), { sourceId: 'src-m', time: 8 }))).toBe(AUDIO_REFUSAL.pastEnd);
  });
});

describe('moveAudio', () => {
  it('re-hangs the sound on the clip under its new start', () => {
    const next = moveAudio(project([item()]), { itemId: 'bed', start: 6 });
    expect(only(next)[0]?.anchor).toEqual({ clipId: 'clip-2', offset: 2 });
  });

  it('refuses a drop over another sound on the lane, and one at or past the end', () => {
    const base = project([item({ duration: 2 }), item({ id: 'voice', anchor: { clipId: 'clip-2', offset: 0 }, duration: 2 })]);
    expect(refusalKey(() => moveAudio(base, { itemId: 'bed', start: 3.5 }))).toBe(AUDIO_REFUSAL.overlap);
    expect(refusalKey(() => moveAudio(base, { itemId: 'bed', start: 8 }))).toBe(AUDIO_REFUSAL.pastEnd);
  });

  it('moves to another lane when asked', () => {
    const base = { ...project([item()]), audio: [{ id: 'lane-a', items: [item()] }, { id: 'lane-b', items: [] }] };
    const next = moveAudio(base, { itemId: 'bed', start: 0, trackId: 'lane-b' });
    expect(next.audio?.map((lane) => lane.items.length)).toEqual([0, 1]);
  });
});

describe('trimAudio', () => {
  it('trims the head and keeps the sound aligned with the picture', () => {
    const next = trimAudio(project([item()]), { itemId: 'bed', start: 1, end: 4 });
    const [trimmed] = only(next);
    expect(trimmed).toMatchObject({ sourceStart: 1, duration: 3 });
    expect(audioWindow(next, trimmed!).start).toBe(1);
  });

  it('keeps envelope points on their material and drops the ones trimmed away', () => {
    const base = project([item({ envelope: [{ time: 0.5, gain: 1 }, { time: 2, gain: 0 }] })]);
    const [trimmed] = only(trimAudio(base, { itemId: 'bed', start: 1, end: 4 }));
    expect(trimmed?.envelope).toEqual([{ time: 1, gain: 0 }]);
  });

  it('clamps fades that no longer fit', () => {
    const [trimmed] = only(trimAudio(project([item({ fadeIn: 2, fadeOut: 2 })]), { itemId: 'bed', start: 0, end: 3 }));
    expect((trimmed?.fadeIn ?? 0) + (trimmed?.fadeOut ?? 0)).toBeLessThanOrEqual(3);
  });

  it('refuses a trim past the source, an empty one, and one that would start before the film', () => {
    const base = project([item()]);
    expect(refusalKey(() => trimAudio(base, { itemId: 'bed', start: 0, end: 9 }))).toBe(AUDIO_REFUSAL.trimPastSource);
    expect(refusalKey(() => trimAudio(base, { itemId: 'bed', start: 2, end: 2 }))).toBe(AUDIO_REFUSAL.trimPastSource);
    const late = project([item({ sourceStart: 2 })]);
    expect(refusalKey(() => trimAudio(late, { itemId: 'bed', start: -1, end: 4 }))).toBe(AUDIO_REFUSAL.beforeStart);
  });
});

describe('splitAudio', () => {
  it('cuts the sound in two at the time, each half keeping its own fade and envelope', () => {
    const base = project([item({ fadeIn: 1, fadeOut: 1, envelope: [{ time: 1, gain: 1 }, { time: 3, gain: 0.5 }] })]);
    const [left, right] = only(splitAudio(base, { itemId: 'bed', time: 2 }));
    expect(left).toMatchObject({ id: 'bed', duration: 2, fadeIn: 1, envelope: [{ time: 1, gain: 1 }] });
    expect(left?.fadeOut).toBeUndefined();
    expect(right).toMatchObject({ sourceStart: 2, duration: 2, fadeOut: 1, envelope: [{ time: 1, gain: 0.5 }] });
    expect(right?.fadeIn).toBeUndefined();
    expect(right?.id).not.toBe('bed');
  });

  it('refuses a cut on an edge of the sound', () => {
    expect(refusalKey(() => splitAudio(project([item()]), { itemId: 'bed', time: 0 }))).toBe(AUDIO_REFUSAL.splitOnBoundary);
    expect(refusalKey(() => splitAudio(project([item()]), { itemId: 'bed', time: 4 }))).toBe(AUDIO_REFUSAL.splitOnBoundary);
  });
});

describe('setAudioProperties', () => {
  it('sets volume, mute, fades, speed, denoise and ducking, and null turns ducking off', () => {
    const base = project([item()]);
    const next = setAudioProperties(base, {
      itemId: 'bed', volume: 0.5, muted: true, fadeIn: 1, fadeOut: 1, speed: 2, denoise: true, ducking: { amountDb: -12, ramp: 0.5 },
    });
    expect(only(next)[0]).toMatchObject({ volume: 0.5, muted: true, fadeIn: 1, fadeOut: 1, speed: 2, denoise: true, ducking: { amountDb: -12, ramp: 0.5 } });
    expect(only(setAudioProperties(next, { itemId: 'bed', ducking: null }))[0]?.ducking).toBeUndefined();
  });

  it('refuses fades longer than the sound, and a speed that runs it into its neighbour', () => {
    expect(refusalKey(() => setAudioProperties(project([item()]), { itemId: 'bed', fadeIn: 3, fadeOut: 2 }))).toBe(AUDIO_REFUSAL.fadesTooLong);
    const crowded = project([item({ duration: 2 }), item({ id: 'voice', anchor: { clipId: 'clip-1', offset: 2 }, duration: 2 })]);
    expect(refusalKey(() => setAudioProperties(crowded, { itemId: 'bed', speed: 0.5 }))).toBe(AUDIO_REFUSAL.overlap);
  });

  it('is loud about values no control can produce', () => {
    const base = project([item()]);
    expect(() => setAudioProperties(base, { itemId: 'bed', volume: 3 })).toThrow(/volume/);
    expect(() => setAudioProperties(base, { itemId: 'bed', speed: 9 })).toThrow(/speed/);
    expect(() => setAudioProperties(base, { itemId: 'bed', fadeIn: 6 })).toThrow(/fade/);
    expect(() => setAudioProperties(base, { itemId: 'bed', ducking: { amountDb: 0, ramp: 1 } })).toThrow(/ducking/);
    expect(() => setAudioProperties(base, { itemId: 'nope', volume: 1 })).toThrow(/no audio item/);
  });
});

describe('setEnvelope', () => {
  it('sorts the points, clamps them to the sound and to 0–1, and an empty list removes the envelope', () => {
    const next = setEnvelope(project([item()]), {
      itemId: 'bed',
      points: [{ time: 3, gain: 2 }, { time: -1, gain: 0.5 }, { time: Number.NaN, gain: 1 }],
    });
    expect(only(next)[0]?.envelope).toEqual([{ time: 0, gain: 0.5 }, { time: 3, gain: 1 }]);
    expect(only(setEnvelope(next, { itemId: 'bed', points: [] }))[0]?.envelope).toBeUndefined();
  });
});

describe('recordAnalysis', () => {
  it('writes merged, sorted speech windows inside the source, and the denoised asset', () => {
    const next = recordAnalysis(project(), {
      sourceId: 'src-m',
      speech: [[5, 9], [1, 2], [1.5, 3]],
      denoisedAssetId: 'clean',
    });
    expect(next.sources.find((source) => source.id === 'src-m')).toMatchObject({
      speech: [[1, 3], [5, 8]],
      denoisedAssetId: 'clean',
    });
  });

  it('refuses a source the project does not hold', () => {
    expect(refusalKey(() => recordAnalysis(project(), { sourceId: 'nope', denoisedAssetId: 'x' }))).toBe(AUDIO_REFUSAL.sourceMissing);
  });
});
```

- [ ] **Step 3: Run both to verify they fail**

Script `vt.sh` (already in the scratchpad) runs `npx vitest run "$@"` in `/mnt/d/Aura/web`:

Run: `MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/vt.sh src/videoStudio/__tests__/audioLane.test.ts src/videoStudio/__tests__/commands_audio.test.ts`
Expected: FAIL — `Failed to resolve import "../audioLane"` / `"../commands_audio"`.

- [ ] **Step 4: Write `audioLane.ts`**

```ts
// audioLane.ts — where a sound sits in project time, and how it follows the video lane.
//
// A sound hangs off a clip like an overlay and rides the ripple with it, but it may run past
// that clip — a music bed covers many — and it is cut at the project's end, never lengthening
// the film. The one rule that differs from an overlay is the one that matters: a video edit
// never makes a sound disappear. When the content under its anchor goes, it keeps its project
// time on whatever clip covers it now, or on the last clip.

import {
  audioTracks,
  clipAt,
  clipStart,
  projectDuration,
  type AudioItem,
  type AudioTrack,
  type OverlayAnchor,
  type VideoProject,
} from './project';

/** A surviving piece of a resliced clip, in source seconds from where that clip started, and
 *  the id it carries after the edit. `commands.ts` builds these; the audio lanes read them. */
export interface Placement {
  readonly id: string;
  readonly from: number;
  readonly to: number;
}

interface Span {
  readonly start: number;
  readonly end: number;
}

export function audioLength(item: AudioItem): number {
  return item.duration / Math.abs(item.speed ?? 1);
}

/** The sound's window in project time: from its anchor, and never past the project's end. */
export function audioWindow(project: VideoProject, item: AudioItem): Span {
  const clip = project.video.find((candidate) => candidate.id === item.anchor.clipId);
  const base = clipStart(project, item.anchor.clipId);
  if (clip === undefined || base === undefined) return { start: 0, end: 0 };
  const total = projectDuration(project);
  const start = Math.min(base + Math.max(0, item.anchor.offset) / Math.abs(clip.speed ?? 1), total);
  return { start, end: Math.min(start + audioLength(item), total) };
}

/** Where a sound starting at `time` hangs: the clip under it, or — past the last frame — the
 *  last clip at its start. Nothing only when there is no clip at all. */
export function anchorAt(project: VideoProject, time: number): OverlayAnchor | undefined {
  const at = Math.max(0, time);
  const clip = clipAt(project, at);
  if (clip !== undefined) {
    const start = clipStart(project, clip.id) ?? 0;
    return { clipId: clip.id, offset: (at - start) * Math.abs(clip.speed ?? 1) };
  }
  const last = project.video.at(-1);
  return last === undefined ? undefined : { clipId: last.id, offset: 0 };
}

export function findAudioItem(project: VideoProject, itemId: string): AudioItem | undefined {
  return audioTracks(project)
    .flatMap((track) => track.items)
    .find((item) => item.id === itemId);
}

/** Whether anything else on this lane covers `span`: the rule the commands enforce and the
 *  lane picker asks, from both sides, as for overlays. */
export function audioLaneIsBusy(
  project: VideoProject,
  track: AudioTrack,
  span: Span,
  exceptId?: string,
): boolean {
  return track.items.some((other) => {
    if (other.id === exceptId) return false;
    const window = audioWindow(project, other);
    return span.start < window.end && window.start < span.end;
  });
}

/** The first lane free over `span`, or `undefined` — which is what opens a new one. */
export function freeAudioTrack(
  project: VideoProject,
  span: Span,
  exceptId?: string,
): string | undefined {
  return audioTracks(project).find((track) => !audioLaneIsBusy(project, track, span, exceptId))
    ?.id;
}

/**
 * Re-hang every sound after the video lane was resliced. A sound whose clip was not touched is
 * left alone and rides the ripple. One whose clip survives follows the slice holding its offset.
 * One whose anchor content went keeps its project time on whatever clip covers it now — except a
 * sound extracted from a clip that went whole, which goes with it (spec §Model). With no clip
 * left there is nothing to hang on, and a sound anchored to nothing would make the saved file
 * unloadable, so the lanes empty. A project saved before audio existed stays without the key.
 */
export function reanchorAudio(
  before: VideoProject,
  after: VideoProject,
  placements: ReadonlyMap<string, readonly Placement[]>,
): readonly AudioTrack[] | undefined {
  if (before.audio === undefined) return undefined;
  const removedWhole = (clipId: string | undefined) =>
    clipId !== undefined && placements.get(clipId)?.length === 0;
  return before.audio.map((track) => ({
    ...track,
    items: track.items.flatMap((item): AudioItem[] => {
      if (after.video.length === 0 || removedWhole(item.extractedFrom)) return [];
      const placed = placements.get(item.anchor.clipId);
      if (placed === undefined) return [item];
      const place = placed.find(
        ({ from, to }) => item.anchor.offset >= from && item.anchor.offset < to,
      );
      if (place !== undefined) {
        return [{ ...item, anchor: { clipId: place.id, offset: item.anchor.offset - place.from } }];
      }
      const anchor = anchorAt(after, audioWindow(before, item).start);
      return anchor === undefined ? [] : [{ ...item, anchor }];
    }),
  }));
}

/** The project without the sound `itemId`, or `undefined` when no lane holds it. */
export function withoutAudioItem(project: VideoProject, itemId: string): VideoProject | undefined {
  if (findAudioItem(project, itemId) === undefined) return undefined;
  return {
    ...project,
    audio: audioTracks(project).map((track) => ({
      ...track,
      items: track.items.filter((item) => item.id !== itemId),
    })),
  };
}
```

- [ ] **Step 5: Wire it into `commands.ts`**

Export the tolerance (it is shared with `commands_audio.ts`), replace the local `Placement` with the lane's, and let the two lane-rebuilding paths carry the audio:

```ts
// near the top
import { reanchorAudio, withoutAudioItem, type Placement } from './audioLane';

export const EPSILON = 1e-6;   // was `const EPSILON`

// delete the local `interface Placement extends Slice { readonly id: string; }`

// resliceLane: replace the final return
  const next: VideoProject = {
    ...project,
    video: normalizeJunctions(video),
    overlays: reanchor(project.overlays, placements),
  };
  const audio = reanchorAudio(project, next, placements);
  return audio === undefined ? next : { ...next, audio };

// removeItem: the clip branch becomes
  if (clip !== undefined) {
    const next: VideoProject = {
      ...project,
      video: normalizeJunctions(project.video.filter((item) => item.id !== clip.id)),
      overlays: project.overlays.map((lane) => ({
        ...lane,
        items: lane.items.filter((item) => item.anchor.clipId !== clip.id),
      })),
    };
    const audio = reanchorAudio(project, next, new Map([[clip.id, []]]));
    return audio === undefined ? next : { ...next, audio };
  }
  const withoutSound = withoutAudioItem(project, args.itemId);
  if (withoutSound !== undefined) return withoutSound;
```

Update `removeItem`'s doc comment: "Remove a clip, an overlay or a sound. A clip takes the overlays anchored to it with it; the sounds on it re-anchor instead (audioLane.ts), except one extracted from it."

In `project.ts`, the `EnvelopePoint.time` comment becomes `// source seconds from the item's sourceStart: a point stays on its sound when the speed changes`.

- [ ] **Step 6: Write `commands_audio.ts`**

```ts
// commands_audio.ts — the audio lanes' commands. The same contract as commands.ts: a pure
// function from project to project, arguments as JSON, a refusal the UI can translate, and a
// caller out of step with the model is loud. Sub-project 2 exposes each one to Aura as a tool
// with these arguments, so none of them touches the DOM or carries a sentence.

import {
  anchorAt,
  audioLaneIsBusy,
  audioLength,
  audioWindow,
  freeAudioTrack,
} from './audioLane';
import { CommandRefusal, EPSILON } from './commands';
import {
  audioTracks,
  projectDuration,
  sourceOf,
  type AudioDucking,
  type AudioItem,
  type AudioTrack,
  type EnvelopePoint,
  type VideoProject,
} from './project';

export const AUDIO_REFUSAL = {
  noClip: 'videoStudio.audio.refusal.noClip',
  pastEnd: 'videoStudio.audio.refusal.pastEnd',
  beforeStart: 'videoStudio.audio.refusal.beforeStart',
  overlap: 'videoStudio.audio.refusal.overlap',
  notSound: 'videoStudio.audio.refusal.notSound',
  fadesTooLong: 'videoStudio.audio.refusal.fadesTooLong',
  sourceMissing: 'videoStudio.refusal.sourceMissing',
  trimPastSource: 'videoStudio.refusal.trimPastSource',
  splitOnBoundary: 'videoStudio.refusal.splitOnBoundary',
} as const;

const MAX_FADE = 5;

interface Located {
  readonly track: AudioTrack;
  readonly item: AudioItem;
}

function locateAudio(project: VideoProject, itemId: string): Located {
  for (const track of audioTracks(project)) {
    const item = track.items.find((candidate) => candidate.id === itemId);
    if (item !== undefined) return { track, item };
  }
  throw new Error(`videoStudio: no audio item named ${itemId}`);
}

/** Set or drop one optional field: under exactOptionalPropertyTypes an absent value is a
 *  missing key, never `undefined`. */
function withField<K extends keyof AudioItem>(
  item: AudioItem,
  key: K,
  value: AudioItem[K] | undefined,
): AudioItem {
  const { [key]: _dropped, ...rest } = item;
  return (value === undefined ? rest : { ...rest, [key]: value }) as AudioItem;
}

/** Replace `itemId` with `items` (one, or two after a split) on its lane, or move it to `toTrack`. */
function placeItems(
  project: VideoProject,
  itemId: string,
  items: readonly AudioItem[],
  toTrack?: string,
): VideoProject {
  return {
    ...project,
    audio: audioTracks(project).map((track) => {
      const index = track.items.findIndex((candidate) => candidate.id === itemId);
      const kept = track.items.filter((candidate) => candidate.id !== itemId);
      if (toTrack !== undefined) {
        return track.id === toTrack ? { ...track, items: [...kept, ...items] } : { ...track, items: kept };
      }
      if (index === -1) return track;
      return { ...track, items: [...kept.slice(0, index), ...items, ...kept.slice(index)] };
    }),
  };
}

function refuseOverlap(project: VideoProject, track: AudioTrack, item: AudioItem): void {
  if (audioLaneIsBusy(project, track, audioWindow(project, item), item.id)) {
    throw new CommandRefusal(AUDIO_REFUSAL.overlap);
  }
}

/** Fades that fit the sound: each clamped to 5 s and together to its timeline length. */
function fittedFades(item: AudioItem): AudioItem {
  const length = audioLength(item);
  const fadeIn = Math.min(item.fadeIn ?? 0, length, MAX_FADE);
  const fadeOut = Math.min(item.fadeOut ?? 0, length - fadeIn, MAX_FADE);
  return withField(withField(item, 'fadeIn', fadeIn > 0 ? fadeIn : undefined), 'fadeOut', fadeOut > 0 ? fadeOut : undefined);
}

/** The envelope over a window of the sound's source, re-based to that window's start. */
function envelopeWithin(
  points: readonly EnvelopePoint[] | undefined,
  from: number,
  to: number,
): readonly EnvelopePoint[] | undefined {
  const kept = (points ?? [])
    .filter((point) => point.time >= from - EPSILON && point.time <= to + EPSILON)
    .map((point) => ({ ...point, time: Math.max(0, point.time - from) }));
  return kept.length === 0 ? undefined : kept;
}

export interface AddAudioArgs {
  readonly sourceId: string;
  /** Project seconds where the sound starts. */
  readonly time: number;
  readonly label?: string | undefined;
}

/**
 * Put a sound on the first lane free over its window, hung on the clip under `time`. It plays
 * its whole source; the project's end cuts it. A video source lends its sound only when it has
 * one — a clip with no audio track has nothing to put on a lane.
 */
export function addAudio(project: VideoProject, args: AddAudioArgs): VideoProject {
  const source = sourceOf(project, args.sourceId);
  if (
    source === undefined ||
    source.kind === 'image' ||
    source.duration <= 0 ||
    (source.kind === 'video' && source.hasAudio === false)
  ) {
    throw new CommandRefusal(AUDIO_REFUSAL.notSound);
  }
  const anchor = anchorAt(project, args.time);
  if (anchor === undefined) throw new CommandRefusal(AUDIO_REFUSAL.noClip);
  if (args.time >= projectDuration(project) - EPSILON) {
    throw new CommandRefusal(AUDIO_REFUSAL.pastEnd);
  }
  return placeOnFreeLane(project, {
    id: crypto.randomUUID(),
    sourceId: source.id,
    anchor,
    sourceStart: 0,
    duration: source.duration,
    volume: 1,
    muted: false,
    ...(args.label === undefined ? {} : { label: args.label }),
  });
}

/** A new sound joins the first lane free over its window; only one with nowhere to go opens a
 *  lane of its own. Shared by every command that creates a sound. */
function placeOnFreeLane(project: VideoProject, item: AudioItem): VideoProject {
  const trackId = freeAudioTrack(project, audioWindow(project, item));
  const tracks = audioTracks(project);
  return {
    ...project,
    audio:
      trackId === undefined
        ? [...tracks, { id: crypto.randomUUID(), items: [item] }]
        : tracks.map((track) => (track.id === trackId ? { ...track, items: [...track.items, item] } : track)),
  };
}

export interface MoveAudioArgs {
  readonly itemId: string;
  /** Project seconds where the sound should now start. */
  readonly start: number;
  readonly trackId?: string | undefined;
}

/** A drag on the audio lane: the sound re-hangs on the clip under its new start. */
export function moveAudio(project: VideoProject, args: MoveAudioArgs): VideoProject {
  const { track, item } = locateAudio(project, args.itemId);
  if (args.start >= projectDuration(project) - EPSILON) {
    throw new CommandRefusal(AUDIO_REFUSAL.pastEnd);
  }
  const anchor = anchorAt(project, args.start);
  if (anchor === undefined) throw new CommandRefusal(AUDIO_REFUSAL.noClip);
  const destination =
    args.trackId === undefined ? track : audioTracks(project).find((lane) => lane.id === args.trackId);
  if (destination === undefined) throw new Error(`videoStudio: no audio track named ${String(args.trackId)}`);
  const moved = { ...item, anchor };
  refuseOverlap(project, destination, moved);
  return placeItems(project, item.id, [moved], destination.id === track.id ? undefined : destination.id);
}

export interface TrimAudioArgs {
  readonly itemId: string;
  /** Source seconds from the sound's current sourceStart, as `trimClip` measures a clip. */
  readonly start: number;
  readonly end: number;
}

/**
 * Set which part of its source a sound plays. Trimming the head moves the sound's start by
 * the same material, so what is left stays under the picture it was under; handing material
 * back moves it earlier, and never before the film starts.
 */
export function trimAudio(project: VideoProject, args: TrimAudioArgs): VideoProject {
  const { track, item } = locateAudio(project, args.itemId);
  const source = sourceOf(project, item.sourceId);
  if (source === undefined) throw new CommandRefusal(AUDIO_REFUSAL.sourceMissing);
  const sourceStart = item.sourceStart + args.start;
  if (
    args.end - args.start <= EPSILON ||
    sourceStart < -EPSILON ||
    item.sourceStart + args.end > source.duration + EPSILON
  ) {
    throw new CommandRefusal(AUDIO_REFUSAL.trimPastSource);
  }
  const start = audioWindow(project, item).start + args.start / Math.abs(item.speed ?? 1);
  if (start < -EPSILON) throw new CommandRefusal(AUDIO_REFUSAL.beforeStart);
  if (start >= projectDuration(project) - EPSILON) throw new CommandRefusal(AUDIO_REFUSAL.pastEnd);
  const anchor = anchorAt(project, start);
  if (anchor === undefined) throw new CommandRefusal(AUDIO_REFUSAL.noClip);
  const duration = args.end - args.start;
  const trimmed = fittedFades(
    withField(
      { ...item, anchor, sourceStart: Math.max(0, sourceStart), duration },
      'envelope',
      envelopeWithin(item.envelope, args.start, args.end),
    ),
  );
  refuseOverlap(project, track, trimmed);
  return placeItems(project, item.id, [trimmed]);
}

export interface SplitAudioArgs {
  readonly itemId: string;
  /** Project seconds, strictly inside the sound. */
  readonly time: number;
}

/** Cut a sound in two at `time`. The left half keeps the fade in, the right the fade out, and
 *  each envelope point goes to the half it falls in. */
export function splitAudio(project: VideoProject, args: SplitAudioArgs): VideoProject {
  const { item } = locateAudio(project, args.itemId);
  const window = audioWindow(project, item);
  if (args.time <= window.start + EPSILON || args.time >= window.end - EPSILON) {
    throw new CommandRefusal(AUDIO_REFUSAL.splitOnBoundary);
  }
  const cut = (args.time - window.start) * Math.abs(item.speed ?? 1);
  const anchor = anchorAt(project, args.time);
  if (anchor === undefined) throw new CommandRefusal(AUDIO_REFUSAL.noClip);
  const left = fittedFades(
    withField(withField({ ...item, duration: cut }, 'fadeOut', undefined), 'envelope', envelopeWithin(item.envelope, 0, cut)),
  );
  const right = fittedFades(
    withField(
      withField(
        { ...item, id: crypto.randomUUID(), anchor, sourceStart: item.sourceStart + cut, duration: item.duration - cut },
        'fadeIn',
        undefined,
      ),
      'envelope',
      envelopeWithin(item.envelope, cut, item.duration),
    ),
  );
  return placeItems(project, item.id, [left, right]);
}

export interface SetAudioPropertiesArgs {
  readonly itemId: string;
  readonly volume?: number;
  readonly muted?: boolean;
  readonly fadeIn?: number;
  readonly fadeOut?: number;
  readonly speed?: number;
  readonly denoise?: boolean;
  /** `null` turns ducking off. */
  readonly ducking?: AudioDucking | null;
}

function checkRanges(args: SetAudioPropertiesArgs): void {
  if (args.volume !== undefined && (args.volume < 0 || args.volume > 2)) {
    throw new Error('videoStudio: audio volume must be between 0 and 2');
  }
  if (args.speed !== undefined && (args.speed < 0.25 || args.speed > 4)) {
    throw new Error('videoStudio: audio speed must be between 0.25 and 4');
  }
  for (const fade of [args.fadeIn, args.fadeOut]) {
    if (fade !== undefined && (fade < 0 || fade > MAX_FADE)) {
      throw new Error('videoStudio: a fade must be between 0 and 5 seconds');
    }
  }
  const ducking = args.ducking;
  if (
    ducking !== undefined &&
    ducking !== null &&
    (ducking.amountDb < -24 || ducking.amountDb > -3 || ducking.ramp < 0.1 || ducking.ramp > 2)
  ) {
    throw new Error('videoStudio: ducking must lower by 3–24 dB over 0.1–2 seconds');
  }
}

/** The inspector's Audio and Speed tabs. Fades that together outlast the sound are a decision
 *  the operator can see and change, so they are refused, not clamped. */
export function setAudioProperties(project: VideoProject, args: SetAudioPropertiesArgs): VideoProject {
  const { track, item } = locateAudio(project, args.itemId);
  checkRanges(args);
  const { itemId: _itemId, ducking, ...scalars } = args;
  let next: AudioItem = { ...item, ...scalars };
  if (ducking !== undefined) next = withField(next, 'ducking', ducking ?? undefined);
  if ((next.fadeIn ?? 0) + (next.fadeOut ?? 0) > audioLength(next) + EPSILON) {
    throw new CommandRefusal(AUDIO_REFUSAL.fadesTooLong);
  }
  refuseOverlap(project, track, next);
  return placeItems(project, item.id, [next]);
}

export interface SetEnvelopeArgs {
  readonly itemId: string;
  readonly points: readonly EnvelopePoint[];
}

/** Replace the envelope: sorted, inside the sound, gains 0–1 (the envelope only attenuates;
 *  boosting is the volume's job). An empty list removes it. */
export function setEnvelope(project: VideoProject, args: SetEnvelopeArgs): VideoProject {
  const { item } = locateAudio(project, args.itemId);
  const points = args.points
    .filter((point) => Number.isFinite(point.time) && Number.isFinite(point.gain))
    .map((point) => ({
      time: Math.min(Math.max(point.time, 0), item.duration),
      gain: Math.min(Math.max(point.gain, 0), 1),
    }))
    .sort((a, b) => a.time - b.time);
  return placeItems(project, item.id, [withField(item, 'envelope', points.length === 0 ? undefined : points)]);
}

export interface RecordAnalysisArgs {
  readonly sourceId: string;
  readonly speech?: readonly (readonly [number, number])[];
  readonly denoisedAssetId?: string;
}

/** Merge overlapping windows, clamp them to the source, and keep only the ones with length. */
function cleanSpeech(
  windows: readonly (readonly [number, number])[],
  duration: number,
): readonly (readonly [number, number])[] {
  const sorted = windows
    .filter(([from, to]) => Number.isFinite(from) && Number.isFinite(to))
    .map(([from, to]): [number, number] => [Math.max(0, from), Math.min(duration, to)])
    .filter(([from, to]) => to - from > EPSILON)
    .sort((a, b) => a[0] - b[0]);
  const merged: [number, number][] = [];
  for (const window of sorted) {
    const last = merged.at(-1);
    if (last !== undefined && window[0] <= last[1]) last[1] = Math.max(last[1], window[1]);
    else merged.push([...window]);
  }
  return merged;
}

/** The only door by which browser-side analysis enters the project (spec §Commands). */
export function recordAnalysis(project: VideoProject, args: RecordAnalysisArgs): VideoProject {
  const source = sourceOf(project, args.sourceId);
  if (source === undefined) throw new CommandRefusal(AUDIO_REFUSAL.sourceMissing);
  const recorded = {
    ...source,
    ...(args.speech === undefined ? {} : { speech: cleanSpeech(args.speech, source.duration) }),
    ...(args.denoisedAssetId === undefined ? {} : { denoisedAssetId: args.denoisedAssetId }),
  };
  return {
    ...project,
    sources: project.sources.map((candidate) => (candidate.id === source.id ? recorded : candidate)),
  };
}
```

- [ ] **Step 7: Write the audio strings**

`web/src/i18n/resources.videoStudioAudio.ts`:

```ts
// resources.videoStudioAudio.ts — the audio lanes' strings, merged under `videoStudio.audio`.
// `refusal.*` paths are not a choice: commands_audio.ts raises them BY KEY and the workspace
// shows `t(reasonKey)` unchanged.

export const videoStudioAudioEn = {
  lane: 'Audio {{index}}',
  item: 'Sound {{index}}',
  add: 'Add audio',
  pick: 'Choose a sound',
  reading: 'Reading the sound…',
  extract: 'Extract audio',
  fadeIn: 'Fade in',
  fadeOut: 'Fade out',
  startsAt: 'Starts at',
  in: 'In',
  out: 'Out',
  trimStart: 'Start of sound {{index}}',
  trimEnd: 'End of sound {{index}}',
  refusal: {
    noClip: 'A sound hangs on a clip: add a clip first.',
    pastEnd: 'There is no film at that point to put a sound on.',
    beforeStart: 'A sound cannot start before the film does.',
    overlap: 'Two sounds cannot cover the same instant of one audio lane.',
    notSound: 'That source has no sound to play.',
    fadesTooLong: 'The fades together are longer than the sound.',
    undecodable: 'This browser cannot decode that sound.',
  },
};

export const videoStudioAudioIt: typeof videoStudioAudioEn = {
  lane: 'Audio {{index}}',
  item: 'Suono {{index}}',
  add: 'Aggiungi audio',
  pick: 'Scegli un suono',
  reading: 'Lettura del suono…',
  extract: 'Estrai audio',
  fadeIn: 'Dissolvenza in entrata',
  fadeOut: 'Dissolvenza in uscita',
  startsAt: 'Inizia a',
  in: 'Inizio',
  out: 'Fine',
  trimStart: 'Inizio del suono {{index}}',
  trimEnd: 'Fine del suono {{index}}',
  refusal: {
    noClip: 'Un suono è agganciato a una clip: aggiungi prima una clip.',
    pastEnd: 'In quel punto non c’è filmato su cui mettere un suono.',
    beforeStart: 'Un suono non può iniziare prima del filmato.',
    overlap: 'Due suoni non possono coprire lo stesso istante di una traccia audio.',
    notSound: 'Quella sorgente non ha un suono da riprodurre.',
    fadesTooLong: 'Le dissolvenze insieme durano più del suono.',
    undecodable: 'Questo browser non riesce a decodificare quel suono.',
  },
};
```

In `resources.videoStudio.ts`: `import { videoStudioAudioEn, videoStudioAudioIt } from './resources.videoStudioAudio';`, then `audio: videoStudioAudioEn,` as the last key of `videoStudioEn.videoStudio` and `audio: videoStudioAudioIt,` in `videoStudioIt.videoStudio`. Add one line to the header comment: "the audio lanes' strings live in resources.videoStudioAudio.ts and are merged here as `audio`".

- [ ] **Step 8: Run the tests to verify they pass**

Run: `MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/vt.sh src/videoStudio/ src/i18n/`
Expected: PASS — `audioLane.test.ts` and `commands_audio.test.ts` green, `commands.test.ts` still 48/48, `projectStore.test.ts` still green, the i18n parity tests green. (`videoflow_fonts.test.ts` fails in WSL only on its JSON import attribute — a known WSL-only failure, green in CI.)

- [ ] **Step 9: Put the pure core under mutation**

Spec §Tasks asks Stryker ≥ 70 % killed on the pure core. Add `"src/videoStudio/audioLane.ts"` and `"src/videoStudio/commands_audio.ts"` to `web/stryker.config.json` `mutate`, and `'src/videoStudio/__tests__/audioLane.test.ts'`, `'src/videoStudio/__tests__/commands_audio.test.ts'` to `mutationTests` in `web/vitest.stryker.config.ts` (under a comment `// Video Studio audio core (spec 2026-09-27).`). Stryker runs in CI only (critical-mutation job); after CI, the job's `critical-mutation` artifact carries `web/reports/mutation/mutation.json`, from which the per-file score of each new file is read (killed+timeout over killed+timeout+survived+no-coverage) and must be ≥ 70 %; survivors below that get a killing test before the next task.

- [ ] **Step 10: Static checks, coverage, commit**

Run `webcheck.sh` with the new and touched files (prettier write, typecheck, oxlint, lint-contract, knip, prettier check), then `npx vitest run --coverage --coverage.reportOnFailure=true src/videoStudio src/i18n` and read the per-file lines for `audioLane.ts` and `commands_audio.ts`.
Expected: 0 errors everywhere; both new files ≥ 85 % lines.

```bash
git add web/src/videoStudio/audioLane.ts web/src/videoStudio/commands_audio.ts web/src/i18n/resources.videoStudioAudio.ts web/src/videoStudio/__tests__/audioLane.test.ts web/src/videoStudio/__tests__/commands_audio.test.ts
git commit -m "feat(video-studio): audio lanes in the core — windows, re-anchoring, commands" -- <the five files above> web/src/videoStudio/commands.ts web/src/videoStudio/project.ts web/src/i18n/resources.videoStudio.ts web/stryker.config.json web/vitest.stryker.config.ts
```

Commit body: the three rules (never outlives the video, never disappears on a video edit, extracted audio goes with its clip) and that the re-anchor lands in the same commit that first lets a lane change (final-review carry-over from Plan A).

---

### Task 2: The curve and the compile — every audible layer's volume where the mixer reads it

**Files:**
- Create: `web/src/videoStudio/volumeCurve.ts`
- Create: `web/src/videoStudio/videoflow_audio.ts`
- Create: `web/e2e/support/audioMeasure.ts`
- Create: `web/e2e/video-studio-mix.spec.ts`
- Modify: `web/src/videoStudio/videoflow.ts` (drop the dead static `volume`; add the audio layers; write the volumes into the compiled JSON)
- Modify: `web/e2e/support/videoStudio.ts` (`AUDIO_FIXTURES`, `reopen`, `uploadClip` and `exportTo` move here from the audio spec)
- Modify: `web/e2e/video-studio-audio.spec.ts` (imports the moved helpers)
- Modify: `web/stryker.config.json`, `web/vitest.stryker.config.ts`
- Test: `web/src/videoStudio/__tests__/volumeCurve.test.ts`, `web/src/videoStudio/__tests__/videoflow_audio.test.ts`, `web/src/videoStudio/__tests__/videoflow.test.ts` (one expectation rewritten, see Step 6)

**Interfaces:**
- Consumes: `audioWindow(project, item)` from Task 1; `AudioItem`, `EnvelopePoint`, `VideoProject` from `project.ts`.
- Produces (`volumeCurve.ts`): `CURVE_STEP = 0.01`, `interface VolumeKeyframe {time; value}`, `interface CurveInput {sourceStart; speed; length; volume; fadeIn?; fadeOut?; envelope?}`, `gainAt(input, t): number` (t = timeline seconds from the layer's start), `volumeKeyframes(input): readonly VolumeKeyframe[] | undefined` (undefined = the gain is 1 throughout, nothing to write).
- Produces (`videoflow_audio.ts`): `addAudioItems(flow, project, urls)`, `withVolumes(project, json): VideoJSON`.
- Produces (E2E support): `AUDIO_FIXTURES`, `reopen(page, project, clipAsset)`, `uploadClip(page)`, `exportTo(page, editor, info, fileName)`, `windowPowers(page, bytes)`, `levelBetween(powers, from, to)`.

- [ ] **Step 1: Write the failing curve tests**

`web/src/videoStudio/__tests__/volumeCurve.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import { CURVE_STEP, gainAt, volumeKeyframes, type CurveInput } from '../volumeCurve';

const flat: CurveInput = { sourceStart: 0, speed: 1, length: 4, volume: 1 };

function valueAt(frames: ReturnType<typeof volumeKeyframes>, time: number): number | undefined {
  return frames?.findLast((frame) => frame.time <= time + 1e-9)?.value;
}

describe('volumeKeyframes', () => {
  it('writes nothing for a layer at full volume with no fade', () => {
    expect(volumeKeyframes(flat)).toBeUndefined();
  });

  it('holds a constant volume with one keyframe at the layer own sourceStart', () => {
    expect(volumeKeyframes({ ...flat, sourceStart: 4.0001, volume: 0.5 })).toEqual([
      { time: 4.0001, value: 0.5 },
    ]);
  });

  it('samples a fade in from silence, every step, and holds once it is up', () => {
    const frames = volumeKeyframes({ ...flat, volume: 0.5, fadeIn: 2 });
    expect(frames?.[0]).toEqual({ time: 0, value: 0 });
    expect(valueAt(frames, 1)).toBeCloseTo(0.25, 3);
    expect(frames?.at(-1)?.value).toBeCloseTo(0.5, 6);
    expect(frames?.at(-1)?.time).toBeLessThanOrEqual(2 + 1e-9);
    expect((frames?.[1]?.time ?? 0) - (frames?.[0]?.time ?? 0)).toBeCloseTo(CURVE_STEP, 9);
  });

  it('speaks source seconds: a trimmed layer at double speed maps its ramp through both', () => {
    const frames = volumeKeyframes({ ...flat, sourceStart: 3, speed: 2, length: 2, fadeIn: 1 });
    // Half a timeline second into the layer is one source second past its sourceStart.
    expect(valueAt(frames, 4)).toBeCloseTo(0.5, 3);
    expect((frames?.[1]?.time ?? 0) - 3).toBeCloseTo(CURVE_STEP * 2, 9);
  });

  it('ends a fade out at silence on the layer last source instant', () => {
    const frames = volumeKeyframes({ ...flat, fadeOut: 1 });
    expect(frames?.at(-1)).toEqual({ time: 4, value: 0 });
    expect(valueAt(frames, 2.9)).toBe(1);
  });

  it('rises then falls once when the fades overlap, never dipping twice', () => {
    const values = (volumeKeyframes({ ...flat, fadeIn: 3, fadeOut: 3 }) ?? []).map((frame) => frame.value);
    const peak = values.indexOf(Math.max(...values));
    const rising = values.slice(0, peak + 1).every((value, index, all) => index === 0 || value >= (all[index - 1] ?? 0));
    const falling = values.slice(peak).every((value, index, all) => index === 0 || value <= (all[index - 1] ?? 0));
    expect(rising && falling).toBe(true);
  });
});

describe('gainAt', () => {
  it('follows the envelope linearly between its points and holds it past either end', () => {
    const input = { ...flat, envelope: [{ time: 1, gain: 1 }, { time: 3, gain: 0 }] };
    expect(gainAt(input, 0)).toBe(1);
    expect(gainAt(input, 2)).toBeCloseTo(0.5, 9);
    expect(gainAt(input, 3.5)).toBe(0);
  });

  it('reads envelope points in source seconds, so a faster layer reaches them sooner', () => {
    const input = { ...flat, speed: 2, envelope: [{ time: 0, gain: 1 }, { time: 2, gain: 0 }] };
    expect(gainAt(input, 0.5)).toBeCloseTo(0.5, 9);
  });

  it('multiplies volume, fade and envelope', () => {
    const input = { ...flat, volume: 2, fadeIn: 2, envelope: [{ time: 0, gain: 0.5 }] };
    expect(gainAt(input, 1)).toBeCloseTo(0.5, 9);
  });
});
```

- [ ] **Step 2: Run it to verify it fails**

Run: `MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/vt.sh src/videoStudio/__tests__/volumeCurve.test.ts`
Expected: FAIL — `Failed to resolve import "../volumeCurve"`.

- [ ] **Step 3: Write `volumeCurve.ts`**

```ts
// volumeCurve.ts — a layer's gain over time, as the keyframes VideoFlow's mixer can play.
//
// S1 measured the three rules this obeys (spikes/video-studio-audio/FINDINGS.md): keyframe times
// are absolute source seconds; the mixer never interpolates, it steps from one keyframe to the
// next (`setValueAtTime`); and before the first keyframe the gain is 1. So a curve starts with a
// keyframe at the layer's own sourceStart, and every ramp is sampled densely enough to sound
// continuous.

import type { EnvelopePoint } from './project';

/** Seconds between the samples of a ramp: 10 ms steps are finer than the ear hears as stairs. */
export const CURVE_STEP = 0.01;
/** Gains closer than this are one gain, and a keyframe for them is a keyframe for nothing. */
const SAME_GAIN = 1e-4;

export interface VolumeKeyframe {
  readonly time: number;
  readonly value: number;
}

export interface CurveInput {
  /** Where the layer starts in its source — the keyframes' zero, read off the layer itself. */
  readonly sourceStart: number;
  readonly speed: number;
  /** How long the layer plays, in timeline seconds. */
  readonly length: number;
  readonly volume: number;
  readonly fadeIn?: number | undefined;
  readonly fadeOut?: number | undefined;
  /** Source seconds from `sourceStart`, gains 0–1. */
  readonly envelope?: readonly EnvelopePoint[] | undefined;
}

function envelopeAt(points: readonly EnvelopePoint[] | undefined, time: number): number {
  if (points === undefined || points.length === 0) return 1;
  const next = points.findIndex((point) => point.time > time);
  if (next === 0) return points[0]?.gain ?? 1;
  if (next === -1) return points.at(-1)?.gain ?? 1;
  const from = points[next - 1];
  const to = points[next];
  if (from === undefined || to === undefined) return 1;
  return from.gain + ((to.gain - from.gain) * (time - from.time)) / (to.time - from.time);
}

/** The gain `t` timeline seconds into the layer. The fades are a MIN of two ramps, so fades that
 *  overlap on a short layer make one rise and one fall, never a double dip. */
export function gainAt(input: CurveInput, t: number): number {
  const fadeIn = input.fadeIn ?? 0;
  const fadeOut = input.fadeOut ?? 0;
  const fade = Math.min(
    1,
    fadeIn > 0 ? t / fadeIn : 1,
    fadeOut > 0 ? (input.length - t) / fadeOut : 1,
  );
  return input.volume * Math.max(0, fade) * envelopeAt(input.envelope, t * input.speed);
}

/** The keyframes that play `gainAt` on VideoFlow's mixer, or `undefined` when the gain is 1
 *  throughout and the mixer's own default already says so. */
export function volumeKeyframes(input: CurveInput): readonly VolumeKeyframe[] | undefined {
  const times: number[] = [];
  for (let index = 0; index * CURVE_STEP < input.length; index += 1) times.push(index * CURVE_STEP);
  times.push(input.length);
  const frames: VolumeKeyframe[] = [];
  for (const t of times) {
    const value = gainAt(input, t);
    const last = frames.at(-1);
    if (last !== undefined && Math.abs(last.value - value) < SAME_GAIN) continue;
    frames.push({ time: input.sourceStart + t * input.speed, value });
  }
  const only = frames.length === 1 ? frames[0] : undefined;
  return only !== undefined && Math.abs(only.value - 1) < SAME_GAIN ? undefined : frames;
}
```

- [ ] **Step 4: Run it to verify it passes**

Run: `MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/vt.sh src/videoStudio/__tests__/volumeCurve.test.ts`
Expected: PASS, 9 tests.

- [ ] **Step 5: Write the failing compile tests**

`web/src/videoStudio/__tests__/videoflow_audio.test.ts`:

```ts
import type VideoFlow from '@videoflow/core';
import type { VideoJSON } from '@videoflow/core';
import { describe, expect, it, vi } from 'vitest';
import type { AudioItem, VideoItem, VideoProject } from '../project';
import { addAudioItems, withVolumes } from '../videoflow_audio';

const urls = { assetUrl: (id: string) => `/api/assets/${id}/content` };

function project(items: AudioItem[] = [], clip: Partial<VideoItem> = {}): VideoProject {
  return {
    id: 'p',
    name: 'demo',
    size: { width: 320, height: 180 },
    fps: 30,
    sources: [
      { id: 'src-a', assetId: 'a', kind: 'video', duration: 10, size: { width: 320, height: 180 }, hasAudio: true },
      { id: 'src-m', assetId: 'm', kind: 'audio', duration: 8, size: { width: 0, height: 0 } },
      { id: 'src-i', assetId: 'i', kind: 'image', duration: 0, size: { width: 8, height: 6 } },
    ],
    video: [
      { id: 'clip-1', sourceId: 'src-a', duration: 4, sourceStart: 0, muted: false, ...clip },
      { id: 'still', sourceId: 'src-i', duration: 2, sourceStart: 0, muted: false, volume: 0.5 },
    ],
    overlays: [],
    audio: [{ id: 'lane', items }],
  };
}

function sound(over: Partial<AudioItem> = {}): AudioItem {
  return {
    id: 'bed',
    sourceId: 'src-m',
    anchor: { clipId: 'clip-1', offset: 1 },
    sourceStart: 2,
    duration: 4,
    volume: 1,
    muted: false,
    ...over,
  };
}

type Layer = VideoJSON['layers'][number];

function layer(name: string, settings: Record<string, unknown> = {}): Layer {
  return {
    id: name,
    type: 'audio',
    settings: { name, enabled: true, startTime: 0, sourceDuration: 4, sourceStart: 0, ...settings },
    properties: {},
    animations: [],
  };
}

function json(...layers: Layer[]): VideoJSON {
  return { name: 'demo', duration: 6, width: 320, height: 180, fps: 30, backgroundColor: '#000', layers };
}

function flowSpy() {
  const addAudio = vi.fn();
  return { addAudio, flow: { addAudio } as unknown as VideoFlow };
}

describe('addAudioItems', () => {
  it('adds one audio layer per sound at its window, in source seconds, cut at the film end', () => {
    const { addAudio, flow } = flowSpy();
    // The film is 6 s; the sound starts at 1 s and would play 2 s at double speed.
    addAudioItems(flow, project([sound({ speed: 2 })]), urls);
    expect(addAudio).toHaveBeenCalledWith(
      { mute: false },
      { name: 'bed', source: '/api/assets/m/content', startTime: 1, sourceStart: 2, sourceDuration: 4, speed: 2 },
    );
    addAudio.mockClear();
    addAudioItems(flow, project([sound({ duration: 8 })]), urls);
    // Eight source seconds from 1 s run past the film's 6 s: only five of them play.
    expect(addAudio.mock.calls[0]?.[1]).toMatchObject({ startTime: 1, sourceDuration: 5, speed: 1 });
  });

  it('carries mute where the mixer reads it', () => {
    const { addAudio, flow } = flowSpy();
    addAudioItems(flow, project([sound({ muted: true })]), urls);
    expect(addAudio.mock.calls[0]?.[0]).toEqual({ mute: true });
  });

  it('adds nothing for a sound with no window, and nothing for a project with no lanes', () => {
    const { addAudio, flow } = flowSpy();
    const { audio: _audio, ...legacy } = project();
    addAudioItems(flow, legacy, urls);
    addAudioItems(flow, project([sound({ anchor: { clipId: 'gone', offset: 0 } })]), urls);
    expect(addAudio).not.toHaveBeenCalled();
  });

  it('is loud about a sound whose source the project lost', () => {
    const { flow } = flowSpy();
    expect(() => {
      addAudioItems(flow, project([sound({ sourceId: 'lost' })]), urls);
    }).toThrow(/lost/);
  });
});

describe('withVolumes', () => {
  it('writes a clip volume as a keyframe at the layer own sourceStart, nudge included', () => {
    const out = withVolumes(project([], { volume: 0.5 }), json(layer('clip-1', { sourceStart: 4.0001 })));
    expect(out.layers[0]?.animations).toEqual([
      { property: 'volume', keyframes: [{ time: 4.0001, value: 0.5 }] },
    ]);
  });

  it('leaves a clip at full volume, a muted clip and a still exactly as compiled', () => {
    const untouched = json(layer('clip-1'), layer('still'));
    expect(withVolumes(project(), untouched)).toEqual(untouched);
    expect(withVolumes(project([], { volume: 0.5, muted: true }), untouched)).toEqual(untouched);
  });

  it('writes a sound fade in source seconds read off the layer settings', () => {
    const out = withVolumes(
      project([sound({ fadeIn: 1 })]),
      json(layer('bed', { sourceStart: 2, speed: 2, sourceDuration: 4 })),
    );
    const frames = out.layers[0]?.animations[0]?.keyframes ?? [];
    expect(frames[0]).toEqual({ time: 2, value: 0 });
    // The fade lasts one timeline second: two source seconds at double speed.
    expect(frames.at(-1)?.time).toBeCloseTo(4, 6);
  });

  it('replaces a volume animation instead of adding a second, and leaves nameless layers alone', () => {
    const compiled = json(
      {
        ...layer('clip-1'),
        animations: [
          { property: 'volume', keyframes: [{ time: 0, value: 1 }] },
          { property: 'pan', keyframes: [] },
        ],
      },
      { ...layer('wash'), settings: { enabled: true, startTime: 0, sourceDuration: 1 } },
    );
    const out = withVolumes(project([], { volume: 0.25 }), compiled);
    expect(out.layers[0]?.animations.map((animation) => animation.property)).toEqual(['pan', 'volume']);
    expect(out.layers[1]).toEqual(compiled.layers[1]);
  });
});
```

- [ ] **Step 6: Run them to verify they fail, and rewrite the dead-volume expectation**

Run: `MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/vt.sh src/videoStudio/__tests__/videoflow_audio.test.ts`
Expected: FAIL — `Failed to resolve import "../videoflow_audio"`.

In `videoflow.test.ts`, the test "carries the selected clip rotation, fit and volume into VideoFlow" pins `volume: 0.4` in `addVideo`'s properties: the form S1 measured never reaching the mix (0.5 and 1 both −24.08 dBFS). The test itself is wrong, so it is rewritten, with the reason in the commit body: rename it "carries the selected clip rotation and fit into VideoFlow, and no static volume", delete `volume: 0.4,` from the `toMatchObject` expectation (not from the fixture), and add after that expectation:

```ts
    // A static `volume` never reaches the mixer (S1); the clip's volume is a keyframe now.
    expect(calls.videos[0]?.props).not.toHaveProperty('volume');
```

- [ ] **Step 7: Write `videoflow_audio.ts` and wire it into `toVideoJSON`**

```ts
// videoflow_audio.ts — the audio half of the compile: one AudioLayer per sound, and every audible
// layer's volume written where VideoFlow's mixer reads it.
//
// That place is the compiled JSON's `animations` and nowhere else (S1): a static `volume` in a
// layer's properties never reaches the mix. So the volumes are written AFTER `compile()`, onto the
// layers it returns, each against that layer's own `sourceStart`, `speed` and `sourceDuration` —
// the numbers the mixer maps keyframes through, cut nudge included.

import type VideoFlow from '@videoflow/core';
import type { VideoJSON } from '@videoflow/core';
import type { AssetSource } from '../chat/artifacts/renderers/assetSourceContext';
import { audioWindow } from './audioLane';
import { audioTracks, sourceOf, type VideoProject } from './project';
import { volumeKeyframes, type CurveInput } from './volumeCurve';

type Loudness = Pick<CurveInput, 'volume' | 'fadeIn' | 'fadeOut' | 'envelope'>;

/** Every sound as a VideoFlow AudioLayer, at the window its anchor gives it. */
export function addAudioItems(
  flow: VideoFlow,
  project: VideoProject,
  urls: Pick<AssetSource, 'assetUrl'>,
): void {
  for (const item of audioTracks(project).flatMap((track) => track.items)) {
    const window = audioWindow(project, item);
    const length = window.end - window.start;
    // A sound with no window — its anchor gone, or pushed to the film's last instant — has
    // nothing to play, so it contributes no layer.
    if (length <= 0) continue;
    const source = sourceOf(project, item.sourceId);
    if (source === undefined) {
      throw new Error(
        `videoStudio: sound ${item.id} points at a source the project lost: ${item.sourceId}`,
      );
    }
    const speed = item.speed ?? 1;
    flow.addAudio(
      { mute: item.muted },
      {
        name: item.id,
        source: urls.assetUrl(source.assetId),
        startTime: window.start,
        sourceStart: item.sourceStart,
        sourceDuration: length * Math.abs(speed),
        speed,
      },
    );
  }
}

/** What each audible layer should sound like, by the name the compile gave it. */
function loudnessByLayer(project: VideoProject): ReadonlyMap<string, Loudness> {
  const byName = new Map<string, Loudness>();
  for (const clip of project.video) {
    const source = sourceOf(project, clip.sourceId);
    if (!clip.muted && source?.kind === 'video' && source.hasAudio !== false) {
      byName.set(clip.id, { volume: clip.volume ?? 1 });
    }
  }
  for (const item of audioTracks(project).flatMap((track) => track.items)) {
    if (item.muted) continue;
    byName.set(item.id, {
      volume: item.volume,
      fadeIn: item.fadeIn,
      fadeOut: item.fadeOut,
      envelope: item.envelope,
    });
  }
  return byName;
}

/** The compiled JSON with every audible layer's volume curve in its `animations`. */
export function withVolumes(project: VideoProject, json: VideoJSON): VideoJSON {
  const loudness = loudnessByLayer(project);
  return {
    ...json,
    layers: json.layers.map((layer) => {
      const name: unknown = layer.settings.name;
      const wanted = typeof name === 'string' ? loudness.get(name) : undefined;
      if (wanted === undefined) return layer;
      const speed = Math.abs(Number(layer.settings.speed ?? 1)) || 1;
      const keyframes = volumeKeyframes({
        ...wanted,
        sourceStart: layer.settings.sourceStart ?? 0,
        speed,
        length: layer.settings.sourceDuration / speed,
      });
      if (keyframes === undefined) return layer;
      return {
        ...layer,
        animations: [
          ...layer.animations.filter((animation) => animation.property !== 'volume'),
          { property: 'volume', keyframes: [...keyframes] },
        ],
      };
    }),
  };
}
```

In `videoflow.ts`:
- header: "mute where the mixer reads it" → "mute and volume where the mixer reads them".
- `import { addAudioItems, withVolumes } from './videoflow_audio';`
- in `addClip`'s `flow.addVideo` properties, delete `volume: clip.volume ?? 1,` and extend the comment above them: "…the mixer reads `mute` in its PROPERTIES (spike 108 §6) and `volume` only in the compiled `animations` (S1), which `withVolumes` writes."
- the end of `toVideoJSON` becomes:

```ts
  for (const track of project.overlays) {
    for (const item of track.items) addOverlay(flow, project, urls, item);
  }
  addAudioItems(flow, project, urls);
  // No layer was added with `waitFor`, so the flow pointer is still at zero: this one wait is what
  // gives the compiled JSON the lane's own length rather than a float sum of layer ends — and what
  // keeps a sound running past the last frame from lengthening the film.
  flow.wait(projectDuration(project));
  return withVolumes(project, await flow.compile());
```

- [ ] **Step 8: Run the unit tests to verify they pass**

Run: `MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/vt.sh src/videoStudio/`
Expected: PASS — `volumeCurve.test.ts` 9, `videoflow_audio.test.ts` 8, `videoflow.test.ts` green with the rewritten expectation (its mock's `compile()` returns `{ layers: [] }`, which `withVolumes` maps to itself); `videoflow_fonts.test.ts` fails only on its WSL-only JSON import attribute.

- [ ] **Step 9: Write the E2E helpers**

`web/e2e/support/audioMeasure.ts`:

```ts
import type { Page } from '@playwright/test';

// audioMeasure.ts — how loud an exported file is, a tenth of a second at a time. The audio is
// decoded in the page, like the frames in video-studio.spec.ts: Node has no WebCodecs, and the
// browser's decoder is the one the operator's player uses.

export const WINDOW = 0.1;

/** Mean power (mean square over every channel) of each whole 100 ms window of the file's audio. */
export async function windowPowers(page: Page, bytes: Buffer): Promise<readonly number[]> {
  return page.evaluate(
    async ({ base64, window }) => {
      const data = Uint8Array.from(atob(base64), (c) => c.charCodeAt(0));
      const decoded = await new OfflineAudioContext(1, 1, 48_000).decodeAudioData(data.buffer);
      const size = Math.round(decoded.sampleRate * window);
      const channels = Array.from({ length: decoded.numberOfChannels }, (_, index) =>
        decoded.getChannelData(index),
      );
      const powers: number[] = [];
      for (let start = 0; start + size <= decoded.length; start += size) {
        let sum = 0;
        for (const channel of channels) {
          for (let at = start; at < start + size; at += 1) sum += (channel[at] ?? 0) ** 2;
        }
        powers.push(sum / (size * channels.length));
      }
      return powers;
    },
    { base64: bytes.toString('base64'), window: WINDOW },
  );
}

/** The level, in dBFS, of the windows lying wholly inside [from, to). */
export function levelBetween(powers: readonly number[], from: number, to: number): number {
  const inside = powers.slice(Math.ceil(from / WINDOW - 1e-9), Math.floor(to / WINDOW + 1e-9));
  if (inside.length === 0) {
    throw new Error(`no whole window between ${String(from)} and ${String(to)} s`);
  }
  const mean = inside.reduce((sum, power) => sum + power, 0) / inside.length;
  return 10 * Math.log10(Math.max(mean, 1e-20));
}
```

In `web/e2e/support/videoStudio.ts` add (imports: `type TestInfo` from `@playwright/test`, `uploadBytes` from `./assetUpload`):

```ts
export const AUDIO_FIXTURES = resolve(FIXTURES, 'audio');

/** Puts a project file in the library and opens the Studio on it, the way a reload does. */
export async function reopen(page: Page, project: object, clipAsset: string): Promise<Locator> {
  const fileId = await uploadBytes(
    page,
    Buffer.from(JSON.stringify(project)),
    'project.json',
    'application/json',
  );
  await page.addInitScript((id) => {
    window.localStorage.setItem('aura.videoStudio.lastSavedProject', id);
  }, fileId);
  await openStudioWith(page, clipAsset, 'audio lane check');
  await page.getByRole('button', { name: 'Reopen the last project you saved here' }).click();
  return page.getByRole('dialog', { name: 'Video editor' });
}

/** Signs in and puts `clip-a.mp4` (4 s, 320×180, a tone) in the library through the media door. */
export async function uploadClip(page: Page): Promise<string> {
  await gotoAuthenticated(page, '/');
  return uploadAsset(page, resolve(FIXTURES, 'clip-a.mp4'), 'clip-a.mp4', 'video/mp4', {
    use: 'media',
  });
}

/** Presses Export and keeps the file with the run's other output. */
export async function exportTo(
  page: Page,
  editor: Locator,
  info: TestInfo,
  fileName: string,
): Promise<string> {
  const downloading = page.waitForEvent('download', { timeout: 10 * 60_000 });
  await editor.getByRole('button', { name: 'Export', exact: true }).click();
  const path = info.outputPath(fileName);
  await (await downloading).saveAs(path);
  return path;
}
```

In `video-studio-audio.spec.ts` delete the local `reopen` and `uploadClip` and the local `AUDIO` constant, import `AUDIO_FIXTURES, reopen, uploadClip` from `./support/videoStudio`, and drop the imports the move leaves unused.

- [ ] **Step 10: Write the mix E2E**

`web/e2e/video-studio-mix.spec.ts`:

```ts
import { randomUUID } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import type { Page, TestInfo } from '@playwright/test';
import { expect, test } from './support/assetCleanup';
import { uploadAsset } from './support/assetUpload';
import { levelBetween, windowPowers } from './support/audioMeasure';
import { AUDIO_FIXTURES, containerFacts, exportTo, reopen, uploadClip } from './support/videoStudio';

// video-studio-mix.spec.ts — what an export sounds like. Each project is written as a file and
// opened the way a reload opens one, so these measure the compile and the mixer alone: every
// volume here reaches the MP4 only through the keyframes videoflow_audio.ts writes (S1).

const FRAME = { width: 320, height: 180 };

function film(clip: string, name: string, volumes: readonly [number, number], muted: boolean) {
  return {
    id: randomUUID(),
    name,
    size: FRAME,
    fps: 30,
    sources: [{ id: 'src-a', assetId: clip, kind: 'video', duration: 4, size: FRAME, hasAudio: true }],
    video: volumes.map((volume, index) => ({
      id: `clip-${String(index + 1)}`,
      sourceId: 'src-a',
      duration: 4,
      sourceStart: 0,
      muted,
      volume,
    })),
    overlays: [],
  };
}

async function exportLevels(page: Page, project: object, clip: string, info: TestInfo, name: string) {
  const editor = await reopen(page, project, clip);
  await expect(editor.getByRole('button', { name: 'Clip 2' })).toBeVisible({ timeout: 60_000 });
  const path = await exportTo(page, editor, info, name);
  return { facts: await containerFacts(path), powers: await windowPowers(page, readFileSync(path)) };
}

test.describe('the mix', () => {
  test('music at 50 % sits 6 dB under 100 %, fades in from silence and stops with the film', async ({
    page,
  }, info) => {
    test.setTimeout(12 * 60_000);
    const clip = await uploadClip(page);
    const music = await uploadAsset(page, resolve(AUDIO_FIXTURES, 'music.wav'), 'music.wav', 'audio/wav', {
      use: 'media',
    });
    const base = film(clip, 'mix music check', [1, 1], true);
    const bed = { sourceId: 'src-m', sourceStart: 0, muted: false };
    const project = {
      ...base,
      sources: [
        ...base.sources,
        { id: 'src-m', assetId: music, kind: 'audio', duration: 8, size: { width: 0, height: 0 } },
      ],
      audio: [
        {
          id: 'lane-1',
          items: [
            { ...bed, id: 'soft', anchor: { clipId: 'clip-1', offset: 0 }, duration: 4, volume: 0.5, fadeIn: 2 },
            // Eight seconds of music hung on a clip that starts at 4 s: the film cuts it at 8.
            { ...bed, id: 'full', anchor: { clipId: 'clip-2', offset: 0 }, duration: 8, volume: 1 },
          ],
        },
      ],
    };
    const { facts, powers } = await exportLevels(page, project, clip, info, 'mix-music.mp4');
    const levels = {
      start: levelBetween(powers, 0, 0.1),
      soft: levelBetween(powers, 2.5, 3.5),
      full: levelBetween(powers, 4.5, 7.5),
    };
    await info.attach('mix-music-levels', {
      contentType: 'application/json',
      body: JSON.stringify({ facts, levels }, null, 2),
    });
    expect(facts.duration).toBeGreaterThan(7.9);
    expect(facts.duration).toBeLessThan(8.1);
    expect(levels.full - levels.soft).toBeGreaterThan(5);
    expect(levels.full - levels.soft).toBeLessThan(7);
    expect(levels.soft - levels.start).toBeGreaterThanOrEqual(15);
  });

  test('a clip at 50 % sits 6 dB under the same clip at 100 %', async ({ page }, info) => {
    test.setTimeout(12 * 60_000);
    const clip = await uploadClip(page);
    const project = film(clip, 'mix clip volume check', [0.5, 1], false);
    const { powers } = await exportLevels(page, project, clip, info, 'mix-clip.mp4');
    const levels = { half: levelBetween(powers, 0.5, 3.5), full: levelBetween(powers, 4.5, 7.5) };
    await info.attach('mix-clip-levels', {
      contentType: 'application/json',
      body: JSON.stringify(levels, null, 2),
    });
    expect(levels.full - levels.half).toBeGreaterThan(5);
    expect(levels.full - levels.half).toBeLessThan(7);
  });
});
```

- [ ] **Step 11: Run the mix E2E RED against the VM's current image**

The VM still runs the image without this task: it loads these project files (Plan A's loader) but ignores both the audio items and the clip's static volume. From WSL: `e2e-vm.sh video-studio-mix.spec.ts --project=chrome`.
Expected: FAIL on both tests, on the level assertions (the music is absent, so `full − soft` is nowhere near 6 dB; `full − half` ≈ 0) — not on loading or exporting.

- [ ] **Step 12: Static checks, coverage, mutation scope, dist, commit, push, GREEN on the VM**

Add `"src/videoStudio/volumeCurve.ts"` and `"src/videoStudio/videoflow_audio.ts"` to `stryker.config.json` `mutate`, their two test files to `vitest.stryker.config.ts` under the Task 1 comment. Run `webcheck.sh` (prettier, typecheck, oxlint, lint-contract, knip, format) over every touched file and the E2E files, then coverage over `src/videoStudio` (`volumeCurve.ts`, `videoflow_audio.ts`, `videoflow.ts` ≥ 85 % lines), then `build.sh` (`npm run build`, refreshes `internal/webui/dist`).

```bash
git add web/src/videoStudio/volumeCurve.ts web/src/videoStudio/videoflow_audio.ts web/src/videoStudio/__tests__/volumeCurve.test.ts web/src/videoStudio/__tests__/videoflow_audio.test.ts web/e2e/support/audioMeasure.ts web/e2e/video-studio-mix.spec.ts
git commit -m "feat(video-studio): export sounds, and every volume through the mixer's keyframes" -- <the six files above> web/src/videoStudio/videoflow.ts web/src/videoStudio/__tests__/videoflow.test.ts web/e2e/support/videoStudio.ts web/e2e/video-studio-audio.spec.ts web/stryker.config.json web/vitest.stryker.config.ts
git add internal/webui/dist && git commit -m "build(web): rebuild the embedded dist for the audio mix" -- internal/webui/dist
```

The first commit's body states the live bug it fixes (the clip Volume slider was a no-op: S1 measured 0.5 and 1 both at −24.08 dBFS) and why `videoflow.test.ts`'s volume expectation was rewritten rather than kept. Push from WSL, wait for every CI workflow (*Publish Aura edge image* included), `wait-contains.sh <full sha of the dist commit>`, then run the spec GREEN on the VM: `e2e-vm.sh video-studio-mix.spec.ts --project=chrome`. Read both attached level JSONs, record the numbers in the ledger, and confirm the cleanup fixture passed.

---

### Task 3: Sounds in the editor — the audio door, the lanes, the inspector

**Files:**
- Modify: `web/src/mediaEdit/videoMedia.ts` (`probeAudio`; the abort plumbing shared with `probeVideo`)
- Modify: `web/src/videoStudio/VideoStudio_sources.ts` (`AUDIO_ACCEPT`, `SOURCE_ACCEPT` takes sounds, `probeSource` routes them, `sourceEdit` places them, the upload hints them)
- Create: `web/src/videoStudio/VideoStudio_selection.ts` (the workspace's pure selection helpers, out of `VideoStudio.tsx`)
- Create: `web/src/videoStudio/Timeline_audio.tsx`
- Modify: `web/src/videoStudio/Timeline_items.tsx` (export `Handle` and `ItemButton`; `kind` gains `'audio'`)
- Modify: `web/src/videoStudio/Timeline.tsx` (audio lanes under the video lane; drag and resize dispatch by item)
- Create: `web/src/videoStudio/Inspector_audio.tsx`
- Modify: `web/src/videoStudio/Inspector_clip.tsx` (its Audio and Speed controls move to `Inspector_audio.tsx`), `web/src/videoStudio/Inspector.tsx` (a sound gets its inspector)
- Modify: `web/src/videoStudio/VideoStudio_rail.tsx` (*Add audio*; `FilePicker`), `web/src/videoStudio/VideoStudio_mobile.tsx` (*Add audio*; a sound's three tools), `web/src/videoStudio/VideoStudio.tsx`
- Create: `web/src/styles/video-studio-audio.css`; Modify: `web/src/styles/index.css`
- Create: `web/e2e/video-studio-lane.spec.ts`
- Test: `web/src/mediaEdit/__tests__/videoMedia.test.ts`, `web/src/videoStudio/__tests__/VideoStudio_sources.test.ts`, `__tests__/VideoStudio_selection.test.ts` (new), `__tests__/Timeline_audio.test.tsx` (new), `__tests__/Inspector_audio.test.tsx` (new), `__tests__/VideoStudio_mobile.test.tsx`, `__tests__/VideoStudio_audio.test.tsx` (new), `__tests__/VideoStudio.test.tsx` (its `videoMedia` mock gains `probeAudio`)

**Interfaces:**
- Consumes: Task 1's `addAudio`, `moveAudio`, `trimAudio`, `splitAudio`, `setAudioProperties`, `findAudioItem`, `audioWindow`, `audioLength`; Task 2's compile (a sound added here is heard in the export).
- Produces: `probeAudio(blob, signal?): Promise<{duration; decodable}>`; `AUDIO_ACCEPT`; `REFUSAL_UNDECODABLE_SOUND`; `interface SourcePlacement {time; label?}`, `sourceEdit(probed, assetId, placement?)`; `overlayCount`, `addedItem`, `holds`, `unplayableClips`, `splitEdit(selectedId, time)`; `AudioLaneItems`; `type AudioTab = 'audio' | 'speed' | 'time'`, `VolumeSlider`, `SpeedSlider`, `ClipAudioControls({clip, onCommand})`, `AudioItemInspector`; `FilePicker`. Task 4 adds its button to `ClipAudioControls`.

Rulings carried into this task (ledgered at execution): the rail's *Add audio* opens an audio file picker directly — the panel with *Record voice* and *Text to speech* is Plan C's (T5), and a panel with one entry would be ceremony; audio lanes sit under the video lane, which is also "under the overlay lanes" (spec §UI) since overlays draw above it.

- [ ] **Step 1: Write the failing door tests**

Append to `web/src/mediaEdit/__tests__/videoMedia.test.ts` (and import `probeAudio` beside `probeVideo`):

```ts
describe('probeAudio', () => {
  it('reports the length of the sound and whether this browser decodes it', async () => {
    state.audio = { canDecode: () => Promise.resolve(false) };
    await expect(probeAudio(new Blob())).resolves.toEqual({ duration: 10, decodable: false });
    expect(state.disposed).toBe(1);
  });

  it('refuses a file without an audio track', async () => {
    state.audio = null;
    await expect(probeAudio(new Blob())).rejects.toThrow('no audio track');
    expect(state.disposed).toBe(1);
  });

  it('refuses at once under a signal that already aborted', async () => {
    const controller = new AbortController();
    controller.abort();
    await expect(probeAudio(new Blob(), controller.signal)).rejects.toHaveProperty('name', 'AbortError');
  });
});
```

In `web/src/videoStudio/__tests__/VideoStudio_sources.test.ts`: the `videoMedia` mock becomes `vi.hoisted(() => ({ probeVideo: vi.fn(), probeAudio: vi.fn() }))`, the imports add `REFUSAL_UNDECODABLE_SOUND`, `uploadSource`, and these cases go at the end:

```ts
const SOUND = { kind: 'audio' as const, duration: 6, width: 0, height: 0 };

describe('a sound at the door', () => {
  it('is probed as a sound when the bytes say they are one, and has no frame', async () => {
    media.probeAudio.mockResolvedValue({ duration: 6, decodable: true });
    await expect(probeSource(new Blob(['x'], { type: 'audio/wav' }))).resolves.toEqual(SOUND);
    expect(media.probeVideo).not.toHaveBeenCalled();
  });

  it('is refused in its own words when this browser cannot decode it or read it', async () => {
    media.probeAudio.mockResolvedValue({ duration: 6, decodable: false });
    await expect(probeSource(new Blob(['x'], { type: 'audio/ogg' }))).rejects.toThrow(REFUSAL_UNDECODABLE_SOUND);
    media.probeAudio.mockRejectedValue(new Error('no audio track'));
    await expect(probeSource(new Blob(['x'], { type: 'audio/ogg' }))).rejects.toThrow(REFUSAL_UNDECODABLE_SOUND);
  });

  it('goes on a lane at the time asked, named after its file, and never re-frames the project', () => {
    const film: VideoProject = {
      ...emptyProject('film', DEFAULT_SIZE, 30),
      sources: [{ id: 'src-a', assetId: 'a', kind: 'video', duration: 8, size: { width: 640, height: 360 } }],
      video: [{ id: 'clip-1', sourceId: 'src-a', duration: 8, sourceStart: 0, muted: false }],
    };
    const next = sourceEdit(SOUND, 'sound-asset', { time: 2, label: 'bed.wav' })(film);
    expect(next.size).toEqual(DEFAULT_SIZE);
    expect(next.sources.at(-1)).toMatchObject({ assetId: 'sound-asset', kind: 'audio', duration: 6 });
    expect(next.audio?.[0]?.items[0]).toMatchObject({ anchor: { clipId: 'clip-1', offset: 2 }, label: 'bed.wav' });
    expect(next.video).toHaveLength(1);
  });

  it('is refused, not parked, when there is no film to hang it on', () => {
    expect(() => sourceEdit(SOUND, 'sound-asset', { time: 0 })(emptyProject('empty', DEFAULT_SIZE, 30))).toThrow(
      'videoStudio.audio.refusal.noClip',
    );
  });
});
```

And, in the same file, `uploadSource` hints a sound as one (the `api` and `upload` modules are mocked like `VideoStudio.test.tsx` does, `vi.mock('../../chat/attachments/api', () => assets)` with `presignAsset` resolving `{ asset: { id: 'x' }, upload: { upload_url: 'u', required_headers: {} } }` and `finalizeMediaAsset` resolving `{ id: 'x' }`):

```ts
it('files a sound as a sound', async () => {
  await uploadSource(new File(['x'], 'bed.wav', { type: 'audio/wav' }));
  expect(assets.presignAsset).toHaveBeenCalledWith(expect.objectContaining({ modality_hint: 'audio' }));
});
```

- [ ] **Step 2: Run them to verify they fail**

Run: `MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/vt.sh src/mediaEdit/__tests__/videoMedia.test.ts src/videoStudio/__tests__/VideoStudio_sources.test.ts`
Expected: FAIL — `probeAudio` is not exported; `REFUSAL_UNDECODABLE_SOUND` is undefined; `sourceEdit` adds a clip instead of a sound.

- [ ] **Step 3: Write the door**

`videoMedia.ts` — the body of `probeVideo` becomes a reader over a shared opener:

```ts
export interface AudioInfo {
  readonly duration: number;
  readonly decodable: boolean;
}

/** Open the bytes, run `read` over them, and dispose of them whatever happens — at once when
 *  `signal` aborts, rather than after a read that may never return. */
function readInput<T>(source: Blob, read: (input: Input) => Promise<T>, signal?: AbortSignal): Promise<T> {
  if (signal?.aborted) return Promise.reject(abortError());
  const input = new Input({ source: new BlobSource(source), formats: ALL_FORMATS });
  let stop: () => void = () => undefined;
  const stopped = new Promise<never>((_resolve, reject) => {
    stop = () => {
      input.dispose();
      reject(abortError());
    };
  });
  signal?.addEventListener('abort', stop, { once: true });
  return Promise.race([read(input), stopped]).finally(() => {
    signal?.removeEventListener('abort', stop);
    input.dispose();
  });
}

export function probeVideo(source: Blob, signal?: AbortSignal): Promise<VideoInfo> {
  return readInput(
    source,
    async (input) => {
      const video = await input.getPrimaryVideoTrack();
      if (video === null) throw new Error('the file has no video track');
      const audio = await input.getPrimaryAudioTrack();
      return {
        duration: await input.computeDuration(),
        width: await video.getDisplayWidth(),
        height: await video.getDisplayHeight(),
        hasAudio: audio !== null,
        decodable: await video.canDecode(),
      };
    },
    signal,
  );
}

/** A sound's length, and whether this browser can decode it: the audio lane's door. */
export function probeAudio(source: Blob, signal?: AbortSignal): Promise<AudioInfo> {
  return readInput(
    source,
    async (input) => {
      const audio = await input.getPrimaryAudioTrack();
      if (audio === null) throw new Error('the file has no audio track');
      return { duration: await input.computeDuration(), decodable: await audio.canDecode() };
    },
    signal,
  );
}
```

`VideoStudio_sources.ts`:
- imports: `probeAudio` beside `probeVideo`; `import { addAudio } from './commands_audio';`
- the refusals comment reads "The three refusals the SHELL raises" and gains `export const REFUSAL_UNDECODABLE_SOUND = 'videoStudio.audio.refusal.undecodable';`
- the accept lists:

```ts
/**
 * What the audio picker takes: MIME types only, because the probe is routed by the picked file's
 * type and the server files a sound by it (`InferModality`, audio/* → ModalityAudio). Every one is
 * a container Chromium and mediabunny both read.
 */
export const AUDIO_ACCEPT =
  'audio/mpeg,audio/wav,audio/x-wav,audio/mp4,audio/x-m4a,audio/aac,audio/ogg,audio/webm,audio/flac';

/** …the existing paragraph… A sound picked here goes to an audio lane, as Clideo does. */
export const SOURCE_ACCEPT = `video/mp4,video/webm,image/png,image/jpeg,image/webp,${AUDIO_ACCEPT}`;
```

- the probe (after `probeImage`):

```ts
/** Read a sound, or refuse it in its own words: the clip's refusal speaks of a black export. */
async function probeSound(bytes: Blob): Promise<ProbedSource> {
  let probed;
  try {
    probed = await probeAudio(bytes);
  } catch {
    throw new CommandRefusal(REFUSAL_UNDECODABLE_SOUND);
  }
  if (!probed.decodable) throw new CommandRefusal(REFUSAL_UNDECODABLE_SOUND);
  return { kind: 'audio', duration: probed.duration, width: 0, height: 0 };
}
```

and in `probeSource`, after the image line: `if (bytes.type.startsWith('audio/')) return probeSound(bytes);`

- `sourceEdit`:

```ts
/** Where a probed SOUND goes: the project time it starts at and the name its item shows. A clip
 *  or a still needs neither — it joins the end of the video lane. */
export interface SourcePlacement {
  readonly time: number;
  readonly label?: string | undefined;
}

export function sourceEdit(
  probed: ProbedSource,
  assetId: string,
  placement: SourcePlacement = { time: 0 },
): Edit {
  const size = { width: probed.width, height: probed.height };
  return (project) => {
    const source: ProjectSource = { …unchanged… };
    const withSource: VideoProject = {
      ...project,
      // A sound has no frame to give: only a picture re-frames a project nobody framed.
      size: probed.kind !== 'audio' && framedByDefault(project) ? size : project.size,
      sources: [...project.sources, source],
    };
    if (probed.kind === 'audio') {
      return addAudio(withSource, { sourceId: source.id, time: placement.time, label: placement.label });
    }
    return addClip(withSource, {
      sourceId: source.id,
      duration: probed.kind === 'image' ? IMAGE_SECONDS : probed.duration,
    });
  };
}
```

(its doc comment gains: "A sound goes on the first free audio lane at `placement.time` instead, hung on the clip there.")

- `uploadSource`: `modality_hint: file.type.startsWith('image/') ? 'image' : file.type.startsWith('audio/') ? 'audio' : 'video',` and the comment above it: "…A still is filed as one, and a sound as one."

- [ ] **Step 4: Run them to verify they pass**

Run the same command. Expected: PASS.

- [ ] **Step 5: Write the failing selection, lane and inspector tests**

`web/src/videoStudio/__tests__/VideoStudio_selection.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import type { VideoProject } from '../project';
import { addedItem, holds, overlayCount, splitEdit, unplayableClips } from '../VideoStudio_selection';

function project(): VideoProject {
  return {
    id: 'p',
    name: 'demo',
    size: { width: 320, height: 180 },
    fps: 30,
    sources: [
      { id: 'src-a', assetId: 'a', kind: 'video', duration: 10, size: { width: 320, height: 180 } },
      { id: 'src-m', assetId: 'm', kind: 'audio', duration: 8, size: { width: 0, height: 0 } },
    ],
    video: [
      { id: 'clip-1', sourceId: 'src-a', duration: 4, sourceStart: 0, muted: false },
      { id: 'clip-2', sourceId: 'src-a', duration: 4, sourceStart: 4, muted: false },
    ],
    overlays: [{ id: 'o', items: [{ id: 'title', kind: 'text', anchor: { clipId: 'clip-1', offset: 0 }, duration: 1, props: {} }] }],
    audio: [
      {
        id: 'lane',
        items: [{ id: 'bed', sourceId: 'src-m', anchor: { clipId: 'clip-1', offset: 0 }, sourceStart: 0, duration: 8, volume: 1, muted: false }],
      },
    ],
  };
}

describe('the selection helpers', () => {
  it('knows a clip, an overlay and a sound by id, and nothing else', () => {
    for (const id of ['clip-2', 'title', 'bed']) expect(holds(project(), id)).toBe(true);
    expect(holds(project(), 'gone')).toBe(false);
    expect(holds(project(), undefined)).toBe(false);
  });

  it('names the overlay or sound an edit added, for the workspace to select', () => {
    const base = project();
    const withSound: VideoProject = {
      ...base,
      audio: [{ id: 'lane', items: [...(base.audio?.[0]?.items ?? []), { ...base.audio![0]!.items[0]!, id: 'voice' }] }],
    };
    expect(addedItem(base, withSound)).toBe('voice');
    expect(addedItem(base, base)).toBeUndefined();
  });

  it('counts overlays and names the clips whose source is gone', () => {
    expect(overlayCount(project())).toBe(1);
    expect(unplayableClips(project(), ['src-a'])).toEqual(['clip-1', 'clip-2']);
  });

  it('splits the selected sound at the time, and the clip under it when a sound is not selected', () => {
    const cutSound = splitEdit('bed', 2)(project());
    expect(cutSound.audio?.[0]?.items).toHaveLength(2);
    expect(cutSound.video).toHaveLength(2);
    const cutClip = splitEdit('clip-1', 2)(project());
    expect(cutClip.video).toHaveLength(3);
    expect(splitEdit(undefined, 2)(project()).video).toHaveLength(3);
  });
});
```

(oxlint forbids the non-null assertions in `withSound`: write the added item through a local `const bed = base.audio?.[0]?.items[0]; if (bed === undefined) throw new Error('fixture lost its sound');` first.)

`web/src/videoStudio/__tests__/Timeline_audio.test.tsx` — the dnd-timeline seam is intercepted exactly as `Timeline.test.tsx` does (copy its `gestures` hoist, its `vi.mock('dnd-timeline', …)` and its key-echoing `react-i18next` mock), then:

```ts
import { fireEvent, render, screen } from '@testing-library/react';
import type { DragEndEvent, ResizeEndEvent, Span, TimelineContextProps, useTimelineMonitor } from 'dnd-timeline';
import { describe, expect, it, vi } from 'vitest';
import type { AudioItem, VideoProject } from '../project';
import { Timeline } from '../Timeline';

function sound(id: string, offset: number, over: Partial<AudioItem> = {}): AudioItem {
  return { id, sourceId: 'src-m', anchor: { clipId: 'clip-1', offset }, sourceStart: 1, duration: 2, volume: 1, muted: false, ...over };
}

function project(): VideoProject {
  return {
    id: 'p',
    name: 'demo',
    size: { width: 320, height: 180 },
    fps: 25,
    sources: [
      { id: 'src-a', assetId: 'a', kind: 'video', duration: 20, size: { width: 320, height: 180 } },
      { id: 'src-m', assetId: 'm', kind: 'audio', duration: 6, size: { width: 0, height: 0 } },
    ],
    video: [{ id: 'clip-1', sourceId: 'src-a', duration: 8, sourceStart: 0, muted: false }],
    overlays: [],
    audio: [
      { id: 'lane-a', items: [sound('bed', 0, { label: 'bed.wav' }), sound('tail', 2)] },
      { id: 'lane-b', items: [sound('voice', 4)] },
    ],
  };
}

function released(id: string, span: Span, extra: Record<string, unknown> = {}) {
  const strategy = () => span;
  return {
    active: { id, data: { current: { span, getSpanFromDragEvent: strategy, getSpanFromResizeEvent: strategy } } },
    ...extra,
  } as unknown as DragEndEvent & ResizeEndEvent;
}

function mount(selectedId?: string) {
  const onCommand = vi.fn();
  const onSelect = vi.fn();
  render(<Timeline project={project()} selectedId={selectedId} playhead={0} onCommand={onCommand} onSelect={onSelect} onScrub={vi.fn()} />);
  const applied = () => (onCommand.mock.calls[0]?.[0] as (p: VideoProject) => VideoProject)(project());
  return { onCommand, onSelect, applied };
}

const items = (next: VideoProject) => next.audio?.flatMap((lane) => lane.items) ?? [];

describe('Timeline, on the audio lanes', () => {
  it('draws one lane per audio track under the video lane, numbering sounds across lanes', () => {
    mount();
    const lanes = screen.getAllByRole('group').map((lane) => lane.getAttribute('aria-label'));
    expect(lanes.slice(-3)).toEqual(['videoStudio.timeline.videoLane', 'videoStudio.audio.lane 1', 'videoStudio.audio.lane 2']);
    expect(screen.getByRole('button', { name: 'videoStudio.audio.item 3' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'videoStudio.audio.item 1' }).textContent).toContain('bed.wav');
  });

  it('selects a sound on press', () => {
    const { onSelect } = mount();
    fireEvent.pointerDown(screen.getByRole('button', { name: 'videoStudio.audio.item 2' }));
    expect(onSelect).toHaveBeenCalledWith('tail');
  });

  it('drops a dragged sound where it is let go, onto the sound lane under it', () => {
    const { applied } = mount();
    gestures.drag?.(released('voice', { start: 6, end: 8 }, { over: { id: 'lane-a' } }));
    const next = applied();
    expect(next.audio?.[0]?.items.map((item) => item.id)).toContain('voice');
    expect(items(next).find((item) => item.id === 'voice')?.anchor).toEqual({ clipId: 'clip-1', offset: 6 });
  });

  it('keeps a sound on its own lane when it is let go over the video lane', () => {
    const { applied } = mount();
    gestures.drag?.(released('voice', { start: 5, end: 7 }, { over: { id: 'video' } }));
    expect(applied().audio?.[1]?.items[0]?.anchor.offset).toBe(5);
  });

  it('trims only the edge that was dragged, keeping the other exact', () => {
    const head = mount();
    gestures.resize?.(released('voice', { start: 4.5, end: 6 }, { direction: 'start' }));
    expect(items(head.applied()).find((item) => item.id === 'voice')).toMatchObject({ sourceStart: 1.5, duration: 1.5 });
  });

  it('steps a sound handle by a frame from the keyboard', () => {
    const { applied } = mount('voice');
    fireEvent.keyDown(screen.getByRole('slider', { name: 'videoStudio.audio.trimEnd 3' }), { key: 'ArrowLeft' });
    expect(items(applied()).find((item) => item.id === 'voice')?.duration).toBeCloseTo(2 - 1 / 25, 9);
  });
});
```

`web/src/videoStudio/__tests__/Inspector_audio.test.tsx` — the key-echoing `react-i18next` mock and the `mount`/`commit`/`openTab` helpers of `Inspector.test.tsx` (copied), then:

```ts
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { Inspector } from '../Inspector';
import type { AudioItem, VideoProject } from '../project';

function project(item: Partial<AudioItem> = {}): VideoProject {
  return {
    id: 'p',
    name: 'demo',
    size: { width: 320, height: 180 },
    fps: 25,
    sources: [
      { id: 'src-a', assetId: 'a', kind: 'video', duration: 20, size: { width: 320, height: 180 }, hasAudio: true },
      { id: 'src-m', assetId: 'm', kind: 'audio', duration: 8, size: { width: 0, height: 0 } },
    ],
    video: [{ id: 'clip-1', sourceId: 'src-a', duration: 8, sourceStart: 0, muted: false }],
    overlays: [],
    audio: [{ id: 'lane', items: [{ id: 'bed', sourceId: 'src-m', anchor: { clipId: 'clip-1', offset: 1 }, sourceStart: 2, duration: 4, volume: 1, muted: false, ...item }] }],
  };
}

const bed = (next: VideoProject) => next.audio?.[0]?.items[0];

describe('Inspector, on a sound', () => {
  it('offers the sound its three tabs, and falls back to Audio from a tab it does not have', () => {
    render(<Inspector project={project()} selectedId="bed" onCommand={vi.fn()} activeClipTab="adjust" />);
    expect(screen.getAllByRole('tab').map((tab) => tab.textContent)).toEqual([
      'videoStudio.inspector.tabs.audio',
      'videoStudio.inspector.tabs.speed',
      'videoStudio.inspector.tabs.time',
    ]);
    expect(screen.getByRole('slider', { name: 'videoStudio.inspector.volume' })).toBeTruthy();
  });

  it('commits volume, mute and both fades', () => {
    const view = mount(project(), 'bed');
    fireEvent.keyDown(screen.getByRole('slider', { name: 'videoStudio.inspector.volume' }), { key: 'PageDown' });
    expect(bed(view.applied())?.volume).toBeCloseTo(0.9, 9);
    fireEvent.click(screen.getByRole('switch', { name: 'videoStudio.inspector.mute' }));
    expect(bed(view.commands[1]?.(project()) ?? project())?.muted).toBe(true);
    fireEvent.keyDown(screen.getByRole('slider', { name: 'videoStudio.audio.fadeIn' }), { key: 'PageUp' });
    expect(bed(view.commands[2]?.(project()) ?? project())?.fadeIn).toBeCloseTo(1, 9);
    fireEvent.keyDown(screen.getByRole('slider', { name: 'videoStudio.audio.fadeOut' }), { key: 'End' });
    expect(bed(view.commands[3]?.(project()) ?? project())?.fadeOut).toBe(4);
  });

  it('sets the speed from a preset', () => {
    const view = mount(project(), 'bed');
    openTab('videoStudio.inspector.tabs.speed');
    fireEvent.click(screen.getByRole('button', { name: '2×' }));
    expect(bed(view.applied())?.speed).toBe(2);
  });

  it('moves the sound from its start field and trims it from In and Out, in source seconds', () => {
    const view = mount(project(), 'bed');
    openTab('videoStudio.inspector.tabs.time');
    expect(screen.getByLabelText('videoStudio.audio.startsAt')).toHaveProperty('value', '00:01.0');
    commit(screen.getByLabelText('videoStudio.audio.startsAt'), '00:03.0');
    expect(bed(view.applied())?.anchor.offset).toBe(3);
    commit(screen.getByLabelText('videoStudio.audio.in'), '00:03.0');
    expect(bed(view.commands[1]?.(project()) ?? project())).toMatchObject({ sourceStart: 3, duration: 3 });
    commit(screen.getByLabelText('videoStudio.audio.out'), '00:05.0');
    expect(bed(view.commands[2]?.(project()) ?? project())).toMatchObject({ sourceStart: 2, duration: 3 });
  });
});

describe('Inspector, on a clip Audio tab', () => {
  it('still sets the clip volume and mute from the controls it now shares', () => {
    const view = mount(project(), 'clip-1');
    openTab('videoStudio.inspector.tabs.audio');
    fireEvent.keyDown(screen.getByRole('slider', { name: 'videoStudio.inspector.volume' }), { key: 'Home' });
    expect(view.applied().video[0]?.volume).toBe(0);
  });
});
```

`web/src/videoStudio/__tests__/VideoStudio_mobile.test.tsx` gains (its render helper passes the two new props, `soundSelected={false}` and `onAddAudio={vi.fn()}` by default):

```ts
it('offers Add audio beside the other add actions when nothing is selected', () => {
  const onAddAudio = vi.fn();
  renderTools({ selectedId: undefined, onAddAudio });
  fireEvent.click(screen.getByRole('button', { name: 'Add audio' }));
  expect(onAddAudio).toHaveBeenCalledTimes(1);
});

it('offers a sound only the tools a sound has', () => {
  renderTools({ selectedId: 'bed', soundSelected: true });
  expect(screen.queryByRole('button', { name: i18n.t('videoStudio.mobile.adjust') })).toBeNull();
  for (const key of ['speed', 'audio', 'time']) {
    expect(screen.getByRole('button', { name: i18n.t(`videoStudio.mobile.${key}`) })).toBeTruthy();
  }
});
```

(`renderTools` is the file's existing render helper, extended to accept these props; if it has none, one is written that renders `MobileVideoTools` with every handler a `vi.fn()` and the overrides spread last.)

`web/src/videoStudio/__tests__/VideoStudio_audio.test.tsx` — the mocks of `VideoStudio.test.tsx` copied (renderer-dom, renderer-browser, `../videoflow`, `../../mediaEdit/videoMedia` with `{ probeVideo: vi.fn(), probeAudio: vi.fn() }`, `../../mediaEdit/download`, `../projectStore`, `../../chat/attachments/api`, `../../chat/attachments/upload`), then:

```ts
function film(withSound = false): VideoProject {
  return {
    id: 'p',
    name: 'demo',
    size: { width: 1920, height: 1080 },
    fps: 25,
    sources: [
      { id: 'src-a', assetId: 'asset-a', kind: 'video', duration: 20, size: { width: 1920, height: 1080 } },
      { id: 'src-m', assetId: 'asset-m', kind: 'audio', duration: 8, size: { width: 0, height: 0 } },
    ],
    video: [
      { id: 'clip-1', sourceId: 'src-a', duration: 4, sourceStart: 0, muted: false },
      { id: 'clip-2', sourceId: 'src-a', duration: 4, sourceStart: 4, muted: false },
    ],
    overlays: [],
    ...(withSound
      ? { audio: [{ id: 'lane', items: [{ id: 'bed', sourceId: 'src-m', anchor: { clipId: 'clip-1', offset: 0 }, sourceStart: 0, duration: 8, volume: 1, muted: false }] }] }
      : {}),
  };
}

const sound = (index: number) => screen.findByRole('button', { name: i18n.t('videoStudio.audio.item', { index }) });

function pickSound(file = new File(['x'], 'bed.wav', { type: 'audio/wav' })): void {
  fireEvent.change(screen.getByLabelText(i18n.t('videoStudio.audio.pick')), { target: { files: [file] } });
}

beforeEach(() => {
  media.probeAudio.mockResolvedValue({ duration: 6, decodable: true });
  assets.presignAsset.mockResolvedValue({ asset: { id: 'sound-asset' }, upload: { upload_url: 'u', required_headers: {} } });
  assets.finalizeMediaAsset.mockResolvedValue({ id: 'sound-asset' });
});

afterEach(() => {
  vi.clearAllMocks();
});

describe('VideoStudio, with sounds', () => {
  it('puts a picked sound on an audio lane at the playhead, named after its file, and selects it', async () => {
    render(<VideoStudio open={{ kind: 'project', project: film() }} onClose={vi.fn()} />);
    pickSound();
    expect((await sound(1)).textContent).toContain('bed.wav');
    expect(assets.presignAsset).toHaveBeenCalledWith(expect.objectContaining({ modality_hint: 'audio' }));
    expect(screen.getByRole('tab', { name: i18n.t('videoStudio.inspector.tabs.audio') }).getAttribute('aria-selected')).toBe('true');
  });

  it('refuses a sound before uploading it when there is no film to hang it on', async () => {
    render(<VideoStudio open={{ kind: 'project', project: { ...film(), video: [] } }} onClose={vi.fn()} />);
    pickSound();
    expect((await screen.findByRole('alert')).textContent).toBe(i18n.t('videoStudio.audio.refusal.noClip'));
    expect(assets.presignAsset).not.toHaveBeenCalled();
  });

  it('splits the selected sound at the playhead and leaves the clips alone', async () => {
    render(<VideoStudio open={{ kind: 'project', project: film(true) }} onClose={vi.fn()} />);
    fireEvent.pointerDown(await sound(1));
    const playhead = screen.getByRole('slider', { name: i18n.t('videoStudio.timeline.playhead') });
    fireEvent.keyDown(playhead, { key: 'ArrowRight', shiftKey: true });
    fireEvent.keyDown(playhead, { key: 'ArrowRight', shiftKey: true });
    fireEvent.click(screen.getByRole('button', { name: i18n.t('videoStudio.command.split') }));
    expect(await sound(2)).toBeTruthy();
    expect(screen.queryByRole('button', { name: i18n.t('videoStudio.timeline.clip', { index: 3 }) })).toBeNull();
  });
});
```

In `VideoStudio.test.tsx` the `media` hoist becomes `{ probeVideo: vi.fn(), probeAudio: vi.fn() }` (one line; the file stays under 600).

- [ ] **Step 6: Run them to verify they fail**

Run: `MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/vt.sh src/videoStudio/__tests__/VideoStudio_selection.test.ts src/videoStudio/__tests__/Timeline_audio.test.tsx src/videoStudio/__tests__/Inspector_audio.test.tsx src/videoStudio/__tests__/VideoStudio_mobile.test.tsx src/videoStudio/__tests__/VideoStudio_audio.test.tsx`
Expected: FAIL — `../VideoStudio_selection` does not resolve; no audio lanes are drawn; a sound selected shows "select an item"; no *Add audio*; no audio picker.

- [ ] **Step 7: Write the selection helpers and the lane**

`web/src/videoStudio/VideoStudio_selection.ts`:

```ts
// VideoStudio_selection.ts — what the workspace asks of a project around its selection: whether an
// id still names something after an edit, which item an edit added, and what Split means for what
// is selected. Pure, so the shell's own file keeps to drawing.

import { findAudioItem } from './audioLane';
import { splitAt } from './commands';
import { splitAudio } from './commands_audio';
import { audioTracks, type VideoProject } from './project';

type Edit = (project: VideoProject) => VideoProject;

export function overlayCount(project: VideoProject): number {
  return project.overlays.reduce((total, lane) => total + lane.items.length, 0);
}

/** Overlays and sounds: the items an edit can add and the workspace then selects. */
function itemIds(project: VideoProject): readonly string[] {
  return [
    ...project.overlays.flatMap((lane) => lane.items.map((item) => item.id)),
    ...audioTracks(project).flatMap((lane) => lane.items.map((item) => item.id)),
  ];
}

export function addedItem(before: VideoProject, after: VideoProject): string | undefined {
  const had = new Set(itemIds(before));
  return itemIds(after).find((id) => !had.has(id));
}

export function holds(project: VideoProject, id: string | undefined): boolean {
  if (id === undefined) return false;
  return project.video.some((clip) => clip.id === id) || itemIds(project).includes(id);
}

export function unplayableClips(project: VideoProject, missing: readonly string[]): readonly string[] {
  return project.video.filter((clip) => missing.includes(clip.sourceId)).map((clip) => clip.id);
}

/** Split cuts what is selected when that is a sound, and the clip under the playhead otherwise. */
export function splitEdit(selectedId: string | undefined, time: number): Edit {
  return (project) =>
    selectedId !== undefined && findAudioItem(project, selectedId) !== undefined
      ? splitAudio(project, { itemId: selectedId, time })
      : splitAt(project, { time });
}
```

`Timeline_items.tsx`: `export function Handle`, `export function ItemButton`, `readonly kind: 'clip' | 'overlay' | 'audio';`, and the header's first paragraph gains: "A sound on an audio lane is drawn by Timeline_audio.tsx, with this file's handle and block."

`web/src/videoStudio/Timeline_audio.tsx`:

```tsx
import { useItem, type Span } from 'dnd-timeline';
import { useTranslation } from 'react-i18next';
import { audioWindow } from './audioLane';
import { sourceOf, type AudioItem, type AudioTrack, type VideoProject } from './project';
import { Handle, ItemButton } from './Timeline_items';
import { TOUCH_FLOOR, type TrimSpan } from './timelineView';

// Timeline_audio.tsx — the sounds on an audio lane. A sound drags and trims like a clip, but a drop
// is a free position, not a place in a sequence: `moveAudio` re-hangs it on whatever clip is under
// its new start. As for a clip, the handles' pointer drag is dnd-timeline's and reaches the shell
// as `onResizeEnd`; a keystroke is the only edit this file emits.

/** Two edges this close are one boundary, and two handles on it would fight for the press. */
const TOUCHING = 1e-6;

interface AudioItemViewProps {
  readonly item: AudioItem;
  readonly number: number;
  readonly span: Span;
  readonly sourceEnd: number;
  readonly abutsStart: boolean;
  readonly abutsEnd: boolean;
  readonly frame: number;
  readonly selected: boolean;
  readonly onSelect: (id: string) => void;
  readonly onTrim: (itemId: string, args: TrimSpan) => void;
}

function AudioItemView({
  item,
  number,
  span,
  sourceEnd,
  abutsStart,
  abutsEnd,
  frame,
  selected,
  onSelect,
  onTrim,
}: AudioItemViewProps) {
  const { t } = useTranslation();
  const { setNodeRef, setActivatorNodeRef, attributes, listeners, itemStyle, itemContentStyle } =
    useItem({ id: item.id, span, resizeHandleWidth: TOUCH_FLOOR });
  const position = { index: number };
  const label = t('videoStudio.audio.item', position);
  const end = item.sourceStart + item.duration;
  return (
    <div
      ref={setNodeRef}
      style={itemStyle}
      onPointerDown={listeners.onPointerDown}
      onPointerMove={listeners.onPointerMove}
    >
      <div style={itemContentStyle}>
        <ItemButton
          label={label}
          length={span.end - span.start}
          selected={selected}
          kind="audio"
          preview={<span className="video-studio-audio-name">{item.label ?? label}</span>}
          idle="border-border bg-surface-3"
          attributes={attributes}
          activatorRef={setActivatorNodeRef}
          onSelect={() => {
            onSelect(item.id);
          }}
        />
      </div>
      <Handle
        side="start"
        abuts={abutsStart}
        selected={selected}
        label={t('videoStudio.audio.trimStart', position)}
        value={item.sourceStart}
        min={0}
        max={end - frame}
        frame={frame}
        onSet={(at) => {
          onTrim(item.id, { start: at - item.sourceStart, end: item.duration });
        }}
      />
      <Handle
        side="end"
        abuts={abutsEnd}
        selected={selected}
        label={t('videoStudio.audio.trimEnd', position)}
        value={end}
        min={item.sourceStart + frame}
        max={sourceEnd}
        frame={frame}
        onSet={(at) => {
          onTrim(item.id, { start: 0, end: at - item.sourceStart });
        }}
      />
    </div>
  );
}

interface AudioLaneItemsProps {
  readonly project: VideoProject;
  readonly track: AudioTrack;
  /** How many sounds the lanes above hold: the numbering runs across lanes, so a name is unique. */
  readonly before: number;
  readonly selectedId: string | undefined;
  readonly onSelect: (id: string) => void;
  readonly onTrim: (itemId: string, args: TrimSpan) => void;
}

/** One lane's sounds. A handle beside a neighbour stays inside its own sound, for the reason a
 *  clip's does (Timeline_items.tsx, `Handle`). */
export function AudioLaneItems({ project, track, before, selectedId, onSelect, onTrim }: AudioLaneItemsProps) {
  const windows = track.items.map((item) => audioWindow(project, item));
  const touches = (at: number, edge: 'start' | 'end', self: number) =>
    windows.some((other, index) => index !== self && Math.abs(other[edge] - at) < TOUCHING);
  return (
    <>
      {track.items.map((item, index) => {
        const span = windows[index] ?? { start: 0, end: 0 };
        return (
          <AudioItemView
            key={item.id}
            item={item}
            number={before + index + 1}
            span={span}
            sourceEnd={Math.max(sourceOf(project, item.sourceId)?.duration ?? 0, item.sourceStart + item.duration)}
            abutsStart={touches(span.start, 'end', index)}
            abutsEnd={touches(span.end, 'start', index)}
            frame={1 / project.fps}
            selected={item.id === selectedId}
            onSelect={onSelect}
            onTrim={onTrim}
          />
        );
      })}
    </>
  );
}
```

`Timeline.tsx`:
- imports: `import { audioWindow, findAudioItem } from './audioLane';`, `import { moveAudio, trimAudio } from './commands_audio';`, `audioTracks` from `./project`, `import { AudioLaneItems } from './Timeline_audio';`
- header, last paragraph gains: "A sound's lane is not a sequence: its drop is a free position, re-hung on the clip under it."
- `LanesProps` gains `readonly onMoveAudio: (itemId: string, start: number, trackId: string | undefined) => void;` and `readonly onTrimAudio: (itemId: string, args: TrimSpan) => void;` (and `Lanes` destructures both).
- `onDragEnd`'s last two lines become:

```ts
      const id = String(drag.active.id);
      if (findAudioItem(project, id) !== undefined) {
        // A sound lands where it is let go, onto the sound lane it is over; over anything else it
        // keeps its own lane.
        const over = drag.over?.id;
        onMoveAudio(id, span.start, audioTracks(project).find((track) => track.id === over)?.id);
        return;
      }
      onMove(id, insertIndexFor(project, id, span.start));
```

- after the video `</Lane>`:

```tsx
      {audioTracks(project).map((track, index, tracks) => (
        <Lane
          key={track.id}
          id={track.id}
          label={t('videoStudio.audio.lane', { index: index + 1 })}
          droppable
        >
          <AudioLaneItems
            project={project}
            track={track}
            before={tracks.slice(0, index).reduce((count, lane) => count + lane.items.length, 0)}
            selectedId={selectedId}
            onSelect={onSelect}
            onTrim={onTrimAudio}
          />
        </Lane>
      ))}
```

- in `Timeline`:

```ts
  function onMoveAudio(itemId: string, start: number, trackId: string | undefined) {
    onCommand((current) => moveAudio(current, { itemId, start, trackId }));
  }
  function onTrimAudio(itemId: string, args: TrimSpan) {
    onCommand((current) => trimAudio(current, { itemId, ...args }));
  }
```

passed to `<Lanes … onMoveAudio={onMoveAudio} onTrimAudio={onTrimAudio} />`, and `onResizeEnd` becomes:

```tsx
      onResizeEnd={(event: ResizeEndEvent) => {
        const id = String(event.active.id);
        const span = event.active.data.current.getSpanFromResizeEvent?.(event);
        if (span === null || span === undefined) return;
        const sound = findAudioItem(project, id);
        if (sound !== undefined) {
          // Only the dragged edge moves and the other is kept exactly: a sound the film's end cuts
          // short keeps the part nobody can see instead of losing it to the visible span.
          const moved = trimArgsFromSpan(audioWindow(project, sound).start, span, sound.speed);
          onTrimAudio(
            id,
            event.direction === 'start'
              ? { start: moved.start, end: sound.duration }
              : { start: 0, end: moved.end },
          );
          return;
        }
        const from = clipStart(project, id);
        const clip = project.video.find((item) => item.id === id);
        if (from === undefined || clip === undefined) return;
        onTrim(id, trimArgsFromSpan(from, span, clip.speed));
      }}
```

- [ ] **Step 8: Write the inspector**

`web/src/videoStudio/Inspector_audio.tsx`:

```tsx
import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { formatTimecode } from '../mediaEdit/timecode';
import { TimeField } from '../mediaEdit/TimeField';
import { audioLength, audioWindow } from './audioLane';
import { setClipPresentation, setMuted } from './commands';
import {
  moveAudio,
  setAudioProperties,
  trimAudio,
  type SetAudioPropertiesArgs,
} from './commands_audio';
import type { AudioItem, VideoItem, VideoProject } from './project';
import { Button } from '@/components/ui/button';
import { Slider } from '@/components/ui/slider';
import { Switch } from '@/components/ui/switch';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';

// Inspector_audio.tsx — what a sound shows in the inspector, and the audio controls a clip shares
// with it: one volume slider, one mute, one speed control for both, so the two never drift.

type Commit = (edit: (current: VideoProject) => VideoProject) => void;
export type AudioTab = 'audio' | 'speed' | 'time';
const AUDIO_TABS: readonly AudioTab[] = ['audio', 'speed', 'time'];
const MAX_FADE = 5;

/** Volume, 0–200 %: a clip's and a sound's. */
export function VolumeSlider({ volume, onCommit }: { readonly volume: number; readonly onCommit: (volume: number) => void }) {
  const { t } = useTranslation();
  return (
    <label className="video-studio-adjustment">
      <span>{t('videoStudio.inspector.volume')}</span>
      <span className="font-mono tabular-nums">{Math.round(volume * 100)}%</span>
      <Slider
        key={volume}
        aria-label={t('videoStudio.inspector.volume')}
        min={0}
        max={200}
        step={1}
        defaultValue={[Math.round(volume * 100)]}
        onValueCommit={([next = 100]) => {
          onCommit(next / 100);
        }}
      />
    </label>
  );
}

function MuteSwitch({ muted, onChange }: { readonly muted: boolean; readonly onChange: (muted: boolean) => void }) {
  const { t } = useTranslation();
  const id = useId();
  return (
    <div className="flex items-center gap-2 text-xs text-text-muted">
      <Switch id={id} checked={muted} aria-label={t('videoStudio.inspector.mute')} onCheckedChange={onChange} />
      <label htmlFor={id}>{t('videoStudio.inspector.mute')}</label>
    </div>
  );
}

/** The playback rate, 0.25–4×, with four presets: a clip's and a sound's. */
export function SpeedSlider({ speed, onCommit }: { readonly speed: number; readonly onCommit: (speed: number) => void }) {
  const { t } = useTranslation();
  return (
    <>
      <label className="video-studio-adjustment">
        <span>{t('videoStudio.inspector.playbackRate')}</span>
        <span className="font-mono tabular-nums">{speed.toFixed(2)}×</span>
        <Slider
          key={speed}
          aria-label={t('videoStudio.inspector.playbackRate')}
          min={25}
          max={400}
          step={5}
          defaultValue={[Math.round(speed * 100)]}
          onValueCommit={([next = 100]) => {
            onCommit(next / 100);
          }}
        />
      </label>
      <div className="grid grid-cols-4 gap-1">
        {[0.5, 1, 1.5, 2].map((value) => (
          <Button
            key={value}
            type="button"
            variant={speed === value ? 'default' : 'ghost'}
            size="sm"
            onClick={() => {
              onCommit(value);
            }}
          >
            {value}×
          </Button>
        ))}
      </div>
    </>
  );
}

/** A clip's Audio tab. */
export function ClipAudioControls({ clip, onCommand }: { readonly clip: VideoItem; readonly onCommand: Commit }) {
  return (
    <div className="video-studio-tab-panel">
      <VolumeSlider
        volume={clip.volume ?? 1}
        onCommit={(volume) => {
          onCommand((current) => setClipPresentation(current, { clipId: clip.id, volume }));
        }}
      />
      <MuteSwitch
        muted={clip.muted}
        onChange={(muted) => {
          onCommand((current) => setMuted(current, { clipId: clip.id, muted }));
        }}
      />
    </div>
  );
}

function FadeSlider({ label, value, max, onCommit }: { readonly label: string; readonly value: number; readonly max: number; readonly onCommit: (seconds: number) => void }) {
  return (
    <label className="video-studio-adjustment">
      <span>{label}</span>
      <span className="font-mono tabular-nums">{value.toFixed(1)}s</span>
      <Slider
        key={value}
        aria-label={label}
        min={0}
        max={max}
        step={0.1}
        defaultValue={[value]}
        onValueCommit={([next = value]) => {
          onCommit(next);
        }}
      />
    </label>
  );
}

interface AudioItemInspectorProps {
  readonly project: VideoProject;
  readonly item: AudioItem;
  readonly onCommand: Commit;
  /** The workspace's tab, shared with the clip inspector; one a sound lacks falls back to Audio. */
  readonly activeTab?: string;
  readonly onTabChange?: (tab: AudioTab) => void;
}

export function AudioItemInspector({ project, item, onCommand, activeTab, onTabChange }: AudioItemInspectorProps) {
  const { t } = useTranslation();
  const [internalTab, setInternalTab] = useState<AudioTab>('audio');
  const asked = activeTab ?? internalTab;
  const tab = AUDIO_TABS.find((candidate) => candidate === asked) ?? 'audio';
  const set = (values: Omit<SetAudioPropertiesArgs, 'itemId'>) => {
    onCommand((current) => setAudioProperties(current, { itemId: item.id, ...values }));
  };
  const trim = (start: number, end: number) => {
    onCommand((current) => trimAudio(current, { itemId: item.id, start, end }));
  };
  const starts = audioWindow(project, item).start;
  const fadeRoom = Math.min(MAX_FADE, audioLength(item));
  const end = item.sourceStart + item.duration;
  return (
    <Tabs
      value={tab}
      onValueChange={(value) => {
        const next = AUDIO_TABS.find((candidate) => candidate === value) ?? 'audio';
        setInternalTab(next);
        onTabChange?.(next);
      }}
      className="min-w-0 gap-3"
    >
      <TabsList className="video-studio-tool-tabs">
        {AUDIO_TABS.map((name) => (
          <TabsTrigger key={name} value={name}>
            {t(`videoStudio.inspector.tabs.${name}`)}
          </TabsTrigger>
        ))}
      </TabsList>
      <TabsContent value="audio">
        <div className="video-studio-tab-panel">
          <VolumeSlider volume={item.volume} onCommit={(volume) => { set({ volume }); }} />
          <MuteSwitch muted={item.muted} onChange={(muted) => { set({ muted }); }} />
          <FadeSlider label={t('videoStudio.audio.fadeIn')} value={item.fadeIn ?? 0} max={fadeRoom} onCommit={(fadeIn) => { set({ fadeIn }); }} />
          <FadeSlider label={t('videoStudio.audio.fadeOut')} value={item.fadeOut ?? 0} max={fadeRoom} onCommit={(fadeOut) => { set({ fadeOut }); }} />
        </div>
      </TabsContent>
      <TabsContent value="speed">
        <div className="video-studio-tab-panel">
          <SpeedSlider speed={item.speed ?? 1} onCommit={(speed) => { set({ speed }); }} />
        </div>
      </TabsContent>
      <TabsContent value="time">
        <div className="video-studio-tab-panel">
          <TimeField
            key={`at-${item.id}-${formatTimecode(starts)}`}
            label={t('videoStudio.audio.startsAt')}
            value={starts}
            onCommit={(start) => {
              onCommand((current) => moveAudio(current, { itemId: item.id, start }));
            }}
          />
          <TimeField
            key={`in-${item.id}-${formatTimecode(item.sourceStart)}`}
            label={t('videoStudio.audio.in')}
            value={item.sourceStart}
            onCommit={(at) => { trim(at - item.sourceStart, item.duration); }}
          />
          <TimeField
            key={`out-${item.id}-${formatTimecode(end)}`}
            label={t('videoStudio.audio.out')}
            value={end}
            onCommit={(at) => { trim(0, at - item.sourceStart); }}
          />
        </div>
      </TabsContent>
    </Tabs>
  );
}
```

(prettier expands the one-line handlers; the tab trigger keys use a template key, covered by these tests rather than the static i18n gate.)

`Inspector_clip.tsx`: delete `AudioControls` and `SpeedControls`; `import { ClipAudioControls, SpeedSlider, type AudioTab } from './Inspector_audio';`; `export type ClipTab = 'transform' | 'animation' | 'adjust' | AudioTab;`; the Audio tab renders `<ClipAudioControls clip={props.clip} onCommand={props.onCommand} />`; the Speed tab renders

```tsx
        <div className="video-studio-tab-panel">
          <SpeedSlider
            speed={props.clip.speed ?? 1}
            onCommit={(speed) => {
              props.onCommand((current) =>
                setClipPresentation(current, { clipId: props.clip.id, speed }),
              );
            }}
          />
        </div>
```

and the imports the move leaves unused (`useId`, `setMuted`, `Switch`, `Slider` if no longer used) go.

`Inspector.tsx`: `import { findAudioItem } from './audioLane';`, `import { AudioItemInspector } from './Inspector_audio';`, `const sound = selectedId === undefined ? undefined : findAudioItem(project, selectedId);`, and between the clip and overlay branches:

```tsx
      ) : sound !== undefined ? (
        <AudioItemInspector
          project={project}
          item={sound}
          onCommand={onCommand}
          {...(activeClipTab === undefined ? {} : { activeTab: activeClipTab })}
          {...(onClipTabChange === undefined ? {} : { onTabChange: onClipTabChange })}
        />
```

- [ ] **Step 9: Wire the workspace**

`VideoStudio_rail.tsx`: `Music` from lucide-react; props gain `readonly onAddAudio: () => void;`; after the *Add source* button:

```tsx
      <button type="button" className="video-studio-rail-button" onClick={onAddAudio}>
        <Music aria-hidden="true" />
        <span>{t('videoStudio.audio.add')}</span>
      </button>
```

and, below `StudioRail`:

```tsx
interface FilePickerProps {
  readonly inputRef: RefObject<HTMLInputElement | null>;
  readonly accept: string;
  readonly label: string;
  readonly onFile: (file: File) => void;
}

/** A hidden file input the rail's buttons open. It is cleared on every pick: the same file picked
 *  twice in a row fires no change otherwise, and a retry after a refusal is exactly that case. */
export function FilePicker({ inputRef, accept, label, onFile }: FilePickerProps) {
  return (
    <input
      ref={inputRef}
      type="file"
      accept={accept}
      className="sr-only"
      aria-label={label}
      onChange={(event) => {
        const file = event.target.files?.[0];
        event.target.value = '';
        if (file !== undefined) onFile(file);
      }}
    />
  );
}
```

`VideoStudio_mobile.tsx`: `Music` from lucide-react; props gain `readonly soundSelected: boolean;` and `readonly onAddAudio: () => void;`;

```ts
/** A sound has no frame, no look and no motion: only these of the clip's tools apply to it. */
const SOUND_TABS: readonly ClipTab[] = ['speed', 'audio', 'time'];
```

inside the component `const tools = soundSelected ? INSPECTOR_TOOLS.filter(({ tab }) => SOUND_TABS.includes(tab)) : INSPECTOR_TOOLS;` and both `INSPECTOR_TOOLS.slice(…)` become `tools.slice(…)`; the nothing-selected bar gains, after *Add source*:

```tsx
        <Button type="button" variant="ghost" className="video-studio-mobile-tool" onClick={onAddAudio}>
          <Music aria-hidden="true" />
          <span>{t('videoStudio.audio.add')}</span>
        </Button>
```

`VideoStudio.tsx`:
- delete `overlayCount`, `addedOverlay`, `holds`, `unplayableClips`; `import { addedItem, holds, overlayCount, splitEdit, unplayableClips } from './VideoStudio_selection';`; `import { findAudioItem } from './audioLane';`; `splitAt` leaves the commands import; `AUDIO_ACCEPT` joins the sources import; `FilePicker` joins the rail import.
- `const audioInput = useRef<HTMLInputElement>(null);` beside `fileInput`.
- `commit`: `reselect(next, addedItem(before, next));`
- `addFile`:

```ts
  async function addFile(file: File) {
    const at = playhead;
    setProblem(undefined);
    setStatus(says('videoStudio.source.reading'));
    try {
      // Probed first, uploaded second: a clip this browser cannot decode never costs a transfer.
      const probed = await probeSource(file);
      const placement = { time: at, label: file.name };
      // And tried before the transfer too: a sound with no film under it is refused here, not
      // after its bytes have become an asset nothing points at.
      const current = history?.current;
      if (current !== undefined) sourceEdit(probed, '', placement)(current);
      setStatus(says('videoStudio.source.uploading', { name: file.name }));
      const assetId = await uploadSource(file);
      setStatus(undefined);
      run(sourceEdit(probed, assetId, placement));
    } catch (error) {
      setStatus(undefined);
      setProblem(failure(error, 'videoStudio.source.failed'));
    }
  }
```

(the existing comment about clearing the status before the commit stays above `setStatus(undefined)`.)
- the rail: `onAddAudio={() => audioInput.current?.click()}` and `onSplit={() => { run(splitEdit(selectedId, playhead)); }}`.
- the inline `<input type="file" …>` becomes

```tsx
            <FilePicker
              inputRef={fileInput}
              accept={SOURCE_ACCEPT}
              label={t('videoStudio.source.pick')}
              onFile={(file) => void addFile(file)}
            />
            <FilePicker
              inputRef={audioInput}
              accept={AUDIO_ACCEPT}
              label={t('videoStudio.audio.pick')}
              onFile={(file) => void addFile(file)}
            />
```

- `MobileVideoTools` gains `soundSelected={selectedId !== undefined && findAudioItem(project, selectedId) !== undefined}`, `onAddAudio={() => audioInput.current?.click()}`, and its `onSplit` runs `splitEdit(selectedId, playhead)`.

`web/src/styles/video-studio-audio.css` (imported in `styles/index.css` after `video-studio-controls.css`):

```css
/* A sound on its lane: the clip's block in a colour of its own, so a lane of sound never reads as
   a lane of picture. */
.video-studio-audio {
  position: relative;
  gap: 6px;
  border-color: color-mix(in oklab, var(--video-studio-accent) 55%, var(--video-studio-border-strong));
  border-radius: 5px;
  background:
    repeating-linear-gradient(90deg, color-mix(in oklab, var(--video-studio-accent) 22%, transparent) 0 2px, transparent 2px 6px),
    color-mix(in oklab, var(--video-studio-accent) 14%, var(--video-studio-panel-strong));
  color: var(--video-studio-text);
}

.video-studio-audio[aria-current='true'] {
  border-color: var(--video-studio-selection);
  box-shadow: inset 0 0 0 2px var(--video-studio-selection);
}

.video-studio-audio-name {
  position: relative;
  z-index: 1;
  min-width: 0;
  overflow: hidden;
  font-size: 11px;
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}
```

- [ ] **Step 10: Run the unit tests to verify they pass**

Run: `MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/vt.sh src/videoStudio/ src/mediaEdit/`
Expected: PASS — every new test green, `Timeline.test.tsx`, `Inspector.test.tsx`, `VideoStudio.test.tsx`, `VideoStudio_mobile.test.tsx`, `VideoStudio_sources.test.ts` unchanged and green.

- [ ] **Step 11: Write the UI E2E**

`web/e2e/video-studio-lane.spec.ts`:

```ts
import { randomUUID } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import type { Locator, Page } from '@playwright/test';
import { expect, test } from './support/assetCleanup';
import { levelBetween, windowPowers } from './support/audioMeasure';
import { AUDIO_FIXTURES, exportTo, pressAddAction, reopen, uploadClip } from './support/videoStudio';

// video-studio-lane.spec.ts — sounds put in and shaped through the editor's own controls, the way
// an operator does it: the rail's Add audio, the inspector's sliders, Split on a selected sound.
// The export is then measured, so every control is proven by what it did to the file.

const FRAME = { width: 320, height: 180 };

/** Two muted 4 s clips: the only sound in the export is the one the test puts there. */
function silentFilm(clip: string, name: string) {
  return {
    id: randomUUID(),
    name,
    size: FRAME,
    fps: 30,
    sources: [{ id: 'src-a', assetId: clip, kind: 'video', duration: 4, size: FRAME, hasAudio: true }],
    video: ['clip-1', 'clip-2'].map((id) => ({ id, sourceId: 'src-a', duration: 4, sourceStart: 0, muted: true })),
    overlays: [],
  };
}

async function addMusic(page: Page, editor: Locator) {
  const chooser = page.waitForEvent('filechooser');
  await pressAddAction(editor, 'Add audio');
  await (await chooser).setFiles(resolve(AUDIO_FIXTURES, 'music.wav'));
  const sound = editor.getByRole('button', { name: 'Sound 1' });
  await expect(sound).toBeVisible({ timeout: 60_000 });
  await expect(sound).toContainText('music.wav');
  return sound;
}

test('a sound shaped in the inspector exports at the volume and fade it was given', async ({ page }, info) => {
  test.skip(
    info.project.name.startsWith('mobile'),
    'the sliders are driven from the keyboard, which a phone does not have; the phone path is the test below',
  );
  test.setTimeout(12 * 60_000);
  const clip = await uploadClip(page);
  const editor = await reopen(page, silentFilm(clip, 'lane ui check'), clip);
  await expect(editor.getByRole('button', { name: 'Clip 2' })).toBeVisible({ timeout: 60_000 });
  await addMusic(page, editor);
  const inspector = editor.getByRole('region', { name: 'Properties' });

  // The first half: 50 % and a 2 s fade in. PageDown is ten steps of the 0–200 % slider.
  const volume = inspector.getByRole('slider', { name: 'Volume' });
  for (let press = 0; press < 5; press += 1) await volume.press('PageDown');
  await expect(volume).toHaveAttribute('aria-valuenow', '50');
  const fadeIn = inspector.getByRole('slider', { name: 'Fade in' });
  await fadeIn.press('PageUp');
  await fadeIn.press('PageUp');
  await expect(fadeIn).toHaveAttribute('aria-valuenow', '2');

  // Split at 4 s with the sound selected: the sound is cut, the clips are not.
  const playhead = editor.getByRole('slider', { name: 'Playhead' });
  for (let press = 0; press < 4; press += 1) await playhead.press('Shift+ArrowRight');
  await editor.getByRole('button', { name: 'Split at the playhead' }).click();
  await expect(editor.getByRole('button', { name: 'Sound 2' })).toBeVisible();
  await expect(editor.getByRole('button', { name: 'Clip 3' })).toHaveCount(0);

  // The second half, which the split selected and which carries the 50 % over, back to 100 %.
  await editor.getByRole('button', { name: 'Sound 2' }).click();
  for (let press = 0; press < 5; press += 1) await volume.press('PageUp');
  await expect(volume).toHaveAttribute('aria-valuenow', '100');

  const path = await exportTo(page, editor, info, 'lane-ui.mp4');
  const powers = await windowPowers(page, readFileSync(path));
  const levels = {
    start: levelBetween(powers, 0, 0.1),
    soft: levelBetween(powers, 2.5, 3.5),
    full: levelBetween(powers, 4.5, 7.5),
  };
  await info.attach('lane-ui-levels', { contentType: 'application/json', body: JSON.stringify(levels, null, 2) });
  await info.attach('lane-ui-editor', { contentType: 'image/png', body: await editor.screenshot() });
  expect(levels.full - levels.soft).toBeGreaterThan(5);
  expect(levels.full - levels.soft).toBeLessThan(7);
  expect(levels.soft - levels.start).toBeGreaterThanOrEqual(15);
});

test('a sound goes on its lane under a thumb, with the tools a sound has', async ({ page }, info) => {
  test.skip(!info.project.name.startsWith('mobile'), 'the phone claims belong to the phone projects');
  test.setTimeout(6 * 60_000);
  const clip = await uploadClip(page);
  const editor = await reopen(page, silentFilm(clip, 'lane phone check'), clip);
  await expect(editor.getByRole('button', { name: 'Clip 2' })).toBeVisible({ timeout: 60_000 });
  await addMusic(page, editor);
  const tools = editor.getByRole('navigation', { name: 'Mobile editing tools' });
  await expect(tools.getByRole('button', { name: 'Audio' })).toBeVisible();
  await expect(tools.getByRole('button', { name: 'Adjust' })).toHaveCount(0);
  await tools.getByRole('button', { name: 'Audio' }).click();
  await expect(editor.getByRole('slider', { name: 'Fade in' })).toBeVisible();
  await info.attach('lane-phone', { contentType: 'image/png', body: await page.screenshot() });
});
```

(The accessible names are the English strings in `resources.videoStudio.ts`: `mobileTools` is 'Mobile editing tools', the rail's split is 'Split at the playhead', the mobile tools are 'Adjust', 'Audio', 'Speed', 'Time'.)

- [ ] **Step 12: Run the UI E2E RED on the VM's current image**

`e2e-vm.sh video-studio-lane.spec.ts`. Expected: FAIL at `pressAddAction(editor, 'Add audio')` — the button does not exist yet — on both projects.

- [ ] **Step 13: Static checks, coverage, dist, commit, push, GREEN on the VM**

`webcheck.sh` over every touched file; coverage ≥ 85 % lines on `VideoStudio_sources.ts`, `VideoStudio_selection.ts`, `Timeline.tsx`, `Timeline_audio.tsx`, `Timeline_items.tsx`, `Inspector_audio.tsx`, `Inspector_clip.tsx`, `Inspector.tsx`, `VideoStudio_rail.tsx`, `VideoStudio_mobile.tsx`, `VideoStudio.tsx`, `videoMedia.ts`; every touched file ≤ 600 lines (`wc -l`); `build.sh`.

```bash
git add web/src/videoStudio/VideoStudio_selection.ts web/src/videoStudio/Timeline_audio.tsx web/src/videoStudio/Inspector_audio.tsx web/src/styles/video-studio-audio.css web/e2e/video-studio-lane.spec.ts web/src/videoStudio/__tests__/VideoStudio_selection.test.ts web/src/videoStudio/__tests__/Timeline_audio.test.tsx web/src/videoStudio/__tests__/Inspector_audio.test.tsx web/src/videoStudio/__tests__/VideoStudio_audio.test.tsx
git commit -m "feat(video-studio): audio lanes, the audio door and the sound inspector" -- <the nine files above> web/src/mediaEdit/videoMedia.ts web/src/mediaEdit/__tests__/videoMedia.test.ts web/src/videoStudio/VideoStudio_sources.ts web/src/videoStudio/__tests__/VideoStudio_sources.test.ts web/src/videoStudio/Timeline.tsx web/src/videoStudio/Timeline_items.tsx web/src/videoStudio/Inspector.tsx web/src/videoStudio/Inspector_clip.tsx web/src/videoStudio/VideoStudio.tsx web/src/videoStudio/VideoStudio_rail.tsx web/src/videoStudio/VideoStudio_mobile.tsx web/src/videoStudio/__tests__/VideoStudio_mobile.test.tsx web/src/videoStudio/__tests__/VideoStudio.test.tsx web/src/styles/index.css
git add internal/webui/dist && git commit -m "build(web): rebuild the embedded dist for the audio lanes" -- internal/webui/dist
```

Push, CI green, `wait-contains.sh <dist sha>`, then `e2e-vm.sh video-studio-lane.spec.ts` (both projects) GREEN; read the level JSON and both screenshots.

---

### Task 4: Extract audio

**Files:**
- Modify: `web/src/videoStudio/commands_audio.ts` (`extractAudio`)
- Modify: `web/src/videoStudio/Inspector_audio.tsx` (the button in `ClipAudioControls`)
- Create: `web/e2e/video-studio-extract.spec.ts`
- Test: `web/src/videoStudio/__tests__/commands_audio.test.ts`, `web/src/videoStudio/__tests__/Inspector_audio.test.tsx`

**Interfaces:**
- Consumes: `placeOnFreeLane` (Task 1, module-private in `commands_audio.ts`), `ClipAudioControls` (Task 3).
- Produces: `extractAudio(project, {clipId}): VideoProject`.

- [ ] **Step 1: Write the failing tests**

Append to `commands_audio.test.ts` (import `extractAudio`; `audioWindow` is already imported):

```ts
describe('extractAudio', () => {
  it('mutes the clip and lays its sound over the same source, window, speed and volume', () => {
    const base = { ...project(), video: project().video.map((clip) => (clip.id === 'clip-2' ? { ...clip, speed: 2, volume: 0.5 } : clip)) };
    const next = extractAudio(base, { clipId: 'clip-2' });
    const [sound] = only(next);
    expect(next.video[1]?.muted).toBe(true);
    expect(sound).toMatchObject({
      sourceId: 'src-a',
      anchor: { clipId: 'clip-2', offset: 0 },
      sourceStart: 4,
      duration: 4,
      speed: 2,
      volume: 0.5,
      muted: false,
      extractedFrom: 'clip-2',
    });
    expect(audioWindow(next, sound!)).toEqual({ start: 4, end: 6 });
  });

  it('opens a lane when the first one is taken over the clip window', () => {
    const next = extractAudio(project([item()]), { clipId: 'clip-1' });
    expect(next.audio).toHaveLength(2);
  });

  it('refuses a clip whose source has no sound, and a still', () => {
    const silent = { ...project(), video: [{ id: 'quiet', sourceId: 'src-mute', duration: 5, sourceStart: 0, muted: false }] };
    expect(refusalKey(() => extractAudio(silent, { clipId: 'quiet' }))).toBe(AUDIO_REFUSAL.notSound);
    const still = { ...project(), video: [{ id: 'pic', sourceId: 'src-img', duration: 5, sourceStart: 0, muted: false }] };
    expect(refusalKey(() => extractAudio(still, { clipId: 'pic' }))).toBe(AUDIO_REFUSAL.notSound);
  });

  it('is loud about a clip the project does not have', () => {
    expect(() => extractAudio(project(), { clipId: 'nope' })).toThrow(/no clip/);
  });
});
```

(`sound!` is written as a guarded local, as oxlint requires.) Append to `Inspector_audio.test.tsx`:

```ts
it('extracts the clip sound from its Audio tab', () => {
  const view = mount(project(), 'clip-1');
  openTab('videoStudio.inspector.tabs.audio');
  fireEvent.click(screen.getByRole('button', { name: 'videoStudio.audio.extract' }));
  const next = view.applied();
  expect(next.video[0]?.muted).toBe(true);
  expect(next.audio?.flatMap((lane) => lane.items).some((item) => item.extractedFrom === 'clip-1')).toBe(true);
});
```

- [ ] **Step 2: Run them to verify they fail**

Run: `MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/vt.sh src/videoStudio/__tests__/commands_audio.test.ts src/videoStudio/__tests__/Inspector_audio.test.tsx`
Expected: FAIL — `extractAudio` is not exported; no *Extract audio* button.

- [ ] **Step 3: Write the command and the button**

In `commands_audio.ts`:

```ts
export interface ExtractAudioArgs {
  readonly clipId: string;
}

/**
 * Extract audio (spec §Model): the clip goes quiet and its sound becomes an item over the same
 * source, window, speed and volume, hung on the clip at offset 0 and marked as extracted from it —
 * so removing the clip removes it. From then on the two are independent; undo restores both.
 */
export function extractAudio(project: VideoProject, args: ExtractAudioArgs): VideoProject {
  const clip = project.video.find((candidate) => candidate.id === args.clipId);
  if (clip === undefined) throw new Error(`videoStudio: no clip named ${args.clipId}`);
  const source = sourceOf(project, clip.sourceId);
  if (source?.kind !== 'video' || source.hasAudio === false) {
    throw new CommandRefusal(AUDIO_REFUSAL.notSound);
  }
  const quiet: VideoProject = {
    ...project,
    video: project.video.map((candidate) => (candidate.id === clip.id ? { ...candidate, muted: true } : candidate)),
  };
  return placeOnFreeLane(quiet, {
    id: crypto.randomUUID(),
    sourceId: source.id,
    anchor: { clipId: clip.id, offset: 0 },
    sourceStart: clip.sourceStart,
    duration: clip.duration,
    volume: clip.volume ?? 1,
    muted: false,
    extractedFrom: clip.id,
    ...(clip.speed === undefined ? {} : { speed: clip.speed }),
  });
}
```

In `Inspector_audio.tsx`, `import { AudioLines } from 'lucide-react';`, `extractAudio` joins the `./commands_audio` import, and `ClipAudioControls` ends with:

```tsx
      <Button
        type="button"
        variant="outline"
        size="sm"
        onClick={() => {
          onCommand((current) => extractAudio(current, { clipId: clip.id }));
        }}
      >
        <AudioLines aria-hidden="true" />
        {t('videoStudio.audio.extract')}
      </Button>
```

(with `const { t } = useTranslation();` at its top).

- [ ] **Step 4: Run them to verify they pass**

Run the same command. Expected: PASS.

- [ ] **Step 5: Write the Extract E2E**

`web/e2e/video-studio-extract.spec.ts`:

```ts
import { randomUUID } from 'node:crypto';
import { readFileSync } from 'node:fs';
import type { Locator, Page, TestInfo } from '@playwright/test';
import { expect, test } from './support/assetCleanup';
import { levelBetween, windowPowers } from './support/audioMeasure';
import { exportTo, reopen, setField, uploadClip } from './support/videoStudio';

// video-studio-extract.spec.ts — Extract audio, proven by the file: the clip goes quiet and its
// tone is still in the export, carried by the new sound; trimming that sound's first second then
// leaves that second silent while the picture plays on.

const FRAME = { width: 320, height: 180 };

async function levels(page: Page, editor: Locator, info: TestInfo, name: string) {
  const powers = await windowPowers(page, readFileSync(await exportTo(page, editor, info, name)));
  return { first: levelBetween(powers, 0.1, 0.9), rest: levelBetween(powers, 1.5, 3.5) };
}

test('an extracted sound carries the clip tone, and its trim is heard', async ({ page }, info) => {
  test.skip(info.project.name.startsWith('mobile'), 'one export pair is enough; the controls are the same on a phone');
  test.setTimeout(15 * 60_000);
  const clip = await uploadClip(page);
  const project = {
    id: randomUUID(),
    name: 'extract check',
    size: FRAME,
    fps: 30,
    sources: [{ id: 'src-a', assetId: clip, kind: 'video', duration: 4, size: FRAME, hasAudio: true }],
    video: [{ id: 'clip-1', sourceId: 'src-a', duration: 4, sourceStart: 0, muted: false }],
    overlays: [],
  };
  const editor = await reopen(page, project, clip);
  await editor.getByRole('button', { name: 'Clip 1' }).click();
  const inspector = editor.getByRole('region', { name: 'Properties' });
  await inspector.getByRole('tab', { name: 'Audio' }).click();
  await inspector.getByRole('button', { name: 'Extract audio' }).click();
  await expect(editor.getByRole('button', { name: 'Sound 1' })).toBeVisible();

  const extracted = await levels(page, editor, info, 'extract.mp4');

  // The new sound is selected; its In moves one second into the tone.
  await inspector.getByRole('tab', { name: 'Time' }).click();
  await setField(inspector, 'In', '00:01.0');
  const trimmed = await levels(page, editor, info, 'extract-trimmed.mp4');

  // And the clip really is muted: its own switch says so.
  await editor.getByRole('button', { name: 'Clip 1' }).click();
  await inspector.getByRole('tab', { name: 'Audio' }).click();
  await expect(inspector.getByRole('switch', { name: 'Mute' })).toBeChecked();

  await info.attach('extract-levels', { contentType: 'application/json', body: JSON.stringify({ extracted, trimmed }, null, 2) });
  expect(extracted.first).toBeGreaterThan(-40);
  expect(extracted.rest).toBeGreaterThan(-40);
  expect(trimmed.first).toBeLessThan(-60);
  expect(Math.abs(trimmed.rest - extracted.rest)).toBeLessThan(1);
});
```

- [ ] **Step 6: Run the Extract E2E RED on the VM's current image**

`e2e-vm.sh video-studio-extract.spec.ts --project=chrome`. Expected: FAIL at the *Extract audio* button (it does not exist on the running image).

- [ ] **Step 7: Static checks, mutation, dist, commit, push, GREEN on the VM**

`webcheck.sh`; coverage on `commands_audio.ts` and `Inspector_audio.tsx` ≥ 85 %; `build.sh`.

```bash
git add web/e2e/video-studio-extract.spec.ts
git commit -m "feat(video-studio): extract a clip's audio onto a lane" -- web/src/videoStudio/commands_audio.ts web/src/videoStudio/Inspector_audio.tsx web/src/videoStudio/__tests__/commands_audio.test.ts web/src/videoStudio/__tests__/Inspector_audio.test.tsx web/e2e/video-studio-extract.spec.ts
git add internal/webui/dist && git commit -m "build(web): rebuild the embedded dist for Extract audio" -- internal/webui/dist
```

Push, CI green (read the critical-mutation artifact: `audioLane.ts`, `commands_audio.ts`, `volumeCurve.ts`, `videoflow_audio.ts` each ≥ 70 % killed), `wait-contains.sh <dist sha>`, `e2e-vm.sh video-studio-extract.spec.ts --project=chrome` GREEN; read the level JSON.

---

## Final review

After Task 4, the whole-branch review (superpowers:executing-plans §Final Review) runs from `review-package` over `<Plan B base>..HEAD` with a headless `claude -p` reviewer on the most capable model, given this plan, the spec and the Review Focus above verbatim. Critical and Important findings enter one fix pass, each RED→GREEN with the full `src/videoStudio` suite; minors are ledgered and reported.
