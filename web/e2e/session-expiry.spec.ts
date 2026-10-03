import { expect, test } from '@playwright/test';
import { gotoAuthenticated } from './auth';

// A session that ends while the cockpit is open must send the person to the login page
// with the expiry notice, and signing in again must bring them back to the conversation
// they had open. Before api/sessionExpiry.ts every panel showed its own HTTP 401 and only
// a manual reload reached /login.
test('an ended session returns to the open conversation after signing in again', async ({
  page,
}) => {
  const email = process.env.AURA_E2E_AUTHULA_EMAIL;
  const password = process.env.AURA_E2E_AUTHULA_PASSWORD;
  if (email === undefined || password === undefined) {
    throw new Error('AURA_E2E_AUTHULA_EMAIL/PASSWORD are required for the session-expiry spec');
  }

  // Its own session: clearing the cookies below must not touch the shared cached one.
  await gotoAuthenticated(page, '/', { isolatedSession: true });
  const conversationID = await page.evaluate(async () => {
    const res = await fetch('/api/conversations', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ title: 'Session expiry return path' }),
    });
    return ((await res.json()) as { ID: string }).ID;
  });
  await page.goto(`/c/${conversationID}`, { waitUntil: 'domcontentloaded' });
  await expect(page).toHaveURL(new RegExp(`/c/${conversationID}$`));

  await page.context().clearCookies();
  await page.evaluate(async () => {
    await fetch('/api/me');
  });

  await expect(page).toHaveURL(
    `/login?expired=1&next=${encodeURIComponent(`/c/${conversationID}`)}`,
  );
  await expect(page.getByText('Your session expired. Sign in again to continue.')).toBeVisible();

  await page.getByLabel('Operator email').fill(email);
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();

  await expect(page).toHaveURL(new RegExp(`/c/${conversationID}$`), { timeout: 30_000 });
});
