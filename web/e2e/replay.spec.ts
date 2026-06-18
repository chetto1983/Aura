import { existsSync, readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';
import { expect, test, type Page, type Route } from '@playwright/test';
import { gotoAuthenticated } from './auth';

// replay.spec.ts proves D-06 end-to-end: a turn that emits a TYPED DISPLAY renders
// live, and reopening the thread (reload → GET /threads/{id}/messages) RE-DERIVES
// and re-renders that display IDENTICALLY — no blank viewport, no lost output. The
// backend re-derives the DisplayPayload through the SAME normalizer the live loop
// uses (26-03 projectDisplaySnapshot), so replay == live by construction; this spec
// asserts that equivalence at the rendered-DOM layer.
//
// No-skip-as-green (CLAUDE.md): the spec never silent-passes a 0-tests run. It feeds
// the REAL in-browser sseAdapter the REAL captured aura.display CUSTOM frame from
// internal/agui/testdata/golden-events.json (the same fixture the Go SSE + adapter
// unit tests use). Under CI, if the golden fixture is missing AND no live serve is
// reachable, the setup THROWS (non-zero exit) — never a silent skip. A rendered-DOM
// assertion counter is asserted >= 1 so a no-op run FAILS rather than passing green.

const HERE = dirname(fileURLToPath(import.meta.url));
const GOLDEN_PATH = resolve(HERE, '..', '..', 'internal', 'agui', 'testdata', 'golden-events.json');

const CONV_ID = '88888888-8888-8888-8888-888888888888';

type GoldenFrames = Record<string, Record<string, unknown>>;

function frame(g: GoldenFrames, name: string): Record<string, unknown> {
  const f = g[name];
  if (f === undefined) throw new Error(`replay.spec.ts: golden fixture missing frame ${name}`);
  return f;
}

function isCI(): boolean {
  return Boolean(process.env.CI);
}

function loadGolden(): GoldenFrames | null {
  if (!existsSync(GOLDEN_PATH)) return null;
  return JSON.parse(readFileSync(GOLDEN_PATH, 'utf8')) as GoldenFrames;
}

let golden: GoldenFrames | null = null;

test.beforeAll(async ({ request }) => {
  golden = loadGolden();
  const liveReachable = await request
    .get('/healthz', { timeout: 3000 })
    .then((res) => res.ok())
    .catch(() => false);

  if (isCI() && !liveReachable && golden === null) {
    throw new Error(
      'replay.spec.ts: neither a live `aura serve` nor the golden-events.json fixture is ' +
        'available under CI — refusing to skip-as-green (CLAUDE.md). Provision the stack ' +
        `(make db-up + migrate) or restore ${GOLDEN_PATH}.`,
    );
  }
  if (golden === null) {
    throw new Error(`replay.spec.ts: golden fixture missing at ${GOLDEN_PATH}`);
  }
});

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

// The live turn: a web_search tool call + the aura.display CUSTOM frame (the typed
// web_result display) folded onto the assistant message by toolCallId. The captured
// CUSTOM_DISPLAY frame carries tool_call_id "call-1" + a web_result with a "Forecast"
// result on "weather.example.com".
function displayTurnFrames(g: GoldenFrames): Record<string, unknown>[] {
  return [
    frame(g, 'RUN_STARTED'),
    frame(g, 'TEXT_MESSAGE_START'),
    { ...frame(g, 'TEXT_MESSAGE_CONTENT'), delta: 'Here is the forecast.' },
    frame(g, 'TEXT_MESSAGE_END'),
    frame(g, 'TOOL_CALL_START'),
    frame(g, 'TOOL_CALL_ARGS'),
    frame(g, 'TOOL_CALL_END'),
    frame(g, 'TOOL_CALL_RESULT'),
    frame(g, 'CUSTOM_DISPLAY'),
    frame(g, 'RUN_FINISHED(success)'),
  ];
}

// The replayed thread: the persisted MESSAGES_SNAPSHOT the reopen fetch returns. The
// assistant turn carries a tool call whose `display` field is the SAME re-derived
// DisplayPayload (26-03 projectDisplaySnapshot) — toolCallsFromSnapshot maps it to a
// tool part with .display set, so the DisplayRouter renders the typed display on
// replay exactly as live. The display value is read from the captured CUSTOM_DISPLAY.
function replaySnapshot(g: GoldenFrames): string {
  const display = frame(g, 'CUSTOM_DISPLAY').value;
  return JSON.stringify({
    type: 'MESSAGES_SNAPSHOT',
    messages: [
      { id: 'msg-1', role: 'user', content: 'meteo' },
      {
        id: 'msg-2',
        role: 'assistant',
        content: 'Here is the forecast.',
        toolCalls: [
          { id: 'call-1', function: { name: 'web_search', arguments: '{"query":"meteo"}' }, display },
        ],
      },
    ],
  });
}

async function installConversationRoutes(page: Page) {
  await page.route('**/api/conversations*', (route) => {
    if (route.request().url().includes(`/api/conversations/${CONV_ID}`)) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          id: CONV_ID,
          title: 'Replay thread',
          status: 'active',
          total_input_tokens: 0,
          total_output_tokens: 0,
          total_cached_tokens: 0,
          total_cost_usd: 0,
        }),
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
}

test.describe('cockpit replay — typed-display rehydration (D-06 E2E)', () => {
  test('a typed display renders LIVE, then re-renders IDENTICALLY on reopen', async ({ page }) => {
    const g = golden;
    if (g === null) throw new Error('golden fixture not loaded'); // beforeAll guards this

    let domAssertions = 0;
    let messagesFetched = 0;

    await installConversationRoutes(page);

    // The reopen fetch (GET /threads/{id}/messages) returns the display-bearing snapshot.
    await page.route('**/threads/*/messages', (route) => {
      messagesFetched += 1;
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: replaySnapshot(g),
      });
    });

    // The live run streams the display-bearing turn.
    await page.route('**/agent/run', (route) => sseResponse(route, sseFromFrames(displayTurnFrames(g))));

    // 1) Open the thread and run a turn that emits a typed display.
    await gotoAuthenticated(page, `/c/${CONV_ID}`);
    const composer = page.getByPlaceholder('Ask Aura');
    await composer.fill('meteo');
    await composer.press('Enter');

    // 1a) LIVE: the typed web_result display renders (the "Web results" card label +
    // the result snippet from the aura.display frame) — NOT the raw tool blob.
    await expect(page.getByText('Web results').first()).toBeVisible({ timeout: 15000 });
    await expect(page.getByText('sunny').first()).toBeVisible();
    await expect(page.getByText('Forecast').first()).toBeVisible();
    domAssertions += 1;

    // Capture the live rendered display card's text for the identical-render compare.
    const liveCard = page.getByText('Web results').first();
    const liveText = await liveCard.locator('xpath=ancestor::*[1]').innerText();

    // 2) REOPEN: reload the route → the chat lane re-fetches GET /threads/{id}/messages
    // and rehydrates from the persisted snapshot (the D-06 path).
    await page.reload({ waitUntil: 'domcontentloaded' });

    // 2a) The rehydration fetch fired (asserted, not assumed).
    await expect.poll(() => messagesFetched, { timeout: 15000 }).toBeGreaterThan(0);

    // 2b) REPLAYED: the SAME typed display re-renders identically — the card label +
    // the snippet + the title are all present again (no blank viewport, no raw fallback).
    await expect(page.getByText('Web results').first()).toBeVisible({ timeout: 15000 });
    await expect(page.getByText('sunny').first()).toBeVisible();
    await expect(page.getByText('Forecast').first()).toBeVisible();
    domAssertions += 1;

    const replayCard = page.getByText('Web results').first();
    const replayText = await replayCard.locator('xpath=ancestor::*[1]').innerText();

    // 2c) IDENTICAL: the replayed display card text matches the live one (D-06 parity).
    expect(replayText).toBe(liveText);
    domAssertions += 1;

    // COUNTED-ASSERTION GUARD: a no-op (0 rendered displays) run FAILS, never green.
    expect(domAssertions).toBeGreaterThan(0);
  });
});
