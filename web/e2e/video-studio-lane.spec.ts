import { randomUUID } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import type { Locator, Page } from '@playwright/test';
import { expect, test } from './support/assetCleanup';
import { uploadAsset } from './support/assetUpload';
import { levelBetween, windowPowers } from './support/audioMeasure';
import {
  AUDIO_FIXTURES,
  exportTo,
  pressAddAction,
  reopen,
  setField,
  uploadClip,
} from './support/videoStudio';

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
    sources: [
      { id: 'src-a', assetId: clip, kind: 'video', duration: 4, size: FRAME, hasAudio: true },
    ],
    video: ['clip-1', 'clip-2'].map((id) => ({
      id,
      sourceId: 'src-a',
      duration: 4,
      sourceStart: 0,
      muted: true,
    })),
    overlays: [],
  };
}

async function addMusic(page: Page, editor: Locator): Promise<void> {
  const chooser = page.waitForEvent('filechooser');
  await pressAddAction(editor, 'Add audio');
  await (await chooser).setFiles(resolve(AUDIO_FIXTURES, 'music.wav'));
  const sound = editor.getByRole('button', { name: 'Sound 1' });
  await expect(sound).toBeVisible({ timeout: 60_000 });
  await expect(sound).toContainText('music.wav');
}

/** A slider's thumb: Radix names the slider's root, and the thumb inside it is what keys move. */
function slider(scope: Locator, name: string): Locator {
  return scope.getByLabel(name, { exact: true }).getByRole('slider');
}

test('a sound shaped in the inspector exports at the volume and fade it was given', async ({
  page,
}, info) => {
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
  const volume = slider(inspector, 'Volume');
  for (let press = 0; press < 5; press += 1) await volume.press('PageDown');
  await expect(volume).toHaveAttribute('aria-valuenow', '50');
  const fadeIn = slider(inspector, 'Fade in');
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

  await info.attach('lane-ui-editor', { contentType: 'image/png', body: await page.screenshot() });
  const path = await exportTo(page, editor, info, 'lane-ui.mp4');
  const powers = await windowPowers(page, readFileSync(path));
  const levels = {
    start: levelBetween(powers, 0, 0.1),
    soft: levelBetween(powers, 2.5, 3.5),
    full: levelBetween(powers, 4.5, 7.5),
  };
  await info.attach('lane-ui-levels', {
    contentType: 'application/json',
    body: JSON.stringify(levels, null, 2),
  });
  expect(levels.full - levels.soft).toBeGreaterThan(5);
  expect(levels.full - levels.soft).toBeLessThan(7);
  expect(levels.soft - levels.start).toBeGreaterThanOrEqual(15);
});

test('a sound goes on its lane under a thumb, with the tools a sound has', async ({
  page,
}, info) => {
  test.skip(
    !info.project.name.startsWith('mobile'),
    'the phone claims belong to the phone projects; a desktop viewport would prove nothing about a thumb',
  );
  test.setTimeout(6 * 60_000);
  const clip = await uploadClip(page);
  const editor = await reopen(page, silentFilm(clip, 'lane phone check'), clip);
  await expect(editor.getByRole('button', { name: 'Clip 2' })).toBeVisible({ timeout: 60_000 });
  await addMusic(page, editor);
  const tools = editor.getByRole('navigation', { name: 'Mobile editing tools' });
  await expect(tools.getByRole('button', { name: 'Audio', exact: true })).toBeVisible();
  await expect(tools.getByRole('button', { name: 'Adjust', exact: true })).toHaveCount(0);
  await tools.getByRole('button', { name: 'Audio', exact: true }).click();
  await expect(slider(editor, 'Fade in')).toBeVisible();
  await info.attach('lane-phone', { contentType: 'image/png', body: await page.screenshot() });
});

test('a trim that crowds one sound onto another gives it a lane of its own, and both stay editable', async ({
  page,
}, info) => {
  test.skip(
    info.project.name.startsWith('mobile'),
    'the lane rule is the same under a thumb; the phone path is the test above',
  );
  test.setTimeout(6 * 60_000);
  const clip = await uploadClip(page);
  const music = await uploadAsset(
    page,
    resolve(AUDIO_FIXTURES, 'music.wav'),
    'music.wav',
    'audio/wav',
    { use: 'media' },
  );
  const film = silentFilm(clip, 'lane crowd check');
  const sound = (id: string, clipId: string, duration: number) => ({
    id,
    sourceId: 'src-m',
    anchor: { clipId, offset: 0 },
    sourceStart: 0,
    duration,
    volume: 1,
    muted: false,
  });
  // One lane: 0–3 s on clip 1 and 4–6 s on clip 2. Trimming clip 1 to 2 s brings clip 2's sound
  // to 2–4 s, over the first one.
  const editor = await reopen(
    page,
    {
      ...film,
      sources: [
        ...film.sources,
        { id: 'src-m', assetId: music, kind: 'audio', duration: 8, size: { width: 0, height: 0 } },
      ],
      audio: [{ id: 'lane-a', items: [sound('early', 'clip-1', 3), sound('late', 'clip-2', 2)] }],
    },
    clip,
  );
  await expect(editor.getByRole('button', { name: 'Sound 2' })).toBeVisible({ timeout: 60_000 });
  await expect(editor.getByRole('group', { name: 'Audio 2' })).toHaveCount(0);

  await editor.getByRole('button', { name: 'Clip 1' }).click();
  const inspector = editor.getByRole('region', { name: 'Properties' });
  await inspector.getByRole('tab', { name: 'Time' }).click();
  await setField(inspector, 'End', '00:02.0');
  const second = editor.getByRole('group', { name: 'Audio 2' });
  await expect(second.getByRole('button', { name: 'Sound 2' })).toBeVisible();

  for (const name of ['Sound 1', 'Sound 2']) {
    await editor.getByRole('button', { name }).click();
    await inspector.getByRole('tab', { name: 'Audio' }).click();
    await inspector.getByRole('switch', { name: 'Mute' }).click();
    await expect(inspector.getByRole('switch', { name: 'Mute' })).toBeChecked();
  }
  await expect(editor.getByRole('alert')).toHaveCount(0);
  await info.attach('lane-crowd', { contentType: 'image/png', body: await page.screenshot() });
});
