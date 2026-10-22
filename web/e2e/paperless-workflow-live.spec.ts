import { randomUUID } from 'node:crypto';
import { expect, test } from '@playwright/test';
import type { SavedView } from '../src/documents/workspaceApi';
import { api, ensureLabel, type LiveRow } from './documentsLive';
import {
  fixturePDF,
  fixtureWAV,
  indexed,
  indexingTimeout,
  openPaperless,
  cleanFiles,
  uploadSources,
} from './paperlessFixtures';

test.use({ trace: 'off', video: 'off', screenshot: 'off', actionTimeout: 20_000 });
test.describe('Paperless document workflow on the real backend', () => {
  test.skip(
    process.env.AURA_E2E_LIVE_DOCUMENTS !== '1',
    'requires an explicitly selected live stack',
  );
  test.describe.configure({ mode: 'serial' });

  for (const width of [1440, 390]) {
    test(`upload, content search, review and durable saved view at ${String(width)}px`, async ({
      page,
      browser,
      baseURL,
    }, testInfo) => {
      test.setTimeout(8 * 60_000);
      if (!baseURL) throw new Error('Live baseURL is required');
      await page.setViewportSize({ width, height: 900 });
      const run = randomUUID().replaceAll('-', '');
      const token = `CedarAcceptance${run}`;
      const pdfName = `acceptance-${run}.pdf`;
      const codeName = `acceptance-${run}.rs`;
      const audioName = `acceptance-${run}.wav`;
      const labelName = `Acceptance ${run}`;
      const viewName = `View ${run}`;
      const names = [pdfName, codeName, audioName];
      let label = '';
      let view: SavedView | undefined;
      await openPaperless(page);
      try {
        label = await ensureLabel(page, 'type', labelName);
        await page.locator('input[type=file]').setInputFiles([
          {
            name: pdfName,
            mimeType: 'application/pdf',
            buffer: fixturePDF(`Contract ${token} payment thirty days`),
          },
          {
            name: codeName,
            mimeType: 'text/plain',
            buffer: Buffer.from(`// ${token}\nfn main() { println!("synthetic acceptance"); }\n`),
          },
          { name: audioName, mimeType: 'audio/wav', buffer: fixtureWAV() },
        ]);
        const queue = page.getByRole('listitem').filter({ hasText: pdfName });
        await expect(queue.getByRole('status')).toHaveText('Stored — waiting for indexing', {
          timeout: 30_000,
        });
        await expect.poll(async () => (await uploadSources(page, names)).length).toBe(names.length);
        await page.reload();
        await expect(queue).toBeVisible();
        await expect(queue.getByRole('status')).toHaveText('Indexed', { timeout: indexingTimeout });
        const pdf = await indexed(page, pdfName);
        const code = await indexed(page, codeName);
        const audio = await indexed(page, audioName);
        expect([pdf.file_type, code.file_type, audio.file_type]).toEqual(['pdf', 'code', 'audio']);

        await page.goto('/documents/library');
        const search = page.getByRole('searchbox', { name: 'Search documents' });
        await search.fill(token);
        const result = page.getByRole('button', { name: pdfName, exact: true });
        await expect(result).toBeVisible();
        await expect(page.getByRole('button', { name: codeName, exact: true })).toHaveCount(0);
        const filenameOnly = await api<{ rows: LiveRow[] }>(
          page,
          'GET',
          `/api/documents?search_mode=name&q=${token}`,
        );
        expect(filenameOnly.rows.some((row) => row.id === pdf.id)).toBe(false);
        expect(new URL(page.url()).searchParams.get('q')).toBe(token);
        await page.getByRole('button', { name: 'Save this view', exact: true }).click();
        await page.getByRole('textbox', { name: 'View name' }).fill(viewName);
        await page.getByRole('button', { name: 'Save', exact: true }).click();
        await expect
          .poll(async () => {
            view = (
              await api<{ views: SavedView[] }>(page, 'GET', '/api/documents/views')
            ).views.find((item) => item.name === viewName);
            return view !== undefined;
          })
          .toBe(true);
        await result.click();
        const direct = page.url();
        await expect(page.getByRole('heading', { name: pdfName, exact: true })).toBeVisible();
        await page.goBack();
        await expect(search).toHaveValue(token);
        await expect(result).toBeVisible();
        await result.click();
        if (width === 390) await page.getByRole('tab', { name: 'Preview', exact: true }).click();
        await expect(page.locator(`iframe[title="${pdfName}"]`)).toBeVisible();
        await expect(page.locator(`iframe[title="${pdfName}"]`)).toHaveAttribute(
          'src',
          '/apps/pdfcraft/?host=parent',
        );
        await page.getByRole('tab', { name: 'Content', exact: true }).click();
        await expect(
          page.getByRole('tabpanel').filter({ hasText: 'Indexed text may omit' }),
        ).toContainText(token);
        await page.getByRole('tab', { name: 'Details', exact: true }).click();
        await page.getByRole('combobox', { name: 'Type', exact: true }).click();
        await page.getByRole('option', { name: labelName, exact: true }).click();
        await page.getByLabel('Document date', { exact: true }).fill('2026-10-22');
        await page.getByRole('button', { name: 'Save details', exact: true }).click();
        await expect(
          page.getByRole('button', { name: 'Save details', exact: true }),
        ).toBeDisabled();
        const reviewResponse = page.waitForResponse(
          (response) =>
            response.request().method() === 'POST' &&
            new URL(response.url()).pathname === `/api/documents/${pdf.id}/review`,
        );
        await page.getByRole('button', { name: 'Reviewed, save and next', exact: true }).click();
        expect((await reviewResponse).status()).toBe(200);
        const reviewed = await api<LiveRow>(page, 'GET', `/api/documents/${pdf.id}`);
        expect(reviewed.reviewed_at).toBeTruthy();
        expect(reviewed.values).toEqual(
          expect.arrayContaining([
            expect.objectContaining({ field: 'type', value_key: label }),
            expect.objectContaining({ field: 'document_date', value_key: '2026-10-22' }),
          ]),
        );
        await page.goto('/documents/views');
        await page.reload();
        await page
          .getByRole('listitem')
          .filter({ has: page.locator(`input[value="${viewName}"]`) })
          .getByRole('link', { name: 'Open view', exact: true })
          .click();
        await expect(search).toHaveValue(token);
        await expect(result).toBeVisible();
        await page.goto(direct);
        await page.reload();
        await expect(page.getByRole('combobox', { name: 'Type', exact: true })).toContainText(
          labelName,
        );
        await page
          .getByRole('region', { name: pdfName, exact: true })
          .getByRole('button', { name: 'Documents', exact: true })
          .click();
        await expect(search).toHaveValue(token);
        await expect(result).toBeVisible();
        const libraryShot = testInfo.outputPath(`${String(width)}-library.png`);
        await page.screenshot({ path: libraryShot });
        await testInfo.attach(`${String(width)}-library`, {
          path: libraryShot,
          contentType: 'image/png',
        });
        const freshContext = await browser.newContext({
          baseURL,
          storageState: await page.context().storageState(),
          viewport: { width, height: 900 },
          serviceWorkers: 'block',
          ignoreHTTPSErrors: true,
        });
        try {
          const freshPage = await freshContext.newPage();
          await freshPage.goto('/documents/views');
          await freshPage
            .getByRole('listitem')
            .filter({ has: freshPage.locator(`input[value="${viewName}"]`) })
            .getByRole('link', { name: 'Open view', exact: true })
            .click();
          await expect(freshPage.getByRole('searchbox', { name: 'Search documents' })).toHaveValue(
            token,
          );
          await expect(freshPage.getByRole('button', { name: pdfName, exact: true })).toBeVisible();
        } finally {
          await freshContext.close();
        }

        await page.goto(`/documents/library?file_type=code&search_mode=name&q=${codeName}`);
        await page.getByRole('button', { name: codeName, exact: true }).click();
        if (width === 390) await page.getByRole('tab', { name: 'Preview', exact: true }).click();
        await expect(page.locator('pre')).toContainText('fn main()');
        await expect(page.getByRole('button', { name: /save (file|changes)/i })).toHaveCount(0);
        await page.goto(`/documents/library?file_type=audio&search_mode=name&q=${audioName}`);
        await page.getByRole('button', { name: audioName, exact: true }).click();
        if (width === 390) await page.getByRole('tab', { name: 'Preview', exact: true }).click();
        const player = page.locator('audio');
        const play = page.getByRole('button', { name: `Play ${audioName}`, exact: true });
        await expect(play).toBeVisible();
        await expect
          .poll(() => player.evaluate((element: HTMLAudioElement) => element.duration))
          .toBeCloseTo(1, 1);
        await play.click();
        await expect
          .poll(() => player.evaluate((element: HTMLAudioElement) => element.currentTime))
          .toBeGreaterThan(0);
        await page.getByRole('button', { name: `Pause ${audioName}`, exact: true }).click();
        const audioShot = testInfo.outputPath(`${String(width)}-audio-preview.png`);
        await page.screenshot({ path: audioShot });
        await testInfo.attach(`${String(width)}-audio-preview`, {
          path: audioShot,
          contentType: 'image/png',
        });
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
          true,
        );
      } finally {
        await cleanFiles(page, await uploadSources(page, names));
        if (view) await api(page, 'DELETE', `/api/documents/views/${view.id}`);
        if (label) await api(page, 'DELETE', `/api/documents/labels/${label}`);
      }
    });
  }
});
