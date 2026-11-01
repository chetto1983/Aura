import { useTranslation } from 'react-i18next';
import { RuntimeHealthPanel } from '../health/RuntimeHealthPanel';
import { useRuntimeHealth } from '../health/useRuntimeHealth';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';

type Tone = 'success' | 'warning' | 'danger';

function toneClass(tone: Tone): string {
  if (tone === 'success') return 'bg-success';
  if (tone === 'danger') return 'bg-danger';
  return 'bg-warning';
}

export function RuntimeStatusChip() {
  const { t } = useTranslation();
  const { readyz, readyzError, isPending } = useRuntimeHealth();
  const hasResult = readyz !== undefined || readyzError;
  const label = !hasResult
    ? t('health.status.unknown')
    : readyzError || !readyz
      ? t('health.status.unavailable')
      : readyz.status === 200 && readyz.body.ready
        ? t('health.status.ready')
        : t('health.status.degraded');
  const tone: Tone = !hasResult
    ? 'warning'
    : readyzError || !readyz
      ? 'danger'
      : readyz.status === 200 && readyz.body.ready
        ? 'success'
        : 'warning';

  return (
    <div
      role="status"
      aria-busy={isPending}
      aria-label={`${t('health.title')}: ${label}`}
      className="shrink-0"
    >
      <Popover>
        <PopoverTrigger
          aria-label={t('health.showDetails', { title: t('health.title'), status: label })}
          className="inline-flex min-h-9 items-center gap-2 rounded-[var(--radius-pill)] border border-border bg-surface-2 px-2.5 text-xs font-medium text-text-muted outline-none transition-colors hover:border-border-strong hover:text-text focus-visible:ring-2 focus-visible:ring-ring data-[state=open]:border-border-strong data-[state=open]:text-text sm:px-3"
        >
          <span aria-hidden="true" className={`h-2 w-2 shrink-0 rounded-sm ${toneClass(tone)}`} />
          <span>{label}</span>
        </PopoverTrigger>
        <PopoverContent align="end" className="w-80 max-w-[calc(100vw-2rem)] p-0">
          <RuntimeHealthPanel />
        </PopoverContent>
      </Popover>
    </div>
  );
}
