import { randomUUID } from 'node:crypto';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import type { Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';
import { expect, readAsset, test } from './support/assetCleanup';
import { finalizeStored, storeBytes, uploadAsset, uploadBytes } from './support/assetUpload';
import { FIXTURES, openStudioWith } from './support/videoStudio';

// video-studio-audio.spec.ts — the audio half of the editor against a running Aura: here, that a
// sound the editor stores is filed with the media and never sent to speech-to-text. The claim is
// only worth something against a control, so the same file also goes through the plain finalize,
// and the test waits until that one has visibly been processed.

const AUDIO = resolve(dirname(fileURLToPath(import.meta.url)), 'fixtures/video-studio/audio');

test.describe('audio sources in the library', () => {
  test('an editor sound is filed under media and never transcribed', async ({ page }) => {
    test.setTimeout(5 * 60_000);
    await gotoAuthenticated(page, '/');
    // The media upload goes FIRST. Jobs are claimed oldest first, so a job the media finalize
    // wrongly queued would run before the control's: by the time the control is processed, the
    // media row could no longer read `accepted`.
    const media = await uploadAsset(page, resolve(AUDIO, 'music.wav'), 'music.wav', 'audio/wav', {
      use: 'media',
    });
    const control = await uploadAsset(page, resolve(AUDIO, 'music.wav'), 'music.wav', 'audio/wav');

    // The worker demonstrably runs: the plain upload leaves `accepted` for processing and beyond.
    await expect
      .poll(async () => (await readAsset(page, control)).status, {
        timeout: 180_000,
        intervals: [2_000],
      })
      .not.toMatch(/^(accepted|processing)$/);

    const row = await readAsset(page, media);
    expect(row.modality).toBe('audio');
    expect(row.object_key.startsWith('media/'), row.object_key).toBe(true);
    expect(row.status).toBe('accepted');
    expect(row.summary).toBe('');
  });

  for (const [what, stored] of [
    ['a document', { fileName: 'manual.pdf', mimeType: 'application/pdf' }],
    [
      'a document hinted as a sound',
      { fileName: 'manual.pdf', mimeType: 'audio/wav', hint: 'audio' as const },
    ],
  ] as const) {
    test(`the media door refuses ${what}`, async ({ page }) => {
      test.setTimeout(2 * 60_000);
      await gotoAuthenticated(page, '/');
      const id = await storeBytes(page, Buffer.from('%PDF test'), stored);
      const refused = await finalizeStored(page, id, '?use=media');
      expect(refused.status).toBe(400);
      expect(refused.body).toContain('not of the expected modality');
      const row = await readAsset(page, id);
      expect(row.status).not.toMatch(/^(accepted|processing|complete)$/);
      expect(row.document_id ?? '').toBe('');
    });
  }
});

/** A project in the shape saved before audio existed: no `audio` key at all. */
function legacyProject(clipAsset: string, name: string) {
  return {
    id: randomUUID(),
    name,
    size: { width: 320, height: 180 },
    fps: 30,
    sources: [
      {
        id: 'src-a',
        assetId: clipAsset,
        kind: 'video',
        duration: 4,
        size: { width: 320, height: 180 },
        hasAudio: true,
      },
    ],
    video: [{ id: 'clip-1', sourceId: 'src-a', duration: 4, sourceStart: 0, muted: false }],
    overlays: [],
  };
}

/** Puts a project file in the library and opens the Studio on it, the way a reload does. */
async function reopen(page: Page, project: object, clipAsset: string) {
  const fileId = await uploadBytes(
    page,
    Buffer.from(JSON.stringify(project)),
    'project.json',
    'application/json',
  );
  await page.addInitScript((id) => {
    window.localStorage.setItem('aura.videoStudio.lastSavedProject', id);
  }, fileId);
  await openStudioWith(page, clipAsset, 'audio lane check');
  await page.getByRole('button', { name: 'Reopen the last project you saved here' }).click();
  return page.getByRole('dialog', { name: 'Video editor' });
}

async function uploadClip(page: Page): Promise<string> {
  await gotoAuthenticated(page, '/');
  return uploadAsset(page, resolve(FIXTURES, 'clip-a.mp4'), 'clip-a.mp4', 'video/mp4', {
    use: 'media',
  });
}

test.describe('the project file with audio lanes', () => {
  test('a project saved before audio existed opens unchanged', async ({ page }) => {
    test.setTimeout(3 * 60_000);
    const clip = await uploadClip(page);
    const editor = await reopen(page, legacyProject(clip, 'legacy audio check'), clip);
    await expect(editor.getByRole('button', { name: 'Clip 1' })).toBeVisible({ timeout: 60_000 });
    await expect(
      editor.getByRole('heading', { name: 'legacy audio check', includeHidden: true }),
    ).toBeAttached();
    await expect(editor.getByRole('alert')).toHaveCount(0);
  });

  test('an audio lane naming a missing source is refused at load', async ({ page }) => {
    test.setTimeout(3 * 60_000);
    const clip = await uploadClip(page);
    const broken = {
      ...legacyProject(clip, 'broken audio check'),
      audio: [
        {
          id: 'audio-1',
          items: [
            {
              id: 'bed',
              sourceId: 'src-gone',
              anchor: { clipId: 'clip-1', offset: 0 },
              sourceStart: 0,
              duration: 2,
              volume: 1,
              muted: false,
            },
          ],
        },
      ],
    };
    const editor = await reopen(page, broken, clip);
    await expect(editor.getByRole('alert')).toBeVisible({ timeout: 60_000 });
    await expect(editor.getByRole('button', { name: 'Clip 1' })).toHaveCount(0);
  });
});
