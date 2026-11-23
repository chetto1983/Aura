import { expect, test, type APIRequestContext, type Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';

const servers = ['calendar', 'memory', 'whatsapp'] as const;
const origin = process.env.AURA_E2E_ORIGIN ?? 'http://127.0.0.1:9080';
const runRealAgent = process.env.AURA_E2E_REAL_AGENT === '1';
// scripts/pim_tls_mail_up.sh: GreenMail over TLS on loopback 993 (IMAPS) and 465 (SMTPS), its API
// at AURA_E2E_FAKE_MAIL_API (the script reads the same variable), and a PIM on :8093 that trusts its certificate. The form sends no security key for a
// domain discovery does not know, so the PIM takes it from these two ports (spec A2), and the host
// is private, which only the operator, who holds identity.create, may name (spec A5).
const tlsMailUp = process.env.AURA_E2E_PIM_TLS_MAIL === '1';
const fakeMailApi = process.env.AURA_E2E_FAKE_MAIL_API ?? 'http://127.0.0.1:18025';
const fakeMailHost = '127.0.0.1';
const recipient = 'pim-e2e-recipient@aura.test';

interface GreenMailMessage {
  readonly subject?: string;
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

async function openMailSettings(page: Page) {
  await page.goto('/?settings=mail', { waitUntil: 'domcontentloaded' });
  await expect(
    page.getByRole('heading', { name: /^(Mail and calendar|Posta e calendario)$/ }),
  ).toBeVisible({ timeout: 30_000 });
}

function accountRow(page: Page, address: string) {
  return page.getByRole('listitem').filter({ hasText: address });
}

/** Connects `address` from Settings → «Posta e calendario», as a person does: the address, then
 * the server settings discovery could not fill, then «Collega». The PIM signs in to both servers
 * over TLS before it stores anything. Answers the account id the PIM stored. */
async function connectMailbox(page: Page, address: string): Promise<string> {
  const addressField = page.getByLabel(/^(Email address|Indirizzo email)$/);
  await addressField.fill(address);
  const discovered = page.waitForResponse(
    (response) =>
      response.request().method() === 'POST' &&
      new URL(response.url()).pathname === '/api/connect/pim/discover',
    { timeout: 30_000 },
  );
  // Leaving the field asks the PIM about the domain, and its answer refills the server fields:
  // they are typed only once it has landed.
  await addressField.blur();
  await discovered;
  const serverSettings = page
    .locator('details')
    .filter({ hasText: /Server settings|Impostazioni server/ });
  await expect(serverSettings).toHaveAttribute('open', '');
  await serverSettings.getByLabel(/^(IMAP host|Host IMAP)$/).fill(fakeMailHost);
  await serverSettings.getByLabel(/^(IMAP port|Porta IMAP)$/).fill('993');
  await serverSettings.getByLabel(/^(SMTP host|Host SMTP)$/).fill(fakeMailHost);
  await serverSettings.getByLabel(/^(SMTP port|Porta SMTP)$/).fill('465');
  await expect(serverSettings.getByLabel(/^(Username|Nome utente)$/)).toHaveValue(address);
  await page.getByLabel(/^Password$/).fill('local-e2e-only');

  const createdResponse = page.waitForResponse(
    (response) =>
      response.request().method() === 'POST' &&
      new URL(response.url()).pathname === '/api/connect/pim/accounts',
    { timeout: 40_000 },
  );
  await page.getByRole('button', { name: /^(Connect|Collega)$/ }).click();
  const response = await createdResponse;
  const body = await response.text();
  expect(response.status(), body).toBe(201);
  // A sending server that failed its check would still be stored, with a warning (spec A4).
  const created = JSON.parse(body) as { readonly id?: unknown; readonly smtpWarning?: unknown };
  expect(created.smtpWarning, body).toBeUndefined();
  expect(typeof created.id).toBe('string');
  await expect(
    page.getByRole('status').filter({ hasText: /^(Connected|Collegato)/ }),
  ).toBeVisible();
  await expect(accountRow(page, address)).toBeVisible({ timeout: 30_000 });
  return created.id as string;
}

/** Removes a connected mailbox from its row, as a person does. Settings opens afresh, so the row is
 * awaited while the account list loads rather than taken as absent. */
async function removeMailbox(page: Page, address: string) {
  await openMailSettings(page);
  const row = accountRow(page, address);
  await expect(row).toBeVisible({ timeout: 30_000 });
  await row.getByRole('button', { name: /^(Remove|Rimuovi)$/ }).click();
  await row.getByRole('button', { name: /^(Remove for good|Rimuovi definitivamente)$/ }).click();
  await expect(row).toBeHidden({ timeout: 30_000 });
}

function e2eAddress(project: string): string {
  return `pim-e2e-${project}-${Date.now().toString()}@aura.test`;
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

/** Waits until GreenMail holds `subject` in the recipient's inbox; the mailbox exists only once
 * the first mail to it has arrived. */
async function waitForDeliveredMail(request: APIRequestContext, subject: string) {
  await expect
    .poll(
      async () => {
        const response = await request.get(
          `${fakeMailApi}/api/user/${encodeURIComponent(recipient)}/messages/INBOX`,
        );
        if (!response.ok()) return undefined;
        const messages = (await response.json()) as readonly GreenMailMessage[];
        return messages.find((message) => message.subject === subject)?.subject;
      },
      { timeout: 60_000, intervals: [250, 500, 1_000] },
    )
    .toBe(subject);
}

// The shipped sidecars take no consent: Aura mints their tokens itself (379f41c06,
// cmd/aura/mcp_oauth_provider.go selfIssuedReason), so the panel says why and offers no
// Connect, and each server is healthy with no one having authorized anything.
const selfIssuedReason =
  "Aura issues this sidecar's credential itself and renews it before it expires";

test('runs the built-in MCP servers on credentials Aura issues itself', async ({ page }) => {
  test.setTimeout(120_000);

  await gotoAuthenticated(page, '/');
  // gotoAuthenticated only fixtures the orthogonal first-run and shell-health
  // requests. MCP inventory, authorization and probes stay live.

  await expect(page.locator('.aura-shell')).toBeVisible();
  await openGovernance(page);

  const serverList = page.getByRole('list', { name: 'MCP servers' });

  for (const server of servers) {
    await serverList.getByRole('button').filter({ hasText: server }).first().click();
    await expect(page.getByRole('heading', { name: server, exact: true })).toBeVisible();

    const authorization = page
      .getByRole('heading', { name: 'Your authorization', exact: true })
      .locator('..');
    await expect(authorization.getByText(selfIssuedReason)).toBeVisible({ timeout: 30_000 });
    await expect(authorization.getByRole('button')).toHaveCount(0);
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

// CI does not run these two yet: the fixture needs the PIM image of PR #5 (aura-pim-mcp), which is
// unpublished. Wiring ci.yml's web-e2e job to scripts/pim_tls_mail_up.sh belongs to task L1 step 1
// of docs/superpowers/plans/2026-11-20-pim-account-connect-repair.md, once that image is out.
const tlsMailSkip = 'run scripts/pim_tls_mail_up.sh, then set AURA_E2E_PIM_TLS_MAIL=1';

test('connects a mailbox over TLS from Settings, and removes it', async ({ page }, testInfo) => {
  test.skip(!tlsMailUp, tlsMailSkip);
  test.setTimeout(120_000);
  const address = e2eAddress(testInfo.project.name);

  await gotoAuthenticated(page, '/');
  await openMailSettings(page);
  await connectMailbox(page, address);
  await removeMailbox(page, address);
});

test('sends through the real agent from a mailbox connected in Settings', async ({
  page,
  request,
}, testInfo) => {
  test.skip(!tlsMailUp, tlsMailSkip);
  test.skip(!runRealAgent, 'set AURA_E2E_REAL_AGENT=1 to run the real PIM agent gate');
  test.setTimeout(360_000);
  const address = e2eAddress(testInfo.project.name);

  await gotoAuthenticated(page, '/');
  await openMailSettings(page);
  const accountId = await connectMailbox(page, address);
  let conversationId: string | undefined;
  try {
    const subject = `Aura PIM Cockpit E2E ${Date.now().toString()}`;
    const created = await sameOriginJSON(page, '/api/conversations', 'POST', {
      title: `PIM live E2E ${subject}`,
    });
    expect(created.status, created.text).toBe(201);
    conversationId = (JSON.parse(created.text) as { readonly ID: string }).ID;

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
      `Usa calendar__calendar con action send_email e accountId ${accountId}. ` +
        `Invia a ${recipient}, oggetto esatto "${subject}", corpo testo "PIM cockpit live E2E green".`,
    );
    await composer.press('Enter');

    // The gateway's scopes are rows of one choice, sent by the Approve pill.
    const approveForConversation = page.getByRole('option', {
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
    await page.getByRole('button', { name: /^(Approve|Approva)$/ }).click();
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
    await waitForDeliveredMail(request, subject);
  } finally {
    if (conversationId !== undefined) {
      await sameOriginJSON(
        page,
        `/api/conversations/${encodeURIComponent(conversationId)}`,
        'DELETE',
      );
    }
    // A failed clean-up must not hide why the test failed; the fixture's PIM keeps no volume, so
    // the next `pim_tls_mail_up.sh` starts empty anyway.
    await removeMailbox(page, address).catch((error: unknown) => {
      testInfo.annotations.push({
        type: 'cleanup',
        description: `mailbox ${address} not removed: ${String(error)}`,
      });
    });
  }
});
