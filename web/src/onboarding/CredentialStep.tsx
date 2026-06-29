import { useId } from 'react';
import { useTranslation } from 'react-i18next';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { SecretInput } from '@/components/ui/secret-input';

// CredentialStep (ONBD-01a / D-05): the new-operator email + initial-password fields. The
// password is masked by default and only revealable in-place by explicit operator action. Its value
// is held in wizard state ONLY to send on /provision, and it is NEVER rendered back as text anywhere
// afterward (no review echo, no confirmation readback), preserving the no-leak posture
// (T-28-06-01). The 2FA hint states the new user sets up two-factor on first login and the password
// is never shown again.
//
// This is a presentational, controlled step: the wizard owns email/password and the validity gate
// (both non-empty enables the primary CTA). Token utilities only; 44px targets; visible focus ring.

export interface CredentialStepProps {
  readonly email: string;
  readonly password: string;
  readonly confirmPassword: string;
  readonly securityQuestion: string;
  readonly securityAnswer: string;
  readonly onEmailChange: (value: string) => void;
  readonly onPasswordChange: (value: string) => void;
  readonly onConfirmPasswordChange: (value: string) => void;
  readonly onSecurityQuestionChange: (value: string) => void;
  readonly onSecurityAnswerChange: (value: string) => void;
}

export function CredentialStep({
  email,
  password,
  confirmPassword,
  securityQuestion,
  securityAnswer,
  onEmailChange,
  onPasswordChange,
  onConfirmPasswordChange,
  onSecurityQuestionChange,
  onSecurityAnswerChange,
}: CredentialStepProps) {
  const { t } = useTranslation();
  const emailId = useId();
  const passwordId = useId();
  const confirmPasswordId = useId();
  const questionId = useId();
  const answerId = useId();
  const hintId = useId();
  const mismatchId = useId();
  const answerHintId = useId();
  const passwordLabel = t('onboarding.credentials.passwordLabel');
  const confirmPasswordLabel = t('onboarding.credentials.confirmPasswordLabel');
  const securityAnswerLabel = t('onboarding.credentials.securityAnswerLabel');
  const passwordMismatch = confirmPassword !== '' && password !== confirmPassword;

  return (
    <div className="flex flex-col gap-6">
      <h2 className="font-display text-[22px] font-semibold text-text">
        {t('onboarding.credentials.heading')}
      </h2>

      <div className="flex flex-col gap-2">
        <Label htmlFor={emailId} className="text-[13px] font-semibold text-text">
          {t('onboarding.credentials.emailLabel')}
        </Label>
        <Input
          id={emailId}
          type="email"
          autoComplete="off"
          inputMode="email"
          value={email}
          placeholder={t('onboarding.credentials.emailPlaceholder')}
          onChange={(e) => {
            onEmailChange(e.target.value);
          }}
          className="bg-surface-2 text-[15.5px]"
        />
      </div>

      <div className="flex flex-col gap-2">
        <Label htmlFor={passwordId} className="text-[13px] font-semibold text-text">
          {passwordLabel}
        </Label>
        <SecretInput
          id={passwordId}
          autoComplete="new-password"
          value={password}
          aria-describedby={hintId}
          onChange={(e) => {
            onPasswordChange(e.target.value);
          }}
          showLabel={t('secret.show', { label: passwordLabel })}
          hideLabel={t('secret.hide', { label: passwordLabel })}
          className="bg-surface-2 text-[15.5px]"
        />
        <p id={hintId} className="text-[13px] leading-relaxed text-text-muted">
          {t('onboarding.credentials.passwordHint')}
        </p>
      </div>

      <div className="flex flex-col gap-2">
        <Label htmlFor={confirmPasswordId} className="text-[13px] font-semibold text-text">
          {confirmPasswordLabel}
        </Label>
        <SecretInput
          id={confirmPasswordId}
          autoComplete="new-password"
          value={confirmPassword}
          aria-describedby={passwordMismatch ? mismatchId : undefined}
          aria-invalid={passwordMismatch ? true : undefined}
          onChange={(e) => {
            onConfirmPasswordChange(e.target.value);
          }}
          showLabel={t('secret.show', { label: confirmPasswordLabel })}
          hideLabel={t('secret.hide', { label: confirmPasswordLabel })}
          className="bg-surface-2 text-[15.5px]"
        />
        {passwordMismatch ? (
          <p id={mismatchId} role="alert" className="text-[13px] leading-relaxed text-danger">
            {t('onboarding.credentials.passwordMismatch')}
          </p>
        ) : null}
      </div>

      <div className="flex flex-col gap-2">
        <Label htmlFor={questionId} className="text-[13px] font-semibold text-text">
          {t('onboarding.credentials.securityQuestionLabel')}
        </Label>
        <Input
          id={questionId}
          type="text"
          autoComplete="off"
          value={securityQuestion}
          onChange={(e) => {
            onSecurityQuestionChange(e.target.value);
          }}
          className="bg-surface-2 text-[15.5px]"
        />
      </div>

      <div className="flex flex-col gap-2">
        <Label htmlFor={answerId} className="text-[13px] font-semibold text-text">
          {securityAnswerLabel}
        </Label>
        <SecretInput
          id={answerId}
          autoComplete="off"
          value={securityAnswer}
          aria-describedby={answerHintId}
          onChange={(e) => {
            onSecurityAnswerChange(e.target.value);
          }}
          showLabel={t('secret.show', { label: securityAnswerLabel })}
          hideLabel={t('secret.hide', { label: securityAnswerLabel })}
          className="bg-surface-2 text-[15.5px]"
        />
        <p id={answerHintId} className="text-[13px] leading-relaxed text-text-muted">
          {t('onboarding.credentials.securityAnswerHint')}
        </p>
      </div>
    </div>
  );
}
