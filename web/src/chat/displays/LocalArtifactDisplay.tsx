import type { TFunction } from 'i18next';
import { useTranslation } from 'react-i18next';
import type { DisplayArtifact } from './types';
import { DisplayCardShell } from './DisplayCardShell';

// LocalArtifactDisplay (local_artifact): a render-only chip for a produced file —
// filename + a human byte size + a mono path chip. There is no fetch and no download
// action wired this phase (the artifact lives on the server runDir); it is a
// legible reference to what the tool produced. Values render React-escaped.

export interface LocalArtifactDisplayProps {
  readonly payload: { readonly artifact?: DisplayArtifact };
}

const KB = 1024;
const MB = KB * 1024;
const GB = MB * 1024;

/** A compact human byte size via the i18n size keys (B / KB / MB / GB, 1 decimal). */
function formatSize(bytes: number, t: TFunction): string {
  if (bytes < KB) return t('display.artifact.sizeBytes', { count: bytes });
  if (bytes < MB) return t('display.artifact.sizeKb', { value: (bytes / KB).toFixed(1) });
  if (bytes < GB) return t('display.artifact.sizeMb', { value: (bytes / MB).toFixed(1) });
  return t('display.artifact.sizeGb', { value: (bytes / GB).toFixed(1) });
}

export function LocalArtifactDisplay({ payload }: LocalArtifactDisplayProps) {
  const { t } = useTranslation();
  const artifact = payload.artifact;
  const label = t('display.type.local_artifact');
  const filename = artifact?.filename ?? t('display.artifact.noName');
  const size = artifact?.size_bytes !== undefined ? formatSize(artifact.size_bytes, t) : undefined;

  return (
    <DisplayCardShell label={label} meta={size}>
      <div className="flex flex-col gap-2">
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
        {artifact?.path !== undefined && artifact.path !== '' ? (
          <span className="flex flex-wrap items-baseline gap-2">
            <span className="text-[0.75rem] font-medium uppercase text-text-faint">
              {t('display.artifact.pathLabel')}
            </span>
            <span className="break-all rounded-[var(--radius-sm)] bg-surface px-2 py-0.5 font-mono text-[0.75rem] text-text-muted">
              {artifact.path}
            </span>
          </span>
        ) : null}
      </div>
    </DisplayCardShell>
  );
}
