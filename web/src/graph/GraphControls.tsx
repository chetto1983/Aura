import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { labelFamilyColor } from './graphIntent';
import type { GraphSchema } from './types';
import { Button } from '@/components/ui/button';

// GraphControls is the graph workspace's read-only control pane: refresh,
// live-schema filters, and the ArcadeDB SQL preview. It owns only the local
// query-preview toggle and delegates every graph transition to GraphExplorer.

export interface GraphControlsProps {
  readonly schema: GraphSchema | undefined;
  readonly activeLabels: ReadonlySet<string>;
  readonly activeRelTypes: ReadonlySet<string>;
  readonly query: string;
  readonly onRefresh: () => void;
  readonly onToggleLabel: (label: string) => void;
  readonly onToggleRelType: (relType: string) => void;
}

const EMPTY_SCHEMA: GraphSchema = { labels: [], rel_types: [] };

function ToggleChip({
  label,
  color,
  active,
  onToggle,
}: {
  readonly label: string;
  readonly color: string;
  readonly active: boolean;
  readonly onToggle: () => void;
}) {
  return (
    <Button
      type="button"
      variant={active ? 'default' : 'outline'}
      aria-pressed={active}
      onClick={onToggle}
      className="h-auto justify-start whitespace-normal px-3 py-2 text-left text-[13px]"
    >
      <span
        aria-hidden="true"
        className="size-2.5 shrink-0 rounded-full ring-1 ring-white/30"
        style={{ backgroundColor: color }}
      />
      <span className="block min-w-0 overflow-wrap-anywhere">{label}</span>
    </Button>
  );
}

export function GraphControls({
  schema,
  activeLabels,
  activeRelTypes,
  query,
  onRefresh,
  onToggleLabel,
  onToggleRelType,
}: GraphControlsProps) {
  const { t } = useTranslation();
  const [queryOpen, setQueryOpen] = useState(false);

  const labels = schema?.labels ?? [];
  const relTypes = schema?.rel_types ?? [];
  const colorSchema = schema ?? EMPTY_SCHEMA;

  return (
    <div className="flex h-full min-h-0 flex-col gap-6 overflow-y-auto bg-surface p-4">
      <Button
        type="button"
        onClick={onRefresh}
        className="h-auto px-3 py-2 text-[14px] leading-tight"
      >
        <span className="block min-w-0 overflow-wrap-anywhere">{t('graph.cta.refreshMemory')}</span>
      </Button>

      {labels.length > 0 ? (
        <section className="flex flex-col gap-2">
          <h3 className="text-[13px] font-semibold uppercase tracking-wide text-text-muted">
            {t('graph.filter.labels')}
          </h3>
          <div className="flex flex-col gap-1">
            {labels.map((label) => (
              <ToggleChip
                key={label}
                label={label}
                color={labelFamilyColor(label, undefined, colorSchema)}
                active={activeLabels.has(label)}
                onToggle={() => {
                  onToggleLabel(label);
                }}
              />
            ))}
          </div>
        </section>
      ) : null}

      {relTypes.length > 0 ? (
        <section className="flex flex-col gap-2">
          <h3 className="text-[13px] font-semibold uppercase tracking-wide text-text-muted">
            {t('graph.filter.relTypes')}
          </h3>
          <div className="flex flex-col gap-1">
            {relTypes.map((relType) => (
              <ToggleChip
                key={relType}
                label={relType}
                color={labelFamilyColor(relType, undefined, colorSchema)}
                active={activeRelTypes.has(relType)}
                onToggle={() => {
                  onToggleRelType(relType);
                }}
              />
            ))}
          </div>
        </section>
      ) : null}

      {query.length > 0 ? (
        <section className="mt-auto flex flex-col gap-2">
          <Button
            type="button"
            variant="outline"
            aria-expanded={queryOpen}
            onClick={() => {
              setQueryOpen((open) => !open);
            }}
            className="justify-start px-3 text-left text-[13px] text-accent-text"
          >
            {queryOpen ? t('graph.query.hide') : t('graph.query.show')}
          </Button>
          {queryOpen ? (
            <div className="flex flex-col gap-1">
              <pre className="overflow-x-auto whitespace-pre-wrap break-words rounded-md bg-bg p-3 font-mono text-[13px] text-text">
                {query}
              </pre>
              <p className="text-[13px] text-text-muted">{t('graph.query.note')}</p>
            </div>
          ) : null}
        </section>
      ) : null}
    </div>
  );
}
