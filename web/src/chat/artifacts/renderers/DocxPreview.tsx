import { useEffect, useRef, useState } from 'react';
import { useAssetContent } from './useAssetContent';
import { PreviewError, PreviewLoading, type RendererProps } from './PreviewStatus';

// .docx (D-07 / D-08): docx-preview (and its jszip dep) is loaded with a DYNAMIC import()
// INSIDE the effect, so it lands ONLY in this lazy chunk and never the cache-hot main
// bundle. The fetched Blob is handed to renderAsync, which injects the rendered document
// into our refs; className:'aura-docx' prefixes every injected style rule so nothing leaks
// to document.head (T-37B-12). Cleanup clears the body container so an asset switch or a
// re-render starts from a clean slate.
export default function DocxPreview({ assetId }: RendererProps) {
  const { data: blob, error } = useAssetContent(assetId, 'blob');
  const bodyRef = useRef<HTMLDivElement>(null);
  const styleRef = useRef<HTMLDivElement>(null);
  const [renderError, setRenderError] = useState<string>();

  useEffect(() => {
    const body = bodyRef.current;
    if (blob === undefined || body === null) return;
    const controller = new AbortController();
    void (async () => {
      const { renderAsync } = await import('docx-preview');
      if (controller.signal.aborted) return;
      await renderAsync(blob, body, styleRef.current ?? undefined, {
        className: 'aura-docx',
        inWrapper: true,
        ignoreLastRenderedPageBreak: true,
      });
    })().catch((e: unknown) => {
      if (!controller.signal.aborted) setRenderError(String(e));
    });
    return () => {
      controller.abort();
      body.innerHTML = '';
    };
  }, [blob]);

  if (error !== undefined) return <PreviewError detail={error} />;
  if (renderError !== undefined) return <PreviewError detail={renderError} />;
  if (blob === undefined) return <PreviewLoading />;
  return (
    <div className="h-full overflow-auto bg-white p-6 text-black">
      <div ref={styleRef} />
      <div ref={bodyRef} className="aura-docx" />
    </div>
  );
}
