import { useState } from 'react';
import { useAssetSource } from './assetSourceContext';
import { PreviewError, type RendererProps } from './PreviewStatus';
import { safeStreamSrc } from './safeStreamSrc';
import { VideoPlayer } from '@/components/media-player';

// video/mp4 and video/webm (spec §5, R22/R24): the element streams the tier's Range route, so
// a clip starts and seeks without being downloaded whole, and no blob or object URL is ever
// made. Click to play with the browser's own controls; never autoplay, including when a
// thread is reopened.
export default function VideoPreview({ assetId, fileName }: RendererProps) {
  const { streamUrl } = useAssetSource();
  const src = streamUrl(assetId);
  const [failedSrc, setFailedSrc] = useState<string>();
  if (safeStreamSrc(src) === null || failedSrc === src) return <PreviewError />;
  return (
    <VideoPlayer
      src={src}
      title={fileName}
      ratio="auto"
      onMediaError={() => {
        setFailedSrc(src);
      }}
    />
  );
}
