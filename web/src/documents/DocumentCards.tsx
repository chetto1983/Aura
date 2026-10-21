import { useState } from 'react';
import { FileText, Code, Music, Film, Image as ImageIcon } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { DocumentRow } from './documentsApi';
import type { SelectionApi } from './documentsCellContext';
import { displayDate, valuesFor, type LabelNames } from './documentsModel';
import { Checkbox } from '@/components/ui/checkbox';

function Thumbnail({ row }: { readonly row: DocumentRow }) {
  const [failed, setFailed] = useState(false);
  const { t } = useTranslation();
  const kind = row.file_type ?? 'other';
  const Icon =
    kind === 'code'
      ? Code
      : kind === 'audio'
        ? Music
        : kind === 'video'
          ? Film
          : kind === 'image'
            ? ImageIcon
            : FileText;
  const supported = ['pdf', 'word', 'spreadsheet', 'presentation', 'image', 'video'].includes(kind);
  return (
    <div className="grid aspect-[4/3] place-items-center overflow-hidden rounded-t-xl border-b border-border bg-surface-2">
      {supported && !failed ? (
        <img
          loading="lazy"
          src={`/api/documents/${encodeURIComponent(row.id)}/thumbnail?v=${encodeURIComponent(row.raw_sha256 ?? row.indexed_at)}`}
          alt={row.file_name}
          onError={() => {
            setFailed(true);
          }}
          className="h-full w-full object-contain"
        />
      ) : (
        <div className="flex flex-col items-center gap-2 text-text-muted">
          <Icon aria-hidden="true" className="size-10" />
          <span className="text-xs">{t(`documents.workspace.fileTypes.${kind}`)}</span>
        </div>
      )}
    </div>
  );
}
export function DocumentCards({
  rows,
  names,
  cards,
  selection,
  onOpen,
}: {
  readonly rows: readonly DocumentRow[];
  readonly names: LabelNames;
  readonly cards: boolean;
  readonly selection: SelectionApi;
  readonly onOpen: (id: string) => void;
}) {
  const { t, i18n } = useTranslation();
  return (
    <div className="min-h-0 flex-1 overflow-y-auto p-3">
      <label className="mb-3 flex min-h-11 items-center gap-2 text-sm">
        <Checkbox
          checked={
            selection.pageState === 'all'
              ? true
              : selection.pageState === 'some'
                ? 'indeterminate'
                : false
          }
          onCheckedChange={selection.togglePage}
        />
        {t('documents.bulk.selectPage')}
      </label>
      <ul
        className={
          cards
            ? 'grid grid-cols-[repeat(auto-fill,minmax(min(100%,230px),1fr))] gap-4'
            : 'space-y-2'
        }
      >
        {rows.map((row) => {
          const labels = (field: 'type' | 'counterparty' | 'tags') =>
            valuesFor(row, field)
              .map((value) => names[field].get(value.value_key) ?? value.value_key)
              .join(', ');
          const date = valuesFor(row, 'document_date')[0]?.value_key;
          return (
            <li key={row.id} className="min-w-0 rounded-xl border border-border bg-surface">
              {cards && (
                <button
                  type="button"
                  className="block w-full"
                  aria-label={`${t('documents.workspace.open')}: ${row.file_name}`}
                  onClick={() => {
                    onOpen(row.id);
                  }}
                >
                  <Thumbnail key={`${row.id}:${row.raw_sha256 ?? row.indexed_at}`} row={row} />
                </button>
              )}
              <div className="flex items-start gap-3 p-3">
                <Checkbox
                  aria-label={t('documents.bulk.selectRow', { name: row.file_name })}
                  checked={selection.isSelected(row.id)}
                  onCheckedChange={() => {
                    selection.toggle(row);
                  }}
                />
                <div className="min-w-0 flex-1">
                  <button
                    type="button"
                    onClick={() => {
                      onOpen(row.id);
                    }}
                    className="w-full break-words text-left text-sm font-semibold text-info hover:underline"
                  >
                    {row.file_name}
                  </button>
                  <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-xs text-text-muted">
                    <span>
                      {date ? displayDate(date, i18n.language) : row.indexed_at.slice(0, 10)}
                    </span>
                    <span>
                      {row.reviewed_at
                        ? t('documents.status.reviewed')
                        : t('documents.workspace.inbox')}
                    </span>
                  </div>
                  <p className="mt-2 break-words text-xs">
                    {[labels('type'), labels('counterparty')].filter(Boolean).join(' · ')}
                  </p>
                  {labels('tags') && (
                    <p className="mt-1 break-words text-xs text-text-muted">{labels('tags')}</p>
                  )}
                </div>
              </div>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
