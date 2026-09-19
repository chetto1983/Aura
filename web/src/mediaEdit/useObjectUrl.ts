import { useEffect, useMemo, useRef } from 'react';

interface PendingRevoke {
  readonly url: string;
  readonly timer: ReturnType<typeof setTimeout>;
}

/** An object URL for `blob`, revoked when the blob changes or the component unmounts.
 *
 * Derived rather than stored: minting it in an effect and pushing it through setState costs a
 * paint with nothing in it. The revoke waits one task because Strict Mode runs every effect's
 * cleanup and then the effect again on the same memoised URL; that second run takes the pending
 * revoke back, so the URL on screen is never a dead one. (Strict Mode also calls the memo twice
 * and drops one URL unrevoked — a development-only leak React's double render imposes.) */
export function useObjectUrl(blob: Blob): string {
  const url = useMemo(() => URL.createObjectURL(blob), [blob]);
  const pendingRevoke = useRef<PendingRevoke>(undefined);
  useEffect(() => {
    if (pendingRevoke.current?.url === url) clearTimeout(pendingRevoke.current.timer);
    return () => {
      pendingRevoke.current = {
        url,
        timer: setTimeout(() => {
          URL.revokeObjectURL(url);
        }, 0),
      };
    };
  }, [url]);
  return url;
}
