import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { expect, test } from '@playwright/test';
import { gotoAuthenticated } from './auth';
import { deleteAssets, readAsset, trackCreatedAssets } from './support/assetCleanup';
import { uploadAsset } from './support/assetUpload';

// video-studio-audio.spec.ts — the audio half of the editor against a running Aura: here, that a
// sound the editor stores is filed with the media and never sent to speech-to-text. The claim is
// only worth something against a control, so the same file also goes through the plain finalize,
// and the test waits until that one has visibly been processed.

const AUDIO = resolve(dirname(fileURLToPath(import.meta.url)), 'fixtures/video-studio/audio');

test.describe('audio sources in the library', () => {
  test('an editor sound is filed under media and never transcribed', async ({ page }) => {
    test.setTimeout(5 * 60_000);
    const created = trackCreatedAssets(page);
    await gotoAuthenticated(page, '/');
    try {
      const control = await uploadAsset(
        page,
        resolve(AUDIO, 'music.wav'),
        'music.wav',
        'audio/wav',
      );
      const media = await uploadAsset(page, resolve(AUDIO, 'music.wav'), 'music.wav', 'audio/wav', {
        use: 'media',
      });

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

      const refused = await page.evaluate(async () => {
        const presign = await fetch('/api/assets/presign', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            thread_id: '',
            file_name: 'manual.pdf',
            mime_type: 'application/pdf',
            size_bytes: 9,
            modality_hint: 'unknown',
          }),
        });
        const { asset, upload } = (await presign.json()) as {
          asset: { id: string };
          upload: { upload_url: string; required_headers?: Record<string, string> };
        };
        await fetch(upload.upload_url, {
          method: 'PUT',
          headers: upload.required_headers ?? {},
          body: '%PDF test',
        });
        return (await fetch(`/api/assets/${asset.id}/finalize?use=media`, { method: 'POST' }))
          .status;
      });
      expect(refused).toBe(400);
    } finally {
      await deleteAssets(page, created.ids());
    }
  });
});
