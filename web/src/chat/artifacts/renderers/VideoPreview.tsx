import { useState } from 'react';
import { useAssetSource } from './assetSourceContext';
import { PreviewError, type RendererProps } from './PreviewStatus';

// video/mp4 and video/webm (spec §5, R22/R24): the element streams the tier's Range route, so
// a clip starts and seeks without being downloaded whole, and no blob or object URL is ever
// made. Click to play with the browser's own controls; never autoplay, including when a
// thread is reopened.
export default function VideoPreview({ assetId, fileName }: RendererProps) {
  const { streamUrl } = useAssetSource();
  const src = streamUrl(assetId);
  const [failedSrc, setFailedSrc] = useState<string>();
  if (failedSrc === src) return <PreviewError />;
  return (
    // Generated and delivered clips carry no caption track to point a <track> at.
    // eslint-disable-next-line jsx-a11y/media-has-caption
    <video
      src={src}
      controls
      playsInline
      preload="metadata"
      aria-label={fileName}
      onError={() => {
        setFailedSrc(src);
      }}
      // The element keeps the clip's own proportions: w-full with a capped height left a box
      // wider than the clip, which the browser filled with black bars on a short window (seen
      // in the cockpit on 2026-09-17). h-auto/w-auto inside the two maximums cannot letterbox.
      className="max-h-[70vh] h-auto w-auto max-w-full rounded-[var(--radius-md)] bg-surface-2"
    />
  );
}
