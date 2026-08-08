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

// The widget renders every entry TWICE — once in the sidebar tree, once in the file panel —
// so an unscoped getByText() is ambiguous and .first() resolves to the TREE node. On a narrow
// viewport SVAR parks that tree off-canvas (.wx-sidebar-narrow is position:absolute at
// x=-300, measured x=-222 for the row itself), which is unreachable: toBeVisible() still
// passes on it (Playwright's visibility check ignores the viewport) but dblclick can never
// satisfy actionability, so the spec burned its 30s timeout on mobile-chrome while the card
// sat plainly on screen at y=375. Desktop only passed because there the sidebar is on-screen.
//
// data-id=":body" is the widget's OWN identifier for the file panel — it marks the container
// in all three view modes (wx-cards / wx-list / wx-panels) and its click handler switches on
// "body" as a panel identity (dist/index.cjs; setID prefixes a literal ":"). Scoping to it
// selects the card the user actually touches, on both projects. `.first()` because the panel
// nests a second :body container inside the outer one.
function panelEntry(page: Page, name: string) {
  return page.locator('[data-id=":body"]').first().getByText(name, { exact: true });
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

  // toBeInViewport, not just toBeVisible: the assertion this replaces passed against the
  // off-canvas tree node, so it proved the row existed in the DOM and not that anyone could
  // see it. In-viewport is the claim the test is actually making.
  await expect(panelEntry(page, 'contabilita')).toBeInViewport();
  await expect(panelEntry(page, 'listino-2026.pdf')).toBeVisible();
  // No assertion on the widget's own toolbar controls: this spec proves the MOUNT — that
  // rows come back and render — and asserting a searchbox by role was a guess copied from
  // the workspace this replaced, which had its own labelled input. It failed on both
  // projects while the rows rendered fine.
});

test('descending into a folder loads it on demand', async ({ page }, testInfo) => {
  await openFiles(page, testInfo.project.name);

  // The root payload carries no child of /contabilita, so this row can only appear if the
  // widget asked for the folder and the workspace answered with provide-data.
  await panelEntry(page, 'contabilita').dblclick();
  await expect(panelEntry(page, 'fattura-acme.pdf')).toBeVisible();
});
