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

async function validateConversationRailResize(
  page: import('@playwright/test').Page,
  testInfo: import('@playwright/test').TestInfo,
) {
  const resizeHandle = page.getByRole('separator', { name: 'Resize conversation sidebar' });
  const viewportWidth = page.viewportSize()?.width ?? 0;
  if (viewportWidth < 1024) {
    await expect(resizeHandle).toHaveCount(0);
    await testInfo.attach('mobile-chat-shell-no-resizer.png', {
      body: await page.screenshot(),
      contentType: 'image/png',
    });
    return;
  }

  await expect(resizeHandle).toBeVisible();
  const navigationRail = page.locator('#chat-navigation');
  await expect(navigationRail).toBeVisible();

  const before = await navigationRail.boundingBox();
  const handleBox = await resizeHandle.boundingBox();
  expect(before).not.toBeNull();
  expect(handleBox).not.toBeNull();
  if (before === null || handleBox === null) return;

  await resizeHandle.focus();
  await page.keyboard.press('ArrowRight');
  await expect
    .poll(async () => (await navigationRail.boundingBox())?.width ?? 0)
    .toBeGreaterThan(before.width + 20);
  const keyboardWidth = (await navigationRail.boundingBox())?.width ?? before.width;

  const dragBox = await resizeHandle.boundingBox();
  expect(dragBox).not.toBeNull();
  if (dragBox === null) return;
  await page.mouse.move(dragBox.x + dragBox.width / 2, dragBox.y + dragBox.height / 2);
  await page.mouse.down();
  await page.mouse.move(dragBox.x + dragBox.width / 2 + 96, dragBox.y + dragBox.height / 2, {
    steps: 6,
  });
  await page.mouse.up();

  await expect
    .poll(async () => (await navigationRail.boundingBox())?.width ?? 0)
    .toBeGreaterThan(before.width + 70);
  const resizedWidth = (await navigationRail.boundingBox())?.width ?? keyboardWidth;

  await testInfo.attach('desktop-chat-shell-resized.png', {
    body: await page.screenshot(),
    contentType: 'image/png',
  });

  await page.reload();
  await expect(page.getByRole('img', { name: /aura/i })).toBeVisible({ timeout: 15000 });
  await page.getByRole('button', { name: 'Chat', exact: true }).first().click();
  await expect(resizeHandle).toBeVisible();
  await expect
    .poll(async () => (await navigationRail.boundingBox())?.width ?? 0)
    .toBeGreaterThan(resizedWidth - 12);

  await testInfo.attach('desktop-chat-shell-resized-after-reload.png', {
    body: await page.screenshot(),
    contentType: 'image/png',
  });
}

async function expectChatComposerDocked(page: import('@playwright/test').Page) {
  const chatRegion = page.getByRole('region', { name: 'Chat' });
  const composerInput = page.getByRole('textbox', { name: 'Ask Aura' });
  await expect(chatRegion).toBeVisible();
  await expect(composerInput).toBeVisible();

  await expect
    .poll(async () => {
      const chatBox = await chatRegion.boundingBox();
      const mainBox = await page.locator('main').boundingBox();
      const inputBox = await composerInput.boundingBox();
      if (chatBox === null || mainBox === null || inputBox === null) {
        return Number.POSITIVE_INFINITY;
      }
      const chatMainGap = Math.abs(mainBox.y + mainBox.height - (chatBox.y + chatBox.height));
      const composerChatGap = chatBox.y + chatBox.height - (inputBox.y + inputBox.height);
      return Math.max(chatMainGap, composerChatGap);
    })
    .toBeLessThan(96);
}

async function expectDialogCentered(
  page: import('@playwright/test').Page,
  dialog: import('@playwright/test').Locator,
) {
  await expect
    .poll(async () =>
      dialog.evaluate((node) => {
        const portal = node.parentElement === document.body;
        const outsideSideNav = node.closest('.shell-side-nav') === null;
        return portal && outsideSideNav;
      }),
    )
    .toBe(true);

  const box = await dialog.boundingBox();
  const viewport = page.viewportSize();
  expect(box).not.toBeNull();
  expect(viewport).not.toBeNull();
  if (box === null || viewport === null) return;

  const centerX = box.x + box.width / 2;
  const centerY = box.y + box.height / 2;
  expect(Math.abs(centerX - viewport.width / 2)).toBeLessThan(24);
  expect(Math.abs(centerY - viewport.height / 2)).toBeLessThan(48);
  await expect
    .poll(async () =>
      dialog.evaluate((node) => {
        const rect = node.getBoundingClientRect();
        const topElement = document.elementFromPoint(
          rect.left + rect.width / 2,
          rect.top + rect.height / 2,
        );
        return topElement !== null && node.contains(topElement);
      }),
    )
    .toBe(true);
  await expect
    .poll(async () => dialog.evaluate((node) => getComputedStyle(node).opacity))
    .toBe('1');
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
    await installShellUiRoutes(page);
    await gotoAuthenticated(page, '/');
    await expect(page.getByRole('banner').getByText('Aura', { exact: true }).first()).toBeVisible({
      timeout: 15000,
    });
    const body = (await page.locator('body').innerText()).toLowerCase();
    for (const phrase of MARKETING_HERO_BLOCKLIST) {
      expect(body).not.toContain(phrase);
    }
  });

  test('shows logout in the layout and returns to login', async ({ page }) => {
    await installShellUiRoutes(page);
    await gotoAuthenticated(page, '/');

    const signOut = page.getByRole('button', { name: 'Sign out' });
    await expect(signOut).toBeVisible({ timeout: 15000 });
    await signOut.click();

    await expect(page).toHaveURL(/\/login(?:[?#]|$)/);
  });

  test('conversation menu floats above panes and navigation is chat-only', async ({
    page,
  }, testInfo) => {
    await installShellUiRoutes(page);
    await gotoAuthenticated(page, '/');
    await expect(page.getByRole('banner').getByText('Aura', { exact: true }).first()).toBeVisible({
      timeout: 15000,
    });
    await page.getByRole('button', { name: 'Chat', exact: true }).first().click();
    await expectChatComposerDocked(page);
    await validateConversationRailResize(page, testInfo);

    const openNavigation = page.getByRole('button', { name: 'Open navigation' });
    if (await openNavigation.isVisible()) {
      await openNavigation.click();
      await expect(page.getByRole('dialog', { name: 'Aura' })).toBeVisible();
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

    await menu.getByRole('menuitem', { name: 'Delete permanently' }).click();
    const deleteDialog = page.getByRole('dialog', { name: 'Delete conversation?' });
    await expect(deleteDialog).toBeVisible();
    await expect(deleteDialog.getByRole('button', { name: 'Keep conversation' })).toBeFocused();
    await expectDialogCentered(page, deleteDialog);
    await testInfo.attach('conversation-delete-dialog-centered.png', {
      body: await page.screenshot(),
      contentType: 'image/png',
    });
    await deleteDialog.getByRole('button', { name: 'Keep conversation' }).click();
    await expect(deleteDialog).toHaveCount(0);

    const navigationDialog = page.getByRole('dialog', { name: 'Aura' });
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
