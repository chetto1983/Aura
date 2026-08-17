import { expect, test, type Page, type Route } from '@playwright/test';
import { gotoAuthenticated } from './auth';

// composer-skills.spec.ts drives the composer '/' skill-&-command menu end-to-end against the
// REAL rebuilt cockpit bundle served by `aura serve` (the freshly-baked internal/webui/dist
// embed). It proves the full flow — open the '/' menu, filter, select a skill, send — and
// asserts the intercepted POST /agent/run body carries `aura.skill` equal to the selected
// skill name (the WEBSKILL-02 wire proof), plus the one COMMAND in the menu: '/compact' POSTs
// the compaction and draws the in-chat marker, and fires no run.
//
// The pinned-pill assertions went with the pill: a selected skill is written INTO the message
// ('/skill-creator …') by the library's directive formatter, so the transcript shows the turn
// the operator sent. The `/new` and `/clear` quick actions were removed on the operator's
// call — one duplicated the sidebar's new-chat button, the other emptied the box you are
// typing in — so the two tests that drove them are gone rather than skipped.
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

// The stable conversation id the /c/:id route binds against (mocked below).
const CONV_ID = '37d5c0de-0000-4a00-9000-5ec0de5177ee';
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
  compactCount: number;
}

// trackRequests counts the two mutating POSTs at the wire (page.on('request') fires for ALL
// requests, incl. routed ones) WITHOUT touching fulfillment: /agent/run (a send) and
// /compact (the command). runBody captures the last /agent/run body so a test can assert
// `aura.skill` by exact equality. A command that fires a run FAILS its guard: a command is
// not a message, and the whole point of the type split is that it never becomes one.
function trackRequests(page: Page): RunTracker {
  const tracker: RunTracker = { runCount: 0, runBody: null, compactCount: 0 };
  page.on('request', (req) => {
    const url = req.url();
    if (req.method() !== 'POST') return;
    if (url.includes('/agent/run')) {
      tracker.runCount += 1;
      tracker.runBody = req.postData();
    } else if (url.includes('/compact')) {
      tracker.compactCount += 1;
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
  // conversations: a GET {id} → detail; the list → []. The regex excludes deeper sub-paths
  // (/rot-events, /messages, /compaction) via the trailing `$`, so those fall to their own
  // routes below.
  await page.route(/\/api\/conversations(\/[^/?]+)?(\?.*)?$/, (route) => {
    const req = route.request();
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
  // The thread starts uncompacted; the POST is what moves the watermark (and the marker).
  await page.route('**/api/conversations/*/compaction', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ covers_through_seq: 0, source_turns: 0, summary: '' }),
    }),
  );
  await page.route('**/api/conversations/*/compact', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        covers_through_seq: 2,
        source_turns: 2,
        summary: 'The operator asked about skills; Aura listed them.',
        tokens_before: 41000,
        tokens_after: 2600,
      }),
    }),
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

test.describe('cockpit composer / menu — open→filter→select→send carries aura.skill, and /compact compacts (WEBSKILL-01/02/03)', () => {
  test('selecting a skill (click) writes it into the message and the send carries aura.skill === the selected name', async ({
    page,
  }) => {
    test.setTimeout(120_000);
    let domAssertions = 0;
    const tracker = trackRequests(page);
    await installComposerRoutes(page);

    await gotoAuthenticated(page, `/c/${CONV_ID}`);
    const composer = page.getByPlaceholder('Ask Aura');
    await expect(composer).toBeVisible();

    // 1) OPEN: typing '/' at the empty composer opens the listbox with the command first
    // (commands are few and explicit, so they lead a bare '/') and every mocked skill after.
    await composer.fill('/');
    await expect(page.getByRole('listbox')).toBeVisible({ timeout: 10000 });
    await expect(page.getByRole('option', { name: /Compact/ })).toBeVisible();
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

    // 3) SELECT (click): the library's directive formatter writes the skill INTO the message
    // ('/skill-creator ') and closes the menu, so the turn the operator sends is the turn the
    // transcript shows.
    await page.getByRole('option', { name: new RegExp(SKILL_NAME) }).click();
    await expect(composer).toHaveValue(new RegExp(`^/${SKILL_NAME}`));
    await expect(page.getByRole('listbox')).toHaveCount(0);
    domAssertions += 1;

    // 4) SEND: Enter sends and the intercepted /agent/run body carries `aura.skill` === the
    // selected name (WEBSKILL-02).
    await composer.fill(`/${SKILL_NAME} build me a skill`);
    await Promise.all([
      page.waitForRequest((req) => req.url().includes('/agent/run') && req.method() === 'POST'),
      composer.press('Enter'),
    ]);
    await expect.poll(() => tracker.runCount, { timeout: 10000 }).toBe(1);
    const parsed = JSON.parse(tracker.runBody ?? '{}') as { aura?: { skill?: string } };
    expect(parsed.aura?.skill).toBe(SKILL_NAME);
    domAssertions += 1;

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
    await expect(composer).toHaveValue(new RegExp(`^/${SKILL_NAME}`));
    expect(tracker.runCount).toBe(0); // the selecting Enter fired NO run
    domAssertions += 1;

    // Now a full message + Enter (menu closed) SENDS, carrying the named skill on the body.
    await composer.fill(`/${SKILL_NAME} scaffold a new skill`);
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

  test('the /compact command compacts the thread and marks it in the transcript, firing no run', async ({
    page,
  }) => {
    test.setTimeout(120_000);
    let domAssertions = 0;
    const tracker = trackRequests(page);
    await installComposerRoutes(page);
    // A two-turn transcript, so the marker has a message to sit under: the snapshot ids
    // (msg-1, msg-2) are what the client reads back as backendSeq, and the mocked POST
    // answers covers_through_seq=2 — the second turn.
    await page.route('**/threads/*/messages', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          type: 'MESSAGES_SNAPSHOT',
          messages: [
            { id: 'msg-1', role: 'user', content: 'quali skill hai' },
            { id: 'msg-2', role: 'assistant', content: 'skill-creator e pdf-extract' },
          ],
        }),
      }),
    );

    await gotoAuthenticated(page, `/c/${CONV_ID}`);
    const composer = page.getByPlaceholder('Ask Aura');
    await expect(composer).toBeVisible();
    await expect(page.getByText('skill-creator e pdf-extract')).toBeVisible({ timeout: 10000 });
    // Nothing is compacted yet, so there is no marker to see.
    await expect(page.getByTestId('compaction-marker')).toHaveCount(0);
    domAssertions += 1;

    // '/compact' filters to the command (no skill's name or description contains it).
    await composer.fill('/compact');
    await expect(page.getByRole('listbox')).toBeVisible({ timeout: 10000 });
    const compact = page.getByRole('option', { name: /Compact/ });
    await expect(compact).toBeVisible();
    await expect(page.getByRole('listbox').getByRole('option')).toHaveCount(1);
    domAssertions += 1;

    // Picking it POSTs the compaction and clears the composer — a command is a verb the
    // composer performs, not a message, so the text it was invoked with does not survive.
    await Promise.all([
      page.waitForRequest((req) => req.url().includes('/compact') && req.method() === 'POST'),
      compact.click(),
    ]);
    await expect.poll(() => tracker.compactCount, { timeout: 10000 }).toBe(1);
    await expect(composer).toHaveValue('');
    domAssertions += 1;

    // The marker lands in the transcript, carrying what the compaction did, and expands to
    // the summary the model will now replay in place of those turns.
    const marker = page.getByTestId('compaction-marker');
    await expect(marker).toBeVisible({ timeout: 10000 });
    await expect(marker).toContainText('Context compacted');
    await expect(marker).toContainText('2 earlier turns');
    domAssertions += 1;

    await marker.getByRole('button').click();
    await expect(marker).toContainText('The operator asked about skills');
    domAssertions += 1;

    // A command never becomes a turn.
    expect(tracker.runCount).toBe(0);
    domAssertions += 1;

    expect(domAssertions).toBeGreaterThanOrEqual(6);
  });
});
