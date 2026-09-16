import { useEffect, useState } from 'react';
import { useAssetContent } from './renderers/useAssetContent';

// useBlobPreview — the object-URL lifecycle hook for the image/pdf preview renderers
// (D-06/WEBART-05). The bytes come from useAssetContent(assetId, 'blob'), which owns the
// fetch through useAssetSource()'s resolved URL + credentials, its AbortController and its
// stale-asset guard. This hook only re-labels the current blob with the SSE mime_type (so
// <img>/<iframe> sniff the right type) and hands it out as a blob: object URL, minting one
// URL per blob and revoking it when the blob or the mime changes or the component unmounts
// (Pitfall 5 / T-37B-11): no leaked blob: entries, no stale-file flash. Video streams its
// asset URL directly and never touches this hook.

export interface BlobPreview {
  /** The blob: object URL for the relabelled asset, once fetched. */
  readonly url?: string;
  /** A human-readable error when the authenticated fetch failed. */
  readonly error?: string;
}

interface Minted {
  readonly source: Blob;
  readonly mimeType: string | undefined;
  readonly url: string;
}

export function useBlobPreview(assetId: string, mimeType?: string): BlobPreview {
  const { data, error } = useAssetContent(assetId, 'blob');
  const [minted, setMinted] = useState<Minted>();

  useEffect(() => {
    if (data === undefined) return;
    const url = URL.createObjectURL(mimeType ? new Blob([data], { type: mimeType }) : data);
    let current = true;
    // Publish outside the effect body, like durationFormat's settle capture: the URL is
    // minted and owned by this run, whose cleanup revokes it.
    queueMicrotask(() => {
      if (current) setMinted({ source: data, mimeType, url });
    });
    return () => {
      current = false;
      URL.revokeObjectURL(url);
    };
  }, [data, mimeType]);

  if (error !== undefined) return { error };
  // A URL minted for another blob or another mime is already revoked: never surface it.
  if (minted === undefined || minted.source !== data || minted.mimeType !== mimeType) return {};
  return { url: minted.url };
}
