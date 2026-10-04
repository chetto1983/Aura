import { expect, test, type Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';

// A search hit opens its thread on the matched message. Before, the thread opened at the
// bottom, where assistant-ui keeps every thread, and a match in an early turn sat thousands of
// pixels above the screen (prd.md §7, 2026-10-04). The conversation is real; its history and
// the search answer are stubbed, so the spec needs no model to have turns to search.

const TURNS = 24;
const HIT_SEQ = 3;
const MARKER = 'zefirosegnaposto';

function snapshotTurn(seq: number) {
  const filler = 'Riga di riempimento perché il thread superi l’altezza della finestra. '.repeat(6);
  return {
    id: `msg-${String(seq)}`,
    role: seq % 2 === 1 ? 'user' : 'assistant',
    content: `Turno ${String(seq)}. ${seq === HIT_SEQ ? `La parola è ${MARKER}. ` : ''}${filler}`,
  };
}

async function sameOriginFetch(page: Page, url: string, method: string, body?: string) {
  return page.evaluate(
    async ({ u, m, b }) => {
      const init: RequestInit = { method: m, credentials: 'same-origin' };
      if (b !== undefined) {
        init.body = b;
        init.headers = { 'Content-Type': 'application/json' };
      }
      const response = await fetch(u, init);
      return { status: response.status, text: await response.text() };
    },
    { u: url, m: method, b: body },
  );
}

test.describe('a search hit opens its thread on the match', () => {
  let conversationId = '';

  test.beforeEach(async ({ page }) => {
    await gotoAuthenticated(page, '/');
    const created = await sameOriginFetch(
      page,
      '/api/conversations',
      'POST',
      JSON.stringify({ title: 'Search hit scroll' }),
    );
    expect(created.status, created.text).toBe(201);
    conversationId = (JSON.parse(created.text) as { ID: string }).ID;

    const turns = Array.from({ length: TURNS }, (_, i) => snapshotTurn(i + 1));
    await page.route(`**/threads/${conversationId}/messages`, (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ type: 'MESSAGES_SNAPSHOT', messages: turns }),
      }),
    );
    await page.route('**/api/conversations/search?**', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([
          {
            ConversationID: conversationId,
            Seq: HIT_SEQ,
            Content: turns[HIT_SEQ - 1]?.content,
            Similarity: 1,
          },
        ]),
      }),
    );
  });

  test.afterEach(async ({ page }) => {
    const deleted = await sameOriginFetch(page, `/api/conversations/${conversationId}`, 'DELETE');
    expect([200, 204], deleted.text).toContain(deleted.status);
  });

  test('the matched message is in view and marked; a plain open stays at the bottom', async ({
    page,
  }) => {
    // On a phone the search lives in the navigation drawer.
    const search = page.locator('#conversation-search:visible');
    const openNavigation = page.getByRole('button', { name: 'Open navigation' });
    await expect(search.or(openNavigation)).toBeVisible();
    if (await openNavigation.isVisible()) await openNavigation.click();
    await search.fill(MARKER);
    await page.locator('li button:visible').filter({ hasText: MARKER }).click();
    await page.waitForURL(`**/c/${conversationId}`);

    const hit = page.locator(`[data-message-id="msg-${String(HIT_SEQ)}"]`);
    const last = page.locator(`[data-message-id="msg-${String(TURNS)}"]`);
    await expect(hit).toHaveAttribute('data-search-hit', '');
    await expect(hit).toBeInViewport();
    await expect(last).not.toBeInViewport();
    await expect(hit).not.toHaveAttribute('data-search-hit', { timeout: 5_000 });

    // Opened the ordinary way, the same thread lands on its last message.
    await page.goto('/', { waitUntil: 'domcontentloaded' });
    await page.goto(`/c/${conversationId}`, { waitUntil: 'domcontentloaded' });
    await expect(last).toBeInViewport();
    await expect(hit).not.toBeInViewport();
  });
});
