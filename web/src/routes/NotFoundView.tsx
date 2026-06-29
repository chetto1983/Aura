import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';

// CLIENT-side 404 ONLY. This renders when React Router has no matching client
// route (a deep link the SPA doesn't know). It is NOT the server's API 404 — the
// backend returns a real 404 for excluded prefixes (/api/, /agent/, /healthz, …)
// via internal/webui.spaFallback (Plan 24-02), never this view.
export function NotFoundView() {
  const { t } = useTranslation();

  return (
    <main className="grid h-dvh place-items-center bg-bg px-6 text-center text-text">
      <div className="max-w-md">
        <h1 className="text-xl font-medium text-text">{t('notFound.title')}</h1>
        <p className="mt-2 text-sm text-text-muted">
          {t('notFound.beforeLink')}{' '}
          <Link to="/" className="text-accent-text underline-offset-2 hover:underline">
            {t('notFound.link')}
          </Link>
          .
        </p>
      </div>
    </main>
  );
}
