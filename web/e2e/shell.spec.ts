import { expect, test } from '@playwright/test';
import { gotoAuthenticated } from './auth';

// RED until 23-03 wires `aura serve` to mount internal/webui at "/" over the real
// Vite build. Today the served bytes are the placeholder (or serve has no web mount
// yet), so the brand-visible + copy-contract assertions fail. That redness is the
// Wave-0 contract this spec turns green in 23-03.

// The marketing-hero copy the operator console must NOT ship (ux-spec §350 / SC4).
const MARKETING_HERO_BLOCKLIST = [
  'get started for free',
  'sign up today',
  'trusted by',
  'supercharge',
  'unlock your',
  'the future of',
];

async function installShellUiRoutes(page: import('@playwright/test').Page) {
  await page.route('**/api/conversations*', (route) => {
    if (route.request().url().includes('/rot-events')) {
      return route.fulfill({ status: 200, contentType: 'application/json', body: '[]' });
    }
    if (/\/api\/conversations\/[^/?]+$/.test(route.request().url())) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          ID: 'e2e-shell-chat',
          Title: 'E2E Shell Chat',
          TitleSet: true,
          IdentityID: 'operator',
          Status: 'active',
          Model: 'e2e',
          TotalInputTokens: 0,
          TotalOutputTokens: 0,
          TotalCachedTokens: 0,
          TotalCostUSD: 0,
          CreatedAt: '2026-07-01T10:00:00Z',
        }),
      });
    }
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([
        {
          ID: 'e2e-shell-chat',
          Title: 'E2E Shell Chat',
          TitleSet: true,
          IdentityID: 'operator',
          Status: 'active',
          Model: 'e2e',
          TotalInputTokens: 0,
          TotalOutputTokens: 0,
          TotalCachedTokens: 0,
          TotalCostUSD: 0,
          CreatedAt: '2026-07-01T10:00:00Z',
        },
      ]),
    });
  });
  await page.route('**/api/approvals', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  );
  await page.route('**/threads/*/messages', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ type: 'MESSAGES_SNAPSHOT', messages: [] }),
    }),
  );
  await page.route('**/api/graph/schema', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ labels: ['Entity'], rel_types: [] }),
    }),
  );
  await page.route('**/api/graph/query', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        nodes: [],
        edges: [],
        schema: { labels: ['Entity'], rel_types: [] },
        query: '',
      }),
    }),
  );
}

test.describe('embedded operator shell', () => {
  test('applies dark theme + operator density before paint', async ({ page }) => {
    await gotoAuthenticated(page, '/');
    const html = page.locator('html');
    // theme-before-paint contract (SC2): the attributes are present on the first
    // response HTML, set synchronously, so there is no flash of unstyled theme.
    await expect(html).toHaveAttribute('data-theme', 'dark');
    await expect(html).toHaveAttribute('data-density', /compact|operator|review/);
  });

  test('shows the Aura brand and no marketing-hero copy', async ({ page }) => {
    await gotoAuthenticated(page, '/');
    await expect(page.getByRole('img', { name: /aura/i })).toBeVisible();
    const body = (await page.locator('body').innerText()).toLowerCase();
    for (const phrase of MARKETING_HERO_BLOCKLIST) {
      expect(body).not.toContain(phrase);
    }
  });

  test('shows logout in the layout and returns to login', async ({ page }) => {
    await gotoAuthenticated(page, '/');

    const signOut = page.getByRole('button', { name: 'Sign out' });
    await expect(signOut).toBeVisible();
    await signOut.click();

    await expect(page).toHaveURL(/\/login(?:[?#]|$)/);
  });

  test('conversation menu floats above panes and navigation is chat-only', async ({ page }) => {
    await installShellUiRoutes(page);
    await gotoAuthenticated(page, '/');
    await expect(page.getByRole('img', { name: /aura/i })).toBeVisible({ timeout: 15000 });
    await page.getByRole('button', { name: 'Chat', exact: true }).first().click();

    const openNavigation = page.getByRole('button', { name: 'Open navigation' });
    if (await openNavigation.isVisible()) {
      await openNavigation.click();
      await expect(page.getByRole('dialog', { name: 'Navigation' })).toBeVisible();
    }

    const row = page.getByRole('button', { name: 'E2E Shell Chat' });
    await expect(row).toBeVisible({ timeout: 15000 });

    await row
      .locator('xpath=ancestor::li[1]')
      .getByRole('button', { name: 'Conversation actions' })
      .click();
    const menu = page.getByRole('menu', { name: 'Conversation actions' });
    await expect(menu).toBeVisible();
    await expect(menu.getByRole('menuitem', { name: 'Rename' })).toBeVisible();
    await expect(menu).toHaveCSS('position', 'fixed');
    await expect(menu).toHaveCSS('z-index', '70');
    await expect
      .poll(async () =>
        menu.evaluate(
          (node) => node.parentElement === document.body && !node.closest('.shell-side-nav'),
        ),
      )
      .toBe(true);

    const navigationDialog = page.getByRole('dialog', { name: 'Navigation' });
    if (await navigationDialog.isVisible()) {
      await navigationDialog.getByRole('button', { name: 'Close panel' }).click();
      await expect(navigationDialog).toHaveCount(0);
    }

    await page.getByRole('button', { name: 'Graph', exact: true }).first().click();
    await expect(page.getByTestId('graph-workspace')).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Conversations', exact: true })).toHaveCount(0);
    await expect(page.getByPlaceholder('Search conversations')).toHaveCount(0);
    await expect(page.locator('.shell-side-nav')).toHaveCount(0);
  });
});
