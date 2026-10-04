import { useCallback, useRef, useState, type MouseEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { isIOSHomeScreenApp } from '@/lib/installedApp';

// useSaveFile — what a download link does in iOS's home-screen app. Everywhere else the link
// downloads as it always did. There a download navigates to iOS's file page, which has no way
// back and may sit in a browser with no session (prd.md §3), so the link fetches the bytes
// itself and hands them to the share sheet ("Save to Files") instead: the sheet closes onto
// the cockpit.
//
// The share sheet needs the tap's user activation, and an activation does not reliably
// survive a fetch: WebKit says so with this exact case (webkit.org/blog/13862). So the first
// tap fetches and shares only while the activation is still live; when it has lapsed the link
// turns into "Save" and the second tap shares the bytes already in hand.

export type SaveState = 'idle' | 'preparing' | 'ready' | 'failed';

// Declared optional because a browser may lack them; lib.dom types them as always present.
type SharingNavigator = Partial<Pick<Navigator, 'canShare' | 'share' | 'userActivation'>>;

function sharingNavigator(): SharingNavigator {
  return navigator;
}

function canShareFile(fileName: string, mimeType: string): boolean {
  const { canShare } = sharingNavigator();
  if (canShare === undefined) return false;
  return canShare.call(navigator, { files: [new File([], fileName, { type: mimeType })] });
}

interface Held {
  readonly href: string;
  readonly state: SaveState;
}

export function useSaveFile(
  href: string,
  fileName: string,
  mimeType: string,
  credentials: RequestCredentials = 'same-origin',
): { state: SaveState; onClick: (event: MouseEvent<HTMLAnchorElement>) => void } {
  const [held, setHeld] = useState<Held>({ href, state: 'idle' });
  const file = useRef<{ href: string; file: File } | undefined>(undefined);
  // A state left by another file is not this one's: the link may be reused for a new href.
  const state = held.href === href ? held.state : 'idle';

  const share = useCallback((target: string, ready: File) => {
    const { share: open } = sharingNavigator();
    if (open === undefined) {
      // canShare without share: no sheet to hand the bytes to, and the link must say so.
      file.current = undefined;
      setHeld({ href: target, state: 'failed' });
      return;
    }
    open.call(navigator, { files: [ready] }).then(
      () => {
        file.current = undefined;
        setHeld({ href: target, state: 'idle' });
      },
      (err: unknown) => {
        const reason = err instanceof DOMException ? err.name : '';
        // AbortError is the person closing the sheet; NotAllowedError is a lapsed activation,
        // which the next tap renews.
        if (reason === 'NotAllowedError') {
          setHeld({ href: target, state: 'ready' });
          return;
        }
        file.current = undefined;
        setHeld({ href: target, state: reason === 'AbortError' ? 'idle' : 'failed' });
      },
    );
  }, []);

  const onClick = useCallback(
    (event: MouseEvent<HTMLAnchorElement>) => {
      if (file.current?.href === href) {
        event.preventDefault();
        share(href, file.current.file);
        return;
      }
      if (!isIOSHomeScreenApp() || !canShareFile(fileName, mimeType)) return;
      event.preventDefault();
      if (state === 'preparing') return;
      setHeld({ href, state: 'preparing' });
      void fetch(href, { credentials })
        .then((res) => {
          if (!res.ok) throw new Error(`HTTP ${String(res.status)}`);
          return res.blob();
        })
        .then((blob) => {
          const ready = new File([blob], fileName, { type: mimeType || blob.type });
          file.current = { href, file: ready };
          if (sharingNavigator().userActivation?.isActive === true) {
            share(href, ready);
            return;
          }
          setHeld({ href, state: 'ready' });
        })
        .catch(() => {
          setHeld({ href, state: 'failed' });
        });
    },
    [credentials, fileName, href, mimeType, share, state],
  );

  return { state, onClick };
}

const STATE_LABEL = {
  preparing: 'artifacts.save.preparing',
  ready: 'artifacts.save.ready',
  failed: 'artifacts.save.failed',
} as const;

/** The label a link shows while it is not plainly "download": undefined when idle. */
export function useSaveLabel(state: SaveState): string | undefined {
  const { t } = useTranslation();
  return state === 'idle' ? undefined : t(STATE_LABEL[state]);
}
