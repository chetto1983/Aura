import { expect, test } from '@playwright/test';
import { gotoAuthenticated } from './auth';

interface BrowserUploadProbe {
  readonly assetId: string;
  readonly uploadUrl: string;
  readonly uploadStatus: number;
}

test.describe('multimodal browser uploads', () => {
  test('presigned upload is browser-safe and host-reachable', async ({ page }) => {
    const consoleFailures: string[] = [];
    page.on('console', (message) => {
      if (message.type() !== 'error') return;
      const text = message.text();
      if (/Content-Length|ERR_NAME_NOT_RESOLVED|FetchEvent|Failed to fetch/i.test(text)) {
        consoleFailures.push(text);
      }
    });
    page.on('pageerror', (error) => {
      consoleFailures.push(error.message);
    });

    await gotoAuthenticated(page, '/');

    const result = await page.evaluate(async (): Promise<BrowserUploadProbe> => {
      const body = new Blob(['phase26 browser upload'], { type: 'image/png' });
      const presignRes = await fetch('/api/assets/presign', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          thread_id: 'phase26-browser-upload',
          file_name: 'phase26-browser-upload.png',
          mime_type: 'image/png',
          size_bytes: body.size,
          modality_hint: 'image',
        }),
      });
      if (!presignRes.ok) {
        throw new Error(`presign failed: HTTP ${String(presignRes.status)}`);
      }
      const presigned = (await presignRes.json()) as {
        asset: { id: string };
        upload: {
          upload_url: string;
          method?: string;
          required_headers?: Record<string, string>;
        };
      };
      const headers = presigned.upload.required_headers ?? {};
      const unsafeHeaders = Object.keys(headers).filter(
        (name) => name.toLowerCase() === 'content-length',
      );
      if (unsafeHeaders.length > 0) {
        throw new Error(`presign returned browser-forbidden headers: ${unsafeHeaders.join(', ')}`);
      }
      if (new URL(presigned.upload.upload_url).hostname === 'garage') {
        throw new Error(`presign returned container-only host: ${presigned.upload.upload_url}`);
      }

      const uploadStatus = await new Promise<number>((resolve, reject) => {
        const xhr = new XMLHttpRequest();
        xhr.open(presigned.upload.method ?? 'PUT', presigned.upload.upload_url);
        for (const [name, value] of Object.entries(headers)) {
          xhr.setRequestHeader(name, value);
        }
        xhr.onload = () => {
          resolve(xhr.status);
        };
        xhr.onerror = () => {
          reject(new Error(`upload failed: ${presigned.upload.upload_url}`));
        };
        xhr.send(body);
      });
      await fetch(`/api/assets/${encodeURIComponent(presigned.asset.id)}`, { method: 'DELETE' });
      return { assetId: presigned.asset.id, uploadUrl: presigned.upload.upload_url, uploadStatus };
    });

    expect(result.assetId).toMatch(/[0-9a-f-]{36}/i);
    const uploadUrl = new URL(result.uploadUrl);
    expect(uploadUrl.hostname).toBe('127.0.0.1');
    expect(uploadUrl.hostname).not.toBe('garage');
    expect(['http:', 'https:']).toContain(uploadUrl.protocol);
    expect(result.uploadStatus).toBeGreaterThanOrEqual(200);
    expect(result.uploadStatus).toBeLessThan(300);
    expect(consoleFailures).toEqual([]);
  });
});
