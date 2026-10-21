import type { DiffValue, DocumentRow, Facets, FieldsDiff, FilterField } from './documentsApi';
import { VALUE_FIELD, valuesFor } from './documentsModel';

/** What the operator touched in the edit bar. A single field holds the label id to set on every
 * document, or null to clear it; absent leaves it alone. Tags hold a change per tag. */
export interface BulkPending {
  readonly type?: string | null;
  readonly counterparty?: string | null;
  readonly tags: Readonly<Record<string, 'add' | 'remove'>>;
  readonly custom?: Readonly<Record<string, string | null | undefined>>;
}

export const NO_PENDING: BulkPending = { tags: {} };

export type ValueState = 'all' | 'some' | 'none';

/** valueState is how many of the selected documents hold a value: the three states the edit
 * bar shows beside each one. */
export function valueState(
  rows: readonly DocumentRow[],
  field: FilterField,
  id: string,
): { readonly state: ValueState; readonly count: number } {
  const count = rows.filter((row) => valuesFor(row, field).some((v) => v.value_key === id)).length;
  const state: ValueState = count === 0 ? 'none' : count === rows.length ? 'all' : 'some';
  return { state, count };
}

/** The values a selection holds on a field, which clearing the field removes. */
export type HeldValues = (field: string) => readonly string[];

/** heldInRows reads them from the selected documents, when they are all loaded. */
export function heldInRows(rows: readonly DocumentRow[]): HeldValues {
  return (field) => [
    ...new Set(rows.flatMap((row) => valuesFor(row, field).map((v) => v.value_key))),
  ];
}

/** heldInFacets reads them from the counts of a filter's whole result: a filter selection's
 * documents are not all loaded. */
export function heldInFacets(facets: Facets): HeldValues {
  return (field) => Object.keys(facets[field] ?? {}).filter((id) => (facets[field]?.[id] ?? 0) > 0);
}

/** bulkDiff is the difference the edit bar sends: only the fields the operator touched. Clearing
 * a single field removes every value the selection holds on it. */
export function bulkDiff(pending: BulkPending, held: HeldValues): FieldsDiff {
  const set: DiffValue[] = [];
  const remove: DiffValue[] = [];
  for (const field of ['type', 'counterparty'] as const) {
    const target = pending[field];
    if (target === undefined) continue;
    if (target !== null) {
      set.push({ field: VALUE_FIELD[field], value_key: target });
      continue;
    }
    for (const key of [...held(field)].sort())
      remove.push({ field: VALUE_FIELD[field], value_key: key });
  }
  for (const [tag, change] of Object.entries(pending.tags)) {
    (change === 'add' ? set : remove).push({ field: VALUE_FIELD.tags, value_key: tag });
  }
  for (const [field, value] of Object.entries(pending.custom ?? {})) {
    if (value === undefined) continue;
    if (value === null) for (const key of held(field)) remove.push({ field, value_key: key });
    else if (value.trim()) set.push({ field, value_key: value.trim() });
  }
  return { set, remove };
}
