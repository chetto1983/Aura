import { expect, test } from '@playwright/test';
import { gotoAuthenticated } from './auth';

// Hermetic for the two Telegram account routes only: the Settings page, the list, the
// confirmation and the fetch wrapper that attaches the Idempotency-Key are real; the accounts are
// a fixture, because Playwright has no database client. That one identity cannot list or unlink
// another's accounts is proven by cmd/aura/two_identity_telegram_e2e_test.go, not here.
test('Settings lists the linked Telegram accounts and unlinks one after the confirmation', async ({
  page,
}) => {
  let accounts = [
    {
      telegramUserId: 4503599627370495,
      username: 'andrea',
      firstName: 'Andrea',
      addedAt: '2026-10-01T09:00:00Z',
      lastSeenAt: '2026-10-27T18:30:00Z',
    },
    {
      telegramUserId: 777,
      username: '',
      firstName: '',
      addedAt: '2026-10-02T09:00:00Z',
      lastSeenAt: null,
    },
  ];
  const deletes: { readonly path: string; readonly key: string | null }[] = [];
  await page.route(/\/api\/settings\/telegram\/accounts(\/\d+)?$/, async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (request.method() === 'GET') {
      await route.fulfill({ json: accounts });
      return;
    }
    if (request.method() === 'DELETE') {
      deletes.push({ path, key: await request.headerValue('Idempotency-Key') });
      const id = Number(path.split('/').pop());
      accounts = accounts.filter((account) => account.telegramUserId !== id);
      await route.fulfill({ status: 204, body: '' });
      return;
    }
    await route.fallback();
  });

  await gotoAuthenticated(page, '/?settings=channels');
  const list = page.getByRole('region', { name: 'Linked accounts' });
  await expect(list.getByRole('listitem')).toHaveCount(2);
  await expect(list.getByRole('listitem').first()).toContainText('@andrea');

  await list
    .getByRole('listitem')
    .filter({ hasText: 'Andrea' })
    .getByRole('button', { name: 'Unlink' })
    .click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('Unlink Andrea?');
  await dialog.getByRole('button', { name: 'Unlink' }).click();

  await expect(list.getByRole('listitem')).toHaveCount(1);
  await expect(list.getByRole('listitem').first()).toContainText('777');
  expect(deletes).toHaveLength(1);
  expect(deletes[0]?.path).toBe('/api/settings/telegram/accounts/4503599627370495');
  expect(deletes[0]?.key).toBeTruthy();
});
