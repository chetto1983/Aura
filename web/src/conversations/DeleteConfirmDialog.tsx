import { useTranslation } from 'react-i18next';
import { ConfirmDialog } from '@/components/ui/confirm-dialog';

// DeleteConfirmDialog gates the D-07 hard delete (T-25-14): "Delete permanently"
// never fires Store.Delete without this confirm. The shared ConfirmDialog is
// portal-backed, focus-trapped, Escape-dismissable, and always centered.

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

  return (
    <ConfirmDialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) {
          onCancel();
        }
      }}
      title={t('conversations.delete.title')}
      description={t('conversations.delete.body', { title })}
      cancelLabel={t('conversations.delete.cancel')}
      confirmLabel={t('conversations.delete.confirm')}
      onConfirm={onConfirm}
    />
  );
}
