import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { RuntimeHealthPanel } from '../health/RuntimeHealthPanel';
import i18n from '../i18n/i18n';

function renderPanel() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <RuntimeHealthPanel />
    </QueryClientProvider>,
  );
}

function urlOf(input: RequestInfo | URL): string {
  if (typeof input === 'string') {
    return input;
  }
  if (input instanceof URL) {
    return input.href;
  }
  return input.url;
}

// Route a stubbed fetch by URL so /healthz and /readyz return distinct bodies.
function stubHealth(
  healthz: { status: number; body: unknown },
  readyz: { status: number; body: unknown },
) {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      const r = urlOf(input).includes('/readyz') ? readyz : healthz;
      return Promise.resolve(new Response(JSON.stringify(r.body), { status: r.status }));
    }),
  );
}

describe('RuntimeHealthPanel', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    void i18n.changeLanguage('en');
  });

  it('shows the loading caption before the first poll resolves', () => {
    // A fetch that never resolves keeps both queries pending.
    const pending = new Promise<Response>(() => {
      /* never resolves */
    });
    vi.stubGlobal(
      'fetch',
      vi.fn(() => pending),
    );
    renderPanel();
    expect(screen.getByText('Checking runtime...')).toBeTruthy();
    expect(screen.getByRole('heading', { name: 'Runtime', level: 2 })).toBeTruthy();
  });

  describe('with a healthy stack', () => {
    beforeEach(() => {
      stubHealth(
        {
          status: 200,
          body: {
            ok: true,
            scheduler_last_tick: '2026-06-16T18:00:00Z',
            bind_address: '127.0.0.1:9080',
            build_version: 'dev',
          },
        },
        { status: 200, body: { ready: true, deps: { postgres: 'ok', memory: 'ok' } } },
      );
    });

    it('renders a TEXT status label for every row (never colour-only)', async () => {
      renderPanel();
      // Row labels present.
      for (const label of [
        'Liveness',
        'Readiness',
        'Postgres',
        'Memory',
        'Bind address',
        'Build',
      ]) {
        await waitFor(() => {
          expect(screen.getByText(label)).toBeTruthy();
        });
      }
      // Status text labels (the meaning carrier — not the colour) are present.
      await waitFor(() => {
        expect(screen.getByText('Live')).toBeTruthy();
      });
      // "Ready" appears for /readyz + postgres + memory → assert at least one.
      await waitFor(() => {
        expect(screen.getAllByText('Ready').length).toBeGreaterThan(0);
      });
      // Mono machine values surfaced via /healthz (D-07, no new endpoint).
      await waitFor(() => {
        expect(screen.getByText('127.0.0.1:9080')).toBeTruthy();
      });
      await waitFor(() => {
        expect(screen.getByText('dev')).toBeTruthy();
      });
    });
  });

  it('marks a not-ready dependency as Degraded (warning, with text)', async () => {
    stubHealth(
      { status: 200, body: { ok: true } },
      { status: 503, body: { ready: false, deps: { postgres: 'ok', memory: 'dial timeout' } } },
    );
    renderPanel();
    await waitFor(() => {
      expect(screen.getByText('Readiness')).toBeTruthy();
    });
    // The not-ready readiness + memory dep render the Degraded text label.
    await waitFor(() => {
      expect(screen.getAllByText('Degraded').length).toBeGreaterThan(0);
    });
  });

  it('marks readiness + deps Unavailable when only /readyz is unreachable', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        if (urlOf(input).includes('/readyz')) {
          return Promise.reject(new Error('readyz down'));
        }
        return Promise.resolve(new Response(JSON.stringify({ ok: true }), { status: 200 }));
      }),
    );
    renderPanel();
    // Liveness is Live (/healthz ok) but readiness + postgres + memory read Unavailable.
    await waitFor(() => {
      expect(screen.getByText('Live')).toBeTruthy();
    });
    await waitFor(() => {
      expect(screen.getAllByText('Unavailable').length).toBeGreaterThan(0);
    });
  });

  it('renders the unreachable alert when both probes error', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.reject(new Error('connection refused'))),
    );
    renderPanel();
    await waitFor(() => {
      const alert = screen.getByRole('alert');
      expect(alert.textContent).toContain("Can't read runtime status");
    });
  });

  it('renders the runtime health panel in Italian', async () => {
    await i18n.changeLanguage('it');
    stubHealth(
      { status: 200, body: { ok: true, bind_address: '127.0.0.1:9080', build_version: 'dev' } },
      { status: 200, body: { ready: true, deps: { postgres: 'ok', memory: 'ok' } } },
    );

    renderPanel();

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'Stato runtime', level: 2 })).toBeTruthy();
    });
    await waitFor(() => {
      expect(screen.getByText('Vitalità')).toBeTruthy();
    });
    expect(screen.getAllByText('Pronto').length).toBeGreaterThan(0);
    expect(screen.getByText('Indirizzo bind')).toBeTruthy();
  });
});
