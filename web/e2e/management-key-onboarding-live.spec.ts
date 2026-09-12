import { randomUUID } from 'node:crypto';
import { expect, test, type Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';
import {
  createIdentity,
  getJSON,
  identities,
  openIdentities,
  removeIdentity,
  runTurn,
  setSpendingCap,
  signInAs,
  skipFirstRunSetup,
} from './identities';
import { sameOriginFetch } from './live';

// The management-key design's Definition of Done (spec 2026-09-10, §Testing, E2E) on an installed
// Aura whose first-run setup is done: the keys OpenRouter holds, a billed admin turn, the admin's
// Credit panel, and a second identity minted at zero. The OpenRouter account is read with the
// management key, which comes from the environment and is only ever sent to openrouter.ai.
//
// Driving the cockpit (create, credit, sign in as, remove an identity) lives in ./identities,
// shared with the two-role witness.

const runLive = process.env.AURA_E2E_LIVE_MANAGEMENT_KEY === '1';
const managementKey = process.env.AURA_E2E_OPENROUTER_MANAGEMENT_KEY ?? '';
const servicesCap = Number(process.env.AURA_E2E_SERVICES_CAP_USD ?? '10');
const OPENROUTER_API = 'https://openrouter.ai/api/v1';
const SERVICES_KEY_NAME = 'aura-services';
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

interface OpenRouterKey {
  readonly hash: string;
  readonly name: string;
  readonly limit: number | null;
  readonly usage: number;
  readonly disabled: boolean;
  readonly external_user?: string | null;
}

async function openRouter<T>(page: Page, path: string): Promise<T> {
  const response = await page.request.get(`${OPENROUTER_API}${path}`, {
    headers: { Authorization: `Bearer ${managementKey}` },
  });
  expect(response.status(), `GET ${path}`).toBe(200);
  return ((await response.json()) as { data: T }).data;
}

function activeKey(keys: readonly OpenRouterKey[], name: string): OpenRouterKey {
  const key = keys.find((candidate) => candidate.name === name && !candidate.disabled);
  if (key === undefined) throw new Error(`OpenRouter has no active key named ${name}`);
  return key;
}

async function deleteCurrentConversation(page: Page): Promise<void> {
  const conversationID = new URL(page.url()).pathname.split('/c/')[1];
  if (conversationID === undefined || conversationID === '') return;
  const response = await sameOriginFetch(page, `/api/conversations/${conversationID}`, {
    method: 'DELETE',
  });
  expect([200, 204], response.text).toContain(response.status);
}

test.describe('live management-key onboarding', () => {
  test.skip(!runLive, 'set AURA_E2E_LIVE_MANAGEMENT_KEY=1 against an installed, set-up Aura');

  test('mints, bills and caps OpenRouter keys by role', async ({ page, browser }, testInfo) => {
    test.skip(testInfo.project.name !== 'chrome', 'one live account witness is sufficient');
    test.setTimeout(900_000);
    expect(managementKey, 'AURA_E2E_OPENROUTER_MANAGEMENT_KEY').not.toBe('');
    const baseURL = String(testInfo.project.use.baseURL);

    await gotoAuthenticated(page, '/');
    const me = await getJSON<{ identity_id: string; name?: string }>(page, '/api/me');
    const adminName = me.name ?? '';
    expect(adminName, 'the admin identity has a name').not.toBe('');

    // The admin's key has no limit, the services key the chosen cap, and every key Aura minted is
    // named after a person who can sign in.
    const keys = await openRouter<OpenRouterKey[]>(page, '/keys');
    const own = activeKey(keys, me.identity_id);
    expect(own.limit).toBeNull();
    expect(own.external_user).toBe(me.identity_id);
    expect(activeKey(keys, SERVICES_KEY_NAME).limit).toBe(servicesCap);
    const people = new Set(
      (await identities(page))
        .filter((identity) => identity.kind === 'user')
        .map((identity) => identity.id),
    );
    const strays = keys.filter(
      (key) => !key.disabled && UUID.test(key.name) && !people.has(key.name),
    );
    expect(
      strays.map((key) => key.name),
      'active keys named after no person',
    ).toEqual([]);

    // An admin turn is billed to the admin's own key, and the spend page lists the admin.
    const adminTurn = await runTurn(
      page,
      'Non usare strumenti. Rispondi soltanto con AURA_ADMIN_TURN_OK',
    );
    expect(adminTurn.some((frame) => frame.type === 'RUN_FINISHED')).toBe(true);
    await deleteCurrentConversation(page);
    await expect
      .poll(async () => (await openRouter<OpenRouterKey>(page, `/keys/${own.hash}`)).usage, {
        timeout: 180_000,
        intervals: [10_000],
      })
      .toBeGreaterThan(own.usage);
    const overview = page.waitForResponse(
      (response) => new URL(response.url()).pathname === '/api/admin/spend/overview',
      { timeout: 120_000 },
    );
    await openIdentities(page);
    expect((await overview).status()).toBe(200);
    await expect(
      page
        .getByRole('listitem')
        .filter({ hasText: adminName })
        .filter({ hasText: 'Lifetime spend' }),
    ).toBeVisible({ timeout: 30_000 });
    const spend = await getJSON<{
      top_identities: { identity_id: string; lifetime_spend: number }[];
    }>(page, '/api/admin/spend/overview');
    const adminRow = spend.top_identities.find((row) => row.identity_id === me.identity_id);
    expect(adminRow?.lifetime_spend ?? 0).toBeGreaterThan(0);

    // The admin's Credit panel says there is no limit.
    await page.getByRole('button', { name: `Show credit for ${adminName}` }).click();
    await expect(page.getByText('No limit', { exact: true })).toBeVisible();

    // A second identity is minted at zero, refused until topped up, and cannot write the route.
    const memberEmail = `e2e-member-${String(Date.now())}@example.com`;
    const memberPassword = `E2e-${randomUUID()}`;
    await createIdentity(page, memberEmail, memberPassword);
    const member = (await identities(page)).find((identity) => identity.name === memberEmail);
    if (member === undefined) throw new Error(`${memberEmail} is not in the roster`);
    try {
      const memberKey = activeKey(await openRouter<OpenRouterKey[]>(page, '/keys'), member.id);
      expect(memberKey.limit).toBe(0);

      const memberPage = await signInAs(browser, baseURL, memberEmail, memberPassword);
      await skipFirstRunSetup(memberPage);
      const refusedPrompt = 'Rispondi soltanto con AURA_MEMBER_TURN';
      await runTurn(memberPage, refusedPrompt);
      await expect(
        memberPage.getByRole('alert').filter({ hasText: 'has no remaining credit for this turn' }),
      ).toBeVisible({ timeout: 60_000 });
      // The refused turn's title call is refused too, so the conversation takes the fallback
      // title: what the member typed, never the skills block the history opens with (it read
      // "Active skill instruc…" before 5aff02bc1).
      await expect(
        memberPage.getByRole('button', { name: refusedPrompt, exact: true }),
      ).toBeVisible({ timeout: 60_000 });
      const routeWrite = await sameOriginFetch(memberPage, '/api/settings/llm-profile', {
        method: 'PUT',
        body: JSON.stringify({ settings: { AURA_LLM_PROVIDER: 'openrouter' } }),
      });
      expect(routeWrite.status, routeWrite.text).toBe(403);

      // The save answers once the store, the provider and the cached refusal are updated.
      // OpenRouter itself takes about 25s more to honour a raised limit, so the member's turn
      // is retried below until the provider lets it through.
      await setSpendingCap(page, memberEmail, '0.50');
      expect((await openRouter<OpenRouterKey>(page, `/keys/${memberKey.hash}`)).limit).toBe(0.5);
      await expect(async () => {
        await memberPage.goto('/', { waitUntil: 'domcontentloaded' });
        const toppedUp = await runTurn(
          memberPage,
          'Non usare strumenti. Rispondi soltanto con AURA_MEMBER_TURN_OK',
        );
        expect(toppedUp.some((frame) => frame.type === 'RUN_ERROR')).toBe(false);
        expect(toppedUp.some((frame) => frame.type === 'RUN_FINISHED')).toBe(true);
      }).toPass({ timeout: 180_000, intervals: [30_000] });
      await memberPage.context().close();
    } finally {
      await removeIdentity(page, memberEmail);
    }
    const afterRemoval = await openRouter<OpenRouterKey[]>(page, '/keys');
    expect(afterRemoval.some((key) => key.name === member.id && !key.disabled)).toBe(false);
  });

  test('follows the services cap an admin changes after the first run', async ({
    page,
  }, testInfo) => {
    test.skip(testInfo.project.name !== 'chrome', 'one live account witness is sufficient');
    // Long enough for both polls: a test timeout that fires first skips the finally below and
    // leaves the raised cap stored.
    test.setTimeout(300_000);
    expect(managementKey, 'AURA_E2E_OPENROUTER_MANAGEMENT_KEY').not.toBe('');
    await gotoAuthenticated(page, '/');
    const servicesLimit = async () =>
      activeKey(await openRouter<OpenRouterKey[]>(page, '/keys'), SERVICES_KEY_NAME).limit;
    expect(await servicesLimit()).toBe(servicesCap);

    // The cap lives in Model routing. Saving it must move the key OpenRouter enforces, not only
    // the stored value: until 2026-09-11 the cap was read when the key was minted and never again.
    const saveCap = async (value: number) => {
      await page.goto('/?settings=model', { waitUntil: 'domcontentloaded' });
      const field = page.getByLabel('Services key monthly cap (USD)');
      await expect(field).toBeVisible({ timeout: 30_000 });
      await field.fill(String(value));
      await page.getByRole('button', { name: 'Save runtime settings' }).click();
      await expect(page.getByText('Runtime settings saved.')).toBeVisible({ timeout: 60_000 });
      await expect.poll(servicesLimit, { timeout: 60_000, intervals: [2_000] }).toBe(value);
    };
    try {
      await saveCap(servicesCap + 1);
    } finally {
      await saveCap(servicesCap);
    }
  });
});
