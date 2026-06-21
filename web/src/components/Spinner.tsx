// Spinner — the in-flight indicator for primary write CTAs (governance write surfaces:
// Install server / Save changes / Trust & approve / Stage for approval). It is decorative
// (aria-hidden); the host button carries aria-busy so assistive tech announces the busy
// state. border-current inherits the button's text colour (on-accent), so it reads on the
// accent fill. The spin is disabled under prefers-reduced-motion (motion-reduce convention,
// ContextBudgetGauge.tsx precedent).
export function Spinner() {
  return (
    <span
      data-testid="spinner"
      aria-hidden="true"
      className="inline-block h-3.5 w-3.5 shrink-0 animate-spin rounded-full border-2 border-current border-t-transparent motion-reduce:animate-none"
    />
  );
}
