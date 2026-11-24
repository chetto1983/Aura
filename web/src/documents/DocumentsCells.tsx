import { useContext } from 'react';
import { Check } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { ICellProps } from '@svar-ui/react-grid';
import { sourceOf, type DocumentRow, type FieldValue, type FilterField } from './documentsApi';
import {
  DocumentsCellContext,
  FILTER_VALUE_ACTION,
  OPEN_DOCUMENT_ACTION,
} from './documentsCellContext';
import { displayDate, PROPOSED_PREFIX, valuesFor } from './documentsModel';

type CellAction = ICellProps['onAction'];

function SourceMark({ value }: { readonly value: FieldValue }) {
  const { t } = useTranslation();
  const source = sourceOf(value.set_by);
  if (source === null || value.accepted) return null;
  return (
    <span
      title={value.evidence}
      className="ml-1.5 shrink-0 rounded-md border border-warning/50 px-1.5 text-[11px] leading-5 text-warning"
    >
      {t(`documents.source.${source}`)}
    </span>
  );
}

/** A label value: its name, a click that filters by it, and the mark of its source. */
function LabelValue({
  value,
  field,
  onAction,
}: {
  readonly value: FieldValue;
  readonly field: FilterField;
  readonly onAction: CellAction;
}) {
  const { t } = useTranslation();
  const names = useContext(DocumentsCellContext);
  if (value.value_key.startsWith(PROPOSED_PREFIX)) {
    return (
      <span className="text-text-muted">
        {t('documents.proposed', { name: value.value_key.slice(PROPOSED_PREFIX.length) })}
      </span>
    );
  }
  const name = names?.[field].get(value.value_key) ?? t('documents.unknownLabel');
  return (
    <span className="inline-flex min-w-0 items-center">
      <button
        type="button"
        data-action="ignore-click"
        className="truncate text-left text-info underline-offset-2 hover:underline focus-visible:outline-2 focus-visible:outline-ring"
        onClick={() => {
          onAction({ action: FILTER_VALUE_ACTION, data: { field, id: value.value_key } });
        }}
      >
        {name}
      </button>
      <SourceMark value={value} />
    </span>
  );
}

/** The document's name, which opens its detail. */
export function NameCell({ row, onAction }: ICellProps) {
  const doc = row as unknown as DocumentRow;
  return (
    <button
      type="button"
      data-action="ignore-click"
      className="truncate text-left font-semibold text-text underline-offset-2 hover:underline focus-visible:outline-2 focus-visible:outline-ring"
      onClick={() => {
        onAction({ action: OPEN_DOCUMENT_ACTION, data: { id: doc.id } });
      }}
    >
      {doc.file_name}
    </button>
  );
}

/** The type or the counterparty, as the column's id names it. */
export function FieldCell({ row, column, onAction }: ICellProps) {
  const field = column.id as FilterField;
  const [value] = valuesFor(row as unknown as DocumentRow, field);
  if (value === undefined) return <span className="text-text-muted">—</span>;
  return <LabelValue value={value} field={field} onAction={onAction} />;
}

export function TagsCell({ row, onAction }: ICellProps) {
  return (
    <span className="flex min-w-0 flex-wrap gap-1">
      {valuesFor(row as unknown as DocumentRow, 'tags').map((value) => (
        <span key={value.value_key} className="rounded-md bg-surface-3 px-1.5">
          <LabelValue value={value} field="tags" onAction={onAction} />
        </span>
      ))}
    </span>
  );
}

export function DateCell({ row }: ICellProps) {
  const { i18n } = useTranslation();
  const [value] = valuesFor(row as unknown as DocumentRow, 'document_date');
  if (value === undefined) return <span className="text-text-muted">—</span>;
  return (
    <span className="inline-flex items-center tabular-nums">
      {displayDate(value.value_key, i18n.language)}
      <SourceMark value={value} />
    </span>
  );
}

/** A custom field's value, the column's id being "custom:<id>". */
export function CustomCell({ row, column }: ICellProps) {
  const [value] = valuesFor(row as unknown as DocumentRow, String(column.id));
  if (value === undefined) return <span className="text-text-muted">—</span>;
  const shown =
    typeof value.value === 'string' || typeof value.value === 'number'
      ? value.value
      : value.value_key;
  return (
    <span className="inline-flex items-center">
      {String(shown)}
      <SourceMark value={value} />
    </span>
  );
}

export function StatusCell({ row }: ICellProps) {
  const { t } = useTranslation();
  const doc = row as unknown as DocumentRow;
  if (doc.needs_review)
    return <span className="font-bold text-warning">{t('documents.status.review')}</span>;
  if (doc.reviewed_at === undefined) return null;
  return (
    <span className="inline-flex items-center gap-1 text-success">
      <Check aria-hidden="true" className="size-4" />
      {t('documents.status.reviewed')}
    </span>
  );
}
