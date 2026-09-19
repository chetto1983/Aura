# Media editing in the cockpit — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Edit a photo (Filerobot) or a clip (Mediabunny) in the browser from the Studio, chat and attachments, with no AI model: photos go to the Studio library or download, clips download.

**Architecture:** A shared module `web/src/mediaEdit/`. Pure rules (`timecode`, `cropMath`, `editRules`) are unit-tested; a Mediabunny adapter (`videoMedia`) and a Filerobot setup (`filerobotSetup`) isolate the two libraries; one root-level `MediaEditorProvider` owns the open editor so any surface — including a Radix modal — can hand off to it; `EditMediaButton` is the only thing each surface adds.

**Tech Stack:** React 19, TypeScript 7 (strict, `exactOptionalPropertyTypes`, `noUncheckedIndexedAccess`, `verbatimModuleSyntax`), Vite 8, Vitest + Testing Library (jsdom), Playwright, i18next, TanStack Query, `react-filerobot-image-editor` 5.0.0-beta.159, `mediabunny` 1.58.1, `styled-components` 6, `@emotion/is-prop-valid`.

**Spec:** `docs/superpowers/specs/2026-09-19-studio-media-editing-design.md` (Part 1). Part 2 (video to the model) is the separate plan `2026-09-19-video-to-model.md`.

## Global Constraints

- Commit directly on `master`; never create a branch. Stage files by explicit path, never `git add -A` (other sessions leave dirty files).
- Commit subject imperative and short; body says why; end with `Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>`.
- No file over 600 lines (`scripts/` file-size gate covers `.ts`/`.tsx`).
- Web gates: `npm run lint` (oxlint type-aware, 0 warnings), `npm run typecheck`, `npm run format:check`, `npm run deadcode` (knip), `npm run dup` (jscpd), `npm test` (Vitest, coverage ≥ 85 % statements/branches/functions/lines). Mutation (Stryker) runs in CI only — never locally.
- Every user-visible string goes through i18next with `en` and `it` (the parity test fails otherwise).
- No request may leave the origin: Filerobot runs with `useBackendTranslations={false}` and without its AI tab.
- Filerobot: `savingPixelRatio={1}`, `previewPixelRatio={window.devicePixelRatio || 1}`; wrap it in `StyleSheetManager shouldForwardProp`.
- Mediabunny trims with `copy: { boundaryPolicy: 'expand' }` (frame-exact on MP4 via edit list, spike 105); never `shrink`.
- A conversion whose `discardedTracks` holds a video or audio track for a reason other than `discarded_by_user` produces no file.
- Editable types: `image/png`, `image/jpeg`, `image/webp`, `video/mp4`, `video/webm`. Nothing else shows the Edit button.
- The webui dist is rebuilt with `npm run build` in `web/` on Windows and committed (`internal/webui/dist`).

## Spec adjustments made while planning (recorded here, folded into the spec in Task 1)

1. The trim fields show and parse `mm:ss.s` with their own `timecode.ts` pair; `durationFormat.ts` renders locale sentences ("3.2 s") that cannot round-trip through an input.
2. The photo editor hides Filerobot's Save button (`removeSaveButton`) and exports through `getCurrentImgDataFnRef` from Aura's own header (Download, Save to library, Close). `onBeforeSave` is therefore not used, and the only unsaved-changes prompt is Aura's `ConfirmDialog`.
3. The "too large → re-encode as JPEG" fallback is dropped: the presign route answers **400** with the server's own sentence (`internal/agui/assets_api.go:93-95`), so the only honest thing to do is show that sentence; Download stays available.
4. Studio availability is read **before** uploading, from the library query (`useStudioLibrary`): a 503 there disables Save to library, so no orphan upload is created.
5. The open editor lives in a `MediaEditorProvider` mounted in `AppShell`, not inside each button: `PreviewModal` is a Radix Dialog whose focus trap would swallow an editor opened inside it, so the button closes the modal and hands the target to the provider. The share page is not under `AppShell`, which is a second guard beside `AssetSource.editable`.
6. The button hides for SVG/GIF when the surface knows the MIME type; on the Studio stage (which does not) the editor resolves it with `getAsset` and says "This format cannot be edited here" if needed.

---

## File structure

```
web/src/mediaEdit/
  timecode.ts               format/parse "mm:ss.s"                              (Task 1)
  cropMath.ts               rotation-aware crop presets and moves                 (Task 1)
  editRules.ts              editable kinds, output names, conversion options,
                            the discarded-track guard                             (Task 2)
  filerobotSetup.ts         Italian table, theme from Aura tokens, prop filter    (Task 3)
  filerobot-translations.d.ts  type shim for the parity test's deep import        (Task 3)
  MediaEditorLayer.tsx      full-screen, inert-backed surface (not Radix)         (Task 4)
  useObjectUrl.ts           object URL with revocation                            (Task 4)
  download.ts               save a Blob under a file name                         (Task 4)
  videoMedia.ts             Mediabunny adapter: probe, filmstrip, export          (Task 5)
  VideoTimeline.tsx         filmstrip + two keyboard/pointer handles              (Task 6)
  TimeField.tsx             the Start/End input                                   (Task 6)
  CropOverlay.tsx           movable crop box over the preview                     (Task 7)
  VideoEditor.tsx           the clip editor                                       (Task 7)
  PhotoEditor.tsx           the photo editor                                      (Task 8)
  mediaEditorContext.ts     the open-editor context object                        (Task 9)
  MediaEditorProvider.tsx   owns the open editor                                  (Task 9)
  MediaEditorHost.tsx       resolves the asset, loads bytes, picks an editor      (Task 9)
  EditMediaButton.tsx       the only thing a surface adds                         (Task 9)
  __tests__/*.test.ts(x)
web/src/i18n/resources.mediaEdit.ts                                                (Task 4)
web/e2e/media-edit.spec.ts, web/e2e/fixtures/media-edit/{clip.mp4,photo.png}       (Task 11)
```

Modified: `web/package.json`, `web/package-lock.json`, `web/vitest.config.ts` (Task 3),
`web/src/i18n/resources.ts` (Task 4), `web/src/chat/artifacts/renderers/assetSourceContext.ts` and
`web/src/AppShell.tsx` (Task 9), `web/src/studio/StudioStage.tsx`, `web/src/components/image.tsx`,
`web/src/chat/artifacts/renderers/GeneratedImagePreview.tsx`,
`web/src/chat/displays/LocalArtifactDisplay.tsx`, `web/src/chat/artifacts/PreviewModal.tsx`,
`web/src/chat/attachments/AttachmentCard.tsx`, `web/src/chat/attachments/types.ts` (Task 10).

All commands run from `D:/Repo/Aura/web` unless stated.

---

### Task 1: Dependencies, `timecode`, `cropMath`

**Files:**
- Modify: `web/package.json`, `web/package-lock.json`
- Create: `web/src/mediaEdit/timecode.ts`, `web/src/mediaEdit/cropMath.ts`
- Test: `web/src/mediaEdit/__tests__/timecode.test.ts`, `web/src/mediaEdit/__tests__/cropMath.test.ts`
- Modify: `docs/superpowers/specs/2026-09-19-studio-media-editing-design.md` (fold the six adjustments above)

**Interfaces:**
- Produces: `formatTimecode(seconds: number): string`, `parseTimecode(text: string): number | undefined`;
  `type Rotation = 0 | 90 | 180 | 270`, `type CropPreset = 'original' | '1:1' | '9:16' | '16:9' | '4:3' | '3:4'`,
  `CROP_PRESETS: readonly CropPreset[]`, `interface Size { width; height }`, `interface CropRect { left; top; width; height }`,
  `rotateSize(size: Size, rotation: Rotation): Size`, `presetRect(frame: Size, preset: CropPreset): CropRect`,
  `moveRect(rect: CropRect, dx: number, dy: number, frame: Size): CropRect`.

- [ ] **Step 1: Install the libraries**

```bash
npm install --save-exact react-filerobot-image-editor@5.0.0-beta.159 mediabunny@1.58.1
npm install styled-components@^6.5.3 @emotion/is-prop-valid@^1.4.0
npm ls react-konva konva styled-components
```
Expected: `react-konva@19.x` and `konva@9.3.18` appear as peers resolved under `react-filerobot-image-editor` (npm installs peers). Do **not** add `konva` or `react-konva` to `package.json`: nothing in Aura imports them, and knip would report them unused.

- [ ] **Step 2: Write the failing tests**

`web/src/mediaEdit/__tests__/timecode.test.ts`:
```ts
import { describe, expect, it } from 'vitest';
import { formatTimecode, parseTimecode } from '../timecode';

describe('formatTimecode', () => {
  it.each([
    [0, '00:00.0'],
    [2.5, '00:02.5'],
    [59.96, '01:00.0'],
    [75.25, '01:15.3'],
    [-3, '00:00.0'],
  ])('writes %s as %s', (seconds, text) => {
    expect(formatTimecode(seconds)).toBe(text);
  });
});

describe('parseTimecode', () => {
  it.each([
    ['2.5', 2.5],
    ['00:02.5', 2.5],
    ['1:15.3', 75.3],
    ['10', 10],
    [' 00:03 ', 3],
  ])('reads %s as %s seconds', (text, seconds) => {
    expect(parseTimecode(text)).toBeCloseTo(seconds, 5);
  });

  it.each(['', 'abc', '1:2:3', '1:60', '1.5:10', '-1', '0x10', '1e3', '1..2', ':5'])(
    'refuses %j',
    (text) => {
      expect(parseTimecode(text)).toBeUndefined();
    },
  );

  it('reads back every tenth it writes over ten minutes', () => {
    for (let tenth = 0; tenth < 6000; tenth += 1) {
      expect(parseTimecode(formatTimecode(tenth / 10))).toBeCloseTo(tenth / 10, 5);
    }
  });
});
```

`web/src/mediaEdit/__tests__/cropMath.test.ts`:
```ts
import { describe, expect, it } from 'vitest';
import { CROP_PRESETS, moveRect, presetRect, rotateSize } from '../cropMath';

const HD = { width: 1280, height: 720 };

describe('rotateSize', () => {
  it('swaps the sides on a quarter turn only', () => {
    expect(rotateSize(HD, 0)).toEqual(HD);
    expect(rotateSize(HD, 180)).toEqual(HD);
    expect(rotateSize(HD, 90)).toEqual({ width: 720, height: 1280 });
    expect(rotateSize(HD, 270)).toEqual({ width: 720, height: 1280 });
  });
});

describe('presetRect', () => {
  it('fills an HD frame with the largest centred square', () => {
    expect(presetRect(HD, '1:1')).toEqual({ left: 280, top: 0, width: 720, height: 720 });
  });

  it('keeps even sides for the encoder', () => {
    expect(presetRect(HD, '9:16')).toEqual({ left: 438, top: 0, width: 404, height: 720 });
    expect(presetRect({ width: 1279, height: 719 }, 'original')).toEqual({
      left: 0,
      top: 0,
      width: 1278,
      height: 718,
    });
  });

  it('works in the rotated frame', () => {
    expect(presetRect(rotateSize(HD, 90), '9:16')).toEqual({
      left: 0,
      top: 0,
      width: 720,
      height: 1280,
    });
  });

  it('stays inside the frame for every preset and rotation', () => {
    for (const rotation of [0, 90, 180, 270] as const) {
      const frame = rotateSize({ width: 1918, height: 1080 }, rotation);
      for (const preset of CROP_PRESETS) {
        const rect = presetRect(frame, preset);
        expect(rect.left).toBeGreaterThanOrEqual(0);
        expect(rect.top).toBeGreaterThanOrEqual(0);
        expect(rect.left + rect.width).toBeLessThanOrEqual(frame.width);
        expect(rect.top + rect.height).toBeLessThanOrEqual(frame.height);
        expect(rect.width % 2).toBe(0);
        expect(rect.height % 2).toBe(0);
      }
    }
  });
});

describe('moveRect', () => {
  const square = presetRect(HD, '1:1');

  it('moves by whole pixels', () => {
    expect(moveRect(square, -100.4, 0, HD)).toEqual({ ...square, left: 180 });
  });

  it('never leaves the frame', () => {
    expect(moveRect(square, -1000, 50, HD)).toEqual({ ...square, left: 0, top: 0 });
    expect(moveRect(square, 1000, 0, HD)).toEqual({ ...square, left: 560 });
  });
});
```

- [ ] **Step 3: Run them to verify they fail**

Run: `npx vitest run src/mediaEdit/__tests__/timecode.test.ts src/mediaEdit/__tests__/cropMath.test.ts`
Expected: FAIL — `Failed to resolve import "../timecode"` / `"../cropMath"`.

- [ ] **Step 4: Implement**

`web/src/mediaEdit/timecode.ts`:
```ts
// timecode.ts — the "mm:ss.s" form the trim fields show and accept. The two functions are a
// pair: whatever formatTimecode writes, parseTimecode reads back to the same tenth, which is
// why the fields do not reuse chat/durationFormat.ts (a locale sentence, not an input format).

/** Seconds → "mm:ss.s", rounded to the tenth; negative time reads as zero. */
export function formatTimecode(seconds: number): string {
  const tenths = Math.round(Math.max(0, seconds) * 10);
  const minutes = Math.floor(tenths / 600);
  const rest = (tenths % 600) / 10;
  return `${String(minutes).padStart(2, '0')}:${rest.toFixed(1).padStart(4, '0')}`;
}

/** A plain decimal: digits with at most one dot. Number() alone would also accept "0x10",
 *  "1e3" and signs, none of which is a time the operator typed. */
function decimal(part: string): number | undefined {
  if (part === '') return undefined;
  for (const char of part) {
    if (char !== '.' && (char < '0' || char > '9')) return undefined;
  }
  const value = Number(part);
  return Number.isFinite(value) ? value : undefined;
}

/** "ss", "ss.s", "mm:ss" or "mm:ss.s" → seconds; undefined for anything else. */
export function parseTimecode(text: string): number | undefined {
  const parts = text.trim().split(':');
  if (parts.length === 1) return decimal(parts[0] ?? '');
  if (parts.length !== 2) return undefined;
  const minutes = decimal(parts[0] ?? '');
  const seconds = decimal(parts[1] ?? '');
  if (minutes === undefined || seconds === undefined) return undefined;
  if (!Number.isInteger(minutes) || seconds >= 60) return undefined;
  return minutes * 60 + seconds;
}
```

`web/src/mediaEdit/cropMath.ts`:
```ts
// cropMath.ts — the crop box of the video editor, in the frame the encoder will see. Mediabunny
// applies `rotate` before `crop` (mediabunny.d.ts, ConversionVideoOptions), and a track's
// display size already includes the rotation its file declares, so the frame is the display
// size turned by the operator's own rotation.

export type Rotation = 0 | 90 | 180 | 270;
export type CropPreset = 'original' | '1:1' | '9:16' | '16:9' | '4:3' | '3:4';
export const CROP_PRESETS: readonly CropPreset[] = ['original', '1:1', '9:16', '16:9', '4:3', '3:4'];

export interface Size {
  readonly width: number;
  readonly height: number;
}

export interface CropRect {
  readonly left: number;
  readonly top: number;
  readonly width: number;
  readonly height: number;
}

const RATIOS: Readonly<Record<Exclude<CropPreset, 'original'>, readonly [number, number]>> = {
  '1:1': [1, 1],
  '9:16': [9, 16],
  '16:9': [16, 9],
  '4:3': [4, 3],
  '3:4': [3, 4],
};

export function rotateSize(size: Size, rotation: Rotation): Size {
  return rotation === 90 || rotation === 270
    ? { width: size.height, height: size.width }
    : size;
}

// H.264 with 4:2:0 chroma needs even sides.
function evenFloor(value: number): number {
  return Math.max(2, Math.floor(value / 2) * 2);
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

/** The largest rectangle of the preset's ratio inside `frame`, centred, with even sides. */
export function presetRect(frame: Size, preset: CropPreset): CropRect {
  if (preset === 'original') {
    return { left: 0, top: 0, width: evenFloor(frame.width), height: evenFloor(frame.height) };
  }
  const [w, h] = RATIOS[preset];
  const scale = Math.min(frame.width / w, frame.height / h);
  const width = evenFloor(w * scale);
  const height = evenFloor(h * scale);
  return {
    left: Math.floor((frame.width - width) / 2),
    top: Math.floor((frame.height - height) / 2),
    width,
    height,
  };
}

/** `rect` moved by (dx, dy) frame pixels, kept whole and inside `frame`. */
export function moveRect(rect: CropRect, dx: number, dy: number, frame: Size): CropRect {
  return {
    ...rect,
    left: clamp(Math.round(rect.left + dx), 0, frame.width - rect.width),
    top: clamp(Math.round(rect.top + dy), 0, frame.height - rect.height),
  };
}
```

- [ ] **Step 5: Run them to verify they pass**

Run: `npx vitest run src/mediaEdit/__tests__/timecode.test.ts src/mediaEdit/__tests__/cropMath.test.ts`
Expected: PASS (all cases).

- [ ] **Step 6: Fold the planning adjustments into the spec**

Edit `docs/superpowers/specs/2026-09-19-studio-media-editing-design.md`:
- `timecode.ts` line → `timecode.ts   pure: format and parse "mm:ss.s" (a round-tripping pair)`.
- Photo editor "Config" bullet: replace `onBeforeSave={() => false}`, `defaultSavedImageName`, `defaultSavedImageType` with `removeSaveButton` + export through `getCurrentImgDataFnRef` from Aura's header; unsaved changes → Aura's `ConfirmDialog` only.
- Photo "too large" sub-bullet → "A refused upload shows the server's own sentence (the presign route answers 400 with it); Download stays available."
- Photo "Studio unwired" sub-bullet → "Studio availability is read before uploading from the library query; a 503 disables Save to library with the sentence …".
- Architecture: add `MediaEditorProvider` in `AppShell` owning the open editor; `EditMediaButton` hands the target to it (and a modal closes itself first).
- `EditMediaButton` bullet: hides on SVG/GIF when the surface knows the MIME type; the Studio stage resolves it in the editor.

- [ ] **Step 7: Commit**

```bash
git add web/package.json web/package-lock.json web/src/mediaEdit/timecode.ts web/src/mediaEdit/cropMath.ts web/src/mediaEdit/__tests__/timecode.test.ts web/src/mediaEdit/__tests__/cropMath.test.ts docs/superpowers/specs/2026-09-19-studio-media-editing-design.md
git commit -m "feat(media-edit): add the trim timecode and crop geometry" -m "The editors' two pieces of pure maths, tested alone, plus the libraries the editors will load and the spec adjustments found while planning." -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 2: `editRules` — kinds, names, conversion options, the discard guard

**Files:**
- Create: `web/src/mediaEdit/editRules.ts`
- Test: `web/src/mediaEdit/__tests__/editRules.test.ts`

**Interfaces:**
- Consumes: `CropRect`, `Rotation` (Task 1).
- Produces: `type EditKind = 'image' | 'video'`; `editableKind(mimeType: string): EditKind | undefined`;
  `type ImageExtension = 'png' | 'jpeg' | 'webp'`; `imageExtension(mimeType: string): ImageExtension`;
  `type VideoContainer = 'mp4' | 'webm'`; `videoContainer(mimeType: string): VideoContainer`;
  `editedBase(fileName: string, suffix: string): string`; `editedName(fileName: string, suffix: string, extension: string): string`;
  `interface VideoEdit { start; end; rotation: Rotation; crop?: CropRect; mute: boolean }`;
  `conversionOptions(edit: VideoEdit): EditConversion` where `EditConversion = Pick<ConversionOptions, 'trim' | 'copy' | 'video' | 'audio'>`;
  `interface BlockedTrack { type: 'video' | 'audio'; codec: string; reason: DiscardedTrack['reason'] }`;
  `blockingDiscards(discarded: readonly DiscardedTrack[]): BlockedTrack[]`.

- [ ] **Step 1: Write the failing test**

`web/src/mediaEdit/__tests__/editRules.test.ts`:
```ts
import type { DiscardedTrack, InputTrack } from 'mediabunny';
import { describe, expect, it } from 'vitest';
import {
  blockingDiscards,
  conversionOptions,
  editableKind,
  editedBase,
  editedName,
  imageExtension,
  videoContainer,
} from '../editRules';

function discarded(type: string, codec: string | null, reason: DiscardedTrack['reason']): DiscardedTrack {
  return { track: { type, codec } as unknown as InputTrack, reason, options: {} } as DiscardedTrack;
}

describe('editableKind', () => {
  it.each([
    ['image/png', 'image'],
    ['image/jpeg', 'image'],
    ['IMAGE/WEBP', 'image'],
    ['video/mp4', 'video'],
    ['video/webm; codecs="vp9,opus"', 'video'],
  ])('%s is %s', (mime, kind) => {
    expect(editableKind(mime)).toBe(kind);
  });

  it.each(['image/svg+xml', 'image/gif', 'video/quicktime', 'application/octet-stream', ''])(
    '%j is not editable',
    (mime) => {
      expect(editableKind(mime)).toBeUndefined();
    },
  );
});

describe('names and containers', () => {
  it('keeps the base name and swaps the extension', () => {
    expect(editedName('beach.mp4', 'edited', 'mp4')).toBe('beach-edited.mp4');
    expect(editedName('my.holiday.webm', 'modificato', 'webm')).toBe('my.holiday-modificato.webm');
    expect(editedName('.hidden', 'edited', 'png')).toBe('.hidden-edited.png');
    expect(editedBase('photo.jpeg', 'modificata')).toBe('photo-modificata');
  });

  it('follows the source type', () => {
    expect(imageExtension('image/jpeg')).toBe('jpeg');
    expect(imageExtension('image/webp')).toBe('webp');
    expect(imageExtension('image/png')).toBe('png');
    expect(videoContainer('video/webm')).toBe('webm');
    expect(videoContainer('video/mp4')).toBe('mp4');
  });
});

describe('conversionOptions', () => {
  it('trims on the copy path that keeps the start frame exact', () => {
    expect(conversionOptions({ start: 2.5, end: 6, rotation: 0, mute: false })).toEqual({
      trim: { start: 2.5, end: 6 },
      copy: { boundaryPolicy: 'expand' },
      video: { rotate: 0 },
    });
  });

  it('adds crop, rotation and a muted audio track only when asked', () => {
    const crop = { left: 280, top: 0, width: 720, height: 720 };
    expect(conversionOptions({ start: 0, end: 4, rotation: 90, crop, mute: true })).toEqual({
      trim: { start: 0, end: 4 },
      copy: { boundaryPolicy: 'expand' },
      video: { rotate: 90, crop },
      audio: { discard: true },
    });
  });
});

describe('blockingDiscards', () => {
  it('lets through what the operator discarded', () => {
    expect(blockingDiscards([discarded('audio', 'aac', 'discarded_by_user')])).toEqual([]);
  });

  it('blocks a track this browser cannot process', () => {
    expect(
      blockingDiscards([
        discarded('video', 'hevc', 'undecodable_source_codec'),
        discarded('audio', null, 'no_encodable_target_codec'),
        discarded('subtitle', 'webvtt', 'max_track_count_of_type_reached'),
      ]),
    ).toEqual([
      { type: 'video', codec: 'hevc', reason: 'undecodable_source_codec' },
      { type: 'audio', codec: 'unknown', reason: 'no_encodable_target_codec' },
    ]);
  });
});
```

- [ ] **Step 2: Run it to verify it fails**

Run: `npx vitest run src/mediaEdit/__tests__/editRules.test.ts`
Expected: FAIL — `Failed to resolve import "../editRules"`.

- [ ] **Step 3: Implement**

`web/src/mediaEdit/editRules.ts`:
```ts
import type { ConversionOptions, DiscardedTrack } from 'mediabunny';
import type { CropRect, Rotation } from './cropMath';

// editRules.ts — what can be edited, what the result is called, and what Mediabunny is asked
// to do. Pure, so every rule is tested without a browser codec.

export type EditKind = 'image' | 'video';
export type ImageExtension = 'png' | 'jpeg' | 'webp';
export type VideoContainer = 'mp4' | 'webm';

// Filerobot reads png/jpeg/webp (its SUPPORTED_IMAGE_TYPES); the asset allowlist stores MP4 and
// WebM video (internal/assets/limits.go). SVG, GIF and QuickTime are not offered: an SVG does not
// rasterise safely, a GIF would lose its frames, and the server refuses .mov.
const EDITABLE: Readonly<Record<string, EditKind>> = {
  'image/png': 'image',
  'image/jpeg': 'image',
  'image/webp': 'image',
  'video/mp4': 'video',
  'video/webm': 'video',
};

/** The media type without parameters, lower-cased ("video/webm; codecs=…" → "video/webm"). */
function essence(mimeType: string): string {
  return (mimeType.split(';')[0] ?? '').trim().toLowerCase();
}

export function editableKind(mimeType: string): EditKind | undefined {
  return EDITABLE[essence(mimeType)];
}

export function imageExtension(mimeType: string): ImageExtension {
  const type = essence(mimeType);
  if (type === 'image/jpeg') return 'jpeg';
  if (type === 'image/webp') return 'webp';
  return 'png';
}

export function videoContainer(mimeType: string): VideoContainer {
  return essence(mimeType) === 'video/webm' ? 'webm' : 'mp4';
}

export function editedBase(fileName: string, suffix: string): string {
  const dot = fileName.lastIndexOf('.');
  const base = dot > 0 ? fileName.slice(0, dot) : fileName;
  return `${base}-${suffix}`;
}

export function editedName(fileName: string, suffix: string, extension: string): string {
  return `${editedBase(fileName, suffix)}.${extension}`;
}

export interface VideoEdit {
  readonly start: number;
  readonly end: number;
  readonly rotation: Rotation;
  readonly crop?: CropRect;
  readonly mute: boolean;
}

export type EditConversion = Pick<ConversionOptions, 'trim' | 'copy' | 'video' | 'audio'>;

/** Always the `expand` copy path: Mediabunny copies what it can (a trim, a mute) and transcodes
 *  only what it must (a crop, WebM). On MP4 the copy keeps the preceding key frame behind an
 *  edit list, so playback starts exactly on the requested frame (spike 105). */
export function conversionOptions(edit: VideoEdit): EditConversion {
  const options: EditConversion = {
    trim: { start: edit.start, end: edit.end },
    copy: { boundaryPolicy: 'expand' },
    video:
      edit.crop === undefined ? { rotate: edit.rotation } : { rotate: edit.rotation, crop: edit.crop },
  };
  return edit.mute ? { ...options, audio: { discard: true } } : options;
}

export interface BlockedTrack {
  readonly type: 'video' | 'audio';
  readonly codec: string;
  readonly reason: DiscardedTrack['reason'];
}

/** The tracks this browser dropped on its own. `conversion.isValid` is not enough: spike 105 saw
 *  Firefox drop an HEVC video track and still return a valid, audio-only file. */
export function blockingDiscards(discarded: readonly DiscardedTrack[]): BlockedTrack[] {
  const blocked: BlockedTrack[] = [];
  for (const { track, reason } of discarded) {
    if (reason === 'discarded_by_user') continue;
    if (track.type !== 'video' && track.type !== 'audio') continue;
    blocked.push({ type: track.type, codec: track.codec ?? 'unknown', reason });
  }
  return blocked;
}
```

- [ ] **Step 4: Run it to verify it passes**

Run: `npx vitest run src/mediaEdit/__tests__/editRules.test.ts`
Expected: PASS. If `tsc` later reports that `track.codec` is not on `InputTrack`, narrow first: `const codec = 'codec' in track ? track.codec : null;`.

- [ ] **Step 5: Commit**

```bash
git add web/src/mediaEdit/editRules.ts web/src/mediaEdit/__tests__/editRules.test.ts
git commit -m "feat(media-edit): decide what is editable and what a cut asks for" -m "Editable types, output names and Mediabunny options in one pure module, with the guard that refuses a file a browser silently stripped of a track." -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 3: `filerobotSetup` — Italian table, theme, prop filter

**Files:**
- Create: `web/src/mediaEdit/filerobotSetup.ts`, `web/src/mediaEdit/filerobot-translations.d.ts`
- Modify: `web/vitest.config.ts` (inline the package for the test runner)
- Test: `web/src/mediaEdit/__tests__/filerobotSetup.test.ts`

**Interfaces:**
- Produces: `FILEROBOT_IT: Readonly<Record<string, string>>` (128 keys);
  `filerobotTranslations(language: string): Readonly<Record<string, string>> | undefined`;
  `type FilerobotTheme`; `filerobotTheme(root?: Element): FilerobotTheme`;
  `forwardDomProp(prop: string, target: unknown): boolean`.

- [ ] **Step 1: Let Vitest load the package**

`react-filerobot-image-editor` ships ES modules under `lib/` without `"type": "module"`, so Node cannot load them when Vitest externalises `node_modules`. In `web/vitest.config.ts`, inside `test: { … }`, add:
```ts
    // react-filerobot-image-editor ships ESM `lib/*.js` without "type": "module"; Node would
    // read it as CommonJS. Inlining lets Vite transform it (the parity test imports its table).
    server: { deps: { inline: ['react-filerobot-image-editor'] } },
```

- [ ] **Step 2: Write the failing test**

`web/src/mediaEdit/__tests__/filerobotSetup.test.ts`:
```ts
import defaultTranslations from 'react-filerobot-image-editor/lib/context/defaultTranslations';
import { describe, expect, it } from 'vitest';
import { FILEROBOT_IT, filerobotTheme, filerobotTranslations, forwardDomProp } from '../filerobotSetup';

describe('FILEROBOT_IT', () => {
  it('covers exactly the keys Filerobot ships', () => {
    // A new Filerobot key would otherwise fall back to English without anyone noticing.
    expect(Object.keys(FILEROBOT_IT).sort()).toEqual(Object.keys(defaultTranslations).sort());
  });

  it('translates every key', () => {
    for (const [key, value] of Object.entries(FILEROBOT_IT)) {
      expect(value, key).not.toBe('');
    }
  });
});

describe('filerobotTranslations', () => {
  it('is the Italian table for Italian and the library default otherwise', () => {
    expect(filerobotTranslations('it')).toBe(FILEROBOT_IT);
    expect(filerobotTranslations('it-IT')).toBe(FILEROBOT_IT);
    expect(filerobotTranslations('en')).toBeUndefined();
  });
});

describe('filerobotTheme', () => {
  it('reads the colours and the font from the Aura tokens', () => {
    const root = document.createElement('div');
    root.style.setProperty('--color-accent', '#ff5500');
    root.style.setProperty('--color-surface', '#101010');
    root.style.setProperty('--font-sans', 'Test Sans');
    document.body.append(root);
    const theme = filerobotTheme(root);
    expect(theme.palette?.['accent-primary']).toBe('#ff5500');
    expect(theme.palette?.['bg-primary']).toBe('#101010');
    expect(theme.typography?.fontFamily).toBe('Test Sans');
    root.remove();
  });

  it('leaves out a token the page does not define', () => {
    const root = document.createElement('div');
    document.body.append(root);
    expect(filerobotTheme(root).palette).toEqual({});
    root.remove();
  });
});

describe('forwardDomProp', () => {
  it('drops styled props on DOM elements and keeps them on components', () => {
    expect(forwardDomProp('showTabsDrawer', 'div')).toBe(false);
    expect(forwardDomProp('aria-label', 'div')).toBe(true);
    expect(forwardDomProp('showTabsDrawer', () => null)).toBe(true);
  });
});
```

- [ ] **Step 3: Run it to verify it fails**

Run: `npx vitest run src/mediaEdit/__tests__/filerobotSetup.test.ts`
Expected: FAIL — `Failed to resolve import "../filerobotSetup"`.

- [ ] **Step 4: Implement**

`web/src/mediaEdit/filerobot-translations.d.ts`:
```ts
// The parity test imports Filerobot's own English table, which the package ships without types.
declare module 'react-filerobot-image-editor/lib/context/defaultTranslations' {
  const translations: Readonly<Record<string, string>>;
  export default translations;
}
```

`web/src/mediaEdit/filerobotSetup.ts`:
```ts
import isPropValid from '@emotion/is-prop-valid';
import type { ComponentProps } from 'react';
import type FilerobotImageEditor from 'react-filerobot-image-editor';

// filerobotSetup.ts — everything Aura hands Filerobot that is not a prop value: the Italian
// table, the theme read from Aura's tokens, and the prop filter styled-components 6 needs.
//
// The Italian table is Aura's own. Filerobot can fetch translations from Scaleflex's service
// (useBackendTranslations, on by default), which sends a request off-origin and, without a
// Scaleflex grid id, returns nothing (spike 105). The editor always runs with it off.

export type FilerobotTheme = NonNullable<ComponentProps<typeof FilerobotImageEditor>['theme']>;

export const FILEROBOT_IT: Readonly<Record<string, string>> = {
  name: 'Nome',
  save: 'Salva',
  saveAs: 'Salva come',
  back: 'Indietro',
  loading: 'Caricamento…',
  resetOperations: 'Annulla tutte le modifiche',
  changesLoseWarningHint: 'Se premi “reimposta” perderai le modifiche. Vuoi continuare?',
  discardChangesWarningHint: "Se chiudi la finestra, l'ultima modifica non verrà salvata.",
  cancel: 'Annulla',
  apply: 'Applica',
  warning: 'Attenzione',
  confirm: 'Conferma',
  discardChanges: 'Scarta le modifiche',
  undoTitle: "Annulla l'ultima operazione",
  redoTitle: "Ripeti l'ultima operazione",
  showImageTitle: "Mostra l'immagine originale",
  zoomInTitle: 'Ingrandisci',
  fitTitle: 'Adatta',
  zoomOutTitle: 'Riduci',
  toggleZoomMenuTitle: 'Menu dello zoom',
  adjustTab: 'Regola',
  finetuneTab: 'Ritocca',
  filtersTab: 'Filtri',
  watermarkTab: 'Filigrana',
  annotateTabLabel: 'Annota',
  resize: 'Ridimensiona',
  resizeTab: 'Ridimensiona',
  imageName: "Nome dell'immagine",
  invalidImageError: 'Immagine non valida.',
  uploadImageError: "Errore durante il caricamento dell'immagine.",
  areNotImages: 'non sono immagini',
  isNotImage: "non è un'immagine",
  toBeUploaded: 'da caricare',
  cropTool: 'Ritaglia',
  original: 'Originale',
  custom: 'Personalizzato',
  square: 'Quadrato',
  landscape: 'Orizzontale',
  portrait: 'Verticale',
  ellipse: 'Ellisse',
  classicTv: 'TV classica',
  cinemascope: 'Cinemascope',
  arrowTool: 'Freccia',
  blurTool: 'Sfocatura',
  brightnessTool: 'Luminosità',
  contrastTool: 'Contrasto',
  ellipseTool: 'Ellisse',
  unFlipX: 'Annulla specchio orizzontale',
  flipX: 'Specchia in orizzontale',
  unFlipY: 'Annulla specchio verticale',
  flipY: 'Specchia in verticale',
  hsvTool: 'HSV',
  hue: 'Tonalità',
  brightness: 'Luminosità',
  saturation: 'Saturazione',
  value: 'Valore',
  imageTool: 'Immagine',
  importing: 'Importazione…',
  addImage: 'Aggiungi immagine',
  uploadImage: 'Carica immagine',
  fromGallery: 'Dalla galleria',
  lineTool: 'Linea',
  penTool: 'Penna',
  polygonTool: 'Poligono',
  sides: 'Lati',
  rectangleTool: 'Rettangolo',
  cornerRadius: 'Raggio degli angoli',
  resizeWidthTitle: 'Larghezza in pixel',
  resizeHeightTitle: 'Altezza in pixel',
  toggleRatioLockTitle: 'Blocca le proporzioni',
  resetSize: 'Ripristina la dimensione originale',
  rotateTool: 'Ruota',
  textTool: 'Testo',
  textSpacings: 'Spaziatura del testo',
  textAlignment: 'Allineamento del testo',
  fontFamily: 'Carattere',
  size: 'Dimensione',
  letterSpacing: 'Spaziatura delle lettere',
  lineHeight: 'Interlinea',
  warmthTool: 'Calore',
  addWatermark: 'Aggiungi filigrana',
  addTextWatermark: 'Aggiungi filigrana di testo',
  addWatermarkTitle: 'Scegli il tipo di filigrana',
  uploadWatermark: 'Carica filigrana',
  addWatermarkAsText: 'Aggiungi come testo',
  padding: 'Margine',
  paddings: 'Margini',
  shadow: 'Ombra',
  horizontal: 'Orizzontale',
  vertical: 'Verticale',
  blur: 'Sfocatura',
  opacity: 'Opacità',
  transparency: 'Trasparenza',
  position: 'Posizione',
  stroke: 'Contorno',
  saveAsModalTitle: 'Salva come',
  extension: 'Estensione',
  format: 'Formato',
  nameIsRequired: 'Il nome è obbligatorio.',
  quality: 'Qualità',
  imageDimensionsHoverTitle: "Dimensione dell'immagine salvata (larghezza × altezza)",
  cropSizeLowerThanResizedWarning:
    "Nota: l'area di ritaglio è più piccola del ridimensionamento applicato e la qualità potrebbe peggiorare",
  actualSize: 'Dimensione reale (100%)',
  fitSize: 'Adatta alla finestra',
  addImageTitle: "Seleziona l'immagine da aggiungere…",
  mutualizedFailedToLoadImg: "Impossibile caricare l'immagine.",
  tabsMenu: 'Menu',
  download: 'Scarica',
  width: 'Larghezza',
  height: 'Altezza',
  cropItemNoEffect: 'Anteprima non disponibile per questo formato',
  px: 'px',
  invalidTextContent: 'Testo non valido',
  baselineShift: 'Spostamento della linea di base',
  aiTab: 'Strumenti AI',
  objectRemovalTool: 'Rimozione di oggetti',
  objectRemovalBrushSize: 'Dimensione del pennello (px)',
  objectRemovalApplyButton: 'Applica',
  objectRemovalBrushMode: 'Modalità del pennello',
  objectRemovalBrushCircleType: 'Pennello rotondo',
  objectRemovalBrushSquareType: 'Pennello quadrato',
  objectRemovalApplyingText: "Rimozione dell'area selezionata",
  objectRemovalMarkModeTooltip: 'Segna',
  objectRemovalUnMarkModeTooltip: 'Togli il segno',
  objectRemovalCancelConfirmationTitle: "Annulla l'operazione",
  objectRemovalCancelConfirmationHint: "Vuoi davvero annullare la rimozione dell'oggetto da",
  objectRemovalCancelConfirmationHintCompletion: 'questa risorsa?',
  theKeyword: 'la',
};

/** Aura's table for Italian; undefined lets Filerobot use its own English. Filter names
 *  (Clarendon, Sepia…) are raw labels in Filerobot and stay English whatever this holds. */
export function filerobotTranslations(language: string): Readonly<Record<string, string>> | undefined {
  return language.toLowerCase().startsWith('it') ? FILEROBOT_IT : undefined;
}

// Scaleflex palette key → Aura token. The editor reads the tokens once, when it opens.
const PALETTE_TOKENS: Readonly<Record<string, string>> = {
  'accent-primary': '--color-accent',
  'accent-primary-hover': '--color-accent-pressed',
  'accent-primary-active': '--color-accent-pressed',
  'bg-primary': '--color-surface',
  'bg-secondary': '--color-surface-2',
  'bg-primary-active': '--color-accent-muted',
  'txt-primary': '--color-text',
  'txt-secondary': '--color-text-muted',
  'borders-primary': '--color-border',
  'borders-secondary': '--color-border-strong',
  'icons-secondary': '--color-text-muted',
  'link-primary': '--color-accent-text',
};

export function filerobotTheme(root: Element = document.documentElement): FilerobotTheme {
  const style = getComputedStyle(root);
  const palette: Record<string, string> = {};
  for (const [key, token] of Object.entries(PALETTE_TOKENS)) {
    const value = style.getPropertyValue(token).trim();
    if (value !== '') palette[key] = value;
  }
  const font = style.getPropertyValue('--font-sans').trim();
  return {
    palette,
    ...(font === '' ? {} : { typography: { fontFamily: font } }),
  } as FilerobotTheme;
}

/** styled-components 6 stopped filtering unknown props, and Filerobot's styled components pass
 *  theirs (showTabsDrawer, isPhoneScreen, active…) straight to the DOM: about 35 React warnings
 *  per mount in spike 105. This restores the v5 filter for DOM targets only. */
export function forwardDomProp(prop: string, target: unknown): boolean {
  return typeof target === 'string' ? isPropValid(prop) : true;
}
```

- [ ] **Step 5: Run it to verify it passes**

Run: `npx vitest run src/mediaEdit/__tests__/filerobotSetup.test.ts`
Expected: PASS. If the key test fails, the diff names the missing or extra keys — fix the table, never the test.

- [ ] **Step 6: Commit**

```bash
git add web/vitest.config.ts web/src/mediaEdit/filerobotSetup.ts web/src/mediaEdit/filerobot-translations.d.ts web/src/mediaEdit/__tests__/filerobotSetup.test.ts
git commit -m "feat(media-edit): give Filerobot Aura's Italian, colours and prop filter" -m "Filerobot's own translations come from Scaleflex's servers; Aura ships its own table, checked key for key against the library, so nothing leaves the origin." -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 4: Strings, the editor layer, object URLs, downloads

**Files:**
- Create: `web/src/i18n/resources.mediaEdit.ts`, `web/src/mediaEdit/MediaEditorLayer.tsx`, `web/src/mediaEdit/useObjectUrl.ts`, `web/src/mediaEdit/download.ts`
- Modify: `web/src/i18n/resources.ts` (import + spread in both locales)
- Test: `web/src/mediaEdit/__tests__/MediaEditorLayer.test.tsx`, `web/src/mediaEdit/__tests__/download.test.ts`

**Interfaces:**
- Produces: `mediaEditEn`, `mediaEditIt` (keys under `mediaEdit.*`, listed below);
  `MediaEditorLayer({ label: string; onEscape: () => void; children: ReactNode })`;
  `useObjectUrl(blob: Blob): string`; `downloadBlob(blob: Blob, fileName: string): void`.

- [x] **Step 1: Add the strings**

`web/src/i18n/resources.mediaEdit.ts`:
```ts
// resources.mediaEdit.ts — every string of the photo and video editors Aura draws around
// Filerobot and Mediabunny. Filerobot's own labels live in mediaEdit/filerobotSetup.ts.

export const mediaEditEn = {
  mediaEdit: {
    edit: 'Edit',
    editName: 'Edit {{name}}',
    title: 'Media editor',
    close: 'Close',
    loading: 'Opening the file…',
    loadFailed: 'The file could not be opened.',
    notEditable: 'This format cannot be edited here.',
    large: {
      body: 'This video is {{size}}. The editor keeps it in memory while you work. Open it anyway?',
      confirm: 'Open anyway',
    },
    suffix: { image: 'edited', video: 'edited' },
    photo: {
      download: 'Download',
      saveLibrary: 'Save to library',
      saving: 'Saving… {{percent}}%',
      saved: 'Saved to the Studio library',
      studioOff: 'The Studio is not active: download the photo instead.',
      failed: 'Saving failed: {{reason}}',
      retry: 'Retry',
      discardTitle: 'Discard your changes?',
      discardBody: 'The edits you have not saved will be lost.',
      discard: 'Discard',
      keep: 'Keep editing',
    },
    video: {
      tools: 'Tools',
      tool: { trim: 'Trim', crop: 'Crop', rotate: 'Rotate', audio: 'Audio' },
      start: 'Start',
      end: 'End',
      startHandle: 'Start of the selection',
      endHandle: 'End of the selection',
      play: 'Play the selection',
      reset: 'Reset',
      presets: 'Crop format',
      original: 'Original',
      cropArea: 'Crop area: drag it or move it with the arrow keys',
      rotateLeft: 'Rotate 90° left',
      rotateRight: 'Rotate 90° right',
      mute: 'Remove audio',
      noAudio: 'This clip has no audio',
      size: '{{width}} × {{height}}',
      save: 'Save',
      saving: 'Saving… {{percent}}%',
      cancel: 'Cancel',
      trackVideo: 'video',
      trackAudio: 'audio',
      blocked: 'This browser cannot process the {{track}} track ({{codec}}). Try Chrome or Edge.',
      blockedUnknown: 'This browser cannot process this file. Try Chrome or Edge.',
      failed: 'The export failed: {{reason}}',
    },
  },
};

export const mediaEditIt = {
  mediaEdit: {
    edit: 'Modifica',
    editName: 'Modifica {{name}}',
    title: 'Editor media',
    close: 'Chiudi',
    loading: 'Apertura del file…',
    loadFailed: 'Non è stato possibile aprire il file.',
    notEditable: 'Questo formato non si può modificare qui.',
    large: {
      body: "Questo video pesa {{size}}. L'editor lo tiene in memoria mentre lavori. Aprirlo comunque?",
      confirm: 'Apri comunque',
    },
    suffix: { image: 'modificata', video: 'modificato' },
    photo: {
      download: 'Scarica',
      saveLibrary: 'Salva in libreria',
      saving: 'Salvataggio… {{percent}}%',
      saved: 'Salvata nella libreria dello Studio',
      studioOff: 'Lo Studio non è attivo: scarica la foto.',
      failed: 'Salvataggio non riuscito: {{reason}}',
      retry: 'Riprova',
      discardTitle: 'Scartare le modifiche?',
      discardBody: 'Le modifiche non salvate andranno perse.',
      discard: 'Scarta',
      keep: 'Continua a modificare',
    },
    video: {
      tools: 'Strumenti',
      tool: { trim: 'Taglia', crop: 'Ritaglia', rotate: 'Ruota', audio: 'Audio' },
      start: 'Inizio',
      end: 'Fine',
      startHandle: 'Inizio della selezione',
      endHandle: 'Fine della selezione',
      play: 'Riproduci la selezione',
      reset: 'Reimposta',
      presets: 'Formato del ritaglio',
      original: 'Originale',
      cropArea: "Area di ritaglio: trascinala o spostala con le frecce",
      rotateLeft: 'Ruota di 90° a sinistra',
      rotateRight: 'Ruota di 90° a destra',
      mute: 'Togli audio',
      noAudio: 'Questo video non ha audio',
      size: '{{width}} × {{height}}',
      save: 'Salva',
      saving: 'Salvataggio… {{percent}}%',
      cancel: 'Annulla',
      trackVideo: 'video',
      trackAudio: 'audio',
      blocked:
        'Questo browser non riesce a elaborare la traccia {{track}} ({{codec}}). Prova con Chrome o Edge.',
      blockedUnknown: 'Questo browser non riesce a elaborare questo file. Prova con Chrome o Edge.',
      failed: 'Esportazione non riuscita: {{reason}}',
    },
  },
};
```

In `web/src/i18n/resources.ts` add the import beside the others
`import { mediaEditEn, mediaEditIt } from './resources.mediaEdit';` and spread `...mediaEditEn,` right after `...studioEn,` and `...mediaEditIt,` right after `...studioIt,`.

- [x] **Step 2: Write the failing tests**

`web/src/mediaEdit/__tests__/MediaEditorLayer.test.tsx`:
```tsx
import { fireEvent, render, screen } from '@testing-library/react';
import { createPortal } from 'react-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { MediaEditorLayer } from '../MediaEditorLayer';

let root: HTMLDivElement;

beforeEach(() => {
  root = document.createElement('div');
  root.id = 'root';
  const opener = document.createElement('button');
  opener.textContent = 'opener';
  root.append(opener);
  document.body.append(root);
  opener.focus();
});

afterEach(() => {
  root.remove();
});

describe('MediaEditorLayer', () => {
  it('covers the page, makes it inert and gives focus back on close', () => {
    const { unmount } = render(
      <MediaEditorLayer label="Edit clip.mp4" onEscape={vi.fn()}>
        body
      </MediaEditorLayer>,
      { container: document.body.appendChild(document.createElement('div')) },
    );
    const layer = screen.getByRole('dialog', { name: 'Edit clip.mp4' });
    expect(layer.parentElement).toBe(document.body);
    expect(root.hasAttribute('inert')).toBe(true);
    expect(document.activeElement).toBe(layer);
    unmount();
    expect(root.hasAttribute('inert')).toBe(false);
    expect(document.activeElement?.textContent).toBe('opener');
  });

  it('closes on Escape inside the layer', () => {
    const onEscape = vi.fn();
    render(
      <MediaEditorLayer label="Edit" onEscape={onEscape}>
        <input aria-label="inside" />
      </MediaEditorLayer>,
    );
    fireEvent.keyDown(screen.getByLabelText('inside'), { key: 'Escape' });
    expect(onEscape).toHaveBeenCalledTimes(1);
  });

  it('ignores Escape pressed in a menu the editor portalled out of the layer', () => {
    const onEscape = vi.fn();
    render(
      <MediaEditorLayer label="Edit" onEscape={onEscape}>
        {createPortal(<input aria-label="portal menu" />, document.body)}
      </MediaEditorLayer>,
    );
    fireEvent.keyDown(screen.getByLabelText('portal menu'), { key: 'Escape' });
    expect(onEscape).not.toHaveBeenCalled();
  });
});
```

`web/src/mediaEdit/__tests__/download.test.ts`:
```ts
import { afterEach, describe, expect, it, vi } from 'vitest';
import { downloadBlob } from '../download';

afterEach(() => {
  vi.restoreAllMocks();
  vi.useRealTimers();
});

describe('downloadBlob', () => {
  it('clicks a download link and revokes its URL afterwards', () => {
    vi.useFakeTimers();
    const create = vi.fn(() => 'blob:edited');
    const revoke = vi.fn();
    // jsdom has no object URLs; assign them rather than replacing the URL class jsdom uses.
    Object.assign(URL, { createObjectURL: create, revokeObjectURL: revoke });
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});
    downloadBlob(new Blob(['x']), 'clip-edited.mp4');
    expect(click).toHaveBeenCalledTimes(1);
    const anchor = click.mock.contexts[0] as HTMLAnchorElement;
    expect(anchor.download).toBe('clip-edited.mp4');
    expect(anchor.href).toBe('blob:edited');
    expect(revoke).not.toHaveBeenCalled();
    vi.runAllTimers();
    expect(revoke).toHaveBeenCalledWith('blob:edited');
  });
});
```

- [x] **Step 3: Run them to verify they fail**

Run: `npx vitest run src/mediaEdit/__tests__/MediaEditorLayer.test.tsx src/mediaEdit/__tests__/download.test.ts`
Expected: FAIL — unresolved imports.

- [x] **Step 4: Implement**

`web/src/mediaEdit/MediaEditorLayer.tsx`:
```tsx
import { useEffect, useRef, type ReactNode } from 'react';
import { createPortal } from 'react-dom';

// MediaEditorLayer — the full-screen surface both editors draw in. It is deliberately not a
// Radix Dialog: Filerobot's menus, colour picker and modals portal to document.body, and a Radix
// focus trap with outside-dismiss treats each of them as "outside" and closes the editor on the
// first click. Instead the app root goes inert while the layer is open, which keeps pointer and
// keyboard off the page underneath without trapping the editor's own portals.

interface MediaEditorLayerProps {
  readonly label: string;
  readonly onEscape: () => void;
  readonly children: ReactNode;
}

export function MediaEditorLayer({ label, onEscape, children }: MediaEditorLayerProps) {
  const layerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const appRoot = document.getElementById('root');
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    appRoot?.setAttribute('inert', '');
    layerRef.current?.focus();
    return () => {
      appRoot?.removeAttribute('inert');
      opener?.focus();
    };
  }, []);

  return createPortal(
    <div
      ref={layerRef}
      role="dialog"
      aria-modal="true"
      aria-label={label}
      tabIndex={-1}
      onKeyDown={(event) => {
        // React bubbles events through portals, so a key pressed in a Filerobot menu reaches this
        // handler too; only a key pressed in the layer's own DOM closes the editor.
        if (event.key !== 'Escape') return;
        if (!(event.target instanceof Node) || !layerRef.current?.contains(event.target)) return;
        event.stopPropagation();
        onEscape();
      }}
      className="fixed inset-0 z-50 flex flex-col bg-bg text-text outline-none"
    >
      {children}
    </div>,
    document.body,
  );
}
```

`web/src/mediaEdit/useObjectUrl.ts`:
```ts
import { useEffect, useMemo } from 'react';

/** An object URL for `blob`, revoked when the blob changes or the component unmounts. */
export function useObjectUrl(blob: Blob): string {
  const url = useMemo(() => URL.createObjectURL(blob), [blob]);
  useEffect(
    () => () => {
      URL.revokeObjectURL(url);
    },
    [url],
  );
  return url;
}
```

`web/src/mediaEdit/download.ts`:
```ts
/** Hands `blob` to the browser as a download named `fileName`. The URL is revoked on the next
 *  task: revoking it synchronously can cancel the download in Firefox. */
export function downloadBlob(blob: Blob, fileName: string): void {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = fileName;
  anchor.click();
  setTimeout(() => {
    URL.revokeObjectURL(url);
  }, 0);
}
```

- [x] **Step 5: Run them to verify they pass, plus the i18n gates**

Run: `npx vitest run src/mediaEdit/__tests__ src/i18n/__tests__`
Expected: PASS, including `resources.parity.test.ts`.

- [x] **Step 6: Commit**

```bash
git add web/src/i18n/resources.mediaEdit.ts web/src/i18n/resources.ts web/src/mediaEdit/MediaEditorLayer.tsx web/src/mediaEdit/useObjectUrl.ts web/src/mediaEdit/download.ts web/src/mediaEdit/__tests__/MediaEditorLayer.test.tsx web/src/mediaEdit/__tests__/download.test.ts
git commit -m "feat(media-edit): add the editor layer, its strings and downloads" -m "A full-screen layer that keeps the page inert without trapping focus, because Filerobot's menus portal outside any dialog and a Radix trap would close the editor on the first click." -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 5: `videoMedia` — the Mediabunny adapter

**Files:**
- Create: `web/src/mediaEdit/videoMedia.ts`
- Test: `web/src/mediaEdit/__tests__/videoMedia.test.ts`

**Interfaces:**
- Consumes: `conversionOptions`, `blockingDiscards`, `videoContainer`, `VideoEdit`, `BlockedTrack` (Task 2).
- Produces: `interface VideoInfo { duration: number; width: number; height: number; hasAudio: boolean }`;
  `probeVideo(source: Blob): Promise<VideoInfo>`;
  `filmstrip(source: Blob, count: number, height: number): Promise<CanvasImageSource[]>`;
  `type ExportResult = { kind: 'done'; blob: Blob } | { kind: 'blocked'; tracks: readonly BlockedTrack[] } | { kind: 'canceled' }`;
  `exportVideo(source: Blob, mimeType: string, edit: VideoEdit, onProgress: (fraction: number) => void, signal: AbortSignal): Promise<ExportResult>`.

- [x] **Step 1: Write the failing test**

`web/src/mediaEdit/__tests__/videoMedia.test.ts`:
```ts
import { beforeEach, describe, expect, it, vi } from 'vitest';

// jsdom has no WebCodecs, so Mediabunny is replaced by a recorder. What is tested is Aura's glue:
// which options reach Conversion.init, what a discarded track does, how Cancel reaches the
// conversion, and what the caller gets back. The real library runs in the E2E suite.
const state = vi.hoisted(() => ({
  initOptions: undefined as unknown,
  discarded: [] as unknown[],
  valid: true,
  cancel: vi.fn(),
  executeGate: undefined as Promise<void> | undefined,
  disposed: 0,
  video: { displayWidth: 1280, displayHeight: 720, canDecode: async () => true } as unknown,
  audio: {} as unknown,
}));

vi.mock('mediabunny', () => {
  class Input {
    constructor(readonly options: unknown) {}
    getPrimaryVideoTrack = async () => state.video;
    getPrimaryAudioTrack = async () => state.audio;
    computeDuration = async () => 10;
    dispose = () => {
      state.disposed += 1;
    };
  }
  class Output {
    target: { buffer: ArrayBuffer | null };
    format: { mimeType: string };
    constructor(options: { format: { mimeType: string }; target: { buffer: ArrayBuffer | null } }) {
      this.target = options.target;
      this.format = options.format;
    }
  }
  class BufferTarget {
    buffer: ArrayBuffer | null = null;
  }
  class Mp4OutputFormat {
    mimeType = 'video/mp4';
  }
  class WebMOutputFormat {
    mimeType = 'video/webm';
  }
  class BlobSource {
    constructor(readonly blob: Blob) {}
  }
  class CanvasSink {
    async *canvasesAtTimestamps(stamps: number[]) {
      for (const timestamp of stamps) yield { canvas: { timestamp }, timestamp, duration: 0 };
    }
  }
  const Conversion = {
    init: async (options: { output: Output }) => {
      state.initOptions = options;
      return {
        isValid: state.valid,
        discardedTracks: state.discarded,
        onProgress: undefined as ((p: number) => void) | undefined,
        cancel: state.cancel,
        async execute(this: { onProgress?: (p: number) => void }) {
          this.onProgress?.(0.5);
          if (state.executeGate) await state.executeGate;
          options.output.target.buffer = new ArrayBuffer(8);
        },
      };
    },
  };
  return { ALL_FORMATS: [], BlobSource, BufferTarget, CanvasSink, Conversion, Input, Mp4OutputFormat, Output, WebMOutputFormat };
});

const { exportVideo, filmstrip, probeVideo } = await import('../videoMedia');

beforeEach(() => {
  state.discarded = [];
  state.valid = true;
  state.executeGate = undefined;
  state.cancel.mockReset();
  state.disposed = 0;
  state.audio = {};
});

const EDIT = { start: 1, end: 3, rotation: 0 as const, mute: false };

describe('probeVideo', () => {
  it('reports duration, display size and audio, and releases the file', async () => {
    await expect(probeVideo(new Blob())).resolves.toEqual({
      duration: 10,
      width: 1280,
      height: 720,
      hasAudio: true,
    });
    state.audio = null;
    await expect(probeVideo(new Blob())).resolves.toMatchObject({ hasAudio: false });
    expect(state.disposed).toBe(2);
  });
});

describe('filmstrip', () => {
  it('samples the middle of each slot', async () => {
    const frames = await filmstrip(new Blob(), 4, 90);
    expect(frames.map((f) => (f as unknown as { timestamp: number }).timestamp)).toEqual([
      1.25, 3.75, 6.25, 8.75,
    ]);
  });
});

describe('exportVideo', () => {
  it('passes the cut to Mediabunny and returns the file with its type', async () => {
    const progress = vi.fn();
    const result = await exportVideo(new Blob(), 'video/mp4', EDIT, progress, new AbortController().signal);
    expect(result.kind).toBe('done');
    expect(result.kind === 'done' ? result.blob.type : '').toBe('video/mp4');
    expect(progress).toHaveBeenCalledWith(0.5);
    expect(state.initOptions).toMatchObject({
      trim: { start: 1, end: 3 },
      copy: { boundaryPolicy: 'expand' },
      showWarnings: false,
    });
  });

  it('writes WebM for a WebM source', async () => {
    const result = await exportVideo(new Blob(), 'video/webm', EDIT, vi.fn(), new AbortController().signal);
    expect(result.kind === 'done' ? result.blob.type : '').toBe('video/webm');
  });

  it('refuses a file the browser would strip of its video', async () => {
    state.discarded = [{ track: { type: 'video', codec: 'hevc' }, reason: 'undecodable_source_codec' }];
    await expect(
      exportVideo(new Blob(), 'video/mp4', EDIT, vi.fn(), new AbortController().signal),
    ).resolves.toEqual({
      kind: 'blocked',
      tracks: [{ type: 'video', codec: 'hevc', reason: 'undecodable_source_codec' }],
    });
  });

  it('cancels the running conversion when the signal aborts', async () => {
    let release: () => void = () => {};
    state.executeGate = new Promise<void>((resolve) => {
      release = resolve;
    });
    const controller = new AbortController();
    const running = exportVideo(new Blob(), 'video/mp4', EDIT, vi.fn(), controller.signal);
    await Promise.resolve();
    await Promise.resolve();
    controller.abort();
    release();
    await expect(running).resolves.toEqual({ kind: 'canceled' });
    expect(state.cancel).toHaveBeenCalledTimes(1);
  });
});
```

- [x] **Step 2: Run it to verify it fails**

Run: `npx vitest run src/mediaEdit/__tests__/videoMedia.test.ts`
Expected: FAIL — `Failed to resolve import "../videoMedia"`.

- [x] **Step 3: Implement**

`web/src/mediaEdit/videoMedia.ts`:
```ts
import {
  ALL_FORMATS,
  BlobSource,
  BufferTarget,
  CanvasSink,
  Conversion,
  Input,
  Mp4OutputFormat,
  Output,
  WebMOutputFormat,
} from 'mediabunny';
import {
  blockingDiscards,
  conversionOptions,
  videoContainer,
  type BlockedTrack,
  type VideoEdit,
} from './editRules';

// videoMedia.ts — the only file that runs Mediabunny. Source and output both live in memory
// (BlobSource + BufferTarget), which is why the editor asks before opening a very large clip.

export interface VideoInfo {
  readonly duration: number;
  /** Display size: the file's own rotation already applied. */
  readonly width: number;
  readonly height: number;
  readonly hasAudio: boolean;
}

export type ExportResult =
  | { readonly kind: 'done'; readonly blob: Blob }
  | { readonly kind: 'blocked'; readonly tracks: readonly BlockedTrack[] }
  | { readonly kind: 'canceled' };

function openInput(source: Blob): Input {
  return new Input({ source: new BlobSource(source), formats: ALL_FORMATS });
}

export async function probeVideo(source: Blob): Promise<VideoInfo> {
  const input = openInput(source);
  try {
    const video = await input.getPrimaryVideoTrack();
    if (video === null) throw new Error('the file has no video track');
    const audio = await input.getPrimaryAudioTrack();
    return {
      duration: await input.computeDuration(),
      width: video.displayWidth,
      height: video.displayHeight,
      hasAudio: audio !== null,
    };
  } finally {
    input.dispose();
  }
}

/** `count` frames `height` pixels tall, one from the middle of each equal slot of the clip. An
 *  undecodable track yields none: the timeline still works, without pictures. */
export async function filmstrip(source: Blob, count: number, height: number): Promise<CanvasImageSource[]> {
  const input = openInput(source);
  try {
    const video = await input.getPrimaryVideoTrack();
    if (video === null || !(await video.canDecode())) return [];
    const slot = (await input.computeDuration()) / count;
    const stamps = Array.from({ length: count }, (_, index) => (index + 0.5) * slot);
    const sink = new CanvasSink(video, { height });
    const frames: CanvasImageSource[] = [];
    for await (const wrapped of sink.canvasesAtTimestamps(stamps)) {
      if (wrapped !== null) frames.push(wrapped.canvas);
    }
    return frames;
  } finally {
    input.dispose();
  }
}

export async function exportVideo(
  source: Blob,
  mimeType: string,
  edit: VideoEdit,
  onProgress: (fraction: number) => void,
  signal: AbortSignal,
): Promise<ExportResult> {
  const input = openInput(source);
  const output = new Output({
    format:
      videoContainer(mimeType) === 'webm'
        ? new WebMOutputFormat()
        : new Mp4OutputFormat({ fastStart: 'in-memory' }),
    target: new BufferTarget(),
  });
  try {
    const conversion = await Conversion.init({
      input,
      output,
      ...conversionOptions(edit),
      showWarnings: false,
    });
    const blocked = blockingDiscards(conversion.discardedTracks);
    if (!conversion.isValid || blocked.length > 0) return { kind: 'blocked', tracks: blocked };
    conversion.onProgress = (fraction) => {
      onProgress(fraction);
    };
    const cancel = () => {
      void conversion.cancel();
    };
    signal.addEventListener('abort', cancel, { once: true });
    try {
      await conversion.execute();
    } finally {
      signal.removeEventListener('abort', cancel);
    }
    if (signal.aborted) return { kind: 'canceled' };
    const buffer = output.target.buffer;
    if (buffer === null) throw new Error('the export produced no bytes');
    return { kind: 'done', blob: new Blob([buffer], { type: output.format.mimeType }) };
  } catch (error) {
    if (signal.aborted) return { kind: 'canceled' };
    throw error;
  } finally {
    input.dispose();
  }
}
```

- [x] **Step 4: Run it to verify it passes**

Run: `npx vitest run src/mediaEdit/__tests__/videoMedia.test.ts`
Expected: PASS. Then `npx tsc --noEmit -p .` — if the typings of `Output`/`BufferTarget` make `output.target.buffer` unknown, annotate `const target = new BufferTarget();` and read `target.buffer`.

- [x] **Step 5: Commit**

```bash
git add web/src/mediaEdit/videoMedia.ts web/src/mediaEdit/__tests__/videoMedia.test.ts
git commit -m "feat(media-edit): run clip probing, thumbnails and export through Mediabunny" -m "One adapter owns the library: it reports what a clip is, draws the filmstrip, and exports a cut that can be cancelled and never ships a file missing a track." -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 6: `VideoTimeline` and `TimeField`

**Files:**
- Create: `web/src/mediaEdit/VideoTimeline.tsx`, `web/src/mediaEdit/TimeField.tsx`
- Test: `web/src/mediaEdit/__tests__/VideoTimeline.test.tsx`

**Interfaces:**
- Consumes: `formatTimecode`, `parseTimecode` (Task 1).
- Produces: `MIN_SPAN = 0.1`;
  `VideoTimeline({ duration, start, end, frames: readonly CanvasImageSource[], onChange: (start: number, end: number) => void, startLabel: string, endLabel: string })`;
  `TimeField({ label: string; value: number; onCommit: (seconds: number) => void })` — mount with `key={formatTimecode(value)}` so an outside change resets the text.

- [x] **Step 1: Write the failing test**

`web/src/mediaEdit/__tests__/VideoTimeline.test.tsx`:
```tsx
import { fireEvent, render, screen } from '@testing-library/react';
import { beforeAll, describe, expect, it, vi } from 'vitest';
import { TimeField } from '../TimeField';
import { VideoTimeline } from '../VideoTimeline';

beforeAll(() => {
  // jsdom implements neither pointer capture nor layout.
  HTMLElement.prototype.setPointerCapture = vi.fn();
  HTMLElement.prototype.hasPointerCapture = vi.fn(() => true);
});

function mount(start = 2, end = 6) {
  const onChange = vi.fn();
  render(
    <VideoTimeline
      duration={10}
      start={start}
      end={end}
      frames={[]}
      onChange={onChange}
      startLabel="Start of the selection"
      endLabel="End of the selection"
    />,
  );
  vi.spyOn(screen.getByTestId('video-timeline'), 'getBoundingClientRect').mockReturnValue({
    left: 0,
    width: 100,
    top: 0,
    height: 56,
    right: 100,
    bottom: 56,
    x: 0,
    y: 0,
    toJSON: () => ({}),
  });
  return onChange;
}

describe('VideoTimeline', () => {
  it('exposes both handles as sliders with the time as text', () => {
    mount();
    const start = screen.getByRole('slider', { name: 'Start of the selection' });
    expect(start).toHaveAttribute('aria-valuenow', '2');
    expect(start).toHaveAttribute('aria-valuetext', '00:02.0');
    expect(screen.getByRole('slider', { name: 'End of the selection' })).toHaveAttribute('aria-valuemin', '2.1');
  });

  it('moves a handle a tenth per arrow and a second with Shift', () => {
    const onChange = mount();
    fireEvent.keyDown(screen.getByRole('slider', { name: 'Start of the selection' }), { key: 'ArrowRight' });
    expect(onChange).toHaveBeenLastCalledWith(2.1, 6);
    fireEvent.keyDown(screen.getByRole('slider', { name: 'End of the selection' }), { key: 'ArrowLeft', shiftKey: true });
    expect(onChange).toHaveBeenLastCalledWith(2, 5);
  });

  it('never lets the start reach the end', () => {
    const onChange = mount(5.95, 6);
    fireEvent.keyDown(screen.getByRole('slider', { name: 'Start of the selection' }), { key: 'ArrowRight', shiftKey: true });
    expect(onChange).toHaveBeenLastCalledWith(5.9, 6);
  });

  it('follows the pointer along the track', () => {
    const onChange = mount();
    const start = screen.getByRole('slider', { name: 'Start of the selection' });
    fireEvent.pointerDown(start, { pointerId: 1, clientX: 25 });
    expect(onChange).toHaveBeenLastCalledWith(2.5, 6);
    fireEvent.pointerMove(start, { pointerId: 1, clientX: 40 });
    expect(onChange).toHaveBeenLastCalledWith(4, 6);
  });
});

describe('TimeField', () => {
  it('commits a valid time on blur and restores an invalid one', () => {
    const onCommit = vi.fn();
    render(<TimeField label="Start" value={2} onCommit={onCommit} />);
    const input = screen.getByLabelText('Start');
    expect(input).toHaveValue('00:02.0');
    fireEvent.change(input, { target: { value: '00:03.5' } });
    fireEvent.blur(input);
    expect(onCommit).toHaveBeenCalledWith(3.5);
    fireEvent.change(input, { target: { value: 'soon' } });
    expect(input).toHaveAttribute('aria-invalid', 'true');
    fireEvent.blur(input);
    expect(input).toHaveValue('00:02.0');
  });
});
```

- [x] **Step 2: Run it to verify it fails**

Run: `npx vitest run src/mediaEdit/__tests__/VideoTimeline.test.tsx`
Expected: FAIL — unresolved imports.

- [x] **Step 3: Implement**

`web/src/mediaEdit/VideoTimeline.tsx`:
```tsx
import { useEffect, useRef, type KeyboardEvent, type PointerEvent } from 'react';
import { formatTimecode } from './timecode';

// VideoTimeline — the filmstrip with two handles (start, end), after Adobe Express and 123apps.
// The handles are sliders for assistive tech and the keyboard: arrows move a tenth of a second,
// Shift+arrows a whole second.

export const MIN_SPAN = 0.1;

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

function tenth(value: number): number {
  return Math.round(value * 10) / 10;
}

function Frame({ source }: { readonly source: CanvasImageSource }) {
  const ref = useRef<HTMLCanvasElement>(null);
  useEffect(() => {
    const canvas = ref.current;
    const context = canvas?.getContext('2d');
    if (!canvas || !context) return;
    context.drawImage(source, 0, 0, canvas.width, canvas.height);
  }, [source]);
  return <canvas ref={ref} width={160} height={90} aria-hidden="true" className="h-full min-w-0 flex-1" />;
}

interface HandleProps {
  readonly label: string;
  readonly value: number;
  readonly min: number;
  readonly max: number;
  readonly position: string;
  readonly onKeyDown: (event: KeyboardEvent<HTMLDivElement>) => void;
  readonly onPointerDown: (event: PointerEvent<HTMLDivElement>) => void;
  readonly onPointerMove: (event: PointerEvent<HTMLDivElement>) => void;
}

function Handle({ label, value, min, max, position, ...handlers }: HandleProps) {
  return (
    <div
      role="slider"
      tabIndex={0}
      aria-label={label}
      aria-valuemin={tenth(min)}
      aria-valuemax={tenth(max)}
      aria-valuenow={tenth(value)}
      aria-valuetext={formatTimecode(value)}
      style={{ left: position }}
      className="absolute inset-y-0 z-10 flex w-11 -translate-x-1/2 cursor-ew-resize touch-none items-center justify-center rounded-sm focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
      {...handlers}
    >
      <span aria-hidden="true" className="h-full w-2.5 rounded-sm bg-accent" />
    </div>
  );
}

interface VideoTimelineProps {
  readonly duration: number;
  readonly start: number;
  readonly end: number;
  readonly frames: readonly CanvasImageSource[];
  readonly onChange: (start: number, end: number) => void;
  readonly startLabel: string;
  readonly endLabel: string;
}

export function VideoTimeline({ duration, start, end, frames, onChange, startLabel, endLabel }: VideoTimelineProps) {
  const trackRef = useRef<HTMLDivElement>(null);
  const setStart = (value: number) => {
    onChange(clamp(tenth(value), 0, tenth(end - MIN_SPAN)), end);
  };
  const setEnd = (value: number) => {
    onChange(start, clamp(tenth(value), tenth(start + MIN_SPAN), duration));
  };

  function timeAt(clientX: number): number {
    const box = trackRef.current?.getBoundingClientRect();
    if (box === undefined || box.width === 0) return 0;
    return clamp((clientX - box.left) / box.width, 0, 1) * duration;
  }

  function keys(set: (value: number) => void, value: number) {
    return (event: KeyboardEvent<HTMLDivElement>) => {
      const step = event.shiftKey ? 1 : 0.1;
      const back = event.key === 'ArrowLeft' || event.key === 'ArrowDown';
      const forward = event.key === 'ArrowRight' || event.key === 'ArrowUp';
      if (!back && !forward) return;
      event.preventDefault();
      set(value + (back ? -step : step));
    };
  }

  function grab(set: (value: number) => void) {
    return (event: PointerEvent<HTMLDivElement>) => {
      event.currentTarget.setPointerCapture(event.pointerId);
      set(timeAt(event.clientX));
    };
  }

  function follow(set: (value: number) => void) {
    return (event: PointerEvent<HTMLDivElement>) => {
      if (event.currentTarget.hasPointerCapture(event.pointerId)) set(timeAt(event.clientX));
    };
  }

  const at = (value: number) => `${String((value / duration) * 100)}%`;
  const fromEnd = `${String(100 - (end / duration) * 100)}%`;

  return (
    <div
      ref={trackRef}
      data-testid="video-timeline"
      className="relative h-14 w-full touch-none overflow-hidden rounded-[var(--radius-md)] bg-surface-3 select-none"
    >
      <div className="flex h-full">
        {frames.map((frame, index) => (
          // Thumbnails are positional: the index is their identity.
          // eslint-disable-next-line react/no-array-index-key
          <Frame key={index} source={frame} />
        ))}
      </div>
      <div aria-hidden="true" className="absolute inset-y-0 left-0 bg-bg/70" style={{ width: at(start) }} />
      <div aria-hidden="true" className="absolute inset-y-0 right-0 bg-bg/70" style={{ width: fromEnd }} />
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-y-0 border-y-2 border-accent"
        style={{ left: at(start), right: fromEnd }}
      />
      <Handle
        label={startLabel}
        value={start}
        min={0}
        max={end - MIN_SPAN}
        position={at(start)}
        onKeyDown={keys(setStart, start)}
        onPointerDown={grab(setStart)}
        onPointerMove={follow(setStart)}
      />
      <Handle
        label={endLabel}
        value={end}
        min={start + MIN_SPAN}
        max={duration}
        position={at(end)}
        onKeyDown={keys(setEnd, end)}
        onPointerDown={grab(setEnd)}
        onPointerMove={follow(setEnd)}
      />
    </div>
  );
}
```
If oxlint does not know `react/no-array-index-key`, remove the disable comment (oxlint reports unused disables).

`web/src/mediaEdit/TimeField.tsx`:
```tsx
import { useId, useState } from 'react';
import { formatTimecode, parseTimecode } from './timecode';
import { Input } from '@/components/ui/input';

// The Start/End field. It keeps its own text while the operator types and commits on blur or
// Enter; a caller that changes the value from outside remounts it with key={formatTimecode(v)}.
export function TimeField({
  label,
  value,
  onCommit,
}: {
  readonly label: string;
  readonly value: number;
  readonly onCommit: (seconds: number) => void;
}) {
  const id = useId();
  const [text, setText] = useState(formatTimecode(value));
  const parsed = parseTimecode(text);
  return (
    <div className="flex flex-col gap-1 text-xs text-text-muted">
      <label htmlFor={id}>{label}</label>
      <Input
        id={id}
        value={text}
        inputMode="decimal"
        aria-invalid={parsed === undefined}
        onChange={(event) => {
          setText(event.target.value);
        }}
        onKeyDown={(event) => {
          if (event.key === 'Enter') event.currentTarget.blur();
        }}
        onBlur={() => {
          if (parsed === undefined) setText(formatTimecode(value));
          else onCommit(parsed);
        }}
        className="w-24 font-mono tabular-nums"
      />
    </div>
  );
}
```

- [x] **Step 4: Run it to verify it passes**

Run: `npx vitest run src/mediaEdit/__tests__/VideoTimeline.test.tsx`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add web/src/mediaEdit/VideoTimeline.tsx web/src/mediaEdit/TimeField.tsx web/src/mediaEdit/__tests__/VideoTimeline.test.tsx
git commit -m "feat(media-edit): add the trim timeline and time fields" -m "A filmstrip with two handles that work by pointer and by keyboard, and fields that read and write the same mm:ss.s the handles announce." -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 7: `CropOverlay` and `VideoEditor`

**Files:**
- Create: `web/src/mediaEdit/CropOverlay.tsx`, `web/src/mediaEdit/VideoEditor.tsx`
- Test: `web/src/mediaEdit/__tests__/VideoEditor.test.tsx`

**Interfaces:**
- Consumes: Tasks 1–6 (`presetRect`, `rotateSize`, `moveRect`, `CROP_PRESETS`, `editedName`, `videoContainer`, `VideoEdit`, `MediaEditorLayer`, `useObjectUrl`, `downloadBlob`, `probeVideo`, `filmstrip`, `exportVideo`, `VideoTimeline`, `TimeField`, `MIN_SPAN`); `Asset` from `web/src/chat/attachments/types.ts`.
- Produces: `interface EditorProps { asset: Asset; source: Blob; onClose: () => void }` (exported from `VideoEditor.tsx`, reused by `PhotoEditor.tsx`); default export `VideoEditor(props: EditorProps)`;
  `CropOverlay({ frame: Size; rect: CropRect; onMove: (rect: CropRect) => void; label: string })`.

- [x] **Step 1: Write the failing test**

`web/src/mediaEdit/__tests__/VideoEditor.test.tsx`:
```tsx
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import type { Asset } from '../../chat/attachments/types';

const media = vi.hoisted(() => ({
  probeVideo: vi.fn(),
  filmstrip: vi.fn(async () => []),
  exportVideo: vi.fn(),
}));
vi.mock('../videoMedia', () => media);
const downloadBlob = vi.hoisted(() => vi.fn());
vi.mock('../download', () => ({ downloadBlob }));

const { default: VideoEditor } = await import('../VideoEditor');

const ASSET: Asset = {
  id: 'asset-clip',
  status: 'complete',
  modality: 'video',
  file_name: 'clip.mp4',
  mime_type: 'video/mp4',
  declared_size_bytes: 1000,
  size_bytes: 1000,
};

beforeEach(() => {
  Object.assign(URL, { createObjectURL: vi.fn(() => 'blob:source'), revokeObjectURL: vi.fn() });
  media.probeVideo.mockResolvedValue({ duration: 10, width: 1280, height: 720, hasAudio: true });
  media.exportVideo.mockResolvedValue({ kind: 'done', blob: new Blob(['x'], { type: 'video/mp4' }) });
  downloadBlob.mockReset();
});

async function mount() {
  render(<VideoEditor asset={ASSET} source={new Blob()} onClose={vi.fn()} />);
  await screen.findByRole('slider', { name: 'Start of the selection' });
}

describe('VideoEditor', () => {
  it('saves the whole clip untouched by default and downloads it', async () => {
    await mount();
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(downloadBlob).toHaveBeenCalledWith(expect.any(Blob), 'clip-edited.mp4');
    });
    expect(media.exportVideo).toHaveBeenCalledWith(
      expect.any(Blob),
      'video/mp4',
      { start: 0, end: 10, rotation: 0, mute: false },
      expect.any(Function),
      expect.any(AbortSignal),
    );
  });

  it('sends the typed range', async () => {
    await mount();
    const start = screen.getByLabelText('Start');
    fireEvent.change(start, { target: { value: '00:02.5' } });
    fireEvent.blur(start);
    const end = screen.getByLabelText('End');
    fireEvent.change(end, { target: { value: '6' } });
    fireEvent.blur(end);
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(media.exportVideo.mock.calls[0]?.[2]).toMatchObject({ start: 2.5, end: 6 });
    });
  });

  it('crops to a centred square in the rotated frame', async () => {
    await mount();
    fireEvent.click(screen.getByRole('radio', { name: 'Crop' }));
    fireEvent.click(screen.getByRole('button', { name: '1:1' }));
    expect(screen.getByText('720 × 720')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('radio', { name: 'Rotate' }));
    fireEvent.click(screen.getByRole('button', { name: 'Rotate 90° right' }));
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(media.exportVideo.mock.calls[0]?.[2]).toMatchObject({
        rotation: 90,
        crop: { left: 0, top: 280, width: 720, height: 720 },
      });
    });
  });

  it('turns left from zero to 270', async () => {
    await mount();
    fireEvent.click(screen.getByRole('radio', { name: 'Rotate' }));
    fireEvent.click(screen.getByRole('button', { name: 'Rotate 90° left' }));
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(media.exportVideo.mock.calls[0]?.[2]).toMatchObject({ rotation: 270 });
    });
  });

  it('cannot mute a silent clip', async () => {
    media.probeVideo.mockResolvedValue({ duration: 10, width: 1280, height: 720, hasAudio: false });
    await mount();
    fireEvent.click(screen.getByRole('radio', { name: 'Audio' }));
    expect(screen.getByRole('switch', { name: 'Remove audio' })).toBeDisabled();
    expect(screen.getByText('This clip has no audio')).toBeInTheDocument();
  });

  it('names the track the browser cannot process and downloads nothing', async () => {
    media.exportVideo.mockResolvedValue({
      kind: 'blocked',
      tracks: [{ type: 'video', codec: 'hevc', reason: 'undecodable_source_codec' }],
    });
    await mount();
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'This browser cannot process the video track (hevc). Try Chrome or Edge.',
    );
    expect(downloadBlob).not.toHaveBeenCalled();
  });

  it('cancels a running export', async () => {
    let signal: AbortSignal | undefined;
    media.exportVideo.mockImplementation(
      (_s: Blob, _m: string, _e: unknown, _p: unknown, abort: AbortSignal) =>
        new Promise((resolve) => {
          signal = abort;
          abort.addEventListener('abort', () => {
            resolve({ kind: 'canceled' });
          });
        }),
    );
    await mount();
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Cancel' }));
    expect(signal?.aborted).toBe(true);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled();
    });
    expect(downloadBlob).not.toHaveBeenCalled();
  });
});
```
Check the toggle-group item role in `web/src/components/ui/toggle-group.tsx` before running: Radix `ToggleGroup type="single"` items are `role="radio"`. If the project's wrapper renders them otherwise, use that role in the test.

- [x] **Step 2: Run it to verify it fails**

Run: `npx vitest run src/mediaEdit/__tests__/VideoEditor.test.tsx`
Expected: FAIL — `Failed to resolve import "../VideoEditor"`.

- [x] **Step 3: Implement `CropOverlay`**

`web/src/mediaEdit/CropOverlay.tsx`:
```tsx
import { useRef, type KeyboardEvent, type PointerEvent } from 'react';
import { moveRect, type CropRect, type Size } from './cropMath';

// The crop box drawn over the preview. It moves, it does not resize (the presets set the size).
// Positions are percentages of the frame, so the box follows the preview at any size; a drag is
// converted back to frame pixels through the parent's rendered width.

function percent(part: number, whole: number): string {
  return `${String((part / whole) * 100)}%`;
}

export function CropOverlay({
  frame,
  rect,
  onMove,
  label,
}: {
  readonly frame: Size;
  readonly rect: CropRect;
  readonly onMove: (rect: CropRect) => void;
  readonly label: string;
}) {
  const last = useRef<{ readonly x: number; readonly y: number } | undefined>(undefined);

  function drag(event: PointerEvent<HTMLDivElement>) {
    const from = last.current;
    const box = event.currentTarget.parentElement?.getBoundingClientRect();
    if (from === undefined || box === undefined || box.width === 0 || box.height === 0) return;
    last.current = { x: event.clientX, y: event.clientY };
    onMove(
      moveRect(
        rect,
        ((event.clientX - from.x) * frame.width) / box.width,
        ((event.clientY - from.y) * frame.height) / box.height,
        frame,
      ),
    );
  }

  function keys(event: KeyboardEvent<HTMLDivElement>) {
    const step = event.shiftKey ? 50 : 10;
    const moves: Readonly<Record<string, readonly [number, number]>> = {
      ArrowLeft: [-step, 0],
      ArrowRight: [step, 0],
      ArrowUp: [0, -step],
      ArrowDown: [0, step],
    };
    const move = moves[event.key];
    if (move === undefined) return;
    event.preventDefault();
    onMove(moveRect(rect, move[0], move[1], frame));
  }

  return (
    <div
      role="group"
      tabIndex={0}
      aria-label={label}
      data-testid="crop-box"
      style={{
        left: percent(rect.left, frame.width),
        top: percent(rect.top, frame.height),
        width: percent(rect.width, frame.width),
        height: percent(rect.height, frame.height),
      }}
      className="absolute cursor-move touch-none border-2 border-accent shadow-[0_0_0_9999px_rgb(0_0_0/0.55)] focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
      onPointerDown={(event) => {
        event.currentTarget.setPointerCapture(event.pointerId);
        last.current = { x: event.clientX, y: event.clientY };
      }}
      onPointerMove={drag}
      onPointerUp={() => {
        last.current = undefined;
      }}
      onKeyDown={keys}
    />
  );
}
```

- [x] **Step 4: Implement `VideoEditor`**

`web/src/mediaEdit/VideoEditor.tsx`:
```tsx
import { RotateCcw, RotateCw, Play, X } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { Asset } from '../chat/attachments/types';
import { CropOverlay } from './CropOverlay';
import { CROP_PRESETS, presetRect, rotateSize, type CropPreset, type CropRect, type Rotation } from './cropMath';
import { downloadBlob } from './download';
import { editedName, videoContainer, type BlockedTrack, type VideoEdit } from './editRules';
import { MediaEditorLayer } from './MediaEditorLayer';
import { TimeField } from './TimeField';
import { formatTimecode } from './timecode';
import { useObjectUrl } from './useObjectUrl';
import { exportVideo, filmstrip, probeVideo, type VideoInfo } from './videoMedia';
import { MIN_SPAN, VideoTimeline } from './VideoTimeline';
import { Button } from '@/components/ui/button';
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select';
import { Switch } from '@/components/ui/switch';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';

// VideoEditor — trim, crop, rotate and mute a clip in the browser, after the layout of Adobe
// Express's and 123apps' trimmers: preview on top, filmstrip below, Start/End and Save last.

export interface EditorProps {
  readonly asset: Asset;
  readonly source: Blob;
  readonly onClose: () => void;
}

type Tool = 'trim' | 'crop' | 'rotate' | 'audio';
const TOOLS: readonly Tool[] = ['trim', 'crop', 'rotate', 'audio'];
const FILMSTRIP_FRAMES = 10;

function turn(rotation: Rotation, quarter: 1 | -1): Rotation {
  return (((rotation + quarter * 90) % 360) + 360) % 360 as Rotation;
}

export default function VideoEditor({ asset, source, onClose }: EditorProps) {
  const { t } = useTranslation();
  const url = useObjectUrl(source);
  const videoRef = useRef<HTMLVideoElement>(null);
  const abortRef = useRef<AbortController | undefined>(undefined);
  const [info, setInfo] = useState<VideoInfo>();
  const [frames, setFrames] = useState<CanvasImageSource[]>([]);
  const [tool, setTool] = useState<Tool>('trim');
  const [range, setRange] = useState({ start: 0, end: 0 });
  const [rotation, setRotation] = useState<Rotation>(0);
  const [preset, setPreset] = useState<CropPreset>('original');
  const [moved, setMoved] = useState<CropRect>();
  const [mute, setMute] = useState(false);
  const [progress, setProgress] = useState<number>();
  const [problem, setProblem] = useState<string>();

  useEffect(() => {
    let live = true;
    void (async () => {
      try {
        const probed = await probeVideo(source);
        if (!live) return;
        setInfo(probed);
        setRange({ start: 0, end: probed.duration });
        const strip = await filmstrip(source, FILMSTRIP_FRAMES, 90);
        if (live) setFrames(strip);
      } catch (error) {
        if (live) setProblem(t('mediaEdit.video.failed', { reason: error instanceof Error ? error.message : String(error) }));
      }
    })();
    return () => {
      live = false;
    };
  }, [source, t]);

  const frame = info === undefined ? undefined : rotateSize(info, rotation);
  const crop = frame === undefined || preset === 'original' ? undefined : (moved ?? presetRect(frame, preset));
  const output = crop ?? (frame === undefined ? undefined : presetRect(frame, 'original'));

  function blockedSentence(tracks: readonly BlockedTrack[]): string {
    const first = tracks[0];
    if (first === undefined) return t('mediaEdit.video.blockedUnknown');
    const track = first.type === 'video' ? t('mediaEdit.video.trackVideo') : t('mediaEdit.video.trackAudio');
    return t('mediaEdit.video.blocked', { track, codec: first.codec });
  }

  async function save() {
    const controller = new AbortController();
    abortRef.current = controller;
    setProblem(undefined);
    setProgress(0);
    const base = { start: range.start, end: range.end, rotation, mute };
    const edit: VideoEdit = crop === undefined ? base : { ...base, crop };
    try {
      const result = await exportVideo(source, asset.mime_type, edit, setProgress, controller.signal);
      if (result.kind === 'blocked') setProblem(blockedSentence(result.tracks));
      if (result.kind === 'done') {
        downloadBlob(result.blob, editedName(asset.file_name, t('mediaEdit.suffix.video'), videoContainer(asset.mime_type)));
      }
    } catch (error) {
      setProblem(t('mediaEdit.video.failed', { reason: error instanceof Error ? error.message : String(error) }));
    } finally {
      abortRef.current = undefined;
      setProgress(undefined);
    }
  }

  function reset() {
    if (info !== undefined) setRange({ start: 0, end: info.duration });
    setRotation(0);
    setPreset('original');
    setMoved(undefined);
    setMute(false);
  }

  function playSelection() {
    const video = videoRef.current;
    if (video === null) return;
    video.currentTime = range.start;
    void video.play();
  }

  const quarter = rotation === 90 || rotation === 270;
  const toolLabel = (item: Tool) => t(`mediaEdit.video.tool.${item}`);

  return (
    <MediaEditorLayer label={t('mediaEdit.editName', { name: asset.file_name })} onEscape={onClose}>
      <header className="flex items-center gap-3 border-b border-border px-4 py-2">
        <h2 className="min-w-0 flex-1 truncate font-mono text-sm">{asset.file_name}</h2>
        <Button type="button" variant="ghost" size="sm" onClick={reset}>
          {t('mediaEdit.video.reset')}
        </Button>
        <Button type="button" variant="ghost" size="sm" aria-label={t('mediaEdit.close')} onClick={onClose}>
          <X aria-hidden="true" className="size-4" />
        </Button>
      </header>

      <div className="flex items-center gap-2 px-4 py-2">
        <ToggleGroup
          type="single"
          value={tool}
          onValueChange={(value) => {
            if (value !== '') setTool(value as Tool);
          }}
          aria-label={t('mediaEdit.video.tools')}
          className="hidden sm:flex"
        >
          {TOOLS.map((item) => (
            <ToggleGroupItem key={item} value={item}>
              {toolLabel(item)}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
        <NativeSelect
          aria-label={t('mediaEdit.video.tools')}
          value={tool}
          onChange={(event) => {
            setTool(event.target.value as Tool);
          }}
          className="sm:hidden"
        >
          {TOOLS.map((item) => (
            <NativeSelectOption key={item} value={item}>
              {toolLabel(item)}
            </NativeSelectOption>
          ))}
        </NativeSelect>
      </div>

      <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 overflow-auto px-4">
        {frame === undefined ? (
          <p role="status" className="text-sm text-text-muted">
            {t('mediaEdit.loading')}
          </p>
        ) : (
          <div
            className="relative w-full overflow-hidden rounded-[var(--radius-md)] bg-black"
            style={{
              aspectRatio: `${String(frame.width)} / ${String(frame.height)}`,
              maxWidth: `min(100%, calc(52vh * ${String(frame.width / frame.height)}))`,
            }}
          >
            {/* eslint-disable-next-line jsx-a11y/media-has-caption -- the operator's own clip
                carries no captions and this element is a preview, not playback of content. */}
            <video
              ref={videoRef}
              src={url}
              muted={mute}
              playsInline
              onTimeUpdate={(event) => {
                if (event.currentTarget.currentTime >= range.end) event.currentTarget.pause();
              }}
              className="absolute top-1/2 left-1/2 max-w-none object-fill"
              style={{
                width: quarter ? `${String((frame.height / frame.width) * 100)}%` : '100%',
                height: quarter ? `${String((frame.width / frame.height) * 100)}%` : '100%',
                transform: `translate(-50%, -50%) rotate(${String(rotation)}deg)`,
              }}
            />
            {crop === undefined ? null : (
              <CropOverlay frame={frame} rect={crop} onMove={setMoved} label={t('mediaEdit.video.cropArea')} />
            )}
          </div>
        )}

        {tool === 'crop' && frame !== undefined ? (
          <div role="group" aria-label={t('mediaEdit.video.presets')} className="flex flex-wrap justify-center gap-1">
            {CROP_PRESETS.map((item) => (
              <Button
                key={item}
                type="button"
                size="sm"
                variant={preset === item ? 'default' : 'ghost'}
                aria-pressed={preset === item}
                onClick={() => {
                  setPreset(item);
                  setMoved(undefined);
                }}
              >
                {item === 'original' ? t('mediaEdit.video.original') : item}
              </Button>
            ))}
          </div>
        ) : null}

        {tool === 'rotate' ? (
          <div className="flex gap-2">
            <Button type="button" variant="ghost" size="sm" aria-label={t('mediaEdit.video.rotateLeft')} onClick={() => {
              setRotation(turn(rotation, -1));
              setMoved(undefined);
            }}>
              <RotateCcw aria-hidden="true" className="size-4" />
            </Button>
            <Button type="button" variant="ghost" size="sm" aria-label={t('mediaEdit.video.rotateRight')} onClick={() => {
              setRotation(turn(rotation, 1));
              setMoved(undefined);
            }}>
              <RotateCw aria-hidden="true" className="size-4" />
            </Button>
          </div>
        ) : null}

        {tool === 'audio' && info !== undefined ? (
          <div className="flex items-center gap-2 text-sm">
            <Switch
              id="media-edit-mute"
              checked={mute}
              disabled={!info.hasAudio}
              onCheckedChange={setMute}
              aria-label={t('mediaEdit.video.mute')}
            />
            <label htmlFor="media-edit-mute">{t('mediaEdit.video.mute')}</label>
            {info.hasAudio ? null : <span className="text-text-muted">{t('mediaEdit.video.noAudio')}</span>}
          </div>
        ) : null}
      </div>

      {info === undefined ? null : (
        <footer className="flex flex-col gap-3 border-t border-border px-4 py-3">
          <VideoTimeline
            duration={info.duration}
            start={range.start}
            end={range.end}
            frames={frames}
            onChange={(start, end) => {
              setRange({ start, end });
            }}
            startLabel={t('mediaEdit.video.startHandle')}
            endLabel={t('mediaEdit.video.endHandle')}
          />
          <div className="flex flex-wrap items-end gap-3">
            <Button type="button" variant="ghost" size="sm" aria-label={t('mediaEdit.video.play')} onClick={playSelection}>
              <Play aria-hidden="true" className="size-4" />
            </Button>
            <TimeField
              key={`start-${formatTimecode(range.start)}`}
              label={t('mediaEdit.video.start')}
              value={range.start}
              onCommit={(value) => {
                setRange({ start: Math.min(Math.max(0, value), range.end - MIN_SPAN), end: range.end });
              }}
            />
            <TimeField
              key={`end-${formatTimecode(range.end)}`}
              label={t('mediaEdit.video.end')}
              value={range.end}
              onCommit={(value) => {
                setRange({ start: range.start, end: Math.max(Math.min(info.duration, value), range.start + MIN_SPAN) });
              }}
            />
            {output === undefined ? null : (
              <span className="font-mono text-xs text-text-muted tabular-nums">
                {t('mediaEdit.video.size', { width: output.width, height: output.height })}
              </span>
            )}
            <div className="ms-auto flex items-center gap-2">
              {progress === undefined ? null : (
                <>
                  <span role="status" className="text-xs text-text-muted tabular-nums">
                    {t('mediaEdit.video.saving', { percent: Math.round(progress * 100) })}
                  </span>
                  <Button type="button" variant="ghost" size="sm" onClick={() => abortRef.current?.abort()}>
                    {t('mediaEdit.video.cancel')}
                  </Button>
                </>
              )}
              <Button type="button" size="sm" disabled={progress !== undefined} onClick={() => void save()}>
                {t('mediaEdit.video.save')}
              </Button>
            </div>
          </div>
          {problem === undefined ? null : (
            <p role="alert" className="text-sm text-danger">
              {problem}
            </p>
          )}
        </footer>
      )}
    </MediaEditorLayer>
  );
}
```
Notes for the implementer:
- `turn` must return a `Rotation`; the double modulo keeps −90 → 270.
- Keep the file under 600 lines; if prettier pushes it over, move the tool panels into `VideoTools.tsx`.
- If `toggle-group`'s `ToggleGroup` in this repo exposes a different prop API, adapt to it (read `web/src/components/ui/toggle-group.tsx`); the test addresses items by `role="radio"` and their label.

- [x] **Step 5: Run the test to verify it passes**

Run: `npx vitest run src/mediaEdit/__tests__/VideoEditor.test.tsx`
Expected: PASS.

- [x] **Step 6: Commit**

```bash
git add web/src/mediaEdit/CropOverlay.tsx web/src/mediaEdit/VideoEditor.tsx web/src/mediaEdit/__tests__/VideoEditor.test.tsx
git commit -m "feat(media-edit): add the clip editor" -m "Trim, crop, rotate and mute a clip in the browser, laid out after the professional web trimmers, with a cancellable export that refuses a file the browser would strip of a track." -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 8: `PhotoEditor`

**Files:**
- Create: `web/src/mediaEdit/PhotoEditor.tsx`
- Test: `web/src/mediaEdit/__tests__/PhotoEditor.test.tsx`

**Interfaces:**
- Consumes: `EditorProps` (Task 7), `editedBase`, `imageExtension` (Task 2), `FILEROBOT…` helpers (Task 3), `MediaEditorLayer`, `useObjectUrl`, `downloadBlob` (Task 4); `uploadStudioFrame` (`web/src/studio/frameUpload.ts`), `useStudioLibrary`, `studioKeys` (`web/src/studio/useStudio.ts`), `StudioError` (`web/src/studio/studioApi.ts`), `ConfirmDialog` (`web/src/components/ui/confirm-dialog.tsx`).
- Produces: default export `PhotoEditor(props: EditorProps)`.

- [x] **Step 1: Write the failing test**

`web/src/mediaEdit/__tests__/PhotoEditor.test.tsx`:
```tsx
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { useEffect } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import type { Asset } from '../../chat/attachments/types';
import { StudioError } from '../../studio/studioApi';

const PNG = 'data:image/png;base64,iVBORw0KGgo=';
const exportImage = vi.hoisted(() => vi.fn());
const seen = vi.hoisted(() => ({ props: undefined as Record<string, unknown> | undefined }));

vi.mock('react-filerobot-image-editor', () => ({
  TABS: { ADJUST: 'Adjust', FINETUNE: 'Finetune', FILTERS: 'Filters', ANNOTATE: 'Annotate', RESIZE: 'Resize' },
  TOOLS: { CROP: 'Crop' },
  default: function FakeFilerobot(props: {
    getCurrentImgDataFnRef: { current?: unknown };
    onModify: () => void;
  }) {
    seen.props = props as unknown as Record<string, unknown>;
    useEffect(() => {
      props.getCurrentImgDataFnRef.current = exportImage;
    });
    return (
      <button type="button" onClick={() => props.onModify()}>
        fake edit
      </button>
    );
  },
}));

const uploadStudioFrame = vi.hoisted(() => vi.fn());
vi.mock('../../studio/frameUpload', () => ({ uploadStudioFrame }));
const listStudioLibrary = vi.hoisted(() => vi.fn());
vi.mock('../../studio/studioApi', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../studio/studioApi')>()),
  listStudioLibrary,
}));
const downloadBlob = vi.hoisted(() => vi.fn());
vi.mock('../download', () => ({ downloadBlob }));

const { default: PhotoEditor } = await import('../PhotoEditor');

const ASSET: Asset = {
  id: 'asset-photo',
  status: 'complete',
  modality: 'image',
  file_name: 'beach.png',
  mime_type: 'image/png',
  declared_size_bytes: 10,
  size_bytes: 10,
};

function mount(onClose = vi.fn()) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <PhotoEditor asset={ASSET} source={new Blob()} onClose={onClose} />
    </QueryClientProvider>,
  );
  return onClose;
}

beforeEach(() => {
  Object.assign(URL, { createObjectURL: vi.fn(() => 'blob:photo'), revokeObjectURL: vi.fn() });
  exportImage.mockReset().mockReturnValue({ imageData: { imageBase64: PNG, mimeType: 'image/png' }, designState: {}, hideLoadingSpinner: vi.fn() });
  uploadStudioFrame.mockReset().mockResolvedValue({ id: 'lib-1', file_name: 'beach-edited.png', mime_type: 'image/png' });
  listStudioLibrary.mockReset().mockResolvedValue([]);
  downloadBlob.mockReset();
});

describe('PhotoEditor', () => {
  it('runs Filerobot offline, at natural size, without its own Save button', async () => {
    mount();
    await screen.findByText('fake edit');
    expect(seen.props).toMatchObject({
      useBackendTranslations: false,
      savingPixelRatio: 1,
      removeSaveButton: true,
      source: 'blob:photo',
    });
  });

  it('downloads the edited photo under the edited name', async () => {
    mount();
    fireEvent.click(await screen.findByRole('button', { name: 'Download' }));
    await waitFor(() => {
      expect(downloadBlob).toHaveBeenCalledWith(expect.any(Blob), 'beach-edited.png');
    });
    expect(exportImage).toHaveBeenCalledWith({ name: 'beach-edited', extension: 'png', quality: 0.92 }, 1);
  });

  it('saves to the Studio library and says so', async () => {
    mount();
    const save = await screen.findByRole('button', { name: 'Save to library' });
    await waitFor(() => {
      expect(save).toBeEnabled();
    });
    fireEvent.click(save);
    expect(await screen.findByText('Saved to the Studio library')).toBeInTheDocument();
    const file = uploadStudioFrame.mock.calls[0]?.[0] as File;
    expect(file.name).toBe('beach-edited.png');
  });

  it('disables Save to library when the Studio is not active', async () => {
    listStudioLibrary.mockRejectedValue(new StudioError(503, '', 'studio unavailable'));
    mount();
    expect(await screen.findByText('The Studio is not active: download the photo instead.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Save to library' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Download' })).toBeEnabled();
  });

  it('shows the server sentence on a failed upload and retries', async () => {
    uploadStudioFrame.mockRejectedValueOnce(new Error('image exceeds 26214400 bytes'));
    mount();
    const save = await screen.findByRole('button', { name: 'Save to library' });
    await waitFor(() => {
      expect(save).toBeEnabled();
    });
    fireEvent.click(save);
    expect(await screen.findByRole('alert')).toHaveTextContent('image exceeds 26214400 bytes');
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(await screen.findByText('Saved to the Studio library')).toBeInTheDocument();
  });

  it('asks before closing over unsaved edits', async () => {
    const onClose = mount();
    fireEvent.click(await screen.findByText('fake edit'));
    fireEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(onClose).not.toHaveBeenCalled();
    fireEvent.click(await screen.findByRole('button', { name: 'Discard' }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('closes at once when nothing changed', async () => {
    const onClose = mount();
    fireEvent.click(await screen.findByRole('button', { name: 'Close' }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
```

- [x] **Step 2: Run it to verify it fails**

Run: `npx vitest run src/mediaEdit/__tests__/PhotoEditor.test.tsx`
Expected: FAIL — `Failed to resolve import "../PhotoEditor"`.

- [x] **Step 3: Implement**

`web/src/mediaEdit/PhotoEditor.tsx`:
```tsx
import { useQueryClient } from '@tanstack/react-query';
import { Download, Library, X } from 'lucide-react';
import { useMemo, useRef, useState } from 'react';
import FilerobotImageEditor, { TABS, TOOLS, type getCurrentImgDataFunction } from 'react-filerobot-image-editor';
import { useTranslation } from 'react-i18next';
import { StyleSheetManager } from 'styled-components';
import { uploadStudioFrame } from '../studio/frameUpload';
import { StudioError } from '../studio/studioApi';
import { studioKeys, useStudioLibrary } from '../studio/useStudio';
import { downloadBlob } from './download';
import { editedBase, imageExtension } from './editRules';
import { filerobotTheme, filerobotTranslations, forwardDomProp } from './filerobotSetup';
import { MediaEditorLayer } from './MediaEditorLayer';
import { useObjectUrl } from './useObjectUrl';
import type { EditorProps } from './VideoEditor';
import { Button } from '@/components/ui/button';
import { ConfirmDialog } from '@/components/ui/confirm-dialog';

// PhotoEditor — Filerobot inside Aura's layer, with Aura's own header: Download and Save to
// library are both available from the start (spec, adversarial review finding 2), and the Studio's
// availability is read from the library query before anything is uploaded.

type SaveState =
  | { readonly kind: 'idle' }
  | { readonly kind: 'saving'; readonly progress: number }
  | { readonly kind: 'saved' }
  | { readonly kind: 'failed'; readonly reason: string };

const QUALITY = 0.92;

function reason(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export default function PhotoEditor({ asset, source, onClose }: EditorProps) {
  const { t, i18n } = useTranslation();
  const client = useQueryClient();
  const url = useObjectUrl(source);
  const exportRef = useRef<getCurrentImgDataFunction | undefined>(undefined);
  const theme = useMemo(() => filerobotTheme(), []);
  const library = useStudioLibrary(true);
  const [dirty, setDirty] = useState(false);
  const [state, setState] = useState<SaveState>({ kind: 'idle' });
  const [confirmClose, setConfirmClose] = useState(false);

  const studioOff = library.error instanceof StudioError && library.error.status === 503;
  const extension = imageExtension(asset.mime_type);
  const base = editedBase(asset.file_name, t('mediaEdit.suffix.image'));
  const fileName = `${base}.${extension}`;
  const translations = filerobotTranslations(i18n.language);

  async function render(): Promise<Blob> {
    const exportImage = exportRef.current;
    if (exportImage === undefined) throw new Error('the editor is not ready');
    const { imageData } = exportImage({ name: base, extension, quality: QUALITY }, 1);
    if (imageData.imageBase64 === undefined) throw new Error('the editor returned no image');
    return (await fetch(imageData.imageBase64)).blob();
  }

  async function download() {
    try {
      downloadBlob(await render(), fileName);
    } catch (error) {
      setState({ kind: 'failed', reason: reason(error) });
    }
  }

  async function saveToLibrary() {
    setState({ kind: 'saving', progress: 0 });
    try {
      const blob = await render();
      await uploadStudioFrame(new File([blob], fileName, { type: blob.type }), (progress) => {
        setState({ kind: 'saving', progress });
      });
      await client.invalidateQueries({ queryKey: studioKeys.library() });
      setDirty(false);
      setState({ kind: 'saved' });
    } catch (error) {
      setState({ kind: 'failed', reason: reason(error) });
    }
  }

  function requestClose() {
    if (dirty) setConfirmClose(true);
    else onClose();
  }

  const saving = state.kind === 'saving';

  return (
    <MediaEditorLayer label={t('mediaEdit.editName', { name: asset.file_name })} onEscape={requestClose}>
      <header className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-2">
        <h2 className="min-w-0 flex-1 truncate font-mono text-sm">{asset.file_name}</h2>
        <div role="status" className="text-xs text-text-muted tabular-nums">
          {state.kind === 'saving' ? t('mediaEdit.photo.saving', { percent: Math.round(state.progress * 100) }) : null}
          {state.kind === 'saved' ? t('mediaEdit.photo.saved') : null}
          {studioOff ? t('mediaEdit.photo.studioOff') : null}
        </div>
        <Button type="button" variant="ghost" size="sm" disabled={saving} onClick={() => void download()}>
          <Download aria-hidden="true" className="size-4" />
          {t('mediaEdit.photo.download')}
        </Button>
        <Button type="button" size="sm" disabled={saving || studioOff || library.isPending} onClick={() => void saveToLibrary()}>
          <Library aria-hidden="true" className="size-4" />
          {t('mediaEdit.photo.saveLibrary')}
        </Button>
        <Button type="button" variant="ghost" size="sm" aria-label={t('mediaEdit.close')} onClick={requestClose}>
          <X aria-hidden="true" className="size-4" />
        </Button>
      </header>
      {state.kind === 'failed' ? (
        <div role="alert" className="flex items-center gap-2 border-b border-danger/40 px-4 py-2 text-sm text-danger">
          {t('mediaEdit.photo.failed', { reason: state.reason })}
          <Button type="button" variant="ghost" size="sm" onClick={() => void saveToLibrary()}>
            {t('mediaEdit.photo.retry')}
          </Button>
        </div>
      ) : null}
      <div className="min-h-0 flex-1">
        <StyleSheetManager shouldForwardProp={forwardDomProp}>
          <FilerobotImageEditor
            source={url}
            savingPixelRatio={1}
            previewPixelRatio={window.devicePixelRatio || 1}
            useBackendTranslations={false}
            language={translations === undefined ? 'en' : 'it'}
            {...(translations === undefined ? {} : { translations })}
            theme={theme}
            tabsIds={[TABS.ADJUST, TABS.FINETUNE, TABS.FILTERS, TABS.ANNOTATE, TABS.RESIZE]}
            defaultTabId={TABS.ADJUST}
            defaultToolId={TOOLS.CROP}
            Rotate={{ angle: 90, componentType: 'buttons' }}
            removeSaveButton
            getCurrentImgDataFnRef={exportRef}
            onModify={() => {
              setDirty(true);
              if (state.kind === 'saved') setState({ kind: 'idle' });
            }}
          />
        </StyleSheetManager>
      </div>
      <ConfirmDialog
        open={confirmClose}
        onOpenChange={setConfirmClose}
        title={t('mediaEdit.photo.discardTitle')}
        description={t('mediaEdit.photo.discardBody')}
        cancelLabel={t('mediaEdit.photo.keep')}
        confirmLabel={t('mediaEdit.photo.discard')}
        confirmVariant="destructive"
        onConfirm={onClose}
      />
    </MediaEditorLayer>
  );
}
```
Notes for the implementer:
- `getCurrentImgDataFunction` is exported from the package's typings (`lib/index.d.ts`, `export type getCurrentImgDataFunction`). If the `getCurrentImgDataFnRef` prop rejects a `RefObject<… | undefined>`, pass `exportRef as { current?: getCurrentImgDataFunction }`.
- Check `confirmVariant` values in `web/src/components/ui/button.tsx` (`destructive` or `danger`) and use the one that exists.
- The `ConfirmDialog` is a Radix dialog portalled to `document.body`, outside `#root`, so the inert root does not block it.

- [x] **Step 4: Run it to verify it passes**

Run: `npx vitest run src/mediaEdit/__tests__/PhotoEditor.test.tsx`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add web/src/mediaEdit/PhotoEditor.tsx web/src/mediaEdit/__tests__/PhotoEditor.test.tsx
git commit -m "feat(media-edit): add the photo editor" -m "Filerobot inside Aura's layer with Aura's header: download at any time, save to the Studio library only when the Studio answers, and a single prompt before discarding edits." -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 9: The editor host, provider and button

**Files:**
- Create: `web/src/mediaEdit/mediaEditorContext.ts`, `web/src/mediaEdit/MediaEditorProvider.tsx`, `web/src/mediaEdit/MediaEditorHost.tsx`, `web/src/mediaEdit/EditMediaButton.tsx`
- Modify: `web/src/chat/artifacts/renderers/assetSourceContext.ts` (add `editable`), `web/src/AppShell.tsx` (mount the provider)
- Test: `web/src/mediaEdit/__tests__/EditMediaButton.test.tsx`, `web/src/mediaEdit/__tests__/MediaEditorHost.test.tsx`

**Interfaces:**
- Consumes: `editableKind`, `EditKind` (Task 2), `MediaEditorLayer` (Task 4), `PhotoEditor`, `VideoEditor` (default exports), `getAsset` (`web/src/chat/attachments/api.ts`), `useAssetContent` (`web/src/chat/artifacts/renderers/useAssetContent.ts`), `formatSize` (`web/src/chat/artifacts/artifactMeta.ts`).
- Produces: `interface EditTarget { assetId: string; kind: EditKind }`; `OpenEditorContext`; `useOpenEditor(): ((target: EditTarget) => void) | undefined`;
  `MediaEditorProvider({ children })`; default export `MediaEditorHost({ assetId, kind, onClose })`;
  `EditMediaButton({ assetId, kind, mimeType?, fileName?, compact?, className?, onOpen? })`;
  `AssetSource.editable?: true`.

- [x] **Step 1: Write the failing tests**

`web/src/mediaEdit/__tests__/EditMediaButton.test.tsx`:
```tsx
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import { AssetSourceContext, useAssetSource } from '../../chat/artifacts/renderers/assetSourceContext';
import { EditMediaButton } from '../EditMediaButton';
import { OpenEditorContext } from '../mediaEditorContext';

function Harness({ editable, children }: { editable: boolean; children: React.ReactNode }) {
  const source = useAssetSource();
  const { editable: _drop, ...rest } = source;
  return (
    <AssetSourceContext.Provider value={editable ? source : rest}>{children}</AssetSourceContext.Provider>
  );
}

function mount(node: React.ReactNode, { editable = true, open = vi.fn() } = {}) {
  render(
    <OpenEditorContext.Provider value={open}>
      <Harness editable={editable}>{node}</Harness>
    </OpenEditorContext.Provider>,
  );
  return open;
}

describe('EditMediaButton', () => {
  it('hands the asset to the editor and lets the surface close first', () => {
    const onOpen = vi.fn();
    const open = mount(<EditMediaButton assetId="a1" kind="video" fileName="clip.mp4" onOpen={onOpen} />);
    fireEvent.click(screen.getByRole('button', { name: 'Edit clip.mp4' }));
    expect(onOpen).toHaveBeenCalledTimes(1);
    expect(open).toHaveBeenCalledWith({ assetId: 'a1', kind: 'video' });
  });

  it('shows nothing on a source that is not editable', () => {
    mount(<EditMediaButton assetId="a1" kind="image" />, { editable: false });
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('shows nothing without an editor to open', () => {
    render(<EditMediaButton assetId="a1" kind="image" />);
    expect(screen.queryByRole('button')).toBeNull();
  });

  it.each(['image/gif', 'image/svg+xml', 'video/quicktime'])('shows nothing for %s', (mimeType) => {
    mount(<EditMediaButton assetId="a1" kind="image" mimeType={mimeType} />);
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('shows an icon-only button when compact', () => {
    mount(<EditMediaButton assetId="a1" kind="image" mimeType="image/png" compact />);
    expect(screen.getByRole('button', { name: 'Edit' })).not.toHaveTextContent('Edit');
  });
});
```

`web/src/mediaEdit/__tests__/MediaEditorHost.test.tsx`:
```tsx
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import type { Asset } from '../../chat/attachments/types';

const getAsset = vi.hoisted(() => vi.fn());
vi.mock('../../chat/attachments/api', () => ({ getAsset }));
vi.mock('../PhotoEditor', () => ({
  default: ({ asset, source }: { asset: Asset; source: Blob }) => (
    <p>photo {asset.file_name} {String(source.size)}</p>
  ),
}));
vi.mock('../VideoEditor', () => ({
  default: ({ asset }: { asset: Asset }) => <p>video {asset.file_name}</p>,
}));

const { default: MediaEditorHost } = await import('../MediaEditorHost');

function asset(over: Partial<Asset>): Asset {
  return {
    id: 'a1',
    status: 'complete',
    modality: 'image',
    file_name: 'beach.png',
    mime_type: 'image/png',
    declared_size_bytes: 3,
    size_bytes: 3,
    ...over,
  };
}

function mount(kind: 'image' | 'video' = 'image') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <MediaEditorHost assetId="a1" kind={kind} onClose={vi.fn()} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn(async () => new Response(new Blob(['abc']))));
});

describe('MediaEditorHost', () => {
  it('loads the bytes and opens the photo editor', async () => {
    getAsset.mockResolvedValue(asset({}));
    mount();
    expect(await screen.findByText('photo beach.png 3')).toBeInTheDocument();
  });

  it('opens the clip editor for a video', async () => {
    getAsset.mockResolvedValue(asset({ modality: 'video', file_name: 'clip.mp4', mime_type: 'video/mp4' }));
    mount('video');
    expect(await screen.findByText('video clip.mp4')).toBeInTheDocument();
  });

  it('says so when the format cannot be edited', async () => {
    getAsset.mockResolvedValue(asset({ mime_type: 'image/gif', file_name: 'loop.gif' }));
    mount();
    expect(await screen.findByText('This format cannot be edited here.')).toBeInTheDocument();
  });

  it('says so when the file cannot be read', async () => {
    getAsset.mockRejectedValue(new Error('HTTP 404'));
    mount();
    expect(await screen.findByText('The file could not be opened.')).toBeInTheDocument();
  });

  it('asks before loading a very large clip', async () => {
    getAsset.mockResolvedValue(
      asset({ modality: 'video', file_name: 'long.mp4', mime_type: 'video/mp4', size_bytes: 600 * 1024 * 1024 }),
    );
    mount('video');
    fireEvent.click(await screen.findByRole('button', { name: 'Open anyway' }));
    expect(await screen.findByText('video long.mp4')).toBeInTheDocument();
  });
});
```

- [x] **Step 2: Run them to verify they fail**

Run: `npx vitest run src/mediaEdit/__tests__/EditMediaButton.test.tsx src/mediaEdit/__tests__/MediaEditorHost.test.tsx`
Expected: FAIL — unresolved imports.

- [x] **Step 3: Add `editable` to the asset source**

In `web/src/chat/artifacts/renderers/assetSourceContext.ts`, add to `interface AssetSource` after `renderUrl?`:
```ts
  /** Present only on the identity-scoped tier: the in-browser editors (web/src/mediaEdit) may
   *  open this source's images and clips. A share tier leaves it out, so a public page never
   *  offers editing — the same optional-capability pattern as `renderUrl`. */
  readonly editable?: true;
```
and add `editable: true,` to `IDENTITY_SCOPED`.

- [x] **Step 4: Implement the context, provider, host and button**

`web/src/mediaEdit/mediaEditorContext.ts`:
```ts
import { createContext, useContext } from 'react';
import type { EditKind } from './editRules';

export interface EditTarget {
  readonly assetId: string;
  readonly kind: EditKind;
}

/** How a surface opens the editor. Undefined outside AppShell (the share pages), where the
 *  button therefore renders nothing. */
export const OpenEditorContext = createContext<((target: EditTarget) => void) | undefined>(undefined);

export function useOpenEditor(): ((target: EditTarget) => void) | undefined {
  return useContext(OpenEditorContext);
}
```

`web/src/mediaEdit/MediaEditorProvider.tsx`:
```tsx
import { lazy, Suspense, useState, type ReactNode } from 'react';
import { OpenEditorContext, type EditTarget } from './mediaEditorContext';

// The one open editor, owned at the shell. A surface inside a Radix modal (PreviewModal) closes
// the modal and hands the target here; an editor opened inside the modal would be caught by the
// modal's focus trap. The host is lazy, so Filerobot and Mediabunny load on the first click.
const MediaEditorHost = lazy(() => import('./MediaEditorHost'));

export function MediaEditorProvider({ children }: { readonly children: ReactNode }) {
  const [target, setTarget] = useState<EditTarget>();
  return (
    <OpenEditorContext.Provider value={setTarget}>
      {children}
      {target === undefined ? null : (
        <Suspense fallback={null}>
          <MediaEditorHost
            key={target.assetId}
            assetId={target.assetId}
            kind={target.kind}
            onClose={() => {
              setTarget(undefined);
            }}
          />
        </Suspense>
      )}
    </OpenEditorContext.Provider>
  );
}
```

`web/src/mediaEdit/MediaEditorHost.tsx`:
```tsx
import { useQuery } from '@tanstack/react-query';
import { X } from 'lucide-react';
import { lazy, Suspense, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { getAsset } from '../chat/attachments/api';
import type { Asset } from '../chat/attachments/types';
import { formatSize } from '../chat/artifacts/artifactMeta';
import { useAssetContent } from '../chat/artifacts/renderers/useAssetContent';
import { editableKind, type EditKind } from './editRules';
import { MediaEditorLayer } from './MediaEditorLayer';
import { Button } from '@/components/ui/button';

// MediaEditorHost — resolves what the asset is (the Studio stage does not know its MIME type or
// file name; getAsset does on every surface), loads its bytes, then picks the editor.

const PhotoEditor = lazy(() => import('./PhotoEditor'));
const VideoEditor = lazy(() => import('./VideoEditor'));

// Source and output both sit in memory while a clip is edited; past this size the operator is
// asked first. The server's own video limit already bounds what can exist at all.
const LARGE_VIDEO_BYTES = 500 * 1024 * 1024;

function Notice({ onClose, children }: { readonly onClose: () => void; readonly children: ReactNode }) {
  const { t } = useTranslation();
  return (
    <MediaEditorLayer label={t('mediaEdit.title')} onEscape={onClose}>
      <header className="flex justify-end border-b border-border px-4 py-2">
        <Button type="button" variant="ghost" size="sm" aria-label={t('mediaEdit.close')} onClick={onClose}>
          <X aria-hidden="true" className="size-4" />
        </Button>
      </header>
      <div role="status" className="flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center text-sm text-text-muted">
        {children}
      </div>
    </MediaEditorLayer>
  );
}

function LoadedEditor({ asset, onClose }: { readonly asset: Asset; readonly onClose: () => void }) {
  const { t } = useTranslation();
  const { data, error } = useAssetContent(asset.id, 'blob');
  if (error !== undefined) return <Notice onClose={onClose}>{t('mediaEdit.loadFailed')}</Notice>;
  if (data === undefined) return <Notice onClose={onClose}>{t('mediaEdit.loading')}</Notice>;
  const Editor = editableKind(asset.mime_type) === 'image' ? PhotoEditor : VideoEditor;
  return (
    <Suspense fallback={<Notice onClose={onClose}>{t('mediaEdit.loading')}</Notice>}>
      <Editor asset={asset} source={data} onClose={onClose} />
    </Suspense>
  );
}

export default function MediaEditorHost({
  assetId,
  kind,
  onClose,
}: {
  readonly assetId: string;
  readonly kind: EditKind;
  readonly onClose: () => void;
}) {
  const { t } = useTranslation();
  const [largeAccepted, setLargeAccepted] = useState(false);
  const asset = useQuery({
    queryKey: ['media-edit', 'asset', assetId],
    queryFn: () => getAsset(assetId),
    retry: false,
  });

  if (asset.isPending) return <Notice onClose={onClose}>{t('mediaEdit.loading')}</Notice>;
  if (asset.isError) return <Notice onClose={onClose}>{t('mediaEdit.loadFailed')}</Notice>;
  if (editableKind(asset.data.mime_type) !== kind) {
    return <Notice onClose={onClose}>{t('mediaEdit.notEditable')}</Notice>;
  }
  if (kind === 'video' && asset.data.size_bytes > LARGE_VIDEO_BYTES && !largeAccepted) {
    return (
      <Notice onClose={onClose}>
        <p>{t('mediaEdit.large.body', { size: formatSize(asset.data.size_bytes, t) })}</p>
        <Button
          type="button"
          size="sm"
          onClick={() => {
            setLargeAccepted(true);
          }}
        >
          {t('mediaEdit.large.confirm')}
        </Button>
      </Notice>
    );
  }
  return <LoadedEditor asset={asset.data} onClose={onClose} />;
}
```
Check `formatSize`'s signature in `web/src/chat/artifacts/artifactMeta.ts` (it is called as `formatSize(bytes, t)` in `LocalArtifactDisplay.tsx`).

`web/src/mediaEdit/EditMediaButton.tsx`:
```tsx
import { Pencil } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAssetSource } from '../chat/artifacts/renderers/assetSourceContext';
import { editableKind, type EditKind } from './editRules';
import { useOpenEditor } from './mediaEditorContext';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';

// The only thing a surface adds to offer editing. It renders nothing on a non-editable source
// (the share pages), outside the shell's provider, or for a type the editors do not take.

interface EditMediaButtonProps {
  readonly assetId: string;
  readonly kind: EditKind;
  /** When the surface knows it; the Studio stage does not, and the editor resolves it. */
  readonly mimeType?: string;
  readonly fileName?: string;
  readonly compact?: boolean;
  readonly className?: string;
  /** Runs before the editor opens: a modal closes itself here. */
  readonly onOpen?: () => void;
}

export function EditMediaButton({ assetId, kind, mimeType, fileName, compact = false, className, onOpen }: EditMediaButtonProps) {
  const { t } = useTranslation();
  const { editable } = useAssetSource();
  const open = useOpenEditor();
  if (editable !== true || open === undefined) return null;
  if (mimeType !== undefined && mimeType !== '' && editableKind(mimeType) !== kind) return null;
  const label = fileName === undefined ? t('mediaEdit.edit') : t('mediaEdit.editName', { name: fileName });
  return (
    <Button
      type="button"
      size="sm"
      variant="ghost"
      aria-label={label}
      data-required-touch-target
      onClick={() => {
        onOpen?.();
        open({ assetId, kind });
      }}
      className={cn('min-h-8 gap-1.5 py-1 text-xs', className)}
    >
      <Pencil aria-hidden="true" className="size-3.5" />
      {compact ? null : t('mediaEdit.edit')}
    </Button>
  );
}
```

In `web/src/AppShell.tsx`: import `import { MediaEditorProvider } from './mediaEdit/MediaEditorProvider';` and wrap the whole returned tree — make `<MediaEditorProvider>` the outermost element of the `return (…)` at line 391 (outside `WorkerWatchProvider`) and close it at the end. The file stays under 600 lines.

- [x] **Step 5: Run the tests to verify they pass**

Run: `npx vitest run src/mediaEdit src/chat/artifacts`
Expected: PASS.

- [x] **Step 6: Commit**

```bash
git add web/src/mediaEdit/mediaEditorContext.ts web/src/mediaEdit/MediaEditorProvider.tsx web/src/mediaEdit/MediaEditorHost.tsx web/src/mediaEdit/EditMediaButton.tsx web/src/mediaEdit/__tests__/EditMediaButton.test.tsx web/src/mediaEdit/__tests__/MediaEditorHost.test.tsx web/src/chat/artifacts/renderers/assetSourceContext.ts web/src/AppShell.tsx
git commit -m "feat(media-edit): open the editors from one shell-level host" -m "The shell owns the open editor so any surface, a modal included, can hand it a file; the asset source says who may edit, and the share pages never can." -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 10: Put the button on every surface

**Files:**
- Modify: `web/src/studio/StudioStage.tsx` (`StageActions`), `web/src/components/image.tsx` (`ImageActions` gains `extra`), `web/src/chat/artifacts/renderers/GeneratedImagePreview.tsx`, `web/src/chat/displays/LocalArtifactDisplay.tsx` (video figcaption), `web/src/chat/artifacts/PreviewModal.tsx` (header), `web/src/chat/attachments/AttachmentCard.tsx` (action row), `web/src/chat/attachments/types.ts` (`AssetModality` gains `'video'`)
- Test: extend `web/src/studio/__tests__/StudioStage.test.tsx` (or create it), `web/src/chat/attachments/__tests__/AttachmentCard.test.tsx`, `web/src/chat/artifacts/__tests__/PreviewModal.test.tsx`, `web/src/chat/displays/__tests__/LocalArtifactDisplay.test.tsx` — use the existing files under those `__tests__` folders if present.

**Interfaces:**
- Consumes: `EditMediaButton` (Task 9), `previewKind(mime, filename)` (`artifactMeta.ts`).
- Produces: `ImageActions` prop `extra?: ReactNode` rendered before the download link.

- [x] **Step 1: Write the failing tests**

For each surface add one test that renders it inside `OpenEditorContext.Provider value={open}` and asserts the button and the hand-off. Example for `AttachmentCard` (`web/src/chat/attachments/__tests__/AttachmentCard.test.tsx`; append to the existing file if it exists):
```tsx
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import '../../../i18n/i18n';
import { OpenEditorContext } from '../../../mediaEdit/mediaEditorContext';
import { AttachmentCard } from '../AttachmentCard';
import type { Asset } from '../types';

function card(over: Partial<Asset>) {
  const open = vi.fn();
  const asset: Asset = {
    id: 'att-1',
    status: 'complete',
    modality: 'video',
    file_name: 'phone.mp4',
    mime_type: 'video/mp4',
    declared_size_bytes: 1,
    size_bytes: 1,
    ...over,
  };
  render(
    <OpenEditorContext.Provider value={open}>
      <AttachmentCard asset={asset} />
    </OpenEditorContext.Provider>,
  );
  return open;
}

describe('AttachmentCard editing', () => {
  it('offers to edit a ready video attachment', () => {
    const open = card({});
    fireEvent.click(screen.getByRole('button', { name: 'Edit phone.mp4' }));
    expect(open).toHaveBeenCalledWith({ assetId: 'att-1', kind: 'video' });
  });

  it('offers to edit a ready image attachment', () => {
    card({ modality: 'image', file_name: 'shot.jpg', mime_type: 'image/jpeg' });
    expect(screen.getByRole('button', { name: 'Edit shot.jpg' })).toBeInTheDocument();
  });

  it('does not offer it while uploading or for a document', () => {
    card({ status: 'uploaded' });
    expect(screen.queryByRole('button', { name: /^Edit/ })).toBeNull();
  });
});
```
`StudioStage` (append to `web/src/studio/__tests__/StudioStage.test.tsx`, or create it with these imports):
```tsx
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import { OpenEditorContext } from '../../mediaEdit/mediaEditorContext';
import { StudioStage } from '../StudioStage';
import type { StudioRecord } from '../studioApi';

const COMPLETED: StudioRecord = {
  id: 'r1',
  kind: 'image',
  status: 'completed',
  model: 'test/model',
  prompt: 'a boat',
  used: {},
  asset_id: 'img-1',
  created_at: '2026-09-19T10:00:00Z',
};

function stage(record: StudioRecord) {
  const open = vi.fn();
  render(
    <OpenEditorContext.Provider value={open}>
      <StudioStage record={record} onReuse={vi.fn()} reuseState="ready" />
    </OpenEditorContext.Provider>,
  );
  return open;
}

describe('StudioStage editing', () => {
  it('offers Edit beside Download on a finished result', () => {
    const open = stage(COMPLETED);
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    expect(open).toHaveBeenCalledWith({ assetId: 'img-1', kind: 'image' });
  });

  it('offers nothing to edit on a failed generation', () => {
    const { asset_id: _none, ...failed } = COMPLETED;
    stage({ ...failed, status: 'failed', error: { code: 'provider_error', message: 'boom' } });
    expect(screen.queryByRole('button', { name: 'Edit' })).toBeNull();
  });
});
```

`PreviewModal` (append to its existing test file, reusing that file's renderer mocks):
```tsx
describe('PreviewModal editing', () => {
  const image: Asset = {
    id: 'p1',
    status: 'complete',
    modality: 'image',
    file_name: 'shot.png',
    mime_type: 'image/png',
    declared_size_bytes: 1,
    size_bytes: 1,
  };

  it('closes itself and hands the image to the editor', () => {
    const open = vi.fn();
    const onClose = vi.fn();
    render(
      <OpenEditorContext.Provider value={open}>
        <PreviewModal active={image} onClose={onClose} />
      </OpenEditorContext.Provider>,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Edit shot.png' }));
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(open).toHaveBeenCalledWith({ assetId: 'p1', kind: 'image' });
  });

  it('offers nothing to edit on a PDF', () => {
    render(
      <OpenEditorContext.Provider value={vi.fn()}>
        <PreviewModal active={{ ...image, file_name: 'spec.pdf', mime_type: 'application/pdf' }} onClose={vi.fn()} />
      </OpenEditorContext.Provider>,
    );
    expect(screen.queryByRole('button', { name: /^Edit/ })).toBeNull();
  });
});
```

`GeneratedImagePreview` (its existing test file already stubs `useBlobPreview`): render it inside `OpenEditorContext.Provider value={open}` with `fileName="beach.png"` and `mimeType="image/png"`, click `Edit beach.png`, expect `open` called with `{ assetId, kind: 'image' }`.

`LocalArtifactDisplay` (its existing test file): render a payload whose artifact is `{ asset_id: 'v1', filename: 'clip.mp4', mime_type: 'video/mp4' }` inside the provider, click `Edit clip.mp4`, expect `open` called with `{ assetId: 'v1', kind: 'video' }`.

- [x] **Step 2: Run them to verify they fail**

Run: `npx vitest run src/chat src/studio`
Expected: FAIL — no `Edit` buttons yet.

- [x] **Step 3: Implement the placements**

`web/src/chat/attachments/types.ts`: `export type AssetModality = 'document' | 'image' | 'audio' | 'video' | 'unknown';` and above it:
```ts
// 'video' is what the server assigns to an .mp4/.webm upload (internal/assets/limits.go
// InferModality) even when the browser hinted 'unknown'.
```

`web/src/chat/attachments/AttachmentCard.tsx`, inside the action `<div className="flex shrink-0 items-center gap-1">`, first child:
```tsx
          {(asset.modality === 'image' || asset.modality === 'video') && isReadyAsset(asset) ? (
            <EditMediaButton
              assetId={asset.id}
              kind={asset.modality}
              mimeType={asset.mime_type}
              fileName={asset.file_name}
              compact
              className="min-h-[44px] min-w-[44px] px-2 text-text-muted hover:bg-surface-3 hover:text-text"
            />
          ) : null}
```

`web/src/components/image.tsx` `ImageActions`: add `extra` to the props (`readonly extra?: ReactNode;`, import `type ReactNode`) and render `{extra}` right before the download `<a>`.

`web/src/chat/artifacts/renderers/GeneratedImagePreview.tsx`: pass
```tsx
          extra={
            <EditMediaButton assetId={assetId} kind="image" mimeType={mimeType} fileName={fileName} compact />
          }
```
to `ImageActions`.

`web/src/chat/displays/LocalArtifactDisplay.tsx`, video figcaption: replace `<DownloadLink … />` with
```tsx
          <span className="flex shrink-0 items-center gap-1">
            <EditMediaButton assetId={assetId} kind="video" mimeType={mimeType} fileName={filename} compact />
            <DownloadLink assetId={assetId} filename={filename} />
          </span>
```

`web/src/chat/artifacts/PreviewModal.tsx`, in the header before the download `<a>`:
```tsx
              {(() => {
                const kind = previewKind(active.mime_type, active.file_name);
                return kind === 'image' || kind === 'video' ? (
                  <EditMediaButton
                    assetId={active.id}
                    kind={kind}
                    mimeType={active.mime_type}
                    fileName={active.file_name}
                    onOpen={onClose}
                  />
                ) : null;
              })()}
```
(import `previewKind` from `./artifactMeta` if not already imported; if the IIFE trips a lint rule, compute `const editKind = active === undefined ? undefined : previewKind(active.mime_type, active.file_name);` before `return`.)

`web/src/studio/StudioStage.tsx`, `StageActions`, inside the `record.asset_id === undefined ? null : (…)` branch — make it a fragment holding the existing Download button and:
```tsx
          <EditMediaButton assetId={record.asset_id} kind={record.kind} className="py-1 text-xs" />
```

- [x] **Step 4: Run the suites to verify they pass**

Run: `npx vitest run src/chat src/studio src/mediaEdit src/components`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add web/src/studio/StudioStage.tsx web/src/components/image.tsx web/src/chat/artifacts/renderers/GeneratedImagePreview.tsx web/src/chat/displays/LocalArtifactDisplay.tsx web/src/chat/artifacts/PreviewModal.tsx web/src/chat/attachments/AttachmentCard.tsx web/src/chat/attachments/types.ts
git add web/src/studio/__tests__ web/src/chat/attachments/__tests__ web/src/chat/artifacts/__tests__ web/src/chat/displays/__tests__
git commit -m "feat(media-edit): offer Edit on Studio results, chat media and attachments" -m "Every place an operator meets one of their images or clips now opens the editor, including video attachments, which the server already stored as video but the cockpit drew as bare files." -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```
(Stage only the test files you created or changed; check `git status --short` before the second `git add`.)

---

### Task 11: E2E, lazy-chunk check, full gates, bundle

**Files:**
- Create: `web/e2e/fixtures/media-edit/clip.mp4`, `web/e2e/fixtures/media-edit/photo.png`, `web/e2e/media-edit.spec.ts`
- Modify: `internal/webui/dist/**` (rebuilt)

- [x] **Step 1: Generate the fixtures (a few KB each)**

From `D:/Repo/Aura` in Git Bash:
```bash
mkdir -p web/e2e/fixtures/media-edit
MSYS_NO_PATHCONV=1 docker run --rm -v "$(pwd -W)/web/e2e/fixtures/media-edit:/m" jrottenberg/ffmpeg:7.1-alpine \
  -hide_banner -loglevel error -y -f lavfi -i testsrc2=s=320x180:r=24:d=4 -f lavfi -i sine=f=440:d=4 \
  -c:v libx264 -pix_fmt yuv420p -g 24 -c:a aac -b:a 64k -shortest -movflags +faststart /m/clip.mp4
MSYS_NO_PATHCONV=1 docker run --rm -v "$(pwd -W)/web/e2e/fixtures/media-edit:/m" jrottenberg/ffmpeg:7.1-alpine \
  -hide_banner -loglevel error -y -f lavfi -i testsrc2=s=256x256 -frames:v 1 /m/photo.png
ls -la web/e2e/fixtures/media-edit
```
Expected: `clip.mp4` under 150 KB, `photo.png` under 150 KB.

- [x] **Step 2: Write the E2E spec**

`web/e2e/media-edit.spec.ts`:
```ts
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { expect, test, type Page } from '@playwright/test';
import { ALL_FORMATS, FilePathSource, Input } from 'mediabunny';
import type { StudioRecord } from '../src/studio/studioApi';
import { gotoAuthenticated } from './auth';

// The editors against the real `aura serve` of the E2E suite: the fixture bytes are uploaded
// through the real asset routes and read back through the real download route. Only the Studio's
// own routes are stubbed, because a CI deployment has no OpenRouter key and serves the Studio as
// unwired (503); the stubs are what a wired Studio answers.

const FIXTURES = resolve(__dirname, 'fixtures/media-edit');

async function upload(page: Page, file: string, mimeType: string): Promise<string> {
  const bytes = readFileSync(resolve(FIXTURES, file)).toString('base64');
  return page.evaluate(
    async ({ bytes, file, mimeType }) => {
      const body = Uint8Array.from(atob(bytes), (c) => c.charCodeAt(0));
      const presign = await fetch('/api/assets/presign', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ thread_id: '', file_name: file, mime_type: mimeType, size_bytes: body.byteLength, modality_hint: 'unknown' }),
      });
      if (!presign.ok) throw new Error(`presign: HTTP ${String(presign.status)}`);
      const { asset, upload } = (await presign.json()) as {
        asset: { id: string };
        upload: { upload_url: string; required_headers?: Record<string, string> };
      };
      const put = await fetch(upload.upload_url, { method: 'PUT', headers: upload.required_headers ?? {}, body });
      if (!put.ok) throw new Error(`put: HTTP ${String(put.status)}`);
      const done = await fetch(`/api/assets/${asset.id}/finalize`, { method: 'POST' });
      if (!done.ok) throw new Error(`finalize: HTTP ${String(done.status)}`);
      return asset.id;
    },
    { bytes, file, mimeType },
  );
}

function record(kind: 'image' | 'video', assetId: string): StudioRecord {
  return {
    id: `rec-${kind}`,
    kind,
    status: 'completed',
    model: 'test/model',
    prompt: `media edit ${kind}`,
    used: {},
    asset_id: assetId,
    created_at: new Date().toISOString(),
  };
}

async function openStudioWith(page: Page, rec: StudioRecord, library: 'ok' | 'off') {
  await page.route('**/api/studio/models?**', (route) =>
    route.fulfill({ json: { default: 'test/model', models: [{ id: 'test/model', audio: false, seed: false }] } }),
  );
  await page.route('**/api/studio/history?**', (route) => route.fulfill({ json: { records: [rec] } }));
  await page.route('**/api/studio/library**', (route) =>
    library === 'ok' ? route.fulfill({ json: { assets: [] } }) : route.fulfill({ status: 503, body: 'studio unavailable' }),
  );
  await page.addInitScript(() => {
    window.localStorage.setItem('aura.shell.surface', 'studio');
    window.localStorage.setItem('aura.studio.history-open', '1');
  });
  await gotoAuthenticated(page, '/');
  await page.locator(`[data-record-id="${rec.id}"]`).click();
}

test.describe('media editing', () => {
  test('trims a clip on the copy path and downloads the cut', async ({ page }, info) => {
    await gotoAuthenticated(page, '/');
    const assetId = await upload(page, 'clip.mp4', 'video/mp4');
    await openStudioWith(page, record('video', assetId), 'ok');
    await page.getByRole('button', { name: 'Edit', exact: true }).click();
    const editor = page.getByRole('dialog', { name: 'Edit clip.mp4' });
    await expect(editor.getByRole('slider', { name: 'Start of the selection' })).toBeVisible({ timeout: 30_000 });
    await editor.getByLabel('Start').fill('1');
    await editor.getByLabel('Start').blur();
    await editor.getByLabel('End').fill('3');
    await editor.getByLabel('End').blur();
    const downloading = page.waitForEvent('download');
    await editor.getByRole('button', { name: 'Save' }).click();
    const download = await downloading;
    expect(download.suggestedFilename()).toBe('clip-edited.mp4');
    const path = info.outputPath('clip-edited.mp4');
    await download.saveAs(path);
    const input = new Input({ source: new FilePathSource(path), formats: ALL_FORMATS });
    const duration = await input.computeDuration();
    input.dispose();
    expect(duration).toBeGreaterThan(1.9);
    expect(duration).toBeLessThan(2.2);
  });

  test('saves an edited photo to the Studio library', async ({ page }) => {
    await gotoAuthenticated(page, '/');
    const assetId = await upload(page, 'photo.png', 'image/png');
    let finalized = false;
    await page.route('**/api/studio/uploads/*/finalize', (route) => {
      finalized = true;
      return route.fulfill({ json: { id: 'lib-1', file_name: 'photo-edited.png', mime_type: 'image/png' } });
    });
    await openStudioWith(page, record('image', assetId), 'ok');
    await page.getByRole('button', { name: 'Edit', exact: true }).click();
    const editor = page.getByRole('dialog', { name: 'Edit photo.png' });
    await expect(editor.getByText('Filters', { exact: true })).toBeVisible({ timeout: 30_000 });
    await editor.getByText('Filters', { exact: true }).click();
    await editor.getByText('Sepia', { exact: true }).click();
    await editor.getByRole('button', { name: 'Save to library' }).click();
    await expect(editor.getByText('Saved to the Studio library')).toBeVisible();
    expect(finalized).toBe(true);
  });

  test('keeps Download when the Studio is not active', async ({ page }) => {
    await gotoAuthenticated(page, '/');
    const assetId = await upload(page, 'photo.png', 'image/png');
    await openStudioWith(page, record('image', assetId), 'off');
    await page.getByRole('button', { name: 'Edit', exact: true }).click();
    const editor = page.getByRole('dialog', { name: 'Edit photo.png' });
    await expect(editor.getByText('The Studio is not active: download the photo instead.')).toBeVisible({ timeout: 30_000 });
    await expect(editor.getByRole('button', { name: 'Save to library' })).toBeDisabled();
    const downloading = page.waitForEvent('download');
    await editor.getByRole('button', { name: 'Download' }).click();
    expect((await downloading).suggestedFilename()).toBe('photo-edited.png');
  });
});
```
If the Studio page with a 503 library still loads its stage (the library is only read by pickers), the third case holds; if the Studio page itself refuses to render with the stubbed routes, read `web/src/studio/StudioWorkspace.tsx` for the other routes it calls and stub them too.

- [x] **Step 3: Run the E2E locally**

Run (needs the local `aura` binary and stack the suite already uses): `npx playwright test e2e/media-edit.spec.ts --project=chrome`
Expected: 3 passed. On failure, open the trace (`npx playwright show-trace`) before changing anything.

- [x] **Step 4: Full web gates**

Run: `npm run lint && npm run typecheck && npm run format:check && npm run deadcode && npm run dup && npm test`
Expected: all green; Vitest coverage ≥ 85 % on all four metrics. Fix formatting with `npx prettier --write <file>` on the files you touched only.

- [x] **Step 5: Rebuild the bundle and check the chunks**

```bash
npm run build
ENTRY=$(grep -o 'assets/index-[^"]*\.js' ../internal/webui/dist/index.html | head -1)
grep -c "Konva" "../internal/webui/dist/$ENTRY" || true
grep -c "Mp4OutputFormat" "../internal/webui/dist/$ENTRY" || true
grep -l "Konva" ../internal/webui/dist/assets/*.js | head -3
grep -l "Mp4OutputFormat" ../internal/webui/dist/assets/*.js | head -3
```
Expected: `0` and `0` for the entry chunk; each library found in its own lazy chunk (not `index-*.js`).

- [x] **Step 6: Commit and push**

```bash
git add web/e2e/media-edit.spec.ts web/e2e/fixtures/media-edit/clip.mp4 web/e2e/fixtures/media-edit/photo.png internal/webui/dist
git commit -m "test(media-edit): drive both editors end to end and ship the bundle" -m "Real uploads, a real trim read back with Mediabunny and a real photo save; the editors land in lazy chunks, never in the entry bundle." -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
git push origin master
```
Expected: lefthook pre-push green (build, web gate, deadcode, payload manifest).

---

### Task 12: Live verification (definition of done)

**Files:**
- Create: `docs/superpowers/verification/2026-09-19-media-editing.md`

- [ ] **Step 1: Update the stack once the edge image carries the commit**

In WSL, from a script file (never piped through `bash -s`):
```bash
cd /opt/aura && docker compose pull aura && docker compose up -d aura && docker compose up -d --no-deps --force-recreate aura-migrate
docker inspect aura --format '{{index .Config.Labels "org.opencontainers.image.revision"}}'
```
Expected: the revision label equals the pushed commit.

- [ ] **Step 2: Exercise the real flows on `https://localhost`**

With `AURA_E2E_ORIGIN=https://localhost` and Playwright (Chrome), in one scripted pass: edit an existing Studio image (Filters → Save to library → it appears in the Studio library picker); open a chat where the agent generated a video, Edit → trim 1–3 s → download → duration ≈ 2 s; upload a phone MP4 and a JPEG as chat attachments, Edit each. Take desktop (1440×900) and phone (390×844) screenshots of both editors. Repeat the video trim in Firefox and record whether an HEVC source is refused with the sentence.

Measure the one open question the spec leaves: export the same MP4 with **rotation only** (90°, no trim change, no crop) and record the export time and `ffprobe` of the result — a copy path shows the original codec parameters plus a 90° display matrix and finishes in milliseconds; a transcode shows new encoder parameters and takes seconds.

- [ ] **Step 3: Record the evidence**

Write `docs/superpowers/verification/2026-09-19-media-editing.md`: what was run, on which revision, results per flow, screenshot paths, the Firefox outcome, and what this does **not** prove (Safari/iOS, very large clips).

- [ ] **Step 4: Commit and push**

```bash
git add docs/superpowers/verification/2026-09-19-media-editing.md
git commit -m "docs(media-edit): record the live verification" -m "Both editors exercised on the running stack from every surface, desktop and phone." -m "Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
git push origin master
```
