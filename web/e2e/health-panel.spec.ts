import { expect, test } from '@playwright/test';
import { gotoAuthenticated } from './auth';

// WEB-04: the embedded operator shell summarizes runtime readiness in the header, and
// the chip opens the detailed panel. The main shell must not reserve a redundant right
// rail for rows that are already summarized.

test.describe('runtime readiness status', () => {
  test('applies dark theme + operator density before paint', async ({ page }) => {
    await gotoAuthenticated(page, '/');
    const html = page.locator('html');
    await expect(html).toHaveAttribute('data-theme', 'dark');
    await expect(html).toHaveAttribute('data-density', /compact|operator|review/);
  });

  test('renders header readiness without the old right rail', async ({ page }, testInfo) => {
    await gotoAuthenticated(page, '/');

    await expect(
      page.getByRole('status', {
        name: /^(Runtime|Stato runtime): (Ready|Pronto|Degraded|Degradato|Unavailable|Non disponibile)$/,
      }),
    ).toBeVisible({ timeout: 15000 });

    await expect(page.getByRole('button', { name: 'Open runtime status' })).toHaveCount(0);
    await expect(page.getByRole('dialog', { name: 'Display workspace' })).toHaveCount(0);
    await expect(page.locator('#runtime-rail')).toHaveCount(0);

    await testInfo.attach('header-runtime-readiness.png', {
      body: await page.screenshot(),
      contentType: 'image/png',
    });
  });

  test('opens the runtime detail from the header chip', async ({ page }) => {
    await gotoAuthenticated(page, '/');

    await page
      .getByRole('button', {
        name: /^(Runtime|Stato runtime): .+\. (Show details|Mostra dettagli)$/,
      })
      .click();

    await expect(
      page.getByRole('heading', { name: /^(Runtime|Stato runtime)$/, level: 2 }),
    ).toBeVisible();
  });
});
