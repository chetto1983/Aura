import { useTranslation } from 'react-i18next';
import { stepState, type Phase } from './onboardingWizardModel';

// OnboardingStepper renders the linear desktop step strip + the compact mobile "Step N of M"
// indicator. Extracted from OnboardingWizard so its per-step state logic is directly testable
// (the 28-03 mutation-hardening playbook — assert the rendered aria/data-state, not just the
// className). Color is never the only encoding: the active step carries an accent dot AND a
// data-state="active" / aria-current="step"; the mobile indicator is text.

const STEP_KEYS: readonly Phase[] = [
  'credentials',
  'capabilities',
  'interview',
  'review',
  'complete',
];

// The 'complete' phase's stepper label is the Telegram step (the final post-create surface).
const STEP_LABEL_KEY: Record<Phase, string> = {
  credentials: 'onboarding.steps.credentials',
  capabilities: 'onboarding.steps.capabilities',
  interview: 'onboarding.steps.interview',
  review: 'onboarding.steps.review',
  complete: 'onboarding.steps.telegram',
};

export function OnboardingStepper({ phaseIndex }: { readonly phaseIndex: number }) {
  const { t } = useTranslation();
  const labels = STEP_KEYS.map((key) => ({ key, label: t(STEP_LABEL_KEY[key]) }));
  const activeLabel = labels[phaseIndex]?.label ?? '';

  return (
    <div className="shrink-0 border-b border-border bg-surface px-4 py-3">
      {/* Desktop: the full linear strip. */}
      <ol className="hidden items-center gap-2 lg:flex" aria-label={t('onboarding.title')}>
        {labels.map((s, i) => {
          const state = stepState(i, phaseIndex);
          const isLast = i === labels.length - 1;
          return (
            <li key={s.key} data-state={state} className="flex items-center gap-2">
              <span
                aria-hidden="true"
                className={`h-2 w-2 rounded-full ${
                  state === 'active'
                    ? 'bg-accent-text'
                    : state === 'done'
                      ? 'bg-success'
                      : 'bg-surface-3'
                }`}
              />
              <span
                aria-current={state === 'active' ? 'step' : undefined}
                className={`text-[13px] font-semibold ${
                  state === 'active' ? 'text-text' : 'text-text-muted'
                }`}
              >
                {s.label}
              </span>
              {isLast ? null : <span aria-hidden="true" className="mx-1 h-px w-6 bg-border" />}
            </li>
          );
        })}
      </ol>

      {/* Mobile: a compact text indicator. */}
      <p className="text-[13px] font-semibold text-text-muted lg:hidden">
        {t('onboarding.progress', { current: phaseIndex + 1, total: labels.length })}
        {' · '}
        <span className="text-text">{activeLabel}</span>
      </p>
    </div>
  );
}
