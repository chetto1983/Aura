// onboardingWizardModel.ts holds the OnboardingWizard's PURE logic — extracted out of the JSX-heavy
// component so every branch is directly unit-testable (the 28-03 mutation-hardening playbook:
// "extract pure logic for direct unit tests so its branch/equality mutants are killable without the
// DOM"). No React, no className strings here — only the wizard's decision logic + the step model.

/** The linear wizard phases, in order. */
export type Phase = 'credentials' | 'capabilities' | 'interview' | 'review' | 'complete';

export const PHASES: readonly Phase[] = [
  'credentials',
  'capabilities',
  'interview',
  'review',
  'complete',
];

/** The mapped provision-error copy key, or undefined when there is no error. */
export type ProvisionErrorKind = 'noCapability' | 'duplicate' | 'rolledBack';

/** The interview session statuses that end the interview (advance the wizard to review). */
const TERMINAL_STATUSES: ReadonlySet<string> = new Set(['completed', 'skipped', 'canceled']);

/** isTerminalStatus reports whether an interview /step status ends the interview. */
export function isTerminalStatus(status: string): boolean {
  return TERMINAL_STATUSES.has(status);
}

/** isAuthError matches the data-layer's Error("HTTP 401") so the wizard renders the sign-in-again
 * auth-error state instead of the generic error/permission copy (the GraphExplorer precedent). */
export function isAuthError(err: unknown): boolean {
  return err instanceof Error && err.message === 'HTTP 401';
}

/** provisionErrorKind maps a thrown Error("HTTP <n>") from /provision to the matching distinct copy
 * key: a 403 → no-capability, a 409 → duplicate/empty email, anything else → rolled-back. */
export function provisionErrorKind(err: unknown): ProvisionErrorKind {
  if (err instanceof Error) {
    if (err.message === 'HTTP 403') return 'noCapability';
    if (err.message === 'HTTP 409') return 'duplicate';
  }
  return 'rolledBack';
}

/** phaseIndex is the 0-based position of a phase in the linear flow (−1 if unknown). */
export function phaseIndex(phase: Phase): number {
  return PHASES.indexOf(phase);
}

/** credentialsValid gates the credentials → capabilities advance: a non-empty email AND a non-empty
 * password (the password is required for provisioning). */
export function credentialsValid(email: string, password: string): boolean {
  return email.trim() !== '' && password !== '';
}

/** A stepper item's visual state relative to the active phase. */
export type StepState = 'done' | 'active' | 'upcoming';

/** stepState classifies a stepper index against the active phase index (drives the dot/label tone
 * without color being the only encoding — the active step also carries aria-current). */
export function stepState(index: number, activeIndex: number): StepState {
  if (index === activeIndex) return 'active';
  if (index < activeIndex) return 'done';
  return 'upcoming';
}
