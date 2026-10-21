import { Check, ChevronDown, Minus, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { BulkPending, ValueState } from './bulkModel';
import type { CustomField, FilterField, Label } from './documentsApi';
import { BulkCustomFields } from './BulkCustomFields';
import { Button } from '@/components/ui/button';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';

export interface BulkBarProps {
  readonly customFields: readonly CustomField[];
  readonly canClearCustom: boolean;
  readonly count: number;
  /** The filter's whole result, offered when the selection is a list of ids smaller than it. */
  readonly filterTotal: number | null;
  readonly onSelectFilter: () => void;
  readonly onClear: () => void;
  readonly lists: Readonly<Record<FilterField, readonly Label[]>>;
  readonly stateOf: (
    field: FilterField,
    id: string,
  ) => { readonly state: ValueState; readonly count: number };
  readonly pending: BulkPending;
  readonly onPendingChange: (pending: BulkPending) => void;
  readonly canApply: boolean;
  readonly onApply: () => void;
}

const FIELD_LABEL: Readonly<Record<FilterField, string>> = {
  type: 'documents.columns.type',
  counterparty: 'documents.columns.counterparty',
  tags: 'documents.columns.tags',
};

/**
 * The edit bar that takes the filter bar's place while documents are selected (spec decision 4,
 * mockup 2): each value with its three states on the selection, and Apply, which sends only what
 * the operator touched.
 */
export function BulkBar(props: BulkBarProps) {
  const { count, filterTotal, onSelectFilter, onClear, canApply, onApply } = props;
  const { t } = useTranslation();
  return (
    <div
      role="toolbar"
      aria-label={t('documents.bulk.bar')}
      className="flex flex-wrap items-center gap-2 border-b border-border bg-text px-3 py-2 text-bg"
    >
      <span className="px-1 font-bold">{t('documents.bulk.selected', { count })}</span>
      {filterTotal !== null && (
        <button
          type="button"
          className="min-h-[44px] px-1 text-sm underline underline-offset-2"
          onClick={onSelectFilter}
        >
          {t('documents.bulk.selectAll', { count: filterTotal })}
        </button>
      )}
      <span className="flex-1" />
      {(['type', 'counterparty', 'tags'] as const).map((field) => (
        <BulkFieldMenu key={field} field={field} {...props} />
      ))}
      <Button type="button" size="sm" disabled={!canApply} onClick={onApply}>
        {t('documents.bulk.apply')}
      </Button>
      <BulkCustomFields
        fields={props.customFields}
        pending={props.pending}
        onChange={props.onPendingChange}
        canClear={props.canClearCustom}
      />
      <Button
        type="button"
        size="sm"
        variant="ghost"
        aria-label={t('documents.bulk.clearSelection')}
        className="text-bg hover:text-text"
        onClick={onClear}
      >
        <X aria-hidden="true" />
      </Button>
    </div>
  );
}

function BulkFieldMenu({
  field,
  count,
  lists,
  stateOf,
  pending,
  onPendingChange,
}: BulkBarProps & { readonly field: FilterField }) {
  const { t } = useTranslation();
  const label = t(FIELD_LABEL[field]);
  const single = field !== 'tags';
  const target = single ? pending[field] : undefined;

  function stateText(state: ValueState, held: number): string {
    if (state === 'all') return t('documents.bulk.onAll');
    if (state === 'none') return t('documents.bulk.onNone');
    return t('documents.bulk.onSome', { count: held, total: count });
  }

  // The change a click makes: a single field takes the value on every document, or nothing; a tag
  // cycles through the changes its state allows, and back to untouched.
  function toggle(id: string, state: ValueState) {
    if (single) {
      onPendingChange({ ...pending, [field]: target === id ? undefined : id });
      return;
    }
    const current = pending.tags[id];
    const order: readonly ('add' | 'remove' | undefined)[] =
      state === 'none'
        ? [undefined, 'add']
        : state === 'all'
          ? [undefined, 'remove']
          : [undefined, 'add', 'remove'];
    const next = order[(order.indexOf(current) + 1) % order.length];
    const tags = Object.fromEntries(
      Object.entries({ ...pending.tags, [id]: next }).filter(
        (entry): entry is [string, 'add' | 'remove'] => entry[1] !== undefined,
      ),
    );
    onPendingChange({ ...pending, tags });
  }

  function change(id: string): string {
    if (single) {
      if (target === id) return t('documents.bulk.toAll');
      return target !== undefined && stateOf(field, id).state !== 'none'
        ? t('documents.bulk.toNone')
        : '';
    }
    const tag = pending.tags[id];
    return tag === 'add'
      ? t('documents.bulk.toAll')
      : tag === 'remove'
        ? t('documents.bulk.toNone')
        : '';
  }

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="outline"
          size="sm"
          aria-label={label}
          className="bg-transparent text-bg"
        >
          {label}
          <ChevronDown aria-hidden="true" data-icon="inline-end" />
        </Button>
      </PopoverTrigger>
      <PopoverContent
        aria-label={t('documents.bulk.menu', { field: label, count })}
        align="end"
        className="flex w-80 flex-col gap-1 p-3"
      >
        <ul className="flex max-h-72 flex-col overflow-y-auto">
          {lists[field].map((l) => {
            const { state, count: held } = stateOf(field, l.id);
            return (
              <li key={l.id}>
                <BulkRow
                  name={l.name}
                  state={state}
                  detail={`${stateText(state, held)} ${change(l.id)}`.trim()}
                  touched={change(l.id) !== ''}
                  onClick={() => {
                    toggle(l.id, state);
                  }}
                />
              </li>
            );
          })}
          {single && (
            <li>
              <BulkRow
                name={t(
                  field === 'type' ? 'documents.bulk.noType' : 'documents.bulk.noCounterparty',
                )}
                state="none"
                detail={target === null ? t('documents.bulk.toAll') : ''}
                touched={target === null}
                onClick={() => {
                  onPendingChange({ ...pending, [field]: target === null ? undefined : null });
                }}
              />
            </li>
          )}
        </ul>
        <p className="border-t border-border pt-2 text-xs text-text-muted">
          {t('documents.bulk.legend')}
        </p>
      </PopoverContent>
    </Popover>
  );
}

function BulkRow(props: {
  readonly name: string;
  readonly state: ValueState;
  readonly detail: string;
  readonly touched: boolean;
  readonly onClick: () => void;
}) {
  const { name, state, detail, touched, onClick } = props;
  return (
    <button
      type="button"
      onClick={onClick}
      className={`flex min-h-[44px] w-full items-center gap-2 rounded-md px-1.5 text-left hover:bg-surface-3 ${
        touched ? 'bg-success/10' : ''
      }`}
    >
      <span aria-hidden="true" className="grid w-5 place-items-center">
        {state === 'all' ? (
          <Check className="size-4" />
        ) : state === 'some' ? (
          <Minus className="size-4" />
        ) : null}
      </span>
      <span className="flex-1 truncate">{name}</span>
      <span className={`text-xs ${touched ? 'font-semibold text-success' : 'text-text-muted'}`}>
        {detail}
      </span>
    </button>
  );
}
