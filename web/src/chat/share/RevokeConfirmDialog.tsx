import { useTranslation } from 'react-i18next';
import { ConfirmDialog } from '@/components/ui/confirm-dialog';

// RevokeConfirmDialog gates the D-01/T-37F-71 destructive, irreversible share revoke:
// "Revoke link" never fires shareApi.revokeShare without this confirm. Copied from
// web/src/conversations/DeleteConfirmDialog.tsx (~41 LOC) per the plan's explicit
// instruction, swapping its conversations.delete.* keys for the real share.revokeConfirm.*
// namespace (resources.share.ts) — share.revokeConfirm.body carries no interpolation
// placeholder, so unlike DeleteConfirmDialog this dialog needs no name/title prop. The shared
// ConfirmDialog is portal-backed, focus-trapped, Escape-dismissable, and always centered.

export interface RevokeConfirmDialogProps {
  readonly open: boolean;
  /** Confirmed the revoke. */
  readonly onConfirm: () => void;
  /** Cancelled (button, backdrop, or Esc) — the share modal returns to 'shared'. */
  readonly onCancel: () => void;
}

export function RevokeConfirmDialog({ open, onConfirm, onCancel }: RevokeConfirmDialogProps) {
  const { t } = useTranslation();

  return (
    <ConfirmDialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) {
          onCancel();
        }
      }}
      title={t('share.revokeConfirm.title')}
      description={t('share.revokeConfirm.body')}
      cancelLabel={t('share.modal.cancel')}
      confirmLabel={t('share.revokeConfirm.confirm')}
      onConfirm={onConfirm}
    />
  );
}
