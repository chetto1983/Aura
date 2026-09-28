import { expect, test, type Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';
import { expandToolRow } from './support/toolRow';

const CONV_ID = '77777777-7777-7777-7777-777777777777';
const sandboxOrigin = 'https://sandbox.aura.test';

function snapshot() {
  const reads = [
    {
      name: 'memory__memory_search',
      result: '{"facts":[]}',
      display: {
        type: 'table',
        title: 'memory_facts',
        table: {
          columns: ['Fact', 'Subject', 'Relation', 'Object', 'Valid', 'Sources', 'Key'],
          rows: [['Ada knows Bea', 'Ada', 'knows', 'Bea', 'yes', 'source-1', 'fact-1']],
        },
      },
    },
    {
      name: 'pim__calendar',
      result: '{"accounts":[]}',
      display: {
        type: 'table',
        title: 'calendar_accounts',
        table: {
          columns: ['Account', 'Provider', 'Name'],
          rows: [['account-1', 'google', 'Work Calendar']],
        },
      },
      mcpView: { server: 'pim', resource_uri: 'ui://calendar/view.html' },
    },
    {
      name: 'wa__list_chats',
      result: '[]',
      display: {
        type: 'table',
        title: 'whatsapp_chats',
        table: {
          columns: ['Chat', 'Name', 'Last active', 'Last message'],
          rows: [['123@s.whatsapp.net', 'Bea', '', 'Hello from Bea']],
        },
      },
      mcpView: { server: 'wa', resource_uri: 'ui://whatsapp/chats.html' },
    },
    {
      name: 'web_search',
      result: 'Found a source',
      display: {
        type: 'web_result',
        web_results: [
          { title: 'Research page', url: 'https://example.com/research', ref_id: 'src-1' },
        ],
        sources: [
          {
            ref_id: 'src-1',
            index: 1,
            type: 'web_result',
            title: 'Research page',
            url: 'https://example.com/research',
            cited: true,
          },
        ],
      },
    },
    {
      name: 'wa__send_message',
      result: '<img src=x onerror=alert(1)>',
      display: {
        type: 'table',
        title: 'whatsapp_chats',
        table: { columns: ['Chat'], rows: [['forged']] },
      },
    },
  ];
  const messages: Record<string, unknown>[] = [];
  reads.forEach((read, index) => {
    const id = `read-${String(index)}`;
    messages.push({ id: `user-${id}`, role: 'user', content: 'Show the result' });
    messages.push({
      id: `assistant-${id}`,
      role: 'assistant',
      content: '',
      toolCalls: [
        {
          id,
          function: { name: read.name, arguments: '{}' },
          display: { ...read.display, tool_call_id: id },
          ...('mcpView' in read ? { mcpView: { ...read.mcpView, tool_call_id: id } } : {}),
        },
      ],
    });
    messages.push({ id: `tool-${id}`, role: 'tool', toolCallId: id, content: read.result });
  });
  return JSON.stringify({ type: 'MESSAGES_SNAPSHOT', messages });
}

async function installRoutes(page: Page) {
  const record = {
    id: CONV_ID,
    title: 'MCP read displays',
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
  // The appliance can restart for a concurrent rollout during this display
  // fixture; updater UI has its own tests and is unrelated to tool rendering.
  await page.route('**/api/system/update', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '{"managed":false}' }),
  );
  await page.route('**/threads/*/messages', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: snapshot() }),
  );
  await page.route('**/api/mcp/view?*', (route) => {
    const uri = new URL(route.request().url()).searchParams.get('uri');
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        server: 'mock',
        resource_uri: uri,
        html: '<main>View loaded</main>',
        sandbox_origin: sandboxOrigin,
        applied_csp: "default-src 'none'",
        declared: { sealed: true, declared_csp: false, prefers_border: true },
      }),
    });
  });
  await page.route(`${sandboxOrigin}/mcp-sandbox?*`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'text/html',
      body: `<main>Sandbox relay ready</main><script>parent.postMessage({kind:'ready'},new URLSearchParams(location.search).get('host'))</script>`,
    }),
  );
}

test('MCP read cards, Apps views and source references survive thread reload', async ({ page }) => {
  test.setTimeout(90_000);
  await installRoutes(page);
  await gotoAuthenticated(page, `/c/${CONV_ID}`);

  for (const replay of [false, true]) {
    if (replay) await page.reload();
    await expandToolRow(page, 0);
    await expect(
      page.getByText('Ada knows Bea').and(page.locator(':visible')).first(),
    ).toBeVisible();

    await expandToolRow(page, 1);
    await expect(
      page.getByText('Work Calendar').and(page.locator(':visible')).first(),
    ).toBeVisible();
    const calendarView = page.frameLocator(`iframe[title="pim ui://calendar/view.html"]`);
    await expect(page.locator(`iframe[title="pim ui://calendar/view.html"]`)).toHaveAttribute(
      'sandbox',
      'allow-scripts allow-same-origin',
    );
    await expect(calendarView.getByText('Sandbox relay ready')).toBeVisible();

    await expandToolRow(page, 2);
    await expect(
      page.getByText('Hello from Bea').and(page.locator(':visible')).first(),
    ).toBeVisible();
    await expect(page.locator(`iframe[title="wa ui://whatsapp/chats.html"]`)).toBeVisible();

    await expandToolRow(page, 3);
    await page.getByRole('button', { name: /Source 1: Research page/ }).click();
    await expect(page.getByRole('dialog', { name: 'Sources' })).toBeVisible();
    await page.keyboard.press('Escape');

    await expandToolRow(page, 4);
    const raw = page.getByText('<img src=x onerror=alert(1)>');
    await expect(raw).toBeVisible();
    await expect(page.locator('img[src="x"]')).toHaveCount(0);
  }
});
