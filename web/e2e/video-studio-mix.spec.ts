import { randomUUID } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import type { Page, TestInfo } from '@playwright/test';
import { expect, test } from './support/assetCleanup';
import { uploadAsset } from './support/assetUpload';
import { levelBetween, windowPowers } from './support/audioMeasure';
import {
  AUDIO_FIXTURES,
  containerFacts,
  exportTo,
  reopen,
  uploadClip,
} from './support/videoStudio';

// video-studio-mix.spec.ts — what an export sounds like. Each project is written as a file and
// opened the way a reload opens one, so these measure the compile and the mixer alone: every
// volume here reaches the MP4 only through the keyframes videoflow_audio.ts writes (S1).

const FRAME = { width: 320, height: 180 };

function film(clip: string, name: string, volumes: readonly [number, number], muted: boolean) {
  return {
    id: randomUUID(),
    name,
    size: FRAME,
    fps: 30,
    sources: [
      { id: 'src-a', assetId: clip, kind: 'video', duration: 4, size: FRAME, hasAudio: true },
    ],
    video: volumes.map((volume, index) => ({
      id: `clip-${String(index + 1)}`,
      sourceId: 'src-a',
      duration: 4,
      sourceStart: 0,
      muted,
      volume,
    })),
    overlays: [],
  };
}

async function exportLevels(
  page: Page,
  project: object,
  clip: string,
  info: TestInfo,
  name: string,
) {
  const editor = await reopen(page, project, clip);
  await expect(editor.getByRole('button', { name: 'Clip 2' })).toBeVisible({ timeout: 60_000 });
  const path = await exportTo(page, editor, info, name);
  return {
    facts: await containerFacts(path),
    powers: await windowPowers(page, readFileSync(path)),
  };
}

test.describe('the mix', () => {
  test('music at 50 % sits 6 dB under 100 %, fades in from silence and stops with the film', async ({
    page,
  }, info) => {
    test.setTimeout(12 * 60_000);
    const clip = await uploadClip(page);
    const music = await uploadAsset(
      page,
      resolve(AUDIO_FIXTURES, 'music.wav'),
      'music.wav',
      'audio/wav',
      { use: 'media' },
    );
    const base = film(clip, 'mix music check', [1, 1], true);
    const bed = { sourceId: 'src-m', sourceStart: 0, muted: false };
    const project = {
      ...base,
      sources: [
        ...base.sources,
        { id: 'src-m', assetId: music, kind: 'audio', duration: 8, size: { width: 0, height: 0 } },
      ],
      audio: [
        {
          id: 'lane-1',
          items: [
            {
              ...bed,
              id: 'soft',
              anchor: { clipId: 'clip-1', offset: 0 },
              duration: 4,
              volume: 0.5,
              fadeIn: 2,
            },
            // Eight seconds of music hung on a clip that starts at 4 s: the film cuts it at 8.
            { ...bed, id: 'full', anchor: { clipId: 'clip-2', offset: 0 }, duration: 8, volume: 1 },
          ],
        },
      ],
    };
    const { facts, powers } = await exportLevels(page, project, clip, info, 'mix-music.mp4');
    const levels = {
      start: levelBetween(powers, 0, 0.1),
      soft: levelBetween(powers, 2.5, 3.5),
      full: levelBetween(powers, 4.5, 7.5),
    };
    await info.attach('mix-music-levels', {
      contentType: 'application/json',
      body: JSON.stringify({ facts, levels }, null, 2),
    });
    expect(facts.duration).toBeGreaterThan(7.9);
    expect(facts.duration).toBeLessThan(8.1);
    expect(levels.full - levels.soft).toBeGreaterThan(5);
    expect(levels.full - levels.soft).toBeLessThan(7);
    expect(levels.soft - levels.start).toBeGreaterThanOrEqual(15);
  });

  test('a clip at 50 % sits 6 dB under the same clip at 100 %', async ({ page }, info) => {
    test.setTimeout(12 * 60_000);
    const clip = await uploadClip(page);
    const project = film(clip, 'mix clip volume check', [0.5, 1], false);
    const { powers } = await exportLevels(page, project, clip, info, 'mix-clip.mp4');
    const levels = { half: levelBetween(powers, 0.5, 3.5), full: levelBetween(powers, 4.5, 7.5) };
    await info.attach('mix-clip-levels', {
      contentType: 'application/json',
      body: JSON.stringify(levels, null, 2),
    });
    expect(levels.full - levels.half).toBeGreaterThan(5);
    expect(levels.full - levels.half).toBeLessThan(7);
  });
});
