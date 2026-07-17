import { existsSync } from 'node:fs';
import { basename } from 'node:path';
import { expect, test, type Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';
import { collectBrowserHealth } from './support/browserHealth';

const runRealAgent = process.env.AURA_E2E_REAL_AGENT === '1';
const origin = process.env.AURA_E2E_ORIGIN ?? 'http://127.0.0.1:9080';
const documentPath = process.env.AURA_E2E_RAG_DOC_PATH ?? 'D:/tmp/Rag_docs/Corso Base Robot.docx';
const prompt =
  'Nel documento allegato "Corso Base Robot.docx", usa document_search e dimmi quali tipi di robot sono elencati. Non usare web_search. Rispondi in una sola frase in italiano.';

interface FetchResult {
  readonly status: number;
  readonly text: string;
}

interface ConversationRow {
  readonly ID: string;
}

interface AssetRow {
  readonly id: string;
  readonly status: string;
  readonly file_name: string;
  readonly document_id?: string;
  readonly error_message?: string;
}

interface CatalogDocumentRow {
  readonly id: string;
  readonly status: string;
  readonly metadata: Record<string, unknown>;
}

interface AgentProbe {
  readonly complete: boolean;
  readonly request: {
    readonly method: string;
    readonly pathname: string;
    readonly body: string;
  };
  readonly response?: {
    readonly status: number;
    readonly body: string;
  };
  readonly error?: string;
}

interface ScoreCheck {
  readonly name: string;
  readonly pass: boolean;
  readonly detail: string;
}

interface ToolEvidence {
  readonly id: string;
  readonly args?: Record<string, unknown>;
  readonly result: string;
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
    if (frame.type !== 'TOOL_CALL_START' || typeof frame.toolCallName !== 'string') return [];
    return [frame.toolCallName];
  });
}

function toolEvidences(
  frames: readonly Record<string, unknown>[],
  name: string,
): readonly ToolEvidence[] {
  return frames.flatMap((start) => {
    if (
      start.type !== 'TOOL_CALL_START' ||
      start.toolCallName !== name ||
      typeof start.toolCallId !== 'string'
    ) {
      return [];
    }
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
    let args: Record<string, unknown> | undefined;
    try {
      const parsed = JSON.parse(argsText) as unknown;
      if (typeof parsed === 'object' && parsed !== null && !Array.isArray(parsed)) {
        args = parsed as Record<string, unknown>;
      }
    } catch {
      args = undefined;
    }
    const result = frames
      .filter(
        (frame) =>
          frame.type === 'TOOL_CALL_RESULT' &&
          frame.toolCallId === id &&
          typeof frame.content === 'string',
      )
      .map((frame) => frame.content as string)
      .join('\n');
    return [{ id, ...(args === undefined ? {} : { args }), result }];
  });
}

function usageTokens(frames: readonly Record<string, unknown>[]): number {
  let total = 0;
  for (const frame of frames) {
    if (frame.type !== 'STATE_DELTA' || !Array.isArray(frame.delta)) continue;
    for (const operation of frame.delta as readonly unknown[]) {
      if (typeof operation !== 'object' || operation === null) continue;
      const value = operation as { readonly path?: unknown; readonly value?: unknown };
      if (
        (value.path === '/prompt_tokens' || value.path === '/completion_tokens') &&
        typeof value.value === 'number'
      ) {
        total += value.value;
      }
    }
  }
  return total;
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

async function createConversation(page: Page): Promise<string> {
  const response = await sameOriginFetch(page, '/api/conversations', {
    method: 'POST',
    body: JSON.stringify({ title: `Calm Prism real-agent ${new Date().toISOString()}` }),
  });
  expect(response.status, response.text).toBe(201);
  const row = JSON.parse(response.text) as ConversationRow;
  expect(row.ID).toMatch(/^[0-9a-f-]{36}$/i);
  return row.ID;
}

async function deleteConversation(page: Page, conversationId: string) {
  const response = await sameOriginFetch(
    page,
    `/api/conversations/${encodeURIComponent(conversationId)}`,
    { method: 'DELETE' },
  );
  expect([200, 204], response.text).toContain(response.status);
}

async function deleteAsset(page: Page, assetId: string) {
  const response = await sameOriginFetch(page, `/api/assets/${encodeURIComponent(assetId)}`, {
    method: 'DELETE',
  });
  expect(response.status, response.text).toBe(200);
  const asset = JSON.parse(response.text) as AssetRow;
  expect(asset).toMatchObject({ id: assetId, status: 'deleted' });
}

async function deleteCatalogDocument(page: Page, documentId: string) {
  const response = await sameOriginFetch(page, `/api/documents/${encodeURIComponent(documentId)}`, {
    method: 'DELETE',
  });
  expect(response.status, response.text).toBe(200);
  const document = JSON.parse(response.text) as CatalogDocumentRow;
  expect(document).toMatchObject({ id: documentId, status: 'deleted' });
}

async function cleanupRealAgent(
  page: Page,
  assetId: string | undefined,
  catalogDocumentId: string | undefined,
  conversationId: string,
) {
  const errors: unknown[] = [];
  let documentId = catalogDocumentId;
  const attempt = async (operation: () => Promise<void>) => {
    try {
      await operation();
    } catch (error) {
      errors.push(error);
    }
  };
  if (documentId === undefined && assetId !== undefined) {
    await attempt(async () => {
      documentId = (await catalogDocumentForAsset(page, assetId)).id;
    });
  }
  if (assetId !== undefined) await attempt(() => deleteAsset(page, assetId));
  const knownDocumentId = documentId;
  if (knownDocumentId !== undefined) {
    await attempt(() => deleteCatalogDocument(page, knownDocumentId));
  }
  await attempt(() => deleteConversation(page, conversationId));
  if (errors.length > 0) throw new AggregateError(errors, 'real-agent cleanup failed');
}

async function threadAssets(page: Page, conversationId: string): Promise<readonly AssetRow[]> {
  const response = await sameOriginFetch(
    page,
    `/api/assets?thread_id=${encodeURIComponent(conversationId)}`,
  );
  expect(response.status, response.text).toBe(200);
  const value = JSON.parse(response.text) as unknown;
  return Array.isArray(value) ? (value as AssetRow[]) : [];
}

async function pollSearchableThreadAsset(
  page: Page,
  conversationId: string,
  assetId: string,
): Promise<AssetRow> {
  const deadline = Date.now() + 240_000;
  let last: AssetRow | undefined;
  while (Date.now() < deadline) {
    last = (await threadAssets(page, conversationId)).find((asset) => asset.id === assetId);
    const documentId = last?.document_id?.trim() ?? '';
    if (last?.status === 'searchable' && documentId.length > 0) return last;
    if (last !== undefined && ['failed', 'refused', 'deleted', 'canceled'].includes(last.status)) {
      throw new Error(`asset processing ended before searchable: ${JSON.stringify(last)}`);
    }
    await new Promise((resolveDelay) => setTimeout(resolveDelay, 1500));
  }
  throw new Error(
    `thread asset ${assetId} did not become searchable; last=${last === undefined ? 'missing' : JSON.stringify(last)}`,
  );
}

async function catalogDocumentForAsset(page: Page, assetId: string): Promise<CatalogDocumentRow> {
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    const response = await sameOriginFetch(page, '/api/documents?limit=100');
    expect(response.status, response.text).toBe(200);
    const value = JSON.parse(response.text) as unknown;
    const rows = Array.isArray(value) ? (value as CatalogDocumentRow[]) : [];
    const document = rows.find((row) => row.metadata.source_asset_id === assetId);
    if (document !== undefined) return document;
    await new Promise((resolveDelay) => setTimeout(resolveDelay, 500));
  }
  throw new Error(`catalog document for asset ${assetId} did not appear`);
}

async function installAgentProbe(page: Page) {
  await page.addInitScript(() => {
    const probeWindow = window as typeof window & { __auraRealAgentProbe?: AgentProbe };
    const originalFetch = window.fetch.bind(window);
    window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL =
        typeof input === 'string' || input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL, window.location.href);
      const method = (
        init?.method ?? (input instanceof Request ? input.method : 'GET')
      ).toUpperCase();
      const isAgentRun =
        url.origin === window.location.origin && url.pathname === '/agent/run' && method === 'POST';
      let requestBody = '';
      if (isAgentRun) {
        if (typeof init?.body === 'string') requestBody = init.body;
        else if (input instanceof Request) requestBody = await input.clone().text();
        probeWindow.__auraRealAgentProbe = {
          complete: false,
          request: { method, pathname: url.pathname, body: requestBody },
        };
      }
      try {
        const response = await originalFetch(input, init);
        if (isAgentRun) {
          void response
            .clone()
            .text()
            .then(
              (body) => {
                probeWindow.__auraRealAgentProbe = {
                  complete: true,
                  request: { method, pathname: url.pathname, body: requestBody },
                  response: { status: response.status, body },
                };
              },
              (error: unknown) => {
                probeWindow.__auraRealAgentProbe = {
                  complete: true,
                  request: { method, pathname: url.pathname, body: requestBody },
                  error: String(error),
                };
              },
            );
        }
        return response;
      } catch (error) {
        if (isAgentRun) {
          probeWindow.__auraRealAgentProbe = {
            complete: true,
            request: { method, pathname: url.pathname, body: requestBody },
            error: String(error),
          };
        }
        throw error;
      }
    };
  });
}

async function readCompletedProbe(page: Page): Promise<AgentProbe> {
  await page.waitForFunction(
    () =>
      Boolean(
        (window as typeof window & { __auraRealAgentProbe?: AgentProbe }).__auraRealAgentProbe
          ?.complete,
      ),
    undefined,
    { timeout: 300_000 },
  );
  return page.evaluate(() => {
    const probe = (window as typeof window & { __auraRealAgentProbe?: AgentProbe })
      .__auraRealAgentProbe;
    if (probe === undefined) throw new Error('real-agent fetch probe missing after completion');
    return probe;
  });
}

async function overflow(page: Page): Promise<number> {
  return page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  );
}

test.describe('real-agent Calm Prism acceptance', () => {
  test.skip(!runRealAgent, 'set AURA_E2E_REAL_AGENT=1 to run the paid/local real-agent gate');

  test('scores the real composer, DOCX RAG, persistence, and responsive health at 10/10', async ({
    page,
  }, testInfo) => {
    test.setTimeout(1_500_000);
    if (!existsSync(documentPath))
      throw new Error(`real-agent DOCX fixture missing: ${documentPath}`);

    await gotoAuthenticated(page, '/');
    await page.unroute('**/api/onboarding/status');
    await page.unroute('**/healthz');
    await page.unroute('**/readyz');
    const conversationId = await createConversation(page);
    const checks: ScoreCheck[] = [];
    let uploadedAssetId: string | undefined;
    let catalogDocumentId: string | undefined;
    let replayPage: Page | undefined;

    try {
      await installAgentProbe(page);
      const health = collectBrowserHealth(page, origin);
      await page.goto(`/c/${encodeURIComponent(conversationId)}`, {
        waitUntil: 'domcontentloaded',
      });

      const composer = page.getByRole('textbox', { name: 'Ask Aura' });
      await expect(composer).toBeVisible({ timeout: 30_000 });
      checks.push({
        name: 'authenticated composer',
        pass: !page.url().includes('/login'),
        detail: page.url(),
      });

      const presignResponsePromise = page.waitForResponse(
        (response) =>
          new URL(response.url()).origin === new URL(origin).origin &&
          new URL(response.url()).pathname === '/api/assets/presign' &&
          response.request().method() === 'POST',
        { timeout: 30_000 },
      );
      const fileName = basename(documentPath);
      await page.locator('input[type="file"][aria-label="Add files"]').setInputFiles(documentPath);
      const presignResponse = await presignResponsePromise;
      expect(presignResponse.status()).toBe(200);
      const presignBody = (await presignResponse.json()) as {
        readonly asset?: { readonly id?: unknown };
      };
      expect(presignBody.asset?.id).toEqual(expect.any(String));
      uploadedAssetId = presignBody.asset?.id as string;
      const attachmentChip = page.getByRole('button', { name: `Remove ${fileName}` }).locator('..');
      await expect(attachmentChip.getByText(fileName, { exact: true })).toBeVisible({
        timeout: 30_000,
      });
      await expect(attachmentChip.getByText('Ready', { exact: true })).toBeVisible({
        timeout: 240_000,
      });
      const document = await pollSearchableThreadAsset(page, conversationId, uploadedAssetId);
      const documentId = document?.document_id?.trim() ?? '';
      const catalogDocument = await catalogDocumentForAsset(page, uploadedAssetId);
      catalogDocumentId = catalogDocument.id;
      const searchDocumentMetadata = catalogDocument.metadata.search_document_id;
      const catalogSearchDocumentId =
        typeof searchDocumentMetadata === 'string' ? searchDocumentMetadata.trim() : '';
      const documentReady =
        document?.status === 'searchable' &&
        documentId.length > 0 &&
        catalogSearchDocumentId === documentId;
      checks.push({
        name: 'searchable real DOCX upload',
        pass: documentReady,
        detail:
          document === undefined
            ? 'asset missing'
            : `${document.status}:${document.document_id ?? ''}`,
      });

      const runResponsePromise = page.waitForResponse(
        (response) =>
          new URL(response.url()).origin === new URL(origin).origin &&
          new URL(response.url()).pathname === '/agent/run' &&
          response.request().method() === 'POST',
        { timeout: 300_000 },
      );
      await composer.fill(prompt);
      await composer.press('Enter');
      const runResponse = await runResponsePromise;
      await expect(page.getByTestId('footer-settled-status')).toContainText('Run complete', {
        timeout: 300_000,
      });
      const probe = await readCompletedProbe(page);
      const responseBody = probe.response?.body ?? '';
      const frames = sseFrames(responseBody);
      const answer = frameText(frames);
      const names = toolNames(frames);
      const documentSearches = toolEvidences(frames, 'document_search');
      const requestBody = JSON.parse(probe.request.body) as {
        readonly aura?: { readonly attachment_ids?: readonly string[] };
      };
      const attachmentIds = requestBody.aura?.attachment_ids ?? [];

      checks.push({
        name: 'real run request / HTTP 200',
        pass:
          probe.request.method === 'POST' &&
          probe.request.pathname === '/agent/run' &&
          runResponse.status() === 200 &&
          probe.response?.status === 200 &&
          attachmentIds.length === 1 &&
          attachmentIds[0] === uploadedAssetId,
        detail: `browser=${String(runResponse.status())}, probe=${String(probe.response?.status)}, assets=${attachmentIds.join(',')}`,
      });
      const started = frames.some((frame) => frame.type === 'RUN_STARTED');
      const finished = frames.some(
        (frame) =>
          frame.type === 'RUN_FINISHED' &&
          typeof frame.outcome === 'object' &&
          frame.outcome !== null &&
          (frame.outcome as { readonly type?: unknown }).type === 'success',
      );
      const errored = frames.some((frame) => frame.type === 'RUN_ERROR');
      checks.push({
        name: 'successful RUN lifecycle',
        pass: started && finished && !errored && probe.error === undefined,
        detail: `started=${String(started)}, finished=${String(finished)}, error=${String(errored)}`,
      });
      checks.push({
        name: 'document_search tool',
        pass:
          documentSearches.length > 0 &&
          documentSearches.every((search) => search.args?.document_id === documentId) &&
          !names.includes('web_search'),
        detail: `tools=${names.join(', ')}, calls=${JSON.stringify(documentSearches)}`,
      });
      const normalizedAnswer = answer.toLocaleLowerCase('it');
      const groundedResult = documentSearches.find((search) => {
        const result = search.result.toLocaleLowerCase('it');
        return result.includes('industriali') && result.includes('collaborativi');
      });
      checks.push({
        name: 'grounded answer facts',
        pass:
          groundedResult !== undefined &&
          normalizedAnswer.includes('industriali') &&
          normalizedAnswer.includes('collaborativi'),
        detail: `result=${groundedResult?.result ?? ''}; answer=${answer}`,
      });
      const tokens = usageTokens(frames);
      checks.push({ name: 'non-zero usage', pass: tokens > 0, detail: `${String(tokens)} tokens` });

      const desktopOverflow = await overflow(page);
      const composerBox = await page.getByTestId('chat-composer').boundingBox();
      const desktopWidth = page.viewportSize()?.width ?? 0;
      const desktopContained =
        desktopOverflow <= 1 &&
        composerBox !== null &&
        composerBox.x >= -1 &&
        composerBox.x + composerBox.width <= desktopWidth + 1;
      checks.push({
        name: 'desktop containment',
        pass: desktopContained,
        detail: `overflow=${String(desktopOverflow)}, composer=${JSON.stringify(composerBox)}`,
      });

      health.assertClean();
      replayPage = await page.context().newPage();
      const replayHealth = collectBrowserHealth(replayPage, origin);
      await replayPage.goto(`/c/${encodeURIComponent(conversationId)}`, {
        waitUntil: 'domcontentloaded',
      });
      await expect(replayPage.getByText(prompt, { exact: true })).toBeVisible({ timeout: 30_000 });
      const replayProse = replayPage.locator(
        '[data-message-role="assistant"] [data-message-prose]',
      );
      await expect(replayProse.filter({ hasText: /robot industriali/i }).last()).toBeVisible({
        timeout: 30_000,
      });
      await expect(replayProse.filter({ hasText: /robot collaborativi/i }).last()).toBeVisible({
        timeout: 30_000,
      });
      checks.push({ name: 'persisted replay', pass: true, detail: conversationId });

      await replayPage.setViewportSize({ width: 390, height: 844 });
      await expect(replayPage.getByText(prompt, { exact: true })).toBeVisible();
      const mobileOverflow = await overflow(replayPage);
      replayHealth.assertClean();
      checks.push({
        name: 'mobile overflow plus strict browser health',
        pass: mobileOverflow <= 1,
        detail: `overflow=${String(mobileOverflow)}, health=clean`,
      });

      const score = checks.filter((check) => check.pass).length;
      for (const check of checks) {
        console.log(`${check.pass ? 'PASS' : 'FAIL'} ${check.name}: ${check.detail}`);
      }
      console.log(`SCORE: ${String(score)}/10 = ${String(score * 10)}%`);
      await testInfo.attach('real-agent-score.json', {
        body: Buffer.from(JSON.stringify({ score, checks }, null, 2)),
        contentType: 'application/json',
      });
      expect(checks).toHaveLength(10);
      expect(score).toBe(10);
    } finally {
      try {
        await replayPage?.close();
      } finally {
        await cleanupRealAgent(page, uploadedAssetId, catalogDocumentId, conversationId);
      }
    }
  });
});
