import { randomBytes } from 'node:crypto';
import { existsSync } from 'node:fs';
import { expect, test } from '@playwright/test';
import {
  configuredRemoteStatus,
  openRemoteAccess,
  remoteAccessFixture,
} from './support/remoteAccessFixture';

// Trace captures request bodies and input actions. Disable every recorder before credentials
// enter the page; a retry must not write a Cloudflare token to a trace, screenshot or video.
test.use({ trace: 'off', screenshot: 'off', video: 'off', viewport: { width: 390, height: 844 } });

test('refuses a token, requires an account choice, persists nameserver wait across reload', async ({
  page,
}, info) => {
  const fixture = await remoteAccessFixture(page);
  const token = randomBytes(24).toString('hex');
  const requestURLs: string[] = [];
  page.on('request', (request) => requestURLs.push(request.url()));
  await openRemoteAccess(page);
  fixture.refuseToken = true;
  await page.getByLabel('Cloudflare API token', { exact: true }).fill(token);
  await page.getByRole('button', { name: 'Verify token', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('could not be verified');
  fixture.refuseToken = false;
  await page.getByRole('button', { name: 'Verify token', exact: true }).click();
  await expect(page.getByLabel('Cloudflare account', { exact: true })).toHaveValue('');
  await page.getByLabel('Registered domain', { exact: true }).fill('example.test');
  await expect(page.getByRole('button', { name: 'Save and start setup' })).toBeDisabled();
  await page.getByLabel('Cloudflare account', { exact: true }).selectOption('account-b');
  await page.getByRole('button', { name: 'Save and start setup' }).click();
  await expect(page.getByText('one.ns.cloudflare.com')).toBeVisible();
  expect(fixture.requests.at(-1)).toEqual({
    path: '',
    method: 'PUT',
    body: {
      enabled: true,
      generation: 0,
      account_id: 'account-b',
      zone_name: 'example.test',
      public_label: 'aura',
      warp_label: 'aura-warp',
      api_token: '<redacted>',
    },
  });
  await page.reload();
  await expect(page.getByText('one.ns.cloudflare.com')).toBeVisible();
  await expect(page.getByLabel('Cloudflare API token', { exact: true })).toHaveCount(0);
  // Boolean assertions keep even a failing diagnostic from printing the secret.
  expect((await page.content()).includes(token)).toBe(false);
  expect(requestURLs.some((url) => url.includes(token))).toBe(false);
  expect(
    await page.evaluate(
      (secret) =>
        [localStorage, sessionStorage].some((storage) => JSON.stringify(storage).includes(secret)),
      token,
    ),
  ).toBe(false);
  expect(existsSync(info.outputPath('trace.zip'))).toBe(false);
});

test('configured controls refresh truthfully, disable, re-enable saved credentials and delete exact host', async ({
  page,
}) => {
  const fixture = await remoteAccessFixture(page, configuredRemoteStatus);
  await openRemoteAccess(page);
  await expect(page.getByText('Direct IP or LAN ingress bypasses Cloudflare Access')).toBeVisible();
  await expect(
    page.getByRole('link', { name: 'Rotate token in Cloudflare Dashboard' }),
  ).toHaveAttribute('href', 'https://developers.cloudflare.com/tunnel/reference/tunnel-tokens/');
  await page.getByRole('button', { name: 'Refresh Dashboard-rotated token' }).click();
  expect(fixture.requests.at(-1)).toEqual({
    path: '/token/refresh',
    method: 'POST',
    body: { generation: 4 },
  });
  await page.getByRole('button', { name: 'Disable remote access', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Re-enable remote access' })).toBeVisible();
  await page.getByRole('button', { name: 'Re-enable remote access' }).click();
  expect(fixture.requests.at(-1)).toEqual({
    path: '',
    method: 'PUT',
    body: {
      enabled: true,
      generation: 4,
      account_id: 'account-b',
      zone_name: 'example.test',
      public_label: 'custom',
      warp_label: 'private',
    },
  });
  fixture.status = { ...configuredRemoteStatus, phase: 'error', last_error: 'permission_refused' };
  await page.reload();
  await expect(page.getByRole('alert')).toContainText('needs attention');
  const trigger = page.getByRole('button', { name: 'Delete remote access', exact: true });
  await trigger.focus();
  await page.keyboard.press('Enter');
  const dialog = page.getByRole('alertdialog');
  await expect(dialog).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(trigger).toBeFocused();
  await trigger.press('Enter');
  const confirmation = dialog.getByRole('textbox');
  await confirmation.fill('wrong.example.test');
  const remove = dialog.getByRole('button', { name: 'Delete permanently' });
  await expect(remove).toBeDisabled();
  await confirmation.fill('custom.example.test');
  fixture.refuseDelete = true;
  await remove.click();
  await expect(dialog.getByRole('alert')).toBeVisible();
  await expect(confirmation).toHaveValue('custom.example.test');
  fixture.refuseDelete = false;
  await remove.click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByText('Deleting resources', { exact: true })).toBeVisible();
});

test('external acceptance is interactive and never sends a client tunnel marker', async ({
  page,
}) => {
  const fixture = await remoteAccessFixture(page, {
    ...configuredRemoteStatus,
    phase: 'connecting',
    acceptance_required: true,
  });
  await openRemoteAccess(page);
  await expect(page.getByRole('button', { name: 'Confirm external access' })).toHaveCount(0);
  await expect(page.getByRole('link', { name: 'Open public hostname' })).toHaveAttribute(
    'href',
    'https://custom.example.test/?settings=remote-access',
  );
  fixture.status = { ...fixture.status, public_hostname: new URL(page.url()).hostname };
  await page.reload();
  const request = page.waitForRequest('**/remote-access/accept-external');
  await page.getByRole('button', { name: 'Confirm external access' }).click();
  const sent = await request;
  expect(sent.postDataJSON()).toEqual({ generation: 4 });
  expect(sent.headers()['x-aura-remote-ingress']).toBeUndefined();
  await expect(page.locator('#remote-access-heading')).toHaveText('Remote access status');
});

test('390px Italian layout fits, labels remain readable and controls meet 44px', async ({
  page,
}, info) => {
  await remoteAccessFixture(page, configuredRemoteStatus, 'it');
  await openRemoteAccess(page);
  const panel = page.locator('section[aria-labelledby="remote-access-heading"]');
  const geometry = await panel.evaluate((element) => {
    const controls = [...element.querySelectorAll<HTMLElement>('button, a, input, select')];
    return {
      viewport: document.documentElement.clientWidth,
      pageWidth: document.documentElement.scrollWidth,
      panelWidth: element.clientWidth,
      panelScroll: element.scrollWidth,
      controls: controls.map((control) => ({
        label: control.getAttribute('aria-label') ?? control.textContent?.trim(),
        width: control.getBoundingClientRect().width,
        height: control.getBoundingClientRect().height,
        clipped:
          control.scrollWidth > control.clientWidth + 1 ||
          control.scrollHeight > control.clientHeight + 1,
      })),
    };
  });
  await info.attach('390px-geometry', {
    body: JSON.stringify(geometry, null, 2),
    contentType: 'application/json',
  });
  expect(geometry.viewport).toBe(390);
  expect(geometry.pageWidth).toBeLessThanOrEqual(geometry.viewport);
  expect(geometry.panelScroll).toBeLessThanOrEqual(geometry.panelWidth);
  for (const control of geometry.controls) {
    expect(control.height, control.label).toBeGreaterThanOrEqual(44);
    expect(control.width, control.label).toBeGreaterThanOrEqual(44);
    expect(control.clipped, control.label).toBe(false);
  }
});
