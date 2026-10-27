import type { ThreadMessageLike } from '@assistant-ui/react';
import { listThreadAssets } from './attachments/api';
import type { Asset } from './attachments/types';
import { attachAssetsToUserMessages } from './ExternalStoreChat_folds';
import { mapTreeMessages } from './ExternalStoreChat_tree';
import { fetchThreadTree, treeHeadPath, type ThreadTree } from './sseAdapter_snapshot';

/**
 * The persisted conversation tree as the chat shows it. The snapshot names a user turn's files
 * by id only (migration 0116), so every path that puts the server's history on screen goes
 * through here; one that skipped it dropped the card of a photo sent earlier, until a reload
 * (measured 2026-10-27). A failed asset listing leaves the turns without cards rather than
 * failing the whole history; an abort still aborts.
 */
export async function fetchThreadHistoryTree(
  threadId: string,
  signal?: AbortSignal,
): Promise<ThreadTree> {
  const tree = await fetchThreadTree(threadId, signal);
  let assets: Asset[] = [];
  try {
    assets = await listThreadAssets(threadId, signal);
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') throw err;
  }
  return mapTreeMessages(tree, (all) => attachAssetsToUserMessages(all, assets));
}

/** The branch the persisted thread opens on, for a caller that replaces the visible list. */
export async function fetchThreadHistory(
  threadId: string,
  signal?: AbortSignal,
): Promise<ThreadMessageLike[]> {
  return treeHeadPath(await fetchThreadHistoryTree(threadId, signal));
}
