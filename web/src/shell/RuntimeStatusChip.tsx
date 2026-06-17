import { useTranslation } from 'react-i18next';
import { useRuntimeHealth } from '../health/useRuntimeHealth';

export function RuntimeStatusChip({ onOpen }: { readonly onOpen: () => void }) {
  const { t } = useTranslation();
  const { readyz, readyzError } = useRuntimeHealth();
  const ready = !readyzError && readyz?.status === 200 && readyz.body.ready;
  const label = ready ? t('health.status.ready') : t('health.status.degraded');

  return (
    <button
      type="button"
      onClick={onOpen}
      aria-label={t('shell.openRuntime')}
      className="flex min-h-9 items-center gap-2 rounded-[var(--radius-pill)] border border-border bg-surface-2 px-3 text-xs font-medium text-text-muted outline-none hover:border-border-strong hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent lg:hidden"
    >
      <span
        aria-hidden="true"
        className={`h-2 w-2 rounded-sm ${ready ? 'bg-success' : 'bg-warning'}`}
      />
      <span>{label}</span>
    </button>
  );
}
