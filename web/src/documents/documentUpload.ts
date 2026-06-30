import { finalizeAsset, getAsset, presignAsset } from '../chat/attachments/api';
import type { Asset } from '../chat/attachments/types';

type ProgressHandler = (progress: number) => void;

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
  return pollUntilDocumentReady(finalized);
}

async function pollUntilDocumentReady(asset: Asset): Promise<Asset> {
  if (asset.status === 'searchable' || asset.status === 'complete') return asset;
  if (asset.status === 'failed' || asset.status === 'refused') return asset;
  await new Promise((resolve) => window.setTimeout(resolve, 1000));
  return pollUntilDocumentReady(await getAsset(asset.id));
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
    xhr.onerror = () => reject(new Error('upload failed'));
    xhr.send(file);
  });
}
