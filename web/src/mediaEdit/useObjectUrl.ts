import { useEffect, useState } from 'react';

interface Minted {
  readonly blob: Blob;
  readonly url: string;
}

/** An object URL for `blob`, or `undefined` until the component has committed: callers draw
 *  nothing (or their loading state) until then.
 *
 *  The URL is minted in an effect, never during render. A render React throws away — Strict
 *  Mode's second pass, an abandoned concurrent render — would otherwise mint a URL nobody
 *  revokes, pinning the whole blob for the life of the tab. The effect's cleanup revokes what
 *  that effect minted. */
export function useObjectUrl(blob: Blob): string | undefined {
  const [minted, setMinted] = useState<Minted>();
  useEffect(() => {
    const url = URL.createObjectURL(blob);
    let current = true;
    // Published outside the effect body, like useBlobPreview: a run already cleaned up (Strict
    // Mode's first effect) publishes nothing.
    queueMicrotask(() => {
      if (current) setMinted({ blob, url });
    });
    return () => {
      current = false;
      URL.revokeObjectURL(url);
    };
  }, [blob]);
  // A URL minted for an earlier blob is already revoked: never hand it out.
  return minted?.blob === blob ? minted.url : undefined;
}
