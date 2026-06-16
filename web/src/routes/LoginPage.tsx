import { useRef, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { LOGIN_COPY } from './loginCopy';

type SubmitState = 'idle' | 'submitting';

export function LoginPage() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const sessionExpired = searchParams.get('expired') === '1';

  const [state, setState] = useState<SubmitState>('idle');
  const [error, setError] = useState<string | null>(null);
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
      setError(LOGIN_COPY.errorWrongPassphrase);
    } catch {
      setError(LOGIN_COPY.errorNetwork);
    } finally {
      setState('idle');
      // Move focus back to the field on a failed submit (WCAG 3.3.1).
      fieldRef.current?.focus();
    }
  }

  const submitting = state === 'submitting';

  return (
    <main className="grid min-h-dvh place-items-center bg-bg px-8 text-text">
      <div className="w-full max-w-sm rounded-[var(--radius-lg)] border border-border bg-surface p-4">
        <div className="flex flex-col items-center gap-2 pb-4">
          <img src="/logo.png" alt="Aura" width={32} height={32} className="rounded-sm" />
          <h1 className="text-xl font-medium text-text">{LOGIN_COPY.title}</h1>
          <p className="text-sm text-text-muted">{LOGIN_COPY.subtitle}</p>
        </div>

        {sessionExpired ? (
          <p role="status" className="mb-3 text-sm text-warning">
            {LOGIN_COPY.sessionExpired}
          </p>
        ) : null}

        <form
          aria-label={LOGIN_COPY.cta}
          onSubmit={(event) => {
            event.preventDefault();
            void submit(event.currentTarget);
          }}
          className="flex flex-col gap-3"
          noValidate
        >
          <div className="flex flex-col gap-1">
            <label htmlFor="passphrase" className="text-sm text-text">
              {LOGIN_COPY.fieldLabel}
            </label>
            <input
              ref={fieldRef}
              id="passphrase"
              name="passphrase"
              type="password"
              autoComplete="current-password"
              // Omit aria-invalid on a pristine/valid field rather than emitting
              // aria-invalid="false": `cond || undefined` makes React drop the attribute.
              // Valid aria-invalid tokens are only true|false|grammar|spelling (axe aria-valid-attr-value).
              aria-invalid={error !== null || undefined}
              aria-describedby="passphrase-hint"
              className="min-h-[var(--row-h)] rounded-[var(--radius-md)] border border-border bg-surface-2 px-3 text-sm text-text outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
            />
            <p id="passphrase-hint" className="text-[0.6875rem] text-text-faint">
              {LOGIN_COPY.fieldHint}
            </p>
          </div>

          {error !== null ? (
            <p role="alert" className="text-sm text-danger">
              {error}
            </p>
          ) : null}

          <button
            type="submit"
            disabled={submitting}
            className="min-h-[44px] rounded-[var(--radius-md)] bg-accent px-4 text-sm font-medium text-[#0B0E14] outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent disabled:opacity-70"
          >
            {submitting ? LOGIN_COPY.ctaInFlight : LOGIN_COPY.cta}
          </button>
        </form>
      </div>
    </main>
  );
}
