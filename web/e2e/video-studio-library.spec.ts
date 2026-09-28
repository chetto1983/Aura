import { randomUUID } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { STUDIO_LIBRARY_LIMIT } from '../src/studio/studioApi';
import { expect, test } from './support/assetCleanup';
import { uploadAsset, uploadBytes } from './support/assetUpload';
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
// video and the picture by the timeline they lengthen. Each asset gets a name no earlier run used, so the list's
// button is this run's asset and nothing else. The list is paged: an asset older than a full page
// is reached through Show more.

function unique(extension: string): string {
  return `library-${randomUUID().slice(0, 8)}.${extension}`;
}

/** A tenth of a second of 16 kHz mono 16-bit silence: a sound the library lists, and nothing
 *  more — the paging test needs a page of them, never their audio. */
function silence(): Buffer {
  const data = 1600 * 2;
  const wav = Buffer.alloc(44 + data);
  wav.write('RIFF', 0);
  wav.writeUInt32LE(36 + data, 4);
  wav.write('WAVEfmt ', 8);
  wav.writeUInt32LE(16, 16);
  wav.writeUInt16LE(1, 20);
  wav.writeUInt16LE(1, 22);
  wav.writeUInt32LE(16_000, 24);
  wav.writeUInt32LE(32_000, 28);
  wav.writeUInt16LE(2, 32);
  wav.writeUInt16LE(16, 34);
  wav.write('data', 36);
  wav.writeUInt32LE(data, 40);
  return wav;
}

// A full page of newer sounds puts an older one on the next page: the panel does not list it
// until Show more reads that page, and then it is picked like any other. On the phone too.
test('a sound older than a full page is reached through Show more', async ({ page }, info) => {
  test.setTimeout(10 * 60_000);
  const clip = await uploadClip(page);
  const older = unique('wav');
  await uploadAsset(page, resolve(AUDIO_FIXTURES, 'music.wav'), older, 'audio/wav', {
    use: 'media',
  });
  for (let newer = 0; newer < STUDIO_LIBRARY_LIMIT; newer += 1) {
    await uploadBytes(page, silence(), unique('wav'), 'audio/wav', { use: 'media' });
  }
  const editor = await reopen(page, silentFilm(clip, 'library paging'), clip);
  const panel = await openAudioPanel(page, editor);
  const more = panel.getByRole('button', { name: 'Show more' });
  await expect(more).toBeVisible({ timeout: 30_000 });
  await expect(panel.getByRole('button', { name: older })).toHaveCount(0);
  // The list scrolls inside the panel: each picture shows its bottom, where the page ends.
  await panel.getByRole('listitem').last().scrollIntoViewIfNeeded();
  await more.scrollIntoViewIfNeeded();
  await info.attach('library-before-more', {
    contentType: 'image/png',
    body: await page.screenshot(),
  });

  // The desktop and phone projects run at once against the operator's one library, so the other
  // project's newer sounds can push this one past the next page: read older pages until it shows.
  const picked = panel.getByRole('button', { name: older });
  await expect(async () => {
    if ((await more.isVisible()) && (await more.isEnabled())) await more.click();
    await expect(picked).toBeVisible({ timeout: 2_000 });
  }).toPass({ timeout: 60_000 });
  await picked.scrollIntoViewIfNeeded();
  await info.attach('library-more', { contentType: 'image/png', body: await page.screenshot() });
  await picked.click();
  const sound = editor.getByRole('button', { name: 'Sound 1' });
  await expect(sound).toBeVisible({ timeout: 60_000 });
  await expect(sound).toContainText(older);
  await info.attach('library-more-on-lane', {
    contentType: 'image/png',
    body: await page.screenshot(),
  });
});

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

// Operator, 2026-09-28: a picture picked here was refused as an undecodable clip — the download
// route answers every asset as application/octet-stream, and the pick read that as a video.
for (const kind of [
  { what: 'video', file: resolve(FIXTURES, 'clip-b.mp4'), type: 'video/mp4', shot: 'library-clip' },
  {
    what: 'picture',
    file: resolve(FIXTURES, '../media-edit/photo.png'),
    type: 'image/png',
    shot: 'library-picture',
  },
] as const) {
  test(`a ${kind.what} picked from the library becomes the next clip`, async ({ page }, info) => {
    test.skip(info.project.name.startsWith('mobile'), 'the phone opens the same panel');
    test.setTimeout(10 * 60_000);
    const clip = await uploadClip(page);
    const name = unique(kind.file.split('.').pop() ?? '');
    await uploadAsset(page, kind.file, name, kind.type, { use: 'media' });
    const editor = await reopen(page, silentFilm(clip, `library ${kind.what}`), clip);
    const panel = await openClipPanel(page, editor);
    const picked = panel.getByRole('button', { name });
    await expect(picked).toBeVisible({ timeout: 30_000 });
    await picked.click();
    await expect(editor.getByRole('button', { name: 'Clip 3' })).toBeVisible({ timeout: 60_000 });
    await expect(editor.getByRole('alert')).toHaveCount(0);
    await expect(panel).toHaveCount(0);
    await info.attach(kind.shot, { contentType: 'image/png', body: await page.screenshot() });
  });
}
