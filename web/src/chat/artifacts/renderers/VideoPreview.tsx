import { useState } from 'react';
import { useAssetSource } from './assetSourceContext';
import { PreviewError, type RendererProps } from './PreviewStatus';
import { safeStreamSrc } from './safeStreamSrc';
import { VideoPlayer } from '@/components/media-player';

// video/mp4 and video/webm (spec §5, R22/R24): the element streams the tier's Range route, so
// a clip starts and seeks without being downloaded whole, and no blob or object URL is ever
// made. Click to play with the browser's own controls; never autoplay, including when a
// thread is reopened. A chat message keeps the player's card width; the preview modal passes
// className to size it to the clip instead.
export default function VideoPreview({
  assetId,
  fileName,
  className,
}: RendererProps & { readonly className?: string }) {
  const { streamUrl } = useAssetSource();
  const src = streamUrl(assetId);
  const [failedSrc, setFailedSrc] = useState<string>();
  if (safeStreamSrc(src) === null || failedSrc === src) return <PreviewError />;
  return (
    <VideoPlayer
      src={src}
      title={fileName}
      ratio="auto"
      {...(className === undefined ? {} : { className })}
      onMediaError={() => {
        setFailedSrc(src);
      }}
    />
  );
}
