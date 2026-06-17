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
});
