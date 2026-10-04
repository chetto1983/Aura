import { CheckCircle2, Database, Network } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { GraphSchema } from './types';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';

// The graph's empty state: the schema is online but no connected memory is stored yet, so
// it shows what the database can hold instead of a blank canvas.
export function EvidenceReadinessBoard({
  schema,
  onRefresh,
}: {
  readonly schema: GraphSchema | undefined;
  readonly onRefresh: () => void;
}) {
  const { t } = useTranslation();
  const labels = schema?.labels ?? [];
  const relTypes = schema?.rel_types ?? [];
  const visibleLabels = labels.slice(0, 6);
  const visibleRelTypes = relTypes.slice(0, 5);
  const hiddenLabelCount = Math.max(0, labels.length - visibleLabels.length);
  const hiddenRelTypeCount = Math.max(0, relTypes.length - visibleRelTypes.length);

  return (
    <section
      aria-label={t('graph.empty.readinessAria')}
      className="flex h-full min-h-0 items-center justify-center overflow-y-auto p-4 text-left sm:p-6"
    >
      <div className="grid w-full max-w-5xl gap-5 lg:grid-cols-[minmax(0,1fr)_18rem]">
        <div className="min-w-0">
          <div className="mb-4 flex flex-wrap items-center gap-2">
            <Badge variant="success" className="gap-1.5">
              <CheckCircle2 aria-hidden="true" />
              {t('graph.empty.schemaOnline')}
            </Badge>
            <Badge variant="secondary">
              {t('graph.empty.nodeTypeCount', { count: labels.length })}
            </Badge>
            <Badge variant="secondary">
              {t('graph.empty.connectionTypeCount', { count: relTypes.length })}
            </Badge>
          </div>
          <h2 className="font-display text-[22px] font-semibold text-text">
            {t('graph.empty.heading')}
          </h2>
          <p className="mt-2 max-w-2xl text-[15px] leading-relaxed text-text-muted">
            {t('graph.empty.body')}
          </p>
          <div className="mt-5 flex flex-wrap gap-2">
            <Button type="button" onClick={onRefresh} className="min-w-0">
              <span className="block min-w-0 overflow-wrap-anywhere">
                {t('graph.cta.refreshMemory')}
              </span>
            </Button>
          </div>
        </div>

        <div className="grid min-w-0 content-start gap-3">
          <div className="rounded-lg border border-border bg-surface/80 p-3">
            <div className="mb-3 flex items-center gap-2 text-[13px] font-semibold uppercase text-text-muted">
              <Database aria-hidden="true" className="size-4 text-accent-text" />
              {t('graph.empty.nodeTypes')}
            </div>
            <div className="flex flex-wrap gap-2">
              {visibleLabels.map((label) => (
                <Badge
                  key={label}
                  variant="secondary"
                  className="max-w-full overflow-wrap-anywhere"
                >
                  {label}
                </Badge>
              ))}
              {hiddenLabelCount > 0 ? <Badge variant="secondary">+{hiddenLabelCount}</Badge> : null}
            </div>
          </div>

          <div className="rounded-lg border border-border bg-surface/80 p-3">
            <div className="mb-3 flex items-center gap-2 text-[13px] font-semibold uppercase text-text-muted">
              <Network aria-hidden="true" className="size-4 text-accent-text" />
              {t('graph.empty.connectionTypes')}
            </div>
            <div className="flex flex-wrap gap-2">
              {visibleRelTypes.map((relType) => (
                <Badge
                  key={relType}
                  variant="secondary"
                  className="max-w-full overflow-wrap-anywhere"
                >
                  {relType}
                </Badge>
              ))}
              {hiddenRelTypeCount > 0 ? (
                <Badge variant="secondary">+{hiddenRelTypeCount}</Badge>
              ) : null}
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}
