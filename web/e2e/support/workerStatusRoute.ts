import type { Page } from '@playwright/test';

// These conversations exist only in fixture routes. Live-worker tests must retain
// the real discovery stream so missing server events remain observable failures.
export async function stubEmptyWorkerStatuses(page: Page, conversationIds: readonly string[]) {
  for (const id of conversationIds) {
    await page.route(`**/api/conversations/${id}/swarm/events`, (route) =>
      route.fulfill({
        status: 200,
        contentType: 'text/event-stream',
        body: 'retry: 60000\n: no workers in this fixture\n\n',
      }),
    );
  }
}
