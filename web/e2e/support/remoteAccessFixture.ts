import { expect, type Page } from '@playwright/test';
import type {
  RemoteAccessConfiguration,
  RemoteAccessReason,
  RemoteAccessStatusDTO,
} from '../../src/settings/remoteAccess/remoteAccessApi';
import { gotoAuthenticated } from '../auth';
import { installCalmPrismFixture } from './calmPrismFixture';

export const initialRemoteStatus: RemoteAccessStatusDTO = {
  phase: 'disabled',
  enabled: false,
  generation: 0,
  api_token_set: false,
  nameservers: [],
  reason: '',
  accepted: false,
  reachable: false,
  has_resources: false,
  abandoned: [],
};
export const configuredRemoteStatus: RemoteAccessStatusDTO = {
  ...initialRemoteStatus,
  phase: 'healthy',
  enabled: true,
  generation: 4,
  api_token_set: true,
  accepted: true,
  reachable: true,
  has_resources: true,
  account_id: 'account-b',
  account_name: 'Chosen account',
  zone_name: 'example.test',
  public_label: 'custom',
  public_hostname: 'custom.example.test',
};

const ACCOUNTS = [
  { id: 'account-a', name: 'First account' },
  { id: 'account-b', name: 'Chosen account' },
];

// The sentences internal/agui/remote_access_api.go writes: fixed text plus the reason code.
const REFUSAL_SENTENCE = 'invalid or incomplete remote access configuration';

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
    /** The reason the next token verification is refused with; null lets it through. */
    refuseVerify: null as Exclude<RemoteAccessReason, ''> | null,
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
        fixture.refuseVerify === null
          ? { json: { accounts: ACCOUNTS } }
          : { status: 400, json: { error: REFUSAL_SENTENCE, reason: fixture.refuseVerify } },
      );
      return;
    }
    if (request.method() === 'PUT') {
      fixture.status = savedStatus(fixture.status, body);
      await route.fulfill({ status: 202, json: {} });
      return;
    }
    if (path === '/accept-external') {
      fixture.status = { ...fixture.status, phase: 'healthy', accepted: true, reachable: true };
      await route.fulfill({ status: 202, json: {} });
      return;
    }
    await route.fulfill({ status: 404, json: { error: `fixture has no ${path}`, reason: '' } });
  });
  return fixture;
}

/** What the server does with a save: no domain is a stored token and account (the first step), a
 * first domain waits for the nameservers, and a new domain goes through the connection again with
 * its phone test forgotten. */
function savedStatus(before: RemoteAccessStatusDTO, body: Partial<RemoteAccessConfiguration>) {
  const generation = before.generation + 1;
  const accountId = body.account_id ?? '';
  const zone = body.zone_name ?? '';
  const label = body.public_label ?? '';
  const account = ACCOUNTS.find((candidate) => candidate.id === accountId);
  const credentials = {
    api_token_set: before.api_token_set || body.api_token !== undefined,
    account_id: accountId,
    ...(account === undefined ? {} : { account_name: account.name }),
  };
  if (zone === '') {
    return {
      ...initialRemoteStatus,
      ...credentials,
      generation,
      enabled: body.enabled === true,
    };
  }
  const sameAddress = zone === before.zone_name && label === before.public_label;
  const firstDomain = before.zone_name === undefined;
  return {
    ...before,
    ...credentials,
    generation,
    enabled: body.enabled === true,
    phase: firstDomain ? ('waiting_nameservers' as const) : ('provisioning' as const),
    nameservers: firstDomain ? ['one.ns.cloudflare.com', 'two.ns.cloudflare.com'] : [],
    accepted: sameAddress && before.accepted,
    reachable: false,
    has_resources: true,
    zone_name: zone,
    public_label: label,
    public_hostname: `${label}.${zone}`,
  };
}

export async function openRemoteAccess(page: Page) {
  // RequireAuth gates the whole origin (internal/agui/auth.go): a bare goto answers
  // 302 /login without a session, and the sign-in form has no settings panel to find.
  // The routes above are page-level stubs, so the panel stays hermetic behind a real session.
  await gotoAuthenticated(page, '/?settings=remote-access');
  // Settings is a lazily-loaded chunk fetched on first open, which outruns the default
  // five-second expectation.
  await expect(page.locator('#remote-access-heading')).toBeVisible({ timeout: 30_000 });
  await page.evaluate(() => document.fonts.ready);
}
