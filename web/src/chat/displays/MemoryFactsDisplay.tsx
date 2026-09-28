import { useTranslation } from 'react-i18next';
import { TableDisplay } from './TableDisplay';
import type { DisplayPayload } from './types';

const FACT_COLUMNS = [
  'fact',
  'subject',
  'relation',
  'object',
  'valid',
  'sources',
  'factKey',
] as const;
const ENTITY_COLUMNS = ['entity', 'class', 'kind', 'facts'] as const;

export function MemoryFactsDisplay({ payload }: { readonly payload: DisplayPayload }) {
  const { t } = useTranslation();
  const table = payload.table;
  if (!table) return null;
  const entities = payload.title === 'memory_entities';
  const keys = entities ? ENTITY_COLUMNS : FACT_COLUMNS;
  const columns = keys.map((key) => t(`display.memory.column.${key}`));
  return (
    <TableDisplay
      payload={{ table: { ...table, columns } }}
      label={t(entities ? 'display.memory.entities' : 'display.memory.facts')}
    />
  );
}
