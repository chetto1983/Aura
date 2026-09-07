import { Users } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useWatchWorker } from './workerWatchControls';
import { Button } from '@/components/ui/button';

export function WorkerActivityButton() {
  const { t } = useTranslation();
  const { workers, statuses, watchWorker } = useWatchWorker();
  if (workers.length === 0) return null;
  const active = workers.filter((worker) =>
    ['running', 'needs_user_input', 'stalled'].includes(
      statuses.get(worker.child_id)?.status ?? worker.status,
    ),
  );
  const selected = active.at(-1) ?? workers.at(-1);
  return (
    <Button
      variant="ghost"
      className="mr-auto min-h-[44px] gap-2 text-text-muted"
      aria-label={t('swarm.openAgents')}
      onClick={() => {
        if (selected !== undefined) watchWorker(selected.child_id);
      }}
    >
      <Users className="size-4" aria-hidden="true" />
      <span>{t('swarm.team')}</span>
      <span className="rounded-full bg-surface-2 px-2 py-0.5 text-xs tabular-nums">
        {workers.length}
      </span>
      {active.length > 0 ? (
        <span className="text-xs text-accent-text">
          {t('swarm.activeCount', { count: active.length })}
        </span>
      ) : null}
    </Button>
  );
}
