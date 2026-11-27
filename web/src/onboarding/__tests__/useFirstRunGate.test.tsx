import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import { fetchOnboardingStatus, type OnboardingStatus } from '../onboardingApi';
import { useFirstRunGate } from '../useFirstRunGate';

vi.mock('../onboardingApi', () => ({ fetchOnboardingStatus: vi.fn() }));

// useFirstRunGate decides when AppShell shows first-run setup: once per session, for a profile
// still owed or a route step an admin must take, or on a ?onboarding=1 link.

const NOTHING_OWED: OnboardingStatus = {
  required: false,
  completed: true,
  skipped: false,
  routeRequired: false,
};

function gate(linkRequested = false) {
  const onOpen = vi.fn();
  const clearLink = vi.fn();
  const hook = renderHook(() => useFirstRunGate({ linkRequested, onOpen, clearLink }));
  return { ...hook, onOpen, clearLink };
}

describe('useFirstRunGate', () => {
  afterEach(() => {
    vi.clearAllMocks();
    sessionStorage.clear();
  });

  it.each([
    ['the profile is still owed', { ...NOTHING_OWED, required: true, completed: false }],
    ['only the route step is required', { ...NOTHING_OWED, routeRequired: true }],
  ])('opens once when %s, carrying the status', async (_case, status) => {
    vi.mocked(fetchOnboardingStatus).mockResolvedValue(status);
    const { result, onOpen } = gate();

    await waitFor(() => {
      expect(result.current.open).toBe(true);
    });
    expect(result.current.status).toEqual(status);
    expect(onOpen).toHaveBeenCalledTimes(1);
  });

  it('stays closed when nothing is owed', async () => {
    vi.mocked(fetchOnboardingStatus).mockResolvedValue(NOTHING_OWED);
    const { result, onOpen } = gate();

    await waitFor(() => {
      expect(fetchOnboardingStatus).toHaveBeenCalledTimes(1);
    });
    await act(async () => {
      await Promise.resolve();
    });
    expect(result.current.open).toBe(false);
    expect(onOpen).not.toHaveBeenCalled();
  });

  it('opens from a ?onboarding=1 link once the status is read, and clears the link', async () => {
    const status = { ...NOTHING_OWED, routeRequired: true };
    vi.mocked(fetchOnboardingStatus).mockResolvedValue(status);
    const { result, clearLink } = gate(true);

    expect(clearLink).toHaveBeenCalledTimes(1);
    await waitFor(() => {
      expect(result.current.open).toBe(true);
    });
    expect(result.current.status).toEqual(status);
  });

  it('still opens from a link when the status cannot be read', async () => {
    vi.mocked(fetchOnboardingStatus).mockRejectedValue(new Error('HTTP 502'));
    const { result } = gate(true);

    await waitFor(() => {
      expect(result.current.open).toBe(true);
    });
    expect(result.current.status).toBeUndefined();
  });

  it('closes', async () => {
    vi.mocked(fetchOnboardingStatus).mockResolvedValue({ ...NOTHING_OWED, required: true });
    const { result } = gate();
    await waitFor(() => {
      expect(result.current.open).toBe(true);
    });

    act(() => {
      result.current.close();
    });
    expect(result.current.open).toBe(false);
  });

  // A reload used to open it again over the cockpit, a server's form included (2026-11-27).
  it('stays closed for the rest of the session once closed', async () => {
    vi.mocked(fetchOnboardingStatus).mockResolvedValue({ ...NOTHING_OWED, required: true });
    const first = gate();
    await waitFor(() => {
      expect(first.result.current.open).toBe(true);
    });
    act(() => {
      first.result.current.close();
    });
    first.unmount();

    const reloaded = gate();
    await waitFor(() => {
      expect(fetchOnboardingStatus).toHaveBeenCalledTimes(2);
    });
    await act(async () => {
      await Promise.resolve();
    });
    expect(reloaded.result.current.open).toBe(false);
    expect(reloaded.onOpen).not.toHaveBeenCalled();
  });

  it.each([
    ['a required route step', { ...NOTHING_OWED, routeRequired: true }, false],
    ['a ?onboarding=1 link', { ...NOTHING_OWED, required: true }, true],
  ])('opens after a dismissal for %s', async (_case, status, linkRequested) => {
    sessionStorage.setItem('aura.onboarding.dismissed', '1');
    vi.mocked(fetchOnboardingStatus).mockResolvedValue(status);
    const { result } = gate(linkRequested);

    await waitFor(() => {
      expect(result.current.open).toBe(true);
    });
  });
});
