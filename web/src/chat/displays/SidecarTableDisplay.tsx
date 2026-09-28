import { useTranslation } from 'react-i18next';
import { TableDisplay } from './TableDisplay';
import type { DisplayPayload } from './types';
import { sidecarTableSpec } from './sidecarTableSpec';

export function SidecarTableDisplay({ payload }: { readonly payload: DisplayPayload }) {
  const { t } = useTranslation();
  const table = payload.table;
  const spec = sidecarTableSpec(payload.title);
  if (!table || !spec) return null;
  return (
    <TableDisplay
      payload={{
        table: {
          ...table,
          columns: spec.columns.map((key) => t(`display.sidecar.column.${key}`)),
        },
      }}
      label={t(`display.sidecar.${spec.title}`)}
    />
  );
}
