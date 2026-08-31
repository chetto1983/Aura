import { useEffect, useState } from 'react';
import { useAssetContent } from './useAssetContent';
import { PreviewError, PreviewLoading, type RendererProps } from './PreviewStatus';

// .xlsx (D-07 / D-08 / T-37B-09): read-excel-file is DYNAMICALLY imported inside the effect,
// so it lands ONLY in this lazy chunk. The /universal entry behaves identically in the
// browser and in jsdom (no Web Worker spawn) and reads straight from the ArrayBuffer.
// Unlike SheetJS's sheet_to_html there is NO library-side HTML escaping to lean on: every
// cell value and sheet name goes through escapeHtml here, and the concatenated tables render
// inside an EMPTY-sandbox iframe (sandbox="" — no allow-scripts, no allow-same-origin), so
// even a missed escape cannot execute. Defense in depth: escaped values + escaped names +
// inert frame.

const SHEET_STYLE =
  'body{font:13px system-ui,sans-serif;margin:0;padding:12px;color:#111}' +
  'h3{font-size:12px;text-transform:uppercase;letter-spacing:.05em;color:#555;margin:16px 0 6px}' +
  'table{border-collapse:collapse;margin-bottom:8px}' +
  'td,th{border:1px solid #d0d0d0;padding:3px 8px;white-space:nowrap}';

/** Escape sheet names AND cell values before interpolation into the srcDoc. read-excel-file
 *  hands back raw values, so this escaping is load-bearing; the empty sandbox is the second
 *  layer, not the first. */
function escapeHtml(value: string): string {
  return value
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

// read-excel-file's d.ts types date cells as `typeof Date` (the constructor) but the runtime
// hands back Date INSTANCES — Cell is the runtime-truthful shape the cast at the call site
// restores.
type Cell = string | number | boolean | Date | null;

function cellText(value: Cell): string {
  if (value === null) return '';
  if (value instanceof Date) {
    const iso = value.toISOString();
    return iso.endsWith('T00:00:00.000Z') ? iso.slice(0, 10) : iso;
  }
  return String(value);
}

export default function XlsxPreview({ assetId, fileName }: RendererProps) {
  const { data: buffer, error } = useAssetContent(assetId, 'arrayBuffer');
  const [doc, setDoc] = useState<string>();
  const [parseError, setParseError] = useState<string>();

  useEffect(() => {
    if (buffer === undefined) return;
    const controller = new AbortController();
    void (async () => {
      const { default: readXlsxFile } = await import('read-excel-file/universal');
      const sheets = await readXlsxFile(buffer);
      if (controller.signal.aborted) return;
      const body = sheets
        .map(({ sheet, data }) => {
          const rows = data
            .map(
              (row) =>
                `<tr>${row.map((cell) => `<td>${escapeHtml(cellText(cell as Cell))}</td>`).join('')}</tr>`,
            )
            .join('');
          const table = rows === '' ? '' : `<table>${rows}</table>`;
          return `<section><h3>${escapeHtml(sheet)}</h3>${table}</section>`;
        })
        .join('');
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
