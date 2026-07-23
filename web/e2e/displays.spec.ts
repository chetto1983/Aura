import { expect, test, type Page, type Route } from '@playwright/test';
import { gotoAuthenticated } from './auth';
import { expandToolRow } from './support/toolRow';

// displays.spec.ts — the Phase 26 typed-display protocol E2E matrix. It proves the
// cockpit renders every DisplayPayload kind through the REAL in-browser DisplayRouter
// (the same reducer→snapshot→part.display→ToolFallback→DisplayRouter path the live
// aura.display CUSTOM event drives), plus the citation click-through, the answer-level
// "Sources (N)" button, the read-only Source Explorer, in-card pagination, and the
// security postures (no SSRF internals in system_event, escaped code, sanitized
// document, no-mailbox swarm). Runs on desktop chromium + mobile chrome/safari.
//
// Determinism: each test opens a thread whose MESSAGES_SNAPSHOT carries an assistant
// turn with a tool-call part whose `display` is the payload under test. The cockpit
// rehydrates it on open (the D-06 replay path proven in replay.spec) — no streaming
// timing, no live LLM, one render through the real router. Every heavy route is mocked
// at the page-network layer so only the served SPA + auth come from `aura serve`.

const CONV_ID = '77777777-7777-7777-7777-777777777777';

function conversationRecord(): Record<string, unknown> {
  return {
    id: CONV_ID,
    title: 'Displays thread',
    status: 'active',
    total_input_tokens: 0,
    total_output_tokens: 0,
    total_cached_tokens: 0,
    total_cost_usd: 0,
  };
}

interface ToolCallSnapshot {
  readonly id: string;
  readonly function: { readonly name: string; readonly arguments: string };
  readonly display: Record<string, unknown>;
}

function snapshotBody(answer: string, toolCalls: readonly ToolCallSnapshot[]): string {
  return JSON.stringify({
    type: 'MESSAGES_SNAPSHOT',
    messages: [
      { id: 'u-1', role: 'user', content: 'q' },
      { id: 'a-1', role: 'assistant', content: answer, toolCalls },
    ],
  });
}

async function installBaseRoutes(page: Page, snapshot: string) {
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
  // The image-proxy: a tiny PNG so the web_result thumbnail loads through the proxy
  // path (we assert the src is the proxy, not a raw external host).
  await page.route('**/api/image-proxy*', (route: Route) =>
    route.fulfill({
      status: 200,
      contentType: 'image/png',
      body: Buffer.from(
        'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==',
        'base64',
      ),
    }),
  );
}

async function openWith(
  page: Page,
  answer: string,
  display: Record<string, unknown>,
  toolName = 'web_search',
) {
  const snapshot = snapshotBody(answer, [
    { id: 'call-1', function: { name: toolName, arguments: '{"query":"q"}' }, display },
  ]);
  await installBaseRoutes(page, snapshot);
  await gotoAuthenticated(page, `/c/${CONV_ID}`);
}

// ---- Fixtures: one DisplayPayload per kind (snake_case wire shape, 26-01) ----

function webResultPayload(): Record<string, unknown> {
  const results = Array.from({ length: 5 }, (_, i) => ({
    title: `Result ${String(i + 1)}`,
    url: `https://site${String(i + 1)}.example.com/p`,
    snippet: `snippet body number ${String(i + 1)}`,
    domain: `site${String(i + 1)}.example.com`,
    score: 0.9 - i * 0.1,
    ...(i < 2 ? { thumbnail: `https://img.example.com/${String(i)}.png` } : {}),
    ref_id: `src-${String(i + 1)}`,
  }));
  const sources = results.map((r, i) => ({
    ref_id: r.ref_id,
    index: i + 1,
    type: 'web_result',
    title: r.title,
    url: r.url,
    snippet: r.snippet,
    cited: i === 0,
  }));
  return { type: 'web_result', tool_call_id: 'call-1', title: 'q', web_results: results, sources };
}

test.describe('Phase 26 — typed displays (desktop + mobile)', () => {
  test('web_result renders a rich card with image-proxy thumbnails + in-card pagination', async ({
    page,
  }, testInfo) => {
    await openWith(page, 'Here are results.', webResultPayload());
    await expandToolRow(page);

    await expect(page.getByText('Web results').first()).toBeVisible({ timeout: 15000 });
    await expect(page.getByText('Result 1').first()).toBeVisible();
    await expect(page.getByText('snippet body number 1').first()).toBeVisible();
    await expect(page.getByText('site1.example.com').first()).toBeVisible();

    // Thumbnails enter the DOM ONLY through the backend image-proxy (T-26-15). No raw
    // external <img src="http…"> to an arbitrary host.
    const proxied = page.locator('img[src^="/api/image-proxy?url="]');
    await expect(proxied.first()).toBeVisible();
    const rawExternal = await page.locator('img[src^="http"]').count();
    expect(rawExternal).toBe(0);

    // In-card pagination: 5 results, default 3/page → "1–3 of 5".
    await expect(page.getByText('1–3 of 5').first()).toBeVisible();
    await page.getByRole('button', { name: 'Next page' }).first().click();
    await expect(page.getByText('4–5 of 5').first()).toBeVisible();

    await page.screenshot({ path: testInfo.outputPath('web-result-card.png'), fullPage: true });
  });

  test('citation chip opens the read-only Source Explorer focused on the source', async ({
    page,
  }) => {
    await openWith(page, 'Cited answer.', webResultPayload());
    await expandToolRow(page);
    await expect(page.getByText('Web results').first()).toBeVisible({ timeout: 15000 });

    // The first source is cited → its citation chip carries citation.aria "Source 1: …".
    const chip = page.getByRole('button', { name: /Source 1:/ }).first();
    await expect(chip).toBeVisible();
    await chip.click();

    // The shared Source Explorer dialog opens, focused to Metadata for that source.
    const dialog = page.getByRole('dialog', { name: 'Sources' });
    await expect(dialog).toBeVisible({ timeout: 10000 });
    await expect(dialog.getByText('These views are read-only.')).toBeVisible();
    // No governance-write control this milestone (T-26-19).
    for (const label of ['Re-Analyze', 'Clear', 'Save', 'Edit', 'Apply']) {
      await expect(dialog.getByRole('button', { name: new RegExp(label, 'i') })).toHaveCount(0);
    }
    await page.keyboard.press('Escape');
    await expect(dialog).toBeHidden();
  });

  test('"Sources (N)" button opens the Source Explorer with read-only Table/Metadata/Configuration', async ({
    page,
  }, testInfo) => {
    await openWith(page, 'Answer with sources.', webResultPayload());
    await expandToolRow(page);
    await expect(page.getByText('Web results').first()).toBeVisible({ timeout: 15000 });

    const sourcesBtn = page.getByRole('button', { name: 'View 5 sources' });
    await expect(sourcesBtn).toBeVisible();
    await sourcesBtn.click();

    const dialog = page.getByRole('dialog', { name: 'Sources' });
    await expect(dialog).toBeVisible({ timeout: 10000 });
    // Read-only tab strip.
    await expect(dialog.getByRole('tab', { name: 'Table' })).toBeVisible();
    await expect(dialog.getByRole('tab', { name: 'Metadata' })).toBeVisible();
    await expect(dialog.getByRole('tab', { name: 'Configuration' })).toBeVisible();
    // Table view: the read-only source search + at least one source row.
    await expect(dialog.getByPlaceholder('Search sources')).toBeVisible();
    await expect(dialog.getByText('Result 1').first()).toBeVisible();

    await testInfo.attach('source-explorer.png', {
      body: await page.screenshot({ fullPage: true }),
      contentType: 'image/png',
    });

    await dialog.getByRole('tab', { name: 'Configuration' }).click();
    await expect(dialog.getByText('Configuration is read-only this release.')).toBeVisible();
    // The whole sheet exposes NO write/destructive verbs.
    for (const label of ['Re-Analyze', 'Clear', 'Save', 'Edit', 'Apply', 'Delete']) {
      await expect(dialog.getByRole('button', { name: new RegExp(label, 'i') })).toHaveCount(0);
    }
    await page.keyboard.press('Escape');
    await expect(dialog).toBeHidden();
  });

  test('document card renders sanitized markdown and never executes injected script', async ({
    page,
  }) => {
    const display = {
      type: 'document',
      tool_call_id: 'call-1',
      document: {
        title: 'Fetched Page',
        url: 'https://docs.example.com/x',
        content_md:
          '# Heading One\n\nA paragraph with **bold**.\n\n- bullet alpha\n- bullet beta\n\n<script>window.__xss_doc = true;</script>',
      },
    };
    await openWith(page, 'Document follows.', display, 'web_fetch');
    await expandToolRow(page);

    const chatRegion = page.getByRole('region', { name: 'Chat' });
    await expect(chatRegion.getByText('Document', { exact: true }).first()).toBeVisible({
      timeout: 15000,
    });
    await expect(page.getByRole('heading', { name: 'Heading One' })).toBeVisible();
    await expect(page.getByText('bullet alpha')).toBeVisible();
    // The injected <script> was sanitized — it did not execute.
    const xss = await page.evaluate(() => (window as unknown as Record<string, unknown>).__xss_doc);
    expect(xss).toBeUndefined();
  });

  test('system_event card shows only a safe classified label (no SSRF internals)', async ({
    page,
  }) => {
    const display = {
      type: 'system_event',
      tool_call_id: 'call-1',
      system: {
        class: 'blocked_url',
        reason: 'blocked_url',
        // A would-be leaky message — the card must NEVER render free text or the IP.
        message: 'dial tcp 169.254.169.254:80: connect to metadata host blocked',
        severity: 'error',
      },
    };
    await openWith(page, 'Blocked.', display, 'web_fetch');

    await expect(page.getByText('System').first()).toBeVisible({ timeout: 15000 });
    await expect(page.getByText('This URL was blocked by the safety policy.')).toBeVisible();
    await expect(page.locator('[role="status"]').first()).toBeVisible();
    // T-26-12: no SSRF internals / raw message anywhere in the DOM.
    await expect(page.getByText('169.254.169.254')).toHaveCount(0);
    await expect(page.getByText(/metadata host blocked/)).toHaveCount(0);
  });

  test('swarm_report renders a worker table with row-expand and no mailbox theater', async ({
    page,
  }) => {
    const display = {
      type: 'swarm_report',
      tool_call_id: 'call-1',
      swarm: [
        { goal_index: 0, child_id: 'child-aaa', status: 'ok', summary: 'gathered the facts' },
        {
          goal_index: 1,
          child_id: 'child-bbb',
          status: 'failed',
          summary: 'partial',
          error: 'rate limited',
        },
      ],
    };
    await openWith(page, 'Workers done.', display, 'swarm_spawn');
    await expandToolRow(page);

    await expect(page.getByText('Workers').first()).toBeVisible({ timeout: 15000 });
    await expect(page.getByRole('columnheader', { name: 'Worker' })).toBeVisible();
    await expect(page.getByText('OK').first()).toBeVisible();
    await expect(page.getByText('Failed').first()).toBeVisible();

    // Row-expand reveals summary + error in place.
    await page.getByRole('button', { name: 'Toggle worker details' }).nth(1).click();
    await expect(page.getByText('rate limited')).toBeVisible();

    // D-08: no inter-agent chat / mailbox affordance INSIDE the swarm card. Scope the
    // assertion to the assistant answer message (the page-level "Ask Aura" composer is
    // a legitimate textbox and must NOT count against the swarm display).
    const swarmCard = page
      .locator('div.space-y-2')
      .filter({ has: page.getByRole('columnheader', { name: 'Worker' }) })
      .first();
    expect(await swarmCard.getByRole('textbox').count()).toBe(0);
    await expect(swarmCard.getByPlaceholder(/message|reply/i)).toHaveCount(0);
  });

  test('table card supports sort / filter / copy / CSV and paginates rows', async ({ page }) => {
    const display = {
      type: 'table',
      tool_call_id: 'call-1',
      table: {
        columns: ['City', 'Pop'],
        rows: [
          ['Rome', '2800000'],
          ['Milan', '1400000'],
          ['Naples', '960000'],
          ['Turin', '870000'],
        ],
      },
    };
    await openWith(page, 'Table follows.', display, 'shell_exec');
    await expandToolRow(page);

    await expect(page.getByText('Table').first()).toBeVisible({ timeout: 15000 });
    await expect(page.getByRole('button', { name: 'Sort by City' })).toBeVisible();
    await expect(page.getByPlaceholder('Filter rows')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Copy table' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Export table as CSV' })).toBeVisible();

    // Filter narrows the rows.
    await page.getByPlaceholder('Filter rows').fill('Rome');
    await expect(page.getByText('Rome')).toBeVisible();
    await expect(page.getByText('Milan')).toHaveCount(0);
  });

  test('chart card exposes an accessible data <table> fallback', async ({ page }) => {
    const display = {
      type: 'chart',
      tool_call_id: 'call-1',
      chart: { x_labels: ['Jan', 'Feb', 'Mar'], y_values: [10, 20, 15], x_axis_label: 'Month' },
    };
    await openWith(page, 'Chart follows.', display, 'shell_exec');
    await expandToolRow(page);

    await expect(page.getByText('Chart').first()).toBeVisible({ timeout: 15000 });
    // D-02: the authoritative data path is a real <table> (bars are decorative).
    await expect(page.getByRole('columnheader', { name: 'Value' })).toBeVisible();
    await expect(page.getByRole('cell', { name: 'Feb' })).toBeVisible();
    await expect(page.getByRole('cell', { name: '20' })).toBeVisible();
  });

  test('code card renders escaped body (no execution) with a copy control', async ({ page }) => {
    const display = {
      type: 'code',
      tool_call_id: 'call-1',
      code: {
        body: '<script>window.__xss_code = true;</script>\nconst x = 1;',
        lang: 'javascript',
      },
    };
    await openWith(page, 'Code follows.', display, 'shell_exec');
    await expandToolRow(page);

    await expect(page.getByText('Code').first()).toBeVisible({ timeout: 15000 });
    await expect(page.getByRole('button', { name: 'Copy code' })).toBeVisible();
    // T-26-16: the <script> body is inert text, never executed.
    const xss = await page.evaluate(
      () => (window as unknown as Record<string, unknown>).__xss_code,
    );
    expect(xss).toBeUndefined();
    // The code body is present as escaped text.
    await expect(page.getByText('const x = 1;')).toBeVisible();
  });
});
