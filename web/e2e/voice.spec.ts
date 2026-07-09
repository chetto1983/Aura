import { expect, test, type Page, type Route } from '@playwright/test';
import { gotoAuthenticated } from './auth';

// voice.spec.ts is the Phase-37C terminal acceptance E2E (WEBVOICE-01..04): it drives the
// web voice lane end-to-end against the REAL rebuilt cockpit bundle served by the live
// `aura` container — the per-message speaker (TTS), Composer dictation (STT), and the
// graceful degrade when voice is unconfigured.
//
// Backend reality: the web voice lane is local↔cloud SELECTABLE (buildWebTTSClient /
// buildWebSTTClient default to the local aura-tts/aura-stt sidecars and switch to cloud
// when AURA_TTS_MODEL / AURA_STT_CLOUD_MODEL is set; nil only when NEITHER is configured).
// So GET /api/voice/capabilities reflects WHATEVER backend the runner wires: a dev host
// with the local sidecars up reports {true,true}, while CI (which boots `aura serve`
// WITHOUT the voice sidecars) reports {false,false}. The E2E is therefore backend-
// INDEPENDENT: the first test proves only that the routes are MOUNTED on the freshly-built
// binary (the pre-voice binary 404s them — a boolean {tts,stt} shape + a non-404
// /api/tts+/api/stt), and the enabled-path UI (speaker + dictation) is exercised with
// Playwright ROUTE INTERCEPTION of the voice endpoints (capabilities {true,true} + mocked
// /api/tts + /api/stt) — the golden-replay pattern artifacts.spec.ts uses to drive the REAL
// served SPA + REAL assistant-ui adapters deterministically, without depending on a real
// voice backend. Real audible playback + live dictation accuracy stay Manual-Only (37C
// VALIDATION.md).
//
// No-skip-as-green (CLAUDE.md): every test drives a real navigation + real auth against
// the live container and guards a COUNTED number of DOM/route facts (a no-op run FAILS,
// not passes). The shared auth helper THROWS when neither a live serve nor the Authula
// stack is reachable — a sub-second green is impossible here.

// Fake media so getUserMedia + MediaRecorder resolve headless for the dictation test;
// harmless to the other tests. Applied file-wide (chromium + mobile-chrome are both
// Chromium, which honours these flags).
test.use({
  launchOptions: {
    args: [
      '--use-fake-device-for-media-stream',
      '--use-fake-ui-for-media-stream',
      '--autoplay-policy=no-user-gesture-required',
    ],
  },
});

// A stable conversation id the /c/:id route binds the chat lane against (mocked below).
const CONV_ID = '5c0ffee5-0000-4a00-9000-5ec0de5177ee';
const REPLY_TEXT = 'Voice acceptance gate reply.';
const TRANSCRIPT = 'voice acceptance gate transcript';

// isExpectedBrowserConsoleNoise mirrors chat.spec.ts: the served shell emits a couple of
// benign console lines (a viewport meta warning, an /api/auth/config access-control note)
// that are not voice regressions and must not trip the degrade test's no-error assertion.
function isExpectedBrowserConsoleNoise(text: string): boolean {
  return (
    text === 'Viewport argument key "interactive-widget" not recognized and ignored.' ||
    text.includes('/api/auth/config due to access control checks.')
  );
}

// assistantTurnFrames is a minimal AG-UI turn (captured translator shapes, per the
// artifacts.spec sanctioned approach): a short assistant text answer. The REAL sseAdapter
// reducer folds it onto one assistant ThreadMessageLike, whose ActionBar then carries the
// caps-gated speaker control.
function assistantTurnFrames(): Record<string, unknown>[] {
  return [
    { type: 'RUN_STARTED', threadId: CONV_ID, runId: 'run-voice' },
    { type: 'TEXT_MESSAGE_START', messageId: 'msg-voice' },
    { type: 'TEXT_MESSAGE_CONTENT', messageId: 'msg-voice', delta: REPLY_TEXT },
    { type: 'TEXT_MESSAGE_END', messageId: 'msg-voice' },
    { type: 'RUN_FINISHED', threadId: CONV_ID, runId: 'run-voice', outcome: { type: 'success' } },
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

// installConversationRoutes mirrors artifacts.spec.ts: a clean empty thread so the chat
// lane mounts deterministically (no real conversation/asset/approval fetch interferes).
async function installConversationRoutes(page: Page) {
  // Match BOTH the list (/api/conversations) and the id (/api/conversations/{id}) — a glob
  // `*` does not cross `/`, so the id path would otherwise fall through to the live backend
  // and 404 (which trips the degrade test's no-console-errors guard). Deeper sub-paths
  // (/rot-events, /messages) have their own routes below and are excluded by the trailing $.
  await page.route(/\/api\/conversations(\/[^/?]+)?(\?.*)?$/, (route) => {
    if (route.request().url().includes(`/api/conversations/${CONV_ID}`)) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          id: CONV_ID,
          title: 'Voice thread',
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
}

test.describe('cockpit voice lane — speaker + dictation + degrade against the live container (WEBVOICE-01..04)', () => {
  test('the rebuilt binary MOUNTS the voice routes (capabilities 200 + /api/tts + /api/stt not 404) — the pre-voice binary 404s them', async ({
    page,
  }) => {
    test.setTimeout(120_000);
    // Backend-INDEPENDENT proof the build embedded voice_api.go: the three routes are
    // MOUNTED (the pre-voice binary 404s them). We assert the CONTRACT SHAPE, not a specific
    // backend state — capabilities reflects whatever voice backend the runner wires (a dev
    // host with the local aura-tts/aura-stt sidecars reports {true,true}; CI, which boots
    // `aura serve` without them, reports {false,false}). Real synth is Manual-Only.
    await gotoAuthenticated(page, '/');

    const probe = await page.evaluate(async () => {
      const capsRes = await fetch('/api/voice/capabilities', {
        headers: { Accept: 'application/json' },
        credentials: 'same-origin',
      });
      const caps = (await capsRes.json().catch(() => null)) as {
        tts?: unknown;
        stt?: unknown;
      } | null;
      const ttsRes = await fetch('/api/tts', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Accept: 'audio/mpeg' },
        credentials: 'same-origin',
        body: JSON.stringify({ text: 'Aura voice acceptance probe.' }),
      });
      const sttRes = await fetch('/api/stt', { method: 'POST', credentials: 'same-origin' });
      return {
        capStatus: capsRes.status,
        caps,
        ttsStatus: ttsRes.status,
        sttStatus: sttRes.status,
      };
    });

    // GET /api/voice/capabilities is RequireAuth-only + always 200 (a probe, never 404/503),
    // returning a boolean {tts,stt} shape whose values mirror whichever backend is wired.
    expect(probe.capStatus).toBe(200);
    expect(typeof probe.caps?.tts).toBe('boolean');
    expect(typeof probe.caps?.stt).toBe('boolean');
    // POST /api/tts + /api/stt are MOUNTED (the pre-voice binary 404s them). They answer
    // 200 (backend wired) / 400 (bad input) / 503 (unconfigured) — never 404 or 405.
    expect(probe.ttsStatus).not.toBe(404);
    expect(probe.ttsStatus).not.toBe(405);
    expect(probe.sttStatus).not.toBe(404);
    expect(probe.sttStatus).not.toBe(405);
  });

  test('speaker: clicking the message speaker POSTs /api/tts (audio/mpeg) and enters the speaking state', async ({
    page,
  }) => {
    test.setTimeout(120_000);
    let assertions = 0;

    // Stub the Audio element so the speaking state is deterministic headless (real audible
    // playback is Manual-Only): play() resolves and the utterance stays "running" (never
    // fires onended), so the ActionBar swaps to Stop reading and holds for the assertion.
    await page.addInitScript(() => {
      class FakeAudio {
        public onended: (() => void) | null = null;
        public onerror: (() => void) | null = null;
        public src: string;
        constructor(src?: string) {
          this.src = src ?? '';
          const w = window as unknown as { __auraAudioPlays?: number };
          w.__auraAudioPlays = (w.__auraAudioPlays ?? 0) + 1;
        }
        play(): Promise<void> {
          return Promise.resolve();
        }
        pause(): void {
          // no-op: headless has no audio sink; cancel() only needs pause() to exist.
        }
      }
      (window as unknown as { Audio: unknown }).Audio = FakeAudio;
    });

    await installConversationRoutes(page);
    // Route-mock the voice backend so the UI contract is deterministic in BOTH environments
    // (CI boots `aura serve` with NO voice sidecars → {false,false}; a dev host has them →
    // {true,true}). capabilities {tts:true} renders the speaker control; POST /api/tts returns
    // a small audio/mpeg blob the stubbed Audio "plays". Real synth is Manual-Only.
    await page.route('**/api/voice/capabilities', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ tts: true, stt: true }),
      }),
    );
    await page.route('**/api/tts', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'audio/mpeg',
        body: Buffer.from([0xff, 0xf3, 0x00, 0x00]),
      }),
    );
    await page.route('**/agent/run', (route) =>
      sseResponse(route, sseFromFrames(assistantTurnFrames())),
    );

    await gotoAuthenticated(page, `/c/${CONV_ID}`);
    const composer = page.getByPlaceholder('Ask Aura');
    await composer.fill('read this back to me');
    await composer.press('Enter');

    // The assistant reply renders; its ActionBar carries the caps.tts Read-aloud control.
    await expect(page.getByText(REPLY_TEXT).first()).toBeVisible({ timeout: 15000 });
    const speak = page.getByRole('button', { name: 'Read aloud' });
    await expect(speak).toBeVisible({ timeout: 10000 });
    assertions += 1;

    // Click Read aloud → the app POSTs /api/tts and, on the mocked audio/mpeg response, plays.
    const [ttsResponse] = await Promise.all([
      page.waitForResponse(
        (res) => res.url().includes('/api/tts') && res.request().method() === 'POST',
        { timeout: 60000 },
      ),
      speak.click(),
    ]);
    expect(ttsResponse.status()).toBe(200);
    expect(ttsResponse.headers()['content-type'] ?? '').toContain('audio/mpeg');
    assertions += 1;

    // Speaking state: the control swaps Read aloud → Stop reading; playback was initiated.
    await expect(page.getByRole('button', { name: 'Stop reading' })).toBeVisible({
      timeout: 10000,
    });
    assertions += 1;
    const plays = await page.evaluate(
      () => (window as unknown as { __auraAudioPlays?: number }).__auraAudioPlays ?? 0,
    );
    expect(plays).toBeGreaterThan(0);

    // COUNTED-ASSERTION GUARD: a no-op (control never rendered / never POSTed) run FAILS.
    expect(assertions).toBeGreaterThanOrEqual(3);
  });

  test('dictation: the mic inserts an editable transcript into the composer, then Send works', async ({
    page,
    context,
  }) => {
    test.setTimeout(120_000);
    let assertions = 0;
    await context.grantPermissions(['microphone']);

    await installConversationRoutes(page);
    // Route-mock the voice backend deterministically (CI boots `aura serve` with NO local
    // sidecars → {false,false}; a dev host has them → {true,true}). capabilities {stt:true}
    // makes the mic dictation-primary; POST /api/stt returns a FIXED transcript. Real
    // mic→transcript accuracy is Manual-Only (a fake media device records silence, which no
    // real STT can transcribe to a known string).
    await page.route('**/api/voice/capabilities', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ tts: true, stt: true }),
      }),
    );
    await page.route('**/api/stt', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ text: TRANSCRIPT }),
      }),
    );
    await page.route('**/agent/run', (route) =>
      sseResponse(route, sseFromFrames(assistantTurnFrames())),
    );

    await gotoAuthenticated(page, `/c/${CONV_ID}`);
    const composer = page.getByPlaceholder('Ask Aura');
    await expect(composer).toBeVisible();

    // caps.stt=true → the mic renders as "Dictate", not the "Record audio" attachment path.
    const dictate = page.getByRole('button', { name: 'Dictate' });
    await expect(dictate).toBeVisible({ timeout: 10000 });
    await expect(page.getByRole('button', { name: 'Record audio' })).toHaveCount(0);
    assertions += 1;

    // Start dictation → the mic swaps to "Stop dictation" while the MediaRecorder listens.
    await dictate.click();
    const stopDictation = page.getByRole('button', { name: 'Stop dictation' });
    await expect(stopDictation).toBeVisible({ timeout: 10000 });
    assertions += 1;

    // Record a moment of (fake) audio, then stop → the adapter POSTs /api/stt and inserts.
    // The wait also guarantees getUserMedia + MediaRecorder.start have settled before stop
    // (a stop before the fake mic opens would no-op the recorder under a starved event loop).
    await page.waitForTimeout(1500);
    const [sttResponse] = await Promise.all([
      page.waitForResponse(
        (res) => res.url().includes('/api/stt') && res.request().method() === 'POST',
      ),
      stopDictation.click(),
    ]);
    expect(sttResponse.status()).toBe(200);

    // The transcript lands in the composer INPUT (editable), not a chat bubble.
    await expect
      .poll(async () => (await composer.inputValue()).toLowerCase(), { timeout: 10000 })
      .toContain(TRANSCRIPT);
    assertions += 1;

    // Editable: extend the transcript in place (fill throws on a disabled/readonly input).
    const dictated = await composer.inputValue();
    await composer.fill(`${dictated} and send it`);
    await expect(composer).toHaveValue(/and send it/i);
    assertions += 1;

    // Send works end-to-end: Enter drives a real POST /agent/run and the dictated user turn
    // renders in-thread.
    const [runRequest] = await Promise.all([
      page.waitForRequest((req) => req.url().includes('/agent/run') && req.method() === 'POST'),
      composer.press('Enter'),
    ]);
    expect(runRequest.method()).toBe('POST');
    await expect(page.getByText(new RegExp(TRANSCRIPT, 'i')).first()).toBeVisible({
      timeout: 15000,
    });
    assertions += 1;

    expect(assertions).toBeGreaterThanOrEqual(5);
  });

  test('degrade: capabilities {false,false} hides the speaker, keeps the mic in attachment mode, no console errors', async ({
    page,
  }) => {
    test.setTimeout(120_000);
    let assertions = 0;
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

    await installConversationRoutes(page);
    // ROUTE-MOCK the capabilities probe to {false,false} — the real deployment default
    // (cloud voice unset), made explicit + deterministic here.
    await page.route('**/api/voice/capabilities', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ tts: false, stt: false }),
      }),
    );
    await page.route('**/agent/run', (route) =>
      sseResponse(route, sseFromFrames(assistantTurnFrames())),
    );

    await gotoAuthenticated(page, `/c/${CONV_ID}`);
    const composer = page.getByPlaceholder('Ask Aura');
    await composer.fill('no voice configured here');
    await composer.press('Enter');
    await expect(page.getByText(REPLY_TEXT).first()).toBeVisible({ timeout: 15000 });

    // The SPA reads {false,false} from the probe (page.evaluate fetch, per the plan).
    const caps = await page.evaluate(async () => {
      const res = await fetch('/api/voice/capabilities', {
        headers: { Accept: 'application/json' },
        credentials: 'same-origin',
      });
      return { status: res.status, body: (await res.json()) as { tts: boolean; stt: boolean } };
    });
    expect(caps.status).toBe(200);
    expect(caps.body).toEqual({ tts: false, stt: false });
    assertions += 1;

    // The assistant ActionBar rendered (Regenerate is assistant-only) so the SPEAKER's
    // absence is a real degrade, not just "no message".
    await expect(page.getByRole('button', { name: 'Regenerate' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Read aloud' })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Stop reading' })).toHaveCount(0);
    assertions += 1;

    // The mic stays in attachment-record mode ("Record audio"), NOT dictation ("Dictate").
    await expect(page.getByRole('button', { name: 'Record audio' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Dictate' })).toHaveCount(0);
    assertions += 1;

    // No console errors leaked while degrading (WEBVOICE-03/04).
    expect(consoleProblems).toEqual([]);
    assertions += 1;

    expect(assertions).toBeGreaterThanOrEqual(4);
  });
});
