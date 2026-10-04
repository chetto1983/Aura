import type { Page } from '@playwright/test';

/** Stubs the chat shell's reads that a Graph spec does not exercise, so the surface opens
 * from a fixture conversation without reaching the real server for them. */
export async function installGraphShellRoutes(page: Page, conversationId: string): Promise<void> {
  await page.route('**/api/conversations*', (route) => {
    if (route.request().url().includes(`/api/conversations/${conversationId}`)) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          id: conversationId,
          title: 'Graph thread',
          status: 'active',
          total_input_tokens: 0,
          total_output_tokens: 0,
          total_cached_tokens: 0,
          total_cost_usd: 0,
        }),
      });
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
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ type: 'MESSAGES_SNAPSHOT', messages: [] }),
    }),
  );
}
