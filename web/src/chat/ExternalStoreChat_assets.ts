import { useCallback, type Dispatch, type SetStateAction } from 'react';
import type { ThreadMessageLike } from '@assistant-ui/react';
import { deleteAsset, promoteAsset, retryAsset } from './attachments/api';
import { removeAssetFromMessages, replaceAssetInMessages } from './ExternalStoreChat_folds';

// ExternalStoreChat_assets — the attachment-chip action callbacks split out of
// ExternalStoreChat.tsx (600-LOC cap, refactor-on-touch): each maps an asset
// mutation onto the visible message list via the pure fold helpers. Failures
// are swallowed — the chip keeps its prior state and the user can retry.

export function useAssetActions(setMessages: Dispatch<SetStateAction<ThreadMessageLike[]>>) {
  const handleAssetRetry = useCallback(
    (assetID: string) => {
      void retryAsset(assetID)
        .then((asset) => {
          setMessages((prev) => replaceAssetInMessages(prev, asset));
        })
        .catch(() => undefined);
    },
    [setMessages],
  );

  const handleAssetPromote = useCallback(
    (assetID: string) => {
      void promoteAsset(assetID)
        .then((asset) => {
          setMessages((prev) => replaceAssetInMessages(prev, asset));
        })
        .catch(() => undefined);
    },
    [setMessages],
  );

  const handleAssetRemove = useCallback(
    (assetID: string) => {
      void deleteAsset(assetID)
        .then(() => {
          setMessages((prev) => removeAssetFromMessages(prev, assetID));
        })
        .catch(() => undefined);
    },
    [setMessages],
  );

  return { handleAssetRetry, handleAssetPromote, handleAssetRemove };
}
