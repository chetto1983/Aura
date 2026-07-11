import { expect, test, type Page, type Route } from '@playwright/test';
import { gotoAuthenticated } from './auth';

// composer-skills.spec.ts is the Phase-37D terminal acceptance E2E (WEBSKILL-01/02/03): it
// drives the composer '/' skill-&-command picker end-to-end against the REAL rebuilt cockpit
// bundle served by `aura serve` (the freshly-baked internal/webui/dist embed). It proves the
// full flow — open the '/' menu, filter, select a skill, see the removable pill, send — and
// asserts the intercepted POST /agent/run body carries `aura.skill` equal to the selected
// skill name (the WEBSKILL-02 wire proof), plus the two pure-client quick actions (new-chat
// starts a fresh conversation; clear resets the composer input + pinned pill).
//
// Golden-replay, NOT a live agent turn (plan prohibition): GET /api/composer/skills and the
// /agent/run SSE are BOTH mocked at the page-network layer (mirroring artifacts.spec.ts /
// replay.spec.ts `sseFromFrames`), so the spec exercises the REAL in-browser picker +
// sseAdapter + run-envelope fold without any backend agent loop. The skills list, the
// conversation/asset fetches, and the run stream are all deterministic route fixtures.
//
// No-skip-as-green (CLAUDE.md): every test asserts a COUNTED number of DOM/route facts
// (`domAssertions` guarded `> 0`) AND the run-body interceptor asserts `aura.skill` by exact
// equality, so a no-op run FAILS rather than passing green. The auth + serve harness is the
// same live gate the sibling e2e specs use (playwright.config webServer → `aura serve`); when
// neither a live serve nor the Authula stack is reachable the shared auth helper THROWS. Runs
// on chromium (desktop) + mobile-chrome (Pixel 5) so the picker holds on a touch viewport too.

// Stable conversation ids the /c/:id route binds against (mocked below). NEW_CONV_ID is the
// conversation the new-chat quick action creates + navigates to.
const CONV_ID = '37d5c0de-0000-4a00-9000-5ec0de5177ee';
const NEW_CONV_ID = '37d5c0de-1111-4a00-9000-5ec0de5177ee';
const SKILL_NAME = 'skill-creator';

// The mocked GET /api/composer/skills snapshot (37D-02 `{name,description,type}` projection).
// Two skills with disjoint names/descriptions so a '/creator' filter narrows to EXACTLY the
// one skill (no command matches 'creator', no other skill's name/description contains it).
function composerSkillsBody(): string {
  return JSON.stringify({
    skills: [
      { name: SKILL_NAME, description: 'Scaffold and evaluate Aura skills', type: 'instruction' },
      { name: 'pdf-extract', description: 'Extract text and tables from PDFs', type: 'executable' },
    ],
  });
}

// A minimal AG-UI assistant turn (captured translator shapes, per the artifacts.spec sanctioned
// approach): a short assistant text answer the REAL sseAdapter folds onto one message. The turn
// itself carries no skill — the skill rides the run-request body (asserted via the interceptor).
function assistantTurnFrames(): Record<string, unknown>[] {
  return [
    { type: 'RUN_STARTED', threadId: CONV_ID, runId: 'run-skill' },
    { type: 'TEXT_MESSAGE_START', messageId: 'msg-skill' },
    { type: 'TEXT_MESSAGE_CONTENT', messageId: 'msg-skill', delta: 'On it.' },
    { type: 'TEXT_MESSAGE_END', messageId: 'msg-skill' },
    { type: 'RUN_FINISHED', threadId: CONV_ID, runId: 'run-skill', outcome: { type: 'success' } },
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

// The single-conversation detail projection (lowercase {id,title,status,totals}) the chat lane
// reads for the footer; mirrors artifacts.spec.ts installConversationRoutes.
function conversationDetail(id: string): string {
  return JSON.stringify({
    id,
    title: 'Skills thread',
    status: 'active',
    total_input_tokens: 0,
    total_output_tokens: 0,
    total_cached_tokens: 0,
    total_cost_usd: 0,
  });
}

interface RunTracker {
  runCount: number;
  runBody: string | null;
  createCount: number;
}

// trackRequests counts the two mutating POSTs at the wire (page.on('request') fires for ALL
// requests, incl. routed ones) WITHOUT touching fulfillment: /agent/run (a send) and the
// /api/conversations create (new-chat). runBody captures the last /agent/run body so a test can
// assert `aura.skill` by exact equality. A quick action that fires either POST FAILS its guard.
function trackRequests(page: Page): RunTracker {
  const tracker: RunTracker = { runCount: 0, runBody: null, createCount: 0 };
  page.on('request', (req) => {
    const url = req.url();
    if (req.method() !== 'POST') return;
    if (url.includes('/agent/run')) {
      tracker.runCount += 1;
      tracker.runBody = req.postData();
    } else if (/\/api\/conversations(\?.*)?$/.test(url)) {
      tracker.createCount += 1;
    }
  });
  return tracker;
}

// installComposerRoutes mocks every network dependency of the chat lane so it mounts
// deterministically against CONV_ID (and NEW_CONV_ID after new-chat), with the skills list +
// /agent/run SSE served from fixtures — the golden-replay layer.
async function installComposerRoutes(page: Page) {
  await page.route('**/api/composer/skills', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: composerSkillsBody() }),
  );
  // conversations: POST create → the new conversation (the store projection reads `ID`); a
  // GET {id} → detail; the list → []. The regex excludes deeper sub-paths (/rot-events,
  // /messages) via the trailing `$`, so those fall to their own routes below.
  await page.route(/\/api\/conversations(\/[^/?]+)?(\?.*)?$/, (route) => {
    const req = route.request();
    if (req.method() === 'POST') {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ ID: NEW_CONV_ID, Title: '', TitleSet: false }),
      });
    }
    const detailId = /\/api\/conversations\/([^/?]+)/.exec(req.url())?.[1];
    if (detailId !== undefined) {
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

test.describe('cockpit composer skill-picker — open→filter→select→pill→send carries aura.skill + new-chat/clear (WEBSKILL-01/02/03)', () => {
  test('selecting a skill (click) pins the removable pill and the send carries aura.skill === the selected name', async ({
    page,
  }) => {
    test.setTimeout(120_000);
    let domAssertions = 0;
    const tracker = trackRequests(page);
    await installComposerRoutes(page);

    await gotoAuthenticated(page, `/c/${CONV_ID}`);
    const composer = page.getByPlaceholder('Ask Aura');
    await expect(composer).toBeVisible();

    // 1) OPEN: typing '/' at the empty composer opens the APG listbox with BOTH groups
    // (Quick commands + Skills) and every mocked skill.
    await composer.fill('/');
    await expect(page.getByRole('listbox')).toBeVisible({ timeout: 10000 });
    await expect(page.getByRole('group', { name: 'Quick commands' })).toBeVisible();
    await expect(page.getByRole('group', { name: 'Skills' })).toBeVisible();
    await expect(page.getByRole('option', { name: new RegExp(SKILL_NAME) })).toBeVisible();
    await expect(page.getByRole('option', { name: /pdf-extract/ })).toBeVisible();
    domAssertions += 1;

    // 2) FILTER: '/creator' narrows to EXACTLY the one matching skill — no command matches
    // 'creator', and pdf-extract's name/description does not contain it. Scope the count to
    // the skill-picker listbox: the 37E reasoning-effort control is a sibling native <select>
    // whose <option> children also expose role="option", so a page-wide getByRole('option')
    // would additionally count the effort levels (auto/…) and inflate the count.
    await composer.fill('/creator');
    await expect(page.getByRole('listbox').getByRole('option')).toHaveCount(1);
    await expect(page.getByRole('option', { name: new RegExp(SKILL_NAME) })).toBeVisible();
    await expect(page.getByRole('option', { name: /pdf-extract/ })).toHaveCount(0);
    domAssertions += 1;

    // 3) SELECT (click): picking the skill pins the removable SkillPill and clears the
    // '/'-filter text (the pinned skill rides the pill, not the text), closing the menu.
    await page.getByRole('option', { name: new RegExp(SKILL_NAME) }).click();
    await expect(
      page.getByRole('button', { name: `Remove pinned skill ${SKILL_NAME}` }),
    ).toBeVisible();
    await expect(composer).toHaveValue('');
    await expect(page.getByRole('listbox')).toHaveCount(0);
    domAssertions += 1;

    // 4) SEND: a plain message (no leading '/') keeps the menu closed; Enter sends and the
    // intercepted /agent/run body carries `aura.skill` === the selected name (WEBSKILL-02).
    await composer.fill('build me a skill');
    await expect(page.getByRole('listbox')).toHaveCount(0);
    await Promise.all([
      page.waitForRequest((req) => req.url().includes('/agent/run') && req.method() === 'POST'),
      composer.press('Enter'),
    ]);
    await expect.poll(() => tracker.runCount, { timeout: 10000 }).toBe(1);
    const parsed = JSON.parse(tracker.runBody ?? '{}') as { aura?: { skill?: string } };
    expect(parsed.aura?.skill).toBe(SKILL_NAME);
    domAssertions += 1;

    // 5) The pinned skill applies to exactly one turn — the pill clears after a successful send.
    await expect(
      page.getByRole('button', { name: `Remove pinned skill ${SKILL_NAME}` }),
    ).toHaveCount(0);

    // COUNTED-ASSERTION GUARD: a no-op (menu never opened / body never carried the skill) FAILS.
    expect(domAssertions).toBeGreaterThanOrEqual(4);
  });

  test('the picker Enter selects the active option WITHOUT sending; a closed-menu Enter sends and carries aura.skill', async ({
    page,
  }) => {
    test.setTimeout(120_000);
    let domAssertions = 0;
    const tracker = trackRequests(page);
    await installComposerRoutes(page);

    await gotoAuthenticated(page, `/c/${CONV_ID}`);
    const composer = page.getByPlaceholder('Ask Aura');
    await expect(composer).toBeVisible();

    // Open + filter to the single skill, then press Enter WHILE the menu is open: it SELECTS
    // (pins the pill) and MUST NOT send — the D-09/T-37D-08 discipline (keys intercepted only
    // while open; Enter-send stays the library's when closed).
    await composer.fill(`/${SKILL_NAME.slice(0, 5)}`); // '/skill' → matches skill-creator only
    await expect(page.getByRole('listbox')).toBeVisible({ timeout: 10000 });
    await composer.press('Enter');
    await expect(
      page.getByRole('button', { name: `Remove pinned skill ${SKILL_NAME}` }),
    ).toBeVisible();
    await expect(composer).toHaveValue('');
    expect(tracker.runCount).toBe(0); // the selecting Enter fired NO run
    domAssertions += 1;

    // Now a plain message + Enter (menu closed) SENDS, carrying the pinned skill on the body.
    await composer.fill('scaffold a new skill');
    await Promise.all([
      page.waitForRequest((req) => req.url().includes('/agent/run') && req.method() === 'POST'),
      composer.press('Enter'),
    ]);
    await expect.poll(() => tracker.runCount, { timeout: 10000 }).toBe(1);
    const parsed = JSON.parse(tracker.runBody ?? '{}') as { aura?: { skill?: string } };
    expect(parsed.aura?.skill).toBe(SKILL_NAME);
    domAssertions += 1;

    expect(domAssertions).toBeGreaterThanOrEqual(2);
  });

  test('the new-chat quick action starts a fresh conversation (POST + route change) and fires no /agent/run', async ({
    page,
  }) => {
    test.setTimeout(120_000);
    let domAssertions = 0;
    const tracker = trackRequests(page);
    await installComposerRoutes(page);

    await gotoAuthenticated(page, `/c/${CONV_ID}`);
    const composer = page.getByPlaceholder('Ask Aura');
    await expect(composer).toBeVisible();

    // '/new' filters to the New chat command (no skill's name/description contains 'new').
    await composer.fill('/new');
    await expect(page.getByRole('listbox')).toBeVisible({ timeout: 10000 });
    const newChat = page.getByRole('option', { name: /New chat/ });
    await expect(newChat).toBeVisible();
    domAssertions += 1;

    // Picking New chat POSTs /api/conversations (create) and navigates to /c/{new id} — a pure
    // client action: no /agent/run fires for it.
    await newChat.click();
    await expect.poll(() => tracker.createCount, { timeout: 10000 }).toBe(1);
    await expect(page).toHaveURL(new RegExp(`/c/${NEW_CONV_ID}`), { timeout: 10000 });
    domAssertions += 1;
    expect(tracker.runCount).toBe(0);
    domAssertions += 1;

    expect(domAssertions).toBeGreaterThanOrEqual(3);
  });

  test('the clear quick action resets the composer input and removes the pinned skill, firing no /agent/run', async ({
    page,
  }) => {
    test.setTimeout(120_000);
    let domAssertions = 0;
    const tracker = trackRequests(page);
    await installComposerRoutes(page);

    await gotoAuthenticated(page, `/c/${CONV_ID}`);
    const composer = page.getByPlaceholder('Ask Aura');
    await expect(composer).toBeVisible();

    // First pin a skill so the pill is present.
    await composer.fill('/creator');
    await page.getByRole('option', { name: new RegExp(SKILL_NAME) }).click();
    const pillRemove = page.getByRole('button', { name: `Remove pinned skill ${SKILL_NAME}` });
    await expect(pillRemove).toBeVisible();
    domAssertions += 1;

    // Re-open the menu with '/clear' (the pinned pill stays), then pick Clear: it empties the
    // composer input AND unpins the skill — a pure client reset, no /agent/run.
    await composer.fill('/clear');
    const clear = page.getByRole('option', { name: /Clear/ });
    await expect(clear).toBeVisible({ timeout: 10000 });
    await expect(pillRemove).toBeVisible();
    await clear.click();
    await expect(composer).toHaveValue('');
    await expect(pillRemove).toHaveCount(0);
    domAssertions += 1;
    expect(tracker.runCount).toBe(0);
    domAssertions += 1;

    expect(domAssertions).toBeGreaterThanOrEqual(3);
  });
});
