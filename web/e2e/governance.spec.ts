import { expect, test, type Locator, type Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';

// governance.spec.ts — the Phase 28 Governance boards E2E. It proves the operator can open the
// 'governance' workspace (the live mode), the MCP/Skills/Scheduler section rail is a real nav
// landmark (a sidebar column on desktop, a scrollable strip above the board on mobile),
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
  await page.route('**/api/governance/skills/audit*', (route) => route.fulfill(json({ rows: [] })));
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

// The board switcher is the shared SectionRail — a nav landmark, not a tablist. Scope every
// lookup to it: 'Skills' and 'Audit' also name controls inside the boards themselves.
async function dragHandleBy(page: Page, handle: Locator, deltaX: number) {
  const box = await handle.boundingBox();
  if (box === null) throw new Error('resize handle has no bounding box');
  const y = box.y + box.height / 2;
  await page.mouse.move(box.x + box.width / 2, y);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width / 2 + deltaX, y, { steps: 10 });
  await page.mouse.up();
}

function railItem(page: Page, name: string) {
  return page
    .getByRole('navigation', { name: 'Governance sections' })
    .getByRole('button', { name });
}

test.describe('Phase 28 — Governance boards (desktop + mobile)', () => {
  test('opens the workspace with the MCP/Skills/Scheduler section rail', async ({
    page,
  }, testInfo) => {
    await openGovernance(page);

    // The obsolete read-only banner is gone now that governance has write-capable flows.
    await expect(
      page.getByText('Read-only — viewing only. Changes arrive in a later phase.'),
    ).toHaveCount(0);

    // The rail is a real nav landmark carrying the three governance boards.
    await expect(railItem(page, 'MCP servers')).toBeVisible();
    await expect(railItem(page, 'Skills')).toBeVisible();
    await expect(railItem(page, 'Scheduler')).toBeVisible();

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
    await railItem(page, 'Scheduler').click();

    await expect(page.getByText('reminder')).toBeVisible({ timeout: 15000 });
    await expect(page.getByText('0 9 * * *')).toBeVisible();

    await page
      .getByRole('button', { name: /reminder/ })
      .first()
      .click();
    await expect(page.getByText('Run history')).toBeVisible();
    await expect(page.getByText('first run')).toBeVisible();
    await expect(page.getByText('Showing 1 of 1')).toBeVisible();
  });

  test('interactive controls meet the 44px touch-target floor', async ({ page }) => {
    await openGovernance(page);
    await expect(page.getByRole('navigation', { name: 'Governance sections' })).toBeVisible({
      timeout: 15000,
    });

    for (const name of ['MCP servers', 'Skills', 'Scheduler']) {
      const box = await railItem(page, name).boundingBox();
      expect(box, `rail item ${name} has a bounding box`).not.toBeNull();
      if (box !== null) {
        expect(box.height, `rail item ${name} ≥ 44px tall`).toBeGreaterThanOrEqual(44);
      }
    }
  });

  test('the rail and the board list resize independently', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'chrome', 'both columns are lg-only');
    await openGovernance(page);
    await expect(page.getByText('github')).toBeVisible({ timeout: 15000 });

    const rail = page.getByRole('navigation', { name: 'Governance sections' });
    const listHandle = page.getByRole('separator', { name: 'Resize the list column' });
    const railHandle = page.getByRole('separator', { name: 'Resize the sections rail' });

    const railBefore = (await rail.boundingBox())?.width ?? 0;
    const listBefore = Number(await listHandle.getAttribute('aria-valuenow'));

    // Dragging the LIST handle moves the list by the drag delta and nothing else. The
    // regression this pins: a width measured from the VIEWPORT instead of from the grid's own
    // left edge, which made the column jump by the rail's full width on the first pointer move.
    await dragHandleBy(page, listHandle, 60);
    const listAfter = Number(await listHandle.getAttribute('aria-valuenow'));
    expect(listAfter).toBeGreaterThan(listBefore + 40);
    expect(listAfter).toBeLessThan(listBefore + 80);
    expect((await rail.boundingBox())?.width).toBe(railBefore);

    // The rail has its own handle, with the chat sidebar's bounds.
    await dragHandleBy(page, railHandle, 50);
    expect((await rail.boundingBox())?.width ?? 0).toBeGreaterThan(railBefore + 30);
  });

  test('skills lifecycle is a state-filter group with no run/activate row control', async ({
    page,
  }) => {
    await openGovernance(page);
    await railItem(page, 'Skills').click();

    // Amendment #97 merged the four lifecycle tabs into ONE list filtered by state: the
    // stages are a role=group of filter chips ('Pending' went away with the approval
    // stage), not a nested tablist. Each chip carries its row count, hence the ^ anchors.
    const filters = page.getByRole('group', { name: 'Filter by state' });
    await expect(filters.getByRole('button', { name: /^All/ })).toBeVisible({ timeout: 15000 });
    await expect(filters.getByRole('button', { name: /^Active/ })).toBeVisible();
    await expect(filters.getByRole('button', { name: /^Archived/ })).toBeVisible();

    // The audit ledger is a toggle in the same toolbar — scoped to it, because the rail's
    // Audit section now shares the word (`pressed` does NOT separate them: Playwright reads a
    // button with no aria-pressed as unpressed).
    const toolbar = filters.locator('..');
    await expect(toolbar.getByRole('button', { name: 'Audit' })).toBeVisible();

    // No run/activate control exists on rows; the one allowed write CTA is "Install skill".
    await expect(page.getByRole('button', { name: /^(run|activate)\b/i })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Install skill' })).toBeVisible();
  });
});

test.describe('Phase 28 — Governance mobile bottom sheet', () => {
  test.use({ viewport: { width: 414, height: 896 } });

  test('a selected MCP row opens a backdrop-dismissable bottom sheet on mobile', async ({
    page,
  }) => {
    await openGovernance(page);
    await expect(page.getByText('github')).toBeVisible({ timeout: 15000 });

    await page
      .getByRole('button', { name: /github/ })
      .first()
      .click();
    // The detail (bottom sheet) shows the env KEY; closing via the backdrop dismisses it.
    await expect(page.getByText('GITHUB_TOKEN')).toBeVisible();

    // The detail ✕ close button restores the list-only view.
    await page.getByRole('button', { name: 'Close' }).first().click();
    await expect(page.getByText('GITHUB_TOKEN')).toBeHidden();
  });
});
