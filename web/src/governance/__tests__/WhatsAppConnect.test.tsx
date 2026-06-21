import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import type { WhatsAppConnectStatus } from '../governanceApi';

// WhatsAppConnect test — the cockpit "Link device" section. It mocks the connect API to drive
// each status state: connected (JID + Unlink → logout → invalidate), waiting_qr (the QR <img>
// + scan instructions), and the status-query error (the graceful bridge-offline note). It also
// covers the isWhatsAppServer recipe/source/name heuristic.

const whatsappConnectStatus = vi.fn();
const whatsappConnectLogout = vi.fn();

vi.mock('../governanceApi', async () => {
  const actual = await vi.importActual<typeof import('../governanceApi')>('../governanceApi');
  return {
    WHATSAPP_CONNECT_QR_PATH: '/api/connect/whatsapp/qr.png',
    isWhatsAppServer: actual.isWhatsAppServer,
    whatsappConnectStatus: (...a: unknown[]) => whatsappConnectStatus(...a) as Promise<unknown>,
    whatsappConnectLogout: (...a: unknown[]) => whatsappConnectLogout(...a) as Promise<void>,
  };
});

const { WhatsAppConnect } = await import('../WhatsAppConnect');
const { isWhatsAppServer } = await import('../governanceApi');

function client() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
}

function Wrapper({ children, qc }: { children: ReactNode; qc: QueryClient }) {
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

function renderConnect() {
  const qc = client();
  render(<WhatsAppConnect />, { wrapper: ({ children }) => <Wrapper qc={qc}>{children}</Wrapper> });
  return qc;
}

const CONNECTED: WhatsAppConnectStatus = {
  state: 'connected',
  paired: true,
  jid: '393331234567@s.whatsapp.net',
  connected: true,
};

const WAITING: WhatsAppConnectStatus = {
  state: 'waiting_qr',
  paired: false,
  jid: '',
  connected: false,
};

describe('isWhatsAppServer', () => {
  it('matches by name, recipe source, and source substring; rejects others', () => {
    expect(isWhatsAppServer({ name: 'whatsapp', source: '' })).toBe(true);
    expect(isWhatsAppServer({ name: 'wa', source: 'recipe:whatsapp' })).toBe(true);
    expect(isWhatsAppServer({ name: 'wa', source: 'ghcr.io/.../whatsapp-mcp' })).toBe(true);
    expect(isWhatsAppServer({ name: 'github', source: 'recipe:github' })).toBe(false);
    expect(isWhatsAppServer({ name: 'filesystem', source: 'manual' })).toBe(false);
  });
});

describe('WhatsAppConnect', () => {
  beforeEach(() => {
    whatsappConnectStatus.mockReset();
    whatsappConnectLogout.mockReset();
  });
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('shows Connected + the JID + an Unlink button when paired', async () => {
    whatsappConnectStatus.mockResolvedValue(CONNECTED);
    renderConnect();

    expect(await screen.findByText('Connected')).toBeTruthy();
    expect(screen.getByText('393331234567@s.whatsapp.net')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Unlink' })).toBeTruthy();
    // The QR image must NOT render in the connected state.
    expect(screen.queryByRole('img')).toBeNull();
  });

  it('Unlink fires the logout mutation', async () => {
    whatsappConnectStatus.mockResolvedValue(CONNECTED);
    whatsappConnectLogout.mockResolvedValue(undefined);
    renderConnect();

    const unlink = await screen.findByRole('button', { name: 'Unlink' });
    fireEvent.click(unlink);
    await waitFor(() => {
      expect(whatsappConnectLogout).toHaveBeenCalledTimes(1);
    });
  });

  it('shows the QR image + scan instructions while waiting for a scan', async () => {
    whatsappConnectStatus.mockResolvedValue(WAITING);
    renderConnect();

    const img = await screen.findByRole('img');
    expect(img.getAttribute('src')).toContain('/api/connect/whatsapp/qr.png');
    expect(img.getAttribute('src')).toContain('ts=');
    expect(screen.getByText(/Linked Devices/i)).toBeTruthy();
    // Not connected → no Unlink button.
    expect(screen.queryByRole('button', { name: 'Unlink' })).toBeNull();
  });

  it('advances the QR cache-bust tick over time while waiting', async () => {
    whatsappConnectStatus.mockResolvedValue(WAITING);
    renderConnect();

    const before = (await screen.findByRole('img')).getAttribute('src');
    expect(before).toContain('ts=0');

    // After one QR tick interval (~3s, the component's QR_TICK_MS) the cache-bust ts advances
    // so the rotated QR re-fetches.
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 3200));
    });
    await waitFor(() => {
      const after = screen.getByRole('img').getAttribute('src');
      expect(after).not.toBe(before);
    });
  });

  it('shows the bridge-offline note when the status query errors', async () => {
    whatsappConnectStatus.mockRejectedValue(new Error('HTTP 503'));
    renderConnect();

    expect(await screen.findByText(/WhatsApp bridge offline/i)).toBeTruthy();
    expect(screen.queryByRole('img')).toBeNull();
  });
});
