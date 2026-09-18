import type { CSSProperties } from 'react';
import { useTranslation } from 'react-i18next';
import { assetDownloadUrl, assetStreamUrl, type StudioRecord } from './studioApi';

// StudioHistoryCard — one row of the history: what it looks like, how it ended, and what was
// asked for. A clip has no still to show, so its own first frame is used: `preload="metadata"`
// fetches the header and the first frame and nothing else, which is what a thumbnail needs and
// all of what a 40 MB clip must not download to draw one.

const STATUS_TONE: Readonly<Record<string, string>> = {
  pending: 'bg-info',
  in_progress: 'bg-info',
  completed: 'bg-success',
  failed: 'bg-danger',
  expired: 'bg-danger',
  cancelled: 'bg-text-disabled',
};

interface StudioHistoryCardProps {
  readonly record: StudioRecord;
  /** Its place in the list, which staggers the reveal. */
  readonly index: number;
  readonly selected: boolean;
  readonly onSelect: (record: StudioRecord) => void;
}

function statusLabel(status: string, t: ReturnType<typeof useTranslation>['t']): string {
  switch (status) {
    case 'pending':
      return t('studio.status.pending');
    case 'in_progress':
      return t('studio.status.in_progress');
    case 'completed':
      return t('studio.status.completed');
    case 'failed':
      return t('studio.status.failed');
    case 'expired':
      return t('studio.status.expired');
    case 'cancelled':
      return t('studio.status.cancelled');
    default:
      return status;
  }
}

function Thumbnail({ record }: { readonly record: StudioRecord }) {
  if (record.asset_id === undefined) {
    return <span className="block size-full bg-surface-3" />;
  }
  if (record.kind === 'video') {
    return (
      // eslint-disable-next-line jsx-a11y/media-has-caption -- a generated clip carries none,
      // and this element is a still: it has no controls and is never played here.
      <video
        src={assetStreamUrl(record.asset_id)}
        preload="metadata"
        muted
        aria-hidden="true"
        className="size-full object-cover"
      />
    );
  }
  return (
    <img
      src={assetDownloadUrl(record.asset_id)}
      alt=""
      aria-hidden="true"
      className="size-full object-cover"
    />
  );
}

export function StudioHistoryCard({ record, index, selected, onSelect }: StudioHistoryCardProps) {
  const { t } = useTranslation();
  return (
    <button
      type="button"
      aria-current={selected ? 'true' : undefined}
      onClick={() => {
        onSelect(record);
      }}
      style={{ '--studio-reveal-index': index } as CSSProperties}
      className={`studio-reveal flex w-full items-center gap-2 rounded-[var(--radius-md)] border p-1.5 text-left transition-colors focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none ${
        selected
          ? 'border-border-strong bg-surface-2'
          : 'border-transparent hover:border-border hover:bg-surface-2'
      }`}
    >
      <span className="size-10 shrink-0 overflow-hidden rounded-[var(--radius-sm)] bg-surface-3">
        <Thumbnail record={record} />
      </span>
      <span className="flex min-w-0 flex-col gap-0.5">
        <span className="flex items-center gap-1.5 text-[11px] text-text-faint">
          <span
            aria-hidden="true"
            className={`size-1.5 rounded-full ${STATUS_TONE[record.status] ?? 'bg-text-disabled'}`}
          />
          {statusLabel(record.status, t)}
        </span>
        <span className="truncate text-xs text-text">{record.prompt}</span>
      </span>
    </button>
  );
}
