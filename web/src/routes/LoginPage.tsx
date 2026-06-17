import { useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { ariaInvalid } from '../a11y/aria';
import { LanguageSwitcher } from '../i18n/LanguageSwitcher';
import { ThemeSwitcher } from '../theme/ThemeSwitcher';

type SubmitState = 'idle' | 'submitting';
type LoginErrorKey = 'login.errors.wrongPassphrase' | 'login.errors.network';

export function LoginPage() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const { t } = useTranslation();
  const sessionExpired = searchParams.get('expired') === '1';

  const [state, setState] = useState<SubmitState>('idle');
  const [error, setError] = useState<LoginErrorKey | null>(null);
  const [showPassword, setShowPassword] = useState(false);
  const fieldRef = useRef<HTMLInputElement>(null);

  async function submit(form: HTMLFormElement) {
    setError(null);
    setState('submitting');

    const entry = new FormData(form).get('passphrase');
    const passphrase = typeof entry === 'string' ? entry : '';
    const body = new URLSearchParams({ passphrase });

    try {
      // POST same-origin to the server's /login (Plan 24-03). On success the server
      // 303-redirects to "/" and sets the HttpOnly cookie; fetch follows the redirect,
      // so res.ok reflects the final "/" GET. A 401 is the generic auth failure.
      const res = await fetch('/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body,
        credentials: 'same-origin',
      });
      if (res.ok) {
        void navigate('/');
        return;
      }
      // The server returns a GENERIC 401 with no oracle (it cannot tell us whether the
      // secret is unset vs. wrong) — surface the wrong-passphrase copy for that case.
      setError('login.errors.wrongPassphrase');
    } catch {
      setError('login.errors.network');
    } finally {
      setState('idle');
      // Move focus back to the field on a failed submit (WCAG 3.3.1).
      fieldRef.current?.focus();
    }
  }

  const submitting = state === 'submitting';

  return (
    <main className="grid min-h-dvh place-items-center bg-bg px-6 py-8 text-text">
      <div className="w-full max-w-md rounded-[var(--radius-xl)] border border-border bg-surface p-5 shadow-[var(--shadow-popover)] sm:p-6">
        <div className="flex items-center justify-between gap-2 pb-5">
          <ThemeSwitcher />
          <LanguageSwitcher />
        </div>
        <div className="flex flex-col items-center gap-3 pb-6">
          <img
            src="/logo.png"
            alt="Aura"
            width={112}
            height={112}
            className="h-24 w-24 rounded-[var(--radius-xl)] object-cover shadow-[0_18px_70px_rgb(26_115_232_/_0.24)] sm:h-28 sm:w-28"
          />
          <h1 className="font-display text-3xl font-medium text-text">{t('login.title')}</h1>
          <p className="text-sm text-text-muted">{t('login.subtitle')}</p>
        </div>

        {sessionExpired ? (
          <p role="status" className="mb-3 text-sm text-warning">
            {t('login.sessionExpired')}
          </p>
        ) : null}

        <form
          aria-busy={submitting}
          aria-label={t('login.cta')}
          onSubmit={(event) => {
            event.preventDefault();
            void submit(event.currentTarget);
          }}
          className="flex flex-col gap-3"
          noValidate
        >
          <div className="flex flex-col gap-1">
            <label htmlFor="passphrase" className="text-sm text-text">
              {t('login.fieldLabel')}
            </label>
            <div className="relative">
              <input
                ref={fieldRef}
                id="passphrase"
                name="passphrase"
                type={showPassword ? 'text' : 'password'}
                autoComplete="current-password"
                aria-invalid={ariaInvalid(error !== null)}
                aria-describedby="passphrase-hint"
                className="min-h-[var(--row-h)] w-full rounded-[var(--radius-md)] border border-border bg-surface-2 pl-3 pr-11 text-sm text-text outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
              />
              <button
                type="button"
                onClick={() => {
                  setShowPassword((v) => !v);
                }}
                aria-pressed={showPassword}
                aria-label={showPassword ? t('login.hidePassword') : t('login.showPassword')}
                className="absolute inset-y-0 right-0 flex min-h-[var(--row-h)] min-w-11 items-center justify-center rounded-r-[var(--radius-md)] px-3 text-text-muted hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
              >
                {showPassword ? (
                  <svg
                    width="16"
                    height="16"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    aria-hidden="true"
                  >
                    <path d="M9.88 9.88a3 3 0 1 0 4.24 4.24" />
                    <path d="M10.73 5.08A10.43 10.43 0 0 1 12 5c7 0 10 7 10 7a13.16 13.16 0 0 1-1.67 2.68" />
                    <path d="M6.61 6.61A13.53 13.53 0 0 0 2 12s3 7 10 7a9.74 9.74 0 0 0 5.39-1.61" />
                    <line x1="2" y1="2" x2="22" y2="22" />
                  </svg>
                ) : (
                  <svg
                    width="16"
                    height="16"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    aria-hidden="true"
                  >
                    <path d="M2 12s3-7 10-7 10 7 10 7-3 7-10 7-10-7-10-7Z" />
                    <circle cx="12" cy="12" r="3" />
                  </svg>
                )}
              </button>
            </div>
            <p id="passphrase-hint" className="text-[0.6875rem] text-text-faint">
              {t('login.fieldHint')}
            </p>
          </div>

          {error !== null ? (
            <p role="alert" className="text-sm text-danger">
              {t(error)}
            </p>
          ) : null}

          <button
            type="submit"
            disabled={submitting}
            className="min-h-[44px] rounded-[var(--radius-md)] bg-accent px-4 text-sm font-medium text-on-accent outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent disabled:opacity-70"
          >
            {submitting ? t('login.ctaInFlight') : t('login.cta')}
          </button>
        </form>
      </div>
    </main>
  );
}
