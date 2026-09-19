import { renderHook, waitFor } from '@testing-library/react';
import { StrictMode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { useObjectUrl } from '../useObjectUrl';

const create = vi.fn<(blob: Blob) => string>();
const revoke = vi.fn<(url: string) => void>();

beforeEach(() => {
  let minted = 0;
  create.mockReset().mockImplementation(() => {
    minted += 1;
    return `blob:${String(minted)}`;
  });
  revoke.mockReset();
  // jsdom has no object URLs; assign them rather than replacing the URL class jsdom uses.
  Object.assign(URL, { createObjectURL: create, revokeObjectURL: revoke });
});

function minted(): string[] {
  return create.mock.results.map((result) => String(result.value)).sort();
}

function revoked(): string[] {
  return revoke.mock.calls.map(([url]) => url).sort();
}

describe('useObjectUrl', () => {
  it('hands out nothing until committed, then one URL per blob and never a stale one', async () => {
    const seen: (string | undefined)[] = [];
    const first = new Blob(['a']);
    const { result, rerender, unmount } = renderHook(
      ({ blob }) => {
        const url = useObjectUrl(blob);
        seen.push(url);
        return url;
      },
      { initialProps: { blob: first } },
    );
    await waitFor(() => {
      expect(result.current).toBe('blob:1');
    });
    expect(seen).toEqual([undefined, 'blob:1']);
    rerender({ blob: first });
    expect(create).toHaveBeenCalledTimes(1);
    seen.length = 0;
    rerender({ blob: new Blob(['b']) });
    await waitFor(() => {
      expect(result.current).toBe('blob:2');
    });
    expect(seen).toEqual([undefined, 'blob:2']);
    expect(revoked()).toEqual(['blob:1']);
    unmount();
    expect(revoked()).toEqual(minted());
  });

  it("revokes every URL it mints, through Strict Mode's double effects too", async () => {
    const blob = new Blob(['a']);
    const { result, unmount } = renderHook(() => useObjectUrl(blob), { wrapper: StrictMode });
    await waitFor(() => {
      expect(result.current).toBeDefined();
    });
    expect(revoke).not.toHaveBeenCalledWith(result.current);
    unmount();
    expect(revoked()).toEqual(minted());
  });
});
