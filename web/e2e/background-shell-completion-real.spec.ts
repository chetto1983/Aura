import { expect, test, type Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';

const runRealAgent = process.env.AURA_E2E_REAL_AGENT === '1';

interface FetchResult {
  readonly status: number;
  readonly text: string;
}

interface ConversationRow {
  readonly ID: string;
}

interface SnapshotToolCall {
  readonly function?: { readonly name?: string; readonly arguments?: string };
}

interface SnapshotMessage {
  readonly role?: string;
  readonly content?: unknown;
  readonly toolCalls?: readonly SnapshotToolCall[];
}

interface MessagesSnapshot {
  readonly type?: string;
  readonly messages?: readonly SnapshotMessage[];
}

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

async function createConversation(page: Page, title: string): Promise<string> {
  const response = await sameOriginFetch(page, '/api/conversations', {
    method: 'POST',
    body: JSON.stringify({ title }),
  });
  expect(response.status, response.text).toBe(201);
  const conversation = JSON.parse(response.text) as ConversationRow;
  expect(conversation.ID).toMatch(/^[0-9a-f-]{36}$/i);
  return conversation.ID;
}

async function deleteConversation(page: Page, conversationId: string): Promise<void> {
  const response = await sameOriginFetch(
    page,
    `/api/conversations/${encodeURIComponent(conversationId)}`,
    { method: 'DELETE' },
  );
  expect([200, 204], response.text).toContain(response.status);
}

function sseFrames(body: string): readonly Record<string, unknown>[] {
  return body.split(/\r?\n\r?\n/).flatMap((block) => {
    const data = block
      .split(/\r?\n/)
      .filter((line) => line.startsWith('data:'))
      .map((line) => line.slice(5).replace(/^ /, ''))
      .join('\n');
    if (data.length === 0) return [];
    try {
      return [JSON.parse(data) as Record<string, unknown>];
    } catch {
      return [];
    }
  });
}

function toolNames(frames: readonly Record<string, unknown>[]): readonly string[] {
  return frames.flatMap((frame) =>
    frame.type === 'TOOL_CALL_START' && typeof frame.toolCallName === 'string'
      ? [frame.toolCallName]
      : [],
  );
}

function shellExecArgs(
  frames: readonly Record<string, unknown>[],
): Record<string, unknown> | undefined {
  const start = frames.find(
    (frame) =>
      frame.type === 'TOOL_CALL_START' &&
      frame.toolCallName === 'shell_exec' &&
      typeof frame.toolCallId === 'string',
  );
  if (start === undefined) return undefined;
  const id = start.toolCallId;
  const argsText = frames
    .filter(
      (frame) =>
        frame.type === 'TOOL_CALL_ARGS' &&
        frame.toolCallId === id &&
        typeof frame.delta === 'string',
    )
    .map((frame) => frame.delta as string)
    .join('');
  try {
    const parsed = JSON.parse(argsText) as unknown;
    return typeof parsed === 'object' && parsed !== null && !Array.isArray(parsed)
      ? (parsed as Record<string, unknown>)
      : undefined;
  } catch {
    return undefined;
  }
}

function textContent(value: unknown): string {
  if (typeof value === 'string') return value;
  if (value === null || value === undefined) return '';
  try {
    return JSON.stringify(value);
  } catch {
    return '';
  }
}

async function readSnapshot(page: Page, conversationId: string): Promise<MessagesSnapshot> {
  const response = await sameOriginFetch(
    page,
    `/threads/${encodeURIComponent(conversationId)}/messages`,
  );
  expect(response.status, response.text).toBe(200);
  return JSON.parse(response.text) as MessagesSnapshot;
}

async function waitForCompletionTurn(
  page: Page,
  conversationId: string,
  completionToken: string,
): Promise<MessagesSnapshot> {
  const deadline = Date.now() + 90_000;
  let last: MessagesSnapshot = {};
  while (Date.now() < deadline) {
    last = await readSnapshot(page, conversationId);
    const messages = last.messages ?? [];
    const assistantMatches = messages.filter(
      (message) =>
        message.role === 'assistant' && textContent(message.content).includes(completionToken),
    );
    if (assistantMatches.length > 0) return last;
    await new Promise((resolveDelay) => setTimeout(resolveDelay, 1_000));
  }
  throw new Error(
    `automatic background-shell completion turn did not appear; snapshot=${JSON.stringify(last)}`,
  );
}

test.describe('real background-shell completion hook', () => {
  test.skip(!runRealAgent, 'set AURA_E2E_REAL_AGENT=1 to run the real-agent validation');

  test('reactivates the owning conversation once and lets the agent collect final output', async ({
    page,
  }) => {
    test.setTimeout(300_000);
    await gotoAuthenticated(page, '/');

    let browserRunRequests = 0;
    page.on('request', (request) => {
      if (new URL(request.url()).pathname === '/agent/run' && request.method() === 'POST') {
        browserRunRequests++;
      }
    });

    const suffix = `${String(Date.now())}-${Math.random().toString(16).slice(2, 8)}`;
    const outputToken = `AURA_BG_OUTPUT_${suffix}`;
    const startedToken = `AURA_BG_STARTED_${suffix}`;
    const completionToken = `AURA_BG_HOOK_${suffix}`;
    const prompt = [
      `Avvia esattamente una chiamata shell_exec in background con il comando "sleep 4; printf '${outputToken}\\n'".`,
      'Imposta background=true. Nel primo turno non chiamare shell_poll né shell_kill.',
      `Dopo aver ricevuto lo shell_id, termina il primo turno scrivendo soltanto ${startedToken}.`,
      'Quando Aura ti consegna automaticamente la notifica runtime che quel processo è terminato, usa shell_poll sullo shell_id per leggere il risultato.',
      `Solo dopo aver visto ${outputToken} nell'output, rispondi soltanto ${completionToken}.`,
    ].join(' ');
    const conversationId = await createConversation(page, `Background hook ${suffix}`);

    try {
      await page.goto(`/c/${encodeURIComponent(conversationId)}`, {
        waitUntil: 'domcontentloaded',
      });
      const composer = page.getByRole('textbox', { name: 'Ask Aura' });
      await expect(composer).toBeVisible({ timeout: 30_000 });

      const runResponsePromise = page.waitForResponse(
        (response) =>
          new URL(response.url()).pathname === '/agent/run' &&
          response.request().method() === 'POST',
        { timeout: 180_000 },
      );
      await composer.fill(prompt);
      await composer.press('Enter');
      const runResponse = await runResponsePromise;
      expect(runResponse.status()).toBe(200);
      const frames = sseFrames(await runResponse.text());
      const initialTools = toolNames(frames);
      expect(initialTools).toContain('shell_exec');
      expect(initialTools).not.toContain('shell_poll');
      expect(shellExecArgs(frames)?.background).toBe(true);

      const snapshot = await waitForCompletionTurn(page, conversationId, completionToken);
      const messages = snapshot.messages ?? [];
      const completionNotices = messages.filter(
        (message) =>
          message.role === 'user' &&
          textContent(message.content).includes('Background shell') &&
          textContent(message.content).includes('completed'),
      );
      const pollCalls = messages
        .flatMap((message) => message.toolCalls ?? [])
        .filter((call) => call.function?.name === 'shell_poll');
      const completionAnswers = messages.filter(
        (message) =>
          message.role === 'assistant' && textContent(message.content).includes(completionToken),
      );

      expect(snapshot.type).toBe('MESSAGES_SNAPSHOT');
      expect(completionNotices).toHaveLength(1);
      expect(pollCalls).toHaveLength(1);
      expect(completionAnswers).toHaveLength(1);
      expect(browserRunRequests).toBe(1);
    } finally {
      await deleteConversation(page, conversationId);
    }
  });
});
