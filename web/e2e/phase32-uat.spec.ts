import { expect, test, type Locator, type Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';

// phase32-uat.spec.ts — automates the two human-verification items recorded in
// .planning/phases/32-quality-cleanup-dead-code-shared-helpers/32-VERIFICATION.md:
//   (1) the canonical web/src/a11y/focusTrap.ts (trapTabKey) keyboard behaviour in the
//       McpLifecycleCluster RemoveDialog — Tab/Shift+Tab cycle through every focusable
//       element and wrap at the edges, focus never escapes the dialog; and
//   (2) the rich SkeletonBlock CSS-wave loading visuals in the three migrated consumers
//       (ConversationSidebar, SearchPanel, governanceView), proving the retired shadcn
//       `animate-pulse` ui/skeleton is gone (no `.animate-pulse` survives anywhere).
// Runs on desktop chromium against the already-running serve (AURA_E2E_ORIGIN reuses the
// container on 127.0.0.1:9080). The /api/* surface is mocked at the page-network layer so
// only the served SPA + Authula come from `aura serve`.

const CONV_ID = '99999999-9999-9999-9999-999999999999';

// Durable UAT evidence (relative to the web/ cwd) — committed alongside 32-UAT.md as visual proof.
const EVIDENCE_DIR = '../.planning/phases/32-quality-cleanup-dead-code-shared-helpers/uat-evidence';

function json(body: unknown) {
  return { status: 200, contentType: 'application/json', body: JSON.stringify(body) } as const;
}

const MCP_SERVERS = {
  servers: [
    {
      name: 'github',
      source: 'recipe:github',
      trust: 'trusted',
      riskPolicy: 'trusted',
      runtime: 'stdio',
      startupState: 'configured',
      authStatus: 'ok',
      profiles: [],
      networkAllowlist: [],
      envKeys: [],
    },
  ],
};

const conversationRow = {
  id: CONV_ID,
  title: 'Gov thread',
  status: 'active',
  total_input_tokens: 0,
  total_output_tokens: 0,
  total_cached_tokens: 0,
  total_cost_usd: 0,
};

// Quiet the cockpit's first-paint side fetches so each surface under test renders cleanly.
async function stubShellRoutes(page: Page) {
  await page.route('**/api/conversations/*/rot-events', (r) => r.fulfill(json([])));
  await page.route('**/api/approvals', (r) => r.fulfill(json([])));
  await page.route('**/threads/*/messages', (r) =>
    r.fulfill(json({ type: 'MESSAGES_SNAPSHOT', messages: [] })),
  );
}

// Assert a node is the rich SkeletonBlock (CSS-wave: aura-skeleton-wave + gradient), not the
// retired shadcn `animate-pulse` skeleton. Returns the computed snapshot for sizing checks.
async function waveSkeletonStyles(locator: Locator) {
  const styles = await locator.first().evaluate((el) => {
    const cs = getComputedStyle(el);
    return {
      animationName: cs.animationName,
      backgroundImage: cs.backgroundImage,
      backgroundSize: cs.backgroundSize,
      // Border-box height (the SkeletonBlock sets box-sizing:border-box + a 1px border), so this
      // equals the explicit h/w prop the migrated consumer passes. getComputedStyle().height
      // would report the content box (height − 2px border) and read as a subpixel mismatch.
      boxHeight: (el as HTMLElement).getBoundingClientRect().height,
      // The cockpit roots at 15.5px/rem (not the browser default 16px), so heights must be
      // checked in rem against the live root size — an absolute-px assertion reads as a mismatch.
      rootPx: parseFloat(getComputedStyle(document.documentElement).fontSize),
      className: (el as HTMLElement).className,
    };
  });
  expect(styles.className, 'uses the canonical .skeleton-block').toContain('skeleton-block');
  expect(styles.animationName, 'drives the aura-skeleton-wave keyframe').toBe('aura-skeleton-wave');
  expect(styles.backgroundImage, 'paints the wave gradient').toContain('linear-gradient');
  return styles;
}

function expectRemHeight(
  styles: { boxHeight: number; rootPx: number },
  rem: number,
  label: string,
) {
  const expected = rem * styles.rootPx;
  expect(
    Math.abs(styles.boxHeight - expected),
    `${label} (~${String(rem)}rem = ${String(expected)}px, got ${String(styles.boxHeight)})`,
  ).toBeLessThanOrEqual(1.5);
}

test.describe('Phase 32 UAT — focusTrap keyboard accessibility (McpLifecycleCluster RemoveDialog)', () => {
  test('Tab/Shift+Tab cycle and wrap; focus stays trapped; safe action default-focused; Escape dismisses', async ({
    page,
  }, testInfo) => {
    await stubShellRoutes(page);
    await page.route('**/api/conversations**', (route) => {
      const path = new URL(route.request().url()).pathname;
      if (path === `/api/conversations/${CONV_ID}`) return route.fulfill(json(conversationRow));
      return route.fulfill(json([]));
    });
    await page.route('**/api/governance/mcp', (r) => r.fulfill(json(MCP_SERVERS)));
    await page.route('**/api/governance/mcp/*/probe', (r) =>
      r.fulfill(json({ name: 'github', ok: true, tool_count: 4, detail: 'ok (4 tools)' })),
    );
    await page.route('**/api/governance/skills?*', (r) => r.fulfill(json({ skills: [] })));
    await page.route('**/api/governance/skills/audit*', (r) => r.fulfill(json({ rows: [] })));
    await page.route('**/api/governance/scheduler', (r) => r.fulfill(json({ tasks: [] })));

    await gotoAuthenticated(page, `/c/${CONV_ID}`);
    await page.getByRole('button', { name: 'Governance', exact: true }).first().click();

    // Open the github MCP detail (the lifecycle cluster lives in the detail pane).
    const githubRow = page.getByRole('button', { name: /github/ }).first();
    await expect(githubRow).toBeVisible({ timeout: 15000 });
    await githubRow.click();

    // Open the destructive confirmation dialog from the lifecycle cluster's toolbar.
    await page.getByRole('button', { name: 'Remove server' }).first().click();

    // Destructive confirmation uses role="alertdialog" (WAI-ARIA: interrupt semantics for an
    // irreversible action), not a plain dialog.
    const dialog = page.getByRole('alertdialog');
    await expect(dialog).toBeVisible();
    await expect(dialog).toHaveAttribute('aria-modal', 'true');
    await expect(dialog.getByText(/Remove "github"\?/)).toBeVisible();

    // The dialog's full focusable set: Keep server (safe) + Remove server (destructive).
    const keep = dialog.getByRole('button', { name: 'Keep server' });
    const remove = dialog.getByRole('button', { name: 'Remove server' });
    await expect(keep).toBeVisible();
    await expect(remove).toBeVisible();

    const focusInside = () =>
      page.evaluate(() => {
        const dlg = document.querySelector('[role="alertdialog"]');
        const active = document.activeElement;
        return dlg !== null && active !== null && dlg.contains(active);
      });

    // 1) The SAFE action (Keep server) is default-focused, never the destructive Remove (NN/g).
    await expect(keep).toBeFocused();
    expect(await focusInside()).toBe(true);

    // 2) Tab advances to the next focusable (Remove server) — focus stays inside the dialog.
    await page.keyboard.press('Tab');
    await expect(remove).toBeFocused();
    expect(await focusInside()).toBe(true);

    // 3) Tab at the LAST focusable wraps forward to the first (Keep server) — trapped.
    await page.keyboard.press('Tab');
    await expect(keep).toBeFocused();
    expect(await focusInside()).toBe(true);

    // 4) Shift+Tab at the FIRST focusable wraps backward to the last (Remove server) — trapped.
    await page.keyboard.press('Shift+Tab');
    await expect(remove).toBeFocused();
    expect(await focusInside()).toBe(true);

    await testInfo.attach('phase32-focustrap-dialog.png', {
      body: await page.screenshot({ path: `${EVIDENCE_DIR}/phase32-focustrap-dialog.png` }),
      contentType: 'image/png',
    });

    // 5) Escape dismisses the dialog without performing the destructive action.
    await page.keyboard.press('Escape');
    await expect(page.getByRole('alertdialog')).toHaveCount(0);
  });
});

test.describe('Phase 32 UAT — SkeletonBlock CSS-wave loading visuals (3 migrated consumers)', () => {
  test.beforeEach(async ({ page }) => {
    // Defeat any reduced-motion default so the wave animation is live for the computed-style check.
    await page.emulateMedia({ reducedMotion: 'no-preference' });
    await stubShellRoutes(page);
  });

  test('ConversationSidebar renders the rich skeleton while the conversation list loads', async ({
    page,
  }, testInfo) => {
    test.skip(
      testInfo.project.name !== 'chromium',
      'the left nav (conversation list) is a hidden off-canvas drawer on the mobile viewport; the skeleton is verified on desktop chromium',
    );
    // Hang the conversation LIST so the sidebar stays in its loading (skeleton) state.
    await page.route('**/api/conversations**', async (route) => {
      const path = new URL(route.request().url()).pathname;
      if (path === '/api/conversations') {
        await new Promise((res) => setTimeout(res, 12000));
        return route.fulfill(json([]));
      }
      return route.fulfill(json([]));
    });

    await gotoAuthenticated(page, '/');

    // The loading wrapper is a live region (role="status") so AT announces the loading state.
    const sidebarLoading = page.getByRole('status', { name: 'Loading conversations...' });
    await expect(sidebarLoading).toBeVisible({ timeout: 15000 });
    const sidebarSkeleton = sidebarLoading.locator('.skeleton-block');
    await expect(sidebarSkeleton.first()).toBeVisible({ timeout: 15000 });
    const styles = await waveSkeletonStyles(sidebarSkeleton);
    // Sidebar rows pair a 1rem title block (16px) with a 0.75rem subtitle block (12px);
    // the first block is the 1rem title.
    expectRemHeight(styles, 1, 'sidebar title block is 1rem tall');
    // The retired shadcn animate-pulse skeleton must be gone everywhere on the page.
    await expect(page.locator('.animate-pulse')).toHaveCount(0);

    await testInfo.attach('phase32-skeleton-sidebar.png', {
      body: await page.screenshot({ path: `${EVIDENCE_DIR}/phase32-skeleton-sidebar.png` }),
      contentType: 'image/png',
    });
  });

  test('SearchPanel renders the rich skeleton while a search is in flight', async ({
    page,
  }, testInfo) => {
    test.skip(
      testInfo.project.name !== 'chromium',
      'the search panel lives in the hidden off-canvas nav drawer on the mobile viewport; the skeleton is verified on desktop chromium',
    );
    await page.route('**/api/conversations**', async (route) => {
      const path = new URL(route.request().url()).pathname;
      if (path === '/api/conversations/search') {
        await new Promise((res) => setTimeout(res, 12000)); // keep the search request pending
        return route.fulfill(json([]));
      }
      return route.fulfill(json([])); // the list resolves so only the search shows a skeleton
    });

    await gotoAuthenticated(page, '/');

    const input = page.locator('#conversation-search');
    await expect(input).toBeVisible({ timeout: 15000 });
    await input.fill('engineering');

    // The searching wrapper is a live region (role="status") so AT announces the in-flight search.
    const searchLoading = page.getByRole('status', { name: 'Searching...' });
    await expect(searchLoading).toBeVisible({ timeout: 10000 });
    const searchSkeletons = searchLoading.locator('.skeleton-block');
    await expect(searchSkeletons).toHaveCount(2, { timeout: 10000 });
    const styles = await waveSkeletonStyles(searchSkeletons);
    expectRemHeight(styles, 3.5, 'search rows are 3.5rem tall');
    await expect(page.locator('.animate-pulse')).toHaveCount(0);

    await testInfo.attach('phase32-skeleton-search.png', {
      body: await page.screenshot({ path: `${EVIDENCE_DIR}/phase32-skeleton-search.png` }),
      contentType: 'image/png',
    });
  });

  test('Governance board renders the rich skeleton while MCP servers load', async ({
    page,
  }, testInfo) => {
    await page.route('**/api/conversations**', (route) => {
      const path = new URL(route.request().url()).pathname;
      if (path === `/api/conversations/${CONV_ID}`) return route.fulfill(json(conversationRow));
      return route.fulfill(json([]));
    });
    // Hang the MCP board query → BoardStateView 'loading' → three SkeletonBlocks.
    await page.route('**/api/governance/mcp', async (route) => {
      await new Promise((res) => setTimeout(res, 12000));
      return route.fulfill(json(MCP_SERVERS));
    });
    await page.route('**/api/governance/skills?*', (r) => r.fulfill(json({ skills: [] })));
    await page.route('**/api/governance/scheduler', (r) => r.fulfill(json({ tasks: [] })));

    await gotoAuthenticated(page, `/c/${CONV_ID}`);
    await page.getByRole('button', { name: 'Governance', exact: true }).first().click();

    const boardSkeletons = page.getByRole('status', { name: 'Loading' }).locator('.skeleton-block');
    await expect(boardSkeletons).toHaveCount(3, { timeout: 15000 });
    const styles = await waveSkeletonStyles(boardSkeletons);
    expectRemHeight(styles, 3, 'governance loading rows are 3rem tall');
    await expect(page.locator('.animate-pulse')).toHaveCount(0);

    await testInfo.attach('phase32-skeleton-governance.png', {
      body: await page.screenshot({ path: `${EVIDENCE_DIR}/phase32-skeleton-governance.png` }),
      contentType: 'image/png',
    });
  });
});
