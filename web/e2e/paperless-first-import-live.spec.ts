import { randomUUID } from 'node:crypto';
import { writeFile } from 'node:fs/promises';
import { expect, test, type Page } from '@playwright/test';
import type { IngestionState } from '../src/documents/workspaceApi';
import { createIdentity, removeIdentity, signInAs, skipFirstRunSetup } from './identities';
import { cleanFiles, fixturePDF, indexingTimeout, openPaperless } from './paperlessFixtures';

test.use({ trace: 'off', video: 'off', screenshot: 'off' });

test('a first Files import refreshes an already mounted empty Library', async ({
  page,
  browser,
  baseURL,
}, testInfo) => {
  test.skip(
    process.env.AURA_E2E_LIVE_DOCUMENTS !== '1',
    'requires an explicitly selected live stack',
  );
  test.setTimeout(10 * 60_000);
  if (!baseURL) throw new Error('Live baseURL is required');
  const run = randomUUID();
  const email = `first-import-${run}@example.com`;
  const password = `Aa1!${randomUUID()}z`;
  const name = `first-import-${run}.pdf`;
  let created = false;
  let tenant: Page | undefined;
  let source: string | undefined;
  await openPaperless(page);
  try {
    await createIdentity(page, email, password);
    created = true;
    tenant = await signInAs(browser, baseURL, email, password);
    await skipFirstRunSetup(tenant);
    const initialResponse = tenant.waitForResponse(
      (response) => new URL(response.url()).pathname === '/api/documents/ingestion',
    );
    await tenant.goto('/documents/library?file_type=all&display=cards');
    const initial = (await (await initialResponse).json()) as { indexing: IngestionState | null };
    expect(initial.indexing).toBeNull();
    await expect(tenant.getByText('No documents match this view.', { exact: true })).toBeVisible();
    const mounted = await tenant.locator('[data-document-destination]').elementHandle();
    if (!mounted) throw new Error('Library did not mount');
    const libraryURL = tenant.url();
    let navigations = 0;
    tenant.on('framenavigated', () => navigations++);
    const completedResponse = tenant.waitForResponse(
      async (response) => {
        if (new URL(response.url()).pathname !== '/api/documents/ingestion') return false;
        const snapshot = (await response.json()) as { indexing: IngestionState | null };
        return snapshot.indexing !== null;
      },
      { timeout: indexingTimeout },
    );
    const uploaded = await tenant.evaluate(
      async ({ fileName, bytes }) => {
        const form = new FormData();
        form.append(
          'file',
          new File([new Uint8Array(bytes)], fileName, { type: 'application/pdf' }),
        );
        const response = await fetch('/api/filemanager/upload?id=%2F', {
          method: 'POST',
          credentials: 'same-origin',
          body: form,
        });
        return { status: response.status, body: (await response.json()) as { id: string } };
      },
      { fileName: name, bytes: [...fixturePDF(`First import ${run}`)] },
    );
    expect(uploaded.status).toBe(200);
    expect(uploaded.body.id).toBe(`/${name}`);
    source = uploaded.body.id.slice(1);
    const completed = (await (await completedResponse).json()) as { indexing: IngestionState };
    expect(completed.indexing).toMatchObject({ status: 'ready', in_progress: 0, errors: 0 });
    expect(completed.indexing.finished).toBeGreaterThan(0);
    await expect(tenant.getByText(name, { exact: true })).toBeVisible({ timeout: 10_000 });
    expect(tenant.url()).toBe(libraryURL);
    expect(navigations).toBe(0);
    expect(await mounted.evaluate((element) => element.isConnected)).toBe(true);
    const queue = await tenant.evaluate(() =>
      Object.keys(sessionStorage)
        .filter((key) => key.startsWith('aura.documents.uploads.v1.'))
        .flatMap((key) => JSON.parse(sessionStorage.getItem(key) ?? '[]') as unknown[]),
    );
    expect(queue).toEqual([]);
    const snapshotsPath = testInfo.outputPath('first-import-snapshots.json');
    await writeFile(
      snapshotsPath,
      JSON.stringify({ initial, completed, name, navigations, queue }, null, 2),
    );
    await testInfo.attach('first-import-snapshots', {
      path: snapshotsPath,
      contentType: 'application/json',
    });
    const screenshotPath = testInfo.outputPath('mounted-library.png');
    await tenant.screenshot({ path: screenshotPath });
    await testInfo.attach('mounted-library-after-first-import', {
      path: screenshotPath,
      contentType: 'image/png',
    });
  } finally {
    try {
      if (tenant && source) await cleanFiles(tenant, [source]);
    } finally {
      try {
        await tenant?.context().close();
      } finally {
        if (created) await removeIdentity(page, email);
      }
    }
  }
});
