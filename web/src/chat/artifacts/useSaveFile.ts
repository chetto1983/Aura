import { useCallback, useRef, useState, type MouseEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { isIOSHomeScreenApp } from '@/lib/installedApp';

// What a download does in iOS's home-screen app, for one file or several. Everywhere else a
// download stays what it always was. There it navigates to iOS's file page, which has no way
// back and may sit in a browser with no session (prd.md §3), so the bytes are fetched here and
// handed to the share sheet ("Save to Files") instead -- several files in ONE sheet -- and the
// sheet closes onto the cockpit.
//
// The share sheet needs the tap's user activation, and an activation does not reliably
// survive a fetch: WebKit says so with this exact case (webkit.org/blog/13862). So the first
// tap fetches and shares only while the activation is still live; when it has lapsed the
// control turns into "Save" and the second tap shares the bytes already in hand.

export type SaveState = 'idle' | 'preparing' | 'ready' | 'failed';

/** One file to save: where its bytes are, its name, and its media type ('' when unknown). */
export interface SaveLink {
  readonly href: string;
  readonly fileName: string;
  readonly mimeType: string;
}

/** The last files the share path took, and where it stands with them. */
export interface HeldSave {
  readonly links: readonly SaveLink[];
  readonly state: SaveState;
}

// Declared optional because a browser may lack them; lib.dom types them as always present.
type SharingNavigator = Partial<Pick<Navigator, 'canShare' | 'share' | 'userActivation'>>;

function sharingNavigator(): SharingNavigator {
  return navigator;
}

// The probes describe the real files: iOS decides by name and type whether the sheet takes them.
function canShareFiles(links: readonly SaveLink[]): boolean {
  const { canShare } = sharingNavigator();
  if (canShare === undefined) return false;
  const probes = links.map((link) => new File([], link.fileName, { type: link.mimeType }));
  return canShare.call(navigator, { files: probes });
}

/** Whether two lists name the same files in the same order: a state held for one list is not
 *  another's. */
export function sameLinks(a: readonly SaveLink[], b: readonly SaveLink[]): boolean {
  return a.length === b.length && a.every((link, index) => link.href === b[index]?.href);
}

async function fetchFile(link: SaveLink, credentials: RequestCredentials): Promise<File> {
  const res = await fetch(link.href, { credentials });
  if (!res.ok) throw new Error(`HTTP ${String(res.status)}`);
  const blob = await res.blob();
  return new File([blob], link.fileName, { type: link.mimeType || blob.type });
}

const IDLE: HeldSave = { links: [], state: 'idle' };

/**
 * The share-sheet path for any number of files. save(links) answers true when it took them --
 * iOS's home-screen app, and a sheet that accepts them -- and false when the caller should
 * download as it always did. save is stable, so a widget built once can keep calling it.
 */
export function useShareFiles(credentials: RequestCredentials = 'same-origin'): {
  held: HeldSave;
  save: (links: readonly SaveLink[]) => boolean;
} {
  const [held, setHeld] = useState<HeldSave>(IDLE);
  // The same state, read by save without becoming one of its dependencies.
  const current = useRef<HeldSave>(IDLE);
  const kept = useRef<{ links: readonly SaveLink[]; files: File[] } | undefined>(undefined);

  const hold = useCallback((next: HeldSave) => {
    current.current = next;
    setHeld(next);
  }, []);

  const share = useCallback(
    (links: readonly SaveLink[], files: File[]) => {
      const { share: open } = sharingNavigator();
      if (open === undefined) {
        // canShare without share: no sheet to hand the bytes to, and the control must say so.
        kept.current = undefined;
        hold({ links, state: 'failed' });
        return;
      }
      open.call(navigator, { files }).then(
        () => {
          kept.current = undefined;
          hold({ links, state: 'idle' });
        },
        (err: unknown) => {
          const reason = err instanceof DOMException ? err.name : '';
          // AbortError is the person closing the sheet; NotAllowedError is a lapsed activation,
          // which the next tap renews.
          if (reason === 'NotAllowedError') {
            hold({ links, state: 'ready' });
            return;
          }
          kept.current = undefined;
          hold({ links, state: reason === 'AbortError' ? 'idle' : 'failed' });
        },
      );
    },
    [hold],
  );

  const save = useCallback(
    (links: readonly SaveLink[]): boolean => {
      const ready = kept.current;
      if (ready !== undefined && sameLinks(ready.links, links)) {
        share(links, ready.files);
        return true;
      }
      if (!isIOSHomeScreenApp() || links.length === 0 || !canShareFiles(links)) return false;
      if (current.current.state === 'preparing' && sameLinks(current.current.links, links)) {
        return true;
      }
      hold({ links, state: 'preparing' });
      void Promise.all(links.map((link) => fetchFile(link, credentials)))
        .then((files) => {
          kept.current = { links, files };
          if (sharingNavigator().userActivation?.isActive === true) {
            share(links, files);
            return;
          }
          hold({ links, state: 'ready' });
        })
        .catch(() => {
          hold({ links, state: 'failed' });
        });
      return true;
    },
    [credentials, hold, share],
  );

  return { held, save };
}

/** useShareFiles for one download link: its click is taken over only when the sheet takes it. */
export function useSaveFile(
  href: string,
  fileName: string,
  mimeType: string,
  credentials: RequestCredentials = 'same-origin',
): { state: SaveState; onClick: (event: MouseEvent<HTMLAnchorElement>) => void } {
  const { held, save } = useShareFiles(credentials);
  // A state left by another file is not this one's: the link may be reused for a new href.
  const state = sameLinks(held.links, [{ href, fileName, mimeType }]) ? held.state : 'idle';
  const onClick = useCallback(
    (event: MouseEvent<HTMLAnchorElement>) => {
      if (save([{ href, fileName, mimeType }])) event.preventDefault();
    },
    [fileName, href, mimeType, save],
  );
  return { state, onClick };
}

const STATE_LABEL = {
  preparing: 'artifacts.save.preparing',
  ready: 'artifacts.save.ready',
  failed: 'artifacts.save.failed',
} as const;

/** The label a control shows while it is not plainly "download": undefined when idle. */
export function useSaveLabel(state: SaveState): string | undefined {
  const { t } = useTranslation();
  return state === 'idle' ? undefined : t(STATE_LABEL[state]);
}
