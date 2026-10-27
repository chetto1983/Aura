import { useContext, useEffect, useRef, useState, type RefObject } from 'react';
import { useTranslation } from 'react-i18next';
import type { TFunction } from 'i18next';
import { Maximize2, Minimize2 } from 'lucide-react';
import { useAssetSource } from './assetSourceContext';
import { useAssetContent } from './useAssetContent';
import { PreviewError, PreviewErrorWithDownload, type RendererProps } from './PreviewStatus';
import { PreviewAction } from './PreviewAction';
import { previewActionClass } from './previewActionsContext';
import { PreviewCloseGuardContext } from './previewCloseGuardContext';

// A Craft app (docs/superpowers/specs/2026-10-19-craft-web-apps-design.md) aura serves at
// /apps/<app>/ from the image. Framed in host mode it announces <app>:ready, and the file goes to
// it as bytes, read through the asset source so the share page's token-scoped route works
// unchanged. The frame is same-origin because the app answers only its own origin's parent, so
// only messages from THIS frame on THIS origin are heard. A save comes back as <app>:save: where
// the source can write (the file manager's), it goes over the file under its own name, or beside
// it under a new one; elsewhere the frame says that nothing made in it is stored. Where a save is
// stored, an edit the app reports as <app>:dirty is not in the file yet: the frame says so, holds
// the host's first close (PreviewCloseGuardContext) and the page's unload. A frame that has
// not said ready after a while (a 404 outside the image, a wasm the browser refuses) gets the
// original file offered over it; the frame stays, so an app that starts late on a slow link
// still takes over.

export type CraftApp =
  'gridcraft' | 'pdfcraft' | 'wordcraft' | 'deckcraft' | 'photocraft' | 'filmcraft';

// WordCraft's wasm is 15 MB gzipped: over a remote link of a few Mbit/s it takes longer than 15 s.
const READY_WITHIN_MS = 30_000;

interface CraftMessage {
  readonly type?: string;
  readonly error?: string;
  readonly dirty?: boolean;
  readonly name?: string;
  readonly bytes?: ArrayBuffer;
}

type SaveState =
  | { readonly kind: 'none' }
  | { readonly kind: 'unsaved' }
  | { readonly kind: 'pending' }
  | { readonly kind: 'saved'; readonly name: string | undefined }
  | { readonly kind: 'failed'; readonly message: string };

interface CraftFrameProps extends RendererProps {
  readonly app: CraftApp;
}

interface Notice {
  readonly role: 'alert' | 'status';
  readonly tone: 'bg-danger' | 'bg-warning';
  readonly text: string;
}

/** What the frame's corner says: a refused save, then an edit not yet saved, then the last save,
 *  which the chat's tier reports as attached to the message rather than saved to the library. */
function noticeOf(
  t: TFunction,
  saving: SaveState,
  dirty: boolean,
  held: boolean,
  intoMessage: boolean,
): Notice | null {
  if (saving.kind === 'failed') {
    const text = t('artifacts.preview.saveFailed', { message: saving.message });
    return { role: 'alert', tone: 'bg-danger', text };
  }
  if (saving.kind === 'pending')
    return { role: 'status', tone: 'bg-warning', text: t('artifacts.preview.saving') };
  if (dirty && held) {
    return { role: 'alert', tone: 'bg-warning', text: t('artifacts.preview.closeDiscards') };
  }
  if (dirty) {
    return { role: 'status', tone: 'bg-warning', text: t('artifacts.preview.unsavedChanges') };
  }
  if (saving.kind === 'none') return null;
  if (saving.kind === 'saved' && saving.name)
    return {
      role: 'status',
      tone: 'bg-warning',
      text: t(intoMessage ? 'artifacts.preview.attachedAs' : 'artifacts.preview.savedAs', {
        name: saving.name,
      }),
    };
  const saved = intoMessage ? 'artifacts.preview.attached' : 'artifacts.preview.saved';
  const key = saving.kind === 'saved' ? saved : 'artifacts.preview.notSaved';
  return { role: 'status', tone: 'bg-warning', text: t(key) };
}

export default function CraftFrame({ app, assetId, fileName }: CraftFrameProps) {
  const { t } = useTranslation();
  const root = useRef<HTMLDivElement>(null);
  const frame = useRef<HTMLIFrameElement>(null);
  const { data, error } = useAssetContent(assetId, 'arrayBuffer');
  const [ready, setReady] = useState(false);
  const [late, setLate] = useState(false);
  const [failed, setFailed] = useState<string>();
  const [saving, setSaving] = useState<SaveState>({ kind: 'none' });
  const [dirty, setDirty] = useState(false);
  const retry = useRef<(() => void) | null>(null);
  const { save, savesInto } = useAssetSource();
  const guard = useContext(PreviewCloseGuardContext);
  const setUnsaved = guard?.setUnsaved;

  useEffect(() => {
    const controller = new AbortController();
    const active = () => !controller.signal.aborted;
    const queue: { bytes: ArrayBuffer; name: string | undefined }[] = [];
    let writing = false;
    let blocked = false;
    const drain = async () => {
      if (writing || blocked || !active() || !save) return;
      writing = true;
      try {
        while (queue.length && active()) {
          const item = queue[0];
          if (!item) break;
          setSaving({ kind: 'pending' });
          try {
            await save(assetId, item.bytes, item.name, controller.signal);
          } catch (err) {
            if (active()) {
              blocked = true;
              setSaving({
                kind: 'failed',
                message: err instanceof Error ? err.message : String(err),
              });
            }
            return;
          }
          if (!active()) return;
          queue.shift();
          if (!queue.length) setSaving({ kind: 'saved', name: item.name });
        }
      } finally {
        writing = false;
      }
    };
    retry.current = () => {
      blocked = false;
      void drain();
    };
    const onMessage = (event: MessageEvent<CraftMessage | null>) => {
      if (
        event.source !== frame.current?.contentWindow ||
        event.origin !== window.location.origin
      ) {
        return;
      }
      const message = event.data ?? {};
      if (message.type === `${app}:ready`) setReady(true);
      else if (message.type === `${app}:failed`) setFailed(message.error ?? '');
      else if (message.type === `${app}:save` && save !== undefined && message.bytes) {
        const saveAs = message.name === fileName ? undefined : message.name;
        queue.push({ bytes: message.bytes, name: saveAs });
        void drain();
      } else if (save !== undefined && message.type === `${app}:dirty`) {
        setDirty(message.dirty === true);
      } else if (save === undefined && (message.type === `${app}:save` || message.dirty === true)) {
        setSaving({ kind: 'unsaved' });
      }
    };
    window.addEventListener('message', onMessage);
    return () => {
      controller.abort();
      queue.length = 0;
      window.removeEventListener('message', onMessage);
    };
  }, [app, assetId, fileName, save]);

  const unsaved = dirty || saving.kind === 'pending' || saving.kind === 'failed';
  useEffect(() => {
    if (!unsaved) return;
    setUnsaved?.(true);
    const holdUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault();
    };
    window.addEventListener('beforeunload', holdUnload);
    return () => {
      window.removeEventListener('beforeunload', holdUnload);
      setUnsaved?.(false);
    };
  }, [unsaved, setUnsaved]);

  useEffect(() => {
    if (ready) return;
    const timer = window.setTimeout(() => {
      setLate(true);
    }, READY_WITHIN_MS);
    return () => {
      window.clearTimeout(timer);
    };
  }, [ready]);

  useEffect(() => {
    const target = frame.current?.contentWindow;
    if (!ready || data === undefined || target === null || target === undefined) return;
    // The bytes are transferred, so the frame gets a copy and the fetched buffer stays whole.
    const bytes = data.slice(0);
    target.postMessage({ type: `${app}:open`, name: fileName, bytes }, window.location.origin, [
      bytes,
    ]);
  }, [app, ready, data, fileName]);

  const problem = error ?? failed;
  if (problem !== undefined) return <PreviewError detail={problem} />;
  const notice = noticeOf(t, saving, dirty, guard?.held === true, savesInto === 'message');
  return (
    <div ref={root} className="relative h-full w-full bg-bg">
      {document.fullscreenEnabled ? <FullscreenAction target={root} /> : null}
      <iframe
        ref={frame}
        src={`/apps/${app}/?host=parent`}
        title={fileName}
        allow="clipboard-read; clipboard-write; fullscreen"
        className="h-full w-full border-0 bg-white"
      />
      {late && !ready ? (
        <div className="absolute inset-0 flex items-center justify-center bg-bg/90">
          <PreviewErrorWithDownload
            assetId={assetId}
            fileName={fileName}
            detail={t('artifacts.preview.notStarted')}
          />
        </div>
      ) : null}
      {notice === null ? null : (
        <p
          role={notice.role}
          className={`absolute right-3 bottom-3 rounded-md ${notice.tone} px-3 py-1.5 text-xs font-semibold text-bg shadow-lg`}
        >
          {notice.text}
          {saving.kind === 'failed' && (
            <button
              type="button"
              className="ml-3 min-h-[44px] underline"
              onClick={() => {
                retry.current?.();
              }}
            >
              {t('artifacts.preview.retrySave')}
            </button>
          )}
        </p>
      )}
    </div>
  );
}

// The apps' own full-screen buttons send egui's ViewportCommand::Fullscreen, which eframe 0.36's
// web backend leaves unhandled ("not implemented in web backend"), so the cockpit offers its own,
// in the host's header. The frame goes full screen with its save notices; Esc leaves it.
function FullscreenAction({ target }: { readonly target: RefObject<HTMLDivElement | null> }) {
  const { t } = useTranslation();
  const [full, setFull] = useState(false);
  useEffect(() => {
    const sync = () => {
      setFull(target.current !== null && document.fullscreenElement === target.current);
    };
    document.addEventListener('fullscreenchange', sync);
    return () => {
      document.removeEventListener('fullscreenchange', sync);
    };
  }, [target]);
  const Icon = full ? Minimize2 : Maximize2;
  return (
    <PreviewAction>
      <button
        type="button"
        className={previewActionClass}
        onClick={() => {
          if (full) void document.exitFullscreen();
          else void target.current?.requestFullscreen();
        }}
      >
        <Icon aria-hidden="true" className="size-4" />
        {t(full ? 'artifacts.preview.exitFullscreen' : 'artifacts.preview.fullscreen')}
      </button>
    </PreviewAction>
  );
}
