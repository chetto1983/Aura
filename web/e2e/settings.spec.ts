import { test, expect } from './fixtures';

/**
 * Slice 14d - runtime settings page E2E.
 *
 * Covers the auth'd /settings dashboard surface end-to-end:
 *   - sidebar nav item visible
 *   - panel renders with grouped sections
 *   - inputs reflect current values from the backend
 *   - dirty-tracking shows the revert affordance
 *   - Save round-trips through POST /settings (proven by reading the
 *     value back after save)
 */

test.describe('settings page (14d)', () => {
  test('sidebar has Settings nav item', async ({ authedPage: page }) => {
    await page.goto('/');
    const link = page.getByRole('navigation').getByRole('link', { name: /^settings$/i });
    await expect(link).toBeVisible();
  });

  test('panel renders grouped sections', async ({ authedPage: page }) => {
    await page.goto('/settings');
    await expect(page.getByRole('heading', { name: /^settings$/i })).toBeVisible();
    // Each group renders an h2 - check at least the LLM provider one.
    await expect(page.getByRole('heading', { name: /container runtime/i })).toBeVisible();
    await expect(page.getByRole('heading', { name: /llm provider/i })).toBeVisible();
    await expect(page.getByRole('heading', { name: /code sandbox/i })).toBeVisible();
    await expect(page.getByRole('heading', { name: /budget/i })).toBeVisible();
    await expect(page.getByRole('heading', { name: /agent orchestration/i })).toBeVisible();
    await expect(page.locator('input#AURA_ENV_PATH')).toBeVisible();
    await expect(page.locator('input#DB_PATH')).toBeVisible();
    await expect(page.locator('input#AURA_PROMPT_VERSION')).toBeVisible();
    const sandboxSwitch = page.getByRole('switch', { name: /sandbox enabled/i });
    await expect(sandboxSwitch).toBeVisible();
    await expect(page.locator('input#AURA_ENV_PATH')).toBeEnabled();
    await expect(sandboxSwitch).toBeEnabled();
  });

  test('Test connection button is present and disabled when no base URL', async ({ authedPage: page }) => {
    await page.goto('/settings');
    const btn = page.getByRole('button', { name: /test connection/i });
    await expect(btn).toBeVisible();
    // Save button starts disabled because no edits yet.
    const saveBtn = page.getByRole('button', { name: /^save$/i });
    await expect(saveBtn).toBeDisabled();
  });

  test('editing a field marks it dirty and Save round-trips through the API', async ({ authedPage: page, request }) => {
    await page.goto('/settings');

    // Pick a low-impact numeric field. Avoid provider/model settings here:
    // if a run is interrupted after saving, a stranded fake model can break
    // the live scheduled agent jobs that share this dashboard.
    const key = 'SUMMARIZER_COOLDOWN_SECONDS';
    const input = page.locator(`input#${key}`);
    await expect(input).toBeVisible({ timeout: 5_000 });

    const original = (await input.inputValue()) || '';
    const n = Number.parseInt(original, 10);
    const probe = String((Number.isFinite(n) ? n : 300) + 1);
    const token = process.env.AURA_E2E_TOKEN!;

    try {
      await input.fill(probe);
      // Dirty: revert affordance appears, Save gains a count.
      await expect(page.getByRole('button', { name: /save\s+(\(\d+\)|\u00b7\s*\d+)/i })).toBeEnabled();

      // Save and wait for the request to settle.
      await Promise.all([
        page.waitForResponse((r) => r.url().endsWith('/api/settings') && r.request().method() === 'POST'),
        page.getByRole('button', { name: /save/i }).click(),
      ]);

      // After save the Save button should drop back to disabled (no
      // pending changes).
      await expect(page.getByRole('button', { name: /^save$/i })).toBeDisabled({ timeout: 5_000 });
    } finally {
      // Restore via the API so failed assertions after Save do not leave the
      // running install with this probe setting.
      const reset = await request.post('/api/settings', {
        headers: { Authorization: `Bearer ${token}` },
        data: { updates: { [key]: original } },
      });
      expect(reset.status()).toBe(200);
    }
  });

  test('boolean fields render as a switch and toggle marks dirty', async ({ authedPage: page }) => {
    await page.goto('/settings');
    // OCR_ENABLED is a bool in the catalog - should render with role="switch".
    const sw = page.getByRole('switch', { name: /ocr enabled/i });
    await expect(sw).toBeVisible({ timeout: 5_000 });

    const before = await sw.isChecked();
    await sw.click();
    const after = await sw.isChecked();
    expect(after).not.toBe(before);

    // Save activates.
    await expect(page.getByRole('button', { name: /save\s+(\(\d+\)|\u00b7\s*\d+)/i })).toBeEnabled();
    // Revert without saving so we don't leave OCR flipped on the running bot.
    await page.getByRole('button', { name: /revert/i }).first().click();
    await expect(page.getByRole('button', { name: /^save$/i })).toBeDisabled();
  });

  test('enum fields render as a dropdown with the catalog options', async ({ authedPage: page }) => {
    await page.goto('/settings');
    // SUMMARIZER_MODE is an enum - should be a <select> with off/review/auto.
    const select = page.locator('select#SUMMARIZER_MODE');
    await expect(select).toBeVisible({ timeout: 5_000 });
    const optionTexts = await select.locator('option').allTextContents();
    expect(optionTexts).toEqual(expect.arrayContaining(['off', 'review', 'auto']));

    const webSearchProvider = page.locator('select#WEB_SEARCH_PROVIDER');
    await expect(webSearchProvider).toBeVisible();
    await expect(webSearchProvider).toHaveValue(/^(disabled|searxng)$/);
    const webSearchOptions = await webSearchProvider.locator('option').allTextContents();
    expect(webSearchOptions).toEqual(['disabled', 'searxng']);
  });

  test('orchestration settings save and reload with enum and bounded numeric controls', async ({ authedPage: page, request }) => {
    await page.goto('/settings');

    const delegation = page.locator('select#AURA_DELEGATION_MODE');
    const retention = page.locator('input#AURA_TRACE_RETENTION_DAYS');
    await expect(delegation).toBeVisible({ timeout: 5_000 });
    await expect(retention).toBeVisible();

    const originalDelegation = await delegation.inputValue();
    const originalRetention = await retention.inputValue();
    const probeDelegation = originalDelegation === 'bounded' ? 'fast' : 'bounded';
    const probeRetention = originalRetention === '31' ? '32' : '31';
    const token = process.env.AURA_E2E_TOKEN!;

    try {
      await delegation.selectOption(probeDelegation);
      await retention.fill(probeRetention);
      await expect(page.getByRole('button', { name: /save\s+(\(\d+\)|\u00b7\s*\d+)/i })).toBeEnabled();

      await Promise.all([
        page.waitForResponse((r) => r.url().endsWith('/api/settings') && r.request().method() === 'POST'),
        page.getByRole('button', { name: /save/i }).click(),
      ]);
      await expect(page.getByRole('button', { name: /^save$/i })).toBeDisabled({ timeout: 5_000 });

      await page.reload();
      await expect(page.locator('select#AURA_DELEGATION_MODE')).toHaveValue(probeDelegation);
      await expect(page.locator('input#AURA_TRACE_RETENTION_DAYS')).toHaveValue(probeRetention);
    } finally {
      const reset = await request.post('/api/settings', {
        headers: { Authorization: `Bearer ${token}` },
        data: {
          updates: {
            AURA_DELEGATION_MODE: originalDelegation,
            AURA_TRACE_RETENTION_DAYS: originalRetention,
          },
        },
      });
      expect(reset.status()).toBe(200);
    }
  });
});
