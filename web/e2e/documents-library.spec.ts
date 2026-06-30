import { expect, test } from '@playwright/test';
import { gotoAuthenticated } from './auth';

test('document library exposes product file manager controls', async ({ page }, testInfo) => {
  await gotoAuthenticated(page, '/');

  const nav =
    testInfo.project.name === 'chromium'
      ? page.getByRole('navigation', { name: /Primary|Principale/ })
      : page.getByRole('navigation', { name: /Modes|Modalit/ });
  await nav.getByRole('button', { name: /Documents|Documenti/ }).click();

  await expect(
    page.getByRole('heading', { name: /Document library|Libreria documenti/ }),
  ).toBeVisible();
  await expect(
    page.getByRole('searchbox', { name: /Search documents|Cerca documenti/ }),
  ).toBeVisible();
  await expect(page.getByRole('tab', { name: /All|Tutti/ })).toBeVisible();
  await expect(page.getByRole('button', { name: /Upload|Carica/ })).toBeVisible();
  await expect(page.getByRole('button', { name: /List view|Vista elenco/ })).toBeVisible();
});
