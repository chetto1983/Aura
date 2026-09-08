import { useTranslation } from 'react-i18next';
import { lazy, Suspense } from 'react';
import { AlertTriangle, Download, File } from 'lucide-react';
import { formatSize, previewKind } from '../artifacts/artifactMeta';
import { PreviewLoading } from '../artifacts/renderers/PreviewStatus';
import type { DisplayArtifact } from './types';
import { DisplayCardShell } from './DisplayCardShell';

const InlineHtmlArtifact = lazy(() => import('../artifacts/InlineHtmlArtifact'));

// Only delivered asset IDs enable previews. Host/container paths never reach the UI.

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

  if (assetId && previewKind(artifact.mime_type ?? '', filename) === 'html') {
    return (
      <Suspense fallback={<PreviewLoading />}>
        <InlineHtmlArtifact
          key={assetId}
          assetId={assetId}
          fileName={filename}
          mimeType={artifact.mime_type ?? 'text/html'}
        />
      </Suspense>
    );
  }

  return (
    <DisplayCardShell label={label} meta={size}>
      <div className="flex flex-col gap-3">
        <span className="flex items-center gap-2">
          <File aria-hidden="true" className="size-4 shrink-0" />
          <span className="truncate font-mono text-sm text-text" title={filename}>
            {filename}
          </span>
        </span>
        {assetId ? (
          <a
            href={`/api/assets/${encodeURIComponent(assetId)}/download`}
            download={filename}
            aria-label={t('display.artifact.downloadAria', { filename })}
            className="group inline-flex w-fit items-center gap-2 rounded-[var(--radius-sm)] border border-accent/40 bg-surface-2 px-3 py-1.5 text-sm font-medium text-accent-text transition-colors hover:border-accent hover:bg-surface focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
          >
            <Download aria-hidden="true" className="size-4 shrink-0" />
            {t('display.artifact.download')}
          </a>
        ) : (
          <span
            role="note"
            className="inline-flex w-fit items-center gap-1.5 text-[0.8125rem] text-warning"
          >
            <AlertTriangle aria-hidden="true" className="size-4 shrink-0" />
            {t('display.artifact.deliveryUnavailable')}
          </span>
        )}
      </div>
    </DisplayCardShell>
  );
}
