import { finalizeAsset, getAsset, presignAsset } from '../chat/attachments/api';
import { attachmentPollDelayMs } from '../chat/attachments/useAttachmentUploads';
import type { Asset } from '../chat/attachments/types';

type ProgressHandler = (progress: number) => void;

export interface IngestionWatchOptions {
  readonly signal?: AbortSignal;
  readonly pollIntervalMs?: number;
}

// Statuses the server can hand back that mean the upload will never become a document.
const rejectedStatuses = new Set<Asset['status']>(['failed', 'refused', 'deleted', 'canceled']);
const settledStatuses = new Set<Asset['status']>([...rejectedStatuses, 'searchable', 'complete']);

// Resolves once the bytes are stored and the asset is accepted -- NOT once the server has
// finished extracting, chunking and embedding it. Ingestion scales with the file, so an
// upload dialog that awaited it sat at "100%" for minutes and never closed at all when the
// pipeline stalled short of 'searchable'. The library list owns the rest of the lifecycle:
// the row lands in the processing tab and waitForDocumentIngestion flips it when it settles.
export async function uploadLibraryDocument(
  file: File,
  onProgress: ProgressHandler = () => undefined,
): Promise<Asset> {
  const presigned = await presignAsset({
    thread_id: '',
    scope: 'library',
    file_name: file.name,
    mime_type: file.type || 'application/octet-stream',
    size_bytes: file.size,
    modality_hint: 'document',
  });
  await uploadToPresignedURL(
    presigned.upload.upload_url,
    presigned.upload.method,
    presigned.upload.required_headers,
    file,
    onProgress,
  );
  const finalized = await finalizeAsset(presigned.asset.id);
  if (rejectedStatuses.has(finalized.status)) {
    throw new Error(finalized.error_message ?? `document upload ${finalized.status}`);
  }
  return finalized;
}

// Polls an accepted asset until the server settles it either way. Runs in the background
// behind a closed dialog, so it must be abortable: the caller aborts on unmount.
export async function waitForDocumentIngestion(
  asset: Asset,
  options: IngestionWatchOptions = {},
): Promise<Asset> {
  const { signal, pollIntervalMs } = options;
  let current = asset;
  const startedAt = Date.now();
  while (!settledStatuses.has(current.status) && !isAborted(signal)) {
    await delay(pollIntervalMs ?? attachmentPollDelayMs(Date.now() - startedAt), signal);
    if (isAborted(signal)) break;
    current = await getAsset(current.id);
  }
  return current;
}

function isAborted(signal: AbortSignal | undefined): boolean {
  return signal?.aborted === true;
}

function delay(ms: number, signal: AbortSignal | undefined): Promise<void> {
  return new Promise((resolve) => {
    let timer = 0;
    const done = () => {
      window.clearTimeout(timer);
      signal?.removeEventListener('abort', done);
      resolve();
    };
    timer = window.setTimeout(done, ms);
    signal?.addEventListener('abort', done, { once: true });
  });
}

function uploadToPresignedURL(
  url: string,
  method: string,
  headers: Record<string, string>,
  file: File,
  onProgress: ProgressHandler,
): Promise<void> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open(method, url);
    for (const [key, value] of Object.entries(headers)) xhr.setRequestHeader(key, value);
    xhr.upload.onprogress = (event) => {
      if (event.lengthComputable && event.total > 0) onProgress(event.loaded / event.total);
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
