import { randomUUID } from 'node:crypto';
import { readFileSync } from 'node:fs';
import type { Locator, Page, TestInfo } from '@playwright/test';
import { expect, test } from './support/assetCleanup';
import { levelBetween, windowPowers } from './support/audioMeasure';
import { exportTo, reopen, setField, uploadClip } from './support/videoStudio';

// video-studio-extract.spec.ts — Extract audio, proven by the file: the clip goes quiet and its
// tone is still in the export, carried by the new sound; trimming that sound's first second then
// leaves that second silent while the picture plays on.

const FRAME = { width: 320, height: 180 };

async function levels(page: Page, editor: Locator, info: TestInfo, name: string) {
  const powers = await windowPowers(page, readFileSync(await exportTo(page, editor, info, name)));
  return { first: levelBetween(powers, 0.1, 0.9), rest: levelBetween(powers, 1.5, 3.5) };
}

test('an extracted sound carries the clip tone, and its trim is heard', async ({ page }, info) => {
  test.skip(
    info.project.name.startsWith('mobile'),
    'one export pair is enough; the controls are the same on a phone',
  );
  test.setTimeout(15 * 60_000);
  const clip = await uploadClip(page);
  const project = {
    id: randomUUID(),
    name: 'extract check',
    size: FRAME,
    fps: 30,
    sources: [
      { id: 'src-a', assetId: clip, kind: 'video', duration: 4, size: FRAME, hasAudio: true },
    ],
    video: [{ id: 'clip-1', sourceId: 'src-a', duration: 4, sourceStart: 0, muted: false }],
    overlays: [],
  };
  const editor = await reopen(page, project, clip);
  await editor.getByRole('button', { name: 'Clip 1' }).click();
  const inspector = editor.getByRole('region', { name: 'Properties' });
  await inspector.getByRole('tab', { name: 'Audio' }).click();
  await inspector.getByRole('button', { name: 'Extract audio' }).click();
  await expect(editor.getByRole('button', { name: 'Sound 1' })).toBeVisible();
  await info.attach('extract-editor', { contentType: 'image/png', body: await page.screenshot() });

  const extracted = await levels(page, editor, info, 'extract.mp4');

  // The new sound is selected; its In moves one second into the tone.
  await inspector.getByRole('tab', { name: 'Time' }).click();
  await setField(inspector, 'In', '00:01.0');
  const trimmed = await levels(page, editor, info, 'extract-trimmed.mp4');

  // And the clip really is muted: its own switch says so.
  await editor.getByRole('button', { name: 'Clip 1' }).click();
  await inspector.getByRole('tab', { name: 'Audio' }).click();
  await expect(inspector.getByRole('switch', { name: 'Mute' })).toBeChecked();

  await info.attach('extract-levels', {
    contentType: 'application/json',
    body: JSON.stringify({ extracted, trimmed }, null, 2),
  });
  expect(extracted.first).toBeGreaterThan(-40);
  expect(extracted.rest).toBeGreaterThan(-40);
  expect(trimmed.first).toBeLessThan(-60);
  expect(Math.abs(trimmed.rest - extracted.rest)).toBeLessThan(1);
});
