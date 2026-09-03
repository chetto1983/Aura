import { RefreshCw, UploadCloud } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { RemoteImagePreview } from './AttachmentImage';
import type { Asset } from './types';
import { isReadyAsset } from './upload';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';

interface AttachmentCardProps {
  readonly asset: Asset;
  readonly onRetry?: (id: string) => void;
  readonly onPromote?: (id: string) => void;
}

export function AttachmentCard({ asset, onRetry, onPromote }: AttachmentCardProps) {
  const { t } = useTranslation();
  const status = statusText(asset.status, t);
  // The detail line EXPLAINS; it does not restate the badge. It used to fall back to the
  // status word, so a refused asset with no server message printed "Refused" twice, once
  // as a badge and once as its own explanation.
  const detail = asset.error_message ?? asset.summary ?? '';
  const showsImage = asset.modality === 'image' && isReadyAsset(asset);
  return (
    <Card className="max-w-sm gap-2 overflow-hidden bg-surface-2 px-3 py-2 text-sm">
      {showsImage ? (
        <RemoteImagePreview
          assetId={asset.id}
          fileName={asset.file_name}
          className="-mx-3 -mt-2 max-h-80 w-[calc(100%+1.5rem)] object-contain"
        />
      ) : null}
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="truncate font-medium">{asset.file_name}</p>
          {status !== '' ? (
            <Badge variant={statusVariant(asset.status)} className="mt-1 text-[0.75rem]">
              {status}
            </Badge>
          ) : null}
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
              className="inline-flex min-h-[44px] min-w-[44px] items-center justify-center px-2 text-xs text-text-muted hover:bg-surface-3 hover:text-text"
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
              className="inline-flex min-h-[44px] min-w-[44px] items-center justify-center px-2 text-xs text-text-muted hover:bg-surface-3 hover:text-text"
            >
              <UploadCloud data-icon aria-hidden="true" className="size-3.5" />
              {t('chat.attachments.promote')}
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

// Same stale assumption statusText carried: gating on searchable/complete meant this
// button could never appear, because assets stop at `processing` on this deployment.
function isPromotable(asset: Asset): boolean {
  if (asset.scope === 'library') return false;
  return isReadyAsset(asset);
}

// A status word only when there is one worth saying.
//
// This used to call everything except searchable/complete "processing" -- and upload.ts
// records the measurement that those two states are never reached on this deployment
// (2026-08-13: presigned 6 / processing 2 / accepted 2 / searchable 0), because the
// statement that activates a version lost its callers. So an attachment the model had
// already read and answered about sat under a permanent "Elaborazione" badge, seen live
// 2026-09-03 on a screenshot the vision model described in full while the chip claimed it
// was still being worked on.
//
// Readiness is now the same question the composer already answers with isReadyAsset --
// the one that decides whether the turn may be sent at all -- so the badge cannot
// disagree with the button. A ready attachment says nothing: the picture, or the
// filename, is the whole message.
function statusText(status: Asset['status'], t: ReturnType<typeof useTranslation>['t']): string {
  switch (status) {
    case 'failed':
      return t('chat.attachments.failed');
    case 'refused':
      return t('chat.attachments.refused');
    case 'accepted':
    case 'processing':
    case 'searchable':
    case 'embedding':
    case 'complete':
      return '';
    // A vanishing asset is not one being worked on. Saying "processing" while the bytes
    // are being removed is how a deleted attachment read as a stuck upload.
    case 'deleting':
    case 'deleted':
    case 'canceled':
      return t('chat.attachments.failed');
    default:
      return t('chat.attachments.processing');
  }
}

function statusVariant(status: Asset['status']): 'secondary' | 'warning' | 'success' | 'danger' {
  switch (status) {
    case 'failed':
    case 'refused':
      return 'danger';
    default:
      return 'warning';
  }
}
