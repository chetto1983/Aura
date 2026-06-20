// OnboardingWizardNav is the shared Back / Next control bar for the form-style wizard phases
// (credentials + capabilities). The interview / review / complete phases own their own CTAs.
// Extracted from OnboardingWizard to keep that file lean (CLAUDE.md no-god-class).

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
      <button
        type="button"
        onClick={onBack}
        className="min-h-[44px] rounded-md border border-border bg-surface-2 px-4 py-2 text-[13px] font-semibold text-text-muted outline-none transition-colors hover:border-border-strong hover:text-text focus-visible:ring-2 focus-visible:ring-ring"
      >
        {backLabel}
      </button>
      <button
        type="button"
        disabled={nextDisabled}
        onClick={onNext}
        className="min-h-[44px] rounded-md bg-accent px-6 py-2 text-[13px] font-semibold text-on-accent outline-none transition-opacity hover:opacity-90 focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
      >
        {nextLabel}
      </button>
    </div>
  );
}
