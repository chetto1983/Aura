import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ariaInvalid } from '../a11y/aria';
import { createFirstOperator } from './bootstrapApi';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { SecretInput } from '@/components/ui/secret-input';

type SubmitState = 'idle' | 'submitting' | 'done';
type BootstrapErrorKey =
  | 'login.bootstrap.errors.generic'
  | 'login.bootstrap.errors.required'
  | 'login.bootstrap.errors.passwordLength'
  | 'login.bootstrap.errors.passwordMismatch';

interface BootstrapPanelProps {
  readonly onCancel: () => void;
  readonly onCreated?: (created: { readonly email: string; readonly password: string }) => void;
}

export function BootstrapPanel({ onCancel, onCreated }: BootstrapPanelProps) {
  const { t } = useTranslation();
  const [state, setState] = useState<SubmitState>('idle');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [securityQuestion, setSecurityQuestion] = useState('');
  const [securityAnswer, setSecurityAnswer] = useState('');
  const [error, setError] = useState<BootstrapErrorKey | null>(null);
  const emailRef = useRef<HTMLInputElement>(null);
  const passwordRef = useRef<HTMLInputElement>(null);
  const confirmPasswordRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    emailRef.current?.focus();
  }, []);

  async function submit() {
    const normalizedEmail = email.trim();
    const normalizedQuestion = securityQuestion.trim();
    const normalizedAnswer = securityAnswer.trim();
    if (
      normalizedEmail === '' ||
      password === '' ||
      confirmPassword === '' ||
      normalizedQuestion === '' ||
      normalizedAnswer === ''
    ) {
      setError('login.bootstrap.errors.required');
      emailRef.current?.focus();
      return;
    }
    if (password.length < 8) {
      setError('login.bootstrap.errors.passwordLength');
      passwordRef.current?.focus();
      return;
    }
    if (password !== confirmPassword) {
      setError('login.bootstrap.errors.passwordMismatch');
      confirmPasswordRef.current?.focus();
      return;
    }
    setError(null);
    setState('submitting');
    try {
      await createFirstOperator({
        email: normalizedEmail,
        password,
        securityQuestion: normalizedQuestion,
        securityAnswer: normalizedAnswer,
      });
      onCreated?.({ email: normalizedEmail, password });
      setState('done');
      setPassword('');
      setConfirmPassword('');
    } catch {
      setError('login.bootstrap.errors.generic');
      setState('idle');
      emailRef.current?.focus();
    }
  }

  if (state === 'done') {
    return (
      <section className="flex flex-col gap-4" aria-live="polite">
        <div className="flex flex-col gap-1">
          <h2 className="text-lg font-semibold text-text">{t('login.bootstrap.doneTitle')}</h2>
          <p className="text-sm text-text-muted">{t('login.bootstrap.doneBody')}</p>
        </div>
        <Button type="button" onClick={onCancel}>
          {t('login.reset.backToSignIn')}
        </Button>
      </section>
    );
  }

  const submitting = state === 'submitting';

  return (
    <section className="flex flex-col gap-4" aria-labelledby="bootstrap-title">
      <div className="flex flex-col gap-1">
        <h2 id="bootstrap-title" className="text-lg font-semibold text-text">
          {t('login.bootstrap.title')}
        </h2>
        <p className="text-sm text-text-muted">{t('login.bootstrap.subtitle')}</p>
      </div>

      <form
        aria-busy={submitting}
        aria-label={t('login.bootstrap.form')}
        className="flex flex-col gap-3"
        noValidate
        onSubmit={(event) => {
          event.preventDefault();
          void submit();
        }}
      >
        <div className="flex flex-col gap-1">
          <Label htmlFor="bootstrap-email">{t('login.authula.emailLabel')}</Label>
          <Input
            ref={emailRef}
            id="bootstrap-email"
            name="email"
            type="email"
            autoComplete="username"
            value={email}
            onChange={(event) => {
              setEmail(event.target.value);
            }}
            aria-invalid={ariaInvalid(error === 'login.bootstrap.errors.required')}
            className="min-h-[var(--row-h)] bg-surface-2 text-sm"
          />
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="bootstrap-password">{t('login.authula.passwordLabel')}</Label>
          <SecretInput
            ref={passwordRef}
            id="bootstrap-password"
            name="password"
            autoComplete="new-password"
            value={password}
            onChange={(event) => {
              setPassword(event.target.value);
            }}
            showLabel={t('login.authula.showPassword')}
            hideLabel={t('login.authula.hidePassword')}
            aria-invalid={ariaInvalid(
              error === 'login.bootstrap.errors.required' ||
                error === 'login.bootstrap.errors.passwordLength' ||
                error === 'login.bootstrap.errors.passwordMismatch',
            )}
            className="min-h-[var(--row-h)] bg-surface-2 text-sm"
          />
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="bootstrap-confirm-password">
            {t('login.bootstrap.confirmPasswordLabel')}
          </Label>
          <SecretInput
            ref={confirmPasswordRef}
            id="bootstrap-confirm-password"
            name="confirmPassword"
            autoComplete="new-password"
            value={confirmPassword}
            onChange={(event) => {
              setConfirmPassword(event.target.value);
            }}
            showLabel={t('secret.show', { label: t('login.bootstrap.confirmPasswordLabel') })}
            hideLabel={t('secret.hide', { label: t('login.bootstrap.confirmPasswordLabel') })}
            aria-invalid={ariaInvalid(
              error === 'login.bootstrap.errors.required' ||
                error === 'login.bootstrap.errors.passwordMismatch',
            )}
            className="min-h-[var(--row-h)] bg-surface-2 text-sm"
          />
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="bootstrap-security-question">
            {t('login.bootstrap.securityQuestionLabel')}
          </Label>
          <Input
            id="bootstrap-security-question"
            name="securityQuestion"
            type="text"
            autoComplete="off"
            value={securityQuestion}
            onChange={(event) => {
              setSecurityQuestion(event.target.value);
            }}
            aria-invalid={ariaInvalid(error === 'login.bootstrap.errors.required')}
            className="min-h-[var(--row-h)] bg-surface-2 text-sm"
          />
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="bootstrap-security-answer">
            {t('login.bootstrap.securityAnswerLabel')}
          </Label>
          <SecretInput
            id="bootstrap-security-answer"
            name="securityAnswer"
            autoComplete="off"
            value={securityAnswer}
            onChange={(event) => {
              setSecurityAnswer(event.target.value);
            }}
            showLabel={t('secret.show', { label: t('login.bootstrap.securityAnswerLabel') })}
            hideLabel={t('secret.hide', { label: t('login.bootstrap.securityAnswerLabel') })}
            aria-invalid={ariaInvalid(error === 'login.bootstrap.errors.required')}
            className="min-h-[var(--row-h)] bg-surface-2 text-sm"
          />
        </div>

        {error !== null ? (
          <Alert variant="destructive">
            <AlertDescription>{t(error)}</AlertDescription>
          </Alert>
        ) : null}

        <Button type="submit" disabled={submitting} className="text-sm">
          {submitting ? t('login.bootstrap.createInFlight') : t('login.bootstrap.createCta')}
        </Button>
        <Button
          type="button"
          variant="ghost"
          onClick={onCancel}
          className="text-sm text-text-muted hover:text-text"
        >
          {t('login.reset.backToSignIn')}
        </Button>
      </form>
    </section>
  );
}
