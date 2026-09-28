import { expect, test, type Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';
import { expandToolRow } from './support/toolRow';

const CONV_ID = '77777777-7777-7777-7777-777777777777';

const calls = [
  {
    name: 'task',
    result:
      '1 task(s):\n  11111111-1111-1111-1111-111111111111  kind=reminder  every  next=2026-10-01T09:00:00Z',
    display: {
      type: 'table',
      title: 'native_tasks',
      table: {
        columns: ['Task', 'Kind', 'Schedule', 'Next', 'State'],
        rows: [
          [
            '11111111-1111-1111-1111-111111111111',
            'reminder',
            'every',
            '2026-10-01T09:00:00Z',
            'active',
          ],
        ],
      },
    },
  },
  {
    name: 'skill',
    result: '- research: Find source material.',
    display: {
      type: 'table',
      title: 'native_skills',
      table: { columns: ['Skill', 'Description'], rows: [['research', 'Find source material.']] },
    },
  },
  {
    name: 'plugin_pack',
    result: '1 pack(s)\n  sales  v1  2 skills  1 connectors  3 commands  owner/repo/sales',
    display: {
      type: 'table',
      title: 'native_packs',
      table: {
        columns: ['Pack', 'Version', 'Skills', 'Connectors', 'Commands', 'Source'],
        rows: [['sales', 'v1', '2', '1', '3', 'owner/repo/sales']],
      },
    },
  },
  {
    name: 'document_search',
    result: '{"documents":[{"document_id":"doc_1","passages":[{"citation_token":"cite-1"}]}]}',
  },
  {
    name: 'document_open',
    result: '{"path":"/workspace/document.pdf","document_id":"doc_1"}',
  },
  {
    name: 'shell_exec',
    result: '<script>window.__chart = true</script>',
    display: { type: 'chart', chart: { x_labels: ['Jan'], y_values: [1] } },
  },
] as const;

function snapshot() {
  const messages: Record<string, unknown>[] = [];
  calls.forEach((call, index) => {
    const id = `native-${String(index)}`;
    messages.push({ id: `user-${id}`, role: 'user', content: 'Show this result' });
    messages.push({
      id: `assistant-${id}`,
      role: 'assistant',
      content: '',
      toolCalls: [
        {
          id,
          function: { name: call.name, arguments: '{}' },
          ...('display' in call ? { display: { ...call.display, tool_call_id: id } } : {}),
        },
      ],
    });
    messages.push({ id: `tool-${id}`, role: 'tool', toolCallId: id, content: call.result });
  });
  return JSON.stringify({ type: 'MESSAGES_SNAPSHOT', messages });
}

async function installRoutes(page: Page) {
  const record = {
    id: CONV_ID,
    title: 'Native result displays',
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
  await page.route('**/api/system/update', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '{"managed":false}' }),
  );
  await page.route('**/threads/*/messages', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: snapshot() }),
  );
}

test('native read lists and raw evidence receipts survive reload', async ({ page }) => {
  await installRoutes(page);
  await gotoAuthenticated(page, `/c/${CONV_ID}`);

  for (const replay of [false, true]) {
    if (replay) await page.reload();
    await expandToolRow(page, 0);
    await expect(
      page.getByText('Scheduled tasks').and(page.locator(':visible')).first(),
    ).toBeVisible();
    await expect(
      page.getByText('11111111-1111-1111-1111-111111111111').and(page.locator(':visible')).first(),
    ).toBeVisible();

    await expandToolRow(page, 1);
    await expect(
      page.getByText('Installed skills').and(page.locator(':visible')).first(),
    ).toBeVisible();
    await expect(
      page.getByText('Find source material.').and(page.locator(':visible')).first(),
    ).toBeVisible();

    await expandToolRow(page, 2);
    await expect(
      page.getByText('Plugin packs').and(page.locator(':visible')).first(),
    ).toBeVisible();
    await expect(
      page.getByText('owner/repo/sales').and(page.locator(':visible')).first(),
    ).toBeVisible();

    await expandToolRow(page, 3);
    await expect(page.getByText(/cite-1/)).toBeVisible();
    await expandToolRow(page, 4);
    await expect(page.getByText(/\/workspace\/document.pdf/)).toBeVisible();
    await expect(page.getByRole('link', { name: /document.pdf/ })).toHaveCount(0);

    await expandToolRow(page, 5);
    await expect(page.getByText('<script>window.__chart = true</script>')).toBeVisible();
    expect(
      await page.evaluate(() => (window as unknown as Record<string, unknown>).__chart),
    ).toBeUndefined();
  }
});
