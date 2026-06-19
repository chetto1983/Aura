import { useEffect, useRef, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { useTranslation } from 'react-i18next';
import { focusFirstDescendant, trapTabKey } from '../a11y/focusTrap';
import { useScrollLock } from './useScrollLock';

/**
 * §3.1c distinguishes an explicit dismiss (close button / Esc / backdrop tap → restore the
 * remembered nav) from a swipe-dismiss (do NOT restore). The Drawer only ever originates
 * explicit closes; the swipe path lives in the edge-swipe handler. The arg is optional so
 * existing call sites stay backward-compatible.
 */
export type DrawerCloseIntent = 'explicit' | 'swipe';

export interface DrawerProps {
  readonly open: boolean;
  readonly title: string;
  readonly side: 'left' | 'right';
  readonly onClose: (intent?: DrawerCloseIntent) => void;
  readonly children: ReactNode;
}

export function Drawer({ open, title, side, onClose, children }: DrawerProps) {
  const { t } = useTranslation();
  const panelRef = useRef<HTMLDivElement>(null);
  useScrollLock(open);

  useEffect(() => {
    if (!open) return;
    const panel = panelRef.current;
    focusFirstDescendant(panel);

    function onKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        event.preventDefault();
        onClose('explicit');
        return;
      }
      trapTabKey(event, panel);
    }

    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [open, onClose]);

  if (!open) return null;

  return createPortal(
    <div className="fixed inset-0 z-50 lg:hidden" role="presentation">
      <button
        type="button"
        aria-label={t('shell.closePanel')}
        className="absolute inset-0 cursor-default bg-bg/70 backdrop-blur-sm"
        onClick={() => {
          onClose('explicit');
        }}
      />
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        className={`absolute top-0 flex h-[100svh] w-[min(22rem,88vw)] flex-col overflow-hidden border-border bg-surface shadow-[var(--shadow-drawer)] ${
          side === 'left' ? 'left-0 border-r' : 'right-0 border-l'
        }`}
      >
        <div className="flex min-h-14 items-center justify-between gap-3 border-b border-border px-3">
          <h2 className="font-display text-base text-text">{title}</h2>
          <button
            type="button"
            onClick={() => {
              onClose('explicit');
            }}
            aria-label={t('shell.closePanel')}
            className="flex min-h-10 min-w-10 items-center justify-center rounded-[var(--radius-md)] text-text-muted outline-none hover:bg-surface-2 hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
          >
            <span aria-hidden="true">x</span>
          </button>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto">{children}</div>
      </div>
    </div>,
    document.body,
  );
}
