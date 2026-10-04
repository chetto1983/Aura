import { createRequire } from 'node:module';
import { expect, test, type Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';
import { installGraphShellRoutes } from './support/graphRoutes';

// graph-a11y.spec.ts — the Phase 27 accessibility + auth-resilience E2E. It runs axe against the
// live Graph Explorer surface (0 serious/critical WCAG-AA violations), asserts the non-hover
// access path (tap/keyboard opens the inspector, NOT hover), the canvas accessible name, AND the
// named session-expiry case: a 401 on the next graph fetch lands on the sign-in page with a
// VISIBLE expiry notice, never a silent blank canvas (B3, threat T-27-03). axe is loaded directly from the resolvable
// axe-core engine (node_modules/axe-core/axe.js) via addScriptTag — no @axe-core/playwright dep.

const require = createRequire(import.meta.url);
const AXE_PATH = require.resolve('axe-core/axe.min.js');

const CONV_ID = '99999999-9999-9999-9999-999999999999';

const POPULATED = {
  nodes: [
    {
      id: 'n1',
      caption: 'Alpha Entity',
      labels: ['Entity'],
      degree: 2,
      props: { name: 'Alpha Entity' },
    },
    {
      id: 'n2',
      caption: 'Beta Document',
      labels: ['Document'],
      degree: 1,
      props: { url: 'https://docs.example.test/b' },
      ref_id: 'src-2',
      citations: ['Cited source X'],
    },
  ],
  edges: [{ id: 'e1', source: 'n1', target: 'n2', rel_type: 'MENTIONS' }],
  schema: { labels: ['Entity', 'Document'], rel_types: ['MENTIONS'] },
  query: 'MATCH (e:Entity)-[r]-(n) RETURN e, r, n',
};

interface AxeResult {
  violations: { id: string; impact?: string; nodes: unknown[] }[];
}

interface AxeRunner {
  run: (
    context: Document,
    options: { runOnly: { type: string; values: string[] } },
  ) => Promise<AxeResult>;
}
declare global {
  interface Window {
    axe: AxeRunner;
  }
}

async function openGraphSurface(page: Page) {
  await gotoAuthenticated(page, `/c/${CONV_ID}`);
  await page.getByRole('button', { name: 'Graph', exact: true }).first().click();
  await expect(page.getByRole('img', { name: /Memory graph:/ })).toBeVisible({ timeout: 15000 });
}

async function runAxe(page: Page): Promise<AxeResult> {
  await page.addScriptTag({ path: AXE_PATH });
  return page.evaluate(async () => {
    return window.axe.run(document, {
      runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa'] },
    });
  });
}

test.describe('Phase 27 — Graph Explorer accessibility + auth resilience', () => {
  test('the graph surface has no serious/critical WCAG-AA axe violations', async ({ page }) => {
    await installGraphShellRoutes(page, CONV_ID);
    await page.route('**/api/graph/schema', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(POPULATED.schema),
      }),
    );
    await page.route('**/api/graph/query', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(POPULATED),
      }),
    );
    await openGraphSurface(page);

    const result = await runAxe(page);
    const serious = result.violations.filter(
      (v) => v.impact === 'serious' || v.impact === 'critical',
    );
    expect(serious, JSON.stringify(serious.map((v) => v.id))).toHaveLength(0);
  });

  test('tap/keyboard opens the inspector (hover is never the only access path, D-03)', async ({
    page,
  }) => {
    await installGraphShellRoutes(page, CONV_ID);
    await page.route('**/api/graph/schema', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(POPULATED.schema),
      }),
    );
    await page.route('**/api/graph/query', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(POPULATED),
      }),
    );
    await openGraphSurface(page);

    // Keyboard: focus the node-list item and press Enter — the inspector opens (no hover).
    const item = page.getByRole('button', { name: /Alpha Entity/ });
    await item.focus();
    await page.keyboard.press('Enter');
    await expect(page.getByRole('button', { name: 'Pin path' })).toBeVisible();
  });

  test('session expires mid-workspace sends the person to sign in again, not a blank canvas', async ({
    page,
  }) => {
    // B3 / T-27-03: open the workspace authenticated, then make the NEXT graph fetch return 401
    // (expired/absent session). The expiry must be visible, never a silent blank. Since
    // api/sessionExpiry.ts the auth gate's 401, the one carrying the session challenge, sends
    // the whole cockpit to the login page with the expiry notice and the way back (Authula
    // SPEC §4.8), so that is what the person sees.
    await installGraphShellRoutes(page, CONV_ID);
    await page.route('**/api/graph/schema', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(POPULATED.schema),
      }),
    );

    let firstQuery = true;
    await page.route('**/api/graph/query', (route) => {
      if (firstQuery) {
        firstQuery = false;
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(POPULATED),
        });
      }
      // The session has expired — the second fetch is rejected 401.
      return route.fulfill({
        status: 401,
        headers: { 'WWW-Authenticate': 'Session' },
        contentType: 'application/json',
        body: '{"error":"unauthorized"}',
      });
    });

    await openGraphSurface(page);

    // Trigger the next graph refresh; the expired authentication now returns 401.
    await page.getByRole('button', { name: 'Load memory graph' }).first().click();

    // A VISIBLE expiry on the sign-in page, with the way back, never a blank canvas.
    await expect(page).toHaveURL(`/login?expired=1&next=${encodeURIComponent(`/c/${CONV_ID}`)}`, {
      timeout: 10000,
    });
    await expect(page.getByText('Your session expired. Sign in again to continue.')).toBeVisible();
    await expect(page.getByRole('img', { name: /Memory graph:/ })).toHaveCount(0);
  });
});
