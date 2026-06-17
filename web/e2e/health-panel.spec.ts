import { expect, test } from '@playwright/test';
import { gotoAuthenticated } from './auth';

// WEB-04: the embedded operator shell renders a read-only runtime health panel that
// polls the existing GET /healthz + GET /readyz (no new backend endpoint, D-07) and
// pairs a status dot with a TEXT label per row (never colour-only, WCAG 1.4.1). This
// spec drives the panel against the real served shell, alongside the theme-before-paint
// (D-08) contract the login + health surfaces inherit from Phase 23.

test.describe('runtime health panel', () => {
  test('applies dark theme + operator density before paint', async ({ page }) => {
    await gotoAuthenticated(page, '/');
    const html = page.locator('html');
    // theme-before-paint (D-08): attributes are present on the first response HTML,
    // set synchronously by the index.html pre-paint script — no flash.
    await expect(html).toHaveAttribute('data-theme', 'dark');
    await expect(html).toHaveAttribute('data-density', /compact|operator|review/);
  });

  test('renders the Runtime panel with dot+label status rows over /healthz + /readyz', async ({
    page,
  }) => {
    await gotoAuthenticated(page, '/');

    const panel = page.getByRole('region', { name: /runtime/i });
    await expect(panel.getByRole('heading', { name: 'Runtime' })).toBeVisible();

    // Static row labels prove the rows rendered (the non-pending, non-unreachable
    // branch) rather than the "Checking runtime…" / error states.
    for (const label of ['Liveness', 'Readiness', 'Postgres', 'Neo4j', 'Bind address', 'Build']) {
      await expect(panel.getByText(label, { exact: true })).toBeVisible();
    }

    // At least one row pairs a status DOT with a visible TEXT label (status-not-colour-only).
    // PG is up for the e2e stack, so liveness resolves to "Live"; tolerate "Unavailable"
    // if the probe is briefly not ready so the assertion is about the text label, not the dot.
    await expect(panel.getByText(/^(Live|Unavailable)$/)).toBeVisible();
  });
});
