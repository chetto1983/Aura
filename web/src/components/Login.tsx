import { useState, useEffect } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { ExternalLink } from 'lucide-react';
import { api, ApiError } from '@/api';
import { setToken, getToken, clearToken } from '@/lib/auth';

interface TelegramInfo {
  username: string;
  url: string;
  start_url: string;
  qr_url: string;
}

// Login is the dashboard's only unauthenticated view. The user pastes a
// token they got from Telegram (/start on first run, /login after that)
// and we verify it by calling /auth/whoami. If valid, the token is stashed
// in localStorage and we navigate home; if not, the input remains and an
// error is shown.
//
// The "expired=1" query param is set by api.ts's handle401 redirect so
// returning users see a hint rather than a blank "please log in" page.
export function Login() {
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const [token, setTokenInput] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [telegram, setTelegram] = useState<TelegramInfo | null>(null);
  const [telegramState, setTelegramState] = useState<'loading' | 'ready' | 'unavailable'>('loading');

  // If the user already has a stored token, try it once; on success skip
  // the form and go home, on failure clear it so the form is rendered.
  useEffect(() => {
    const stored = getToken();
    if (!stored) return;
    let cancelled = false;
    void (async () => {
      try {
        await api.whoami();
        if (!cancelled) navigate('/', { replace: true });
      } catch {
        if (!cancelled) clearToken();
      }
    })();
    return () => { cancelled = true; };
  }, [navigate]);

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const res = await fetch('/telegram', { credentials: 'same-origin' });
        if (!res.ok) {
          if (!cancelled) setTelegramState('unavailable');
          return;
        }
        const info = (await res.json()) as TelegramInfo;
        if (!cancelled && info.start_url && info.qr_url) {
          setTelegram(info);
          setTelegramState('ready');
        } else if (!cancelled) {
          setTelegramState('unavailable');
        }
      } catch {
        if (!cancelled) setTelegramState('unavailable');
      }
    })();
    return () => { cancelled = true; };
  }, []);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    const trimmed = token.trim();
    if (!trimmed) {
      setError('Paste the token from Telegram first.');
      return;
    }
    setSubmitting(true);
    setError(null);
    setToken(trimmed);
    try {
      await api.whoami();
      navigate('/', { replace: true });
    } catch (err) {
      clearToken();
      if (err instanceof ApiError && err.status === 401) {
        // api.ts already redirected via handle401; reset the message in
        // case we beat the redirect.
        setError('That token was rejected. Ask the bot for a fresh one.');
      } else {
        const msg = err instanceof Error ? err.message : String(err);
        setError(msg);
      }
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="relative flex h-full items-center justify-center overflow-hidden p-6">
      {/* Ambient brand glow (decorative; the body gradient is also active) */}
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-0 -z-10"
        style={{
          background:
            'radial-gradient(600px 400px at 50% 35%, rgba(77, 168, 255, 0.18), transparent 65%)',
        }}
      />

      <div className="w-full max-w-md space-y-6">
        <div className="text-center">
          <div className="mx-auto mb-4 flex justify-center">
            <LoginBrandMark />
          </div>
          <h1 className="text-2xl font-semibold tracking-tight">Aura dashboard</h1>
          <p className="mt-2 text-sm text-muted-foreground">
            Paste your dashboard token to sign in. In Telegram, send
            <br />/start for first setup or /login for a fresh token.
          </p>
        </div>

        {params.get('expired') === '1' && (
          <div className="rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-sm text-amber-700 dark:text-amber-300">
            Your session expired or was revoked. Paste a fresh token below.
          </div>
        )}

        <TelegramEntry telegram={telegram} state={telegramState} />

        <form onSubmit={(e) => void submit(e)} className="space-y-3">
          <label className="block text-sm font-medium">
            Token
            <input
              type="password"
              autoFocus
              autoComplete="off"
              value={token}
              onChange={(e) => setTokenInput(e.target.value)}
              placeholder="paste here"
              className="mt-1 w-full rounded-md border bg-background px-3 py-2 text-sm font-mono"
            />
          </label>
          {error && (
            <div className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {error}
            </div>
          )}
          <button
            type="submit"
            disabled={submitting}
            className="w-full rounded-md bg-primary px-3 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
          >
            {submitting ? 'Verifying…' : 'Sign in'}
          </button>
        </form>

        <div className="text-center text-xs text-muted-foreground">
          <p>Tip: tokens are delivered only through your private Telegram chat.</p>
        </div>
      </div>
    </div>
  );
}

function TelegramEntry({
  telegram,
  state,
}: {
  telegram: TelegramInfo | null;
  state: 'loading' | 'ready' | 'unavailable';
}) {
  if (!telegram) {
    return (
      <div className="rounded-md border border-primary/20 bg-primary/5 px-3 py-3 text-center text-sm text-muted-foreground">
        {state === 'loading'
          ? 'Finding your Telegram bot...'
          : 'Start Aura, then refresh this page to show the Telegram QR and link.'}
      </div>
    );
  }

  return (
    <div className="grid gap-3 rounded-md border border-primary/20 bg-primary/5 p-3 sm:grid-cols-[176px_1fr] sm:items-center">
      <a
        href={telegram.start_url}
        target="_blank"
        rel="noreferrer"
        className="mx-auto block size-44 overflow-hidden rounded-md bg-white p-2 shadow-[0_0_24px_-14px_var(--primary)]"
        aria-label={`Open Telegram bot @${telegram.username}`}
      >
        <img
          src={telegram.qr_url}
          alt={`QR code for Telegram bot @${telegram.username}`}
          width="160"
          height="160"
          className="size-40"
        />
      </a>
      <div className="space-y-3 text-center sm:text-left">
        <div>
          <p className="text-sm font-medium text-foreground">Telegram bot</p>
          <a
            href={telegram.url}
            target="_blank"
            rel="noreferrer"
            className="text-sm text-primary underline-offset-4 hover:underline"
          >
            @{telegram.username}
          </a>
        </div>
        <a
          href={telegram.start_url}
          target="_blank"
          rel="noreferrer"
          className="inline-flex min-h-10 items-center justify-center gap-2 rounded-md bg-primary px-3 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90"
        >
          Open Telegram
          <ExternalLink size={14} aria-hidden="true" />
        </a>
        <p className="text-xs leading-5 text-muted-foreground">
          Send /start once, then paste the token Aura sends back here.
        </p>
      </div>
    </div>
  );
}

// LoginBrandMark is a larger version of the sidebar BrandMark with a
// stronger outer halo — sets the tone for the unauth'd entry point.
function LoginBrandMark() {
  return (
    <svg
      width="64"
      height="64"
      viewBox="0 0 40 40"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      aria-hidden="true"
    >
      <defs>
        <radialGradient id="aura-orb-lg" cx="50%" cy="40%" r="65%">
          <stop offset="0%" stopColor="var(--primary)" stopOpacity="0.85" />
          <stop offset="55%" stopColor="var(--primary)" stopOpacity="0.2" />
          <stop offset="100%" stopColor="var(--primary)" stopOpacity="0" />
        </radialGradient>
      </defs>
      <circle cx="20" cy="20" r="19" fill="url(#aura-orb-lg)" />
      <circle cx="20" cy="20" r="14" stroke="var(--primary)" strokeOpacity="0.55" strokeWidth="1.4" />
      <path
        d="M12 28 L20 11 L28 28 M16.5 22 L24 22 M24 11 L28 11 L28 15"
        stroke="var(--primary)"
        strokeWidth="2.4"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}
