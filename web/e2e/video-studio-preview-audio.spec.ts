import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { gotoAuthenticated } from './auth';
import { expect, test } from './support/assetCleanup';
import { uploadAsset } from './support/assetUpload';
import { levelBetween, windowPowers } from './support/audioMeasure';
import { exportTo, FIXTURES, openStudioWith } from './support/videoStudio';

test('a Garage clip plays its sound and exports video plus standalone WAV', async ({
  page,
}, info) => {
  test.setTimeout(2 * 60_000);
  await page.addInitScript(() => {
    const audible: HTMLAudioElement[] = [];
    (window as typeof window & { __previewAudio?: HTMLAudioElement[] }).__previewAudio = audible;
    const play = Reflect.get(HTMLMediaElement.prototype, 'play');
    HTMLMediaElement.prototype.play = function () {
      if (this instanceof HTMLAudioElement && this.src.startsWith('blob:')) audible.push(this);
      return play.call(this);
    };
  });

  await gotoAuthenticated(page, '/');
  const source = resolve(FIXTURES, 'clip-a.mp4');
  const name = `preview-audio-${String(Date.now())}.mp4`;
  const assetId = await uploadAsset(page, source, name, 'video/mp4', { use: 'media' });
  await openStudioWith(page, assetId, 'clip sound check');
  await page.getByRole('button', { name: 'Open Garage library' }).first().click();
  const listing = page
    .getByRole('dialog', { name: 'Choose from Garage' })
    .locator('[data-id=":body"]')
    .first();
  await listing.getByText('media', { exact: true }).dblclick();
  await listing.getByText(name, { exact: true }).dblclick();
  const editor = page.getByRole('dialog', { name: 'Video editor' });
  await expect(editor.getByRole('button', { name: 'Clip 1' })).toBeVisible({ timeout: 60_000 });
  await editor.getByRole('button', { name: 'Play the project' }).click();
  await expect
    .poll(
      () =>
        page.evaluate(() =>
          (window as typeof window & { __previewAudio?: HTMLAudioElement[] }).__previewAudio?.some(
            (audio) => !audio.paused && audio.currentTime > 0,
          ),
        ),
      { timeout: 15_000 },
    )
    .toBe(true);
  await editor.getByRole('button', { name: 'Pause the project' }).click();

  const output = await exportTo(page, editor, info, 'clip-sound.mp4');
  const originalLevel = levelBetween(await windowPowers(page, readFileSync(source)), 1, 3);
  const exportedLevel = levelBetween(await windowPowers(page, readFileSync(output)), 1, 3);
  expect(Math.abs(exportedLevel - originalLevel)).toBeLessThan(1);

  const downloading = page.waitForEvent('download', { timeout: 60_000 });
  await editor.getByRole('button', { name: 'Export audio (WAV)' }).click();
  const audio = await downloading;
  expect(audio.suggestedFilename()).toMatch(/\.wav$/);
  const audioPath = info.outputPath('clip-sound.wav');
  await audio.saveAs(audioPath);
  const wav = readFileSync(audioPath);
  expect(wav.toString('ascii', 0, 4)).toBe('RIFF');
  const audioLevel = levelBetween(await windowPowers(page, wav), 1, 3);
  expect(Math.abs(audioLevel - originalLevel)).toBeLessThan(1);
});
