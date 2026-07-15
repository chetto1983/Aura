import { RefreshCw, UploadCloud, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { Asset } from './types';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';

interface AttachmentCardProps {
  readonly asset: Asset;
  readonly onRetry?: (id: string) => void;
  readonly onPromote?: (id: string) => void;
  readonly onRemove?: (id: string) => void;
}

export function AttachmentCard({ asset, onRetry, onPromote, onRemove }: AttachmentCardProps) {
  const { t } = useTranslation();
  const detail = asset.error_message ?? asset.summary ?? statusText(asset.status, t);
  return (
    <Card className="max-w-sm gap-2 bg-surface-2 px-3 py-2 text-sm">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="truncate font-medium">{asset.file_name}</p>
          <Badge variant={statusVariant(asset.status)} className="mt-1 text-[0.75rem]">
            {statusText(asset.status, t)}
          </Badge>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          {onRetry !== undefined && asset.status === 'failed' ? (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              data-required-touch-target
              onClick={() => {
                onRetry(asset.id);
              }}
              className="inline-flex min-h-11 min-w-11 items-center justify-center px-2 text-xs text-text-muted hover:bg-surface-3 hover:text-text"
            >
              <RefreshCw data-icon aria-hidden="true" className="size-3.5" />
              {t('chat.attachments.retry')}
            </Button>
          ) : null}
          {onPromote !== undefined && isPromotable(asset) ? (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              data-required-touch-target
              onClick={() => {
                onPromote(asset.id);
              }}
              className="inline-flex min-h-11 min-w-11 items-center justify-center px-2 text-xs text-text-muted hover:bg-surface-3 hover:text-text"
            >
              <UploadCloud data-icon aria-hidden="true" className="size-3.5" />
              {t('chat.attachments.promote')}
            </Button>
          ) : null}
          {onRemove !== undefined && asset.status !== 'deleted' && asset.status !== 'canceled' ? (
            <Button
              type="button"
              variant="ghost"
              size="icon"
              aria-label={t('chat.attachments.remove', { name: asset.file_name })}
              data-required-touch-target
              onClick={() => {
                onRemove(asset.id);
              }}
              className="inline-flex h-11 min-h-11 w-11 min-w-11 items-center justify-center rounded-full text-text-muted hover:bg-surface-3 hover:text-text"
            >
              <X data-icon aria-hidden="true" className="size-3.5" />
            </Button>
          ) : null}
        </div>
      </div>
      {detail.length > 0 ? (
        <p className="mt-2 text-xs leading-relaxed text-text-muted">{detail}</p>
      ) : null}
    </Card>
  );
}

function isPromotable(asset: Asset): boolean {
  if (asset.scope === 'library') return false;
  return asset.status === 'searchable' || asset.status === 'complete';
}

function statusText(status: Asset['status'], t: ReturnType<typeof useTranslation>['t']): string {
  switch (status) {
    case 'failed':
      return t('chat.attachments.failed');
    case 'refused':
      return t('chat.attachments.refused');
    case 'searchable':
    case 'complete':
      return t('chat.attachments.ready');
    default:
      return t('chat.attachments.processing');
  }
}

function statusVariant(status: Asset['status']): 'secondary' | 'warning' | 'success' | 'danger' {
  switch (status) {
    case 'failed':
    case 'refused':
      return 'danger';
    case 'searchable':
    case 'complete':
      return 'success';
    default:
      return 'warning';
  }
}
