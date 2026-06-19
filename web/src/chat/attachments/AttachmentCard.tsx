import { useTranslation } from 'react-i18next';
import type { Asset } from './types';

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
    <div className="max-w-sm rounded-[var(--radius-md)] border border-border bg-surface-2 px-3 py-2 text-sm text-text">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="truncate font-medium">{asset.file_name}</p>
          <p className="text-xs text-text-muted">{statusText(asset.status, t)}</p>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          {onRetry !== undefined && asset.status === 'failed' ? (
            <button
              type="button"
              onClick={() => {
                onRetry(asset.id);
              }}
              className="rounded-[var(--radius-sm)] px-2 py-1 text-xs text-text-muted outline-none hover:bg-surface-3 hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
            >
              {t('chat.attachments.retry')}
            </button>
          ) : null}
          {onPromote !== undefined && isPromotable(asset) ? (
            <button
              type="button"
              onClick={() => {
                onPromote(asset.id);
              }}
              className="rounded-[var(--radius-sm)] px-2 py-1 text-xs text-text-muted outline-none hover:bg-surface-3 hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
            >
              {t('chat.attachments.promote')}
            </button>
          ) : null}
          {onRemove !== undefined && asset.status !== 'deleted' && asset.status !== 'canceled' ? (
            <button
              type="button"
              aria-label={t('chat.attachments.remove', { name: asset.file_name })}
              onClick={() => {
                onRemove(asset.id);
              }}
              className="flex min-h-7 min-w-7 items-center justify-center rounded-full text-text-muted outline-none hover:bg-surface-3 hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
            >
              <span aria-hidden="true">x</span>
            </button>
          ) : null}
        </div>
      </div>
      {detail.length > 0 ? (
        <p className="mt-2 text-xs leading-relaxed text-text-muted">{detail}</p>
      ) : null}
    </div>
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
