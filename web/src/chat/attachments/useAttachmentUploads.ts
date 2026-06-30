import { useCallback, useMemo, useRef, useState } from 'react';
import { deleteAsset, finalizeAsset, getAsset, presignAsset } from './api';
import type { Asset, AssetModality, UploadItem } from './types';

interface UseAttachmentUploadOptions {
  readonly pollIntervalMs?: number;
  readonly makeLocalId?: () => string;
}

export interface AttachmentUploads {
  readonly items: readonly UploadItem[];
  readonly readyAssetIds: readonly string[];
  readonly hasBlockingUploads: boolean;
  readonly addFiles: (files: FileList | readonly File[]) => void;
  readonly remove: (localId: string) => void;
  readonly clearReady: () => void;
}

const terminalStatuses = new Set<Asset['status']>(['failed', 'refused', 'deleted', 'canceled']);
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

export function useAttachmentUploads(
  threadId: string,
  options: UseAttachmentUploadOptions = {},
): AttachmentUploads {
  const [items, setItems] = useState<UploadItem[]>([]);
  const itemsRef = useRef<UploadItem[]>([]);
  const { makeLocalId: configuredMakeLocalId, pollIntervalMs: configuredPollIntervalMs } = options;
  const makeLocalId = useCallback(
    () => configuredMakeLocalId?.() ?? crypto.randomUUID(),
    [configuredMakeLocalId],
  );

  const setItemsSynced = useCallback((next: (prev: UploadItem[]) => UploadItem[]) => {
    setItems((prev) => {
      const updated = next(prev);
      itemsRef.current = updated;
      return updated;
    });
  }, []);

  const updateItem = useCallback(
    (localId: string, patch: Partial<UploadItem>) => {
      setItemsSynced((prev) =>
        prev.map((item) => (item.localId === localId ? { ...item, ...patch } : item)),
      );
    },
    [setItemsSynced],
  );

  const pollUntilReady = useCallback(
    async (asset: Asset): Promise<Asset> => {
      let current = asset;
      const startedAt = Date.now();
      while (!isReadyAsset(current) && !terminalStatuses.has(current.status)) {
        const elapsedMs = Date.now() - startedAt;
        await delay(configuredPollIntervalMs ?? attachmentPollDelayMs(elapsedMs));
        current = await getAsset(current.id);
      }
      return current;
    },
    [configuredPollIntervalMs],
  );

  const runUpload = useCallback(
    async (localId: string, file: File) => {
      try {
        const presigned = await presignAsset({
          thread_id: threadId,
          file_name: file.name,
          mime_type: file.type || 'application/octet-stream',
          size_bytes: file.size,
          modality_hint: inferModality(file),
        });
        updateItem(localId, { asset: presigned.asset, status: 'uploading', progress: 0 });
        await putWithProgress(
          presigned.upload.upload_url,
          file,
          presigned.upload.required_headers,
          (progress) => {
            updateItem(localId, { progress });
          },
        );
        const finalized = await finalizeAsset(presigned.asset.id);
        updateItem(localId, { asset: finalized, status: 'processing', progress: 1 });
        const ready = await pollUntilReady(finalized);
        updateItem(localId, {
          asset: ready,
          status: uploadStatusForAsset(ready),
          progress: 1,
          ...(ready.error_message !== undefined ? { error: ready.error_message } : {}),
        });
      } catch (err) {
        updateItem(localId, { status: 'failed', error: errorMessage(err) });
      }
    },
    [pollUntilReady, threadId, updateItem],
  );

  const addFiles = useCallback(
    (filesLike: FileList | readonly File[]) => {
      const files = Array.from(filesLike);
      const queued = files.map((file) => ({
        localId: makeLocalId(),
        file,
        progress: 0,
        status: 'queued' as const,
      }));
      setItemsSynced((prev) => [...prev, ...queued]);
      for (const item of queued) {
        void runUpload(item.localId, item.file);
      }
    },
    [makeLocalId, runUpload, setItemsSynced],
  );

  const remove = useCallback(
    (localId: string) => {
      const assetID = itemsRef.current.find((item) => item.localId === localId)?.asset?.id;
      setItemsSynced((prev) => prev.filter((item) => item.localId !== localId));
      if (assetID !== undefined) void deleteAsset(assetID).catch(() => undefined);
    },
    [setItemsSynced],
  );

  const clearReady = useCallback(() => {
    setItemsSynced((prev) => prev.filter((item) => item.status !== 'ready'));
  }, [setItemsSynced]);

  const readyAssetIds = useMemo(
    () =>
      items
        .filter((item) => item.status === 'ready' && item.asset !== undefined)
        .map((item) => item.asset?.id)
        .filter((id): id is string => id !== undefined),
    [items],
  );
  const hasBlockingUploads = useMemo(
    () =>
      items.some(
        (item) =>
          item.status === 'queued' || item.status === 'uploading' || item.status === 'processing',
      ),
    [items],
  );

  return { items, readyAssetIds, hasBlockingUploads, addFiles, remove, clearReady };
}

function putWithProgress(
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

function inferModality(file: File): AssetModality {
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

function isReadyAsset(asset: Asset): boolean {
  if (asset.modality === 'document')
    return asset.status === 'searchable' || asset.status === 'complete';
  if (asset.modality === 'image' || asset.modality === 'audio') return asset.status === 'complete';
  return asset.status === 'searchable' || asset.status === 'complete';
}

export function attachmentPollDelayMs(elapsedMs: number): number {
  if (elapsedMs < 5_000) return 500;
  if (elapsedMs < 60_000) return 1_500;
  return 5_000;
}

function uploadStatusForAsset(asset: Asset): UploadItem['status'] {
  if (isReadyAsset(asset)) return 'ready';
  if (asset.status === 'refused') return 'refused';
  if (asset.status === 'failed' || asset.status === 'deleted' || asset.status === 'canceled') {
    return 'failed';
  }
  return 'processing';
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : 'upload failed';
}

function toError(err: unknown): Error {
  return err instanceof Error ? err : new Error(String(err));
}
