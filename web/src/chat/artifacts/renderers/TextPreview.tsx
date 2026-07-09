import { useAssetContent } from './useAssetContent';
import { PreviewError, PreviewLoading, type RendererProps } from './PreviewStatus';

// text/* + the code/data extension family (D-07): the raw bytes are shown as
// React-escaped monospace inside a <pre>. Never parsed as markdown or HTML — React
// entity-escapes the exact bytes, so there is zero injection surface (T-37B-10).
export default function TextPreview({ assetId }: RendererProps) {
  const { data, error } = useAssetContent(assetId, 'text');
  if (error !== undefined) return <PreviewError detail={error} />;
  if (data === undefined) return <PreviewLoading />;
  return (
    <pre className="h-full overflow-auto whitespace-pre-wrap break-words p-4 font-mono text-sm text-text">
      {data}
    </pre>
  );
}
