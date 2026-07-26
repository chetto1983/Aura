import { expect, test, type Locator, type Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';

// onboarding.spec.ts - Phase 28 Plan 06 E2E. It proves the full-screen onboarding wizard is a
// shell overlay, not a governance tab, and drives the happy path against mocked routes:
// credentials + seed form -> capability picker -> review -> provision -> Telegram deep-link + QR
// -> linked poll. The same test runs on chromium and mobile-chrome. Amendment #95 removed the
// interview phase, so the stepper is four phases and the seed rides in the /provision body.

const CONV_ID = '99999999-9999-9999-9999-999999999999';
const SESSION_TOKEN = 'sess-e2e-28-06';
const PASSWORD = 'E2E-Super-Secret-PW-28406';
const SECURITY_QUESTION = 'First school?';
const SECURITY_ANSWER = 'E2E-Recovery-Answer-28406';
// Non-ASCII on purpose: the seed must reach the wire byte-identical (Amendment #95).
const OPERATOR_NAME = 'José-María';
const DEEP_LINK = 'https://t.me/AuraBot?start=onb-e2e-28-06';
const BOT_TOKEN = '1234567890:AAH-this-token-must-not-render';
const QR_SVG =
  '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><rect width="16" height="16"/></svg>';

async function clickFirstVisible(locator: Locator) {
  const count = await locator.count();
  for (let i = 0; i < count; i += 1) {
    const candidate = locator.nth(i);
    if (await candidate.isVisible()) {
      await candidate.click();
      return;
    }
  }
  throw new Error('no visible candidate found');
}

async function hasVisibleCandidate(locator: Locator) {
  const count = await locator.count();
  for (let i = 0; i < count; i += 1) {
    if (await locator.nth(i).isVisible()) {
      return true;
    }
  }
  return false;
}

async function waitForVisibleCandidate(locator: Locator) {
  await expect.poll(async () => hasVisibleCandidate(locator), { timeout: 10_000 }).toBe(true);
}

async function installShellRoutes(page: Page) {
  await page.route('**/api/settings', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ restart_required: false, settings: [] }),
    }),
  );
  await page.route('**/api/conversations*', (route) => {
    if (route.request().url().includes(`/api/conversations/${CONV_ID}`)) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          id: CONV_ID,
          title: 'Onboarding thread',
          status: 'active',
          total_input_tokens: 0,
          total_output_tokens: 0,
          total_cached_tokens: 0,
          total_cost_usd: 0,
        }),
      });
    }
    return route.fulfill({ status: 200, contentType: 'application/json', body: '[]' });
  });
  await page.route('**/api/conversations/*/rot-events', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  );
  await page.route('**/api/approvals', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  );
  await page.route('**/threads/*/messages', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ type: 'MESSAGES_SNAPSHOT', messages: [] }),
    }),
  );
}

async function installOnboardingRoutes(page: Page, provisionBodies: unknown[]) {
  let telegramPolls = 0;

  await page.route('**/api/onboarding/start', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        sessionToken: SESSION_TOKEN,
        capabilityOptions: ['skills.read', 'scheduler.read'],
      }),
    }),
  );

  await page.route('**/api/onboarding/*/provision', async (route) => {
    provisionBodies.push(await route.request().postDataJSON());
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        identityId: 'identity-e2e',
        deepLink: DEEP_LINK,
        qrSvg: QR_SVG,
      }),
    });
  });

  await page.route('**/api/onboarding/*/telegram-status', (route) => {
    telegramPolls += 1;
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ linked: telegramPolls >= 2 }),
    });
  });
}

async function openOnboarding(page: Page) {
  await gotoAuthenticated(page, `/c/${CONV_ID}`);

  const settingsButtons = page.getByRole('button', { name: 'Settings' });
  const openNavigation = page.getByRole('button', { name: 'Open navigation' });
  await expect
    .poll(
      async () =>
        (await hasVisibleCandidate(settingsButtons)) ||
        (await openNavigation.isVisible().catch(() => false)),
      { timeout: 10_000 },
    )
    .toBe(true);
  if (!(await hasVisibleCandidate(settingsButtons))) {
    await openNavigation.click();
  }
  await waitForVisibleCandidate(settingsButtons);
  await clickFirstVisible(settingsButtons);
  const createButtons = page.getByRole('button', { name: 'Create identity' });
  await waitForVisibleCandidate(createButtons);
  await clickFirstVisible(createButtons);
  await expect(page.getByRole('dialog', { name: 'Create identity' })).toBeVisible();
}

test.describe('Phase 28 Plan 06 - Onboarding wizard (desktop + mobile)', () => {
  test('drives the full provisioning flow and keeps secrets out of rendered DOM', async ({
    page,
  }, testInfo) => {
    const provisionBodies: unknown[] = [];
    await installShellRoutes(page);
    await installOnboardingRoutes(page, provisionBodies);

    await openOnboarding(page);

    const dialog = page.getByRole('dialog', { name: 'Create identity' });
    const viewport = page.viewportSize();
    const box = await dialog.boundingBox();
    if (viewport !== null && box !== null) {
      expect(Math.round(box.width)).toBeGreaterThanOrEqual(viewport.width - 2);
      expect(Math.round(box.height)).toBeGreaterThanOrEqual(viewport.height - 2);
    }
    if (testInfo.project.name.includes('mobile')) {
      await expect(page.getByText(/Step 1 of 4/)).toBeVisible();
    }

    await dialog.getByRole('textbox', { name: 'Operator email' }).fill('new@example.com');
    await dialog.getByRole('textbox', { name: 'Initial password', exact: true }).fill(PASSWORD);
    await dialog
      .getByRole('textbox', { name: 'Confirm initial password', exact: true })
      .fill(PASSWORD);
    await dialog.getByRole('textbox', { name: 'Security question' }).fill(SECURITY_QUESTION);
    await dialog
      .getByRole('textbox', { name: 'Security answer', exact: true })
      .fill(SECURITY_ANSWER);

    // The Amendment-#95 seed form lives in the credentials phase. Leaving the WHOLE form blank is
    // valid (that is the skip); filling any field makes Name required, because the mapper keys
    // every fact off it. The typed value must reach /provision unchanged.
    await dialog.getByRole('textbox', { name: 'Name', exact: true }).fill(OPERATOR_NAME);
    await dialog.getByRole('combobox', { name: 'Language' }).selectOption('it');
    await dialog.getByRole('textbox', { name: 'Where you are' }).fill('Caraglio');
    // combobox, not textbox: the time-zone input carries a `list` attribute for the IANA
    // datalist (asserted in SeedProfileForm.test.tsx), and HTML-AAM maps input[list] to
    // combobox. The unit test queries by label so it never saw the role; only a browser did.
    await dialog.getByRole('combobox', { name: 'Time zone' }).fill('Europe/Rome');
    await dialog.getByRole('textbox', { name: 'What you do' }).fill('founder');
    await dialog.getByRole('textbox', { name: 'Organisation' }).fill('PmSync');
    await page.getByRole('button', { name: 'Continue' }).click();

    await expect(page.getByText('Capabilities for the new identity')).toBeVisible();
    await expect(page.getByRole('checkbox', { name: '*' })).toHaveCount(0);
    await page.getByRole('checkbox', { name: /skills\.read/ }).check();
    await page.getByRole('button', { name: 'Continue' }).click();

    await expect(page.getByText('Review and create')).toBeVisible();
    await expect(page.getByText('new@example.com')).toBeVisible();
    await expect(page.getByText('skills.read')).toBeVisible();
    await expect(page.getByText(PASSWORD)).toHaveCount(0);

    await dialog.getByRole('button', { name: 'Create identity' }).click();
    await expect(page.getByText('Identity created')).toBeVisible();

    const telegram = page.getByRole('link', { name: 'Open in Telegram' });
    await expect(telegram).toBeVisible();
    await expect(telegram).toHaveAttribute('href', DEEP_LINK);
    await expect(page.getByRole('img', { name: 'QR code to link Telegram' })).toBeVisible();

    const html = await page.locator('body').innerHTML();
    expect(html).not.toContain(PASSWORD);
    expect(html).not.toContain(SECURITY_ANSWER);
    expect(html).not.toContain(BOT_TOKEN);

    await expect(page.getByText('Telegram linked')).toBeVisible({ timeout: 5000 });

    expect(provisionBodies).toEqual([
      {
        email: 'new@example.com',
        password: PASSWORD,
        securityQuestion: SECURITY_QUESTION,
        securityAnswer: SECURITY_ANSWER,
        capabilities: ['skills.read'],
        linkTelegram: true,
        seed: {
          name: OPERATOR_NAME,
          lang: 'it',
          location: 'Caraglio',
          timezone: 'Europe/Rome',
          role: 'founder',
          company: 'PmSync',
        },
      },
    ]);
  });
});
