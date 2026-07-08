import { useEffect, useState } from 'react';

// useAssetContent — the shared same-origin fetch primitive for the text/html/docx/xlsx
// renderers (the object-URL image/pdf pair use useBlobPreview instead). One authenticated
// GET of the 37A route GET /api/assets/{id}/download, read as the requested body kind and
// aborted on unmount / asset change. Extracted so the four non-object-URL renderers don't
// duplicate the fetch+abort block (jscpd threshold 0). `kind` is a string literal, so the
// effect deps stay stable — no refetch loop from an inline reader closure.

export type AssetContentKind = 'text' | 'blob' | 'arrayBuffer';

interface Loaded {
  readonly id: string;
  readonly data?: unknown;
  readonly error?: string;
}

export function useAssetContent(assetId: string, kind: 'text'): { data?: string; error?: string };
export function useAssetContent(assetId: string, kind: 'blob'): { data?: Blob; error?: string };
export function useAssetContent(
  assetId: string,
  kind: 'arrayBuffer',
): { data?: ArrayBuffer; error?: string };
export function useAssetContent(
  assetId: string,
  kind: AssetContentKind,
): { data?: unknown; error?: string } {
  const [loaded, setLoaded] = useState<Loaded>();

  useEffect(() => {
    const controller = new AbortController();
    void (async () => {
      const res = await fetch(`/api/assets/${assetId}/download`, {
        credentials: 'same-origin',
        signal: controller.signal,
      });
      if (!res.ok) throw new Error(`HTTP ${String(res.status)}`);
      const data =
        kind === 'text'
          ? await res.text()
          : kind === 'blob'
            ? await res.blob()
            : await res.arrayBuffer();
      setLoaded({ id: assetId, data });
    })().catch((e: unknown) => {
      if (!controller.signal.aborted) setLoaded({ id: assetId, error: String(e) });
    });
    return () => {
      controller.abort();
    };
  }, [assetId, kind]);

  // Only surface a result that belongs to the CURRENT asset — a stale body from a prior
  // asset is never returned while its replacement is still loading.
  if (loaded?.id !== assetId) return {};
  if (loaded.error !== undefined) return { error: loaded.error };
  return { data: loaded.data };
}
