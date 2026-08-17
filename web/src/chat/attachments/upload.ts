import { getAsset } from './api';
import type { Asset, AssetModality } from './types';

// upload.ts — the transport half of an Aura attachment: the browser-side PUT with progress,
// the modality guess the presign needs, and the readiness rules the poll waits on. Extracted
// from useAttachmentUploads when that hook was replaced by an assistant-ui AttachmentAdapter,
// so the adapter and any future caller share ONE copy of these rules.

const browserForbiddenUploadHeaders = new Set([
  'accept-charset',
  'accept-encoding',
  'access-control-request-headers',
  'access-control-request-method',
  'connection',
  'content-length',
  'cookie',
  'cookie2',
  'date',
  'dnt',
  'expect',
  'host',
  'keep-alive',
  'origin',
  'referer',
  'te',
  'trailer',
  'transfer-encoding',
  'upgrade',
  'via',
]);

const documentExtensions = new Set([
  '.pdf',
  '.docx',
  '.pptx',
  '.xlsx',
  '.xlsm',
  '.html',
  '.htm',
  '.csv',
  '.md',
  '.markdown',
  '.txt',
  '.json',
  '.xml',
  '.epub',
]);

const terminalStatuses = new Set<Asset['status']>(['failed', 'refused', 'deleted', 'canceled']);

/** PUT the file at the presigned URL, reporting fractional progress.
 *
 * XMLHttpRequest rather than fetch because only XHR reports upload progress. Headers the
 * browser forbids a page to set are skipped rather than attempted: setRequestHeader throws
 * on them, and the presigner may legitimately list some (e.g. Content-Length) that the
 * browser fills in itself. */
export function putWithProgress(
  url: string,
  file: File,
  headers: Record<string, string>,
  onProgress: (progress: number) => void,
): Promise<void> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open('PUT', url);
    for (const [key, value] of Object.entries(headers)) {
      if (isBrowserForbiddenUploadHeader(key)) continue;
      try {
        xhr.setRequestHeader(key, value);
      } catch (err) {
        reject(toError(err));
        return;
      }
    }
    xhr.upload.onprogress = (event) => {
      if (event.lengthComputable) onProgress(event.loaded / event.total);
    };
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) resolve();
      else reject(new Error(`upload failed: HTTP ${String(xhr.status)}`));
    };
    xhr.onerror = () => {
      reject(new Error('upload failed'));
    };
    xhr.send(file);
  });
}

function isBrowserForbiddenUploadHeader(name: string): boolean {
  const normalized = name.trim().toLowerCase();
  return (
    browserForbiddenUploadHeaders.has(normalized) ||
    normalized.startsWith('proxy-') ||
    normalized.startsWith('sec-')
  );
}

export function inferModality(file: File): AssetModality {
  const type = file.type.toLowerCase();
  const name = file.name.toLowerCase();
  if (type.startsWith('image/')) return 'image';
  if (type.startsWith('audio/')) return 'audio';
  if (type === 'application/pdf' || hasDocumentExtension(name)) return 'document';
  return 'unknown';
}

function hasDocumentExtension(name: string): boolean {
  for (const ext of documentExtensions) {
    if (name.endsWith(ext)) return true;
  }
  return false;
}

/** A document is usable once it is searchable; media only once fully processed. */
export function isReadyAsset(asset: Asset): boolean {
  if (asset.modality === 'document')
    return asset.status === 'searchable' || asset.status === 'complete';
  if (asset.modality === 'image' || asset.modality === 'audio') return asset.status === 'complete';
  return asset.status === 'searchable' || asset.status === 'complete';
}

export function isTerminalAsset(asset: Asset): boolean {
  return terminalStatuses.has(asset.status);
}

/** Back off as the wait grows: sub-second while the pipeline is likely to answer, then
 * seconds once it clearly is not. */
export function attachmentPollDelayMs(elapsedMs: number): number {
  if (elapsedMs < 5_000) return 500;
  if (elapsedMs < 60_000) return 1_500;
  return 5_000;
}

/** Poll the asset until it is usable or terminally not. `now` and `sleep` are injected so a
 * test can drive the backoff without real time. */
export async function pollUntilReady(
  asset: Asset,
  options: {
    pollIntervalMs?: number | undefined;
    now?: () => number;
    sleep?: (ms: number) => Promise<void>;
  } = {},
): Promise<Asset> {
  const now = options.now ?? (() => Date.now());
  const sleep = options.sleep ?? delay;
  let current = asset;
  const startedAt = now();
  while (!isReadyAsset(current) && !isTerminalAsset(current)) {
    await sleep(options.pollIntervalMs ?? attachmentPollDelayMs(now() - startedAt));
    current = await getAsset(current.id);
  }
  return current;
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
}

function toError(err: unknown): Error {
  return err instanceof Error ? err : new Error(String(err));
}
