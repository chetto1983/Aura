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
  await info.attach('envelope-waveform', {
    contentType: 'image/png',
    body: await page.screenshot(),
  });

  // A point three quarters in, away from the trim handles over the edges, pulled to the floor.
  const box = await waveform.boundingBox();
  if (box === null) throw new Error('the waveform has no box');
  const x = box.x + box.width * 0.75;
  await page.mouse.dblclick(x, box.y + box.height / 2);
  const points = waveform.locator('ellipse');
  await expect(points).toHaveCount(3);
  // The new point is the one under the double-click, whatever order the plugin draws them in.
  const centre = (one: { x: number; width: number }) => one.x + one.width / 2;
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
  // The music plays at −21.24 dBFS in an export, not its source's −18.24: the mono bed is panned
  // into stereo (measured by video-studio-lane.spec.ts, 2026-09-27). The envelope now falls from
  // full gain at 0 s to the floor at 6 s, so the first second loses 1.6 dB to that slope (−22.8
  // dBFS) and the dip far more.
  expect(levels.start).toBeGreaterThan(-24);
  expect(levels.start - levels.dip).toBeGreaterThan(10);
});
