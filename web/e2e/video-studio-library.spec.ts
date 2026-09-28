import { randomUUID } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { expect, test } from './support/assetCleanup';
import { uploadAsset } from './support/assetUpload';
import { levelBetween, windowPowers } from './support/audioMeasure';
import {
  AUDIO_FIXTURES,
  exportTo,
  FIXTURES,
  openAudioPanel,
  openClipPanel,
  reopen,
  silentFilm,
  uploadClip,
} from './support/videoStudio';

// video-studio-library.spec.ts — the library as a door into the Studio (operator, 2026-09-28: "miss
// load audio from garage"). An asset already in Garage is picked from Add a sound or Add a clip
// and lands on the timeline without being uploaded again; the sound is proven by the export, the
// clip by the timeline it lengthens. Each asset gets a name no earlier run used, so the list's
// button is this run's asset and nothing else.

function unique(extension: string): string {
  return `library-${randomUUID().slice(0, 8)}.${extension}`;
}

test('a sound picked from the library goes on a lane and plays in the export', async ({
  page,
}, info) => {
  test.skip(info.project.name.startsWith('mobile'), 'the phone opens the same panel');
  test.setTimeout(10 * 60_000);
  const clip = await uploadClip(page);
  const name = unique('wav');
  await uploadAsset(page, resolve(AUDIO_FIXTURES, 'music.wav'), name, 'audio/wav', {
    use: 'media',
  });
  const editor = await reopen(page, silentFilm(clip, 'library sound'), clip);
  const panel = await openAudioPanel(page, editor);
  const picked = panel.getByRole('button', { name });
  await expect(picked).toBeVisible({ timeout: 30_000 });
  await picked.click();
  const sound = editor.getByRole('button', { name: 'Sound 1' });
  await expect(sound).toBeVisible({ timeout: 60_000 });
  await expect(sound).toContainText(name);
  await expect(editor.getByRole('alert')).toHaveCount(0);
  await info.attach('library-sound', { contentType: 'image/png', body: await page.screenshot() });

  const powers = await windowPowers(
    page,
    readFileSync(await exportTo(page, editor, info, 'library-sound.mp4')),
  );
  // The film is silent: whatever plays is the library's music, which an export carries at
  // −21.24 dBFS (the mono bed panned into stereo, Plan B's lane E2E).
  const music = levelBetween(powers, 1, 3);
  await info.attach('library-levels', {
    contentType: 'application/json',
    body: JSON.stringify({ music }, null, 2),
  });
  expect(music).toBeGreaterThan(-30);
});

test('a video picked from the library becomes the next clip', async ({ page }, info) => {
  test.skip(info.project.name.startsWith('mobile'), 'the phone opens the same panel');
  test.setTimeout(10 * 60_000);
  const clip = await uploadClip(page);
  const name = unique('mp4');
  await uploadAsset(page, resolve(FIXTURES, 'clip-b.mp4'), name, 'video/mp4', { use: 'media' });
  const editor = await reopen(page, silentFilm(clip, 'library clip'), clip);
  const panel = await openClipPanel(page, editor);
  const picked = panel.getByRole('button', { name });
  await expect(picked).toBeVisible({ timeout: 30_000 });
  await picked.click();
  await expect(editor.getByRole('button', { name: 'Clip 3' })).toBeVisible({ timeout: 60_000 });
  await expect(editor.getByRole('alert')).toHaveCount(0);
  await expect(panel).toHaveCount(0);
  await info.attach('library-clip', { contentType: 'image/png', body: await page.screenshot() });
});
