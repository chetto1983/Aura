import { useTranslation } from 'react-i18next';
import { StepStrip } from '../components/StepStrip';
import { PHASES, stepState, type Phase } from './onboardingWizardModel';

// The strip walks PHASES itself rather than a second hand-kept list — the two fell out of step
// the moment the capability phase was removed from the model, and a stepper that renders a step
// the wizard can never reach is worse than no stepper.
//
// The 'complete' phase's stepper label is the Telegram step (the final post-create surface).
const STEP_LABEL_KEY: Record<Phase, string> = {
  credentials: 'onboarding.steps.credentials',
  review: 'onboarding.steps.review',
  complete: 'onboarding.steps.telegram',
};

export function OnboardingStepper({ phaseIndex }: { readonly phaseIndex: number }) {
  const { t } = useTranslation();
  const steps = PHASES.map((key, i) => ({
    key,
    label: t(STEP_LABEL_KEY[key]),
    state: stepState(i, phaseIndex),
  }));

  return (
    <div className="shrink-0 border-b border-border bg-surface px-4 py-3">
      <StepStrip steps={steps} label={t('onboarding.title')} />
    </div>
  );
}
