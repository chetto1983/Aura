import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAssetSource } from './assetSourceContext';
import { PreviewError, type RendererProps } from './PreviewStatus';
import { safeStreamSrc } from './safeStreamSrc';
import { AudioPlayer } from '@/components/media-player';

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
