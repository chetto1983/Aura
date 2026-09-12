import { expect, type Browser, type Page } from '@playwright/test';
import { sameOriginFetch, streamFrames } from './live';

// Drive-the-cockpit helpers shared by the live multi-identity specs. Both the management-key
// witness and the two-role witness create an identity, credit it, sign in as it and remove it;
// a second copy of any of these would be a second thing to keep true when the UI moves.

export interface Identity {
  readonly id: string;
  readonly name: string;
  readonly kind: string;
}

export async function getJSON<T>(page: Page, path: string): Promise<T> {
  const response = await sameOriginFetch(page, path);
  expect(response.status, `${path}: ${response.text}`).toBe(200);
  return JSON.parse(response.text) as T;
}

export async function identities(page: Page): Promise<readonly Identity[]> {
  return (
    (await getJSON<{ identities?: Identity[] }>(page, '/api/admin/identities')).identities ?? []
  );
}

/** Send one prompt and return the run's frames — the wire, not the rendering. */
export async function runTurn(
  page: Page,
  prompt: string,
): Promise<readonly Record<string, unknown>[]> {
  const composer = page.getByRole('textbox', { name: 'Ask Aura' });
  await expect(composer).toBeVisible({ timeout: 30_000 });
  const responsePromise = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === '/agent/run' && response.request().method() === 'POST',
    { timeout: 300_000 },
  );
  await composer.fill(prompt);
  await composer.press('Enter');
  const response = await responsePromise;
  return streamFrames(await response.text());
}

export async function openIdentities(page: Page): Promise<void> {
  await page.goto('/?settings=identities', { waitUntil: 'domcontentloaded' });
  await expect(page.getByRole('button', { name: 'Create identity' })).toBeVisible({
    timeout: 30_000,
  });
}

export async function signInAs(
  browser: Browser,
  baseURL: string,
  email: string,
  password: string,
): Promise<Page> {
  const context = await browser.newContext({
    baseURL,
    ignoreHTTPSErrors: true,
    serviceWorkers: 'block',
  });
  const page = await context.newPage();
  await page.addInitScript(() => {
    window.localStorage.setItem('aura.language', 'en');
  });
  await page.goto('/login', { waitUntil: 'domcontentloaded' });
  await page.getByLabel('Operator email').fill(email);
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).not.toHaveURL(/\/login(?:[?#]|$)/, { timeout: 30_000 });
  return page;
}

export async function skipFirstRunSetup(page: Page): Promise<void> {
  const setup = page.getByRole('dialog', { name: 'Set up your profile' });
  await expect(setup).toBeVisible({ timeout: 30_000 });
  await setup.getByRole('button', { name: 'Skip profile setup' }).click();
  await setup.getByRole('button', { name: 'Skip Telegram setup' }).click();
  await setup.getByRole('button', { name: 'Done' }).click();
  await expect(setup).toBeHidden();
}

export async function createIdentity(page: Page, email: string, password: string): Promise<void> {
  await openIdentities(page);
  await page.getByRole('button', { name: 'Create identity' }).click();
  const wizard = page.getByRole('dialog', { name: 'Create identity' });
  await wizard.getByLabel('Operator email').fill(email);
  await wizard.getByLabel('Initial password', { exact: true }).fill(password);
  await wizard.getByLabel('Confirm initial password', { exact: true }).fill(password);
  await wizard.getByLabel('Security question').fill('E2E recovery word');
  await wizard.getByLabel('Security answer', { exact: true }).fill('e2e');
  await wizard.getByRole('button', { name: 'Continue' }).click();
  await expect(wizard.getByText('Starting credit')).toBeVisible();
  await wizard.getByRole('button', { name: 'Create identity' }).click();
  await expect(wizard.getByRole('heading', { name: 'Identity created' })).toBeVisible({
    timeout: 120_000,
  });
  await wizard.getByRole('button', { name: 'Done' }).click();
}

export async function removeIdentity(page: Page, email: string): Promise<void> {
  await openIdentities(page);
  await page.getByRole('button', { name: `Remove ${email}` }).click();
  await page.getByLabel(`Type ${email} to confirm`).fill(email);
  // The row swaps its Remove button for a spinner while the saga runs, so the button going
  // away proves nothing: the route's own answer does.
  const removal = page.waitForResponse(
    (response) =>
      response.request().method() === 'DELETE' &&
      new URL(response.url()).pathname.startsWith('/api/admin/identities/'),
    { timeout: 300_000 },
  );
  await page.getByRole('button', { name: 'Remove permanently' }).click();
  expect((await removal).status()).toBe(200);
  await expect(page.getByRole('listitem').filter({ hasText: email })).toHaveCount(0);
}

/** Raise an identity's spending cap from the admin's Credit panel and wait for the save to
 *  answer. OpenRouter takes about 25 more seconds to honour it — the advisory says so, and a
 *  caller that needs the provider to agree polls for it. */
export async function setSpendingCap(page: Page, email: string, usd: string): Promise<void> {
  await openIdentities(page);
  await page.getByRole('button', { name: `Show credit for ${email}` }).click();
  await page.getByLabel('Spending cap').fill(usd);
  await page.getByRole('button', { name: 'Save cap' }).click();
  await expect(page.getByText('Takes about 25 seconds to apply.')).toBeVisible({
    timeout: 120_000,
  });
}
