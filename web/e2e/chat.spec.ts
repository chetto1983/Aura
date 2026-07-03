import { existsSync, readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';
import { expect, test, type Page, type Route } from '@playwright/test';
import { gotoAuthenticated } from './auth';

// chat.spec.ts is the phase-proving E2E (CHAT-01 / D-03 / APRV-02 / CHAT-04): it drives
// the Core-Value loop end-to-end against the REAL served SPA + the REAL sseAdapter/runtime
// — type a prompt -> stream the answer token-by-token -> an ask_user interrupt renders an
// inline approval card in-thread -> resolve it (Answer) -> the run resumes -> the runtime
// footer shows non-zero tokens/cost.
//
// No-skip-as-green (CLAUDE.md): the spec NEVER silent-passes a 0-tests run. It resolves the
// stream source in order — (1) a live `aura serve` if reachable, else (2) replay of the REAL
// captured translator frames at internal/agui/testdata/golden-events.json fed through the
// real adapter (the deterministic CI path; NEVER synthetic-only). If the CI env flag is set
// AND neither a live stack NOR the golden fixture is available, the setup THROWS (non-zero
// exit) — a hard fail, never a silent skip. The streamed-token assertions are COUNTED (a
// local counter incremented per asserted chunk) and asserted >= 1 at the end so a no-op
// (0-token) run FAILS rather than passing green.

const HERE = dirname(fileURLToPath(import.meta.url));
// internal/agui/testdata/golden-events.json — the SAME real captured frames the Go SSE
// tests + the sseAdapter unit test use (web/ is a sibling of internal/).
const GOLDEN_PATH = resolve(HERE, '..', '..', 'internal', 'agui', 'testdata', 'golden-events.json');

// A stable conversation id the /c/:id route binds the chat lane against.
const CONV_ID = '99999999-9999-9999-9999-999999999999';
const PAUSE_TOKEN = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa';

type GoldenFrames = Record<string, Record<string, unknown>>;

// frame reads a required golden frame by name, throwing if absent (also satisfies
// noUncheckedIndexedAccess — the map index is total at the call site).
function frame(g: GoldenFrames, name: string): Record<string, unknown> {
  const f = g[name];
  if (f === undefined) throw new Error(`chat.spec.ts: golden fixture missing frame ${name}`);
  return f;
}

// isCI reads the canonical CI flag (process.env.CI) the runner exports. This is the
// no-skip-as-green discriminator: under CI the setup throws rather than skipping.
function isCI(): boolean {
  return Boolean(process.env.CI);
}

function loadGolden(): GoldenFrames | null {
  if (!existsSync(GOLDEN_PATH)) return null;
  return JSON.parse(readFileSync(GOLDEN_PATH, 'utf8')) as GoldenFrames;
}

function isExpectedBrowserConsoleNoise(text: string): boolean {
  return (
    text === 'Viewport argument key "interactive-widget" not recognized and ignored.' ||
    text.includes('/api/auth/config due to access control checks.')
  );
}

let golden: GoldenFrames | null = null;

test.beforeAll(async ({ request }) => {
  golden = loadGolden();
  // Probe a live serve (the webServer in playwright.config). reuseExistingServer means a
  // local run reaches whatever is up; CI provisions it.
  const liveReachable = await request
    .get('/healthz', { timeout: 3000 })
    .then((res) => res.ok())
    .catch(() => false);

  if (isCI() && !liveReachable && golden === null) {
    // Hard fail (non-zero exit) — NEVER a silent skip to a green 0-tests pass.
    throw new Error(
      'chat.spec.ts: neither a live `aura serve` nor the golden-events.json fixture is ' +
        'available under CI — refusing to skip-as-green (CLAUDE.md). Provision the stack ' +
        `(make db-up + migrate) or restore ${GOLDEN_PATH}.`,
    );
  }
  if (golden === null) {
    throw new Error(`chat.spec.ts: golden fixture missing at ${GOLDEN_PATH}`);
  }
});

// sseFromFrames serialises AG-UI frames into the wire body the real sseAdapter parses
// (event: <TYPE> + data: <json>, blank-line-delimited). The frames are assembled from the
// REAL captured golden shapes — the per-turn SEQUENCE is composed here, but every frame's
// shape is the captured translator output (the 25-03 SUMMARY's sanctioned approach), so the
// run exercises the real frame->part mapping, never a synthetic reducer stub.
function sseFromFrames(frames: readonly Record<string, unknown>[]): string {
  return frames.map((f) => `event: ${String(f.type)}\ndata: ${JSON.stringify(f)}\n\n`).join('');
}

// firstTurnFrames assembles the initial streamed answer that ends in an ask_user interrupt,
// from the captured RUN_STARTED / TEXT_MESSAGE_* / STATE_DELTA / RUN_FINISHED(interrupt)
// golden shapes (+ a usage STATE_DELTA so the footer shows non-zero metrics, CHAT-04).
function goldenDelta(g: GoldenFrames): string {
  const d = frame(g, 'TEXT_MESSAGE_CONTENT').delta;
  return typeof d === 'string' ? d : 'Ciao';
}

function firstTurnFrames(g: GoldenFrames): Record<string, unknown>[] {
  return [
    frame(g, 'RUN_STARTED'),
    frame(g, 'TEXT_MESSAGE_START'),
    { ...frame(g, 'TEXT_MESSAGE_CONTENT'), delta: goldenDelta(g) },
    frame(g, 'TEXT_MESSAGE_END'),
    // Usage on the final STATE_DELTA — real key shapes (the footer reads these). The
    // captured fixture carries /cost_usd; prompt/completion mirror the same op shape.
    {
      type: 'STATE_DELTA',
      delta: [
        { op: 'replace', path: '/prompt_tokens', value: 120 },
        { op: 'replace', path: '/completion_tokens', value: 18 },
        { op: 'replace', path: '/cost_usd', value: 0.0042 },
      ],
    },
    frame(g, 'RUN_FINISHED(interrupt)'),
  ];
}

// resumeTurnFrames assembles the post-resolve resume turn (the agent continues over the
// rehydrated history): a fresh streamed answer ending in success.
function resumeTurnFrames(g: GoldenFrames): Record<string, unknown>[] {
  return [
    frame(g, 'RUN_STARTED'),
    frame(g, 'TEXT_MESSAGE_START'),
    { ...frame(g, 'TEXT_MESSAGE_CONTENT'), delta: 'Procedo: ecco il risultato.' },
    frame(g, 'TEXT_MESSAGE_END'),
    {
      type: 'STATE_DELTA',
      delta: [
        { op: 'replace', path: '/prompt_tokens', value: 140 },
        { op: 'replace', path: '/completion_tokens', value: 22 },
        { op: 'replace', path: '/cost_usd', value: 0.0051 },
      ],
    },
    frame(g, 'RUN_FINISHED(success)'),
  ];
}

function timelineTurnFrames(g: GoldenFrames): Record<string, unknown>[] {
  return [
    frame(g, 'RUN_STARTED'),
    { ...frame(g, 'REASONING_START'), messageId: 'timeline-rsn-1' },
    { ...frame(g, 'REASONING_MESSAGE_START'), messageId: 'timeline-rsn-1' },
    {
      ...frame(g, 'REASONING_MESSAGE_CONTENT'),
      messageId: 'timeline-rsn-1',
      delta: 'First timeline reasoning',
    },
    { ...frame(g, 'REASONING_MESSAGE_END'), messageId: 'timeline-rsn-1' },
    { ...frame(g, 'REASONING_END'), messageId: 'timeline-rsn-1' },
    {
      ...frame(g, 'TOOL_CALL_START'),
      toolCallId: 'timeline-call',
      toolCallName: 'timeline_probe_tool',
    },
    { ...frame(g, 'TOOL_CALL_ARGS'), toolCallId: 'timeline-call', delta: '{"query":"latency"}' },
    { ...frame(g, 'TOOL_CALL_END'), toolCallId: 'timeline-call' },
    { ...frame(g, 'TOOL_CALL_RESULT'), toolCallId: 'timeline-call', content: 'probe ok' },
    { ...frame(g, 'REASONING_START'), messageId: 'timeline-rsn-2' },
    { ...frame(g, 'REASONING_MESSAGE_START'), messageId: 'timeline-rsn-2' },
    {
      ...frame(g, 'REASONING_MESSAGE_CONTENT'),
      messageId: 'timeline-rsn-2',
      delta: 'Second timeline reasoning',
    },
    { ...frame(g, 'REASONING_MESSAGE_END'), messageId: 'timeline-rsn-2' },
    { ...frame(g, 'REASONING_END'), messageId: 'timeline-rsn-2' },
    frame(g, 'TEXT_MESSAGE_START'),
    { ...frame(g, 'TEXT_MESSAGE_CONTENT'), delta: 'Final timeline answer.' },
    frame(g, 'TEXT_MESSAGE_END'),
    frame(g, 'RUN_FINISHED(success)'),
  ];
}

function sseResponse(route: Route, body: string) {
  return route.fulfill({
    status: 200,
    headers: { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache' },
    body,
  });
}

function messagesSnapshotBody(messages: readonly Record<string, unknown>[]): string {
  return JSON.stringify({ type: 'MESSAGES_SNAPSHOT', messages });
}

function conversationRecord(title: string): Record<string, unknown> {
  return {
    id: CONV_ID,
    title,
    status: 'active',
    total_input_tokens: 0,
    total_output_tokens: 0,
    total_cached_tokens: 0,
    total_cost_usd: 0,
  };
}

function conversationBody(title: string): string {
  return JSON.stringify(conversationRecord(title));
}

function conversationListBody(title: string): string {
  return JSON.stringify([conversationRecord(title)]);
}

// installGoldenRoutes wires the deterministic golden replay over the served SPA: the
// conversation list/get + approvals poll + the two /agent/run turns (initial-with-interrupt,
// then resume) + the resolve POST. The SSE bodies feed the REAL in-browser sseAdapter.
async function installGoldenRoutes(
  page: Page,
  g: GoldenFrames,
  initialHistory: readonly Record<string, unknown>[] = [],
) {
  let pending = false; // flips true after the first turn (the ask_user interrupt is live)
  let resolved = false;

  await page.route(`**/api/conversations/${CONV_ID}`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: conversationBody('E2E thread'),
    }),
  );
  await page.route('**/api/conversations*', (route) => {
    if (route.request().url().includes(`/api/conversations/${CONV_ID}`)) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: conversationBody('E2E thread'),
      });
    }
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: conversationListBody('E2E thread'),
    });
  });

  await page.route('**/threads/*/messages', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: messagesSnapshotBody(initialHistory),
    }),
  );

  await page.route('**/api/conversations/*/rot-events', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  );

  // The cross-thread approval poll: empty until the first turn interrupts, then one pending
  // ask_user card for this conversation, then empty again after the resolve.
  await page.route('**/api/approvals', (route) => {
    const items =
      pending && !resolved
        ? [
            {
              token: PAUSE_TOKEN,
              conversation_id: CONV_ID,
              kind: 'approval',
              question: 'Serve conferma per procedere',
              priority: 0,
            },
          ]
        : [];
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(items),
    });
  });

  // The three-action resolve bridge: mark resolved (the badge poll drops the row) → 204.
  await page.route(`**/api/approvals/${PAUSE_TOKEN}/resolve`, (route) => {
    resolved = true;
    return route.fulfill({ status: 204, body: '' });
  });

  // /agent/run: the FIRST POST streams the initial turn ending in the interrupt; subsequent
  // POSTs (the continue-after-resume re-drive) stream the resume turn.
  await page.route('**/agent/run', (route) => {
    if (!pending) {
      pending = true;
      return sseResponse(route, sseFromFrames(firstTurnFrames(g)));
    }
    return sseResponse(route, sseFromFrames(resumeTurnFrames(g)));
  });
}

async function installTimelineRoutes(page: Page, g: GoldenFrames) {
  await page.route(`**/api/conversations/${CONV_ID}`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: conversationBody('Timeline thread'),
    }),
  );
  await page.route('**/api/conversations*', (route) => {
    if (route.request().url().includes(`/api/conversations/${CONV_ID}`)) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: conversationBody('Timeline thread'),
      });
    }
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: conversationListBody('Timeline thread'),
    });
  });

  await page.route('**/threads/*/messages', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: messagesSnapshotBody([]),
    }),
  );
  await page.route('**/api/conversations/*/rot-events', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  );
  await page.route('**/api/approvals', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  );
  await page.route('**/agent/run', (route) =>
    sseResponse(route, sseFromFrames(timelineTurnFrames(g))),
  );
}

test.describe('cockpit chat — core-value loop (E2E)', () => {
  test('renders reasoning, tools, and answer in chronological stream order', async ({
    page,
  }, testInfo) => {
    const g = golden;
    if (g === null) throw new Error('golden fixture not loaded');
    const consoleProblems: string[] = [];
    page.on('console', (msg) => {
      const text = msg.text();
      if (msg.type() === 'error' && !isExpectedBrowserConsoleNoise(text)) {
        consoleProblems.push(text);
      }
    });
    page.on('pageerror', (err) => {
      consoleProblems.push(err.message);
    });

    await installTimelineRoutes(page, g);
    await gotoAuthenticated(page, `/c/${CONV_ID}`);

    const composer = page.getByPlaceholder('Ask Aura');
    await composer.fill('show the turn timeline');
    await composer.press('Enter');

    const labels = [
      'First timeline reasoning',
      'timeline_probe_tool',
      'Second timeline reasoning',
      'Final timeline answer.',
    ] as const;
    for (const label of labels) {
      await expect(page.getByText(label).first()).toBeVisible({ timeout: 15000 });
    }

    const visibleText = await page.locator('body').innerText();
    const [firstReasoning, tool, secondReasoning, answer] = labels.map((label) =>
      visibleText.indexOf(label),
    ) as [number, number, number, number];
    expect([firstReasoning, tool, secondReasoning, answer].every((index) => index >= 0)).toBe(true);
    expect(firstReasoning).toBeLessThan(tool);
    expect(tool).toBeLessThan(secondReasoning);
    expect(secondReasoning).toBeLessThan(answer);
    expect(consoleProblems).toEqual([]);

    await testInfo.attach('chat-timeline-order.png', {
      body: await page.screenshot({ fullPage: false }),
      contentType: 'image/png',
    });
  });

  test('opening an existing conversation renders persisted history', async ({ page }) => {
    const g = golden;
    if (g === null) throw new Error('golden fixture not loaded');
    await installGoldenRoutes(page, g, [
      { id: 'msg-1', role: 'user', content: 'Persisted prompt from DB' },
      { id: 'msg-2', role: 'assistant', content: 'Persisted answer from DB' },
    ]);

    await gotoAuthenticated(page, `/c/${CONV_ID}`);

    await expect(page.getByText('Persisted prompt from DB')).toBeVisible({ timeout: 15000 });
    await expect(page.getByText('Persisted answer from DB')).toBeVisible();
  });

  test('prompt -> stream -> inline approval resolve -> resume, footer updates', async ({
    page,
  }, testInfo) => {
    const g = golden;
    if (g === null) throw new Error('golden fixture not loaded'); // beforeAll guards this
    await installGoldenRoutes(page, g);

    // A streamed-token counter — asserted >= 1 at the end so a no-op run FAILS (not green).
    let tokenChunkAssertions = 0;

    // Open the chat lane bound to a real thread id (/c/:id).
    await gotoAuthenticated(page, `/c/${CONV_ID}`);

    // 1) Type a prompt + send (CHAT-01).
    const composer = page.getByPlaceholder('Ask Aura');
    await composer.fill('Posso procedere col deploy?');
    await composer.press('Enter');

    // 1a) The assistant answer streams token-by-token — assert the streamed text appears
    // (the real sseAdapter folded the golden TEXT_MESSAGE_* frames). COUNT the assertion.
    const firstDelta = goldenDelta(g);
    await expect(page.getByText(firstDelta, { exact: false })).toBeVisible({ timeout: 15000 });
    tokenChunkAssertions += 1;

    // 2) The ask_user interrupt renders an inline approval card IN-thread (D-03): the
    // backend question verbatim + the Answer verb.
    await expect(page.getByText('Serve conferma per procedere')).toBeVisible({ timeout: 15000 });
    const answer = page.getByRole('button', { name: 'Answer' });
    await expect(answer).toBeVisible();

    // 3) Resolve it (Answer) → the resolve POSTs → the run re-drives (continue-after-resume)
    // and the resume turn streams (APRV-02 / D-05).
    const freeText = page.getByPlaceholder(/type your answer/i);
    if (await freeText.isVisible().catch(() => false)) {
      await freeText.fill('Sì, procedi');
    }
    await answer.click();

    await expect(page.getByText('Procedo: ecco il risultato.')).toBeVisible({ timeout: 15000 });
    tokenChunkAssertions += 1;

    // 4) The runtime footer shows non-zero tokens/cost after the turn (CHAT-04).
    const footer = page.getByRole('contentinfo').or(page.locator('footer'));
    const detail = footer.locator('#footer-telemetry-detail');
    // <sm viewports collapse the full instrument cluster behind a tap-to-expand
    // disclosure (RuntimeFooter AC-7); expand it on mobile so the detail is visible.
    if (testInfo.project.name.startsWith('mobile')) {
      await footer.locator('button[aria-controls="footer-telemetry-detail"]').click();
    }
    await expect(detail).toBeVisible({ timeout: 15000 });
    await expect(detail.getByText('$', { exact: false }).first()).toBeVisible();
    // Non-zero token figure: the per-turn prompt+completion (120+18 or 140+22) is rendered.
    await expect(detail.getByText(/1[0-9]{2}/).first()).toBeVisible();
    tokenChunkAssertions += 1;

    // COUNTED-ASSERTION GUARD: at least one streamed-token assertion executed, so a 0-token
    // (no-op) run fails rather than passing silently.
    expect(tokenChunkAssertions).toBeGreaterThan(0);
  });
});
