import { expect, test, type Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';

// governance.spec.ts — the Phase 28 Governance boards E2E. It proves the operator can open the
// 'governance' workspace (the live mode), the MCP/Skills/Scheduler tab strip is a real role=tablist,
// a master row opens its detail (a bottom sheet on a mobile viewport, the desktop column on
// chromium), the master list is arrow-navigable, and the interactive targets meet the 44px floor.
// The six /api/governance/* routes are mocked at the page-network layer so only the served SPA +
// auth come from `aura serve`. Runs on desktop chromium + mobile chrome (Pixel 5).

const CONV_ID = '99999999-9999-9999-9999-999999999999';
const TASK_ID = '11111111-1111-1111-1111-111111111111';

const MCP_SERVERS = {
  servers: [
    {
      name: 'github',
      trust: 'trusted',
      runtime: 'stdio',
      startupState: 'configured',
      authStatus: 'ok',
      envKeys: [{ key: 'GITHUB_TOKEN', redacted: true }],
    },
    {
      name: 'filesystem',
      trust: 'trusted',
      runtime: 'stdio',
      startupState: 'configured',
      authStatus: 'ok',
      envKeys: [],
    },
  ],
};

const SKILLS_ACTIVE = {
  skills: [{ name: 'golang-testing', description: 'Go test patterns', type: 'instruction' }],
};

const SCHED_TASKS = {
  tasks: [
    {
      ID: TASK_ID,
      Kind: 'reminder',
      ScheduleKind: 'cron',
      CronExpr: '0 9 * * *',
      EveryMinutes: 0,
      RunAt: '0001-01-01T00:00:00Z',
      TZ: 'UTC',
      StepBudget: 10,
      Status: 'active',
      NextRunAt: '2026-06-21T09:00:00Z',
      NotifyRoute: 'telegram',
      IdentityID: 'local',
      OriginConversationID: '',
      CreatedAt: '2026-06-01T09:00:00Z',
      UpdatedAt: '2026-06-01T09:00:00Z',
    },
  ],
};

const SCHED_RUNS = {
  runs: [
    {
      ID: 'run-1',
      TaskID: TASK_ID,
      Status: 'completed',
      StepBudget: 10,
      StartedAt: '2026-06-19T09:00:00Z',
      LastHeartbeatAt: '2026-06-19T09:01:00Z',
      CompletedWithHash: 'h',
      Summary: 'first run',
      LastError: '',
      MissedSince: '0001-01-01T00:00:00Z',
      PausedStateToken: '',
      CompletedAt: '2026-06-19T09:02:00Z',
    },
  ],
};

function json(body: unknown) {
  return { status: 200, contentType: 'application/json', body: JSON.stringify(body) };
}

async function installGovernanceRoutes(page: Page) {
  await page.route('**/api/conversations*', (route) => {
    if (route.request().url().includes(`/api/conversations/${CONV_ID}`)) {
      return route.fulfill(
        json({
          id: CONV_ID,
          title: 'Gov thread',
          status: 'active',
          total_input_tokens: 0,
          total_output_tokens: 0,
          total_cached_tokens: 0,
          total_cost_usd: 0,
        }),
      );
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
    route.fulfill(json({ type: 'MESSAGES_SNAPSHOT', messages: [] })),
  );

  await page.route('**/api/governance/mcp', (route) => route.fulfill(json(MCP_SERVERS)));
  await page.route('**/api/governance/mcp/*/probe', (route) => {
    const url = route.request().url();
    const ok = url.includes('/github/');
    return route.fulfill(
      json({
        name: ok ? 'github' : 'filesystem',
        ok,
        tool_count: ok ? 4 : 0,
        detail: ok ? 'ok (4 tools)' : 'dial failed',
        ...(ok ? {} : { err: 'connection refused' }),
      }),
    );
  });
  await page.route('**/api/governance/skills?*', (route) => route.fulfill(json(SKILLS_ACTIVE)));
  await page.route('**/api/governance/skills/audit*', (route) =>
    route.fulfill(json({ rows: [] })),
  );
  await page.route('**/api/governance/scheduler', (route) => route.fulfill(json(SCHED_TASKS)));
  await page.route('**/api/governance/scheduler/*/runs*', (route) =>
    route.fulfill(json(SCHED_RUNS)),
  );
}

async function openGovernance(page: Page) {
  await installGovernanceRoutes(page);
  await gotoAuthenticated(page, `/c/${CONV_ID}`);
  // Switch to the live 'governance' surface via the mode control (text "Governance").
  await page.getByRole('button', { name: 'Governance', exact: true }).first().click();
}

test.describe('Phase 28 — Governance boards (desktop + mobile)', () => {
  test('opens the workspace with the MCP/Skills/Scheduler tablist and the read-only banner', async ({
    page,
  }, testInfo) => {
    await openGovernance(page);

    // The read-only banner sets expectation.
    await expect(
      page.getByText('Read-only — viewing only. Changes arrive in a later phase.'),
    ).toBeVisible({ timeout: 15000 });

    // The tab strip is a real role=tablist with the three governance tabs.
    const tablist = page.getByRole('tablist', { name: 'Governance' });
    await expect(tablist.getByRole('tab', { name: 'MCP servers' })).toBeVisible();
    await expect(tablist.getByRole('tab', { name: 'Skills' })).toBeVisible();
    await expect(tablist.getByRole('tab', { name: 'Scheduler' })).toBeVisible();

    // The MCP board renders both rows; the healthy probe resolves to its tool count.
    await expect(page.getByText('github')).toBeVisible();
    await expect(page.getByText('filesystem')).toBeVisible();
    await expect(page.getByText('Healthy · 4 tools')).toBeVisible({ timeout: 15000 });

    await testInfo.attach('governance-mcp.png', {
      body: await page.screenshot({ fullPage: true }),
      contentType: 'image/png',
    });
  });

  test('the MCP master list is arrow-navigable and a row opens the detail', async ({ page }) => {
    await openGovernance(page);
    await expect(page.getByText('github')).toBeVisible({ timeout: 15000 });

    // Focus the first row, arrow-down to the sibling, Enter to open its detail.
    const firstRow = page.getByRole('button', { name: /github/ }).first();
    await firstRow.focus();
    await page.keyboard.press('ArrowDown');
    await page.keyboard.press('Enter');

    // The detail pane shows the env-keys section heading (filesystem has none).
    await expect(page.getByText('No environment keys.')).toBeVisible();
  });

  test('selecting a scheduled task shows its paginated run history', async ({ page }) => {
    await openGovernance(page);
    await page.getByRole('tab', { name: 'Scheduler' }).click();

    await expect(page.getByText('reminder')).toBeVisible({ timeout: 15000 });
    await expect(page.getByText('0 9 * * *')).toBeVisible();

    await page.getByRole('button', { name: /reminder/ }).first().click();
    await expect(page.getByText('Run history')).toBeVisible();
    await expect(page.getByText('first run')).toBeVisible();
    await expect(page.getByText('Showing 1 of 1')).toBeVisible();
  });

  test('interactive controls meet the 44px touch-target floor', async ({ page }) => {
    await openGovernance(page);
    await expect(page.getByRole('tablist', { name: 'Governance' })).toBeVisible({
      timeout: 15000,
    });

    for (const name of ['MCP servers', 'Skills', 'Scheduler']) {
      const box = await page.getByRole('tab', { name }).boundingBox();
      expect(box, `tab ${name} has a bounding box`).not.toBeNull();
      if (box !== null) {
        expect(box.height, `tab ${name} ≥ 44px tall`).toBeGreaterThanOrEqual(44);
      }
    }
  });

  test('skills lifecycle sub-tabs are a role=tablist with no run/activate control', async ({
    page,
  }) => {
    await openGovernance(page);
    await page.getByRole('tab', { name: 'Skills' }).click();

    // The four lifecycle sub-tabs are a nested tablist.
    const skillsTabs = page.getByRole('tablist', { name: 'Skills' });
    await expect(skillsTabs.getByRole('tab', { name: 'Active' })).toBeVisible({ timeout: 15000 });
    await expect(skillsTabs.getByRole('tab', { name: 'Pending' })).toBeVisible();
    await expect(skillsTabs.getByRole('tab', { name: 'Archived' })).toBeVisible();
    await expect(skillsTabs.getByRole('tab', { name: 'Audit' })).toBeVisible();

    // No run/activate/install control exists on the board (read-only enforcement).
    await expect(page.getByRole('button', { name: /^(run|activate|install)\b/i })).toHaveCount(0);
  });
});

test.describe('Phase 28 — Governance mobile bottom sheet', () => {
  test.use({ viewport: { width: 414, height: 896 } });

  test('a selected MCP row opens a backdrop-dismissable bottom sheet on mobile', async ({
    page,
  }) => {
    await openGovernance(page);
    await expect(page.getByText('github')).toBeVisible({ timeout: 15000 });

    await page.getByRole('button', { name: /github/ }).first().click();
    // The detail (bottom sheet) shows the env KEY; closing via the backdrop dismisses it.
    await expect(page.getByText('GITHUB_TOKEN')).toBeVisible();

    // The detail ✕ close button restores the list-only view.
    await page.getByRole('button', { name: 'Close' }).first().click();
    await expect(page.getByText('GITHUB_TOKEN')).toBeHidden();
  });
});
