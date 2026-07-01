import { existsSync, readFileSync, statSync } from 'node:fs';
import { basename } from 'node:path';
import { expect, test, type Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';

const runLiveRag = process.env.AURA_E2E_LIVE_RAG === '1';
const docPath = toHostPath(
  process.env.AURA_E2E_RAG_DOC_PATH ?? 'D:/tmp/Rag_docs/Corso Base Robot.docx',
);
const docMime = 'application/vnd.openxmlformats-officedocument.wordprocessingml.document';
const prompt =
  'Nel documento allegato "Corso Base Robot.docx", usa document_search e dimmi quali tipi di robot sono elencati. Non usare web_search. Rispondi in una sola frase in italiano.';

interface FetchResult {
  readonly status: number;
  readonly text: string;
}

interface ConversationRow {
  readonly ID: string;
}

interface Asset {
  readonly id: string;
  readonly status: string;
  readonly file_name: string;
  readonly document_id?: string;
  readonly error_message?: string;
}

interface PresignResponse {
  readonly asset: Asset;
  readonly upload: {
    readonly upload_url: string;
    readonly method?: string;
    readonly required_headers?: Record<string, string>;
  };
}

function toHostPath(path: string): string {
  const drivePath = /^([A-Za-z]):[\\/](.*)$/.exec(path);
  if (process.platform === 'win32' || drivePath === null) return path;
  const [, drive, rest] = drivePath;
  if (drive === undefined || rest === undefined) return path;
  return `/mnt/${drive.toLowerCase()}/${rest.replaceAll('\\', '/')}`;
}

function sseFrames(body: string): readonly Record<string, unknown>[] {
  return body
    .replace(/\r\n/g, '\n')
    .split('\n\n')
    .flatMap((block) => {
      const data = block
        .split('\n')
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

function frameText(frames: readonly Record<string, unknown>[]): string {
  return frames
    .filter((frame) => frame.type === 'TEXT_MESSAGE_CONTENT' && typeof frame.delta === 'string')
    .map((frame) => frame.delta as string)
    .join('');
}

function toolNames(frames: readonly Record<string, unknown>[]): readonly string[] {
  return frames.flatMap((frame) => {
    if (frame.type !== 'TOOL_CALL_START') return [];
    return typeof frame.toolCallName === 'string' ? [frame.toolCallName] : [];
  });
}

async function sameOriginFetch(
  page: Page,
  url: string,
  init: {
    readonly method?: string;
    readonly headers?: Record<string, string>;
    readonly body?: string;
  } = {},
): Promise<FetchResult> {
  return page.evaluate(
    async ({ url: requestURL, init: requestInit }) => {
      const res = await fetch(requestURL, {
        ...requestInit,
        credentials: 'same-origin',
      });
      return { status: res.status, text: await res.text() };
    },
    { url, init },
  );
}

async function createConversation(page: Page): Promise<string> {
  const res = await sameOriginFetch(page, '/api/conversations', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ title: 'Live RAG replay smoke' }),
  });
  expect(res.status, res.text).toBe(201);
  const body = JSON.parse(res.text) as ConversationRow;
  expect(body.ID).toMatch(/[0-9a-f-]{36}/i);
  return body.ID;
}

async function uploadDocument(page: Page, threadId: string): Promise<Asset> {
  if (!existsSync(docPath)) {
    throw new Error(`live RAG fixture missing: ${docPath}`);
  }
  const file = readFileSync(docPath);
  const presign = await sameOriginFetch(page, '/api/assets/presign', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      thread_id: threadId,
      file_name: basename(docPath),
      mime_type: docMime,
      size_bytes: statSync(docPath).size,
      modality_hint: 'document',
    }),
  });
  expect(presign.status, presign.text).toBe(200);
  const signed = JSON.parse(presign.text) as PresignResponse;

  const upload = await fetch(signed.upload.upload_url, {
    method: signed.upload.method ?? 'PUT',
    headers: signed.upload.required_headers ?? {},
    body: file,
  });
  expect(upload.status, await upload.text()).toBeGreaterThanOrEqual(200);
  expect(upload.status).toBeLessThan(300);

  const finalized = await sameOriginFetch(
    page,
    `/api/assets/${encodeURIComponent(signed.asset.id)}/finalize`,
    { method: 'POST' },
  );
  expect(finalized.status, finalized.text).toBe(200);
  return pollSearchableAsset(page, signed.asset.id);
}

async function pollSearchableAsset(page: Page, assetId: string): Promise<Asset> {
  const deadline = Date.now() + 180_000;
  let last: Asset | undefined;
  while (Date.now() < deadline) {
    const res = await sameOriginFetch(page, `/api/assets/${encodeURIComponent(assetId)}`);
    expect(res.status, res.text).toBe(200);
    last = JSON.parse(res.text) as Asset;
    if (last.status === 'searchable' && typeof last.document_id === 'string') return last;
    if (last.status === 'failed' || last.status === 'refused') {
      throw new Error(`asset processing failed: ${last.error_message ?? last.status}`);
    }
    await new Promise((resolve) => setTimeout(resolve, 1500));
  }
  throw new Error(`asset ${assetId} did not become searchable; last=${JSON.stringify(last)}`);
}

async function runAgent(page: Page, threadId: string, assetId: string): Promise<string> {
  const res = await sameOriginFetch(page, '/agent/run', {
    method: 'POST',
    headers: {
      Accept: 'text/event-stream',
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      threadId,
      messages: [{ id: `user-${String(Date.now())}`, role: 'user', content: prompt }],
      aura: { attachment_ids: [assetId] },
    }),
  });
  expect(res.status, res.text).toBe(200);
  return res.text;
}

async function expectReplayVisible(page: Page, threadId: string) {
  await page.goto(`/c/${encodeURIComponent(threadId)}`, { waitUntil: 'domcontentloaded' });
  await expect(page.getByText('Corso Base Robot.docx').first()).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText(prompt, { exact: true })).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText(/robot industriali/i).first()).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText(/robot collaborativi/i).first()).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText('knowledge_base', { exact: false })).toHaveCount(0);
  await expect(page.getByText('User message:', { exact: false })).toHaveCount(0);
}

test.describe('live RAG replay smoke', () => {
  test.skip(!runLiveRag, 'set AURA_E2E_LIVE_RAG=1 to run the paid/local live RAG smoke');

  test('uploads a real DOCX, uses document_search, and replays cleanly on mobile', async ({
    page,
  }) => {
    test.setTimeout(300_000);

    await gotoAuthenticated(page, '/');

    const threadId = await createConversation(page);
    const asset = await uploadDocument(page, threadId);
    const sse = await runAgent(page, threadId, asset.id);
    const frames = sseFrames(sse);
    const answer = frameText(frames);

    expect(toolNames(frames)).toContain('document_search');
    expect(answer.toLowerCase()).toContain('industriali');
    expect(answer.toLowerCase()).toContain('collaborativi');

    await expectReplayVisible(page, threadId);

    await page.setViewportSize({ width: 390, height: 844 });
    await expectReplayVisible(page, threadId);
    const horizontalOverflow = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
    );
    expect(horizontalOverflow).toBeLessThanOrEqual(1);
  });
});
