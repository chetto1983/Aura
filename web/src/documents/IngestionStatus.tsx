import { useTranslation } from 'react-i18next';
import type { IngestionState } from './workspaceApi';

export function IngestionStatus({ state }: { readonly state: IngestionState | null | undefined }) {
  const { t, i18n } = useTranslation();
  if (!state) return <p>{t('documents.workspace.unknown')}</p>;
  const active = state.in_progress > 0 || state.status.toLowerCase() === 'running';
  const status = active
    ? 'indexRunning'
    : state.status.toLowerCase() === 'ready'
      ? 'indexIdle'
      : 'unknown';
  const observed = new Date(state.observed_at ?? '');
  return (
    <>
      <p className="font-medium">{t(`documents.workspace.${status}`)}</p>
      {state.errors > 0 && (
        <p className="mt-2 text-sm text-warning">
          {t('documents.workspace.indexIssues', { count: state.errors })}
        </p>
      )}
      {!Number.isNaN(observed.getTime()) && (
        <p className="mt-2 text-xs text-text-muted">
          {t('documents.workspace.observed', {
            time: observed.toLocaleString(i18n.language, {
              dateStyle: 'short',
              timeStyle: 'short',
            }),
          })}
        </p>
      )}
      <details className="mt-3 text-xs text-text-muted">
        <summary className="min-h-11 cursor-pointer py-3 text-info">
          {t('documents.workspace.indexDetails')}
        </summary>
        <p>{t('documents.workspace.status', { status: state.status })}</p>
        <p>
          {t('documents.workspace.components', {
            active: state.in_progress,
            finished: state.finished,
            errors: state.errors,
          })}
        </p>
      </details>
    </>
  );
}
