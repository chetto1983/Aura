import { Suspense, lazy, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { DownloadCloud, PackageOpen, X } from 'lucide-react';
import type { Asset } from '../attachments/types';
import { ArtifactRow } from './ArtifactRow';
import { downloadAll } from './downloadAll';
import { useThreadArtifacts } from './useThreadArtifacts';

// ArtifactsPanel (D-13/D-17/WEBART-05/06): the self-contained "Artefatti" surface.
// Props are exactly { threadId, onClose } so the AppShell (plan 07) mounts it — as a
// ResizablePanel on desktop or a right Drawer on mobile — without re-deriving any
// contract. It reads the durable derived view (useThreadArtifacts: agent-only,
// newest-first over the identity-scoped list), renders one ArtifactRow per row, a
// considered empty-state, a throttled "Scarica tutto" bulk download with N/M
// progress (skips degraded rows, disabled during the run, aborted on unmount/thread
// switch), and a LAZILY-mounted PreviewModal for the active row — so docx-preview/
// xlsx and the other renderer chunks stay out of the panel's static import graph
// until the user first opens a preview.

// PreviewModal is a NAMED export; map it to the default shape React.lazy expects.
// The dynamic import() keeps the modal + its six lazy renderer chunks off the
// panel's static graph until an artifact is actually previewed (D-08).
const PreviewModal = lazy(() =>
  import('./PreviewModal').then((m) => ({ default: m.PreviewModal })),
);

export interface ArtifactsPanelProps {
  readonly threadId: string;
  /** Collapse the panel (desktop toggle) or dismiss the drawer (mobile). */
  readonly onClose: () => void;
}

interface Progress {
  readonly done: number;
  readonly total: number;
}

export function ArtifactsPanel({ threadId, onClose }: ArtifactsPanelProps) {
  const { t } = useTranslation();
  const { data, isLoading } = useThreadArtifacts(threadId);
  const rows = data ?? [];
  const acceptedCount = rows.reduce((n, a) => (a.status === 'accepted' ? n + 1 : n), 0);

  const [active, setActive] = useState<Asset | undefined>(undefined);
  const [progress, setProgress] = useState<Progress | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  const running = progress !== null;

  // Abort an in-flight bulk download when the thread changes or the panel unmounts,
  // so a stale run never keeps firing clicks against the previous conversation.
  useEffect(() => {
    return () => {
      abortRef.current?.abort();
      abortRef.current = null;
    };
  }, [threadId]);

  async function handleDownloadAll() {
    if (running || acceptedCount === 0) return;
    const controller = new AbortController();
    abortRef.current = controller;
    setProgress({ done: 0, total: acceptedCount });
    try {
      await downloadAll(
        rows,
        (done, total) => {
          setProgress({ done, total });
        },
        { signal: controller.signal },
      );
    } finally {
      abortRef.current = null;
      setProgress(null);
    }
  }

  return (
    <section
      aria-label={t('artifacts.title')}
      className="flex h-full min-h-0 flex-col gap-3 bg-surface px-3 pb-3 pt-3"
    >
      <header className="flex shrink-0 items-center gap-2">
        <PackageOpen aria-hidden="true" className="size-4 text-accent-text" strokeWidth={1.75} />
        <h2 className="flex-1 truncate text-[0.75rem] font-semibold uppercase tracking-[0.14em] text-text-faint">
          {t('artifacts.title')}
        </h2>
        <button
          type="button"
          onClick={onClose}
          aria-label={t('artifacts.toggleAria')}
          className="grid size-7 shrink-0 cursor-pointer place-items-center rounded-md text-text-faint transition-colors hover:bg-surface-3 hover:text-text focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
        >
          <X className="size-4" />
        </button>
      </header>

      <button
        type="button"
        onClick={() => {
          void handleDownloadAll();
        }}
        disabled={running || acceptedCount === 0}
        className="group flex shrink-0 items-center justify-center gap-2 rounded-lg border border-accent/40 bg-surface-2/60 px-3 py-2 text-[0.8125rem] font-medium text-accent-text transition-colors hover:border-accent hover:bg-surface-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:cursor-not-allowed disabled:opacity-45 disabled:hover:border-accent/40 disabled:hover:bg-surface-2/60"
      >
        <DownloadCloud className="size-4 transition-transform group-hover:translate-y-px" />
        {progress !== null
          ? t('artifacts.downloadAllProgress', { done: progress.done, total: progress.total })
          : t('artifacts.downloadAll')}
      </button>

      <div className="min-h-0 flex-1 overflow-y-auto">
        {rows.length === 0 ? (
          isLoading ? null : (
            <EmptyState />
          )
        ) : (
          <ul className="flex flex-col gap-1.5">
            {rows.map((asset, index) => (
              <li
                key={asset.id}
                className="animate-in fade-in-0 slide-in-from-bottom-1 fill-mode-backwards"
                style={{ animationDelay: `${String(Math.min(index, 8) * 40)}ms` }}
              >
                <ArtifactRow asset={asset} onPreview={setActive} />
              </li>
            ))}
          </ul>
        )}
      </div>

      {active !== undefined && (
        <Suspense fallback={null}>
          <PreviewModal
            active={active}
            onClose={() => {
              setActive(undefined);
            }}
          />
        </Suspense>
      )}
    </section>
  );
}

/** The considered zero-state: a soft glyph plate + the localized empty copy, so an
 *  empty thread reads as intentional rather than broken. */
function EmptyState() {
  const { t } = useTranslation();
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 px-4 text-center">
      <span className="grid size-14 place-items-center rounded-2xl bg-surface-2/60 text-text-faint ring-1 ring-border">
        <PackageOpen className="size-6" strokeWidth={1.5} />
      </span>
      <div className="flex flex-col gap-1">
        <p className="text-[0.8125rem] font-medium text-text-muted">{t('artifacts.empty')}</p>
        <p className="max-w-[28ch] text-[0.75rem] text-text-faint">{t('artifacts.emptyHint')}</p>
      </div>
    </div>
  );
}
