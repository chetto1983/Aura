import { useRef, useState } from 'react';
import { PowerOff, Trash2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../../components/Spinner';
import { FailureLine } from './ReasonNotice';
import type { RemoteAccessStatusDTO } from './remoteAccessApi';
import { useStepAction } from './useStepAction';
import { Button } from '@/components/ui/button';
import { ConfirmDialog } from '@/components/ui/confirm-dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

export interface ManageActionsProps {
  readonly status: RemoteAccessStatusDTO;
  readonly onDisable: () => Promise<void>;
  readonly onDelete: (hostname: string) => Promise<void>;
}

export function ManageActions({ status, onDisable, onDelete }: ManageActionsProps) {
  const { t } = useTranslation();
  const disable = useStepAction();
  const remove = useStepAction();
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [confirmation, setConfirmation] = useState('');
  const deleteTrigger = useRef<HTMLButtonElement>(null);
  const hostname = status.public_hostname ?? '';

  function closeDelete(open: boolean) {
    setDeleteOpen(open);
    if (!open) {
      setConfirmation('');
      remove.clear();
      requestAnimationFrame(() => deleteTrigger.current?.focus());
    }
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap gap-2 [&>[data-slot=button]]:w-full sm:[&>[data-slot=button]]:w-auto">
        {status.enabled ? (
          <Button
            type="button"
            variant="outline"
            disabled={disable.running}
            onClick={() => void disable.run(onDisable)}
          >
            {disable.running ? <Spinner /> : <PowerOff aria-hidden="true" />}
            {t('remoteAccess.phone.disable')}
          </Button>
        ) : null}
        {status.has_resources && hostname !== '' ? (
          <Button
            ref={deleteTrigger}
            type="button"
            variant="outline"
            className="border-danger text-danger hover:bg-danger/10"
            onClick={() => {
              setDeleteOpen(true);
            }}
          >
            <Trash2 aria-hidden="true" />
            {t('remoteAccess.phone.delete')}
          </Button>
        ) : null}
      </div>
      {disable.failure === null ? null : <FailureLine failure={disable.failure} />}
      <ConfirmDialog
        open={deleteOpen}
        onOpenChange={closeDelete}
        role="alertdialog"
        title={t('remoteAccess.delete.title')}
        description={t('remoteAccess.delete.body')}
        cancelLabel={t('remoteAccess.delete.cancel')}
        confirmLabel={t('remoteAccess.delete.confirm')}
        confirmDisabled={confirmation !== hostname}
        confirmPending={remove.running}
        onConfirm={async () => {
          if (await remove.run(() => onDelete(confirmation))) closeDelete(false);
        }}
      >
        {remove.failure === null ? null : <FailureLine failure={remove.failure} />}
        <div className="grid gap-1.5">
          <Label htmlFor="remote-access-delete-confirm">
            {t('remoteAccess.delete.label', { hostname })}
          </Label>
          <Input
            id="remote-access-delete-confirm"
            value={confirmation}
            onChange={(event) => {
              setConfirmation(event.target.value);
            }}
            autoCapitalize="none"
            autoComplete="off"
          />
        </div>
      </ConfirmDialog>
    </div>
  );
}
