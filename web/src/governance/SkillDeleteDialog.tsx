import { useTranslation } from 'react-i18next';
import { ConfirmDialog } from '@/components/ui/confirm-dialog';

// SkillDeleteDialog gates the one irreversible action on the skills board. Delete is the
// only verb here that asks: archive is reversible and frequent, so it fires from the row
// with no ceremony, and if everything asked, nobody would read any of it.
//
// The copy names the skill and says what is lost, and it points at archive as the
// reversible alternative IN THE TEXT rather than as a third button — a dialog with two
// ways to say yes is a dialog you can agree to by accident. Focus opens on Cancel, Escape
// dismisses, and focus returns to the trigger (the shared ConfirmDialog's contract).

export interface SkillDeleteDialogProps {
  readonly open: boolean;
  /** The skill name interpolated into the title and body copy. */
  readonly name: string;
  readonly onConfirm: () => void;
  readonly onCancel: () => void;
  readonly pending: boolean;
}

export function SkillDeleteDialog({
  open,
  name,
  onConfirm,
  onCancel,
  pending,
}: SkillDeleteDialogProps) {
  const { t } = useTranslation();

  return (
    <ConfirmDialog
      role="alertdialog"
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) {
          onCancel();
        }
      }}
      title={t('governance.skills.delete.title', { name })}
      description={t('governance.skills.delete.body')}
      cancelLabel={t('governance.skills.delete.cancel')}
      confirmLabel={t('governance.skills.delete.confirm')}
      confirmPending={pending}
      onConfirm={onConfirm}
    />
  );
}
