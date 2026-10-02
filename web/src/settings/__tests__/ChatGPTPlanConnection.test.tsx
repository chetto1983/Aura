import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n';
import { ChatGPTPlanConnection } from '../ChatGPTPlanConnection';

const authURL = '/browser/chatgpt-0123456789abcdef01234567';

function urlOf(input: RequestInfo | URL): string {
  return typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status });
}

async function startLogin() {
  const button = await screen.findByRole<HTMLButtonElement>('button', {
    name: 'Continue with ChatGPT',
  });
  await waitFor(() => {
    expect(button.disabled).toBe(false);
  });
  fireEvent.click(button);
}

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('ChatGPT plan connection', () => {
  it('opens authorization on click, polls and notifies when the account is ready', async () => {
    const popup = { location: { href: '' }, opener: window, close: vi.fn() };
    const open = vi.spyOn(window, 'open').mockReturnValue(popup as unknown as Window);
    let connected = false;
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      if (urlOf(input).endsWith('/login')) {
        expect(init?.credentials).toBe('same-origin');
        return Promise.resolve(
          json({
            auth_url: authURL,
            status: 'authorization_required',
          }),
        );
      }
      return Promise.resolve(
        json({
          connected,
          email: connected ? 'operator@example.com' : '',
          plan_enabled: connected,
          status: connected ? 'approved' : 'disconnected',
        }),
      );
    });
    vi.stubGlobal('fetch', fetchMock);
    const onConnectionChange = vi.fn();
    render(<ChatGPTPlanConnection onConnectionChange={onConnectionChange} />);
    await startLogin();
    await waitFor(() => {
      expect(popup.location.href).toBe(`${window.location.origin}${authURL}`);
    });
    expect(open).toHaveBeenCalledWith('about:blank', 'aura-chatgpt-login', expect.any(String));
    expect(popup.opener).toBeNull();
    connected = true;
    fireEvent(window, new Event('focus'));
    await screen.findByText('operator@example.com');
    await waitFor(() => {
      expect(onConnectionChange).toHaveBeenLastCalledWith(true);
    });
    expect(popup.close).toHaveBeenCalled();
  });

  it('provides a safe login link when the browser blocks the popup', async () => {
    vi.spyOn(window, 'open').mockReturnValue(null);
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) =>
        Promise.resolve(
          json(
            urlOf(input).endsWith('/login')
              ? {
                  auth_url: authURL,
                  status: 'authorization_required',
                }
              : { connected: false, email: '', plan_enabled: false, status: 'disconnected' },
          ),
        ),
      ),
    );
    render(<ChatGPTPlanConnection onConnectionChange={vi.fn()} />);
    await startLogin();
    const link = await screen.findByRole('link', { name: 'Open ChatGPT sign-in' });
    expect(link.getAttribute('href')).toBe(authURL);
    expect(link.getAttribute('rel')).toContain('noopener');
  });

  it('disconnects only the current authenticated connection', async () => {
    const fetchMock = vi.fn((_input: RequestInfo | URL, init?: RequestInit) =>
      Promise.resolve(
        json(
          init?.method === 'DELETE'
            ? { disconnected: true }
            : {
                connected: true,
                email: 'operator@example.com',
                plan_enabled: true,
                status: 'approved',
              },
        ),
      ),
    );
    vi.stubGlobal('fetch', fetchMock);
    const onConnectionChange = vi.fn();
    render(<ChatGPTPlanConnection onConnectionChange={onConnectionChange} />);
    fireEvent.click(await screen.findByRole('button', { name: 'Disconnect ChatGPT' }));
    await screen.findByRole('button', { name: 'Continue with ChatGPT' });
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/settings/chatgpt',
      expect.objectContaining({ method: 'DELETE', credentials: 'same-origin' }),
    );
    expect(onConnectionChange).toHaveBeenLastCalledWith(false);
  });

  it('keeps a missing inference grant disabled and displays the server explanation', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          json({
            connected: true,
            email: 'operator@example.com',
            plan_enabled: false,
            status: 'approved',
            error: 'Plan access is not enabled.',
          }),
        ),
      ),
    );
    const onConnectionChange = vi.fn();
    render(<ChatGPTPlanConnection onConnectionChange={onConnectionChange} />);
    expect(await screen.findByRole('alert')).toHaveProperty(
      'textContent',
      'Plan access is not enabled.',
    );
    expect(onConnectionChange).toHaveBeenLastCalledWith(false);
    expect(screen.getByRole('button', { name: 'Continue with ChatGPT' })).toBeDefined();
  });

  it('rejects an unexpected authorization destination and can retry', async () => {
    vi.spyOn(window, 'open').mockReturnValue(null);
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) =>
        Promise.resolve(
          json(
            urlOf(input).endsWith('/login')
              ? {
                  auth_url: 'https://untrusted.example/authorize',
                  status: 'authorization_required',
                }
              : { connected: false, email: '', plan_enabled: false, status: 'disconnected' },
          ),
        ),
      ),
    );
    render(<ChatGPTPlanConnection onConnectionChange={vi.fn()} />);
    await startLogin();
    await screen.findByRole('alert');
    expect(screen.queryByRole('link', { name: 'Open ChatGPT sign-in' })).toBeNull();
    expect(screen.getByRole('button', { name: 'Retry' })).toBeDefined();
  });

  it('keeps the authorization popup open while a retained credential awaits replacement', async () => {
    const popup = { location: { href: '' }, opener: window, close: vi.fn() };
    vi.spyOn(window, 'open').mockReturnValue(popup as unknown as Window);
    let status = 'disconnected';
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        if (urlOf(input).endsWith('/login')) {
          status = 'authorization_required';
          return Promise.resolve(json({ auth_url: authURL, status }));
        }
        return Promise.resolve(
          json({
            connected: status !== 'disconnected',
            plan_enabled: status !== 'disconnected',
            email: 'retained@example.com',
            status,
          }),
        );
      }),
    );
    const onConnectionChange = vi.fn();
    render(<ChatGPTPlanConnection onConnectionChange={onConnectionChange} />);
    await startLogin();
    await waitFor(() => {
      expect(popup.location.href).toBe(`${window.location.origin}${authURL}`);
    });
    fireEvent(window, new Event('focus'));
    await screen.findByText('retained@example.com');
    expect(popup.close).not.toHaveBeenCalled();
    expect(onConnectionChange).toHaveBeenLastCalledWith(false);
    status = 'approved';
    fireEvent(window, new Event('focus'));
    await screen.findByText('ChatGPT connected');
    expect(popup.close).toHaveBeenCalled();
  });

  it('refreshes connection status when remote disconnect fails after removing the local credential', async () => {
    let connected = true;
    vi.stubGlobal(
      'fetch',
      vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
        if (init?.method === 'DELETE') {
          connected = false;
          return Promise.resolve(json({ error: 'Remote revocation failed.' }, 502));
        }
        return Promise.resolve(
          json({
            connected,
            plan_enabled: connected,
            email: 'operator@example.com',
            status: connected ? 'approved' : 'disconnected',
          }),
        );
      }),
    );
    const onConnectionChange = vi.fn();
    render(<ChatGPTPlanConnection onConnectionChange={onConnectionChange} />);
    fireEvent.click(await screen.findByRole('button', { name: 'Disconnect ChatGPT' }));
    expect(await screen.findByRole('alert')).toHaveProperty(
      'textContent',
      'Remote revocation failed.',
    );
    expect(onConnectionChange).toHaveBeenLastCalledWith(false);
    expect(screen.queryByRole('button', { name: 'Disconnect ChatGPT' })).toBeNull();
  });

  it('offers reconnect when stored credentials appear connected but require renewed consent', async () => {
    const popup = { location: { href: '' }, opener: window, close: vi.fn() };
    vi.spyOn(window, 'open').mockReturnValue(popup as unknown as Window);
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) =>
        Promise.resolve(
          json(
            urlOf(input).endsWith('/login')
              ? {
                  auth_url: authURL,
                  status: 'authorization_required',
                }
              : {
                  connected: true,
                  email: 'operator@example.com',
                  plan_enabled: true,
                  status: 'approved',
                },
          ),
        ),
      ),
    );
    render(<ChatGPTPlanConnection />);
    fireEvent.click(await screen.findByRole('button', { name: 'Reconnect ChatGPT' }));
    await waitFor(() => {
      expect(popup.location.href).toBe(`${window.location.origin}${authURL}`);
    });
    expect(
      screen.getByText(
        'Complete sign-in in the Aura browser window. This page updates automatically.',
      ),
    ).toBeDefined();
  });

  it('surfaces status failures, then retries without exposing a stale account', async () => {
    let fails = true;
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          fails
            ? json({ error: 'Your session expired.' }, 401)
            : json({ connected: false, email: '', plan_enabled: false, status: 'disconnected' }),
        ),
      ),
    );
    render(<ChatGPTPlanConnection onConnectionChange={vi.fn()} />);
    expect(await screen.findByRole('alert')).toHaveProperty('textContent', 'Your session expired.');
    fails = false;
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    await screen.findByText('Connect ChatGPT to see the models available with your plan.');
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('ends polling after the login deadline and aborts requests when unmounted', async () => {
    vi.useFakeTimers();
    const fetchMock = vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
      expect(init?.signal).toBeDefined();
      return Promise.resolve(
        json({
          connected: false,
          email: '',
          plan_enabled: false,
          status: 'authorization_required',
        }),
      );
    });
    vi.stubGlobal('fetch', fetchMock);
    const view = render(<ChatGPTPlanConnection onConnectionChange={vi.fn()} />);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(600_001);
    });
    expect(screen.getByRole('alert').textContent).toContain('Sign-in timed out');
    const count = fetchMock.mock.calls.length;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_000);
    });
    expect(fetchMock).toHaveBeenCalledTimes(count);
    view.unmount();
    expect(fetchMock.mock.calls[0]?.[1]?.signal?.aborted).toBe(true);
  });
});
