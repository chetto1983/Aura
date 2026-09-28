import { expect, test, type Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';
import { expandToolRow } from './support/toolRow';

const CONV_ID = '66666666-6666-6666-6666-666666666666';

const calls = [
  {
    name: 'todo_write',
    display: {
      type: 'todo',
      todo: {
        items: [
          { content: 'Inspect output', status: 'in_progress', active_form: 'Inspecting output' },
        ],
      },
    },
    result: '[~] Inspect output',
    slot: 'todo-list',
    text: 'Inspect output',
  },
  {
    name: 'shell_exec',
    display: {
      type: 'terminal',
      terminal: { command: 'echo ready', output: 'ready', exit_code: 0 },
    },
    result: 'ready\n[aura_shell {"exit_code":0}]',
    slot: 'terminal-block',
    text: 'echo ready',
  },
  {
    name: 'patch',
    display: {
      type: 'diff',
      diff: {
        filename: 'note.txt',
        additions: 1,
        deletions: 1,
        lines: [
          { kind: 'removed', text: 'old' },
          { kind: 'added', text: 'new' },
        ],
      },
    },
    result: '--- a/note.txt\n+++ b/note.txt\n@@ -1 +1 @@\n-old\n+new',
    slot: 'code-diff',
    text: 'note.txt',
  },
  {
    name: 'read_file',
    display: { type: 'code', code: { body: 'file content', filename: 'note.txt', first_line: 1 } },
    result: '1| file content',
    slot: 'code-block',
    text: 'file content',
  },
  {
    name: 'search_files',
    display: {
      type: 'table',
      table: {
        columns: ['File', 'Line', 'Kind', 'Text'],
        rows: [['src/a.go', '12', 'match', 'needle <b>safe</b>']],
        notice: 'Results are partial',
      },
    },
    result: 'src/a.go:12: needle <b>safe</b>',
    slot: 'data-table',
    text: 'needle <b>safe</b>',
  },
  {
    name: 'search_files',
    display: { type: 'todo' },
    result: 'literal <img src=x onerror=alert(1)>',
    slot: null,
    text: 'literal <img src=x onerror=alert(1)>',
  },
] as const;

function snapshot(): string {
  const messages: Record<string, unknown>[] = [];
  calls.forEach((call, index) => {
    const id = `native-${String(index)}`;
    messages.push({ id: `user-${id}`, role: 'user', content: call.name });
    messages.push({
      id: `assistant-${id}`,
      role: 'assistant',
      content: 'Tool result follows.',
      toolCalls: [
        {
          id,
          function: { name: call.name, arguments: '{}' },
          display: { tool_call_id: id, ...call.display },
        },
      ],
    });
    messages.push({ id: `result-${id}`, role: 'tool', toolCallId: id, content: call.result });
  });
  return JSON.stringify({ type: 'MESSAGES_SNAPSHOT', messages });
}

async function installRoutes(page: Page) {
  const record = {
    id: CONV_ID,
    title: 'Native displays',
    status: 'active',
    total_input_tokens: 0,
    total_output_tokens: 0,
    total_cached_tokens: 0,
    total_cost_usd: 0,
  };
  await page.route('**/api/conversations*', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(
        route.request().url().includes(`/api/conversations/${CONV_ID}`) ? record : [record],
      ),
    }),
  );
  await page.route('**/api/conversations/*/rot-events', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  );
  await page.route('**/api/approvals', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  );
  await page.route('**/threads/*/messages', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: snapshot() }),
  );
}

test('native tool displays and malformed fallback survive replay', async ({ page }) => {
  await installRoutes(page);
  await gotoAuthenticated(page, `/c/${CONV_ID}`);

  async function inspect() {
    await expect(page.getByTestId('tool-row')).toHaveCount(calls.length);
    const observed: string[] = [];
    for (const [index, call] of calls.entries()) {
      await expandToolRow(page, index);
      const row = page.getByTestId('tool-row').nth(index);
      if (call.slot === null) {
        await expect(row.getByText(call.text)).toBeVisible();
        await expect(row.locator('img')).toHaveCount(0);
      } else {
        const display = row.locator(`[data-slot="${call.slot}"]`);
        await expect(display).toBeVisible();
        await expect(display).toContainText(call.text);
      }
      observed.push((await row.textContent()) ?? '');
    }
    return observed;
  }

  const first = await inspect();
  await page.reload();
  expect(await inspect()).toEqual(first);
});
