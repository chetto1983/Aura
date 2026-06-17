import { useEffect, useId, useRef } from 'react';
import { useTranslation } from 'react-i18next';

// DeleteConfirmDialog gates the D-07 hard delete (T-25-14): "Delete permanently"
// never fires Store.Delete without this confirm. It is a native <dialog> opened
// with showModal() so the browser supplies the focus trap + backdrop + Esc-cancel
// for free; we layer the UI-SPEC a11y contract on top:
//   • confirm is NOT the default focus — Cancel ("Keep conversation") is, so an
//     accidental Enter keeps the conversation (safe default).
//   • Esc cancels (native <dialog> "cancel" event → onCancel).
//   • focus returns to the trigger on close (we restore the previously-focused el).
//   • the danger confirm carries aria-describedby pointing at the body text.

export interface DeleteConfirmDialogProps {
  readonly open: boolean;
  /** The conversation title interpolated into the body copy. */
  readonly title: string;
  /** Confirmed the permanent delete. */
  readonly onConfirm: () => void;
  /** Cancelled (button, backdrop, or Esc). */
  readonly onCancel: () => void;
}

export function DeleteConfirmDialog({
  open,
  title,
  onConfirm,
  onCancel,
}: DeleteConfirmDialogProps) {
  const { t } = useTranslation();
  const dialogRef = useRef<HTMLDialogElement>(null);
  const cancelRef = useRef<HTMLButtonElement>(null);
  const bodyId = useId();
  // Remember what had focus before we opened so we can restore it on close (WCAG
  // 2.4.3 focus order — the operator returns to the row they acted on).
  const restoreRef = useRef<HTMLElement | null>(null);

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (open) {
      if (!dialog.open) {
        restoreRef.current = document.activeElement as HTMLElement | null;
        dialog.showModal();
        // Default focus on Cancel (the safe action), NOT on the danger confirm.
        cancelRef.current?.focus();
      }
    } else if (dialog.open) {
      dialog.close();
      restoreRef.current?.focus();
      restoreRef.current = null;
    }
  }, [open]);

  return (
    <dialog
      ref={dialogRef}
      aria-labelledby={`${bodyId}-title`}
      aria-describedby={bodyId}
      // Native cancel (Esc / backdrop) → keep the conversation.
      onCancel={(event) => {
        event.preventDefault();
        onCancel();
      }}
      className="max-w-sm rounded-[var(--radius-lg)] border border-border bg-surface p-4 text-text backdrop:bg-black/50"
    >
      <h2 id={`${bodyId}-title`} className="text-base font-medium text-text">
        {t('conversations.delete.title')}
      </h2>
      <p id={bodyId} className="mt-2 text-sm text-text-muted">
        {t('conversations.delete.body', { title })}
      </p>
      <div className="mt-4 flex justify-end gap-2">
        <button
          ref={cancelRef}
          type="button"
          onClick={onCancel}
          className="min-h-[44px] rounded-[var(--radius-md)] border border-border px-3 text-sm text-text outline-none hover:bg-surface-2 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
        >
          {t('conversations.delete.cancel')}
        </button>
        <button
          type="button"
          onClick={onConfirm}
          aria-describedby={bodyId}
          className="min-h-[44px] rounded-[var(--radius-md)] bg-danger px-3 text-sm font-medium text-[#0B0E14] outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
        >
          {t('conversations.delete.confirm')}
        </button>
      </div>
    </dialog>
  );
}
