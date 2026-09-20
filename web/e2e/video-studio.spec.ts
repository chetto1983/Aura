import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { expect, test, type Locator, type Page } from '@playwright/test';
import { ALL_FORMATS, FilePathSource, Input } from 'mediabunny';
import type { StudioRecord } from '../src/studio/studioApi';
import { gotoAuthenticated } from './auth';
import { uploadAsset } from './support/assetUpload';

// video-studio.spec.ts — the multi-track editor against a running Aura, driven the way an
// operator drives it: a Studio result opened in the editor, a second clip picked from disk, a
// title typed into the inspector, and the file the Export button writes read back frame by frame.
//
// It exists because the unit suite cannot reach three things:
//
//   1. THE PICTURE. jsdom has no WebCodecs and no layout — every rect it reports is zero, so
//      dnd-timeline's scale collapses and a simulated drag proves nothing. The reorder and the
//      trim are therefore pointer gestures HERE and pure functions there.
//   2. THE FILE. `exportProject` was unit-tested against a mocked renderer. This is where a real
//      MP4 comes out, is measured for length and frame, and is searched for the title.
//   3. THE NETWORK. VideoFlow fetches fonts.googleapis.com unless `withLocalFonts` replaces
//      `loadFont`, and no unit test can see a request a real renderer would make. Every test here
//      records every http(s) origin the session touched and insists nothing left the appliance.
//
// The two clips are deliberately TWINS: identical H.264 video (the same `testsrc2`, byte for byte
// — their video streams share an MD5), different audio tones. So the exported composition shows
// the same picture at t and at t+4, and any pixel that differs between those two instants was put
// there by the editor. That is how the title is found without OCR — see `twinFrameDiff`.

const FIXTURES = resolve(dirname(fileURLToPath(import.meta.url)), 'fixtures/video-studio');

/** What the export must say, measured rather than assumed: two 4 s clips, no gap, no overlap. */
const EXPECTED_DURATION = 8;
/** The project takes its frame from its first source, and both fixtures are 320×180. */
const EXPECTED_FRAME = { width: 320, height: 180 };

/** The title. `fontSize` is in `em` and VideoFlow's root em is 1 % of the project width, so 14
 *  is 14 % of 320 px ≈ 45 px of glyph — big enough that the diff below is not counting noise. */
const TITLE = { text: 'TITLE', size: '14', colour: '#ff00ff' };

/** Two instants of the composition that show the SAME source frame: one inside the title's
 *  window (it starts at 0 and lasts 3 s), one 4 s later in the twin clip, where it has ended. */
const INSIDE_THE_TITLE = [1, 5] as const;
/** The control: the same relationship, at an instant the title never covers. */
const OUTSIDE_THE_TITLE = [3.5, 7.5] as const;

/** A pixel counts as changed when one of its channels moved this far. Measured twice on these
 *  very fixtures: re-encoding the two clips end to end with x264 and diffing the twin frames
 *  leaves 24 and 52 pixels of 57 600 above this line — the codec's own noise, nothing else —
 *  and the editor's own export leaves 0, because both halves encode the same picture the same
 *  way. Against that floor, the title measured 1 966 changed pixels, 960 of them its colour. */
const CHANGED_CHANNEL = 48;
/** How near a changed pixel has to be to the title's colour to count as its ink. */
const INK_DISTANCE = 60;

interface FrameDiff {
  /** Pixels that differ between the twin instants. */
  readonly changed: number;
  /** Of those, the ones painted in the title's colour. */
  readonly inked: number;
}

function videoRecord(assetId: string, prompt: string): StudioRecord {
  return {
    id: 'rec-video-studio',
    kind: 'video',
    status: 'completed',
    model: 'test/model',
    prompt,
    used: {},
    asset_id: assetId,
    created_at: new Date().toISOString(),
  };
}

interface NetworkLog {
  /** Every http(s) origin touched so far, in order of first sight. */
  origins(): readonly string[];
  /** A point in that list, so a later call can ask what a single gesture added. */
  mark(): number;
  since(mark: number): readonly string[];
  /** The object-store origins the appliance's OWN presign answers named, once they have been
   *  read. An empty set means the session never asked to upload anything. */
  store(): Promise<readonly string[]>;
}

/**
 * Every http(s) origin the session touched, from before the first navigation. Both the page and
 * its context are listened to: the export runs `worker: true`, and a dedicated worker's requests
 * reach the context even where they do not surface on the page.
 *
 * The second half of this is what makes the claim portable without weakening it. An upload goes
 * to a presigned URL, and which origin that is belongs to the DEPLOYMENT, not to the editor: on
 * the appliance Caddy fronts the store on the browsing origin itself, while CI presigns to
 * garage on another port. So the allowed set is not a list written here — it is exactly what the
 * cockpit's own `/api/assets/presign` answer named, and nothing else is tolerated.
 */
function watchNetwork(page: Page): NetworkLog {
  const seen: string[] = [];
  const presigned = new Set<string>();
  const reading: Promise<void>[] = [];
  const record = (url: string) => {
    if (!url.startsWith('http:') && !url.startsWith('https:')) return;
    const origin = new URL(url).origin;
    if (!seen.includes(origin)) seen.push(origin);
  };
  page.on('request', (request) => {
    record(request.url());
  });
  page.context().on('request', (request) => {
    record(request.url());
  });
  page.on('response', (response) => {
    if (!response.url().includes('/api/assets/presign')) return;
    reading.push(
      response
        .json()
        .then((body: { upload?: { upload_url?: unknown } }) => {
          const url = body.upload?.upload_url;
          if (typeof url === 'string') presigned.add(new URL(url).origin);
        })
        .catch(() => undefined),
    );
  });
  return {
    origins: () => [...seen],
    mark: () => seen.length,
    since: (mark) => seen.slice(mark),
    store: async () => {
      await Promise.all(reading);
      return [...presigned].sort();
    },
  };
}

/**
 * Nothing left the appliance. Every origin is either the cockpit the operator is looking at or
 * the object store the cockpit itself presigned an upload to — no font CDN, no telemetry, no
 * third party of any kind.
 */
async function expectNothingLeftTheAppliance(page: Page, network: NetworkLog) {
  const here = new URL(page.url()).origin;
  const store = await network.store();
  const strangers = network
    .origins()
    .filter((origin) => origin !== here && !store.includes(origin));
  expect(strangers, `off-origin requests (store origins: ${store.join(', ') || 'none'})`).toEqual(
    [],
  );
}

/**
 * The Studio with one completed video record pointing at a real uploaded asset. Only the Studio's
 * own routes are stubbed — a CI deployment has no OpenRouter key and serves them 503 — and the
 * asset those routes name is the fixture, uploaded through the real presign/PUT/finalize path.
 */
async function openStudioWith(page: Page, assetId: string, prompt: string) {
  await page.route('**/api/studio/models?**', (route) =>
    route.fulfill({
      json: { default: 'test/model', models: [{ id: 'test/model', audio: false, seed: false }] },
    }),
  );
  await page.route('**/api/studio/history?**', (route) =>
    route.fulfill({ json: { records: [videoRecord(assetId, prompt)] } }),
  );
  await page.route('**/api/studio/library**', (route) => route.fulfill({ json: { assets: [] } }));
  await page.addInitScript(() => {
    window.localStorage.setItem('aura.shell.surface', 'studio');
  });
  await gotoAuthenticated(page, '/');
}

/** Signs in, puts `clip-a.mp4` in the library, and opens the editor on it as the Studio would. */
async function editorOnSeededClip(page: Page, prompt: string): Promise<Locator> {
  await gotoAuthenticated(page, '/');
  const seeded = await uploadAsset(
    page,
    resolve(FIXTURES, 'clip-a.mp4'),
    'clip-a.mp4',
    'video/mp4',
  );
  await openStudioWith(page, seeded, prompt);
  await page.getByRole('button', { name: 'Open in the video editor' }).click();
  const editor = page.getByRole('dialog', { name: 'Video editor' });
  await expect(editor.getByRole('button', { name: 'Clip 1' })).toBeVisible({ timeout: 60_000 });
  await expect(editor.getByRole('heading', { name: prompt })).toBeVisible();
  return editor;
}

/** Picks a file through the editor's own button, the way an operator does. */
async function addClip(page: Page, editor: Locator, file: string) {
  const chooser = page.waitForEvent('filechooser');
  await editor.getByRole('button', { name: 'Add a clip' }).click();
  await (await chooser).setFiles(resolve(FIXTURES, file));
}

/** Commits one inspector field: type, then blur, which is what the fields listen for. */
async function setField(inspector: Locator, label: string, value: string) {
  const field = inspector.getByLabel(label, { exact: true });
  await field.fill(value);
  await field.blur();
}

/** A lane item prints its own length as `mm:ss.s`; this reads that back as a number. */
function seconds(timecode: string): number {
  const [minutes, rest] = timecode.trim().split(':');
  if (minutes === undefined || rest === undefined) {
    throw new Error(`not a timecode: ${timecode}`);
  }
  return Number(minutes) * 60 + Number(rest);
}

/** The layout box of a locator, or a failure that says which one had none. */
async function boxOf(target: Locator, what: string) {
  const box = await target.boundingBox();
  if (box === null) throw new Error(`${what} has no layout box`);
  return box;
}

/**
 * A pointer drag with intermediate moves, pressing at `anchor` across the target's width.
 * dnd-kit's PointerSensor activates on distance, so a single jump from press to release never
 * crosses it: the gesture has to be made of steps. The anchor matters for a trim — dnd-timeline
 * reads the press position against the item's rect to tell a resize from a move.
 */
async function dragBy(page: Page, target: Locator, what: string, dx: number, anchor = 0.5) {
  const box = await boxOf(target, what);
  const from = { x: box.x + box.width * anchor, y: box.y + box.height / 2 };
  await page.mouse.move(from.x, from.y);
  await page.mouse.down();
  for (const step of [0.1, 0.35, 0.6, 0.85, 1]) {
    await page.mouse.move(from.x + dx * step, from.y, { steps: 4 });
  }
  await page.mouse.up();
}

/**
 * How much of the picture differs between two instants that show the same source frame, and how
 * much of that difference is the title's own colour.
 *
 * Decoding happens in the page rather than in Node: Node has no WebCodecs, and a `<video>` fed
 * the exported bytes is also the bluntest available proof that what came out is playable.
 */
async function twinFrameDiff(
  page: Page,
  mp4: Buffer,
  pairs: readonly (readonly [number, number])[],
  ink: string,
): Promise<readonly FrameDiff[]> {
  return page.evaluate(
    async ({ base64, pairs, ink, changedChannel, inkDistance }) => {
      const bytes = Uint8Array.from(atob(base64), (c) => c.charCodeAt(0));
      const url = URL.createObjectURL(new Blob([bytes], { type: 'video/mp4' }));
      const video = document.createElement('video');
      video.muted = true;
      video.preload = 'auto';
      video.src = url;
      try {
        await new Promise<void>((resolve, reject) => {
          video.onloadeddata = () => {
            resolve();
          };
          video.onerror = () => {
            reject(new Error('the exported file did not decode in the browser'));
          };
        });
        const canvas = new OffscreenCanvas(video.videoWidth, video.videoHeight);
        const context = canvas.getContext('2d', { willReadFrequently: true });
        if (context === null) throw new Error('no 2d context');
        const target = [
          parseInt(ink.slice(1, 3), 16),
          parseInt(ink.slice(3, 5), 16),
          parseInt(ink.slice(5, 7), 16),
        ];
        const frameAt = async (time: number) => {
          await new Promise<void>((resolve) => {
            video.onseeked = () => {
              resolve();
            };
            video.currentTime = time;
          });
          context.drawImage(video, 0, 0);
          return context.getImageData(0, 0, canvas.width, canvas.height).data;
        };
        const diffs = [];
        for (const [first, second] of pairs) {
          const a = await frameAt(first);
          const b = await frameAt(second);
          let changed = 0;
          let inked = 0;
          for (let i = 0; i < a.length; i += 4) {
            const moved = Math.max(
              Math.abs((a[i] ?? 0) - (b[i] ?? 0)),
              Math.abs((a[i + 1] ?? 0) - (b[i + 1] ?? 0)),
              Math.abs((a[i + 2] ?? 0) - (b[i + 2] ?? 0)),
            );
            if (moved < changedChannel) continue;
            changed += 1;
            const off = Math.max(
              Math.abs((a[i] ?? 0) - (target[0] ?? 0)),
              Math.abs((a[i + 1] ?? 0) - (target[1] ?? 0)),
              Math.abs((a[i + 2] ?? 0) - (target[2] ?? 0)),
            );
            if (off <= inkDistance) inked += 1;
          }
          diffs.push({ changed, inked });
        }
        return diffs;
      } finally {
        URL.revokeObjectURL(url);
      }
    },
    {
      base64: mp4.toString('base64'),
      pairs: pairs.map(([a, b]) => [a, b] as [number, number]),
      ink,
      changedChannel: CHANGED_CHANNEL,
      inkDistance: INK_DISTANCE,
    },
  );
}

/** What the container says, read with Mediabunny in Node — no decoder needed for any of it. */
async function containerFacts(path: string) {
  const input = new Input({ source: new FilePathSource(path), formats: ALL_FORMATS });
  try {
    const video = await input.getPrimaryVideoTrack();
    if (video === null) throw new Error('the export has no video track');
    const audio = await input.getPrimaryAudioTrack();
    return {
      duration: await input.computeDuration(),
      width: await video.getDisplayWidth(),
      height: await video.getDisplayHeight(),
      hasAudio: audio !== null,
    };
  } finally {
    input.dispose();
  }
}

test.describe('the multi-track video editor', () => {
  test('builds two clips and a title, exports them, and touches no other origin', async ({
    page,
  }, info) => {
    // A render of 240 frames plus two uploads. Generous, and a ceiling rather than a wait: what
    // passes this test is the assertions below, never the clock.
    test.setTimeout(12 * 60_000);
    const network = watchNetwork(page);
    const prompt = 'video studio cycle one';
    const editor = await editorOnSeededClip(page, prompt);

    // A second clip, picked from disk through the editor's own button: probed, uploaded and
    // appended to the sequence.
    await addClip(page, editor, 'clip-b.mp4');
    await expect(editor.getByRole('button', { name: 'Clip 2' })).toBeVisible({ timeout: 60_000 });

    // The title hangs on the clip under the playhead, which starts at zero, and lasts three
    // seconds. The workspace selects what it just added, so the inspector is already on it.
    await editor.getByRole('button', { name: 'Add a title' }).click();
    await expect(editor.getByRole('button', { name: 'Title 1' })).toBeVisible();
    const inspector = editor.getByRole('region', { name: 'Properties' });
    await setField(inspector, 'Text', TITLE.text);
    await setField(inspector, 'Text size', TITLE.size);
    await setField(inspector, 'Colour', TITLE.colour);

    // From here to the download is the render: the worker, the fonts, the clips it re-fetches.
    // Nothing it does is allowed to reach an origin the session had not already used.
    const beforeExport = network.mark();
    const downloading = page.waitForEvent('download', { timeout: 10 * 60_000 });
    await editor.getByRole('button', { name: 'Export', exact: true }).click();
    const download = await downloading;
    expect(network.since(beforeExport), 'origins the render reached for').toEqual([]);
    expect(download.suggestedFilename()).toBe('video-studio-cycle-one.mp4');
    const path = info.outputPath('video-studio-cycle-one.mp4');
    await download.saveAs(path);

    const facts = await containerFacts(path);
    expect(facts.duration).toBeGreaterThan(EXPECTED_DURATION - 0.1);
    expect(facts.duration).toBeLessThan(EXPECTED_DURATION + 0.1);
    expect({ width: facts.width, height: facts.height }).toEqual(EXPECTED_FRAME);
    expect(facts.hasAudio).toBe(true);

    const [withTitle, withoutTitle] = await twinFrameDiff(
      page,
      readFileSync(path),
      [INSIDE_THE_TITLE, OUTSIDE_THE_TITLE],
      TITLE.colour,
    );
    if (withTitle === undefined || withoutTitle === undefined) {
      throw new Error('the frame diff answered fewer pairs than it was asked');
    }
    // Inside its window the title is on the picture, in the colour the inspector was given.
    expect(withTitle.inked).toBeGreaterThan(300);
    // Outside it the same source frame comes back unmarked: whatever the codec moved between the
    // twins, none of it is the title's colour.
    expect(withoutTitle.inked).toBe(0);
    expect(withoutTitle.changed).toBeLessThan(withTitle.changed / 4);

    // The numbers themselves, kept with the run: a threshold is only honest next to what it
    // was measured against.
    await info.attach('export-measurements', {
      contentType: 'application/json',
      body: JSON.stringify({ facts, withTitle, withoutTitle }, null, 2),
    });

    await expectNothingLeftTheAppliance(page, network);
  });

  test('refuses a clip it cannot decode before a byte of it is uploaded', async ({ page }) => {
    test.setTimeout(5 * 60_000);
    const network = watchNetwork(page);
    const editor = await editorOnSeededClip(page, 'video studio refusal');

    // Counted rather than assumed: the probe runs before the upload, so a refusal must cost no
    // transfer. Registered after the seeding upload, which is the test's own and not the UI's.
    let presigned = 0;
    page.on('request', (request) => {
      if (request.url().includes('/api/assets/presign')) presigned += 1;
    });

    // The fixture has to be the RIGHT kind of broken. `unplayable.mp4` is MPEG-4 Part 2 in an
    // MP4: Mediabunny parses it and answers a duration and a display size, and no browser holds
    // a decoder for it. A file that merely failed to PARSE would collect the same refusal
    // through a different door and would prove nothing about decodability.
    const fixture = await containerFacts(resolve(FIXTURES, 'unplayable.mp4'));
    expect(fixture).toMatchObject({ width: 320, height: 180 });
    expect(fixture.duration).toBeGreaterThan(0);

    await addClip(page, editor, 'unplayable.mp4');
    await expect(
      editor
        .getByRole('alert')
        .filter({ hasText: 'This browser cannot decode that clip, so it would export as black' }),
    ).toBeVisible({ timeout: 30_000 });
    expect(presigned).toBe(0);
    await expect(editor.getByRole('button', { name: 'Clip 2' })).toHaveCount(0);

    await expectNothingLeftTheAppliance(page, network);
  });

  test('reorders and trims by pointer, where the timeline has a real scale', async ({
    page,
  }, info) => {
    test.skip(
      info.project.name.startsWith('mobile'),
      'a drag of a 44 px trim handle and an Alt+Arrow reorder are a mouse and a keyboard gesture; ' +
        'Playwright drives one touch point and no hardware keys, so a phone cannot make either — ' +
        "the phone's own path is the test below",
    );
    test.setTimeout(6 * 60_000);
    const network = watchNetwork(page);
    const editor = await editorOnSeededClip(page, 'video studio gestures');
    await addClip(page, editor, 'clip-b.mp4');
    await expect(editor.getByRole('button', { name: 'Clip 2' })).toBeVisible({ timeout: 60_000 });

    const first = editor.getByRole('button', { name: 'Clip 1' });
    const second = editor.getByRole('button', { name: 'Clip 2' });
    // The clips are twins, so they are made tellable apart first: the second is trimmed to two
    // seconds through the inspector, and the lane prints each clip's length on it.
    await second.click();
    const inspector = editor.getByRole('region', { name: 'Properties' });
    await setField(inspector, 'End', '00:02.0');
    await expect(second).toHaveText('00:02.0');
    await expect(first).toHaveText('00:04.0');

    // A real pointer drag, far enough left to carry the second clip's start past the first's.
    const span = (await boxOf(second, 'clip 2')).x - (await boxOf(first, 'clip 1')).x;
    await dragBy(page, second, 'clip 2', -span);
    // The labels are positional, so after a reorder it is the LENGTHS that have swapped places.
    await expect(first).toHaveText('00:02.0');
    await expect(second).toHaveText('00:04.0');

    // And a real trim: the press lands in the item's resize band, which is what tells
    // dnd-timeline this is a resize and not a move. The handle straddles the item's edge, so
    // pressing at a quarter of its width puts the pointer inside the clip.
    const handle = editor.getByRole('slider', { name: 'End of clip 2' });
    const was = Number(await handle.getAttribute('aria-valuenow'));
    await dragBy(
      page,
      handle,
      'the end handle of clip 2',
      -(await boxOf(second, 'clip 2')).width / 3,
      0.25,
    );
    // Shorter than it was, and still a clip: a trim that emptied it or left it unchanged would
    // both satisfy "not four seconds".
    const trimmed = seconds((await second.textContent()) ?? '');
    expect(trimmed).toBeGreaterThan(0);
    expect(trimmed).toBeLessThan(4);
    expect(Number(await handle.getAttribute('aria-valuenow'))).toBeLessThan(was);

    await expectNothingLeftTheAppliance(page, network);
  });

  test('works under a thumb: the lanes move, the fields commit', async ({ page }, info) => {
    test.skip(
      !info.project.name.startsWith('mobile'),
      'the phone claims belong to the phone projects; a desktop viewport would prove nothing about a thumb',
    );
    test.setTimeout(6 * 60_000);
    const network = watchNetwork(page);
    const editor = await editorOnSeededClip(page, 'video studio on a phone');
    await addClip(page, editor, 'clip-b.mp4');
    await expect(editor.getByRole('button', { name: 'Clip 2' })).toBeVisible({ timeout: 60_000 });

    // The lane's view is a RANGE, not a scrollbar: zooming is what moves it, and the zoom
    // buttons are the thumb's way to that. A pinch is not — Playwright drives one touch point.
    const marks = editor.getByTestId('timeline-mark');
    const ruler = () => marks.evaluateAll((nodes) => nodes.map((n) => n.textContent).join(' '));
    const before = await ruler();
    await editor.getByRole('button', { name: 'Zoom in' }).tap();
    await expect.poll(ruler).not.toBe(before);

    // A tap selects, and the inspector's fields commit what a thumb types into them.
    await editor.getByRole('button', { name: 'Clip 1' }).tap();
    const inspector = editor.getByRole('region', { name: 'Properties' });
    await setField(inspector, 'End', '00:03.0');
    await expect(editor.getByRole('button', { name: 'Clip 1' })).toHaveText('00:03.0');

    await expectNothingLeftTheAppliance(page, network);
  });
});
