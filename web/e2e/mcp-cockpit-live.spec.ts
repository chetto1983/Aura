import { expect, test, type APIRequestContext, type Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';

const servers = ['calendar', 'memory', 'whatsapp'] as const;
const origin = process.env.AURA_E2E_ORIGIN ?? 'http://127.0.0.1:9080';
const fakeSmtpOrigin = process.env.AURA_E2E_FAKE_SMTP_ORIGIN ?? 'http://127.0.0.1:1080';
const runRealAgent = process.env.AURA_E2E_REAL_AGENT === '1';
const pimAccountId = 'pim-e2e';

interface MailCatcherMessage {
  readonly id: number;
  readonly subject: string;
  readonly recipients: readonly string[];
}

async function openGovernance(page: Page) {
  // Desktop and mobile expose the same mode button under different nav landmarks.
  await page.getByRole('button', { name: 'Governance', exact: true }).first().click();
  await expect(page.getByRole('list', { name: 'MCP servers' })).toBeVisible();
}

async function closeServerDetailIfOverlay(page: Page) {
  const close = page
    .getByRole('complementary', { name: 'Select a row to see details' })
    .getByRole('button', { name: 'Close', exact: true });
  if (await close.isVisible()) await close.click();
}

async function openCalendar(page: Page) {
  await openGovernance(page);
  await page
    .getByRole('list', { name: 'MCP servers' })
    .getByRole('button')
    .filter({ hasText: 'calendar' })
    .first()
    .click();
  await expect(page.getByRole('heading', { name: 'calendar', exact: true })).toBeVisible();
  await expect(
    page.getByRole('heading', {
      name: /Connect calendar \/ PIM account|Collega account calendario \/ PIM/i,
    }),
  ).toBeVisible();
}

async function removePimAccountIfPresent(page: Page) {
  const account = page.getByRole('listitem').filter({ hasText: pimAccountId });
  if (!(await account.isVisible())) return;
  await account.getByRole('button', { name: /Disconnect|Disconnetti/i }).click();
  await expect(account).toBeHidden({ timeout: 30_000 });
}

async function createPimAccount(page: Page) {
  await page.getByLabel(/^(Provider)$/i).selectOption('imap');
  await page.getByLabel(/^(Account ID|ID account)$/i).fill(pimAccountId);
  await page.getByLabel(/^(Display name|Nome visualizzato)$/i).fill('Aura PIM E2E');
  await page.getByLabel(/^(IMAP host|Host IMAP)$/i).fill('aura-fake-smtp');
  await page.getByLabel(/^(IMAP port|Porta IMAP)$/i).fill('1143');
  await page.getByLabel(/^(SMTP host|Host SMTP)$/i).fill('aura-fake-smtp');
  await page.getByLabel(/^(SMTP port|Porta SMTP)$/i).fill('1025');
  await page.getByLabel(/^(Username|Nome utente)$/i).fill('aura-pim@aura.invalid');
  await page.getByLabel(/^Password$/i).fill('local-e2e-only');
  const createdResponse = page.waitForResponse(
    (response) =>
      response.request().method() === 'POST' &&
      new URL(response.url()).pathname === '/api/connect/pim/accounts',
    { timeout: 30_000 },
  );
  await page.getByRole('button', { name: /Create account|Crea account/i }).click();
  const created = await createdResponse;
  const createdBody = await created.text();
  expect(created.ok(), createdBody).toBe(true);
  const canonicalId = (JSON.parse(createdBody) as { readonly id?: unknown }).id;
  expect(typeof canonicalId).toBe('string');
  await expect(page.getByText(/Account created|Account creato/i)).toBeVisible({
    timeout: 30_000,
  });
  await expect(page.getByRole('listitem').filter({ hasText: pimAccountId })).toBeVisible({
    timeout: 30_000,
  });
  return canonicalId as string;
}

async function sameOriginJSON(page: Page, path: string, method: 'POST' | 'DELETE', body?: object) {
  return page.evaluate(
    async ({ requestPath, requestMethod, requestBody }) => {
      const response = await fetch(requestPath, {
        method: requestMethod,
        credentials: 'same-origin',
        ...(requestBody === undefined
          ? {}
          : { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(requestBody) }),
      });
      return { status: response.status, text: await response.text() };
    },
    { requestPath: path, requestMethod: method, requestBody: body },
  );
}

async function waitForCapturedMail(request: APIRequestContext, subject: string) {
  await expect
    .poll(
      async () => {
        const response = await request.get(`${fakeSmtpOrigin}/messages`);
        if (!response.ok()) return undefined;
        const messages = (await response.json()) as readonly MailCatcherMessage[];
        return messages.find((message) => message.subject === subject);
      },
      { timeout: 60_000, intervals: [250, 500, 1_000] },
    )
    .toMatchObject({
      subject,
      recipients: expect.arrayContaining([expect.stringContaining('pim-e2e@aura.invalid')]),
    });
}

test('authorizes the built-in MCP servers through the live cockpit', async ({ page, context }) => {
  test.setTimeout(120_000);

  await gotoAuthenticated(page, '/');
  // gotoAuthenticated only fixtures the orthogonal first-run and shell-health
  // requests. MCP inventory, authorization, probes and popup callbacks stay live.

  await expect(page.locator('.aura-shell')).toBeVisible();
  await openGovernance(page);

  const serverList = page.getByRole('list', { name: 'MCP servers' });

  for (const server of servers) {
    await serverList.getByRole('button').filter({ hasText: server }).first().click();
    await expect(page.getByRole('heading', { name: server, exact: true })).toBeVisible();

    const authorization = page
      .getByRole('heading', { name: 'Your authorization', exact: true })
      .locator('..');
    await expect(authorization).toBeVisible();

    const connect = authorization.getByRole('button', { name: 'Connect', exact: true });
    if (await connect.isVisible()) {
      const popupPromise = context.waitForEvent('page');
      await connect.click();
      const popup = await popupPromise;
      await popup.waitForLoadState('domcontentloaded');
      await expect(popup.getByText('Aura is authorized. You can close this tab.')).toBeVisible({
        timeout: 30_000,
      });
      await popup.close();
    }

    await expect(authorization.getByText('Authorized for your identity')).toBeVisible({
      timeout: 30_000,
    });
    await closeServerDetailIfOverlay(page);
  }

  await page.reload({ waitUntil: 'domcontentloaded' });
  await openGovernance(page);
  const refreshedList = page.getByRole('list', { name: 'MCP servers' });
  for (const server of servers) {
    const row = refreshedList.getByRole('button').filter({ hasText: server }).first();
    await row.click();
    const status = row.getByRole('status');
    try {
      await expect(status).toHaveText(/Healthy · \d+ tools/, { timeout: 15_000 });
    } catch (error) {
      const detail = await page
        .getByRole('heading', { name: server, exact: true })
        .locator('../..')
        .innerText();
      throw new Error(`${server} cockpit detail:\n${detail}`, { cause: error });
    }
    await closeServerDetailIfOverlay(page);
  }
});

test('configures PIM in the cockpit and sends through the real agent to fake SMTP', async ({
  page,
  request,
}) => {
  test.skip(!runRealAgent, 'set AURA_E2E_REAL_AGENT=1 to run the real PIM agent gate');
  test.setTimeout(360_000);

  await gotoAuthenticated(page, '/');
  await openCalendar(page);
  await removePimAccountIfPresent(page);
  const canonicalAccountId = await createPimAccount(page);

  const subject = `Aura PIM Cockpit E2E ${Date.now().toString()}`;
  const created = await sameOriginJSON(page, '/api/conversations', 'POST', {
    title: `PIM live E2E ${subject}`,
  });
  expect(created.status, created.text).toBe(201);
  const conversationId = (JSON.parse(created.text) as { readonly ID: string }).ID;

  try {
    await page.goto(`${origin}/c/${encodeURIComponent(conversationId)}`, {
      waitUntil: 'domcontentloaded',
    });
    await page
      .getByRole('navigation', { name: 'Primary' })
      .getByRole('button', { name: 'Chat', exact: true })
      .click();
    const composer = page.getByRole('textbox', { name: 'Ask Aura' });
    await expect(composer).toBeVisible({ timeout: 30_000 });
    await composer.fill(
      `Usa calendar__calendar con action send_email e accountId ${canonicalAccountId}. ` +
        `Invia a pim-e2e@aura.invalid, oggetto esatto "${subject}", corpo testo "PIM cockpit live E2E green".`,
    );
    await composer.press('Enter');

    const approveForConversation = page.getByRole('button', {
      name: /Approve .* for this conversation|Approva .* per questa conversazione/i,
    });
    await expect(approveForConversation).toBeVisible({ timeout: 90_000 });
    const resolutionResponse = page.waitForResponse(
      (response) =>
        response.request().method() === 'POST' &&
        new URL(response.url()).pathname.startsWith('/api/approvals/') &&
        new URL(response.url()).pathname.endsWith('/resolve'),
      { timeout: 90_000 },
    );
    const resumedRunResponse = page
      .waitForResponse(
        (response) =>
          response.request().method() === 'POST' &&
          new URL(response.url()).pathname === '/agent/run',
        { timeout: 30_000 },
      )
      .catch(() => undefined);
    await approveForConversation.click();
    const resolved = await resolutionResponse;
    const resolvedBody = await resolved.text();
    expect(resolved.ok(), resolvedBody).toBe(true);
    expect(JSON.parse(resolvedBody) as object).toMatchObject({ outcome: 'continue' });
    const resumed = await resumedRunResponse;
    if (resumed === undefined) {
      throw new Error(`approval resolved without a follow-up /agent/run response: ${resolvedBody}`);
    }
    expect(resumed.ok()).toBe(true);
    expect(await resumed.finished()).toBeNull();
    await expect(page.getByTestId('footer-settled-status')).toContainText(
      /Run complete|Esecuzione completata/i,
      {
        timeout: 240_000,
      },
    );
    await waitForCapturedMail(request, subject);
  } finally {
    await sameOriginJSON(
      page,
      `/api/conversations/${encodeURIComponent(conversationId)}`,
      'DELETE',
    );
    await page.goto(origin, { waitUntil: 'domcontentloaded' });
    await openCalendar(page);
    await removePimAccountIfPresent(page);
  }
});
