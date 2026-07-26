// onboardingWizardModel.ts holds the OnboardingWizard's PURE logic — extracted out of the JSX-heavy
// component so every branch is directly unit-testable (the 28-03 mutation-hardening playbook:
// "extract pure logic for direct unit tests so its branch/equality mutants are killable without the
// DOM"). No React, no className strings here — only the wizard's decision logic + the step model.

/** The linear wizard phases, in order. */
export type Phase = 'credentials' | 'capabilities' | 'review' | 'complete';

export const PHASES: readonly Phase[] = ['credentials', 'capabilities', 'review', 'complete'];

/** The mapped provision-error copy key, or undefined when there is no error. */
export type ProvisionErrorKind = 'noCapability' | 'duplicate' | 'rolledBack';

/** isAuthError matches the data-layer's Error("HTTP 401") so the wizard renders the sign-in-again
 * auth-error state instead of the generic error/permission copy (the GraphExplorer precedent). */
export function isAuthError(err: unknown): boolean {
  return err instanceof Error && err.message === 'HTTP 401';
}

/** isForbiddenError matches Error("HTTP 403") so start/provision permission failures can
 * render the no-capability copy instead of a retryable backend outage. */
export function isForbiddenError(err: unknown): boolean {
  return err instanceof Error && err.message === 'HTTP 403';
}

/** provisionErrorKind maps a thrown Error("HTTP <n>") from /provision to the matching distinct copy
 * key: a 403 → no-capability, a 409 → duplicate/empty email, anything else → rolled-back. */
export function provisionErrorKind(err: unknown): ProvisionErrorKind {
  if (err instanceof Error) {
    if (isForbiddenError(err)) return 'noCapability';
    if (err.message === 'HTTP 409') return 'duplicate';
  }
  return 'rolledBack';
}

/** phaseIndex is the 0-based position of a phase in the linear flow (−1 if unknown). */
export function phaseIndex(phase: Phase): number {
  return PHASES.indexOf(phase);
}

/** credentialsValid gates the credentials -> capabilities advance. */
export function credentialsValid(
  email: string,
  password: string,
  confirmPassword: string,
  securityQuestion: string,
  securityAnswer: string,
): boolean {
  return (
    email.trim() !== '' &&
    password !== '' &&
    confirmPassword !== '' &&
    password === confirmPassword &&
    securityQuestion.trim() !== '' &&
    securityAnswer.trim() !== ''
  );
}

/** The per-field length caps the server enforces on the seed form (internal/agui/onboarding_api.go
 * onboardingSeedFieldMaxLen / onboardingLangMaxLen / onboardingTimezoneMaxLen). Mirrored here so an
 * over-long value is a visible inline error instead of an opaque sanitized 400. */
export const SEED_FIELD_LIMITS = {
  name: 256,
  lang: 32,
  location: 256,
  timezone: 64,
  role: 256,
  company: 256,
} as const;

export type SeedField = keyof typeof SEED_FIELD_LIMITS;

export const SEED_FIELDS: readonly SeedField[] = [
  'name',
  'lang',
  'location',
  'timezone',
  'role',
  'company',
];

/** The server counts BYTES (Go's len over the JSON string), so a UTF-16 `.length` would let an
 * accented or CJK value pass here and 400 there — an unrecoverable dead-end, since neither surface
 * offers a way back to the field from the error state. */
const SEED_ENCODER = new TextEncoder();

/** seedFieldTooLong reports whether a seed field exceeds the server's cap for that field, measured
 * in UTF-8 bytes exactly as the server measures it. */
export function seedFieldTooLong(field: SeedField, value: string): boolean {
  return SEED_ENCODER.encode(value).length > SEED_FIELD_LIMITS[field];
}

/** seedBlank reports whether the operator typed nothing at all — the submission the server derives
 * as a skip. */
export function seedBlank(seed: Partial<Record<SeedField, string | undefined>>): boolean {
  return SEED_FIELDS.every((field) => (seed[field] ?? '').trim() === '');
}

/** seedNameMissing mirrors the server's one conditional requirement: a seed that says anything must
 * say WHO. Without a name the mapper keys every fact off the placeholder subject "operator" and no
 * PERSON entity is written for them to resolve against — the orphan shape Amendment #95 exists to
 * end. A blank seed is exempt: that is the skip. */
export function seedNameMissing(seed: Partial<Record<SeedField, string | undefined>>): boolean {
  return !seedBlank(seed) && (seed.name ?? '').trim() === '';
}

/** seedValid gates the seed form's primary CTA: every field is optional and an entirely blank seed
 * is a valid submission, so the only invalid states are exceeding a cap and filling the form
 * without a name. */
export function seedValid(seed: Partial<Record<SeedField, string | undefined>>): boolean {
  return (
    !SEED_FIELDS.some((field) => seedFieldTooLong(field, seed[field] ?? '')) &&
    !seedNameMissing(seed)
  );
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
