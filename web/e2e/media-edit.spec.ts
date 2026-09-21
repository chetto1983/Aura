import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { expect, test, type Page } from '@playwright/test';
import { ALL_FORMATS, FilePathSource, Input } from 'mediabunny';
import type { StudioRecord } from '../src/studio/studioApi';
import { gotoAuthenticated } from './auth';
import { uploadAsset } from './support/assetUpload';

// The editors against the real `aura serve` of the E2E suite: the fixture bytes are uploaded
// through the real asset routes and read back through the real download route. Only the Studio's
// own routes and the agent's run are stubbed, because a CI deployment has no OpenRouter key: it
// serves the Studio as unwired (503) and cannot answer a turn. The stubs are what a wired
// deployment answers.

const FIXTURES = resolve(dirname(fileURLToPath(import.meta.url)), 'fixtures/media-edit');

function upload(page: Page, file: string, mimeType: string): Promise<string> {
  return uploadAsset(page, resolve(FIXTURES, file), file, mimeType);
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
  test('trims a clip in the unified editor and downloads the cut', async ({ page }, info) => {
    test.setTimeout(2 * 60_000);
    await gotoAuthenticated(page, '/');
    const assetId = await upload(page, 'clip.mp4', 'video/mp4');
    await openStudioWith(page, record('video', assetId), 'ok');
    await page.getByRole('button', { name: 'Edit', exact: true }).click();
    const editor = page.getByRole('dialog', { name: 'Video editor' });
    await expect(editor.getByRole('button', { name: 'Clip 1' })).toBeVisible({ timeout: 30_000 });
    await editor.getByRole('button', { name: 'Clip 1' }).click();
    await editor.getByRole('tab', { name: 'Time' }).click();
    await editor.getByLabel('Start', { exact: true }).fill('1');
    await editor.getByLabel('Start', { exact: true }).blur();
    await editor.getByLabel('End', { exact: true }).fill('3');
    await editor.getByLabel('End', { exact: true }).blur();
    const downloading = page.waitForEvent('download');
    await editor.getByRole('button', { name: 'Export', exact: true }).click();
    const download = await downloading;
    expect(download.suggestedFilename()).toBe('clip.mp4');
    const path = info.outputPath('clip.mp4');
    await download.saveAs(path);
    const input = new Input({ source: new FilePathSource(path), formats: ALL_FORMATS });
    const duration = await input.computeDuration();
    input.dispose();
    expect(duration).toBeGreaterThan(1.9);
    expect(duration).toBeLessThan(2.2);
  });

  test('applies Clideo-style clip controls and junction transitions', async ({
    page,
  }, testInfo) => {
    test.setTimeout(60_000);
    await gotoAuthenticated(page, '/');
    const assetId = await upload(page, 'clip.mp4', 'video/mp4');
    await openStudioWith(page, record('video', assetId), 'ok');
    await page.getByRole('button', { name: 'Edit', exact: true }).click();
    const editor = page.getByRole('dialog', { name: 'Video editor' });
    await expect(editor.getByRole('button', { name: 'Clip 1' })).toBeVisible({ timeout: 30_000 });
    const shell = editor.locator('.video-studio-shell');
    await page.evaluate(() => {
      document.documentElement.dataset.theme = 'light';
    });
    await expect(shell).toHaveCSS('background-color', 'rgb(248, 250, 251)');
    await expect(shell).toHaveCSS('color', 'rgb(23, 32, 38)');
    await page.evaluate(() => {
      document.documentElement.dataset.theme = 'dark';
    });
    await expect(shell).toHaveCSS('background-color', 'rgb(16, 18, 20)');
    await expect(shell).toHaveCSS('color', 'rgb(238, 242, 246)');

    const mobile = testInfo.project.name.startsWith('mobile');
    const mobileTools = editor.getByRole('navigation', { name: 'Mobile editing tools' });
    const properties = editor.locator('.video-studio-properties');
    const timeline = editor.locator('.video-studio-timeline-shell');
    async function openTool(
      name: 'Transform' | 'Adjust' | 'Audio' | 'Animations' | 'Speed' | 'Time',
    ) {
      if (mobile) {
        await mobileTools.getByRole('button', { name, exact: true }).click();
        await expect(properties).toHaveAttribute('data-mobile-open', 'true');
        await expect(properties).toBeVisible();
        await expect(timeline).toHaveAttribute('data-mobile-obscured', 'true');
      } else {
        await editor.getByRole('tab', { name, exact: true }).click();
      }
    }

    if (mobile) {
      await expect(mobileTools).toBeVisible();
      await expect(editor.getByRole('toolbar', { name: 'Editing commands' })).toBeHidden();
      const timelineBox = await editor.locator('.video-studio-timeline').boundingBox();
      const editorBox = await editor.boundingBox();
      if (timelineBox === null || editorBox === null) throw new Error('mobile timeline has no box');
      expect(timelineBox.x + timelineBox.width).toBeLessThanOrEqual(editorBox.x + editorBox.width);
    }

    await openTool('Transform');
    await editor.getByRole('radio', { name: 'Crop' }).click();
    await editor.getByRole('button', { name: '1:1' }).click();
    const stage = editor.getByTestId('video-stage');
    await expect
      .poll(async () => {
        const box = await stage.boundingBox();
        return box === null ? 0 : box.width / box.height;
      })
      .toBeCloseTo(1, 2);
    await editor.getByRole('button', { name: 'Rotate 90° right' }).click();
    await expect(editor.getByText('90°', { exact: true })).toBeVisible();

    await openTool('Adjust');
    const brightness = editor.getByLabel('Brightness').getByRole('slider');
    await brightness.press('End');
    await expect(brightness).toHaveAttribute('aria-valuenow', '100');

    await openTool('Audio');
    await editor.getByRole('switch', { name: 'Mute' }).click();
    await expect(editor.getByRole('switch', { name: 'Mute' })).toBeChecked();

    await openTool('Animations');
    await editor.getByRole('button', { name: 'Fade' }).click();
    await expect(editor.getByRole('button', { name: 'Fade' })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    const transitionDuration = editor.getByRole('group', { name: 'Duration' }).getByRole('slider');
    await expect(transitionDuration).toHaveAttribute('aria-valuenow', '1');
    await transitionDuration.press('End');
    await expect(transitionDuration).toHaveAttribute('aria-valuenow', '2');
    await editor.getByRole('button', { name: 'Blur' }).click();
    await expect(editor.getByRole('button', { name: 'Blur' })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    await openTool('Speed');
    await editor.getByRole('button', { name: '2×' }).click();
    if (mobile) {
      await mobileTools.getByRole('button', { name: 'Speed', exact: true }).click();
      await expect(properties).toHaveAttribute('data-mobile-open', 'false');
      await expect(timeline).toHaveAttribute('data-mobile-obscured', 'false');
    }
    await expect(editor.getByRole('button', { name: 'Clip 1' })).toHaveText('00:02.0');
    await openTool('Time');
    await expect(editor.getByLabel('End', { exact: true })).toBeVisible();
    if (mobile) {
      await mobileTools.getByRole('button', { name: 'Time', exact: true }).click();
      await expect(properties).toHaveAttribute('data-mobile-open', 'false');
      await expect(timeline).toHaveAttribute('data-mobile-obscured', 'false');
    }
    await expect(editor.getByRole('button', { name: 'Clip 1' })).toBeVisible();

    await editor.locator('input[type="file"]').setInputFiles(resolve(FIXTURES, 'clip.mp4'));
    await expect(editor.getByRole('button', { name: 'Clip 2' })).toBeVisible({ timeout: 30_000 });
    await editor.getByRole('button', { name: 'Transition between clips 1 and 2' }).click();
    await expect(editor.getByRole('heading', { name: 'Transition' })).toBeVisible();
    await editor.getByRole('button', { name: 'Crossfade', exact: true }).click();
    await expect(editor.getByRole('button', { name: 'Crossfade', exact: true })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    await expect(editor.getByRole('group', { name: 'Duration' })).toBeVisible();
    if (mobile) {
      await mobileTools.getByRole('button', { name: 'Close tool panel' }).click();
      await expect(timeline).toHaveAttribute('data-mobile-obscured', 'false');
    }
  });

  test('saves an edited photo to the Studio library', async ({ page }, testInfo) => {
    if (testInfo.project.name === 'chrome') {
      await page.setViewportSize({ width: 996, height: 800 });
    }
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
    const stage = page.locator('.studio-stage');
    const composerBox = await page.locator('.studio-composer').boundingBox();
    if (composerBox === null) throw new Error('the Studio composer has no layout box');
    for (const action of [
      stage.getByRole('link', { name: 'Download' }),
      stage.getByRole('button', { name: 'Edit', exact: true }),
      stage.getByRole('button', { name: 'Reuse' }),
    ]) {
      await expect(action).toBeVisible();
      const actionBox = await action.boundingBox();
      if (actionBox === null) throw new Error('a Studio action has no layout box');
      expect(actionBox.y + actionBox.height).toBeLessThanOrEqual(composerBox.y);
    }
    await stage.getByRole('button', { name: 'Edit', exact: true }).click();
    const editor = page.getByRole('dialog', { name: 'Edit photo.png' });
    // Below 760px Filerobot moves its tabs into a drawer behind the topbar's menu button.
    if (testInfo.project.name.startsWith('mobile')) {
      await editor.getByTestId('FIE-topbar-menu-button').click({ timeout: 30_000 });
    }
    await expect(editor.getByTestId('FIE-tab-adjust')).toHaveCSS(
      'background-color',
      'rgb(31, 55, 96)',
    );
    await expect(editor.getByTestId('FIE-tab-item-label-adjust')).toHaveCSS(
      'color',
      'rgb(211, 227, 253)',
    );
    await expect(editor.getByTestId('FIE-tab-filters')).toHaveCSS(
      'background-color',
      'rgb(48, 55, 65)',
    );
    await expect(editor.getByTestId('FIE-tab-item-label-filters')).toHaveCSS(
      'color',
      'rgb(238, 242, 246)',
    );
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
    const editor = page.getByRole('dialog', { name: 'Video editor' });
    await expect(editor.getByRole('button', { name: 'Clip 1' })).toBeVisible({ timeout: 30_000 });
  });
});
