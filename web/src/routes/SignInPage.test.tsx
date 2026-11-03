import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import '../i18n/i18n';
import { SignInPage } from './SignInPage';

const session = 'signin-0123456789abcdef01234567';

function urlOf(input: RequestInfo | URL): string {
  return typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
}

function setup(options: { status?: string; error?: string; streamFails?: boolean } = {}) {
  const close = vi.spyOn(window, 'close').mockImplementation(() => undefined);
  const fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = urlOf(input);
    if (init?.method === 'DELETE') return Promise.resolve(new Response(null, { status: 204 }));
    if (url.endsWith('.aura-ready')) {
      return options.streamFails
        ? Promise.reject(new TypeError('Failed to fetch'))
        : Promise.resolve(new Response(null, { status: 204 }));
    }
    return Promise.resolve(
      new Response(
        JSON.stringify(
          url.includes('/api/sign-in/')
            ? { cockpitOrigin: window.location.origin, streamOrigin: 'https://localhost:8444' }
            : {
                flowId: 'flow-1',
                server: 'canva',
                status: options.status ?? 'authorization_required',
                error: options.error,
              },
        ),
      ),
    );
  });
  vi.stubGlobal('fetch', fetch);
  render(
    <MemoryRouter initialEntries={[`/signing-in/${session}?flow=flow-1`]}>
      <Routes>
        <Route path="/signing-in/:session" element={<SignInPage />} />
      </Routes>
    </MemoryRouter>,
  );
  return { close, fetch };
}

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('sign-in page', () => {
  it('closes itself when the flow ends', async () => {
    for (const status of ['approved', 'error']) {
      const { close } = setup({ status });
      await waitFor(() => {
        expect(close).toHaveBeenCalled();
      });
    }
  });

  it('cancels the flow when the tab goes', async () => {
    const { fetch } = setup();
    await screen.findByTitle('Sign-in desktop');
    fireEvent(window, new Event('pagehide'));
    expect(fetch).toHaveBeenCalledWith(
      `/api/sign-in/${session}`,
      expect.objectContaining({ method: 'DELETE', keepalive: true }),
    );
  });

  it('offers the paste-back when the stream fails', async () => {
    setup();
    const frame = await screen.findByTitle('Sign-in desktop');
    fireEvent.error(frame);
    expect(await screen.findByRole('link', { name: /Use your own browser/ })).not.toBeNull();
  });

  it('says try again when the time ran out', async () => {
    setup({ status: 'error', error: 'authorization expired' });
    expect(await screen.findByText(/time ran out.*try again/i)).not.toBeNull();
  });

  it('offers certificate onboarding before displaying an unreachable frame', async () => {
    const { fetch } = setup({ streamFails: true });
    const trust = await screen.findByRole('link', { name: /Open the desktop address/ });
    expect(trust.getAttribute('href')).toBe(`https://localhost:8444/sign-in/${session}/`);
    expect(trust.getAttribute('target')).toBe('_blank');
    expect(screen.queryByTitle('Sign-in desktop')).toBeNull();
    expect(screen.getByRole('link', { name: /Use your own browser/ })).not.toBeNull();
    expect(fetch).toHaveBeenCalledWith(
      `https://localhost:8444/sign-in/${session}/.aura-ready`,
      expect.objectContaining({ credentials: 'include', mode: 'cors' }),
    );
  });

  it('polls every two seconds and confines a reachable stream to its own origin', async () => {
    vi.useFakeTimers();
    const { fetch } = setup();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    const frame = screen.getByTitle('Sign-in desktop');
    expect(frame.getAttribute('sandbox')).toBe('allow-scripts allow-same-origin');
    expect(frame.getAttribute('allow')).toBe('clipboard-read; clipboard-write');
    expect(frame.getAttribute('src')).toBe(`https://localhost:8444/sign-in/${session}/`);
    const before = fetch.mock.calls.filter(([url]) => urlOf(url).includes('/flow/')).length;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2000);
    });
    expect(fetch.mock.calls.filter(([url]) => urlOf(url).includes('/flow/'))).toHaveLength(
      before + 1,
    );
  });
});
