import type { ReactNode } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';

vi.mock('../mcpAuthApi', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../mcpAuthApi')>()),
  fetchMcpAuthorization: vi.fn(),
  startMcpAuthorization: vi.fn(),
  pollMcpAuthorizationFlow: vi.fn(),
  revokeMcpAuthorization: vi.fn(),
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
});

describe('McpAuthorizationPanel', () => {
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
});
