import { expect, test, type Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';

const runRealAgent = process.env.AURA_E2E_REAL_AGENT === '1';

interface FetchResult {
  readonly status: number;
  readonly text: string;
}

interface ConversationRow {
  readonly ID: string;
}

async function sameOriginFetch(
  page: Page,
  url: string,
  init: { readonly method?: string; readonly body?: string } = {},
): Promise<FetchResult> {
  return page.evaluate(
    async ({ requestURL, requestInit }) => {
      const options: RequestInit = { credentials: 'same-origin' };
      if (requestInit.method !== undefined) options.method = requestInit.method;
      if (requestInit.body !== undefined) {
        options.body = requestInit.body;
        options.headers = { 'Content-Type': 'application/json' };
      }
      const response = await fetch(requestURL, options);
      return { status: response.status, text: await response.text() };
    },
    { requestURL: url, requestInit: init },
  );
}

async function createConversation(page: Page, title: string): Promise<string> {
  const response = await sameOriginFetch(page, '/api/conversations', {
    method: 'POST',
    body: JSON.stringify({ title }),
  });
  expect(response.status, response.text).toBe(201);
  const conversation = JSON.parse(response.text) as ConversationRow;
  expect(conversation.ID).toMatch(/^[0-9a-f-]{36}$/i);
  return conversation.ID;
}

async function deleteConversation(page: Page, conversationId: string): Promise<void> {
  const response = await sameOriginFetch(
    page,
    `/api/conversations/${encodeURIComponent(conversationId)}`,
    { method: 'DELETE' },
  );
  expect([200, 204], response.text).toContain(response.status);
}

test.describe('real multi-conversation ownership', () => {
  test.skip(!runRealAgent, 'set AURA_E2E_REAL_AGENT=1 to run the real-agent validation');

  test('keeps A and B isolated across a live run, canonical URL, and second tab', async ({
    page,
  }) => {
    test.setTimeout(240_000);
    await gotoAuthenticated(page, '/');

    const suffix = `${String(Date.now())}-${Math.random().toString(16).slice(2, 8)}`;
    const titleA = `Thread A ${suffix}`;
    const titleB = `Thread B ${suffix}`;
    const answerToken = `THREAD-A-${suffix}`;
    const prompt = `Rispondi soltanto con ${answerToken}. Non usare strumenti.`;
    const answerTokenB = `THREAD-B-${suffix}`;
    const promptB = `Rispondi soltanto con ${answerTokenB}. Non usare strumenti.`;
    const conversationIds: string[] = [];
    let secondPage: Page | undefined;

    try {
      const threadA = await createConversation(page, titleA);
      conversationIds.push(threadA);
      const threadB = await createConversation(page, titleB);
      conversationIds.push(threadB);

      await page.goto(`/c/${encodeURIComponent(threadA)}`, { waitUntil: 'domcontentloaded' });
      await expect(page.getByRole('button', { name: titleA, exact: true })).toHaveAttribute(
        'aria-current',
        'true',
      );

      const composer = page.getByPlaceholder('Ask Aura');
      await composer.fill(prompt);
      const runResponsePromise = page.waitForResponse(
        (response) =>
          new URL(response.url()).pathname === '/agent/run' &&
          response.request().method() === 'POST',
      );
      await composer.press('Enter');
      const runResponse = await runResponsePromise;
      expect(runResponse.status()).toBe(200);
      const runBody = runResponse.request().postDataJSON() as { readonly threadId?: string };
      expect(runBody.threadId).toBe(threadA);

      await page.getByRole('button', { name: titleB, exact: true }).click();
      await expect(page).toHaveURL(new RegExp(`/c/${threadB}$`));
      await expect(page.getByRole('button', { name: titleB, exact: true })).toHaveAttribute(
        'aria-current',
        'true',
      );
      await expect(page.getByText(prompt, { exact: true })).toHaveCount(0);
      await expect(page.getByText(answerToken, { exact: false })).toHaveCount(0);

      const composerB = page.getByPlaceholder('Ask Aura');
      await composerB.fill(promptB);
      const runBResponsePromise = page.waitForResponse(
        (response) =>
          new URL(response.url()).pathname === '/agent/run' &&
          response.request().method() === 'POST',
      );
      await composerB.press('Enter');
      const runBResponse = await runBResponsePromise;
      expect(runBResponse.status()).toBe(200);
      const runBBody = runBResponse.request().postDataJSON() as { readonly threadId?: string };
      expect(runBBody.threadId).toBe(threadB);

      secondPage = await page.context().newPage();
      await gotoAuthenticated(secondPage, `/c/${encodeURIComponent(threadB)}`);
      await expect(secondPage).toHaveURL(new RegExp(`/c/${threadB}$`));
      await expect(secondPage.getByRole('button', { name: titleB, exact: true })).toHaveAttribute(
        'aria-current',
        'true',
      );
      await expect(secondPage.getByText(prompt, { exact: true })).toHaveCount(0);
      await expect(secondPage.getByText(promptB, { exact: true })).toBeVisible();
      await secondPage.close();
      secondPage = undefined;

      await page.getByRole('button', { name: titleA, exact: true }).click();
      await expect(page).toHaveURL(new RegExp(`/c/${threadA}$`));
      await expect(page.getByText(prompt, { exact: true })).toBeVisible({ timeout: 180_000 });
      await expect(page.getByText(answerToken, { exact: false }).last()).toBeVisible({
        timeout: 180_000,
      });
      await page.getByPlaceholder('Ask Aura').fill(`follow-up ${suffix}`);
      await expect(page.getByRole('button', { name: 'Send message' })).toBeEnabled({
        timeout: 180_000,
      });
      await page.getByPlaceholder('Ask Aura').fill('');

      await page.getByRole('button', { name: titleB, exact: true }).click();
      await expect(page).toHaveURL(new RegExp(`/c/${threadB}$`));
      await expect(page.getByText(promptB, { exact: true })).toBeVisible({ timeout: 180_000 });
      await expect(page.getByText(answerTokenB, { exact: false }).last()).toBeVisible({
        timeout: 180_000,
      });
      await expect(page.getByText(prompt, { exact: true })).toHaveCount(0);
      await expect(page.getByText(answerToken, { exact: false })).toHaveCount(0);
    } finally {
      await secondPage?.close();
      for (const conversationId of conversationIds.reverse()) {
        await deleteConversation(page, conversationId);
      }
    }
  });
});
