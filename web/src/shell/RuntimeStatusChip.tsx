import { useTranslation } from 'react-i18next';
import { useRuntimeHealth } from '../health/useRuntimeHealth';
import { Button } from '@/components/ui/button';

export function RuntimeStatusChip({ onOpen }: { readonly onOpen: () => void }) {
  const { t } = useTranslation();
  const { readyz, readyzError } = useRuntimeHealth();
  const ready = !readyzError && readyz?.status === 200 && readyz.body.ready;
  const label = ready ? t('health.status.ready') : t('health.status.degraded');

  return (
    <Button
      type="button"
      variant="outline"
      onClick={onOpen}
      aria-label={t('shell.openRuntime')}
      className="rounded-[var(--radius-pill)] bg-surface-2 px-3 text-xs font-medium text-text-muted hover:border-border-strong hover:text-text lg:hidden"
    >
      <span
        aria-hidden="true"
        className={`h-2 w-2 rounded-sm ${ready ? 'bg-success' : 'bg-warning'}`}
      />
      <span>{label}</span>
    </Button>
  );
}
