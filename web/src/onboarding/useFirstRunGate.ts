import { useCallback, useEffect, useRef, useState } from 'react';
import { fetchOnboardingStatus, type OnboardingStatus } from './onboardingApi';

export interface FirstRunGateOptions {
  /** A ?onboarding=1 link asked for the setup. */
  readonly linkRequested: boolean;
  /** Called once, when the setup opens. */
  readonly onOpen: () => void;
  /** Takes the ?onboarding=1 parameter out of the URL. */
  readonly clearLink: () => void;
}

export interface FirstRunGate {
  readonly open: boolean;
  /** What the daemon said when the setup opened; undefined when it could not be read. */
  readonly status: OnboardingStatus | undefined;
  readonly close: () => void;
}

/**
 * useFirstRunGate opens first-run setup once per session: when the daemon says the profile is
 * still owed or an admin's route step is required, or when a ?onboarding=1 link asked for it. A
 * link opens it only once the status is read, or fails to be, so the route step knows whether it
 * is required.
 */
export function useFirstRunGate({
  linkRequested,
  onOpen,
  clearLink,
}: FirstRunGateOptions): FirstRunGate {
  const [open, setOpen] = useState(false);
  const [status, setStatus] = useState<OnboardingStatus | undefined>(undefined);
  const opened = useRef(false);
  const linked = useRef(false);

  useEffect(() => {
    if (!linkRequested || linked.current) return;
    linked.current = true;
    clearLink();
  }, [clearLink, linkRequested]);

  useEffect(() => {
    let cancelled = false;
    const openWith = (next: OnboardingStatus | undefined) => {
      if (cancelled || opened.current) return;
      opened.current = true;
      setStatus(next);
      setOpen(true);
      onOpen();
    };
    void fetchOnboardingStatus().then(
      (next) => {
        if (linked.current || next.required || next.routeRequired) openWith(next);
      },
      () => {
        if (linked.current) openWith(undefined);
      },
    );
    return () => {
      cancelled = true;
    };
  }, [onOpen]);

  const close = useCallback(() => {
    setOpen(false);
  }, []);
  return { open, status, close };
}
