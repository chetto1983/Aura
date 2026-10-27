import { expect, test, type Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';

// Spec 2026-10-27, acceptance 9: conversation branches driven through the cockpit against a
// real model. Send, edit, go back with the arrow, send again, regenerate, then reload: the tree
// holds three leaves, every request continues the branch on screen, and the reload opens on
// the branch written last. Recorded in
// docs/superpowers/verification/2026-10-27-conversation-branches-local.md.

const runLive = process.env.AURA_E2E_REAL_AGENT === '1';
const turnTimeout = 1_200_000;

interface SnapshotMessage {
  readonly id: string;
  readonly parentId: string | null;
  readonly role: string;
  readonly content?: string;
}

async function sameOriginJSON(page: Page, url: string, method = 'GET', body?: unknown) {
  return page.evaluate(
    async ({ requestURL, requestMethod, requestBody }) => {
      const init: RequestInit = { method: requestMethod, credentials: 'same-origin' };
      if (requestBody !== undefined) {
        init.body = JSON.stringify(requestBody);
        init.headers = { 'Content-Type': 'application/json' };
      }
      const response = await fetch(requestURL, init);
      return { status: response.status, text: await response.text() };
    },
    { requestURL: url, requestMethod: method, requestBody: body },
  );
}

/** Wait until the run a click or a key starts has ended, and return the body it posted. The
 *  footer still reads "Run complete" from the run before, so the stream closing and the stop
 *  button leaving are the end of this one: a step taken earlier acts on a thread whose last
 *  answer has no action bar yet. */
async function runOnce(page: Page, path: RegExp, start: () => Promise<void>) {
  const request = page.waitForRequest(
    (candidate) => candidate.method() === 'POST' && path.test(new URL(candidate.url()).pathname),
    { timeout: turnTimeout },
  );
  await start();
  const posted = await request;
  const response = await posted.response();
  expect(response?.ok(), `${posted.url()} answered ${String(response?.status())}`).toBe(true);
  expect(await response?.finished()).toBeNull();
  await expect(
    page.getByRole('button', { name: 'Stop the current response', exact: true }),
  ).toHaveCount(0, { timeout: turnTimeout });
  return JSON.parse(posted.postData() ?? '{}') as Record<string, unknown>;
}

async function send(page: Page, text: string) {
  const composer = page.getByRole('textbox', { name: 'Ask Aura' });
  await composer.fill(text);
  return runOnce(page, /^\/agent\/run$/, () => composer.press('Enter'));
}

function leavesOf(messages: readonly SnapshotMessage[]): string[] {
  const parents = new Set(messages.map((message) => message.parentId));
  return messages.filter((message) => !parents.has(message.id)).map((message) => message.id);
}

test.describe('conversation branches against a live model', () => {
  test.skip(!runLive, 'set AURA_E2E_REAL_AGENT=1 to drive the local model');

  test('navigates, continues and reloads the conversation tree', async ({ page }, testInfo) => {
    test.setTimeout(4 * turnTimeout);
    await gotoAuthenticated(page, '/');
    const created = await sameOriginJSON(page, '/api/conversations', 'POST', {
      title: `branches live ${new Date().toISOString()}`,
    });
    expect(created.status, created.text).toBe(201);
    const threadId = (JSON.parse(created.text) as { readonly ID: string }).ID;
    const historyReads: number[] = [];
    page.on('request', (request) => {
      if (new URL(request.url()).pathname === `/threads/${threadId}/messages`) {
        historyReads.push(Date.now());
      }
    });
    await page.goto(`/c/${encodeURIComponent(threadId)}`, { waitUntil: 'domcontentloaded' });

    const first = await send(page, 'Answer with one word only: the capital of France.');
    expect((first.aura as Record<string, unknown> | undefined)?.parent_seq).toBeUndefined();

    await page.getByRole('button', { name: 'Edit' }).last().click();
    await page
      .getByRole('textbox', { name: 'Edit message' })
      .fill('Answer with one word only: the capital of Italy.');
    const edit = await runOnce(page, /\/edit$/, () =>
      page.getByRole('button', { name: 'Save and re-run' }).click(),
    );
    expect(edit).toMatchObject({ diverge_seq: 1, role: 'user' });

    // A run that ends refreshes what it changed; the switch is measured once that has settled.
    await expect
      .poll(() => Date.now() - (historyReads.at(-1) ?? 0), { timeout: 30_000 })
      .toBeGreaterThan(3_000);
    const switchedAt = Date.now();
    await page.getByRole('button', { name: 'Previous branch' }).first().click();
    await expect(page.getByText('the capital of France.')).toBeVisible();
    await page.waitForTimeout(3_000);
    await testInfo.attach('history reads', {
      body: JSON.stringify(historyReads.map((at) => at - switchedAt)),
      contentType: 'application/json',
    });
    expect(historyReads.filter((at) => at >= switchedAt)).toEqual([]);

    const second = await send(page, 'Answer with one word only: the capital of Spain.');
    expect((second.aura as Record<string, unknown> | undefined)?.parent_seq).toBe(2);

    const regenerate = await runOnce(page, /\/edit$/, () =>
      page.getByRole('button', { name: 'Regenerate' }).last().click(),
    );
    expect(regenerate).toMatchObject({ diverge_seq: 6, role: 'assistant' });

    await page.reload({ waitUntil: 'domcontentloaded' });
    await expect(page.getByText('the capital of Spain.')).toBeVisible({ timeout: 60_000 });
    await expect(page.getByText('the capital of Italy.')).toHaveCount(0);

    const snapshot = await sameOriginJSON(page, `/threads/${threadId}/messages`);
    expect(snapshot.status, snapshot.text).toBe(200);
    const messages = (JSON.parse(snapshot.text) as { readonly messages: SnapshotMessage[] })
      .messages;
    await testInfo.attach('snapshot', { body: snapshot.text, contentType: 'application/json' });
    const leaves = leavesOf(messages);
    expect(leaves, snapshot.text).toHaveLength(3);
    expect(messages.filter((message) => message.parentId === null)).toHaveLength(2);
  });
});
