import { expect, type Page } from '@playwright/test';

// The Documents mode opens on the archive overview (b237f0c); the file manager is the
// "Files / folders" destination of the documents navigation, which a desktop shows in the side
// rail and a phone behind the toolbar's navigation button.

/** Opens Documents from the shell's mode nav, then its Files / folders destination. */
export async function openFilesFolders(page: Page, projectName: string): Promise<void> {
  const modes =
    projectName === 'chrome'
      ? page.getByRole('navigation', { name: /Primary|Principale/ })
      : page.getByRole('navigation', { name: /Modes|Modalit/ });
  await modes.getByRole('button', { name: /Documents|Documenti/ }).click();
  // .first(): the shell's region; Files / folders nests its own region of the same name.
  await expect(page.getByRole('region', { name: /Documents|Documenti/ }).first()).toBeVisible();
  const navName = /Document navigation|Navigazione documenti/;
  // Tailwind's lg breakpoint, where DocumentsSurface swaps the drawer for the side rail.
  if ((page.viewportSize()?.width ?? 0) < 1024) {
    await page.getByRole('button', { name: navName }).click();
  }
  await page
    .getByRole('navigation', { name: navName })
    .getByRole('link', { name: /Files \/ folders|File \/ cartelle/ })
    .click();
  await expect(page).toHaveURL(/\/documents\/files$/);
}
