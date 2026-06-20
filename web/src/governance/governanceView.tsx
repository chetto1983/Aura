/* eslint-disable react-refresh/only-export-components */
import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';

// governanceView.tsx holds the shared view-state primitives every governance board reuses,
// so the loading / empty / error / error-auth contract is written ONCE (CLAUDE.md "reusable
// code" — never duplicate the GraphExplorer state blocks across three boards). It mirrors the
// GraphExplorer state machine: an expired-session 401 throws `Error("HTTP 401")` in the data
// layer, which TanStack Query surfaces here as a VISIBLE error-auth state, never a blank board
// (threat T-28-03-04). Every backend-supplied string is rendered by the boards as React-escaped
// text — this module only renders translated copy.

/** The five board view states (28-UI-SPEC §State contract), mirroring GraphExplorer's
 * ViewStatus. `error-auth` is the expired-session path; `error` is any other failure. */
export type BoardStatus = 'loading' | 'populated' | 'empty' | 'error' | 'error-auth';

/** isAuthError matches the data-layer's `Error("HTTP 401")` so a board renders the
 * sign-in-again copy instead of the generic error copy (the GraphExplorer precedent). */
export function isAuthError(err: unknown): boolean {
  return err instanceof Error && err.message === 'HTTP 401';
}

/** boardStatus collapses a TanStack-style query result into a BoardStatus. `isEmpty` lets
 * each board decide emptiness (e.g. zero rows). A 401 → 'error-auth'; any other error →
 * 'error'; loading before data → 'loading'. */
export function boardStatus(opts: {
  readonly isLoading: boolean;
  readonly isError: boolean;
  readonly error: unknown;
  readonly isEmpty: boolean;
}): BoardStatus {
  if (opts.isError) {
    return isAuthError(opts.error) ? 'error-auth' : 'error';
  }
  if (opts.isLoading) {
    return 'loading';
  }
  return opts.isEmpty ? 'empty' : 'populated';
}

export interface BoardStateViewProps {
  readonly status: BoardStatus;
  readonly emptyHeading: string;
  readonly emptyBody: string;
  readonly onRetry: () => void;
  /** Rendered only when status === 'populated'. */
  readonly children: ReactNode;
}

/** BoardStateView renders the loading / empty / error / error-auth frames (token classes
 * copied from GraphExplorer) and only mounts `children` once populated. role="status" on the
 * loading region and role="alert" on the error regions keep the states announced to AT. */
export function BoardStateView({
  status,
  emptyHeading,
  emptyBody,
  onRetry,
  children,
}: BoardStateViewProps) {
  const { t } = useTranslation();

  if (status === 'loading') {
    return (
      <div role="status" className="grid h-full place-items-center p-8 text-sm text-text-muted">
        {t('governance.loading')}
      </div>
    );
  }

  if (status === 'error-auth') {
    return (
      <div role="alert" className="grid h-full place-items-center p-8 text-center">
        <p className="max-w-md text-[15.5px] text-danger">{t('governance.authExpired')}</p>
      </div>
    );
  }

  if (status === 'error') {
    return (
      <div role="alert" className="grid h-full place-items-center p-8 text-center">
        <div className="flex max-w-md flex-col items-center gap-3">
          <p className="text-[15.5px] text-danger">{t('governance.error')}</p>
          <button
            type="button"
            onClick={onRetry}
            className="min-h-[44px] rounded-md border border-border bg-surface-2 px-4 py-2 text-[13px] font-semibold text-text transition-colors hover:border-border-strong"
          >
            {t('governance.retry')}
          </button>
        </div>
      </div>
    );
  }

  if (status === 'empty') {
    return (
      <div className="grid h-full place-items-center p-8 text-center">
        <div className="flex max-w-sm flex-col items-center gap-3">
          <span aria-hidden="true" className="text-4xl leading-none text-accent-text opacity-70">
            ◍
          </span>
          <h2 className="font-display text-[22px] font-semibold text-text">{emptyHeading}</h2>
          <p className="text-[15.5px] leading-relaxed text-text-muted">{emptyBody}</p>
        </div>
      </div>
    );
  }

  return <>{children}</>;
}
