import { expect, type Page } from '@playwright/test';

// Compact-chat spec AC-8 (docs/superpowers/specs/2026-07-23-cockpit-compact-chat-ui-spec.md):
// typed evidence displays render BEHIND the collapsed 44px tool row — every state is
// collapsed by default. E2E tests asserting on display content must expand the row first.
// system_event / local_artifact payloads render inline with NO row (AC-9) — never call
// this for those.
export async function expandToolRow(page: Page, nth = 0): Promise<void> {
  const trigger = page.getByTestId('tool-row').nth(nth).locator('button[aria-expanded]').first();
  await expect(trigger).toBeVisible({ timeout: 15000 });
  if ((await trigger.getAttribute('aria-expanded')) === 'true') return;
  await trigger.click();
  await expect(trigger).toHaveAttribute('aria-expanded', 'true');
}
