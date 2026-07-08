import { useBlobPreview } from '../useBlobPreview';
import { PreviewError, PreviewLoading, type RendererProps } from './PreviewStatus';

// application/pdf: browsers render PDF natively, so an <iframe src={objectURL}> shows the
// built-in viewer at zero bundle cost (no pdf.js). The object URL is same-origin-but-opaque
// (a blob:), and srcDoc cannot carry binary PDF bytes — src={objectURL} is the correct feed.
export default function PdfPreview({ assetId, mimeType, fileName }: RendererProps) {
  const { url, error } = useBlobPreview(assetId, mimeType);
  if (error !== undefined) return <PreviewError detail={error} />;
  if (url === undefined) return <PreviewLoading />;
  return <iframe src={url} title={fileName} className="h-full w-full border-0 bg-white" />;
}
