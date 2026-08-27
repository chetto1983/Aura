import { expect, test, type Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';

const runLive = process.env.AURA_E2E_NATIVE_MEDIA === '1';
const waitForTextFallback = process.env.AURA_E2E_WAIT_TEXT_FALLBACK === '1';
const expectedCode = 'ZEBRA 417';

interface FetchResult {
  readonly status: number;
  readonly text: string;
}

async function sameOriginFetch(
  page: Page,
  url: string,
  init: {
    readonly method?: string;
    readonly body?: string;
    readonly headers?: Record<string, string>;
  } = {},
): Promise<FetchResult> {
  return page.evaluate(
    async ({ requestURL, requestInit }) => {
      const response = await fetch(requestURL, {
        ...requestInit,
        credentials: 'same-origin',
      });
      return { status: response.status, text: await response.text() };
    },
    { requestURL: url, requestInit: init },
  );
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

async function createConversation(page: Page): Promise<string> {
  const response = await sameOriginFetch(page, '/api/conversations', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ title: 'Native media live acceptance' }),
  });
  expect(response.status, response.text).toBe(201);
  const row = JSON.parse(response.text) as { readonly ID: string };
  return row.ID;
}

async function uploadCodeImage(page: Page, threadID: string): Promise<string> {
  return page.evaluate(
    async ({ code, thread }) => {
      const canvas = document.createElement('canvas');
      canvas.width = 900;
      canvas.height = 360;
      const context = canvas.getContext('2d');
      if (context === null) throw new Error('2D canvas unavailable');
      context.fillStyle = '#ffffff';
      context.fillRect(0, 0, canvas.width, canvas.height);
      context.fillStyle = '#000000';
      context.font = 'bold 132px sans-serif';
      context.textAlign = 'center';
      context.textBaseline = 'middle';
      context.fillText(code, canvas.width / 2, canvas.height / 2);
      const blob = await new Promise<Blob>((resolve, reject) => {
        canvas.toBlob((value) => {
          if (value === null) reject(new Error('PNG encoding failed'));
          else resolve(value);
        }, 'image/png');
      });

      const presignResponse = await fetch('/api/assets/presign', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          thread_id: thread,
          file_name: 'native-media-acceptance.png',
          mime_type: 'image/png',
          size_bytes: blob.size,
          modality_hint: 'image',
        }),
      });
      if (!presignResponse.ok) {
        throw new Error(`presign failed: HTTP ${String(presignResponse.status)}`);
      }
      const presigned = (await presignResponse.json()) as {
        readonly asset: { readonly id: string };
        readonly upload: {
          readonly upload_url: string;
          readonly method?: string;
          readonly required_headers?: Record<string, string>;
        };
      };
      const uploadStatus = await new Promise<number>((resolve, reject) => {
        const request = new XMLHttpRequest();
        request.open(presigned.upload.method ?? 'PUT', presigned.upload.upload_url);
        for (const [name, value] of Object.entries(presigned.upload.required_headers ?? {})) {
          request.setRequestHeader(name, value);
        }
        request.onload = () => {
          resolve(request.status);
        };
        request.onerror = () => {
          reject(new Error('Garage upload failed'));
        };
        request.send(blob);
      });
      if (uploadStatus < 200 || uploadStatus >= 300) {
        throw new Error(`Garage upload failed: HTTP ${String(uploadStatus)}`);
      }
      const finalizeResponse = await fetch(
        `/api/assets/${encodeURIComponent(presigned.asset.id)}/finalize`,
        { method: 'POST', credentials: 'same-origin' },
      );
      if (!finalizeResponse.ok) {
        throw new Error(`finalize failed: HTTP ${String(finalizeResponse.status)}`);
      }
      return presigned.asset.id;
    },
    { code: expectedCode, thread: threadID },
  );
}

async function waitForAttachmentSummary(page: Page, assetID: string): Promise<void> {
  const deadline = Date.now() + 180_000;
  let last = '';
  while (Date.now() < deadline) {
    const response = await sameOriginFetch(page, `/api/assets/${encodeURIComponent(assetID)}`);
    expect(response.status, response.text).toBe(200);
    last = response.text;
    const asset = JSON.parse(response.text) as {
      readonly status?: string;
      readonly summary?: string;
      readonly error_message?: string;
    };
    if ((asset.summary ?? '').toUpperCase().includes(expectedCode)) return;
    if (['failed', 'refused', 'deleted', 'canceled'].includes(asset.status ?? '')) {
      throw new Error(`asset processing failed before text fallback: ${last}`);
    }
    await new Promise((resolve) => setTimeout(resolve, 1_500));
  }
  throw new Error(`asset summary did not contain ${expectedCode}; last=${last}`);
}

test.describe('selected-model native media live acceptance', () => {
  test.skip(!runLive, 'set AURA_E2E_NATIVE_MEDIA=1 with a capability-advertising local primary');

  test('projects a current-turn Garage image into the selected primary', async ({ page }) => {
    test.setTimeout(300_000);
    await gotoAuthenticated(page, '/');
    const threadID = await createConversation(page);
    let assetID = '';
    try {
      assetID = await uploadCodeImage(page, threadID);
      if (waitForTextFallback) await waitForAttachmentSummary(page, assetID);
      const response = await sameOriginFetch(page, '/agent/run', {
        method: 'POST',
        headers: { Accept: 'text/event-stream', 'Content-Type': 'application/json' },
        body: JSON.stringify({
          threadId: threadID,
          messages: [
            {
              id: `user-${String(Date.now())}`,
              role: 'user',
              content:
                'The image is already attached to this message. Do not call any tool. Read the exact code directly from its pixels and reply with only that code.',
            },
          ],
          aura: { attachment_ids: [assetID] },
        }),
      });
      expect(response.status, response.text).toBe(200);
      const frames = sseFrames(response.text);
      const runErrors = frames.filter((frame) => frame.type === 'RUN_ERROR');
      expect(runErrors, JSON.stringify(runErrors.slice(0, 3))).toHaveLength(0);
      expect(frames.some((frame) => frame.type === 'RUN_FINISHED')).toBe(true);
      const answer = frameText(frames);
      expect(
        answer.toUpperCase().includes(expectedCode),
        JSON.stringify({ answer: answer.slice(0, 1_000), frames: frames.slice(0, 20) }),
      ).toBe(true);
    } finally {
      if (assetID !== '') {
        await sameOriginFetch(page, `/api/assets/${encodeURIComponent(assetID)}`, {
          method: 'DELETE',
        });
      }
      await sameOriginFetch(page, `/api/conversations/${encodeURIComponent(threadID)}`, {
        method: 'DELETE',
      });
    }
  });
});
