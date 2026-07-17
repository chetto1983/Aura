import { describe, expect, it } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { useSharePanel } from './useSharePanel';

// useSharePanel.test.ts — covers every <behavior> row from 37F-14-PLAN.md Task 2, including the
// no-persistence assertion (open -> remount -> closed), which is the mutant a copied
// localStorage block from useArtifactsPanel would introduce (T-37F-68).

describe('useSharePanel', () => {
  it('starts closed', () => {
    const { result } = renderHook(() => useSharePanel());
    expect(result.current.shareModalState).toBe('closed');
  });

  it('openShare sets the state to open; closeShare returns it to closed', () => {
    const { result } = renderHook(() => useSharePanel());

    act(() => {
      result.current.openShare();
    });
    expect(result.current.shareModalState).toBe('open');

    act(() => {
      result.current.closeShare();
    });
    expect(result.current.shareModalState).toBe('closed');
  });

  it('does not persist: a remount after opening starts closed again', () => {
    const first = renderHook(() => useSharePanel());
    act(() => {
      first.result.current.openShare();
    });
    expect(first.result.current.shareModalState).toBe('open');
    first.unmount();

    const second = renderHook(() => useSharePanel());
    expect(second.result.current.shareModalState).toBe('closed');
  });

  it('grep sentinel: never touches localStorage/sessionStorage', () => {
    // Guards against a copy-paste of useArtifactsPanel's persistence block regressing this hook.
    expect(useSharePanel.toString()).not.toMatch(/localStorage|sessionStorage/);
  });

  it('returns referentially stable callbacks across re-renders', () => {
    const { result, rerender } = renderHook(() => useSharePanel());
    const { openShare: openBefore, closeShare: closeBefore } = result.current;
    rerender();
    expect(result.current.openShare).toBe(openBefore);
    expect(result.current.closeShare).toBe(closeBefore);
  });
});
