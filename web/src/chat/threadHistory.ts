import type { ThreadMessageLike } from '@assistant-ui/react';
import { listThreadAssets } from './attachments/api';
import type { Asset } from './attachments/types';
import { attachAssetsToUserMessages } from './ExternalStoreChat_folds';
import { fetchThreadMessages } from './sseAdapter';

/**
 * The persisted thread as the chat shows it. The snapshot names a user turn's files by id
 * only (migration 0116), so every path that replaces the visible list with the server's goes
 * through here; one that skipped it dropped the card of a photo sent earlier, until a reload
 * (measured 2026-10-27). A failed asset listing leaves the turns without cards rather than
 * failing the whole history; an abort still aborts.
 */
export async function fetchThreadHistory(
  threadId: string,
  signal?: AbortSignal,
): Promise<ThreadMessageLike[]> {
  const messages = await fetchThreadMessages(threadId, signal);
  let assets: Asset[] = [];
  try {
    assets = await listThreadAssets(threadId, signal);
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') throw err;
  }
  return attachAssetsToUserMessages(messages, assets);
}
