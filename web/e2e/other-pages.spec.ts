import { test, expect } from './fixtures';

test('conversations filters are URL-backed and desktop rows open with keyboard', async ({ authedPage: page }) => {
  await page.route('**/api/conversations/stats', async (route) => {
    await route.fulfill({
      json: {
        total_rows: 1,
        distinct_chats: 1,
        oldest_at: '2026-05-05T10:00:00Z',
        newest_at: '2026-05-05T10:00:00Z',
      },
    });
  });
  await page.route('**/api/conversations**', async (route) => {
    if (route.request().url().includes('/api/conversations/101')) {
      await route.fulfill({
        json: {
          id: 101,
          chat_id: 42,
          turn_index: 7,
          role: 'user',
          content: 'Keyboard-opened conversation detail',
          tool_calls: '',
          tool_calls_count: 0,
          llm_calls: 0,
          elapsed_ms: 0,
          tokens_in: 0,
          tokens_out: 0,
          created_at: '2026-05-05T10:00:00Z',
        },
      });
      return;
    }
    await route.fulfill({
      json: [{
        id: 101,
        chat_id: 42,
        role: 'user',
        content: 'Keyboard-opened conversation detail',
        tool_calls_count: 0,
        created_at: '2026-05-05T10:00:00Z',
      }],
    });
  });

  await page.goto('/conversations');
  await page.getByLabel(/chat id/i).fill('42');
  await expect(page).toHaveURL(/chat_id=42/);
  const hasTools = page.getByLabel(/has tool calls/i);
  await hasTools.click();
  await expect(hasTools).toBeChecked();
  await expect(page).toHaveURL(/has_tools=1/);

  const row = page.getByRole('button', { name: /open turn 101/i });
  await row.focus();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('dialog')).toBeVisible();
  await expect(page).toHaveURL(/#turn-101$/);
});

test('skills, MCP, summaries, swarm, and maintenance expose normal-user controls', async ({ authedPage: page }) => {
  await page.route('**/api/skills/catalog**', async (route) => {
    await route.fulfill({ json: [] });
  });
  await page.route('**/api/skills', async (route) => {
    await route.fulfill({
      json: [{ name: 'demo-skill', description: 'A useful local skill.' }],
    });
  });
  await page.route('**/api/skills/demo-skill', async (route) => {
    await route.fulfill({
      json: { name: 'demo-skill', content: '# Demo skill', truncated: false },
    });
  });
  await page.route('**/api/mcp/servers', async (route) => {
    await route.fulfill({ json: [] });
  });
  await page.route('**/api/summaries**', async (route) => {
    await route.fulfill({ json: [] });
  });
  await page.route('**/api/swarm/runs/swarm_1', async (route) => {
    await route.fulfill({
      json: {
        id: 'swarm_1',
        goal: 'Audit run',
        status: 'failed',
        created_at: '2026-05-05T10:00:00Z',
        completed_at: '2026-05-05T10:00:01Z',
        last_error: 'Audit failure',
        task_counts: { total: 1, completed: 0, failed: 1 },
        metrics: { llm_calls: 0, tokens_total: 0, speedup: 0, wall_ms: 1000 },
        tasks: [{
          id: 'task_1',
          role: 'worker',
          subject: 'Check status',
          status: 'failed',
          depth: 0,
          llm_calls: 0,
          tool_calls: 0,
          elapsed_ms: 1,
          last_error: 'Audit failure',
        }],
      },
    });
  });
  await page.route('**/api/swarm/runs**', async (route) => {
    if (route.request().url().includes('/api/swarm/runs/swarm_1')) {
      await route.fallback();
      return;
    }
    await route.fulfill({
      json: [{
        id: 'swarm_1',
        goal: 'Audit run',
        status: 'failed',
        created_at: '2026-05-05T10:00:00Z',
        completed_at: '2026-05-05T10:00:01Z',
        last_error: 'Audit failure',
        task_counts: { total: 1, completed: 0, failed: 1 },
        metrics: { llm_calls: 0, tokens_total: 0, speedup: 0, wall_ms: 1000 },
      }],
    });
  });
  await page.route('**/api/maintenance/issues**', async (route) => {
    await route.fulfill({
      json: [{
        id: 11,
        slug: 'demo-page',
        severity: 'high',
        kind: 'broken_link',
        status: 'open',
        message: 'Broken link found',
        broken_link: 'missing-page',
        created_at: '2026-05-05T10:00:00Z',
      }],
    });
  });

  await page.goto('/skills');
  await expect(page.getByRole('tab', { name: /local/i })).toBeVisible();
  await page.getByRole('tab', { name: /catalog/i }).click();
  await expect(page).toHaveURL(/tab=catalog/);
  await expect(page.getByLabel(/search skill catalog/i)).toBeVisible();

  await page.goto('/mcp');
  await expect(page.getByRole('button', { name: /refresh/i })).toBeVisible();

  await page.goto('/summaries');
  await expect(page.getByRole('link', { name: /open settings/i })).toBeVisible();

  await page.goto('/swarm');
  await expect(page.getByText(/^Failed$/).first()).toBeVisible();
  await expect(page.getByRole('button', { name: /audit run/i })).toHaveAttribute('aria-pressed', 'true');

  await page.goto('/maintenance');
  await expect(page.locator('main span', { hasText: /^High$/ }).first()).toBeVisible();
  await expect(page.locator('main span', { hasText: /^Open$/ }).first()).toBeVisible();
});
