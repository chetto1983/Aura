import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import { CapabilityAdminPanel } from '../CapabilityAdminPanel';

function urlOf(input: RequestInfo | URL): string {
  if (typeof input === 'string') return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

const ROSTER = {
  identities: [
    { id: 'id-a', name: 'Alice', kind: 'user', capabilities: ['agent.run'] },
    { id: 'id-b', name: 'Bob', kind: 'user', capabilities: ['*'] },
  ],
};

function stubRoster() {
  const spy = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = urlOf(input);
    const method = init?.method ?? 'GET';
    if (url.includes('/api/me')) {
      return Promise.resolve(
        new Response(JSON.stringify({ identity_id: 'id-b', capabilities: ['*'] }), { status: 200 }),
      );
    }
    if (url.includes('/api/admin/identities') && method !== 'GET') {
      return Promise.resolve(
        new Response(JSON.stringify({ identity_id: 'id-a', capabilities: [] }), { status: 200 }),
      );
    }
    return Promise.resolve(new Response(JSON.stringify(ROSTER), { status: 200 }));
  });
  vi.stubGlobal('fetch', spy);
  return spy;
}

function renderPanel() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <CapabilityAdminPanel />
    </QueryClientProvider>,
  );
}

describe('CapabilityAdminPanel', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('lists the selected identity capabilities with a revoke control', async () => {
    stubRoster();
    renderPanel();
    // First identity (Alice) is the default selection; her agent.run badge carries a revoke button.
    expect(await screen.findByRole('button', { name: 'Revoke agent.run' })).toBeTruthy();
  });

  it('grants a typed capability via POST', async () => {
    const spy = stubRoster();
    renderPanel();
    await screen.findByRole('button', { name: 'Revoke agent.run' });

    fireEvent.change(screen.getByLabelText('Grant'), { target: { value: 'documents.read' } });
    fireEvent.click(screen.getByRole('button', { name: 'Grant' }));

    await waitFor(() => {
      const posted = spy.mock.calls.find((c) => c[1]?.method === 'POST');
      expect(posted).toBeDefined();
      const body = posted?.[1]?.body;
      expect(typeof body).toBe('string');
      expect(JSON.parse(body as string)).toEqual({ capability: 'documents.read' });
    });
  });

  it('revokes a capability via DELETE', async () => {
    const spy = stubRoster();
    renderPanel();
    fireEvent.click(await screen.findByRole('button', { name: 'Revoke agent.run' }));

    await waitFor(() => {
      const deleted = spy.mock.calls.find((c) => c[1]?.method === 'DELETE');
      expect(deleted).toBeDefined();
      expect(deleted && urlOf(deleted[0])).toContain(
        '/api/admin/identities/id-a/capabilities/agent.run',
      );
    });
  });

  it('renders the wildcard as system-managed with no revoke control', async () => {
    stubRoster();
    renderPanel();
    await screen.findByRole('button', { name: 'Revoke agent.run' });

    // Switch to Bob (holds '*') — the wildcard shows the managed label, never a revoke button.
    fireEvent.change(screen.getByLabelText('Identity'), { target: { value: 'id-b' } });
    expect(await screen.findByText('Full access (system-managed)')).toBeTruthy();
    expect(screen.queryByRole('button', { name: /Revoke \*/ })).toBeNull();
  });

  it('surfaces the roster error state', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        if (urlOf(input).includes('/api/me')) {
          return Promise.resolve(
            new Response(JSON.stringify({ identity_id: 'x', capabilities: ['*'] }), { status: 200 }),
          );
        }
        return Promise.resolve(new Response('boom', { status: 500 }));
      }),
    );
    renderPanel();
    expect(
      await screen.findByText("Couldn't load identities. Check the server and try again."),
    ).toBeTruthy();
  });
});
