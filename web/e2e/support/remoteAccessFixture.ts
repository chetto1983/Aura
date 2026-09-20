import { expect, type Page } from '@playwright/test';
import type { RemoteAccessStatusDTO } from '../../src/settings/remoteAccess/remoteAccessApi';
import { installCalmPrismFixture } from './calmPrismFixture';

export const initialRemoteStatus: RemoteAccessStatusDTO = {
  phase: 'disabled',
  enabled: false,
  generation: 0,
  api_token_set: false,
  tunnel_token_set: false,
  connector: 'disconnected',
  acceptance_required: false,
};
export const configuredRemoteStatus: RemoteAccessStatusDTO = {
  ...initialRemoteStatus,
  phase: 'healthy',
  enabled: true,
  generation: 4,
  api_token_set: true,
  tunnel_token_set: true,
  connector: 'healthy',
  account_id: 'account-b',
  zone_name: 'example.test',
  public_hostname: 'custom.example.test',
  warp_hostname: 'private.example.test',
};

export async function remoteAccessFixture(
  page: Page,
  initial = initialRemoteStatus,
  language = 'en',
) {
  await installCalmPrismFixture(page, 'light', true, false);
  await page.addInitScript((language) => {
    localStorage.setItem('aura.language', language);
  }, language);
  await page.route('**/api/me', (route) =>
    route.fulfill({
      json: {
        identity_id: 'operator',
        capabilities: ['identity.create', 'governance.write', 'agent.run'],
      },
    }),
  );
  const fixture = {
    status: { ...initial },
    refuseToken: false,
    refuseDelete: false,
    requests: [] as { path: string; method: string; body: Record<string, unknown> }[],
  };
  await page.route('**/api/settings/remote-access**', async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname.replace('/api/settings/remote-access', '');
    if (request.method() === 'GET') {
      await route.fulfill({ json: fixture.status });
      return;
    }
    const body = request.postDataJSON() as Record<string, unknown>;
    // Never retain credential values in fixture diagnostics or assertions.
    const { api_token: token, ...safeBody } = body;
    fixture.requests.push({
      path,
      method: request.method(),
      body: {
        ...safeBody,
        ...(token === undefined ? {} : { api_token: '<redacted>' }),
      },
    });
    expect(request.headers()['idempotency-key']).toBeTruthy();
    if (path === '/token/verify') {
      await route.fulfill(
        fixture.refuseToken
          ? { status: 403, json: { error: 'token refused' } }
          : {
              json: {
                accounts: [
                  { id: 'account-a', name: 'First account' },
                  { id: 'account-b', name: 'Chosen account' },
                ],
              },
            },
      );
      return;
    }
    if (request.method() === 'PUT') {
      fixture.status = {
        ...configuredRemoteStatus,
        phase: body.api_token ? 'waiting_nameservers' : 'connecting',
        enabled: body.enabled === true,
        nameservers: ['one.ns.cloudflare.com', 'two.ns.cloudflare.com'],
      };
    }
    if (path === '/disable')
      fixture.status = { ...fixture.status, enabled: false, phase: 'disabled' };
    if (path === '/accept-external')
      fixture.status = { ...fixture.status, phase: 'healthy', acceptance_required: false };
    if (request.method() === 'DELETE') {
      if (fixture.refuseDelete) {
        await route.fulfill({ status: 503, json: { error: 'retry later' } });
        return;
      }
      expect(body).toEqual({ hostname: fixture.status.public_hostname });
      fixture.status = { ...fixture.status, phase: 'deleting', enabled: false };
    }
    await route.fulfill({ status: 202, json: {} });
  });
  return fixture;
}

export async function openRemoteAccess(page: Page) {
  await page.goto('/?settings=remote-access');
  await expect(page.locator('#remote-access-heading')).toBeVisible();
  await page.evaluate(() => document.fonts.ready);
}
