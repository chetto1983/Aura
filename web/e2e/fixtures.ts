import { test as base, expect, type Page } from '@playwright/test';

/**
 * Reads the bearer token from AURA_E2E_TOKEN, or skips the suite if it
 * isn't set. We deliberately don't read from a file path (no
 * .e2e-token) so that secrets never accidentally land in the working
 * tree.
 */
function readToken(): string {
  const token = process.env.AURA_E2E_TOKEN;
  if (!token) {
    test.skip(
      true,
      'AURA_E2E_TOKEN not set — mint one via the request_dashboard_token Telegram tool, then export it before running playwright',
    );
  }
  return token!;
}

/** Logs in by seeding the dashboard's localStorage with the bearer token. */
async function loginViaLocalStorage(page: Page, token: string) {
  // First navigate to the app so localStorage is scoped to its origin.
  await page.goto('/login');
  await page.evaluate((t) => window.localStorage.setItem('aura_token', t), token);
  await page.goto('/');
  // The shell shows the sidebar only when authed.
  await expect(page.getByRole('navigation')).toBeVisible({ timeout: 5_000 });
}

export const test = base.extend<{ authedPage: Page }>({
  authedPage: async ({ page }, fixtureUse) => {
    const token = readToken();
    await loginViaLocalStorage(page, token);
    await fixtureUse(page);
  },
});

export { expect };
