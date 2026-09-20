# Studio Video Studio, cycle 1 — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A multi-track editor in Aura's Studio — several clips in sequence with text and image layers over them, edited through commands, exported to a downloaded MP4, and saved as a project you can reopen.

**Architecture:** A pure project model with ripple sequence semantics on the video lane and overlays anchored to their clip; every edit is a pure command, with immer patches for undo; one adapter file owns VideoFlow (project → `VideoJSON`, export and preview) and applies the four rules spike 107 measured; the UI is `dnd-timeline` lanes plus a stage and an inspector.

**Tech Stack:** React 19, Vite, TypeScript, vitest, Playwright; `@videoflow/core` + `@videoflow/renderer-browser` + `@videoflow/renderer-dom` (Apache-2.0), `dnd-timeline` (MIT), `immer` (MIT); mediabunny 1.58.1, already shipped.

**Spec:** `docs/superpowers/specs/2026-09-20-studio-video-studio-design.md` (its adversarial review, whose findings shaped it, is `docs/superpowers/specs/2026-09-20-studio-video-studio-review.md`)

## Global Constraints

- Commit directly on `master`; never create a branch. Stage by explicit path (`git commit <paths>`), never `git add -A`, never `--no-verify` — other sessions leave dirty files in this tree.
- Commit subject imperative and short; body says why; end with `Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>`.
- No file over 600 lines (`scripts/check-file-size.sh` covers `.ts`/`.tsx`).
- Web gates, from `web/`: `npm run lint` (oxlint type-aware, 0 warnings), `npm run typecheck`, `npm run format:check`, `npm run deadcode` (knip), `npm run dup` (jscpd), `npm test` (vitest, coverage ≥ 85 % on statements, branches, functions and lines). Mutation testing runs in CI only.
- Every user-visible string goes through i18next with `en` and `it`; the parity test fails otherwise.
- **No request may leave the origin.** VideoFlow's renderers fetch Google Fonts unless `loadFont` is replaced (spike 107); fonts and the worker are served by us.
- The export **downloads**. The Studio library and its finalize are image-only in Go (`cmd/aura/serve_studio.go:163-171`); the library door is cycle 2 and nothing here offers it.
- The video lane is a sequence: clips abut, edits ripple, `moveClip` inserts. Overlays anchor to a clip as `{ clipId, offset }` and ripple with it. Collisions apply to overlay lanes only.
- Live feedback (a drag in flight, a scrub) is view state. Only the release writes a command.
- Konva is not used and is not a dependency of `web/`.

---

## File structure

| File | Responsibility |
|---|---|
| `web/src/videoStudio/project.ts` | the project types and the pure queries: clip start times, total duration, overlay resolution, the ripple arithmetic |
| `web/src/videoStudio/commands.ts` | the commands, each a pure `(project, args) => project` that throws a typed refusal |
| `web/src/videoStudio/history.ts` | immer patches, the undo/redo stacks, one step per gesture |
| `web/src/videoStudio/videoflow.ts` | the only file that imports VideoFlow: project → `VideoJSON`, the local-font override, the nudge, one decoded source per asset, export and preview |
| `web/src/videoStudio/Timeline.tsx` | lanes, ruler, playhead, zoom buttons, drag and trim on `dnd-timeline` |
| `web/src/videoStudio/Stage.tsx` | the preview surface and the selection box |
| `web/src/videoStudio/Inspector.tsx` | the selected item's fields |
| `web/src/videoStudio/projectStore.ts` | load and save the project as a `.json` asset through the presign path |
| `web/src/videoStudio/VideoStudio.tsx` | the workspace that holds the parts and owns the editor state |
| `web/src/i18n/resources.videoStudio.ts` | every string of the editor, `en` and `it` |
| `.planning/spikes/108-video-studio-audio/` | Task 1's measurement |

---

### Task 1: Measure what the design rests on (spike 108)

The spec gates on this: spike 107 composed ONE video layer, so multi-clip audio has never been rendered, and the `+0.1 ms` nudge was proven at 24 fps while a phone clip is 30.

**Files:**
- Create: `.planning/spikes/108-video-studio-audio/README.md`, `.planning/spikes/108-video-studio-audio/run-audio.mjs`, `.planning/spikes/108-video-studio-audio/.gitignore` (`out/`)
- Modify: `.planning/spikes/MANIFEST.md` (row 108, idea `studio-media-editing`)

**Interfaces:**
- Produces: the numbers Task 5 needs — whether one decoded buffer per source is required, and the nudge that holds at 30 fps.

- [ ] **Step 1: Reuse the spike 107 harness**

The harness is `.planning/spikes/107-studio-timeline-editor/app/` and it already has VideoFlow installed, the local-font override (`src/fonts.js`) and a compose lab (`src/ComposeLab.jsx`). Run `npm install` there if `node_modules/` is gone. Add a scenario rather than a new app.

- [ ] **Step 2: Build the two-clip scenario**

In `app/src/project.js`, beside `buildComposition`, add:

```js
// Two clips with audio, the second split in two, the third muted: the shape the editor's
// video lane produces, and the one spike 107 never rendered.
export async function buildAudioComposition({ a = '/clip-a.mp4', b = '/clip-b.mp4' } = {}) {
  const $ = new VideoFlow({ name: 'spike-108', width: 320, height: 180, fps: 30, backgroundColor: '#000000' });
  $.addVideo({ fit: 'cover' }, { name: 'one', source: a, startTime: 0, sourceStart: 0, sourceDuration: 4 });
  $.addVideo({ fit: 'cover' }, { name: 'two', source: b, startTime: 4, sourceStart: 0, sourceDuration: 2 });
  $.addVideo({ fit: 'cover' }, { name: 'three', source: b, startTime: 6, sourceStart: 2, sourceDuration: 2, muted: true });
  $.wait('8s');
  return $.compile();
}
```

Two source clips with audio are needed. Make them with the 105 spike's `make-media.sh`, or directly:

```bash
MSYS_NO_PATHCONV=1 docker run --rm -v "$(pwd -W)/.planning/spikes/108-video-studio-audio/out:/m" jrottenberg/ffmpeg:7.1-alpine \
  -hide_banner -loglevel error -y -f lavfi -i testsrc2=s=320x180:r=30:d=4 -f lavfi -i sine=f=440:d=4 \
  -c:v libx264 -pix_fmt yuv420p -c:a aac -shortest /m/clip-a.mp4
MSYS_NO_PATHCONV=1 docker run --rm -v "$(pwd -W)/.planning/spikes/108-video-studio-audio/out:/m" jrottenberg/ffmpeg:7.1-alpine \
  -hide_banner -loglevel error -y -f lavfi -i testsrc2=s=320x180:r=30:d=4 -f lavfi -i sine=f=880:d=4 \
  -c:v libx264 -pix_fmt yuv420p -c:a aac -shortest /m/clip-b.mp4
```

- [ ] **Step 3: Render it and watch the memory**

`run-audio.mjs` drives the harness with Playwright (import from `file:///D:/Repo/Aura/web/node_modules/playwright/index.mjs`, `chromium.launch({ channel: 'chrome' })`), exactly as spike 107's runners do. Per render, record:

- wall time and the exported blob's size;
- `performance.memory.usedJSHeapSize` before and after (Chrome only, which is why the channel is chrome);
- how many times the mixer decodes: patch `AudioContext.prototype.decodeAudioData` on the page to count calls, and report the count against the number of distinct sources (2) and of layers (3).

Expected, if the review is right: 3 decodes for 2 sources — the same file decoded once per layer.

- [ ] **Step 4: Probe the result**

```bash
MSYS_NO_PATHCONV=1 docker run --rm -v "$(pwd -W)/.planning/spikes/108-video-studio-audio/out:/m" jrottenberg/ffmpeg:7.1-alpine \
  ffprobe -hide_banner -show_streams -show_format /m/render.mp4
```

Record: duration ≈ 8 s, one audio stream, and that the third clip is silent (decode the audio and check the RMS of its last two seconds, or read it with mediabunny in the page).

- [ ] **Step 5: Re-measure the nudge at 30 fps**

Spike 107 found that without `sourceStart + 0.0001` ten frames of 96 repeat the previous one, at 24 fps. Render the two-clip scenario twice, with and without the nudge, and compare frames at the cut points (grab frames with ffmpeg at `0:03.9`, `0:04.0`, `0:04.1` and diff them). Record whether the nudge is still needed, and whether it is still `0.1 ms` at 30 fps.

- [ ] **Step 6: Write the README and the MANIFEST row**

`.planning/spikes/108-video-studio-audio/README.md` in spike 105's format (`spike: 108`, `idea: studio-media-editing`, `type: standard`, a verdict), with sections What This Validates / How to Run / Investigation Trail / Results. Results must answer, in numbers: how many decodes per render, the peak heap, the render time, whether the audio is right, and the nudge at 30 fps. End with **the decision for Task 5**: one decoded buffer per source, or per layer as VideoFlow does it.

- [ ] **Step 7: Commit**

```bash
git add .planning/spikes/108-video-studio-audio .planning/spikes/MANIFEST.md
git commit -m "docs(spike-108): measure multi-clip audio and the cut nudge at 30 fps" -m "What the editor's video lane really costs to render, before its model is written." -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 2: The project model

**Files:**
- Create: `web/src/videoStudio/project.ts`
- Test: `web/src/videoStudio/__tests__/project.test.ts`
- Modify: `web/package.json` (add the four dependencies)

**Interfaces:**
- Produces: `VideoProject`, `ProjectSource`, `VideoItem`, `OverlayItem`, `OverlayTrack`, `clipStarts(project): number[]`, `projectDuration(project): number`, `clipAt(project, time): VideoItem | undefined`, `overlayWindow(project, item): { start: number; end: number }`, `sourceOf(project, item): ProjectSource`, `emptyProject(name, size, fps): VideoProject`.

- [ ] **Step 1: Add the dependencies**

```bash
cd web
npm install --save-exact @videoflow/core@1.3.4 @videoflow/renderer-browser@1.3.4 @videoflow/renderer-dom@1.3.4
npm install --save dnd-timeline@^3.1.1 immer@^11.1.18
```

Expected: `package-lock.json` changes; `npm run deadcode` will flag all five as unused until Task 5 and Task 6 import them — add them to `ignoreDependencies` in `web/knip.json` in this task's commit and remove each entry in the task that first imports it.

- [ ] **Step 2: Write the failing test**

`web/src/videoStudio/__tests__/project.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import {
  clipAt,
  clipStarts,
  emptyProject,
  overlayWindow,
  projectDuration,
  type VideoProject,
} from '../project';

function project(): VideoProject {
  return {
    ...emptyProject('demo', { width: 1920, height: 1080 }, 30),
    sources: [
      { id: 'src-a', assetId: 'asset-a', kind: 'video', duration: 8, size: { width: 1920, height: 1080 }, fps: 30 },
      { id: 'src-b', assetId: 'asset-b', kind: 'image', duration: 0, size: { width: 800, height: 600 }, fps: 0 },
    ],
    video: [
      { id: 'clip-1', sourceId: 'src-a', duration: 3, sourceStart: 0, muted: false },
      { id: 'clip-2', sourceId: 'src-a', duration: 2, sourceStart: 4, muted: true },
    ],
    overlays: [
      {
        id: 'lane-1',
        items: [{ id: 'title', anchor: { clipId: 'clip-2', offset: 0.5 }, duration: 1, props: { text: 'ciao' } }],
      },
    ],
  };
}

describe('the video lane is a sequence', () => {
  it('gives every clip the start its predecessors leave it', () => {
    expect(clipStarts(project())).toEqual([0, 3]);
  });

  it('is as long as its clips together', () => {
    expect(projectDuration(project())).toBe(5);
  });

  it('answers which clip covers an instant, and which does not', () => {
    expect(clipAt(project(), 2.9)?.id).toBe('clip-1');
    expect(clipAt(project(), 3)?.id).toBe('clip-2');
    expect(clipAt(project(), 5)).toBeUndefined();
  });
});

describe('an overlay follows its clip', () => {
  it('resolves against the clip it is anchored to', () => {
    expect(overlayWindow(project(), { clipId: 'clip-2', offset: 0.5 }, 1)).toEqual({ start: 3.5, end: 4.5 });
  });

  it('is clipped to its clip rather than spilling past it', () => {
    expect(overlayWindow(project(), { clipId: 'clip-2', offset: 1.5 }, 3)).toEqual({ start: 4.5, end: 5 });
  });
});
```

- [ ] **Step 3: Run it and watch it fail**

Run: `cd web && npx vitest run src/videoStudio/__tests__/project.test.ts`
Expected: FAIL — `Cannot find module '../project'`.

- [ ] **Step 4: Write the model**

`web/src/videoStudio/project.ts`:

```ts
// project.ts — the editor's whole state as data: sources by asset id, a video lane that is a
// sequence, and overlay lanes whose items hang off a clip rather than off the timeline.
//
// A clip has no start of its own. Its start is what the clips before it leave, which is what
// makes ripple the default instead of a feature: trim one and every later clip moves with it,
// and so do the overlays anchored to them.

export interface ProjectSize {
  readonly width: number;
  readonly height: number;
}

export interface ProjectSource {
  readonly id: string;
  readonly assetId: string;
  readonly kind: 'video' | 'image';
  readonly duration: number; // seconds; 0 for an image, which takes the duration its item asks for
  readonly size: ProjectSize;
  readonly fps: number;
}

export interface VideoItem {
  readonly id: string;
  readonly sourceId: string;
  readonly duration: number;
  readonly sourceStart: number;
  readonly muted: boolean;
}

export interface OverlayAnchor {
  readonly clipId: string;
  readonly offset: number; // seconds after the clip starts
}

export interface OverlayItem {
  readonly id: string;
  readonly anchor: OverlayAnchor;
  readonly duration: number;
  readonly props: Readonly<Record<string, unknown>>;
}

export interface OverlayTrack {
  readonly id: string;
  readonly items: readonly OverlayItem[];
}

export interface VideoProject {
  readonly id: string;
  readonly name: string;
  readonly size: ProjectSize;
  readonly fps: number;
  readonly sources: readonly ProjectSource[];
  readonly video: readonly VideoItem[];
  readonly overlays: readonly OverlayTrack[];
}

export function emptyProject(name: string, size: ProjectSize, fps: number): VideoProject {
  return { id: crypto.randomUUID(), name, size, fps, sources: [], video: [], overlays: [] };
}

/** Where each clip begins, in project time. The i-th start is the sum of the durations before it. */
export function clipStarts(project: VideoProject): number[] {
  const starts: number[] = [];
  let at = 0;
  for (const clip of project.video) {
    starts.push(at);
    at += clip.duration;
  }
  return starts;
}

export function projectDuration(project: VideoProject): number {
  return project.video.reduce((total, clip) => total + clip.duration, 0);
}

/** The clip covering `time`, start inclusive and end exclusive, or undefined past the end. */
export function clipAt(project: VideoProject, time: number): VideoItem | undefined {
  const starts = clipStarts(project);
  for (let i = project.video.length - 1; i >= 0; i -= 1) {
    if (time >= starts[i]) {
      const clip = project.video[i];
      return time < starts[i] + clip.duration ? clip : undefined;
    }
  }
  return undefined;
}

export function clipStart(project: VideoProject, clipId: string): number | undefined {
  const index = project.video.findIndex((clip) => clip.id === clipId);
  return index === -1 ? undefined : clipStarts(project)[index];
}

/** An overlay's window in project time. It never outlives the clip it hangs on. */
export function overlayWindow(
  project: VideoProject,
  anchor: OverlayAnchor,
  duration: number,
): { readonly start: number; readonly end: number } {
  const clip = project.video.find((item) => item.id === anchor.clipId);
  const base = clipStart(project, anchor.clipId);
  if (clip === undefined || base === undefined) return { start: 0, end: 0 };
  const start = base + Math.max(0, anchor.offset);
  return { start, end: Math.min(start + duration, base + clip.duration) };
}

export function sourceOf(project: VideoProject, sourceId: string): ProjectSource | undefined {
  return project.sources.find((source) => source.id === sourceId);
}
```

- [ ] **Step 5: Run the test again**

Run: `cd web && npx vitest run src/videoStudio/__tests__/project.test.ts`
Expected: PASS, 5 tests.

- [ ] **Step 6: Gates and commit**

```bash
cd web && npx tsc --noEmit -p . && npx oxlint --type-aware src/videoStudio && npx prettier --check src/videoStudio
cd .. && git commit web/src/videoStudio/project.ts web/src/videoStudio/__tests__/project.test.ts web/package.json web/package-lock.json web/knip.json -m "feat(video-studio): model a project whose video lane is a sequence" -m "A clip has no start of its own: it begins where the clips before it end, so ripple is the model rather than a feature, and an overlay anchored to a clip moves with it." -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 3: The commands

**Files:**
- Create: `web/src/videoStudio/commands.ts`
- Test: `web/src/videoStudio/__tests__/commands.test.ts`

**Interfaces:**
- Consumes: everything Task 2 produces.
- Produces: `addClip`, `trimClip`, `splitAt`, `removeRange`, `moveClip`, `setMuted`, `addOverlay`, `setProperty`, `removeItem`, and `CommandRefusal` (an `Error` subclass carrying `reasonKey`, an i18n key).

- [ ] **Step 1: Write the failing test**

`web/src/videoStudio/__tests__/commands.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import { CommandRefusal, moveClip, removeRange, setMuted, splitAt, trimClip } from '../commands';
import { clipStarts, projectDuration, type VideoProject } from '../project';

function project(): VideoProject {
  return {
    id: 'p', name: 'demo', size: { width: 1920, height: 1080 }, fps: 30,
    sources: [{ id: 'src-a', assetId: 'a', kind: 'video', duration: 10, size: { width: 1920, height: 1080 }, fps: 30 }],
    video: [
      { id: 'clip-1', sourceId: 'src-a', duration: 4, sourceStart: 0, muted: false },
      { id: 'clip-2', sourceId: 'src-a', duration: 4, sourceStart: 4, muted: false },
    ],
    overlays: [{ id: 'lane-1', items: [{ id: 'title', anchor: { clipId: 'clip-2', offset: 1 }, duration: 1, props: {} }] }],
  };
}

describe('trimClip ripples', () => {
  it('shortens the clip and pulls everything after it back', () => {
    const next = trimClip(project(), { clipId: 'clip-1', start: 1, end: 4 });
    expect(next.video[0].duration).toBe(3);
    expect(next.video[0].sourceStart).toBe(1);
    expect(clipStarts(next)).toEqual([0, 3]);
    expect(projectDuration(next)).toBe(7);
  });

  it('refuses a trim past the source', () => {
    expect(() => trimClip(project(), { clipId: 'clip-2', start: 0, end: 9 })).toThrow(CommandRefusal);
  });
});

describe('splitAt', () => {
  it('cuts one clip into two that together are the original', () => {
    const next = splitAt(project(), { time: 1.5 });
    expect(next.video).toHaveLength(3);
    expect(next.video[0].duration).toBe(1.5);
    expect(next.video[1].duration).toBe(2.5);
    expect(next.video[1].sourceStart).toBe(1.5);
    expect(projectDuration(next)).toBe(8);
  });

  it('refuses a split on a boundary, which would make an empty clip', () => {
    expect(() => splitAt(project(), { time: 4 })).toThrow(CommandRefusal);
  });
});

describe('removeRange', () => {
  it('cuts a hole out of the middle and closes it', () => {
    const next = removeRange(project(), { from: 1, to: 3 });
    expect(projectDuration(next)).toBe(6);
    expect(clipStarts(next)).toEqual([0, 1, 2]);
  });
});

describe('moveClip inserts, never overlaps', () => {
  it('puts the clip at the index asked for and keeps the lane abutting', () => {
    const next = moveClip(project(), { clipId: 'clip-2', toIndex: 0 });
    expect(next.video.map((clip) => clip.id)).toEqual(['clip-2', 'clip-1']);
    expect(clipStarts(next)).toEqual([0, 4]);
  });

  it('carries the overlays anchored to the moved clip', () => {
    const next = moveClip(project(), { clipId: 'clip-2', toIndex: 0 });
    expect(next.overlays[0].items[0].anchor.clipId).toBe('clip-2');
  });
});

describe('setMuted', () => {
  it('mutes one clip and leaves the rest alone', () => {
    const next = setMuted(project(), { clipId: 'clip-1', muted: true });
    expect(next.video[0].muted).toBe(true);
    expect(next.video[1].muted).toBe(false);
  });
});
```

- [ ] **Step 2: Run it and watch it fail**

Run: `cd web && npx vitest run src/videoStudio/__tests__/commands.test.ts`
Expected: FAIL — `Cannot find module '../commands'`.

- [ ] **Step 3: Write the commands**

`web/src/videoStudio/commands.ts`. Every command is `(project, args) => project`, returns a new project and never mutates its input. A refusal is:

```ts
/** A refusal the UI can translate: the key is an i18n key, never an English sentence. */
export class CommandRefusal extends Error {
  constructor(readonly reasonKey: string) {
    super(reasonKey);
    this.name = 'CommandRefusal';
  }
}
```

The commands, with the rules the spec sets:

- `addClip(project, { sourceId, duration, sourceStart, atIndex })` — appends by default, inserts at `atIndex` when given; refuses `videoStudio.refusal.sourceMissing` if the source is not in the project.
- `trimClip(project, { clipId, start, end })` — `start` and `end` are source-media seconds; the new duration is `end - start`; refuses `videoStudio.refusal.trimPastSource` when `end` exceeds the source's duration or `end - start <= 0`.
- `splitAt(project, { time })` — splits the clip covering `time`; the right half gets `sourceStart + offset`; refuses `videoStudio.refusal.splitOnBoundary` when the offset is 0 or the whole clip; overlays anchored to the clip stay with the half that contains their offset.
- `removeRange(project, { from, to })` — trims or drops every clip the range covers, and the lane closes; refuses `videoStudio.refusal.emptyRange` when `to <= from`.
- `moveClip(project, { clipId, toIndex })` — reorders the lane; anchors are ids, so overlays travel with their clip untouched.
- `setMuted(project, { clipId, muted })`.
- `addOverlay(project, { trackId, kind, anchor, duration, props })` — adds a lane when `trackId` is absent; refuses `videoStudio.refusal.overlayOverlap` when the window collides on that lane (use `overlayWindow` for both).
- `setProperty(project, { itemId, key, value })`, `removeItem(project, { itemId })` — `removeItem` on a clip also removes the overlays anchored to it.

Keep the file under 600 lines; if it approaches, split the overlay commands into `commands.overlay.ts` and re-export.

- [ ] **Step 4: Run the test again**

Run: `cd web && npx vitest run src/videoStudio/__tests__/commands.test.ts`
Expected: PASS, 8 tests.

- [ ] **Step 5: Gates and commit**

```bash
cd web && npx tsc --noEmit -p . && npx oxlint --type-aware src/videoStudio && npx prettier --check src/videoStudio && npx vitest run src/videoStudio
cd .. && git commit web/src/videoStudio/commands.ts web/src/videoStudio/__tests__/commands.test.ts -m "feat(video-studio): make every edit a command the lane ripples through" -m "A trim moves what follows it, a split keeps the two halves together, a move inserts rather than overlaps, and a refusal carries an i18n key rather than an English sentence." -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 4: Undo and redo

**Files:**
- Create: `web/src/videoStudio/history.ts`
- Test: `web/src/videoStudio/__tests__/history.test.ts`

**Interfaces:**
- Consumes: `VideoProject`, the commands.
- Produces: `createHistory(initial): History`, with `apply(fn: (p: VideoProject) => VideoProject): VideoProject`, `undo(): VideoProject`, `redo(): VideoProject`, `canUndo: boolean`, `canRedo: boolean`, `current: VideoProject`.

- [ ] **Step 1: Write the failing test**

```ts
import { describe, expect, it } from 'vitest';
import { createHistory } from '../history';
import { setMuted, trimClip } from '../commands';
import { projectDuration, type VideoProject } from '../project';

// … same `project()` fixture as commands.test.ts …

describe('history', () => {
  it('undoes one command and redoes it', () => {
    const history = createHistory(project());
    history.apply((p) => trimClip(p, { clipId: 'clip-1', start: 0, end: 2 }));
    expect(projectDuration(history.current)).toBe(6);
    expect(projectDuration(history.undo())).toBe(8);
    expect(projectDuration(history.redo())).toBe(6);
  });

  it('keeps a gesture to one step, however many commands it ran', () => {
    const history = createHistory(project());
    history.transaction((p) => {
      let next = p;
      for (let end = 4; end > 2; end -= 0.1) next = trimClip(next, { clipId: 'clip-1', start: 0, end });
      return next;
    });
    history.undo();
    expect(projectDuration(history.current)).toBe(8);
  });

  it('drops the redo branch once a new command lands', () => {
    const history = createHistory(project());
    history.apply((p) => setMuted(p, { clipId: 'clip-1', muted: true }));
    history.undo();
    history.apply((p) => setMuted(p, { clipId: 'clip-2', muted: true }));
    expect(history.canRedo).toBe(false);
  });
});
```

- [ ] **Step 2: Run it and watch it fail**

Run: `cd web && npx vitest run src/videoStudio/__tests__/history.test.ts`
Expected: FAIL — `Cannot find module '../history'`.

- [ ] **Step 3: Write it with immer patches**

```ts
import { applyPatches, enablePatches, produceWithPatches, type Patch } from 'immer';

import type { VideoProject } from './project';

// Patches, not snapshots: a project carries every clip and overlay, and an editing session is
// hundreds of steps. `produceWithPatches` gives both directions of each step for the price of
// the step.
enablePatches();

interface Step {
  readonly forward: readonly Patch[];
  readonly back: readonly Patch[];
}
```

`apply` runs one command and pushes one step. `transaction` runs a whole gesture — the caller's function may run any number of commands — and pushes the single step between its start and its end, which is how a 30-event drag stays one undo.

- [ ] **Step 4: Run the test again**

Run: `cd web && npx vitest run src/videoStudio/__tests__/history.test.ts`
Expected: PASS, 3 tests.

- [ ] **Step 5: Gates and commit**

```bash
cd web && npx tsc --noEmit -p . && npx oxlint --type-aware src/videoStudio && npx prettier --check src/videoStudio && npm run deadcode
cd .. && git commit web/src/videoStudio/history.ts web/src/videoStudio/__tests__/history.test.ts web/knip.json -m "feat(video-studio): undo a gesture, not a pixel" -m "immer patches record both directions of each step, and a transaction collapses a whole drag into one." -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 5: The VideoFlow adapter

**Files:**
- Create: `web/src/videoStudio/videoflow.ts`, `web/public/fonts/atkinson.css` (if absent, serving the family the cockpit already ships)
- Test: `web/src/videoStudio/__tests__/videoflow.test.ts`

**Interfaces:**
- Consumes: `VideoProject`, `clipStarts`, `overlayWindow`, `sourceOf`; the asset URL helper the cockpit already uses for media (`/api/assets/<id>/download`).
- Produces: `toVideoJSON(project, urls): Promise<VideoJSON>`, `exportProject(project, urls, { onProgress, signal }): Promise<Blob>`, `useLocalFonts(renderer): Renderer`.

- [ ] **Step 1: Read what the spike proved before writing anything**

Read `.planning/spikes/107-studio-timeline-editor/app/src/project.js` (the fluent API: `new VideoFlow({ name, width, height, fps, backgroundColor })`, `$.addVideo(props, settings)`, `$.addText`, `$.addImage(...).animate(from, to, opts)`, `$.wait('4s')`, `$.compile()`), `app/src/fonts.js` (the `loadFont` override — there is no config option) and `app/src/ComposeLab.jsx` (`new BrowserRenderer(json)`, `await renderer.exportVideo({ worker, onProgress })`, `renderer.destroy()`). Read spike 108's README for the audio decision.

- [ ] **Step 2: Write the failing test**

The test mocks `@videoflow/core` and `@videoflow/renderer-browser` and asserts the SHAPE we hand them, which is what breaks:

```ts
import { beforeEach, describe, expect, it, vi } from 'vitest';

const calls = vi.hoisted(() => ({ videos: [] as unknown[], texts: [] as unknown[], compiled: 0 }));
vi.mock('@videoflow/core', () => ({
  default: class {
    addVideo(props: unknown, settings: unknown) { calls.videos.push({ props, settings }); return { animate: vi.fn() }; }
    addText(props: unknown, settings: unknown) { calls.texts.push({ props, settings }); return { animate: vi.fn() }; }
    addImage() { return { animate: vi.fn() }; }
    wait() {}
    compile() { calls.compiled += 1; return { layers: [], duration: 7 }; }
  },
}));

describe('toVideoJSON', () => {
  it('places every clip at the start its lane gives it, with the cut nudge', async () => {
    await toVideoJSON(project(), urls);
    expect(calls.videos).toHaveLength(2);
    expect(calls.videos[1]).toMatchObject({ settings: { startTime: 4, sourceStart: 4.0001, sourceDuration: 3 } });
  });

  it('carries mute per clip', async () => { /* settings.muted === true on the muted one */ });

  it('resolves an overlay against its clip, not against the timeline', async () => {
    // a title anchored { clipId: 'clip-2', offset: 0.5 } lands at startTime 4.5
  });
});
```

- [ ] **Step 3: Run it and watch it fail**

Run: `cd web && npx vitest run src/videoStudio/__tests__/videoflow.test.ts`
Expected: FAIL — `Cannot find module '../videoflow'`.

- [ ] **Step 4: Write the adapter**

It owns four things, each with the comment that says why:

1. **The font override** — copy `useLocalFonts` from the spike, pointing at `/fonts/*.css` served by us. Without it the renderers fetch `fonts.googleapis.com`: 26 requests, measured.
2. **The nudge** — `sourceStart + 0.0001` on every clip whose `sourceStart > 0`, with spike 107's measurement in the comment and spike 108's confirmation at 30 fps.
3. **One decoded buffer per source**, if spike 108 says VideoFlow decodes per layer: build the composition so that clips sharing a source share one decode. If the spike says otherwise, write down that it does not, and why.
4. **Export**: `new BrowserRenderer(json)`, `exportVideo({ worker: true, onProgress })`, `destroy()` in a `finally`, and an `AbortSignal` that calls `destroy()` — the previous cycle shipped an editor that kept transcoding after its dialog closed, and this one starts with the abort wired.

- [ ] **Step 5: Run the test again**

Run: `cd web && npx vitest run src/videoStudio/__tests__/videoflow.test.ts`
Expected: PASS, 3 tests.

- [ ] **Step 6: Gates and commit**

```bash
cd web && npx tsc --noEmit -p . && npx oxlint --type-aware src/videoStudio && npx prettier --check src/videoStudio && npm run deadcode
cd .. && git commit web/src/videoStudio/videoflow.ts web/src/videoStudio/__tests__/videoflow.test.ts web/public/fonts web/knip.json -m "feat(video-studio): hand the project to VideoFlow on Aura's terms" -m "Fonts from our own origin, the measured cut nudge, mute per clip, overlays resolved against their clip, and an export that stops when its editor closes." -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 6: The timeline

**Files:**
- Create: `web/src/videoStudio/Timeline.tsx`
- Test: `web/src/videoStudio/__tests__/Timeline.test.tsx`

**Interfaces:**
- Consumes: `VideoProject`, `clipStarts`, `overlayWindow`; `dnd-timeline`.
- Produces: `<Timeline project selectedId onSelect onCommand onScrub playhead />`, where `onCommand` receives a command thunk `(p: VideoProject) => VideoProject` — the workspace decides whether it is a transaction.

- [ ] **Step 1: Read dnd-timeline's contract**

`web/node_modules/dnd-timeline/dist/index.d.ts`. It hands the shell a `span` on `onDragEnd` and nothing else: the insert-versus-overlap semantics are ours, and Task 3 already made them. Zoom is ours too — there is no pinch gesture, which the spec says out loud.

- [ ] **Step 2: Write the failing test**

Assert behaviour, not pixels: a drag that ends over another clip emits a `moveClip` thunk with the right index; a trim handle emits `trimClip` with source-media seconds; a click emits `onSelect`; the ruler's marks are at the zoom's step; the playhead sits where the time says; the zoom buttons change the step and never go below one second per screen.

- [ ] **Step 3: Run it and watch it fail** — `Cannot find module '../Timeline'`.

- [ ] **Step 4: Write the component**

Lanes top to bottom: the video lane, then one row per overlay track. Items are 44 px tall minimum with 44 px hit areas on the trim handles — the touch floor from the previous cycle. Keyboard: arrows move the playhead a frame, shift-arrows a second, and the handles are sliders with `aria-valuenow` in seconds, exactly as `VideoTimeline` does it in `web/src/mediaEdit/VideoTimeline.tsx` — read it and follow it.

- [ ] **Step 5: Run the test again** — PASS.

- [ ] **Step 6: Gates and commit**

```bash
cd web && npx tsc --noEmit -p . && npx oxlint --type-aware src/videoStudio && npx prettier --check src/videoStudio && npx vitest run src/videoStudio && npm run deadcode
cd .. && git commit web/src/videoStudio/Timeline.tsx web/src/videoStudio/__tests__/Timeline.test.tsx web/knip.json -m "feat(video-studio): draw the lanes and turn gestures into commands" -m "A drop is an insert, a handle is a slider with its time, and the zoom is on buttons because dnd-timeline has no pinch." -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 7: The stage and the inspector

**Files:**
- Create: `web/src/videoStudio/Stage.tsx`, `web/src/videoStudio/Inspector.tsx`
- Test: `web/src/videoStudio/__tests__/Stage.test.tsx`, `web/src/videoStudio/__tests__/Inspector.test.tsx`

**Interfaces:**
- Consumes: `VideoProject`, `clipAt`, `overlayWindow`, the commands, `@videoflow/renderer-dom`.
- Produces: `<Stage project time selectedId onSelect onCommand />`, `<Inspector project selectedId onCommand />`.

- [ ] **Step 1: Write the failing tests**

Stage: it mounts a `DomRenderer` on the host, calls `loadVideo` with the compiled project once, `seek`s when the time prop changes, `destroy`s on unmount, and a drag on the selection box emits `setProperty` with the new position — clamped to the frame. Mock `@videoflow/renderer-dom` the way Task 5's test mocks the core.

Inspector: for a clip it shows start, end and mute, and each field emits its command on commit — Enter or blur, and an invalid value restores, exactly as `web/src/mediaEdit/TimeField.tsx` does (reuse it). For a text overlay it shows the text, the size, the colour and the animation; for an image overlay the scale and the animation.

- [ ] **Step 2: Run them and watch them fail.**

- [ ] **Step 3: Write the two components.** No Konva: the selection box is a positioned `div` and the drag is pointer events, which is also the only live-feedback path in the editor.

- [ ] **Step 4: Run the tests again** — PASS.

- [ ] **Step 5: Gates and commit**

```bash
cd web && npx tsc --noEmit -p . && npx oxlint --type-aware src/videoStudio && npx prettier --check src/videoStudio && npx vitest run src/videoStudio
cd .. && git commit web/src/videoStudio/Stage.tsx web/src/videoStudio/Inspector.tsx web/src/videoStudio/__tests__/Stage.test.tsx web/src/videoStudio/__tests__/Inspector.test.tsx -m "feat(video-studio): preview the project and edit what is selected" -m "The stage is VideoFlow's DOM renderer with a plain selection box over it; every field commits a command, and an invalid one restores." -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 8: The workspace, the strings, and the file

**Files:**
- Create: `web/src/videoStudio/VideoStudio.tsx`, `web/src/videoStudio/projectStore.ts`, `web/src/i18n/resources.videoStudio.ts`
- Test: `web/src/videoStudio/__tests__/VideoStudio.test.tsx`, `web/src/videoStudio/__tests__/projectStore.test.ts`
- Modify: `web/src/i18n/resources.ts` (import and spread in both locales), `web/src/studio/StudioWorkspace.tsx` (the entrance), `web/src/mediaEdit/VideoEditor.tsx` (one way forward into the editor)

**Interfaces:**
- Consumes: everything above; `presignAsset` and the upload helpers in `web/src/chat/attachments/upload.ts`; `probeVideo` from `web/src/mediaEdit/videoMedia.ts`.
- Produces: the route the Studio opens, and `loadProject(assetId)` / `saveProject(project)`.

- [ ] **Step 1: The strings**

`resources.videoStudio.ts` with `videoStudio.*`: the title, the lane names, every command's button, the refusal sentences keyed exactly as `CommandRefusal.reasonKey` (`videoStudio.refusal.trimPastSource`, `.splitOnBoundary`, `.emptyRange`, `.overlayOverlap`, `.sourceMissing`, `.sourceUndecodable`, `.sourceMissingAsset`), the export progress and its failures, and the save states. English and Italian, both complete: the parity test fails otherwise.

- [ ] **Step 2: The project file**

`projectStore.ts` saves the project as a `.json` asset through the presign path the attachments already use, and loads it by asset id. It is a document, so it lands in `chat/` — the media folder is for the sources. Test: a round trip preserves the project exactly, and a load of a project whose source asset is gone returns the project with that source marked missing.

- [ ] **Step 3: The probe at the door**

Adding a source probes it first with `probeVideo`, which the single-clip editor already ships. A source the browser cannot decode is refused with `videoStudio.refusal.sourceUndecodable` — VideoFlow would otherwise disable the layer and render black frames with no error. Test it with a `probeVideo` that rejects.

- [ ] **Step 4: The workspace**

`VideoStudio.tsx` holds the history, the selection and the playhead; passes `onCommand` to the three panels; catches `CommandRefusal` and shows `t(error.reasonKey)`; runs the export with a progress bar (`role="progressbar"`, the values the previous cycle's review asked for) and a cancel that aborts; downloads with `downloadBlob` from `web/src/mediaEdit/download.ts`.

- [ ] **Step 5: The entrances**

In the Studio, a button that opens the editor on a new project seeded with the selected result. In `VideoEditor.tsx`, one way forward — "Open in the editor" — which creates a project from the clip being trimmed. Neither replaces the quick editor.

- [ ] **Step 6: Run the tests** — every file under 600 lines, `npx vitest run src/videoStudio src/i18n`.

- [ ] **Step 7: Gates and commit**

```bash
cd web && npm run lint && npm run typecheck && npm run format:check && npm run deadcode && npm run dup
cd .. && git commit web/src/videoStudio web/src/i18n/resources.videoStudio.ts web/src/i18n/resources.ts web/src/studio/StudioWorkspace.tsx web/src/mediaEdit/VideoEditor.tsx -m "feat(video-studio): open, edit, save and export a project" -m "The workspace wires the three panels to one history, refuses in the operator's language, probes a source before it can become black frames, and keeps the quick editor as the first answer for one clip." -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 9: End to end, the bundle, and the verification

**Files:**
- Create: `web/e2e/video-studio.spec.ts`, `web/e2e/fixtures/video-studio/clip-a.mp4`, `web/e2e/fixtures/video-studio/clip-b.mp4`, `docs/superpowers/verification/2026-09-20-video-studio.md`
- Modify: `internal/webui/dist/**` (rebuilt)

- [ ] **Step 1: The fixtures** — two 4 s clips with audio, made with the ffmpeg commands from Task 1, a few hundred KB each. `.gitattributes` already marks `*.mp4 binary`.

- [ ] **Step 2: The spec**

Three cases, driven through the real UI:
1. two clips and a title, exported and downloaded; the file read back with mediabunny — duration 8 s ± 0.1, the size the project asks for, and the title present in a frame inside its window and absent outside it;
2. a source the browser cannot decode (the HEVC fixture from the previous cycle) refused at the door with the translated sentence;
3. a network log with **zero** off-origin requests for the whole session.

- [ ] **Step 3: Run it** — `npx playwright test e2e/video-studio.spec.ts --project=chrome --project=mobile-chrome`. On the phone project, the lanes scroll and the fields work; whatever cannot be done with a thumb is skipped with the reason named in the skip, not hidden.

- [ ] **Step 4: Full gates** — from `web/`: `npm run lint && npm run typecheck && npm run format:check && npm run deadcode && npm run dup && npm test`. Coverage ≥ 85 % on all four metrics.

- [ ] **Step 5: The bundle**

```bash
cd web && npm run build
ENTRY=$(grep -o 'assets/index-[^"]*\.js' ../internal/webui/dist/index.html | head -1)
grep -c "VideoFlow" "../internal/webui/dist/$ENTRY" || true
grep -l "VideoFlow" ../internal/webui/dist/assets/*.js | head -3
```
Expected: `0` in the entry chunk; VideoFlow only in the VideoStudio chunk, which is lazy like the other two editors.

- [ ] **Step 6: Live verification**

Push, wait for the edge image, update the appliance (`docker compose pull aura && docker compose up -d aura && docker compose up -d --no-deps --force-recreate aura-migrate`), then on `https://localhost`: build a project from two Studio results, add a title, export, and play the downloaded file. Record it in `docs/superpowers/verification/2026-09-20-video-studio.md` with the revision, the numbers, the screenshots, and what it does not prove (long projects, Safari, a real phone).

- [ ] **Step 7: Commit and push**

```bash
git add web/e2e/video-studio.spec.ts web/e2e/fixtures/video-studio internal/webui/dist docs/superpowers/verification/2026-09-20-video-studio.md
git commit -m "test(video-studio): drive a real project end to end and ship the bundle" -m "Two clips and a title exported and read back, an undecodable source refused at the door, and a network log with nothing leaving the origin." -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
git push origin master
```

---

## Self-review

**Spec coverage.** Lane semantics → Tasks 2 and 3. The measurement the spec gates on → Task 1. One decode per source → Task 5, decided by Task 1. The probe at `addClip` → Task 8 step 3. fps in the project → Task 2. Project stored as a `.json` asset → Task 8 step 2. Export downloads → Task 8 step 4. Dependencies declared, Konva absent → Task 2 step 1. Phone honesty → Tasks 6 and 9. Zero off-origin → Tasks 5 and 9. Library door and agent door are cycles 2 and 3 and appear in no task here.

**Placeholders.** Tasks 6, 7 and 8 describe their tests rather than printing them in full: their components do not exist yet and their assertions depend on the markup the implementer writes. The behaviours to assert are listed one by one, which is the contract; the exact code is not, and that is deliberate rather than an omission.

**Type consistency.** `VideoItem`, `OverlayItem`, `OverlayAnchor`, `VideoProject` are defined in Task 2 and used unchanged in 3, 5, 6, 7 and 8. `CommandRefusal.reasonKey` (Task 3) matches the key list in Task 8 step 1. `onCommand` takes a thunk in Tasks 6 and 7 and is consumed as one in Task 8.
