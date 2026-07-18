import { readJSON } from '../http';
import type { Asset, PresignAssetRequest, PresignResponse } from './types';

function assetURL(id: string): string {
  return `/api/assets/${encodeURIComponent(id)}`;
}

export async function presignAsset(req: PresignAssetRequest): Promise<PresignResponse> {
  const res = await fetch('/api/assets/presign', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
    credentials: 'same-origin',
    body: JSON.stringify(req),
  });
  return readJSON<PresignResponse>(res);
}

export async function finalizeAsset(id: string): Promise<Asset> {
  const res = await fetch(`${assetURL(id)}/finalize`, {
    method: 'POST',
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  });
  return readJSON<Asset>(res);
}

export async function getAsset(id: string): Promise<Asset> {
  const res = await fetch(assetURL(id), {
    method: 'GET',
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  });
  return readJSON<Asset>(res);
}

export async function listThreadAssets(threadId: string, signal?: AbortSignal): Promise<Asset[]> {
  const init: RequestInit = {
    method: 'GET',
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  };
  if (signal !== undefined) init.signal = signal;
  const res = await fetch(`/api/assets?thread_id=${encodeURIComponent(threadId)}`, init);
  const value = await readJSON<unknown>(res);
  return Array.isArray(value) ? (value as Asset[]) : [];
}

export async function promoteAsset(id: string): Promise<Asset> {
  const res = await fetch(`${assetURL(id)}/promote`, {
    method: 'POST',
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  });
  return readJSON<Asset>(res);
}

export async function retryAsset(id: string): Promise<Asset> {
  const res = await fetch(`${assetURL(id)}/retry`, {
    method: 'POST',
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  });
  return readJSON<Asset>(res);
}

export async function deleteAsset(id: string): Promise<Asset> {
  const res = await fetch(assetURL(id), {
    method: 'DELETE',
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  });
  return readJSON<Asset>(res);
}
