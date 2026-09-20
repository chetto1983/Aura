import { readFileSync } from 'node:fs';
import type { Page } from '@playwright/test';

// assetUpload.ts — putting fixture bytes into the identity's library the way the cockpit does:
// presign, PUT to the object store, finalize. It runs INSIDE the page so every call rides the
// session cookie the test already has, and so the PUT leaves the browser rather than Node — a
// seeded asset that arrived by another route would not prove the browser can reach the store.

/** Uploads a fixture file through the real asset routes and answers with its asset id. */
export async function uploadAsset(
  page: Page,
  path: string,
  fileName: string,
  mimeType: string,
): Promise<string> {
  const bytes = readFileSync(path).toString('base64');
  return page.evaluate(
    async ({ bytes, file, mimeType }) => {
      const body = Uint8Array.from(atob(bytes), (c) => c.charCodeAt(0));
      const presign = await fetch('/api/assets/presign', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          thread_id: '',
          file_name: file,
          mime_type: mimeType,
          size_bytes: body.byteLength,
          modality_hint: 'unknown',
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
      const done = await fetch(`/api/assets/${asset.id}/finalize`, { method: 'POST' });
      if (!done.ok) throw new Error(`finalize: HTTP ${String(done.status)}`);
      return asset.id;
    },
    { bytes, file: fileName, mimeType },
  );
}
