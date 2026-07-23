import { expect, test, type Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';

// shell-overscroll.spec.ts — regression guard for the "empty band below the footer"
// over-scroll (operator report 2026-07-23). Root cause: sr-only status spans in the
// chat/tool cards are position:absolute; the grid shell and the overflow-y-auto
// message viewport are position:static and establish no containing block, so in a
// LONG chat the spans resolve to the ICB at their deep static Y, escape every
// overflow clip, and extend document.documentElement.scrollHeight past the viewport
// — a scrollable dead band the shell (h-[100dvh] overflow-hidden) was supposed to
// prevent. The fix makes .aura-shell position:relative so its overflow-hidden clips
// them. This test fails (docScrollHeight >> innerHeight) without that fix.
//
// It uses a MOCKED long conversation with many settled tool-call turns (each renders
// a ToolActivityCard carrying an sr-only status word), so it needs no live agent and
// stays deterministic.

const CONV_ID = '77777777-7777-7777-7777-777777777777';

function conversationRecord(): Record<string, unknown> {
  return {
    id: CONV_ID,
    title: 'Over-scroll guard',
    status: 'active',
    total_input_tokens: 0,
    total_output_tokens: 0,
    total_cached_tokens: 0,
    total_cost_usd: 0,
  };
}

function longToolChatSnapshot(): string {
  const messages: unknown[] = [];
  for (let i = 0; i < 24; i++) {
    messages.push({ id: `u-${String(i)}`, role: 'user', content: `question number ${String(i)}` });
    messages.push({
      id: `a-${String(i)}`,
      role: 'assistant',
      content: `Settled answer paragraph number ${String(i)}.`,
      toolCalls: [
        {
          id: `call-${String(i)}`,
          function: { name: 'web_search', arguments: `{"query":"q${String(i)}"}` },
          result: `result blob for turn ${String(i)}`,
        },
      ],
    });
  }
  return JSON.stringify({ type: 'MESSAGES_SNAPSHOT', messages });
}

async function installRoutes(page: Page, snapshot: string) {
  await page.route('**/api/conversations*', (route) => {
    if (route.request().url().includes(`/api/conversations/${CONV_ID}`)) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(conversationRecord()),
      });
    }
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([conversationRecord()]),
    });
  });
  await page.route('**/api/conversations/*/rot-events', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  );
  await page.route('**/api/approvals', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  );
  await page.route('**/threads/*/messages', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: snapshot }),
  );
}

for (const vp of [
  { name: 'desktop', width: 1900, height: 940 },
  { name: 'desktop-short', width: 1440, height: 760 },
]) {
  test(`long tool-card chat never over-scrolls the document (${vp.name})`, async ({ page }) => {
    await page.setViewportSize({ width: vp.width, height: vp.height });
    await installRoutes(page, longToolChatSnapshot());
    await gotoAuthenticated(page, `/c/${CONV_ID}`);
    await expect(page.getByText('question number 23').first()).toBeAttached({ timeout: 15000 });
    await page.waitForTimeout(400);

    const geo = await page.evaluate(() => ({
      innerHeight: window.innerHeight,
      docScrollHeight: document.documentElement.scrollHeight,
      bodyScrollHeight: document.body.scrollHeight,
    }));
    // The shell is h-[100dvh] overflow-hidden: the document must never exceed the
    // viewport (a stray absolute descendant escaping the clip breaks this).
    expect(geo.docScrollHeight, JSON.stringify(geo)).toBeLessThanOrEqual(geo.innerHeight + 1);
    expect(geo.bodyScrollHeight, JSON.stringify(geo)).toBeLessThanOrEqual(geo.innerHeight + 1);
  });
}
