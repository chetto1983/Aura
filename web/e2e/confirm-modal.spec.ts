import { test, expect } from './fixtures';

/**
 * Custom modal smoke — covers the imperative confirm()/prompt() shipped in
 * web/src/components/common/ConfirmModal.tsx that replaced window.confirm
 * / window.prompt for destructive actions.
 *
 * The modal is invoked from real call-sites (Conversations purge prompt,
 * skill/task delete confirms). These tests never click the final Confirm
 * button on real data — they only exercise the open/cancel/validation
 * paths so the live install is left untouched.
 */

test.describe('ConfirmModal — prompt flow on /conversations', () => {
  test('opens prompt with default value, validates, and Esc cancels', async ({ authedPage: page }) => {
    await page.goto('/conversations');

    const purgeBtn = page.getByRole('button', { name: /purge older than/i });
    // Button is disabled until stats load and total_rows > 0; if the
    // archive is empty the button is disabled and the modal can't open.
    await expect(purgeBtn).toBeVisible({ timeout: 5_000 });
    if (await purgeBtn.isDisabled()) {
      test.skip(true, 'No archived turns — purge button disabled, prompt cannot open');
      return;
    }

    await purgeBtn.click();

    // Modal should mount with title + prefilled "30" + numeric inputMode.
    const modal = page.getByTestId('confirm-modal');
    await expect(modal).toBeVisible({ timeout: 3_000 });
    await expect(modal.getByText(/purge older turns/i)).toBeVisible();

    const input = page.getByTestId('confirm-modal-input');
    await expect(input).toBeFocused();
    await expect(input).toHaveValue('30');
    await expect(input).toHaveAttribute('inputmode', 'numeric');

    // Validation: typing "0" then submitting via Enter should surface
    // an inline error rather than dispatching the cleanup.
    await input.fill('0');
    await input.press('Enter');
    await expect(page.getByTestId('confirm-modal-error')).toHaveText(/positive integer/i);
    // Modal stays open after a failed validation.
    await expect(modal).toBeVisible();

    // Esc closes — the modal should unmount and no toast should fire.
    await page.keyboard.press('Escape');
    await expect(modal).toBeHidden({ timeout: 2_000 });
  });
});

test.describe('ConfirmModal — destructive confirm on /skills', () => {
  test('Delete button opens destructive confirm; Cancel closes without firing', async ({ authedPage: page, request }) => {
    // Need at least one local skill to have a Delete button to click.
    const TOKEN = process.env.AURA_E2E_TOKEN!;
    const skills = await request.get('/api/skills', {
      headers: { Authorization: `Bearer ${TOKEN}` },
    });
    expect(skills.status()).toBe(200);
    const list = (await skills.json()) as Array<{ name: string }>;
    if (list.length === 0) {
      test.skip(true, 'No local skills installed — delete button cannot render');
      return;
    }
    const targetName = list[0].name;

    await page.goto('/skills');
    // Click the first row's Delete button — there's no per-row test id,
    // so use role + name. SkillsPanel renders the name as font-mono.
    const deleteBtn = page.locator('button', { hasText: 'Delete' }).first();
    await expect(deleteBtn).toBeVisible({ timeout: 5_000 });
    await deleteBtn.click();

    const modal = page.getByTestId('confirm-modal');
    await expect(modal).toBeVisible({ timeout: 3_000 });
    await expect(modal.getByText(new RegExp(`Delete skill "${targetName}"`))).toBeVisible();

    // Destructive confirms should focus Cancel by default — Enter must
    // not destroy data on accidental keypress.
    const cancel = page.getByTestId('confirm-modal-cancel');
    await expect(cancel).toBeFocused({ timeout: 2_000 });

    // Confirm button has the destructive variant.
    const confirmBtn = page.getByTestId('confirm-modal-confirm');
    await expect(confirmBtn).toHaveAttribute('data-variant', 'destructive');

    // Cancel closes the modal without invoking the API.
    await cancel.click();
    await expect(modal).toBeHidden({ timeout: 2_000 });

    // Re-fetch to verify the skill still exists.
    const after = await request.get('/api/skills', {
      headers: { Authorization: `Bearer ${TOKEN}` },
    });
    const afterList = (await after.json()) as Array<{ name: string }>;
    expect(afterList.find((s) => s.name === targetName)).toBeTruthy();
  });
});

test.describe('ConfirmModal — overlay click cancels', () => {
  test('clicking outside dismisses the modal', async ({ authedPage: page }) => {
    await page.goto('/conversations');
    const purgeBtn = page.getByRole('button', { name: /purge older than/i });
    await expect(purgeBtn).toBeVisible({ timeout: 5_000 });
    if (await purgeBtn.isDisabled()) {
      test.skip(true, 'No archived turns — purge button disabled');
      return;
    }
    await purgeBtn.click();
    const modal = page.getByTestId('confirm-modal');
    await expect(modal).toBeVisible();
    // Click in the top-left corner of the viewport — well outside
    // the dialog content but inside the Radix overlay.
    await page.mouse.click(5, 5);
    await expect(modal).toBeHidden({ timeout: 2_000 });
  });
});
