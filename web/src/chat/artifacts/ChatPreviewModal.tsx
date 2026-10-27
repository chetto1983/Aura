import { useMemo } from 'react';
import { composerSource } from './composerSource';
import { PreviewModal, type PreviewModalProps } from './PreviewModal';
import { AssetSourceContext, IDENTITY_SCOPED } from './renderers/assetSourceContext';

/** The chat's preview: an artifact opened from the thread or the Artifacts panel can be edited
 *  in its Craft app, and what the app saves goes into the next message (composerSource). */
export function ChatPreviewModal({ active, onClose }: PreviewModalProps) {
  const source = useMemo(
    () => (active === undefined ? IDENTITY_SCOPED : composerSource(active)),
    [active],
  );
  return (
    <AssetSourceContext.Provider value={source}>
      <PreviewModal active={active} onClose={onClose} />
    </AssetSourceContext.Provider>
  );
}
