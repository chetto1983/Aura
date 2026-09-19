import { renderHook } from '@testing-library/react';
import { StrictMode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useObjectUrl } from '../useObjectUrl';

const create = vi.fn<(blob: Blob) => string>();
const revoke = vi.fn<(url: string) => void>();

beforeEach(() => {
  vi.useFakeTimers();
  let minted = 0;
  create.mockReset().mockImplementation(() => {
    minted += 1;
    return `blob:${String(minted)}`;
  });
  revoke.mockReset();
  // jsdom has no object URLs; assign them rather than replacing the URL class jsdom uses.
  Object.assign(URL, { createObjectURL: create, revokeObjectURL: revoke });
});

afterEach(() => {
  vi.useRealTimers();
});

describe('useObjectUrl', () => {
  it('mints one URL per blob and revokes it once the blob changes or the owner unmounts', () => {
    const first = new Blob(['a']);
    const { result, rerender, unmount } = renderHook(({ blob }) => useObjectUrl(blob), {
      initialProps: { blob: first },
    });
    expect(result.current).toBe('blob:1');
    rerender({ blob: first });
    expect(create).toHaveBeenCalledTimes(1);
    rerender({ blob: new Blob(['b']) });
    expect(result.current).toBe('blob:2');
    vi.runAllTimers();
    expect(revoke).toHaveBeenCalledExactlyOnceWith('blob:1');
    unmount();
    vi.runAllTimers();
    expect(revoke).toHaveBeenLastCalledWith('blob:2');
  });

  it('keeps the URL it returns alive through Strict Mode re-running its effects', () => {
    const blob = new Blob(['a']);
    const { result, unmount } = renderHook(() => useObjectUrl(blob), { wrapper: StrictMode });
    vi.runAllTimers();
    expect(revoke).not.toHaveBeenCalled();
    unmount();
    vi.runAllTimers();
    expect(revoke).toHaveBeenCalledExactlyOnceWith(result.current);
  });
});
