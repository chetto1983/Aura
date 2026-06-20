import { useId } from 'react';
import { useTranslation } from 'react-i18next';

// CredentialStep (ONBD-01a / D-05) — the new-operator email + initial-password fields. The
// password is WRITE-ONLY: it is a masked type="password" input whose value is held in the wizard
// state ONLY to send on /provision, and it is NEVER rendered back as text anywhere afterward (no
// review echo, no confirmation readback) — the no-leak posture (T-28-06-01). The 2FA hint states
// the new user sets up two-factor on first login and the password is never shown again.
//
// This is a presentational, controlled step: the wizard owns email/password and the validity gate
// (both non-empty enables the primary CTA). Token utilities only; 44px targets; visible focus ring.

export interface CredentialStepProps {
  readonly email: string;
  readonly password: string;
  readonly onEmailChange: (value: string) => void;
  readonly onPasswordChange: (value: string) => void;
}

export function CredentialStep({
  email,
  password,
  onEmailChange,
  onPasswordChange,
}: CredentialStepProps) {
  const { t } = useTranslation();
  const emailId = useId();
  const passwordId = useId();
  const hintId = useId();

  return (
    <div className="flex flex-col gap-6">
      <h2 className="font-display text-[22px] font-semibold text-text">
        {t('onboarding.credentials.heading')}
      </h2>

      <div className="flex flex-col gap-2">
        <label htmlFor={emailId} className="text-[13px] font-semibold text-text">
          {t('onboarding.credentials.emailLabel')}
        </label>
        <input
          id={emailId}
          type="email"
          autoComplete="off"
          inputMode="email"
          value={email}
          placeholder={t('onboarding.credentials.emailPlaceholder')}
          onChange={(e) => {
            onEmailChange(e.target.value);
          }}
          className="min-h-[44px] rounded-md border border-border bg-surface-2 px-3 py-2 text-[15.5px] text-text outline-none transition-colors placeholder:text-text-faint focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-ring"
        />
      </div>

      <div className="flex flex-col gap-2">
        <label htmlFor={passwordId} className="text-[13px] font-semibold text-text">
          {t('onboarding.credentials.passwordLabel')}
        </label>
        {/* WRITE-ONLY: type="password" masks the value; it is never rendered as text again. */}
        <input
          id={passwordId}
          type="password"
          autoComplete="new-password"
          value={password}
          aria-describedby={hintId}
          onChange={(e) => {
            onPasswordChange(e.target.value);
          }}
          className="min-h-[44px] rounded-md border border-border bg-surface-2 px-3 py-2 text-[15.5px] text-text outline-none transition-colors focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-ring"
        />
        <p id={hintId} className="text-[13px] leading-relaxed text-text-muted">
          {t('onboarding.credentials.passwordHint')}
        </p>
      </div>
    </div>
  );
}
