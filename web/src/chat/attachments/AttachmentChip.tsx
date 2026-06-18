import { useTranslation } from 'react-i18next';
import type { UploadItem } from './types';

interface AttachmentChipProps {
  readonly item: UploadItem;
  readonly onRemove: (localId: string) => void;
}

export function AttachmentChip({ item, onRemove }: AttachmentChipProps) {
  const { t } = useTranslation();
  const label = statusLabel(item, t);
  return (
    <span className="inline-flex min-h-9 max-w-full items-center gap-2 rounded-[var(--radius-md)] border border-border bg-surface-2 px-2 py-1 text-xs text-text">
      <span className="min-w-0 truncate">{item.file.name}</span>
      <span className="shrink-0 text-text-muted">{label}</span>
      <button
        type="button"
        aria-label={t('chat.attachments.remove', { name: item.file.name })}
        onClick={() => {
          onRemove(item.localId);
        }}
        className="flex min-h-7 min-w-7 items-center justify-center rounded-full text-text-muted outline-none hover:bg-surface-3 hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
      >
        <span aria-hidden="true">x</span>
      </button>
    </span>
  );
}

function statusLabel(item: UploadItem, t: ReturnType<typeof useTranslation>['t']): string {
  switch (item.status) {
    case 'queued':
      return t('chat.attachments.processing');
    case 'uploading':
      return t('chat.attachments.uploading', { progress: Math.round(item.progress * 100) });
    case 'processing':
      return t('chat.attachments.processing');
    case 'ready':
      return t('chat.attachments.ready');
    case 'failed':
      return t('chat.attachments.failed');
    case 'refused':
      return t('chat.attachments.refused');
  }
}
