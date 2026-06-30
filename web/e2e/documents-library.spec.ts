import { expect, test } from '@playwright/test';
import { gotoAuthenticated } from './auth';

test('document library exposes product file manager controls', async ({ page }) => {
  await gotoAuthenticated(page, '/');
  await page.getByRole('navigation', { name: 'Primary' }).getByRole('button', { name: /Documents/ }).click();

  await expect(page.getByRole('heading', { name: /Document library|Libreria documenti/ })).toBeVisible();
  await expect(page.getByRole('searchbox', { name: /Search documents|Cerca documenti/ })).toBeVisible();
  await expect(page.getByRole('tab', { name: /All|Tutti/ })).toBeVisible();
  await expect(page.getByRole('button', { name: /Upload|Carica/ })).toBeVisible();
  await expect(page.getByRole('button', { name: /List view|Vista elenco/ })).toBeVisible();
});
