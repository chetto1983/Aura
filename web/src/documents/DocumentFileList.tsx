import { FileText, Image, PackageOpen } from 'lucide-react';
import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { DocumentActionMenu } from './DocumentActionMenu';
import type { DocumentItem, DocumentVersion } from './documentApi';
import { formatBytes } from './documentFormat';
import type { ViewMode } from './DocumentFilterBar';
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

// rowSize renders a document's size from the loaded detail version when present,
// otherwise from the denormalized active-version size on the list row, and finally
// "-" when neither is known (no active version, or a genuinely zero-byte version).
function rowSize(document: DocumentItem, version: DocumentVersion | undefined): string {
  if (version !== undefined) return formatBytes(version.size_bytes);
  if (document.active_size_bytes !== undefined && document.active_size_bytes > 0) {
    return formatBytes(document.active_size_bytes);
  }
  return '-';
}

interface DocumentFileListProps {
  readonly documents: readonly DocumentItem[];
  readonly activeVersions: ReadonlyMap<string, DocumentVersion | undefined>;
  readonly tab: DocumentTab;
  readonly viewMode: ViewMode;
  readonly selectedIds: ReadonlySet<string>;
  readonly activeId: string;
  readonly loading: boolean;
  readonly error: boolean;
  readonly onToggleSelected: (id: string) => void;
  readonly onOpenDetails: (id: string) => void;
  readonly onOpenActions: (id: string) => void;
  readonly onAskDocument: (document: DocumentItem) => void;
  readonly onEditTags: (document: DocumentItem) => void;
  readonly onRetry: (document: DocumentItem) => void;
  readonly onDelete: (document: DocumentItem) => void;
  readonly onRefresh: () => void;
}

export function DocumentFileList({
  documents,
  activeVersions,
  tab,
  viewMode,
  selectedIds,
  activeId,
  loading,
  error,
  onToggleSelected,
  onOpenDetails,
  onOpenActions,
  onAskDocument,
  onEditTags,
  onRetry,
  onDelete,
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

  if (viewMode === 'grid') {
    return (
      <DocumentGrid
        documents={visible}
        activeVersions={activeVersions}
        selectedIds={selectedIds}
        activeId={activeId}
        onToggleSelected={onToggleSelected}
        onOpenDetails={onOpenDetails}
        onOpenActions={onOpenActions}
        onAskDocument={onAskDocument}
        onEditTags={onEditTags}
        onRetry={onRetry}
        onDelete={onDelete}
      />
    );
  }

  return (
    <>
      <DocumentMobileList
        documents={visible}
        activeVersions={activeVersions}
        onOpenDetails={onOpenDetails}
        onOpenActions={onOpenActions}
        onAskDocument={onAskDocument}
        onEditTags={onEditTags}
        onRetry={onRetry}
        onDelete={onDelete}
      />
      <div className="hidden min-h-0 flex-1 overflow-auto px-4 py-3 sm:block sm:px-6">
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
                      onCheckedChange={() => {
                        onToggleSelected(document.id);
                      }}
                    />
                  </td>
                  <td className="min-w-0 px-2 py-2">
                    <button
                      type="button"
                      className="flex min-w-0 items-center gap-3 text-left outline-none focus-visible:ring-2 focus-visible:ring-ring"
                      onClick={() => {
                        onOpenDetails(document.id);
                      }}
                    >
                      <span className="grid size-9 shrink-0 place-items-center rounded-md border border-border bg-surface text-accent-text">
                        <KindIcon kind={kind} />
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
                    {rowSize(document, version)}
                  </td>
                  <td className="rounded-r-md px-2 py-2 text-right">
                    <DocumentActionMenu
                      document={document}
                      onOpen={() => {
                        onOpenActions(document.id);
                      }}
                      onAsk={onAskDocument}
                      onEditTags={onEditTags}
                      onRetry={onRetry}
                      onDelete={onDelete}
                    />
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </>
  );
}

interface DocumentMobileListProps {
  readonly documents: readonly DocumentItem[];
  readonly activeVersions: ReadonlyMap<string, DocumentVersion | undefined>;
  readonly onOpenDetails: (id: string) => void;
  readonly onOpenActions: (id: string) => void;
  readonly onAskDocument: (document: DocumentItem) => void;
  readonly onEditTags: (document: DocumentItem) => void;
  readonly onRetry: (document: DocumentItem) => void;
  readonly onDelete: (document: DocumentItem) => void;
}

function DocumentMobileList({
  documents,
  activeVersions,
  onOpenDetails,
  onOpenActions,
  onAskDocument,
  onEditTags,
  onRetry,
  onDelete,
}: DocumentMobileListProps) {
  const { t } = useTranslation();
  return (
    <div className="min-h-0 flex-1 overflow-auto px-4 pb-5 pt-2 sm:hidden">
      <div className="mx-auto w-full max-w-md">
        <p className="px-0 py-2 text-[12px] font-semibold text-text-muted">
          {t('documents.view.name')}
        </p>
        <ul aria-label={t('documents.title')} className="divide-y divide-border/70">
          {documents.map((document) => {
            const version = activeVersions.get(document.id);
            const kind = documentKindFor(document, version);
            return (
              <li key={document.id} className="flex min-w-0 items-center gap-2 py-2.5">
                <button
                  type="button"
                  className="flex min-w-0 flex-1 items-center gap-3 text-left outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  onClick={() => {
                    onOpenDetails(document.id);
                  }}
                >
                  <span className="grid size-9 shrink-0 place-items-center rounded-md border border-border bg-surface-2 text-accent-text">
                    <KindIcon kind={kind} />
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-[14px] font-semibold text-text">
                      {document.title}
                    </span>
                    <span className="mt-0.5 block text-[12px] text-text-muted">
                      {formatDocumentWeekday(document.updated_at ?? document.created_at)}
                    </span>
                  </span>
                </button>
                <DocumentActionMenu
                  document={document}
                  onOpen={() => {
                    onOpenActions(document.id);
                  }}
                  onAsk={onAskDocument}
                  onEditTags={onEditTags}
                  onRetry={onRetry}
                  onDelete={onDelete}
                />
              </li>
            );
          })}
        </ul>
      </div>
    </div>
  );
}

function formatDocumentWeekday(value: string | undefined): string {
  if (value === undefined || value.trim() === '') return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '-';
  return new Intl.DateTimeFormat(undefined, { weekday: 'long' }).format(date);
}

function KindIcon({ kind }: { readonly kind: ReturnType<typeof documentKindFor> }) {
  if (kind === 'image') return <Image className="size-4" aria-hidden="true" />;
  return <FileText className="size-4" aria-hidden="true" />;
}

interface DocumentGridProps {
  readonly documents: readonly DocumentItem[];
  readonly activeVersions: ReadonlyMap<string, DocumentVersion | undefined>;
  readonly selectedIds: ReadonlySet<string>;
  readonly activeId: string;
  readonly onToggleSelected: (id: string) => void;
  readonly onOpenDetails: (id: string) => void;
  readonly onOpenActions: (id: string) => void;
  readonly onAskDocument: (document: DocumentItem) => void;
  readonly onEditTags: (document: DocumentItem) => void;
  readonly onRetry: (document: DocumentItem) => void;
  readonly onDelete: (document: DocumentItem) => void;
}

// DocumentGrid is the card layout for the list/grid view toggle. It surfaces the
// same facts as the table row (icon, title, tags, status, size) as a tappable card.
function DocumentGrid({
  documents,
  activeVersions,
  selectedIds,
  activeId,
  onToggleSelected,
  onOpenDetails,
  onOpenActions,
  onAskDocument,
  onEditTags,
  onRetry,
  onDelete,
}: DocumentGridProps) {
  const { t } = useTranslation();
  return (
    <div className="min-h-0 flex-1 overflow-auto px-4 py-3 sm:px-6">
      <ul
        aria-label={t('documents.title')}
        className="mx-auto grid w-full max-w-6xl grid-cols-[repeat(auto-fill,minmax(15rem,1fr))] gap-3"
      >
        {documents.map((document) => {
          const version = activeVersions.get(document.id);
          const kind = documentKindFor(document, version);
          const selected = selectedIds.has(document.id);
          return (
            <li
              key={document.id}
              data-selected={selected || undefined}
              className={cn(
                'group relative rounded-lg border border-border bg-bg p-3 transition-colors hover:bg-surface',
                activeId === document.id ? 'border-border-strong bg-surface-2' : '',
              )}
            >
              <div className="flex items-start justify-between gap-2">
                <Checkbox
                  aria-label={`Select ${document.title}`}
                  checked={selected}
                  onCheckedChange={() => {
                    onToggleSelected(document.id);
                  }}
                />
                <DocumentActionMenu
                  document={document}
                  onOpen={() => {
                    onOpenActions(document.id);
                  }}
                  onAsk={onAskDocument}
                  onEditTags={onEditTags}
                  onRetry={onRetry}
                  onDelete={onDelete}
                />
              </div>
              <button
                type="button"
                className="mt-1 flex w-full min-w-0 flex-col items-start gap-2 text-left outline-none focus-visible:ring-2 focus-visible:ring-ring"
                onClick={() => {
                  onOpenDetails(document.id);
                }}
              >
                <span className="grid size-10 shrink-0 place-items-center rounded-md border border-border bg-surface text-accent-text">
                  <KindIcon kind={kind} />
                </span>
                <span className="block w-full truncate text-[14px] font-semibold text-text">
                  {document.title}
                </span>
                <span className="flex flex-wrap gap-1">
                  {document.tags.slice(0, 2).map((tag) => (
                    <Badge key={tag} variant="secondary">
                      {tag}
                    </Badge>
                  ))}
                </span>
              </button>
              <div className="mt-2 flex items-center justify-between text-[12px] text-text-muted">
                <Badge variant={statusToneFor(document.status)}>{document.status}</Badge>
                <span>{rowSize(document, version)}</span>
              </div>
            </li>
          );
        })}
      </ul>
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
