import { act, renderHook } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { useSurfaceRestore } from '../useSurfaceRestore';

// §3.1c "one heavy surface at a time, remembered" — the shell-level intent-restore
// reducer. Distinct from the mode-persistence hook (useSurfaceIntent) and from the
// DOM focus-restore the Drawer already does. Pure state logic, unit-tested here.
describe('useSurfaceRestore (§3.1c intent-aware restore)', () => {
  it('starts idle: nothing open, nothing remembered', () => {
    const { result } = renderHook(() => useSurfaceRestore());
    expect(result.current.navOpen).toBe(false);
    expect(result.current.overlayOpen).toBe(false);
    expect(result.current.navWasOpenBeforeOverlay).toBe(false);
  });

  it('opening the overlay while the nav is open auto-closes the nav AND remembers it', () => {
    const { result } = renderHook(() => useSurfaceRestore());
    act(() => {
      result.current.openNav();
    });
    expect(result.current.navOpen).toBe(true);
    act(() => {
      result.current.openOverlay();
    });
    expect(result.current.overlayOpen).toBe(true);
    expect(result.current.navOpen).toBe(false);
    expect(result.current.navWasOpenBeforeOverlay).toBe(true);
  });

  it('closing the overlay explicitly restores the remembered nav', () => {
    const { result } = renderHook(() => useSurfaceRestore());
    act(() => {
      result.current.openNav();
    });
    act(() => {
      result.current.openOverlay();
    });
    act(() => {
      result.current.closeOverlay('explicit');
    });
    expect(result.current.overlayOpen).toBe(false);
    expect(result.current.navOpen).toBe(true);
    expect(result.current.navWasOpenBeforeOverlay).toBe(false);
  });

  it('closing the overlay via swipe does NOT restore the nav', () => {
    const { result } = renderHook(() => useSurfaceRestore());
    act(() => {
      result.current.openNav();
    });
    act(() => {
      result.current.openOverlay();
    });
    act(() => {
      result.current.closeOverlay('swipe');
    });
    expect(result.current.overlayOpen).toBe(false);
    expect(result.current.navOpen).toBe(false);
    expect(result.current.navWasOpenBeforeOverlay).toBe(false);
  });

  it('opening the overlay when the nav is already closed remembers nothing (no-op restore)', () => {
    const { result } = renderHook(() => useSurfaceRestore());
    act(() => {
      result.current.openOverlay();
    });
    expect(result.current.navOpen).toBe(false);
    expect(result.current.navWasOpenBeforeOverlay).toBe(false);
    // Explicit close has nothing to restore — the nav stays closed.
    act(() => {
      result.current.closeOverlay('explicit');
    });
    expect(result.current.overlayOpen).toBe(false);
    expect(result.current.navOpen).toBe(false);
  });

  it('opening the nav while the overlay is open enforces one heavy surface (overlay yields)', () => {
    const { result } = renderHook(() => useSurfaceRestore());
    act(() => {
      result.current.openOverlay();
    });
    act(() => {
      result.current.openNav();
    });
    expect(result.current.navOpen).toBe(true);
    expect(result.current.overlayOpen).toBe(false);
    // Re-opening the overlay now remembers the freshly-opened nav.
    act(() => {
      result.current.openOverlay();
    });
    expect(result.current.navWasOpenBeforeOverlay).toBe(true);
  });

  it('closeNav clears the open nav without touching the remembered flag default', () => {
    const { result } = renderHook(() => useSurfaceRestore());
    act(() => {
      result.current.openNav();
    });
    act(() => {
      result.current.closeNav();
    });
    expect(result.current.navOpen).toBe(false);
    expect(result.current.navWasOpenBeforeOverlay).toBe(false);
  });
});
