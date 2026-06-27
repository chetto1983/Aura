import { useEffect, useRef, useState } from 'react';
import { Eye, EyeOff } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { ariaInvalid } from '../a11y/aria';
import {
  booleanField,
  readCookie,
  readJSON,
  stringField,
  valueOrFallback,
} from '../auth/authConfig';
import { LanguageSwitcher } from '../i18n/LanguageSwitcher';
import { ThemeSwitcher } from '../theme/ThemeSwitcher';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Checkbox } from '@/components/ui/checkbox';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

type SubmitState = 'idle' | 'submitting';
type AuthProvider = 'passphrase' | 'authula';
type AuthulaStep = 'credentials' | 'totp';
type SubmitOutcome = 'done' | 'continue' | 'failed';
type LoginErrorKey =
  | 'login.errors.wrongPassphrase'
  | 'login.errors.wrongCredentials'
  | 'login.errors.wrongCode'
  | 'login.errors.network';

interface ParticlePoint {
  id: string;
  x: number;
  y: number;
  r: number;
  delay: string;
}

interface ParticleLink {
  key: string;
  from: ParticlePoint;
  to: ParticlePoint;
  delay: string;
}

interface AuthConfig {
  provider: AuthProvider;
  authBasePath: string;
  csrfCookieName: string;
  csrfHeaderName: string;
  csrfToken: string;
}

const defaultAuthConfig: AuthConfig = {
  provider: 'passphrase',
  authBasePath: '/auth',
  csrfCookieName: '__Host-authula_csrf_token',
  csrfHeaderName: 'X-AUTHULA-CSRF-TOKEN',
  csrfToken: '',
};

const loginParticles = [
  { id: 'north-west', x: 8, y: 24, r: 0.58, delay: '-1s' },
  { id: 'west-low', x: 16, y: 68, r: 0.48, delay: '-4s' },
  { id: 'west-mid', x: 27, y: 38, r: 0.42, delay: '-2s' },
  { id: 'south-mid', x: 38, y: 78, r: 0.56, delay: '-6s' },
  { id: 'north-core', x: 47, y: 18, r: 0.5, delay: '-3s' },
  { id: 'center-core', x: 59, y: 52, r: 0.68, delay: '-5s' },
  { id: 'north-east', x: 70, y: 28, r: 0.44, delay: '-7s' },
  { id: 'south-east', x: 79, y: 72, r: 0.54, delay: '-2.5s' },
  { id: 'east-mid', x: 90, y: 42, r: 0.46, delay: '-4.5s' },
  { id: 'far-south-east', x: 96, y: 84, r: 0.62, delay: '-8s' },
] as const satisfies readonly ParticlePoint[];

const loginLinks: readonly ParticleLink[] = [
  { key: 'north-west-west-mid', from: loginParticles[0], to: loginParticles[2], delay: '0s' },
  { key: 'west-low-west-mid', from: loginParticles[1], to: loginParticles[2], delay: '-0.7s' },
  { key: 'west-low-south-mid', from: loginParticles[1], to: loginParticles[3], delay: '-1.4s' },
  { key: 'west-mid-north-core', from: loginParticles[2], to: loginParticles[4], delay: '-2.1s' },
  { key: 'west-mid-center-core', from: loginParticles[2], to: loginParticles[5], delay: '-2.8s' },
  { key: 'south-mid-center-core', from: loginParticles[3], to: loginParticles[5], delay: '-3.5s' },
  { key: 'north-core-north-east', from: loginParticles[4], to: loginParticles[6], delay: '-4.2s' },
  { key: 'center-core-north-east', from: loginParticles[5], to: loginParticles[6], delay: '-4.9s' },
  { key: 'center-core-south-east', from: loginParticles[5], to: loginParticles[7], delay: '-5.6s' },
  { key: 'north-east-east-mid', from: loginParticles[6], to: loginParticles[8], delay: '-6.3s' },
  { key: 'south-east-east-mid', from: loginParticles[7], to: loginParticles[8], delay: '-7s' },
  {
    key: 'south-east-far-south-east',
    from: loginParticles[7],
    to: loginParticles[9],
    delay: '-7.7s',
  },
];

function percent(value: number): string {
  return `${String(value)}%`;
}

async function loadAuthConfig(): Promise<AuthConfig> {
  if (typeof fetch !== 'function') return defaultAuthConfig;
  try {
    const res = await fetch('/api/auth/config', {
      headers: { Accept: 'application/json' },
      credentials: 'same-origin',
    });
    if (!res.ok) return defaultAuthConfig;
    const raw = await readJSON(res);
    if (stringField(raw, 'provider') !== 'authula') return defaultAuthConfig;

    const csrfHeaderName = valueOrFallback(
      stringField(raw, 'csrf_header_name'),
      defaultAuthConfig.csrfHeaderName,
    );
    return {
      provider: 'authula',
      authBasePath: valueOrFallback(
        stringField(raw, 'auth_base_path'),
        defaultAuthConfig.authBasePath,
      ),
      csrfCookieName: valueOrFallback(
        stringField(raw, 'csrf_cookie_name'),
        defaultAuthConfig.csrfCookieName,
      ),
      csrfHeaderName,
      csrfToken: valueOrFallback(
        stringField(raw, 'csrf_token'),
        res.headers.get(csrfHeaderName) ?? '',
      ),
    };
  } catch {
    return defaultAuthConfig;
  }
}

function authulaHeaders(config: AuthConfig): Record<string, string> {
  const token = config.csrfToken || readCookie(config.csrfCookieName);
  if (token === '') {
    throw new Error('missing csrf token');
  }
  return {
    'Content-Type': 'application/json',
    [config.csrfHeaderName]: token,
  };
}

export function LoginPage() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const { t } = useTranslation();
  const sessionExpired = searchParams.get('expired') === '1';

  const [state, setState] = useState<SubmitState>('idle');
  const [authConfig, setAuthConfig] = useState<AuthConfig>(defaultAuthConfig);
  const [authulaStep, setAuthulaStep] = useState<AuthulaStep>('credentials');
  const [useBackupCode, setUseBackupCode] = useState(false);
  const [error, setError] = useState<LoginErrorKey | null>(null);
  const [showPassword, setShowPassword] = useState(false);
  const passphraseRef = useRef<HTMLInputElement>(null);
  const emailRef = useRef<HTMLInputElement>(null);
  const codeRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    let cancelled = false;
    void loadAuthConfig().then((config) => {
      if (!cancelled) setAuthConfig(config);
    });
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (authConfig.provider === 'authula' && authulaStep === 'totp') {
      codeRef.current?.focus();
    }
  }, [authConfig.provider, authulaStep]);

  async function submitPassphrase(form: HTMLFormElement): Promise<SubmitOutcome> {
    const entry = new FormData(form).get('passphrase');
    const passphrase = typeof entry === 'string' ? entry : '';
    const body = new URLSearchParams({ passphrase });

    const res = await fetch('/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body,
      credentials: 'same-origin',
    });
    if (res.ok) {
      void navigate('/');
      return 'done';
    }
    setError('login.errors.wrongPassphrase');
    return 'failed';
  }

  async function submitAuthulaCredentials(form: HTMLFormElement): Promise<SubmitOutcome> {
    const data = new FormData(form);
    const email = data.get('email');
    const password = data.get('password');
    const res = await fetch(`${authConfig.authBasePath}/email-password/sign-in`, {
      method: 'POST',
      headers: authulaHeaders(authConfig),
      body: JSON.stringify({
        email: typeof email === 'string' ? email : '',
        password: typeof password === 'string' ? password : '',
      }),
      credentials: 'same-origin',
    });
    if (!res.ok) {
      setError('login.errors.wrongCredentials');
      return 'failed';
    }
    const payload = await readJSON(res);
    if (booleanField(payload, 'totp_redirect')) {
      setAuthulaStep('totp');
      return 'continue';
    }
    void navigate('/');
    return 'done';
  }

  async function submitAuthulaCode(form: HTMLFormElement): Promise<SubmitOutcome> {
    const data = new FormData(form);
    const code = data.get('code');
    const trustDevice = data.get('trust_device') === 'on';
    const path = useBackupCode ? '/totp/verify-backup-code' : '/totp/verify';
    const res = await fetch(`${authConfig.authBasePath}${path}`, {
      method: 'POST',
      headers: authulaHeaders(authConfig),
      body: JSON.stringify({
        code: typeof code === 'string' ? code : '',
        trust_device: trustDevice,
      }),
      credentials: 'same-origin',
    });
    if (res.ok) {
      void navigate('/');
      return 'done';
    }
    setError('login.errors.wrongCode');
    return 'failed';
  }

  function focusActiveField() {
    if (authConfig.provider === 'authula') {
      if (authulaStep === 'totp') {
        codeRef.current?.focus();
      } else {
        emailRef.current?.focus();
      }
      return;
    }
    passphraseRef.current?.focus();
  }

  async function submit(form: HTMLFormElement) {
    setError(null);
    setState('submitting');

    let outcome: SubmitOutcome = 'failed';
    try {
      if (authConfig.provider === 'authula') {
        outcome =
          authulaStep === 'totp'
            ? await submitAuthulaCode(form)
            : await submitAuthulaCredentials(form);
      } else {
        outcome = await submitPassphrase(form);
      }
    } catch {
      setError('login.errors.network');
    } finally {
      setState('idle');
      if (outcome === 'failed') {
        focusActiveField();
      }
    }
  }

  const submitting = state === 'submitting';
  const authula = authConfig.provider === 'authula';
  const credentialError = error !== null && authulaStep === 'credentials';
  const codeError = error !== null && authulaStep === 'totp';
  const passwordToggleLabel = authula
    ? showPassword
      ? t('login.authula.hidePassword')
      : t('login.authula.showPassword')
    : showPassword
      ? t('login.hidePassword')
      : t('login.showPassword');
  const submitLabel =
    authula && authulaStep === 'totp'
      ? submitting
        ? t('login.authula.verifyInFlight')
        : t('login.authula.verifyCta')
      : submitting
        ? t('login.ctaInFlight')
        : t('login.cta');

  return (
    <main className="relative isolate grid min-h-dvh place-items-center overflow-hidden bg-bg px-6 py-8 text-text">
      <LoginParticleBackground />
      <Card className="relative z-10 w-full max-w-md gap-0 rounded-[var(--radius-xl)] bg-surface/95 p-5 shadow-[0_28px_90px_rgb(0_0_0_/_0.34)] backdrop-blur-xl sm:p-6">
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
          <Alert role="status" className="mb-3">
            <AlertDescription>{t('login.sessionExpired')}</AlertDescription>
          </Alert>
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
          {!authula ? (
            <div className="flex flex-col gap-1">
              <Label htmlFor="passphrase">{t('login.fieldLabel')}</Label>
              <div className="relative">
                <Input
                  ref={passphraseRef}
                  id="passphrase"
                  name="passphrase"
                  type={showPassword ? 'text' : 'password'}
                  autoComplete="current-password"
                  aria-invalid={ariaInvalid(error !== null)}
                  aria-describedby="passphrase-hint"
                  className="min-h-[var(--row-h)] bg-surface-2 pr-11 text-sm"
                />
                <PasswordToggle
                  pressed={showPassword}
                  label={passwordToggleLabel}
                  onClick={() => {
                    setShowPassword((value) => !value);
                  }}
                />
              </div>
              <p id="passphrase-hint" className="text-[0.75rem] text-text-muted">
                {t('login.fieldHint')}
              </p>
            </div>
          ) : authulaStep === 'credentials' ? (
            <>
              <div className="flex flex-col gap-1">
                <Label htmlFor="authula-email">{t('login.authula.emailLabel')}</Label>
                <Input
                  key="authula-email"
                  ref={emailRef}
                  id="authula-email"
                  name="email"
                  type="email"
                  autoComplete="username"
                  aria-invalid={ariaInvalid(credentialError)}
                  className="min-h-[var(--row-h)] bg-surface-2 text-sm"
                />
              </div>
              <div className="flex flex-col gap-1">
                <Label htmlFor="authula-password">{t('login.authula.passwordLabel')}</Label>
                <div className="relative">
                  <Input
                    id="authula-password"
                    name="password"
                    type={showPassword ? 'text' : 'password'}
                    autoComplete="current-password"
                    aria-invalid={ariaInvalid(credentialError)}
                    className="min-h-[var(--row-h)] bg-surface-2 pr-11 text-sm"
                  />
                  <PasswordToggle
                    pressed={showPassword}
                    label={passwordToggleLabel}
                    onClick={() => {
                      setShowPassword((value) => !value);
                    }}
                  />
                </div>
              </div>
            </>
          ) : (
            <>
              <div className="flex flex-col gap-1">
                <Label htmlFor="authula-code">
                  {useBackupCode
                    ? t('login.authula.backupCodeLabel')
                    : t('login.authula.totpLabel')}
                </Label>
                <Input
                  key={useBackupCode ? 'authula-backup-code' : 'authula-totp-code'}
                  ref={codeRef}
                  id="authula-code"
                  name="code"
                  type="text"
                  inputMode={useBackupCode ? 'text' : 'numeric'}
                  autoComplete="one-time-code"
                  aria-invalid={ariaInvalid(codeError)}
                  className="min-h-[var(--row-h)] bg-surface-2 text-sm"
                />
              </div>
              <Label className="flex items-center gap-2 text-sm font-normal text-text-muted">
                <Checkbox name="trust_device" />
                {t('login.authula.trustDevice')}
              </Label>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => {
                  setUseBackupCode((value) => !value);
                  setError(null);
                }}
                className="self-start px-0 text-sm text-accent hover:bg-transparent hover:underline"
              >
                {useBackupCode ? t('login.authula.useTotpCode') : t('login.authula.useBackupCode')}
              </Button>
            </>
          )}

          {error !== null ? (
            <Alert variant="destructive">
              <AlertDescription>{t(error)}</AlertDescription>
            </Alert>
          ) : null}

          <Button type="submit" disabled={submitting} className="text-sm">
            {submitLabel}
          </Button>
        </form>
      </Card>
    </main>
  );
}

function LoginParticleBackground() {
  return (
    <div
      aria-hidden="true"
      data-login-background=""
      data-testid="login-animated-background"
      className="pointer-events-none absolute inset-0 -z-10 overflow-hidden"
    >
      <div className="login-animated-field absolute inset-0" />
      <svg
        className="login-particle-network absolute inset-0 h-full w-full"
        viewBox="0 0 100 100"
        preserveAspectRatio="xMidYMid slice"
        focusable="false"
      >
        <g className="login-network-layer">
          {loginLinks.map((link) => (
            <line
              key={link.key}
              data-login-link=""
              x1={link.from.x}
              y1={link.from.y}
              x2={link.to.x}
              y2={link.to.y}
              style={{ animationDelay: link.delay }}
            />
          ))}
          {loginParticles.map((particle) => (
            <circle
              key={particle.id}
              data-login-particle=""
              cx={particle.x}
              cy={particle.y}
              r={particle.r}
              style={{
                animationDelay: particle.delay,
                transformOrigin: `${percent(particle.x)} ${percent(particle.y)}`,
              }}
            />
          ))}
          <path
            className="login-signal-path"
            d="M8 24 L27 38 L59 52 L70 28 L90 42 L96 84"
            vectorEffect="non-scaling-stroke"
          />
        </g>
      </svg>
    </div>
  );
}

function PasswordToggle({
  pressed,
  label,
  onClick,
}: {
  pressed: boolean;
  label: string;
  onClick: () => void;
}) {
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon"
      onClick={onClick}
      aria-pressed={pressed}
      aria-label={label}
      className="absolute inset-y-0 right-0 h-auto min-h-[var(--row-h)] w-11 rounded-l-none text-text-muted hover:text-text"
    >
      {pressed ? (
        <EyeOff data-icon="icon" aria-hidden="true" focusable="false" />
      ) : (
        <Eye data-icon="icon" aria-hidden="true" focusable="false" />
      )}
    </Button>
  );
}
