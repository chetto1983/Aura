import { useBlobPreview } from '../useBlobPreview';
import { PreviewError, PreviewLoading, type RendererProps } from './PreviewStatus';

// image/* (except SVG, which previewKind gates to 'download' — T-37B-05): the
// relabelled blob URL feeds a plain <img>. A raster image can't execute, so no parser
// and no sandbox are needed; object-fit keeps large images inside the modal viewport.
export default function ImagePreview({ assetId, mimeType, fileName }: RendererProps) {
  const { url, error } = useBlobPreview(assetId, mimeType);
  if (error !== undefined) return <PreviewError detail={error} />;
  if (url === undefined) return <PreviewLoading />;
  return <img src={url} alt={fileName} className="mx-auto max-h-full max-w-full object-contain" />;
}
