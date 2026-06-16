// Exact copy strings from 24-UI-SPEC §Copywriting Contract → Login page.
// Kept in its own module so LoginPage.tsx exports only the component (react-refresh).
export const LOGIN_COPY = {
  title: 'Aura',
  subtitle: 'Sign in to continue',
  fieldLabel: 'Operator passphrase',
  fieldHint: 'Required when Aura is exposed beyond loopback.',
  cta: 'Sign in',
  ctaInFlight: 'Signing in…',
  errorWrongPassphrase: "That passphrase didn't match. Check it and try again.",
  errorNotConfigured:
    "Sign-in isn't available — this Aura instance has no operator passphrase configured.",
  errorNetwork: "Couldn't reach Aura. Check the server is running and try again.",
  sessionExpired: 'Your session expired. Sign in again to continue.',
} as const;
