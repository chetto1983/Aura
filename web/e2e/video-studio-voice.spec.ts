import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import type { Locator, Page, TestInfo } from '@playwright/test';
import { expect, test } from './support/assetCleanup';
import { levelBetween, windowPowers } from './support/audioMeasure';
import {
  AUDIO_FIXTURES,
  exportTo,
  pressAddAction,
  reopen,
  silentFilm,
  uploadClip,
} from './support/videoStudio';

// video-studio-voice.spec.ts — the two sounds the operator makes instead of picking: a text read
// aloud by Aura's voice, and a voice recorded through the microphone. Chromium's fake microphone
// plays speech.wav (1 s of silence, then a phrase until 3.66 s), so the recording is proven by the
// file: speech where the phrase is, and the silence before it still silent — the fake device's own
// beep, which it plays when it cannot read the file, would fill that second.

test.use({
  launchOptions: {
    args: [
      '--use-fake-device-for-media-stream',
      '--use-fake-ui-for-media-stream',
      `--use-file-for-fake-audio-capture=${resolve(AUDIO_FIXTURES, 'speech.wav')}`,
    ],
  },
});

/** Waits for the reopened film, then opens the rail's Add audio panel. The wait comes first:
 *  `pressAddAction` looks for the rail once, and an editor still loading has none. */
async function openAudioPanel(page: Page, editor: Locator): Promise<Locator> {
  await expect(editor.getByRole('button', { name: 'Clip 2' })).toBeVisible({ timeout: 60_000 });
  await pressAddAction(editor, 'Add audio');
  const panel = page.getByRole('dialog', { name: 'Add a sound' });
  await expect(panel).toBeVisible();
  return panel;
}

/** Where this Aura has no voice (CI), /api/tts answers with what the lab VM's voice said. */
async function voiceOrStandIn(page: Page, info: TestInfo) {
  const caps = await page.evaluate(async () => {
    const answer = await fetch('/api/voice/capabilities', { credentials: 'same-origin' });
    return (await answer.json()) as { tts?: boolean; stt?: boolean };
  });
  if (caps.tts === true) return;
  info.annotations.push({
    type: 'voice',
    description: 'no TTS configured here: /api/tts answered with speech-phrase.mp3',
  });
  await page.route('**/api/voice/capabilities', (route) =>
    route.fulfill({ json: { tts: true, stt: caps.stt === true } }),
  );
  await page.route('**/api/tts', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'audio/mpeg',
      body: readFileSync(resolve(AUDIO_FIXTURES, 'speech-phrase.mp3')),
    }),
  );
}

test('a text read aloud lands at the playhead and speaks in the export', async ({ page }, info) => {
  test.skip(info.project.name.startsWith('mobile'), 'the phone bar opens the same panel');
  test.setTimeout(10 * 60_000);
  const clip = await uploadClip(page);
  await voiceOrStandIn(page, info);
  const editor = await reopen(page, silentFilm(clip, 'speech check'), clip);
  const panel = await openAudioPanel(page, editor);
  await panel
    .getByLabel('Text to read aloud')
    .fill('The river runs past the old mill every morning.');
  await panel.getByRole('button', { name: 'Read it aloud' }).click();
  const sound = editor.getByRole('button', { name: 'Sound 1' });
  await expect(sound).toBeVisible({ timeout: 120_000 });
  await expect(sound).toContainText('The river runs past the old mill');
  await expect(panel).toHaveCount(0);
  await info.attach('speech-lane', { contentType: 'image/png', body: await page.screenshot() });

  const powers = await windowPowers(
    page,
    readFileSync(await exportTo(page, editor, info, 'speech.mp4')),
  );
  const levels = { speech: levelBetween(powers, 0.3, 2.3) };
  await info.attach('speech-levels', {
    contentType: 'application/json',
    body: JSON.stringify(levels, null, 2),
  });
  // The film is two muted clips: any sound in its first seconds is the voice's.
  expect(levels.speech).toBeGreaterThan(-40);
});

test('a voice recorded through the microphone lands on a lane and in the export', async ({
  page,
}, info) => {
  test.skip(info.project.name.startsWith('mobile'), 'the phone bar opens the same panel');
  test.setTimeout(10 * 60_000);
  const clip = await uploadClip(page);
  const editor = await reopen(page, silentFilm(clip, 'recording check'), clip);
  const panel = await openAudioPanel(page, editor);
  await panel.getByRole('button', { name: 'Record voice' }).click();
  const stop = panel.getByRole('button', { name: 'Stop and add' });
  await expect(stop).toBeVisible();
  // Four seconds of the fake microphone: speech.wav's lead silence and its first phrase.
  await page.waitForTimeout(4_000);
  await info.attach('recording-live', { contentType: 'image/png', body: await page.screenshot() });
  await stop.click();
  const sound = editor.getByRole('button', { name: 'Sound 1' });
  await expect(sound).toBeVisible({ timeout: 60_000 });
  await expect(sound).toContainText('Recording');
  await info.attach('recording-lane', { contentType: 'image/png', body: await page.screenshot() });

  const powers = await windowPowers(
    page,
    readFileSync(await exportTo(page, editor, info, 'recording.mp4')),
  );
  const levels = { lead: levelBetween(powers, 0.1, 0.8), phrase: levelBetween(powers, 1.2, 3.4) };
  await info.attach('recording-levels', {
    contentType: 'application/json',
    body: JSON.stringify(levels, null, 2),
  });
  expect(levels.phrase).toBeGreaterThan(-40);
  expect(levels.phrase - levels.lead).toBeGreaterThan(20);
});
