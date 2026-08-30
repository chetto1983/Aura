import { expect, test, type Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';

const runLive = process.env.AURA_E2E_LIVE_MODEL_ROUTES === '1';
const ollamaBaseURL =
  process.env.AURA_E2E_OLLAMA_BASE_URL ?? 'http://host.docker.internal:11434/v1';
const witnessURL = process.env.AURA_E2E_OLLAMA_WITNESS_URL;

interface FetchResult {
  readonly status: number;
  readonly text: string;
}

interface ConversationRow {
  readonly ID: string;
}

interface RouteProfile {
  readonly provider: string;
  readonly baseURL: string;
  readonly model: string;
}

interface RunEvidence {
  readonly frames: readonly Record<string, unknown>[];
  readonly text: string;
}

interface RuntimeMetadata {
  readonly capabilities: {
    readonly backend: string;
    readonly default: string;
    readonly detected: boolean;
    readonly levels: readonly string[];
  };
  readonly contextWindow: number;
}

const localProfile: RouteProfile = {
  provider: 'llamacpp',
  baseURL: 'http://aura-llm:8084/v1',
  model: 'gemma-4-12b',
};

const ollamaProfile: RouteProfile = {
  provider: 'ollama',
  baseURL: ollamaBaseURL,
  model: 'gemma4:31b-cloud',
};

async function sameOriginFetch(
  page: Page,
  url: string,
  init: { readonly method?: string; readonly body?: string } = {},
): Promise<FetchResult> {
  return page.evaluate(
    async ({ requestURL, requestInit }) => {
      const options: RequestInit = { credentials: 'same-origin' };
      if (requestInit.method !== undefined) options.method = requestInit.method;
      if (requestInit.body !== undefined) {
        options.body = requestInit.body;
        options.headers = { 'Content-Type': 'application/json' };
      }
      const response = await fetch(requestURL, options);
      return { status: response.status, text: await response.text() };
    },
    { requestURL: url, requestInit: init },
  );
}

async function putProfile(page: Page, profile: RouteProfile): Promise<void> {
  const response = await sameOriginFetch(page, '/api/settings/llm-profile', {
    method: 'PUT',
    body: JSON.stringify({
      settings: {
        AURA_LLM_PROVIDER: profile.provider,
        AURA_LLM_BASE_URL: profile.baseURL,
        AURA_LLM_MODEL: profile.model,
      },
    }),
  });
  expect(response.status, response.text).toBe(200);
  expect(JSON.parse(response.text)).toMatchObject({ updated: 3, restart_required: false });
}

async function readRuntimeMetadata(page: Page): Promise<RuntimeMetadata | null> {
  const [meResponse, capabilitiesResponse] = await Promise.all([
    sameOriginFetch(page, '/api/me'),
    sameOriginFetch(page, '/api/composer/reasoning-capabilities'),
  ]);
  if (meResponse.status !== 200 || capabilitiesResponse.status !== 200) return null;

  const me = JSON.parse(meResponse.text) as { readonly context_window?: number };
  const capabilities = JSON.parse(capabilitiesResponse.text) as RuntimeMetadata['capabilities'];
  return {
    capabilities,
    contextWindow: me.context_window ?? 0,
  };
}

async function expectOllamaRuntimeMetadata(page: Page): Promise<void> {
  await expect
    .poll(() => readRuntimeMetadata(page), { timeout: 30_000 })
    .toEqual({
      capabilities: {
        backend: 'ollama',
        default: 'auto',
        detected: true,
        levels: ['auto', 'off', 'low', 'mid', 'high'],
      },
      contextWindow: 262_144,
    });
}

async function createConversation(page: Page, title: string): Promise<string> {
  const response = await sameOriginFetch(page, '/api/conversations', {
    method: 'POST',
    body: JSON.stringify({ title }),
  });
  expect(response.status, response.text).toBe(201);
  const row = JSON.parse(response.text) as ConversationRow;
  expect(row.ID).toMatch(/^[0-9a-f-]{36}$/i);
  return row.ID;
}

async function deleteConversation(page: Page, conversationID: string): Promise<void> {
  const response = await sameOriginFetch(
    page,
    `/api/conversations/${encodeURIComponent(conversationID)}`,
    { method: 'DELETE' },
  );
  expect([200, 204], response.text).toContain(response.status);
}

function streamFrames(body: string): readonly Record<string, unknown>[] {
  return body
    .replace(/\r\n/g, '\n')
    .split('\n\n')
    .flatMap((block) => {
      const data = block
        .split('\n')
        .filter((line) => line.startsWith('data:'))
        .map((line) => line.slice(5).replace(/^ /, ''))
        .join('\n');
      if (data === '') return [];
      try {
        return [JSON.parse(data) as Record<string, unknown>];
      } catch {
        return [];
      }
    });
}

function reassembledText(frames: readonly Record<string, unknown>[]): string {
  return frames
    .filter((frame) => frame.type === 'TEXT_MESSAGE_CONTENT' && typeof frame.delta === 'string')
    .map((frame) => frame.delta as string)
    .join('');
}

async function runSentinel(
  page: Page,
  conversationID: string,
  sentinel: string,
  effort?: 'high',
): Promise<RunEvidence> {
  await page.goto(`/c/${encodeURIComponent(conversationID)}`, { waitUntil: 'domcontentloaded' });
  const composer = page.getByRole('textbox', { name: 'Ask Aura' });
  await expect(composer).toBeVisible({ timeout: 30_000 });
  if (effort !== undefined) {
    const selector = page.getByRole('combobox', { name: 'Reasoning effort' });
    await expect(selector.locator('option')).toHaveText([
      'Auto',
      'Off',
      'Low',
      'Medium',
      'High',
    ]);
    await selector.selectOption(effort);
    await expect(selector).toHaveValue(effort);
    await expect(page.getByTestId('footer-visible-metrics')).toContainText('262k');
  }
  const responsePromise = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === '/agent/run' &&
      response.request().method() === 'POST',
    { timeout: 300_000 },
  );
  await composer.fill(`Non usare strumenti. Rispondi soltanto con ${sentinel}`);
  await composer.press('Enter');
  const response = await responsePromise;
  expect(response.status()).toBe(200);
  const frames = streamFrames(await response.text());
  expect(frames.some((frame) => frame.type === 'RUN_STARTED')).toBe(true);
  expect(frames.some((frame) => frame.type === 'RUN_FINISHED')).toBe(true);
  return { frames, text: reassembledText(frames) };
}

test.describe('live primary-model hot reload', () => {
  test.skip(!runLive, 'set AURA_E2E_LIVE_MODEL_ROUTES=1 against a rebuilt Aura');

  test('switches Ollama and llama.cpp without an Aura restart', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'chrome', 'one live model witness is sufficient');
    test.setTimeout(720_000);
    await gotoAuthenticated(page, '/?settings=model-routing');

    const conversations: string[] = [];
    try {
      await putProfile(page, localProfile);
      await putProfile(page, ollamaProfile);
      await expectOllamaRuntimeMetadata(page);
      const ollamaConversation = await createConversation(
        page,
        `Ollama route ${String(Date.now())}`,
      );
      conversations.push(ollamaConversation);
      const ollamaRun = await runSentinel(
        page,
        ollamaConversation,
        'AURA_OLLAMA_ROUTE_OK',
        'high',
      );
      const reasoningFrames = ollamaRun.frames.filter(
        (frame) =>
          frame.type === 'REASONING_MESSAGE_CONTENT' &&
          typeof frame.delta === 'string' &&
          frame.delta.length > 0,
      );
      expect(reasoningFrames.length).toBeGreaterThan(0);
      expect(reasoningFrames.every((frame) => frame.delta !== '[reasoning redacted]')).toBe(true);
      await expect(page.getByTestId('reasoning-pill')).toBeVisible();

      if (witnessURL !== undefined) {
        const witnessResponse = await page.request.get(witnessURL);
        expect(witnessResponse.status()).toBe(200);
        const witness = (await witnessResponse.json()) as {
          readonly api_show: number;
          readonly chat_completions: number;
          readonly authorization_seen: number;
        };
        expect(witness).toMatchObject({
          authorization_seen: 0,
        });
        expect(witness.api_show).toBeGreaterThanOrEqual(1);
        expect(witness.chat_completions).toBeGreaterThanOrEqual(1);
      } else {
        expect(ollamaRun.text).toContain('AURA_OLLAMA_ROUTE_OK');
      }

      await putProfile(page, localProfile);
      const localConversation = await createConversation(page, `Local route ${String(Date.now())}`);
      conversations.push(localConversation);
      const localRun = await runSentinel(page, localConversation, 'AURA_LOCAL_ROUTE_OK');
      expect(localRun.text).toContain('AURA_LOCAL_ROUTE_OK');
    } finally {
      await putProfile(page, ollamaProfile);
      for (const conversationID of conversations) {
        await deleteConversation(page, conversationID);
      }
    }
  });
});
