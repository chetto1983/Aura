import { readFileSync } from 'node:fs';
import type { Page } from '@playwright/test';

// assetUpload.ts — putting fixture bytes into the identity's library the way the cockpit does:
// presign, PUT to the object store, finalize. It runs INSIDE the page so every call rides the
// session cookie the test already has, and so the PUT leaves the browser rather than Node — a
// seeded asset that arrived by another route would not prove the browser can reach the store.

export interface StoredBytes {
  readonly fileName: string;
  readonly mimeType: string;
  /** What the client claims at presign; the server's own inference is what `unknown` asks for. */
  readonly hint?: 'unknown' | 'audio';
}

/** Presigns and PUTs the bytes, failing on either step, and answers the asset id unfinalized. */
export async function storeBytes(page: Page, bytes: Buffer, stored: StoredBytes): Promise<string> {
  return page.evaluate(
    async ({ base64, fileName, mimeType, hint }) => {
      const body = Uint8Array.from(atob(base64), (c) => c.charCodeAt(0));
      const presign = await fetch('/api/assets/presign', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          thread_id: '',
          file_name: fileName,
          mime_type: mimeType,
          size_bytes: body.byteLength,
          modality_hint: hint,
        }),
      });
      if (!presign.ok) throw new Error(`presign: HTTP ${String(presign.status)}`);
      const { asset, upload } = (await presign.json()) as {
        asset: { id: string };
        upload: { upload_url: string; required_headers?: Record<string, string> };
      };
      const put = await fetch(upload.upload_url, {
        method: 'PUT',
        headers: upload.required_headers ?? {},
        body,
      });
      if (!put.ok) throw new Error(`put: HTTP ${String(put.status)}`);
      return asset.id;
    },
    { base64: bytes.toString('base64'), ...stored, hint: stored.hint ?? 'unknown' },
  );
}

export interface Finalized {
  readonly status: number;
  readonly body: string;
}

/** Finalizes a stored asset and answers what the route said, refusal or not. */
export async function finalizeStored(page: Page, id: string, query = ''): Promise<Finalized> {
  return page.evaluate(
    async ({ assetId, suffix }) => {
      const done = await fetch(`/api/assets/${assetId}/finalize${suffix}`, { method: 'POST' });
      return { status: done.status, body: await done.text() };
    },
    { assetId: id, suffix: query },
  );
}

export interface UploadOptions {
  /** `media` finalizes the way the editor does: accepted, never processed. */
  readonly use?: 'media';
}

/** Uploads bytes through the real asset routes and answers with the asset id. */
export async function uploadBytes(
  page: Page,
  bytes: Buffer,
  fileName: string,
  mimeType: string,
  options: UploadOptions = {},
): Promise<string> {
  const id = await storeBytes(page, bytes, { fileName, mimeType });
  const done = await finalizeStored(page, id, options.use === 'media' ? '?use=media' : '');
  if (done.status < 200 || done.status > 299) {
    throw new Error(`finalize: HTTP ${String(done.status)} ${done.body}`);
  }
  return id;
}

/** Uploads a fixture file through the real asset routes and answers with its asset id. */
export async function uploadAsset(
  page: Page,
  path: string,
  fileName: string,
  mimeType: string,
  options: UploadOptions = {},
): Promise<string> {
  return uploadBytes(page, readFileSync(path), fileName, mimeType, options);
}
