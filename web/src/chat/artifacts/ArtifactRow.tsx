import { useTranslation } from 'react-i18next';
import { AlertTriangle, Download } from 'lucide-react';
import type { Asset } from '../attachments/types';
import { categoryIcon, categoryLabel, formatSize } from './artifactMeta';

// ArtifactRow (D-05/D-12/D-18): one agent deliverable in the "Artefatti" panel. It
// mirrors LocalArtifactDisplay's two-state split — an ACCEPTED asset gets the 37A-
// proven same-origin download anchor (GET /api/assets/{id}/download, session cookie
// rides it) plus a click-to-preview row body; a DEGRADED asset (any non-accepted
// status: still processing, failed, refused) gets a disabled role="note" delivery-
// unavailable affordance and NO preview trigger. Only the opaque `id` ever reaches
// the DOM — never object_key/object_bucket or a host/container path (T-37B-16): the
// Asset JSON never carries them here and we address the download purely by id.
//
// The download control is an <a> SIBLING of the preview <button> (not nested — an
// anchor inside a button is invalid HTML); its click is stopped from bubbling so
// downloading never also opens the preview.

export interface ArtifactRowProps {
  readonly asset: Asset;
  /** Opens the in-cockpit preview for this asset (accepted rows only). */
  readonly onPreview: (asset: Asset) => void;
}

export function ArtifactRow({ asset, onPreview }: ArtifactRowProps) {
  const { t } = useTranslation();
  const Icon = categoryIcon(asset.mime_type, asset.file_name);
  const label = categoryLabel(asset.mime_type, asset.file_name, t);
  const size = formatSize(asset.size_bytes, t);
  const degraded = asset.status !== 'accepted';

  return (
    <div
      className="group/row relative flex items-center gap-3 rounded-lg border border-transparent bg-surface-2/40 px-2.5 py-2 transition-colors hover:border-border hover:bg-surface-2 data-[degraded=true]:opacity-70 data-[degraded=true]:hover:border-transparent data-[degraded=true]:hover:bg-surface-2/40"
      data-degraded={degraded}
    >
      <IconTile Icon={Icon} degraded={degraded} />

      {degraded ? (
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <ArtifactName name={asset.file_name} />
          <span
            role="note"
            className="inline-flex items-center gap-1.5 text-[0.75rem] font-medium text-warning"
          >
            <AlertTriangle aria-hidden="true" className="size-3.5 shrink-0" strokeWidth={2} />
            {t('display.artifact.deliveryUnavailable')}
          </span>
        </div>
      ) : (
        <>
          <button
            type="button"
            onClick={() => {
              onPreview(asset);
            }}
            className="flex min-w-0 flex-1 cursor-pointer flex-col gap-0.5 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
          >
            <ArtifactName name={asset.file_name} />
            <span className="truncate text-[0.75rem] uppercase tracking-wide text-text-faint">
              {label} · {size}
            </span>
          </button>
          <a
            href={`/api/assets/${encodeURIComponent(asset.id)}/download`}
            download={asset.file_name}
            onClick={(e) => {
              e.stopPropagation();
            }}
            aria-label={t('display.artifact.downloadAria', { filename: asset.file_name })}
            className="grid size-8 shrink-0 place-items-center rounded-full border border-transparent text-text-faint opacity-0 transition-all hover:border-accent hover:bg-surface hover:text-accent-text focus-visible:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent group-hover/row:opacity-100"
          >
            <Download className="size-4 transition-transform group-hover/row:translate-y-px" />
          </a>
        </>
      )}
    </div>
  );
}

/** The category glyph in a rounded tile — the row's visual anchor. */
function IconTile({ Icon, degraded }: { Icon: typeof Download; degraded: boolean }) {
  return (
    <span
      className={
        'grid size-9 shrink-0 place-items-center rounded-md ring-1 transition-colors ' +
        (degraded
          ? 'bg-surface text-text-faint ring-border/60'
          : 'bg-surface text-accent-text ring-border group-hover/row:ring-accent/40')
      }
    >
      <Icon aria-hidden="true" className="size-[1.125rem]" strokeWidth={1.75} />
    </span>
  );
}

/** The monospace, truncating filename shared by both row states. */
function ArtifactName({ name }: { name: string }) {
  return (
    <span className="truncate font-mono text-[0.8125rem] text-text" title={name}>
      {name}
    </span>
  );
}
