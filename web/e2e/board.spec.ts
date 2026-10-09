import { expect, test, type Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';

// E2E of the work board against the real mount and the real database: nothing is routed. The
// widget is SVAR React Kanban driven by its own RestDataProvider, so what this proves is the
// pair the unit suite cannot: the browser's pointer drag reaching PUT .../move, and the board
// a reload brings back being the one the server stored.
//
// Every project signs in as the same operator and so shares one board. Each test therefore
// names its cards uniquely and removes them, and the one that edits columns runs on the
// desktop project only, because a column limit is board-wide state the other project would
// see change under it.

interface StoredCard {
  readonly id: string;
  readonly label: string;
  readonly column: string;
  readonly tags: readonly string[];
}

async function openBoard(page: Page, projectName: string): Promise<void> {
  await gotoAuthenticated(page, '/');
  const nav =
    projectName === 'chrome'
      ? page.getByRole('navigation', { name: /Primary|Principale/ })
      : page.getByRole('navigation', { name: /Modes|Modalit/ });
  await nav.getByRole('button', { name: /^(Board|Bacheca)$/ }).click();
  await expect(page.getByRole('region', { name: 'Kanban board' })).toBeVisible();
}

// In-page fetch on purpose: it carries the session cookie and passes through the cockpit's
// own Idempotency-Key wrapper, exactly as the widget's requests do.
function api<T>(page: Page, method: string, path: string, body?: unknown): Promise<T> {
  return page.evaluate(
    async ({ method, path, body }) => {
      const res = await fetch(path, {
        method,
        headers: { 'Content-Type': 'application/json' },
        ...(body === undefined ? {} : { body: JSON.stringify(body) }),
      });
      if (!res.ok) throw new Error(`${method} ${path}: HTTP ${String(res.status)}`);
      return (await res.json()) as T;
    },
    { method, path, body },
  );
}

async function removeCards(page: Page, prefix: string): Promise<void> {
  const cards = await api<StoredCard[]>(page, 'GET', '/api/board/cards');
  for (const card of cards.filter((c) => c.label.startsWith(prefix))) {
    await api(page, 'DELETE', `/api/board/cards/${card.id}`);
  }
}

function column(page: Page, id: string) {
  // setID prefixes a string id with ":"; the attribute is the widget's own column identity.
  return page.locator(`[data-kanban-column-cards=":${id}"]`);
}

test('a card added, renamed and dragged to Doing is where the server left it after a reload', async ({
  page,
}, testInfo) => {
  const label = `e2e drag ${testInfo.project.name} ${String(Date.now())}`;
  await openBoard(page, testInfo.project.name);
  try {
    const added = page.waitForResponse(
      (r) => r.url().endsWith('/api/board/cards') && r.request().method() === 'POST',
    );
    await page.getByRole('button', { name: 'Add card to To do' }).click();
    expect((await added).ok()).toBe(true);

    await expect(page.getByText('Discuss in chat')).toBeVisible();
    await page.getByRole('textbox', { name: /^Title/ }).fill(label);
    await page.getByRole('textbox', { name: 'Tags' }).fill('e2e, smoke');
    const renamed = page.waitForResponse(
      (r) => /\/api\/board\/cards\/[^/]+$/.test(r.url()) && r.request().method() === 'PUT',
    );
    await page.getByText('Save', { exact: true }).click();
    const rename = await renamed;
    expect(rename.ok()).toBe(true);
    expect(rename.request().postDataJSON()).toMatchObject({ label, tags: ['e2e', 'smoke'] });

    const card = column(page, 'todo').getByText(label, { exact: true });
    // A real board may already hold cards, so the new one can sit below the column's fold; a
    // pointer drag only starts on a card under the pointer.
    await card.scrollIntoViewIfNeeded();
    await expect(card).toBeInViewport();
    const from = await card.boundingBox();
    const to = await column(page, 'doing').boundingBox();
    if (from === null || to === null)
      throw new Error('the card or the Doing column is not laid out');
    const moved = page.waitForResponse(
      (r) => r.url().endsWith('/move') && r.request().method() === 'PUT',
    );
    await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2);
    await page.mouse.down();
    await page.mouse.move(to.x + to.width / 2, to.y + 40, { steps: 12 });
    await page.mouse.up();
    expect((await moved).ok()).toBe(true);

    await page.reload({ waitUntil: 'domcontentloaded' });
    await expect(column(page, 'doing').getByText(label, { exact: true })).toBeVisible();
    const stored = await api<StoredCard[]>(page, 'GET', '/api/board/cards');
    expect(stored.find((c) => c.label === label)).toMatchObject({
      column: 'doing',
      tags: ['e2e', 'smoke'],
    });
  } finally {
    await removeCards(page, label);
  }
});

test('a column over its limit is marked, and the cards stay', async ({ page }, testInfo) => {
  test.skip(
    testInfo.project.name !== 'chrome',
    'column limits are board-wide; one project owns them',
  );
  const prefix = `e2e limit ${String(Date.now())}`;
  await openBoard(page, testInfo.project.name);
  const before = await api<{ columns: { id: string; label: string; cardLimit?: number }[] }>(
    page,
    'GET',
    '/api/board',
  );
  try {
    for (const n of [1, 2]) {
      await api(page, 'POST', '/api/board/cards', {
        label: `${prefix} ${String(n)}`,
        column: 'done',
      });
    }
    await page.reload({ waitUntil: 'domcontentloaded' });
    await page.getByRole('button', { name: 'Columns' }).click();
    const dialog = page.getByRole('dialog', { name: 'Board columns' });
    const done = before.columns.findIndex((c) => c.id === 'done') + 1;
    await dialog.getByRole('textbox', { name: `Card limit of column ${String(done)}` }).fill('1');
    await dialog.getByRole('button', { name: 'Save columns' }).click();
    await expect(dialog).toBeHidden();

    const section = page.locator('section.wx-column').filter({ has: column(page, 'done') });
    await expect(section).toHaveClass(/wx-over-limit/);
    await expect(column(page, 'done').getByText(`${prefix} 2`, { exact: true })).toBeVisible();
  } finally {
    await api(page, 'PUT', '/api/board/columns', before.columns);
    await removeCards(page, prefix);
  }
});
