import { useEffect, useState } from 'react';
import { useAssetContent } from './useAssetContent';
import { PreviewError, PreviewLoading, type RendererProps } from './PreviewStatus';

// .xlsx (D-07 / D-08 / T-37B-09): SheetJS is DYNAMICALLY imported inside the effect, so it
// lands ONLY in this lazy chunk. Each sheet becomes an HTML <table> via sheet_to_html
// (which escapes cell VALUES); we additionally escape the sheet NAME ourselves. The
// concatenated tables render inside an EMPTY-sandbox iframe (sandbox="" — no allow-scripts,
// no allow-same-origin), so even if the library's cell escaping ever regressed, nothing in
// the frame can execute. Defense in depth: escaped values + escaped names + inert frame.

const SHEET_STYLE =
  'body{font:13px system-ui,sans-serif;margin:0;padding:12px;color:#111}' +
  'h3{font-size:12px;text-transform:uppercase;letter-spacing:.05em;color:#555;margin:16px 0 6px}' +
  'table{border-collapse:collapse;margin-bottom:8px}' +
  'td,th{border:1px solid #d0d0d0;padding:3px 8px;white-space:nowrap}';

/** Escape the sheet NAME before it is interpolated into the srcDoc (sheet_to_html already
 *  escapes cell values). The empty sandbox makes this belt-and-braces, not load-bearing. */
function escapeHtml(value: string): string {
  return value
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

export default function XlsxPreview({ assetId, fileName }: RendererProps) {
  const { data: buffer, error } = useAssetContent(assetId, 'arrayBuffer');
  const [doc, setDoc] = useState<string>();
  const [parseError, setParseError] = useState<string>();

  useEffect(() => {
    if (buffer === undefined) return;
    const controller = new AbortController();
    void (async () => {
      const { read, utils } = await import('xlsx');
      if (controller.signal.aborted) return;
      const wb = read(buffer, { type: 'array' });
      const body = wb.SheetNames.map((name) => {
        const sheet = wb.Sheets[name];
        const table = sheet ? utils.sheet_to_html(sheet) : '';
        return `<section><h3>${escapeHtml(name)}</h3>${table}</section>`;
      }).join('');
      setDoc(`<!doctype html><meta charset="utf-8"><style>${SHEET_STYLE}</style>${body}`);
    })().catch((e: unknown) => {
      if (!controller.signal.aborted) setParseError(String(e));
    });
    return () => {
      controller.abort();
    };
  }, [buffer]);

  if (error !== undefined) return <PreviewError detail={error} />;
  if (parseError !== undefined) return <PreviewError detail={parseError} />;
  if (doc === undefined) return <PreviewLoading />;
  return (
    <iframe srcDoc={doc} sandbox="" title={fileName} className="h-full w-full border-0 bg-white" />
  );
}
