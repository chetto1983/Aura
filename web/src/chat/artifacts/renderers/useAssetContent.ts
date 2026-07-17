import { useEffect, useState } from 'react';
import { useAssetSource } from './assetSourceContext';

// useAssetContent — the shared fetch primitive for the text/html/docx/xlsx renderers (the
// object-URL image/pdf pair use useBlobPreview instead). One GET through useAssetSource()'s
// resolved URL + credentials (the 37A identity-scoped asset route by default; a token-scoped
// public route once a provider is mounted — R-05), read as the requested body kind and
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
  const { assetUrl, credentials } = useAssetSource();

  useEffect(() => {
    const controller = new AbortController();
    void (async () => {
      const res = await fetch(assetUrl(assetId), {
        credentials,
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
  }, [assetId, kind, assetUrl, credentials]);

  // Only surface a result that belongs to the CURRENT asset — a stale body from a prior
  // asset is never returned while its replacement is still loading.
  if (loaded?.id !== assetId) return {};
  if (loaded.error !== undefined) return { error: loaded.error };
  return { data: loaded.data };
}
