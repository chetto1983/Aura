import { createElement, type ReactNode } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { AssetSourceContext, useAssetSource, type AssetSource } from './assetSourceContext';
import { useAssetContent } from './useAssetContent';

// assetSourceContext (R-05): the seam behind every artifact renderer's byte fetch. The default
// (no provider mounted) MUST reproduce today's identity-scoped fetch exactly — that is the
// whole argument against prop-threading. A mounted provider (the future public share page)
// resolves through a token-scoped URL with `credentials: 'omit'` instead.

function stubFetch(): ReturnType<typeof vi.fn> {
  const fetchMock = vi.fn(() =>
    Promise.resolve({
      ok: true,
      status: 200,
      text: () => Promise.resolve('body'),
    } as unknown as Response),
  );
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('useAssetSource (no provider mounted)', () => {
  it('resolves the identity-scoped download URL with same-origin credentials', () => {
    const { result } = renderHook(() => useAssetSource());
    expect(result.current.assetUrl('asset-1')).toBe('/api/assets/asset-1/download');
    expect(result.current.credentials).toBe('same-origin');
  });

  it('percent-encodes the asset id — never interpolated raw', () => {
    const { result } = renderHook(() => useAssetSource());
    expect(result.current.assetUrl('a b/c#d')).toBe(
      `/api/assets/${encodeURIComponent('a b/c#d')}/download`,
    );
    expect(result.current.assetUrl('a b/c#d')).not.toContain('a b/c#d');
  });
});

describe('useAssetSource (provider mounted)', () => {
  it('resolves through the provided token-scoped resolver with omit credentials', () => {
    const tokenScoped: AssetSource = {
      assetUrl: (assetId) => `/s/tok123/asset/${encodeURIComponent(assetId)}`,
      credentials: 'omit',
    };
    const wrapper = ({ children }: { children: ReactNode }) =>
      createElement(AssetSourceContext.Provider, { value: tokenScoped }, children);
    const { result } = renderHook(() => useAssetSource(), { wrapper });
    expect(result.current.assetUrl('asset-9')).toBe('/s/tok123/asset/asset-9');
    expect(result.current.credentials).toBe('omit');
  });
});

describe('useAssetContent + useAssetSource integration', () => {
  it('fetches through the default identity-scoped URL with same-origin credentials when no provider is mounted', async () => {
    const fetchMock = stubFetch();
    renderHook(() => useAssetContent('asset-7', 'text'));

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalled();
    });
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/api/assets/asset-7/download');
    expect(init.credentials).toBe('same-origin');
  });

  it('fetches whatever a mounted provider resolves, with the resolved credentials', async () => {
    const fetchMock = stubFetch();
    const tokenScoped: AssetSource = {
      assetUrl: (assetId) => `/s/tok123/asset/${encodeURIComponent(assetId)}`,
      credentials: 'omit',
    };
    const wrapper = ({ children }: { children: ReactNode }) =>
      createElement(AssetSourceContext.Provider, { value: tokenScoped }, children);
    renderHook(() => useAssetContent('asset-7', 'text'), { wrapper });

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalled();
    });
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/s/tok123/asset/asset-7');
    expect(init.credentials).toBe('omit');
  });
});
