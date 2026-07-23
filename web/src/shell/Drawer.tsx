import { useEffect, useRef, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { focusFirstDescendant, trapTabKey } from '../a11y/focusTrap';
import { useScrollLock } from './useScrollLock';
import { Button } from '@/components/ui/button';

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
  const onCloseRef = useRef(onClose);
  useScrollLock(open);

  useEffect(() => {
    onCloseRef.current = onClose;
  }, [onClose]);

  useEffect(() => {
    if (!open) return;
    const panel = panelRef.current;
    const returnFocus =
      document.activeElement instanceof HTMLElement ? document.activeElement : null;
    focusFirstDescendant(panel);

    function onKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        event.preventDefault();
        onCloseRef.current('explicit');
        return;
      }
      trapTabKey(event, panel);
    }

    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('keydown', onKeyDown);
      returnFocus?.focus();
    };
  }, [open]);

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
        className={`absolute top-0 flex h-[100dvh] w-[min(22rem,88vw)] flex-col overflow-hidden border-border bg-surface shadow-[var(--shadow-drawer)] ${
          side === 'left' ? 'left-0 border-r' : 'right-0 border-l'
        }`}
      >
        <div className="flex min-h-14 items-center justify-between gap-3 border-b border-border px-3">
          <h2 className="font-display text-base text-text">{title}</h2>
          <Button
            type="button"
            variant="ghost"
            size="icon"
            onClick={() => {
              onClose('explicit');
            }}
            aria-label={t('shell.closePanel')}
            className="text-text-muted hover:bg-surface-2 hover:text-text"
          >
            <X data-icon="icon" aria-hidden="true" focusable="false" />
          </Button>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto">{children}</div>
      </div>
    </div>,
    document.body,
  );
}
