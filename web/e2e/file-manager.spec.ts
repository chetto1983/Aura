import { expect, test, type Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';

// E2E of the corpus browser, which is SVAR React File Manager driven by
// GET /api/filemanager/files. It replaces the two document-library specs, whose subject —
// a hand-written catalog workspace — no longer exists.
//
// The listing is mocked so the assertions never depend on seeded data, but everything
// downstream of it is real: the live cockpit container renders the actual widget, and the
// folder descent below only passes if request-data/provide-data genuinely round-trips.

const root = [
  { id: '/contabilita', type: 'folder', lazy: true },
  { id: '/listino-2026.pdf', type: 'file', size: 1204233, date: '2026-07-19T16:40:00Z' },
];

const contabilita = [
  { id: '/contabilita/fattura-acme.pdf', type: 'file', size: 22083, date: '2026-06-11T10:00:00Z' },
];

async function mockListing(page: Page): Promise<void> {
  await page.route(
    (url) => url.pathname.startsWith('/api/filemanager/files'),
    async (route) => {
      const path = new URL(route.request().url()).pathname;
      const prefix = '/api/filemanager/files/';
      const body = path.startsWith(prefix)
        ? decodeURIComponent(path.slice(prefix.length)) === '/contabilita'
          ? contabilita
          : []
        : root;
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(body),
      });
    },
  );
}

async function openFiles(page: Page, projectName: string): Promise<void> {
  await mockListing(page);
  await gotoAuthenticated(page, '/');
  const nav =
    projectName === 'chrome'
      ? page.getByRole('navigation', { name: /Primary|Principale/ })
      : page.getByRole('navigation', { name: /Modes|Modalit/ });
  await nav.getByRole('button', { name: /Documents|Documenti/ }).click();
  await expect(page.getByRole('region', { name: /Documents|Documenti/ })).toBeVisible();
}

test('the file manager lists the bucket root', async ({ page }, testInfo) => {
  await openFiles(page, testInfo.project.name);

  await expect(page.getByText('contabilita', { exact: true }).first()).toBeVisible();
  await expect(page.getByText('listino-2026.pdf', { exact: true }).first()).toBeVisible();
  // No assertion on the widget's own toolbar controls: this spec proves the MOUNT — that
  // rows come back and render — and asserting a searchbox by role was a guess copied from
  // the workspace this replaced, which had its own labelled input. It failed on both
  // projects while the rows rendered fine.
});

test('descending into a folder loads it on demand', async ({ page }, testInfo) => {
  await openFiles(page, testInfo.project.name);

  // The root payload carries no child of /contabilita, so this row can only appear if the
  // widget asked for the folder and the workspace answered with provide-data.
  // scrollIntoViewIfNeeded first: on the mobile project the card sits below the fold and
  // the dblclick retried until timeout against an element outside the viewport.
  const folder = page.getByText('contabilita', { exact: true }).first();
  await folder.scrollIntoViewIfNeeded();
  await folder.dblclick();
  await expect(page.getByText('fattura-acme.pdf', { exact: true }).first()).toBeVisible();
});
