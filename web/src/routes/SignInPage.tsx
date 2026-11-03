import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useParams, useSearchParams } from 'react-router';
import { getJSON } from '../api/json';
import { pollMcpAuthorizationFlow } from '../governance/mcpAuthApi';
import { Button } from '@/components/ui/button';

interface SignInOrigins {
  cockpitOrigin: string;
  streamOrigin: string;
}

export function SignInPage() {
  const { session = '' } = useParams();
  const [params] = useSearchParams();
  const flow = params.get('flow') ?? '';
  return <SignInSessionPage key={`${session}:${flow}`} session={session} flow={flow} />;
}

function SignInSessionPage({ session, flow }: { session: string; flow: string }) {
  const { t } = useTranslation();
  const [desktop, setDesktop] = useState<string>();
  const [reachable, setReachable] = useState(false);
  const [problem, setProblem] = useState<'stream' | 'expired' | 'failed'>();
  const [retry, setRetry] = useState(0);
  const ended = useRef(false);

  useEffect(() => {
    const controller = new AbortController();
    let checking = false;
    let failures = 0;
    const check = async () => {
      if (checking || ended.current) return;
      checking = true;
      try {
        const state = await pollMcpAuthorizationFlow(flow);
        if (controller.signal.aborted) return;
        failures = 0;
        const expired = state.expiresAt !== undefined && Date.parse(state.expiresAt) <= Date.now();
        if (state.status === 'approved' || state.status === 'error' || expired) {
          ended.current = true;
          if (expired || /expir|time.*out/i.test(state.error ?? '')) setProblem('expired');
          else if (state.status === 'error') setProblem('failed');
          window.close();
        }
      } catch {
        if (!controller.signal.aborted && ++failures >= 3) setProblem('failed');
      } finally {
        checking = false;
      }
    };
    void check();
    const timer = window.setInterval(() => {
      void check();
    }, 2000);
    const leave = () => {
      if (!ended.current) {
        void fetch(`/api/sign-in/${encodeURIComponent(session)}`, {
          method: 'DELETE',
          credentials: 'same-origin',
          keepalive: true,
          headers: { 'Idempotency-Key': crypto.randomUUID() },
        }).catch(() => undefined);
      }
    };
    window.addEventListener('pagehide', leave);
    return () => {
      controller.abort();
      window.clearInterval(timer);
      window.removeEventListener('pagehide', leave);
    };
  }, [flow, session]);

  useEffect(() => {
    const controller = new AbortController();
    const connect = async () => {
      try {
        const origins = await getJSON<SignInOrigins>(
          `/api/sign-in/${encodeURIComponent(session)}`,
          controller.signal,
        );
        if (
          origins.cockpitOrigin !== window.location.origin ||
          origins.streamOrigin === origins.cockpitOrigin
        )
          throw new Error('invalid sign-in origin');
        const address = `${origins.streamOrigin}/sign-in/${encodeURIComponent(session)}/`;
        setDesktop(address);
        const response = await fetch(`${address}.aura-ready`, {
          credentials: 'include',
          mode: 'cors',
          signal: controller.signal,
        });
        if (!response.ok) throw new Error('desktop unavailable');
        if (!controller.signal.aborted && !ended.current) {
          setProblem(undefined);
          setReachable(true);
        }
      } catch {
        if (!controller.signal.aborted && !ended.current) setProblem('stream');
      }
    };
    void connect();
    return () => {
      controller.abort();
    };
  }, [session, retry]);

  return (
    <main className="flex h-dvh flex-col bg-background text-foreground">
      <header className="flex flex-wrap items-center gap-3 border-b px-4 py-3">
        <h1 className="font-semibold">{t('signIn.title')}</h1>
        <p className="text-sm text-muted-foreground">{t('signIn.private')}</p>
        <a
          className="ml-auto underline"
          href="/"
          onClick={(event) => {
            const opener = window.opener as Window | null;
            if (opener !== null) {
              event.preventDefault();
              opener.focus();
              window.close();
            }
          }}
        >
          {t('signIn.ownBrowser')}
        </a>
      </header>
      {problem !== undefined && (
        <section role="alert" className="space-y-3 p-6">
          <p>{t(`signIn.${problem}`)}</p>
          {problem === 'stream' && desktop !== undefined && (
            <>
              <p>{t('signIn.certificate')}</p>
              <a
                href={desktop}
                target="_blank"
                rel="noopener noreferrer"
                className="block underline"
              >
                {t('signIn.trust')}
              </a>
              <Button
                onClick={() => {
                  setReachable(false);
                  setRetry((value) => value + 1);
                }}
              >
                {t('signIn.retry')}
              </Button>
            </>
          )}
        </section>
      )}
      {reachable && problem === undefined && (
        <iframe
          className="min-h-0 flex-1 border-0"
          title={t('signIn.desktop')}
          src={desktop}
          sandbox="allow-scripts allow-same-origin"
          allow="clipboard-read; clipboard-write"
          referrerPolicy="no-referrer"
          onError={() => {
            setReachable(false);
            setProblem('stream');
          }}
        />
      )}
      {!reachable && problem === undefined && (
        <p role="status" className="p-6">
          {t('signIn.loading')}
        </p>
      )}
    </main>
  );
}
