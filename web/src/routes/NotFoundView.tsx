import { Link } from 'react-router-dom';

// CLIENT-side 404 ONLY. This renders when React Router has no matching client
// route (a deep link the SPA doesn't know). It is NOT the server's API 404 — the
// backend returns a real 404 for excluded prefixes (/api/, /agent/, /healthz, …)
// via internal/webui.spaFallback (Plan 24-02), never this view.
export function NotFoundView() {
  return (
    <main className="grid h-dvh place-items-center bg-bg px-6 text-center text-text">
      <div className="max-w-md">
        <h1 className="text-xl font-medium text-text">Page not found</h1>
        <p className="mt-2 text-sm text-text-muted">
          That cockpit page doesn&apos;t exist.{' '}
          <Link to="/" className="text-accent underline-offset-2 hover:underline">
            Return to dashboard
          </Link>
          .
        </p>
      </div>
    </main>
  );
}
