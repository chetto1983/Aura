import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { GraphSchema } from './types';

// SeedFilterPanel — the LEFT pane of the Frame-06 three-pane workspace: the seed CTA, the
// label/rel-type include-filter toggles (driven by the live schema), and the read-only Cypher
// preview. The Cypher preview is DISPLAY-ONLY (D-04/D-09) — it renders `result.query` as text
// in Commit Mono, NEVER an editable input. No Cypher authoring surface exists anywhere here.
//
// Filter toggles are 44px touch targets (D-03 mobile access path); accent only when active;
// `aria-invalid` is omitted when valid (the project a11y rule — never aria-invalid="false").
// The component is presentational: it raises onToggleLabel/onToggleRelType/onSeed to the
// GraphExplorer reducer; it owns only the local "Cypher expanded" toggle.

export interface SeedFilterPanelProps {
  readonly schema: GraphSchema | undefined;
  readonly activeLabels: ReadonlySet<string>;
  readonly activeRelTypes: ReadonlySet<string>;
  readonly query: string;
  readonly canSeed: boolean;
  readonly onSeed: () => void;
  readonly onToggleLabel: (label: string) => void;
  readonly onToggleRelType: (relType: string) => void;
}

function ToggleChip({
  label,
  active,
  onToggle,
}: {
  readonly label: string;
  readonly active: boolean;
  readonly onToggle: () => void;
}) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onToggle}
      className={`min-h-[44px] rounded-md border px-3 py-1 text-left text-[13px] font-semibold transition-colors ${
        active
          ? 'border-accent bg-accent text-on-accent'
          : 'border-border bg-surface-2 text-text hover:border-border-strong'
      }`}
    >
      {label}
    </button>
  );
}

export function SeedFilterPanel({
  schema,
  activeLabels,
  activeRelTypes,
  query,
  canSeed,
  onSeed,
  onToggleLabel,
  onToggleRelType,
}: SeedFilterPanelProps) {
  const { t } = useTranslation();
  const [cypherOpen, setCypherOpen] = useState(false);

  const labels = schema?.labels ?? [];
  const relTypes = schema?.rel_types ?? [];

  return (
    <div className="flex h-full min-h-0 flex-col gap-6 overflow-y-auto bg-surface p-4">
      <button
        type="button"
        onClick={onSeed}
        disabled={!canSeed}
        className="min-h-[44px] rounded-md bg-accent px-3 py-2 text-[14px] font-semibold leading-tight text-on-accent transition-opacity disabled:opacity-40"
      >
        {t('graph.cta.seedConversation')}
      </button>

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
          <button
            type="button"
            aria-expanded={cypherOpen}
            onClick={() => {
              setCypherOpen((v) => !v);
            }}
            className="min-h-[44px] rounded-md border border-border bg-surface-2 px-3 py-1 text-left text-[13px] font-semibold text-accent-text"
          >
            {cypherOpen ? t('graph.cypher.hide') : t('graph.cypher.show')}
          </button>
          {cypherOpen ? (
            <div className="flex flex-col gap-1">
              {/* Read-only — rendered as TEXT (no dangerouslySetInnerHTML), never an input. */}
              <pre className="overflow-x-auto whitespace-pre-wrap break-words rounded-md bg-bg p-3 font-mono text-[13px] text-text">
                {query}
              </pre>
              <p className="text-[13px] text-text-muted">{t('graph.cypher.note')}</p>
            </div>
          ) : null}
        </section>
      ) : null}
    </div>
  );
}
