// OnboardingWizardNav is the shared Back / Next control bar for the form-style wizard phases
// (credentials + capabilities). The interview / review / complete phases own their own CTAs.
// Extracted from OnboardingWizard to keep that file lean (CLAUDE.md no-god-class).

import { Button } from '@/components/ui/button';

export function OnboardingWizardNav({
  onBack,
  backLabel,
  onNext,
  nextLabel,
  nextDisabled,
}: {
  readonly onBack: () => void;
  readonly backLabel: string;
  readonly onNext: () => void;
  readonly nextLabel: string;
  readonly nextDisabled: boolean;
}) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-2">
      <Button
        type="button"
        variant="outline"
        onClick={onBack}
        className="text-text-muted hover:text-text"
      >
        {backLabel}
      </Button>
      <Button type="button" disabled={nextDisabled} onClick={onNext} className="px-6">
        {nextLabel}
      </Button>
    </div>
  );
}
