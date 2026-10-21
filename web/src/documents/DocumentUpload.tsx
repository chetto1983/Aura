import { useRef } from 'react';
import { Link } from 'react-router';
import { useTranslation } from 'react-i18next';
import { Upload } from 'lucide-react';
import type { useDocumentUploads } from './useDocumentUploads';
import { documentHref, readDocumentQuery } from './documentRoute';
import { Button } from '@/components/ui/button';

export function DocumentUpload({
  onFiles,
}: {
  readonly onFiles: (files: readonly File[]) => void;
}) {
  const { t } = useTranslation();
  const input = useRef<HTMLInputElement>(null);
  return (
    <>
      <Button onClick={() => input.current?.click()} className="gap-2">
        <Upload aria-hidden="true" className="size-4" />
        {t('documents.workspace.upload')}
      </Button>
      <input
        ref={input}
        type="file"
        multiple
        hidden
        aria-hidden="true"
        tabIndex={-1}
        aria-label={t('documents.workspace.upload')}
        onChange={(event) => {
          onFiles(Array.from(event.target.files ?? []));
          event.target.value = '';
        }}
      />
    </>
  );
}
export function UploadQueue({ queue }: { readonly queue: ReturnType<typeof useDocumentUploads> }) {
  const { t } = useTranslation();
  return (
    <div className="mt-4">
      {queue.lookupFailed && (
        <p role="alert" className="mb-3 text-sm text-warning">
          {t('documents.workspace.lookupFailed')}
        </p>
      )}
      {queue.items.length === 0 && (
        <p className="text-sm text-text-muted">{t('documents.workspace.noUploads')}</p>
      )}
      <ul className="space-y-2">
        {queue.items.map((item) => (
          <li
            key={item.key}
            className="flex flex-wrap items-center gap-3 rounded-xl border border-border bg-surface p-3"
          >
            <div className="min-w-0 flex-[1_1_14rem]">
              <p className="break-words text-sm font-semibold">{item.name}</p>
              <p role="status" className="text-xs text-text-muted">
                {t(`documents.workspace.${item.status}`)}
              </p>
              {item.error && (
                <p role="alert" className="text-sm text-danger">
                  {item.error}
                </p>
              )}
            </div>
            {item.documentId ? (
              <Link
                className="text-sm text-info underline"
                to={documentHref(
                  item.documentId,
                  '/documents/library',
                  readDocumentQuery('file_type=all'),
                )}
              >
                {t('documents.workspace.open')}
              </Link>
            ) : item.sourceKey ? (
              <Link className="text-sm text-info underline" to="/documents/files">
                {t('documents.workspace.files')}
              </Link>
            ) : null}
            {item.status === 'failed' && item.file && (
              <Button
                variant="outline"
                onClick={() => {
                  queue.retry(item.key);
                }}
              >
                {t('documents.workspace.retry')}
              </Button>
            )}
            {item.status === 'needs_file' && (
              <label className="text-sm text-info">
                {t('documents.workspace.choose')}
                <input
                  type="file"
                  className="block max-w-52 text-xs"
                  onChange={(event) => {
                    const file = event.target.files?.[0];
                    if (file) queue.retry(item.key, file);
                  }}
                />
              </label>
            )}
            {item.status !== 'queued' && item.status !== 'uploading' && (
              <Button
                variant="ghost"
                onClick={() => {
                  queue.dismiss(item.key);
                }}
              >
                {t('documents.workspace.dismiss')}
              </Button>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}
