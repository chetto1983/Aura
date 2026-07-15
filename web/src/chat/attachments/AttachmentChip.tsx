import { X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { UploadItem } from './types';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';

interface AttachmentChipProps {
  readonly item: UploadItem;
  readonly onRemove: (localId: string) => void;
}

export function AttachmentChip({ item, onRemove }: AttachmentChipProps) {
  const { t } = useTranslation();
  const label = statusLabel(item, t);
  return (
    <span className="inline-flex min-h-10 min-w-0 max-w-full items-center gap-2 rounded-md border border-border bg-surface-2 px-2 py-1 text-xs text-text">
      <span className="min-w-0 truncate">{item.file.name}</span>
      <Badge variant={statusVariant(item.status)} className="shrink-0 text-[0.75rem]">
        {label}
      </Badge>
      <Button
        type="button"
        variant="ghost"
        size="icon"
        aria-label={t('chat.attachments.remove', { name: item.file.name })}
        data-required-touch-target
        onClick={() => {
          onRemove(item.localId);
        }}
        className="h-11 min-h-11 w-11 min-w-11 shrink-0 rounded-full text-text-muted hover:bg-surface-3 hover:text-text"
      >
        <X data-icon aria-hidden="true" className="size-3.5" />
      </Button>
    </span>
  );
}

function statusVariant(
  status: UploadItem['status'],
): 'secondary' | 'warning' | 'success' | 'danger' {
  switch (status) {
    case 'ready':
      return 'success';
    case 'uploading':
    case 'processing':
    case 'queued':
      return 'warning';
    case 'failed':
    case 'refused':
      return 'danger';
  }
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
