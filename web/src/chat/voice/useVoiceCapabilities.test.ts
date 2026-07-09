import { afterEach, describe, expect, it, vi } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { useVoiceCapabilities } from './useVoiceCapabilities';

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('useVoiceCapabilities', () => {
  it('resolves {tts,stt} from a 200 (same-origin cookie) and fetches exactly once', async () => {
    const fetchMock = vi.fn<typeof fetch>(() =>
      Promise.resolve(jsonResponse({ tts: true, stt: false })),
    );
    vi.stubGlobal('fetch', fetchMock);

    const { result } = renderHook(() => useVoiceCapabilities());
    // Default while the probe is in flight.
    expect(result.current).toEqual({ tts: false, stt: false });

    await waitFor(() => {
      expect(result.current).toEqual({ tts: true, stt: false });
    });
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/voice/capabilities',
      expect.objectContaining({
        credentials: 'same-origin',
        headers: { Accept: 'application/json' },
      }),
    );
  });

  it('defaults to {false,false} on a non-2xx (never adopts the body)', async () => {
    const fetchMock = vi.fn<typeof fetch>(() =>
      Promise.resolve(jsonResponse({ tts: true, stt: true }, 503)),
    );
    vi.stubGlobal('fetch', fetchMock);

    const { result } = renderHook(() => useVoiceCapabilities());
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalled();
    });
    await Promise.resolve();
    expect(result.current).toEqual({ tts: false, stt: false });
  });

  it('defaults to {false,false} when the fetch rejects', async () => {
    const fetchMock = vi.fn<typeof fetch>(() => Promise.reject(new Error('offline')));
    vi.stubGlobal('fetch', fetchMock);

    const { result } = renderHook(() => useVoiceCapabilities());
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalled();
    });
    await Promise.resolve();
    expect(result.current).toEqual({ tts: false, stt: false });
  });

  it('coerces non-boolean flags to false (only strict true enables)', async () => {
    // tts flips to true (proving setCaps ran) while the truthy-but-not-true stt is coerced off.
    vi.stubGlobal(
      'fetch',
      vi.fn<typeof fetch>(() => Promise.resolve(jsonResponse({ tts: true, stt: 1 }))),
    );
    const { result } = renderHook(() => useVoiceCapabilities());
    await waitFor(() => {
      expect(result.current).toEqual({ tts: true, stt: false });
    });
  });
});
