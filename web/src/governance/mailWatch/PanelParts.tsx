import { useTranslation } from 'react-i18next';
import { errorSentence } from './mailWatchLogic';
import { SkeletonBlock } from '@/components/skeleton';
import { ConfirmDialog } from '@/components/ui/confirm-dialog';

// PanelParts.tsx is what the rules, labels and settings panels share: their heading, the shape a
// list takes while it loads, and the confirmation in front of a removal.

export function PanelHeading({
  title,
  intro,
}: {
  readonly title: string;
  readonly intro?: string;
}) {
  return (
    <div>
      <h3 className="font-display text-[20px] leading-tight text-text">{title}</h3>
      {intro === undefined ? null : <p className="mt-1 text-[13px] text-text-muted">{intro}</p>}
    </div>
  );
}

export function PanelPending() {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col gap-2">
      <p role="status" className="sr-only">
        {t('governance.mailWatch.panelLoading')}
      </p>
      {[0, 1, 2].map((row) => (
        <SkeletonBlock key={row} height={52} />
      ))}
    </div>
  );
}

interface RemoveConfirmProps {
  readonly open: boolean;
  readonly title: string;
  readonly description: string;
  readonly cancelLabel: string;
  readonly confirmLabel: string;
  readonly pending: boolean;
  readonly error: Error | null;
  readonly onCancel: () => void;
  readonly onConfirm: () => void;
}

export function RemoveConfirm({
  open,
  title,
  description,
  cancelLabel,
  confirmLabel,
  pending,
  error,
  onCancel,
  onConfirm,
}: RemoveConfirmProps) {
  const { t } = useTranslation();
  return (
    <ConfirmDialog
      open={open}
      onOpenChange={(next) => {
        if (!next) onCancel();
      }}
      title={title}
      description={description}
      cancelLabel={cancelLabel}
      confirmLabel={confirmLabel}
      confirmPending={pending}
      onConfirm={onConfirm}
    >
      {error === null ? null : (
        <p role="alert" className="text-[13px] text-danger">
          {errorSentence(error, t)}
        </p>
      )}
    </ConfirmDialog>
  );
}
