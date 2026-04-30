import { useState, useEffect } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { Key } from 'lucide-react';
import { api, ApiError } from '@/api';
import { setToken, getToken, clearToken } from '@/lib/auth';

// Login is the dashboard's only unauthenticated view. The user pastes a
// token they got from Telegram (via the request_dashboard_token tool)
// and we verify it by calling /auth/whoami. If valid, the token is
// stashed in localStorage and we navigate home; if not, the input
// remains and an error is shown.
//
// The "expired=1" query param is set by api.ts's handle401 redirect so
// returning users see a hint rather than a blank "please log in" page.
export function Login() {
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const [token, setTokenInput] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

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
    <div className="flex h-full items-center justify-center p-6">
      <div className="w-full max-w-md space-y-6">
        <div className="text-center">
          <div className="mx-auto mb-3 inline-flex size-12 items-center justify-center rounded-full bg-primary/10 text-primary">
            <Key size={20} />
          </div>
          <h1 className="text-2xl font-semibold">Aura dashboard</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            Paste your dashboard token to sign in. Ask the bot in Telegram
            <br />for a token if you don&apos;t have one.
          </p>
        </div>

        {params.get('expired') === '1' && (
          <div className="rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-sm text-amber-700 dark:text-amber-300">
            Your session expired or was revoked. Paste a fresh token below.
          </div>
        )}

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
          <p>Tip: in Telegram, ask &quot;give me a dashboard token&quot;.</p>
        </div>
      </div>
    </div>
  );
}
