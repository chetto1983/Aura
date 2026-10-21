import { useCallback, useMemo, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import {
  bulkDiff,
  heldInFacets,
  heldInRows,
  NO_PENDING,
  valueState,
  type BulkPending,
  type ValueState,
} from './bulkModel';
import {
  bulkApply,
  CountChangedError,
  errorMessage,
  isEmptyDiff,
  type DocumentRow,
  type DocumentsFilter,
  type Facets,
  type FilterField,
} from './documentsApi';
import type { SelectionApi } from './documentsCellContext';
import { useCapabilities } from '@/admin/useAdmin';

/** The documents ticked, or every document of the filter minus the ones unticked. */
type Selection =
  | { readonly kind: 'ids'; readonly rows: ReadonlyMap<string, DocumentRow> }
  | { readonly kind: 'filter'; readonly exclude: readonly string[] }
  | null;

interface ConfirmState {
  readonly open: boolean;
  readonly expected: number;
  readonly changedCount: number | null;
  readonly error: string;
  readonly busy: boolean;
}

const CLOSED: ConfirmState = {
  open: false,
  expected: 0,
  changedCount: null,
  error: '',
  busy: false,
};

interface BulkEditInput {
  readonly rows: readonly DocumentRow[];
  readonly total: number;
  readonly filter: DocumentsFilter;
  readonly facets: Facets;
}

/**
 * useBulkEdit holds a bulk edit from selection to write: what is selected, what the operator
 * touched, the confirmation, and the request. A filter selection carries the count the operator
 * confirmed, and a changed count asks again with the new one.
 */
export function useBulkEdit({ rows, total, filter, facets }: BulkEditInput) {
  const { t } = useTranslation();
  const { identityId } = useCapabilities();
  const queryClient = useQueryClient();
  const [selection, setSelection] = useState<Selection>(null);
  const [pending, setPending] = useState<BulkPending>(NO_PENDING);
  const [confirm, setConfirm] = useState<ConfirmState>(CLOSED);
  const resultKey = JSON.stringify(filter);
  const [selectedResult, setSelectedResult] = useState(resultKey);
  if (selectedResult !== resultKey) {
    setSelectedResult(resultKey);
    setSelection(null);
    setPending(NO_PENDING);
    setConfirm(CLOSED);
  }

  const count =
    selection === null
      ? 0
      : selection.kind === 'ids'
        ? selection.rows.size
        : total - selection.exclude.length;

  const clear = useCallback(() => {
    setSelection(null);
    setPending(NO_PENDING);
    setConfirm(CLOSED);
  }, []);

  const isSelected = useCallback(
    (id: string) =>
      selection !== null &&
      (selection.kind === 'ids' ? selection.rows.has(id) : !selection.exclude.includes(id)),
    [selection],
  );

  const toggle = useCallback((row: DocumentRow) => {
    setSelection((current) => {
      if (current?.kind === 'filter') {
        const exclude = current.exclude.includes(row.id)
          ? current.exclude.filter((id) => id !== row.id)
          : [...current.exclude, row.id];
        return { kind: 'filter', exclude };
      }
      const next = new Map(current?.rows ?? []);
      if (next.has(row.id)) next.delete(row.id);
      else next.set(row.id, row);
      return next.size === 0 ? null : { kind: 'ids', rows: next };
    });
  }, []);

  const onPage = rows.filter((row) => isSelected(row.id)).length;
  const pageState: SelectionApi['pageState'] =
    rows.length > 0 && onPage === rows.length ? 'all' : onPage > 0 ? 'some' : 'none';

  const selectionApi = useMemo<SelectionApi>(
    () => ({
      isSelected,
      toggle,
      pageState,
      togglePage: () => {
        for (const row of rows) {
          if ((pageState === 'all') === isSelected(row.id)) toggle(row);
        }
      },
    }),
    [isSelected, toggle, pageState, rows],
  );

  const selectedRows = selection?.kind === 'ids' ? [...selection.rows.values()] : [];
  const stateOf = (field: FilterField, id: string): { state: ValueState; count: number } => {
    if (selection?.kind === 'ids') return valueState(selectedRows, field, id);
    const held = Math.min(facets[field]?.[id] ?? 0, count);
    return { state: held === 0 ? 'none' : held >= count ? 'all' : 'some', count: held };
  };
  const diff = bulkDiff(
    pending,
    selection?.kind === 'ids' ? heldInRows(selectedRows) : heldInFacets(facets),
  );

  async function send(expected: number) {
    if (selection === null) return;
    setConfirm((c) => ({ ...c, busy: true, error: '' }));
    try {
      await bulkApply(
        selection.kind === 'ids'
          ? { kind: 'ids', ids: [...selection.rows.keys()] }
          : { kind: 'filter', filter, exclude: selection.exclude, expectedCount: expected },
        diff,
      );
      clear();
      await queryClient.invalidateQueries({ queryKey: ['documents', identityId] });
      await queryClient.invalidateQueries({ queryKey: ['documents-review'] });
    } catch (error) {
      if (error instanceof CountChangedError) {
        setConfirm({
          open: true,
          expected: error.count,
          changedCount: error.count,
          error: '',
          busy: false,
        });
        return;
      }
      const message = errorMessage(error);
      setConfirm((c) => ({ ...c, busy: false, error: t('documents.bulk.failed', { message }) }));
    }
  }

  return {
    active: selection !== null,
    clear,
    selectionApi,
    bar: {
      canClearCustom: selection?.kind === 'ids',
      count,
      filterTotal: selection?.kind === 'ids' && total > count ? total : null,
      onSelectFilter: () => {
        setSelection({ kind: 'filter', exclude: [] });
        setPending((current) => ({
          ...current,
          custom: Object.fromEntries(
            Object.entries(current.custom ?? {}).filter(([, value]) => value !== null),
          ),
        }));
      },
      onClear: clear,
      stateOf,
      pending,
      onPendingChange: setPending,
      canApply:
        !isEmptyDiff(diff) &&
        !Object.values(pending.custom ?? {}).some(
          (value) => typeof value === 'string' && !value.trim(),
        ),
      onApply: () => {
        setConfirm({ ...CLOSED, open: true, expected: count });
      },
    },
    confirm: {
      open: confirm.open,
      onOpenChange: (open: boolean) => {
        if (!open) setConfirm(CLOSED);
      },
      pending,
      count: confirm.expected,
      changedCount: confirm.changedCount,
      error: confirm.error,
      busy: confirm.busy,
      onConfirm: () => {
        void send(confirm.expected);
      },
    },
  };
}
