import { FileText, Image, MoreHorizontal, PackageOpen } from 'lucide-react';
import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import type { DocumentItem, DocumentVersion } from './documentApi';
import { formatBytes } from './documentFormat';
import {
  documentKindFor,
  documentMatchesTab,
  formatDocumentDate,
  statusToneFor,
  type DocumentTab,
} from './documentViewModel';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { cn } from '@/lib/utils';

interface DocumentFileListProps {
  readonly documents: readonly DocumentItem[];
  readonly activeVersions: ReadonlyMap<string, DocumentVersion | undefined>;
  readonly tab: DocumentTab;
  readonly selectedIds: ReadonlySet<string>;
  readonly activeId: string;
  readonly loading: boolean;
  readonly error: boolean;
  readonly onToggleSelected: (id: string) => void;
  readonly onOpenDetails: (id: string) => void;
  readonly onOpenActions: (id: string) => void;
  readonly onRefresh: () => void;
}

export function DocumentFileList({
  documents,
  activeVersions,
  tab,
  selectedIds,
  activeId,
  loading,
  error,
  onToggleSelected,
  onOpenDetails,
  onOpenActions,
  onRefresh,
}: DocumentFileListProps) {
  const { t } = useTranslation();
  const visible = useMemo(
    () =>
      documents.filter((document) =>
        documentMatchesTab(document, activeVersions.get(document.id), tab),
      ),
    [activeVersions, documents, tab],
  );

  if (loading) return <DocumentLoadingRows />;
  if (error) {
    return (
      <div
        role="alert"
        className="mx-auto grid min-h-64 w-full max-w-6xl place-items-center px-4 text-center"
      >
        <div className="grid gap-3">
          <p className="text-sm text-danger">{t('documents.error.list')}</p>
          <Button type="button" variant="outline" onClick={onRefresh}>
            {t('documents.actions.refresh')}
          </Button>
        </div>
      </div>
    );
  }
  if (visible.length === 0) {
    return (
      <div className="mx-auto grid min-h-64 w-full max-w-6xl place-items-center px-4 text-center">
        <div className="grid justify-items-center gap-3">
          <PackageOpen className="size-8 text-text-faint" aria-hidden="true" />
          <p className="text-sm font-semibold text-text">{t('documents.empty')}</p>
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-0 flex-1 overflow-auto px-4 py-3 sm:px-6">
      <table className="mx-auto w-full max-w-6xl table-fixed border-separate border-spacing-y-1">
        <caption className="sr-only">{t('documents.title')}</caption>
        <thead className="text-left text-[12px] font-semibold text-text-muted">
          <tr>
            <th className="w-10 px-2 py-2" scope="col">
              <span className="sr-only">Select</span>
            </th>
            <th className="px-2 py-2" scope="col">
              {t('documents.view.name')}
            </th>
            <th className="hidden w-36 px-2 py-2 md:table-cell" scope="col">
              {t('documents.view.status')}
            </th>
            <th className="hidden w-36 px-2 py-2 lg:table-cell" scope="col">
              {t('documents.view.modified')}
            </th>
            <th className="hidden w-28 px-2 py-2 sm:table-cell" scope="col">
              {t('documents.view.size')}
            </th>
            <th className="w-12 px-2 py-2" scope="col">
              <span className="sr-only">Actions</span>
            </th>
          </tr>
        </thead>
        <tbody>
          {visible.map((document) => {
            const version = activeVersions.get(document.id);
            const kind = documentKindFor(document, version);
            const selected = selectedIds.has(document.id);
            return (
              <tr
                key={document.id}
                aria-selected={selected}
                className={cn(
                  'group h-14 rounded-md bg-bg text-[14px] text-text transition-colors hover:bg-surface',
                  activeId === document.id ? 'bg-surface-2' : '',
                )}
              >
                <td className="rounded-l-md px-2 py-2">
                  <Checkbox
                    aria-label={`Select ${document.title}`}
                    checked={selected}
                    onCheckedChange={() => { onToggleSelected(document.id); }}
                  />
                </td>
                <td className="min-w-0 px-2 py-2">
                  <button
                    type="button"
                    className="flex min-w-0 items-center gap-3 text-left outline-none focus-visible:ring-2 focus-visible:ring-ring"
                    onClick={() => { onOpenDetails(document.id); }}
                  >
                    <span className="grid size-9 shrink-0 place-items-center rounded-md border border-border bg-surface text-accent-text">
                      {kind === 'image' ? (
                        <Image className="size-4" aria-hidden="true" />
                      ) : (
                        <FileText className="size-4" aria-hidden="true" />
                      )}
                    </span>
                    <span className="min-w-0">
                      <span className="block truncate font-semibold">{document.title}</span>
                      <span className="mt-0.5 flex flex-wrap gap-1">
                        {document.tags.slice(0, 2).map((tag) => (
                          <Badge key={tag} variant="secondary">
                            {tag}
                          </Badge>
                        ))}
                      </span>
                    </span>
                  </button>
                </td>
                <td className="hidden px-2 py-2 md:table-cell">
                  <Badge variant={statusToneFor(document.status)}>{document.status}</Badge>
                </td>
                <td className="hidden px-2 py-2 text-text-muted lg:table-cell">
                  {formatDocumentDate(document.updated_at ?? document.created_at)}
                </td>
                <td className="hidden px-2 py-2 text-text-muted sm:table-cell">
                  {version === undefined ? '-' : formatBytes(version.size_bytes)}
                </td>
                <td className="rounded-r-md px-2 py-2 text-right">
                  <Button
                    type="button"
                    size="icon"
                    variant="ghost"
                    aria-label={`Actions for ${document.title}`}
                    onClick={() => { onOpenActions(document.id); }}
                  >
                    <MoreHorizontal aria-hidden="true" />
                  </Button>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function DocumentLoadingRows() {
  return (
    <div role="status" className="mx-auto grid w-full max-w-6xl gap-2 px-4 py-4 sm:px-6">
      {Array.from({ length: 6 }, (_, index) => (
        <div key={index} className="h-14 rounded-md border border-border bg-surface/70" />
      ))}
    </div>
  );
}
