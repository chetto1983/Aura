import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAssetSource } from './assetSourceContext';
import { PreviewError, type RendererProps } from './PreviewStatus';
import { AudioPlayer } from '@/components/media-player';

// Only a tier-owned stream sibling may reach the player. The active source selects
// the tier, and this guard prevents a malformed provider URL from leaving Aura.
function safeStreamSrc(raw: string): string | null {
  if (!raw.startsWith('/') || raw.startsWith('//')) return null;
  try {
    const url = new URL(raw, location.origin);
    if (url.origin !== location.origin || url.search || url.hash) return null;
    const path = url.pathname;
    if (
      !/^\/api\/assets\/[^/]+\/stream$/.test(path) &&
      !/^\/s\/[^/]+\/asset\/[^/]+\/stream$/.test(path) &&
      !/^\/api\/shares\/[^/]+\/asset\/[^/]+\/stream$/.test(path)
    )
      return null;
    return url.href;
  } catch {
    return null;
  }
}

export default function AudioPreview({ assetId, fileName }: RendererProps) {
  const { t } = useTranslation();
  const { streamUrl } = useAssetSource();
  const src = safeStreamSrc(streamUrl(assetId));
  const [failedSrc, setFailedSrc] = useState<string | null>(null);
  if (src === null || failedSrc === src) return <PreviewError />;
  return (
    <AudioPlayer
      src={src}
      title={fileName}
      onMediaError={() => {
        setFailedSrc(src);
      }}
      labels={{
        play: t('artifacts.preview.play'),
        pause: t('artifacts.preview.pause'),
        seek: t('artifacts.preview.seek'),
        unavailable: t('artifacts.preview.error'),
      }}
    />
  );
}
