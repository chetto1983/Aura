import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { expect, test, type Page } from '@playwright/test';
import { ALL_FORMATS, FilePathSource, Input } from 'mediabunny';
import type { StudioRecord } from '../src/studio/studioApi';
import { gotoAuthenticated } from './auth';

// The editors against the real `aura serve` of the E2E suite: the fixture bytes are uploaded
// through the real asset routes and read back through the real download route. Only the Studio's
// own routes and the agent's run are stubbed, because a CI deployment has no OpenRouter key: it
// serves the Studio as unwired (503) and cannot answer a turn. The stubs are what a wired
// deployment answers.

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

interface Tone {
  readonly width: number;
  readonly height: number;
  readonly red: number;
  readonly blue: number;
  /** The share of pixels at red ≥ green ≥ blue, where a sepia matrix puts every pixel. */
  readonly warm: number;
}

/** Decodes image bytes in the page, which throws on anything that is not an image, and
 *  measures their colour. */
async function tone(page: Page, bytes: Buffer): Promise<Tone> {
  return page.evaluate(async (base64) => {
    const data = Uint8Array.from(atob(base64), (c) => c.charCodeAt(0));
    const bitmap = await createImageBitmap(new Blob([data]));
    const context = new OffscreenCanvas(bitmap.width, bitmap.height).getContext('2d');
    if (context === null) throw new Error('no 2d context');
    context.drawImage(bitmap, 0, 0);
    const { data: rgba } = context.getImageData(0, 0, bitmap.width, bitmap.height);
    let red = 0;
    let blue = 0;
    let warm = 0;
    for (let i = 0; i < rgba.length; i += 4) {
      const [r = 0, g = 0, b = 0] = rgba.subarray(i, i + 3);
      red += r;
      blue += b;
      if (r >= g && g >= b) warm += 1;
    }
    const pixels = rgba.length / 4;
    return {
      width: bitmap.width,
      height: bitmap.height,
      red: red / pixels,
      blue: blue / pixels,
      warm: warm / pixels,
    };
  }, bytes.toString('base64'));
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
  // A loaded square image is what pushes the stage's action row toward the phone's pinned
  // composer; tapping Edit before it arrives would race past that layout.
  if (rec.kind === 'image') {
    await expect(page.getByRole('img', { name: rec.prompt })).toBeVisible({ timeout: 30_000 });
  }
}

test.describe('media editing', () => {
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

  test('saves an edited photo to the Studio library', async ({ page }, testInfo) => {
    await gotoAuthenticated(page, '/');
    const assetId = await upload(page, 'photo.png', 'image/png');
    // The presign and the PUT to the object store are real. The PUT's body is the file the
    // editor exported; the Studio's finalize is stubbed, so the asset row never learns its size
    // and the download route could not hand those bytes back.
    const stored: Buffer[] = [];
    await page.route(/\.png\?/, (route) => {
      if (route.request().method() !== 'PUT') return route.fallback();
      const body = route.request().postDataBuffer();
      if (body !== null) stored.push(body);
      return route.continue();
    });
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
    // Below 760px Filerobot moves its tabs into a drawer behind the topbar's menu button.
    if (testInfo.project.name.startsWith('mobile')) {
      await editor.getByTestId('FIE-topbar-menu-button').click({ timeout: 30_000 });
    }
    await expect(editor.getByText('Filters', { exact: true })).toBeVisible({ timeout: 30_000 });
    await editor.getByText('Filters', { exact: true }).click();
    await editor.getByText('Sepia', { exact: true }).click();
    await editor.getByRole('button', { name: 'Save to library' }).click();
    await expect(editor.getByText('Saved to the Studio library')).toBeVisible();
    expect(finalized).toBe(true);
    const edited = stored.at(-1);
    if (edited === undefined) throw new Error('the edited photo never reached the object store');
    expect(edited.subarray(0, 8).toString('hex')).toBe('89504e470d0a1a0a');
    const after = await tone(page, edited);
    const before = await tone(page, readFileSync(resolve(FIXTURES, 'photo.png')));
    expect(after).toMatchObject({ width: 256, height: 256 });
    // Konva's sepia matrix leaves every pixel at red ≥ green ≥ blue; testsrc2's bars do not.
    expect(after.warm).toBeGreaterThan(0.95);
    expect(before.warm).toBeLessThan(0.8);
    expect(after.red - after.blue).toBeGreaterThan(before.red - before.blue);
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

  test('opens a clip attached in the chat from its attachment card', async ({ page }) => {
    // Held open: the turn stays in flight, so the operator's message and its card stay on screen.
    await page.route('**/agent/run', () => undefined);
    await gotoAuthenticated(page, '/');
    const composer = page.getByRole('textbox', { name: /Ask Aura/ });
    await expect(composer).toBeVisible({ timeout: 30_000 });
    const chooser = page.waitForEvent('filechooser');
    await page.getByRole('button', { name: 'Add files' }).click();
    await (await chooser).setFiles(resolve(FIXTURES, 'clip.mp4'));
    await composer.fill('here is the clip');
    // Send stays disabled until the upload is done.
    const send = page.getByRole('button', { name: 'Send message' });
    await expect(send).toBeEnabled({ timeout: 30_000 });
    await send.click();
    await page.getByRole('button', { name: 'Edit clip.mp4' }).click();
    const editor = page.getByRole('dialog', { name: 'Edit clip.mp4' });
    await expect(editor.getByRole('slider', { name: 'Start of the selection' })).toBeVisible({
      timeout: 30_000,
    });
  });
});
