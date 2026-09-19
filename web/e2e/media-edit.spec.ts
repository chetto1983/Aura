import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { expect, test, type Page } from '@playwright/test';
import { ALL_FORMATS, FilePathSource, Input } from 'mediabunny';
import type { StudioRecord } from '../src/studio/studioApi';
import { gotoAuthenticated } from './auth';

// The editors against the real `aura serve` of the E2E suite: the fixture bytes are uploaded
// through the real asset routes and read back through the real download route. Only the Studio's
// own routes are stubbed, because a CI deployment has no OpenRouter key and serves the Studio as
// unwired (503); the stubs are what a wired Studio answers.

const FIXTURES = resolve(dirname(fileURLToPath(import.meta.url)), 'fixtures/media-edit');

async function upload(page: Page, file: string, mimeType: string): Promise<string> {
  const bytes = readFileSync(resolve(FIXTURES, file)).toString('base64');
  return page.evaluate(
    async ({ bytes, file, mimeType }) => {
      const body = Uint8Array.from(atob(bytes), (c) => c.charCodeAt(0));
      const presign = await fetch('/api/assets/presign', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          thread_id: '',
          file_name: file,
          mime_type: mimeType,
          size_bytes: body.byteLength,
          modality_hint: 'unknown',
        }),
      });
      if (!presign.ok) throw new Error(`presign: HTTP ${String(presign.status)}`);
      const { asset, upload } = (await presign.json()) as {
        asset: { id: string };
        upload: { upload_url: string; required_headers?: Record<string, string> };
      };
      const put = await fetch(upload.upload_url, {
        method: 'PUT',
        headers: upload.required_headers ?? {},
        body,
      });
      if (!put.ok) throw new Error(`put: HTTP ${String(put.status)}`);
      const done = await fetch(`/api/assets/${asset.id}/finalize`, { method: 'POST' });
      if (!done.ok) throw new Error(`finalize: HTTP ${String(done.status)}`);
      return asset.id;
    },
    { bytes, file, mimeType },
  );
}

function record(kind: 'image' | 'video', assetId: string): StudioRecord {
  return {
    id: `rec-${kind}`,
    kind,
    status: 'completed',
    model: 'test/model',
    prompt: `media edit ${kind}`,
    used: {},
    asset_id: assetId,
    created_at: new Date().toISOString(),
  };
}

async function openStudioWith(page: Page, rec: StudioRecord, library: 'ok' | 'off') {
  await page.route('**/api/studio/models?**', (route) =>
    route.fulfill({
      json: { default: 'test/model', models: [{ id: 'test/model', audio: false, seed: false }] },
    }),
  );
  await page.route('**/api/studio/history?**', (route) =>
    route.fulfill({ json: { records: [rec] } }),
  );
  await page.route('**/api/studio/library**', (route) =>
    library === 'ok'
      ? route.fulfill({ json: { assets: [] } })
      : route.fulfill({ status: 503, body: 'studio unavailable' }),
  );
  // With one record the stage shows it unpicked; the history is a drawer over the stage on a phone.
  await page.addInitScript(() => {
    window.localStorage.setItem('aura.shell.surface', 'studio');
  });
  await gotoAuthenticated(page, '/');
}

test.describe('media editing', () => {
  // On a phone the Studio composer covers the stage's Download/Edit/Reuse row once a tall result
  // has loaded, and Filerobot moves its tabs behind a menu: the desktop layout is the one proven.
  test.skip(
    ({ isMobile }) => isMobile,
    'the phone Studio stage hides its actions under the composer',
  );

  test('trims a clip on the copy path and downloads the cut', async ({ page }, info) => {
    await gotoAuthenticated(page, '/');
    const assetId = await upload(page, 'clip.mp4', 'video/mp4');
    await openStudioWith(page, record('video', assetId), 'ok');
    await page.getByRole('button', { name: 'Edit', exact: true }).click();
    const editor = page.getByRole('dialog', { name: 'Edit clip.mp4' });
    await expect(editor.getByRole('slider', { name: 'Start of the selection' })).toBeVisible({
      timeout: 30_000,
    });
    await editor.getByLabel('Start', { exact: true }).fill('1');
    await editor.getByLabel('Start', { exact: true }).blur();
    await editor.getByLabel('End', { exact: true }).fill('3');
    await editor.getByLabel('End', { exact: true }).blur();
    const downloading = page.waitForEvent('download');
    await editor.getByRole('button', { name: 'Save' }).click();
    const download = await downloading;
    expect(download.suggestedFilename()).toBe('clip-edited.mp4');
    const path = info.outputPath('clip-edited.mp4');
    await download.saveAs(path);
    const input = new Input({ source: new FilePathSource(path), formats: ALL_FORMATS });
    const duration = await input.computeDuration();
    input.dispose();
    expect(duration).toBeGreaterThan(1.9);
    expect(duration).toBeLessThan(2.2);
  });

  test('saves an edited photo to the Studio library', async ({ page }) => {
    await gotoAuthenticated(page, '/');
    const assetId = await upload(page, 'photo.png', 'image/png');
    let finalized = false;
    await page.route('**/api/studio/uploads/*/finalize', (route) => {
      finalized = true;
      return route.fulfill({
        json: { id: 'lib-1', file_name: 'photo-edited.png', mime_type: 'image/png' },
      });
    });
    await openStudioWith(page, record('image', assetId), 'ok');
    await page.getByRole('button', { name: 'Edit', exact: true }).click();
    const editor = page.getByRole('dialog', { name: 'Edit photo.png' });
    await expect(editor.getByText('Filters', { exact: true })).toBeVisible({ timeout: 30_000 });
    await editor.getByText('Filters', { exact: true }).click();
    await editor.getByText('Sepia', { exact: true }).click();
    await editor.getByRole('button', { name: 'Save to library' }).click();
    await expect(editor.getByText('Saved to the Studio library')).toBeVisible();
    expect(finalized).toBe(true);
  });

  test('keeps Download when the Studio is not active', async ({ page }) => {
    await gotoAuthenticated(page, '/');
    const assetId = await upload(page, 'photo.png', 'image/png');
    await openStudioWith(page, record('image', assetId), 'off');
    await page.getByRole('button', { name: 'Edit', exact: true }).click();
    const editor = page.getByRole('dialog', { name: 'Edit photo.png' });
    await expect(
      editor.getByText('The Studio is not active: download the photo instead.'),
    ).toBeVisible({ timeout: 30_000 });
    await expect(editor.getByRole('button', { name: 'Save to library' })).toBeDisabled();
    const downloading = page.waitForEvent('download');
    await editor.getByRole('button', { name: 'Download' }).click();
    expect((await downloading).suggestedFilename()).toBe('photo-edited.png');
  });
});
