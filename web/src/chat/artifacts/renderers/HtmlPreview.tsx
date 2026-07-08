import { useAssetContent } from './useAssetContent';
import { PreviewError, PreviewLoading, type RendererProps } from './PreviewStatus';

// text/html (D-07 / WEBART-07 / T-37B-08): untrusted agent HTML renders in a NULL-ORIGIN
// iframe. sandbox="allow-scripts" WITHOUT the same-origin token makes the frame's origin null —
// scripts run, but document.cookie is empty, window.parent access throws (cross-origin), and
// fetch('/api/…') carries no ambient session, so the content cannot read our cookies/DOM or
// reach Garage. The bytes are fed via srcDoc (fetched text), NEVER src=downloadURL: the
// attachment Content-Disposition would force a download, and a top-level document served from
// our own origin would defeat the isolation entirely. Granting the same-origin token here is
// forbidden — it would let the sandboxed script drop its own sandbox.
export default function HtmlPreview({ assetId, fileName }: RendererProps) {
  const { data, error } = useAssetContent(assetId, 'text');
  if (error !== undefined) return <PreviewError detail={error} />;
  if (data === undefined) return <PreviewLoading />;
  return (
    <iframe
      srcDoc={data}
      sandbox="allow-scripts"
      title={fileName}
      className="h-full w-full border-0 bg-white"
    />
  );
}
