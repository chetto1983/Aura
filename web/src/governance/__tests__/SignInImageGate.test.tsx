import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n';
import { SignInImageGate } from '../SignInImageGate';

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('sign-in image gate', () => {
  it('asks before the first download and shows its progress', async () => {
    let pulling = false;
    const fetch = vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === 'POST') pulling = true;
      return Promise.resolve(
        new Response(
          JSON.stringify({ present: false, pulling, done: pulling ? 50 : 0, total: 100 }),
        ),
      );
    });
    vi.stubGlobal('fetch', fetch);
    render(<SignInImageGate onReady={vi.fn()} onPaste={vi.fn()} />);
    expect(screen.getByText(/1.17 GB/)).not.toBeNull();
    expect(fetch.mock.calls.some(([, init]) => init?.method === 'POST')).toBe(false);
    fireEvent.click(await screen.findByRole('button', { name: /Download/ }));
    await waitFor(() => {
      expect(screen.getByRole('progressbar').getAttribute('aria-valuenow')).toBe('50');
    });
  });

  it('opens the sign-in page once the image is there', async () => {
    vi.useFakeTimers();
    let ready = false;
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          new Response(JSON.stringify({ present: ready, pulling: !ready, done: 0, total: 100 })),
        ),
      ),
    );
    const start = vi.fn();
    render(<SignInImageGate onReady={start} onPaste={vi.fn()} />);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    ready = true;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1500);
    });
    expect(start).toHaveBeenCalledTimes(1);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1500);
    });
    expect(start).toHaveBeenCalledTimes(1);
  });

  it('offers the paste-back when the image cannot be had', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          new Response(
            JSON.stringify({ present: false, pulling: false, error: 'download failed' }),
          ),
        ),
      ),
    );
    const paste = vi.fn();
    render(<SignInImageGate onReady={vi.fn()} onPaste={paste} />);
    expect(await screen.findByText('download failed')).not.toBeNull();
    fireEvent.click(screen.getByRole('button', { name: /Use your own browser/ }));
    expect(paste).toHaveBeenCalledTimes(1);
  });
});
