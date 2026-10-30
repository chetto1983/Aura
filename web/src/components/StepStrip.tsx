import { TriangleAlert } from 'lucide-react';
import { useTranslation } from 'react-i18next';

// StepStrip renders the linear desktop step strip + the compact mobile "Step N of M" indicator.
// Color is never the only encoding: the current step carries data-state + aria-current, a failed
// step carries an icon, a done or failed step says so to a screen reader, and the mobile indicator
// is text. With onSelect, a step the operator has been through can be reopened.

export type StepState = 'done' | 'active' | 'upcoming' | 'failed';

interface StepStripProps<K extends string> {
  readonly steps: readonly { readonly key: K; readonly label: string; readonly state: StepState }[];
  readonly label: string;
  readonly onSelect?: (key: K) => void;
  /** The step on screen, when that is not the active one: once every step is done none is. */
  readonly current?: K;
}

const DOT_TONE: Record<Exclude<StepState, 'failed'>, string> = {
  active: 'bg-accent-text',
  done: 'bg-success',
  upcoming: 'bg-surface-3',
};

export function StepStrip<K extends string>({
  steps,
  label,
  onSelect,
  current,
}: StepStripProps<K>) {
  const { t } = useTranslation();
  const currentKey = current ?? steps.find((step) => step.state === 'active')?.key;
  const currentIndex = steps.findIndex((step) => step.key === currentKey);

  return (
    <>
      {/* Desktop: the full linear strip. */}
      <ol className="hidden items-center gap-2 lg:flex" aria-label={label}>
        {steps.map((step, i) => {
          const isCurrent = step.key === currentKey;
          const tone = isCurrent ? 'text-text' : 'text-text-muted';
          return (
            // The size sits here because a button takes its font from its parent (atmosphere.css).
            <li
              key={step.key}
              data-state={step.state}
              className="flex items-center gap-2 text-[13px] font-semibold"
            >
              {step.state === 'failed' ? (
                <TriangleAlert aria-hidden="true" className="size-3.5 shrink-0 text-danger" />
              ) : (
                <span
                  aria-hidden="true"
                  className={`h-2 w-2 rounded-full ${DOT_TONE[step.state]}`}
                />
              )}
              {onSelect !== undefined && !isCurrent && step.state !== 'upcoming' ? (
                <button
                  type="button"
                  className={`min-h-[44px] rounded-sm px-1 underline-offset-4 outline-none hover:text-text hover:underline focus-visible:ring-2 focus-visible:ring-ring ${tone}`}
                  onClick={() => {
                    onSelect(step.key);
                  }}
                >
                  {step.label}
                </button>
              ) : (
                <span aria-current={isCurrent ? 'step' : undefined} className={tone}>
                  {step.label}
                </span>
              )}
              {step.state === 'done' || step.state === 'failed' ? (
                <span className="sr-only">{t(`stepStrip.${step.state}`)}</span>
              ) : null}
              {i === steps.length - 1 ? null : (
                <span aria-hidden="true" className="mx-1 h-px w-6 bg-border" />
              )}
            </li>
          );
        })}
      </ol>

      {/* Mobile: a compact text indicator. */}
      <p className="text-[13px] font-semibold text-text-muted lg:hidden">
        {t('stepStrip.progress', { current: currentIndex + 1, total: steps.length })}
        {' · '}
        <span className="text-text">{steps[currentIndex]?.label ?? ''}</span>
      </p>
    </>
  );
}
