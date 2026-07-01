import { expect, test, type Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';

// E2E validation of the Document Library size column (defect surfaced 2026-07-01,
// fixed the same day).
//
// Original symptom (reporter screenshot): every row EXCEPT the auto-selected first
// row rendered "-" in the Size column even though the file had a real size.
//
// Root cause: GET /api/documents (handleDocumentList → ListDocuments) returned
// catalog items with NO version/size; DocumentsWorkspace only knew the size of the
// one document whose detail it had fetched, so every other row fell back to "-".
//
// Fix: ListDocuments now denormalizes the active version's `active_size_bytes` +
// `active_content_type` onto each list row (one batched query, no N+1), and
// DocumentFileList renders that per-row size directly. These tests mock the two
// document endpoints so the assertions never depend on seeded data, and drive the
// LIVE cockpit container (AURA_E2E_ORIGIN) end to end.

interface RawDoc {
  id: string;
  identity_id: string;
  scope: 'library';
  title: string;
  tags: string[];
  metadata: Record<string, unknown>;
  active_version_id: string;
  status: 'ready';
  created_at: string;
  updated_at: string;
  active_size_bytes?: number;
  active_content_type?: string;
}

interface DocFixture {
  readonly id: string;
  readonly title: string;
  readonly day: string;
  // sizeBytes 0 models a document with no active version → row must show "-".
  readonly sizeBytes: number;
  // formatBytes() output for sizeBytes; "-" when there is no active version.
  readonly expectedSize: string;
}

// ALPHA is the first (auto-selected) row; BETA is a non-selected row with a real
// size; GAMMA has no active version, so its row must still render "-".
const ALPHA: DocFixture = {
  id: 'doc-a',
  title: 'Alpha.pdf',
  day: '30',
  sizeBytes: 18022,
  expectedSize: '17.6 KB',
};
const BETA: DocFixture = {
  id: 'doc-b',
  title: 'Beta.pdf',
  day: '29',
  sizeBytes: 4096,
  expectedSize: '4 KB',
};
const GAMMA: DocFixture = {
  id: 'doc-c',
  title: 'Gamma.pdf',
  day: '28',
  sizeBytes: 0,
  expectedSize: '-',
};
const FIXTURES: readonly DocFixture[] = [ALPHA, BETA, GAMMA];

function rawDoc(fixture: DocFixture): RawDoc {
  const base: RawDoc = {
    id: fixture.id,
    identity_id: 'operator',
    scope: 'library',
    title: fixture.title,
    tags: [],
    metadata: {},
    active_version_id: fixture.sizeBytes > 0 ? `${fixture.id}-v1` : '',
    status: 'ready',
    created_at: `2026-06-${fixture.day}T00:00:00Z`,
    updated_at: `2026-06-${fixture.day}T00:00:00Z`,
  };
  if (fixture.sizeBytes > 0) {
    base.active_size_bytes = fixture.sizeBytes;
    base.active_content_type = 'application/pdf';
  }
  return base;
}

const LIST: readonly RawDoc[] = FIXTURES.map(rawDoc);

function detailFor(id: string) {
  const fixture = FIXTURES.find((item) => item.id === id) ?? ALPHA;
  return {
    document: rawDoc(fixture),
    versions: [
      {
        id: `${fixture.id}-v1`,
        document_id: fixture.id,
        version_number: 1,
        status: 'ready',
        sha256: fixture.id,
        content_type: 'application/pdf',
        size_bytes: fixture.sizeBytes,
        storage_object_id: `obj-${fixture.id}`,
      },
    ],
  };
}

async function mockDocumentApi(page: Page) {
  await page.route('**/api/documents**', async (route) => {
    const path = new URL(route.request().url()).pathname;
    const detail = /\/api\/documents\/([^/]+)$/.exec(path);
    if (detail) {
      const id = decodeURIComponent(detail[1] ?? '');
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(detailFor(id)),
      });
    }
    if (path.endsWith('/api/documents')) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(LIST),
      });
    }
    return route.continue();
  });
}

// The Size column is `hidden sm:table-cell` — it only renders at >= 640px, so the
// assertions below apply to the desktop (chromium) profile, not the 393px mobile
// profiles where the column is intentionally absent.
function skipUnlessDesktop(projectName: string) {
  test.skip(
    projectName !== 'chromium',
    'Size column is hidden below the sm breakpoint (mobile profiles)',
  );
}

async function openDocuments(page: Page, projectName: string) {
  const nav =
    projectName === 'chromium'
      ? page.getByRole('navigation', { name: /Primary|Principale/ })
      : page.getByRole('navigation', { name: /Modes|Modalit/ });
  await nav.getByRole('button', { name: /Documents|Documenti/ }).click();
  await expect(
    page.getByRole('heading', { name: /Document library|Libreria documenti/ }),
  ).toBeVisible();
}

test('every document row shows its file size, not just the selected row', async ({
  page,
}, testInfo) => {
  skipUnlessDesktop(testInfo.project.name);

  await gotoAuthenticated(page, '/');
  await mockDocumentApi(page);
  await openDocuments(page, testInfo.project.name);

  const alphaRow = page.getByRole('row').filter({ hasText: ALPHA.title });
  const betaRow = page.getByRole('row').filter({ hasText: BETA.title });
  await expect(alphaRow).toBeVisible();
  await expect(betaRow).toBeVisible();

  // Selected (first) row shows its size from the loaded detail...
  await expect(alphaRow).toContainText(ALPHA.expectedSize);
  // ...and the non-selected row now shows its size from the denormalized list field.
  await expect(betaRow).toContainText(BETA.expectedSize);
});

test('a document with no active version still renders "-" for size', async ({ page }, testInfo) => {
  skipUnlessDesktop(testInfo.project.name);

  await gotoAuthenticated(page, '/');
  await mockDocumentApi(page);
  await openDocuments(page, testInfo.project.name);

  const gammaRow = page.getByRole('row').filter({ hasText: GAMMA.title });
  await expect(gammaRow).toBeVisible();
  await expect(gammaRow).toContainText('-');
  await expect(gammaRow).not.toContainText('KB');
});

test('the grid view toggle switches the list to a card layout', async ({ page }, testInfo) => {
  skipUnlessDesktop(testInfo.project.name);

  await gotoAuthenticated(page, '/');
  await mockDocumentApi(page);
  await openDocuments(page, testInfo.project.name);

  // List view is a table by default.
  await expect(page.getByRole('table')).toBeVisible();

  await page.getByRole('button', { name: /Grid view|Vista griglia/ }).click();

  // Grid view drops the table for a card list carrying the same facts (title + size).
  await expect(page.getByRole('table')).toHaveCount(0);
  const card = page.getByRole('listitem').filter({ hasText: BETA.title });
  await expect(card).toBeVisible();
  await expect(card).toContainText(BETA.expectedSize);

  // Toggling back restores the table.
  await page.getByRole('button', { name: /List view|Vista elenco/ }).click();
  await expect(page.getByRole('table')).toBeVisible();
});

test('deleting a document removes its row (delete flow completes)', async ({ page }, testInfo) => {
  skipUnlessDesktop(testInfo.project.name);

  // Stateful mock: DELETE marks the id gone; subsequent list calls exclude it.
  const deleted = new Set<string>();
  await page.route('**/api/documents**', async (route) => {
    const req = route.request();
    const path = new URL(req.url()).pathname;
    const detailMatch = /\/api\/documents\/([^/]+)$/.exec(path);
    if (req.method() === 'DELETE' && detailMatch) {
      deleted.add(decodeURIComponent(detailMatch[1] ?? ''));
      return route.fulfill({ status: 200, contentType: 'application/json', body: '{}' });
    }
    if (detailMatch) {
      const id = decodeURIComponent(detailMatch[1] ?? '');
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(detailFor(id)),
      });
    }
    if (path.endsWith('/api/documents')) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(LIST.filter((d) => !deleted.has(d.id))),
      });
    }
    return route.continue();
  });

  await gotoAuthenticated(page, '/');
  await openDocuments(page, testInfo.project.name);

  const betaRow = page.getByRole('row').filter({ hasText: BETA.title });
  await expect(betaRow).toBeVisible();

  await betaRow.getByRole('button', { name: `Actions for ${BETA.title}` }).click();
  await page
    .getByRole('menu')
    .getByRole('menuitem', { name: /Delete document|Elimina documento/ })
    .click();

  const dialog = page.getByRole('dialog');
  await dialog.getByRole('textbox').fill('DELETE');
  await dialog.getByRole('button', { name: /Delete permanently|Elimina definitivamente/ }).click();

  // The row is gone after the delete + list refresh; the other row remains.
  await expect(page.getByRole('row').filter({ hasText: BETA.title })).toHaveCount(0);
  await expect(page.getByRole('row').filter({ hasText: ALPHA.title })).toBeVisible();
});
