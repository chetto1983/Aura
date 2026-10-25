import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Checkbox } from '@/components/ui/checkbox';
import { ConfirmDialog } from '@/components/ui/confirm-dialog';
import { Label } from '@/components/ui/label';

// PluginRemoveDialog asks once before a plugin goes. Its data stays unless the operator ticks
// the box, and the box says where that data is: in the operator's own box, the only one this
// removal reaches. The board keys the dialog by plugin, so every opening starts unticked.

export interface PluginRemoveDialogProps {
  /** The plugin to remove; the dialog is open while it is set. */
  readonly name: string | undefined;
  readonly pending: boolean;
  readonly onConfirm: (deleteData: boolean) => void;
  readonly onCancel: () => void;
}

export function PluginRemoveDialog({
  name,
  pending,
  onConfirm,
  onCancel,
}: PluginRemoveDialogProps) {
  const { t } = useTranslation();
  const dataId = useId();
  const [deleteData, setDeleteData] = useState(false);

  return (
    <ConfirmDialog
      role="alertdialog"
      open={name !== undefined}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) onCancel();
      }}
      title={t('governance.plugins.remove.title', { name })}
      description={t('governance.plugins.remove.body')}
      cancelLabel={t('governance.plugins.remove.cancel')}
      confirmLabel={t('governance.plugins.remove.confirm')}
      confirmPending={pending}
      onConfirm={() => {
        onConfirm(deleteData);
      }}
    >
      <div className="flex items-center gap-1">
        <Checkbox
          id={dataId}
          checked={deleteData}
          disabled={pending}
          onCheckedChange={(checked) => {
            setDeleteData(checked === true);
          }}
        />
        <Label htmlFor={dataId} className="text-[13px] font-normal text-text">
          {t('governance.plugins.remove.deleteData')}
        </Label>
      </div>
    </ConfirmDialog>
  );
}
