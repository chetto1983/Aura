import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import { ModelSettingsPanel } from '../ModelSettingsPanel';

// The decision model is a live row: the mail watch reads it on its next tick. Saving it is one
// PUT, and it leaves no restart banner behind.

const LIST = {
  restart_required: false,
  restart_supported: true,
  restart_keys: [],
  settings: [
    { key: 'AURA_DECIDE_MODEL', value: '', has_value: false, overridden: false, applied: 'live' },
  ].map((row) => ({ ...row, label: row.key, kind: 'string', secret: false })),
};

function json(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

function stubFetch(localSidecar: boolean) {
  const calls: string[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
      calls.push(`${init?.method ?? 'GET'} ${url}`);
      if (init?.method === 'PUT') return Promise.resolve(json({ key: url.split('/').pop() }));
      if (url.startsWith('/api/settings/decisions-models')) {
        return Promise.resolve(
          json({
            models: [{ kind: 'decisions', id: 'liquid/d1.1', has_price: false }],
            local_sidecar: localSidecar,
          }),
        );
      }
      if (url.startsWith('/api/settings/')) return Promise.resolve(json({ models: [] }));
      return Promise.resolve(json(LIST));
    }),
  );
  return calls;
}

describe('ModelSettingsPanel decision model', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('saves the decision model as one live PUT, with no restart banner', async () => {
    const calls = stubFetch(true);
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <ModelSettingsPanel groups={['backends']} />
      </QueryClientProvider>,
    );

    const route = within(await screen.findByRole('radiogroup', { name: 'Decision model route' }));
    // The catalogue's local_sidecar reaches the control through the pane.
    await waitFor(() => {
      expect(route.getByRole('radio', { name: 'Local' }).hasAttribute('disabled')).toBe(false);
    });
    fireEvent.click(route.getByRole('radio', { name: 'Off' }));
    fireEvent.click(screen.getByRole('button', { name: 'Save runtime settings' }));

    await waitFor(() => {
      expect(screen.getByText('Runtime settings saved.')).toBeTruthy();
    });
    expect(calls.filter((call) => call.startsWith('PUT '))).toEqual([
      'PUT /api/settings/AURA_DECIDE_MODEL',
    ]);
    expect(calls).toContain('GET /api/settings/decisions-models');
    expect(screen.queryByRole('button', { name: 'Restart Aura' })).toBeNull();
  });
});
