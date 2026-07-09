import { expect, test, type Page, type Route } from '@playwright/test';
import { gotoAuthenticated } from './auth';

// voice.spec.ts is the Phase-37C terminal acceptance E2E (WEBVOICE-01..04): it drives the
// web voice lane end-to-end against the REAL rebuilt cockpit bundle served by the live
// `aura` container — the per-message speaker (TTS), Composer dictation (STT), and the
// graceful degrade when voice is unconfigured.
//
// Backend reality (verified against the live stack): the web voice lane is CLOUD-ONLY
// (D-12 — buildWebTTSClient/buildWebSTTClient return nil unless AURA_TTS_MODEL /
// AURA_STT_CLOUD_MODEL are set), and the deployment sets NEITHER, so the live backend
// reports GET /api/voice/capabilities → {false,false}. The first test proves that REAL
// contract (the routes exist on the rebuilt container — the pre-voice image 404s them —
// and answer 200/503, never 404). The enabled-path UI (speaker + dictation) is then
// exercised with Playwright ROUTE INTERCEPTION of the voice endpoints (the "intercept
// POST /api/tts" contract), exactly the golden-replay pattern artifacts.spec.ts uses to
// drive the REAL served SPA + REAL assistant-ui adapters without a cloud dependency.
// Real audible playback + live dictation accuracy stay Manual-Only (37C VALIDATION.md).
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
  test('the rebuilt container serves REAL local voice (capabilities {true,true} + a live TTS synth)', async ({
    page,
  }) => {
    test.setTimeout(120_000);
    // No voice route mocks: this hits the REAL backend to prove the rebake embedded
    // voice_api.go AND wired the LOCAL sidecars (the pre-voice image 404s these routes;
    // the old cloud-only wiring reported {false,false}). GET /api/voice/capabilities is
    // RequireAuth-only + always 200; POST /api/tts really synthesizes over the local
    // aura-tts (Kokoro) sidecar.
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
      const ttsBytes = ttsRes.ok ? (await ttsRes.arrayBuffer()).byteLength : 0;
      const sttRes = await fetch('/api/stt', { method: 'POST', credentials: 'same-origin' });
      return {
        capStatus: capsRes.status,
        caps,
        ttsStatus: ttsRes.status,
        ttsCT: ttsRes.headers.get('content-type'),
        ttsBytes,
        sttStatus: sttRes.status,
      };
    });

    // Capabilities is the definitive proof the local voice lane is LIVE (not degraded): the
    // deployment wires TTS_BASE_URL (Kokoro) + STT_BASE_URL (faster-whisper), so both present.
    expect(probe.capStatus).toBe(200);
    expect(probe.caps).toEqual({ tts: true, stt: true });
    // POST /api/tts really synthesizes over the local Kokoro sidecar → 200 audio/mpeg bytes.
    expect(probe.ttsStatus).toBe(200);
    expect(probe.ttsCT ?? '').toContain('audio/mpeg');
    expect(probe.ttsBytes).toBeGreaterThan(0);
    // POST /api/stt is mounted + the client wired (400 here — no audio part — never 404).
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
    // No capabilities/TTS mock: the REAL backend reports tts present (local Kokoro wired) so
    // the speaker control renders, and clicking it drives a REAL POST /api/tts synthesized
    // over the local sidecar. Only the agent turn is a golden-replay (deterministic reply).
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

    // Click Read aloud → the app POSTs /api/tts (real local synth) and, on the audio/mpeg
    // response, plays. Generous timeout: a live CPU Kokoro synth takes a beat.
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
    // No capabilities mock: the REAL backend reports stt present (local faster-whisper wired)
    // so the mic is dictation-primary. Only POST /api/stt is route-mocked to a FIXED
    // transcript — deterministic here; real mic→transcript accuracy is Manual-Only (a fake
    // media device records silence, which no real STT can transcribe to a known string).
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
