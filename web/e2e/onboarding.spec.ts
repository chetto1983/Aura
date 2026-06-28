import { expect, test, type Locator, type Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';

// onboarding.spec.ts - Phase 28 Plan 06 E2E. It proves the full-screen onboarding wizard is a
// shell overlay, not a governance tab, and drives the happy path against mocked Plan-05 routes:
// credentials -> capability picker -> interview answer/edit/confirm -> review -> provision ->
// Telegram deep-link + QR -> linked poll. The same test runs on chromium and mobile-chrome.

const CONV_ID = '99999999-9999-9999-9999-999999999999';
const SESSION_TOKEN = 'sess-e2e-28-06';
const PASSWORD = 'E2E-Super-Secret-PW-28406';
const SECURITY_QUESTION = 'First school?';
const SECURITY_ANSWER = 'E2E-Recovery-Answer-28406';
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
        step: 'identity',
        content: 'What should Aura call you?',
        status: 'active',
        capabilityOptions: ['skills.read', 'scheduler.read'],
      }),
    }),
  );

  await page.route('**/api/onboarding/*/step', async (route) => {
    const body = (await route.request().postDataJSON()) as { intent?: string };
    if (body.intent === 'answer') {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          content: 'Draft ready. Confirm, edit, or skip.',
          step: 'draft',
          status: 'draft',
          draft: '# Agent.md\n\nName: Dav',
        }),
      });
    }
    if (body.intent === 'edit') {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          content: 'Draft updated.',
          step: 'draft',
          status: 'draft',
          draft: '# Agent.md\n\nName: Davide',
        }),
      });
    }
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        content: 'Profile confirmed.',
        step: 'draft',
        status: 'completed',
        draft: '# Agent.md\n\nName: Davide',
      }),
    });
  });

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

  const createButtons = page.getByRole('button', { name: 'Create identity' });
  const openNavigation = page.getByRole('button', { name: 'Open navigation' });
  await expect
    .poll(
      async () =>
        (await hasVisibleCandidate(createButtons)) ||
        (await openNavigation.isVisible().catch(() => false)),
      { timeout: 10_000 },
    )
    .toBe(true);
  if (!(await hasVisibleCandidate(createButtons))) {
    await openNavigation.click();
  }
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
      await expect(page.getByText(/Step 1 of 5/)).toBeVisible();
    }

    await page.getByLabel('Operator email').fill('new@example.com');
    await page.getByLabel('Initial password').fill(PASSWORD);
    await page.getByLabel('Security question').fill(SECURITY_QUESTION);
    await page.getByLabel('Security answer').fill(SECURITY_ANSWER);
    await page.getByRole('button', { name: 'Continue' }).click();

    await expect(page.getByText('Capabilities for the new identity')).toBeVisible();
    await expect(page.getByRole('checkbox', { name: '*' })).toHaveCount(0);
    await page.getByRole('checkbox', { name: /skills\.read/ }).check();
    await page.getByRole('button', { name: 'Continue' }).click();

    await page.getByLabel('Your answer').fill('Call me Dav');
    await page.getByRole('button', { name: 'Continue' }).click();
    await expect(page.getByText(/Name: Dav/)).toBeVisible();

    await page.getByRole('button', { name: 'Edit answer' }).click();
    await page.getByRole('textbox').nth(0).fill('Davide');
    await page.getByRole('button', { name: 'Save changes' }).click();
    await expect(page.getByText(/Name: Davide/)).toBeVisible();

    await page.getByRole('button', { name: /Looks right/ }).click();
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
      },
    ]);
  });
});
