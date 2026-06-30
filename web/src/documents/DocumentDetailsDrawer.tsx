import { Save, Trash2, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../components/Spinner';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import type { DocumentDetail, DocumentItem, DocumentVersion } from './documentApi';
import { DocumentEventsPanel } from './DocumentEventsPanel';
import { formatBytes } from './documentFormat';
import { formatDocumentDate, statusToneFor } from './documentViewModel';

interface DocumentDetailsDrawerProps {
  readonly open: boolean;
  readonly document: DocumentItem | undefined;
  readonly detail: DocumentDetail | undefined;
  readonly activeVersion: DocumentVersion | undefined;
  readonly tagDraft: string;
  readonly savingTags: boolean;
  readonly onTagDraftChange: (value: string) => void;
  readonly onSaveTags: () => void;
  readonly onDelete: () => void;
  readonly onClose: () => void;
}

export function DocumentDetailsDrawer({
  open,
  document,
  detail,
  activeVersion,
  tagDraft,
  savingTags,
  onTagDraftChange,
  onSaveTags,
  onDelete,
  onClose,
}: DocumentDetailsDrawerProps) {
  const { t } = useTranslation();
  if (!open || document === undefined) return null;
  return (
    <aside
      role="dialog"
      aria-modal="false"
      aria-label={document.title}
      className="fixed inset-y-0 right-0 z-30 flex w-full max-w-md flex-col border-l border-border bg-bg shadow-drawer"
    >
      <header className="flex items-start justify-between gap-3 border-b border-border p-4">
        <div className="min-w-0">
          <h2 className="truncate text-[18px] font-semibold text-text">{document.title}</h2>
          <p className="mt-1 text-[13px] text-text-muted">
            {formatDocumentDate(document.updated_at ?? document.created_at)}
          </p>
        </div>
        <Button type="button" size="icon" variant="ghost" aria-label={t('documents.actions.close')} onClick={onClose}>
          <X aria-hidden="true" />
        </Button>
      </header>
      <div className="min-h-0 flex-1 overflow-y-auto p-4">
        <section className="grid gap-3 border-b border-border pb-4">
          <div className="flex items-center justify-between gap-2">
            <h3 className="text-[14px] font-semibold text-text">
              {t('documents.detail.overview')}
            </h3>
            <Badge variant={statusToneFor(document.status)}>{document.status}</Badge>
          </div>
          <dl className="grid gap-2 text-[13px] text-text-muted">
            <MetaItem
              label={t('documents.view.size')}
              value={activeVersion === undefined ? '-' : formatBytes(activeVersion.size_bytes)}
            />
            <MetaItem label="MIME" value={activeVersion?.content_type ?? '-'} />
            <MetaItem label="Scope" value={document.scope} />
          </dl>
          <div className="grid gap-1.5">
            <Label htmlFor="documents-tags">{t('documents.detail.tags')}</Label>
            <Input
              id="documents-tags"
              value={tagDraft}
              onChange={(event) => onTagDraftChange(event.target.value)}
            />
          </div>
          <div className="flex flex-wrap gap-2">
            <Button type="button" disabled={savingTags} onClick={onSaveTags}>
              {savingTags ? <Spinner /> : <Save aria-hidden="true" />}
              {t('documents.actions.saveTags')}
            </Button>
            <Button type="button" variant="destructive" onClick={onDelete}>
              <Trash2 aria-hidden="true" />
              {t('documents.actions.delete')}
            </Button>
          </div>
        </section>
        <section className="grid gap-3 border-b border-border py-4">
          <h3 className="text-[14px] font-semibold text-text">
            {t('documents.detail.versions')}
          </h3>
          {detail?.versions.map((version) => (
            <div
              key={version.id}
              className="grid gap-2 rounded-md border border-border bg-surface px-3 py-2 text-[13px] text-text"
            >
              <div className="flex flex-wrap items-center justify-between gap-2">
                <span>
                  v{version.version_number} · {version.status} · {formatBytes(version.size_bytes)}
                </span>
                {version.id === document.active_version_id ? (
                  <Badge variant="success">active</Badge>
                ) : null}
              </div>
              <dl className="grid gap-1 text-[12px] text-text-muted">
                <MetaItem label="MIME" value={version.content_type} />
                <MetaItem label="SHA-256" value={version.sha256} />
                <MetaItem label="Storage" value={version.storage_object_id} />
              </dl>
            </div>
          ))}
        </section>
        <section className="grid gap-3 py-4">
          <h3 className="text-[14px] font-semibold text-text">
            {t('documents.detail.processing')}
          </h3>
          <DocumentEventsPanel key={document.id} documentId={document.id} />
        </section>
        <details className="border-t border-border pt-4">
          <summary className="cursor-pointer text-[14px] font-semibold text-text">
            {t('documents.detail.advanced')}
          </summary>
          <dl className="mt-3 grid gap-2 text-[12px] text-text-muted">
            <MetaItem label="Document ID" value={document.id} />
            <MetaItem label="Version ID" value={activeVersion?.id ?? '-'} />
            <MetaItem label="SHA-256" value={activeVersion?.sha256 ?? '-'} />
            <MetaItem label="Storage" value={activeVersion?.storage_object_id ?? '-'} />
          </dl>
        </details>
      </div>
    </aside>
  );
}

function MetaItem({ label, value }: { readonly label: string; readonly value: string }) {
  return (
    <div className="min-w-0">
      <dt className="text-[11px] font-semibold uppercase text-text-faint">{label}</dt>
      <dd className="break-all font-mono text-[12px] text-text">{value}</dd>
    </div>
  );
}
