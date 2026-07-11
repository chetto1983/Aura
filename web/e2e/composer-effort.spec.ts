import { expect, test, type Page, type Route } from '@playwright/test';
import { gotoAuthenticated } from './auth';

// composer-effort.spec.ts is the Phase-37E terminal acceptance E2E (WEBMODEL-01/03): it drives the
// capability-aware reasoning-effort selector end-to-end against the REAL rebuilt cockpit bundle
// served by `aura serve`. It proves the D-13 dynamic contract (the selector shows ONLY the levels
// the active model advertises — a subset, NEVER the full 7), the WEBMODEL-03 wire (a picked fixed
// level rides POST /agent/run as `aura.effort`; 'auto' is OMITTED), and the per-conversation
// persistence (the selector restores the stored level on thread reopen).
//
// Golden-replay, NOT a live model turn (plan prohibition): GET /api/composer/reasoning-capabilities,
// the single-conversation detail (carrying the persisted `ReasoningEffort`), and the /agent/run SSE
// are ALL mocked at the page-network layer (mirroring composer-skills.spec.ts), so the spec
// exercises the REAL in-browser selector + sseAdapter + run-envelope fold WITHOUT any backend model
// loop or a real graduated-effort assertion (D-09: fidelity is manual-only; CI asserts only the
// wire + the dynamic set + restore).
//
// No-skip-as-green (CLAUDE.md): every test asserts a COUNTED number of DOM/route facts AND the
// run-body interceptor asserts `aura.effort` by exact equality (present for a fixed level, ABSENT
// for auto), so a no-op run FAILS rather than passing green. The shared auth+serve harness is the
// same live gate the sibling e2e specs use; when neither a live serve nor the Authula stack is
// reachable the shared auth helper THROWS. Runs on chromium + mobile-chrome.

// CONV_ID persists 'high'; PLAIN_ID has no stored effort (hydrates auto) — the reopen test flips
// between them to prove the value is per-conversation, not global.
const CONV_ID = '37e5c0de-0000-4a00-9000-effort00high0';
const PLAIN_ID = '37e5c0de-1111-4a00-9000-effort00none0';

// The mocked capability payload: a model advertising a SUBSET {none,low,high} → the endpoint maps
// to auto-first UI symbols {auto,off,low,high}. The selector must render EXACTLY these four, never
// the full auto/off/low/mid/high/extra/max seven (D-13).
function reasoningCapabilitiesBody(): string {
  return JSON.stringify({
    levels: ['auto', 'off', 'low', 'high'],
    default: 'auto',
    backend: 'openai',
    detected: true,
  });
}

// The single-conversation detail projection is the RAW Go store struct (Go field names, no json
// tags — mirrors useConversations.ts). `ReasoningEffort` is the per-conversation persisted symbol
// the selector hydrates from (37E-03); CONV_ID carries 'high', PLAIN_ID carries '' (→ auto).
function conversationDetail(id: string): string {
  return JSON.stringify({
    ID: id,
    Title: id === CONV_ID ? 'Effort thread' : 'Plain thread',
    TitleSet: true,
    Status: 'active',
    ReasoningEffort: id === CONV_ID ? 'high' : '',
    TotalInputTokens: 0,
    TotalOutputTokens: 0,
    TotalCachedTokens: 0,
    TotalCostUSD: 0,
    CreatedAt: '2026-07-11T00:00:00Z',
  });
}

function assistantTurnFrames(): Record<string, unknown>[] {
  return [
    { type: 'RUN_STARTED', threadId: CONV_ID, runId: 'run-effort' },
    { type: 'TEXT_MESSAGE_START', messageId: 'msg-effort' },
    { type: 'TEXT_MESSAGE_CONTENT', messageId: 'msg-effort', delta: 'On it.' },
    { type: 'TEXT_MESSAGE_END', messageId: 'msg-effort' },
    { type: 'RUN_FINISHED', threadId: CONV_ID, runId: 'run-effort', outcome: { type: 'success' } },
  ];
}

function sseFromFrames(frames: readonly Record<string, unknown>[]): string {
  return frames.map((f) => `event: ${String(f.type)}\ndata: ${JSON.stringify(f)}\n\n`).join('');
}

function sseResponse(route: Route, body: string) {
  return route.fulfill({
    status: 200,
    headers: { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache' },
    body,
  });
}

interface RunTracker {
  runCount: number;
  runBody: string | null;
}

// trackRequests captures each /agent/run body at the wire so a test can assert `aura.effort` by
// exact equality (present for a fixed level, absent for auto). A no-op send FAILS its guard.
function trackRequests(page: Page): RunTracker {
  const tracker: RunTracker = { runCount: 0, runBody: null };
  page.on('request', (req) => {
    if (req.method() !== 'POST' || !req.url().includes('/agent/run')) return;
    tracker.runCount += 1;
    tracker.runBody = req.postData();
  });
  return tracker;
}

// installComposerRoutes mocks every network dependency of the chat lane so it mounts
// deterministically, with the capability set + conversation detail + /agent/run SSE from fixtures.
async function installComposerRoutes(page: Page) {
  await page.route('**/api/composer/reasoning-capabilities', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: reasoningCapabilitiesBody(),
    }),
  );
  await page.route('**/api/composer/skills', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '{"skills":[]}' }),
  );
  await page.route(/\/api\/conversations(\/[^/?]+)?(\?.*)?$/, (route) => {
    const req = route.request();
    const detailId = /\/api\/conversations\/([^/?]+)/.exec(req.url())?.[1];
    if (req.method() === 'GET' && detailId !== undefined) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: conversationDetail(detailId),
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
  await page.route(/\/api\/assets\?thread_id=/, (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  );
  await page.route('**/agent/run', (route) =>
    sseResponse(route, sseFromFrames(assistantTurnFrames())),
  );
}

test.describe('cockpit composer reasoning-effort selector — dynamic levels + aura.effort wire + per-conversation restore (WEBMODEL-01/03)', () => {
  test('renders EXACTLY the advertised subset (auto/off/low/high, not the full 7); auto OMITS effort and picking high sends aura.effort === high', async ({
    page,
  }) => {
    test.setTimeout(120_000);
    let domAssertions = 0;
    const tracker = trackRequests(page);
    await installComposerRoutes(page);

    // Open a thread with NO stored effort → the selector hydrates 'auto'.
    await gotoAuthenticated(page, `/c/${PLAIN_ID}`);
    const composer = page.getByPlaceholder('Ask Aura');
    await expect(composer).toBeVisible();

    // 1) DYNAMIC (D-13): the selector renders EXACTLY the advertised four, auto-first — never the
    // full seven (Medium/Extra/Max are NOT advertised by this model and must be absent).
    const selector = page.getByRole('combobox', { name: 'Reasoning effort' });
    await expect(selector).toBeVisible();
    await expect(selector.locator('option')).toHaveText(['Auto', 'Off', 'Low', 'High']);
    await expect(selector).toHaveValue('auto');
    domAssertions += 1;

    // 2) AUTO OMITS: sending on the default 'auto' carries NO aura.effort (the adaptive default is
    // the absence of the field, WEBMODEL-03).
    await composer.fill('a plain question');
    await Promise.all([
      page.waitForRequest((req) => req.url().includes('/agent/run') && req.method() === 'POST'),
      composer.press('Enter'),
    ]);
    await expect.poll(() => tracker.runCount, { timeout: 10_000 }).toBe(1);
    const autoBody = JSON.parse(tracker.runBody ?? '{}') as { aura?: { effort?: string } };
    expect(autoBody.aura?.effort).toBeUndefined();
    domAssertions += 1;

    // 3) WIRE: pick 'high', send, and the intercepted /agent/run body carries aura.effort === high.
    await selector.selectOption('high');
    await expect(selector).toHaveValue('high');
    await composer.fill('think hard about this');
    await Promise.all([
      page.waitForRequest((req) => req.url().includes('/agent/run') && req.method() === 'POST'),
      composer.press('Enter'),
    ]);
    await expect.poll(() => tracker.runCount, { timeout: 10_000 }).toBe(2);
    const highBody = JSON.parse(tracker.runBody ?? '{}') as { aura?: { effort?: string } };
    expect(highBody.aura?.effort).toBe('high');
    domAssertions += 1;

    // 4) NO CLEAR: unlike the pinned skill, the effort is a per-conversation preference — it stays
    // selected after the send (no reset to auto).
    await expect(selector).toHaveValue('high');
    domAssertions += 1;

    expect(domAssertions).toBeGreaterThanOrEqual(4);
  });

  test('restores the persisted per-conversation level on thread reopen (high on CONV, auto on the other)', async ({
    page,
  }) => {
    test.setTimeout(120_000);
    let domAssertions = 0;
    await installComposerRoutes(page);

    // Open the conversation whose stored effort is 'high' → the selector hydrates to it.
    await gotoAuthenticated(page, `/c/${CONV_ID}`);
    const selector = page.getByRole('combobox', { name: 'Reasoning effort' });
    await expect(selector).toBeVisible();
    await expect(selector).toHaveValue('high');
    domAssertions += 1;

    // Switch to a DIFFERENT conversation (no stored effort) → the selector hydrates 'auto' (the
    // value is per-conversation, not a global sticky).
    await page.goto(`/c/${PLAIN_ID}`, { waitUntil: 'domcontentloaded' });
    await expect(page.getByPlaceholder('Ask Aura')).toBeVisible();
    await expect(selector).toHaveValue('auto');
    domAssertions += 1;

    // REOPEN the first conversation → the persisted 'high' is restored (WEBMODEL-01 persistence).
    await page.goto(`/c/${CONV_ID}`, { waitUntil: 'domcontentloaded' });
    await expect(page.getByPlaceholder('Ask Aura')).toBeVisible();
    await expect(selector).toHaveValue('high');
    domAssertions += 1;

    expect(domAssertions).toBeGreaterThanOrEqual(3);
  });
});
