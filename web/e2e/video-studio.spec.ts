import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import type { Page } from '@playwright/test';
import { expect, test } from './support/assetCleanup';
import {
  FIXTURES,
  addClip,
  boxOf,
  containerFacts,
  dragBy,
  editorOnSeededClip,
  expectNothingLeftTheAppliance,
  pressAddAction,
  seconds,
  setField,
  watchNetwork,
} from './support/videoStudio';

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
//
// These specs also run against the lab VM, signed in as the operator: every asset a test
// presigns is recorded and deleted when it ends, so a run leaves the library as it found it.

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
  /** Of those, the ones where the title's colour APPEARED — near it in the first frame and not
   *  in the second. Counting "changed and near the colour" instead would count the codec's own
   *  jitter inside `testsrc2`'s magenta bar, where both frames are already that colour: CI's
   *  encoder produced exactly 4 such pixels outside the title's window where this host's
   *  produced none. A pixel that was the colour before cannot be evidence the title is there. */
  readonly inked: number;
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
        const isInk = (frame: Uint8ClampedArray, at: number) =>
          Math.max(
            Math.abs((frame[at] ?? 0) - (target[0] ?? 0)),
            Math.abs((frame[at + 1] ?? 0) - (target[1] ?? 0)),
            Math.abs((frame[at + 2] ?? 0) - (target[2] ?? 0)),
          ) <= inkDistance;
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
            if (isInk(a, i) && !isInk(b, i)) inked += 1;
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
    await pressAddAction(editor, 'Add a title');
    await expect(editor.getByRole('button', { name: 'Title 1' })).toBeVisible();
    // At phone width the properties are a sheet only the tool bar opens, and adding a title does
    // not open it; any of the bar's panels shows the selected title's fields.
    const openSheet = editor.getByRole('button', { name: 'Transform' });
    if (await openSheet.isVisible()) await openSheet.click();
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
    // Outside it the same source frame comes back unmarked. Not a hard zero, and the reason is
    // measured rather than conceded: this host's encoder leaves 0 there, CI's leaves a handful
    // of single pixels, and no encoder will ever be promised to be deterministic across both.
    // A hundredfold is the claim that survives either — a title is a word, not four pixels.
    expect(withoutTitle.inked * 100).toBeLessThan(withTitle.inked);
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
    // A clip's trim lives on the inspector's Time tab; it opens on Transform.
    await inspector.getByRole('tab', { name: 'Time' }).click();
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

    // A tap selects, the tool bar's Time panel opens the sheet on the clip's in and out points,
    // and its fields commit what a thumb types into them. The sheet covers the lane, so it is
    // closed before the lane is read.
    await editor.getByRole('button', { name: 'Clip 1' }).tap();
    await editor.getByRole('button', { name: 'Time', exact: true }).tap();
    const inspector = editor.getByRole('region', { name: 'Properties' });
    await setField(inspector, 'End', '00:03.0');
    await editor.getByRole('button', { name: 'Close tool panel' }).tap();
    await expect(editor.getByRole('button', { name: 'Clip 1' })).toHaveText('00:03.0');

    await expectNothingLeftTheAppliance(page, network);
  });

  test('the rail hides the properties for the stage to take their width, and brings them back', async ({
    page,
  }, info) => {
    test.skip(
      info.project.name.startsWith('mobile'),
      'the phone has a properties sheet, not a panel',
    );
    const editor = await editorOnSeededClip(page, 'video studio properties');
    const properties = editor
      .getByRole('toolbar', { name: 'Editing commands' })
      .getByRole('button', { name: 'Properties' });
    const stage = editor.locator('.video-studio-canvas');
    await expect(properties).toHaveAttribute('aria-pressed', 'true');
    const before = await boxOf(stage, 'the stage');

    // Operator, 2026-09-28: "property button do nothing" — with the panel open it now closes it.
    await properties.click();
    await expect(properties).toHaveAttribute('aria-pressed', 'false');
    await expect(editor.getByRole('region', { name: 'Properties' })).toBeHidden();
    await expect
      .poll(async () => (await boxOf(stage, 'the stage')).width)
      .toBeGreaterThan(before.width + 250);
    await info.attach('properties-hidden', {
      contentType: 'image/png',
      body: await page.screenshot(),
    });

    await properties.click();
    await expect(editor.getByRole('region', { name: 'Properties' })).toBeVisible();
    await expect(editor.locator('.video-studio-properties')).toBeFocused();
  });
});
