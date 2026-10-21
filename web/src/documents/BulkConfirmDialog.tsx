import { useTranslation } from 'react-i18next';
import type { BulkPending } from './bulkModel';
import type { CustomField, FilterField } from './documentsApi';
import type { LabelNames } from './documentsModel';
import { ConfirmDialog } from '@/components/ui/confirm-dialog';

interface BulkConfirmDialogProps {
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly pending: BulkPending;
  readonly customFields: readonly CustomField[];
  readonly names: LabelNames;
  readonly count: number;
  /** The count a filter selection holds now, when the server refused the confirmed one. */
  readonly changedCount: number | null;
  readonly error: string;
  readonly busy: boolean;
  readonly onConfirm: () => void;
}

const FIELD_LABEL: Readonly<Record<'type' | 'counterparty', string>> = {
  type: 'documents.columns.type',
  counterparty: 'documents.columns.counterparty',
};

/**
 * The confirmation before a bulk edit (mockup 2): it names every change and how many documents it
 * reaches, and says so again when a filter selection no longer holds the count confirmed.
 */
export function BulkConfirmDialog(props: BulkConfirmDialogProps) {
  const { open, onOpenChange, pending, names, count, changedCount, error, busy, onConfirm } = props;
  const { t } = useTranslation();
  const nameOf = (field: FilterField, id: string) => names[field].get(id) ?? id;
  const changes: string[] = [];
  for (const [field, value] of Object.entries(pending.custom ?? {})) {
    if (value === undefined) continue;
    const label = props.customFields.find((cf) => `custom:${cf.id}` === field)?.name ?? field;
    changes.push(
      t(value === null ? 'documents.bulk.clear' : 'documents.bulk.set', { field: label, value }),
    );
  }
  for (const field of ['type', 'counterparty'] as const) {
    const target = pending[field];
    const label = t(FIELD_LABEL[field]).toLowerCase();
    if (target === null) changes.push(t('documents.bulk.clear', { field: label }));
    else if (target !== undefined)
      changes.push(t('documents.bulk.set', { field: label, value: nameOf(field, target) }));
  }
  for (const [tag, change] of Object.entries(pending.tags)) {
    changes.push(
      t(change === 'add' ? 'documents.bulk.addTag' : 'documents.bulk.removeTag', {
        value: nameOf('tags', tag),
      }),
    );
  }

  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('documents.bulk.confirmTitle')}
      cancelLabel={t('documents.bulk.cancel')}
      confirmLabel={t('documents.bulk.confirm')}
      confirmVariant="default"
      confirmPending={busy}
      onConfirm={onConfirm}
    >
      {changedCount !== null && (
        <p role="status" className="text-sm font-semibold text-warning">
          {t('documents.bulk.countChanged', { count: changedCount })}
        </p>
      )}
      <ul className="list-disc pl-5 text-sm">
        {changes.map((line) => (
          <li key={line}>{line}</li>
        ))}
      </ul>
      <p className="text-sm font-semibold">{t('documents.bulk.onDocuments', { count })}</p>
      <p className="text-xs text-text-muted">{t('documents.bulk.history')}</p>
      {error !== '' && (
        <p role="alert" className="text-sm text-danger">
          {error}
        </p>
      )}
    </ConfirmDialog>
  );
}
