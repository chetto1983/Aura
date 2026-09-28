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

test('the music goes 12 dB down under speech, over ramps as long as the softness', async ({
  page,
}, info) => {
  test.skip(info.project.name.startsWith('mobile'), 'the phone shows the same inspector controls');
  test.setTimeout(15 * 60_000);
  const clip = await uploadClip(page);
  const music = await uploadAsset(
    page,
    resolve(AUDIO_FIXTURES, 'music.wav'),
    'music.wav',
    'audio/wav',
    { use: 'media' },
  );
  const speech = await uploadAsset(
    page,
    resolve(AUDIO_FIXTURES, 'speech.wav'),
    'speech.wav',
    'audio/wav',
    { use: 'media' },
  );
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
        {
          id: 'src-s',
          assetId: speech,
          kind: 'audio',
          duration: 12.905,
          size: { width: 0, height: 0 },
        },
      ],
      audio: [
        { id: 'lane-a', items: [item('bed', 'src-m', 1)] },
        { id: 'lane-b', items: [item('voice', 'src-s', 0.01)] },
      ],
    },
    clip,
  );
  await expect(editor.getByRole('button', { name: 'Sound 1' })).toBeVisible({ timeout: 60_000 });
  await editor.getByRole('button', { name: 'Sound 1' }).click();
  const inspector = editor.getByRole('region', { name: 'Properties' });
  await inspector.getByRole('tab', { name: 'Audio' }).click();
  const ducking = inspector.getByRole('switch', { name: 'Lower under speech' });
  await expect(ducking).toBeVisible();
  await ducking.click();
  await expect(inspector.getByRole('status')).toHaveText('Listening for speech…');
  await expect(inspector.getByRole('switch', { name: 'Lower under speech' })).toBeChecked({
    timeout: 5 * 60_000,
  });
  await expect(editor.getByRole('alert')).toHaveCount(0);
  await info.attach('ducking-inspector', {
    contentType: 'image/png',
    body: await page.screenshot(),
  });

  const powers = await windowPowers(
    page,
    readFileSync(await exportTo(page, editor, info, 'ducked.mp4')),
  );
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
  await info.attach('ducking-levels', {
    contentType: 'application/json',
    body: JSON.stringify(levels, null, 2),
  });
  expect(levels.depth).toBeGreaterThanOrEqual(10);
  expect(levels.depth).toBeLessThanOrEqual(14);
  expect(levels.ramp).toBeGreaterThanOrEqual(SOFTNESS - 0.2);
});
