import { randomBytes } from 'node:crypto';
import { existsSync } from 'node:fs';
import { expect, test, type Page } from '@playwright/test';
import {
  configuredRemoteStatus,
  openRemoteAccess,
  remoteAccessFixture,
} from './support/remoteAccessFixture';
import { chooseValue } from './support/select';

// Trace captures request bodies and input actions. Disable every recorder before credentials
// enter the page; a retry must not write a Cloudflare token to a trace, screenshot or video.
test.use({ trace: 'off', screenshot: 'off', video: 'off', viewport: { width: 390, height: 844 } });

const step = (page: Page, name: string) => page.getByRole('heading', { name, level: 3 });

test('walks forward, back to Dominio, changes the domain and returns to green', async ({
  page,
}, info) => {
  test.setTimeout(90_000);
  const fixture = await remoteAccessFixture(page);
  const token = randomBytes(24).toString('hex');
  const requestURLs: string[] = [];
  page.on('request', (request) => requestURLs.push(request.url()));
  await openRemoteAccess(page);

  // Cloudflare: Next stays shut until the token is verified and an account is chosen.
  await expect(page.getByText('Step 1 of 4')).toBeVisible();
  await expect(step(page, 'Connect your Cloudflare account')).toBeVisible();
  // Nothing opens the list: the operator reads it before creating the token.
  await expect(
    page
      .getByRole('list', { name: 'Permissions the token needs' })
      .getByText('Cloudflare Tunnel Edit'),
  ).toBeVisible();
  await page.getByLabel('Cloudflare API token', { exact: true }).fill(token);
  await expect(page.getByRole('button', { name: 'Next', exact: true })).toBeDisabled();
  await page.getByRole('button', { name: 'Verify token', exact: true }).click();
  await expect(page.getByLabel('Cloudflare account', { exact: true })).toHaveAttribute(
    'data-placeholder',
    '',
  );
  await expect(page.getByRole('button', { name: 'Next', exact: true })).toBeDisabled();
  await chooseValue(page.getByLabel('Cloudflare account', { exact: true }), 'account-b');
  await page.getByRole('button', { name: 'Next', exact: true }).click();

  // The token and the account are saved with access still off, before any domain exists.
  await expect(step(page, 'Choose the domain')).toBeVisible();
  await expect(page.getByText('Step 2 of 4')).toBeVisible();
  expect(fixture.requests.at(-1)).toEqual({
    path: '',
    method: 'PUT',
    body: {
      enabled: false,
      generation: 0,
      account_id: 'account-b',
      zone_name: '',
      public_label: '',
      api_token: '<redacted>',
    },
  });

  // A first setup keeps the domain typed, however far the operator walks.
  await page.getByLabel('Domain', { exact: true }).fill('example.test');
  await page.getByRole('button', { name: 'Back', exact: true }).click();
  await expect(step(page, 'Connect your Cloudflare account')).toBeVisible();
  await page.getByRole('button', { name: 'Next', exact: true }).click();
  await expect(page.getByLabel('Domain', { exact: true })).toHaveValue('example.test');

  await page.getByRole('button', { name: 'Next', exact: true }).click();
  await expect(page.getByText('one.ns.cloudflare.com')).toBeVisible();
  expect(fixture.requests.at(-1)).toEqual({
    path: '',
    method: 'PUT',
    body: {
      enabled: true,
      generation: 1,
      account_id: 'account-b',
      zone_name: 'example.test',
      public_label: 'aura',
    },
  });

  // Cloudflare activates the zone, the tunnel comes up and the phone test passes. The nameserver
  // wait polls every thirty seconds, so the wizard moves by itself on the next one.
  fixture.status = {
    ...fixture.status,
    phase: 'healthy',
    nameservers: [],
    accepted: true,
    reachable: true,
  };
  await expect(step(page, 'Try it from your phone')).toBeVisible({ timeout: 35_000 });
  await expect(page.getByText('Step 4 of 4')).toBeVisible();
  await expect(page.getByText('Reachable from outside')).toBeVisible();
  await expect(page.getByText('https://aura.example.test', { exact: true })).toBeVisible();

  // Back to Dominio: an edit left unsaved on a configured install is dropped, not carried.
  await page.getByRole('button', { name: 'Back', exact: true }).click();
  await expect(step(page, 'Connection to Cloudflare')).toBeVisible();
  await page.getByRole('button', { name: 'Back', exact: true }).click();
  await expect(step(page, 'Choose the domain')).toBeVisible();
  await page.getByLabel('Domain', { exact: true }).fill('abandoned.test');
  await page.getByRole('button', { name: 'Back', exact: true }).click();
  await page.getByRole('button', { name: 'Next', exact: true }).click();
  await expect(page.getByLabel('Domain', { exact: true })).toHaveValue('example.test');

  await page.getByLabel('Domain', { exact: true }).fill('other.test');
  await expect(
    page.getByText('The current address stops working, and you will repeat the test'),
  ).toBeVisible();
  await page.getByRole('button', { name: 'Next', exact: true }).click();
  await expect(step(page, 'Connection to Cloudflare')).toBeVisible();
  expect(fixture.requests.at(-1)).toEqual({
    path: '',
    method: 'PUT',
    body: {
      enabled: true,
      generation: 2,
      account_id: 'account-b',
      zone_name: 'other.test',
      public_label: 'aura',
    },
  });

  // The new address has never passed the phone test, so the wizard asks for it again.
  fixture.status = { ...fixture.status, phase: 'connecting', accepted: false, reachable: false };
  await expect(step(page, 'Try it from your phone')).toBeVisible({ timeout: 15_000 });
  await expect(page.getByText('Open this address from your phone')).toBeVisible();
  await expect(page.getByText('https://aura.other.test', { exact: true })).toBeVisible();
  await expect(page.getByText('Reachable from outside')).toHaveCount(0);

  // The accept button shows only on the public hostname, which this page is not (the phone test
  // below covers it), so the passed test is forced into the status.
  fixture.status = { ...fixture.status, phase: 'healthy', accepted: true, reachable: true };
  await expect(page.getByText('Reachable from outside')).toBeVisible();

  // Boolean assertions keep even a failing diagnostic from printing the secret.
  await page.reload();
  await expect(step(page, 'Try it from your phone')).toBeVisible();
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

test('a no_accounts verify shows its sentence in Cloudflare', async ({ page }) => {
  const fixture = await remoteAccessFixture(page);
  fixture.refuseVerify = 'no_accounts';
  await openRemoteAccess(page);
  await page
    .getByLabel('Cloudflare API token', { exact: true })
    .fill(randomBytes(24).toString('hex'));
  await page.getByRole('button', { name: 'Verify token', exact: true }).click();

  const alert = page.getByRole('alert');
  await expect(alert).toContainText('The token works but sees no account');
  await expect(step(page, 'Connect your Cloudflare account')).toBeVisible();
  await expect(page.getByLabel('Cloudflare account', { exact: true })).toHaveCount(0);
  await expect(page.getByLabel('Cloudflare API token', { exact: true })).toHaveAttribute(
    'aria-invalid',
    'true',
  );

  // The reason's button hands the token field back to the operator.
  await alert.getByRole('button', { name: 'Paste a token with that permission' }).click();
  await expect(page.getByLabel('Cloudflare API token', { exact: true })).toBeFocused();
});

test('the phone step accepts only from the public hostname and never sends a client tunnel marker', async ({
  page,
}) => {
  const fixture = await remoteAccessFixture(page, {
    ...configuredRemoteStatus,
    phase: 'connecting',
    accepted: false,
    reachable: false,
  });
  await openRemoteAccess(page);
  await expect(step(page, 'Try it from your phone')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Confirm external access' })).toHaveCount(0);
  await expect(
    page.getByText('The confirmation is only available from that address.'),
  ).toBeVisible();
  await expect(page.getByRole('link', { name: 'Open the public address' })).toHaveAttribute(
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
  await expect(page.getByText('Reachable from outside')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Confirm external access' })).toHaveCount(0);
});

test('390px Italian layout fits and every control is 44px', async ({ page }, info) => {
  await remoteAccessFixture(page, configuredRemoteStatus, 'it');
  await openRemoteAccess(page);
  const panel = page.locator('section[aria-labelledby="remote-access-heading"]');
  const walk = [
    { heading: 'Prova dal telefono', position: 'Passo 4 di 4' },
    { heading: 'Collegamento con Cloudflare', position: 'Passo 3 di 4' },
    { heading: 'Scegli il dominio', position: 'Passo 2 di 4' },
    { heading: 'Collega il tuo account Cloudflare', position: 'Passo 1 di 4' },
  ];
  for (const [index, { heading, position }] of walk.entries()) {
    if (index > 0) await page.getByRole('button', { name: 'Indietro', exact: true }).click();
    await expect(step(page, heading)).toBeVisible();
    await expect(page.getByText(position)).toBeVisible();
    const geometry = await panel.evaluate((element) => {
      // The desktop strip is display:none at this width; a hidden control has no size to measure.
      const controls = [
        ...element.querySelectorAll<HTMLElement>('button, a, input, select'),
      ].filter((control) => control.getClientRects().length > 0);
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
    await info.attach(`390px-geometry-${heading}`, {
      body: JSON.stringify(geometry, null, 2),
      contentType: 'application/json',
    });
    expect(geometry.viewport).toBe(390);
    expect(geometry.pageWidth, heading).toBeLessThanOrEqual(geometry.viewport);
    expect(geometry.panelScroll, heading).toBeLessThanOrEqual(geometry.panelWidth);
    expect(geometry.controls.length, heading).toBeGreaterThan(0);
    for (const control of geometry.controls) {
      expect(control.height, `${heading}: ${control.label ?? ''}`).toBeGreaterThanOrEqual(44);
      expect(control.width, `${heading}: ${control.label ?? ''}`).toBeGreaterThanOrEqual(44);
      expect(control.clipped, `${heading}: ${control.label ?? ''}`).toBe(false);
    }
  }
});
