import { test, expect } from './fixtures';

/**
 * Phase 12 dashboard E2E.
 *
 * Covers the dashboard side of the live checklist
 * (docs/plans/2026-05-02-phase-12-e2e-checklist.md):
 * - Steps 2 (open dashboard) + 3 (sidebar nav).
 * - Step 5 (open /conversations + drawer).
 * - Step 9 (compounding-rate card on health).
 * - Step 12 (open /summaries).
 * - Step 19 (open /maintenance).
 * - Step 20 (chord shortcuts g v / g u / g x + ? help dialog).
 *
 * Steps 4, 6–8, 10, 11, 13–18, 21 require live Telegram interaction or
 * the nightly maintenance trigger and stay manual until the seed binary
 * + a Telegram MTProto stub land.
 */

test.describe('dashboard sidebar nav (12n)', () => {
  test('three Phase 12 nav items render', async ({ authedPage: page }) => {
    await page.goto('/');
    const nav = page.getByRole('navigation');
    await expect(nav.getByRole('link', { name: /conversations/i })).toBeVisible();
    await expect(nav.getByRole('link', { name: /summaries/i })).toBeVisible();
    await expect(nav.getByRole('link', { name: /maintenance/i })).toBeVisible();
  });

  test('chord shortcut g v navigates to /conversations', async ({ authedPage: page }) => {
    await page.goto('/');
    await page.keyboard.press('g');
    await page.keyboard.press('v');
    await expect(page).toHaveURL(/\/conversations$/);
  });

  test('chord shortcut g u navigates to /summaries', async ({ authedPage: page }) => {
    await page.goto('/');
    await page.keyboard.press('g');
    await page.keyboard.press('u');
    await expect(page).toHaveURL(/\/summaries$/);
  });

  test('chord shortcut g x navigates to /maintenance', async ({ authedPage: page }) => {
    await page.goto('/');
    await page.keyboard.press('g');
    await page.keyboard.press('x');
    await expect(page).toHaveURL(/\/maintenance$/);
  });

  test('? opens the help dialog and lists the new shortcuts', async ({ authedPage: page }) => {
    await page.goto('/');
    await page.keyboard.press('?');
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText(/conversations/i);
    await expect(dialog).toContainText(/summaries/i);
    await expect(dialog).toContainText(/maintenance/i);
  });
});

test.describe('/conversations route (12c, 12j, 12u.1, 12u.2)', () => {
  test('renders the panel without 400ing on empty chat_id', async ({ authedPage: page }) => {
    await page.goto('/conversations');
    // Header / title visible somewhere in the panel.
    await expect(page.getByRole('heading', { name: /conversations/i })).toBeVisible();
    // The fix in 12u.1 + 12u.2 means we never get a "Failed to load conversations" error here.
    await expect(page.getByText(/failed to load/i)).toHaveCount(0);
  });

  test('chat_id filter input is present', async ({ authedPage: page }) => {
    await page.goto('/conversations');
    const input = page.getByPlaceholder(/chat[ _-]?id/i);
    await expect(input).toBeVisible();
  });

  test('clicking a turn row opens the drawer (when seeded data exists)', async ({ authedPage: page }) => {
    test.skip(
      !process.env.AURA_E2E_CHAT_ID,
      'AURA_E2E_CHAT_ID not set — drawer test needs at least one seeded turn',
    );
    await page.goto('/conversations');
    const filter = page.getByPlaceholder(/chat[ _-]?id/i);
    await filter.fill(process.env.AURA_E2E_CHAT_ID!);
    // Wait for at least one turn row.
    const firstRow = page.getByRole('row').nth(1); // row 0 is header
    await expect(firstRow).toBeVisible({ timeout: 5_000 });
    await firstRow.click();
    // Drawer/sheet shows turn detail.
    await expect(page.getByRole('dialog')).toBeVisible();
  });
});

test.describe('/summaries route (12k)', () => {
  test('renders the review queue (empty or populated)', async ({ authedPage: page }) => {
    await page.goto('/summaries');
    await expect(page.getByRole('heading', { name: /summaries|review/i })).toBeVisible();
    // Either pending cards are visible, or an empty/disabled-mode state shows.
    const pending = page.getByRole('button', { name: /approve/i });
    const empty = page.getByText(/no pending|review mode disabled|all caught up/i);
    await expect(pending.or(empty).first()).toBeVisible({ timeout: 5_000 });
  });
});

test.describe('/maintenance route (12l)', () => {
  test('renders the issues panel (empty or populated)', async ({ authedPage: page }) => {
    await page.goto('/maintenance');
    await expect(page.getByRole('heading', { name: /maintenance/i })).toBeVisible();
    const issueCard = page.getByText(/severity: (high|medium|low)/i).first();
    const empty = page.getByText(/all clean|no issues|no maintenance/i);
    await expect(issueCard.or(empty).first()).toBeVisible({ timeout: 5_000 });
  });
});

test.describe('compounding-rate card (12m)', () => {
  test('5th HealthDashboard card surfaces the rate_pct number', async ({ authedPage: page }) => {
    await page.goto('/');
    const card = page.getByText(/compounding rate/i).locator('..').locator('..');
    await expect(card).toBeVisible({ timeout: 5_000 });
    // Headline is a percentage; either "0%" / "12%" / "0.5%" — match \d+(\.\d+)?%.
    await expect(card).toContainText(/\d+(\.\d+)?%/);
  });
});
