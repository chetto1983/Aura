import { expect, type Page } from '@playwright/test';
import { sameOriginFetch } from './live';

// Helpers for the paid media-generation acceptance suite. They may read the cockpit's own
// APIs and upload fixtures; they never inject an assistant message, never call a Go tool
// directly and never mock a generation response. Everything a test asserts about a tool call
// comes from the persisted thread the daemon wrote while the real agent ran.

/** The one switch that arms the paid suite. Test-only: nothing in the product reads it. */
export const mediaGenerationEnabled = process.env.AURA_E2E_MEDIA_GENERATION === '1';

export const videoInlineWaitKey = 'AURA_VIDEO_INLINE_WAIT_SEC';

interface SnapshotFunctionCall {
  readonly name?: unknown;
  readonly arguments?: unknown;
}

interface SnapshotToolCallRow {
  readonly id?: unknown;
  readonly function?: SnapshotFunctionCall;
}

interface SnapshotMessage {
  readonly role?: unknown;
  readonly content?: unknown;
  readonly toolCallId?: unknown;
  readonly toolCalls?: readonly SnapshotToolCallRow[];
}

export interface ThreadSnapshot {
  readonly messages: readonly SnapshotMessage[];
}

/** One assistant tool call as the daemon persisted it, paired with its tool result. */
export interface RecordedToolCall {
  readonly id: string;
  readonly name: string;
  readonly argsText: string;
  readonly result: string;
}

/**
 * requireLiveOrigin fails the run when the paid suite was armed without naming the stack it
 * spends against. A silent fallback to the Playwright default origin could start a managed
 * `aura serve` with no credentials and turn a paid acceptance run into a green no-op.
 */
export function requireLiveOrigin(): string {
  const origin = process.env.AURA_E2E_ORIGIN;
  if (origin === undefined || origin.trim() === '') {
    throw new Error(
      'AURA_E2E_MEDIA_GENERATION=1 requires AURA_E2E_ORIGIN pointing at the running stack',
    );
  }
  return origin;
}

/**
 * requireMediaRuntime proves, before a single paid call, that the daemon can reach the
 * OpenRouter media catalogs with the identity's key. A missing key, an unconfigured route or
 * a settings store that cannot answer fails the run here rather than skipping it.
 */
export async function requireMediaRuntime(page: Page): Promise<void> {
  await requireMediaCatalog(page, '/api/settings/image-models');
  await requireMediaCatalog(page, '/api/settings/video-models');
}

async function requireMediaCatalog(page: Page, path: string): Promise<void> {
  const response = await sameOriginFetch(page, path);
  if (response.status !== 200) {
    throw new Error(`${path} answered ${String(response.status)}: ${response.text}`);
  }
  const body = JSON.parse(response.text) as { readonly models?: readonly unknown[] };
  if ((body.models ?? []).length === 0) {
    throw new Error(`${path} returned an empty catalog; the media route is not configured`);
  }
}

export async function createMediaConversation(page: Page, title: string): Promise<string> {
  const response = await sameOriginFetch(page, '/api/conversations', {
    method: 'POST',
    body: JSON.stringify({ title }),
  });
  if (response.status !== 201) {
    throw new Error(`Conversation creation failed: ${String(response.status)} ${response.text}`);
  }
  const row = JSON.parse(response.text) as { readonly ID: string };
  expect(row.ID).toMatch(/^[0-9a-f-]{36}$/i);
  return row.ID;
}

/** threadSnapshot reads the persisted turn the agent just took: the trace, not the screen. */
export async function threadSnapshot(page: Page, conversationId: string): Promise<ThreadSnapshot> {
  const response = await sameOriginFetch(
    page,
    `/threads/${encodeURIComponent(conversationId)}/messages`,
  );
  expect(response.status, response.text).toBe(200);
  return JSON.parse(response.text) as ThreadSnapshot;
}

function recordedToolCalls(snapshot: ThreadSnapshot): readonly RecordedToolCall[] {
  const results = new Map<string, string>();
  for (const message of snapshot.messages) {
    if (message.role !== 'tool' || typeof message.toolCallId !== 'string') continue;
    results.set(message.toolCallId, typeof message.content === 'string' ? message.content : '');
  }
  return snapshot.messages.flatMap((message) =>
    (message.toolCalls ?? []).flatMap((call) => {
      const id = call.id;
      const name = call.function?.name;
      if (typeof id !== 'string' || typeof name !== 'string') return [];
      const argsText = call.function?.arguments;
      return [
        {
          id,
          name,
          argsText: typeof argsText === 'string' ? argsText : '',
          result: results.get(id) ?? '',
        },
      ];
    }),
  );
}

/**
 * expectPromotedByToolSearch is the assertion a screenshot cannot make: the deferred media
 * tool was discovered by the agent through tool_search — whose result carries the loaded
 * spec — and only then called. A call with no preceding promotion means the tool was already
 * in the manifest or the turn was steered, and the discovery claim is unproven.
 */
export function expectPromotedByToolSearch(
  snapshot: ThreadSnapshot,
  toolName: string,
): RecordedToolCall {
  const calls = recordedToolCalls(snapshot);
  const promotion = calls.findIndex(
    (call) => call.name === 'tool_search' && call.result.includes(toolName),
  );
  expect(promotion, `no tool_search result loaded ${toolName}`).toBeGreaterThanOrEqual(0);
  const called = calls.slice(promotion + 1).find((call) => call.name === toolName);
  if (called === undefined) {
    throw new Error(`${toolName} was never called after tool_search promoted it`);
  }
  return called;
}

export function countToolCalls(snapshot: ThreadSnapshot, toolName: string): number {
  return recordedToolCalls(snapshot).filter((call) => call.name === toolName).length;
}

export function lastToolCall(snapshot: ThreadSnapshot, toolName: string): RecordedToolCall {
  const last = recordedToolCalls(snapshot)
    .filter((call) => call.name === toolName)
    .at(-1);
  if (last === undefined) throw new Error(`the thread holds no ${toolName} call`);
  return last;
}

export function expectToolResultJSON(call: RecordedToolCall): Record<string, unknown> {
  const parsed = JSON.parse(call.result) as unknown;
  expect(typeof parsed === 'object' && parsed !== null, call.result).toBe(true);
  return parsed as Record<string, unknown>;
}

export function toolCallArguments(call: RecordedToolCall): Record<string, unknown> {
  const parsed = JSON.parse(call.argsText) as unknown;
  expect(typeof parsed === 'object' && parsed !== null, call.argsText).toBe(true);
  return parsed as Record<string, unknown>;
}

/**
 * waitForDeliveredAsset polls the persisted thread until one call of `toolName` reports an
 * asset id. It is the delivery fact the runtime wrote, so it also covers a detached clip that
 * a later wake collects — which no single rendered turn can show.
 */
export async function waitForDeliveredAsset(
  page: Page,
  conversationId: string,
  toolName: string,
  timeoutMs: number,
): Promise<string> {
  const deadline = Date.now() + timeoutMs;
  let last = '';
  for (;;) {
    const snapshot = await threadSnapshot(page, conversationId);
    for (const call of recordedToolCalls(snapshot)) {
      if (call.name !== toolName || call.result === '') continue;
      last = call.result;
      const assetID = deliveredAssetID(call.result);
      if (assetID !== undefined) return assetID;
    }
    if (Date.now() >= deadline) {
      throw new Error(`${toolName} delivered no asset within ${String(timeoutMs)}ms: ${last}`);
    }
    await page.waitForTimeout(5_000);
  }
}

function deliveredAssetID(result: string): string | undefined {
  let parsed: unknown;
  try {
    parsed = JSON.parse(result);
  } catch {
    return undefined;
  }
  if (typeof parsed !== 'object' || parsed === null) return undefined;
  const assetID = (parsed as { readonly asset_id?: unknown }).asset_id;
  return typeof assetID === 'string' && assetID !== '' ? assetID : undefined;
}

export async function putSetting(page: Page, key: string, value: string): Promise<void> {
  const response = await sameOriginFetch(page, `/api/settings/${encodeURIComponent(key)}`, {
    method: 'PUT',
    body: JSON.stringify({ value }),
  });
  expect(response.status, response.text).toBe(200);
}

export async function deleteSetting(page: Page, key: string): Promise<void> {
  const response = await sameOriginFetch(page, `/api/settings/${encodeURIComponent(key)}`, {
    method: 'DELETE',
  });
  expect([200, 204], response.text).toContain(response.status);
}

export interface RangeProbe {
  readonly status: number;
  readonly contentRange: string;
  readonly acceptRanges: string;
  readonly contentType: string;
  readonly bytes: number;
}

/**
 * rangeProbe asks the asset stream route for one byte range from inside the page, so the
 * request carries the session cookie exactly as the <video> element's own does.
 */
export async function rangeProbe(page: Page, url: string, range: string): Promise<RangeProbe> {
  return page.evaluate(
    async ({ requestURL, requestRange }) => {
      const response = await fetch(requestURL, {
        credentials: 'same-origin',
        headers: { Range: requestRange },
      });
      const body = await response.arrayBuffer();
      return {
        status: response.status,
        contentRange: response.headers.get('Content-Range') ?? '',
        acceptRanges: response.headers.get('Accept-Ranges') ?? '',
        contentType: response.headers.get('Content-Type') ?? '',
        bytes: body.byteLength,
      };
    },
    { requestURL: url, requestRange: range },
  );
}

export async function sendPrompt(page: Page, prompt: string): Promise<void> {
  const input = page.getByRole('textbox', { name: 'Ask Aura', exact: true });
  await expect(input).toBeVisible({ timeout: 30_000 });
  await input.fill(prompt);
  await input.press('Enter');
}
