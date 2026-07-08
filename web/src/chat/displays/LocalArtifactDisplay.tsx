import { useTranslation } from 'react-i18next';
import { formatSize } from '../artifacts/artifactMeta';
import type { DisplayArtifact } from './types';
import { DisplayCardShell } from './DisplayCardShell';

// LocalArtifactDisplay (local_artifact): a file the agent produced and delivered
// to the web chat (37A-04). When the descriptor carries an `asset_id` (ingest
// succeeded) the card renders an accessible download control — a real same-origin
// anchor targeting GET /api/assets/{asset_id}/download, so the browser performs
// the RequireAuth-gated GET and the forced-attachment response (octet-stream +
// nosniff, 37A-03) saves the file under its name. When `asset_id` is absent the
// delivery degraded (authenticated-but-ingest-failed, D-02/D-05) and the card is
// render-only: filename + size + a "delivery unavailable" note. A raw host/
// container path is NEVER rendered in EITHER branch (D-13) — the reducer omits it
// from the payload, so the browser never receives one. Values render
// React-escaped; the anchor is same-origin only.

export interface LocalArtifactDisplayProps {
  readonly payload: { readonly artifact?: DisplayArtifact };
}

export function LocalArtifactDisplay({ payload }: LocalArtifactDisplayProps) {
  const { t } = useTranslation();
  const artifact = payload.artifact;
  const label = t('display.type.local_artifact');
  const filename = artifact?.filename ?? t('display.artifact.noName');
  const size = artifact?.size_bytes !== undefined ? formatSize(artifact.size_bytes, t) : undefined;
  const assetId = artifact?.asset_id;

  return (
    <DisplayCardShell label={label} meta={size}>
      <div className="flex flex-col gap-3">
        <span className="flex items-center gap-2">
          <svg
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
            className="shrink-0 text-text-faint"
          >
            <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
            <path d="M14 2v6h6" />
          </svg>
          <span className="truncate font-mono text-sm text-text" title={filename}>
            {filename}
          </span>
        </span>
        {assetId ? (
          <a
            href={`/api/assets/${assetId}/download`}
            download={filename}
            aria-label={t('display.artifact.downloadAria', { filename })}
            className="group inline-flex w-fit items-center gap-2 rounded-[var(--radius-sm)] border border-accent/40 bg-surface-2 px-3 py-1.5 text-sm font-medium text-accent-text transition-colors hover:border-accent hover:bg-surface focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
          >
            <svg
              width="15"
              height="15"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
              aria-hidden="true"
              className="shrink-0 transition-transform group-hover:translate-y-0.5"
            >
              <path d="M12 3v12" />
              <path d="m7 10 5 5 5-5" />
              <path d="M5 21h14" />
            </svg>
            {t('display.artifact.download')}
          </a>
        ) : (
          <span
            role="note"
            className="inline-flex w-fit items-center gap-1.5 text-[0.8125rem] text-warning"
          >
            <svg
              width="14"
              height="14"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
              aria-hidden="true"
              className="shrink-0"
            >
              <path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" />
              <path d="M12 9v4" />
              <path d="M12 17h.01" />
            </svg>
            {t('display.artifact.deliveryUnavailable')}
          </span>
        )}
      </div>
    </DisplayCardShell>
  );
}
