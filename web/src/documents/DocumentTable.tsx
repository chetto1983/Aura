import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Grid, Willow, WillowDark, type IApi } from '@svar-ui/react-grid';
import { Locale } from '@svar-ui/react-core';
import { useTranslation } from 'react-i18next';
import '@/styles/svarWidgets';
import { documentsWords } from './documentsLocale';
import { documentColumns } from './documentsColumns';
import { readWidths, saveWidth } from './documentsPrefs';
import {
  DocumentsCellContext,
  SelectionContext,
  type SelectionApi,
  type FilterValueEvent,
  type OpenDocumentEvent,
} from './documentsCellContext';
import type { CustomField, DocumentRow, FilterField } from './documentsApi';
import type { LabelNames } from './documentsModel';
import { useThemeMode } from '@/theme/useThemeMode';

interface Props {
  readonly rows: readonly DocumentRow[];
  readonly names: LabelNames;
  readonly customFields: readonly CustomField[];
  readonly shownFields: readonly string[];
  readonly selection: SelectionApi;
  readonly sort: string;
  readonly onSort: (sort: string) => void;
  readonly onOpen: (id: string) => void;
  readonly onFilter: (field: FilterField, id: string) => void;
}
export function DocumentTable({
  rows,
  names,
  customFields,
  shownFields,
  selection,
  sort,
  onSort,
  onOpen,
  onFilter,
}: Props) {
  const { t, i18n } = useTranslation();
  const [widths, setWidths] = useState(readWidths);
  const Theme = useThemeMode() === 'light' ? Willow : WillowDark;
  const columns = useMemo(
    () => documentColumns(t, customFields, shownFields, true, widths),
    [t, customFields, shownFields, widths],
  );
  const sortRef = useRef({ sort, onSort });
  useEffect(() => {
    sortRef.current = { sort, onSort };
  }, [sort, onSort]);
  const init = useCallback((api: IApi) => {
    api.intercept('sort-rows', (event) => {
      const key = String(event.key);
      if (key === 'file_name' || key === 'document_date')
        sortRef.current.onSort(sortRef.current.sort === key ? `-${key}` : key);
      return false;
    });
    api.on('resize-column', (event) => {
      const width = event.width;
      if (event.inProgress === true || width === undefined) return;
      setWidths((current) => saveWidth(current, String(event.id), width));
    });
  }, []);
  const sortMarks = {
    [sort.replace(/^-/, '')]: {
      order: sort.startsWith('-') ? ('desc' as const) : ('asc' as const),
    },
  };
  return (
    <div className="min-h-0 flex-1 [&>*]:h-full">
      <Theme fonts={false}>
        <Locale words={documentsWords(i18n.language)}>
          <DocumentsCellContext.Provider value={names}>
            <SelectionContext.Provider value={selection}>
              <Grid
                data={rows.map((row) => ({ ...row }))}
                columns={columns}
                sortMarks={sortMarks}
                init={init}
                select={false}
                onFilterValue={(event: FilterValueEvent) => {
                  onFilter(event.field, event.id);
                }}
                onOpenDocument={(event: OpenDocumentEvent) => {
                  onOpen(event.id);
                }}
              />
            </SelectionContext.Provider>
          </DocumentsCellContext.Provider>
        </Locale>
      </Theme>
    </div>
  );
}
