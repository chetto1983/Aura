import { useState } from 'react';
import { useBlobPreview } from '../useBlobPreview';
import { PreviewError, PreviewLoading, type RendererProps } from './PreviewStatus';
import { ToolImage } from '@/components/tool-ui-image';

// image/* (except SVG, which previewKind gates to 'download' — T-37B-05): the
// relabelled blob URL feeds a plain <img>. A raster image can't execute, so no parser
// and no sandbox are needed; object-fit keeps large images inside the modal viewport.
export default function ImagePreview({ assetId, mimeType, fileName }: RendererProps) {
  const { url, error } = useBlobPreview(assetId, mimeType);
  const [failedUrl, setFailedUrl] = useState<string>();
  if (error !== undefined || (url !== undefined && failedUrl === url)) {
    return <PreviewError {...(error === undefined ? {} : { detail: error })} />;
  }
  if (url === undefined) return <PreviewLoading />;
  return (
    <ToolImage
      id={assetId}
      src={url}
      alt={fileName}
      ratio="auto"
      className="mx-auto"
      onError={() => {
        setFailedUrl(url);
      }}
    />
  );
}
