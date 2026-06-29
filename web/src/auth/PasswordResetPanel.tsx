import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ariaInvalid } from '../a11y/aria';
import { completePasswordReset, startPasswordReset, verifyPasswordReset } from './passwordResetApi';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { SecretInput } from '@/components/ui/secret-input';

type ResetStep = 'start' | 'verify' | 'complete' | 'done';
type SubmitState = 'idle' | 'submitting';
type ResetErrorKey =
  | 'login.reset.errors.generic'
  | 'login.reset.errors.emailRequired'
  | 'login.reset.errors.verifyRequired'
  | 'login.reset.errors.passwordRequired'
  | 'login.reset.errors.passwordMismatch';
type ResetNoticeKey = 'login.reset.noticeCodeSent' | null;

interface PasswordResetPanelProps {
  readonly onCancel: () => void;
}

export function PasswordResetPanel({ onCancel }: PasswordResetPanelProps) {
  const { t } = useTranslation();
  const [step, setStep] = useState<ResetStep>('start');
  const [state, setState] = useState<SubmitState>('idle');
  const [email, setEmail] = useState('');
  const [code, setCode] = useState('');
  const [answer, setAnswer] = useState('');
  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [resetToken, setResetToken] = useState('');
  const [error, setError] = useState<ResetErrorKey | null>(null);
  const [notice, setNotice] = useState<ResetNoticeKey>(null);
  const emailRef = useRef<HTMLInputElement>(null);
  const codeRef = useRef<HTMLInputElement>(null);
  const passwordRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (step === 'start') emailRef.current?.focus();
    if (step === 'verify') codeRef.current?.focus();
    if (step === 'complete') passwordRef.current?.focus();
  }, [step]);

  async function submitStart() {
    const normalizedEmail = email.trim();
    if (normalizedEmail === '') {
      setError('login.reset.errors.emailRequired');
      emailRef.current?.focus();
      return;
    }
    setError(null);
    setState('submitting');
    try {
      await startPasswordReset({ email: normalizedEmail });
      setEmail(normalizedEmail);
      setNotice('login.reset.noticeCodeSent');
      setStep('verify');
    } catch {
      setError('login.reset.errors.generic');
      emailRef.current?.focus();
    } finally {
      setState('idle');
    }
  }

  async function submitVerify() {
    const normalizedCode = code.trim();
    const normalizedAnswer = answer.trim();
    if (normalizedCode === '' || normalizedAnswer === '') {
      setError('login.reset.errors.verifyRequired');
      if (normalizedCode === '') {
        codeRef.current?.focus();
      }
      return;
    }
    setError(null);
    setState('submitting');
    try {
      const res = await verifyPasswordReset({
        email,
        code: normalizedCode,
        answer: normalizedAnswer,
      });
      if (res.resetToken.trim() === '') {
        throw new Error('empty reset token');
      }
      setResetToken(res.resetToken);
      setStep('complete');
      setNotice(null);
    } catch {
      setError('login.reset.errors.generic');
      codeRef.current?.focus();
    } finally {
      setState('idle');
    }
  }

  async function submitComplete() {
    if (password === '' || confirmPassword === '') {
      setError('login.reset.errors.passwordRequired');
      passwordRef.current?.focus();
      return;
    }
    if (password !== confirmPassword) {
      setError('login.reset.errors.passwordMismatch');
      return;
    }
    setError(null);
    setState('submitting');
    try {
      await completePasswordReset({ resetToken, password });
      setStep('done');
      setPassword('');
      setConfirmPassword('');
    } catch {
      setError('login.reset.errors.generic');
      passwordRef.current?.focus();
    } finally {
      setState('idle');
    }
  }

  function submit(event: { preventDefault: () => void }) {
    event.preventDefault();
    if (step === 'start') void submitStart();
    if (step === 'verify') void submitVerify();
    if (step === 'complete') void submitComplete();
  }

  const submitting = state === 'submitting';

  if (step === 'done') {
    return (
      <div className="flex flex-col gap-4" aria-live="polite">
        <div className="flex flex-col gap-1">
          <h2 className="text-lg font-semibold text-text">{t('login.reset.doneTitle')}</h2>
          <p className="text-sm text-text-muted">{t('login.reset.doneBody')}</p>
        </div>
        <Button type="button" onClick={onCancel}>
          {t('login.reset.backToSignIn')}
        </Button>
      </div>
    );
  }

  const formLabel =
    step === 'start'
      ? t('login.reset.startForm')
      : step === 'verify'
        ? t('login.reset.verifyForm')
        : t('login.reset.completeForm');

  const cta =
    step === 'start'
      ? submitting
        ? t('login.reset.startInFlight')
        : t('login.reset.startCta')
      : step === 'verify'
        ? submitting
          ? t('login.reset.verifyInFlight')
          : t('login.reset.verifyCta')
        : submitting
          ? t('login.reset.completeInFlight')
          : t('login.reset.completeCta');

  return (
    <section className="flex flex-col gap-4" aria-labelledby="password-reset-title">
      <div className="flex flex-col gap-1">
        <h2 id="password-reset-title" className="text-lg font-semibold text-text">
          {t('login.reset.title')}
        </h2>
        <p className="text-sm text-text-muted">{t('login.reset.subtitle')}</p>
      </div>

      <div aria-live="polite">
        {notice !== null ? (
          <Alert role="status">
            <AlertDescription>{t(notice)}</AlertDescription>
          </Alert>
        ) : null}
      </div>

      <form
        aria-busy={submitting}
        aria-label={formLabel}
        className="flex flex-col gap-3"
        noValidate
        onSubmit={submit}
      >
        {step === 'start' ? (
          <div className="flex flex-col gap-1">
            <Label htmlFor="reset-email">{t('login.authula.emailLabel')}</Label>
            <Input
              ref={emailRef}
              id="reset-email"
              name="email"
              type="email"
              autoComplete="username"
              value={email}
              onChange={(event) => {
                setEmail(event.target.value);
              }}
              aria-invalid={ariaInvalid(error === 'login.reset.errors.emailRequired')}
              className="min-h-[var(--row-h)] bg-surface-2 text-sm"
            />
          </div>
        ) : null}

        {step === 'verify' ? (
          <>
            <div className="flex flex-col gap-1">
              <Label htmlFor="reset-code">{t('login.reset.codeLabel')}</Label>
              <Input
                ref={codeRef}
                id="reset-code"
                name="code"
                type="text"
                inputMode="numeric"
                autoComplete="one-time-code"
                value={code}
                onChange={(event) => {
                  setCode(event.target.value);
                }}
                aria-invalid={ariaInvalid(error !== null)}
                className="min-h-[var(--row-h)] bg-surface-2 text-sm"
              />
            </div>
            <div className="flex flex-col gap-1">
              <Label htmlFor="reset-answer">{t('login.reset.answerLabel')}</Label>
              <SecretInput
                id="reset-answer"
                name="answer"
                autoComplete="off"
                value={answer}
                onChange={(event) => {
                  setAnswer(event.target.value);
                }}
                showLabel={t('secret.show', { label: t('login.reset.answerLabel') })}
                hideLabel={t('secret.hide', { label: t('login.reset.answerLabel') })}
                aria-invalid={ariaInvalid(error !== null)}
                className="min-h-[var(--row-h)] bg-surface-2 text-sm"
              />
            </div>
          </>
        ) : null}

        {step === 'complete' ? (
          <>
            <div className="flex flex-col gap-1">
              <Label htmlFor="reset-password">{t('login.reset.newPasswordLabel')}</Label>
              <SecretInput
                ref={passwordRef}
                id="reset-password"
                name="password"
                autoComplete="new-password"
                value={password}
                onChange={(event) => {
                  setPassword(event.target.value);
                }}
                showLabel={t('secret.show', { label: t('login.reset.newPasswordLabel') })}
                hideLabel={t('secret.hide', { label: t('login.reset.newPasswordLabel') })}
                aria-invalid={ariaInvalid(
                  error === 'login.reset.errors.passwordRequired' ||
                    error === 'login.reset.errors.passwordMismatch',
                )}
                className="min-h-[var(--row-h)] bg-surface-2 text-sm"
              />
            </div>
            <div className="flex flex-col gap-1">
              <Label htmlFor="reset-password-confirm">
                {t('login.reset.confirmPasswordLabel')}
              </Label>
              <SecretInput
                id="reset-password-confirm"
                name="confirm_password"
                autoComplete="new-password"
                value={confirmPassword}
                onChange={(event) => {
                  setConfirmPassword(event.target.value);
                }}
                showLabel={t('secret.show', { label: t('login.reset.confirmPasswordLabel') })}
                hideLabel={t('secret.hide', { label: t('login.reset.confirmPasswordLabel') })}
                aria-invalid={ariaInvalid(
                  error === 'login.reset.errors.passwordRequired' ||
                    error === 'login.reset.errors.passwordMismatch',
                )}
                className="min-h-[var(--row-h)] bg-surface-2 text-sm"
              />
            </div>
          </>
        ) : null}

        {error !== null ? (
          <Alert variant="destructive">
            <AlertDescription>{t(error)}</AlertDescription>
          </Alert>
        ) : null}

        <Button type="submit" disabled={submitting} className="text-sm">
          {cta}
        </Button>
        <Button
          type="button"
          variant="ghost"
          onClick={onCancel}
          className="text-sm text-text-muted"
        >
          {t('login.reset.backToSignIn')}
        </Button>
      </form>
    </section>
  );
}
