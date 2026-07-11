import { expect, test, type Page } from '@playwright/test';

// password-reset.spec.ts - Phase 43 Plan 04 E2E (R5). It proves the online forgot-password flow
// end to end against page.route()-mocked backend legs (D-10: ZERO dependency on the Go break-glass
// command or a live backend), reached from the UNAUTHENTICATED LoginPage "Forgot password?" button
// (never gotoAuthenticated). Two paths:
//   happy - recovery configured: /start -> Telegram code -> security question -> /verify -> set new
//           password -> lands on the "Password updated" done panel; no typed secret in the DOM.
//   deny  - recovery missing: /start returns the SAME neutral notice (anti-enumeration) and a denied
//           /question surfaces the GENERIC error, naming no recovery factor (no identity_recovery /
//           telegram_accounts in the DOM).
// The real backend deny branch (LookupRecoveryByEmail INNER JOIN -> 0 rows) is covered by the R6
// backend tests, not the browser. Mirrors web/e2e/onboarding.spec.ts. Runs on chromium + mobile-chrome.

const OPERATOR_EMAIL = 'operator@example.com';
const TELEGRAM_CODE = '424242';
const SECURITY_QUESTION = 'First school?';
const SECURITY_ANSWER = 'E2E-Recovery-Answer-43404';
const NEW_PASSWORD = 'E2E-Super-Secret-PW-43404';
const RESET_TOKEN = 'reset-token-e2e-43-04';

// The neutral /start notice and the generic /question error are asserted with the SAME constant on
// both paths, so the deny path proves character-for-character parity with the happy path (no
// account/factor enumeration). These are the exact en strings from resources.login.ts.
const NEUTRAL_NOTICE = 'If that account has recovery enabled, check Telegram now.';
const GENERIC_ERROR = "Couldn't reset the password. Check the details and try again.";
const DONE_TITLE = 'Password updated';
const BACK_TO_SIGN_IN = 'Back to sign in';

// installAuthConfig pins /api/auth/config to a bootstrap-unavailable Authula provider so the login
// view renders the credentials form (with "Forgot password?"), not the first-user bootstrap panel.
// The reset flow itself uses plain postJSON (no CSRF header), so the token here is inert filler.
async function installAuthConfig(page: Page) {
  await page.route('**/api/auth/config', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        provider: 'authula',
        auth_base_path: '/auth',
        csrf_cookie_name: '__Host-authula_csrf_token',
        csrf_header_name: 'X-AUTHULA-CSRF-TOKEN',
        csrf_token: 'e2e-reset-csrf',
        bootstrap_available: false,
      }),
    }),
  );
}

// installHappyResetRoutes mocks all four steps so the flow completes: /start neutral ok, /question
// reveals the security question, /verify mints a reset token, /complete succeeds.
async function installHappyResetRoutes(page: Page) {
  await page.route('**/api/auth/password-reset/start', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ status: 'ok' }),
    }),
  );
  await page.route('**/api/auth/password-reset/question', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ question: SECURITY_QUESTION }),
    }),
  );
  await page.route('**/api/auth/password-reset/verify', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ resetToken: RESET_TOKEN }),
    }),
  );
  await page.route('**/api/auth/password-reset/complete', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ status: 'ok' }),
    }),
  );
}

// installDenyResetRoutes keeps /start ALWAYS neutral (the anti-enumeration invariant) and denies
// /question with a 403, driving fetchPasswordResetQuestion to throw so the panel's catch sets the
// generic error. /verify and /complete are intentionally unmocked - the deny path never reaches them.
async function installDenyResetRoutes(page: Page) {
  await page.route('**/api/auth/password-reset/start', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ status: 'ok' }),
    }),
  );
  await page.route('**/api/auth/password-reset/question', (route) =>
    route.fulfill({
      status: 403,
      contentType: 'application/json',
      body: JSON.stringify({ error: 'denied' }),
    }),
  );
}

// openResetPanel reaches the PasswordResetPanel from the unauthenticated LoginPage. It forces the en
// locale (so the asserted copy is deterministic), then clicks "Forgot password?".
async function openResetPanel(page: Page) {
  await page.addInitScript(() => {
    window.localStorage.setItem('aura.language', 'en');
  });
  await installAuthConfig(page);
  await page.goto('/login', { waitUntil: 'domcontentloaded' });

  const forgot = page.getByRole('button', { name: 'Forgot password?' });
  await expect(forgot).toBeVisible();
  await forgot.click();
  await expect(page.getByRole('heading', { name: 'Password reset' })).toBeVisible();
}

test.describe('Phase 43 Plan 04 - Forgot-password E2E (desktop + mobile)', () => {
  test('happy path completes to "Password updated" and keeps the new password out of the DOM', async ({
    page,
  }) => {
    await installHappyResetRoutes(page);
    await openResetPanel(page);

    await page.getByLabel('Operator email', { exact: true }).fill(OPERATOR_EMAIL);
    await page.getByRole('button', { name: 'Send Telegram code' }).click();

    await expect(page.getByText(NEUTRAL_NOTICE)).toBeVisible();

    await page.getByLabel('Telegram code', { exact: true }).fill(TELEGRAM_CODE);
    await page.getByRole('button', { name: 'Continue' }).click();

    await expect(page.getByText(SECURITY_QUESTION)).toBeVisible();

    await page.getByLabel('Security answer', { exact: true }).fill(SECURITY_ANSWER);
    await page.getByRole('button', { name: 'Verify' }).click();

    await page.getByLabel('New password', { exact: true }).fill(NEW_PASSWORD);
    await page.getByLabel('Confirm new password', { exact: true }).fill(NEW_PASSWORD);
    await page.getByRole('button', { name: 'Update password' }).click();

    await expect(page.getByRole('heading', { name: DONE_TITLE })).toBeVisible();
    await expect(page.getByRole('button', { name: BACK_TO_SIGN_IN })).toBeVisible();

    // No-leak: the typed secrets must never survive in the rendered DOM (mirror onboarding.spec.ts).
    const html = await page.locator('body').innerHTML();
    expect(html).not.toContain(NEW_PASSWORD);
    expect(html).not.toContain(SECURITY_ANSWER);
  });

  test('deny path shows the generic, non-enumerating denial with no recovery factor named', async ({
    page,
  }) => {
    await installDenyResetRoutes(page);
    await openResetPanel(page);

    await page.getByLabel('Operator email', { exact: true }).fill(OPERATOR_EMAIL);
    await page.getByRole('button', { name: 'Send Telegram code' }).click();

    // The /start notice is byte-identical to the happy path (same constant) - no account enumeration.
    await expect(page.getByText(NEUTRAL_NOTICE)).toBeVisible();

    await page.getByLabel('Telegram code', { exact: true }).fill(TELEGRAM_CODE);
    await page.getByRole('button', { name: 'Continue' }).click();

    // A denied /question surfaces exactly the generic error - never "which factor is missing".
    await expect(page.getByText(GENERIC_ERROR)).toBeVisible();

    // No factor-name disclosure: the deny DOM must not contain the backend factor identifiers. The
    // only allowed "Telegram" occurrences are the neutral notice + the code label (brand copy), never
    // the telegram_accounts / identity_recovery join columns that would reveal the missing factor.
    const html = await page.locator('body').innerHTML();
    expect(html).not.toContain('identity_recovery');
    expect(html.toLowerCase()).not.toContain('telegram_accounts');
  });
});
