import type { ReactNode } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import { HttpError } from '../../api/json';

vi.mock('../mcpAuthApi', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../mcpAuthApi')>()),
  fetchMcpAuthorization: vi.fn(),
  startMcpAuthorization: vi.fn(),
  pollMcpAuthorizationFlow: vi.fn(),
  revokeMcpAuthorization: vi.fn(),
  pasteMcpAuthorization: vi.fn(),
}));

const api = await import('../mcpAuthApi');
const { McpAuthorizationPanel } = await import('../McpAuthorizationPanel');

// The board's tool count, verification outcome and last error all come from the per-server
// probe query. Measured 2026-09-23 on the 192.168.101.158 install: Linear was authorized and
// mounted with 68 tools, while its card kept the probe taken BEFORE consent — 0 tools,
// "dial failed", "this identity has not authorized this server" — until a page reload.
const PROBE_KEY = ['governance', 'mcp', 'probe', 'linear'];
const STALE_PROBE = { name: 'linear', ok: false, tool_count: 0, detail: 'dial failed' };

function renderPanel() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  client.setQueryData(PROBE_KEY, STALE_PROBE);
  function Providers({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  }
  render(<McpAuthorizationPanel serverName="linear" />, { wrapper: Providers });
  return client;
}

function probeInvalidated(client: QueryClient): boolean {
  return client.getQueryState(PROBE_KEY)?.isInvalidated === true;
}

afterEach(() => {
  vi.resetAllMocks();
  vi.unstubAllGlobals();
});

describe('McpAuthorizationPanel', () => {
  it('opens the private desktop and keeps its link after polling the flow', async () => {
    vi.mocked(api.fetchMcpAuthorization).mockResolvedValue({ supported: true, authorized: false });
    const pending = {
      flowId: 'f1',
      server: 'linear',
      status: 'authorization_required' as const,
      authorizationUrl: 'https://linear.app/consent',
    };
    const desktop = '/signing-in/signin-0123456789abcdef01234567?flow=f1';
    vi.mocked(api.startMcpAuthorization).mockResolvedValue({ ...pending, signInUrl: desktop });
    vi.mocked(api.pollMcpAuthorizationFlow).mockResolvedValue(pending);
    const open = vi.spyOn(window, 'open').mockReturnValue(null);
    renderPanel();
    fireEvent.click(await screen.findByRole('button', { name: 'Connect' }));
    await waitFor(() => {
      expect(api.pollMcpAuthorizationFlow).toHaveBeenCalled();
    });
    expect(open).toHaveBeenCalledWith(desktop, '_blank', 'noopener,noreferrer');
    expect(await screen.findByDisplayValue(desktop)).not.toBeNull();
  });

  it('prepares the image before starting and opening a desktop', async () => {
    vi.mocked(api.fetchMcpAuthorization).mockResolvedValue({ supported: true, authorized: false });
    vi.mocked(api.startMcpAuthorization)
      .mockResolvedValueOnce({ preparing: true, reason: 'Download first' })
      .mockResolvedValueOnce({
        flowId: 'f1',
        server: 'linear',
        status: 'authorization_required',
        signInUrl: '/signing-in/session?flow=f1',
      });
    vi.mocked(api.pollMcpAuthorizationFlow).mockResolvedValue({
      flowId: 'f1',
      server: 'linear',
      status: 'authorization_required',
    });
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response(JSON.stringify({ present: true, pulling: false })))),
    );
    const open = vi.spyOn(window, 'open').mockReturnValue(null);
    renderPanel();
    fireEvent.click(await screen.findByRole('button', { name: 'Connect' }));
    await waitFor(() => {
      expect(open).toHaveBeenCalledWith(
        '/signing-in/session?flow=f1',
        '_blank',
        'noopener,noreferrer',
      );
    });
    expect(api.startMcpAuthorization).toHaveBeenCalledTimes(2);
  });

  it('offers the pasted return address when the server chooses the fallback', async () => {
    vi.mocked(api.fetchMcpAuthorization).mockResolvedValue({ supported: true, authorized: false });
    const pending = {
      flowId: 'f1',
      server: 'linear',
      status: 'authorization_required' as const,
      authorizationUrl: 'https://linear.app/consent',
      via: 'paste' as const,
      reason: 'This address needs your own browser',
    };
    vi.mocked(api.startMcpAuthorization).mockResolvedValue(pending);
    vi.mocked(api.pollMcpAuthorizationFlow).mockResolvedValue(pending);
    vi.spyOn(window, 'open').mockReturnValue(null);
    renderPanel();
    fireEvent.click(await screen.findByRole('button', { name: 'Connect' }));
    expect(await screen.findByLabelText('Return address')).not.toBeNull();
    expect(screen.getByText(pending.reason)).not.toBeNull();
  });

  it('ends the waiting state when closing the desktop removes the flow', async () => {
    vi.mocked(api.fetchMcpAuthorization).mockResolvedValue({ supported: true, authorized: false });
    vi.mocked(api.startMcpAuthorization).mockResolvedValue({
      flowId: 'f1',
      server: 'linear',
      status: 'authorization_required',
      authorizationUrl: 'https://linear.app/consent',
    });
    vi.mocked(api.pollMcpAuthorizationFlow).mockRejectedValue(new HttpError(404));
    vi.spyOn(window, 'open').mockReturnValue(null);
    renderPanel();
    fireEvent.click(await screen.findByRole('button', { name: 'Connect' }));
    expect((await screen.findByRole('alert')).textContent).toContain('sign-in ended');
    expect(screen.queryByDisplayValue('https://linear.app/consent')).toBeNull();
  });
  for (const refused of [false, true]) {
    it(
      refused ? 'shows why a pasted address was refused' : 'completes a flow from a pasted address',
      async () => {
        vi.mocked(api.fetchMcpAuthorization).mockResolvedValue({
          supported: true,
          authorized: false,
        });
        const pending = {
          flowId: 'f1',
          server: 'linear',
          status: 'authorization_required' as const,
          authorizationUrl: 'https://linear.app/oauth/authorize?state=s',
        };
        vi.mocked(api.startMcpAuthorization).mockResolvedValue(pending);
        vi.mocked(api.pollMcpAuthorizationFlow).mockResolvedValue(pending);
        if (refused) {
          vi.mocked(api.pasteMcpAuthorization).mockRejectedValue(
            new Error('This address does not match the pending sign-in.'),
          );
        } else {
          vi.mocked(api.pasteMcpAuthorization).mockResolvedValue({
            ...pending,
            status: 'approved',
          });
        }
        const open = vi.spyOn(window, 'open').mockReturnValue(null);
        const client = renderPanel();
        fireEvent.click(await screen.findByRole('button', { name: 'Use your own browser' }));
        await waitFor(() => {
          expect(api.startMcpAuthorization).toHaveBeenCalledWith('linear', 'paste');
        });
        expect(open).toHaveBeenCalledWith(
          pending.authorizationUrl,
          '_blank',
          'noopener,noreferrer',
        );
        const address = 'http://127.0.0.1:43121/aura/mcp/oauth/callback?state=s&code=c';
        fireEvent.change(await screen.findByLabelText('Return address'), {
          target: { value: address },
        });
        fireEvent.click(screen.getByRole('button', { name: 'Complete sign-in' }));
        await waitFor(() => {
          expect(api.pasteMcpAuthorization).toHaveBeenCalledWith('f1', address);
        });
        if (refused) {
          expect((await screen.findByRole('alert')).textContent).toContain(
            'This address does not match the pending sign-in.',
          );
          expect(probeInvalidated(client)).toBe(false);
        } else {
          await waitFor(() => {
            expect(probeInvalidated(client)).toBe(true);
          });
          expect(screen.queryByLabelText('Return address')).toBeNull();
        }
      },
    );
  }

  it('offers to authorize again when the access expired', async () => {
    vi.mocked(api.fetchMcpAuthorization).mockResolvedValue({
      supported: true,
      authorized: false,
      expired: true,
    });
    renderPanel();
    expect(await screen.findByText('Access expired: authorize again')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Connect' })).toBeTruthy();
  });

  it('re-probes the server once the authorization is approved', async () => {
    vi.mocked(api.fetchMcpAuthorization).mockResolvedValue({ supported: true, authorized: false });
    vi.mocked(api.startMcpAuthorization).mockResolvedValue({
      flowId: 'f1',
      server: 'linear',
      status: 'approved',
    });
    const client = renderPanel();

    fireEvent.click(await screen.findByRole('button', { name: 'Connect' }));

    await waitFor(() => {
      expect(probeInvalidated(client)).toBe(true);
    });
  });

  it('leaves the probe alone while the consent is still pending', async () => {
    vi.mocked(api.fetchMcpAuthorization).mockResolvedValue({ supported: true, authorized: false });
    vi.mocked(api.startMcpAuthorization).mockResolvedValue({
      flowId: 'f1',
      server: 'linear',
      status: 'authorization_required',
      authorizationUrl: 'https://linear.app/oauth/authorize?state=s',
    });
    vi.mocked(api.pollMcpAuthorizationFlow).mockResolvedValue({
      flowId: 'f1',
      server: 'linear',
      status: 'authorization_required',
      authorizationUrl: 'https://linear.app/oauth/authorize?state=s',
    });
    vi.spyOn(window, 'open').mockReturnValue(null);
    const client = renderPanel();

    fireEvent.click(await screen.findByRole('button', { name: 'Connect' }));

    expect(
      await screen.findByDisplayValue('https://linear.app/oauth/authorize?state=s'),
    ).toBeTruthy();
    expect(probeInvalidated(client)).toBe(false);
  });

  it('re-probes the server after its authorization is removed', async () => {
    vi.mocked(api.fetchMcpAuthorization).mockResolvedValue({ supported: true, authorized: true });
    vi.mocked(api.revokeMcpAuthorization).mockResolvedValue({ removed: true });
    const client = renderPanel();

    fireEvent.click(await screen.findByRole('button', { name: 'Disconnect' }));

    await waitFor(() => {
      expect(probeInvalidated(client)).toBe(true);
    });
  });

  // Measured 2026-10-13 on the lab VM: after a completed consent, Disconnect brought back the
  // consent URL the panel had been handed at Connect — a state already spent — and hid the
  // Connect button, so the server could not be authorized again without a reload.
  it('offers Connect after a disconnect, never the consent URL of the flow it already finished', async () => {
    const consentURL = 'https://linear.app/oauth/authorize?state=spent';
    let authorized = false;
    vi.mocked(api.fetchMcpAuthorization).mockImplementation(() =>
      Promise.resolve({ supported: true, authorized }),
    );
    vi.mocked(api.startMcpAuthorization).mockResolvedValue({
      flowId: 'f1',
      server: 'linear',
      status: 'authorization_required',
      authorizationUrl: consentURL,
    });
    vi.mocked(api.pollMcpAuthorizationFlow).mockImplementation(() => {
      authorized = true;
      return Promise.resolve({ flowId: 'f1', server: 'linear', status: 'approved' });
    });
    vi.mocked(api.revokeMcpAuthorization).mockImplementation(() => {
      authorized = false;
      return Promise.resolve({ removed: true });
    });
    vi.spyOn(window, 'open').mockReturnValue(null);
    renderPanel();

    fireEvent.click(await screen.findByRole('button', { name: 'Connect' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Disconnect' }));

    expect(await screen.findByRole('button', { name: 'Connect' })).toBeTruthy();
    expect(screen.queryByDisplayValue(consentURL)).toBeNull();
  });
});
