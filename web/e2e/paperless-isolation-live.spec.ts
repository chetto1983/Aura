import { randomUUID } from 'node:crypto';
import { expect, test, type Page } from '@playwright/test';
import type { SavedView } from '../src/documents/workspaceApi';
import { api } from './documentsLive';
import { createIdentity, removeIdentity, signInAs, skipFirstRunSetup } from './identities';
import { sameOriginFetch } from './live';
import {
  cleanFiles,
  fixturePDF,
  indexed,
  openPaperless,
  uploadSources,
  type Uploaded,
} from './paperlessFixtures';

test.use({ trace: 'off', video: 'off', screenshot: 'off' });

async function ownFixture(page: Page, token: string): Promise<{ doc: Uploaded; view: SavedView }> {
  await page.goto('/documents/processing');
  const name = `isolation-${token}.pdf`;
  await page.locator('input[type=file]').setInputFiles({
    name,
    mimeType: 'application/pdf',
    buffer: fixturePDF(`Private contract ${token}`),
  });
  const doc = await indexed(page, name);
  const { view } = await api<{ view: SavedView }>(page, 'POST', '/api/documents/views', {
    name: `Private ${token}`,
    version: 1,
    query: `q=${token}`,
    display: 'cards',
    pinned: true,
    dashboard: true,
  });
  return { doc, view };
}

async function owns(page: Page, doc: Uploaded, view: SavedView, token: string): Promise<void> {
  expect((await api<Uploaded>(page, 'GET', `/api/documents/${doc.id}`)).id).toBe(doc.id);
  const content = await api<{ passages: { text: string }[] }>(
    page,
    'GET',
    `/api/documents/${doc.id}/content`,
  );
  expect(content.passages.map((passage) => passage.text).join(' ')).toContain(token);
  const thumbnail = await page.evaluate(async (id) => {
    const response = await fetch(`/api/documents/${id}/thumbnail`, { credentials: 'same-origin' });
    return {
      status: response.status,
      type: response.headers.get('content-type'),
      size: (await response.arrayBuffer()).byteLength,
    };
  }, doc.id);
  expect(thumbnail.status).toBe(200);
  expect(thumbnail.type).toMatch(/^image\//);
  expect(thumbnail.size).toBeGreaterThan(100);
  expect(
    (await api<{ views: SavedView[] }>(page, 'GET', '/api/documents/views')).views,
  ).toContainEqual(view);
}

async function denied(page: Page, doc: Uploaded, view: SavedView, token: string): Promise<void> {
  for (const suffix of ['', '/content', '/thumbnail']) {
    const response = await sameOriginFetch(page, `/api/documents/${doc.id}${suffix}`);
    expect(response.status, `foreign document ${suffix || 'detail'}`).toBe(404);
    expect(response.text).not.toContain(token);
    expect(response.text).not.toContain(doc.file_name);
  }
  const views = await api<{ views: SavedView[] }>(page, 'GET', '/api/documents/views');
  expect(views.views.some((item) => item.id === view.id || item.name === view.name)).toBe(false);
  for (const method of ['PUT', 'DELETE']) {
    const response = await sameOriginFetch(page, `/api/documents/views/${view.id}`, {
      method,
      ...(method === 'PUT'
        ? {
            body: JSON.stringify({
              name: 'Unauthorized replacement',
              version: 1,
              query: '',
              display: 'table',
            }),
          }
        : {}),
    });
    expect(response.status, `foreign view ${method}`).toBe(404);
    expect(response.text).not.toContain(token);
  }
  const list = await api<{ rows: Uploaded[] }>(
    page,
    'GET',
    `/api/documents?file_type=all&q=${token}`,
  );
  expect(list.rows.some((row) => row.id === doc.id)).toBe(false);
}

test('documents, indexed text, thumbnails and views remain private between real identities', async ({
  page,
  browser,
  baseURL,
}) => {
  test.skip(
    process.env.AURA_E2E_LIVE_DOCUMENTS !== '1',
    'requires an explicitly selected live stack',
  );
  test.setTimeout(10 * 60_000);
  if (!baseURL) throw new Error('Live baseURL is required');
  const run = randomUUID().replaceAll('-', '');
  const email = `paperless-${run}@example.com`;
  const password = `Aa1!${randomUUID()}z`;
  const ownerToken = `Owner${run}`;
  const tenantToken = `Tenant${run}`;
  const ownerName = `isolation-${ownerToken}.pdf`;
  const tenantName = `isolation-${tenantToken}.pdf`;
  let created = false;
  let tenant: Page | undefined;
  let ownerFixture: Awaited<ReturnType<typeof ownFixture>> | undefined;
  let tenantFixture: Awaited<ReturnType<typeof ownFixture>> | undefined;
  await openPaperless(page);
  try {
    ownerFixture = await ownFixture(page, ownerToken);
    await owns(page, ownerFixture.doc, ownerFixture.view, ownerToken);
    await createIdentity(page, email, password);
    created = true;
    tenant = await signInAs(browser, baseURL, email, password);
    await skipFirstRunSetup(tenant);
    tenantFixture = await ownFixture(tenant, tenantToken);
    await owns(tenant, tenantFixture.doc, tenantFixture.view, tenantToken);
    await denied(tenant, ownerFixture.doc, ownerFixture.view, ownerToken);
    await denied(page, tenantFixture.doc, tenantFixture.view, tenantToken);
    // Foreign mutation attempts cannot change or remove either owner's saved view.
    await owns(page, ownerFixture.doc, ownerFixture.view, ownerToken);
    await owns(tenant, tenantFixture.doc, tenantFixture.view, tenantToken);
  } finally {
    try {
      if (tenant) {
        await cleanFiles(tenant, await uploadSources(tenant, [tenantName]));
        if (tenantFixture)
          await api(tenant, 'DELETE', `/api/documents/views/${tenantFixture.view.id}`);
      }
    } finally {
      await tenant?.context().close();
      try {
        if (created) await removeIdentity(page, email);
      } finally {
        await cleanFiles(page, await uploadSources(page, [ownerName]));
        if (ownerFixture) await api(page, 'DELETE', `/api/documents/views/${ownerFixture.view.id}`);
      }
    }
  }
});
