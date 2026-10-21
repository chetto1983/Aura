import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import {
  fetchCustomFields,
  fetchLabels,
  type CustomField,
  type FilterField,
  type Label,
} from './documentsApi';
import { KIND_OF, labelNames, type LabelNames } from './documentsModel';
import { useCapabilities } from '@/admin/useAdmin';

const NO_LABELS: readonly Label[] = [];
const NO_FIELDS: readonly CustomField[] = [];

function useLabels(field: FilterField) {
  const { identityId } = useCapabilities();
  return useQuery({
    queryKey: ['documents-labels', KIND_OF[field], identityId],
    enabled: !!identityId,
    queryFn: ({ signal }) => fetchLabels(KIND_OF[field], signal),
  });
}

/** The operator's lists, the names behind their ids, and the custom fields. */
export interface DocumentLists {
  readonly lists: Readonly<Record<FilterField, readonly Label[]>>;
  readonly names: LabelNames;
  readonly customFields: readonly CustomField[];
}

/** useDocumentLists reads the operator's lists and custom fields, which the list and the
 * detail share through the query cache, and the names behind each label id. */
export function useDocumentLists(): DocumentLists & { readonly settled: boolean } {
  const { identityId } = useCapabilities();
  const types = useLabels('type');
  const counterparties = useLabels('counterparty');
  const tags = useLabels('tags');
  const customFields = useQuery({
    queryKey: ['documents-custom-fields', identityId],
    enabled: !!identityId,
    queryFn: ({ signal }) => fetchCustomFields(signal),
  });
  const lists = useMemo(
    () => ({
      type: types.data ?? NO_LABELS,
      counterparty: counterparties.data ?? NO_LABELS,
      tags: tags.data ?? NO_LABELS,
    }),
    [types.data, counterparties.data, tags.data],
  );
  const names = useMemo(() => labelNames(lists), [lists]);
  // Settled, not succeeded: a list that failed to load reads as empty rather than holding the
  // detail back for good.
  const settled = [types, counterparties, tags, customFields].every((query) => !query.isPending);
  return { lists, names, customFields: customFields.data ?? NO_FIELDS, settled };
}
