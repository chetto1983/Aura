import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { expect, test } from './support/assetCleanup';
import { uploadAsset } from './support/assetUpload';
import { levelBetween, windowPowers } from './support/audioMeasure';
import { AUDIO_FIXTURES, exportTo, reopen, silentFilm, uploadClip } from './support/videoStudio';

// video-studio-denoise.spec.ts — Noise reduction proven by the file: the noisy-speech fixture
// (speech.wav plus pink noise at 10 dB SNR) is exported before and after the switch. S3 measured
// RNNoise dropping the gaps' floor by 37.7 dB on this file; the threshold is that minus 3 dB. The
// speech itself must stay where it was.

/** Inside the gaps between phrases, past RNNoise's release; inside the phrases, past their edges. */
function levels(powers: readonly number[]) {
  const mean = (a: number, b: number) => (a + b) / 2;
  return {
    gap: mean(levelBetween(powers, 3.9, 4.9), levelBetween(powers, 8.1, 9.1)),
    speech: mean(levelBetween(powers, 1.3, 3.4), levelBetween(powers, 5.5, 7.6)),
  };
}

test('noise reduction drops the noise between phrases and keeps the phrases', async ({
  page,
}, info) => {
  test.skip(info.project.name.startsWith('mobile'), 'the phone shows the same inspector switch');
  test.setTimeout(15 * 60_000);
  const clip = await uploadClip(page);
  const noisy = await uploadAsset(
    page,
    resolve(AUDIO_FIXTURES, 'speech-noisy.wav'),
    'speech-noisy.wav',
    'audio/wav',
    { use: 'media' },
  );
  const film = silentFilm(clip, 'denoise check', 4);
  const editor = await reopen(
    page,
    {
      ...film,
      sources: [
        ...film.sources,
        {
          id: 'src-n',
          assetId: noisy,
          kind: 'audio',
          duration: 12.905,
          size: { width: 0, height: 0 },
        },
      ],
      audio: [
        {
          id: 'lane-a',
          items: [
            {
              id: 'voice',
              sourceId: 'src-n',
              anchor: { clipId: 'clip-1', offset: 0 },
              sourceStart: 0,
              duration: 12.905,
              volume: 1,
              muted: false,
            },
          ],
        },
      ],
    },
    clip,
  );
  await expect(editor.getByRole('button', { name: 'Sound 1' })).toBeVisible({ timeout: 60_000 });
  const before = levels(
    await windowPowers(page, readFileSync(await exportTo(page, editor, info, 'noisy.mp4'))),
  );

  await editor.getByRole('button', { name: 'Sound 1' }).click();
  const inspector = editor.getByRole('region', { name: 'Properties' });
  await inspector.getByRole('tab', { name: 'Audio' }).click();
  const denoise = inspector.getByRole('switch', { name: 'Noise reduction' });
  await expect(denoise).toBeVisible();
  await denoise.click();
  await expect(inspector.getByRole('status')).toHaveText('Cleaning the sound…');
  await expect(inspector.getByRole('switch', { name: 'Noise reduction' })).toBeChecked({
    timeout: 5 * 60_000,
  });
  await expect(editor.getByRole('alert')).toHaveCount(0);
  await info.attach('denoise-inspector', {
    contentType: 'image/png',
    body: await page.screenshot(),
  });

  const after = levels(
    await windowPowers(page, readFileSync(await exportTo(page, editor, info, 'clean.mp4'))),
  );
  await info.attach('denoise-levels', {
    contentType: 'application/json',
    body: JSON.stringify({ before, after }, null, 2),
  });
  expect(before.gap - after.gap).toBeGreaterThanOrEqual(34.7);
  expect(Math.abs(before.speech - after.speech)).toBeLessThan(2);
});
