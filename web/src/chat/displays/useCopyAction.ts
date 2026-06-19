import { useCallback, useEffect, useRef, useState } from 'react';

// useCopyAction — a tiny clipboard hook shared by the copy controls (Copy table,
// and any future Copy code). It writes text to the clipboard and flips a transient
// `copied` flag for ~2s (the shared "Copied" confirmation state, Copywriting
// Contract), reverting to the idle label afterward. The timeout is cleared on
// unmount (no setState-after-unmount leak).
//
// navigator.clipboard is feature-detected: a write failure (or an absent API in a
// non-secure context) is swallowed silently — copy is a best-effort convenience,
// never a hard error path for the operator.

const CONFIRM_MS = 2000;

export function useCopyAction(): { copied: boolean; copy: (text: string) => void } {
  const [copied, setCopied] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(
    () => () => {
      if (timer.current !== null) clearTimeout(timer.current);
    },
    [],
  );

  const copy = useCallback((text: string) => {
    const flash = () => {
      setCopied(true);
      if (timer.current !== null) clearTimeout(timer.current);
      timer.current = setTimeout(() => {
        setCopied(false);
      }, CONFIRM_MS);
    };
    const clip = navigator.clipboard as Clipboard | undefined;
    if (clip?.writeText !== undefined) {
      clip.writeText(text).then(flash, () => {
        // Swallow: copy is best-effort; no operator-facing error.
      });
    }
  }, []);

  return { copied, copy };
}
